package player

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep/v2"
)

// mixDebug prints the start of each mix to stderr.
var mixDebug = os.Getenv("GUIAMP_DEBUG_MIX") != ""

// gaplessStreamer is a beep.Streamer that sequences tracks with zero-gap
// transitions, or with a crossfade or DJ mix when the next track carries a
// Transition. It sits at the bottom of the audio pipeline and manages track
// sources while the speed/EQ/tap/volume/ctrl chain above it lives forever.
//
// It always returns (len(samples), true) — it never stops the speaker.
// When no audio is available, it fills silence.
type gaplessStreamer struct {
	mu             sync.Mutex
	current        beep.Streamer // active track (decoded + resampled)
	next           beep.Streamer // preloaded next track
	nextPlan       *Transition   // how to mix into next; nil = gapless
	currentVersion uint64        // increments whenever manual playback replaces current
	nextToken      uint64        // identifies the pipeline represented by next
	nextTokenSeq   uint64
	drained        atomic.Bool // true when current exhausts with no next

	// mix is the transition in progress; the outgoing track plays inside
	// it as a tail while current is already the incoming track.
	mix        *mixState
	mixing     atomic.Bool
	tailClosed bool // a Replace dropped a mix whose tail is still open

	sr beep.SampleRate
	// silence holds the *silenceConfig; endQuiet counts trailing silence.
	silence  atomic.Value
	endQuiet endSilence
	// clock reads the position and duration of the current track. It runs
	// on the audio goroutine under the speaker lock.
	clock func() (time.Duration, time.Duration)

	// onSwap is called on the audio goroutine, under the speaker lock, on a
	// valid transition. keepOld is true when the old track keeps playing as
	// the tail of a mix; onTailDone then reports when it may be closed.
	onSwap     func(token uint64, keepOld bool)
	onTailDone func()
	onMix      func(Transition)
}

// Stream reads samples from the current track. On exhaustion, it seamlessly
// fills remaining samples from the next track (like beep.Seq). With a
// planned transition it starts mixing the next track at the planned point.
// If no next track is available, it sets drained=true and fills silence.
func (g *gaplessStreamer) Stream(samples [][2]float64) (int, bool) {
	g.mu.Lock()
	cur := g.current
	currentVersion := g.currentVersion
	mix := g.mix
	plan := g.nextPlan
	hasNext := g.next != nil
	dropped := g.tailClosed
	g.tailClosed = false
	tailDone := g.onTailDone
	g.mu.Unlock()

	if dropped && tailDone != nil {
		tailDone()
	}
	if cur == nil {
		clear(samples)
		return len(samples), true
	}
	if mix != nil {
		g.streamMix(samples, mix)
		return len(samples), true
	}

	if plan != nil && hasNext && g.clock != nil {
		if until, ok := g.framesUntilMix(plan); ok && until < len(samples) {
			n := 0
			if until > 0 {
				n, _ = cur.Stream(samples[:until])
			}
			if m := g.beginMix(currentVersion); m != nil {
				g.streamMix(samples[n:], m)
				return len(samples), true
			}
			clear(samples[n:])
			return len(samples), true
		}
	}

	n, ok := cur.Stream(samples)

	// Trailing silence at the very end of a track: hand over now instead
	// of playing the silence. A planned mix handles its own end.
	if ok && n == len(samples) && plan == nil {
		cfg, _ := g.silence.Load().(*silenceConfig)
		if g.silentEnd(samples, cfg) {
			g.endQuiet.run = 0
			n = 0
			ok = false
		}
	}

	if !ok || n < len(samples) {
		// Current track exhausted — try to seamlessly continue with next
		g.mu.Lock()
		if currentVersion != g.currentVersion {
			// A manual replacement won while the old streamer was reading. It
			// owns the next callback; never let the stale read clobber it.
			g.mu.Unlock()
			clear(samples[n:])
			return len(samples), true
		}
		next := g.next
		nextToken := g.nextToken
		g.next = nil
		g.nextPlan = nil
		g.nextToken = 0
		g.current = next
		g.currentVersion++
		swapFn := g.onSwap
		g.mu.Unlock()

		if next != nil {
			// Fill remaining buffer from next track — zero gap
			if n < len(samples) {
				filled, _ := next.Stream(samples[n:])
				n += filled
			}
			// Publish the transition before the UI can observe the new stream as
			// drained. Resource cleanup is dispatched by the callback.
			if swapFn != nil {
				swapFn(nextToken, false)
			}
			g.drained.Store(false)
		} else {
			// No next track — we've drained
			g.drained.Store(true)
		}
	}

	// Fill any remaining with silence
	for i := n; i < len(samples); i++ {
		samples[i] = [2]float64{}
	}
	return len(samples), true
}

// framesUntilMix returns how many output frames remain before the planned
// mix starts. ok is false when the start cannot be placed, for example
// with an unknown duration.
func (g *gaplessStreamer) framesUntilMix(plan *Transition) (int, bool) {
	pos, dur := g.clock()
	start := plan.OutStart
	if start <= 0 {
		if dur <= 0 {
			return 0, false
		}
		start = dur - plan.Length
	}
	if dur > 0 && start > dur {
		start = dur - plan.Length
	}
	return max(0, g.sr.N(start-pos)), true
}

// beginMix makes the next track current inside a new mix. It returns nil
// when a manual replacement won the race.
func (g *gaplessStreamer) beginMix(version uint64) *mixState {
	g.mu.Lock()
	if version != g.currentVersion || g.next == nil || g.nextPlan == nil {
		g.mu.Unlock()
		return nil
	}
	plan := *g.nextPlan
	if mixDebug {
		pos, dur := g.clock()
		fmt.Fprintf(os.Stderr, "mix: begin %s at pos %v dur %v (planned out %v, length %v)\n", plan.Style, pos, dur, plan.OutStart, plan.Length)
	}
	in := newDeck(g.next, g.sr, plan.InRate, plan.Beat)
	m := newMixState(plan, g.current, in, g.sr)
	token := g.nextToken
	g.current = in
	g.mix = m
	g.next = nil
	g.nextPlan = nil
	g.nextToken = 0
	g.currentVersion++
	g.mixing.Store(true)
	swapFn := g.onSwap
	onMix := g.onMix
	g.mu.Unlock()
	if swapFn != nil {
		swapFn(token, true)
	}
	if onMix != nil {
		onMix(plan)
	}
	g.drained.Store(false)
	return m
}

func (g *gaplessStreamer) streamMix(samples [][2]float64, m *mixState) {
	if len(samples) == 0 {
		return
	}
	_, _, done := m.stream(samples)
	if !done {
		return
	}
	g.mu.Lock()
	finished := g.mix == m
	if finished {
		g.mix = nil
		m.finish()
		g.mixing.Store(false)
	}
	tailDone := g.onTailDone
	g.mu.Unlock()
	if finished && tailDone != nil {
		tailDone()
	}
}

// Err always returns nil — errors are handled per-track at decode time.
func (g *gaplessStreamer) Err() error { return nil }

// SetNext preloads the next track's resampled streamer for gapless transition.
func (g *gaplessStreamer) SetNext(s beep.Streamer) uint64 {
	return g.SetNextPlan(s, nil)
}

// SetNextPlan preloads the next track with an optional transition.
func (g *gaplessStreamer) SetNextPlan(s beep.Streamer, plan *Transition) uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next = s
	g.nextPlan = nil
	if s == nil {
		g.nextToken = 0
		return 0
	}
	g.nextPlan = plan
	g.nextTokenSeq++
	g.nextToken = g.nextTokenSeq
	return g.nextToken
}

// dropMixLocked ends a mix that a manual change interrupts. Its tail is
// closed from the next Stream call, outside the caller's locks.
func (g *gaplessStreamer) dropMixLocked() {
	if g.mix != nil {
		g.mix = nil
		g.mixing.Store(false)
		g.tailClosed = true
	}
}

// Replace interrupts the current track and starts a new one immediately.
// Used for manual skip/prev/select operations.
func (g *gaplessStreamer) Replace(s beep.Streamer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dropMixLocked()
	g.current = s
	g.next = nil
	g.nextPlan = nil
	g.currentVersion++
	g.nextToken = 0
	g.drained.Store(false)
}

// Clear removes both current and next tracks. The streamer will output
// silence until Replace or SetNext is called.
func (g *gaplessStreamer) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dropMixLocked()
	g.current = nil
	g.next = nil
	g.nextPlan = nil
	g.currentVersion++
	g.nextToken = 0
	g.drained.Store(false)
}

// Drained reports whether the current track ended with no next track queued.
func (g *gaplessStreamer) Drained() bool {
	return g.drained.Load()
}
