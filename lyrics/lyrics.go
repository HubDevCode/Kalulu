package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bjarneo/cliamp/internal/httpclient"
)

// ErrNotFound is returned when no lyrics could be found from any source.
var ErrNotFound = errors.New("no lyrics found")

// Line represents a single timestamped lyrical line.
type Line struct {
	Start time.Duration
	Text  string
	// Words holds per-word timings of enhanced LRC ("<mm:ss.xx>word"),
	// when the source has them.
	Words []Word
}

// Word is one timed word of a line.
type Word struct {
	Start time.Duration
	Text  string
}

// Hint narrows a lookup to the right recording.
type Hint struct {
	Album    string
	Duration time.Duration
	// VideoID is the YouTube video of the track, for the sources that
	// know tracks by it.
	VideoID string
}

// httpClient is reused across all lyrics API calls.
var httpClient = httpclient.NewAPI(10 * time.Second)

// The base URLs of the LRCLIB and NetEase APIs. Tests point them at a local
// server.
var (
	lrclibBaseURL  = "https://lrclib.net"
	neteaseBaseURL = "http://music.163.com"
)

// sourceTimeout limits each Source that Lookup calls.
const sourceTimeout = 10 * time.Second

// maxResponseBody limits API responses to 2 MB.
const maxResponseBody = 2 << 20

type lrcResponse struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
}

type ncmSearchResponse struct {
	Result struct {
		Songs []struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
			Dt   int    `json:"dt"` // duration in ms
		} `json:"songs"`
	} `json:"result"`
}

type ncmLyricResponse struct {
	Lrc struct {
		Lyric string `json:"lyric"`
	} `json:"lrc"`
}

var lrcRegex = regexp.MustCompile(`\[(\d{2,}):(\d{2})\.(\d{2,3})\](.*)`)

// cleanQuery strips noise from a search query: bracketed text like "[Official Video]",
// parenthesized text like "(Lyric Video)", and common video/audio label suffixes.
//
// The label words are only stripped when they form a genuine trailing label
// (after a dash, or as an "official video/audio" / "lyric(s) video" phrase),
// never as a bare substring. Otherwise legitimate titles like "Videotape",
// "Audioslave", or "Video Games" would be erased.
var noiseRegex = regexp.MustCompile(`(?i)(?:` +
	`\[.*?\]` + // [Official Video]
	`|\(.*?\)` + // (Lyric Video)
	`|\s*-\s*(?:official|lyric|audio|video).*` + // - Official Video
	`|\s+official(?:\s+music)?\s+(?:video|audio).*` + // Official Music Video
	`|\s+lyrics?\s+video.*` + // Lyric Video / Lyrics Video
	`)`)

func cleanQuery(str string) string {
	s := noiseRegex.ReplaceAllString(str, "")
	return strings.TrimSpace(s)
}

// Source finds the lyrics of one track in one place, such as the API of the
// provider that owns the track. It returns ErrNotFound when it has none.
type Source func(ctx context.Context) ([]Line, error)

// Lookup returns the lyrics of a track. It tries the embedded lyrics first,
// then each source in order, then LRCLIB and NetEase with artist and title.
// A source that fails is skipped. When no step finds lyrics, Lookup returns
// the last LRCLIB or NetEase error, so a network failure does not read as
// ErrNotFound. When both answer that they have none, it returns ErrNotFound.
func Lookup(ctx context.Context, embedded, artist, title string, sources ...Source) ([]Line, error) {
	return LookupWith(ctx, embedded, artist, title, Hint{}, sources...)
}

// LookupWith is Lookup with the album and the duration of the recording,
// which let LRCLIB match it exactly and keep out lyrics of another version.
func LookupWith(ctx context.Context, embedded, artist, title string, hint Hint, sources ...Source) ([]Line, error) {
	if lines := ParseEmbedded(embedded); len(lines) > 0 {
		return lines, nil
	}
	for _, source := range sources {
		sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
		lines, err := source(sourceCtx)
		cancel()
		if err == nil && len(lines) > 0 {
			return lines, nil
		}
	}
	return fetchHint(ctx, artist, title, hint)
}

// fetch requests lyrics for the given artist and title.
// It tries LRCLIB first, then falls back to NetEase Cloud Music.
//
// For YouTube/SoundCloud tracks where Artist is the uploader and Title
// contains "Artist - Song", the title is split to build a better query.
func fetch(ctx context.Context, artist, title string) ([]Line, error) {
	return fetchHint(ctx, artist, title, Hint{})
}

func fetchHint(ctx context.Context, artist, title string, hint Hint) ([]Line, error) {
	if artist == "" && title == "" {
		return nil, ErrNotFound
	}

	// YouTube titles often embed the real artist: "Artist - Song (Official Video)".
	// If the title contains " - ", prefer that split over the uploader name.
	if a, t, ok := strings.Cut(title, " - "); ok {
		a = cleanQuery(strings.TrimSpace(a))
		t = cleanQuery(strings.TrimSpace(t))
		if a != "" && t != "" {
			artist = a
			title = t
		}
	}

	query := cleanQuery(artist) + " " + cleanQuery(title)
	query = strings.TrimSpace(query)
	if query == "" {
		query = artist + " " + title
	}

	// An exact match on title, artist, album and duration is the most
	// reliable way to get the lyrics of this very recording.
	if hint.Duration > 0 {
		if lines, err := fetchLRCLIBExact(ctx, artist, title, hint); err == nil && len(lines) > 0 {
			return lines, nil
		}
	}
	lrclib := func(ctx context.Context, query string) ([]Line, error) {
		return fetchLRCLIBScored(ctx, query, hint.Duration)
	}
	var lastErr error
	for _, source := range []func(context.Context, string) ([]Line, error){lrclib, fetchNetEase} {
		lines, err := source(ctx, query)
		if err == nil && len(lines) > 0 {
			return lines, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNotFound
}

// ParseEmbedded converts lyrics read from local file tags into display lines.
// Timestamped LRC data remains synced; plain text is returned as scrollable
// lines at timestamp 0.
func ParseEmbedded(data string) []Line {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil
	}
	lines := parseLRC(data)
	if len(lines) > 0 {
		return lines
	}
	for raw := range strings.SplitSeq(data, "\n") {
		lines = append(lines, Line{Start: 0, Text: strings.TrimSpace(raw)})
	}
	return lines
}

// fetchLRCLIBExact asks LRCLIB for the recording with this title, artist,
// album and duration (it accepts a few seconds of difference).
func fetchLRCLIBExact(ctx context.Context, artist, title string, hint Hint) ([]Line, error) {
	q := url.Values{}
	q.Set("track_name", title)
	q.Set("artist_name", artist)
	if hint.Album != "" {
		q.Set("album_name", hint.Album)
	}
	q.Set("duration", strconv.Itoa(int(hint.Duration.Round(time.Second).Seconds())))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lrclibBaseURL+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := doLRCLIB(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, ErrNotFound
	}
	var r lrcResponse
	if err := httpclient.ReadJSON(resp.Body, maxResponseBody, &r); err != nil {
		return nil, err
	}
	if r.SyncedLyrics != "" {
		return parseLRC(r.SyncedLyrics), nil
	}
	return plainLines(r.PlainLyrics), nil
}

func plainLines(text string) []Line {
	var lines []Line
	for raw := range strings.SplitSeq(text, "\n") {
		lines = append(lines, Line{Start: 0, Text: strings.TrimSpace(raw)})
	}
	if len(lines) == 1 && lines[0].Text == "" {
		return nil
	}
	return lines
}

// fetchLRCLIBScored searches LRCLIB and picks the synced result closest in
// duration. With a known duration, results more than 8 s away belong to
// another version and are left out.
func fetchLRCLIBScored(ctx context.Context, query string, duration time.Duration) ([]Line, error) {
	if duration <= 0 {
		return fetchLRCLIB(ctx, query)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lrclibBaseURL+"/api/search?q="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	resp, err := doLRCLIB(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lrclib: %s", resp.Status)
	}
	var results []lrcResponse
	if err := httpclient.ReadJSON(resp.Body, maxResponseBody, &results); err != nil {
		return nil, err
	}
	best, bestScore := -1, 1e9
	want := duration.Seconds()
	for i, r := range results {
		diff := want - r.Duration
		if diff < 0 {
			diff = -diff
		}
		if r.Duration > 0 && diff > 8 {
			continue
		}
		score := diff
		if r.SyncedLyrics == "" {
			score += 20
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return nil, ErrNotFound
	}
	if r := results[best]; r.SyncedLyrics != "" {
		return parseLRC(r.SyncedLyrics), nil
	}
	if lines := plainLines(results[best].PlainLyrics); len(lines) > 0 {
		return lines, nil
	}
	return nil, ErrNotFound
}

func fetchLRCLIB(ctx context.Context, query string) ([]Line, error) {
	searchURL := lrclibBaseURL + "/api/search?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := doLRCLIB(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lrclib: %s", resp.Status)
	}

	var results []lrcResponse
	if err := httpclient.ReadJSON(resp.Body, maxResponseBody, &results); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	// Prefer synced lyrics.
	for _, r := range results {
		if r.SyncedLyrics != "" {
			return parseLRC(r.SyncedLyrics), nil
		}
	}

	// Fallback to plain lyrics (all lines at timestamp 0).
	if results[0].PlainLyrics != "" {
		var lines []Line
		for raw := range strings.SplitSeq(results[0].PlainLyrics, "\n") {
			lines = append(lines, Line{Start: 0, Text: strings.TrimSpace(raw)})
		}
		return lines, nil
	}

	return nil, ErrNotFound
}

func fetchNetEase(ctx context.Context, query string) ([]Line, error) {
	return fetchNetEaseMatch(ctx, query, "", 0)
}

// fetchNetEaseMatch searches NetEase Cloud Music and takes the song whose
// title and duration match best. The cloudsearch endpoint answers where the
// older web search is rate limited.
func fetchNetEaseMatch(ctx context.Context, query, title string, duration time.Duration) ([]Line, error) {
	searchURL := neteaseBaseURL + "/api/cloudsearch/pc?type=1&limit=8&s=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://music.163.com")
	req.Header.Set("User-Agent", browserUA)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease: %s", resp.Status)
	}

	var searchRes ncmSearchResponse
	if err := httpclient.ReadJSON(resp.Body, maxResponseBody, &searchRes); err != nil {
		// NetEase answers some searches with a result that is not a song
		// list, such as a string. That reply has no song for the query.
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	best, bestScore := -1, math.MaxFloat64
	for i, song := range searchRes.Result.Songs {
		score := float64(i)
		if title != "" && !sameTitle(song.Name, title) {
			score += 50
		}
		if duration > 0 && song.Dt > 0 {
			diff := math.Abs(duration.Seconds() - float64(song.Dt)/1000)
			if diff > 10 {
				continue
			}
			score += diff
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 || (title != "" && bestScore >= 50) {
		return nil, ErrNotFound
	}

	songID := searchRes.Result.Songs[best].Id
	lyricURL := fmt.Sprintf("%s/api/song/lyric?id=%d&lv=1&kv=1&tv=-1", neteaseBaseURL, songID)

	lreq, err := http.NewRequestWithContext(ctx, http.MethodGet, lyricURL, nil)
	if err != nil {
		return nil, err
	}
	lreq.Header.Set("Referer", "https://music.163.com")
	lreq.Header.Set("User-Agent", browserUA)
	lresp, err := httpClient.Do(lreq)
	if err != nil {
		return nil, err
	}
	defer lresp.Body.Close()

	if lresp.StatusCode != 200 {
		return nil, fmt.Errorf("netease: %s", lresp.Status)
	}

	var lyricRes ncmLyricResponse
	if err := httpclient.ReadJSON(lresp.Body, maxResponseBody, &lyricRes); err != nil {
		return nil, err
	}

	lines := dropCredits(parseLRC(lyricRes.Lrc.Lyric))
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

// parseLRC converts standard LRC string blocks into a slice of timestamped Lines.
func parseLRC(data string) []Line {
	var lines []Line
	for raw := range strings.SplitSeq(data, "\n") {
		matches := lrcRegex.FindStringSubmatch(raw)
		if len(matches) == 5 {
			mins, _ := strconv.Atoi(matches[1])
			secs, _ := strconv.Atoi(matches[2])
			ms, _ := strconv.Atoi(matches[3])

			// If LRC millisecond part is hundredths (2 chars), scale it.
			if len(matches[3]) == 2 {
				ms *= 10
			}

			start := time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second + time.Duration(ms)*time.Millisecond
			text, words := parseWords(strings.TrimSpace(matches[4]))
			lines = append(lines, Line{Start: start, Text: text, Words: words})
		}
	}
	return lines
}

var wordTag = regexp.MustCompile(`<(\d{2,}):(\d{2})\.(\d{2,3})>`)

// parseWords reads the "<mm:ss.xx>" word tags of enhanced LRC. It returns
// the line without tags and the timed words, or nil words without tags.
func parseWords(text string) (string, []Word) {
	idx := wordTag.FindAllStringSubmatchIndex(text, -1)
	if len(idx) == 0 {
		return text, nil
	}
	var words []Word
	var plain strings.Builder
	plain.WriteString(text[:idx[0][0]])
	for i, m := range idx {
		mins, _ := strconv.Atoi(text[m[2]:m[3]])
		secs, _ := strconv.Atoi(text[m[4]:m[5]])
		ms, _ := strconv.Atoi(text[m[6]:m[7]])
		if m[7]-m[6] == 2 {
			ms *= 10
		}
		end := len(text)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		word := text[m[1]:end]
		plain.WriteString(word)
		if w := strings.TrimSpace(word); w != "" {
			words = append(words, Word{
				Start: time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second + time.Duration(ms)*time.Millisecond,
				Text:  w,
			})
		}
	}
	return strings.TrimSpace(plain.String()), words
}
