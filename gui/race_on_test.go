//go:build race

package gui

// raceEnabled reports a -race build: the Fyne test driver lays windows out
// from its own goroutines, which the detector reports in widget tests.
const raceEnabled = true
