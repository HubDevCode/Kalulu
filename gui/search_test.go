package gui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// The suggestions must not take the keyboard from the search box: when they
// did, the next letters reached the window shortcuts (N skipped a track).
func TestSuggestionsKeepSearchFocus(t *testing.T) {
	if raceEnabled {
		t.Skip("the Fyne test driver lays out from its own goroutines")
	}
	fa := test.NewApp()
	defer fa.Quit()
	a := newApp(fa, &fakeEngine{done: make(chan struct{})}, Options{Version: "dev", VolumeMin: -50})
	// The test driver runs fyne.Do on the calling goroutine: drop the
	// background loads (covers, Home), which this test does not need.
	uiStopped.Store(true)
	t.Cleanup(func() { uiStopped.Store(false) })
	a.show("search")
	s := a.views["search"].(*searchView)
	c := a.win.Canvas()
	c.Focus(s.entry)
	s.showSuggestions([]string{"daft punk", "daft punk one more time"})
	if !s.popup.Visible() {
		t.Fatal("suggestions are not shown")
	}
	if c.Overlays().Top() != nil {
		t.Fatal("suggestions open an overlay, which steals the keyboard focus")
	}
	if c.Focused() != s.entry {
		t.Fatalf("focus is on %T, want the search entry", c.Focused())
	}
	test.Type(s.entry, "n")
	if s.entry.Text != "n" {
		t.Fatalf("entry text = %q, want the typed letter", s.entry.Text)
	}
}
