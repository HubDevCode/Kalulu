package gui

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
)

const (
	prefVisSensitivity = "visualizer.sensitivity"
	prefVisIntensity   = "visualizer.intensity"
	prefVisColors      = "visualizer.colors"
)

// sphereN is the number of points on the NCS sphere.
const sphereN = 900

// unitSphere holds evenly spread points (a Fibonacci sphere).
var unitSphere = func() [][3]float64 {
	pts := make([][3]float64, sphereN)
	golden := math.Pi * (3 - math.Sqrt(5))
	for i := range pts {
		y := 1 - 2*(float64(i)+0.5)/sphereN
		r := math.Sqrt(1 - y*y)
		th := golden * float64(i)
		pts[i] = [3]float64{math.Cos(th) * r, y, math.Sin(th) * r}
	}
	return pts
}()

// particle is a spark thrown off the sphere on a beat.
type particle struct {
	x, y, vx, vy, life float64
}

// beatTracker finds kicks in the bass energy: a rise well above its
// running average, at most a few times a second.
type beatTracker struct {
	avg      float64
	prev     float64
	cooldown int
	pulse    float64
}

func (b *beatTracker) update(bass float64) bool {
	b.avg = b.avg*0.96 + bass*0.04
	rise := bass - b.prev
	b.prev = bass
	b.pulse *= 0.86
	if b.cooldown > 0 {
		b.cooldown--
		return false
	}
	if bass > 0.18 && bass > b.avg*1.25 && rise > 0.04 {
		b.pulse = 1
		b.cooldown = visFPS / 5
		return true
	}
	return false
}

// visPalette is the palette of the visualizer: the theme, or the colors of
// the current cover.
func (v *visualizer) visPalette() Palette {
	p := v.app.theme.p
	if v.app.prefs.StringWithFallback(prefVisColors, "cover") == "cover" {
		if c, ok := v.app.coverColor(); ok {
			h, s, _ := toHSL(c)
			p.Accent = hsl(h, math.Max(0.55, s), 0.6)
			p.Accent2 = hsl(math.Mod(h+0.12, 1), math.Max(0.5, s), 0.55)
			p.Error = hsl(math.Mod(h+0.95, 1), 0.7, 0.62)
		}
	}
	return p
}

// drawNCS draws the NCS sphere: a rotating point sphere deformed by the
// spectrum, pulsing on the beat, inside a ring of spectrum spikes, with a
// glow and sparks.
func (v *visualizer) drawNCS(w, h int, p Palette, scale float64) {
	cx, cy := float64(w)/2, float64(h)/2
	base := math.Min(cx, cy) * 0.42
	intensity := v.app.prefs.FloatWithFallback(prefVisIntensity, 1)
	pulse := v.beat.pulse * intensity
	radius := base * (1 + 0.10*pulse + 0.06*v.energy*intensity)

	// Glow behind the sphere.
	glowR := radius * (1.9 + 0.3*pulse)
	glow := withAlpha(p.Accent, 0)
	for y := int(cy - glowR); y <= int(cy+glowR); y += 2 {
		for x := int(cx - glowR); x <= int(cx+glowR); x += 2 {
			d := math.Hypot(float64(x)-cx, float64(y)-cy) / glowR
			if d >= 1 {
				continue
			}
			a := (1 - d) * (1 - d) * (0.16 + 0.22*pulse) * 255
			glow.A = uint8(a)
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					setPx(v.img, x+dx, y+dy, glow)
				}
			}
		}
	}

	// Spectrum spikes around the sphere, mirrored left and right.
	n := 90
	vals := v.resample(n, v.bands[:maxBands*3/4])
	ring := radius * 1.32
	for i := 0; i < n*2; i++ {
		val := vals[i%n]
		if i >= n {
			val = vals[n-1-(i-n)]
		}
		angle := float64(i)/float64(n*2)*2*math.Pi - math.Pi/2
		length := 2 + val*base*0.85*intensity
		c := gradientAt(p, val)
		dx, dy := math.Cos(angle), math.Sin(angle)
		for r := 0.0; r < length; r += 0.7 {
			fade := 1 - r/length*0.6
			cc := withAlpha(c, uint8(255*fade))
			for t := -0.6 * scale; t <= 0.6*scale; t += 0.5 {
				setPx(v.img, int(cx+dx*(ring+r)-dy*t), int(cy+dy*(ring+r)+dx*t), cc)
			}
		}
	}

	// The point sphere.
	v.angle += 0.012 + 0.03*v.energy + 0.05*pulse
	ca, sa := math.Cos(v.angle), math.Sin(v.angle)
	tilt := 0.35 + 0.1*math.Sin(v.angle*0.3)
	ct, st := math.Cos(tilt), math.Sin(tilt)
	for _, pt := range unitSphere {
		// Each latitude follows a band: bass at the poles, highs at the equator.
		band := int(math.Abs(pt[1]) * float64(maxBands/2-1))
		disp := 1 + v.bands[maxBands/2-1-band]*0.28*intensity + 0.08*pulse
		x := pt[0]*ca - pt[2]*sa
		z := pt[0]*sa + pt[2]*ca
		y := pt[1]*ct - z*st
		z = pt[1]*st + z*ct
		depth := (z + 1) / 2 // 0 back, 1 front
		persp := 1 / (1.6 - z*0.4)
		sx := cx + x*radius*disp*persp*1.25
		sy := cy + y*radius*disp*persp*1.25
		c := gradientAt(p, 0.25+0.6*depth+0.2*pulse)
		c.A = uint8(70 + 185*depth)
		size := (0.45 + 0.9*depth) * scale * 0.75
		fillCircle(v.img, sx, sy, size, c)
	}

	// Sparks on the beat.
	if v.beat.pulse > 0.99 && intensity > 0.2 {
		for range 14 {
			a := rand.Float64() * 2 * math.Pi
			speed := (1.5 + rand.Float64()*2.5) * scale
			v.sparks = append(v.sparks, particle{
				x: cx + math.Cos(a)*radius, y: cy + math.Sin(a)*radius,
				vx: math.Cos(a) * speed, vy: math.Sin(a) * speed, life: 1,
			})
		}
	}
	live := v.sparks[:0]
	for _, s := range v.sparks {
		s.x += s.vx
		s.y += s.vy
		s.life -= 0.035
		if s.life <= 0 {
			continue
		}
		c := withAlpha(p.FG, uint8(220*s.life))
		fillCircle(v.img, s.x, s.y, 1.2*scale*s.life+0.5, c)
		live = append(live, s)
	}
	v.sparks = live
}

// coverColor returns the dominant color of the playing cover, when known.
func (a *App) coverColor() (color.NRGBA, bool) {
	a.coverMu.Lock()
	defer a.coverMu.Unlock()
	return a.cover, a.hasCover
}

// setCoverImage records the playing cover for the dynamic colors.
func (a *App) setCoverImage(img image.Image) {
	a.coverMu.Lock()
	defer a.coverMu.Unlock()
	if img == nil {
		a.hasCover = false
		return
	}
	a.cover = dominantColor(img)
	a.hasCover = true
}
