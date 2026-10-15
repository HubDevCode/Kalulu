package gui

import (
	"image/color"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

const prefDynamicColors = "appearance.dynamic_colors"

// role picks a color of the palette for a surface.
type role func(Palette) color.Color

var (
	roleBG       role = func(p Palette) color.Color { return p.BG }
	roleSurface  role = func(p Palette) color.Color { return p.Surface }
	roleSurface2 role = func(p Palette) color.Color { return p.Surface2 }
	roleBorder   role = func(p Palette) color.Color { return p.Border }
	roleCard     role = func(p Palette) color.Color { return mix(p.Surface, p.Surface2, 0.5) }
	roleOverlay  role = func(p Palette) color.Color { return p.Surface2 }
	roleAccent   role = func(p Palette) color.Color { return p.Accent }
	// roleArtHolder fills a cover while it loads, or when there is none.
	roleArtHolder role = func(p Palette) color.Color { return mix(p.Surface2, p.Accent, 0.22) }
)

// themed keeps the surfaces that follow the palette.
var themed struct {
	sync.Mutex
	rects     []themedRect
	gradients []themedGradient
	current   Palette
	live      int // surfaces alive at the last prune
}

// The surfaces are held weakly: cards of a search or of a Home reload are
// thrown away all the time, and a strong list would keep every one alive
// and recolor it forever.
type themedRect struct {
	r    weak.Pointer[canvas.Rectangle]
	role role
}

type themedGradient struct {
	g          weak.Pointer[canvas.LinearGradient]
	start, end role
}

// tRect returns a rectangle filled by role, recolored with the palette.
func tRect(r role) *canvas.Rectangle {
	themed.Lock()
	defer themed.Unlock()
	rect := canvas.NewRectangle(r(themed.current))
	themed.rects = append(themed.rects, themedRect{weak.Make(rect), r})
	pruneThemed()
	return rect
}

// pruneThemed drops the collected surfaces once the list doubles. It runs
// with themed locked.
func pruneThemed() {
	n := len(themed.rects) + len(themed.gradients)
	if n < 64 || n < 2*themed.live {
		return
	}
	rects := themed.rects[:0]
	for _, t := range themed.rects {
		if t.r.Value() != nil {
			rects = append(rects, t)
		}
	}
	clear(themed.rects[len(rects):])
	themed.rects = rects
	grads := themed.gradients[:0]
	for _, t := range themed.gradients {
		if t.g.Value() != nil {
			grads = append(grads, t)
		}
	}
	clear(themed.gradients[len(grads):])
	themed.gradients = grads
	themed.live = len(rects) + len(grads)
}

// tPanel is panel with a themed fill.
func tPanel(r role, radius float32, content fyne.CanvasObject) *fyne.Container {
	bg := tRect(r)
	bg.CornerRadius = radius
	return container.NewStack(bg, content)
}

// tGradient is a vertical gradient between two roles.
func tGradient(start, end role) *canvas.LinearGradient {
	themed.Lock()
	defer themed.Unlock()
	g := canvas.NewLinearGradient(start(themed.current), end(themed.current), 0)
	themed.gradients = append(themed.gradients, themedGradient{weak.Make(g), start, end})
	pruneThemed()
	return g
}

// resetThemed forgets the surfaces of a previous build of the window.
func resetThemed(p Palette) {
	themed.Lock()
	defer themed.Unlock()
	themed.rects = nil
	themed.gradients = nil
	themed.live = 0
	themed.current = p
}

// dynAccent is the accent of the cover, read by the theme for the primary
// color, so accent widgets follow the cover without a theme change. The
// icons on it keep the color the base theme chose, so the accent is kept on
// the same side of the luminance.
var dynAccent atomic.Pointer[color.NRGBA]

func setDynAccent(base, p Palette) {
	if p.Accent == base.Accent {
		dynAccent.Store(nil)
		return
	}
	acc := p.Accent
	baseLight := luminance(base.Accent) > 0.6
	for i := 0; i < 12 && (luminance(acc) > 0.6) != baseLight; i++ {
		if baseLight {
			acc = mix(acc, color.NRGBA{255, 255, 255, 255}, 0.15)
		} else {
			acc = mix(acc, color.NRGBA{0, 0, 0, 255}, 0.15)
		}
	}
	dynAccent.Store(&acc)
}

// recolor applies p to every themed surface. It runs on the UI thread.
func recolor(p Palette) {
	themed.Lock()
	themed.current = p
	rects := append([]themedRect(nil), themed.rects...)
	grads := append([]themedGradient(nil), themed.gradients...)
	themed.Unlock()
	for _, t := range rects {
		r := t.r.Value()
		if r == nil {
			continue
		}
		if c := t.role(p); r.FillColor != c {
			r.FillColor = c
			r.Refresh()
		}
	}
	for _, t := range grads {
		g := t.g.Value()
		if g == nil {
			continue
		}
		g.StartColor = t.start(p)
		g.EndColor = t.end(p)
		g.Refresh()
	}
}

// relLum is the WCAG relative luminance.
func relLum(c color.NRGBA) float64 {
	ch := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// contrast is the WCAG contrast ratio of two colors.
func contrast(a, b color.NRGBA) float64 {
	la, lb := relLum(a), relLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// readable moves the lightness of c away from bg until the contrast
// reaches want.
func readable(c, bg color.NRGBA, want float64) color.NRGBA {
	h, s, l := toHSL(c)
	dark := relLum(bg) < 0.4
	for i := 0; i < 30 && contrast(c, bg) < want; i++ {
		if dark {
			l = math.Min(0.95, l+0.03)
		} else {
			l = math.Max(0.05, l-0.03)
		}
		c = hsl(h, s, l)
	}
	return c
}

// paletteFromCover tints the base palette with the hue of the cover: the
// surfaces take a dark (or light) shade of it and the accents its vivid
// tones, always with readable contrast.
func paletteFromCover(base Palette, c color.NRGBA) Palette {
	h, s, _ := toHSL(c)
	if s < 0.08 {
		// A grey cover has no hue to follow: keep the base accents.
		s = 0
	}
	p := base
	tint := math.Min(0.35, s*0.6)
	if base.Light {
		p.BG = hsl(h, tint, 0.965)
		p.Surface = hsl(h, tint, 0.99)
		p.Surface2 = hsl(h, tint, 0.92)
		p.Border = hsl(h, tint, 0.86)
	} else {
		p.BG = hsl(h, tint, 0.065)
		p.Surface = hsl(h, tint, 0.095)
		p.Surface2 = hsl(h, tint, 0.14)
		p.Border = hsl(h, tint, 0.19)
	}
	if s > 0 {
		p.Accent = readable(hsl(h, math.Max(0.6, s), 0.62), p.BG, 4.5)
		p.Accent2 = readable(hsl(math.Mod(h+0.1, 1), math.Max(0.5, s*0.9), 0.58), p.BG, 3)
	}
	p.Accent = readable(p.Accent, p.BG, 4.6)
	p.FGDim = readable(mix(p.FG, p.BG, 0.42), p.BG, 4.6)
	p.Name = base.Name
	return p
}

func lerpPalette(a, b Palette, t float64) Palette {
	m := func(x, y color.NRGBA) color.NRGBA { return mix(x, y, t) }
	out := b
	out.BG, out.Surface, out.Surface2, out.Border = m(a.BG, b.BG), m(a.Surface, b.Surface), m(a.Surface2, b.Surface2), m(a.Border, b.Border)
	out.FG, out.FGDim = m(a.FG, b.FG), m(a.FGDim, b.FGDim)
	out.Accent, out.Accent2 = m(a.Accent, b.Accent), m(a.Accent2, b.Accent2)
	out.Warn, out.Error = m(a.Warn, b.Warn), m(a.Error, b.Error)
	return out
}

// basePalette is the palette the user picked, before the cover tints it.
func (a *App) basePalette() Palette {
	return PaletteByName(a.prefs.StringWithFallback(prefPalette, paletteDark))
}

// followCover moves the window colors toward the cover, or back to the
// base palette without one, in a short fade.
func (a *App) followCover() {
	target := a.basePalette()
	if c, ok := a.coverColor(); ok && a.prefs.BoolWithFallback(prefDynamicColors, true) {
		target = paletteFromCover(target, c)
	}
	a.animatePalette(target)
}

// currentPalette is the palette the themed surfaces show now.
func currentPalette() Palette {
	themed.Lock()
	defer themed.Unlock()
	return themed.current
}

// animatePalette fades the themed surfaces to target. Only the backgrounds,
// panels and gradients change: the widget theme stays, because a Fyne
// theme change refreshes every object of the window and took seconds on
// each track change.
func (a *App) animatePalette(target Palette) {
	a.paletteSeq++
	seq := a.paletteSeq
	from := currentPalette()
	if from.BG == target.BG && from.Accent == target.Accent && from.Surface == target.Surface {
		return
	}
	steps := 10
	if a.reduceMotion() || !a.winVisible.Load() {
		steps = 1
	}
	go func() {
		for i := 1; i <= steps; i++ {
			if steps > 1 {
				time.Sleep(40 * time.Millisecond)
			}
			t := float64(i) / float64(steps)
			runOnUI(func() {
				if seq != a.paletteSeq {
					return
				}
				p := lerpPalette(from, target, smoothstep(t))
				setDynAccent(a.theme.p, p)
				recolor(p)
				if t == 1 {
					a.refreshAccents()
				}
			})
		}
	}()
}

func smoothstep(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// refreshAccents redraws the few always-visible accent widgets once the
// colors settle; the others pick the accent up on their next refresh.
func (a *App) refreshAccents() {
	if b := a.playerBar; b != nil {
		b.play.Refresh()
		b.seek.Refresh()
		b.volume.Refresh()
		b.shuffle.Refresh()
		b.repeat.Refresh()
	}
	for _, n := range a.nav {
		n.refreshStyle()
	}
}
