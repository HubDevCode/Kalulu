package gui

import (
	"hash/fnv"
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	cardW   = 172
	cardImg = 156
	cardGap = 14
)

// card is a cover with a title and a subtitle. Hovering shows a play
// button; clicking the card opens it.
type card struct {
	widget.BaseWidget
	thumb   string
	loaded  bool
	art     *artView
	title   *widget.Label
	sub     *widget.Label
	bg      *canvas.Rectangle
	play    *roundButton
	onOpen  func()
	onPlay  func()
	onMenu  func(pos fyne.Position)
	hovered bool
	hover   hoverPrefetch
}

// newCard builds a card. circle draws the cover round, for artists.
func newCard(a *App, title, sub, thumb string, circle bool, onOpen, onPlay func()) *card {
	c := &card{onOpen: onOpen, onPlay: onPlay}
	radius := float32(10)
	if circle {
		radius = cardImg / 2
	}
	c.art = newArtView(a, cardImg, radius)
	c.art.image.FillMode = canvas.ImageFillCover
	c.art.onTap = c.open
	c.thumb = thumb
	c.title = trunc(boldLabel(title))
	c.sub = trunc(captionLabel(sub))
	if circle {
		c.title.Alignment = fyne.TextAlignCenter
		c.sub.Alignment = fyne.TextAlignCenter
	}
	c.bg = canvas.NewRectangle(color.Transparent)
	c.bg.CornerRadius = 12
	c.play = newRoundButton(theme.MediaPlayIcon(), 40, func() {
		if c.onPlay != nil {
			c.onPlay()
		}
	})
	c.play.Hide()
	c.ExtendBaseWidget(c)
	return c
}

func (c *card) CreateRenderer() fyne.WidgetRenderer {
	playLayer := container.NewVBox(spacer(), container.NewHBox(spacer(), c.play))
	cover := container.NewStack(c.art.Object(), padded(0, 4, 4, 0, playLayer))
	body := container.NewVBox(cover, container.New(&tightVBox{}, c.title, c.sub))
	return widget.NewSimpleRenderer(container.NewStack(c.bg, padded(8, 8, 8, 8, body)))
}

func (c *card) MinSize() fyne.Size {
	return fyne.NewSize(cardW, c.BaseWidget.MinSize().Height)
}

func (c *card) open() {
	if c.onOpen != nil {
		c.onOpen()
	} else if c.onPlay != nil {
		c.onPlay()
	}
}

func (c *card) Tapped(*fyne.PointEvent) { c.open() }

func (c *card) TappedSecondary(ev *fyne.PointEvent) {
	if c.onMenu != nil {
		c.onMenu(ev.AbsolutePosition)
	}
}

func (c *card) MouseIn(*desktop.MouseEvent) {
	c.hover.in()
	c.hovered = true
	c.bg.FillColor = theme.Color(theme.ColorNameHover)
	c.bg.Refresh()
	if c.onPlay != nil {
		c.play.Show()
	}
}

func (c *card) MouseMoved(*desktop.MouseEvent) {}

func (c *card) MouseOut() {
	c.hover.out()
	c.hovered = false
	c.bg.FillColor = color.Transparent
	c.bg.Refresh()
	c.play.Hide()
}

func (c *card) Cursor() desktop.Cursor { return desktop.PointerCursor }

// tile is a compact song entry: a small cover, title and artist, for the
// quick picks grid.
type tile struct {
	widget.BaseWidget
	thumb  string
	loaded bool
	art    *artView
	title  *widget.Label
	sub    *widget.Label
	bg     *canvas.Rectangle
	onPlay func()
	onMenu func(pos fyne.Position)
	hover  hoverPrefetch
}

func newTile(a *App, title, sub, thumb string, onPlay func()) *tile {
	t := &tile{onPlay: onPlay}
	t.art = newArtView(a, 48, 6)
	t.art.image.FillMode = canvas.ImageFillCover
	t.art.onTap = onPlay
	t.thumb = thumb
	t.title = trunc(label(title))
	t.sub = trunc(captionLabel(sub))
	t.bg = canvas.NewRectangle(color.Transparent)
	t.bg.CornerRadius = 8
	t.ExtendBaseWidget(t)
	return t
}

func (t *tile) CreateRenderer() fyne.WidgetRenderer {
	text := container.NewVBox(spacer(), container.New(&tightVBox{}, t.title, t.sub), spacer())
	row := container.NewBorder(nil, nil, padded(0, 8, 0, 0, t.art.Object()), nil, text)
	return widget.NewSimpleRenderer(container.NewStack(t.bg, padded(4, 6, 4, 6, row)))
}

func (t *tile) Tapped(*fyne.PointEvent) {
	if t.onPlay != nil {
		t.onPlay()
	}
}

func (t *tile) TappedSecondary(ev *fyne.PointEvent) {
	if t.onMenu != nil {
		t.onMenu(ev.AbsolutePosition)
	}
}

func (t *tile) MouseIn(*desktop.MouseEvent) {
	t.hover.in()
	t.bg.FillColor = theme.Color(theme.ColorNameHover)
	t.bg.Refresh()
}
func (t *tile) MouseMoved(*desktop.MouseEvent) {}
func (t *tile) MouseOut() {
	t.hover.out()
	t.bg.FillColor = color.Transparent
	t.bg.Refresh()
}
func (t *tile) Cursor() desktop.Cursor { return desktop.PointerCursor }

// pager shows a page of items that fits the width and pages with arrows.
// Cards keep their width; tiles stretch to fill the columns.
type pager struct {
	items   []fyne.CanvasObject
	minW    float32
	rows    int
	stretch bool
	page    int
	per     int
	prev    *widget.Button
	next    *widget.Button
	grid    *fyne.Container
}

func newPager(items []fyne.CanvasObject, minW float32, rows int, stretch bool) *pager {
	p := &pager{items: items, minW: minW, rows: max(1, rows), stretch: stretch}
	p.prev = iconButton(theme.NavigateBackIcon(), func() { p.turn(-1) })
	p.next = iconButton(theme.NavigateNextIcon(), func() { p.turn(1) })
	p.grid = container.New(&pagerLayout{p: p}, items...)
	return p
}

func (p *pager) turn(delta int) {
	pages := p.pages()
	p.page = max(0, min(pages-1, p.page+delta))
	p.grid.Refresh()
}

func (p *pager) pages() int {
	if p.per <= 0 {
		return 1
	}
	return max(1, (len(p.items)+p.per-1)/p.per)
}

func (p *pager) updateArrows() {
	setEnabled(p.prev, p.page > 0)
	setEnabled(p.next, p.page < p.pages()-1)
	hidden := p.pages() <= 1
	if hidden != p.prev.Hidden {
		p.prev.Hidden, p.next.Hidden = hidden, hidden
		p.prev.Refresh()
		p.next.Refresh()
	}
}

func setEnabled(b *widget.Button, on bool) {
	if on && b.Disabled() {
		b.Enable()
	} else if !on && !b.Disabled() {
		b.Disable()
	}
}

type pagerLayout struct{ p *pager }

func (l *pagerLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objects {
		h = max(h, o.MinSize().Height)
	}
	rows := min(l.p.rows, max(1, len(objects)))
	return fyne.NewSize(l.p.minW, h*float32(rows)+cardGap/2*float32(rows-1))
}

func (l *pagerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	p := l.p
	gap := float32(cardGap / 2)
	cols := max(1, int((size.Width+gap)/(p.minW+gap)))
	per := cols * p.rows
	if per != p.per {
		// Keep the first visible item in view when the width changes.
		first := p.page * max(1, p.per)
		p.per = per
		p.page = first / per
	}
	p.page = max(0, min(p.pages()-1, p.page))
	itemW := p.minW
	if p.stretch {
		itemW = (size.Width - gap*float32(cols-1)) / float32(cols)
	}
	h := float32(0)
	for _, o := range objects {
		h = max(h, o.MinSize().Height)
	}
	start := p.page * per
	for i, o := range objects {
		if i < start || i >= start+per {
			o.Hide()
			continue
		}
		o.Show()
		k := i - start
		col, row := k/p.rows, k%p.rows
		if !p.stretch {
			col, row = k%cols, k/cols
			if p.rows > 1 {
				col, row = k/p.rows, k%p.rows
			}
		}
		o.Move(fyne.NewPos(float32(col)*(itemW+gap), float32(row)*(h+gap)))
		o.Resize(fyne.NewSize(itemW, h))
	}
	p.updateArrows()
}

// shelf is a titled row: a heading, the paging arrows and the items.
func shelf(title string, p *pager, extra ...fyne.CanvasObject) fyne.CanvasObject {
	head := label(title)
	head.SizeName = theme.SizeNameSubHeadingText
	head.TextStyle = fyne.TextStyle{Bold: true}
	right := container.NewHBox(append(extra, p.prev, p.next)...)
	return padded(10, 0, 6, 0, container.NewVBox(
		container.NewBorder(nil, nil, nil, right, trunc(head)),
		p.grid,
	))
}

// chip is a rounded clickable label for genres and categories.
type chip struct {
	widget.BaseWidget
	text     *widget.Label
	bg       *canvas.Rectangle
	onTap    func()
	selected bool
}

func newChip(text string, onTap func()) *chip {
	c := &chip{onTap: onTap}
	c.text = label(text)
	c.bg = canvas.NewRectangle(theme.Color(theme.ColorNameButton))
	c.bg.CornerRadius = 16
	c.ExtendBaseWidget(c)
	return c
}

func (c *chip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(c.bg, padded(0, 10, 0, 10, c.text)))
}

func (c *chip) SetSelected(v bool) {
	c.selected = v
	if v {
		c.bg.FillColor = theme.Color(theme.ColorNamePrimary)
		c.text.Importance = widget.MediumImportance
		c.text.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		c.bg.FillColor = theme.Color(theme.ColorNameButton)
		c.text.TextStyle = fyne.TextStyle{}
	}
	c.bg.Refresh()
	c.text.Refresh()
}

func (c *chip) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}
func (c *chip) Cursor() desktop.Cursor { return desktop.PointerCursor }

// chipRow wraps chips over as many lines as needed.
func chipRow(chips ...fyne.CanvasObject) *fyne.Container {
	return container.New(&flowLayout{gap: 8}, chips...)
}

// flowLayout places objects left to right and wraps to new lines.
type flowLayout struct {
	gap   float32
	lines int
}

func (f *flowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	// The height depends on the width; report one line and let Layout grow
	// the container through its parent's next pass.
	h, w := float32(0), float32(0)
	for _, o := range objects {
		m := o.MinSize()
		h = max(h, m.Height)
		w = max(w, m.Width)
	}
	lines := f.lines
	if lines == 0 {
		lines = 1
	}
	return fyne.NewSize(w, h*float32(lines)+f.gap*float32(lines-1))
}

func (f *flowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x, y, lineH := float32(0), float32(0), float32(0)
	lines := 1
	for _, o := range objects {
		m := o.MinSize()
		if x > 0 && x+m.Width > size.Width {
			x = 0
			y += lineH + f.gap
			lineH = 0
			lines++
		}
		o.Move(fyne.NewPos(x, y))
		o.Resize(m)
		x += m.Width + f.gap
		lineH = max(lineH, m.Height)
	}
	f.lines = lines
}

// moodTile is a colored category tile, as the Browse grid of a store.
func moodTile(title string, onTap func()) fyne.CanvasObject {
	bg := canvas.NewRectangle(tileColor(title))
	bg.CornerRadius = 12
	l := label(title)
	l.TextStyle = fyne.TextStyle{Bold: true}
	l.Wrapping = fyne.TextWrapWord
	tap := newTapArea(onTap)
	return fixed(0, 86, container.NewStack(bg, padded(6, 6, 6, 8, container.NewVBox(l)), tap))
}

// tileColor picks a stable vivid color for a name.
func tileColor(name string) color.NRGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	hue := float64(h.Sum32()%360) / 360
	return hsl(hue, 0.55, 0.38)
}

func hsl(h, s, l float64) color.NRGBA {
	hue := func(p, q, t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	return color.NRGBA{
		uint8(255 * hue(p, q, h+1.0/3)), uint8(255 * hue(p, q, h)), uint8(255 * hue(p, q, h-1.0/3)), 255,
	}
}

// dominantColor returns a vivid, darkened average color of img for header
// gradients.
func dominantColor(img image.Image) color.NRGBA {
	b := img.Bounds()
	var r, g, bl, n float64
	step := max(1, b.Dx()/24)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(cr>>8), float64(cg>>8), float64(cb>>8)
			// Weight saturated pixels up, so a grey border does not win.
			sat := (math.Max(fr, math.Max(fg, fb)) - math.Min(fr, math.Min(fg, fb))) / 255
			w := 0.2 + sat*sat*4
			r, g, bl, n = r+fr*w, g+fg*w, bl+fb*w, n+w
		}
	}
	if n == 0 {
		return color.NRGBA{0x40, 0x30, 0x80, 0xff}
	}
	c := color.NRGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 0xff}
	h, s, _ := toHSL(c)
	return hsl(h, math.Min(0.75, math.Max(0.35, s*1.3)), 0.32)
}

func toHSL(c color.NRGBA) (h, s, l float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

// hero is the large header of a detail page: a gradient from the cover's
// color, the cover, a kind label, the title, a subtitle and buttons.
type hero struct {
	root     fyne.CanvasObject
	gradient *canvas.LinearGradient
	art      *artView
	title    *widget.Label
	sub      *widget.Label
	kind     *widget.Label
	buttons  *fyne.Container
	app      *App
}

func newHero(a *App, kind, title, sub, thumb string, circle bool, back func()) *hero {
	h := &hero{app: a}
	p := a.theme.p
	h.gradient = canvas.NewLinearGradient(mix(p.Accent, p.BG, 0.45), p.BG, 0)
	radius := float32(12)
	if circle {
		radius = 100
	}
	h.art = newArtView(a, 200, radius)
	h.art.image.FillMode = canvas.ImageFillCover
	h.art.onLoaded = func(img image.Image) {
		if img != nil {
			c := dominantColor(img)
			if p.Light {
				c = mix(c, p.BG, 0.55)
			}
			h.gradient.StartColor = c
			h.gradient.Refresh()
		}
	}
	if thumb != "" {
		h.art.LoadURL(thumb)
	}
	h.kind = captionLabel(kind)
	h.kind.TextStyle = fyne.TextStyle{Bold: true}
	h.title = label(title)
	h.title.SizeName = theme.SizeNameHeadingText
	h.title.TextStyle = fyne.TextStyle{Bold: true}
	h.title.Wrapping = fyne.TextWrapWord
	h.sub = label(sub)
	h.sub.Wrapping = fyne.TextWrapWord
	h.sub.Importance = widget.LowImportance
	h.buttons = container.NewHBox()
	var top fyne.CanvasObject = fixed(0, 4)
	if back != nil {
		b := widget.NewButtonWithIcon(T("Back"), theme.NavigateBackIcon(), back)
		b.Importance = widget.LowImportance
		top = container.NewHBox(b)
	}
	info := container.NewVBox(spacer(), h.kind, h.title, h.sub, fixed(0, 6), h.buttons)
	row := container.NewBorder(nil, nil, padded(0, 18, 0, 0, h.art.Object()), nil, info)
	h.root = container.NewStack(h.gradient, padded(10, 24, 18, 24, container.NewVBox(top, row)))
	return h
}

// primaryButton is the big accent play button of a hero.
func primaryButton(text string, res fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(text, res, tapped)
	b.Importance = widget.HighImportance
	return b
}

func flatButton(text string, res fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(text, res, tapped)
	b.Importance = widget.LowImportance
	return b
}

// sectionHeading is a bold subheading for a page section.
func sectionHeading(text string) fyne.CanvasObject {
	l := label(text)
	l.SizeName = theme.SizeNameSubHeadingText
	l.TextStyle = fyne.TextStyle{Bold: true}
	return padded(12, 0, 4, 0, trunc(l))
}

// scrollPage wraps page content in a vertical scroll with side margins.
func scrollPage(content fyne.CanvasObject) *container.Scroll {
	return container.NewVScroll(padded(0, 24, 24, 24, content))
}

// loadingBox shows a spinner-like message while a page loads.
func loadingBox(text string) fyne.CanvasObject {
	bar := widget.NewProgressBarInfinite()
	l := dimLabel(text)
	l.Alignment = fyne.TextAlignCenter
	return padded(40, 0, 40, 0, container.NewCenter(fixed(260, 0, container.NewVBox(l, bar))))
}

// lazyArt is a card whose cover downloads when it is first on screen.
type lazyArt interface{ ensureArt() }

func (c *card) ensureArt() {
	if !c.loaded && c.thumb != "" {
		c.loaded = true
		c.art.LoadURL(c.thumb)
	}
}

func (t *tile) ensureArt() {
	if !t.loaded && t.thumb != "" {
		t.loaded = true
		t.art.LoadURL(t.thumb)
	}
}

func (c *card) Resize(size fyne.Size) {
	c.BaseWidget.Resize(size)
	if c.Visible() && size.Width > 0 {
		c.ensureArt()
	}
}

func (t *tile) Resize(size fyne.Size) {
	t.BaseWidget.Resize(size)
	if t.Visible() && size.Width > 0 {
		t.ensureArt()
	}
}
