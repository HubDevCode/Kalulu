package gui

import (
	"image"
	"image/color"
	"math"
	"math/cmplx"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// visStyles are the visualizer modes of the window.
var visStyles = []string{"NCS", "Bars", "Mirror", "Peaks", "Spectrum", "Wave", "Radial", "Dots", "Off"}

const (
	fftSize  = 2048
	visFPS   = 40
	minFreq  = 35.0
	maxFreq  = 16000.0
	floorDB  = -72.0
	rangeDB  = 62.0
	maxBands = 96
)

// visualizer draws the spectrum of the playing audio. It reads the samples
// of the player and runs its own FFT, so its look does not depend on the
// terminal visualizers of the engine.
type visualizer struct {
	widget.BaseWidget
	app    *App
	raster *canvas.Raster
	big    bool

	mu      sync.Mutex
	style   string
	bands   []float64 // smoothed 0..1 per band
	peaks   []float64
	peakVel []float64
	wave    []float64
	energy  float64

	samples []float64
	buf     []complex128
	window  []float64
	tw      []complex128
	img     *image.RGBA
	stop    chan struct{}
	onTap   func()
	// active reports whether the visualizer is on screen; nil means always.
	active func() bool
	beat   beatTracker
	angle  float64
	sparks []particle
}

func newVisualizer(a *App, big bool) *visualizer {
	v := &visualizer{
		app:     a,
		big:     big,
		style:   a.prefs.StringWithFallback(prefVisStyle, "NCS"),
		bands:   make([]float64, maxBands),
		peaks:   make([]float64, maxBands),
		peakVel: make([]float64, maxBands),
		samples: make([]float64, fftSize),
		buf:     make([]complex128, fftSize),
		window:  make([]float64, fftSize),
		tw:      twiddles(fftSize),
		stop:    make(chan struct{}),
	}
	for i := range v.window {
		v.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1))
	}
	v.raster = canvas.NewRaster(v.draw)
	v.raster.ScaleMode = canvas.ImageScaleFastest
	v.ExtendBaseWidget(v)
	go v.loop()
	return v
}

func (v *visualizer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(v.raster)
}

func (v *visualizer) MinSize() fyne.Size {
	if v.big {
		return fyne.NewSize(200, 200)
	}
	return fyne.NewSize(120, 110)
}

func (v *visualizer) Tapped(*fyne.PointEvent) {
	if v.onTap != nil {
		v.onTap()
	}
}

// DoubleTapped cycles the style.
func (v *visualizer) DoubleTapped(*fyne.PointEvent) {
	v.Cycle()
}

func (v *visualizer) Style() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.style
}

func (v *visualizer) SetStyle(style string) {
	v.mu.Lock()
	v.style = style
	v.mu.Unlock()
	v.app.prefs.SetString(prefVisStyle, style)
	v.raster.Refresh()
}

// Cycle moves to the next style.
func (v *visualizer) Cycle() {
	cur := v.Style()
	next := visStyles[0]
	for i, s := range visStyles {
		if s == cur {
			next = visStyles[(i+1)%len(visStyles)]
		}
	}
	v.SetStyle(next)
	v.app.toast("Visualizer: " + next)
}

// Close stops the frame loop.
func (v *visualizer) Close() {
	select {
	case <-v.stop:
	default:
		close(v.stop)
	}
}

func (v *visualizer) loop() {
	ticker := time.NewTicker(time.Second / visFPS)
	defer ticker.Stop()
	idle := 0
	for {
		select {
		case <-v.stop:
			return
		case <-v.app.engine.Done():
			return
		case <-ticker.C:
		}
		// The global style is shared, so another view can change it.
		if s := v.app.prefs.StringWithFallback(prefVisStyle, "NCS"); s != v.Style() {
			v.mu.Lock()
			v.style = s
			v.mu.Unlock()
		}
		if v.Style() == "Off" || !v.app.winVisible.Load() || (v.active != nil && !v.active()) {
			continue
		}
		active := v.analyze()
		if !active {
			idle++
			if idle > visFPS*2 {
				continue // settled; no redraw while silent
			}
		} else {
			idle = 0
		}
		runOnUI(v.raster.Refresh)
	}
}

// analyze updates the bands. It reports false when everything is at rest.
func (v *visualizer) analyze() bool {
	n := v.app.engine.SamplesInto(v.samples)
	sr := float64(v.app.engine.SampleRate())
	if sr <= 0 {
		sr = 44100
	}
	playing := v.app.playing.Load()
	mags := make([]float64, maxBands)
	// Sensitivity shifts the level scale: 2× reads 6 dB hotter.
	sensDB := 20 * math.Log10(math.Max(0.25, v.app.prefs.FloatWithFallback(prefVisSensitivity, 1)))
	if n > 0 && playing {
		for i := range v.buf {
			s := 0.0
			if i < n {
				s = v.samples[i]
			}
			v.buf[i] = complex(s*v.window[i], 0)
		}
		fft(v.buf, v.tw)
		half := fftSize / 2
		for b := 0; b < maxBands; b++ {
			lo := minFreq * math.Pow(maxFreq/minFreq, float64(b)/maxBands)
			hi := minFreq * math.Pow(maxFreq/minFreq, float64(b+1)/maxBands)
			i0 := int(lo / sr * fftSize)
			i1 := int(hi / sr * fftSize)
			i0 = max(1, min(i0, half-1))
			i1 = max(i0+1, min(i1, half))
			peak := 0.0
			for i := i0; i < i1; i++ {
				peak = math.Max(peak, cmplx.Abs(v.buf[i]))
			}
			db := 20 * math.Log10(peak/float64(fftSize)*4+1e-9)
			// Tilt up the highs, which carry less energy.
			db += 4.5 * math.Log2(1+float64(b)/maxBands*8)
			mags[b] = math.Max(0, math.Min(1, (db-floorDB+sensDB)/rangeDB))
		}
	}

	wave := make([]float64, 0)
	if v.Style() == "Wave" && playing {
		buf := make([]float64, 1024)
		m := v.app.engine.WaveformSamplesInto(buf)
		wave = buf[:m]
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	active := false
	energy := 0.0
	for b := range v.bands {
		target := mags[b]
		if target > v.bands[b] {
			v.bands[b] += (target - v.bands[b]) * 0.65
		} else {
			v.bands[b] -= math.Min(v.bands[b]-target, 0.035+v.bands[b]*0.06)
		}
		if v.bands[b] > v.peaks[b] {
			v.peaks[b] = v.bands[b]
			v.peakVel[b] = 0
		} else {
			v.peakVel[b] += 0.0018
			v.peaks[b] = math.Max(v.bands[b], v.peaks[b]-v.peakVel[b])
		}
		if v.bands[b] > 0.002 || v.peaks[b] > 0.002 {
			active = true
		}
		energy += v.bands[b]
	}
	v.energy = v.energy*0.8 + energy/float64(len(v.bands))*0.2
	var bass float64
	for _, b := range v.bands[:10] {
		bass += b
	}
	v.beat.update(bass / 10)
	v.wave = wave
	return active || len(wave) > 0
}

// draw renders the current frame into an RGBA image.
func (v *visualizer) draw(w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if v.img == nil || v.img.Bounds().Dx() != w || v.img.Bounds().Dy() != h {
		v.img = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		clear(v.img.Pix)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.visPalette()
	scale := float64(h) / 140
	if scale < 1 {
		scale = 1
	}
	switch v.style {
	case "Off":
	case "Mirror":
		v.drawBars(w, h, p, true, false, scale)
	case "Peaks":
		v.drawBars(w, h, p, false, true, scale)
	case "Spectrum":
		v.drawSpectrum(w, h, p)
	case "Wave":
		v.drawWave(w, h, p, scale)
	case "Radial":
		v.drawRadial(w, h, p, scale)
	case "NCS":
		v.drawNCS(w, h, p, scale)
	case "Dots":
		v.drawDots(w, h, p, scale)
	default:
		v.drawBars(w, h, p, false, false, scale)
	}
	return v.img
}

// resample maps the analysis bands onto n columns.
func (v *visualizer) resample(n int, src []float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		f := float64(i) / float64(n) * float64(len(src))
		j := int(f)
		t := f - float64(j)
		a := src[min(j, len(src)-1)]
		b := src[min(j+1, len(src)-1)]
		out[i] = a + (b-a)*t
	}
	return out
}

func gradientAt(p Palette, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	if t < 0.7 {
		return mix(p.Accent2, p.Accent, t/0.7)
	}
	return mix(p.Accent, p.Error, (t-0.7)/0.3*0.6)
}

func (v *visualizer) drawBars(w, h int, p Palette, mirror, peaks bool, scale float64) {
	barW := int(7 * scale)
	gap := max(2, int(2*scale))
	n := max(4, (w+gap)/(barW+gap))
	if n > maxBands {
		n = maxBands
		barW = (w+gap)/n - gap
	}
	vals := v.resample(n, v.bands)
	pk := v.resample(n, v.peaks)
	total := n*(barW+gap) - gap
	x0 := (w - total) / 2
	for i, val := range vals {
		x := x0 + i*(barW+gap)
		if mirror {
			half := float64(h) / 2
			bh := int(val * half)
			for y := 0; y < bh; y++ {
				c := gradientAt(p, float64(y)/half)
				alpha := uint8(255 - y*120/max(1, int(half)))
				fillRow(v.img, x, x+barW, int(half)-y-1, withAlpha(c, 255))
				fillRow(v.img, x, x+barW, int(half)+y, withAlpha(c, alpha))
			}
			continue
		}
		bh := int(val * float64(h-int(4*scale)))
		for y := 0; y < bh; y++ {
			c := gradientAt(p, float64(y)/float64(h))
			fillRow(v.img, x, x+barW, h-1-y, c)
		}
		if peaks {
			py := h - 1 - int(pk[i]*float64(h-int(4*scale))) - int(3*scale)
			for t := 0; t < max(2, int(2*scale)); t++ {
				fillRow(v.img, x, x+barW, py+t, p.FG)
			}
		}
	}
}

func (v *visualizer) drawSpectrum(w, h int, p Palette) {
	vals := v.resample(w, v.bands)
	for x, val := range vals {
		top := int(float64(h-2) * (1 - val))
		for y := top; y < h; y++ {
			t := float64(h-y) / float64(h)
			c := gradientAt(p, t)
			a := uint8(70 + 150*float64(h-y)/float64(max(1, h-top))*0.4)
			setPx(v.img, x, y, withAlpha(c, a))
		}
		for t := 0; t < 2; t++ {
			setPx(v.img, x, top+t, gradientAt(p, val))
		}
	}
}

func (v *visualizer) drawWave(w, h int, p Palette, scale float64) {
	mid := float64(h) / 2
	thick := max(1, int(1.5*scale))
	if len(v.wave) < 2 {
		for x := 0; x < w; x++ {
			for t := 0; t < thick; t++ {
				setPx(v.img, x, int(mid)+t, withAlpha(p.Accent, 0x90))
			}
		}
		return
	}
	prevY := -1
	for x := 0; x < w; x++ {
		s := v.wave[x*len(v.wave)/w]
		y := int(mid - s*mid*0.9)
		c := gradientAt(p, math.Abs(s)*1.4)
		if prevY < 0 {
			prevY = y
		}
		lo, hi := min(prevY, y), max(prevY, y)
		for yy := lo; yy <= hi; yy++ {
			for t := 0; t < thick; t++ {
				setPx(v.img, x, yy+t, c)
			}
		}
		prevY = y
	}
}

func (v *visualizer) drawRadial(w, h int, p Palette, scale float64) {
	cx, cy := float64(w)/2, float64(h)/2
	radius := math.Min(cx, cy) * (0.36 + v.energy*0.12)
	maxLen := math.Min(cx, cy) - radius - 2
	n := 72
	vals := v.resample(n, v.bands[:maxBands*3/4])
	for i := 0; i < n*2; i++ {
		val := vals[i%n]
		if i >= n {
			val = vals[n-1-(i-n)]
		}
		angle := float64(i)/float64(n*2)*2*math.Pi - math.Pi/2
		length := 2 + val*maxLen
		c := gradientAt(p, val)
		dx, dy := math.Cos(angle), math.Sin(angle)
		for r := 0.0; r < length; r += 0.5 {
			for t := -scale; t <= scale; t += 0.5 {
				x := cx + dx*(radius+r) - dy*t
				y := cy + dy*(radius+r) + dx*t
				setPx(v.img, int(x), int(y), c)
			}
		}
	}
	// Ring.
	for a := 0.0; a < 2*math.Pi; a += 1 / (radius * 2) {
		setPx(v.img, int(cx+math.Cos(a)*radius), int(cy+math.Sin(a)*radius), withAlpha(p.FG, 0x60))
	}
}

func (v *visualizer) drawDots(w, h int, p Palette, scale float64) {
	cell := int(8 * scale)
	cols := max(4, w/cell)
	rows := max(4, h/cell)
	vals := v.resample(cols, v.bands)
	r := float64(cell) * 0.32
	for cx := 0; cx < cols; cx++ {
		lit := int(vals[cx] * float64(rows))
		for cy := 0; cy < rows; cy++ {
			x := float64(cx*cell + cell/2 + (w-cols*cell)/2)
			y := float64(h - cy*cell - cell/2)
			c := withAlpha(p.FGDim, 0x28)
			if cy < lit {
				c = gradientAt(p, float64(cy)/float64(rows))
			}
			fillCircle(v.img, x, y, r, c)
		}
	}
}

func setPx(img *image.RGBA, x, y int, c color.NRGBA) {
	if !(image.Point{x, y}.In(img.Rect)) {
		return
	}
	i := img.PixOffset(x, y)
	a := uint32(c.A)
	if a == 255 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
		return
	}
	// Premultiplied "over" blend.
	inv := 255 - a
	img.Pix[i] = uint8((uint32(c.R)*a + uint32(img.Pix[i])*inv) / 255)
	img.Pix[i+1] = uint8((uint32(c.G)*a + uint32(img.Pix[i+1])*inv) / 255)
	img.Pix[i+2] = uint8((uint32(c.B)*a + uint32(img.Pix[i+2])*inv) / 255)
	img.Pix[i+3] = uint8(a + uint32(img.Pix[i+3])*inv/255)
}

func fillRow(img *image.RGBA, x0, x1, y int, c color.NRGBA) {
	for x := x0; x < x1; x++ {
		setPx(img, x, y, c)
	}
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.NRGBA) {
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				setPx(img, x, y, c)
			}
		}
	}
}

func twiddles(n int) []complex128 {
	tw := make([]complex128, n/2)
	for i := range tw {
		tw[i] = cmplx.Exp(complex(0, -2*math.Pi*float64(i)/float64(n)))
	}
	return tw
}

// fft is an in-place iterative radix-2 FFT. len(buf) must be a power of two.
func fft(buf []complex128, tw []complex128) {
	n := len(buf)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			buf[i], buf[j] = buf[j], buf[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := n / size
		for start := 0; start < n; start += size {
			for k := 0; k < size/2; k++ {
				t := tw[k*step] * buf[start+k+size/2]
				buf[start+k+size/2] = buf[start+k] - t
				buf[start+k] += t
			}
		}
	}
}
