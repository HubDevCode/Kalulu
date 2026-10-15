package player

import (
	"math"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// levelStreamer plays segments of constant level.
type levelStreamer struct {
	segs []struct {
		frames int
		level  float64
	}
	pos int
}

func (l *levelStreamer) add(frames int, level float64) *levelStreamer {
	l.segs = append(l.segs, struct {
		frames int
		level  float64
	}{frames, level})
	return l
}

func (l *levelStreamer) Stream(samples [][2]float64) (int, bool) {
	n := 0
	for n < len(samples) {
		at, level, ok := l.pos, 0.0, false
		for _, s := range l.segs {
			if at < s.frames {
				level, ok = s.level, true
				break
			}
			at -= s.frames
		}
		if !ok {
			break
		}
		// A sine keeps the RMS at level/sqrt(2).
		v := level * math.Sin(float64(l.pos)*0.3)
		samples[n] = [2]float64{v, v}
		n++
		l.pos++
	}
	return n, n > 0
}

func (l *levelStreamer) Err() error { return nil }

func testPlayer(sr beep.SampleRate, s SilenceSettings) *Player {
	p := &Player{sr: sr, gapless: &gaplessStreamer{sr: sr}}
	p.SetSilenceSkip(s)
	return p
}

func TestTrimLeadingSilence(t *testing.T) {
	const sr = beep.SampleRate(10000)
	tests := []struct {
		name    string
		src     *levelStreamer
		want    time.Duration
		tolerMs float64
	}{
		{"two seconds of silence", (&levelStreamer{}).add(20000, 0).add(20000, 0.5), 2 * time.Second, 51},
		{"quiet ambient intro is kept", (&levelStreamer{}).add(20000, 0.014).add(20000, 0.5), 0, 1},
		{"no silence", (&levelStreamer{}).add(20000, 0.5), 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPlayer(sr, DefaultSilenceSettings())
			tp := &trackPipeline{stream: tt.src}
			got := p.trimLeadingSilence(tp)
			if diff := math.Abs(float64(got-tt.want) / float64(time.Millisecond)); diff > tt.tolerMs {
				t.Fatalf("trimmed %v, want %v", got, tt.want)
			}
			// What plays next is the audible part.
			buf := make([][2]float64, 100)
			n, _ := tp.stream.Stream(buf)
			if n == 0 || rmsOf(buf[:n]) < 0.005 {
				t.Fatalf("first played block is silent (rms %v)", rmsOf(buf[:n]))
			}
		})
	}
}

func TestTrimDisabled(t *testing.T) {
	p := testPlayer(1000, SilenceSettings{})
	src := (&levelStreamer{}).add(2000, 0).add(2000, 0.5)
	tp := &trackPipeline{stream: src}
	if got := p.trimLeadingSilence(tp); got != 0 || src.pos != 0 {
		t.Fatalf("trimmed %v with the skip off", got)
	}
}

func TestEndSilenceSkipsToNext(t *testing.T) {
	const sr = beep.SampleRate(1000)
	// 60 s of music then 10 s of silence; the next track is 0.25.
	out := (&levelStreamer{}).add(60000, 0.5).add(10000, 0)
	in := &constStreamer{level: 0.25, total: 100000}
	g := &gaplessStreamer{sr: sr}
	g.silence.Store(&silenceConfig{end: true, threshold: math.Pow(10, -55.0/20), minEnd: 1500 * time.Millisecond})
	g.clock = func() (time.Duration, time.Duration) { return sr.D(out.pos), 70 * time.Second }
	swaps := 0
	g.onSwap = func(uint64, bool) { swaps++ }
	g.Replace(out)
	g.SetNext(in)
	buf := make([][2]float64, 100)
	for range 650 {
		g.Stream(buf)
	}
	if swaps != 1 {
		t.Fatalf("swaps = %d, want 1", swaps)
	}
	// The swap happens 1.5 s into the silence, not after all 10 s.
	if got := sr.D(out.pos); got > 62*time.Second {
		t.Fatalf("outgoing played until %v, want about 61.5 s", got)
	}
}

func TestMidTrackSilenceIsKept(t *testing.T) {
	const sr = beep.SampleRate(1000)
	// A 5 s break 40 s before the end is part of the track.
	out := (&levelStreamer{}).add(30000, 0.5).add(5000, 0).add(40000, 0.5)
	g := &gaplessStreamer{sr: sr}
	g.silence.Store(&silenceConfig{end: true, threshold: math.Pow(10, -55.0/20), minEnd: 1500 * time.Millisecond})
	g.clock = func() (time.Duration, time.Duration) { return sr.D(out.pos), 75 * time.Second }
	swaps := 0
	g.onSwap = func(uint64, bool) { swaps++ }
	g.Replace(out)
	g.SetNext(&constStreamer{level: 0.25, total: 1000})
	buf := make([][2]float64, 100)
	for range 400 {
		g.Stream(buf)
	}
	if swaps != 0 {
		t.Fatalf("a mid-track break caused %d swaps", swaps)
	}
}

func TestYTDLFormatProfiles(t *testing.T) {
	t.Cleanup(func() { SetStreamQuality(QualityAuto); netRatio.Store(0) })
	tests := []struct {
		q     StreamQuality
		speed float64
		want  StreamQuality
	}{
		{QualityHigh, 0.5, QualityHigh},
		{QualitySaver, 10, QualitySaver},
		{QualityAuto, 0, QualityHigh},
		{QualityAuto, 5, QualityHigh},
		{QualityAuto, 2, QualityBalanced},
		{QualityAuto, 1.1, QualitySaver},
	}
	for _, tt := range tests {
		SetStreamQuality(tt.q)
		netRatio.Store(math.Float64bits(tt.speed))
		if got := EffectiveQuality(); got != tt.want {
			t.Errorf("quality %d at speed %.1f = %d, want %d", tt.q, tt.speed, got, tt.want)
		}
	}
	SetStreamQuality(QualitySaver)
	if f := ytdlFormat(); f[:10] != "worstaudio" {
		t.Errorf("saver format = %s", f)
	}
}
