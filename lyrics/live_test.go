package lyrics

import (
	"context"
	"os"
	"testing"
	"time"
)

// LYRICS_LIVE=1 compares the sources on real songs (ALL=1 asks each one).
func TestLyricsLive(t *testing.T) {
	if os.Getenv("LYRICS_LIVE") == "" {
		t.Skip()
	}
	cache.dir = ""
	cases := []struct {
		artist, title string
		dur           int
	}{
		{"Luis Fonsi", "Despacito ft. Daddy Yankee", 282},
		{"Madame", "QUANTO FORTE TI PENSAVO", 213},
		{"Annalisa", "Esibizionista", 0},
		{"Sfera Ebbasta", "SOUTHSIDE", 0},
		{"The Weeknd", "Blinding Lights", 200},
		{"Kendrick Lamar", "Not Like Us", 274},
		{"Daft Punk - Topic", "Around the World", 429},
	}
	srcs := []string{SourceAuto, SourceLyricsPlus, SourceLRCRed, SourcePaxsenix}
	if os.Getenv("ALL") != "" {
		srcs = append([]string{SourceAuto}, Sources...)
	}
	for _, src := range srcs {
		for _, c := range cases {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			start := time.Now()
			r, err := Find(ctx, "", c.artist, c.title, Hint{Duration: time.Duration(c.dur) * time.Second}, Options{Source: src, Refresh: true})
			cancel()
			w := 0
			for _, l := range r.Lines {
				w += len(l.Words)
			}
			t.Logf("q=%d words=%-4d %-10s %-14s %-28s lines=%3d synced=%-5v from=%-9s err=%v (%v)", quality(r.Lines), w, src, c.artist, c.title, len(r.Lines), synced(r.Lines), r.Source, err, time.Since(start).Round(10*time.Millisecond))
		}
	}
}
