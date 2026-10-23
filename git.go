package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("Git: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func syncRepository(c config) (string, error) {
	_, err := os.Lstat(c.Source)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(c.Source), 0755); err != nil {
			return "", err
		}
		if _, err := git(filepath.Dir(c.Source), "clone", "--", c.URL, c.Source); err != nil {
			return "", err
		}
		return "Quelle geklont.", nil
	}
	if err != nil {
		return "", err
	}
	root, err := git(c.Source, "rev-parse", "--show-toplevel")
	if err != nil || !samePath(root, c.Source) {
		return "", fmt.Errorf("%s ist kein eigener Git-Checkout; vorhandene Dateien bleiben unverändert", c.Source)
	}
	remote, err := git(c.Source, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	if remote != c.URL {
		return "", fmt.Errorf("origin passt nicht zur konfigurierten URL: %s", remote)
	}
	dirty, err := git(c.Source, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if dirty != "" {
		return "", fmt.Errorf("Quelle enthält lokale Änderungen; bitte zuerst selbst sichern oder committen")
	}
	refs, err := git(c.Source, "ls-remote", "origin")
	if err != nil {
		return "", err
	}
	if refs == "" {
		return "Quelle ist noch leer (keine Git-Commits).", nil
	}
	// Explicit HEAD also works for an existing, unborn checkout without upstream.
	if _, err := git(c.Source, "pull", "--ff-only", "origin", "HEAD"); err != nil {
		return "", err
	}
	rev, err := git(c.Source, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return "Quelle aktuell: " + rev, nil
}
