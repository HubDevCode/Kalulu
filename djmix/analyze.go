// Package djmix analyzes tracks and plans DJ transitions between them: the
// tempo, the beat grid, the downbeats and phrases, the musical key, the
// energy curve and the intro and outro of each track decide where a mix
// starts, how long it lasts, whether the tempos are matched and which
// technique (EQ blend, filter sweep, echo out, fade) carries it.
package djmix

import (
	"math"
	"math/cmplx"
	"sort"
)

// AnalysisRate is the sample rate of the mono audio the analysis reads.
const AnalysisRate = 11025

const analysisVersion = 3

// Analysis is what the planner knows about a track.
type Analysis struct {
	Version  int     `json:"version"`
	Duration float64 `json:"duration"` // seconds of audio analyzed

	BPM            float64 `json:"bpm"`
	BeatConfidence float64 `json:"beat_confidence"` // 0..1
	FirstBeat      float64 `json:"first_beat"`      // seconds of the first grid beat
	DownbeatPhase  int     `json:"downbeat_phase"`  // index mod 4 of the downbeats

	Key           int     `json:"key"` // pitch class 0 = C
	Minor         bool    `json:"minor"`
	KeyConfidence float64 `json:"key_confidence"`

	LoudnessDB float64 `json:"loudness_db"` // RMS of the audible part, dBFS
	FirstSound float64 `json:"first_sound"`
	LastSound  float64 `json:"last_sound"`
	IntroEnd   float64 `json:"intro_end"`
	OutroStart float64 `json:"outro_start"`

	// Energy is the loudness per second, 0..1 relative to the loud parts.
	Energy     []float32 `json:"energy"`
	MeanEnergy float64   `json:"mean_energy"`
}

// BeatPeriod is the seconds per beat, 0 without a tempo.
func (a *Analysis) BeatPeriod() float64 {
	if a == nil || a.BPM <= 0 {
		return 0
	}
	return 60 / a.BPM
}

// BeatAt returns the time of grid beat k.
func (a *Analysis) BeatAt(k int) float64 { return a.FirstBeat + float64(k)*a.BeatPeriod() }

// IsDownbeat reports whether grid beat k starts a bar.
func (a *Analysis) IsDownbeat(k int) bool { return ((k-a.DownbeatPhase)%4+4)%4 == 0 }

// Camelot returns the key in Camelot wheel notation, such as "8A".
func (a *Analysis) Camelot() string {
	if a == nil || a.KeyConfidence <= 0 {
		return ""
	}
	n := camelotNumber(a.Key, a.Minor)
	letter := "B"
	if a.Minor {
		letter = "A"
	}
	return itoa(n) + letter
}

// KeyName returns the key such as "A minor".
func (a *Analysis) KeyName() string {
	names := []string{"C", "C♯", "D", "E♭", "E", "F", "F♯", "G", "A♭", "A", "B♭", "B"}
	if a.Minor {
		return names[a.Key] + "m"
	}
	return names[a.Key]
}

func itoa(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}

// camelotNumber maps a key to its wheel number: C major and A minor are 8.
func camelotNumber(key int, minor bool) int {
	if minor {
		key = (key + 3) % 12 // relative major
	}
	// Each step of a fifth (7 semitones) is one step on the wheel.
	for n := 0; n < 12; n++ {
		if (n*7)%12 == key {
			return (n+7)%12 + 1
		}
	}
	return 8
}

// CamelotDistance is 0 for the same key, 1 for a neighbour on the wheel
// (harmonic mixing), larger for clashing keys.
func CamelotDistance(a, b *Analysis) int {
	if a == nil || b == nil || a.KeyConfidence <= 0 || b.KeyConfidence <= 0 {
		return 2
	}
	na, nb := camelotNumber(a.Key, a.Minor), camelotNumber(b.Key, b.Minor)
	d := (na - nb + 12) % 12
	if d > 6 {
		d = 12 - d
	}
	if a.Minor != b.Minor {
		if d == 0 {
			return 1
		}
		return d + 1
	}
	return d
}

// Analyze computes the analysis of mono samples at AnalysisRate.
func Analyze(samples []float32) *Analysis {
	a := &Analysis{Version: analysisVersion, Duration: float64(len(samples)) / AnalysisRate}
	if len(samples) < AnalysisRate*4 {
		return a
	}
	analyzeLevels(a, samples)
	env, low, fps := onsetEnvelope(samples)
	analyzeTempo(a, env, low, fps)
	analyzeKey(a, samples)
	analyzeSections(a)
	return a
}

// --- levels ------------------------------------------------------------------

func analyzeLevels(a *Analysis, x []float32) {
	// RMS per 50 ms for the silence edges, per second for the energy.
	const win = AnalysisRate / 20
	var rms []float64
	for i := 0; i+win <= len(x); i += win {
		var s float64
		for _, v := range x[i : i+win] {
			s += float64(v) * float64(v)
		}
		rms = append(rms, math.Sqrt(s/win))
	}
	const silence = 0.004 // about -48 dBFS
	first, last := 0, len(rms)-1
	for first < len(rms) && rms[first] < silence {
		first++
	}
	for last > first && rms[last] < silence {
		last--
	}
	a.FirstSound = float64(first) / 20
	a.LastSound = float64(last+1) / 20
	var sum float64
	var n int
	for _, r := range rms[first:max(first, last+1)] {
		sum += r * r
		n++
	}
	if n > 0 {
		a.LoudnessDB = 10 * math.Log10(sum/float64(n)+1e-12)
	}
	secs := len(x) / AnalysisRate
	energy := make([]float64, secs)
	for s := range secs {
		var e float64
		for _, v := range x[s*AnalysisRate : (s+1)*AnalysisRate] {
			e += float64(v) * float64(v)
		}
		energy[s] = 10 * math.Log10(e/AnalysisRate+1e-12)
	}
	sorted := append([]float64(nil), energy...)
	sort.Float64s(sorted)
	top := sorted[len(sorted)*95/100]
	a.Energy = make([]float32, secs)
	var mean float64
	for i, e := range energy {
		// 0 at 30 dB below the loud parts, 1 at the loud parts.
		v := math.Max(0, math.Min(1, (e-top+30)/30))
		a.Energy[i] = float32(v)
		mean += v
	}
	if secs > 0 {
		a.MeanEnergy = mean / float64(secs)
	}
}

// --- onsets and tempo ----------------------------------------------------------

const (
	onsetFFT = 512
	onsetHop = 64
)

// onsetEnvelope returns the spectral flux of the whole band and of the bass
// band, and the frames per second.
func onsetEnvelope(x []float32) (env, low []float64, fps float64) {
	fps = float64(AnalysisRate) / onsetHop
	window := hann(onsetFFT)
	tw := twiddles(onsetFFT)
	buf := make([]complex128, onsetFFT)
	prev := make([]float64, onsetFFT/2)
	cur := make([]float64, onsetFFT/2)
	maxBin := onsetFFT / 2 * 5000 / (AnalysisRate / 2)
	lowBin := max(2, onsetFFT*150/AnalysisRate)
	// One complex FFT carries two real frames, A in the real part and B in
	// the imaginary part, which halves the cost of the analysis.
	n := onsetFFT
	flux := func(spec func(k int) float64) (float64, float64) {
		var f, lf float64
		for k := 1; k < maxBin; k++ {
			m := fastLog1p(100 * spec(k))
			cur[k] = m
			if d := m - prev[k]; d > 0 {
				f += d
				if k <= lowBin {
					lf += d
				}
			}
		}
		prev, cur = cur, prev
		return f, lf
	}
	for start := 0; start+n <= len(x); start += 2 * onsetHop {
		second := start+onsetHop+n <= len(x)
		for i := range buf {
			re := float64(x[start+i]) * window[i]
			im := 0.0
			if second {
				im = float64(x[start+onsetHop+i]) * window[i]
			}
			buf[i] = complex(re, im)
		}
		fft(buf, tw)
		// Spectra of the two real frames from the shared transform:
		// A[k] = (Z[k] + conj(Z[n-k]))/2, B[k] = (Z[k] - conj(Z[n-k]))/2i.
		f, lf := flux(func(k int) float64 {
			return cmplx.Abs(buf[k]+cmplx.Conj(buf[n-k])) / 2
		})
		env = append(env, f)
		low = append(low, lf)
		if second {
			f, lf = flux(func(k int) float64 {
				return cmplx.Abs(buf[k]-cmplx.Conj(buf[n-k])) / 2
			})
			env = append(env, f)
			low = append(low, lf)
		}
	}
	return detrend(env, int(fps/2)), detrend(low, int(fps/2)), fps
}

// detrend subtracts a moving average and keeps the positive part.
func detrend(v []float64, half int) []float64 {
	out := make([]float64, len(v))
	var sum float64
	n := 0
	lo, hi := 0, 0
	for i := range v {
		for hi < len(v) && hi <= i+half {
			sum += v[hi]
			hi++
			n++
		}
		for lo < i-half {
			sum -= v[lo]
			lo++
			n--
		}
		out[i] = math.Max(0, v[i]-sum/float64(n))
	}
	return out
}

func interp(v []float64, x float64) float64 {
	i := int(x)
	if i < 0 || i+1 >= len(v) {
		return 0
	}
	f := x - float64(i)
	return v[i]*(1-f) + v[i+1]*f
}

// analyzeTempo finds the tempo by autocorrelation with a tempo prior,
// refines it and the phase with a comb over the whole track, and picks the
// downbeat from the bass onsets.
func analyzeTempo(a *Analysis, env, low []float64, fps float64) {
	n := len(env)
	if n < int(fps*8) {
		return
	}
	// The phase follows the kick and the bass, which sit on the beats, while
	// hats and shakers often sit between them.
	beatEnv := make([]float64, n)
	me, ml := meanOf(env), meanOf(low)
	for i := range beatEnv {
		if me > 0 {
			beatEnv[i] += env[i] / me
		}
		if ml > 0 {
			beatEnv[i] += 2 * low[i] / ml
		}
	}
	env = beatEnv
	// Comb score: the mean onset strength on a beat grid of the tempo, at
	// its best phase, over the mean onset strength. It looks at the whole
	// track, so a steady tempo stands out clearly from its neighbours.
	envMean := meanOf(env)
	combScore := func(e []float64, bpm float64, steps int) (float64, float64) {
		period := fps * 60 / bpm
		bestPhase, best := 0.0, 0.0
		for s := range steps {
			ph := period * float64(s) / float64(steps)
			var sum float64
			k := 0
			for x := ph; x < float64(n-1); x += period {
				sum += interp(e, x)
				k++
			}
			if k > 0 && sum/float64(k) > best {
				best, bestPhase = sum/float64(k), ph
			}
		}
		return best / math.Max(envMean, 1e-9), bestPhase
	}
	prior := func(bpm float64) float64 {
		w := math.Log2(bpm / 118)
		return math.Exp(-0.5 * w * w / (0.55 * 0.55))
	}
	best, bestScore, total, count := 0.0, -1.0, 0.0, 0
	// A real tempo is backed by its half and double time too, which a
	// 3:4 or 2:3 relative of it is not.
	harmonic := func(bpm float64, steps int) float64 {
		raw, _ := combScore(env, bpm, steps)
		half, _ := combScore(env, bpm/2, steps)
		double, _ := combScore(env, bpm*2, steps)
		return raw + 0.5*half + 0.5*double
	}
	for bpm := 60.0; bpm <= 190; bpm += 0.25 {
		raw, _ := combScore(env, bpm, 24)
		s := harmonic(bpm, 24) * prior(bpm)
		total += raw
		count++
		if s > bestScore {
			best, bestScore = bpm, s
		}
	}
	if best == 0 {
		return
	}
	// Fine tempo around the best candidate.
	for bpm := best - 0.5; bpm <= best+0.5; bpm += 0.02 {
		if s := harmonic(bpm, 32) * prior(bpm); s > bestScore {
			best, bestScore = bpm, s
		}
	}
	peak, _ := combScore(env, best, 32)
	mean := total / float64(count)
	conf := math.Max(0, math.Min(1, (peak/mean-1.15)/1.5))

	// Refine the tempo and the phase with a comb over the whole envelope.
	comb := func(period float64) (float64, float64) {
		bestPhase, bestSum := 0.0, -1.0
		steps := 48
		for s := range steps {
			phase := period * float64(s) / float64(steps)
			var sum float64
			for t := phase; t < float64(n-1); t += period {
				sum += interp(env, t)
			}
			if sum > bestSum {
				bestPhase, bestSum = phase, sum
			}
		}
		return bestPhase, bestSum
	}
	refined, phase, top := best, 0.0, -1.0
	for d := -0.15; d <= 0.15; d += 0.01 {
		bpm := best * (1 + d/100)
		ph, sum := comb(fps * 60 / bpm)
		if sum > top {
			refined, phase, top = bpm, ph, sum
		}
	}
	period := fps * 60 / refined
	// Fine phase around the coarse one.
	for d := -period / 48; d <= period/48; d += period / 480 {
		var sum float64
		for t := phase + d; t < float64(n-1); t += period {
			if t >= 0 {
				sum += interp(env, t)
			}
		}
		if sum > top {
			top = sum
			phase += d
		}
	}
	a.BPM = math.Round(refined*100) / 100
	a.BeatConfidence = conf
	a.FirstBeat = (phase + float64(onsetFFT/2)/onsetHop) / fps

	// The downbeat carries the strongest bass onsets.
	var bars [4]float64
	k := 0
	for t := phase; t < float64(n-1); t += period {
		var s float64
		for d := -2.0; d <= 2; d++ {
			s = math.Max(s, interp(low, t+d))
		}
		bars[k%4] += s
		k++
	}
	bestBar := 0
	for i := range 4 {
		if bars[i] > bars[bestBar] {
			bestBar = i
		}
	}
	a.DownbeatPhase = bestBar
}

// --- key -----------------------------------------------------------------------

var (
	majorProfile = []float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = []float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// analyzeKey builds a chromagram and correlates it with the Krumhansl key
// profiles.
func analyzeKey(a *Analysis, x []float32) {
	const size = 4096
	window := hann(size)
	tw := twiddles(size)
	buf := make([]complex128, size)
	var chroma [12]float64
	binClass := make([]int, size/2)
	binWeight := make([]float64, size/2)
	for k := range binClass {
		f := float64(k) * AnalysisRate / size
		binClass[k] = -1
		if f < 55 || f > 2000 {
			continue
		}
		midi := 69 + 12*math.Log2(f/440)
		nearest := math.Round(midi)
		// Weight bins near a note centre more than the ones between notes.
		binWeight[k] = math.Max(0, 1-2*math.Abs(midi-nearest))
		binClass[k] = (int(nearest)%12 + 12) % 12
	}
	frames := 0
	for start := 0; start+size <= len(x); start += size {
		for i := range buf {
			buf[i] = complex(float64(x[start+i])*window[i], 0)
		}
		fft(buf, tw)
		var frame [12]float64
		var total float64
		for k, c := range binClass {
			if c < 0 {
				continue
			}
			m := cmplx.Abs(buf[k]) * binWeight[k]
			frame[c] += m
			total += m
		}
		if total <= 1e-6 {
			continue
		}
		for c := range 12 {
			chroma[c] += math.Sqrt(frame[c] / total)
		}
		frames++
	}
	if frames == 0 {
		return
	}
	type cand struct {
		key   int
		minor bool
		r     float64
	}
	var cands []cand
	for key := range 12 {
		for _, minor := range []bool{false, true} {
			prof := majorProfile
			if minor {
				prof = minorProfile
			}
			rot := make([]float64, 12)
			for i := range 12 {
				rot[(i+key)%12] = prof[i]
			}
			cands = append(cands, cand{key, minor, pearson(chroma[:], rot)})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].r > cands[j].r })
	a.Key, a.Minor = cands[0].key, cands[0].minor
	a.KeyConfidence = math.Max(0, math.Min(1, (cands[0].r-cands[1].r)*8+cands[0].r*0.5))
}

func pearson(x, y []float64) float64 {
	var mx, my float64
	for i := range x {
		mx += x[i]
		my += y[i]
	}
	mx /= float64(len(x))
	my /= float64(len(y))
	var num, dx, dy float64
	for i := range x {
		num += (x[i] - mx) * (y[i] - my)
		dx += (x[i] - mx) * (x[i] - mx)
		dy += (y[i] - my) * (y[i] - my)
	}
	if dx == 0 || dy == 0 {
		return 0
	}
	return num / math.Sqrt(dx*dy)
}

// --- sections ------------------------------------------------------------------

// analyzeSections finds where the body of the track starts and ends, from a
// smoothed energy curve.
func analyzeSections(a *Analysis) {
	a.IntroEnd = a.FirstSound
	a.OutroStart = a.LastSound
	e := a.Energy
	if len(e) < 30 {
		return
	}
	smooth := make([]float64, len(e))
	for i := range e {
		var s float64
		n := 0
		for j := max(0, i-2); j <= min(len(e)-1, i+2); j++ {
			s += float64(e[j])
			n++
		}
		smooth[i] = s / float64(n)
	}
	sorted := append([]float64(nil), smooth...)
	sort.Float64s(sorted)
	body := sorted[len(sorted)/2]
	threshold := body * 0.82
	first := int(a.FirstSound)
	for i := first; i < len(smooth); i++ {
		if smooth[i] >= threshold {
			a.IntroEnd = float64(i)
			break
		}
	}
	last := min(len(smooth)-1, int(a.LastSound))
	for i := last; i > first; i-- {
		if smooth[i] >= threshold {
			a.OutroStart = float64(i + 1)
			break
		}
	}
	// An outro shorter than 6 s or longer than half the track is not one.
	if a.LastSound-a.OutroStart < 6 || a.OutroStart < a.LastSound/2 {
		a.OutroStart = a.LastSound
	}
	if a.IntroEnd-a.FirstSound > a.Duration/2 {
		a.IntroEnd = a.FirstSound
	}
}

// --- FFT -----------------------------------------------------------------------

func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

func twiddles(n int) []complex128 {
	tw := make([]complex128, n/2)
	for i := range tw {
		tw[i] = cmplx.Exp(complex(0, -2*math.Pi*float64(i)/float64(n)))
	}
	return tw
}

func fft(buf []complex128, tw []complex128) {
	n := len(buf)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			buf[i], buf[j] = buf[j], buf[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := n / size
		for start := 0; start < n; start += size {
			for k := 0; k < size/2; k++ {
				t := tw[k*step] * buf[start+k+size/2]
				buf[start+k+size/2] = buf[start+k] - t
				buf[start+k] += t
			}
		}
	}
}

func meanOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// fastLog1p approximates log(1+x) for x >= 0 within about 1e-4, several
// times faster than math.Log1p; the onset flux needs no more.
func fastLog1p(x float64) float64 {
	y := 1 + x
	frac, exp := math.Frexp(y) // y = frac * 2^exp, frac in [0.5, 1)
	// log(frac) on [0.5, 1) via log((1+t)/(1-t)) = 2(t + t^3/3 + t^5/5 + t^7/7).
	t := (frac - 1) / (frac + 1)
	t2 := t * t
	return float64(exp)*math.Ln2 + 2*t*(1+t2*(1.0/3+t2*(1.0/5+t2*(1.0/7+t2/9))))
}
