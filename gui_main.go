package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/djmix"
	"github.com/bjarneo/cliamp/external/radio"
	"github.com/bjarneo/cliamp/gui"
	"github.com/bjarneo/cliamp/internal/appdir"
	"github.com/bjarneo/cliamp/internal/appmeta"
	"github.com/bjarneo/cliamp/internal/playback"
	"github.com/bjarneo/cliamp/internal/resume"
	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/luaplugin"
	"github.com/bjarneo/cliamp/mediactl"
	"github.com/bjarneo/cliamp/player"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/resolve"
	"github.com/bjarneo/cliamp/theme"
	"github.com/bjarneo/cliamp/ui/model"
)

// runGUI starts the graphical player. It builds the same runtime as run --
// providers, player, plugins, media controls and the IPC socket -- with the
// Model in headless mode, and hands the window an in-process engine that
// speaks the V2 operations.
func runGUI(overrides config.Overrides, positional []string, background bool, roomCreate, roomJoin string) error {
	// One Kalulu per user: a second launch hands over its files and brings
	// the window forward, then exits.
	if activateRunning(positional) {
		return nil
	}
	activate, stopInstance, err := listenInstance()
	if err != nil {
		return err
	}
	defer stopInstance()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	overrides.Apply(&cfg)

	closeLog, appliedLevel, logErr := initLogging(cfg.LogLevel)
	defer closeLog()
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "logging: %v (continuing without file log)\n", logErr)
	} else {
		applog.Info("kalulu starting (version=%s level=%s)", appmeta.Version(), appliedLevel)
	}
	setupDiagnostics()
	tuneMemory()

	positional, err = searchArgs(positional)
	if err != nil {
		return err
	}

	providers := buildProviders(cfg, false)
	defer providers.Close()

	if cfg.YouTubeMusic.ExpandPlaylist != nil {
		resolve.ExpandYTPlaylist = *cfg.YouTubeMusic.ExpandPlaylist
	}
	resolved, err := resolve.Args(positional)
	if err != nil {
		return err
	}

	defaultProvider := cfg.Provider
	if defaultProvider == "" {
		defaultProvider = "cliamp"
	}
	resumeState := resume.Load()

	pl := playlist.New()
	if cfg.Playlist != "" && providers.local != nil {
		tracks, err := providers.local.Tracks(cfg.Playlist)
		if err != nil {
			return fmt.Errorf("playlist %q: %w", cfg.Playlist, err)
		}
		pl.Add(tracks...)
	} else if len(positional) == 0 && cfg.AutoPlay && (defaultProvider == "cliamp" || defaultProvider == "radio") {
		resolved.Pending = append(resolved.Pending, radio.BuiltinURL)
	}
	pl.Add(resolved.Tracks...)

	resumeServer := providers.resumeServer(defaultProvider)
	restoredContext := false
	restoredIndex := 0
	restoredResumePath := ""
	if resumeServer != nil && cfg.Playlist == "" && len(positional) == 0 && len(resolved.Pending) == 0 && pl.Len() == 0 {
		if tracks, index, activePath, ok := restoreServerContext(resumeState, resumeServer); ok {
			pl.Add(tracks...)
			restoredContext = true
			restoredIndex = index
			restoredResumePath = activePath
		}
	}

	// Without anything to open, reopen the queue and the track of the last
	// run, paused at the same position.
	var session guiSession
	restoredSession := false
	if cfg.Playlist == "" && len(positional) == 0 && len(resolved.Pending) == 0 && pl.Len() == 0 && !restoredContext {
		if s, ok := loadGUISession(); ok {
			pl.Add(s.Tracks...)
			pl.SetIndex(s.Index)
			session, restoredSession = s, true
		}
	}

	p, closePlayer, err := newPlayer(cfg)
	if err != nil {
		return err
	}
	defer closePlayer()
	providers.registerPlayerHooks(p)
	cfg.ApplyPlayer(p)
	cfg.ApplyPlaylist(pl)

	// Crossfades and DJ mixes: the window sets the mode from its settings.
	dj := djmix.NewService(djmix.DefaultSettings())
	p.SetTransitionPlanner(dj.Planner())

	broker := ipc.NewBroker()
	defer broker.Close()

	luaMgr, luaErr := luaplugin.New(cfg.Plugins, broker, model.ReservedKeys())
	if luaErr != nil {
		applog.Warn("lua plugins: %v", luaErr)
	}
	if luaMgr != nil {
		defer luaMgr.Close()
	}

	m := model.New(p, pl, providers.entries, defaultProvider, providers.localPlaylists(), providers.favorites, providers.history, theme.LoadAll(), luaMgr, config.SaveFunc{})
	m.SetRadioFavorites(providers.radioFavorites)
	if resumeServer != nil {
		m.SetResumeSaver(serverResumeSaver(resumeServer))
	}
	if restoredContext {
		m.SetInitialTrack(restoredIndex)
	}
	m.SetIPCBroker(broker)
	if luaMgr != nil {
		luaMgr.SetStateProvider(luaStateProvider(p, m.PluginStateLoader()))
		if names := luaMgr.Visualizers(); len(names) > 0 {
			m.RegisterLuaVisualizers(names, luaMgr)
		}
	}
	m.SetPendingURLs(resolved.Pending)
	if cfg.Playlist != "" && len(resolved.Tracks) == 0 && len(resolved.Pending) == 0 {
		m.SetLoadedPlaylist(cfg.Playlist)
	}
	// Files and URLs given on the command line play at once, as a desktop
	// player does when it opens a file.
	if (cfg.AutoPlay || len(resolved.Tracks) > 0 || len(resolved.Pending) > 0) && !restoredContext {
		m.SetAutoPlay(true)
	}
	// The window draws its own views, so the Model runs as in headless mode.
	configureModel(&m, cfg, true, false)

	if resumeState.Path != "" && resumeState.PositionSec > 0 {
		switch {
		case restoredResumePath != "":
			m.SetResume(restoredResumePath, resumeState.PositionSec)
		case playlist.IsMixcloudURL(resumeState.Path) || len(positional) > 0:
			m.SetResume(resumeState.Path, resumeState.PositionSec)
		}
	}

	if restoredSession && session.PositionSec > 0 {
		m.SetResume(session.Tracks[session.Index].Path, session.PositionSec)
	}

	prog := tea.NewProgram(m, headlessProgramOptions()...)
	stopSignals := quitOnSignals(prog.Send)
	defer stopSignals()

	authURLs := make(chan gui.AuthURL, 4)
	defer providers.observeAuthURLs(func(msg tea.Msg) {
		prog.Send(msg)
		if auth, ok := msg.(model.ProvAuthURLMsg); ok {
			select {
			case authURLs <- gui.AuthURL{Provider: auth.ProviderName, URL: auth.URL}:
			default:
			}
		}
	})()

	// NowPlaying on macOS owns the main thread, which the window needs, so
	// the media controls run only where they live on their own thread.
	var svc *mediactl.Service
	if runtime.GOOS != "darwin" {
		var svcErr error
		svc, svcErr = wireMediaCtl(prog)
		if svcErr != nil {
			applog.Warn("media control unavailable: %v", svcErr)
		} else if svc != nil {
			defer svc.Close()
		}
	}

	if luaMgr != nil {
		luaSend, stopLuaSend := newOrderedSender(prog.Send)
		defer stopLuaSend()
		luaMgr.SetControlProvider(luaControlProvider(luaSend))
		luaMgr.SetUIProvider(luaUIProvider(luaSend))
	}

	stopIPC, err := startIPC(prog.Send, broker, luaMgr, false)
	if err != nil {
		return err
	}
	defer stopIPC()

	stopSession := keepSession(pl, p)
	engine := newGUIEngine(prog, p, broker, luaMgr)
	go engine.run(func(final tea.Model) {
		saveOnExit(final, true, resumeServer)
		if fm, ok := final.(model.Model); ok {
			fm.WaitReports(reportsExitWait)
		}
	})

	configDir, _ := appdir.Dir()
	restart := gui.Run(engine, gui.Options{
		Version:    appmeta.Version(),
		VolumeMin:  cfg.VolumeMin,
		ConfigPath: filepath.Join(configDir, "config.toml"),
		AuthURLs:   authURLs,
		DJ:         dj,
		Mixer:      p,
		Activate:   activate,
		Background: background,
		RoomCreate: roomCreate,
		RoomJoin:   roomJoin,
	})

	stopSession()
	defer player.StopYTResolver()
	engine.Quit()
	select {
	case <-engine.Done():
	case <-time.After(5 * time.Second):
		applog.Warn("engine did not stop in time")
	}
	if restart {
		return errRestart
	}
	return nil
}

// errRestart asks main to start Kalulu again once runGUI has released the
// audio device, the socket and the plugins.
var errRestart = errors.New("restart requested")

// restartSelf replaces the process with a fresh Kalulu.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Inside an AppImage the binary needs the launcher, which mounts the
	// image and sets up its libraries again.
	if image := os.Getenv("APPIMAGE"); image != "" {
		exe = image
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// guiEngine implements gui.Engine over the Bubble Tea program of the Model.
type guiEngine struct {
	prog       *tea.Program
	player     *player.Player
	broker     *ipc.Broker
	jobs       *ipc.JobStore
	dispatcher ipc.V2Dispatcher
	stopQueue  func()
	done       chan struct{}
	quitOnce   sync.Once

	mu      sync.Mutex
	changed chan struct{}
}

func newGUIEngine(prog *tea.Program, p *player.Player, broker *ipc.Broker, plugins *luaplugin.Manager) *guiEngine {
	jobs := ipc.NewJobStore(ipc.WithJobTTL(2 * time.Minute))
	queue, stopQueue := newOrderedSender(prog.Send)
	e := &guiEngine{
		prog:       prog,
		player:     p,
		broker:     broker,
		jobs:       jobs,
		dispatcher: newV2Dispatcher(queue, jobs, plugins),
		stopQueue:  stopQueue,
		done:       make(chan struct{}),
		changed:    make(chan struct{}),
	}
	go e.watchJobs()
	return e
}

// run runs the program until it quits, then calls exit with the final Model.
func (e *guiEngine) run(exit func(tea.Model)) {
	defer close(e.done)
	final, err := e.prog.Run()
	if err != nil {
		applog.Error("engine: %v", err)
	}
	if final != nil {
		exit(final)
	}
}

// watchJobs wakes the callers that wait for a job on each terminal event.
func (e *guiEngine) watchJobs() {
	for {
		select {
		case <-e.done:
			return
		case <-e.jobs.Events():
			e.mu.Lock()
			close(e.changed)
			e.changed = make(chan struct{})
			e.mu.Unlock()
		}
	}
}

func (e *guiEngine) wakeup() <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.changed
}

func (e *guiEngine) Call(ctx context.Context, operation string, params any) (gui.Result, error) {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return gui.Result{}, err
		}
		raw = data
	}
	result, v2err := e.dispatcher.DispatchV2(ctx, ipc.V2Request{Operation: operation, Params: raw})
	if v2err != nil {
		return gui.Result{}, fmt.Errorf("%s: %s", operation, v2err.Message)
	}
	if result.Job == nil {
		return gui.Result{Snapshot: result.Snapshot}, nil
	}
	id := result.Job.ID
	for {
		wake := e.wakeup()
		job, ok := e.jobs.Get(id)
		if !ok {
			return gui.Result{}, fmt.Errorf("%s: job expired", operation)
		}
		switch job.State {
		case ipc.JobSucceeded, ipc.JobFailed, ipc.JobCanceled:
			return gui.ResultFromJob(job)
		}
		select {
		case <-wake:
		case <-time.After(250 * time.Millisecond):
		case <-e.done:
			return gui.Result{}, fmt.Errorf("%s: engine stopped", operation)
		case <-ctx.Done():
			_ = e.jobs.Cancel(id)
			return gui.Result{}, fmt.Errorf("%s: %w", operation, ctx.Err())
		}
	}
}

func (e *guiEngine) State(ctx context.Context) (ipc.RuntimeSnapshot, error) {
	result, v2err := e.dispatcher.DispatchV2(ctx, ipc.V2Request{Method: "state.get"})
	if v2err != nil {
		return ipc.RuntimeSnapshot{}, fmt.Errorf("state: %s", v2err.Message)
	}
	if result.Snapshot == nil {
		return ipc.RuntimeSnapshot{}, fmt.Errorf("state: empty snapshot")
	}
	return *result.Snapshot, nil
}

func (e *guiEngine) Subscribe(topics []string) (*ipc.Subscription, error) {
	return e.broker.Subscribe(topics)
}

func (e *guiEngine) SamplesInto(dst []float64) int         { return e.player.SamplesInto(dst) }
func (e *guiEngine) WaveformSamplesInto(dst []float64) int { return e.player.WaveformSamplesInto(dst) }
func (e *guiEngine) SampleRate() int                       { return e.player.SampleRate() }
func (e *guiEngine) Done() <-chan struct{}                 { return e.done }

func (e *guiEngine) Quit() {
	e.quitOnce.Do(func() {
		go e.prog.Send(playback.QuitMsg{})
		e.stopQueue()
	})
}
