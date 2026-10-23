package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type row struct {
	skill
	statuses [2]string
}

type resultMsg struct {
	message string
	rows    []row
	err     error
}

type programCheckMsg struct {
	update programUpdate
	err    error
}
type programInstallMsg struct{ err error }

func checkProgramCmd() tea.Cmd {
	return func() tea.Msg {
		update, err := checkProgramUpdate(context.Background(), http.DefaultClient, releaseAPI, version, runtime.GOOS, runtime.GOARCH)
		return programCheckMsg{update, err}
	}
}

func installProgramCmd(update programUpdate) tea.Cmd {
	return func() tea.Msg { return programInstallMsg{installProgram(update)} }
}

type model struct {
	cfg                           config
	targets                       [2]string
	rows                          []row
	selected                      map[string]bool
	cursor, target, width, height int
	busy                          bool
	message                       string
	program                       programUpdate
	programMessage                string
	programChecking               bool
	programConfirm                bool
	programInstalling             bool
	programInstalled              bool
}

func newModel(cfg config, targets [2]string) model {
	return model{cfg: cfg, targets: targets, selected: map[string]bool{}, width: 100, height: 24, busy: true, message: "Quelle wird synchronisiert …", programChecking: stableVersion.MatchString(version)}
}

func readRows(cfg config, targets [2]string, message string, operationErr error) resultMsg {
	skills, err := scanSkills(cfg.Source)
	if operationErr == nil {
		operationErr = err
	}
	var rows []row
	for _, s := range skills {
		rows = append(rows, row{s, [2]string{linkStatus(s.Path, filepath.Join(targets[0], s.Name)), linkStatus(s.Path, filepath.Join(targets[1], s.Name))}})
	}
	return resultMsg{message, rows, operationErr}
}

func syncCmd(cfg config, targets [2]string) tea.Cmd {
	return func() tea.Msg {
		message, err := syncRepository(cfg)
		return readRows(cfg, targets, message, err)
	}
}

func (m model) Init() tea.Cmd {
	if stableVersion.MatchString(version) {
		return tea.Batch(syncCmd(m.cfg, m.targets), checkProgramCmd())
	}
	return syncCmd(m.cfg, m.targets)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case programCheckMsg:
		m.programChecking = false
		m.program = msg.update
		if msg.err != nil {
			m.programMessage = "Programm-Update: " + msg.err.Error()
		} else {
			m.programMessage = programUpdateText(msg.update, version)
		}
	case programInstallMsg:
		m.programInstalling = false
		if msg.err != nil {
			m.programMessage = "Programm-Update: " + msg.err.Error()
		} else {
			m.programInstalled = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case resultMsg:
		m.busy, m.rows, m.message = false, msg.rows, msg.message
		if msg.err != nil {
			m.message = "Fehler: " + msg.err.Error()
		}
		m.cursor = max(0, min(m.cursor, len(m.rows)-1))
		present := map[string]bool{}
		for _, r := range m.rows {
			if m.selected[r.Name] {
				present[r.Name] = true
			}
		}
		m.selected = present
	case tea.KeyPressMsg:
		key := msg.String()
		// Do not quit during replacement: let the operation finish and report its result.
		if m.programInstalling {
			return m, nil
		}
		if m.programConfirm {
			m.programConfirm = false
			if key == "j" || key == "y" {
				m.programInstalling = true
				m.programMessage = "Programm wird heruntergeladen und aktualisiert …"
				return m, installProgramCmd(m.program)
			}
			m.programMessage = programUpdateText(m.program, version) + " · Installation abgebrochen."
			return m, nil
		}
		if key == "ctrl+c" || key == "q" {
			return m, tea.Quit
		}
		if key == "v" && !m.programChecking {
			m.programChecking, m.programMessage = true, "Programmversion wird geprüft …"
			return m, checkProgramCmd()
		}
		if m.busy {
			return m, nil
		}
		switch key {
		case "s":
			if m.program.Available && stableVersion.MatchString(version) && !m.programChecking {
				m.programConfirm = true
			}
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(m.rows)-1), m.cursor+1)
		case "tab":
			m.target = (m.target + 1) % 3
		case "space":
			if len(m.rows) > 0 {
				name := m.rows[m.cursor].Name
				m.selected[name] = !m.selected[name]
			}
		case "a":
			all := true
			for _, r := range m.rows {
				all = all && m.selected[r.Name]
			}
			for _, r := range m.rows {
				m.selected[r.Name] = !all
			}
		case "u":
			m.busy, m.message = true, "Quelle wird aktualisiert …"
			return m, syncCmd(m.cfg, m.targets)
		case "r":
			return m, func() tea.Msg { return readRows(m.cfg, m.targets, "Status neu eingelesen.", nil) }
		case "enter", "i":
			if len(m.rows) == 0 {
				return m, nil
			}
			var chosen []skill
			for _, r := range m.rows {
				if m.selected[r.Name] {
					chosen = append(chosen, r.skill)
				}
			}
			if len(chosen) == 0 {
				chosen = append(chosen, m.rows[m.cursor].skill)
			}
			m.busy, m.message = true, "Installation läuft …"
			return m, func() tea.Msg {
				var messages []string
				for _, s := range chosen {
					for i, target := range m.targets {
						if m.target != 2 && m.target != i {
							continue
						}
						status, err := installSkill(s, target)
						if err != nil {
							status = "Fehler: " + err.Error()
						}
						messages = append(messages, fmt.Sprintf("%s (%s): %s", s.Name, []string{"Codex", "Claude"}[i], status))
					}
				}
				return readRows(m.cfg, m.targets, strings.Join(messages, "\n"), nil)
			}
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	var b strings.Builder
	fmt.Fprintf(&b, "Skill Manager %s\nQuelle: %s\nZiel: %s  (Tab wechseln)\n", version, m.cfg.Source, []string{"Codex", "Claude Code", "Beide"}[m.target])
	programLine := m.programMessage
	if m.programConfirm {
		programLine = fmt.Sprintf("Programm %s → %s installieren? [j/N]", version, m.program.Version)
	}
	if programLine == "" {
		if stableVersion.MatchString(version) {
			programLine = "Programmversion wird im Hintergrund geprüft …"
		} else {
			programLine = "Entwicklungsbuild · v zeigt das aktuelle Release"
		}
	}
	fmt.Fprintln(&b, strings.ReplaceAll(safeText(programLine), "\n", " "))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "     Skill                         Codex          Claude")
	visible := max(1, m.height-16)
	start := max(0, m.cursor-visible+1)
	for i := start; i < min(len(m.rows), start+visible); i++ {
		r := m.rows[i]
		cursor, check := " ", " "
		if i == m.cursor {
			cursor = ">"
		}
		if m.selected[r.Name] {
			check = "x"
		}
		fmt.Fprintf(&b, "%s[%s] %-29s %-14s %s\n", cursor, check, ansi.Truncate(safeText(r.Name), 29, "…"), r.statuses[0], r.statuses[1])
	}
	if len(m.rows) == 0 {
		fmt.Fprintln(&b, "Keine Skills gefunden: skills/<name>/SKILL.md anlegen und committen.")
	}
	fmt.Fprintf(&b, "\n%d Skills · ↑/↓ Auswahl · Leertaste markieren · a alle\nEnter installieren · Tab Ziel · u Git-Update · r Status · q Ende\n\n", len(m.rows))
	fmt.Fprintln(&b, "v Programmversion prüfen · s Programm aktualisieren")
	// Keep a bounded message area so large batches do not hide the controls.
	lines := strings.Split(safeText(m.message), "\n")
	for _, line := range lines[:min(3, len(lines))] {
		fmt.Fprintln(&b, line)
	}
	if len(lines) > 3 {
		fmt.Fprintf(&b, "… %d weitere Meldungen; Status in der Tabelle.\n", len(lines)-3)
	}
	var linesOut []string
	for _, line := range strings.Split(b.String(), "\n") {
		linesOut = append(linesOut, ansi.Truncate(line, max(1, m.width), "…"))
	}
	v := tea.NewView(strings.Join(linesOut, "\n"))
	v.AltScreen = true
	return v
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 32 && r != '\n') || (r >= 127 && r < 160) {
			return -1
		}
		return r
	}, s)
}
