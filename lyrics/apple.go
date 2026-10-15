package lyrics

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bjarneo/cliamp/internal/httpclient"
)

// Apple Music lyrics are the most complete synced lyrics, timed per word.
// Three community services make them reachable without an account:
// LyricsPlus (the backend of YouLy+), lrc.red and Paxsenix.

var (
	lyricsPlusServers = []string{"https://lyricsplus.binimum.org", "https://lyricsplus.prjktla.my.id"}
	lrcRedBaseURL     = "https://lrc.red"
	paxsenixBaseURL   = "https://lyrics.paxsenix.org"
	itunesBaseURL     = "https://itunes.apple.com"
)

// syllable is a timed piece of a line; pieces join into words where the
// text has no space between them.
type syllable struct {
	start time.Duration
	text  string
}

// lineFromSyllables builds a line whose words carry the start of their
// first syllable, one word per field of the text, as the karaoke expects.
func lineFromSyllables(start time.Duration, sylls []syllable) Line {
	var full strings.Builder
	var offsets []int
	for _, s := range sylls {
		offsets = append(offsets, full.Len())
		full.WriteString(s.text)
	}
	text := full.String()
	line := Line{Start: start, Text: strings.Join(strings.Fields(text), " ")}
	inWord := false
	si := 0
	for i, r := range text {
		for si+1 < len(offsets) && offsets[si+1] <= i {
			si++
		}
		if unicode.IsSpace(r) {
			inWord = false
			continue
		}
		if !inWord {
			inWord = true
			at := start
			if si < len(sylls) {
				at = sylls[si].start
			}
			line.Words = append(line.Words, Word{Start: at})
		}
		line.Words[len(line.Words)-1].Text += string(r)
	}
	if len(line.Words) == 0 {
		line.Words = nil
	}
	return line
}

// fetchLyricsPlus asks the public LyricsPlus servers, which gather Apple
// Music, QQ and Musixmatch lyrics.
func fetchLyricsPlus(ctx context.Context, artist, title string, hint Hint) ([]Line, error) {
	q := url.Values{}
	q.Set("title", title)
	q.Set("artist", artist)
	if hint.Album != "" {
		q.Set("album", hint.Album)
	}
	if hint.Duration > 0 {
		q.Set("duration", strconv.Itoa(int(hint.Duration.Seconds())))
	}
	var lastErr error = ErrNotFound
	for _, server := range lyricsPlusServers {
		var r struct {
			Type   string `json:"type"`
			Lyrics []struct {
				Time     float64 `json:"time"`
				Text     string  `json:"text"`
				Syllabus []struct {
					Time float64 `json:"time"`
					Text string  `json:"text"`
				} `json:"syllabus"`
			} `json:"lyrics"`
		}
		err := getJSON(ctx, server+"/v2/lyrics/get?"+q.Encode(), &r)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				lastErr = ErrNotFound
			} else {
				lastErr = err
			}
			continue
		}
		var lines []Line
		for _, l := range r.Lyrics {
			start := time.Duration(l.Time) * time.Millisecond
			if len(l.Syllabus) > 0 {
				sylls := make([]syllable, len(l.Syllabus))
				for i, s := range l.Syllabus {
					sylls[i] = syllable{time.Duration(s.Time) * time.Millisecond, s.Text}
				}
				lines = append(lines, lineFromSyllables(start, sylls))
				continue
			}
			lines = append(lines, Line{Start: start, Text: strings.TrimSpace(l.Text)})
		}
		if strings.EqualFold(r.Type, "none") || len(lines) == 0 {
			continue
		}
		return lines, nil
	}
	return nil, lastErr
}

// fetchLRCRed searches lrc.red, which serves Apple Music TTML by ISRC.
func fetchLRCRed(ctx context.Context, artist, title string, duration time.Duration) ([]Line, error) {
	var r struct {
		Results []struct {
			TrackName  string  `json:"track_name"`
			ArtistName string  `json:"artist_name"`
			Duration   float64 `json:"duration"`
			LyricsURL  string  `json:"lyricsUrl"`
		} `json:"results"`
	}
	if err := getJSON(ctx, lrcRedBaseURL+"/api/v1?q="+url.QueryEscape(strings.TrimSpace(title+" "+artist)), &r); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	best, bestDiff := -1, 1e9
	for i, c := range r.Results {
		if !sameTitle(c.TrackName, title) || !sameArtist(c.ArtistName, artist) {
			continue
		}
		diff := 0.0
		if duration > 0 && c.Duration > 0 {
			diff = absf(duration.Seconds() - c.Duration)
			if diff > 6 {
				continue
			}
		}
		if diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	if best < 0 || !strings.HasPrefix(r.Results[best].LyricsURL, "https://") {
		return nil, ErrNotFound
	}
	doc, err := getText(ctx, r.Results[best].LyricsURL)
	if err != nil {
		return nil, err
	}
	lines := parseTTML(doc)
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

// fetchPaxsenix finds the Apple Music track with the iTunes search and
// asks Paxsenix for its lyrics.
func fetchPaxsenix(ctx context.Context, artist, title string, duration time.Duration) ([]Line, error) {
	id, err := itunesTrackID(ctx, artist, title, duration)
	if err != nil {
		return nil, err
	}
	var r struct {
		Content []struct {
			Timestamp float64 `json:"timestamp"`
			Text      []struct {
				Text      string  `json:"text"`
				Timestamp float64 `json:"timestamp"`
				Part      bool    `json:"part"`
			} `json:"text"`
		} `json:"content"`
	}
	if err := getJSON(ctx, paxsenixBaseURL+"/apple-music/lyrics?id="+strconv.FormatInt(id, 10), &r); err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "403") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var lines []Line
	for _, l := range r.Content {
		var sylls []syllable
		for _, w := range l.Text {
			text := w.Text
			if !w.Part {
				text += " "
			}
			sylls = append(sylls, syllable{time.Duration(w.Timestamp) * time.Millisecond, text})
		}
		lines = append(lines, lineFromSyllables(time.Duration(l.Timestamp)*time.Millisecond, sylls))
	}
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

// itunesTrackID finds the Apple Music id of a song with the public iTunes
// search, matching title, artist and duration.
func itunesTrackID(ctx context.Context, artist, title string, duration time.Duration) (int64, error) {
	var r struct {
		Results []struct {
			TrackID    int64   `json:"trackId"`
			TrackName  string  `json:"trackName"`
			ArtistName string  `json:"artistName"`
			TimeMillis float64 `json:"trackTimeMillis"`
		} `json:"results"`
	}
	u := itunesBaseURL + "/search?entity=song&limit=10&term=" + url.QueryEscape(strings.TrimSpace(artist+" "+title))
	if err := getJSON(ctx, u, &r); err != nil {
		return 0, err
	}
	var best int64
	bestDiff := 1e9
	for _, c := range r.Results {
		if !sameTitle(c.TrackName, title) || !sameArtist(c.ArtistName, artist) {
			continue
		}
		diff := 0.0
		if duration > 0 && c.TimeMillis > 0 {
			diff = absf(duration.Seconds() - c.TimeMillis/1000)
			if diff > 6 {
				continue
			}
		}
		if diff < bestDiff {
			best, bestDiff = c.TrackID, diff
		}
	}
	if best == 0 {
		return 0, ErrNotFound
	}
	return best, nil
}

// sameArtist reports whether two artist credits share the main artist.
func sameArtist(a, b string) bool {
	if b == "" {
		return true
	}
	na, nb := normalize(a), normalize(b)
	return na != "" && nb != "" && (strings.Contains(na, nb) || strings.Contains(nb, na))
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func getText(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ErrNotFound
	}
	data, err := httpclient.ReadBody(resp.Body, maxResponseBody)
	return string(data), err
}
