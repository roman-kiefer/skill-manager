package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func createLink(source, dest string) error {
	if err := os.Symlink(source, dest); err == nil {
		return nil
	}
	return createJunction(source, dest)
}

func createJunction(source, dest string) error {
	// cmd's built-in mklink is used only with literal, shell-safe local paths.
	if strings.ContainsAny(source+dest, "\"%!&|<>^\r\n\x00") || strings.HasPrefix(source, `\\`) || strings.HasPrefix(dest, `\\`) {
		return fmt.Errorf("Junction benötigt lokale Pfade ohne Shell-Sonderzeichen; für Symlinks Windows-Entwicklermodus aktivieren")
	}
	command := fmt.Sprintf(`mklink /J "%s" "%s"`, dest, source)
	cmd := exec.Command("cmd.exe")
	// Go quoting follows CommandLineToArgvW; cmd.exe needs its own literal command line.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /d /c " + command}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Junction: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
