package gui

import (
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/external/radio"
	"github.com/bjarneo/cliamp/ipc"
)

// radioTags are the genres offered as chips. They are Radio Browser tags.
var radioTags = []string{"pop", "rock", "news", "talk", "jazz", "classical", "dance", "electronic",
	"hits", "80s", "90s", "hip hop", "chillout", "lounge", "ambient", "country", "metal", "reggae",
	"latin", "soul", "blues", "folk", "sport", "oldies"}

var (
	radioFavOnce sync.Once
	radioFavs    *radio.Favorites
)

// favoriteStations is the cliamp radio favorites store, shared with the TUI.
func favoriteStations() *radio.Favorites {
	radioFavOnce.Do(func() { radioFavs = radio.LoadFavorites() })
	return radioFavs
}

func stationTrack(st radio.CatalogStation) ipc.TrackInfo {
	genre := st.Tags
	if i := strings.Index(genre, ","); i > 0 {
		genre = genre[:i]
	}
	return ipc.TrackInfo{
		Path: st.URL, Title: st.Name, Station: st.Name, Genre: genre,
		AlbumArtURL: st.Favicon, Stream: true, Realtime: true,
	}
}

func stationSubtitle(st radio.CatalogStation) string {
	var parts []string
	tags := strings.Split(st.Tags, ",")
	for _, t := range tags[:min(2, len(tags))] {
		if t = strings.TrimSpace(t); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 && st.Country != "" {
		parts = append(parts, st.Country)
	}
	return strings.Join(parts, " · ")
}

// stationCard plays a station on click.
func (a *App) stationCard(st radio.CatalogStation) fyne.CanvasObject {
	play := func() { a.playTrack(stationTrack(st)) }
	c := newCard(a, st.Name, stationSubtitle(st), st.Favicon, false, play, play)
	c.art.SetPlaceholder(iconRadio)
	c.art.image.FillMode = canvas.ImageFillContain
	c.onMenu = func(pos fyne.Position) {
		widget.ShowPopUpMenuAtPosition(a.stationMenu(st), a.win.Canvas(), pos)
	}
	return c
}

func (a *App) stationMenu(st radio.CatalogStation) *fyne.Menu {
	favs := favoriteStations()
	label, icon := T("Add to favorite stations"), iconHeartLine
	if favs != nil && favs.Contains(st.URL) {
		label, icon = T("Remove from favorite stations"), iconHeart
	}
	return fyne.NewMenu("",
		fyne.NewMenuItemWithIcon(T("Play"), theme.MediaPlayIcon(), func() { a.playTrack(stationTrack(st)) }),
		fyne.NewMenuItemWithIcon(T("Add to queue"), theme.ContentAddIcon(), func() { a.addTracks([]ipc.TrackInfo{stationTrack(st)}, false) }),
		fyne.NewMenuItemWithIcon(label, icon, func() {
			if favs == nil {
				return
			}
			if _, err := favs.Toggle(st); err != nil {
				a.toastError(err)
				return
			}
			if v, ok := a.views["radio"].(*radioView); ok {
				v.showFavorites()
			}
		}),
	)
}

// radioView browses the Radio Browser directory: the country's top and
// trending stations, genres, search and favorites.
type radioView struct {
	app      *App
	root     fyne.CanvasObject
	body     *fyne.Container
	results  *fyne.Container
	resTitle *widget.Label
	favs     *fyne.Container
	chips    []*chip
	search   *widget.Entry
	where    *widget.Button
	world    bool
	loaded   bool
	seq      int
}

func newRadioView(a *App) *radioView {
	r := &radioView{app: a}
	title := heading(T("Radio"))
	r.where = flatButton("", iconGlobe, func() { a.showCountryMenu(r.where) })
	worldBtn := widget.NewCheck(T("Worldwide"), func(on bool) {
		r.world = on
		r.reload()
	})
	r.search = widget.NewEntry()
	r.search.SetPlaceHolder(T("Search stations…"))
	r.search.ActionItem = widget.NewIcon(theme.SearchIcon())
	r.search.OnSubmitted = func(q string) { r.query(radio.StationQuery{Name: strings.TrimSpace(q)}, "“"+q+"”") }

	var chipObjs []fyne.CanvasObject
	for _, tag := range radioTags {
		tag := tag
		var c *chip
		c = newChip(tag, func() {
			for _, other := range r.chips {
				other.SetSelected(other == c)
			}
			r.query(radio.StationQuery{Tag: tag}, tag)
		})
		r.chips = append(r.chips, c)
		chipObjs = append(chipObjs, c)
	}
	r.resTitle = label("")
	r.resTitle.SizeName = theme.SizeNameSubHeadingText
	r.resTitle.TextStyle = fyne.TextStyle{Bold: true}
	r.results = container.NewGridWrap(fyne.NewSize(cardW, 236))
	r.favs = container.NewVBox()
	r.body = container.NewVBox()

	header := container.NewVBox(
		container.NewBorder(nil, nil, nil, container.NewHBox(worldBtn, r.where), title),
		r.search,
		sectionHeading(T("Genres")),
		chipRow(chipObjs...),
	)
	r.root = scrollPage(container.NewVBox(padded(20, 0, 0, 0, header), r.favs, r.body,
		padded(10, 0, 0, 0, r.resTitle), r.results))
	return r
}

func (r *radioView) Object() fyne.CanvasObject { return r.root }

func (r *radioView) Shown() {
	if !r.loaded {
		r.loaded = true
		r.reload()
	}
	r.showFavorites()
}

func (r *radioView) countryCode() string {
	if r.world {
		return ""
	}
	return r.app.country()
}

func (r *radioView) reload() {
	r.seq++
	seq := r.seq
	code := r.countryCode()
	r.where.SetText(countryName(r.app.country()))
	place := countryName(r.app.country())
	if code == "" {
		place = T("Worldwide")
	}
	top := container.NewStack(loadingBox(T("Loading…")))
	trend := container.NewStack()
	r.body.Objects = []fyne.CanvasObject{top, trend}
	r.body.Refresh()
	load := func(order string, box *fyne.Container, title string) {
		go func() {
			stations, err := radio.Stations(radio.StationQuery{CountryCode: code, Order: order, Limit: 40})
			runOnUI(func() {
				if seq != r.seq {
					return
				}
				if err != nil {
					box.Objects = []fyne.CanvasObject{dimLabel(err.Error())}
				} else {
					var cards []fyne.CanvasObject
					for _, st := range stations {
						cards = append(cards, r.app.stationCard(st))
					}
					box.Objects = []fyne.CanvasObject{shelf(title, newPager(cards, cardW, 1, false))}
				}
				box.Refresh()
				r.body.Refresh()
			})
		}()
	}
	load(radio.SortVotes, top, fmt.Sprintf(T("Popular stations in %s"), place))
	load(radio.SortTrend, trend, T("Trending"))
}

func (r *radioView) showFavorites() {
	favs := favoriteStations()
	r.favs.Objects = nil
	if favs != nil {
		if stations := favs.Stations(); len(stations) > 0 {
			var cards []fyne.CanvasObject
			for _, st := range stations {
				cards = append(cards, r.app.stationCard(st))
			}
			r.favs.Add(shelf(T("Favorite stations"), newPager(cards, cardW, 1, false)))
		}
	}
	r.favs.Refresh()
}

func (r *radioView) query(q radio.StationQuery, title string) {
	q.CountryCode = r.countryCode()
	if q.Name != "" {
		q.CountryCode = ""
	}
	q.Limit = 60
	r.resTitle.SetText(title)
	r.results.Objects = []fyne.CanvasObject{loadingBox(T("Loading…"))}
	r.results.Refresh()
	go func() {
		stations, err := radio.Stations(q)
		runOnUI(func() {
			r.results.Objects = nil
			if err != nil {
				r.app.toastError(err)
			}
			for _, st := range stations {
				r.results.Add(r.app.stationCard(st))
			}
			if len(stations) == 0 {
				r.resTitle.SetText(fmt.Sprintf(T("No results for “%s”"), title))
			}
			r.results.Refresh()
		})
	}()
}
