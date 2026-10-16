package sysproc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW of the Windows API.
const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}
