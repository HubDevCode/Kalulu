package main

import (
	"os"
	"os/exec"
)

// restartSelf starts a fresh Kalulu and lets this process end. Windows has
// no exec that replaces a process.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = os.Environ()
	return cmd.Start()
}
