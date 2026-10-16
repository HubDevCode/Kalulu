package sysproc

import "testing"

func TestCommandHasNoWindow(t *testing.T) {
	cmd := Command("ffmpeg")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("SysProcAttr = %+v, want a hidden window", cmd.SysProcAttr)
	}
}
