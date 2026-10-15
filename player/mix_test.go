package player

import (
	"math"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// constStreamer plays a constant level for a fixed number of frames.
type constStreamer struct {
	level float64
	total int
	pos   int
}

func (c *constStreamer) Stream(samples [][2]float64) (int, bool) {
	if c.pos >= c.total {
		return 0, false
	}
	n := min(len(samples), c.total-c.pos)
	for i := range n {
		samples[i] = [2]float64{c.level, c.level}
	}
	c.pos += n
	return n, true
}

func (c *constStreamer) Err() error { return nil }

func TestGaplessMixesPlannedTransition(t *testing.T) {
	const sr = beep.SampleRate(10000)
	out := &constStreamer{level: 0.5, total: 10000} // 1 s
	in := &constStreamer{level: 0.25, total: 10000}
	var swaps []bool
	tailDone := 0
	var mixed []Transition
	g := &gaplessStreamer{sr: sr}
	g.clock = func() (time.Duration, time.Duration) { return sr.D(out.pos), sr.D(out.total) }
	g.onSwap = func(_ uint64, keepOld bool) { swaps = append(swaps, keepOld) }
	g.onTailDone = func() { tailDone++ }
	g.onMix = func(tr Transition) { mixed = append(mixed, tr) }
	g.Replace(out)
	plan := &Transition{Style: StyleFade, Length: 200 * time.Millisecond, InRate: 1}
	g.SetNextPlan(in, plan)

	buf := make([][2]float64, 500)
	var frames [][2]float64
	for range 30 {
		g.Stream(buf)
		frames = append(frames, buf...)
	}
	if len(swaps) != 1 || !swaps[0] {
		t.Fatalf("swaps = %v, want one swap that keeps the old track", swaps)
	}
	if len(mixed) != 1 || tailDone != 1 {
		t.Fatalf("mixed = %d, tailDone = %d", len(mixed), tailDone)
	}
	// Before the mix only the outgoing level plays.
	if got := frames[7000][0]; math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("frame 7000 = %v, want 0.5", got)
	}
	// The mix starts at 0.8 s: equal-power fade halfway at 0.9 s.
	mid := frames[9000][0]
	want := 0.5*math.Cos(math.Pi/4) + 0.25*math.Sin(math.Pi/4)
	if math.Abs(mid-want) > 0.02 {
		t.Fatalf("mid-mix level = %v, want about %v", mid, want)
	}
	// After the mix only the incoming level plays.
	if got := frames[11000][0]; math.Abs(got-0.25) > 1e-6 {
		t.Fatalf("frame 11000 = %v, want 0.25", got)
	}
	if g.mixing.Load() {
		t.Fatal("still mixing after the transition")
	}
}

func TestGaplessReplaceDropsMix(t *testing.T) {
	const sr = beep.SampleRate(1000)
	out := &constStreamer{level: 1, total: 1000}
	in := &constStreamer{level: 1, total: 1000}
	tailDone := 0
	g := &gaplessStreamer{sr: sr}
	g.clock = func() (time.Duration, time.Duration) { return sr.D(out.pos), sr.D(out.total) }
	g.onSwap = func(uint64, bool) {}
	g.onTailDone = func() { tailDone++ }
	g.Replace(out)
	g.SetNextPlan(in, &Transition{Style: StyleBlend, Length: 500 * time.Millisecond, Beat: 100 * time.Millisecond, InRate: 1})
	buf := make([][2]float64, 100)
	for range 7 {
		g.Stream(buf)
	}
	if !g.mixing.Load() {
		t.Fatal("mix did not start")
	}
	g.Replace(&constStreamer{level: 0, total: 1000})
	g.Stream(buf)
	if tailDone != 1 {
		t.Fatalf("tailDone = %d after a replace, want 1", tailDone)
	}
}

func TestBlendSwapsBass(t *testing.T) {
	m := newMixState(Transition{Style: StyleBlend, Length: 8 * time.Second, Beat: 500 * time.Millisecond}, &constStreamer{}, newDeck(&constStreamer{}, 44100, 1, 0), 44100)
	m.apply(0.1)
	if m.in.params.bassDB > -20 || m.out.params.bassDB != 0 {
		t.Fatalf("early: in bass %v, out bass %v", m.in.params.bassDB, m.out.params.bassDB)
	}
	m.apply(0.9)
	if m.in.params.bassDB < -0.01 || m.out.params.bassDB > -20 {
		t.Fatalf("late: in bass %v, out bass %v", m.in.params.bassDB, m.out.params.bassDB)
	}
}
