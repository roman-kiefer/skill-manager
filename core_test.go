package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func gitTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Skill Manager Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Skill Manager Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
}

func commitTest(t *testing.T, dir, message string) {
	t.Helper()
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "-c", "commit.gpgsign=false", "commit", "-m", message)
}

func TestConfigDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings", "config.json")
	cfg := config{"https://example.invalid/skills.git", filepath.Join(t.TempDir(), "library")}
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(path, config{"other", "other"}); !os.IsExist(err) {
		t.Fatalf("expected exists, got %v", err)
	}
	got, err := loadConfig(path)
	if err != nil || got != cfg {
		t.Fatalf("%+v, %v", got, err)
	}
	writeTestFile(t, path, "{broken")
	if _, err := loadConfig(path); err == nil {
		t.Fatal("invalid config accepted")
	}
}

func TestScanSkills(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"), "# Alpha")
	writeTestFile(t, filepath.Join(root, "skills", "zeta", "SKILL.md"), "# Zeta")
	writeTestFile(t, filepath.Join(root, "skills", "nested", "child", "SKILL.md"), "ignored")
	writeTestFile(t, filepath.Join(root, "skills", "lower", "skill.txt"), "ignored")
	if err := os.MkdirAll(filepath.Join(root, "skills", "directory", "SKILL.md"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := scanSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("%+v", got)
	}
	if _, err := scanSkills(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing source accepted")
	}
	empty := t.TempDir()
	if got, err := scanSkills(empty); err != nil || len(got) != 0 {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestScanRejectsOutsideSkillsDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(outside, "external", "SKILL.md"), "external")
	if err := createLink(outside, filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := scanSkills(root); err == nil {
		t.Fatal("external skills directory accepted")
	}
}

func TestInstallLinksAndConflicts(t *testing.T) {
	root := t.TempDir()
	s := skill{"alpha", filepath.Join(root, "library with spaces", "skills", "alpha")}
	writeTestFile(t, filepath.Join(s.Path, "SKILL.md"), "version one")
	for _, target := range targetDirs(filepath.Join(root, "home")) {
		dest := filepath.Join(target, s.Name)
		if got := linkStatus(s.Path, dest); got != "fehlt" {
			t.Fatal(got)
		}
		if _, err := installSkill(s, target); err != nil {
			t.Fatal(err)
		}
		if got := linkStatus(s.Path, dest); got != "installiert" {
			t.Fatal(got)
		}
		if _, err := installSkill(s, target); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(s.Path, "SKILL.md"), "version two")
		data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
		if err != nil || string(data) != "version two" {
			t.Fatalf("%s, %v", data, err)
		}
	}
	t.Run("existing directory", func(t *testing.T) {
		target := filepath.Join(root, "conflict")
		dest := filepath.Join(target, s.Name)
		writeTestFile(t, filepath.Join(dest, "SKILL.md"), "keep me")
		if _, err := installSkill(s, target); err == nil {
			t.Fatal("overwrote directory")
		}
		data, _ := os.ReadFile(filepath.Join(dest, "SKILL.md"))
		if string(data) != "keep me" {
			t.Fatal("existing file changed")
		}
	})
	t.Run("existing file", func(t *testing.T) {
		target := filepath.Join(root, "file conflict")
		writeTestFile(t, filepath.Join(target, s.Name), "keep file")
		if _, err := installSkill(s, target); err == nil {
			t.Fatal("overwrote file")
		}
	})
	t.Run("other link", func(t *testing.T) {
		target, other := filepath.Join(root, "other target"), filepath.Join(root, "other skill")
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(other, 0755); err != nil {
			t.Fatal(err)
		}
		if err := createLink(other, filepath.Join(target, s.Name)); err != nil {
			t.Fatal(err)
		}
		if _, err := installSkill(s, target); err == nil {
			t.Fatal("replaced other link")
		}
		// A broken link must remain a conflict, too.
		if err := os.Remove(other); err != nil {
			t.Fatal(err)
		}
		if _, err := installSkill(s, target); err == nil {
			t.Fatal("replaced broken link")
		}
	})
}

func TestGitClonePullAndProtection(t *testing.T) {
	gitTestEnv(t)
	root := t.TempDir()
	remote, author, source := filepath.Join(root, "remote.git"), filepath.Join(root, "author"), filepath.Join(root, "local library")
	mustGit(t, root, "init", "--bare", "--initial-branch=main", remote)
	cfg := config{remote, source}
	if _, err := syncRepository(cfg); err != nil {
		t.Fatal(err)
	}
	if message, err := syncRepository(cfg); err != nil || !strings.Contains(message, "leer") {
		t.Fatalf("%s, %v", message, err)
	}
	mustGit(t, root, "clone", remote, author)
	writeTestFile(t, filepath.Join(author, "skills", "alpha", "SKILL.md"), "one")
	commitTest(t, author, "initial skill")
	mustGit(t, author, "push", "origin", "main")
	if _, err := syncRepository(cfg); err != nil {
		t.Fatal(err)
	}
	skills, err := scanSkills(source)
	if err != nil || len(skills) != 1 {
		t.Fatalf("%+v, %v", skills, err)
	}
	target := filepath.Join(root, "installed")
	if _, err := installSkill(skills[0], target); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(author, "skills", "alpha", "SKILL.md"), "two")
	commitTest(t, author, "update skill")
	mustGit(t, author, "push", "origin", "main")
	if _, err := syncRepository(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "alpha", "SKILL.md"))
	if err != nil || string(data) != "two" {
		t.Fatalf("%s, %v", data, err)
	}
	writeTestFile(t, filepath.Join(source, "local.txt"), "keep")
	if _, err := syncRepository(cfg); err == nil {
		t.Fatal("dirty source accepted")
	}
	if _, err := syncRepository(config{"different URL", source}); err == nil {
		t.Fatal("mismatched origin accepted")
	}
	plain := filepath.Join(root, "plain")
	writeTestFile(t, filepath.Join(plain, "keep.txt"), "keep")
	if _, err := syncRepository(config{remote, plain}); err == nil {
		t.Fatal("non-checkout accepted")
	}
	// A divergent local commit must not be reset or merged.
	commitTest(t, source, "local commit")
	writeTestFile(t, filepath.Join(author, "remote.txt"), "remote")
	commitTest(t, author, "diverge")
	mustGit(t, author, "push", "origin", "main")
	before := mustGit(t, source, "rev-parse", "HEAD")
	if _, err := syncRepository(cfg); err == nil {
		t.Fatal("divergent source accepted")
	}
	if after := mustGit(t, source, "rev-parse", "HEAD"); before != after {
		t.Fatal("local commit changed")
	}
}
