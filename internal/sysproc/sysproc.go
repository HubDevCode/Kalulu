// Package sysproc starts the helper programs of playback (yt-dlp, ffmpeg,
// ffprobe and the like). On Windows a GUI process that starts a console
// program opens a console window for it, so every track would flash one;
// the commands of this package start without a window there. Elsewhere
// they are the commands of os/exec.
package sysproc

import (
	"context"
	"os/exec"
)

// Command is exec.Command without a console window on Windows.
func Command(name string, args ...string) *exec.Cmd {
	return hide(exec.Command(name, args...))
}

// CommandContext is exec.CommandContext without a console window on
// Windows.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return hide(exec.CommandContext(ctx, name, args...))
}
