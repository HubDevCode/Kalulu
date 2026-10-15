package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// historyView lists the recently played tracks.
type historyView struct {
	app    *App
	root   fyne.CanvasObject
	sub    *widget.Label
	list   *TrackList
	played []string
}

func newHistoryView(a *App) *historyView {
	h := &historyView{app: a}
	h.sub = dimLabel("")
	h.list = NewTrackList(a)
	h.list.SetPlaceholder(T("Nothing played yet"))
	h.list.OnPlay = func(i int) { a.playTrack(h.list.Tracks[i]) }
	h.list.Menu = func(i int) *fyne.Menu { return fyne.NewMenu("", a.trackMenu(h.list.Tracks[i])...) }
	h.list.Badge = func(i int) string {
		if i < len(h.played) {
			return h.played[i]
		}
		return ""
	}
	refresh := iconButton(theme.ViewRefreshIcon(), h.load)
	clearBtn := widget.NewButtonWithIcon(T("Clear"), theme.DeleteIcon(), func() {
		dialog.ShowConfirm(T("Clear history"), T("Forget every recently played track?"), func(ok bool) {
			if ok {
				a.do("history.clear", nil, func(Result) { h.load() })
			}
		}, a.win)
	})
	clearBtn.Importance = widget.LowImportance
	h.root = container.NewBorder(pageHeader(heading(T("Recently played")), h.sub, refresh, clearBtn), nil, nil, nil, h.list.Object())
	return h
}

func (h *historyView) Object() fyne.CanvasObject { return h.root }

func (h *historyView) Shown() { h.load() }

func (h *historyView) load() {
	go func() {
		r, err := h.app.call("history", P{"limit": 500})
		runOnUI(func() {
			if err != nil {
				h.list.SetPlaceholder(err.Error())
				h.list.SetTracks(nil)
				return
			}
			tracks := make([]ipc.TrackInfo, len(r.Response.History))
			h.played = make([]string, len(r.Response.History))
			for i, item := range r.Response.History {
				tracks[i] = item.Track
				h.played[i] = relativeTime(item.PlayedAt)
			}
			h.sub.SetText(fmt.Sprintf(T("%d tracks"), len(tracks)))
			h.list.SetTracks(tracks)
		})
	}()
}

// relativeTime renders an RFC 3339 time as a short age, such as 5m or 3d.
func relativeTime(stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return t.Format("Jan 2")
}
