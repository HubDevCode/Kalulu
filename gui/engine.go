// Package gui is the graphical front end of Kalulu. It drives the cliamp
// engine (the headless ui/model runtime) through the V2 operations, the same
// surface that the socket clients use, so every feature of the engine is
// reachable from the window without a second implementation of it.
package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bjarneo/cliamp/djmix"
	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/player"
)

// Engine is the runtime that the window controls. The root package builds it
// in-process around the cliamp Model.
type Engine interface {
	// Call runs one V2 operation and waits for its terminal state.
	Call(ctx context.Context, operation string, params any) (Result, error)
	// State reads the runtime snapshot.
	State(ctx context.Context) (ipc.RuntimeSnapshot, error)
	// Subscribe streams the runtime events of topics.
	Subscribe(topics []string) (*ipc.Subscription, error)
	// SamplesInto copies the latest mono samples for the visualizer.
	SamplesInto(dst []float64) int
	// WaveformSamplesInto copies the samples at the output position.
	WaveformSamplesInto(dst []float64) int
	// SampleRate is the output sample rate in Hz.
	SampleRate() int
	// Quit stops the engine and saves the resume state.
	Quit()
	// Done is closed when the engine has stopped.
	Done() <-chan struct{}
}

// Result is the outcome of a successful operation.
type Result struct {
	Response ipc.Response
	Snapshot *ipc.RuntimeSnapshot
}

// Options carries the startup settings of the window.
type Options struct {
	Version   string
	VolumeMin float64
	// ConfigPath is config.toml, for the config editor.
	ConfigPath string
	// AuthURLs delivers provider sign-in URLs to show to the user.
	AuthURLs <-chan AuthURL
	// DJ analyzes tracks and plans crossfades and DJ mixes.
	DJ *djmix.Service
	// Mixer is the player's transition control.
	Mixer Mixer
	// Activate delivers the files of later launches; empty means "show".
	Activate <-chan []string
	// Background starts in the tray without opening the window.
	Background bool
	// RoomCreate opens a shared listening room with this name at start;
	// RoomJoin joins "host:port/CODE".
	RoomCreate string
	RoomJoin   string
}

// Mixer controls the transitions of the player.
type Mixer interface {
	SetTransitionLead(time.Duration)
	TransitionLead() time.Duration
	LastTransition() (player.TransitionEvent, bool)
	Mixing() bool
	SetSilenceSkip(player.SilenceSettings)
	SetBalance(float64)
	SetNormalize(bool)
}

// AuthURL is an interactive sign-in that a provider started.
type AuthURL struct {
	Provider string
	URL      string
}

// ResultFromJob turns a terminal job into a Result or an error.
func ResultFromJob(job ipc.Job) (Result, error) {
	switch job.State {
	case ipc.JobSucceeded:
		var result Result
		if len(job.Result) > 0 {
			if err := json.Unmarshal(job.Result, &result.Response); err != nil {
				return Result{}, fmt.Errorf("%s: decode result: %w", job.Operation, err)
			}
			if !result.Response.OK && result.Response.Error != "" {
				return result, fmt.Errorf("%s", result.Response.Error)
			}
		} else {
			result.Response.OK = true
		}
		result.Snapshot = job.Snapshot
		return result, nil
	case ipc.JobFailed, ipc.JobCanceled:
		if job.Error != nil {
			if job.Error.Detail != "" {
				return Result{}, fmt.Errorf("%s", job.Error.Detail)
			}
			return Result{}, fmt.Errorf("%s: %s", job.Operation, job.Error.Message)
		}
		return Result{}, fmt.Errorf("%s: %s", job.Operation, job.State)
	}
	return Result{}, fmt.Errorf("%s: job did not finish", job.Operation)
}

// call runs an operation with a default timeout. Long provider loads get a
// longer bound than playback controls.
func (a *App) call(operation string, params any) (Result, error) {
	timeout := 15 * time.Second
	switch operation {
	case "provider.playlists", "provider.tracks", "provider.load", "provider.search",
		"provider.artists", "provider.artist_albums", "provider.albums",
		"provider.album_tracks", "provider.load_album", "provider.catalog",
		"url.load", "save", "lyrics", "track.play", "track.queue",
		"playlist.add_many", "playlist.add", "playlist.replace":
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return a.engine.Call(ctx, operation, params)
}

// do runs an operation in the background and reports a failure in the
// status bar. done, when set, runs on the UI thread with the result.
func (a *App) do(operation string, params any, done func(Result)) {
	go func() {
		result, err := a.call(operation, params)
		if err != nil {
			a.toastError(err)
			return
		}
		a.refreshState()
		if done != nil {
			runOnUI(func() { done(result) })
		}
	}()
}

// P is shorthand for an operation parameter object.
type P map[string]any
