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
	start float64
	words []*kWord
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
}

func newKaraoke(a *App) *karaoke {
	k := &karaoke{app: a, current: -1}
	k.message = canvas.NewText("", a.theme.p.FGDim)
	k.message.Alignment = fyne.TextAlignCenter
	k.message.TextSize = 18
	k.ExtendBaseWidget(k)
	return k
}

func (k *karaoke) textSize() float32 {
	return float32(k.app.prefs.FloatWithFallback(prefLyricsSize, 26))
}

func (k *karaoke) centered() bool {
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
			if !k.app.winVisible.Load() {
				continue
			}
			runOnUI(k.animate)
		}
	}()
}

// Stop ends the animation.
func (k *karaoke) Stop() {
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
	follow := time.Since(k.manualAt) > 3*time.Second
	if cur >= 0 && follow {
		l := k.layout[cur]
		k.target = l.y + l.h/2 - k.height*0.38
	} else if cur < 0 && follow && k.synced {
		k.target = 0
	}
	smooth := float32(k.app.prefs.FloatWithFallback(prefLyricsSmooth, 0.16))
	if k.app.reduceMotion() {
		smooth = 1
	}
	moved := math.Abs(float64(k.target-k.scroll)) > 0.5
	if moved {
		k.scroll += (k.target - k.scroll) * smooth
	}
	p := k.app.theme.p
	changedLine := cur != k.current
	k.current = cur
	for i, l := range k.layout {
		top := l.y - k.scroll
		visible := top+l.h > -40 && top < k.height+40
		for _, w := range l.words {
			if visible {
				w.text.Move(fyne.NewPos(w.x, w.y-k.scroll))
				w.text.Show()
			} else {
				w.text.Hide()
				continue
			}
			c := k.wordColor(i, cur, w, pos, p)
			if c != w.text.Color {
				w.text.Color = c
				w.text.Refresh()
			} else if moved {
				w.text.Refresh()
			}
		}
	}
	_ = changedLine
}

// wordColor is the color of a word for the playhead: sung words take the
// accent, the word being sung blends into it, lines far from the current
// one fade.
func (k *karaoke) wordColor(line, cur int, w *kWord, pos float64, p Palette) color.Color {
	if !k.synced {
		return p.FG
	}
	if line == cur {
		switch {
		case pos >= w.end:
			return p.Accent
		case pos <= w.start:
			return withAlpha(p.FG, 0xd0)
		default:
			t := (pos - w.start) / math.Max(0.05, w.end-w.start)
			return mix(withAlpha(p.FG, 0xd0), p.Accent, math.Min(1, t*1.6))
		}
	}
	d := line - cur
	if d < 0 {
		d = -d
	}
	alpha := []uint8{0xff, 0x8c, 0x60, 0x48}[min(3, d)]
	if line < cur {
		alpha = alpha * 3 / 4
	}
	return withAlpha(p.FG, alpha)
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

func (r *karaokeRenderer) MinSize() fyne.Size { return fyne.NewSize(200, 160) }

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
	objs := append([]fyne.CanvasObject{}, k.objects...)
	if len(k.lines) == 0 {
		objs = append(objs, k.message)
	}
	return objs
}

func (r *karaokeRenderer) Destroy() { r.k.Stop() }
