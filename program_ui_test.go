package main

import (
	"errors"
	"strings"
	"testing"
)

func TestProgramUpdateTUI(t *testing.T) {
	setTestVersion(t, "v1.0.0")
	m := newModel(config{}, [2]string{})
	m.busy = false
	next, _ := m.Update(programCheckMsg{update: programUpdate{Version: "v1.2.0", Available: true}})
	m = next.(model)
	next, _ = m.Update(resultMsg{message: "Skill-Quelle aktualisiert"})
	m = next.(model)
	if !strings.Contains(m.View().Content, "v1.2.0") || !strings.Contains(m.View().Content, "Skill-Quelle aktualisiert") {
		t.Fatal("skill result overwrote update notice")
	}
	next, cmd := m.Update(key("s"))
	m = next.(model)
	if !m.programConfirm || cmd != nil {
		t.Fatal("installation was not gated by confirmation")
	}
	next, cmd = m.Update(key("enter"))
	m = next.(model)
	if m.programConfirm || m.programInstalling || cmd != nil {
		t.Fatal("default answer installed update")
	}
	next, _ = m.Update(key("s"))
	m = next.(model)
	next, cmd = m.Update(key("j"))
	m = next.(model)
	if !m.programInstalling || cmd == nil {
		t.Fatal("confirmation did not start install")
	}
	_, cmd = m.Update(key("q"))
	if cmd != nil {
		t.Fatal("quit while replacement in progress")
	}
	next, _ = m.Update(programInstallMsg{err: errors.New("no write permission")})
	m = next.(model)
	if m.programInstalling || m.programInstalled || !strings.Contains(m.programMessage, "no write permission") {
		t.Fatal("install error missing")
	}
	next, cmd = m.Update(programInstallMsg{})
	m = next.(model)
	if !m.programInstalled || cmd == nil {
		t.Fatal("success did not request exit")
	}
}

func TestProgramCheckDoesNotBlockSkills(t *testing.T) {
	setTestVersion(t, "dev")
	m := newModel(config{}, [2]string{})
	if m.programChecking {
		t.Fatal("dev build automatically checks")
	}
	next, cmd := m.Update(key("v"))
	m = next.(model)
	if !m.programChecking || cmd == nil {
		t.Fatal("manual check unavailable during skill sync")
	}
	next, _ = m.Update(programCheckMsg{err: errors.New("offline")})
	m = next.(model)
	if m.programChecking || !strings.Contains(m.programMessage, "offline") {
		t.Fatal("check error missing")
	}
	next, _ = m.Update(readRows(config{}, [2]string{}, "", nil))
	m = next.(model)
	m.busy = false
	next, _ = m.Update(programCheckMsg{update: programUpdate{Version: "v1.2.0"}})
	m = next.(model)
	_, cmd = m.Update(key("s"))
	if cmd != nil {
		t.Fatal("dev build offered self replacement")
	}
}
