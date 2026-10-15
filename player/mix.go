package player

import (
	"github.com/bjarneo/cliamp/applog"
	"math"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep/v2"
)

// Transition styles. The planner picks one for each pair of tracks.
const (
	StyleFade   = "fade"   // equal-power crossfade
	StyleBlend  = "blend"  // beatmatched blend with a bass swap
	StyleFilter = "filter" // high-pass sweep out, low-pass opening in
	StyleEcho   = "echo"   // beat-synced echo out, incoming on the downbeat
	StyleCut    = "cut"    // hard cut on the beat with a short de-click
)

// Transition describes how the outgoing track hands over to the incoming
// one. A planner builds it from the analysis of both tracks; the gapless
// streamer executes it sample-accurately on the audio goroutine.
type Transition struct {
	Style string
	// OutStart is the position in the outgoing track where mixing starts.
	// Zero means Length before the end of the track.
	OutStart time.Duration
	// Length is how long both tracks play together.
	Length time.Duration
	// InOffset is skipped at the start of the incoming track, for example
	// leading silence or up to its first downbeat.
	InOffset time.Duration
	// InRate resamples the incoming track during the mix so its tempo
	// matches the outgoing one (1 = unchanged). After the mix the rate
	// returns to 1 over RampLength.
	InRate     float64
	RampLength time.Duration
	// Beat is the beat period of the outgoing track, for the bass swap and
	// the echo delay. Zero when the tracks have no clear beat.
	Beat time.Duration
	// InGainDB matches the loudness of the incoming track to the outgoing
	// one during the mix; it returns to 0 dB over RampLength afterward.
	InGainDB float64
	// Intensity scales the EQ swap, the filters and the echo, 0..1; zero
	// means full.
	Intensity float64
	// Label describes the transition for the interface, such as
	// "Beatmatch 124 → 126 BPM · 8A → 9A".
	Label string
}

// TransitionPlanner returns the transition from the track at outPath to the
// one at inPath. ok=false keeps the plain gapless handover. It runs on the
// preload goroutine and may wait for an analysis.
type TransitionPlanner func(outPath, inPath string) (t Transition, ok bool)

// TransitionEvent reports a transition to the interface.
type TransitionEvent struct {
	Transition
	Started time.Time
}

// SetTransitionPlanner installs the planner of crossfades and DJ mixes. Nil
// restores gapless playback.
func (p *Player) SetTransitionPlanner(planner TransitionPlanner) {
	p.mu.Lock()
	p.planner = planner
	p.mu.Unlock()
}

// SetTransitionLead sets how long before the end of a track the next one
// must be preloaded, so a transition has its audio ready.
func (p *Player) SetTransitionLead(d time.Duration) {
	p.transitionLead.Store(int64(d))
}

// TransitionLead is the preload lead that transitions need; zero when
// transitions are off.
func (p *Player) TransitionLead() time.Duration {
	return time.Duration(p.transitionLead.Load())
}

// LastTransition returns the transition that ran most recently.
func (p *Player) LastTransition() (TransitionEvent, bool) {
	v, _ := p.lastTransition.Load().(TransitionEvent)
	return v, !v.Started.IsZero()
}

// Mixing reports whether a transition is playing now.
func (p *Player) Mixing() bool { return p.gapless.mixing.Load() }

// planNext asks the planner for the transition into tp and skips the
// incoming offset. It runs on the preload goroutine, before tp reaches the
// speaker, so it can read tp freely.
func (p *Player) planNext(tp *trackPipeline) *Transition {
	p.mu.Lock()
	planner := p.planner
	var outPath string
	if p.current != nil {
		outPath = p.current.path
	}
	p.mu.Unlock()
	if planner == nil || outPath == "" || tp.live {
		p.trimLeadingSilence(tp)
		return nil
	}
	t, ok := planner(outPath, tp.path)
	if ok && t.Length > 0 {
		// The plan comes from an analysis of the audio; when the player
		// knows the track is much longer, that analysis saw a cut download
		// and its mix point would end the song early.
		if total := p.Duration(); total > 0 && t.OutStart > 0 && t.OutStart+t.Length < total-90*time.Second {
			applog.Warn("dj: plan ignored, mix at %v of a %v track", t.OutStart, total)
			ok = false
		}
	}
	if !ok || t.Length <= 0 {
		p.trimLeadingSilence(tp)
		return nil
	}
	if t.InRate <= 0 || math.Abs(t.InRate-1) < 0.0005 {
		t.InRate = 1
	}
	if t.InOffset > 0 {
		skipped := tp.skip(p.sr, t.InOffset)
		t.InOffset = skipped
	} else {
		t.InOffset = p.trimLeadingSilence(tp)
	}
	return &t
}

// skip discards d of audio from the start of the pipeline. A seekable
// decoder seeks; a live prefetch waits for the data; other sources are read
// and dropped. It returns the duration actually skipped.
func (tp *trackPipeline) skip(sr beep.SampleRate, d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d > 90*time.Second {
		d = 90 * time.Second
	}
	if tp.seekable && tp.livePrefetch == nil && tp.decoder != nil {
		target := tp.format.SampleRate.N(d)
		if n := tp.decoder.Len(); n > 0 && target >= n {
			return 0
		}
		if err := tp.decoder.Seek(target); err == nil {
			return d
		}
	}
	if tp.livePrefetch != nil {
		return sr.D(tp.livePrefetch.discard(sr.N(d), 20*time.Second))
	}
	frames := sr.N(d)
	buf := make([][2]float64, 4096)
	done := 0
	for done < frames {
		n, ok := tp.stream.Stream(buf[:min(len(buf), frames-done)])
		done += n
		if !ok || n == 0 {
			break
		}
	}
	return sr.D(done)
}

// discard drops up to frames buffered frames, waiting at most timeout for
// the download. It returns how many frames it dropped.
func (p *livePrefetchStreamer) discard(frames int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	dropped := 0
	p.mu.Lock()
	defer p.mu.Unlock()
	for dropped < frames {
		available := p.availableToReadLocked()
		if available == 0 {
			if p.done || p.closed || time.Now().After(deadline) {
				break
			}
			p.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			p.mu.Lock()
			continue
		}
		n := min(available, frames-dropped)
		p.r = (p.r + n) % len(p.buf)
		p.consumed += int64(n)
		p.full = false
		dropped += n
		p.cond.Broadcast()
	}
	return dropped
}

// --- DSP --------------------------------------------------------------------

// stereoBiquad is a biquad filter with per-channel state whose coefficients
// can change between blocks.
type stereoBiquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     [2]float64
	key                [3]float64 // parameters of the current coefficients
	active             bool
}

func (f *stereoBiquad) setPassthrough() {
	f.active = false
}

func (f *stereoBiquad) set(kind int, sr, freq, q, gainDB float64) {
	key := [3]float64{float64(kind), freq, gainDB}
	if f.active && key == f.key {
		return
	}
	f.key = key
	f.active = true
	freq = math.Max(10, math.Min(freq, sr*0.45))
	w0 := 2 * math.Pi * freq / sr
	cosw, sinw := math.Cos(w0), math.Sin(w0)
	alpha := sinw / (2 * q)
	var b0, b1, b2, a0, a1, a2 float64
	switch kind {
	case filterHighPass:
		b0, b1, b2 = (1+cosw)/2, -(1 + cosw), (1+cosw)/2
		a0, a1, a2 = 1+alpha, -2*cosw, 1-alpha
	case filterLowPass:
		b0, b1, b2 = (1-cosw)/2, 1-cosw, (1-cosw)/2
		a0, a1, a2 = 1+alpha, -2*cosw, 1-alpha
	default: // low shelf, S = 1
		A := math.Pow(10, gainDB/40)
		alpha = sinw / 2 * math.Sqrt((A+1/A)*(1/1-1)+2)
		sq := 2 * math.Sqrt(A) * alpha
		b0 = A * ((A + 1) - (A-1)*cosw + sq)
		b1 = 2 * A * ((A - 1) - (A+1)*cosw)
		b2 = A * ((A + 1) - (A-1)*cosw - sq)
		a0 = (A + 1) + (A-1)*cosw + sq
		a1 = -2 * ((A - 1) + (A+1)*cosw)
		a2 = (A + 1) + (A-1)*cosw - sq
	}
	f.b0, f.b1, f.b2, f.a1, f.a2 = b0/a0, b1/a0, b2/a0, a1/a0, a2/a0
}

func (f *stereoBiquad) process(s [2]float64) [2]float64 {
	if !f.active {
		return s
	}
	var out [2]float64
	for c := range 2 {
		y := f.b0*s[c] + f.b1*f.x1[c] + f.b2*f.x2[c] - f.a1*f.y1[c] - f.a2*f.y2[c]
		f.x2[c], f.x1[c] = f.x1[c], s[c]
		f.y2[c], f.y1[c] = f.y1[c], y
		out[c] = y
	}
	return out
}

const (
	filterLowShelf = iota
	filterHighPass
	filterLowPass
)

// deckFX is the effect chain of one deck: bass shelf, high-pass, low-pass
// and a beat-synced echo.
type deckFX struct {
	sr       float64
	bass     stereoBiquad
	hp       stereoBiquad
	lp       stereoBiquad
	delay    [][2]float64
	delayPos int
}

func newDeckFX(sr beep.SampleRate, beat time.Duration) *deckFX {
	fx := &deckFX{sr: float64(sr)}
	if beat > 0 {
		// A dotted-eighth echo sits between the beats and sounds musical.
		n := sr.N(beat * 3 / 4)
		if n > 0 {
			fx.delay = make([][2]float64, n)
		}
	}
	return fx
}

// fxParams is the state of a deck at one moment of a transition.
type fxParams struct {
	gain     float64 // linear
	bassDB   float64 // low shelf gain at 180 Hz, 0 = flat
	hpHz     float64 // 0 = off
	lpHz     float64 // 0 = off
	echoSend float64 // 0..1 of the dry signal fed to the echo
	echoFB   float64 // echo feedback 0..1
	dry      float64 // 0..1 of the dry signal in the output
}

func neutralFX() fxParams { return fxParams{gain: 1, dry: 1} }

func (fx *deckFX) configure(p fxParams) {
	if math.Abs(p.bassDB) > 0.05 {
		fx.bass.set(filterLowShelf, fx.sr, 180, 0.707, p.bassDB)
	} else {
		fx.bass.setPassthrough()
	}
	if p.hpHz > 20 {
		fx.hp.set(filterHighPass, fx.sr, p.hpHz, 0.75, 0)
	} else {
		fx.hp.setPassthrough()
	}
	if p.lpHz > 0 && p.lpHz < 18000 {
		fx.lp.set(filterLowPass, fx.sr, p.lpHz, 0.75, 0)
	} else {
		fx.lp.setPassthrough()
	}
}

func (fx *deckFX) process(s [2]float64, p fxParams) [2]float64 {
	s = fx.bass.process(s)
	s = fx.hp.process(s)
	s = fx.lp.process(s)
	out := [2]float64{s[0] * p.dry, s[1] * p.dry}
	if len(fx.delay) > 0 && (p.echoSend > 0 || p.echoFB > 0) {
		d := fx.delay[fx.delayPos]
		out[0] += d[0]
		out[1] += d[1]
		// Ping-pong: each repeat swaps the channels.
		fx.delay[fx.delayPos] = [2]float64{
			s[0]*p.echoSend + d[1]*p.echoFB,
			s[1]*p.echoSend + d[0]*p.echoFB,
		}
		fx.delayPos = (fx.delayPos + 1) % len(fx.delay)
	}
	return [2]float64{out[0] * p.gain, out[1] * p.gain}
}

// --- decks and the mix -----------------------------------------------------

// deck plays one track with a variable rate and an effect chain. The
// incoming deck becomes the current stream of the gapless streamer and keeps
// running after the mix, so its tempo can glide back to normal.
type deck struct {
	src       beep.Streamer
	resampler *beep.Resampler
	fx        *deckFX
	params    fxParams
	// rampFrom/rampFrames/rampDone glide the rate and the gain back to
	// neutral after the mix.
	rate       float64
	rampFrom   float64
	gainFrom   float64
	rampFrames int
	rampDone   int
	ramping    bool
}

func newDeck(src beep.Streamer, sr beep.SampleRate, rate float64, beat time.Duration) *deck {
	d := &deck{src: src, fx: newDeckFX(sr, beat), params: neutralFX(), rate: 1}
	if rate != 1 {
		// beep's resampler reads the source at ratio and is retuned live.
		d.resampler = beep.ResampleRatio(3, rate, src)
		d.rate = rate
	}
	return d
}

func (d *deck) setRate(r float64) {
	if d.resampler != nil {
		d.resampler.SetRatio(r)
		d.rate = r
	}
}

// Stream is the deck used as the current stream after a mix: it runs the
// glide back to neutral, then plays plainly.
func (d *deck) Stream(samples [][2]float64) (int, bool) {
	var n int
	var ok bool
	if d.resampler != nil {
		n, ok = d.resampler.Stream(samples)
	} else {
		n, ok = d.src.Stream(samples)
	}
	if d.ramping {
		for i := 0; i < n; i += mixBlock {
			t := math.Min(1, float64(d.rampDone)/float64(max(1, d.rampFrames)))
			e := smoothstep(t)
			d.setRate(d.rampFrom + (1-d.rampFrom)*e)
			d.params.gain = d.gainFrom + (1-d.gainFrom)*e
			end := min(n, i+mixBlock)
			for j := i; j < end; j++ {
				samples[j] = d.fx.process(samples[j], d.params)
			}
			d.rampDone += end - i
		}
		if d.rampDone >= d.rampFrames {
			d.ramping = false
			d.setRate(1)
			d.params = neutralFX()
			d.fx.configure(d.params)
		}
	} else if d.params != neutralFX() {
		for j := 0; j < n; j++ {
			samples[j] = d.fx.process(samples[j], d.params)
		}
	}
	return n, ok
}

func (d *deck) Err() error { return nil }

const mixBlock = 64

// mixState runs one transition: out is the outgoing deck, in the incoming.
type mixState struct {
	plan     Transition
	out      *deck
	in       *deck
	frame    int
	total    int
	tail     int // frames the outgoing echo may ring after the mix
	outBuf   [][2]float64
	outEnded bool
	inGain   float64
	sr       beep.SampleRate
}

func newMixState(plan Transition, out beep.Streamer, in *deck, sr beep.SampleRate) *mixState {
	m := &mixState{
		plan:   plan,
		out:    newDeck(out, sr, 1, plan.Beat),
		in:     in,
		total:  max(1, sr.N(plan.Length)),
		sr:     sr,
		inGain: math.Pow(10, math.Max(-9, math.Min(9, plan.InGainDB))/20),
	}
	if plan.Style == StyleEcho && plan.Beat > 0 {
		m.tail = sr.N(plan.Beat * 8)
	}
	m.apply(0)
	return m
}

func smoothstep(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// ramp maps t inside [a, b] to 0..1.
func ramp(t, a, b float64) float64 {
	if t <= a {
		return 0
	}
	if t >= b {
		return 1
	}
	return (t - a) / (b - a)
}

// apply sets the deck parameters for progress t (0..1, beyond 1 for the
// echo tail).
func (m *mixState) apply(t float64) {
	out, in := neutralFX(), neutralFX()
	beatFrac := 0.0
	if m.plan.Length > 0 && m.plan.Beat > 0 {
		beatFrac = float64(m.plan.Beat) / float64(m.plan.Length)
	}
	switch m.plan.Style {
	case StyleBlend:
		// Incoming rises over the first half with its bass cut; on the
		// middle beat the basses swap; the outgoing then fades out.
		in.gain = math.Sin(math.Pi / 2 * smoothstep(ramp(t, 0, 0.45)))
		out.gain = math.Cos(math.Pi / 2 * smoothstep(ramp(t, 0.55, 1)))
		swap := smoothstep(ramp(t, 0.5-beatFrac/2, 0.5+beatFrac/2))
		in.bassDB = -26 * (1 - swap)
		out.bassDB = -26 * swap
		// A gentle high-pass on the outgoing tail keeps the mix clean.
		out.hpHz = 20 + 280*ramp(t, 0.75, 1)
	case StyleFilter:
		sweep := ramp(t, 0, 0.85)
		out.hpHz = 20 * math.Pow(120, sweep) // 20 Hz to 2.4 kHz
		out.gain = math.Cos(math.Pi / 2 * smoothstep(ramp(t, 0.6, 1)))
		in.lpHz = 400 * math.Pow(45, smoothstep(ramp(t, 0.2, 1))) // 400 Hz up to 18 kHz
		in.gain = math.Sin(math.Pi / 2 * smoothstep(ramp(t, 0, 0.5)))
	case StyleEcho:
		// The outgoing dry signal drops within the first beat into a
		// feedback echo; the incoming lands on the downbeat at the middle.
		end := math.Max(0.15, beatFrac)
		out.dry = 1 - smoothstep(ramp(t, 0, end))
		out.echoSend = 0.85 * (1 - ramp(t, end, end+beatFrac))
		out.echoFB = 0.55
		out.hpHz = 20 + 600*ramp(t, 0, 1)
		in.gain = smoothstep(ramp(t, 0.49, 0.5))
	case StyleCut:
		out.gain = 1 - smoothstep(ramp(t, 0.5, 0.5+math.Max(0.01, beatFrac/8)))
		in.gain = smoothstep(ramp(t, 0.5, 0.5+math.Max(0.01, beatFrac/8)))
	default: // StyleFade
		in.gain = math.Sin(math.Pi / 2 * t)
		out.gain = math.Cos(math.Pi / 2 * math.Min(1, t))
	}
	if t >= 1 {
		in = neutralFX()
		if m.plan.Style != StyleEcho {
			out.gain = 0
		}
	}
	// Intensity tempers the effects toward a plain blend.
	if k := m.plan.Intensity; k > 0 && k < 1 {
		in.bassDB *= k
		out.bassDB *= k
		if out.hpHz > 20 {
			out.hpHz = 20 + (out.hpHz-20)*k
		}
		if in.lpHz > 0 {
			in.lpHz = 18000 - (18000-in.lpHz)*k
		}
		out.echoSend *= k
	}
	in.gain *= m.inGainAt(t)
	m.out.params = out
	m.in.params = in
	m.out.fx.configure(out)
	m.in.fx.configure(in)
}

func (m *mixState) inGainAt(t float64) float64 {
	if t >= 1 {
		return m.inGain
	}
	return m.inGain
}

// stream fills samples with the mix. It reports done when the transition
// and any echo tail are over; the incoming deck then plays on alone.
func (m *mixState) stream(samples [][2]float64) (n int, ok bool, done bool) {
	if cap(m.outBuf) < len(samples) {
		m.outBuf = make([][2]float64, len(samples))
	}
	outBuf := m.outBuf[:len(samples)]
	// Incoming deck, raw: the mix applies its effects here.
	var inN int
	var inOK bool
	if m.in.resampler != nil {
		inN, inOK = m.in.resampler.Stream(samples)
	} else {
		inN, inOK = m.in.src.Stream(samples)
	}
	clear(samples[inN:])
	outN := 0
	if !m.outEnded {
		var outOK bool
		outN, outOK = m.out.src.Stream(outBuf)
		if !outOK || outN < len(outBuf) {
			m.outEnded = true
		}
	}
	clear(outBuf[outN:])
	limit := m.total + m.tail
	for i := 0; i < len(samples); i += mixBlock {
		t := float64(m.frame) / float64(m.total)
		m.apply(t)
		end := min(len(samples), i+mixBlock)
		for j := i; j < end; j++ {
			a := m.in.fx.process(samples[j], m.in.params)
			b := m.out.fx.process(outBuf[j], m.out.params)
			samples[j] = [2]float64{a[0] + b[0], a[1] + b[1]}
		}
		m.frame += end - i
	}
	done = m.frame >= limit || (m.frame >= m.total && (m.outEnded || m.plan.Style != StyleEcho))
	return len(samples), inOK || inN > 0, done
}

// finish hands the incoming deck the glide back to its own tempo and gain.
func (m *mixState) finish() {
	in := m.in
	in.params = neutralFX()
	in.fx.configure(in.params)
	in.gainFrom = m.inGain
	in.rampFrom = in.rate
	in.rampFrames = max(1, m.sr.N(m.plan.RampLength))
	in.rampDone = 0
	in.ramping = in.rate != 1 || math.Abs(m.inGain-1) > 0.001
	if !in.ramping {
		in.params = neutralFX()
	} else {
		in.params.gain = m.inGain
	}
}

// mixFlag lets Player read the mixing state without the speaker lock.
type mixFlag = atomic.Bool
