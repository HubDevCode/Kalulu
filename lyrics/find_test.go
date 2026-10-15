package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLineFromSyllablesJoinsWords(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	l := lineFromSyllables(ms(100), []syllable{{ms(100), "I'm "}, {ms(300), "with"}, {ms(500), "drawals "}, {ms(900), "now"}})
	if l.Text != "I'm withdrawals now" {
		t.Fatalf("text = %q", l.Text)
	}
	if len(l.Words) != len(strings.Fields(l.Text)) {
		t.Fatalf("words = %+v, want one per field", l.Words)
	}
	if l.Words[1].Text != "withdrawals" || l.Words[1].Start != ms(300) || l.Words[2].Start != ms(900) {
		t.Fatalf("words = %+v", l.Words)
	}
}

func TestParseTTMLSyllables(t *testing.T) {
	doc := `<tt><body><div>
<p begin="12.5" end="14"><span begin="12.5" end="12.9">Hel</span><span begin="12.9" end="13.2">lo</span> <span begin="1:13.300" end="1:14">world</span></p>
<p begin="00:00:20.000">Plain &amp; simple</p>
</div></body></tt>`
	lines := parseTTML(doc)
	if len(lines) != 2 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Text != "Hello world" || len(lines[0].Words) != 2 || lines[0].Words[1].Start != 73300*time.Millisecond {
		t.Fatalf("line 0 = %+v", lines[0])
	}
	if lines[1].Start != 20*time.Second || lines[1].Text != "Plain & simple" {
		t.Fatalf("line 1 = %+v", lines[1])
	}
}

func TestQueryParts(t *testing.T) {
	for _, c := range []struct{ artist, title, wantA, wantT string }{
		{"Daft Punk - Topic", "Around the World", "Daft Punk", "Around the World"},
		{"Uploader", "Luis Fonsi - Despacito ft. Daddy Yankee (Official Video)", "Luis Fonsi", "Despacito"},
		{"Madame, Sfera Ebbasta", "Song feat. Someone", "Madame", "Song"},
	} {
		a, ti := queryParts(c.artist, c.title)
		if a != c.wantA || ti != c.wantT {
			t.Errorf("queryParts(%q, %q) = %q, %q; want %q, %q", c.artist, c.title, a, ti, c.wantA, c.wantT)
		}
	}
}

func TestQualityPrefersWordTimings(t *testing.T) {
	plain := []Line{{Text: "a"}}
	line := []Line{{Start: time.Second, Text: "a b"}}
	word := []Line{{Start: time.Second, Text: "a b", Words: []Word{{time.Second, "a"}, {2 * time.Second, "b"}}}}
	if quality(nil) != 0 || quality(plain) != 1 || quality(line) != 2 || quality(word) != 3 {
		t.Fatal("quality order is wrong")
	}
}

// Find asks every source, keeps the best answer and remembers it.
func TestFindPicksBestAndCaches(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	count := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[path]
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/api/get":
			w.Write([]byte(`{"syncedLyrics":"[00:01.00]line synced"}`))
		case "/v2/lyrics/get":
			w.Write([]byte(`{"type":"Word","lyrics":[{"time":1000,"text":"word timed","syllabus":[{"time":1000,"text":"word "},{"time":1500,"text":"timed"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	saved := []string{lrclibBaseURL, neteaseBaseURL, kugouBaseURL, qqBaseURL, unisonBaseURL, ovhBaseURL, lrcRedBaseURL, paxsenixBaseURL, itunesBaseURL}
	savedPlus := lyricsPlusServers
	savedDir := cache.dir
	t.Cleanup(func() {
		lrclibBaseURL, neteaseBaseURL, kugouBaseURL, qqBaseURL, unisonBaseURL, ovhBaseURL, lrcRedBaseURL, paxsenixBaseURL, itunesBaseURL = saved[0], saved[1], saved[2], saved[3], saved[4], saved[5], saved[6], saved[7], saved[8]
		lyricsPlusServers = savedPlus
		cache.dir = savedDir
	})
	for _, p := range []*string{&lrclibBaseURL, &neteaseBaseURL, &kugouBaseURL, &qqBaseURL, &unisonBaseURL, &ovhBaseURL, &lrcRedBaseURL, &paxsenixBaseURL, &itunesBaseURL} {
		*p = srv.URL
	}
	lyricsPlusServers = []string{srv.URL}
	cache.dir = t.TempDir()

	hint := Hint{Duration: 200 * time.Second}
	r, err := Find(context.Background(), "", "Find Artist", "Find Song", hint, Options{})
	if err != nil || r.Source != SourceLyricsPlus || quality(r.Lines) != 3 {
		t.Fatalf("Find = %+v, %v; want the word-timed LyricsPlus answer", r, err)
	}
	before := count("/v2/lyrics/get")
	if r2, err := Find(context.Background(), "", "Find Artist", "Find Song", hint, Options{}); err != nil || r2.Source != r.Source || count("/v2/lyrics/get") != before {
		t.Fatalf("second Find asked the servers again (%v, %v)", r2, err)
	}
	if r3, err := Find(context.Background(), "", "Find Artist", "Find Song", hint, Options{Source: SourceLRCLIB}); err != nil || r3.Source != SourceLRCLIB {
		t.Fatalf("Find from LRCLIB = %+v, %v", r3, err)
	}
}
