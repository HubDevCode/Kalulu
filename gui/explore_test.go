package gui

import "testing"

func TestVideoID(t *testing.T) {
	tests := map[string]string{
		"https://music.youtube.com/watch?v=abc":    "abc",
		"https://www.youtube.com/watch?v=xyz&t=10": "xyz",
		"https://youtu.be/q1w2":                    "q1w2",
		"https://example.com/watch?v=abc":          "",
		"/music/song.mp3":                          "",
	}
	for in, want := range tests {
		if got := videoID(in); got != want {
			t.Errorf("videoID(%q) = %q, want %q", in, got, want)
		}
	}
}
