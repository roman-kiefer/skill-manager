package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsJunctionFallback(t *testing.T) {
	root := t.TempDir()
	source, dest := filepath.Join(root, "source with spaces"), filepath.Join(root, "junction with spaces")
	writeTestFile(t, filepath.Join(source, "SKILL.md"), "junction content")
	if err := createJunction(source, dest); err != nil {
		t.Fatal(err)
	}
	if !samePath(source, dest) {
		a, ea := filepath.EvalSymlinks(source)
		b, eb := filepath.EvalSymlinks(dest)
		link, el := os.Readlink(dest)
		t.Fatalf("junction: source=%q (%v) dest=%q (%v) readlink=%q (%v)", a, ea, b, eb, link, el)
	}
	if err := createJunction(source, dest); err == nil {
		t.Fatal("existing target replaced")
	}
	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || string(data) != "junction content" {
		t.Fatalf("%s, %v", data, err)
	}
	if err := createJunction(source, filepath.Join(root, "unsafe&path")); err == nil {
		t.Fatal("shell metacharacters accepted")
	}
}
