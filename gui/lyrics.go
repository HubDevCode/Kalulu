package gui

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/lyrics"
)

const prefLyricsOffset = "lyrics.offset_ms"

// lyricsView shows the lyrics of the playing track, following the synced
// lines when the source has timings.
type lyricsView struct {
	app    *App
	root   fyne.CanvasObject
	title  *widget.Label
	status *widget.Label
	list   *widget.List
	offset *widget.Label

	lines   []ipc.LyricLine
	synced  bool
	current int
	key     string
	seq     int
	rowH    float32
	// version counts lyric changes for the views that show them.
	version int
	// source is the source picked for the playing track; empty tries all.
	source  string
	refresh bool
	// from names the source of the lyrics shown.
	from string
}

func newLyricsView(a *App) *lyricsView {
	l := &lyricsView{app: a, current: -1}
	l.title = heading(T("Lyrics"))
	l.status = dimLabel(T("Play something to see its lyrics"))
	l.list = widget.NewList(
		func() int { return len(l.lines) },
		func() fyne.CanvasObject {
			lb := widget.NewLabel("")
			lb.Alignment = fyne.TextAlignCenter
			lb.Truncation = fyne.TextTruncateEllipsis
			lb.SizeName = theme.SizeNameSubHeadingText
			return lb
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			lb := o.(*widget.Label)
			text := l.lines[id].Text
			if strings.TrimSpace(text) == "" {
				text = "♪"
			}
			lb.Text = text
			switch {
			case !l.synced:
				lb.Importance = widget.MediumImportance
				lb.TextStyle = fyne.TextStyle{}
			case id == l.current:
				lb.Importance = widget.HighImportance
				lb.TextStyle = fyne.TextStyle{Bold: true}
			case id < l.current:
				lb.Importance = widget.LowImportance
				lb.TextStyle = fyne.TextStyle{}
			default:
				lb.Importance = widget.MediumImportance
				lb.TextStyle = fyne.TextStyle{}
			}
			lb.Refresh()
		},
	)
	l.list.HideSeparators = true
	l.list.OnSelected = func(id widget.ListItemID) {
		l.list.UnselectAll()
		if l.synced && id >= 0 && id < len(l.lines) {
			a.seekTo(l.lines[id].Start - l.offsetSecs())
		}
	}
	l.offset = captionLabel("")
	l.updateOffsetLabel()
	earlier := iconButton(theme.ContentRemoveIcon(), func() { l.shiftOffset(-250) })
	later := iconButton(theme.ContentAddIcon(), func() { l.shiftOffset(250) })
	reload := iconButton(theme.ViewRefreshIcon(), func() { l.reload("") })
	header := pageHeader(l.title, l.status, l.offset, earlier, later, reload, l.gearButton())
	l.root = container.NewBorder(header, nil, nil, nil, padded(0, 40, 20, 40, l.list))
	a.onState(l.update)
	return l
}

func (l *lyricsView) Object() fyne.CanvasObject { return l.root }

func (l *lyricsView) Shown() { l.scrollToCurrent() }

func (l *lyricsView) offsetSecs() float64 {
	return float64(l.app.prefs.IntWithFallback(prefLyricsOffset, 0)) / 1000
}

func (l *lyricsView) shiftOffset(ms int) {
	l.app.prefs.SetInt(prefLyricsOffset, l.app.prefs.IntWithFallback(prefLyricsOffset, 0)+ms)
	l.updateOffsetLabel()
	l.app.toast(fmt.Sprintf(T("Lyrics offset %+.2f s"), l.offsetSecs()))
}

// reload looks the lyrics up again, skipping the cache, from source (empty
// keeps the current choice).
func (l *lyricsView) reload(source string) {
	if source != "" {
		if source == "auto" {
			source = ""
		}
		l.source = source
	}
	l.refresh = true
	l.key = ""
	l.fetch(l.app.state)
}

// gearButton opens the lyrics menu: reload, source and offset.
func (l *lyricsView) gearButton() *widget.Button {
	var b *widget.Button
	b = iconButton(theme.SettingsIcon(), func() {
		c := l.app.win.Canvas()
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(b).Add(fyne.NewPos(0, b.Size().Height))
		widget.ShowPopUpMenuAtPosition(l.menu(), c, pos)
	})
	return b
}

func (l *lyricsView) menu() *fyne.Menu {
	sources := []*fyne.MenuItem{}
	add := func(key, name string) {
		it := fyne.NewMenuItem(name, func() { l.reload(key) })
		it.Checked = l.source == key || (key == "auto" && l.source == "")
		sources = append(sources, it)
	}
	add("auto", T("Automatic (best of all)"))
	sources = append(sources, fyne.NewMenuItemSeparator())
	for _, s := range lyrics.Sources {
		add(s, lyrics.SourceName(s))
	}
	sourceItem := fyne.NewMenuItem(T("Source"), nil)
	sourceItem.ChildMenu = fyne.NewMenu("", sources...)
	offset := fyne.NewMenuItem(fmt.Sprintf(T("Offset: %+.2f s"), l.offsetSecs()), nil)
	offset.Disabled = true
	return fyne.NewMenu("",
		fyne.NewMenuItem(T("Look for the lyrics again"), func() { l.reload("") }),
		sourceItem,
		fyne.NewMenuItemSeparator(),
		offset,
		fyne.NewMenuItem(T("Lyrics earlier (−0.25 s)"), func() { l.shiftOffset(-250) }),
		fyne.NewMenuItem(T("Lyrics later (+0.25 s)"), func() { l.shiftOffset(250) }),
		fyne.NewMenuItem(T("Reset the offset"), func() {
			l.app.prefs.SetInt(prefLyricsOffset, 0)
			l.updateOffsetLabel()
			l.app.toast(T("Lyrics offset reset"))
		}),
	)
}

func (l *lyricsView) updateOffsetLabel() {
	l.offset.SetText(fmt.Sprintf("offset %+.2fs", l.offsetSecs()))
}

func (l *lyricsView) update(s ipc.RuntimeSnapshot) {
	l.fetch(s)
	if !l.synced || len(l.lines) == 0 {
		return
	}
	pos := s.Position + l.offsetSecs()
	i := sort.Search(len(l.lines), func(i int) bool { return l.lines[i].Start > pos }) - 1
	if i != l.current {
		l.current = i
		l.list.Refresh()
		if l.app.current == "player" {
			l.scrollToCurrent()
		}
	}
}

// lineAt returns the synced line at position, for the now playing panel.
func (l *lyricsView) lineAt(position float64) string {
	if !l.synced || l.current < 0 || l.current >= len(l.lines) {
		return ""
	}
	return strings.TrimSpace(l.lines[l.current].Text)
}

func (l *lyricsView) scrollToCurrent() {
	if l.current < 0 || l.current >= len(l.lines) {
		return
	}
	if l.rowH == 0 {
		probe := widget.NewLabel("x")
		probe.SizeName = theme.SizeNameSubHeadingText
		l.rowH = probe.MinSize().Height
	}
	target := float32(l.current)*l.rowH - l.list.Size().Height/2 + l.rowH/2
	l.list.ScrollToOffset(max(0, target))
}

func (l *lyricsView) fetch(s ipc.RuntimeSnapshot) {
	if s.Track == nil {
		if l.key != "" {
			l.key = ""
			l.setLines(nil, T("Play something to see its lyrics"))
			l.title.SetText(T("Lyrics"))
		}
		return
	}
	title, sub := trackTitle(*s.Track)
	key := s.Track.Path + "|" + title
	if key == l.key {
		return
	}
	if l.key != "" && !l.refresh {
		// A new track starts from the automatic choice again.
		l.source = ""
	}
	l.key = key
	l.seq++
	seq := l.seq
	params := P{"name": l.source, "refresh": l.refresh}
	l.refresh = false
	if sub != "" {
		l.title.SetText(title + " — " + sub)
	} else {
		l.title.SetText(title)
	}
	l.setLines(nil, T("Looking for lyrics…"))
	go func() {
		r, err := l.app.call("lyrics", params)
		runOnUI(func() {
			if seq != l.seq {
				return
			}
			notFound := T("No lyrics found · ⚙ to look again or pick another source")
			if err != nil {
				l.from = ""
				l.setLines(nil, notFound)
				return
			}
			lines := r.Response.Lyrics
			if len(lines) == 0 {
				l.from = ""
				l.setLines(nil, notFound)
				return
			}
			l.from = r.Response.LyricsSource
			status := T("Synced lyrics")
			if hasWordTimings(lines) {
				status = T("Lyrics synced word by word")
			} else if !hasTimings(lines) {
				status = T("Plain lyrics")
			}
			if l.from != "" {
				status += " · " + lyrics.SourceName(l.from)
			}
			l.setLines(lines, status)
		})
	}()
}

func hasWordTimings(lines []ipc.LyricLine) bool {
	for _, line := range lines {
		if len(line.Words) > 1 {
			return true
		}
	}
	return false
}

func hasTimings(lines []ipc.LyricLine) bool {
	for _, line := range lines {
		if line.Start > 0 {
			return true
		}
	}
	return false
}

func (l *lyricsView) setLines(lines []ipc.LyricLine, status string) {
	l.version++
	l.lines = lines
	l.synced = hasTimings(lines)
	l.current = -1
	l.status.SetText(status)
	l.list.Refresh()
	l.list.ScrollToTop()
}
