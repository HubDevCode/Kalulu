package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// Album cards show the cover with its record sliding out on the right.
const (
	albumImg   = 116 // cover side
	albumShift = 0.38
	albumDisc  = 0.94 // disc diameter, as a share of the cover
)

// withRecord turns c into an album card. Call it before the card shows.
func (c *card) withRecord(a *App) *card {
	c.art = newArtView(a, albumImg, 4)
	c.art.image.FillMode = canvas.ImageFillCover
	c.art.onTap = c.open
	c.record = true
	return c
}

// recordLayout puts the disc behind the right half of the cover.
type recordLayout struct{}

func (recordLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(albumImg*(albumShift+albumDisc), albumImg)
}

func (recordLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	side := float32(albumImg)
	d := side * albumDisc
	disc, cover := objects[0], objects[1:]
	disc.Move(fyne.NewPos(side*albumShift, (side-d)/2))
	disc.Resize(fyne.NewSize(d, d))
	for _, o := range cover {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(fyne.NewSize(side, side))
	}
}

// The colors of a record, shared by the player vinyl and the album discs:
// a dark glass disc over the page, grooves in the accent, a tinted label.
var (
	vinylDisk          = color.NRGBA{0, 0, 0, 0x5a}
	roleVinyl     role = func(Palette) color.Color { return vinylDisk }
	roleGroove    role = func(p Palette) color.Color { return withAlpha(p.Accent, 0x8c) }
	roleRim       role = func(p Palette) color.Color { return withAlpha(p.FG, 0x1c) }
	roleDiscLabel role = func(p Palette) color.Color { return mix(p.Accent, p.BG, 0.35) }
	roleSpindle   role = func(Palette) color.Color { return color.NRGBA{0x10, 0x10, 0x10, 0xff} }
)

// recordDisc is the disc of an album card, drawn as the player vinyl
// without a picture on its label.
func recordDisc() fyne.CanvasObject {
	objects := []fyne.CanvasObject{tCircle(roleVinyl, nil), withStroke(tCircle(nil, roleRim), 1)}
	for range 5 {
		objects = append(objects, withStroke(tCircle(nil, roleGroove), 1))
	}
	objects = append(objects, tCircle(roleDiscLabel, nil), tCircle(roleSpindle, nil))
	return container.New(discLayout{}, objects...)
}

func withStroke(c *canvas.Circle, w float32) *canvas.Circle {
	c.StrokeWidth = w
	return c
}

// discLayout sizes the disc, rim, grooves, label and spindle as the
// player vinyl does.
type discLayout struct{}

func (discLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(20, 20) }

func (discLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	d := min(size.Width, size.Height)
	cx, cy := size.Width/2, size.Height/2
	place := func(o fyne.CanvasObject, diam float32) {
		o.Move(fyne.NewPos(cx-diam/2, cy-diam/2))
		o.Resize(fyne.NewSize(diam, diam))
	}
	place(objects[0], d)
	place(objects[1], d)
	for i, g := range objects[2:7] {
		place(g, d*(0.56+0.08*float32(i)))
	}
	place(objects[7], d*0.44)
	place(objects[8], max(3, d*0.025))
}
