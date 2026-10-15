package djmix

import (
	"math"
	"testing"
	"time"

	"github.com/bjarneo/cliamp/player"
)

func track(bpm, conf float64, firstBeat float64, phase int, dur float64, key int, minor bool) *Analysis {
	energy := make([]float32, int(dur))
	for i := range energy {
		energy[i] = 0.8
	}
	return &Analysis{
		Version: analysisVersion, Duration: dur, BPM: bpm, BeatConfidence: conf, FirstBeat: firstBeat,
		DownbeatPhase: phase, Key: key, Minor: minor, KeyConfidence: 0.8, LoudnessDB: -12,
		FirstSound: 0.2, LastSound: dur - 1, IntroEnd: 8, OutroStart: dur - 1, Energy: energy, MeanEnergy: 0.8,
	}
}

func onGrid(a *Analysis, t time.Duration, downbeat bool) bool {
	sec := t.Seconds()
	p := a.BeatPeriod()
	k := int(math.Round((sec - a.FirstBeat) / p))
	if math.Abs(a.BeatAt(k)-sec) > 0.002 {
		return false
	}
	return !downbeat || a.IsDownbeat(k)
}

func TestPlanStyles(t *testing.T) {
	dj := DefaultSettings()
	dj.Mode = ModeDJ
	tests := []struct {
		name  string
		a, b  *Analysis
		style string
		rate  bool
	}{
		{"close tempos blend", track(124, 0.8, 0.3, 1, 240, 0, false), track(126, 0.8, 0.5, 2, 240, 7, false), player.StyleBlend, true},
		{"double time blends", track(140, 0.8, 0.3, 0, 240, 9, true), track(71, 0.8, 0.1, 0, 240, 9, true), player.StyleBlend, true},
		{"far tempos filter", track(124, 0.8, 0.3, 0, 240, 0, false), track(110, 0.8, 0.2, 0, 240, 0, false), player.StyleFilter, false},
		{"very far tempos echo", track(128, 0.8, 0.3, 0, 240, 0, false), track(90, 0.8, 0.2, 0, 240, 0, false), player.StyleEcho, false},
		{"no beat fades", track(100, 0.05, 0, 0, 240, 0, false), track(120, 0.05, 0, 0, 240, 0, false), player.StyleFade, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := Plan(tt.a, tt.b, dj)
			if tr.Style != tt.style {
				t.Fatalf("style = %s, want %s (%s)", tr.Style, tt.style, tr.Label)
			}
			if tt.rate == (tr.InRate == 1) {
				t.Fatalf("rate = %v", tr.InRate)
			}
			if tr.Length <= 0 || tr.OutStart <= 0 {
				t.Fatalf("length %v out %v", tr.Length, tr.OutStart)
			}
			if end := tr.OutStart + tr.Length; end.Seconds() > tt.a.LastSound+tt.a.BeatPeriod() {
				t.Fatalf("mix ends at %v after the audio (%v)", end, tt.a.LastSound)
			}
			if tt.style != player.StyleFade {
				if !onGrid(tt.a, tr.OutStart, true) {
					t.Errorf("out start %v is not a downbeat of the outgoing grid", tr.OutStart)
				}
			}
			if tt.style == player.StyleBlend || tt.style == player.StyleFilter {
				if !onGrid(tt.b, tr.InOffset, true) {
					t.Errorf("in offset %v is not a downbeat of the incoming grid", tr.InOffset)
				}
			}
		})
	}
}

func TestPlanBlendMatchesTempo(t *testing.T) {
	s := DefaultSettings()
	s.Mode = ModeDJ
	a, b := track(124, 0.8, 0.3, 1, 240, 0, false), track(128, 0.8, 0.5, 2, 240, 0, false)
	tr := Plan(a, b, s)
	if got := 128 * tr.InRate; math.Abs(got-124) > 0.01 {
		t.Fatalf("incoming tempo after rate = %.2f, want 124", got)
	}
	beats := tr.Length.Seconds() / a.BeatPeriod()
	if math.Abs(beats-math.Round(beats/8)*8) > 0.01 {
		t.Fatalf("blend = %.1f beats, not whole bars", beats)
	}
	if want := s.MixSeconds; math.Abs(tr.Length.Seconds()-want) > 4*a.BeatPeriod() {
		t.Fatalf("blend = %.1f s, want about %.0f s", tr.Length.Seconds(), want)
	}
}

func TestPlanCrossfadeAndOff(t *testing.T) {
	s := DefaultSettings()
	if tr := Plan(nil, nil, s); tr.Length != 0 {
		t.Fatalf("off mode planned %v", tr)
	}
	s.Mode = ModeCrossfade
	s.CrossfadeSec = 5
	tr := Plan(track(120, 0.9, 0, 0, 200, 0, false), track(120, 0.9, 0, 0, 200, 0, false), s)
	if tr.Style != player.StyleFade || tr.Length != 5*time.Second || tr.OutStart.Seconds() != 194 {
		t.Fatalf("crossfade = %+v", tr)
	}
}

func TestPresetsSetLengths(t *testing.T) {
	a, b := track(124, 0.8, 0.3, 1, 300, 0, false), track(126, 0.8, 0.5, 2, 300, 0, false)
	var last float64
	for _, name := range []string{"quick", "smooth", "beatmatch", "long"} {
		s := DefaultSettings()
		s.Mode = ModeDJ
		s.ApplyPreset(name)
		tr := Plan(a, b, s)
		if tr.Length.Seconds() <= last {
			t.Fatalf("%s: %.1f s is not longer than the previous preset (%.1f s)", name, tr.Length.Seconds(), last)
		}
		last = tr.Length.Seconds()
	}
}
