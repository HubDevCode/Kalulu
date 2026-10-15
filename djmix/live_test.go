package djmix

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bjarneo/cliamp/discover"
)

// TestLiveAnalysis analyzes real songs. Set DJMIX_LIVE=1 to run it.
func TestLiveAnalysis(t *testing.T) {
	if os.Getenv("DJMIX_LIVE") == "" {
		t.Skip("set DJMIX_LIVE=1 to download and analyze real songs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	y := discover.NewYTMusic("US", "en")
	var analyses []*Analysis
	for _, q := range []string{"michael jackson billie jean", "daft punk one more time", "dua lipa levitating"} {
		res, err := y.Search(ctx, q)
		if err != nil || len(res.Songs) == 0 {
			t.Fatalf("search %q: %v", q, err)
		}
		song := res.Songs[0]
		start := time.Now()
		samples, err := Decode(ctx, song.WatchURL())
		if err != nil {
			t.Fatalf("decode %s: %v", song.Title, err)
		}
		a := Analyze(samples)
		analyses = append(analyses, a)
		t.Logf("%-32s %6.2f BPM conf %.2f  key %-4s (%s, conf %.2f)  first beat %.2fs  downbeat %d  loud %.1f dB  intro→%.0fs outro %.0fs/%.0fs  (%.1fs)",
			song.Title+" – "+song.Artist, a.BPM, a.BeatConfidence, a.KeyName(), a.Camelot(), a.KeyConfidence,
			a.FirstBeat, a.DownbeatPhase, a.LoudnessDB, a.IntroEnd, a.OutroStart, a.LastSound, time.Since(start).Seconds())
	}
	s := DefaultSettings()
	s.Mode = ModeDJ
	for i := 0; i+1 < len(analyses); i++ {
		tr := Plan(analyses[i], analyses[i+1], s)
		t.Logf("plan %d→%d: %s | out at %.1fs, %.1fs long, in offset %.2fs, rate %.4f, gain %+.1f dB",
			i, i+1, tr.Label, tr.OutStart.Seconds(), tr.Length.Seconds(), tr.InOffset.Seconds(), tr.InRate, tr.InGainDB)
	}
}
