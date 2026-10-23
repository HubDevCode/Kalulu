package gui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// nowPlayingPanel is the right column: cover, track details, visualizer
// and the next tracks.
type nowPlayingPanel struct {
	app  *App
	root fyne.CanvasObject

	art      *artView
	title    *widget.Label
	artist   *widget.Label
	album    *widget.Label
	extra    *widget.Label
	lyric    *karaoke
	lyricBox fyne.CanvasObject
	lyricVer int
	vis      *visualizer
	queue    *widget.List
	queueIdx []int // queue indexes in play order
	content  *fyne.Container
	bottom   fyne.CanvasObject
	glow     *canvas.LinearGradient
	lastKey  string
	nextKey  string
}

func newNowPlayingPanel(a *App) *nowPlayingPanel {
	n := &nowPlayingPanel{app: a}
	n.art = newArtView(a, 0, 14)
	n.art.image.FillMode = canvas.ImageFillCover
	// One themed gradient over the whole panel, from a shade of the
	// accent to the surface: both ends follow the cover colors, so there
	// is no seam where a fixed color met the tinted background.
	n.glow = tGradient(rolePanelGlow, roleSurface)
	n.art.onLoaded = func(img image.Image) {
		a.setCoverImage(img)
		a.followCover()
	}
	n.art.onTap = func() { a.show("player") }
	n.title = heading(T("Nothing playing"))
	n.title.SizeName = theme.SizeNameSubHeadingText
	n.title.Truncation = fyne.TextTruncateEllipsis
	n.artist = trunc(label(""))
	n.artist.Importance = widget.HighImportance
	n.album = trunc(dimLabel(""))
	n.extra = captionLabel("")
	n.extra.Truncation = fyne.TextTruncateEllipsis
	// Three lines of the synced lyrics: the one before, the sung one with
	// a glow, and the next.
	n.lyric = newLyricTicker(a)
	n.lyric.active = a.rightShown.Load
	n.lyricVer = -1
	clip := container.NewScroll(n.lyric)
	clip.Direction = container.ScrollNone
	n.lyricBox = padded(6, 0, 2, 0, fixed(0, 136, clip))
	n.lyricBox.Hide()

	n.vis = newVisualizer(a, false)
	n.vis.active = a.rightShown.Load
	n.vis.onTap = a.showFullVisualizer
	visFrame := tPanel(roleSurface2, 12, padded(8, 8, 8, 8, fixed(0, 96, n.vis)))
	visHeader := container.NewBorder(nil, nil, sectionTitle(T("Visualizer")),
		container.NewHBox(
			iconButton(theme.ViewFullScreenIcon(), a.showFullVisualizer),
			iconButton(theme.MediaReplayIcon(), n.vis.Cycle),
		))

	var addBtn *widget.Button
	addBtn = iconButton(iconPlaylistAdd, func() {
		if a.state.Track == nil {
			return
		}
		item := a.addToPlaylistItem([]ipc.TrackInfo{*a.state.Track})
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(addBtn)
		widget.ShowPopUpMenuAtPosition(item.ChildMenu, a.win.Canvas(), pos.Add(fyne.NewPos(0, addBtn.Size().Height)))
	})
	actions := container.NewHBox(
		iconButton(theme.InfoIcon(), func() {
			if a.state.Track != nil {
				a.showTrackInfo(*a.state.Track)
			}
		}),
		iconButton(iconLyrics, func() { a.show("player") }),
		iconButton(theme.DownloadIcon(), a.saveCurrent),
		addBtn,
	)

	// The details and the lyrics stay at the top and the visualizer at the
	// bottom; only the queue between them scrolls.
	n.queue = widget.NewList(
		func() int { return len(n.queueIdx) },
		func() fyne.CanvasObject { return newTile(a, "", "", "", nil) },
		n.updateQueueRow)
	n.content = container.NewVBox(
		container.New(&panelHeadLayout{n: n}, n.art.Object(),
			container.New(&tightVBox{}, n.title, n.artist, n.album, n.extra)),
		actions,
		n.lyricBox,
		fixed(0, 4),
		sectionTitle(T("Up next")),
	)
	n.bottom = padded(8, 16, 14, 16, container.NewVBox(visHeader, visFrame))
	bg := tRect(roleSurface)
	n.lyric.Start()
	n.root = container.NewStack(bg, n.glow, container.NewBorder(
		padded(16, 16, 0, 16, n.content), n.bottom, nil, nil, padded(0, 10, 0, 10, n.queue)))
	a.onState(n.update)
	return n
}

func (n *nowPlayingPanel) Object() fyne.CanvasObject { return n.root }

func (n *nowPlayingPanel) update(s ipc.RuntimeSnapshot) {
	if s.Track == nil {
		if n.lastKey != "" {
			n.lastKey = ""
			n.art.Clear()
			n.title.SetText(T("Nothing playing"))
			n.artist.SetText("")
			n.album.SetText("")
			n.extra.SetText("Open a file, a folder or a stream, or browse a source in the Library.")
		}
		n.updateLyric(s)
		n.updateUpNext()
		return
	}
	t := *s.Track
	title, sub := trackTitle(t)
	key := t.Path + "|" + title + "|" + t.AlbumArtURL
	if key != n.lastKey {
		n.lastKey = key
		n.art.Load(t)
		n.title.SetText(title)
		n.artist.SetText(sub)
		album := t.Album
		if t.Year > 0 {
			album = strings.TrimSpace(fmt.Sprintf("%s · %d", album, t.Year))
		}
		n.album.SetText(album)
	}
	var extra []string
	if t.Genre != "" {
		extra = append(extra, t.Genre)
	}
	if t.Stream {
		if t.Realtime || s.Duration == 0 {
			extra = append(extra, "Live stream")
		} else {
			extra = append(extra, "Stream")
		}
	}
	if s.Total > 0 {
		extra = append(extra, fmt.Sprintf("Track %d of %d", s.Index+1, s.Total))
	}
	if s.EQPreset != "" && s.EQPreset != "Flat" {
		extra = append(extra, "EQ: "+s.EQPreset)
	}
	if s.StreamError != "" {
		extra = append(extra, "⚠ "+s.StreamError)
	}
	if text := strings.Join(extra, " · "); n.extra.Text != text {
		n.extra.SetText(text)
	}
	n.updateLyric(s)
	n.updateUpNext()
}

// updateLyric follows the synced lyrics in the three-line ticker; it hides
// without timed lyrics.
func (n *nowPlayingPanel) updateLyric(s ipc.RuntimeSnapshot) {
	l := n.app.lyrics
	if l.version != n.lyricVer {
		n.lyricVer = l.version
		n.lyric.SetLines(l.lines, "")
	}
	show := s.Track != nil && hasTimings(l.lines)
	if show == n.lyricBox.Visible() {
		if show {
			n.lyric.SetPosition(s.Position + l.offsetSecs())
		}
		return
	}
	if show {
		n.lyric.SetPosition(s.Position + l.offsetSecs())
		n.lyricBox.Show()
	} else {
		n.lyricBox.Hide()
	}
	n.content.Refresh()
}

// updateUpNext lists every track still to play.
func (n *nowPlayingPanel) updateUpNext() {
	q := n.app.queue
	next := q.upcoming(len(q.all))
	key := fmt.Sprint(next, n.app.state.PlaylistRevision)
	if key == n.nextKey {
		return
	}
	n.nextKey = key
	n.queueIdx = next
	n.queue.Refresh()
}

func (n *nowPlayingPanel) updateQueueRow(id widget.ListItemID, obj fyne.CanvasObject) {
	q := n.app.queue
	if id >= len(n.queueIdx) || n.queueIdx[id] >= len(q.all) {
		return
	}
	index := n.queueIdx[id]
	t := q.all[index]
	tl := obj.(*tile)
	tl.onPlay = func() { n.app.do("queue.play", P{"index": index}, nil) }
	title, sub := trackTitle(t)
	tl.title.SetText(title)
	tl.sub.SetText(sub)
	tl.thumb, tl.loaded = t.AlbumArtURL, true
	if t.AlbumArtURL != "" {
		tl.art.LoadURL(t.AlbumArtURL)
	} else {
		tl.art.Load(t)
	}
}

// panelHeadLayout places the cover and the details. With room to spare
// the cover stands above the details, up to 300 points; on a short window
// a small cover sits beside them, so the queue keeps space to scroll.
type panelHeadLayout struct{ n *nowPlayingPanel }

const (
	panelQueueRoom = 180
	panelSmallArt  = 88
)

// side is the cover size above the details, or 0 for the small cover.
func (l *panelHeadLayout) side(details fyne.Size) float32 {
	n := l.n
	h := n.root.Size().Height
	if h == 0 {
		return 300
	}
	rest := float32(0)
	for _, o := range n.content.Objects[1:] {
		if o.Visible() {
			rest += o.MinSize().Height + theme.Padding()
		}
	}
	free := h - rest - n.bottom.MinSize().Height - panelQueueRoom - 32 - details.Height
	side := min(300, free, n.content.Size().Width)
	if side < 150 {
		return 0
	}
	return side
}

func (l *panelHeadLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	d := objects[1].MinSize()
	if s := l.side(d); s > 0 {
		return fyne.NewSize(max(s, d.Width), s+4+d.Height)
	}
	return fyne.NewSize(panelSmallArt+12+d.Width, max(panelSmallArt, d.Height))
}

func (l *panelHeadLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	art, details := objects[0], objects[1]
	d := details.MinSize()
	if s := l.side(d); s > 0 {
		art.Move(fyne.NewPos((size.Width-s)/2, 0))
		art.Resize(fyne.NewSize(s, s))
		details.Move(fyne.NewPos(0, s+4))
		details.Resize(fyne.NewSize(size.Width, d.Height))
		return
	}
	art.Move(fyne.NewPos(0, (size.Height-panelSmallArt)/2))
	art.Resize(fyne.NewSize(panelSmallArt, panelSmallArt))
	x := float32(panelSmallArt + 12)
	details.Move(fyne.NewPos(x, (size.Height-d.Height)/2))
	details.Resize(fyne.NewSize(size.Width-x, d.Height))
}

// rolePanelGlow is the top of the now playing panel gradient.
var rolePanelGlow role = func(p Palette) color.Color { return mix(p.Surface, p.Accent, 0.2) }
