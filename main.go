package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func run() error {
	return runArgs(os.Args[1:], os.Stdin, os.Stdout)
}

func runArgs(args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("skill-manager", flag.ContinueOnError)
	flags.SetOutput(out)
	configPath := flags.String("config", "", "Konfigurationsdatei (Standard: Benutzerkonfiguration/skill-manager/config.json)")
	repo := flags.String("repo", "", "Git-URL (überschreibt Konfiguration nur für diesen Start)")
	source := flags.String("source", "", "Lokaler Repository-Ordner")
	status := flags.Bool("status", false, "Lokalen Status ohne Git-Zugriff anzeigen")
	update := flags.Bool("update", false, "Skill-Quelle aktualisieren und Status anzeigen")
	showVersion := flags.Bool("version", false, "Programmversion anzeigen")
	checkUpdate := flags.Bool("check-update", false, "Nach neuer Programmversion suchen")
	selfUpdate := flags.Bool("self-update", false, "Programm nach Bestätigung aktualisieren")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("keine Positionsargumente erlaubt; siehe --help")
	}
	modes := 0
	for _, enabled := range []bool{*showVersion, *checkUpdate, *selfUpdate, *status || *update} {
		if enabled {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("--version, --check-update und --self-update einzeln und nicht mit --status/--update kombinieren")
	}
	if *showVersion {
		fmt.Fprintln(out, "skill-manager", version)
		return nil
	}
	if *checkUpdate || *selfUpdate {
		return runProgramUpdate(*selfUpdate, in, out)
	}
	if *configPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*configPath = filepath.Join(configDir, "skill-manager", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	fresh := errors.Is(err, os.ErrNotExist)
	if err != nil && !fresh {
		return err
	}
	if fresh {
		cfg.Source = defaultSource(home)
	}
	if *source != "" {
		cfg.Source = *source
	}
	cfg.Source, err = filepath.Abs(cfg.Source)
	if err != nil {
		return err
	}
	if *repo != "" {
		cfg.URL = *repo
	}
	if fresh && cfg.URL == "" {
		if *status || *update {
			return errors.New("erste Einrichtung benötigt --repo <Git-URL>")
		}
		candidate, _ := git(cfg.Source, "remote", "get-url", "origin")
		fmt.Fprintf(out, "Git-URL [%s]: ", candidate)
		line, readErr := bufio.NewReader(in).ReadString('\n')
		if readErr != nil && strings.TrimSpace(line) == "" {
			return errors.New("keine Eingabe; alternativ --repo <Git-URL> angeben")
		}
		cfg.URL = strings.TrimSpace(line)
		if cfg.URL == "" {
			cfg.URL = candidate
		}
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if fresh {
		if err := saveConfig(*configPath, cfg); err != nil {
			return err
		}
	}
	targets := targetDirs(home)
	if *status || *update {
		if *update {
			message, err := syncRepository(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, message)
		}
		skills, err := scanSkills(cfg.Source)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Quelle: %s\n", cfg.Source)
		for _, s := range skills {
			fmt.Fprintf(out, "%s\tCodex: %s\tClaude: %s\n", s.Name, linkStatus(s.Path, filepath.Join(targets[0], s.Name)), linkStatus(s.Path, filepath.Join(targets[1], s.Name)))
		}
		if len(skills) == 0 {
			fmt.Fprintln(out, "Keine Skills unter skills/*/SKILL.md gefunden.")
		}
		return nil
	}
	final, err := tea.NewProgram(newModel(cfg, targets)).Run()
	if m, ok := final.(model); ok && m.programInstalled {
		fmt.Fprintln(out, "Programm-Update installiert. Bitte Skill Manager neu starten.")
	}
	return err
}
