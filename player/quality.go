package player

import (
	"math"
	"sync/atomic"
	"time"
)

// StreamQuality picks the YouTube (yt-dlp) audio format.
type StreamQuality int32

const (
	// QualityAuto follows the measured download speed between tracks.
	QualityAuto StreamQuality = iota
	// QualityHigh is Opus at about 160 kbps, transparent for most ears.
	QualityHigh
	// QualityBalanced is Opus at about 70 kbps.
	QualityBalanced
	// QualitySaver is Opus at about 50 kbps for very slow connections.
	QualitySaver
)

var (
	streamQuality atomic.Int32
	// netRatio is the measured audio seconds downloaded per second, as
	// float64 bits; 0 means not measured yet.
	netRatio atomic.Uint64
	// networkBuffer is how much decoded audio a network stream keeps ahead.
	networkBuffer atomic.Int64
)

func init() { networkBuffer.Store(int64(12 * time.Second)) }

// SetStreamQuality sets the format of the next YouTube tracks.
func SetStreamQuality(q StreamQuality) { streamQuality.Store(int32(q)) }

// SetNetworkBuffer sets the read-ahead of network streams opened from now.
func SetNetworkBuffer(d time.Duration) {
	networkBuffer.Store(int64(max(2*time.Second, min(d, 120*time.Second))))
}

// NetworkSpeed returns the measured download speed in audio seconds per
// second (1 = just real time), or 0 before the first measurement.
func NetworkSpeed() float64 { return math.Float64frombits(netRatio.Load()) }

// EffectiveQuality resolves Auto to the profile the next track uses.
func EffectiveQuality() StreamQuality {
	q := StreamQuality(streamQuality.Load())
	if q != QualityAuto {
		return q
	}
	switch r := NetworkSpeed(); {
	case r == 0 || r >= 3:
		return QualityHigh
	case r >= 1.6:
		return QualityBalanced
	default:
		return QualitySaver
	}
}

// recordNetworkSpeed folds one fill measurement into the running average.
func recordNetworkSpeed(audio, wall time.Duration) {
	if wall < 50*time.Millisecond || audio <= 0 {
		return
	}
	r := audio.Seconds() / wall.Seconds()
	old := NetworkSpeed()
	if old > 0 {
		r = old*0.8 + r*0.2
	}
	netRatio.Store(math.Float64bits(r))
}

const ytdlFallback = "bestaudio[protocol=https]/bestaudio[protocol=http]/bestaudio[protocol!=m3u8_native][protocol!=m3u8]/bestaudio/best"

// ytdlFormat is the yt-dlp format selector of the current profile. Opus is
// preferred: at the same quality it needs far less bandwidth than AAC.
func ytdlFormat() string {
	switch EffectiveQuality() {
	case QualityBalanced:
		return "bestaudio[acodec=opus][abr<=100][protocol=https]/bestaudio[acodec=opus][abr<=100]/" + ytdlFallback
	case QualitySaver:
		return "worstaudio[acodec=opus][protocol=https]/worstaudio[acodec=opus]/worstaudio[protocol=https]/" + ytdlFallback
	}
	return "bestaudio[acodec=opus][protocol=https]/bestaudio[acodec=opus]/" + ytdlFallback
}
