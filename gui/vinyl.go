package gui

import (
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/draw"
)

// prefCoverStyle picks what the player page shows: the cover ("cover") or
// a record with the cover as its label ("vinyl").
const prefCoverStyle = "player.cover_style"

const (
	coverStyleCover = "cover"
	coverStyleVinyl = "vinyl"
)

func (a *App) coverStyle() string {
	return a.prefs.StringWithFallback(prefCoverStyle, coverStyleCover)
}

// vinylRPM is the turn speed of the record.
const vinylRPM = 33.3

// vinyl is a record that turns while the track plays, the cover on its
// label and a tonearm that lowers on play and lifts on pause.
type vinyl struct {
	widget.BaseWidget
	app *App

	disk    *canvas.Circle
	rim     *canvas.Circle
	grooves []*canvas.Circle
	label   *canvas.Raster
	spindle *canvas.Circle
	arm     *canvas.Line
	weight  *canvas.Line
	head    *canvas.Line
	pivot   *canvas.Circle
	pivotIn *canvas.Circle

	// mu guards the fields the raster generator reads.
	mu    sync.Mutex
	src   *image.NRGBA
	tint  color.NRGBA
	angle float64
	buf   *image.NRGBA

	// UI thread only.
	playing bool
	armT    float64 // 0 lifted, 1 on the record
	running bool
	palette Palette
	size    fyne.Size
}

func newVinyl(a *App) *vinyl {
	v := &vinyl{app: a}
	v.disk = canvas.NewCircle(vinylDisk)
	v.rim = canvas.NewCircle(color.Transparent)
	v.rim.StrokeWidth = 1.5
	for i := 0; i < 5; i++ {
		g := canvas.NewCircle(color.Transparent)
		g.StrokeWidth = 1
		v.grooves = append(v.grooves, g)
	}
	v.label = canvas.NewRaster(v.drawLabel)
	v.spindle = canvas.NewCircle(roleSpindle(Palette{}))
	v.arm = canvas.NewLine(color.White)
	v.arm.StrokeWidth = 5
	v.weight = canvas.NewLine(color.White)
	v.weight.StrokeWidth = 12
	v.head = canvas.NewLine(color.White)
	v.head.StrokeWidth = 9
	v.pivot = canvas.NewCircle(color.White)
	v.pivotIn = canvas.NewCircle(color.White)
	v.applyPalette(currentPalette())
	v.ExtendBaseWidget(v)
	return v
}

func (v *vinyl) CreateRenderer() fyne.WidgetRenderer {
	objects := []fyne.CanvasObject{v.disk, v.rim}
	for _, g := range v.grooves {
		objects = append(objects, g)
	}
	objects = append(objects, v.label, v.spindle, v.weight, v.arm, v.head, v.pivot, v.pivotIn)
	return &vinylRenderer{v: v, objects: objects}
}

// SetImage puts img on the label; nil shows a plain label of the accent.
func (v *vinyl) SetImage(img image.Image) {
	var src *image.NRGBA
	if img != nil {
		// Crop the middle square and keep a fixed size, so each frame
		// samples the same small picture.
		b := img.Bounds()
		side := min(b.Dx(), b.Dy())
		crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
		crop.Max = crop.Min.Add(image.Pt(side, side))
		src = image.NewNRGBA(image.Rect(0, 0, 384, 384))
		draw.CatmullRom.Scale(src, src.Bounds(), img, crop, draw.Src, nil)
	}
	v.mu.Lock()
	v.src = src
	v.mu.Unlock()
	v.label.Refresh()
}

// applyPalette colors the grooves and the arm; the disk stays a dark
// glass over the colors of the page.
func (v *vinyl) applyPalette(p Palette) {
	if v.palette.Accent == p.Accent && v.palette.FG == p.FG && v.palette.BG == p.BG {
		return
	}
	v.palette = p
	groove := roleGroove(p)
	for _, g := range v.grooves {
		g.StrokeColor = groove
		g.Refresh()
	}
	v.rim.StrokeColor = roleRim(p)
	v.rim.Refresh()
	silver := mix(p.FG, color.NRGBA{0x9a, 0x9a, 0xa4, 0xff}, 0.45)
	v.arm.StrokeColor = silver
	v.weight.StrokeColor = mix(silver, color.NRGBA{0x30, 0x30, 0x38, 0xff}, 0.45)
	v.head.StrokeColor = p.Accent
	v.pivot.FillColor = silver
	v.pivotIn.FillColor = mix(silver, color.NRGBA{0x20, 0x20, 0x28, 0xff}, 0.5)
	for _, o := range []fyne.CanvasObject{v.arm, v.weight, v.head, v.pivot, v.pivotIn} {
		o.Refresh()
	}
	v.mu.Lock()
	v.tint = roleDiscLabel(p).(color.NRGBA)
	v.mu.Unlock()
	v.label.Refresh()
}

// SetPlaying turns the record and lowers the arm, or stops and lifts it.
func (v *vinyl) SetPlaying(on bool) {
	if v.playing == on && (v.running || v.armT == boolF(on)) {
		return
	}
	v.playing = on
	if v.app.reduceMotion() {
		v.armT = boolF(on)
		v.layoutArm()
		return
	}
	if !v.running {
		v.running = true
		go v.animate()
	}
}

// Stop halts the animation at once, for a hidden page.
func (v *vinyl) Stop() {
	v.playing = false
	v.armT = 0
	v.layoutArm()
}

func boolF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// animate runs the frames until the record stops and the arm rests.
func (v *vinyl) animate() {
	const frame = 33 * time.Millisecond
	ticker := time.NewTicker(frame)
	defer ticker.Stop()
	last := time.Now()
	for range ticker.C {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		last = now
		done := make(chan bool, 1)
		runOnUI(func() {
			target := boolF(v.playing)
			step := dt / 0.6 // the arm swings in 0.6 s
			switch {
			case v.armT < target:
				v.armT = math.Min(target, v.armT+step)
			case v.armT > target:
				v.armT = math.Max(target, v.armT-step)
			}
			v.layoutArm()
			// The record turns once the needle is down.
			spinning := v.playing && v.armT >= 1 && v.app.winVisible.Load()
			if spinning {
				v.mu.Lock()
				v.angle = math.Mod(v.angle+dt*vinylRPM/60*2*math.Pi, 2*math.Pi)
				v.mu.Unlock()
				v.label.Refresh()
			}
			stop := !v.playing && v.armT == 0
			if stop {
				v.running = false
			}
			done <- stop
		})
		if <-done {
			return
		}
	}
}

// geometry places the record and the arm in a square of side s.
func (v *vinyl) geometry(size fyne.Size) (cx, cy, d float32) {
	s := min(size.Width, size.Height)
	ox, oy := (size.Width-s)/2, (size.Height-s)/2
	d = s * 0.84
	return ox + s*0.45, oy + s*0.5, d
}

// layoutArm moves the tonearm for armT: lifted beside the record or with
// the needle on the outer grooves.
func (v *vinyl) layoutArm() {
	size := v.size
	if size.Width == 0 {
		return
	}
	s := min(size.Width, size.Height)
	ox, oy := (size.Width-s)/2, (size.Height-s)/2
	cx, cy, d := v.geometry(size)
	px, py := ox+s*0.93, oy+s*0.6
	// Needle on the record, and the rest angle swung out to the right.
	nx, ny := cx+d*0.1, cy+d*0.4
	ax, ay := float64(nx-px), float64(ny-py)
	length := math.Hypot(ax, ay)
	play := math.Atan2(ay, ax)
	rest := play - 0.42
	ang := rest + (play-rest)*smoothstep(v.armT)
	dx, dy := float32(math.Cos(ang)), float32(math.Sin(ang))
	hx, hy := px+dx*float32(length), py+dy*float32(length)
	v.arm.Position1 = fyne.NewPos(px, py)
	v.arm.Position2 = fyne.NewPos(hx, hy)
	// The headshell bends a little from the arm.
	hb := ang + 0.5
	hl := s * 0.08
	v.head.Position1 = fyne.NewPos(hx, hy)
	v.head.Position2 = fyne.NewPos(hx+float32(math.Cos(hb))*hl, hy+float32(math.Sin(hb))*hl)
	wl := s * 0.07
	v.weight.Position1 = fyne.NewPos(px, py)
	v.weight.Position2 = fyne.NewPos(px-dx*wl, py-dy*wl)
	for _, o := range []fyne.CanvasObject{v.arm, v.head, v.weight} {
		o.Refresh()
	}
}

// drawLabel renders the turned cover as a round label of w x h pixels.
func (v *vinyl) drawLabel(w, h int) image.Image {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.buf == nil || v.buf.Rect.Dx() != w || v.buf.Rect.Dy() != h {
		v.buf = image.NewNRGBA(image.Rect(0, 0, w, h))
	}
	dst := v.buf
	r := float64(min(w, h)) / 2
	cx, cy := float64(w)/2, float64(h)/2
	cos, sin := math.Cos(-v.angle), math.Sin(-v.angle)
	src := v.src
	var scale, half float64
	if src != nil {
		half = float64(src.Rect.Dx()) / 2
		scale = half / r
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := dst.PixOffset(x, y)
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			dist := math.Hypot(dx, dy)
			cov := math.Max(0, math.Min(1, r-dist))
			if cov == 0 {
				dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = 0, 0, 0, 0
				continue
			}
			c := v.tint
			if src != nil {
				sx := (dx*cos-dy*sin)*scale + half
				sy := (dx*sin+dy*cos)*scale + half
				c = sampleBilinear(src, sx, sy)
			}
			// A soft shade at the edge of the label.
			if edge := dist / r; edge > 0.9 {
				k := 1 - (edge-0.9)*2.2
				c.R, c.G, c.B = uint8(float64(c.R)*k), uint8(float64(c.G)*k), uint8(float64(c.B)*k)
			}
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2] = c.R, c.G, c.B
			dst.Pix[i+3] = uint8(255 * cov)
		}
	}
	return dst
}

func sampleBilinear(img *image.NRGBA, x, y float64) color.NRGBA {
	max := float64(img.Rect.Dx() - 1)
	x = math.Max(0, math.Min(max, x-0.5))
	y = math.Max(0, math.Min(max, y-0.5))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, int(max)), min(y0+1, int(max))
	fx, fy := x-float64(x0), y-float64(y0)
	p := func(px, py int) []uint8 { i := img.PixOffset(px, py); return img.Pix[i : i+4] }
	a, b, c, d := p(x0, y0), p(x1, y0), p(x0, y1), p(x1, y1)
	var out [4]uint8
	for k := 0; k < 4; k++ {
		top := float64(a[k])*(1-fx) + float64(b[k])*fx
		bot := float64(c[k])*(1-fx) + float64(d[k])*fx
		out[k] = uint8(top*(1-fy) + bot*fy + 0.5)
	}
	return color.NRGBA{out[0], out[1], out[2], 0xff}
}

type vinylRenderer struct {
	v       *vinyl
	objects []fyne.CanvasObject
}

func (r *vinylRenderer) Layout(size fyne.Size) {
	v := r.v
	v.size = size
	cx, cy, d := v.geometry(size)
	circle := func(o fyne.CanvasObject, diam float32) {
		o.Move(fyne.NewPos(cx-diam/2, cy-diam/2))
		o.Resize(fyne.NewSize(diam, diam))
	}
	circle(v.disk, d)
	circle(v.rim, d)
	for i, g := range v.grooves {
		circle(g, d*(0.56+0.08*float32(i)))
	}
	circle(v.label, d*0.44)
	circle(v.spindle, max(6, d*0.025))
	s := min(size.Width, size.Height)
	ox, oy := (size.Width-s)/2, (size.Height-s)/2
	px, py := ox+s*0.93, oy+s*0.6
	pr := s * 0.05
	v.pivot.Move(fyne.NewPos(px-pr, py-pr))
	v.pivot.Resize(fyne.NewSize(2*pr, 2*pr))
	v.pivotIn.Move(fyne.NewPos(px-pr/2, py-pr/2))
	v.pivotIn.Resize(fyne.NewSize(pr, pr))
	v.layoutArm()
}

func (r *vinylRenderer) MinSize() fyne.Size           { return fyne.NewSize(120, 120) }
func (r *vinylRenderer) Refresh()                     { r.Layout(r.v.Size()) }
func (r *vinylRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *vinylRenderer) Destroy()                     {}
