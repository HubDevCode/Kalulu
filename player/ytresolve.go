package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bjarneo/cliamp/internal/ytdlcookies"
)

// The YouTube resolver turns a page URL into the direct URL of its audio,
// so ffmpeg can read it at once (and seek in it with HTTP ranges) instead of
// waiting for a yt-dlp download to reach it through a pipe.
//
// yt-dlp is a Python program, and half of its time per track is starting
// Python and loading its extractors. The resolver keeps one Python process
// with yt-dlp loaded and sends it the URLs; the process exits after a few
// idle minutes, so it uses no memory while nothing new plays. When Python
// cannot import yt-dlp (a standalone binary), each URL runs yt-dlp once.

// YTResolved is the direct audio of a page.
type YTResolved struct {
	URL       string
	Duration  time.Duration
	UserAgent string
	expires   time.Time
}

const (
	ytResolveIdle    = 90 * time.Second
	ytResolveTimeout = 25 * time.Second
	ytResolveMaxKeep = 200
)

// ytHelperScript reads {"id","url","format","cookies","prio"} lines and
// answers {"id","url","duration","ua"} or {"id","error"}. One thread
// serves what the user asked for and one the prefetches, each with its own
// YoutubeDL, so a click never waits behind a prefetch.
const ytHelperScript = `
import sys, json, threading, queue
if len(sys.argv) > 1 and sys.argv[1]:
    sys.path.insert(0, sys.argv[1])
import yt_dlp
queues = [queue.Queue(), queue.Queue()]
out = threading.Lock()
def reply(obj):
    with out:
        sys.stdout.write(json.dumps(obj) + "\n")
        sys.stdout.flush()
def worker(q):
    ydls = {}
    while True:
        req = q.get()
        key = (req.get("format", ""), req.get("cookies", ""))
        ydl = ydls.get(key)
        if ydl is None:
            opts = {"quiet": True, "no_warnings": True, "noplaylist": True, "format": key[0] or "bestaudio/best", "socket_timeout": 15}
            if key[1]:
                opts["cookiesfrombrowser"] = (key[1],)
            ydl = yt_dlp.YoutubeDL(opts)
            ydls[key] = ydl
        try:
            info = ydl.extract_info(req["url"], download=False)
            if info.get("_type") == "playlist" and info.get("entries"):
                info = info["entries"][0]
            reply({"id": req["id"], "url": info.get("url", ""), "duration": info.get("duration") or 0,
                   "ua": (info.get("http_headers") or {}).get("User-Agent", ""), "protocol": info.get("protocol", "")})
        except Exception as e:
            reply({"id": req["id"], "error": str(e)[:400]})
for q in queues:
    threading.Thread(target=worker, args=(q,), daemon=True).start()
reply({"id": 0, "ready": True})
for line in sys.stdin:
    try:
        req = json.loads(line)
    except Exception:
        continue
    queues[0 if req.get("prio", 1) == 0 else 1].put(req)
`

type ytHelperReply struct {
	ID       int64   `json:"id"`
	Ready    bool    `json:"ready"`
	URL      string  `json:"url"`
	Duration float64 `json:"duration"`
	UA       string  `json:"ua"`
	Protocol string  `json:"protocol"`
	Error    string  `json:"error"`
}

type ytResolver struct {
	mu       sync.Mutex
	cache    map[string]YTResolved
	order    []string
	inflight map[string]*ytCall

	helperMu  sync.Mutex
	helper    *ytHelper
	noHelper  bool // Python cannot import yt-dlp: use the yt-dlp command
	lastUsed  time.Time
	idleTimer *time.Timer
}

type ytCall struct {
	done chan struct{}
	res  YTResolved
	err  error
}

type ytHelper struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex
	next    int64
	waiting map[int64]chan ytHelperReply
	dead    chan struct{}
}

var ytres = &ytResolver{cache: map[string]YTResolved{}, inflight: map[string]*ytCall{}}

// isYouTubePage reports whether the resolver handles u. Other yt-dlp sites
// keep the pipe: their audio is often HLS or needs cookies on every request.
func isYouTubePage(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	switch strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.") {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		return true
	}
	return false
}

// ytCacheKey keeps a resolution per video and format profile.
func ytCacheKey(pageURL string) string { return ytdlFormat() + "|" + canonicalYouTube(pageURL) }

// canonicalYouTube turns any YouTube or YouTube Music page of a video into
// its www.youtube.com watch page. The audio is the same, but the URLs that
// yt-dlp resolves for music.youtube.com come from a client whose links
// answer 403 to any reader but yt-dlp itself.
func canonicalYouTube(pageURL string) string {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return pageURL
	}
	id := parsed.Query().Get("v")
	if strings.EqualFold(strings.TrimPrefix(parsed.Hostname(), "www."), "youtu.be") {
		id = strings.Trim(parsed.Path, "/")
	}
	if id == "" {
		return pageURL
	}
	return "https://www.youtube.com/watch?v=" + url.QueryEscape(id)
}

// CachedYT returns a still valid resolution of pageURL.
func CachedYT(pageURL string) (YTResolved, bool) {
	ytres.mu.Lock()
	defer ytres.mu.Unlock()
	r, ok := ytres.cache[ytCacheKey(pageURL)]
	if !ok || time.Now().After(r.expires) {
		return YTResolved{}, false
	}
	return r, true
}

// ForgetYT drops the resolution of pageURL, after its URL failed.
func ForgetYT(pageURL string) {
	ytres.mu.Lock()
	defer ytres.mu.Unlock()
	delete(ytres.cache, ytCacheKey(pageURL))
}

// PrefetchYT resolves pageURL in the background at low priority, so a
// later play starts at once. It does nothing for other sites.
func PrefetchYT(pageURL string) {
	if !isYouTubePage(pageURL) {
		return
	}
	if _, ok := CachedYT(pageURL); ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ytResolveTimeout)
		defer cancel()
		_, _ = resolveYT(ctx, pageURL, 1)
	}()
}

// ResolveYT returns the direct audio of a YouTube page.
func ResolveYT(ctx context.Context, pageURL string) (YTResolved, error) {
	return resolveYT(ctx, pageURL, 0)
}

// ResolveYTBackground is ResolveYT at the priority of a prefetch, for work
// that must not delay a track the user asked for.
func ResolveYTBackground(ctx context.Context, pageURL string) (YTResolved, error) {
	return resolveYT(ctx, pageURL, 1)
}

func resolveYT(ctx context.Context, pageURL string, prio int) (YTResolved, error) {
	if !isYouTubePage(pageURL) {
		return YTResolved{}, errors.New("not a YouTube page")
	}
	if r, ok := CachedYT(pageURL); ok {
		return r, nil
	}
	key := ytCacheKey(pageURL)
	ytres.mu.Lock()
	if c, ok := ytres.inflight[key]; ok {
		ytres.mu.Unlock()
		select {
		case <-c.done:
			return c.res, c.err
		case <-ctx.Done():
			return YTResolved{}, ctx.Err()
		}
	}
	c := &ytCall{done: make(chan struct{})}
	ytres.inflight[key] = c
	ytres.mu.Unlock()

	c.res, c.err = ytres.run(ctx, pageURL, prio)
	ytres.mu.Lock()
	delete(ytres.inflight, key)
	if c.err == nil {
		if _, ok := ytres.cache[key]; !ok {
			ytres.order = append(ytres.order, key)
		}
		ytres.cache[key] = c.res
		for len(ytres.order) > ytResolveMaxKeep {
			delete(ytres.cache, ytres.order[0])
			ytres.order = ytres.order[1:]
		}
	}
	ytres.mu.Unlock()
	close(c.done)
	return c.res, c.err
}

func (r *ytResolver) run(ctx context.Context, pageURL string, prio int) (YTResolved, error) {
	pageURL = canonicalYouTube(pageURL)
	if h := r.ensureHelper(); h != nil {
		reply, err := h.ask(ctx, pageURL, ytdlFormat(), ytdlcookies.ForURL(pageURL), prio)
		if err == nil {
			return resolvedFrom(reply)
		}
		if errors.Is(err, errHelperDied) {
			r.dropHelper(h)
		} else {
			return YTResolved{}, err
		}
	}
	return resolveWithCommand(ctx, pageURL)
}

func resolvedFrom(reply ytHelperReply) (YTResolved, error) {
	if reply.Error != "" {
		return YTResolved{}, fmt.Errorf("yt-dlp: %s", reply.Error)
	}
	if reply.URL == "" || strings.HasPrefix(reply.Protocol, "m3u8") || strings.Contains(reply.Protocol, "dash") {
		return YTResolved{}, errors.New("yt-dlp: no direct audio URL")
	}
	return YTResolved{
		URL:       reply.URL,
		Duration:  time.Duration(reply.Duration * float64(time.Second)),
		UserAgent: reply.UA,
		expires:   urlExpiry(reply.URL),
	}, nil
}

// urlExpiry reads the expire parameter of a googlevideo URL, keeping a
// margin; without it the URL is trusted for an hour.
func urlExpiry(u string) time.Time {
	limit := time.Now().Add(5 * time.Hour)
	if parsed, err := url.Parse(u); err == nil {
		if secs, err := strconv.ParseInt(parsed.Query().Get("expire"), 10, 64); err == nil {
			exp := time.Unix(secs, 0).Add(-15 * time.Minute)
			if exp.Before(limit) {
				return exp
			}
			return limit
		}
	}
	return time.Now().Add(time.Hour)
}

// resolveWithCommand runs yt-dlp once for pageURL.
func resolveWithCommand(ctx context.Context, pageURL string) (YTResolved, error) {
	ctx, cancel := context.WithTimeout(ctx, ytResolveTimeout)
	defer cancel()
	args := []string{"-f", ytdlFormat(), "--no-playlist", "--quiet", "--no-warnings", "--socket-timeout", "15",
		"-O", "%(duration)s", "-O", "%(protocol)s", "-O", "%(http_headers.User-Agent)s", "-O", "%(url)s"}
	args = appendYTDLCookieArgs(args, pageURL)
	args = append(args, "--", pageURL)
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.WaitDelay = 3 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return YTResolved{}, fmt.Errorf("yt-dlp: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 4 {
		return YTResolved{}, errors.New("yt-dlp: no direct audio URL")
	}
	dur, _ := strconv.ParseFloat(lines[0], 64)
	return resolvedFrom(ytHelperReply{URL: lines[3], Duration: dur, Protocol: lines[1], UA: lines[2]})
}

// ytdlpPythonPath finds the Python of the yt-dlp script and the path that
// lets it import yt_dlp (the script's own file for a zipapp).
func ytdlpPythonPath() (python, sysPath string, ok bool) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", "", false
	}
	f, err := os.Open(bin)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	line, _ := bufio.NewReader(f).ReadString('\n')
	if !strings.HasPrefix(line, "#!") {
		return "", "", false // a frozen binary: no Python to reuse
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return "", "", false
	}
	python = fields[0]
	if filepath.Base(python) == "env" && len(fields) > 1 {
		if python, err = exec.LookPath(fields[1]); err != nil {
			return "", "", false
		}
	}
	if !strings.HasPrefix(filepath.Base(python), "python") {
		return "", "", false
	}
	// A zipapp starts with the shebang and holds the package itself.
	head := make([]byte, 4096)
	n, _ := io.ReadFull(io.NewSectionReader(f, 0, 4096), head)
	if strings.Contains(string(head[:n]), "PK\x03\x04") {
		sysPath = bin
	}
	return python, sysPath, true
}

var errHelperDied = errors.New("yt-dlp helper stopped")

// ensureHelper starts the helper on first use. It returns nil when yt-dlp
// cannot run as a module.
func (r *ytResolver) ensureHelper() *ytHelper {
	r.helperMu.Lock()
	defer r.helperMu.Unlock()
	r.lastUsed = time.Now()
	if r.noHelper {
		return nil
	}
	if r.helper != nil {
		select {
		case <-r.helper.dead:
			r.helper = nil
		default:
			r.armIdle()
			return r.helper
		}
	}
	python, sysPath, ok := ytdlpPythonPath()
	if !ok {
		r.noHelper = true
		return nil
	}
	h, err := startYTHelper(python, sysPath)
	if err != nil {
		r.noHelper = true
		return nil
	}
	r.helper = h
	r.armIdle()
	return h
}

// armIdle stops the helper after ytResolveIdle without requests.
func (r *ytResolver) armIdle() {
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	r.idleTimer = time.AfterFunc(ytResolveIdle, func() {
		r.helperMu.Lock()
		defer r.helperMu.Unlock()
		if r.helper != nil && time.Since(r.lastUsed) >= ytResolveIdle {
			r.helper.stop()
			r.helper = nil
		}
	})
}

func (r *ytResolver) dropHelper(h *ytHelper) {
	r.helperMu.Lock()
	defer r.helperMu.Unlock()
	if r.helper == h {
		r.helper = nil
	}
	h.stop()
}

func startYTHelper(python, sysPath string) (*ytHelper, error) {
	cmd := exec.Command(python, "-I", "-c", ytHelperScript, sysPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	h := &ytHelper{cmd: cmd, stdin: stdin, waiting: map[int64]chan ytHelperReply{}, dead: make(chan struct{})}
	ready := make(chan bool, 1)
	go func() {
		defer close(h.dead)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		first := true
		for sc.Scan() {
			var reply ytHelperReply
			if json.Unmarshal(sc.Bytes(), &reply) != nil {
				continue
			}
			if first {
				first = false
				ready <- reply.Ready
				continue
			}
			h.mu.Lock()
			ch := h.waiting[reply.ID]
			delete(h.waiting, reply.ID)
			h.mu.Unlock()
			if ch != nil {
				ch <- reply
			}
		}
		if first {
			ready <- false
		}
		_ = cmd.Wait()
	}()
	select {
	case ok := <-ready:
		if !ok {
			h.stop()
			return nil, errors.New("python cannot import yt_dlp")
		}
	case <-time.After(10 * time.Second):
		h.stop()
		return nil, errors.New("yt-dlp helper did not start")
	}
	return h, nil
}

func (h *ytHelper) ask(ctx context.Context, pageURL, format, cookies string, prio int) (ytHelperReply, error) {
	ch := make(chan ytHelperReply, 1)
	h.mu.Lock()
	h.next++
	id := h.next
	h.waiting[id] = ch
	line, _ := json.Marshal(map[string]any{"id": id, "url": pageURL, "format": format, "cookies": cookies, "prio": prio})
	_, err := h.stdin.Write(append(line, '\n'))
	h.mu.Unlock()
	if err != nil {
		return ytHelperReply{}, errHelperDied
	}
	ctx, cancel := context.WithTimeout(ctx, ytResolveTimeout)
	defer cancel()
	select {
	case reply := <-ch:
		return reply, nil
	case <-h.dead:
		return ytHelperReply{}, errHelperDied
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.waiting, id)
		h.mu.Unlock()
		return ytHelperReply{}, ctx.Err()
	}
}

func (h *ytHelper) stop() {
	_ = h.stdin.Close()
	if h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
}

// StopYTResolver ends the helper process, at exit.
func StopYTResolver() {
	ytres.helperMu.Lock()
	defer ytres.helperMu.Unlock()
	if ytres.helper != nil {
		ytres.helper.stop()
		ytres.helper = nil
	}
}
