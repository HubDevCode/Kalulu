package gui

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/bjarneo/cliamp/ipc"
)

// fakeEngine answers the operations the views use with canned data.
type fakeEngine struct{ done chan struct{} }

func (f *fakeEngine) Call(_ context.Context, op string, _ any) (Result, error) {
	r := Result{Response: ipc.Response{OK: true}}
	switch op {
	case "queue.list":
		r.Response.Tracks = demoTracks()
		r.Response.Total = len(r.Response.Tracks)
	case "provider.list":
		r.Response.Providers = []ipc.ProviderInfo{
			{Key: "cliamp", Name: "cliamp radio"}, {Key: "radio", Name: "Radio", Searchable: true},
			{Key: "local", Name: "Local Playlists"}, {Key: "podcast", Name: "Podcasts", Searchable: true},
			{Key: "navidrome", Name: "Navidrome", Searchable: true, BrowseArtists: true, BrowseAlbums: true},
		}
	case "provider.playlists":
		r.Response.Playlists = []ipc.PlaylistInfo{
			{ID: "1", Name: "Favorites", Section: "Playlists", TrackCount: 42, DurationSecs: 9000},
			{ID: "2", Name: "Road trip", Section: "Playlists", TrackCount: 18},
			{ID: "3", Name: "Lo-fi beats", Section: "Stations", Favoritable: true, Favorite: true},
		}
	case "lyrics":
		r.Response.Lyrics = []ipc.LyricLine{
			{Start: 30, Text: "City lights are fading slow"}, {Start: 38, Text: "Engines hum where rivers flow"},
			{Start: 46, Text: "I keep driving through the night"}, {Start: 54, Text: "Chasing echoes of the light"},
			{Start: 62, Text: "Midnight drive, midnight drive, take me where the sky is wide"},
			{Start: 72, Text: "Every road becomes a song"}, {Start: 80, Text: "Every mile I sing along"},
			{Start: 88, Text: "Hold on tight"}}
	case "history":
		for _, t := range demoTracks()[:3] {
			r.Response.History = append(r.Response.History, ipc.HistoryInfo{Track: t, PlayedAt: "2026-10-15T10:00:00Z"})
		}
	}
	return r, nil
}

func (f *fakeEngine) State(context.Context) (ipc.RuntimeSnapshot, error) { return demoState(), nil }
func (f *fakeEngine) Subscribe([]string) (*ipc.Subscription, error) {
	return ipc.NewBroker().Subscribe([]string{"runtime.state"})
}
func (f *fakeEngine) SamplesInto(dst []float64) int         { return 0 }
func (f *fakeEngine) WaveformSamplesInto(dst []float64) int { return 0 }
func (f *fakeEngine) SampleRate() int                       { return 44100 }
func (f *fakeEngine) Quit()                                 {}
func (f *fakeEngine) Done() <-chan struct{}                 { return f.done }

func demoTracks() []ipc.TrackInfo {
	return []ipc.TrackInfo{
		{Title: "Midnight Drive", Artist: "Neon Coast", Album: "Afterglow", DurationSecs: 215, Path: "/m/1.mp3"},
		{Title: "Paper Planes", Artist: "Lumen", Album: "Skylines", DurationSecs: 187, Path: "/m/2.mp3", QueuePosition: 1},
		{Title: "Slow Tide", Artist: "Harbor Lights", Album: "Low Water", DurationSecs: 264, Path: "/m/3.mp3"},
		{Title: "Radio Paradise", Station: "Radio Paradise", Stream: true, Realtime: true, Path: "https://stream/rp"},
		{Title: "Static Bloom", Artist: "Velvet Static", Album: "Bloom", DurationSecs: 301, Path: "/m/5.mp3"},
	}
}

func demoState() ipc.RuntimeSnapshot {
	on, off := true, false
	t := demoTracks()[0]
	return ipc.RuntimeSnapshot{
		State: "playing", Track: &t, LogicalTrack: &t, Position: 64, Duration: 215, Seekable: true,
		Volume: -6, Total: 5, PlayNextTotal: 1, Shuffle: &on, Repeat: "All", Mono: &off, Speed: 1,
		EQPreset: "Rock", EQBands: []float64{5, 4, 2, -1, -2, 2, 4, 5, 5, 5}, PlaylistRevision: 1,
	}
}

// TestPreview renders the window to PNG files when GUIAMP_PREVIEW names a
// directory. It is a visual check, so it is skipped by default.
func TestPreview(t *testing.T) {
	dir := os.Getenv("GUIAMP_PREVIEW")
	if dir == "" {
		t.Skip("set GUIAMP_PREVIEW to a directory to render previews")
	}
	fa := test.NewApp()
	defer fa.Quit()
	a := newApp(fa, &fakeEngine{done: make(chan struct{})}, Options{Version: "dev", VolumeMin: -50})
	a.winVisible.Store(true)
	if r, err := a.call("provider.list", nil); err == nil {
		a.setProviders(r.Response.Providers)
	}
	a.applyState(demoState())
	a.queue.reload()
	if os.Getenv("GUIAMP_PREVIEW_LIVE") != "" {
		livePreview(t, a, dir)
		return
	}
	shots := []struct {
		name string
		view string
		w, h float32
	}{
		{"queue-wide", "queue", 1400, 860},
		{"queue-medium", "queue", 1000, 720},
		{"queue-narrow", "queue", 760, 640},
		{"browse", "provider:local", 1400, 860},
		{"player", "player", 1400, 860},
		{"vinyl", "player", 1400, 860},
		{"eq", "eq", 1400, 900},
		{"settings", "settings", 1400, 900},
		{"search", "search", 1200, 760},
		{"history", "history", 1200, 760},
		{"dynamic", "home", 1400, 860},
		{"light", "queue", 1400, 860},
		{"winamp", "eq", 1400, 860},
	}
	for _, s := range shots {
		switch s.name {
		case "dynamic":
			cover := image.NewRGBA(image.Rect(0, 0, 64, 64))
			draw.Draw(cover, cover.Bounds(), &image.Uniform{color.NRGBA{235, 120, 30, 255}}, image.Point{}, draw.Src)
			a.setCoverImage(cover)
			a.followCover()
			time.Sleep(900 * time.Millisecond)
		case "light":
			a.setPalette(paletteLight)
		case "winamp":
			a.setPalette("Winamp")
		case "vinyl":
			a.prefs.SetString(prefCoverStyle, coverStyleVinyl)
			cover := image.NewRGBA(image.Rect(0, 0, 64, 64))
			draw.Draw(cover, cover.Bounds(), &image.Uniform{color.NRGBA{200, 60, 40, 255}}, image.Point{}, draw.Src)
			draw.Draw(cover, image.Rect(24, 8, 40, 56), &image.Uniform{color.NRGBA{240, 220, 80, 255}}, image.Point{}, draw.Src)
			a.views["player"].(*fullPlayer).vinyl.SetImage(cover)
		}
		a.show(s.view)
		a.applyState(demoState())
		a.win.Resize(fyne.NewSize(s.w, s.h))
		if s.view == "player" {
			time.Sleep(1500 * time.Millisecond)
			a.applyState(demoState())
			time.Sleep(1200 * time.Millisecond)
		}
		img := a.win.Canvas().Capture()
		f, err := os.Create(filepath.Join(dir, s.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

// livePreview renders the discovery pages with real data from the network.
func livePreview(t *testing.T, a *App, dir string) {
	save := func(name string) {
		img := a.win.Canvas().Capture()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	wait := func() { time.Sleep(9 * time.Second) }
	a.win.Resize(fyne.NewSize(1440, 900))
	a.show("home")
	wait()
	a.win.Content().Refresh()
	save("live-home")
	a.show("search")
	s := a.views["search"].(*searchView)
	s.entry.SetText("vasco rossi")
	s.run("vasco rossi")
	wait()
	save("live-search")
	a.show("radio")
	wait()
	save("live-radio")
	a.show("podcasts")
	wait()
	save("live-podcasts")
	a.show("home")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := a.ytm.Search(ctx, "vasco rossi")
	if err == nil && len(res.Artists) > 0 {
		a.openItem(res.Artists[0])
		wait()
		save("live-artist")
	}
	if err == nil && len(res.Albums) > 0 {
		a.openItem(res.Albums[1])
		wait()
		save("live-album")
	}
}

// TestPreviewVisualizers draws each visualizer style with a synthetic
// spectrum, when GUIAMP_PREVIEW names a directory.
func TestPreviewVisualizers(t *testing.T) {
	dir := os.Getenv("GUIAMP_PREVIEW")
	if dir == "" {
		t.Skip("set GUIAMP_PREVIEW to a directory to render previews")
	}
	fa := test.NewApp()
	defer fa.Quit()
	a := newApp(fa, &fakeEngine{done: make(chan struct{})}, Options{Version: "dev", VolumeMin: -50})
	for _, style := range []string{"NCS", "Bars", "Mirror", "Radial"} {
		v := newVisualizer(a, true)
		v.Close()
		v.style = style
		for i := range v.bands {
			x := float64(i) / float64(len(v.bands))
			v.bands[i] = 0.25 + 0.6*math.Exp(-x*3)*(0.7+0.3*math.Sin(x*40))
			v.peaks[i] = v.bands[i] + 0.05
		}
		v.energy = 0.5
		v.beat.pulse = 1
		layer := v.draw(720, 720)
		img := image.NewRGBA(layer.Bounds())
		draw.Draw(img, img.Bounds(), &image.Uniform{a.theme.p.BG}, image.Point{}, draw.Src)
		draw.Draw(img, img.Bounds(), layer, image.Point{}, draw.Over)
		f, err := os.Create(filepath.Join(dir, "vis-"+style+".png"))
		if err != nil {
			t.Fatal(err)
		}
		_ = png.Encode(f, img)
		f.Close()
	}
}
