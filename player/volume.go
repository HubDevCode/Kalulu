package player

import (
	"math"
	"sync/atomic"

	"github.com/gopxl/beep/v2"
)

// volumeStreamer applies dB gain, the optional loudness normalization, the
// L/R balance and the optional mono downmix to an audio stream. Every
// setting is read via atomic operations, eliminating mutex contention with
// the UI thread on the audio hot path.
type volumeStreamer struct {
	s          beep.Streamer
	vol        *atomic.Uint64 // dB stored as Float64bits
	mono       *atomic.Bool
	balance    *atomic.Uint64 // -1 (left) .. +1 (right) as Float64bits
	normalize  *atomic.Bool
	sr         float64
	cachedDB   float64 // last dB value used to compute cachedGain; starts NaN to force first compute
	cachedGain float64 // precomputed linear gain = 10^(dB/20)

	// Normalization state: a slow mean-square envelope and the gain that
	// follows it, so loud and quiet tracks reach a similar level without
	// pumping inside a song.
	env     float64
	normDB  float64
	limiter float64
}

const (
	normTargetDB = -16.0 // RMS target, about -14 LUFS on music
	normMaxBoost = 9.0
	normMaxCut   = -12.0
	normGateDB   = -50.0 // below this the level is silence: the gain holds
)

func (v *volumeStreamer) Stream(samples [][2]float64) (int, bool) {
	n, ok := v.s.Stream(samples)
	if n == 0 {
		return 0, ok
	}
	db := math.Float64frombits(v.vol.Load())
	mono := v.mono.Load()
	// Recompute gain only when volume changes (rare) instead of every Stream() call.
	if db != v.cachedDB {
		v.cachedGain = math.Pow(10, db/20)
		v.cachedDB = db
	}
	gain := v.cachedGain
	if v.normalize != nil && v.normalize.Load() {
		gain *= v.normGain(samples[:n])
	} else {
		v.normDB = 0
	}
	gl, gr := gain, gain
	if v.balance != nil {
		if b := math.Float64frombits(v.balance.Load()); b > 0 {
			gl *= 1 - b
		} else if b < 0 {
			gr *= 1 + b
		}
	}
	norm := v.normalize != nil && v.normalize.Load()
	for i := range n {
		l, r := samples[i][0], samples[i][1]
		if mono {
			mid := (l + r) / 2
			l, r = mid, mid
		}
		l *= gl
		r *= gr
		if norm {
			l, r = v.limit(l, r)
		}
		samples[i][0] = l
		samples[i][1] = r
	}
	return n, ok
}

// normGain updates the loudness envelope with the chunk and returns the
// linear normalization gain to apply to it.
func (v *volumeStreamer) normGain(samples [][2]float64) float64 {
	sr := v.sr
	if sr <= 0 {
		sr = 44100
	}
	var sum float64
	for _, s := range samples {
		sum += (s[0]*s[0] + s[1]*s[1]) / 2
	}
	ms := sum / float64(len(samples))
	dur := float64(len(samples)) / sr
	if ms > math.Pow(10, normGateDB/10) {
		// 3 s integration: follows the track, not every beat.
		a := 1 - math.Exp(-dur/3)
		if v.env == 0 {
			v.env = ms
		} else {
			v.env += a * (ms - v.env)
		}
		levelDB := 10 * math.Log10(v.env)
		want := min(normMaxBoost, max(normMaxCut, normTargetDB-levelDB))
		// Cut fast (2 s), raise slowly (6 s), so a loud start never blasts.
		tc := 6.0
		if want < v.normDB {
			tc = 2
		}
		v.normDB += (want - v.normDB) * (1 - math.Exp(-dur/tc))
	}
	return math.Pow(10, v.normDB/20)
}

// limit is a soft peak limiter that keeps the boosted signal under 0 dBFS.
func (v *volumeStreamer) limit(l, r float64) (float64, float64) {
	peak := max(math.Abs(l), math.Abs(r))
	const ceiling = 0.97
	if v.limiter == 0 {
		v.limiter = 1
	}
	if peak*v.limiter > ceiling {
		v.limiter = ceiling / peak
	} else {
		// Release over about 0.2 s at 44.1 kHz.
		v.limiter += (1 - v.limiter) * 0.0001
	}
	return l * v.limiter, r * v.limiter
}

func (v *volumeStreamer) Err() error { return v.s.Err() }
