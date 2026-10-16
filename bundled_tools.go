package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/internal/sysproc"
)

// bundledToolsDir is the folder next to the binary where the Windows
// installer puts yt-dlp, ffmpeg and ffprobe. It is "" when there is none.
var bundledToolsDir = findBundledTools()

// The installed tools come before the system ones, so playback uses the
// versions the installer verified.
func init() {
	if bundledToolsDir != "" {
		_ = os.Setenv("PATH", bundledToolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}

func findBundledTools() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Join(filepath.Dir(exe), "tools")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

// ytdlpUpdateEvery is how often the bundled yt-dlp updates itself. YouTube
// changes often, and an old yt-dlp stops resolving its tracks.
const ytdlpUpdateEvery = 24 * time.Hour

// updateBundledYTDLP runs "yt-dlp -U" on the bundled yt-dlp once a day. A
// yt-dlp from the system package manager is left to it. The update fails
// quietly when the install folder is read-only.
func updateBundledYTDLP() {
	if bundledToolsDir == "" {
		return
	}
	bin := filepath.Join(bundledToolsDir, "yt-dlp.exe")
	if _, err := os.Stat(bin); err != nil {
		return
	}
	stamp := filepath.Join(bundledToolsDir, ".yt-dlp-updated")
	if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < ytdlpUpdateEvery {
		return
	}
	go func() {
		time.Sleep(30 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		out, err := sysproc.CommandContext(ctx, bin, "-U").CombinedOutput()
		if err != nil {
			applog.Warn("yt-dlp update: %v: %s", err, strings.TrimSpace(string(out)))
			return
		}
		applog.Info("yt-dlp update: %s", strings.TrimSpace(string(out)))
		_ = os.WriteFile(stamp, nil, 0o644)
		now := time.Now()
		_ = os.Chtimes(stamp, now, now)
	}()
}
