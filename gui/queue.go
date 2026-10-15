package gui

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// queueView is the live playlist of the engine.
type queueView struct {
	app    *App
	root   fyne.CanvasObject
	title  *widget.Label
	sub    *widget.Label
	filter *widget.Entry
	tracks *TrackList

	all      []ipc.TrackInfo
	shown    []int // indexes of all that pass the filter
	revision uint64
	loaded   bool
	loading  bool
	current  int

	playNext     *fyne.Container
	playNextBody *fyne.Container
	playNextKey  string
}

func newQueueView(a *App) *queueView {
	q := &queueView{app: a, current: -1}
	q.title = heading(T("Now Playing"))
	q.sub = dimLabel("")
	q.filter = widget.NewEntry()
	q.filter.SetPlaceHolder(T("Filter queue…"))
	q.filter.ActionItem = widget.NewIcon(theme.SearchIcon())
	q.filter.OnChanged = func(string) { q.applyFilter() }

	q.tracks = NewTrackList(a)
	q.tracks.SetPlaceholder("The queue is empty — open files, a folder, a URL, or pick something from the Library")
	q.tracks.OnPlay = func(i int) {
		if real := q.real(i); real >= 0 {
			a.do("queue.play", P{"index": real}, nil)
		}
	}
	q.tracks.IsCurrent = func(i int) bool { return q.real(i) == q.current && q.current >= 0 }
	q.tracks.Badge = func(i int) string {
		if real := q.real(i); real >= 0 && real < len(q.all) && q.all[real].QueuePosition > 0 {
			return fmt.Sprintf("+%d", q.all[real].QueuePosition)
		}
		return ""
	}
	q.tracks.Menu = q.menu

	locate := iconButton(theme.ZoomFitIcon(), q.locate)
	shuffle := iconButton(iconShuffle, a.toggleShuffle)
	save := widget.NewButtonWithIcon(T("Save"), theme.DocumentSaveIcon(), q.saveAsPlaylist)
	save.Importance = widget.LowImportance
	clearBtn := widget.NewButtonWithIcon(T("Clear"), theme.DeleteIcon(), q.clear)
	clearBtn.Importance = widget.LowImportance
	header := pageHeader(q.title, q.sub, fixed(170, 0, q.filter), locate, shuffle, save, clearBtn)

	q.playNextBody = container.NewVBox()
	clearNext := widget.NewButton(T("Clear"), func() { a.do("playnext.clear", nil, nil) })
	clearNext.Importance = widget.LowImportance
	q.playNext = padded(0, 18, 6, 18, tPanel(roleSurface2, 10, padded(6, 10, 6, 10,
		container.NewBorder(container.NewBorder(nil, nil, boldLabel(T("Up next (play-next queue)")), clearNext), nil, nil, nil, q.playNextBody))))
	q.playNext.Hide()

	q.root = container.NewBorder(container.NewVBox(header, q.playNext), nil, nil, nil, q.tracks.Object())
	a.onState(q.update)
	return q
}

func (q *queueView) Object() fyne.CanvasObject { return q.root }

func (q *queueView) Shown() {
	q.locate()
}

// real maps a shown row to its playlist index.
func (q *queueView) real(i int) int {
	if i < 0 || i >= len(q.shown) {
		return -1
	}
	return q.shown[i]
}

func (q *queueView) update(s ipc.RuntimeSnapshot) {
	if s.Playlist != "" {
		q.title.SetText(s.Playlist)
	} else {
		q.title.SetText(T("Now Playing"))
	}
	current := -1
	if s.LogicalTrack != nil || s.Track != nil {
		current = s.Index
	}
	if current != q.current {
		q.current = current
		q.tracks.Refresh()
	}
	if !q.loaded || s.PlaylistRevision != q.revision {
		q.revision = s.PlaylistRevision
		q.reload()
	}
	q.updatePlayNext(s)
}

// reload fetches the whole live playlist in pages.
func (q *queueView) reload() {
	if q.loading {
		return
	}
	q.loading = true
	go func() {
		var all []ipc.TrackInfo
		for offset := 0; ; offset += 200 {
			r, err := q.app.call("queue.list", P{"offset": offset, "limit": 200})
			if err != nil {
				break
			}
			all = append(all, r.Response.Tracks...)
			if len(r.Response.Tracks) == 0 || len(all) >= r.Response.Total {
				break
			}
		}
		runOnUI(func() {
			q.loading = false
			q.loaded = true
			q.all = all
			q.applyFilter()
			q.updateSummary()
			// The revision may have moved while this page loaded.
			if q.app.state.PlaylistRevision != q.revision {
				q.revision = q.app.state.PlaylistRevision
				q.reload()
			}
		})
	}()
}

func (q *queueView) updateSummary() {
	total := 0
	for _, t := range q.all {
		total += t.DurationSecs
	}
	text := fmt.Sprintf(T("%d tracks"), len(q.all))
	if len(q.all) == 1 {
		text = T("1 track")
	}
	if total > 0 {
		text += " · " + fmtDuration(float64(total))
	}
	q.sub.SetText(text)
}

func (q *queueView) applyFilter() {
	needle := strings.ToLower(strings.TrimSpace(q.filter.Text))
	q.shown = q.shown[:0]
	view := make([]ipc.TrackInfo, 0, len(q.all))
	for i, t := range q.all {
		if needle != "" {
			hay := strings.ToLower(t.Title + " " + t.Artist + " " + t.Album + " " + t.Path)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		q.shown = append(q.shown, i)
		view = append(view, t)
	}
	q.tracks.SetTracks(view)
}

// locate scrolls the playing track into view.
func (q *queueView) locate() {
	for i, real := range q.shown {
		if real == q.current {
			q.tracks.ScrollTo(i)
			return
		}
	}
}

// upcoming returns up to n playlist indexes that play after the current
// track: the play-next entries first, then the following rows.
func (q *queueView) upcoming(n int) []int {
	var queued []int
	for i, t := range q.all {
		if t.QueuePosition > 0 {
			queued = append(queued, i)
		}
	}
	sort.Slice(queued, func(a, b int) bool { return q.all[queued[a]].QueuePosition < q.all[queued[b]].QueuePosition })
	out := queued
	for i := q.current + 1; i < len(q.all) && len(out) < n; i++ {
		if q.all[i].QueuePosition == 0 {
			out = append(out, i)
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (q *queueView) updatePlayNext(s ipc.RuntimeSnapshot) {
	if s.PlayNextTotal == 0 {
		if !q.playNext.Hidden {
			q.playNext.Hide()
			q.playNextKey = ""
		}
		return
	}
	var queued []int
	for i, t := range q.all {
		if t.QueuePosition > 0 {
			queued = append(queued, i)
		}
	}
	sort.Slice(queued, func(a, b int) bool { return q.all[queued[a]].QueuePosition < q.all[queued[b]].QueuePosition })
	key := fmt.Sprint(queued, s.PlaylistRevision)
	if key == q.playNextKey && !q.playNext.Hidden {
		return
	}
	q.playNextKey = key
	q.playNextBody.Objects = nil
	for pos, i := range queued {
		pos, i := pos, i
		title, sub := trackTitle(q.all[i])
		if sub != "" {
			title += " — " + sub
		}
		up := iconButton(theme.MoveUpIcon(), func() {
			if pos > 0 {
				q.app.do("playnext.move", P{"index": pos, "to": pos - 1}, nil)
			}
		})
		rm := iconButton(theme.ContentClearIcon(), func() { q.app.do("playnext.remove", P{"index": pos}, nil) })
		q.playNextBody.Add(container.NewBorder(nil, nil, captionLabel(fmt.Sprintf("%d.", pos+1)), container.NewHBox(up, rm), trunc(label(title))))
		if pos >= 7 {
			if more := len(queued) - 8; more > 0 {
				q.playNextBody.Add(captionLabel(fmt.Sprintf("…and %d more", more)))
			}
			break
		}
	}
	q.playNext.Show()
	q.playNextBody.Refresh()
}

func (q *queueView) menu(i int) *fyne.Menu {
	real := q.real(i)
	if real < 0 || real >= len(q.all) {
		return nil
	}
	t := q.all[real]
	a := q.app
	fav := T("Add to favorites")
	favIcon := iconHeartLine
	if t.Bookmark {
		fav = T("Remove from favorites")
		favIcon = iconHeart
	}
	return fyne.NewMenu("",
		fyne.NewMenuItemWithIcon(T("Play"), theme.MediaPlayIcon(), func() { a.do("queue.play", P{"index": real}, nil) }),
		fyne.NewMenuItemWithIcon(T("Play next"), iconPlayNext, func() {
			a.do("queue.enqueue", P{"index": real}, func(Result) { a.toast("Added to play-next") })
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItemWithIcon(T("Move up"), theme.MoveUpIcon(), func() {
			if real > 0 {
				a.do("queue.move", P{"index": real, "to": real - 1}, nil)
			}
		}),
		fyne.NewMenuItemWithIcon(T("Move down"), theme.MoveDownIcon(), func() {
			if real < len(q.all)-1 {
				a.do("queue.move", P{"index": real, "to": real + 1}, nil)
			}
		}),
		fyne.NewMenuItemWithIcon(T("Move to top"), theme.MenuDropUpIcon(), func() { a.do("queue.move", P{"index": real, "to": 0}, nil) }),
		fyne.NewMenuItemWithIcon(T("Move to bottom"), theme.MenuDropDownIcon(), func() {
			a.do("queue.move", P{"index": real, "to": len(q.all) - 1}, nil)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItemWithIcon(fav, favIcon, func() {
			a.do("playlist.bookmark", P{"provider": "local", "track": t}, nil)
		}),
		a.addToPlaylistItem([]ipc.TrackInfo{t}),
		fyne.NewMenuItemWithIcon(T("Track info"), theme.InfoIcon(), func() { a.showTrackInfo(t) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItemWithIcon(T("Remove from queue"), theme.ContentRemoveIcon(), func() { q.remove(real) }),
	)
}

func (q *queueView) remove(real int) {
	q.app.do("queue.remove", P{"index": real}, nil)
}

// removeSelected removes the selected row, for the Delete key.
func (q *queueView) removeSelected() {
	if real := q.real(q.tracks.Selected()); real >= 0 {
		q.remove(real)
	}
}

func (q *queueView) clear() {
	if len(q.all) == 0 {
		return
	}
	dialog.ShowConfirm(T("Clear queue"), fmt.Sprintf(T("Remove all %d tracks from the queue?"), len(q.all)), func(ok bool) {
		if ok {
			q.app.do("queue.clear", nil, nil)
		}
	}, q.app.win)
}

// saveAsPlaylist writes the queue to a saved playlist of the local provider.
func (q *queueView) saveAsPlaylist() {
	if len(q.all) == 0 {
		q.app.toast(T("The queue is empty"))
		return
	}
	tracks := append([]ipc.TrackInfo(nil), q.all...)
	for i := range tracks {
		tracks[i].QueuePosition = 0
		tracks[i].Index = 0
	}
	q.app.newPlaylistWith(tracks)
}
