package djmix

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bjarneo/cliamp/player"
	"github.com/bjarneo/cliamp/playlist"
)

// maxAnalyzed bounds the audio read for one track.
const maxAnalyzed = 15 * time.Minute

// ErrUnsupported marks a source the analysis cannot read, such as a live
// stream or a provider URI.
var ErrUnsupported = errors.New("djmix: source cannot be analyzed")

// Decode reads path as mono float samples at AnalysisRate. It uses ffmpeg,
// and yt-dlp in front of it for YouTube, SoundCloud and the other yt-dlp
// sites. A failed download is tried once more.
func Decode(ctx context.Context, path string) ([]float32, error) {
	samples, err := decodeOnce(ctx, path, true)
	if err != nil && playlist.IsYTDL(path) && ctx.Err() == nil {
		samples, err = decodeOnce(ctx, path, false)
	}
	return samples, err
}

func decodeOnce(ctx context.Context, path string, direct bool) ([]float32, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("djmix: ffmpeg is required for the analysis: %w", err)
	}
	out := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-t", fmt.Sprint(int(maxAnalyzed.Seconds())),
		"-vn", "-ac", "1", "-ar", fmt.Sprint(AnalysisRate), "-f", "f32le", "pipe:1"}
	var cmd *exec.Cmd
	var feeder *exec.Cmd
	switch {
	case playlist.IsYTDL(path) && direct:
		// Resolve the audio URL (shared with playback) and let ffmpeg read
		// it with reconnects.
		var media, ua string
		if res, err := player.ResolveYTBackground(ctx, path); err == nil {
			media, ua = res.URL, res.UserAgent
		} else {
			ytdlp, err := exec.LookPath("yt-dlp")
			if err != nil {
				return nil, fmt.Errorf("djmix: yt-dlp is required for %s: %w", path, err)
			}
			raw, err := exec.CommandContext(ctx, ytdlp, "-g", "-f", "worstaudio[acodec=opus]/worstaudio/bestaudio/best", "--no-playlist", "--quiet", "--no-warnings", "--", path).Output()
			if err != nil {
				return nil, fmt.Errorf("djmix: yt-dlp %s: %w", path, err)
			}
			media = strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
		}
		if media == "" {
			return nil, fmt.Errorf("djmix: yt-dlp found no audio for %s", path)
		}
		in := []string{"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "4"}
		if ua != "" {
			in = append(in, "-user_agent", ua)
		}
		cmd = exec.CommandContext(ctx, ffmpeg, append(append(in, "-i", media), out...)...)
	case playlist.IsYTDL(path):
		ytdlp, err := exec.LookPath("yt-dlp")
		if err != nil {
			return nil, fmt.Errorf("djmix: yt-dlp is required for %s: %w", path, err)
		}
		feeder = exec.CommandContext(ctx, ytdlp, "-f", "worstaudio[acodec=opus]/worstaudio/bestaudio/best", "--no-playlist", "--quiet", "--no-warnings", "-o", "-", path)
		cmd = exec.CommandContext(ctx, ffmpeg, append([]string{"-i", "pipe:0"}, out...)...)
		pipe, err := feeder.StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmd.Stdin = pipe
	case strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://"):
		cmd = exec.CommandContext(ctx, ffmpeg, append([]string{"-i", path}, out...)...)
	case strings.HasPrefix(path, "file://"):
		cmd = exec.CommandContext(ctx, ffmpeg, append([]string{"-i", strings.TrimPrefix(path, "file://")}, out...)...)
	case !strings.Contains(path, "://") && !strings.Contains(path, ":track:"):
		cmd = exec.CommandContext(ctx, ffmpeg, append([]string{"-i", path}, out...)...)
	default:
		return nil, ErrUnsupported
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if feeder != nil {
		if err := feeder.Start(); err != nil {
			return nil, err
		}
		defer func() { _ = feeder.Wait() }()
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Convert while reading: the raw bytes and the samples never both sit
	// in memory for a whole track.
	samples, readErr := readFloat32(io.LimitReader(stdout, int64(maxAnalyzed.Seconds())*AnalysisRate*4))
	waitErr := cmd.Wait()
	if feeder != nil && feeder.Process != nil {
		_ = feeder.Process.Kill()
	}
	// A download cut short would be analyzed as a short track, and its
	// cached analysis would end every future mix early: only a clean
	// decode counts.
	if waitErr != nil && len(samples) >= AnalysisRate*4 && ctx.Err() == nil {
		return nil, fmt.Errorf("djmix: decode %s stopped at %.0fs: %w", path, float64(len(samples))/AnalysisRate, waitErr)
	}
	if readErr != nil && len(samples) >= AnalysisRate*4 {
		return nil, fmt.Errorf("djmix: read %s: %w", path, readErr)
	}
	if len(samples) < AnalysisRate*4 {
		if readErr != nil {
			return nil, readErr
		}
		if waitErr != nil {
			return nil, fmt.Errorf("djmix: decode %s: %w", path, waitErr)
		}
		return nil, fmt.Errorf("djmix: %s is too short to analyze", path)
	}
	return samples, nil
}

// Service analyzes tracks in the background, keeps the results on disk and
// plans the transitions of the player.
type Service struct {
	mu       sync.Mutex
	results  map[string]*Analysis
	waiting  map[string]chan struct{}
	failed   map[string]error
	sem      chan struct{}
	dir      string
	settings atomic.Pointer[Settings]
	lastPlan atomic.Pointer[PlanInfo]
}

// PlanInfo is the last planned transition, for the interface.
type PlanInfo struct {
	OutPath, InPath string
	Transition      player.Transition
	Out, In         *Analysis
	Planned         time.Time
}

// NewService returns a service that caches in the user cache directory.
func NewService(settings Settings) *Service {
	s := &Service{
		results: map[string]*Analysis{},
		waiting: map[string]chan struct{}{},
		failed:  map[string]error{},
		sem:     make(chan struct{}, 1),
	}
	if dir, err := os.UserCacheDir(); err == nil {
		s.dir = filepath.Join(dir, "kalulu", "analysis")
	}
	s.SetSettings(settings)
	return s
}

// SetSettings changes the transition settings. The next transition uses
// them.
func (s *Service) SetSettings(settings Settings) {
	settings.normalize()
	s.settings.Store(&settings)
}

// Settings returns the current settings.
func (s *Service) Settings() Settings { return *s.settings.Load() }

// LastPlan returns the most recently planned transition.
func (s *Service) LastPlan() (PlanInfo, bool) {
	p := s.lastPlan.Load()
	if p == nil {
		return PlanInfo{}, false
	}
	return *p, true
}

func (s *Service) cachePath(path string) string {
	if s.dir == "" {
		return ""
	}
	sum := sha1.Sum([]byte(path))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

// Get returns the analysis of path when it is ready.
func (s *Service) Get(path string) (*Analysis, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.results[path]
	return a, ok
}

// Err returns why the analysis of path failed, if it did.
func (s *Service) Err(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed[path]
}

// Request starts the analysis of path in the background unless it is done
// or running. It returns at once.
func (s *Service) Request(path string) {
	if path == "" {
		return
	}
	s.mu.Lock()
	if _, ok := s.results[path]; ok {
		s.mu.Unlock()
		return
	}
	if _, ok := s.waiting[path]; ok {
		s.mu.Unlock()
		return
	}
	if _, ok := s.failed[path]; ok {
		s.mu.Unlock()
		return
	}
	done := make(chan struct{})
	s.waiting[path] = done
	s.mu.Unlock()
	go s.run(path, done)
}

func (s *Service) run(path string, done chan struct{}) {
	defer close(done)
	a, err := s.load(path)
	if a == nil && err == nil {
		s.sem <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		var samples []float32
		samples, err = Decode(ctx, path)
		cancel()
		<-s.sem
		if err == nil {
			a = Analyze(samples)
			s.save(path, a)
		}
	}
	s.mu.Lock()
	delete(s.waiting, path)
	if err != nil {
		s.failed[path] = err
	} else {
		s.results[path] = a
	}
	// Hours of listening must not fill the memory: the analyses stay on
	// disk and reload in a few milliseconds.
	trimMap(s.results, maxKeptAnalyses, path)
	trimMap(s.failed, maxKeptAnalyses, path)
	s.mu.Unlock()
}

const maxKeptAnalyses = 48

// trimMap drops entries other than keep until m has at most n.
func trimMap[V any](m map[string]V, n int, keep string) {
	for k := range m {
		if len(m) <= n {
			return
		}
		if k != keep {
			delete(m, k)
		}
	}
}

func (s *Service) load(path string) (*Analysis, error) {
	file := s.cachePath(path)
	if file == "" {
		return nil, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}
	var a Analysis
	if json.Unmarshal(data, &a) != nil || a.Version != analysisVersion {
		return nil, nil
	}
	return &a, nil
}

func (s *Service) save(path string, a *Analysis) {
	file := s.cachePath(path)
	if file == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(a)
	if err == nil {
		_ = os.WriteFile(file, data, 0o644)
	}
}

// Wait requests path and waits up to timeout for its analysis.
func (s *Service) Wait(path string, timeout time.Duration) (*Analysis, bool) {
	s.Request(path)
	s.mu.Lock()
	if a, ok := s.results[path]; ok {
		s.mu.Unlock()
		return a, true
	}
	done, ok := s.waiting[path]
	s.mu.Unlock()
	if !ok {
		return nil, false
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
	return s.Get(path)
}

// Lead is how early the player must preload the next track.
func (s *Service) Lead() time.Duration {
	st := s.Settings()
	switch st.Mode {
	case ModeDJ:
		return 120 * time.Second
	case ModeCrossfade:
		return time.Duration(st.CrossfadeSec*float64(time.Second)) + 25*time.Second
	}
	return 0
}

// Planner returns the player's transition planner.
func (s *Service) Planner() player.TransitionPlanner {
	return func(outPath, inPath string) (player.Transition, bool) {
		st := s.Settings()
		if st.Mode == ModeOff {
			return player.Transition{}, false
		}
		var a, b *Analysis
		if st.Mode == ModeDJ || st.SkipSilence || st.LoudnessMatch {
			s.Request(outPath)
			s.Request(inPath)
			wait := 8 * time.Second
			if st.Mode == ModeDJ {
				wait = 40 * time.Second
			}
			a, _ = s.Wait(outPath, wait)
			b, _ = s.Wait(inPath, wait)
		}
		t := Plan(a, b, st)
		if t.Length <= 0 {
			return player.Transition{}, false
		}
		s.lastPlan.Store(&PlanInfo{OutPath: outPath, InPath: inPath, Transition: t, Out: a, In: b, Planned: time.Now()})
		return t, true
	}
}

// readFloat32 reads little-endian float32 samples until EOF.
func readFloat32(r io.Reader) ([]float32, error) {
	samples := make([]float32, 0, AnalysisRate*240)
	buf := make([]byte, 64<<10)
	var carry int
	for {
		n, err := r.Read(buf[carry:])
		n += carry
		whole := n / 4 * 4
		for i := 0; i < whole; i += 4 {
			samples = append(samples, math.Float32frombits(binary.LittleEndian.Uint32(buf[i:])))
		}
		carry = copy(buf, buf[whole:n])
		if err == io.EOF {
			return slices.Clip(samples), nil
		}
		if err != nil {
			return samples, err
		}
	}
}
