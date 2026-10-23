package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

func defaultSource(home string) string {
	// Development checkout: the sibling library is the convenient default.
	if cwd, err := os.Getwd(); err == nil && filepath.Base(cwd) == "skill-manager" {
		return filepath.Join(filepath.Dir(cwd), "skill-library")
	}
	return filepath.Join(home, ".local", "share", "skill-manager", "skill-library")
}

func (c config) validate() error {
	if strings.TrimSpace(c.URL) == "" || strings.HasPrefix(c.URL, "-") || strings.ContainsAny(c.URL, "\x00\r\n") {
		return errors.New("gültige Git-URL oder lokalen Repository-Pfad angeben")
	}
	if c.Source == "" {
		return errors.New("Quellordner fehlt")
	}
	return nil
}

func loadConfig(path string) (config, error) {
	var c config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("Konfiguration %s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return c, fmt.Errorf("Konfiguration %s: %w", path, err)
	}
	return c, nil
}

func saveConfig(path string, c config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Exclusive creation: never replace an existing configuration.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	return errors.Join(writeErr, closeErr)
}

func targetDirs(home string) [2]string {
	return [2]string{filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".claude", "skills")}
}
