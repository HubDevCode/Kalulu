package player

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/gopxl/beep/v2"
)

func sine(amp float64) beep.Streamer {
	i := 0
	return beep.StreamerFunc(func(s [][2]float64) (int, bool) {
		for k := range s {
			v := amp * math.Sin(2*math.Pi*440*float64(i)/44100)
			s[k] = [2]float64{v, v}
			i++
		}
		return len(s), true
	})
}

func rmsDB(s [][2]float64) float64 {
	var sum float64
	for _, x := range s {
		sum += x[0] * x[0]
	}
	return 10 * math.Log10(sum/float64(len(s)))
}

func TestNormalizeBringsTracksTogether(t *testing.T) {
	level := func(amp float64) float64 {
		var vol, bal atomic.Uint64
		var mono, norm atomic.Bool
		norm.Store(true)
		v := &volumeStreamer{s: sine(amp), vol: &vol, mono: &mono, balance: &bal, normalize: &norm, sr: 44100, cachedDB: math.NaN()}
		buf := make([][2]float64, 512)
		for range 44100 * 30 / 512 { // 30 s
			v.Stream(buf)
		}
		return rmsDB(buf)
	}
	quiet, loud := level(0.12), level(0.9) // about -21 and -4 dBFS RMS
	if d := math.Abs(quiet - loud); d > 3 {
		t.Fatalf("normalized levels differ by %.1f dB (quiet %.1f, loud %.1f)", d, quiet, loud)
	}
	if loud > 0 {
		t.Fatalf("loud track clips: %.1f dB", loud)
	}
}

func TestBalance(t *testing.T) {
	var vol, bal atomic.Uint64
	var mono, norm atomic.Bool
	bal.Store(math.Float64bits(-0.5))
	v := &volumeStreamer{s: sine(0.5), vol: &vol, mono: &mono, balance: &bal, normalize: &norm, cachedDB: math.NaN()}
	buf := make([][2]float64, 256)
	v.Stream(buf)
	for _, s := range buf[1:] {
		if s[0] != 0 && math.Abs(s[1]/s[0]-0.5) > 1e-9 {
			t.Fatalf("right/left = %v, want 0.5", s[1]/s[0])
		}
	}
}
