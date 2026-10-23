package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func samePath(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func linkStatus(source, dest string) string {
	if _, err := os.Lstat(dest); os.IsNotExist(err) {
		return "fehlt"
	} else if err != nil {
		return "Fehler"
	}
	if samePath(source, dest) {
		return "installiert"
	}
	return "Konflikt"
}

func installSkill(s skill, target string) (string, error) {
	info, err := os.Stat(filepath.Join(s.Path, "SKILL.md"))
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s hat keine reguläre SKILL.md", s.Name)
	}
	dest := filepath.Join(target, s.Name)
	switch linkStatus(s.Path, dest) {
	case "installiert":
		return "bereits installiert", nil
	case "fehlt":
	default:
		return "", fmt.Errorf("%s existiert bereits und bleibt unverändert", dest)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return "", err
	}
	if err := createLink(s.Path, dest); err != nil {
		return "", err
	}
	return "installiert", nil
}
