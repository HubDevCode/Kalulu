package player

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep/v2"
)

// SilenceSettings controls the skipping of silence at the edges of tracks.
type SilenceSettings struct {
	Start bool
	End   bool
	// ThresholdDB is the RMS level under which 50 ms of audio counts as
	// silence, such as -55. Quiet intros sit well above it.
	ThresholdDB float64
	// MinEnd is how long the end must stay silent before the next track
	// takes over, such as 1.5 s; shorter gaps are intentional pauses.
	MinEnd time.Duration
}

// DefaultSilenceSettings skip clear digital silence only.
func DefaultSilenceSettings() SilenceSettings {
	return SilenceSettings{Start: true, End: true, ThresholdDB: -55, MinEnd: 1500 * time.Millisecond}
}

// maxLeadingSilence bounds the start trim: a longer quiet opening is part
// of the track, not padding.
const maxLeadingSilence = 15 * time.Second

// endWindow is how close to the end trailing silence may be skipped, so a
// silent break in the middle of a track is never cut.
const endWindow = 30 * time.Second

type silenceConfig struct {
	start, end bool
	threshold  float64 // linear RMS
	minEnd     time.Duration
}

// SetSilenceSkip changes the silence settings. They apply from the next
// track start (start trim) and at once (end skip).
func (p *Player) SetSilenceSkip(s SilenceSettings) {
	cfg := &silenceConfig{
		start:     s.Start,
		end:       s.End,
		threshold: math.Pow(10, math.Max(-90, math.Min(-20, s.ThresholdDB))/20),
		minEnd:    max(300*time.Millisecond, s.MinEnd),
	}
	p.silence.Store(cfg)
	p.gapless.silence.Store(cfg)
}

func rmsOf(frames [][2]float64) float64 {
	if len(frames) == 0 {
		return 0
	}
	var sum float64
	for _, f := range frames {
		sum += (f[0]*f[0] + f[1]*f[1]) / 2
	}
	return math.Sqrt(sum / float64(len(frames)))
}

// trimLeadingSilence drops the silence at the start of tp before it plays.
// The first audible block is kept in a small prefix buffer, so nothing
// musical is lost. It runs on the caller's goroutine, never the speaker's.
func (p *Player) trimLeadingSilence(tp *trackPipeline) time.Duration {
	cfg, _ := p.silence.Load().(*silenceConfig)
	if cfg == nil || !cfg.start || tp == nil || tp.live {
		return 0
	}
	block := max(1, p.sr.N(50*time.Millisecond))
	limit := p.sr.N(maxLeadingSilence)
	buf := make([][2]float64, block)
	skipped := 0
	for skipped < limit {
		n := tp.readBlocking(buf, 10*time.Second)
		if n == 0 {
			break
		}
		if rmsOf(buf[:n]) >= cfg.threshold {
			// Keep the audible block, starting at its first loud sample.
			first := 0
			for first < n && math.Abs(buf[first][0])+math.Abs(buf[first][1]) < cfg.threshold {
				first++
			}
			tp.stream = &prefixStreamer{prefix: append([][2]float64(nil), buf[first:n]...), src: tp.stream}
			skipped += first
			return p.sr.D(skipped)
		}
		skipped += n
	}
	if skipped >= limit {
		// Long quiet opening: it was not padding. Nothing can be put back
		// on a pipe, but 15 s of true silence is never music.
		return p.sr.D(skipped)
	}
	return p.sr.D(skipped)
}

// readBlocking fills dst from the pipeline, waiting for a network buffer
// instead of taking its underrun silence. It returns 0 at the end.
func (tp *trackPipeline) readBlocking(dst [][2]float64, timeout time.Duration) int {
	if tp.livePrefetch != nil {
		return tp.livePrefetch.readWait(dst, timeout)
	}
	n, ok := tp.stream.Stream(dst)
	if !ok {
		return 0
	}
	return n
}

// readWait copies buffered frames into dst, waiting for the download.
func (p *livePrefetchStreamer) readWait(dst [][2]float64, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.availableToReadLocked() < len(dst) && !p.done && !p.closed && time.Now().Before(deadline) {
		p.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
		p.mu.Lock()
	}
	n := min(len(dst), p.availableToReadLocked())
	for i := range n {
		dst[i] = p.buf[p.r]
		p.r = (p.r + 1) % len(p.buf)
	}
	p.consumed += int64(n)
	p.full = false
	p.buffering = false
	p.cond.Broadcast()
	return n
}

// prefixStreamer plays a few kept frames, then the source.
type prefixStreamer struct {
	prefix [][2]float64
	src    beep.Streamer
}

func (s *prefixStreamer) Stream(samples [][2]float64) (int, bool) {
	n := 0
	if len(s.prefix) > 0 {
		n = copy(samples, s.prefix)
		s.prefix = s.prefix[n:]
		if n == len(samples) {
			return n, true
		}
	}
	m, ok := s.src.Stream(samples[n:])
	if !ok && n == 0 {
		return 0, false
	}
	return n + m, true
}

func (s *prefixStreamer) Err() error { return s.src.Err() }

// endSilence tracks trailing silence in the gapless streamer.
type endSilence struct {
	run int // consecutive silent frames
}

// silentEnd reports whether the current track has gone quiet for good:
// silent for the minimum time inside the last endWindow of a known length.
func (g *gaplessStreamer) silentEnd(samples [][2]float64, cfg *silenceConfig) bool {
	if cfg == nil || !cfg.end || g.clock == nil {
		g.endQuiet.run = 0
		return false
	}
	if rmsOf(samples) >= cfg.threshold {
		g.endQuiet.run = 0
		return false
	}
	g.endQuiet.run += len(samples)
	if g.sr.D(g.endQuiet.run) < cfg.minEnd {
		return false
	}
	pos, dur := g.clock()
	return dur > 0 && dur-pos <= endWindow
}

// silenceFlag is the shared silence configuration.
type silenceFlag = atomic.Value
