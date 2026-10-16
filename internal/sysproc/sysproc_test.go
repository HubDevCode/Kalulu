package sysproc

import (
	"context"
	"testing"
)

func TestCommandKeepsNameAndArgs(t *testing.T) {
	tests := []struct {
		name string
		cmd  func() []string
	}{
		{"Command", func() []string { return Command("ffmpeg", "-i", "x").Args }},
		{"CommandContext", func() []string { return CommandContext(context.Background(), "ffmpeg", "-i", "x").Args }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cmd()
			if len(got) != 3 || got[0] != "ffmpeg" || got[1] != "-i" || got[2] != "x" {
				t.Fatalf("Args = %q", got)
			}
		})
	}
}
