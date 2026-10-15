// Package discover finds music to play without an account: the YouTube
// Music catalog through its public web API, the Apple podcast charts and the
// Radio Browser station directory. Kalulu builds its Home, Search, Radio and
// Podcast pages from it. Playback still goes through the engine, which plays
// YouTube through yt-dlp.
package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kinds of YouTube Music items.
const (
	KindSong     = "song"
	KindVideo    = "video"
	KindAlbum    = "album"
	KindPlaylist = "playlist"
	KindArtist   = "artist"
	KindMood     = "mood" // a genre or mood category page
	KindEpisode  = "episode"
	KindPodcast  = "podcast"
)

// Item is one entry of a YouTube Music page.
type Item struct {
	Kind     string
	Title    string
	Subtitle string
	Artist   string
	Album    string
	VideoID  string
	// BrowseID opens an album, playlist, artist or mood page.
	BrowseID string
	// Params goes with BrowseID for mood pages.
	Params string
	// PlaylistID plays a playlist or an album directly.
	PlaylistID string
	Thumb      string
	Duration   int
}

// Playable reports whether the item is a single track.
func (it Item) Playable() bool { return it.VideoID != "" }

// WatchURL is the URL the engine plays through yt-dlp.
func (it Item) WatchURL() string {
	return "https://music.youtube.com/watch?v=" + it.VideoID
}

// Shelf is a titled row of items.
type Shelf struct {
	Title string
	Items []Item
}

// Page is a browse result: an optional header item, the tracks of an album
// or playlist, and the shelves of an artist or home page.
type Page struct {
	Header  Item
	Tracks  []Item
	Shelves []Shelf
}

// YTMusic is a client of the YouTube Music web API. Country and Language
// localize the results; both may change at any time.
type YTMusic struct {
	mu       sync.Mutex
	country  string
	language string
	http     *http.Client
	base     string
}

// NewYTMusic returns a client for country (ISO 3166-1, such as "IT") and
// language (such as "it").
func NewYTMusic(country, language string) *YTMusic {
	y := &YTMusic{http: &http.Client{Timeout: 20 * time.Second}, base: "https://music.youtube.com/youtubei/v1/"}
	y.SetLocale(country, language)
	return y
}

// SetLocale changes the country and the language of later requests.
func (y *YTMusic) SetLocale(country, language string) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 {
		country = "US"
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		language = "en"
	}
	y.mu.Lock()
	y.country, y.language = country, language
	y.mu.Unlock()
}

// Country returns the current country code.
func (y *YTMusic) Country() string {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.country
}

const ytmClientVersion = "1.20250101.01.00"

func (y *YTMusic) post(ctx context.Context, endpoint string, body map[string]any) (any, error) {
	y.mu.Lock()
	country, language := y.country, y.language
	y.mu.Unlock()
	body["context"] = map[string]any{
		"client": map[string]any{
			"clientName":    "WEB_REMIX",
			"clientVersion": ytmClientVersion,
			"hl":            language,
			"gl":            country,
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, y.base+endpoint+"?prettyPrint=false", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("Referer", "https://music.youtube.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	resp, err := y.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("youtube music: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("youtube music: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music: HTTP %d", resp.StatusCode)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("youtube music: %w", err)
	}
	return out, nil
}

// Suggest returns search completions for a partial query.
func (y *YTMusic) Suggest(ctx context.Context, input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	data, err := y.post(ctx, "music/get_search_suggestions", map[string]any{"input": input})
	if err != nil {
		return nil, err
	}
	var out []string
	walk(data, func(m map[string]any) bool {
		if r, ok := m["searchSuggestionRenderer"].(map[string]any); ok {
			if s := runsText(get(r, "suggestion")); s != "" {
				out = append(out, s)
			}
			return false
		}
		return true
	})
	return out, nil
}

// Search filters, as the web client sends them.
const (
	filterSongs     = "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D"
	filterVideos    = "EgWKAQIQAWoKEAkQChAFEAMQBA%3D%3D"
	filterAlbums    = "EgWKAQIYAWoKEAkQAxAEEAoQBQ%3D%3D"
	filterArtists   = "EgWKAQIgAWoKEAkQChAFEAMQBA%3D%3D"
	filterPlaylists = "EgeKAQQoAEABagoQAxAEEAoQCRAF"
)

// SearchResult groups the results of a search.
type SearchResult struct {
	Top       []Item
	Songs     []Item
	Videos    []Item
	Albums    []Item
	Artists   []Item
	Playlists []Item
}

// Search runs the query in each category at once.
func (y *YTMusic) Search(ctx context.Context, query string) (SearchResult, error) {
	var res SearchResult
	query = strings.TrimSpace(query)
	if query == "" {
		return res, nil
	}
	type part struct {
		dst    *[]Item
		params string
	}
	parts := []part{
		{&res.Top, ""}, {&res.Songs, filterSongs}, {&res.Videos, filterVideos},
		{&res.Albums, filterAlbums}, {&res.Artists, filterArtists}, {&res.Playlists, filterPlaylists},
	}
	var wg sync.WaitGroup
	errs := make([]error, len(parts))
	for i, p := range parts {
		wg.Add(1)
		go func(i int, p part) {
			defer wg.Done()
			body := map[string]any{"query": query}
			if p.params != "" {
				body["params"] = unescapeParams(p.params)
			}
			data, err := y.post(ctx, "search", body)
			if err != nil {
				errs[i] = err
				return
			}
			*p.dst = collectItems(data)
		}(i, p)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil && len(res.Top)+len(res.Songs) == 0 {
			return res, err
		}
	}
	// The top results may hold a card of the best match first.
	return res, nil
}

func unescapeParams(p string) string {
	return strings.NewReplacer("%3D", "=", "%2B", "+", "%2F", "/").Replace(p)
}

// Home returns the localized home shelves, following continuations up to
// pages times.
func (y *YTMusic) Home(ctx context.Context, pages int) ([]Shelf, error) {
	data, err := y.post(ctx, "browse", map[string]any{"browseId": "FEmusic_home"})
	if err != nil {
		return nil, err
	}
	shelves := collectShelves(data)
	token := continuation(data)
	for i := 0; i < pages && token != ""; i++ {
		more, err := y.post(ctx, "browse", map[string]any{"continuation": token})
		if err != nil {
			break
		}
		shelves = append(shelves, collectShelves(more)...)
		token = continuation(more)
	}
	return shelves, nil
}

// Charts returns the chart shelves of the country.
func (y *YTMusic) Charts(ctx context.Context) ([]Shelf, error) {
	data, err := y.post(ctx, "browse", map[string]any{
		"browseId": "FEmusic_charts",
		"formData": map[string]any{"selectedValues": []string{y.Country()}},
	})
	if err != nil {
		return nil, err
	}
	return collectShelves(data), nil
}

// Moods returns the genre and mood categories.
func (y *YTMusic) Moods(ctx context.Context) ([]Shelf, error) {
	data, err := y.post(ctx, "browse", map[string]any{"browseId": "FEmusic_moods_and_genres"})
	if err != nil {
		return nil, err
	}
	var shelves []Shelf
	walk(data, func(m map[string]any) bool {
		grid, ok := m["gridRenderer"].(map[string]any)
		if !ok {
			return true
		}
		title := runsText(get(grid, "header", "gridHeaderRenderer", "title"))
		var items []Item
		for _, it := range list(grid["items"]) {
			btn, ok := get(it, "musicNavigationButtonRenderer").(map[string]any)
			if !ok {
				continue
			}
			ep, _ := get(btn, "clickCommand", "browseEndpoint").(map[string]any)
			if ep == nil {
				continue
			}
			items = append(items, Item{
				Kind:     KindMood,
				Title:    runsText(btn["buttonText"]),
				BrowseID: str(ep["browseId"]),
				Params:   str(ep["params"]),
			})
		}
		if len(items) > 0 {
			shelves = append(shelves, Shelf{Title: title, Items: items})
		}
		return false
	})
	return shelves, nil
}

// Browse opens an album, playlist, artist or mood page.
func (y *YTMusic) Browse(ctx context.Context, it Item) (Page, error) {
	body := map[string]any{"browseId": it.BrowseID}
	if it.Params != "" {
		body["params"] = it.Params
	}
	data, err := y.post(ctx, "browse", body)
	if err != nil {
		return Page{}, err
	}
	page := Page{Header: it}
	page.Header.Title = firstNonEmpty(headerTitle(data), it.Title)
	if sub := headerSubtitle(data); sub != "" {
		page.Header.Subtitle = sub
	}
	if thumb := headerThumb(data); thumb != "" {
		page.Header.Thumb = thumb
	}
	switch it.Kind {
	case KindAlbum, KindPlaylist, KindPodcast:
		page.Tracks = collectTrackRows(data)
		for i := range page.Tracks {
			if page.Tracks[i].Thumb == "" {
				page.Tracks[i].Thumb = page.Header.Thumb
			}
			if it.Kind == KindAlbum && page.Tracks[i].Album == "" {
				page.Tracks[i].Album = page.Header.Title
			}
			if it.Kind == KindAlbum && page.Tracks[i].Artist == "" {
				page.Tracks[i].Artist = it.Artist
			}
		}
		// A long playlist continues in further pages.
		token := continuation(data)
		for n := 0; n < 4 && token != "" && it.Kind == KindPlaylist; n++ {
			more, err := y.post(ctx, "browse", map[string]any{"continuation": token})
			if err != nil {
				break
			}
			page.Tracks = append(page.Tracks, collectTrackRows(more)...)
			token = continuation(more)
		}
	default:
		page.Shelves = collectShelves(data)
	}
	return page, nil
}

// Radio returns songs similar to the video, the "radio" of YouTube Music. The
// seed itself is left out.
func (y *YTMusic) Radio(ctx context.Context, videoID string) ([]Item, error) {
	data, err := y.post(ctx, "next", map[string]any{
		"videoId":     videoID,
		"playlistId":  "RDAMVM" + videoID,
		"isAudioOnly": true,
	})
	if err != nil {
		return nil, err
	}
	var out []Item
	seen := map[string]bool{videoID: true}
	walk(data, func(m map[string]any) bool {
		r, ok := m["playlistPanelVideoRenderer"].(map[string]any)
		if !ok {
			return true
		}
		id := str(r["videoId"])
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
		var byline []string
		for _, part := range runs(r["longBylineText"]) {
			if part = strings.TrimSpace(part); part != "" && part != "•" {
				byline = append(byline, part)
			}
		}
		it := Item{
			Kind:     KindSong,
			Title:    runsText(r["title"]),
			VideoID:  id,
			Thumb:    bestThumb(r["thumbnail"]),
			Duration: parseDuration(runsText(r["lengthText"])),
		}
		if len(byline) > 0 {
			it.Artist = byline[0]
			for _, part := range byline[1:] {
				if !looksLikeCount(part) && it.Album == "" && !isYear(part) {
					it.Album = part
				}
			}
		}
		it.Subtitle = it.Artist
		out = append(out, it)
		return false
	})
	return out, nil
}

// PlaylistTracks returns the tracks of a playlist or album by playlist ID.
func (y *YTMusic) PlaylistTracks(ctx context.Context, playlistID string) ([]Item, error) {
	if strings.HasPrefix(playlistID, "VL") {
		playlistID = playlistID[2:]
	}
	page, err := y.Browse(ctx, Item{Kind: KindPlaylist, BrowseID: "VL" + playlistID})
	return page.Tracks, err
}

// --- parsing ---------------------------------------------------------------

func collectShelves(data any) []Shelf {
	var shelves []Shelf
	walk(data, func(m map[string]any) bool {
		if c, ok := m["musicCarouselShelfRenderer"].(map[string]any); ok {
			title := runsText(get(c, "header", "musicCarouselShelfBasicHeaderRenderer", "title"))
			if items := parseItems(list(c["contents"])); len(items) > 0 {
				shelves = append(shelves, Shelf{Title: title, Items: items})
			}
			return false
		}
		if c, ok := m["musicShelfRenderer"].(map[string]any); ok {
			title := runsText(c["title"])
			if items := parseItems(list(c["contents"])); len(items) > 0 {
				shelves = append(shelves, Shelf{Title: title, Items: items})
			}
			return false
		}
		if c, ok := m["gridRenderer"].(map[string]any); ok {
			title := runsText(get(c, "header", "gridHeaderRenderer", "title"))
			if items := parseItems(list(c["items"])); len(items) > 0 {
				shelves = append(shelves, Shelf{Title: title, Items: items})
			}
			return false
		}
		return true
	})
	return shelves
}

// collectItems returns every item of a search response in order.
func collectItems(data any) []Item {
	var items []Item
	walk(data, func(m map[string]any) bool {
		if card, ok := m["musicCardShelfRenderer"].(map[string]any); ok {
			head, ok := parseCard(card)
			if ok {
				items = append(items, head)
			}
			if contents := list(card["contents"]); len(contents) > 0 {
				// The songs under an artist card leave the artist out.
				for _, it := range parseItems(contents) {
					if it.Artist == "" && head.Kind == KindArtist {
						it.Artist = head.Title
					}
					if it.Artist == "" && head.Kind == KindAlbum {
						it.Artist = head.Artist
					}
					items = append(items, it)
				}
			}
			return false
		}
		if r, ok := m["musicResponsiveListItemRenderer"].(map[string]any); ok {
			if it, ok := parseListItem(r); ok {
				items = append(items, it)
			}
			return false
		}
		return true
	})
	return dedupe(items)
}

func collectTrackRows(data any) []Item {
	var items []Item
	walk(data, func(m map[string]any) bool {
		if r, ok := m["musicResponsiveListItemRenderer"].(map[string]any); ok {
			if it, ok := parseListItem(r); ok && it.VideoID != "" {
				items = append(items, it)
			}
			return false
		}
		return true
	})
	return dedupe(items)
}

func dedupe(items []Item) []Item {
	seen := map[string]bool{}
	out := items[:0]
	for _, it := range items {
		key := it.Kind + it.VideoID + it.BrowseID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

func parseItems(contents []any) []Item {
	var items []Item
	for _, c := range contents {
		if r, ok := get(c, "musicTwoRowItemRenderer").(map[string]any); ok {
			if it, ok := parseTwoRow(r); ok {
				items = append(items, it)
			}
			continue
		}
		if r, ok := get(c, "musicResponsiveListItemRenderer").(map[string]any); ok {
			if it, ok := parseListItem(r); ok {
				items = append(items, it)
			}
			continue
		}
		if r, ok := get(c, "musicNavigationButtonRenderer").(map[string]any); ok {
			if ep, ok := get(r, "clickCommand", "browseEndpoint").(map[string]any); ok {
				items = append(items, Item{Kind: KindMood, Title: runsText(r["buttonText"]), BrowseID: str(ep["browseId"]), Params: str(ep["params"])})
			}
		}
	}
	return items
}

func parseTwoRow(r map[string]any) (Item, bool) {
	it := Item{
		Title: runsText(r["title"]),
		Thumb: bestThumb(get(r, "thumbnailRenderer")),
	}
	sub := runs(r["subtitle"])
	it.Subtitle = strings.Join(sub, "")
	applyEndpoint(&it, r["navigationEndpoint"])
	if it.Kind == "" {
		applyEndpoint(&it, get(r, "title", "runs", "0", "navigationEndpoint"))
	}
	if id := str(get(r, "thumbnailOverlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint", "watchPlaylistEndpoint", "playlistId")); id != "" {
		it.PlaylistID = id
	}
	parts := splitDots(it.Subtitle)
	switch it.Kind {
	case KindSong, KindVideo:
		for _, p := range parts {
			if !looksLikeCount(p) && !isKindWord(p) && it.Artist == "" {
				it.Artist = p
			}
		}
	case KindAlbum:
		for _, p := range parts {
			if !isKindWord(p) && !isYear(p) && it.Artist == "" {
				it.Artist = p
			}
		}
	}
	return it, it.Title != "" && it.Kind != ""
}

func parseCard(card map[string]any) (Item, bool) {
	it := Item{
		Title: runsText(card["title"]),
		Thumb: bestThumb(get(card, "thumbnail")),
	}
	applyEndpoint(&it, get(card, "title", "runs", "0", "navigationEndpoint"))
	it.Subtitle = runsText(card["subtitle"])
	parts := splitDots(it.Subtitle)
	for _, p := range parts {
		if !isKindWord(p) && !looksLikeCount(p) && it.Artist == "" {
			it.Artist = p
		}
	}
	if it.Kind == KindSong || it.Kind == KindVideo {
		for _, p := range parts {
			if d := parseDuration(p); d > 0 {
				it.Duration = d
			}
		}
	}
	return it, it.Title != "" && it.Kind != ""
}

func parseListItem(r map[string]any) (Item, bool) {
	cols := list(r["flexColumns"])
	var texts [][]string
	for _, c := range cols {
		texts = append(texts, runs(get(c, "musicResponsiveListItemFlexColumnRenderer", "text")))
	}
	it := Item{Thumb: bestThumb(get(r, "thumbnail"))}
	if len(texts) > 0 {
		it.Title = strings.Join(texts[0], "")
	}
	if id := str(get(r, "playlistItemData", "videoId")); id != "" {
		it.VideoID = id
	}
	applyEndpoint(&it, r["navigationEndpoint"])
	if it.Kind == "" && len(cols) > 0 {
		applyEndpoint(&it, get(cols[0], "musicResponsiveListItemFlexColumnRenderer", "text", "runs", "0", "navigationEndpoint"))
	}
	if it.Kind == "" && it.VideoID != "" {
		it.Kind = KindSong
	}
	var rest []string
	for _, t := range texts[min(1, len(texts)):] {
		for _, p := range t {
			if p = strings.TrimSpace(p); p != "" && p != "•" && p != "&" && p != "," {
				rest = append(rest, p)
			}
		}
	}
	it.Subtitle = strings.Join(rest, " • ")
	for _, fc := range list(r["fixedColumns"]) {
		if d := parseDuration(runsText(get(fc, "musicResponsiveListItemFixedColumnRenderer", "text"))); d > 0 {
			it.Duration = d
		}
	}
	// Song rows: type word, artist, album, duration in some order. The
	// navigation endpoints of the runs tell artist and album apart.
	for _, c := range cols[min(1, len(cols)):] {
		for _, run := range list(get(c, "musicResponsiveListItemFlexColumnRenderer", "text", "runs")) {
			ep, _ := get(run, "navigationEndpoint", "browseEndpoint").(map[string]any)
			text := str(get(run, "text"))
			switch pageType(ep) {
			case "MUSIC_PAGE_TYPE_ARTIST", "MUSIC_PAGE_TYPE_USER_CHANNEL":
				if it.Artist == "" {
					it.Artist = text
				}
			case "MUSIC_PAGE_TYPE_ALBUM":
				it.Album = text
			}
			if d := parseDuration(text); d > 0 && it.Duration == 0 {
				it.Duration = d
			}
		}
	}
	if it.Artist == "" && (it.Kind == KindSong || it.Kind == KindVideo) {
		for _, p := range rest {
			if !isKindWord(p) && !looksLikeCount(p) && parseDuration(p) == 0 {
				it.Artist = p
				break
			}
		}
	}
	if it.Kind == KindSong && len(rest) > 0 && strings.EqualFold(rest[0], "video") {
		it.Kind = KindVideo
	}
	return it, it.Title != "" && it.Kind != ""
}

func pageType(ep map[string]any) string {
	return str(get(ep, "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType"))
}

// applyEndpoint sets the kind and the IDs of it from a navigation endpoint.
func applyEndpoint(it *Item, endpoint any) {
	ep, ok := endpoint.(map[string]any)
	if !ok {
		return
	}
	if w, ok := ep["watchEndpoint"].(map[string]any); ok {
		it.VideoID = str(w["videoId"])
		if pl := str(w["playlistId"]); pl != "" && it.PlaylistID == "" {
			it.PlaylistID = pl
		}
		it.Kind = KindSong
		if str(get(w, "watchEndpointMusicSupportedConfigs", "watchEndpointMusicConfig", "musicVideoType")) == "MUSIC_VIDEO_TYPE_UGC" {
			it.Kind = KindVideo
		}
		if it.VideoID == "" && it.PlaylistID != "" {
			it.Kind = KindPlaylist
		}
		return
	}
	if w, ok := ep["watchPlaylistEndpoint"].(map[string]any); ok {
		it.PlaylistID = str(w["playlistId"])
		it.Kind = KindPlaylist
		return
	}
	if b, ok := ep["browseEndpoint"].(map[string]any); ok {
		it.BrowseID = str(b["browseId"])
		it.Params = str(b["params"])
		switch pageType(b) {
		case "MUSIC_PAGE_TYPE_ALBUM", "MUSIC_PAGE_TYPE_AUDIOBOOK":
			it.Kind = KindAlbum
		case "MUSIC_PAGE_TYPE_PLAYLIST":
			it.Kind = KindPlaylist
		case "MUSIC_PAGE_TYPE_ARTIST", "MUSIC_PAGE_TYPE_USER_CHANNEL", "MUSIC_PAGE_TYPE_LIBRARY_ARTIST":
			it.Kind = KindArtist
		case "MUSIC_PAGE_TYPE_PODCAST_SHOW_DETAIL_PAGE":
			it.Kind = KindPodcast
		case "MUSIC_PAGE_TYPE_NON_MUSIC_AUDIO_TRACK_PAGE":
			it.Kind = KindEpisode
		default:
			if strings.HasPrefix(it.BrowseID, "VL") {
				it.Kind = KindPlaylist
			} else if strings.HasPrefix(it.BrowseID, "MPRE") {
				it.Kind = KindAlbum
			} else if strings.HasPrefix(it.BrowseID, "UC") {
				it.Kind = KindArtist
			} else if strings.HasPrefix(it.BrowseID, "FEmusic_moods") {
				it.Kind = KindMood
			}
		}
	}
}

func headerTitle(data any) string {
	var title string
	walk(data, func(m map[string]any) bool {
		for _, key := range []string{"musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer", "musicImmersiveHeaderRenderer", "musicVisualHeaderRenderer", "musicHeaderRenderer"} {
			if h, ok := m[key].(map[string]any); ok {
				title = runsText(h["title"])
				return false
			}
		}
		return title == ""
	})
	return title
}

func headerSubtitle(data any) string {
	var sub string
	walk(data, func(m map[string]any) bool {
		for _, key := range []string{"musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer"} {
			if h, ok := m[key].(map[string]any); ok {
				parts := []string{runsText(h["straplineTextOne"]), runsText(h["subtitle"]), runsText(h["secondSubtitle"])}
				var keep []string
				for _, p := range parts {
					if p != "" {
						keep = append(keep, p)
					}
				}
				sub = strings.Join(keep, " • ")
				return false
			}
		}
		return sub == ""
	})
	return sub
}

func headerThumb(data any) string {
	var thumb string
	walk(data, func(m map[string]any) bool {
		for _, key := range []string{"musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer", "musicImmersiveHeaderRenderer", "musicVisualHeaderRenderer"} {
			if h, ok := m[key].(map[string]any); ok {
				thumb = bestThumb(h["thumbnail"])
				if thumb == "" {
					thumb = bestThumb(h["foregroundThumbnail"])
				}
				return false
			}
		}
		return thumb == ""
	})
	return thumb
}

func continuation(data any) string {
	var token string
	walk(data, func(m map[string]any) bool {
		if token != "" {
			return false
		}
		if c, ok := m["nextContinuationData"].(map[string]any); ok {
			token = str(c["continuation"])
			return false
		}
		if c, ok := m["continuationCommand"].(map[string]any); ok {
			token = str(c["token"])
			return false
		}
		return true
	})
	return token
}

// bestThumb returns the largest thumbnail URL found under v.
func bestThumb(v any) string {
	best, bestW := "", -1.0
	walk(v, func(m map[string]any) bool {
		thumbs, ok := m["thumbnails"].([]any)
		if !ok {
			return true
		}
		for _, t := range thumbs {
			tm, _ := t.(map[string]any)
			w, _ := tm["width"].(float64)
			if u := str(tm["url"]); u != "" && w > bestW {
				best, bestW = u, w
			}
		}
		return false
	})
	if strings.HasPrefix(best, "//") {
		best = "https:" + best
	}
	return upscaleThumb(best)
}

var thumbSize = regexp.MustCompile(`=w\d+-h\d+`)

// upscaleThumb asks Google image URLs for a 544 px square, which stays sharp
// on a large card.
func upscaleThumb(u string) string {
	if strings.Contains(u, "googleusercontent.com") || strings.Contains(u, "ggpht.com") {
		return thumbSize.ReplaceAllString(u, "=w544-h544")
	}
	return u
}

// --- JSON helpers ----------------------------------------------------------

// walk calls fn for each object under v, depth first. fn returns false to
// skip the children of an object.
func walk(v any, fn func(map[string]any) bool) {
	switch t := v.(type) {
	case map[string]any:
		if !fn(t) {
			return
		}
		for _, c := range t {
			walk(c, fn)
		}
	case []any:
		for _, c := range t {
			walk(c, fn)
		}
	}
}

// get follows a path of object keys and list indexes.
func get(v any, path ...string) any {
	for _, p := range path {
		switch t := v.(type) {
		case map[string]any:
			v = t[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func runs(v any) []string {
	var out []string
	for _, r := range list(get(v, "runs")) {
		out = append(out, str(get(r, "text")))
	}
	if len(out) == 0 {
		if s := str(get(v, "simpleText")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func runsText(v any) string { return strings.TrimSpace(strings.Join(runs(v), "")) }

func splitDots(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "•") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var kindWords = map[string]bool{
	"song": true, "brano": true, "video": true, "album": true, "single": true, "singolo": true,
	"ep": true, "playlist": true, "artist": true, "artista": true, "episode": true, "episodio": true,
	"podcast": true, "canción": true, "chanson": true, "lied": true, "titre": true,
}

func isKindWord(s string) bool { return kindWords[strings.ToLower(strings.TrimSpace(s))] }

var countRE = regexp.MustCompile(`(?i)\d.*(views|visualizzazioni|plays|riproduzioni|mi piace|likes|ascolt|subscribers|iscritti|mln|mrd|tracks|brani)`)

func looksLikeCount(s string) bool { return countRE.MatchString(s) }

func isYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n > 1900 && n < 2100
}

// parseDuration reads m:ss or h:mm:ss.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
