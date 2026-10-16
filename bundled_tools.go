package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/internal/sysproc"
	"github.com/bjarneo/cliamp/player"
)

// bundledToolsDir is the folder next to the binary where the Windows
// installer puts yt-dlp, ffmpeg and ffprobe. It is "" when there is none.
var bundledToolsDir = findBundledTools()

// The installed tools come before the system ones, so playback uses the
// versions the installer verified. The bundled Python with the yt-dlp
// zipapp keeps yt-dlp loaded between tracks, as on Linux.
func init() {
	if bundledToolsDir == "" {
		return
	}
	_ = os.Setenv("PATH", bundledToolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	python, zipapp := bundledYTDLPZipapp()
	if python != "" {
		player.UseBundledYTDLP(python, zipapp)
	}
}

// warmBundledYTDLP loads the bundled yt-dlp at start: on Windows the first
// load takes a few seconds, which the first track should not wait for.
func warmBundledYTDLP() {
	if python, _ := bundledYTDLPZipapp(); python != "" {
		player.WarmYT()
	}
}

// bundledYTDLPZipapp returns the bundled Python and yt-dlp zipapp, or ""
// when either is missing.
func bundledYTDLPZipapp() (python, zipapp string) {
	python = filepath.Join(bundledToolsDir, "python", "python.exe")
	zipapp = filepath.Join(bundledToolsDir, "yt-dlp.pyz")
	for _, f := range []string{python, zipapp} {
		if _, err := os.Stat(f); err != nil {
			return "", ""
		}
	}
	return python, zipapp
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

// updateBundledYTDLP runs "yt-dlp -U" once a day on the bundled yt-dlp.exe
// and the zipapp, which update separately. A yt-dlp from the system package
// manager is left to it. The update fails quietly when the install folder
// is read-only, and runs again at the next start.
func updateBundledYTDLP() {
	if bundledToolsDir == "" {
		return
	}
	var cmds [][]string
	if bin := filepath.Join(bundledToolsDir, "yt-dlp.exe"); fileExists(bin) {
		cmds = append(cmds, []string{bin, "-U"})
	}
	if python, zipapp := bundledYTDLPZipapp(); python != "" {
		cmds = append(cmds, []string{python, zipapp, "-U"})
	}
	if len(cmds) == 0 {
		return
	}
	stamp := filepath.Join(bundledToolsDir, ".yt-dlp-updated")
	if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < ytdlpUpdateEvery {
		return
	}
	go func() {
		time.Sleep(30 * time.Second)
		ok := true
		for _, args := range cmds {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			out, err := sysproc.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
			cancel()
			if err != nil {
				applog.Warn("yt-dlp update (%s): %v: %s", filepath.Base(args[len(args)-2]), err, strings.TrimSpace(string(out)))
				ok = false
				continue
			}
			applog.Info("yt-dlp update (%s): %s", filepath.Base(args[len(args)-2]), strings.TrimSpace(string(out)))
		}
		if ok {
			_ = os.WriteFile(stamp, nil, 0o644)
			now := time.Now()
			_ = os.Chtimes(stamp, now, now)
		}
	}()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
