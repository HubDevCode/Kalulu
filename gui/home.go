package gui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/external/radio"
)

// homeView is the start page: recommendations, charts, moods, stations and
// podcasts of the chosen country.
type homeView struct {
	scroll  *container.Scroll
	pending []pendingSection
	app     *App
	root    *fyne.Container
	body    *fyne.Container
	loaded  bool
	seq     int
}

func newHomeView(a *App) *homeView {
	h := &homeView{app: a}
	h.body = container.NewVBox()
	// The header runs to the edges; the sections keep the page margins.
	h.scroll = container.NewVScroll(padded(0, 0, 24, 0, h.body))
	h.scroll.OnScrolled = func(fyne.Position) { h.loadVisible() }
	h.root = container.NewStack(h.scroll)
	return h
}

func (h *homeView) Object() fyne.CanvasObject { return h.root }

func (h *homeView) Shown() {
	if !h.loaded {
		h.loaded = true
		h.reload()
	}
}

func greeting() string {
	switch hour := time.Now().Hour(); {
	case hour >= 5 && hour < 12:
		return T("Good morning")
	case hour >= 12 && hour < 18:
		return T("Good afternoon")
	case hour >= 18 && hour < 23:
		return T("Good evening")
	}
	return T("Good night")
}

// section is a placeholder that a background load fills in.
func (h *homeView) section(load func(ctx context.Context) (fyne.CanvasObject, error)) fyne.CanvasObject {
	box := container.NewStack(loadingBox(T("Loading…")))
	seq := h.seq
	start := func() { go h.runSection(box, seq, load) }
	h.pending = append(h.pending, pendingSection{box: box, start: start})
	return box
}

// pendingSection is a Home section that loads once it nears the view.
type pendingSection struct {
	box    *fyne.Container
	anchor fyne.CanvasObject // the wrapper placed in the page
	start  func()
}

// homeSlots bounds the sections that load at the same time.
var homeSlots = make(chan struct{}, 2)

// loadVisible starts the sections that are on screen or close below it.
func (h *homeView) loadVisible() {
	if h.scroll == nil {
		return
	}
	limit := h.scroll.Offset.Y + h.scroll.Size().Height + 700
	left := h.pending[:0]
	for _, p := range h.pending {
		anchor := p.anchor
		if anchor == nil {
			anchor = p.box
		}
		if anchor.Position().Y <= limit {
			p.start()
			continue
		}
		left = append(left, p)
	}
	h.pending = left
}

func (h *homeView) runSection(box *fyne.Container, seq int, load func(ctx context.Context) (fyne.CanvasObject, error)) {
	homeSlots <- struct{}{}
	defer func() { <-homeSlots }()
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		obj, err := load(ctx)
		runOnUI(func() {
			if seq != h.seq {
				return
			}
			if err != nil {
				retry := flatButton(T("Retry"), theme.ViewRefreshIcon(), h.reload)
				msg := dimLabel(err.Error())
				msg.Wrapping = fyne.TextWrapWord
				box.Objects = []fyne.CanvasObject{container.NewVBox(msg, container.NewHBox(retry))}
			} else if obj == nil {
				box.Objects = nil
			} else {
				box.Objects = []fyne.CanvasObject{obj}
			}
			box.Refresh()
			h.body.Refresh()
			h.loadVisible()
		})
	}()
}

func (h *homeView) reload() {
	h.loaded = true
	h.seq++
	a := h.app
	country := a.country()

	title := label(greeting())
	title.SizeName = theme.SizeNameHeadingText
	title.TextStyle = fyne.TextStyle{Bold: true}
	sub := dimLabel(fmt.Sprintf(T("Music for you in %s"), countryName(country)))
	var where *widget.Button
	where = flatButton(countryName(country), iconGlobe, func() { a.showCountryMenu(where) })
	glow := tGradient(func(p Palette) color.Color { return mix(p.Accent, p.BG, 0.55) }, roleBG)
	header := container.NewStack(glow, padded(26, 48, 18, 48,
		container.NewBorder(nil, nil, nil, container.NewVBox(where), container.NewVBox(title, sub))))

	loaders := map[string]func(ctx context.Context) (fyne.CanvasObject, error){
		"foryou": a.forYou,
		"ytm": func(ctx context.Context) (fyne.CanvasObject, error) {
			shelves, err := a.ytm.Home(ctx, 2)
			if err != nil {
				return nil, fmt.Errorf(T("Could not reach YouTube Music: %s"), err)
			}
			box := container.NewVBox()
			for _, s := range shelves {
				box.Add(a.shelfObject(s))
			}
			return box, nil
		},
		"charts": func(ctx context.Context) (fyne.CanvasObject, error) {
			shelves, err := a.ytm.Charts(ctx)
			if err != nil {
				return nil, err
			}
			box := container.NewVBox()
			for _, s := range shelves {
				if len(s.Items) > 0 {
					s.Title = T("Charts") + " · " + s.Title
					box.Add(a.shelfObject(s))
				}
			}
			return box, nil
		},
		"moods":   func(ctx context.Context) (fyne.CanvasObject, error) { return a.moodsGrid(ctx, 12) },
		"local":   a.localSection,
		"servers": a.serversSection,
		"radio": func(ctx context.Context) (fyne.CanvasObject, error) {
			stations, err := radio.Stations(radio.StationQuery{CountryCode: country, Order: radio.SortVotes, Limit: 30})
			if err != nil {
				return nil, err
			}
			var cards []fyne.CanvasObject
			for _, st := range stations {
				cards = append(cards, a.stationCard(st))
			}
			more := flatButton(T("Show all"), nil, func() { a.show("radio") })
			return shelf(fmt.Sprintf(T("Popular stations in %s"), countryName(country)), newPager(cards, cardW, 1, false), more), nil
		},
		"podcasts": func(ctx context.Context) (fyne.CanvasObject, error) {
			shows, err := discover.TopPodcasts(ctx, country, "")
			if err != nil {
				return nil, err
			}
			var cards []fyne.CanvasObject
			for _, s := range shows {
				cards = append(cards, a.podcastCard(s))
			}
			more := flatButton(T("Show all"), nil, func() { a.show("podcasts") })
			return shelf(fmt.Sprintf(T("Top podcasts in %s"), countryName(country)), newPager(cards, cardW, 1, false), more), nil
		},
	}
	h.pending = nil
	h.body.Objects = []fyne.CanvasObject{header}
	for _, key := range a.homeOrder() {
		if load, ok := loaders[key]; ok {
			w := padded(0, 48, 0, 48, h.section(load))
			h.pending[len(h.pending)-1].anchor = w
			h.body.Add(w)
		}
	}
	h.body.Add(fixed(0, 20))
	h.body.Refresh()
	// Sections are placed now; load the ones in view, the rest on scroll.
	go func() {
		time.Sleep(50 * time.Millisecond)
		runOnUI(h.loadVisible)
	}()
	h.body.Refresh()
}

// moodsGrid shows the genre and mood tiles; limit 0 shows all of them.
func (a *App) moodsGrid(ctx context.Context, limit int) (fyne.CanvasObject, error) {
	shelves, err := a.ytm.Moods(ctx)
	if err != nil {
		return nil, err
	}
	var items []discover.Item
	for _, s := range shelves {
		items = append(items, s.Items...)
	}
	grid := container.NewGridWrap(fyne.NewSize(176, 86))
	all := func(n int) {
		grid.Objects = nil
		for i, it := range items {
			if n > 0 && i >= n {
				break
			}
			it := it
			grid.Add(moodTile(it.Title, func() { a.openItem(it) }))
		}
		grid.Refresh()
	}
	all(limit)
	var more *widget.Button
	more = flatButton(T("Show all"), nil, func() {
		all(0)
		more.Hide()
	})
	if limit == 0 || len(items) <= limit {
		more.Hide()
	}
	head := label(T("Moods & genres"))
	head.SizeName = theme.SizeNameSubHeadingText
	head.TextStyle = fyne.TextStyle{Bold: true}
	return padded(10, 0, 6, 0, container.NewVBox(container.NewBorder(nil, nil, nil, more, head), grid)), nil
}

// localSection shows the saved playlists of the local library.
func (a *App) localSection(ctx context.Context) (fyne.CanvasObject, error) {
	r, err := a.call("provider.playlists", P{"provider": "local", "limit": 60})
	if err != nil || len(r.Response.Playlists) == 0 {
		return nil, nil
	}
	var cards []fyne.CanvasObject
	for _, pl := range r.Response.Playlists {
		pl := pl
		sub := ""
		if pl.TrackCount > 0 {
			sub = fmt.Sprintf(T("%d tracks"), pl.TrackCount)
		}
		c := newCard(a, pl.Name, sub, "", false,
			func() { a.show("provider:local") },
			func() { a.do("provider.load", P{"provider": "local", "playlist": pl.ID}, nil) })
		c.art.SetPlaceholder(iconQueue)
		cards = append(cards, c)
	}
	return shelf(T("Your library"), newPager(cards, cardW, 1, false)), nil
}

// serversSection shows the playlists of each configured music server, such
// as Navidrome, Jellyfin or Plex. Services that are not set up never appear.
func (a *App) serversSection(ctx context.Context) (fyne.CanvasObject, error) {
	skip := map[string]bool{"local": true, "radio": true, "podcast": true, "podcasts": true, "cliamp": true}
	hidden := a.prefSet(prefHiddenSrc)
	box := container.NewVBox()
	for _, p := range a.Providers() {
		if skip[p.Key] || hidden[p.Key] || strings.HasPrefix(p.Key, "yt") {
			continue
		}
		r, err := a.call("provider.playlists", P{"provider": p.Key, "limit": 30})
		if err != nil || len(r.Response.Playlists) == 0 {
			continue
		}
		var cards []fyne.CanvasObject
		key := p.Key
		for _, pl := range r.Response.Playlists {
			pl := pl
			c := newCard(a, pl.Name, p.Name, "", false,
				func() { a.show("provider:" + key) },
				func() { a.do("provider.load", P{"provider": key, "playlist": pl.ID}, nil) })
			c.art.SetPlaceholder(providerIcon(key))
			cards = append(cards, c)
		}
		box.Add(shelf(p.Name, newPager(cards, cardW, 1, false)))
	}
	if len(box.Objects) == 0 {
		return nil, nil
	}
	return box, nil
}
