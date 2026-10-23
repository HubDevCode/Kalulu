package gui

import (
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

const (
	prefLyricsSize   = "lyrics.size"
	prefLyricsAlign  = "lyrics.align"
	prefLyricsSmooth = "lyrics.smooth"
)

// kWord is a laid-out word with its timing.
type kWord struct {
	text       *canvas.Text
	x, y, w    float32
	start, end float64
}

// kLine is a laid-out lyric line.
type kLine struct {
	y, h  float32
	w     float32 // widest row
	start float64
	words []*kWord
	// emph eases to 1 while the line is sung and back to 0 after, so the
	// highlight passes smoothly from one line to the next.
	emph float64
}

// karaoke renders synced lyrics with per-word highlighting and smooth
// scrolling, the single lyrics view of Kalulu.
type karaoke struct {
	widget.BaseWidget
	app *App

	mu       sync.Mutex
	lines    []ipc.LyricLine
	synced   bool
	layout   []*kLine
	objects  []fyne.CanvasObject
	width    float32
	height   float32
	scroll   float32
	target   float32
	manualAt time.Time
	pos      float64
	current  int
	message  *canvas.Text
	stop     chan struct{}
	running  bool
	// wanted is true between Start and Stop. Fyne destroys the renderer
	// of a widget that stayed off screen for a while, which halts the
	// frames; a new renderer starts them again while wanted.
	wanted bool

	// compact is the three-line ticker of the now playing panel: smaller
	// centered text, the sung line in the middle with a glow behind it.
	compact bool
	glow    []*canvas.Rectangle // soft layers, widest first
	glowY   float32
	glowW   float32
	// active, when set, pauses the frames while it reports false.
	active func() bool
	// outer receives the wheel in compact mode, so the panel scrolls.
	outer fyne.Scrollable
}

func newKaraoke(a *App) *karaoke {
	k := &karaoke{app: a, current: -1}
	k.message = canvas.NewText("", a.theme.p.FGDim)
	k.message.Alignment = fyne.TextAlignCenter
	k.message.TextSize = 18
	k.ExtendBaseWidget(k)
	return k
}

// newLyricTicker is the compact karaoke of the now playing panel.
func newLyricTicker(a *App) *karaoke {
	k := newKaraoke(a)
	k.compact = true
	for range 5 {
		r := canvas.NewRectangle(color.Transparent)
		r.Hide()
		k.glow = append(k.glow, r)
	}
	k.message.TextSize = 13
	return k
}

// anchor is where the sung line sits, as a share of the height.
func (k *karaoke) anchor() float32 {
	if k.compact {
		return 0.5
	}
	return 0.38
}

func (k *karaoke) textSize() float32 {
	if k.compact {
		return 15
	}
	return float32(k.app.prefs.FloatWithFallback(prefLyricsSize, 26))
}

func (k *karaoke) centered() bool {
	if k.compact {
		return true
	}
	return k.app.prefs.StringWithFallback(prefLyricsAlign, "left") == "center"
}

// SetLines replaces the lyrics.
func (k *karaoke) SetLines(lines []ipc.LyricLine, message string) {
	k.mu.Lock()
	k.lines = lines
	k.synced = hasTimings(lines)
	k.current = -1
	k.scroll, k.target = 0, 0
	k.width = 0
	k.mu.Unlock()
	k.message.Text = message
	k.Refresh()
}

// SetPosition moves the playhead (seconds, offset applied).
func (k *karaoke) SetPosition(pos float64) {
	k.mu.Lock()
	k.pos = pos
	k.mu.Unlock()
}

func (k *karaoke) CreateRenderer() fyne.WidgetRenderer {
	k.mu.Lock()
	wanted := k.wanted
	k.mu.Unlock()
	if wanted {
		k.run()
	}
	return &karaokeRenderer{k: k}
}

// relayout wraps the words of every line to the width.
func (k *karaoke) relayout(width float32) {
	k.width = width
	size := k.textSize()
	style := fyne.TextStyle{Bold: true}
	space := fyne.MeasureText(" ", size, style).Width
	lineH := fyne.MeasureText("Ag", size, style).Height * 1.12
	gap := lineH * 0.55
	maxW := width - 16
	center := k.centered()
	k.layout = k.layout[:0]
	k.objects = k.objects[:0]
	y := float32(0)
	for i, l := range k.lines {
		kl := &kLine{y: y, start: l.Start}
		var end float64
		if i+1 < len(k.lines) {
			end = k.lines[i+1].Start
		} else {
			end = l.Start + 5
		}
		texts := strings.Fields(l.Text)
		if len(texts) == 0 {
			texts = []string{"♪"}
		}
		// Word timings: the source's, or spread over 85% of the line by
		// length, which follows how singing takes longer for long words.
		starts := make([]float64, len(texts))
		ends := make([]float64, len(texts))
		if len(l.Words) == len(texts) {
			for j, w := range l.Words {
				starts[j] = w.Start
				if j+1 < len(l.Words) {
					ends[j] = l.Words[j+1].Start
				} else {
					ends[j] = math.Min(end, w.Start+1.2)
				}
			}
		} else {
			span := math.Max(0.4, math.Min(end-l.Start, 12)*0.85)
			total := 0
			for _, t := range texts {
				total += len([]rune(t)) + 1
			}
			at := l.Start
			for j, t := range texts {
				d := span * float64(len([]rune(t))+1) / float64(total)
				starts[j], ends[j] = at, at+d
				at += d
			}
		}
		// Wrap.
		type row struct {
			words []*kWord
			w     float32
		}
		rows := []*row{{}}
		for j, t := range texts {
			w := fyne.MeasureText(t, size, style).Width
			r := rows[len(rows)-1]
			if r.w > 0 && r.w+space+w > maxW {
				r = &row{}
				rows = append(rows, r)
			}
			if r.w > 0 {
				r.w += space
			}
			txt := canvas.NewText(t, k.app.theme.p.FG)
			txt.TextSize = size
			txt.TextStyle = style
			kw := &kWord{text: txt, x: r.w, w: w, start: starts[j], end: ends[j]}
			r.w += w
			r.words = append(r.words, kw)
		}
		for ri, r := range rows {
			offset := float32(8)
			if center {
				offset = (width - r.w) / 2
			}
			for _, kw := range r.words {
				kw.x += offset
				kw.y = y + float32(ri)*lineH
				kl.words = append(kl.words, kw)
				k.objects = append(k.objects, kw.text)
			}
		}
		for _, r := range rows {
			kl.w = max(kl.w, r.w)
		}
		kl.h = float32(len(rows)) * lineH
		y += kl.h + gap
		k.layout = append(k.layout, kl)
	}
}

// lineAtY returns the line under a y in the view.
func (k *karaoke) lineAtY(y float32) int {
	for i, l := range k.layout {
		top := l.y - k.scroll
		if y >= top && y <= top+l.h {
			return i
		}
	}
	return -1
}

func (k *karaoke) Tapped(ev *fyne.PointEvent) {
	k.mu.Lock()
	i := k.lineAtY(ev.Position.Y)
	synced := k.synced
	var start float64
	if i >= 0 {
		start = k.layout[i].start
	}
	k.mu.Unlock()
	if i >= 0 && synced {
		k.manualAt = time.Time{}
		k.app.seekTo(math.Max(0, start-k.app.lyrics.offsetSecs()))
	}
}

func (k *karaoke) Scrolled(ev *fyne.ScrollEvent) {
	if k.compact {
		if k.outer != nil {
			k.outer.Scrolled(ev)
		}
		return
	}
	k.mu.Lock()
	k.manualAt = time.Now()
	k.target -= ev.Scrolled.DY
	k.target = float32(math.Max(-float64(k.height)/2, float64(k.target)))
	k.mu.Unlock()
}

func (k *karaoke) Cursor() desktop.Cursor { return desktop.PointerCursor }

// Start runs the animation while the view is visible.
func (k *karaoke) Start() {
	k.mu.Lock()
	k.wanted = true
	k.mu.Unlock()
	k.run()
}

// run starts the frame loop unless it runs.
func (k *karaoke) run() {
	k.mu.Lock()
	if k.running {
		k.mu.Unlock()
		return
	}
	k.running = true
	k.stop = make(chan struct{})
	stop := k.stop
	k.mu.Unlock()
	go func() {
		ticker := time.NewTicker(time.Second / 40)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			if !k.app.winVisible.Load() || (k.active != nil && !k.active()) {
				continue
			}
			runOnUI(k.animate)
		}
	}()
}

// Stop ends the animation.
func (k *karaoke) Stop() {
	k.mu.Lock()
	k.wanted = false
	k.mu.Unlock()
	k.halt()
}

// halt stops the frame loop, keeping wanted.
func (k *karaoke) halt() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.running {
		close(k.stop)
		k.running = false
	}
}

// animate advances the scroll and the colors by one frame.
func (k *karaoke) animate() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.layout) == 0 {
		return
	}
	pos := k.pos
	cur := -1
	if k.synced {
		for i, l := range k.layout {
			if l.start <= pos {
				cur = i
			}
		}
	}
	follow := k.compact || time.Since(k.manualAt) > 3*time.Second
	if cur >= 0 && follow {
		l := k.layout[cur]
		k.target = l.y + l.h/2 - k.height*k.anchor()
	} else if cur < 0 && follow && k.synced {
		k.target = 0
		if k.compact && len(k.layout) > 0 {
			// Before the first line, it waits just below the middle.
			k.target = -k.height * 0.25
		}
	}
	smooth := float32(k.app.prefs.FloatWithFallback(prefLyricsSmooth, 0.16))
	ease := 0.12
	if k.app.reduceMotion() {
		smooth, ease = 1, 1
	}
	moved := math.Abs(float64(k.target-k.scroll)) > 0.5
	if moved {
		k.scroll += (k.target - k.scroll) * smooth
	}
	p := currentPalette()
	k.current = cur
	focus := k.height * k.anchor()
	for i, l := range k.layout {
		want := 0.0
		if i == cur {
			want = 1
		}
		if l.emph != want {
			l.emph += (want - l.emph) * ease
			if math.Abs(want-l.emph) < 0.01 {
				l.emph = want
			}
		}
		top := l.y - k.scroll
		visible := top+l.h > -40 && top < k.height+40
		// Lines dim with their distance from the sung one, gradually.
		dist := math.Abs(float64(top + l.h/2 - focus))
		fade := k.fade(dist, i < cur)
		for _, w := range l.words {
			if visible {
				w.text.Move(fyne.NewPos(w.x, w.y-k.scroll))
				w.text.Show()
			} else {
				w.text.Hide()
				continue
			}
			c := k.wordColor(l, w, pos, p, fade)
			if c != w.text.Color {
				w.text.Color = c
				w.text.Refresh()
			} else if moved {
				w.text.Refresh()
			}
		}
	}
	k.placeGlow(cur, p)
}

// fade is how far toward the background a line that is not sung goes, for
// its distance in points from the sung line.
func (k *karaoke) fade(dist float64, past bool) float64 {
	if !k.synced {
		return 0
	}
	span := math.Max(80, float64(k.height)*0.6)
	near, far := 0.32, 0.86
	if k.compact {
		span, near, far = math.Max(40, float64(k.height)*0.5), 0.5, 0.8
	}
	f := near + (far-near)*smoothstep(dist/span)
	if past {
		f = math.Min(0.92, f+0.06)
	}
	return f
}

// wordColor is the color of a word for the playhead: sung words take the
// accent, the word being sung blends into it, and the other lines blend
// into the background by fade. The colors are opaque: a text with alpha
// drew unevenly.
func (k *karaoke) wordColor(l *kLine, w *kWord, pos float64, p Palette, fade float64) color.Color {
	if !k.synced {
		return p.FG
	}
	bg := mix(p.BG, p.Surface, 0.5)
	idle := mix(p.FG, bg, fade)
	if l.emph == 0 {
		return idle
	}
	unsung := mix(p.FG, bg, 0.12)
	var sung color.NRGBA
	switch {
	case pos >= w.end:
		sung = p.Accent
	case pos <= w.start:
		sung = unsung
	default:
		t := (pos - w.start) / math.Max(0.05, w.end-w.start)
		sung = mix(unsung, p.Accent, math.Min(1, t*1.6))
	}
	if k.compact && pos >= w.end {
		// In the ticker the sung words stay bright, the glow carries the
		// accent.
		sung = mix(p.FG, p.Accent, 0.35)
	}
	return mix(idle, sung, smoothstep(l.emph))
}

// placeGlow moves the soft light behind the sung line of the ticker.
func (k *karaoke) placeGlow(cur int, p Palette) {
	if len(k.glow) == 0 {
		return
	}
	if cur < 0 || !k.synced {
		for _, r := range k.glow {
			r.Hide()
		}
		return
	}
	l := k.layout[cur]
	y := l.y + l.h/2 - k.scroll
	w := min(k.width, l.w+16)
	if k.glow[0].Hidden {
		k.glowY, k.glowW = y, w
	}
	k.glowY += (y - k.glowY) * 0.2
	k.glowW += (w - k.glowW) * 0.2
	// Stacked translucent layers make a soft light that is brightest in
	// the middle of the line.
	fill := withAlpha(p.Accent, 0x10)
	for i, r := range k.glow {
		grow := float32(len(k.glow)-1-i) * 8
		gw, gh := min(k.width, k.glowW+grow*2), l.h+6+grow
		r.FillColor = fill
		r.CornerRadius = gh / 2
		r.Move(fyne.NewPos((k.width-gw)/2, k.glowY-gh/2))
		r.Resize(fyne.NewSize(gw, gh))
		r.Show()
		r.Refresh()
	}
}

type karaokeRenderer struct{ k *karaoke }

func (r *karaokeRenderer) Layout(size fyne.Size) {
	k := r.k
	k.mu.Lock()
	k.height = size.Height
	if size.Width != k.width {
		k.relayout(size.Width)
	}
	k.mu.Unlock()
	r.k.message.Move(fyne.NewPos(0, size.Height/2-14))
	r.k.message.Resize(fyne.NewSize(size.Width, 28))
	k.animate()
}

func (r *karaokeRenderer) MinSize() fyne.Size {
	if r.k.compact {
		return fyne.NewSize(120, 60)
	}
	return fyne.NewSize(200, 160)
}

func (r *karaokeRenderer) Refresh() {
	k := r.k
	k.mu.Lock()
	k.width = 0
	k.mu.Unlock()
	r.Layout(k.Size())
	canvas.Refresh(k)
}

func (r *karaokeRenderer) Objects() []fyne.CanvasObject {
	k := r.k
	k.mu.Lock()
	defer k.mu.Unlock()
	var objs []fyne.CanvasObject
	for _, r := range k.glow {
		objs = append(objs, r)
	}
	objs = append(objs, k.objects...)
	if len(k.lines) == 0 {
		objs = append(objs, k.message)
	}
	return objs
}

func (r *karaokeRenderer) Destroy() { r.k.halt() }
