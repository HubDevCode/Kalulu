package djmix

import (
	"fmt"
	"math"
	"time"

	"github.com/bjarneo/cliamp/player"
)

// Mode is how tracks hand over.
type Mode int

const (
	ModeOff       Mode = iota // gapless
	ModeCrossfade             // fixed-length crossfade
	ModeDJ                    // analyzed, beatmatched DJ transitions
)

// Settings tune the transitions.
type Settings struct {
	Mode Mode `json:"mode"`
	// CrossfadeSec is the length of a crossfade, and of the DJ fallback.
	CrossfadeSec float64 `json:"crossfade_sec"`
	// BeatMatch syncs the incoming tempo to the outgoing one.
	BeatMatch bool `json:"beat_match"`
	// MaxTempoChange is the largest tempo change BeatMatch makes, such as
	// 0.08 for ±8 %, as a DJ's pitch fader.
	MaxTempoChange float64 `json:"max_tempo_change"`
	// HarmonicMixing lengthens the blends of tracks in compatible keys and
	// shortens the clashing ones.
	HarmonicMixing bool `json:"harmonic_mixing"`
	// Effects allows filter sweeps and echo outs.
	Effects bool `json:"effects"`
	// SkipSilence trims silence at both ends.
	SkipSilence bool `json:"skip_silence"`
	// MixOutros starts the mix at the outro of the outgoing track.
	MixOutros bool `json:"mix_outros"`
	// LoudnessMatch levels the incoming track to the outgoing one.
	LoudnessMatch bool `json:"loudness_match"`
	// BlendBeats is the blend length of older settings; MixSeconds replaces it.
	BlendBeats int `json:"blend_beats,omitempty"`
	// Preset names the preset the values came from: smooth, beatmatch,
	// long, quick or manual.
	Preset string `json:"preset"`
	// MixSeconds is the target length of a DJ mix. The planner rounds it to
	// whole bars at the tempo of the outgoing track.
	MixSeconds float64 `json:"mix_seconds"`
	// Intensity scales the EQ swap, the filters and the echo, 0..1.
	Intensity float64 `json:"intensity"`
}

// Presets are the ready-made DJ styles.
var Presets = []string{"smooth", "beatmatch", "long", "quick", "manual"}

// DefaultSettings are a long, beatmatched mix.
func DefaultSettings() Settings {
	s := Settings{Mode: ModeOff, CrossfadeSec: 8, SkipSilence: true, MixOutros: true, LoudnessMatch: true}
	s.ApplyPreset("long")
	return s
}

// ApplyPreset sets the values of a preset; "manual" keeps them.
func (s *Settings) ApplyPreset(name string) {
	s.Preset = name
	switch name {
	case "smooth":
		s.MixSeconds, s.BeatMatch, s.MaxTempoChange, s.Effects, s.HarmonicMixing, s.Intensity = 16, true, 0.06, false, true, 0.55
	case "beatmatch":
		s.MixSeconds, s.BeatMatch, s.MaxTempoChange, s.Effects, s.HarmonicMixing, s.Intensity = 22, true, 0.08, true, true, 0.85
	case "long":
		s.MixSeconds, s.BeatMatch, s.MaxTempoChange, s.Effects, s.HarmonicMixing, s.Intensity = 34, true, 0.10, true, true, 0.9
	case "quick":
		s.MixSeconds, s.BeatMatch, s.MaxTempoChange, s.Effects, s.HarmonicMixing, s.Intensity = 6, true, 0.08, true, false, 1
	}
}

func (s *Settings) normalize() {
	s.CrossfadeSec = math.Max(1, math.Min(20, s.CrossfadeSec))
	s.MaxTempoChange = math.Max(0, math.Min(0.16, s.MaxTempoChange))
	if s.MixSeconds <= 0 {
		// Settings saved before MixSeconds: 32 beats at 120 BPM is 16 s.
		s.MixSeconds = 16
		if s.BlendBeats > 0 {
			s.MixSeconds = float64(s.BlendBeats) / 2
		}
	}
	s.MixSeconds = math.Max(3, math.Min(64, s.MixSeconds))
	if s.Intensity <= 0 {
		s.Intensity = 0.85
	}
	s.Intensity = math.Min(1, s.Intensity)
	if s.Preset == "" {
		s.Preset = "manual"
	}
}

// beatsFor rounds seconds to whole bars (multiples of 8 beats) at bpm.
func beatsFor(seconds, bpm float64) int {
	beats := int(math.Round(seconds*bpm/60/8)) * 8
	return max(8, min(128, beats))
}

func secs(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }

// Plan builds the transition from a to b. a or b may be nil when their
// analysis is not ready; the plan then falls back to a crossfade.
func Plan(a, b *Analysis, s Settings) player.Transition {
	s.normalize()
	switch s.Mode {
	case ModeOff:
		return player.Transition{}
	case ModeCrossfade:
		return crossfade(a, b, s)
	}
	if a == nil || b == nil || a.BPM <= 0 || b.BPM <= 0 {
		t := crossfade(a, b, s)
		t.Label = "Crossfade"
		return t
	}
	return djPlan(a, b, s)
}

func crossfade(a, b *Analysis, s Settings) player.Transition {
	t := player.Transition{Style: player.StyleFade, Length: secs(s.CrossfadeSec), InRate: 1, RampLength: 4 * time.Second, Label: "Crossfade", Intensity: 1}
	if s.SkipSilence {
		if a != nil && a.LastSound > s.CrossfadeSec+1 {
			t.OutStart = secs(a.LastSound - s.CrossfadeSec)
		}
		if b != nil && b.FirstSound > 0.05 && b.FirstSound < 20 {
			t.InOffset = secs(b.FirstSound)
		}
	}
	if s.LoudnessMatch && a != nil && b != nil {
		t.InGainDB = loudnessGain(a, b)
	}
	return t
}

func loudnessGain(a, b *Analysis) float64 {
	if a.LoudnessDB == 0 || b.LoudnessDB == 0 {
		return 0
	}
	return math.Max(-6, math.Min(6, a.LoudnessDB-b.LoudnessDB))
}

// tempoRatio returns the rate that brings b to a's tempo, considering half
// and double time, and whether it is within reach.
func tempoRatio(a, b *Analysis, maxChange float64) (float64, bool) {
	best := 0.0
	for _, f := range []float64{1, 2, 0.5} {
		r := a.BPM / (b.BPM * f)
		if best == 0 || math.Abs(r-1) < math.Abs(best-1) {
			best = r
		}
	}
	return best, math.Abs(best-1) <= maxChange+1e-9
}

// energyAt is the mean energy of a over [from, to) seconds.
func energyAt(a *Analysis, from, to float64) float64 {
	lo, hi := max(0, int(from)), min(len(a.Energy), int(to))
	if hi <= lo {
		return a.MeanEnergy
	}
	var s float64
	for _, e := range a.Energy[lo:hi] {
		s += float64(e)
	}
	return s / float64(hi-lo)
}

// phraseBefore returns the time of the latest downbeat at or before t that
// starts a phrase of 16 beats, or failing that the latest downbeat.
func phraseBefore(a *Analysis, t float64) float64 {
	p := a.BeatPeriod()
	if p <= 0 {
		return t
	}
	k := int(math.Floor((t - a.FirstBeat) / p))
	first := a.DownbeatPhase
	for i := k; i >= max(0, k-32); i-- {
		if a.IsDownbeat(i) && (i-first)%16 == 0 {
			return a.BeatAt(i)
		}
	}
	for i := k; i >= max(0, k-8); i-- {
		if a.IsDownbeat(i) {
			return a.BeatAt(i)
		}
	}
	return a.BeatAt(max(0, k))
}

// firstDownbeat returns the first downbeat of b at or after t.
func firstDownbeat(b *Analysis, t float64) float64 {
	p := b.BeatPeriod()
	k := max(0, int(math.Ceil((t-b.FirstBeat)/p-0.05)))
	for i := k; i < k+8; i++ {
		if b.IsDownbeat(i) {
			return b.BeatAt(i)
		}
	}
	return b.BeatAt(k)
}

func djPlan(a, b *Analysis, s Settings) player.Transition {
	beat := a.BeatPeriod()
	ratio, reachable := tempoRatio(a, b, s.MaxTempoChange)
	clearBeats := a.BeatConfidence >= 0.25 && b.BeatConfidence >= 0.25
	keyDist := CamelotDistance(a, b)
	end := a.LastSound
	if end <= 0 {
		end = a.Duration
	}
	outEnergy := energyAt(a, end-30, end)
	inEnergy := energyAt(b, b.IntroEnd, b.IntroEnd+30)

	t := player.Transition{InRate: 1, Beat: secs(beat)}
	var beats int
	switch {
	case s.BeatMatch && reachable && clearBeats:
		t.Style = player.StyleBlend
		t.InRate = ratio
		beats = beatsFor(s.MixSeconds, a.BPM)
		if s.HarmonicMixing && keyDist > 1 {
			// Clashing keys: halve the overlap so the harmonies do not
			// fight, and let the bass swap do the work.
			beats = max(8, beats/16*8)
		}
		// A long, quiet intro invites a longer blend, up to half again.
		intro := (b.IntroEnd - b.FirstSound) / b.BeatPeriod()
		if limit := beats * 3 / 2; intro >= float64(beats)*1.5 && keyDist <= 1 {
			beats = min(limit, int(intro)/8*8)
		}
	case s.Effects && clearBeats && outEnergy > 0.5 && math.Abs(ratio-1) <= 0.2:
		t.Style = player.StyleFilter
		beats = max(8, beatsFor(s.MixSeconds*0.6, a.BPM))
	case s.Effects && a.BeatConfidence >= 0.25 && outEnergy > 0.45:
		t.Style = player.StyleEcho
		beats = 8
	default:
		t.Style = player.StyleFade
		beats = beatsFor(s.MixSeconds*0.6, a.BPM)
		if a.BeatConfidence < 0.2 {
			// Without a beat the length is in seconds, not bars.
			beats = 0
		}
	}
	length := float64(beats) * beat
	if beats == 0 {
		length = s.MixSeconds * 0.6
	}
	t.Intensity = s.Intensity
	// Keep the mix inside the audible part of the outgoing track.
	if limit := (end - a.FirstSound) / 3; length > limit {
		length = limit
	}
	t.Length = secs(length)

	// Where the mix starts in the outgoing track: at its outro when it has
	// one, else so that it ends with the audio; on a phrase boundary.
	start := end - length
	if s.MixOutros && a.OutroStart < end-6 && a.OutroStart+length <= end+beat {
		start = a.OutroStart
	}
	if t.Style != player.StyleFade || a.BeatConfidence >= 0.2 {
		aligned := phraseBefore(a, start)
		if start-aligned <= 16*beat {
			start = aligned
		}
	}
	if start < a.FirstSound+20 {
		start = math.Max(a.FirstSound, end-length)
	}
	t.OutStart = secs(start)

	// Where the incoming track enters: its first downbeat after the silence.
	entry := b.FirstSound
	if !s.SkipSilence {
		entry = 0
	}
	if clearBeats || t.Style == player.StyleBlend {
		entry = firstDownbeat(b, math.Max(0, entry-0.02))
	}
	if t.Style == player.StyleEcho {
		// The incoming downbeat lands in the middle of the echo, one bar
		// after the outgoing track drops into it.
		entry -= length / 2
	}
	if entry > 0 {
		t.InOffset = secs(entry)
	}

	if s.LoudnessMatch {
		t.InGainDB = loudnessGain(a, b)
	}
	t.RampLength = secs(math.Max(6, 16*b.BeatPeriod()))
	t.Label = describe(t, a, b, beats, outEnergy, inEnergy)
	return t
}

func describe(t player.Transition, a, b *Analysis, beats int, outEnergy, inEnergy float64) string {
	style := map[string]string{
		player.StyleBlend:  "Beatmatch blend",
		player.StyleFilter: "Filter sweep",
		player.StyleEcho:   "Echo out",
		player.StyleFade:   "Fade",
		player.StyleCut:    "Cut",
	}[t.Style]
	label := fmt.Sprintf("%s · %.0f → %.0f BPM", style, a.BPM, b.BPM)
	if t.InRate != 1 {
		label += fmt.Sprintf(" (%+.1f%%)", (t.InRate-1)*100)
	}
	if ka, kb := a.Camelot(), b.Camelot(); ka != "" && kb != "" {
		label += " · " + ka + " → " + kb
	}
	if beats > 0 {
		label += fmt.Sprintf(" · %d beats", beats)
	}
	if inEnergy > outEnergy+0.15 {
		label += " · energy up"
	} else if inEnergy < outEnergy-0.15 {
		label += " · energy down"
	}
	return label
}
