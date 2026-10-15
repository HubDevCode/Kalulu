package gui

import (
	"time"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/player"
)

// hoverPrefetch resolves the audio of a song the pointer rests on, so the
// click that usually follows starts the music at once.
type hoverPrefetch struct {
	url   string
	timer *time.Timer
}

func (h *hoverPrefetch) in() {
	if h.url == "" {
		return
	}
	u := h.url
	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(350*time.Millisecond, func() { player.PrefetchYT(u) })
}

func (h *hoverPrefetch) out() {
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
}

// prefetchUpNext resolves the next tracks of the queue while the current
// one plays, so Next and the automatic change start without waiting.
func (a *App) prefetchUpNext(s ipc.RuntimeSnapshot) {
	if s.Track == nil {
		return
	}
	if s.State != "playing" {
		// The track restored from the last run starts at once on Play.
		if !a.prefetchedStart {
			a.prefetchedStart = true
			go func() {
				time.Sleep(5 * time.Second)
				player.PrefetchYT(s.Track.Path)
			}()
		}
		return
	}
	key := s.Track.Path
	if key == a.prefetchedFor {
		return
	}
	a.prefetchedFor = key
	index := s.Index
	go func() {
		// Leave the first seconds of the new track to its own start.
		time.Sleep(3 * time.Second)
		r, err := a.call("queue.list", P{"offset": index + 1, "limit": 2})
		if err != nil {
			return
		}
		for _, t := range r.Response.Tracks {
			player.PrefetchYT(t.Path)
		}
	}()
}
