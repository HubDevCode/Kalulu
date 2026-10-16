//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf replaces the process with a fresh Kalulu.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Inside an AppImage the binary needs the launcher, which mounts the
	// image and sets up its libraries again.
	if image := os.Getenv("APPIMAGE"); image != "" {
		exe = image
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
