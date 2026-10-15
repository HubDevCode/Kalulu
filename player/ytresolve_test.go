package player

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestIsYouTubePage(t *testing.T) {
	for u, want := range map[string]bool{
		"https://www.youtube.com/watch?v=abc":        true,
		"https://music.youtube.com/watch?v=abc":      true,
		"https://youtu.be/abc":                       true,
		"https://soundcloud.com/a/b":                 false,
		"https://example.com/watch?v=youtube.com":    false,
		"ytsearch1:daft punk":                        false,
		"/home/me/Music/youtube.com/song.mp3":        false,
		"https://www.youtube.com.evil.example/watch": false,
	} {
		if got := isYouTubePage(u); got != want {
			t.Errorf("isYouTubePage(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestURLExpiry(t *testing.T) {
	in2h := time.Now().Add(2 * time.Hour).Unix()
	got := urlExpiry("https://rr1.googlevideo.com/videoplayback?expire=" + strconv.FormatInt(in2h, 10))
	if d := time.Until(got); d < 100*time.Minute || d > 110*time.Minute {
		t.Errorf("expiry in %v, want about 1h45m", d)
	}
	if d := time.Until(urlExpiry("https://example.com/a.webm")); d > time.Hour+time.Second {
		t.Errorf("expiry without parameter in %v, want at most 1h", d)
	}
}

func TestResolvedFromRejectsManifests(t *testing.T) {
	if _, err := resolvedFrom(ytHelperReply{URL: "https://x/manifest.m3u8", Protocol: "m3u8_native"}); err == nil {
		t.Error("an HLS manifest was accepted as a direct URL")
	}
	if _, err := resolvedFrom(ytHelperReply{Error: "Video unavailable"}); err == nil {
		t.Error("an error reply was accepted")
	}
	r, err := resolvedFrom(ytHelperReply{URL: "https://rr1.googlevideo.com/videoplayback?x=1", Protocol: "https", Duration: 212.5})
	if err != nil || r.Duration != 212500*time.Millisecond {
		t.Errorf("resolvedFrom = %+v, %v", r, err)
	}
}

// YTRESOLVE_LIVE=1 resolves a real video through the helper.
func TestResolveYTLive(t *testing.T) {
	if os.Getenv("YTRESOLVE_LIVE") == "" {
		t.Skip("set YTRESOLVE_LIVE=1")
	}
	defer StopYTResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	start := time.Now()
	r, err := ResolveYT(ctx, "https://www.youtube.com/watch?v=HdeYwObD-j4")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cold %v, duration %v", time.Since(start), r.Duration)
	start = time.Now()
	if _, err := ResolveYT(ctx, "https://www.youtube.com/watch?v=yydNF8tuVmU"); err != nil {
		t.Fatal(err)
	}
	t.Logf("warm %v", time.Since(start))
}

func TestCanonicalYouTube(t *testing.T) {
	for in, want := range map[string]string{
		"https://music.youtube.com/watch?v=abc&list=RD": "https://www.youtube.com/watch?v=abc",
		"https://youtu.be/abc":                          "https://www.youtube.com/watch?v=abc",
		"https://www.youtube.com/watch?v=abc":           "https://www.youtube.com/watch?v=abc",
		"https://www.youtube.com/playlist?list=PL1":     "https://www.youtube.com/playlist?list=PL1",
	} {
		if got := canonicalYouTube(in); got != want {
			t.Errorf("canonicalYouTube(%q) = %q, want %q", in, got, want)
		}
	}
}
