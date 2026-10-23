//go:build !windows

package main

import "os"

func createLink(source, dest string) error { return os.Symlink(source, dest) }
