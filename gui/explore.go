package gui

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/external/radio"
	"github.com/bjarneo/cliamp/ipc"
)

const (
	prefCountry  = "country"
	prefAutoplay = "autoplay_similar"
	prefRecent   = "recent_searches"
	prefLanguage = "language"
)

// countryCodes are the countries offered in the location picker.
var countryCodes = []string{
	"AR", "AT", "AU", "BE", "BG", "BR", "CA", "CH", "CL", "CN", "CO", "CZ", "DE", "DK", "EE", "EG",
	"ES", "FI", "FR", "GB", "GR", "HK", "HR", "HU", "ID", "IE", "IL", "IN", "IS", "IT", "JP", "KE",
	"KR", "LT", "LU", "LV", "MA", "MT", "MX", "MY", "NG", "NL", "NO", "NZ", "PE", "PH", "PL", "PT",
	"RO", "RS", "SA", "SE", "SG", "SI", "SK", "TH", "TR", "TW", "UA", "US", "VN", "ZA",
}

// country returns the chosen country, or the one of the session's timezone.
func (a *App) country() string {
	if c := a.prefs.String(prefCountry); len(c) == 2 {
		return c
	}
	if c := radio.DetectHomeCountry(); len(c) == 2 {
		return strings.ToUpper(c)
	}
	return "US"
}

// countryName is the display name of a country code in the interface
// language.
func countryName(code string) string {
	if region, err := language.ParseRegion(code); err == nil {
		namer := display.English.Regions()
		if uiLang == "it" {
			namer = display.Italian.Regions()
		}
		if name := namer.Name(region); name != "" {
			return name
		}
	}
	if name := radio.CountryName(code); name != "" {
		return name
	}
	return code
}

func (a *App) setCountry(code string) {
	if code == a.country() {
		return
	}
	a.prefs.SetString(prefCountry, code)
	a.ytm.SetLocale(code, langCode())
	a.toast(T("Location") + ": " + countryName(code))
	for _, key := range []string{"home", "radio", "podcasts"} {
		if v, ok := a.views[key].(interface{ reload() }); ok {
			v.reload()
		}
	}
}

// countryMenu lets the user pick the country of the recommendations.
func (a *App) countryMenu() *fyne.Menu {
	var items []*fyne.MenuItem
	current := a.country()
	for _, code := range countryCodes {
		code := code
		it := fyne.NewMenuItem(countryName(code), func() { a.setCountry(code) })
		it.Checked = code == current
		items = append(items, it)
	}
	return fyne.NewMenu("", items...)
}

func (a *App) showCountryMenu(from fyne.CanvasObject) {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(from)
	widget.ShowPopUpMenuAtPosition(a.countryMenu(), a.win.Canvas(), pos.Add(fyne.NewPos(0, from.Size().Height)))
}

// ytTrack turns a YouTube Music item into a track the engine plays.
func ytTrack(it discover.Item) ipc.TrackInfo {
	return ipc.TrackInfo{
		Path:         it.WatchURL(),
		Title:        it.Title,
		Artist:       it.Artist,
		Album:        it.Album,
		DurationSecs: it.Duration,
		AlbumArtURL:  it.Thumb,
		Stream:       true,
	}
}

func ytTracks(items []discover.Item) []ipc.TrackInfo {
	out := make([]ipc.TrackInfo, 0, len(items))
	for _, it := range items {
		if it.Playable() {
			out = append(out, ytTrack(it))
		}
	}
	return out
}

// videoID returns the YouTube video ID of a track, or "".
func videoID(path string) string {
	u, err := url.Parse(path)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	switch host {
	case "youtube.com", "music.youtube.com", "m.youtube.com":
		return u.Query().Get("v")
	case "youtu.be":
		return strings.Trim(u.Path, "/")
	}
	return ""
}

// playTracks replaces the queue with tracks and plays the one at index.
func (a *App) playTracks(tracks []ipc.TrackInfo, index int) {
	if len(tracks) == 0 {
		return
	}
	go func() {
		if _, err := a.call("queue.clear", nil); err != nil {
			a.toastError(err)
			return
		}
		if _, err := a.call("tracks.add", P{"tracks": tracks, "play": true, "index": index}); err != nil {
			a.toastError(err)
			return
		}
		a.refreshState()
	}()
}

func shuffled(tracks []ipc.TrackInfo) []ipc.TrackInfo {
	out := append([]ipc.TrackInfo(nil), tracks...)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// playItem plays a YouTube Music item: a song at once, an album or a
// playlist from its first track, an artist from the top songs.
func (a *App) playItem(it discover.Item) {
	if it.Playable() {
		a.playTrack(ytTrack(it))
		return
	}
	a.toast(fmt.Sprintf(T("Loading %s…"), it.Title))
	go func() {
		tracks, err := a.itemTracks(it)
		if err != nil {
			a.toastError(err)
			return
		}
		runOnUI(func() { a.playTracks(tracks, 0) })
	}()
}

// itemTracks returns the playable tracks of an album, playlist or artist.
func (a *App) itemTracks(it discover.Item) ([]ipc.TrackInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch it.Kind {
	case discover.KindArtist:
		page, err := a.ytm.Browse(ctx, it)
		if err != nil {
			return nil, err
		}
		for _, s := range page.Shelves {
			if tracks := ytTracks(s.Items); len(tracks) > 0 {
				return tracks, nil
			}
		}
		return nil, fmt.Errorf("%s", T("No results for “%s”"))
	case discover.KindAlbum, discover.KindPlaylist, discover.KindPodcast:
		if it.BrowseID == "" && it.PlaylistID != "" {
			items, err := a.ytm.PlaylistTracks(ctx, it.PlaylistID)
			return ytTracks(items), err
		}
		page, err := a.ytm.Browse(ctx, it)
		if err != nil {
			return nil, err
		}
		tracks := ytTracks(page.Tracks)
		if len(tracks) == 0 && it.PlaylistID != "" {
			items, err := a.ytm.PlaylistTracks(ctx, it.PlaylistID)
			return ytTracks(items), err
		}
		return tracks, nil
	}
	return nil, fmt.Errorf("nothing to play")
}

// openItem opens the page of an item, or plays a song.
func (a *App) openItem(it discover.Item) {
	switch it.Kind {
	case discover.KindSong, discover.KindVideo, discover.KindEpisode:
		if it.Playable() {
			a.playTrack(ytTrack(it))
			return
		}
	}
	if it.BrowseID == "" && it.PlaylistID != "" {
		it.BrowseID = "VL" + it.PlaylistID
		if it.Kind == "" {
			it.Kind = discover.KindPlaylist
		}
	}
	if it.BrowseID == "" {
		a.playItem(it)
		return
	}
	a.push(newYTPage(a, it))
}

// startRadio plays the track and queues songs similar to it.
func (a *App) startRadio(t ipc.TrackInfo) {
	id := videoID(t.Path)
	if id == "" {
		// Not a YouTube track: find it there first.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			res, err := a.ytm.Search(ctx, strings.TrimSpace(t.Artist+" "+t.Title))
			if err != nil || len(res.Songs) == 0 {
				a.toastError(fmt.Errorf(T("No results for “%s”"), t.Title))
				return
			}
			runOnUI(func() { a.startRadio(ytTrack(res.Songs[0])) })
		}()
		return
	}
	a.toast(T("Radio started"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		items, err := a.ytm.Radio(ctx, id)
		if err != nil {
			a.toastError(err)
			return
		}
		tracks := append([]ipc.TrackInfo{t}, ytTracks(items)...)
		runOnUI(func() { a.playTracks(tracks, 0) })
	}()
}

// autoplay queues similar songs when a YouTube track plays near the end of
// the queue, so the music never stops.
type autoplay struct {
	mu      sync.Mutex
	seeded  map[string]bool
	running bool
}

func (a *App) checkAutoplay(s ipc.RuntimeSnapshot) {
	if !a.prefs.BoolWithFallback(prefAutoplay, true) || s.Track == nil || s.State != "playing" {
		return
	}
	if s.Repeat != "" && s.Repeat != "Off" {
		return
	}
	if s.Total-s.Index > 2 || s.PlayNextTotal > 0 {
		return
	}
	id := videoID(s.Track.Path)
	if id == "" {
		return
	}
	a.auto.mu.Lock()
	if a.auto.seeded == nil {
		a.auto.seeded = map[string]bool{}
	}
	if a.auto.running || a.auto.seeded[id] {
		a.auto.mu.Unlock()
		return
	}
	a.auto.running = true
	a.auto.seeded[id] = true
	a.auto.mu.Unlock()
	have := map[string]bool{}
	for _, t := range a.queue.all {
		if v := videoID(t.Path); v != "" {
			have[v] = true
		}
	}
	go func() {
		defer func() {
			a.auto.mu.Lock()
			a.auto.running = false
			a.auto.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		items, err := a.ytm.Radio(ctx, id)
		if err != nil {
			return
		}
		var fresh []ipc.TrackInfo
		for _, t := range ytTracks(items) {
			if v := videoID(t.Path); !have[v] {
				fresh = append(fresh, t)
			}
			if len(fresh) == 15 {
				break
			}
		}
		if len(fresh) == 0 {
			return
		}
		if _, err := a.call("tracks.add", P{"tracks": fresh}); err == nil {
			a.toast(T("Similar songs added"))
			a.refreshState()
		}
	}()
}

// itemMenu is the context menu of a YouTube Music item.
func (a *App) itemMenu(it discover.Item) *fyne.Menu {
	if it.Playable() {
		t := ytTrack(it)
		items := a.trackMenu(t)
		return fyne.NewMenu("", items...)
	}
	return fyne.NewMenu("",
		fyne.NewMenuItemWithIcon(T("Open"), theme.FolderOpenIcon(), func() { a.openItem(it) }),
		fyne.NewMenuItemWithIcon(T("Play"), theme.MediaPlayIcon(), func() { a.playItem(it) }),
		fyne.NewMenuItemWithIcon(T("Add to queue"), theme.ContentAddIcon(), func() {
			go func() {
				tracks, err := a.itemTracks(it)
				if err != nil {
					a.toastError(err)
					return
				}
				runOnUI(func() { a.addTracks(tracks, false) })
			}()
		}),
	)
}

// itemCard builds a card for a YouTube Music item.
func (a *App) itemCard(it discover.Item) fyne.CanvasObject {
	sub := it.Subtitle
	if it.Artist != "" && (it.Kind == discover.KindSong || it.Kind == discover.KindVideo || it.Kind == discover.KindAlbum) {
		sub = it.Artist
	}
	c := newCard(a, it.Title, sub, it.Thumb, it.Kind == discover.KindArtist,
		func() { a.openItem(it) }, func() { a.playItem(it) })
	if it.Playable() {
		c.hover.url = it.WatchURL()
	}
	c.onMenu = func(pos fyne.Position) {
		widget.ShowPopUpMenuAtPosition(a.itemMenu(it), a.win.Canvas(), pos)
	}
	return c
}

// itemTile builds a quick-pick tile for a song.
func (a *App) itemTile(it discover.Item) fyne.CanvasObject {
	sub := it.Artist
	if sub == "" {
		sub = it.Subtitle
	}
	t := newTile(a, it.Title, sub, it.Thumb, func() { a.openItem(it) })
	if it.Playable() {
		t.hover.url = it.WatchURL()
	}
	t.onMenu = func(pos fyne.Position) {
		widget.ShowPopUpMenuAtPosition(a.itemMenu(it), a.win.Canvas(), pos)
	}
	return t
}

// shelfObject renders a shelf: songs as a grid of tiles, the rest as cards.
func (a *App) shelfObject(s discover.Shelf) fyne.CanvasObject {
	songs := 0
	for _, it := range s.Items {
		if it.Playable() {
			songs++
		}
	}
	var objs []fyne.CanvasObject
	if songs*2 > len(s.Items) && len(s.Items) > 4 {
		for _, it := range s.Items {
			objs = append(objs, a.itemTile(it))
		}
		return shelf(s.Title, newPager(objs, 300, 4, true))
	}
	for _, it := range s.Items {
		objs = append(objs, a.itemCard(it))
	}
	return shelf(s.Title, newPager(objs, cardW, 1, false))
}

// recentSearches returns the last queries, newest first.
func (a *App) recentSearches() []string {
	raw := a.prefs.String(prefRecent)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func (a *App) rememberSearch(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	list := []string{q}
	for _, old := range a.recentSearches() {
		if !strings.EqualFold(old, q) && len(list) < 12 {
			list = append(list, old)
		}
	}
	a.prefs.SetString(prefRecent, strings.Join(list, "\n"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
