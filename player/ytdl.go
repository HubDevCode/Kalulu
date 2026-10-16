package player

import (
	"bufio"
	"context"
	"fmt"
	"github.com/bjarneo/cliamp/applog"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"

	"github.com/bjarneo/cliamp/internal/sysproc"
	"github.com/bjarneo/cliamp/internal/ytdlcookies"
	"github.com/bjarneo/cliamp/internal/ytdlp"
)

// pipeBufSize is the buffer size for audio pipe readers (yt-dlp, ffmpeg).
const pipeBufSize = 64 * 1024

// ytdlPipeTimeout limits how long we wait for yt-dlp to produce initial audio.
const ytdlPipeTimeout = 30 * time.Second

// ytdlCauseGrace bounds how long buildYTDLPipeline waits, after the audio pipe
// closes with no data, for yt-dlp or ffmpeg to report why. yt-dlp typically
// exits quickly with a stderr message (bot wall, 404, DRM); this only matters
// when the process is slow to flush and exit.
const ytdlCauseGrace = 3 * time.Second

const ytdlPipelineMaxAttempts = 3

// YTDLPAvailable reports whether yt-dlp is installed and on PATH.
func YTDLPAvailable() bool {
	_, err := exec.LookPath("yt-dlp")
	return err == nil
}

func appendYTDLCookieArgs(args []string, pageURL string) []string {
	if browser := ytdlcookies.ForURL(pageURL); browser != "" {
		return append(args, "--cookies-from-browser", browser)
	}
	return args
}

// probeYTDLDuration runs a quick yt-dlp --print duration to obtain
// the track duration when --flat-playlist didn't provide it.
func probeYTDLDuration(pageURL string) time.Duration {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"--skip-download", "--no-playlist", "--socket-timeout", "10", "--print", "duration"}
	args = appendYTDLCookieArgs(args, pageURL)
	// "--" stops yt-dlp parsing pageURL as a flag. Callers gate on
	// playlist.IsURL, but keep the terminator so a future caller cannot turn
	// a crafted URL into --exec and reach arbitrary command execution.
	args = append(args, "--", pageURL)
	cmd := sysproc.CommandContext(ctx, "yt-dlp", args...)
	// WaitDelay ensures cmd.Output() doesn't hang indefinitely if the
	// process is killed but I/O pipe goroutines haven't drained. Without
	// this, a zombie yt-dlp child keeping stdout open can block Output()
	// forever, which in turn blocks the start and leaves the UI stuck at
	// "Buffering...".
	cmd.WaitDelay = 3 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// ffmpegInstallHint returns a platform-specific install command suggestion.
func ffmpegInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install ffmpeg"
	case "linux":
		if _, err := exec.LookPath("apt-get"); err == nil {
			return "sudo apt install ffmpeg"
		}
		if _, err := exec.LookPath("pacman"); err == nil {
			return "sudo pacman -S ffmpeg"
		}
		return "see https://ffmpeg.org/download.html"
	case "windows":
		return "winget install ffmpeg"
	default:
		return "see https://ffmpeg.org/download.html"
	}
}

// ytdlPipeStreamer streams PCM audio from a yt-dlp | ffmpeg pipe chain.
// yt-dlp downloads the best audio and writes raw data to stdout; ffmpeg reads
// that via a pipe and converts it to PCM on its stdout, which we consume.
type ytdlPipeStreamer struct {
	pipeReport // total stays 0: the length of a piped page is unknown
	ytdlCmd    *exec.Cmd
	ffmpegCmd  *exec.Cmd
	pipe       io.ReadCloser // ffmpeg stdout (PCM output)
	reader     *bufio.Reader // buffered reader over pipe
	ytdlErr    <-chan error  // yt-dlp exit error from monitoring goroutine
	ffmpegErr  <-chan error  // ffmpeg exit error from monitoring goroutine
	ytdlDone   <-chan struct{}
	ffmpegDone <-chan struct{}
	pcmBuf     []byte
	f32        bool // true = f32le, false = s16le
	closeOnce  sync.Once
}

func (y *ytdlPipeStreamer) Stream(samples [][2]float64) (int, bool) {
	n, ok := streamFromReader(y.reader, samples, &y.pcmBuf, y.f32, y.state)
	y.state.pos.Add(int64(n))
	// On EOF with no frames read, surface why the pipe closed (yt-dlp bot
	// wall, 404, DRM, or undecodable ffmpeg input) instead of a bare EOF.
	if !ok && n == 0 && y.state.err.load() == nil {
		if cause := y.waitCause(0); cause != nil {
			y.state.err.publish(cause)
		}
	}
	return n, ok
}

// waitCause reports why the audio pipe closed without producing audio,
// preferring yt-dlp's reason (bot wall, 404, DRM, region block) over ffmpeg's
// (undecodable input). With d <= 0 it polls without blocking; otherwise it
// waits up to d for a process to report. Returns nil if neither reported an
// error, leaving the caller to surface the bare EOF.
func (y *ytdlPipeStreamer) waitCause(d time.Duration) error {
	if d <= 0 {
		select {
		case e := <-y.ytdlErr:
			if e != nil {
				return e
			}
		default:
		}
		select {
		case e := <-y.ffmpegErr:
			return e
		default:
			return nil
		}
	}
	deadline := time.After(d)
	ytdlDone, ffmpegDone := false, false
	var ffErr error
	for !ytdlDone || !ffmpegDone {
		select {
		case e := <-y.ytdlErr:
			ytdlDone = true
			if e != nil {
				return e
			}
		case e := <-y.ffmpegErr:
			ffmpegDone = true
			ffErr = e
		case <-deadline:
			return ffErr
		}
	}
	return ffErr
}

func (y *ytdlPipeStreamer) Seek(int) error { return nil }

// interrupt releases a blocked PCM read without waiting for yt-dlp or ffmpeg.
// A stalled download otherwise holds Stream, and the speaker lock with it,
// until yt-dlp gets data or gives up. Close reaps both processes later.
func (y *ytdlPipeStreamer) interrupt() {
	_ = y.pipe.Close()
	if y.ytdlCmd != nil && y.ytdlCmd.Process != nil {
		_ = y.ytdlCmd.Process.Kill()
	}
	if y.ffmpegCmd.Process != nil {
		_ = y.ffmpegCmd.Process.Kill()
	}
}

func (y *ytdlPipeStreamer) Close() error {
	y.closeOnce.Do(func() {
		// Kill both processes to stop downloading/decoding.
		if y.ytdlCmd != nil && y.ytdlCmd.Process != nil {
			y.ytdlCmd.Process.Kill()
		}
		if y.ffmpegCmd.Process != nil {
			y.ffmpegCmd.Process.Kill()
		}
		y.pipe.Close()
		// The monitor goroutines own Wait. Killing both children and waiting
		// for their done signals guarantees Close does not leave zombies.
		if y.ytdlDone != nil {
			<-y.ytdlDone
		}
		if y.ffmpegDone != nil {
			<-y.ffmpegDone
		}
	})
	return nil
}

// monitorExit waits for cmd to exit and reports the result on a buffered
// channel: a wrapped error preferring captured stderr over the bare exit code
// on failure, or nil on clean exit. The channel is buffered so the goroutine
// always completes even with no receiver (e.g. after Close kills the process).
func monitorExit(cmd *exec.Cmd, stderr *limitedBuffer, name string) (<-chan error, <-chan struct{}) {
	ch := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := cmd.Wait()
		switch trimmed := strings.TrimSpace(stderr.String()); {
		case err == nil:
			ch <- nil
		case trimmed != "":
			ch <- fmt.Errorf("%s: %w: %s", name, err, trimmed)
		default:
			ch <- fmt.Errorf("%s: %w", name, err)
		}
	}()
	return ch, done
}

// decodeYTDLPipe starts a yt-dlp | ffmpeg pipe chain for the given page URL
// and returns a streaming PCM decoder. If startSec > 0, ffmpeg -ss is used
// to skip to the desired position in the input stream.
func decodeYTDLPipe(pageURL string, sr beep.SampleRate, bitDepth, startSec int) (*ytdlPipeStreamer, beep.Format, error) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return nil, beep.Format{}, fmt.Errorf("yt-dlp is required — install: %s", ytdlp.InstallHint())
	}
	if err := requireFFmpeg(); err != nil {
		return nil, beep.Format{}, err
	}

	// os.Pipe connects yt-dlp stdout → ffmpeg stdin.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("os.Pipe: %w", err)
	}

	// Start yt-dlp: download best audio to stdout.
	// Prefer direct HTTPS/HTTP streams over HLS (m3u8). HLS requires segment
	// downloading and muxing which doesn't pipe cleanly to stdout.
	// Live streams (e.g. YouTube live) expose no audio-only formats at all,
	// only muxed video+audio over HLS, so fall back to "best" as a last
	// resort; the ffmpeg stage below outputs PCM audio and drops the video.
	ytdlArgs := []string{
		"-f", ytdlFormat(),
		"--no-playlist",
		"--quiet",
		"--no-warnings",
		"--socket-timeout", "15",
		"-o", "-",
	}
	ytdlArgs = appendYTDLCookieArgs(ytdlArgs, pageURL)
	ytdlArgs = append(ytdlArgs, "--", pageURL)
	ytdlCmd := sysproc.Command("yt-dlp", ytdlArgs...)
	ytdlCmd.Stdout = pw
	var ytdlStderr limitedBuffer
	ytdlCmd.Stderr = &ytdlStderr
	if err := ytdlCmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, beep.Format{}, fmt.Errorf("yt-dlp start: %w", err)
	}

	// Start ffmpeg: read from pipe, output PCM to stdout.
	// If startSec > 0, use -ss to seek into the input stream.
	_, _, precision := ffmpegPCMArgs(bitDepth)
	var ffmpegArgs []string
	if startSec > 0 {
		ffmpegArgs = append(ffmpegArgs, "-ss", strconv.Itoa(startSec))
	}
	ffmpegArgs = append(ffmpegArgs, "-i", "pipe:0")
	ffmpegArgs = append(ffmpegArgs, pcmOutputArgs(sr, bitDepth)...)
	ffmpegCmd := sysproc.Command("ffmpeg", ffmpegArgs...)
	ffmpegCmd.Stdin = pr
	var ffmpegStderr limitedBuffer
	ffmpegCmd.Stderr = &ffmpegStderr
	// An owned pipe carries ffmpeg's PCM, like the yt-dlp → ffmpeg hop above.
	// StdoutPipe would tie the read end to cmd.Wait, which monitorExit runs
	// during playback: Wait closes it as soon as ffmpeg exits, dropping PCM
	// still in the pipe and failing the next read instead of reaching EOF.
	pcmR, pcmW, err := os.Pipe()
	if err != nil {
		pw.Close()
		pr.Close()
		ytdlCmd.Process.Kill()
		ytdlCmd.Wait()
		return nil, beep.Format{}, fmt.Errorf("os.Pipe: %w", err)
	}
	ffmpegCmd.Stdout = pcmW
	if err := ffmpegCmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		pcmR.Close()
		pcmW.Close()
		ytdlCmd.Process.Kill()
		ytdlCmd.Wait()
		return nil, beep.Format{}, fmt.Errorf("ffmpeg start: %w", err)
	}

	// Close parent's copies of pipe ends. yt-dlp owns pw (write end), ffmpeg
	// owns pr (read end) and pcmW (PCM write end). If the parent keeps these
	// open, EOF won't propagate when the owning process exits.
	pw.Close()
	pr.Close()
	pcmW.Close()

	// Monitor each process's exit so we can surface why the pipe closed. A
	// process's stderr is only safe to read after Wait() returns, so the
	// capture happens inside monitorExit.
	ytdlErrCh, ytdlDone := monitorExit(ytdlCmd, &ytdlStderr, "yt-dlp")
	ffmpegErrCh, ffmpegDone := monitorExit(ffmpegCmd, &ffmpegStderr, "ffmpeg")

	format := beep.Format{
		SampleRate:  sr,
		NumChannels: 2,
		Precision:   precision,
	}

	return &ytdlPipeStreamer{
		ytdlCmd:    ytdlCmd,
		ffmpegCmd:  ffmpegCmd,
		pipe:       pcmR,
		reader:     bufio.NewReaderSize(pcmR, pipeBufSize),
		ytdlErr:    ytdlErrCh,
		ffmpegErr:  ffmpegErrCh,
		ytdlDone:   ytdlDone,
		ffmpegDone: ffmpegDone,
		pipeReport: pipeReport{state: newPipeStreamState(0)},
		f32:        bitDepth == 32,
	}, format, nil
}

// decodeYTDLDirect plays the direct audio URL of a YouTube page with ffmpeg
// alone: it starts in a fraction of a second and seeks with HTTP ranges.
func decodeYTDLDirect(res YTResolved, sr beep.SampleRate, bitDepth, startSec int) (*ytdlPipeStreamer, beep.Format, error) {
	if err := requireFFmpeg(); err != nil {
		return nil, beep.Format{}, err
	}
	_, _, precision := ffmpegPCMArgs(bitDepth)
	args := []string{"-nostdin", "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_on_network_error", "1", "-reconnect_delay_max", "5", "-rw_timeout", "15000000"}
	if res.UserAgent != "" {
		args = append(args, "-user_agent", res.UserAgent)
	}
	if startSec > 0 {
		args = append(args, "-ss", strconv.Itoa(startSec))
	}
	args = append(args, "-i", res.URL)
	args = append(args, pcmOutputArgs(sr, bitDepth)...)
	ffmpegCmd := sysproc.Command("ffmpeg", args...)
	var ffmpegStderr limitedBuffer
	ffmpegCmd.Stderr = &ffmpegStderr
	pcmR, pcmW, err := os.Pipe()
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("os.Pipe: %w", err)
	}
	ffmpegCmd.Stdout = pcmW
	if err := ffmpegCmd.Start(); err != nil {
		pcmR.Close()
		pcmW.Close()
		return nil, beep.Format{}, fmt.Errorf("ffmpeg start: %w", err)
	}
	pcmW.Close()
	ffmpegErrCh, ffmpegDone := monitorExit(ffmpegCmd, &ffmpegStderr, "ffmpeg")
	// No yt-dlp runs: its channels report a clean exit at once.
	ytdlErr := make(chan error, 1)
	ytdlErr <- nil
	ytdlDone := make(chan struct{})
	close(ytdlDone)
	return &ytdlPipeStreamer{
		ffmpegCmd:  ffmpegCmd,
		pipe:       pcmR,
		reader:     bufio.NewReaderSize(pcmR, pipeBufSize),
		ytdlErr:    ytdlErr,
		ffmpegErr:  ffmpegErrCh,
		ytdlDone:   ytdlDone,
		ffmpegDone: ffmpegDone,
		pipeReport: pipeReport{state: newPipeStreamState(0)},
		f32:        bitDepth == 32,
	}, beep.Format{SampleRate: sr, NumChannels: 2, Precision: precision}, nil
}

// directYTDL tries the direct URL of a YouTube page. It reports false when
// the page cannot be resolved or the URL does not play, and the caller then
// uses the yt-dlp pipe.
func (p *Player) directYTDL(pageURL string, startSec int) (*ytdlPipeStreamer, beep.Format, bool, error) {
	if !ytDirectEnabled || !isYouTubePage(pageURL) {
		return nil, beep.Format{}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ytResolveTimeout)
	defer cancel()
	start := time.Now()
	res, err := ResolveYT(ctx, pageURL)
	if err != nil {
		applog.Debug("ytdl: resolve %s failed after %v: %v", pageURL, time.Since(start), err)
		if permanentYTDLError(err) {
			// The pipe would only meet the same refusal two seconds later.
			return nil, beep.Format{}, false, err
		}
		return nil, beep.Format{}, false, nil
	}
	resolved := time.Now()
	decoder, format, err := decodeYTDLDirect(res, p.sr, p.bitDepth, startSec)
	if err != nil {
		return nil, beep.Format{}, false, nil
	}
	if err := prefillYTDLPipe(decoder); err != nil {
		// An expired or refused URL: resolve again next time.
		ForgetYT(pageURL)
		applog.Debug("ytdl: direct URL of %s failed: %v", pageURL, err)
		return nil, beep.Format{}, false, nil
	}
	applog.Debug("ytdl: %s resolved in %v, first audio after %v more", pageURL, resolved.Sub(start), time.Since(resolved))
	return decoder, format, true, nil
}

// ytDirectEnabled lets YouTube pages play from their resolved URL. Tests of
// the pipe turn it off.
var ytDirectEnabled = true

// permanentYTDLError reports a refusal that no retry or other route fixes.
func permanentYTDLError(err error) bool {
	msg := err.Error()
	for _, s := range []string{"Video unavailable", "Private video", "This video is not available", "has been removed", "copyright", "members-only", "age-restricted", "Sign in to confirm your age"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// buildYTDLPipeline creates a trackPipeline for a yt-dlp URL.
// If startSec > 0, playback begins at that offset (seek-by-restart).
func (p *Player) buildYTDLPipeline(pageURL string, startSec int) (*trackPipeline, error) {
	p.streamTitle.Store("")

	decoder, format, ok, err := p.directYTDL(pageURL, startSec)
	if err != nil {
		return nil, err
	}
	if ok {
		return p.prefetchNetworkPipeline(&trackPipeline{
			decoder:      decoder,
			stream:       decoder,
			format:       format,
			seekable:     false,
			path:         pageURL,
			ytdlSeek:     true,
			streamOffset: time.Duration(startSec) * time.Second,
		}, true), nil
	}

	for attempt := 1; ; attempt++ {
		decoder, format, err := decodeYTDLPipe(pageURL, p.sr, p.bitDepth, startSec)
		if err != nil {
			return nil, err
		}

		if err := prefillYTDLPipe(decoder); err != nil {
			if attempt == ytdlPipelineMaxAttempts || !isTransientYTDL403(err) {
				return nil, err
			}
			continue
		}

		// The prefetch reads the pipe away from the speaker callback. A
		// stalled download then plays silence and does not hold the speaker
		// lock, which would freeze every control.
		return p.prefetchNetworkPipeline(&trackPipeline{
			decoder:      decoder,
			stream:       decoder,
			format:       format,
			seekable:     false,
			path:         pageURL,
			ytdlSeek:     true,
			streamOffset: time.Duration(startSec) * time.Second,
		}, true), nil
	}
}

// buildYTDLSource starts the yt-dlp | ffmpeg chain for pageURL at 0. With
// probe and no known duration, it asks yt-dlp for the duration while the chain
// starts.
func (p *Player) buildYTDLSource(pageURL string, knownDuration time.Duration, probe bool) (*trackPipeline, error) {
	if knownDuration == 0 && isYouTubePage(pageURL) {
		// The resolver reads the duration with the URL.
		ctx, cancel := context.WithTimeout(context.Background(), ytResolveTimeout)
		if res, err := ResolveYT(ctx, pageURL); err == nil && res.Duration > 0 {
			knownDuration = res.Duration
		}
		cancel()
	}
	probe = probe && knownDuration == 0
	// Probe duration concurrently with pipeline setup so it doesn't delay playback.
	probeCh := make(chan time.Duration, 1)
	if probe {
		go func() { probeCh <- probeYTDLDuration(pageURL) }()
	}
	tp, err := p.buildYTDLPipeline(pageURL, 0)
	if err != nil {
		return nil, err
	}
	if probe {
		// The probe ran concurrently with buildYTDLPipeline. Try to
		// collect the result, but don't block playback for more than 2s.
		// A hung probeYTDLDuration (e.g. yt-dlp zombie keeping pipes
		// open) previously blocked here forever, leaving the UI stuck
		// at "Buffering...".
		select {
		case d := <-probeCh:
			if d > 0 {
				knownDuration = d
			}
		case <-time.After(2 * time.Second):
			// Probe still running — start playback without duration.
			// The seek bar won't show progress but audio plays immediately.
		}
	}
	tp.knownDuration = knownDuration
	return tp, nil
}

func prefillYTDLPipe(decoder *ytdlPipeStreamer) error {
	// Pre-fill: block until yt-dlp + ffmpeg produce initial audio data.
	// This runs in a tea.Cmd goroutine (not the UI thread), so a chain that
	// fails before its first byte reports why before the track starts. A 30s
	// timeout prevents hanging when yt-dlp is slow to produce output.
	stop := func() { decoder.Close() }
	return peekWithTimeout(decoder.reader, 1, ytdlPipeTimeout, stop, func(err error) error {
		// The audio pipe closed before producing a byte. Prefer the real
		// cause from yt-dlp (e.g. "Sign in to confirm you're not a bot",
		// "HTTP Error 404", DRM, region block) or ffmpeg over the opaque
		// EOF — the pipe only tells us the upstream closed, not why.
		cause := decoder.waitCause(ytdlCauseGrace)
		decoder.Close()
		if cause != nil {
			return cause
		}
		return fmt.Errorf("waiting for audio data: %w", err)
	})
}

func isTransientYTDL403(err error) bool {
	message := err.Error()
	return strings.Contains(message, "yt-dlp:") && strings.Contains(message, "HTTP Error 403: Forbidden")
}
