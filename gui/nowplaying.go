package gui

import (
	"fmt"
	"image"
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

	art     *artView
	title   *widget.Label
	artist  *widget.Label
	album   *widget.Label
	extra   *widget.Label
	lyric   *widget.Label
	vis     *visualizer
	upNext  *fyne.Container
	content *fyne.Container
	glow    *canvas.LinearGradient
	lastKey string
	nextKey string
}

func newNowPlayingPanel(a *App) *nowPlayingPanel {
	n := &nowPlayingPanel{app: a}
	n.art = newArtView(a, 300, 14)
	n.art.image.FillMode = canvas.ImageFillCover
	p := a.theme.p
	n.glow = canvas.NewLinearGradient(mix(p.Surface, p.Accent, 0.25), p.Surface, 0)
	n.art.onLoaded = func(img image.Image) {
		a.setCoverImage(img)
		a.followCover()
		c := mix(p.Surface, p.Accent, 0.25)
		if img != nil {
			c = dominantColor(img)
			if p.Light {
				c = mix(c, p.Surface, 0.6)
			}
		}
		n.glow.StartColor = c
		n.glow.Refresh()
	}
	n.art.onTap = func() { a.show("player") }
	n.title = heading(T("Nothing playing"))
	n.title.SizeName = theme.SizeNameSubHeadingText
	n.title.Wrapping = fyne.TextWrapWord
	n.title.Truncation = fyne.TextTruncateOff
	n.artist = trunc(label(""))
	n.artist.Importance = widget.HighImportance
	n.album = trunc(dimLabel(""))
	n.extra = captionLabel("")
	n.extra.Wrapping = fyne.TextWrapWord
	n.extra.Truncation = fyne.TextTruncateOff
	n.lyric = label("")
	n.lyric.Wrapping = fyne.TextWrapWord
	n.lyric.Truncation = fyne.TextTruncateOff
	n.lyric.TextStyle = fyne.TextStyle{Italic: true}
	n.lyric.Hide()

	n.vis = newVisualizer(a, false)
	n.vis.active = a.rightShown.Load
	n.vis.onTap = a.showFullVisualizer
	visFrame := tPanel(roleSurface2, 12, padded(8, 8, 8, 8, fixed(0, 120, n.vis)))
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

	n.upNext = container.NewVBox()
	n.content = container.NewVBox(
		container.NewCenter(n.art.Object()),
		fixed(0, 4),
		n.title, n.artist, n.album, n.extra, actions,
		n.lyric,
		fixed(0, 6),
		visHeader, visFrame,
		fixed(0, 6),
		sectionTitle(T("Up next")),
		n.upNext,
	)
	bg := tRect(roleSurface)
	glow := container.NewVBox(fixed(0, 520, n.glow))
	n.root = container.NewStack(bg, glow, container.NewVScroll(padded(16, 16, 16, 16, n.content)))
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

// updateLyric shows the current synced line under the details.
func (n *nowPlayingPanel) updateLyric(s ipc.RuntimeSnapshot) {
	line := n.app.lyrics.lineAt(s.Position)
	if line == "" {
		if !n.lyric.Hidden {
			n.lyric.Hide()
			n.content.Refresh()
		}
		return
	}
	if n.lyric.Text != "♪ "+line {
		n.lyric.SetText("♪ " + line)
	}
	if n.lyric.Hidden {
		n.lyric.Show()
		n.content.Refresh()
	}
}

func (n *nowPlayingPanel) updateUpNext() {
	next := n.app.queue.upcoming(5)
	key := ""
	for _, i := range next {
		key += fmt.Sprintf("%d:%s|", i, n.app.queue.tracks.Tracks[i].Path)
	}
	if key == n.nextKey {
		return
	}
	n.nextKey = key
	n.upNext.Objects = nil
	if len(next) == 0 {
		n.upNext.Add(captionLabel(T("The queue ends here")))
	}
	for _, i := range next {
		i := i
		t := n.app.queue.tracks.Tracks[i]
		title, sub := trackTitle(t)
		btn := widget.NewButton("", func() { n.app.do("queue.play", P{"index": i}, nil) })
		btn.Importance = widget.LowImportance
		txt := container.New(&tightVBox{}, trunc(label(title)), trunc(captionLabel(sub)))
		n.upNext.Add(container.NewStack(btn, padded(0, 4, 0, 4, txt)))
	}
	n.upNext.Refresh()
}
