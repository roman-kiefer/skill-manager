package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(text string) tea.KeyPressMsg {
	if text == "enter" {
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	if text == "tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	if text == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Code: rune(text[0]), Text: text}
}

func TestTUISelectionAndInstallBoth(t *testing.T) {
	root := t.TempDir()
	cfg := config{URL: "unused", Source: filepath.Join(root, "library")}
	writeTestFile(t, filepath.Join(cfg.Source, "skills", "alpha", "SKILL.md"), "# Alpha")
	writeTestFile(t, filepath.Join(cfg.Source, "skills", "beta", "SKILL.md"), "# Beta")
	targets := targetDirs(filepath.Join(root, "home"))
	m := newModel(cfg, targets)
	next, _ := m.Update(readRows(cfg, targets, "ready", nil))
	m = next.(model)
	for _, input := range []string{"space", "j", "space", "tab", "tab"} {
		next, _ = m.Update(key(input))
		m = next.(model)
	}
	if !m.selected["alpha"] || !m.selected["beta"] || m.target != 2 {
		t.Fatalf("selection: %+v", m)
	}
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !m.busy {
		t.Fatal("install command missing")
	}
	next, _ = m.Update(cmd())
	m = next.(model)
	for _, r := range m.rows {
		if r.statuses != [2]string{"installiert", "installiert"} {
			t.Fatalf("%+v", r)
		}
	}
	if !strings.Contains(m.View().Content, "Beide") {
		t.Fatal("target missing")
	}
	next, _ = m.Update(resultMsg{rows: nil})
	m = next.(model)
	if m.cursor != 0 || len(m.selected) != 0 {
		t.Fatal("stale selection after update")
	}
	_, cmd = m.Update(key("enter"))
	if cmd != nil {
		t.Fatal("empty list installs")
	}
}
