package gui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// Moving from a card onto its play button sends MouseOut to the card; the
// button must stay shown, or it blinks and misses clicks.
func TestCardPlayButtonStaysWhileHovered(t *testing.T) {
	if raceEnabled {
		t.Skip("the Fyne test driver lays out from its own goroutines")
	}
	fa := test.NewApp()
	defer fa.Quit()
	a := newApp(fa, &fakeEngine{done: make(chan struct{})}, Options{Version: "dev", VolumeMin: -50})
	uiStopped.Store(true)
	t.Cleanup(func() { uiStopped.Store(false) })
	c := newCard(a, "Song", "Artist", "", false, nil, func() {})

	steps := []struct {
		name string
		do   func()
		want bool
	}{
		{"pointer on the card", func() { c.MouseIn(nil) }, true},
		{"pointer onto the play button", func() { c.MouseOut(); c.play.MouseIn(nil); c.settleNow() }, true},
		{"pointer back on the card", func() { c.play.MouseOut(); c.MouseIn(nil); c.settleNow() }, true},
		{"pointer onto the button again", func() { c.MouseOut(); c.play.MouseIn(nil); c.settleNow() }, true},
		{"pointer leaves from the button", func() { c.play.MouseOut(); c.settleNow() }, false},
		{"pointer on the card again", func() { c.MouseIn(nil) }, true},
		{"pointer leaves the card", func() { c.MouseOut(); c.settleNow() }, false},
	}
	for _, s := range steps {
		s.do()
		if got := c.play.Visible(); got != s.want {
			t.Fatalf("%s: play visible = %v, want %v", s.name, got, s.want)
		}
	}
}
