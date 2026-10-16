package gui

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// playerBar is the transport bar at the bottom of the window.
type playerBar struct {
	app  *App
	root fyne.CanvasObject

	art      *artView
	title    *widget.Label
	artist   *widget.Label
	favorite *widget.Button

	shuffle *widget.Button
	repeat  *widget.Button
	play    *roundButton

	seek     *widget.Slider
	pos      *widget.Label
	dur      *widget.Label
	seeking  bool
	seekedAt time.Time

	volume    *widget.Slider
	volIcon   *widget.Button
	volLabel  *widget.Label
	volDrag   time.Time
	preMute   float64
	speed     *widget.Button
	panelBtn  *widget.Button
	lyricsBtn *widget.Button
	eqBtn     *widget.Button

	lastTrack string
	extras    *fyne.Container
	faved     bool
}

// barLayout splits the player bar into track info, transport and the
// controls on the right, and drops the extra buttons when narrow.
type barLayout struct{ b *playerBar }

func (l *barLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objects {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(560, h)
}

func (l *barLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	left, center, right := objects[0], objects[1], objects[2]
	narrow := size.Width < 1000
	if narrow != l.b.extras.Hidden {
		if narrow {
			l.b.extras.Hide()
		} else {
			l.b.extras.Show()
		}
	}
	leftW := min(max(size.Width*0.27, 170), 330)
	rightW := right.MinSize().Width
	if !narrow {
		rightW = min(max(rightW, size.Width*0.27), 420)
	}
	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(leftW, size.Height))
	right.Move(fyne.NewPos(size.Width-rightW, 0))
	right.Resize(fyne.NewSize(rightW, size.Height))
	center.Move(fyne.NewPos(leftW, 0))
	center.Resize(fyne.NewSize(size.Width-leftW-rightW, size.Height))
}

func newPlayerBar(a *App) *playerBar {
	b := &playerBar{app: a, preMute: 0}

	b.art = newArtView(a, 56, 8)
	b.art.onTap = func() { a.show("player") }
	b.title = trunc(boldLabel(T("Nothing playing")))
	b.artist = trunc(captionLabel("Pick something from the library"))
	b.favorite = iconButton(iconHeartLine, b.toggleFavorite)
	b.favorite.Disable()
	info := container.NewBorder(nil, nil, container.NewVBox(spacer(), b.art.Object(), spacer()), container.NewVBox(spacer(), b.favorite, spacer()),
		container.NewVBox(spacer(), container.New(&tightVBox{}, b.title, b.artist), spacer()))
	left := info

	b.shuffle = newToggleButton(iconShuffle, a.toggleShuffle)
	b.repeat = newToggleButton(iconRepeat, a.cycleRepeat)
	prev := iconButton(theme.MediaSkipPreviousIcon(), a.prev)
	next := iconButton(theme.MediaSkipNextIcon(), a.next)
	b.play = newRoundButton(theme.MediaPlayIcon(), 40, a.togglePlay)
	controls := container.NewHBox(spacer(), b.shuffle, prev, b.play, next, b.repeat, spacer())

	b.seek = widget.NewSlider(0, 1)
	b.seek.Step = 0.1
	b.seek.OnChanged = func(v float64) {
		b.seeking = true
		b.pos.SetText(fmtDuration(v))
	}
	b.seek.OnChangeEnded = func(v float64) {
		b.seeking = false
		b.seekedAt = time.Now()
		a.seekTo(v)
	}
	b.pos = captionLabel("0:00")
	b.pos.Alignment = fyne.TextAlignTrailing
	b.dur = captionLabel("0:00")
	seekRow := container.NewBorder(nil, nil, fixed(54, 0, b.pos), fixed(54, 0, b.dur), b.seek)
	center := container.NewVBox(controls, seekRow)

	b.volume = widget.NewSlider(a.opts.VolumeMin, 6)
	if b.volume.Min >= b.volume.Max {
		b.volume.Min = -50
	}
	b.volume.Step = 0.5
	b.volume.OnChanged = func(v float64) {
		b.volDrag = time.Now()
		b.volLabel.SetText(volumeText(v, b.volume.Min))
		a.setVolume(v)
	}
	b.volIcon = iconButton(theme.VolumeUpIcon(), b.toggleMute)
	b.volLabel = captionLabel("")
	b.volLabel.Alignment = fyne.TextAlignTrailing
	b.speed = widget.NewButton("1.00×", b.showSpeedMenu)
	b.speed.Importance = widget.LowImportance
	b.lyricsBtn = newToggleButton(iconLyrics, func() { a.show("player") })
	b.eqBtn = newToggleButton(iconEqualizer, func() { a.show("eq") })
	b.panelBtn = newToggleButton(iconSidebar, a.toggleRightPane)
	b.extras = container.NewHBox(b.speed, b.lyricsBtn, b.eqBtn, b.panelBtn)
	right := container.NewVBox(spacer(), container.NewHBox(spacer(), b.extras, b.volIcon,
		fixed(100, 0, container.NewVBox(spacer(), b.volume, spacer())), fixed(48, 0, container.NewVBox(spacer(), b.volLabel, spacer()))), spacer())

	bar := container.New(&barLayout{b: b}, left, padded(0, 12, 0, 12, center), right)
	bg := tRect(roleSurface)
	b.root = container.NewStack(bg, padded(8, 14, 8, 14, bar))
	a.onState(b.update)
	return b
}

func (b *playerBar) Object() fyne.CanvasObject { return b.root }

func volumeText(db, min float64) string {
	if db <= min+0.01 {
		return "mute"
	}
	return fmt.Sprintf("%+.0f dB", db)
}

func (b *playerBar) update(s ipc.RuntimeSnapshot) {
	switch s.State {
	case "playing":
		b.play.SetIcon(theme.MediaPauseIcon())
	default:
		b.play.SetIcon(theme.MediaPlayIcon())
	}

	if s.Track != nil {
		title, sub := trackTitle(*s.Track)
		if b.title.Text != title {
			b.title.SetText(title)
		}
		if sub == "" {
			sub = s.Track.Album
		}
		if b.artist.Text != sub {
			b.artist.SetText(sub)
		}
		key := s.Track.Path + "|" + s.Track.AlbumArtURL
		if key != b.lastTrack {
			b.lastTrack = key
			b.art.Load(*s.Track)
		}
		b.favorite.Enable()
		if s.Track.Bookmark != b.faved || b.favorite.Icon == nil {
			b.faved = s.Track.Bookmark
			if b.faved {
				b.favorite.SetIcon(theme.NewPrimaryThemedResource(iconHeart))
			} else {
				b.favorite.SetIcon(iconHeartLine)
			}
		}
	} else {
		if b.lastTrack != "" {
			b.lastTrack = ""
			b.art.Clear()
		}
		b.title.SetText(T("Nothing playing"))
		b.artist.SetText("Pick something from the library")
		b.favorite.Disable()
	}

	duration := s.Duration
	if !b.seeking && time.Since(b.seekedAt) > 600*time.Millisecond {
		if duration > 0 {
			b.seek.Max = duration
			b.seek.Value = math.Min(s.Position, duration)
		} else {
			b.seek.Max = 1
			b.seek.Value = 0
		}
		b.seek.Refresh()
		b.pos.SetText(fmtDuration(s.Position))
	}
	if duration > 0 {
		b.dur.SetText(fmtDuration(duration))
	} else if s.Track != nil && s.Track.Stream {
		b.dur.SetText("LIVE")
	} else {
		b.dur.SetText("--:--")
	}
	if s.Seekable && duration > 0 {
		b.seek.Enable()
	} else {
		b.seek.Disable()
	}

	setActive(b.shuffle, s.Shuffle != nil && *s.Shuffle)
	switch s.Repeat {
	case "One":
		setToggleIcon(b.repeat, iconRepeatOne)
		setActive(b.repeat, true)
	case "All":
		setToggleIcon(b.repeat, iconRepeat)
		setActive(b.repeat, true)
	default:
		setToggleIcon(b.repeat, iconRepeat)
		setActive(b.repeat, false)
	}

	if time.Since(b.volDrag) > 800*time.Millisecond {
		if b.volume.Value != s.Volume {
			b.volume.Value = s.Volume
			b.volume.Refresh()
		}
		b.volLabel.SetText(volumeText(s.Volume, b.volume.Min))
	}
	switch {
	case s.Volume <= b.volume.Min+0.01:
		b.volIcon.SetIcon(theme.VolumeMuteIcon())
	case s.Volume < -20:
		b.volIcon.SetIcon(theme.VolumeDownIcon())
	default:
		b.volIcon.SetIcon(theme.VolumeUpIcon())
	}

	speed := s.Speed
	if speed == 0 {
		speed = 1
	}
	if text := fmt.Sprintf("%.2f×", speed); b.speed.Text != text {
		b.speed.SetText(text)
	}
	setActive(b.panelBtn, b.app.prefs.BoolWithFallback(prefRightPane, true))
	setActive(b.lyricsBtn, b.app.current == "player")
	setActive(b.eqBtn, b.app.current == "eq" || (s.EQPreset != "" && s.EQPreset != "Flat"))
}

func (b *playerBar) toggleMute() {
	s := b.app.state
	if s.Volume <= b.volume.Min+0.01 {
		target := b.preMute
		if target <= b.volume.Min {
			target = 0
		}
		b.app.setVolume(target)
		return
	}
	b.preMute = s.Volume
	b.app.setVolume(b.volume.Min)
}

func (b *playerBar) toggleFavorite() {
	s := b.app.state
	if s.Track == nil {
		return
	}
	t := *s.Track
	b.app.do("playlist.bookmark", P{"provider": "local", "playlist": "", "track": t}, func(Result) {
		if t.Bookmark {
			b.app.toast(T("Removed from favorites"))
		} else {
			b.app.toast(T("Added to favorites ♥"))
		}
	})
}

func (b *playerBar) showSpeedMenu() {
	var items []*fyne.MenuItem
	current := b.app.state.Speed
	for _, v := range []float64{0.5, 0.75, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2} {
		v := v
		it := fyne.NewMenuItem(fmt.Sprintf("%.2f×", v), func() { b.app.setSpeed(v) })
		it.Checked = math.Abs(current-v) < 0.001
		items = append(items, it)
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(b.speed)
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, b.app.win.Canvas(), pos.Subtract(fyne.NewPos(0, float32(len(items))*34)))
}

// roundButton is the circular play button.
type roundButton struct {
	widget.BaseWidget
	circle  *canvas.Circle
	icon    *canvas.Image
	size    float32
	onTap   func()
	hovered bool
	// onHover, when set, tells the widget around the button that the
	// pointer entered or left it.
	onHover func(in bool)
}

func newRoundButton(res fyne.Resource, size float32, tapped func()) *roundButton {
	r := &roundButton{size: size, onTap: tapped}
	r.circle = canvas.NewCircle(theme.Color(theme.ColorNamePrimary))
	r.icon = canvas.NewImageFromResource(theme.NewColoredResource(res, theme.ColorNameForegroundOnPrimary))
	r.icon.FillMode = canvas.ImageFillContain
	r.ExtendBaseWidget(r)
	return r
}

func (r *roundButton) SetIcon(res fyne.Resource) {
	inv := theme.NewColoredResource(res, theme.ColorNameForegroundOnPrimary)
	if r.icon.Resource != nil && r.icon.Resource.Name() == inv.Name() {
		return
	}
	r.icon.Resource = inv
	r.icon.Refresh()
}

func (r *roundButton) CreateRenderer() fyne.WidgetRenderer {
	return &roundRenderer{r: r}
}

func (r *roundButton) MinSize() fyne.Size { return fyne.NewSize(r.size+8, r.size+8) }

func (r *roundButton) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *roundButton) MouseIn(*desktop.MouseEvent) {
	r.hovered = true
	r.Refresh()
	if r.onHover != nil {
		r.onHover(true)
	}
}
func (r *roundButton) MouseMoved(*desktop.MouseEvent) {}
func (r *roundButton) MouseOut() {
	r.hovered = false
	r.Refresh()
	if r.onHover != nil {
		r.onHover(false)
	}
}
func (r *roundButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

type roundRenderer struct{ r *roundButton }

func (rr *roundRenderer) Layout(size fyne.Size) {
	d := rr.r.size
	if rr.r.hovered {
		d += 4
	}
	rr.r.circle.Move(fyne.NewPos((size.Width-d)/2, (size.Height-d)/2))
	rr.r.circle.Resize(fyne.NewSize(d, d))
	is := rr.r.size * 0.55
	rr.r.icon.Move(fyne.NewPos((size.Width-is)/2, (size.Height-is)/2))
	rr.r.icon.Resize(fyne.NewSize(is, is))
}
func (rr *roundRenderer) MinSize() fyne.Size { return rr.r.MinSize() }
func (rr *roundRenderer) Refresh() {
	c := theme.Color(theme.ColorNamePrimary)
	rr.r.circle.FillColor = c
	if rr.r.hovered {
		if n, ok := c.(color.NRGBA); ok {
			rr.r.circle.FillColor = mix(n, color.NRGBA{255, 255, 255, 255}, 0.12)
		}
	}
	rr.Layout(rr.r.Size())
	rr.r.circle.Refresh()
	rr.r.icon.Refresh()
}
func (rr *roundRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{rr.r.circle, rr.r.icon}
}
func (rr *roundRenderer) Destroy() {}
