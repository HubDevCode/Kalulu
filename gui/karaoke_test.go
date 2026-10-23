package gui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/bjarneo/cliamp/ipc"
)

func TestKaraokeHighlightsCurrentLine(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()
	a := newApp(fa, &fakeEngine{done: make(chan struct{})}, Options{Version: "dev", VolumeMin: -50})
	// Drop the background loads of the window (Home, covers): the test
	// driver runs them on their own goroutines.
	uiStopped.Store(true)
	t.Cleanup(func() { uiStopped.Store(false) })
	k := newKaraoke(a)
	k.SetLines([]ipc.LyricLine{{Start: 10, Text: "one two"}, {Start: 20, Text: "three four five"}, {Start: 30, Text: "six"}}, "")
	k.Resize(fyne.NewSize(600, 400))
	k.SetPosition(26)
	// The highlight eases in over a few frames.
	for range 60 {
		k.animate()
	}
	p := a.theme.p
	if k.current != 1 {
		t.Fatalf("current = %d, want 1", k.current)
	}
	words := k.layout[1].words
	if words[0].text.Color != color.Color(p.Accent) {
		t.Fatalf("sung word color = %v, want accent %v", words[0].text.Color, p.Accent)
	}
	prev := k.layout[0].words[0].text.Color.(color.NRGBA)
	if prev.A != 0xff || luminance(prev) >= luminance(p.FG)*0.8 {
		t.Fatalf("previous line color = %v, want an opaque dimmed color", prev)
	}
	l := k.layout[1]
	if want := l.y + l.h/2 - k.height*0.38; k.scroll < want-20 || k.scroll > want+20 {
		t.Fatalf("scroll = %v, want about %v", k.scroll, want)
	}
}

// Fyne destroys the renderer of a widget that stayed off screen; the
// frames must start again with the next renderer.
func TestKaraokeRestartsAfterRendererDestroy(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()
	a := newApp(fa, &fakeEngine{done: make(chan struct{})}, Options{Version: "dev", VolumeMin: -50})
	uiStopped.Store(true)
	t.Cleanup(func() { uiStopped.Store(false) })
	k := newLyricTicker(a)
	k.Start()
	defer k.Stop()
	k.CreateRenderer().Destroy()
	k.mu.Lock()
	running := k.running
	k.mu.Unlock()
	if running {
		t.Fatal("frames still run after Destroy")
	}
	k.CreateRenderer()
	k.mu.Lock()
	running = k.running
	k.mu.Unlock()
	if !running {
		t.Fatal("frames did not start again with the new renderer")
	}
}
