package main

import (
	"encoding/json"
	"net/http"
	_ "net/http/pprof" // served only when KALULU_PPROF is set
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/internal/fileutil"
	"github.com/bjarneo/cliamp/player"
	"github.com/bjarneo/cliamp/playlist"
)

// guiSession is the queue, the track and the position of the last run, so
// Kalulu reopens where the music stopped.
type guiSession struct {
	Tracks      []playlist.Track `json:"tracks"`
	Index       int              `json:"index"`
	PositionSec int              `json:"position_sec"`
}

// maxSessionTracks bounds the saved queue: hours of autoplay add many
// tracks, and only the ones around the current one matter.
const maxSessionTracks = 300

func sessionFile() string {
	dir, err := appdir.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kalulu-session.json")
}

func loadGUISession() (guiSession, bool) {
	var s guiSession
	f := sessionFile()
	if f == "" {
		return s, false
	}
	data, err := os.ReadFile(f)
	if err != nil || json.Unmarshal(data, &s) != nil {
		return s, false
	}
	if s.Index < 0 || s.Index >= len(s.Tracks) {
		return s, false
	}
	return s, true
}

// snapshotSession reads the playlist and the player. Both are safe to read
// from any goroutine.
func snapshotSession(pl *playlist.Playlist, p *player.Player) (guiSession, bool) {
	if pl.Len() == 0 {
		return guiSession{}, false
	}
	tracks := pl.Tracks()
	_, idx := pl.Current()
	if idx < 0 || idx >= len(tracks) {
		return guiSession{}, false
	}
	// Keep a window around the current track.
	start := max(0, idx-maxSessionTracks/3)
	end := min(len(tracks), start+maxSessionTracks)
	s := guiSession{Tracks: tracks[start:end], Index: idx - start}
	if !tracks[idx].Realtime {
		s.PositionSec = int(p.Position().Seconds())
	}
	return s, true
}

func saveGUISession(s guiSession) {
	f := sessionFile()
	if f == "" {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	if err := fileutil.WriteFileAtomic(f, data, 0o600); err != nil {
		applog.Warn("session: %v", err)
	}
}

// keepSession saves the session every few seconds while it changes, and
// once more when stop is called.
func keepSession(pl *playlist.Playlist, p *player.Player) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	var lastKey string
	save := func() {
		s, ok := snapshotSession(pl, p)
		if !ok {
			return
		}
		cur := s.Tracks[s.Index]
		key := cur.Path + "|" + time.Duration(s.PositionSec/5*5).String() + "|" + strconv.Itoa(len(s.Tracks))
		if key == lastKey {
			return
		}
		lastKey = key
		saveGUISession(s)
	}
	go func() {
		defer close(finished)
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				save()
				return
			case <-t.C:
				save()
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

// setupDiagnostics writes fatal crashes to kalulu-crash.log next to the log,
// since a desktop launch loses stderr, and serves pprof on KALULU_PPROF.
func setupDiagnostics() {
	if dir, err := appdir.Dir(); err == nil {
		if f, err := os.OpenFile(filepath.Join(dir, "kalulu-crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_ = debug.SetCrashOutput(f, debug.CrashOptions{})
			f.Close()
		}
	}
	if addr := os.Getenv("KALULU_PPROF"); addr != "" {
		go func() { applog.Warn("pprof: %v", http.ListenAndServe(addr, nil)) }()
	}
}

// tuneMemory keeps the heap close to what Kalulu really uses. The default
// lets the heap reach twice the live data before collecting, which for a
// player left open all day is memory held for nothing. KALULU_GOGC and
// GOMEMLIMIT still override it.
func tuneMemory() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(192 << 20)
	}
}
