package gui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/ipc"
)

// searchView searches YouTube Music and every searchable source, with
// suggestions while typing.
type searchView struct {
	app     *App
	root    fyne.CanvasObject
	entry   *widget.Entry
	body    *fyne.Container
	keys    map[string]string // provider name to key
	seq     int
	sugSeq  atomic.Int64
	popup   fyne.CanvasObject // the suggestions panel, over the results
	popBox  *fyne.Container
	browse  fyne.CanvasObject
	lastRun string
}

func newSearchView(a *App) *searchView {
	s := &searchView{app: a, keys: map[string]string{}}
	s.entry = widget.NewEntry()
	s.entry.SetPlaceHolder(T("What do you want to listen to?"))
	s.entry.OnSubmitted = func(q string) { s.run(q) }
	s.entry.OnChanged = s.suggest
	s.popBox = container.NewVBox()
	s.body = container.NewVBox()
	// The search bar is a field of the accent color, as the mobile design,
	// with Cancel beside it.
	barBG := tRect(roleAccent)
	barBG.CornerRadius = 10
	glass := container.NewThemeOverride(container.NewBorder(nil, nil,
		padded(0, 0, 0, 10, widget.NewIcon(theme.SearchIcon())), nil, s.entry), searchBarTheme{})
	bar := fixed(0, 44, container.NewStack(barBG, glass))
	cancel := flatButton(T("Cancel"), nil, func() {
		s.entry.SetText("")
		s.lastRun = ""
		s.hideSuggestions()
		s.showBrowse()
	})
	top := padded(18, 24, 14, 24, container.NewBorder(nil, nil, nil, padded(0, 0, 0, 8, cancel), bar))
	// The suggestions float over the results inside the page, not in a
	// Fyne pop-up: a pop-up takes the keyboard focus from the entry, and
	// the letters typed next went to the shortcuts.
	s.popup = padded(0, 24, 0, 24, tPanel(roleOverlay, 10, padded(4, 4, 4, 4, s.popBox)))
	s.popup.Hide()
	// Browse and the results sit on a sheet with a grab handle.
	sheetBG := tRect(roleSheet)
	sheetBG.TopLeftCornerRadius = 22
	sheetBG.TopRightCornerRadius = 22
	handle := tRect(roleBorder)
	handle.CornerRadius = 3
	grip := padded(10, 0, 4, 0, container.NewCenter(fixed(44, 5, handle)))
	sheet := container.NewStack(sheetBG, container.NewBorder(grip, nil, nil, nil, scrollPage(s.body)))
	results := container.NewStack(padded(0, 16, 0, 16, sheet), container.NewVBox(s.popup))
	s.root = container.NewBorder(top, nil, nil, nil, results)
	return s
}

func (s *searchView) Object() fyne.CanvasObject { return s.root }

func (s *searchView) Shown() {
	s.app.win.Canvas().Focus(s.entry)
	if strings.TrimSpace(s.entry.Text) == "" {
		s.showBrowse()
	}
}

func (s *searchView) setProviders(list []ipc.ProviderInfo) {
	for _, p := range list {
		if p.Searchable {
			s.keys[p.Name] = p.Key
		}
	}
}

// showBrowse is the empty state: recent searches and the genre tiles.
func (s *searchView) showBrowse() {
	s.seq++
	box := container.NewVBox()
	if recent := s.app.recentSearches(); len(recent) > 0 {
		var chips []fyne.CanvasObject
		for _, q := range recent {
			q := q
			chips = append(chips, newChip(q, func() {
				s.entry.SetText(q)
				s.run(q)
			}))
		}
		box.Add(sectionHeading(T("Recent searches")))
		box.Add(chipRow(chips...))
	}
	if s.browse == nil {
		holder := container.NewStack(loadingBox(T("Loading…")))
		s.browse = holder
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			obj, err := s.app.categoryGrid(ctx)
			runOnUI(func() {
				if err != nil {
					s.browse = nil
					holder.Objects = []fyne.CanvasObject{dimLabel(err.Error())}
				} else {
					holder.Objects = []fyne.CanvasObject{obj}
				}
				holder.Refresh()
				s.body.Refresh()
			})
		}()
	}
	box.Add(s.browse)
	s.body.Objects = []fyne.CanvasObject{box}
	s.body.Refresh()
}

// suggest shows search completions a moment after the user stops typing.
func (s *searchView) suggest(text string) {
	seq := s.sugSeq.Add(1)
	text = strings.TrimSpace(text)
	if text == "" {
		s.hideSuggestions()
		s.showBrowse()
		return
	}
	go func() {
		time.Sleep(220 * time.Millisecond)
		if s.sugSeq.Load() != seq {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, err := s.app.ytm.Suggest(ctx, text)
		if err != nil || len(list) == 0 {
			return
		}
		runOnUI(func() {
			if seq != s.sugSeq.Load() || s.lastRun == text {
				return
			}
			s.showSuggestions(list)
		})
	}()
}

func (s *searchView) showSuggestions(list []string) {
	s.popBox.Objects = nil
	for _, q := range list[:min(8, len(list))] {
		q := q
		b := widget.NewButtonWithIcon(q, theme.SearchIcon(), func() {
			s.hideSuggestions()
			s.entry.SetText(q)
			s.run(q)
		})
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		s.popBox.Add(b)
	}
	s.popBox.Refresh()
	s.popup.Show()
}

func (s *searchView) hideSuggestions() {
	s.popup.Hide()
}

func (s *searchView) run(q string) {
	q = strings.TrimSpace(q)
	s.hideSuggestions()
	s.sugSeq.Add(1)
	if q == "" {
		s.showBrowse()
		return
	}
	s.lastRun = q
	s.app.rememberSearch(q)
	s.seq++
	seq := s.seq
	s.body.Objects = []fyne.CanvasObject{loadingBox(T("Searching…"))}
	s.body.Refresh()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var (
			res    discover.SearchResult
			resErr error
			others []ipc.TrackInfo
			wg     sync.WaitGroup
			mu     sync.Mutex
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, resErr = s.app.ytm.Search(ctx, q)
		}()
		for name, key := range s.keys {
			if strings.HasPrefix(key, "yt") {
				continue
			}
			wg.Add(1)
			go func(name, key string) {
				defer wg.Done()
				r, err := s.app.call("provider.search", P{"provider": key, "query": q, "limit": 12})
				if err != nil {
					return
				}
				mu.Lock()
				for _, t := range r.Response.Tracks {
					if t.Station == "" && t.Album == "" {
						t.Album = name
					}
					others = append(others, t)
				}
				mu.Unlock()
			}(name, key)
		}
		wg.Wait()
		runOnUI(func() {
			if seq != s.seq {
				return
			}
			s.showResults(q, res, resErr, others)
		})
	}()
}

func (s *searchView) showResults(q string, res discover.SearchResult, err error, others []ipc.TrackInfo) {
	a := s.app
	box := container.NewVBox()
	if err != nil {
		box.Add(dimLabel(fmt.Sprintf(T("Could not reach YouTube Music: %s"), err)))
	}
	// Top result next to the first songs.
	var top *discover.Item
	for i := range res.Top {
		if res.Top[i].Kind != discover.KindMood {
			top = &res.Top[i]
			break
		}
	}
	songs := res.Songs
	if len(songs) == 0 {
		for _, it := range res.Top {
			if it.Playable() {
				songs = append(songs, it)
			}
		}
	}
	if top != nil || len(songs) > 0 {
		var left fyne.CanvasObject = spacer()
		if top != nil {
			left = s.topCard(*top)
		}
		right := container.NewVBox()
		for _, it := range songs[:min(4, len(songs))] {
			right.Add(a.itemTile(it))
		}
		row := container.New(&splitLayout{left: 0.42, gap: 18}, left, container.NewVBox(sectionHeading(T("Songs")), right))
		box.Add(container.NewVBox(sectionHeading(T("Top result")), row))
	}
	add := func(title string, items []discover.Item) {
		if len(items) == 0 {
			return
		}
		box.Add(a.shelfObject(discover.Shelf{Title: title, Items: items}))
	}
	if len(songs) > 4 {
		add(T("Songs"), songs[4:])
	}
	add(T("Artists"), res.Artists)
	add(T("Albums"), res.Albums)
	add(T("Videos"), res.Videos)
	add(T("Playlists"), res.Playlists)
	if len(others) > 0 {
		var tiles []fyne.CanvasObject
		for _, t := range others {
			t := t
			title, sub := trackTitle(t)
			if t.Album != "" && sub == "" {
				sub = t.Album
			}
			tl := newTile(a, title, sub, t.AlbumArtURL, func() { a.playTrack(t) })
			tl.hover.url = t.Path
			tl.art.SetPlaceholder(iconLibrary)
			tl.onMenu = func(pos fyne.Position) {
				widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", a.trackMenu(t)...), a.win.Canvas(), pos)
			}
			tiles = append(tiles, tl)
		}
		box.Add(shelf(T("Other sources"), newPager(tiles, 300, 3, true)))
	}
	if len(box.Objects) == 0 {
		box.Add(padded(40, 0, 0, 0, container.NewCenter(dimLabel(fmt.Sprintf(T("No results for “%s”"), q)))))
	}
	box.Add(fixed(0, 20))
	s.body.Objects = []fyne.CanvasObject{box}
	s.body.Refresh()
}

// topCard is the large card of the best match.
func (s *searchView) topCard(it discover.Item) fyne.CanvasObject {
	a := s.app
	radius := float32(10)
	if it.Kind == discover.KindArtist {
		radius = 56
	}
	art := newArtView(a, 112, radius)
	art.image.FillMode = canvas.ImageFillCover
	art.LoadURL(it.Thumb)
	art.onTap = func() { a.openItem(it) }
	title := label(it.Title)
	title.SizeName = theme.SizeNameHeadingText
	title.TextStyle = fyne.TextStyle{Bold: true}
	sub := it.Artist
	if kl := kindLabel(it.Kind); kl != "" {
		sub = strings.TrimSpace(kl + " · " + it.Artist)
	} else if it.Kind == discover.KindSong {
		sub = T("Song") + " · " + it.Artist
	} else if it.Kind == discover.KindVideo {
		sub = T("Video") + " · " + it.Artist
	}
	kind := dimLabel(strings.Trim(sub, " ·"))
	play := newRoundButton(theme.MediaPlayIcon(), 44, func() { a.playItem(it) })
	content := container.NewVBox(container.NewHBox(art.Object()), trunc(title), trunc(kind))
	bg := tRect(roleCard)
	bg.CornerRadius = 14
	tap := newTapArea(func() { a.openItem(it) })
	return container.NewStack(bg, tap, padded(16, 16, 16, 16, container.NewBorder(nil, nil, nil,
		container.NewVBox(spacer(), play), content)))
}

// splitLayout puts two objects side by side, the left taking a share of
// the width.
type splitLayout struct {
	left float32
	gap  float32
}

func (l *splitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	a, b := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(a.Width+b.Width+l.gap, max(a.Height, b.Height))
}

func (l *splitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	lw := size.Width*l.left - l.gap/2
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(lw, size.Height))
	objects[1].Move(fyne.NewPos(lw+l.gap, 0))
	objects[1].Resize(fyne.NewSize(size.Width-lw-l.gap, size.Height))
}

// roleSheet fills the sheet under the search bar.
var roleSheet role = func(p Palette) color.Color { return mix(p.Surface, p.Surface2, 0.6) }

// searchBarTheme draws the entry flat on the accent bar, with text that
// stays readable on the accent of the moment.
type searchBarTheme struct{}

func (searchBarTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	base := fyne.CurrentApp().Settings().Theme()
	acc := currentPalette().Accent
	on := color.NRGBA{0x12, 0x10, 0x18, 0xff}
	if luminance(acc) < 0.45 {
		on = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	}
	switch n {
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder, theme.ColorNameHover, theme.ColorNameFocus:
		return color.Transparent
	case theme.ColorNameForeground, theme.ColorNamePrimary:
		return on
	case theme.ColorNamePlaceHolder:
		return withAlpha(on, 0xb0)
	case theme.ColorNameSelection:
		return withAlpha(on, 0x40)
	}
	return base.Color(n, v)
}

func (searchBarTheme) Font(s fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(s)
}

func (searchBarTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(n)
}

func (searchBarTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInputBorder {
		return 0
	}
	return fyne.CurrentApp().Settings().Theme().Size(n)
}
