package model

import (
	"encoding/json"
	"testing"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/ui"
)

func TestV2TracksAdd(t *testing.T) {
	tests := []struct {
		name      string
		params    string
		playing   bool
		wantState ipc.JobState
		wantLen   int
		wantIndex int
	}{
		{name: "append while playing", params: `{"tracks":[{"path":"/m/b.flac"},{"path":"/m/c.flac"}]}`, playing: true, wantState: ipc.JobSucceeded, wantLen: 3, wantIndex: 0},
		{name: "play jumps to the first added", params: `{"tracks":[{"path":"/m/b.flac"}],"play":true}`, playing: true, wantState: ipc.JobSucceeded, wantLen: 2, wantIndex: 1},
		{name: "play starts at index", params: `{"tracks":[{"path":"/m/b.flac"},{"path":"/m/c.flac"}],"play":true,"index":1}`, playing: true, wantState: ipc.JobSucceeded, wantLen: 3, wantIndex: 2},
		{name: "empty list fails", params: `{"tracks":[]}`, wantState: ipc.JobFailed, wantLen: 1},
		{name: "tracks without paths fail", params: `{"tracks":[{"title":"x"}]}`, wantState: ipc.JobFailed, wantLen: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &playbackFakeEngine{playing: tt.playing}
			pl := playlist.New()
			pl.Add(playlist.Track{Path: "/m/a.flac", Title: "A"})
			m := Model{player: engine, playlist: pl, vis: ui.NewVisualizer(float64(engine.SampleRate()))}
			jobs := ipc.NewJobStore()
			job, err := jobs.Create("tracks.add")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.Update(V2RequestMsg{
				Request: ipc.V2Request{Operation: "tracks.add", Params: json.RawMessage(tt.params)},
				Jobs:    jobs,
				JobID:   job.ID,
			})
			done, _ := jobs.Get(job.ID)
			if done.State != tt.wantState {
				t.Fatalf("state = %s, want %s (%+v)", done.State, tt.wantState, done.Error)
			}
			if pl.Len() != tt.wantLen {
				t.Fatalf("playlist length = %d, want %d", pl.Len(), tt.wantLen)
			}
			if tt.wantState == ipc.JobSucceeded && pl.Index() != tt.wantIndex {
				t.Fatalf("index = %d, want %d", pl.Index(), tt.wantIndex)
			}
		})
	}
}

func TestEQPresetNames(t *testing.T) {
	names := EQPresetNames()
	if len(names) != len(eqPresets) || names[0] != "Flat" {
		t.Fatalf("EQPresetNames() = %v", names)
	}
	for _, name := range names {
		if _, ok := EQPresetByName(name); !ok {
			t.Fatalf("preset %q does not resolve", name)
		}
	}
}
