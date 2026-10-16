package gui

import (
	"slices"
	"testing"
)

func foundSet(names ...string) func(string) bool {
	return func(n string) bool { return slices.Contains(names, n) }
}

func TestMissingTools(t *testing.T) {
	tests := []struct {
		name  string
		found []string
		want  []string
	}{
		{"both installed", []string{"yt-dlp", "ffmpeg"}, nil},
		{"none installed", nil, []string{"yt-dlp", "ffmpeg"}},
		{"no ffmpeg", []string{"yt-dlp"}, []string{"ffmpeg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingTools(foundSet(tt.found...))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("missingTools = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInstallCommand(t *testing.T) {
	tests := []struct {
		name    string
		missing []string
		found   []string
		want    string
	}{
		{"nothing missing", nil, []string{"pacman"}, ""},
		{"arch", []string{"yt-dlp", "ffmpeg"}, []string{"pacman"}, "sudo pacman -S --needed yt-dlp ffmpeg"},
		{"debian both", []string{"yt-dlp", "ffmpeg"}, []string{"apt-get"}, "sudo apt install ffmpeg pipx && pipx ensurepath && pipx install yt-dlp"},
		{"debian ffmpeg", []string{"ffmpeg"}, []string{"apt-get"}, "sudo apt install ffmpeg"},
		{"debian yt-dlp", []string{"yt-dlp"}, []string{"apt-get"}, "sudo apt install pipx && pipx ensurepath && pipx install yt-dlp"},
		{"fedora", []string{"ffmpeg"}, []string{"dnf"}, "sudo dnf install ffmpeg"},
		{"opensuse", []string{"yt-dlp"}, []string{"zypper"}, "sudo zypper install yt-dlp"},
		{"unknown", []string{"ffmpeg"}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := installCommand(tt.missing, foundSet(tt.found...)); got != tt.want {
				t.Fatalf("installCommand = %q, want %q", got, tt.want)
			}
		})
	}
}
