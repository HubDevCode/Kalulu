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
	for range 10 {
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
	far := k.layout[0].words[0].text.Color.(color.NRGBA)
	if far.A >= 0xd0 {
		t.Fatalf("previous line alpha = %d, want dimmed", far.A)
	}
	l := k.layout[1]
	if want := l.y + l.h/2 - k.height*0.38; k.scroll < want-20 || k.scroll > want+20 {
		t.Fatalf("scroll = %v, want about %v", k.scroll, want)
	}
}
