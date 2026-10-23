package gui

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// fullPlayer is the immersive now playing page: a large cover over the
// colors of the cover, the controls, and the lyrics or the queue beside.
type fullPlayer struct {
	app           *App
	root          fyne.CanvasObject
	glow          *canvas.LinearGradient
	glow2         *canvas.LinearGradient
	art           *artView
	vinyl         *vinyl
	title         *widget.Label
	artist        *widget.Label
	album         *widget.Label
	badge         *widget.Label
	seek          *widget.Slider
	pos           *widget.Label
	dur           *widget.Label
	play          *roundButton
	karaoke       *karaoke
	status        *widget.Label
	lyricsVersion int
	upNext        *fyne.Container
	tabs          *container.AppTabs
	key           string
	drag          bool
	seekAt        time.Time
	nextKey       string
	exitFull      *widget.Button
}

func newFullPlayer(a *App) *fullPlayer {
	f := &fullPlayer{app: a, lyricsVersion: -1}
	p := a.theme.p
	f.glow = canvas.NewLinearGradient(mix(p.Accent, p.BG, 0.3), p.BG, 0)
	f.glow2 = canvas.NewLinearGradient(withAlpha(p.BG, 0), withAlpha(p.BG, 0xa0), 90)
	f.art = newArtView(a, 360, 18)
	f.art.image.FillMode = canvas.ImageFillCover
	f.vinyl = newVinyl(a)
	f.art.onLoaded = func(img image.Image) {
		f.vinyl.SetImage(img)
		c := mix(p.Accent, p.BG, 0.3)
		if img != nil {
			c = dominantColor(img)
			if p.Light {
				c = mix(c, p.BG, 0.5)
			}
		}
		f.glow.StartColor = c
		f.glow.Refresh()
	}
	f.title = label(T("Nothing playing"))
	f.title.SizeName = theme.SizeNameHeadingText
	f.title.TextStyle = fyne.TextStyle{Bold: true}
	f.title.Wrapping = fyne.TextWrapWord
	f.artist = label("")
	f.artist.SizeName = theme.SizeNameSubHeadingText
	f.artist.Importance = widget.HighImportance
	f.album = dimLabel("")
	f.badge = captionLabel("")
	f.badge.TextStyle = fyne.TextStyle{Bold: true}

	f.seek = widget.NewSlider(0, 1)
	f.seek.Step = 0.1
	f.seek.OnChanged = func(v float64) { f.drag = true; f.pos.SetText(fmtDuration(v)) }
	f.seek.OnChangeEnded = func(v float64) { f.drag = false; f.seekAt = time.Now(); a.seekTo(v) }
	f.pos = captionLabel("0:00")
	f.dur = captionLabel("0:00")
	f.play = newRoundButton(theme.MediaPlayIcon(), 60, a.togglePlay)
	big := func(res fyne.Resource, fn func()) fyne.CanvasObject {
		b := iconButton(res, fn)
		return fixed(48, 48, b)
	}
	controls := container.NewHBox(spacer(),
		big(iconShuffle, a.toggleShuffle), big(theme.MediaSkipPreviousIcon(), a.prev),
		f.play,
		big(theme.MediaSkipNextIcon(), a.next), big(iconRepeat, a.cycleRepeat), spacer())
	seekRow := container.NewBorder(nil, nil, f.pos, f.dur, f.seek)
	actions := container.NewHBox(spacer(),
		flatButton(T("Start radio"), iconRadio, func() {
			if a.state.Track != nil {
				a.startRadio(*a.state.Track)
			}
		}),
		flatButton("", iconHeartLine, a.playerBar.toggleFavorite),
		flatButton("", theme.InfoIcon(), func() {
			if a.state.Track != nil {
				a.showTrackInfo(*a.state.Track)
			}
		}),
		flatButton("DJ", iconDJ, func() { a.show("dj") }),
		spacer())

	left := container.NewVBox(spacer(),
		container.New(&squareLayout{max: 420, app: a}, f.art.Object(), f.vinyl),
		fixed(0, 8),
		f.badge, f.title, f.artist, f.album,
		fixed(0, 6), seekRow, controls, actions,
		spacer(),
	)

	f.karaoke = newKaraoke(a)
	lyricsClip := container.NewScroll(f.karaoke)
	lyricsClip.Direction = container.ScrollNone
	f.status = captionLabel("")
	// The gear over the lyrics reloads them, picks the source and moves
	// the offset, right where a wrong lyric shows.
	gear := a.lyrics.gearButton()
	lyricsPane := container.NewBorder(nil, padded(4, 8, 0, 8, container.NewBorder(nil, nil, nil, gear, f.status)), nil, nil, lyricsClip)
	f.upNext = container.NewVBox()
	f.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon(T("Lyrics"), iconLyrics, lyricsPane),
		container.NewTabItemWithIcon(T("Up next"), iconQueue, container.NewVScroll(f.upNext)),
	)

	back := flatButton(T("Back"), theme.NavigateBackIcon(), func() { a.show(a.prefs.StringWithFallback(prefLastSection, "home")) })
	// Full screen starts from the player bar, which full screen hides, so
	// the way out shows only there.
	f.exitFull = flatButton(T("Exit full screen"), theme.ViewRestoreIcon(), func() { a.setImmersive(false) })
	f.exitFull.Hide()
	body := container.New(&splitLayout{left: 0.46, gap: 36}, padded(0, 0, 0, 0, left), panel(withAlpha(p.Surface, 0x90), 16, padded(10, 10, 10, 10, f.tabs)))
	f.root = container.NewStack(f.glow, f.glow2,
		container.NewBorder(padded(10, 18, 0, 18, container.NewBorder(nil, nil, back, f.exitFull)), nil, nil, nil, padded(4, 36, 24, 36, body)))
	a.onState(f.update)
	return f
}

func (f *fullPlayer) Object() fyne.CanvasObject { return f.root }

func (f *fullPlayer) Shown() {
	f.applyCoverStyle()
	f.key = ""
	f.karaoke.Start()
	f.update(f.app.state)
}

// Hidden stops the lyrics animation while another page shows.
func (f *fullPlayer) Hidden() {
	f.karaoke.Stop()
	f.vinyl.Stop()
	f.app.setImmersive(false)
}

func (f *fullPlayer) update(s ipc.RuntimeSnapshot) {
	a := f.app
	if a.current != "player" {
		return
	}
	if s.State == "playing" {
		f.play.SetIcon(theme.MediaPauseIcon())
	} else {
		f.play.SetIcon(theme.MediaPlayIcon())
	}
	if f.vinyl.Visible() {
		f.vinyl.applyPalette(currentPalette())
		f.vinyl.SetPlaying(s.State == "playing")
	}
	if s.Track != nil {
		t := *s.Track
		title, sub := trackTitle(t)
		key := t.Path + "|" + title
		if key != f.key {
			f.key = key
			f.art.Load(t)
			f.title.SetText(title)
			f.artist.SetText(sub)
			album := t.Album
			if t.Year > 0 {
				album = strings.TrimSpace(fmt.Sprintf("%s · %d", album, t.Year))
			}
			f.album.SetText(album)
		}
	}
	// DJ badge and analysis.
	badge := ""
	if a.opts.Mixer != nil && a.opts.Mixer.Mixing() {
		if ev, ok := a.opts.Mixer.LastTransition(); ok {
			badge = "🎧 " + djLabel(ev.Label)
		}
	} else if a.opts.DJ != nil && s.Track != nil {
		if an, ok := a.opts.DJ.Get(s.Track.Path); ok && an.BPM > 0 {
			badge = fmt.Sprintf("%.0f BPM", an.BPM)
			if c := an.Camelot(); c != "" {
				badge += " · " + c + " (" + an.KeyName() + ")"
			}
		}
	}
	if f.badge.Text != badge {
		f.badge.SetText(badge)
	}
	if !f.drag && time.Since(f.seekAt) > 600*time.Millisecond {
		if s.Duration > 0 {
			f.seek.Max = s.Duration
			f.seek.Value = math.Min(s.Position, s.Duration)
		}
		f.seek.Refresh()
		f.pos.SetText(fmtDuration(s.Position))
	}
	f.dur.SetText(fmtDuration(s.Duration))
	if a.lyrics.version != f.lyricsVersion {
		f.lyricsVersion = a.lyrics.version
		msg := a.lyrics.status.Text
		f.status.SetText(msg)
		f.karaoke.SetLines(a.lyrics.lines, msg)
	}
	f.karaoke.SetPosition(s.Position + a.lyrics.offsetSecs())
	next := a.queue.upcoming(12)
	nextKey := fmt.Sprint(next, s.PlaylistRevision)
	if nextKey == f.nextKey {
		return
	}
	f.nextKey = nextKey
	f.upNext.Objects = nil
	for _, i := range next {
		i := i
		t := a.queue.all[i]
		title, sub := trackTitle(t)
		tl := newTile(a, title, sub, t.AlbumArtURL, func() { a.do("queue.play", P{"index": i}, nil) })
		if t.AlbumArtURL == "" {
			tl.art.Load(t)
		}
		f.upNext.Add(tl)
	}
	f.upNext.Refresh()
}

// applyCoverStyle shows the cover or the record, as the setting asks.
func (f *fullPlayer) applyCoverStyle() {
	if f.app.coverStyle() == coverStyleVinyl {
		f.art.Object().Hide()
		f.vinyl.Show()
	} else {
		f.vinyl.Stop()
		f.vinyl.Hide()
		f.art.Object().Show()
	}
}

// squareLayout sizes its child as a square that fits the width and leaves
// reserve pixels of the window height for what follows it.
type squareLayout struct {
	max float32
	app *App
}

func (l *squareLayout) side(w float32) float32 {
	h := l.app.win.Canvas().Size().Height
	// Room for the header, the details and the controls, and the player
	// bar unless the page is immersive.
	reserve := float32(560)
	if l.app.immersive {
		reserve = 440
	}
	return max(120, min(l.max, w, h-reserve))
}

func (l *squareLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	s := l.side(l.max)
	return fyne.NewSize(140, s)
}

func (l *squareLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	s := l.side(size.Width)
	for _, o := range objects {
		o.Move(fyne.NewPos((size.Width-s)/2, 0))
		o.Resize(fyne.NewSize(s, s))
	}
}
