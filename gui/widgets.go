package gui

import (
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
)

// uiStopped is set once the Fyne event loop has ended, for example on
// SIGTERM, which Fyne handles itself. Calls after that are dropped: the
// driver would run them inline against a terminated GLFW.
var uiStopped atomic.Bool

// runOnUI runs fn on the Fyne event thread.
func runOnUI(fn func()) {
	if uiStopped.Load() {
		return
	}
	fyne.Do(func() {
		if !uiStopped.Load() {
			fn()
		}
	})
}

// fixedLayout gives its children a fixed width and/or height. A zero
// dimension follows the children.
type fixedLayout struct{ w, h float32 }

func (l fixedLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		size = size.Max(o.MinSize())
	}
	if l.w > 0 {
		size.Width = l.w
	}
	if l.h > 0 {
		size.Height = l.h
	}
	return size
}

func (l fixedLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func fixed(w, h float32, objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(fixedLayout{w: w, h: h}, objects...)
}

// panel puts content on a rounded surface.
func panel(fill color.Color, radius float32, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius
	return container.NewStack(bg, content)
}

func padded(top, right, bottom, left float32, content fyne.CanvasObject) *fyne.Container {
	return container.New(&paddingLayout{top, right, bottom, left}, content)
}

type paddingLayout struct{ t, r, b, l float32 }

func (p *paddingLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if o.Visible() {
			size = size.Max(o.MinSize())
		}
	}
	return size.Add(fyne.NewSize(p.l+p.r, p.t+p.b))
}

func (p *paddingLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.NewPos(p.l, p.t))
		o.Resize(size.Subtract(fyne.NewSize(p.l+p.r, p.t+p.b)))
	}
}

// label makes a label that keeps its full width.
func label(text string) *widget.Label {
	return widget.NewLabel(text)
}

// trunc makes l shorten with an ellipsis when its column is narrow.
func trunc(l *widget.Label) *widget.Label {
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}

func dimLabel(text string) *widget.Label {
	l := label(text)
	l.Importance = widget.LowImportance
	return l
}

func captionLabel(text string) *widget.Label {
	l := dimLabel(text)
	l.SizeName = theme.SizeNameCaptionText
	return l
}

func heading(text string) *widget.Label {
	l := label(text)
	l.SizeName = theme.SizeNameHeadingText
	l.TextStyle = fyne.TextStyle{Bold: true}
	return l
}

func boldLabel(text string) *widget.Label {
	l := label(text)
	l.TextStyle = fyne.TextStyle{Bold: true}
	return l
}

// iconButton is a flat icon-only button.
func iconButton(res fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon("", res, tapped)
	b.Importance = widget.LowImportance
	return b
}

// pageHeader is the title row of a view with actions on the right.
func pageHeader(title *widget.Label, subtitle *widget.Label, actions ...fyne.CanvasObject) fyne.CanvasObject {
	left := container.NewVBox(title)
	if subtitle != nil {
		left.Add(subtitle)
	}
	right := container.NewHBox(actions...)
	trunc(title)
	if subtitle != nil {
		trunc(subtitle)
	}
	return padded(14, 18, 6, 18, container.NewBorder(nil, nil, nil, container.NewCenter(right), left))
}

// fmtDuration formats seconds as m:ss or h:mm:ss.
func fmtDuration(secs float64) string {
	if secs <= 0 {
		return "--:--"
	}
	s := int(secs + 0.5)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// trackTitle returns the display title and subtitle of a track.
func trackTitle(t ipc.TrackInfo) (string, string) {
	title := t.Title
	if t.StreamTitle != "" {
		title = t.StreamTitle
	}
	if title == "" {
		title = baseName(t.Path)
	}
	var sub []string
	if t.Artist != "" {
		sub = append(sub, t.Artist)
	}
	if t.Station != "" {
		sub = append(sub, t.Station)
	} else if t.Stream && t.StreamTitle != "" && t.Title != "" {
		sub = append(sub, t.Title)
	}
	return title, strings.Join(sub, " · ")
}

func baseName(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 && i < len(path)-1 {
		path = path[i+1:]
	}
	if i := strings.LastIndex(path, "."); i > 0 && len(path)-i <= 5 {
		path = path[:i]
	}
	return path
}

// TrackList is a track table shared by the queue, the provider browser,
// search and history.
type TrackList struct {
	app      *App
	Tracks   []ipc.TrackInfo
	list     *widget.List
	selected int
	root     fyne.CanvasObject

	// OnPlay plays row i on a double click or the Play menu entry.
	OnPlay func(i int)
	// Menu builds the context menu of row i.
	Menu func(i int) *fyne.Menu
	// IsCurrent marks the playing row.
	IsCurrent func(i int) bool
	// Badge returns an extra marker shown before the duration, such as the
	// play-next position.
	Badge func(i int) string
	// Placeholder shows when the list is empty.
	placeholder *widget.Label
}

func NewTrackList(app *App) *TrackList {
	t := &TrackList{app: app, selected: -1}
	t.list = widget.NewList(
		func() int { return len(t.Tracks) },
		func() fyne.CanvasObject { return newTrackRow(t) },
		func(id widget.ListItemID, o fyne.CanvasObject) { o.(*trackRow).bind(id) },
	)
	t.list.HideSeparators = true
	header := container.New(&rowLayout{},
		captionLabel("#"), captionLabel(strings.ToUpper(T("Title"))), captionLabel(strings.ToUpper(T("Album"))), captionLabel(""), captionLabel(strings.ToUpper(T("Time"))), widget.NewLabel(""))
	t.placeholder = dimLabel("Nothing here yet")
	t.placeholder.Alignment = fyne.TextAlignCenter
	t.placeholder.Hide()
	sep := tRect(roleBorder)
	sep.SetMinSize(fyne.NewSize(1, 1))
	t.root = container.NewBorder(padded(0, 18, 0, 10, container.NewVBox(header, sep)), nil, nil, nil,
		container.NewStack(padded(0, 8, 0, 8, t.list), container.NewCenter(t.placeholder)))
	return t
}

func (t *TrackList) Object() fyne.CanvasObject { return t.root }

// SetTracks replaces the rows.
func (t *TrackList) SetTracks(tracks []ipc.TrackInfo) {
	t.Tracks = tracks
	if t.selected >= len(tracks) {
		t.selected = -1
	}
	t.placeholder.Hidden = len(tracks) > 0
	t.placeholder.Refresh()
	t.list.Refresh()
}

// SetPlaceholder sets the text of the empty list.
func (t *TrackList) SetPlaceholder(text string) {
	t.placeholder.SetText(text)
}

func (t *TrackList) Refresh() { t.list.Refresh() }

// ScrollTo scrolls row i into view.
func (t *TrackList) ScrollTo(i int) {
	if i >= 0 && i < len(t.Tracks) {
		t.list.ScrollTo(i)
	}
}

func (t *TrackList) Selected() int { return t.selected }

func (t *TrackList) selectRow(i int) {
	t.selected = i
	t.list.Refresh()
}

func (t *TrackList) showMenu(i int, pos fyne.Position) {
	if t.Menu == nil {
		return
	}
	t.selectRow(i)
	menu := t.Menu(i)
	if menu == nil || len(menu.Items) == 0 {
		return
	}
	widget.ShowPopUpMenuAtPosition(menu, t.app.win.Canvas(), pos)
}

// rowLayout places the columns of a track row: number, title, album,
// badge, duration, menu.
type rowLayout struct{}

const (
	colNum   = 40
	colBadge = 40
	colTime  = 64
	colMore  = 36
)

func (r *rowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objects {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(colNum+colBadge+colTime+colMore+120, h)
}

func (r *rowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 6 {
		return
	}
	flex := size.Width - colNum - colBadge - colTime - colMore
	titleW, albumW := flex*0.6, flex*0.4
	if size.Width < 640 {
		titleW, albumW = flex, 0
	}
	x := float32(0)
	place := func(o fyne.CanvasObject, w float32) {
		o.Move(fyne.NewPos(x, (size.Height-o.MinSize().Height)/2))
		o.Resize(fyne.NewSize(w, o.MinSize().Height))
		x += w
	}
	place(objects[0], colNum)
	place(objects[1], titleW)
	if albumW == 0 {
		objects[2].Hide()
	} else {
		objects[2].Show()
	}
	place(objects[2], albumW)
	place(objects[3], colBadge)
	place(objects[4], colTime)
	place(objects[5], colMore)
}

// trackRow is one row of a TrackList.
type trackRow struct {
	widget.BaseWidget
	list    *TrackList
	index   int
	hovered bool

	bg      *canvas.Rectangle
	num     *widget.Label
	playing *widget.Icon
	thumb   *artView
	title   *widget.Label
	sub     *widget.Label
	album   *widget.Label
	badge   *widget.Label
	dur     *widget.Label
	more    *widget.Button
	hover   hoverPrefetch
}

var (
	_ fyne.Tappable          = (*trackRow)(nil)
	_ fyne.DoubleTappable    = (*trackRow)(nil)
	_ fyne.SecondaryTappable = (*trackRow)(nil)
	_ desktop.Hoverable      = (*trackRow)(nil)
)

func newTrackRow(list *TrackList) *trackRow {
	r := &trackRow{list: list, index: -1}
	r.bg = canvas.NewRectangle(color.Transparent)
	r.bg.CornerRadius = 6
	r.num = dimLabel("")
	r.num.Alignment = fyne.TextAlignTrailing
	r.playing = widget.NewIcon(theme.MediaPlayIcon())
	r.playing.Hide()
	r.thumb = newArtView(list.app, 40, 6)
	r.thumb.image.FillMode = canvas.ImageFillCover
	r.title = trunc(label(""))
	r.sub = trunc(captionLabel(""))
	r.album = trunc(dimLabel(""))
	r.badge = captionLabel("")
	r.badge.Alignment = fyne.TextAlignCenter
	r.dur = dimLabel("")
	r.dur.Alignment = fyne.TextAlignTrailing
	r.more = iconButton(theme.MoreHorizontalIcon(), func() {
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(r.more)
		r.list.showMenu(r.index, pos.Add(fyne.NewPos(0, r.more.Size().Height)))
	})
	r.ExtendBaseWidget(r)
	return r
}

func (r *trackRow) CreateRenderer() fyne.WidgetRenderer {
	text := container.NewVBox(spacer(), container.New(&tightVBox{}, r.title, r.sub), spacer())
	titleBox := container.NewBorder(nil, nil, container.NewVBox(spacer(), padded(0, 10, 0, 0, r.thumb.Object()), spacer()), nil, text)
	numBox := container.NewStack(r.num, container.NewCenter(r.playing))
	row := container.New(&rowLayout{}, numBox, titleBox, r.album, r.badge, r.dur, r.more)
	return widget.NewSimpleRenderer(container.NewStack(r.bg, padded(0, 4, 0, 0, row)))
}

func (r *trackRow) bind(i int) {
	r.index = i
	if i < 0 || i >= len(r.list.Tracks) {
		return
	}
	t := r.list.Tracks[i]
	title, sub := trackTitle(t)
	current := r.list.IsCurrent != nil && r.list.IsCurrent(i)
	r.num.SetText(fmt.Sprint(i + 1))
	if artKey(t) != r.thumb.current || artKey(t) == "" {
		r.thumb.Load(t)
	}
	r.num.Hidden = current
	r.playing.Hidden = !current
	r.title.Text = title
	if current {
		r.title.Importance = widget.HighImportance
		r.title.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		r.title.Importance = widget.MediumImportance
		r.title.TextStyle = fyne.TextStyle{}
	}
	if t.Unplayable {
		r.title.Importance = widget.LowImportance
	}
	r.title.Refresh()
	r.sub.SetText(sub)
	r.sub.Hidden = sub == ""
	r.album.SetText(t.Album)
	badge := ""
	if r.list.Badge != nil {
		badge = r.list.Badge(i)
	}
	r.badge.SetText(badge)
	switch {
	case t.Stream && t.DurationSecs == 0:
		r.dur.SetText("LIVE")
	default:
		r.dur.SetText(fmtDuration(float64(t.DurationSecs)))
	}
	r.more.Hidden = r.list.Menu == nil
	r.updateBG()
}

func (r *trackRow) updateBG() {
	switch {
	case r.index == r.list.selected:
		r.bg.FillColor = theme.Color(theme.ColorNameSelection)
	case r.hovered:
		r.bg.FillColor = theme.Color(theme.ColorNameHover)
	default:
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
}

func (r *trackRow) Tapped(*fyne.PointEvent) { r.list.selectRow(r.index) }

func (r *trackRow) DoubleTapped(*fyne.PointEvent) {
	r.list.selectRow(r.index)
	if r.list.OnPlay != nil && r.index >= 0 {
		r.list.OnPlay(r.index)
	}
}

func (r *trackRow) TappedSecondary(ev *fyne.PointEvent) {
	r.list.showMenu(r.index, ev.AbsolutePosition)
}

func (r *trackRow) MouseIn(*desktop.MouseEvent) {
	r.hover.url = ""
	if r.index >= 0 && r.index < len(r.list.Tracks) {
		r.hover.url = r.list.Tracks[r.index].Path
	}
	r.hover.in()
	r.hovered = true
	r.updateBG()
}

func (r *trackRow) MouseMoved(*desktop.MouseEvent) {}

func (r *trackRow) MouseOut() {
	r.hover.out()
	r.hovered = false
	r.updateBG()
}

// tightVBox stacks objects without the theme padding between them.
type tightVBox struct{}

func (t *tightVBox) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		size.Width = max(size.Width, m.Width)
		size.Height += m.Height - 2*theme.Padding()
	}
	return size.Add(fyne.NewSize(0, 2*theme.Padding()))
}

func (t *tightVBox) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := make([]fyne.CanvasObject, 0, len(objects))
	total := float32(0)
	for _, o := range objects {
		if o.Visible() {
			visible = append(visible, o)
			total += o.MinSize().Height - 2*theme.Padding()
		}
	}
	y := (size.Height - total - 2*theme.Padding()) / 2
	for _, o := range visible {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h - 2*theme.Padding()
	}
}

// navItem is a sidebar entry with an icon, a label and a selected state.
type navItem struct {
	widget.BaseWidget
	key      string
	icon     *widget.Icon
	text     *widget.Label
	bg       *canvas.Rectangle
	bar      *canvas.Rectangle
	selected bool
	hovered  bool
	onTap    func()
}

func newNavItem(key string, res fyne.Resource, text string, onTap func()) *navItem {
	n := &navItem{key: key, onTap: onTap}
	n.icon = widget.NewIcon(res)
	n.text = trunc(label(text))
	n.bg = canvas.NewRectangle(color.Transparent)
	n.bg.CornerRadius = 8
	n.bar = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	n.bar.CornerRadius = 2
	n.bar.Hide()
	n.ExtendBaseWidget(n)
	return n
}

func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	row := container.NewBorder(nil, nil, padded(0, 0, 0, 6, n.icon), nil, n.text)
	bar := container.NewBorder(nil, nil, fixed(3, 0, padded(9, 0, 9, 0, n.bar)), nil)
	return widget.NewSimpleRenderer(container.NewStack(n.bg, bar, padded(0, 6, 0, 6, row)))
}

// setCompact hides the text, leaving the icon.
func (n *navItem) setCompact(compact bool) {
	if compact {
		n.text.Hide()
	} else {
		n.text.Show()
	}
}

func (n *navItem) setSelected(v bool) {
	n.selected = v
	n.refreshStyle()
}

func (n *navItem) refreshStyle() {
	n.bar.FillColor = theme.Color(theme.ColorNamePrimary)
	n.bar.Hidden = !n.selected
	switch {
	case n.selected:
		n.bg.FillColor = theme.Color(theme.ColorNameSelection)
		n.text.TextStyle = fyne.TextStyle{Bold: true}
	case n.hovered:
		n.bg.FillColor = theme.Color(theme.ColorNameHover)
		n.text.TextStyle = fyne.TextStyle{}
	default:
		n.bg.FillColor = color.Transparent
		n.text.TextStyle = fyne.TextStyle{}
	}
	n.bg.Refresh()
	n.bar.Refresh()
	n.text.Refresh()
}

func (n *navItem) Tapped(*fyne.PointEvent) {
	if n.onTap != nil {
		n.onTap()
	}
}

func (n *navItem) MouseIn(*desktop.MouseEvent)    { n.hovered = true; n.refreshStyle() }
func (n *navItem) MouseMoved(*desktop.MouseEvent) {}
func (n *navItem) MouseOut()                      { n.hovered = false; n.refreshStyle() }

func (n *navItem) Cursor() desktop.Cursor { return desktop.PointerCursor }

// sectionTitle is a small uppercase sidebar heading.
func sectionTitle(text string) fyne.CanvasObject {
	l := captionLabel(strings.ToUpper(text))
	l.TextStyle = fyne.TextStyle{Bold: true}
	return padded(8, 0, 0, 8, l)
}

// spacer returns an expanding spacer for box layouts.
func spacer() fyne.CanvasObject { return layout.NewSpacer() }

// toggleIcons and toggleOn remember the plain icon and the state of each
// toggle button. They are used on the UI thread only.
var (
	toggleIcons = map[*widget.Button]fyne.Resource{}
	toggleOn    = map[*widget.Button]bool{}
)

// newToggleButton is an icon button whose icon takes the accent color while
// it is active; see setActive.
func newToggleButton(res fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon("", res, tapped)
	b.Importance = widget.LowImportance
	toggleIcons[b] = res
	return b
}

// setToggleIcon changes the plain icon of a toggle button.
func setToggleIcon(b *widget.Button, res fyne.Resource) {
	if toggleIcons[b] == res {
		return
	}
	toggleIcons[b] = res
	applyToggle(b)
}

func setActive(b *widget.Button, v bool) {
	if on, ok := toggleOn[b]; ok && on == v {
		return
	}
	toggleOn[b] = v
	applyToggle(b)
}

func applyToggle(b *widget.Button) {
	res := toggleIcons[b]
	if toggleOn[b] {
		res = theme.NewPrimaryThemedResource(res)
	}
	b.SetIcon(res)
}

// containerBorderRight puts right at the right edge of center.
func containerBorderRight(center, right fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, nil, right, center)
}
