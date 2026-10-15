package djmix

import (
	"math"
	"testing"
)

// synth builds a track at bpm: a kick on every beat, louder on the
// downbeats that start at downbeat, hats between beats, and a sustained
// chord of the given pitch classes.
func synth(bpm float64, seconds float64, offset float64, downbeat int, chord []int, minor bool) []float32 {
	n := int(seconds * AnalysisRate)
	x := make([]float32, n)
	beat := 60 / bpm
	for k := 0; ; k++ {
		t := offset + float64(k)*beat
		if t >= seconds {
			break
		}
		amp := 0.45
		if (k-downbeat)%4 == 0 {
			amp = 0.9
		}
		start := int(t * AnalysisRate)
		for i := 0; i < AnalysisRate/8 && start+i < n; i++ {
			tt := float64(i) / AnalysisRate
			f := 50 + 100*math.Exp(-tt*40)
			x[start+i] += float32(amp * math.Exp(-tt*25) * math.Sin(2*math.Pi*f*tt))
		}
		// Off-beat hat: short noise burst.
		hs := int((t + beat/2) * AnalysisRate)
		seed := uint32(k*7919 + 1)
		for i := 0; i < AnalysisRate/40 && hs+i < n; i++ {
			seed = seed*1664525 + 1013904223
			x[hs+i] += float32(0.08 * (float64(seed>>8)/float64(1<<24) - 0.5) * math.Exp(-float64(i)/80))
		}
	}
	for i := range x {
		t := float64(i) / AnalysisRate
		var s float64
		for _, pc := range chord {
			f := 261.63 * math.Pow(2, float64(pc)/12)
			s += math.Sin(2*math.Pi*f*t) + 0.5*math.Sin(2*math.Pi*f*2*t)
		}
		x[i] += float32(0.05 * s)
	}
	_ = minor
	return x
}

func TestAnalyzeTempoBeatsAndKey(t *testing.T) {
	tests := []struct {
		name     string
		bpm      float64
		offset   float64
		downbeat int
		chord    []int
		key      int
		minor    bool
	}{
		{"house in C", 124, 0.31, 0, []int{0, 4, 7}, 0, false},
		{"techno in A minor", 132, 0.12, 2, []int{9, 12, 16}, 9, true},
		{"hip hop in G", 92, 0.5, 1, []int{7, 11, 14}, 7, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Analyze(synth(tt.bpm, 60, tt.offset, tt.downbeat, tt.chord, tt.minor))
			if math.Abs(a.BPM-tt.bpm) > 0.3 {
				t.Fatalf("BPM = %.2f, want %.2f", a.BPM, tt.bpm)
			}
			period := 60 / tt.bpm
			// The grid must land on the kicks.
			phase := math.Mod(a.FirstBeat-tt.offset+10*period, period)
			if phase > period/2 {
				phase -= period
			}
			if math.Abs(phase) > 0.03 {
				t.Fatalf("grid off by %.3f s (first beat %.3f)", phase, a.FirstBeat)
			}
			// Beat k of the grid at time FirstBeat + k*period is kick number
			// round((FirstBeat - offset)/period) + k.
			shift := int(math.Round((a.FirstBeat - tt.offset) / period))
			if !a.IsDownbeat(((tt.downbeat-shift)%4 + 4) % 4) {
				t.Errorf("downbeat phase = %d, want kicks %d mod 4 (shift %d)", a.DownbeatPhase, tt.downbeat, shift)
			}
			if a.Key != tt.key || a.Minor != tt.minor {
				t.Errorf("key = %s (%d minor=%v), want %d minor=%v", a.KeyName(), a.Key, a.Minor, tt.key, tt.minor)
			}
			if a.BeatConfidence < 0.3 {
				t.Errorf("beat confidence %.2f too low", a.BeatConfidence)
			}
		})
	}
}

func TestCamelot(t *testing.T) {
	tests := []struct {
		key   int
		minor bool
		want  string
	}{
		{0, false, "8B"}, {9, true, "8A"}, {7, false, "9B"}, {4, true, "9A"}, {5, false, "7B"}, {2, true, "7A"}, {6, false, "2B"},
	}
	for _, tt := range tests {
		a := &Analysis{Key: tt.key, Minor: tt.minor, KeyConfidence: 1}
		if got := a.Camelot(); got != tt.want {
			t.Errorf("Camelot(%d, %v) = %s, want %s", tt.key, tt.minor, got, tt.want)
		}
	}
	c := &Analysis{Key: 0, KeyConfidence: 1}
	am := &Analysis{Key: 9, Minor: true, KeyConfidence: 1}
	g := &Analysis{Key: 7, KeyConfidence: 1}
	fs := &Analysis{Key: 6, KeyConfidence: 1}
	if CamelotDistance(c, am) != 1 || CamelotDistance(c, g) != 1 || CamelotDistance(c, c) != 0 || CamelotDistance(c, fs) < 3 {
		t.Fatalf("distances: %d %d %d %d", CamelotDistance(c, am), CamelotDistance(c, g), CamelotDistance(c, c), CamelotDistance(c, fs))
	}
}

func TestFastLog1p(t *testing.T) {
	for _, x := range []float64{0, 1e-6, 0.01, 0.5, 1, 3, 10, 100, 1e4, 1e7} {
		if d := math.Abs(fastLog1p(x) - math.Log1p(x)); d > 1e-4 {
			t.Errorf("fastLog1p(%g) off by %g", x, d)
		}
	}
}
