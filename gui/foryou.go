package gui

import (
	"context"
	"fmt"
	"image/color"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/ipc"
)

// taste is what the listening history says about the listener.
type taste struct {
	recent  []ipc.TrackInfo // unique, newest first
	artists []string        // most played first
	seeds   map[string]ipc.TrackInfo
}

// readTaste summarizes the history. The engine keeps it as Recently Played.
func (a *App) readTaste() (taste, error) {
	r, err := a.call("history", P{"limit": 400})
	if err != nil {
		return taste{}, err
	}
	t := taste{seeds: map[string]ipc.TrackInfo{}}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, h := range r.Response.History {
		tr := h.Track
		if tr.Realtime || tr.Feed {
			continue
		}
		if !seen[tr.Path] {
			seen[tr.Path] = true
			t.recent = append(t.recent, tr)
		}
		artist := primaryArtist(tr.Artist)
		if artist == "" {
			continue
		}
		counts[artist]++
		if _, ok := t.seeds[artist]; !ok || (videoID(tr.Path) != "" && videoID(t.seeds[artist].Path) == "") {
			t.seeds[artist] = tr
		}
	}
	for artist := range counts {
		t.artists = append(t.artists, artist)
	}
	sort.SliceStable(t.artists, func(i, j int) bool { return counts[t.artists[i]] > counts[t.artists[j]] })
	return t, nil
}

// primaryArtist keeps the first artist of "A, B & C feat. D".
func primaryArtist(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{",", " & ", " feat", " ft.", " x ", " e ", " and "} {
		if i := strings.Index(strings.ToLower(s), sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// seedVideo returns a YouTube video for the seed track, searching for it
// when it comes from another source.
func (a *App) seedVideo(ctx context.Context, t ipc.TrackInfo) string {
	if id := videoID(t.Path); id != "" {
		return id
	}
	res, err := a.ytm.Search(ctx, strings.TrimSpace(t.Artist+" "+t.Title))
	if err != nil || len(res.Songs) == 0 {
		return ""
	}
	return res.Songs[0].VideoID
}

// forYou builds the personal section of Home from the history.
func (a *App) forYou(ctx context.Context) (fyne.CanvasObject, error) {
	t, err := a.readTaste()
	if err != nil || len(t.recent) == 0 {
		return nil, err
	}
	box := container.NewVBox()

	// Recently played.
	var recent []fyne.CanvasObject
	for _, tr := range t.recent[:min(16, len(t.recent))] {
		tr := tr
		title, sub := trackTitle(tr)
		c := newCard(a, title, sub, tr.AlbumArtURL, false, func() { a.playTrack(tr) }, func() { a.playTrack(tr) })
		c.hover.url = tr.Path
		if tr.AlbumArtURL == "" && isLocalPath(tr.Path) {
			c.art.Load(tr)
		}
		c.onMenu = func(pos fyne.Position) {
			widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", a.trackMenu(tr)...), a.win.Canvas(), pos)
		}
		recent = append(recent, c)
	}

	// Radios of the top artists, in parallel.
	top := t.artists[:min(4, len(t.artists))]
	radios := make([][]discover.Item, len(top))
	var wg sync.WaitGroup
	for i, artist := range top {
		wg.Add(1)
		go func(i int, artist string) {
			defer wg.Done()
			if id := a.seedVideo(ctx, t.seeds[artist]); id != "" {
				radios[i], _ = a.ytm.Radio(ctx, id)
			}
		}(i, artist)
	}
	// Artist pages of the top artists.
	topArtists := make([]discover.Item, len(t.artists[:min(8, len(t.artists))]))
	for i, artist := range t.artists[:len(topArtists)] {
		wg.Add(1)
		go func(i int, artist string) {
			defer wg.Done()
			res, err := a.ytm.Search(ctx, artist)
			if err == nil && len(res.Artists) > 0 {
				topArtists[i] = res.Artists[0]
			}
		}(i, artist)
	}
	wg.Wait()

	played := map[string]bool{}
	for _, tr := range t.recent {
		if id := videoID(tr.Path); id != "" {
			played[id] = true
		}
	}
	var mixItems []discover.Item
	for _, items := range radios {
		n := 0
		for _, it := range items {
			if !played[it.VideoID] && n < 12 {
				mixItems = append(mixItems, it)
				n++
			}
		}
	}
	rand.Shuffle(len(mixItems), func(i, j int) { mixItems[i], mixItems[j] = mixItems[j], mixItems[i] })
	if len(mixItems) > 0 {
		based := strings.Join(top[:min(3, len(top))], ", ")
		mixTracks := ytTracks(mixItems)
		box.Add(a.mixBanner(T("Your daily mix"), fmt.Sprintf(T("Based on %s"), based), mixItems, func() { a.playTracks(mixTracks, 0) }))
	}
	box.Add(shelf(T("Recently played"), newPager(recent, cardW, 1, false)))
	for i, items := range radios {
		var fresh []discover.Item
		for _, it := range items {
			if !played[it.VideoID] {
				fresh = append(fresh, it)
			}
		}
		if len(fresh) > 0 {
			box.Add(a.shelfObject(discover.Shelf{Title: fmt.Sprintf(T("Because you listened to %s"), top[i]), Items: fresh}))
		}
	}
	var artistCards []fyne.CanvasObject
	for _, it := range topArtists {
		if it.BrowseID != "" {
			artistCards = append(artistCards, a.itemCard(it))
		}
	}
	if len(artistCards) > 0 {
		box.Add(shelf(T("Your top artists"), newPager(artistCards, cardW, 1, false)))
	}
	return box, nil
}

// mixBanner is a wide gradient card that plays a mix.
func (a *App) mixBanner(title, sub string, items []discover.Item, play func()) fyne.CanvasObject {
	bg := tRect(func(p Palette) color.Color { return mix(mix(p.Accent, p.Surface, 0.55), p.Accent2, 0.15) })
	bg.CornerRadius = 16
	// A collage of the first covers.
	collage := container.NewGridWithColumns(2)
	for _, it := range items[:min(4, len(items))] {
		art := newArtView(a, 64, 8)
		art.image.FillMode = canvas.ImageFillCover
		art.LoadURL(it.Thumb)
		collage.Add(art.Object())
	}
	t := label(title)
	t.SizeName = theme.SizeNameHeadingText
	t.TextStyle = fyne.TextStyle{Bold: true}
	s := label(sub)
	s.Wrapping = fyne.TextWrapWord
	kind := captionLabel(strings.ToUpper(T("Made for you")))
	kind.TextStyle = fyne.TextStyle{Bold: true}
	btn := newRoundButton(theme.MediaPlayIcon(), 52, play)
	info := container.NewVBox(spacer(), kind, t, s, spacer())
	content := container.NewBorder(nil, nil, padded(0, 18, 0, 0, collage), container.NewVBox(spacer(), btn, spacer()), info)
	tap := newTapArea(play)
	return padded(14, 0, 8, 0, container.NewStack(bg, tap, padded(16, 20, 16, 16, content)))
}
