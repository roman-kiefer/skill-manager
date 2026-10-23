package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type skill struct{ Name, Path string }

func scanSkills(source string) ([]skill, error) {
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "skills")
	// Readlink recognizes Windows junctions even when EvalSymlinks leaves them intact.
	if _, err := os.Readlink(dir); err == nil {
		return nil, fmt.Errorf("skills-Verzeichnis darf kein Link sein")
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var skills []skill
	for _, entry := range entries {
		// Only direct directories, no external directory links or recursive search.
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if _, err := os.Readlink(path); err == nil {
			continue
		}
		info, err := os.Lstat(filepath.Join(path, "SKILL.md"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			skills = append(skills, skill{entry.Name(), path})
		}
	}
	return skills, nil
}
