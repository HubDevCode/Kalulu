package discover

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveYTMusic exercises the real service. Set DISCOVER_LIVE=1 to run it.
func TestLiveYTMusic(t *testing.T) {
	if os.Getenv("DISCOVER_LIVE") == "" {
		t.Skip("set DISCOVER_LIVE=1 to call YouTube Music")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	y := NewYTMusic("IT", "it")
	show := func(label string, items []Item, n int) {
		t.Logf("%s: %d", label, len(items))
		for i, it := range items {
			if i >= n {
				break
			}
			t.Logf("   [%s] %q sub=%q artist=%q album=%q vid=%s browse=%s pl=%s dur=%d thumb=%v", it.Kind, it.Title, it.Subtitle, it.Artist, it.Album, it.VideoID, it.BrowseID, it.PlaylistID, it.Duration, it.Thumb != "")
		}
	}
	sugg, err := y.Suggest(ctx, "vasco ro")
	t.Logf("suggest %v %v", sugg, err)
	res, err := y.Search(ctx, "vasco rossi")
	if err != nil {
		t.Fatal(err)
	}
	show("top", res.Top, 4)
	show("songs", res.Songs, 4)
	show("videos", res.Videos, 2)
	show("albums", res.Albums, 3)
	show("artists", res.Artists, 2)
	show("playlists", res.Playlists, 3)
	home, err := y.Home(ctx, 2)
	t.Logf("home err=%v", err)
	for _, s := range home {
		show("home/"+s.Title, s.Items, 2)
	}
	charts, err := y.Charts(ctx)
	t.Logf("charts err=%v", err)
	for _, s := range charts {
		show("charts/"+s.Title, s.Items, 2)
	}
	moods, err := y.Moods(ctx)
	t.Logf("moods err=%v", err)
	for _, s := range moods {
		show("moods/"+s.Title, s.Items, 3)
	}
	if len(res.Albums) > 0 {
		p, err := y.Browse(ctx, res.Albums[0])
		t.Logf("album %q %q err=%v", p.Header.Title, p.Header.Subtitle, err)
		show("album tracks", p.Tracks, 3)
	}
	if len(res.Playlists) > 0 {
		p, err := y.Browse(ctx, res.Playlists[0])
		t.Logf("playlist %q err=%v", p.Header.Title, err)
		show("playlist tracks", p.Tracks, 3)
	}
	if len(res.Artists) > 0 {
		p, err := y.Browse(ctx, res.Artists[0])
		t.Logf("artist %q err=%v", p.Header.Title, err)
		for _, s := range p.Shelves {
			show("artist/"+s.Title, s.Items, 2)
		}
	}
	if len(moods) > 0 && len(moods[0].Items) > 0 {
		p, err := y.Browse(ctx, moods[0].Items[0])
		t.Logf("mood %q err=%v", p.Header.Title, err)
		for _, s := range p.Shelves {
			show("mood/"+s.Title, s.Items, 2)
		}
	}
	if len(res.Songs) > 0 {
		radio, err := y.Radio(ctx, res.Songs[0].VideoID)
		t.Logf("radio err=%v", err)
		show("radio", radio, 4)
	}
}
