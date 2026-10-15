package gui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestEveryStringHasItalian keeps the Italian table complete: every literal
// passed to T must have an entry.
func TestEveryStringHasItalian(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`\bT\(("(?:[^"\\]|\\.)*")\)`)
	missing := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllSubmatch(data, -1) {
			s, err := strconv.Unquote(string(m[1]))
			if err != nil {
				continue
			}
			if _, ok := italian[s]; !ok {
				missing[s] = f
			}
		}
	}
	for s, f := range missing {
		t.Errorf("%s: no Italian for %q", f, s)
	}
}
