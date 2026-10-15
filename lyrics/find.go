package lyrics

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bjarneo/cliamp/internal/httpclient"
)

// The sources Find can be asked for by name. All of them are free and need
// no account.
const (
	SourceAuto       = ""
	SourceLRCLIB     = "lrclib"
	SourceNetEase    = "netease"
	SourceKugou      = "kugou"
	SourceQQ         = "qq"
	SourceUnison     = "unison"
	SourceOVH        = "lyricsovh"
	SourceLyricsPlus = "lyricsplus"
	SourceLRCRed     = "lrcred"
	SourcePaxsenix   = "paxsenix"
)

// Sources lists the named sources in the order Find tries them. Musixmatch
// is left out on purpose: without an account it answers with decoy lyrics.
// Word-timed Apple Music lyrics come first, then the line-synced catalogs,
// then the plain ones.
var Sources = []string{SourceLyricsPlus, SourceLRCRed, SourcePaxsenix, SourceLRCLIB, SourceUnison, SourceNetEase, SourceQQ, SourceKugou, SourceOVH}

// SourceName is the display name of a source.
func SourceName(source string) string {
	switch source {
	case SourceLRCLIB:
		return "LRCLIB"
	case SourceNetEase:
		return "NetEase"
	case SourceKugou:
		return "Kugou"
	case SourceQQ:
		return "QQ Music"
	case SourceUnison:
		return "BetterLyrics Unison"
	case SourceOVH:
		return "lyrics.ovh"
	case SourceLyricsPlus:
		return "LyricsPlus"
	case SourceLRCRed:
		return "lrc.red"
	case SourcePaxsenix:
		return "Paxsenix (Apple Music)"
	case "embedded":
		return "file tags"
	case "provider":
		return "service"
	}
	return source
}

// Options picks how Find looks.
type Options struct {
	// Source limits the search to one named source; empty tries them all.
	Source string
	// Refresh skips the cache, for a lyric that came out wrong.
	Refresh bool
}

// Result is the lyrics found and where.
type Result struct {
	Lines  []Line
	Source string
}

var (
	kugouBaseURL  = "https://lyrics.kugou.com"
	qqBaseURL     = "https://c.y.qq.com"
	unisonBaseURL = "https://unison.betterlyrics.org"
	ovhBaseURL    = "https://api.lyrics.ovh"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// Find returns the lyrics of a track like LookupWith, and also tries
// Kugou, remembers the answer (on disk when found), prefers synced lyrics
// over plain ones from an earlier source, and can be limited to a source.
func Find(ctx context.Context, embedded, artist, title string, hint Hint, opts Options, sources ...Source) (Result, error) {
	if opts.Source == SourceAuto {
		if lines := ParseEmbedded(embedded); len(lines) > 0 {
			return Result{Lines: lines, Source: "embedded"}, nil
		}
		for _, source := range sources {
			sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
			lines, err := source(sourceCtx)
			cancel()
			if err == nil && len(lines) > 0 {
				return Result{Lines: lines, Source: "provider"}, nil
			}
		}
	}
	artist, title = queryParts(artist, title)
	if artist == "" && title == "" {
		return Result{}, ErrNotFound
	}
	key := cacheKey(artist, title, hint, opts.Source)
	if !opts.Refresh {
		if r, err, ok := cache.get(key); ok {
			return r, err
		}
	}
	r, err := findOnline(ctx, artist, title, hint, opts.Source)
	if ctx.Err() == nil {
		cache.put(key, r, err)
	}
	return r, err
}

// findOnline asks every source at once and keeps the best answer: lyrics
// timed per word over lyrics timed per line over plain text, and the
// earlier source of Sources between equals. It stops waiting soon after a
// word-timed answer, so one slow server does not hold the lyrics back.
func findOnline(ctx context.Context, artist, title string, hint Hint, only string) (Result, error) {
	query := strings.TrimSpace(artist + " " + title)
	fetchers := map[string]func(context.Context) ([]Line, error){
		SourceLyricsPlus: func(ctx context.Context) ([]Line, error) { return fetchLyricsPlus(ctx, artist, title, hint) },
		SourceLRCRed:     func(ctx context.Context) ([]Line, error) { return fetchLRCRed(ctx, artist, title, hint.Duration) },
		SourcePaxsenix:   func(ctx context.Context) ([]Line, error) { return fetchPaxsenix(ctx, artist, title, hint.Duration) },
		SourceLRCLIB: func(ctx context.Context) ([]Line, error) {
			if only == SourceAuto && lrclibCoolingDown() {
				// LRCLIB asked to slow down: the other sources answer now.
				return nil, ErrNotFound
			}
			if hint.Duration > 0 {
				if lines, err := fetchLRCLIBExact(ctx, artist, title, hint); err == nil && len(lines) > 0 {
					return lines, nil
				}
			}
			return fetchLRCLIBScored(ctx, query, hint.Duration)
		},
		SourceUnison:  func(ctx context.Context) ([]Line, error) { return fetchUnison(ctx, artist, title, hint) },
		SourceNetEase: func(ctx context.Context) ([]Line, error) { return fetchNetEaseMatch(ctx, query, title, hint.Duration) },
		SourceQQ:      func(ctx context.Context) ([]Line, error) { return fetchQQ(ctx, artist, title) },
		SourceKugou:   func(ctx context.Context) ([]Line, error) { return fetchKugou(ctx, artist, title, hint.Duration) },
		SourceOVH:     func(ctx context.Context) ([]Line, error) { return fetchOVH(ctx, artist, title) },
	}
	type answer struct {
		rank  int // position in Sources
		lines []Line
		err   error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	answers := make(chan answer, len(Sources))
	asked := 0
	for rank, name := range Sources {
		if only != SourceAuto && name != only {
			continue
		}
		fetch := fetchers[name]
		asked++
		go func() {
			stepCtx, stop := context.WithTimeout(ctx, sourceTimeout)
			defer stop()
			lines, err := fetch(stepCtx)
			answers <- answer{rank, lines, err}
		}()
	}
	if asked == 0 {
		return Result{}, fmt.Errorf("unknown lyrics source %q", only)
	}
	best, bestQ := -1, 0
	var bestLines []Line
	var lastErr error
	var grace <-chan time.Time
	for got := 0; got < asked; {
		select {
		case a := <-answers:
			got++
			if a.err != nil {
				if !errors.Is(a.err, ErrNotFound) {
					lastErr = fmt.Errorf("%s: %w", SourceName(Sources[a.rank]), a.err)
				}
				continue
			}
			q := quality(a.lines)
			if q == 0 {
				continue
			}
			if q > bestQ || (q == bestQ && a.rank < best) {
				best, bestQ, bestLines = a.rank, q, a.lines
			}
			if bestQ == 3 && grace == nil {
				grace = time.After(1500 * time.Millisecond)
			}
		case <-grace:
			got = asked
		case <-ctx.Done():
			got = asked
		}
	}
	if best >= 0 {
		return Result{Lines: bestLines, Source: Sources[best]}, nil
	}
	if lastErr != nil {
		return Result{}, lastErr
	}
	return Result{}, ErrNotFound
}

// quality ranks lyrics: 3 timed per word, 2 per line, 1 plain, 0 empty.
func quality(lines []Line) int {
	if len(lines) == 0 {
		return 0
	}
	if !synced(lines) {
		return 1
	}
	for _, l := range lines {
		if len(l.Words) > 1 {
			return 3
		}
	}
	return 2
}

func synced(lines []Line) bool {
	for _, l := range lines {
		if l.Start > 0 {
			return true
		}
	}
	return false
}

var featRegex = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring|with)\s+.*$`)

// queryParts turns the artist and title of a track into a clean search:
// "Artist - Song (Official Video)" titles give the real artist, the
// "- Topic" of YouTube channels and the featured artists are dropped.
func queryParts(artist, title string) (string, string) {
	if a, t, ok := strings.Cut(title, " - "); ok {
		a = cleanQuery(strings.TrimSpace(a))
		t = cleanQuery(strings.TrimSpace(t))
		if a != "" && t != "" {
			artist, title = a, t
		}
	}
	artist = strings.TrimSuffix(strings.TrimSpace(artist), " - Topic")
	if a, _, ok := strings.Cut(artist, ","); ok {
		artist = a
	}
	artist = featRegex.ReplaceAllString(cleanQuery(artist), "")
	title = featRegex.ReplaceAllString(cleanQuery(title), "")
	return strings.TrimSpace(artist), strings.TrimSpace(title)
}

// normalize keeps letters and digits in lower case, for comparing titles.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sameTitle reports whether two titles name the same song, ignoring case,
// punctuation and a version in brackets.
func sameTitle(a, b string) bool {
	na, nb := normalize(cleanQuery(a)), normalize(cleanQuery(b))
	if na == "" || nb == "" {
		return false
	}
	return na == nb || strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na)
}

// creditRegex matches the credit lines NetEase and Kugou put before the
// lyrics, such as "作词 : Name" or "Composer: Name".
var creditRegex = regexp.MustCompile(`^\s*(?:作词|作曲|编曲|制作人|制作|监制|混音|母带|和声|吉他|贝斯|鼓|录音|出品|发行|词|曲|Lyricist|Lyrics by|Composer|Composed by|Producer|Produced by|Arranger|Written by|Mixing|Mastering)\s*[:：]`)

func dropCredits(lines []Line) []Line {
	out := lines[:0]
	for _, l := range lines {
		if creditRegex.MatchString(l.Text) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// fetchKugou searches the Kugou lyrics service, which has a large catalog
// of synced lyrics and needs no key.
func fetchKugou(ctx context.Context, artist, title string, duration time.Duration) ([]Line, error) {
	keywords := []string{artist + " - " + title, title}
	if artist == "" {
		keywords = []string{title}
	}
	for _, kw := range keywords {
		u := kugouBaseURL + "/search?ver=1&man=yes&client=pc&keyword=" + url.QueryEscape(kw)
		var res struct {
			Candidates []struct {
				ID        json.Number `json:"id"`
				AccessKey string      `json:"accesskey"`
				Song      string      `json:"song"`
				Singer    string      `json:"singer"`
				Duration  int         `json:"duration"` // ms
			} `json:"candidates"`
		}
		if err := getJSON(ctx, u, &res); err != nil {
			return nil, err
		}
		best, bestScore := -1, math.MaxFloat64
		for i, c := range res.Candidates {
			if !sameTitle(c.Song, title) {
				continue
			}
			score := float64(i)
			if duration > 0 && c.Duration > 0 {
				diff := math.Abs(duration.Seconds() - float64(c.Duration)/1000)
				if diff > 10 {
					continue
				}
				score += diff
			}
			if artist != "" && !strings.Contains(normalize(c.Singer), normalize(artist)) {
				score += 20
			}
			if score < bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			continue
		}
		c := res.Candidates[best]
		var dl struct {
			Content string `json:"content"`
		}
		du := fmt.Sprintf("%s/download?ver=1&client=pc&id=%s&accesskey=%s&fmt=lrc&charset=utf8", kugouBaseURL, url.QueryEscape(c.ID.String()), url.QueryEscape(c.AccessKey))
		if err := getJSON(ctx, du, &dl); err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(dl.Content)
		if err != nil {
			return nil, ErrNotFound
		}
		if lines := dropCredits(parseLRC(string(raw))); len(lines) > 0 {
			return lines, nil
		}
	}
	return nil, ErrNotFound
}

func getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return httpclient.ReadJSON(resp.Body, maxResponseBody, out)
}

// lrclibCooldown holds LRCLIB off after it asked to slow down.
var lrclibCooldown struct {
	sync.Mutex
	until time.Time
}

func lrclibCoolingDown() bool {
	lrclibCooldown.Lock()
	defer lrclibCooldown.Unlock()
	return time.Now().Before(lrclibCooldown.until)
}

// doLRCLIB sends a request to LRCLIB. When LRCLIB answers 429 it waits the
// Retry-After it asks for (a second or two) and tries again, instead of
// reporting the track as having no lyrics.
func doLRCLIB(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		lrclibCooldown.Lock()
		wait := time.Until(lrclibCooldown.until)
		lrclibCooldown.Unlock()
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests || attempt == 1 {
			return resp, err
		}
		delay := 2 * time.Second
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			delay = min(time.Duration(secs)*time.Second, 5*time.Second)
		}
		resp.Body.Close()
		lrclibCooldown.Lock()
		lrclibCooldown.until = time.Now().Add(delay)
		lrclibCooldown.Unlock()
	}
}

// The cache keeps answers for the session and found lyrics on disk, so a
// track played again, or opened in two views, asks no server.
var cache = &lyricCache{mem: map[string]cacheEntry{}, dir: lyricsCacheDir()}

type cacheEntry struct {
	r   Result
	err error
	at  time.Time
}

type lyricCache struct {
	mu    sync.Mutex
	mem   map[string]cacheEntry
	order []string
	dir   string
}

const (
	cacheKeep     = 256
	cacheMissKeep = 20 * time.Minute
)

func lyricsCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kalulu", "lyrics")
}

func cacheKey(artist, title string, hint Hint, source string) string {
	return normalize(artist) + "|" + normalize(title) + "|" + strconv.Itoa(int(hint.Duration.Seconds()/3)) + "|" + source
}

func (c *lyricCache) file(key string) string {
	if c.dir == "" {
		return ""
	}
	sum := sha1.Sum([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

type diskLyrics struct {
	Source string `json:"source"`
	Lines  []Line `json:"lines"`
}

func (c *lyricCache) get(key string) (Result, error, bool) {
	c.mu.Lock()
	e, ok := c.mem[key]
	c.mu.Unlock()
	if ok {
		if e.err != nil && time.Since(e.at) > cacheMissKeep {
			return Result{}, nil, false
		}
		return e.r, e.err, true
	}
	if f := c.file(key); f != "" {
		if data, err := os.ReadFile(f); err == nil {
			var d diskLyrics
			if json.Unmarshal(data, &d) == nil && len(d.Lines) > 0 {
				r := Result{Lines: d.Lines, Source: d.Source}
				c.remember(key, cacheEntry{r: r, at: time.Now()})
				return r, nil, true
			}
		}
	}
	return Result{}, nil, false
}

func (c *lyricCache) put(key string, r Result, err error) {
	// Only "none found" is remembered as a failure: a network error may
	// be gone at the next try.
	if err != nil && !errors.Is(err, ErrNotFound) {
		return
	}
	c.remember(key, cacheEntry{r: r, err: err, at: time.Now()})
	if err == nil && len(r.Lines) > 0 {
		if f := c.file(key); f != "" && os.MkdirAll(c.dir, 0o755) == nil {
			if data, err := json.Marshal(diskLyrics{Source: r.Source, Lines: r.Lines}); err == nil {
				_ = os.WriteFile(f, data, 0o644)
			}
		}
	}
}

func (c *lyricCache) remember(key string, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.mem[key]; !ok {
		c.order = append(c.order, key)
	}
	c.mem[key] = e
	for len(c.order) > cacheKeep {
		delete(c.mem, c.order[0])
		c.order = c.order[1:]
	}
}

// fetchUnison asks Unison, the community lyrics of the Better Lyrics
// extension for YouTube Music. It knows tracks by their YouTube video.
func fetchUnison(ctx context.Context, artist, title string, hint Hint) ([]Line, error) {
	q := url.Values{}
	if hint.VideoID != "" {
		q.Set("v", hint.VideoID)
	}
	q.Set("song", title)
	q.Set("artist", artist)
	if hint.Duration > 0 {
		q.Set("duration", strconv.Itoa(int(hint.Duration.Seconds())))
	}
	if hint.Album != "" {
		q.Set("album", hint.Album)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, unisonBaseURL+"/lyrics?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("x-key-id", "kalulu")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	var r struct {
		Data struct {
			Lyrics string `json:"lyrics"`
			Format string `json:"format"`
		} `json:"data"`
	}
	if err := httpclient.ReadJSON(resp.Body, maxResponseBody, &r); err != nil {
		return nil, err
	}
	var lines []Line
	switch r.Data.Format {
	case "lrc":
		lines = parseLRC(r.Data.Lyrics)
	case "ttml":
		lines = parseTTML(r.Data.Lyrics)
	case "plain":
		lines = plainLines(r.Data.Lyrics)
	}
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

// fetchQQ searches QQ Music, whose catalog has synced lyrics for much
// international and Asian music.
func fetchQQ(ctx context.Context, artist, title string) ([]Line, error) {
	var res struct {
		Data struct {
			Song struct {
				Itemlist []struct {
					Mid    string `json:"mid"`
					Name   string `json:"name"`
					Singer string `json:"singer"`
				} `json:"itemlist"`
			} `json:"song"`
		} `json:"data"`
	}
	u := qqBaseURL + "/splcloud/fcgi-bin/smartbox_new.fcg?format=json&key=" + url.QueryEscape(strings.TrimSpace(artist+" "+title))
	if err := getJSONRef(ctx, u, "https://y.qq.com/", &res); err != nil {
		return nil, err
	}
	mid := ""
	for _, it := range res.Data.Song.Itemlist {
		if !sameTitle(it.Name, title) {
			continue
		}
		if artist == "" || strings.Contains(normalize(it.Singer), normalize(artist)) || strings.Contains(normalize(artist), normalize(it.Singer)) {
			mid = it.Mid
			break
		}
	}
	if mid == "" {
		return nil, ErrNotFound
	}
	var lyr struct {
		Lyric string `json:"lyric"`
	}
	if err := getJSONRef(ctx, qqBaseURL+"/lyric/fcgi-bin/fcg_query_lyric_new.fcg?format=json&nobase64=1&songmid="+url.QueryEscape(mid), "https://y.qq.com/", &lyr); err != nil {
		return nil, err
	}
	lines := dropCredits(parseLRC(html.UnescapeString(lyr.Lyric)))
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

// fetchOVH is the last resort: lyrics.ovh has plain lyrics only.
func fetchOVH(ctx context.Context, artist, title string) ([]Line, error) {
	if artist == "" || title == "" {
		return nil, ErrNotFound
	}
	var r struct {
		Lyrics string `json:"lyrics"`
	}
	u := ovhBaseURL + "/v1/" + url.PathEscape(artist) + "/" + url.PathEscape(title)
	if err := getJSON(ctx, u, &r); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	lines := plainLines(strings.ReplaceAll(r.Lyrics, "\r", ""))
	if len(lines) == 0 {
		return nil, ErrNotFound
	}
	return lines, nil
}

func getJSONRef(ctx context.Context, u, referer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Referer", referer)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return httpclient.ReadJSON(resp.Body, maxResponseBody, out)
}

var (
	ttmlP    = regexp.MustCompile(`(?s)<p\b([^>]*)>(.*?)</p>`)
	ttmlLeaf = regexp.MustCompile(`<span\b[^>]*?\bbegin="([^"]+)"[^>]*>([^<]*)</span>`)
	ttmlBeg  = regexp.MustCompile(`\bbegin="([^"]+)"`)
	ttmlTag  = regexp.MustCompile(`<[^>]+>`)
)

// parseTTML reads the lines, and the word timings when present, of a TTML
// lyric document as Apple Music and Unison write them. Timed spans are
// syllables: they form one word unless the document has a space between.
func parseTTML(doc string) []Line {
	var lines []Line
	for _, m := range ttmlP.FindAllStringSubmatch(doc, -1) {
		b := ttmlBeg.FindStringSubmatch(m[1])
		if b == nil {
			continue
		}
		start := ttmlTime(b[1])
		body := m[2]
		leaves := ttmlLeaf.FindAllStringSubmatchIndex(body, -1)
		if len(leaves) == 0 {
			text := strings.Join(strings.Fields(html.UnescapeString(ttmlTag.ReplaceAllString(body, " "))), " ")
			lines = append(lines, Line{Start: start, Text: text})
			continue
		}
		var sylls []syllable
		for k, lf := range leaves {
			text := html.UnescapeString(body[lf[4]:lf[5]])
			gapEnd := len(body)
			if k+1 < len(leaves) {
				gapEnd = leaves[k+1][0]
			}
			if gap := ttmlTag.ReplaceAllString(body[lf[1]:gapEnd], ""); strings.ContainsAny(gap, " \t\n") {
				text += " "
			}
			sylls = append(sylls, syllable{ttmlTime(body[lf[2]:lf[3]]), text})
		}
		lines = append(lines, lineFromSyllables(start, sylls))
	}
	return lines
}

// ttmlTime reads "1:02.345", "62.345", "62.345s" or "00:01:02.345".
func ttmlTime(s string) time.Duration {
	s = strings.TrimSuffix(strings.TrimSpace(s), "s")
	var total float64
	for _, part := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0
		}
		total = total*60 + v
	}
	return time.Duration(total * float64(time.Second))
}
