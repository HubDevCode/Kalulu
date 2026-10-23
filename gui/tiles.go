package gui

import (
	"context"
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/f64"
	fx "golang.org/x/image/math/fixed"
)

// Category tile size in points; the art renders at twice that.
const (
	categoryW = 214
	categoryH = 100
)

// categoryGrid is the Browse grid of the search page: one heading and one
// grid of colored tiles for each YouTube Music shelf of moods and genres.
func (a *App) categoryGrid(ctx context.Context) (fyne.CanvasObject, error) {
	shelves, err := a.ytm.Moods(ctx)
	if err != nil {
		return nil, err
	}
	box := container.NewVBox()
	for _, s := range shelves {
		if len(s.Items) == 0 {
			continue
		}
		grid := container.NewGridWrap(fyne.NewSize(categoryW, categoryH))
		for _, it := range s.Items {
			it := it
			grid.Add(categoryTile(it.Title, func() { a.openItem(it) }))
		}
		title := s.Title
		if title == "" {
			title = T("Browse all")
		}
		box.Add(sectionHeading(title))
		box.Add(grid)
	}
	return box, nil
}

// categoryTile is a vivid tile with its name and a tilted record sleeve in
// the corner.
func categoryTile(title string, onTap func()) fyne.CanvasObject {
	img := canvas.NewImageFromImage(categoryArt(categoryColor(title)))
	img.FillMode = canvas.ImageFillStretch
	img.ScaleMode = canvas.ImageScaleSmooth
	l := label(title)
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.SizeName = theme.SizeNameSubHeadingText
	l.Wrapping = fyne.TextWrapWord
	name := container.NewThemeOverride(padded(4, 64, 0, 6, container.NewVBox(l)), onColorTheme{})
	return container.NewStack(img, name, newTapArea(onTap))
}

// categoryColor picks a stable vivid color for a name.
func categoryColor(name string) color.NRGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return hsl(float64(h.Sum32()%360)/360, 0.58, 0.44)
}

// onColorTheme writes white text on a colored tile in every palette.
type onColorTheme struct{}

func (onColorTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameForeground {
		return color.White
	}
	return fyne.CurrentApp().Settings().Theme().Color(n, v)
}

func (onColorTheme) Font(s fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(s)
}

func (onColorTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(n)
}

func (onColorTheme) Size(n fyne.ThemeSizeName) float32 {
	return fyne.CurrentApp().Settings().Theme().Size(n)
}

var categoryArts = struct {
	sync.Mutex
	m      map[color.NRGBA]image.Image
	sleeve image.Image
}{m: map[color.NRGBA]image.Image{}}

// categoryArt draws the tile background: a rounded rectangle of c with a
// dark "Album" sleeve tilted over its bottom right corner.
func categoryArt(c color.NRGBA) image.Image {
	categoryArts.Lock()
	defer categoryArts.Unlock()
	if img, ok := categoryArts.m[c]; ok {
		return img
	}
	if categoryArts.sleeve == nil {
		categoryArts.sleeve = sleeveImage(160)
	}
	w, h := categoryW*2, categoryH*2
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)

	sleeve := categoryArts.sleeve
	side := float64(sleeve.Bounds().Dx())
	angle := 22 * math.Pi / 180
	cx, cy := float64(w)-side*0.3, float64(h)-side*0.22
	place := func(dx, dy float64) f64.Aff3 {
		cos, sin := math.Cos(angle), math.Sin(angle)
		ox, oy := cx+dx, cy+dy
		return f64.Aff3{
			cos, -sin, ox - cos*side/2 + sin*side/2,
			sin, cos, oy - sin*side/2 - cos*side/2,
		}
	}
	// A soft shadow under the sleeve, then the sleeve.
	shadow := image.NewUniform(color.NRGBA{0, 0, 0, 0x40})
	for _, off := range [][2]float64{{-10, 8}, {-6, 5}} {
		draw.BiLinear.Transform(dst, place(off[0], off[1]), shadow, sleeve.Bounds(), draw.Over, &draw.Options{SrcMask: sleeve, SrcMaskP: image.Point{}})
	}
	draw.BiLinear.Transform(dst, place(0, 0), sleeve, sleeve.Bounds(), draw.Over, nil)
	roundCorners(dst, 20)
	categoryArts.m[c] = dst
	return dst
}

// sleeveImage is a dark square sleeve with "Album" on it.
func sleeveImage(side int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{0x1e, 0x1e, 0x22, 0xff}), image.Point{}, draw.Src)
	f, err := opentype.Parse(gobolditalic.TTF)
	if err != nil {
		return img
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(side) * 0.19, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return img
	}
	defer face.Close()
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: face}
	d.Dot = fx.P(side/7, side*3/10)
	d.DrawString(T("Album"))
	return img
}

// roundCorners clears the pixels outside a rounded rectangle, with a soft
// edge.
func roundCorners(img *image.NRGBA, r float64) {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			qx := math.Max(math.Max(r-px, px-(w-r)), 0)
			qy := math.Max(math.Max(r-py, py-(h-r)), 0)
			if qx == 0 && qy == 0 {
				continue
			}
			cov := math.Max(0, math.Min(1, r-math.Hypot(qx, qy)+0.5))
			if cov >= 1 {
				continue
			}
			i := img.PixOffset(x, y)
			img.Pix[i+3] = uint8(float64(img.Pix[i+3]) * cov)
		}
	}
}
