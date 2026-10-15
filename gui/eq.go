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
	"github.com/bjarneo/cliamp/ui/model"
)

var eqFrequencies = []string{"70", "180", "320", "600", "1k", "3k", "6k", "12k", "14k", "16k"}

// eqPanel is the 10-band equalizer and the audio controls.
type eqPanel struct {
	app     *App
	root    fyne.CanvasObject
	preset  *widget.Select
	sliders []*widget.Slider
	values  []*widget.Label
	dragged time.Time
	speed   *widget.Slider
	speedL  *widget.Label
	speedAt time.Time
	mono    *widget.Check
	device  *widget.Select
	curve   *canvas.Raster
	bands   []float64
}

func newEQPanel(a *App) *eqPanel {
	e := &eqPanel{app: a, bands: make([]float64, 10)}
	names := append(model.EQPresetNames(), "Custom")
	e.preset = widget.NewSelect(names, func(name string) {
		if name == "" || name == a.state.EQPreset {
			return
		}
		a.do("eq", P{"name": name}, nil)
	})

	grid := container.NewGridWithColumns(10)
	for i := range 10 {
		i := i
		s := widget.NewSlider(-12, 12)
		s.Orientation = widget.Vertical
		s.Step = 0.5
		v := captionLabel("0")
		v.Alignment = fyne.TextAlignCenter
		s.OnChanged = func(val float64) {
			e.dragged = time.Now()
			v.SetText(fmt.Sprintf("%+.1f", val))
			e.bands[i] = val
			e.curve.Refresh()
			a.do("eq", P{"band": i, "value": val}, nil)
		}
		f := captionLabel(eqFrequencies[i])
		f.Alignment = fyne.TextAlignCenter
		e.sliders = append(e.sliders, s)
		e.values = append(e.values, v)
		grid.Add(container.NewBorder(v, f, nil, nil, fixed(0, 240, s)))
	}
	e.curve = canvas.NewRaster(e.drawCurve)
	reset := widget.NewButtonWithIcon("Flat", theme.ViewRefreshIcon(), func() { a.do("eq", P{"name": "Flat"}, nil) })
	reset.Importance = widget.LowImportance

	eqCard := tPanel(roleSurface, 14, padded(14, 16, 14, 16, container.NewVBox(
		container.NewBorder(nil, nil, boldLabel(T("Equalizer")), container.NewHBox(captionLabel("Preset"), fixed(180, 0, e.preset), reset)),
		fixed(0, 70, e.curve),
		grid,
		captionLabel("dB per band · 70 Hz – 16 kHz · changes are saved to config.toml"),
	)))

	e.speed = widget.NewSlider(0.25, 2)
	e.speed.Step = 0.05
	e.speedL = label("1.00×")
	e.speed.OnChanged = func(v float64) {
		e.speedAt = time.Now()
		e.speedL.SetText(fmt.Sprintf("%.2f×", v))
	}
	e.speed.OnChangeEnded = func(v float64) { a.setSpeed(v) }
	speedReset := iconButton(theme.ViewRefreshIcon(), func() { a.setSpeed(1) })
	e.mono = widget.NewCheck(T("Mono output (downmix both channels)"), func(on bool) {
		if a.state.Mono != nil && *a.state.Mono != on {
			a.toggleMono()
		}
	})
	e.device = widget.NewSelect(nil, func(name string) {
		if name == "" || name == a.state.Device {
			return
		}
		a.do("device", P{"name": name}, func(Result) { a.toast("Output: " + name) })
	})
	e.device.PlaceHolder = T("System default")
	devRefresh := iconButton(theme.ViewRefreshIcon(), e.loadDevices)
	normalize := widget.NewCheck(T("Normalize the volume (same loudness for every track)"), func(on bool) {
		a.prefs.SetBool(prefNormalize, on)
		a.applyPlaybackPrefs()
	})
	normalize.Checked = a.prefs.BoolWithFallback(prefNormalize, false)
	balance := widget.NewSlider(-100, 100)
	balance.Step = 5
	balance.Value = a.prefs.FloatWithFallback(prefBalance, 0) * 100
	balanceL := label(balanceText(balance.Value))
	balance.OnChanged = func(v float64) {
		balanceL.SetText(balanceText(v))
		a.prefs.SetFloat(prefBalance, v/100)
		if a.opts.Mixer != nil {
			a.opts.Mixer.SetBalance(v / 100)
		}
	}
	balanceReset := iconButton(theme.ViewRefreshIcon(), func() { balance.SetValue(0) })

	audioCard := tPanel(roleSurface, 14, padded(14, 16, 14, 16, container.NewVBox(
		boldLabel("Audio"),
		container.NewBorder(nil, nil, fixed(160, 0, dimLabel(T("Speed"))), container.NewHBox(fixed(60, 0, e.speedL), speedReset), e.speed),
		container.NewBorder(nil, nil, fixed(160, 0, dimLabel(T("Output device"))), devRefresh, e.device),
		container.NewBorder(nil, nil, fixed(160, 0, dimLabel(T("Balance"))), container.NewHBox(fixed(60, 0, balanceL), balanceReset), balance),
		normalize,
		e.mono,
	)))

	content := container.NewVBox(eqCard, audioCard)
	e.root = container.NewBorder(pageHeader(heading(T("Equalizer & audio")), dimLabel(T("Shape the sound of every source"))), nil, nil, nil,
		container.NewVScroll(padded(0, 18, 18, 18, content)))
	a.onState(e.update)
	return e
}

func (e *eqPanel) Object() fyne.CanvasObject { return e.root }

func (e *eqPanel) Shown() { e.loadDevices() }

func (e *eqPanel) loadDevices() {
	e.app.do("device", P{"name": "list"}, func(r Result) {
		var names []string
		for _, d := range r.Response.Devices {
			names = append(names, d.Name)
			if d.Active {
				e.device.Selected = d.Name
			}
		}
		e.device.Options = names
		e.device.Refresh()
	})
}

func (e *eqPanel) update(s ipc.RuntimeSnapshot) {
	if e.preset.Selected != s.EQPreset && s.EQPreset != "" {
		e.preset.Selected = s.EQPreset
		e.preset.Refresh()
	}
	if time.Since(e.dragged) > time.Second && len(s.EQBands) == 10 {
		changed := false
		for i, v := range s.EQBands {
			if math.Abs(e.sliders[i].Value-v) > 0.01 {
				e.sliders[i].Value = v
				e.sliders[i].Refresh()
				e.values[i].SetText(fmt.Sprintf("%+.1f", v))
				changed = true
			}
			e.bands[i] = v
		}
		if changed {
			e.curve.Refresh()
		}
	}
	if time.Since(e.speedAt) > time.Second && s.Speed > 0 && math.Abs(e.speed.Value-s.Speed) > 0.001 {
		e.speed.Value = s.Speed
		e.speed.Refresh()
		e.speedL.SetText(fmt.Sprintf("%.2f×", s.Speed))
	}
	if s.Mono != nil && e.mono.Checked != *s.Mono {
		e.mono.Checked = *s.Mono
		e.mono.Refresh()
	}
	if s.Device != "" && e.device.Selected != s.Device && !strings.Contains(s.Device, "\n") {
		e.device.Selected = s.Device
		e.device.Refresh()
	}
}

// drawCurve draws a smooth line through the band gains.
func (e *eqPanel) drawCurve(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	if w < 2 || h < 2 {
		return img
	}
	p := e.app.theme.p
	mid := float64(h) / 2
	for x := 0; x < w; x++ {
		setPx(img, x, int(mid), withAlpha(p.FGDim, 0x50))
	}
	n := len(e.bands)
	at := func(i int) float64 { return e.bands[max(0, min(n-1, i))] }
	for x := 0; x < w; x++ {
		// Catmull-Rom through the band gains.
		f := float64(x) / float64(w-1) * float64(n-1)
		i := int(f)
		t := f - float64(i)
		p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
		v := 0.5 * (2*p1 + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t*t + (-p0+3*p1-3*p2+p3)*t*t*t)
		y := int(mid - v/12*(mid-3))
		lo, hi := min(y, int(mid)), max(y, int(mid))
		for yy := lo; yy <= hi; yy++ {
			setPx(img, x, yy, withAlpha(p.Accent, 0x40))
		}
		c := gradientAt(p, 0.3+math.Abs(v)/12*0.6)
		for t := -1; t <= 1; t++ {
			setPx(img, x, y+t, c)
		}
	}
	return img
}

const (
	prefBalance   = "audio.balance"
	prefNormalize = "audio.normalize"
)

// balanceText shows the balance as "C", "L 30" or "R 30".
func balanceText(v float64) string {
	switch {
	case v < 0:
		return fmt.Sprintf("L %d", int(-v))
	case v > 0:
		return fmt.Sprintf("R %d", int(v))
	}
	return "C"
}
