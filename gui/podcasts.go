package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/resolve"
)

// podcastCard opens a show; its play button plays the latest episode.
func (a *App) podcastCard(s discover.Podcast) fyne.CanvasObject {
	c := newCard(a, s.Title, s.Author, s.Artwork, false,
		func() { a.push(newPodcastPage(a, s)) },
		func() { a.playLatestEpisode(s) })
	c.art.SetPlaceholder(iconPodcast)
	return c
}

func (a *App) podcastEpisodes(ctx context.Context, s discover.Podcast) ([]ipc.TrackInfo, error) {
	tracks, err := resolve.Feed(ctx, s.FeedURL)
	if err != nil {
		return nil, err
	}
	out := make([]ipc.TrackInfo, 0, len(tracks))
	for _, t := range tracks {
		info := trackInfo(t)
		if info.AlbumArtURL == "" {
			info.AlbumArtURL = s.Artwork
		}
		if info.Album == "" {
			info.Album = s.Title
		}
		if info.Artist == "" {
			info.Artist = s.Author
		}
		out = append(out, info)
	}
	return out, nil
}

func (a *App) playLatestEpisode(s discover.Podcast) {
	a.toast(fmt.Sprintf(T("Loading %s…"), s.Title))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		episodes, err := a.podcastEpisodes(ctx, s)
		if err != nil || len(episodes) == 0 {
			a.toastError(fmt.Errorf("%s: %v", s.Title, err))
			return
		}
		runOnUI(func() { a.playTrack(episodes[0]) })
	}()
}

// newPodcastPage lists the episodes of a show.
func newPodcastPage(a *App, s discover.Podcast) fyne.CanvasObject {
	sub := s.Author
	if s.Genre != "" {
		sub += " • " + genreName(s.Genre)
	}
	h := newHero(a, strings.ToUpper(T("Podcast")), s.Title, sub, s.Artwork, false, a.back)
	list := NewTrackList(a)
	list.SetPlaceholder(T("Loading…"))
	list.OnPlay = func(i int) { a.playTrack(list.Tracks[i]) }
	list.Menu = func(i int) *fyne.Menu { return fyne.NewMenu("", a.trackMenu(list.Tracks[i])...) }
	list.IsCurrent = func(i int) bool {
		st := a.state
		return st.Track != nil && list.Tracks[i].Path == st.Track.Path
	}
	h.buttons.Add(primaryButton(T("Latest episode"), theme.MediaPlayIcon(), func() {
		if len(list.Tracks) > 0 {
			a.playTrack(list.Tracks[0])
		}
	}))
	h.buttons.Add(flatButton(T("Add all to queue"), theme.ContentAddIcon(), func() { a.addTracks(list.Tracks, false) }))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		episodes, err := a.podcastEpisodes(ctx, s)
		runOnUI(func() {
			if err != nil {
				list.SetPlaceholder(err.Error())
				list.SetTracks(nil)
				return
			}
			h.sub.SetText(sub + " • " + fmt.Sprintf(T("%d tracks"), len(episodes)))
			list.SetTracks(episodes)
		})
	}()
	return container.NewBorder(h.root, nil, nil, nil, list.Object())
}

// podcastsView shows the podcast charts of the country by category, and
// podcast search.
type podcastsView struct {
	app    *App
	root   fyne.CanvasObject
	grid   *fyne.Container
	title  *widget.Label
	chips  []*chip
	genre  string
	where  *widget.Button
	loaded bool
	seq    int
}

func newPodcastsView(a *App) *podcastsView {
	p := &podcastsView{app: a}
	p.where = flatButton("", iconGlobe, func() { a.showCountryMenu(p.where) })
	search := widget.NewEntry()
	search.SetPlaceHolder(T("Search podcasts…"))
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	search.OnSubmitted = func(q string) { p.search(q) }

	var chips []fyne.CanvasObject
	add := func(id, name string) {
		var c *chip
		c = newChip(genreName(name), func() {
			for _, o := range p.chips {
				o.SetSelected(o == c)
			}
			p.genre = id
			p.reload()
		})
		p.chips = append(p.chips, c)
		chips = append(chips, c)
	}
	add("", "All")
	for _, g := range discover.PodcastGenres {
		add(g.ID, g.Name)
	}
	p.chips[0].SetSelected(true)
	p.title = label("")
	p.title.SizeName = theme.SizeNameSubHeadingText
	p.title.TextStyle = fyne.TextStyle{Bold: true}
	p.grid = container.NewGridWrap(fyne.NewSize(cardW, 236))
	header := container.NewVBox(
		container.NewBorder(nil, nil, nil, p.where, heading(T("Podcasts"))),
		search,
		sectionHeading(T("Categories")),
		chipRow(chips...),
		padded(10, 0, 0, 0, p.title),
	)
	p.root = scrollPage(container.NewVBox(padded(20, 0, 0, 0, header), p.grid))
	return p
}

func (p *podcastsView) Object() fyne.CanvasObject { return p.root }

func (p *podcastsView) Shown() {
	if !p.loaded {
		p.loaded = true
		p.reload()
	}
}

func (p *podcastsView) reload() {
	country := p.app.country()
	p.where.SetText(countryName(country))
	p.title.SetText(fmt.Sprintf(T("Top podcasts in %s"), countryName(country)))
	p.load(func(ctx context.Context) ([]discover.Podcast, error) {
		return discover.TopPodcasts(ctx, country, p.genre)
	})
}

func (p *podcastsView) search(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		p.reload()
		return
	}
	p.title.SetText("“" + q + "”")
	country := p.app.country()
	p.load(func(ctx context.Context) ([]discover.Podcast, error) {
		return discover.SearchPodcasts(ctx, q, country)
	})
}

func (p *podcastsView) load(fetch func(ctx context.Context) ([]discover.Podcast, error)) {
	p.seq++
	seq := p.seq
	p.grid.Objects = []fyne.CanvasObject{loadingBox(T("Loading…"))}
	p.grid.Refresh()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		shows, err := fetch(ctx)
		runOnUI(func() {
			if seq != p.seq {
				return
			}
			p.grid.Objects = nil
			if err != nil {
				p.app.toastError(err)
			}
			for _, s := range shows {
				p.grid.Add(p.app.podcastCard(s))
			}
			p.grid.Refresh()
		})
	}()
}

// genreName translates a podcast category; "History" means Storia here,
// not the Cronologia page.
func genreName(name string) string {
	if uiLang == "it" && name == "History" {
		return "Storia"
	}
	return T(name)
}
