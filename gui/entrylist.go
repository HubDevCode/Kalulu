package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// entry is one row of an EntryList: a playlist, an artist, an album or a
// section header.
type entry struct {
	Icon   fyne.Resource
	Title  string
	Sub    string
	Right  string
	Header bool
	// Star shows a favorite toggle when set.
	Star   *bool
	OnStar func()
	OnOpen func()
	OnPlay func()
	Menu   func() *fyne.Menu
}

// EntryList is a list of browsable entries.
type EntryList struct {
	app         *App
	entries     []entry
	list        *widget.List
	root        fyne.CanvasObject
	placeholder *widget.Label
	footer      *fyne.Container
}

func NewEntryList(app *App) *EntryList {
	l := &EntryList{app: app}
	l.list = widget.NewList(
		func() int { return len(l.entries) },
		func() fyne.CanvasObject { return newEntryRow(l) },
		func(id widget.ListItemID, o fyne.CanvasObject) { o.(*entryRow).bind(id) },
	)
	l.list.HideSeparators = true
	l.placeholder = dimLabel("")
	l.placeholder.Alignment = fyne.TextAlignCenter
	l.footer = container.NewCenter()
	l.root = container.NewBorder(nil, l.footer, nil, nil,
		container.NewStack(padded(0, 8, 0, 8, l.list), container.NewCenter(l.placeholder)))
	return l
}

func (l *EntryList) Object() fyne.CanvasObject { return l.root }

func (l *EntryList) SetEntries(entries []entry) {
	l.entries = entries
	l.placeholder.Hidden = len(entries) > 0
	l.placeholder.Refresh()
	l.list.Refresh()
}

func (l *EntryList) SetPlaceholder(text string) { l.placeholder.SetText(text) }

// SetFooter shows obj under the list, such as a Load more button.
func (l *EntryList) SetFooter(obj fyne.CanvasObject) {
	if obj == nil {
		l.footer.Objects = nil
	} else {
		l.footer.Objects = []fyne.CanvasObject{padded(6, 0, 10, 0, obj)}
	}
	l.footer.Refresh()
}

type entryRow struct {
	widget.BaseWidget
	list    *EntryList
	index   int
	hovered bool

	bg     *canvas.Rectangle
	icon   *widget.Icon
	title  *widget.Label
	sub    *widget.Label
	right  *widget.Label
	star   *widget.Button
	play   *widget.Button
	header *widget.Label
	body   *fyne.Container
}

var (
	_ fyne.Tappable          = (*entryRow)(nil)
	_ fyne.SecondaryTappable = (*entryRow)(nil)
	_ desktop.Hoverable      = (*entryRow)(nil)
)

func newEntryRow(l *EntryList) *entryRow {
	r := &entryRow{list: l, index: -1}
	r.bg = canvas.NewRectangle(color.Transparent)
	r.bg.CornerRadius = 6
	r.icon = widget.NewIcon(nil)
	r.title = trunc(label(""))
	r.sub = trunc(captionLabel(""))
	r.right = dimLabel("")
	r.star = iconButton(iconHeartLine, func() {
		if e := r.entry(); e != nil && e.OnStar != nil {
			e.OnStar()
		}
	})
	r.play = iconButton(theme.MediaPlayIcon(), func() {
		if e := r.entry(); e != nil && e.OnPlay != nil {
			e.OnPlay()
		}
	})
	r.header = boldLabel("")
	r.header.Importance = widget.HighImportance
	r.header.SizeName = theme.SizeNameCaptionText
	r.ExtendBaseWidget(r)
	return r
}

func (r *entryRow) entry() *entry {
	if r.index < 0 || r.index >= len(r.list.entries) {
		return nil
	}
	return &r.list.entries[r.index]
}

func (r *entryRow) CreateRenderer() fyne.WidgetRenderer {
	text := container.New(&tightVBox{}, r.title, r.sub)
	right := container.NewHBox(r.right, r.star, r.play)
	r.body = container.NewBorder(nil, nil, padded(0, 6, 0, 4, r.icon), right, text)
	return widget.NewSimpleRenderer(container.NewStack(r.bg, r.body, padded(10, 0, 0, 4, r.header)))
}

func (r *entryRow) bind(i int) {
	r.index = i
	e := r.entry()
	if e == nil {
		return
	}
	if e.Header {
		r.body.Hide()
		r.header.SetText(e.Title)
		r.header.Show()
		r.hovered = false
		r.updateBG()
		return
	}
	r.header.Hide()
	r.body.Show()
	r.icon.SetResource(e.Icon)
	r.title.SetText(e.Title)
	r.sub.SetText(e.Sub)
	r.sub.Hidden = e.Sub == ""
	r.right.SetText(e.Right)
	if e.Star != nil {
		r.star.Show()
		if *e.Star {
			r.star.SetIcon(theme.NewPrimaryThemedResource(iconHeart))
		} else {
			r.star.SetIcon(iconHeartLine)
		}
	} else {
		r.star.Hide()
	}
	r.play.Hidden = e.OnPlay == nil
	r.updateBG()
}

func (r *entryRow) updateBG() {
	e := r.entry()
	if r.hovered && e != nil && !e.Header {
		r.bg.FillColor = theme.Color(theme.ColorNameHover)
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
}

func (r *entryRow) Tapped(*fyne.PointEvent) {
	if e := r.entry(); e != nil && e.OnOpen != nil {
		e.OnOpen()
	}
}

func (r *entryRow) TappedSecondary(ev *fyne.PointEvent) {
	e := r.entry()
	if e == nil || e.Menu == nil {
		return
	}
	if menu := e.Menu(); menu != nil {
		widget.ShowPopUpMenuAtPosition(menu, r.list.app.win.Canvas(), ev.AbsolutePosition)
	}
}

func (r *entryRow) MouseIn(*desktop.MouseEvent)    { r.hovered = true; r.updateBG() }
func (r *entryRow) MouseMoved(*desktop.MouseEvent) {}
func (r *entryRow) MouseOut()                      { r.hovered = false; r.updateBG() }
func (r *entryRow) Cursor() desktop.Cursor {
	if e := r.entry(); e != nil && e.OnOpen != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}
