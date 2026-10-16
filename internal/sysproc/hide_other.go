//go:build !windows

package sysproc

import "os/exec"

func hide(cmd *exec.Cmd) *exec.Cmd { return cmd }
