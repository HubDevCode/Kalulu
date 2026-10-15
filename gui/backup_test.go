package gui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSettingsValidates(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		name, body string
		ok         bool
	}{
		{"good", `{"format":"kalulu-settings","version":1,"preferences":{"palette":"Kalulu Dark"},"config_toml":"volume = 0"}`, true},
		{"other app", `{"format":"something","version":1,"preferences":{}}`, false},
		{"future", `{"format":"kalulu-settings","version":99,"preferences":{}}`, false},
		{"bad prefs", `{"format":"kalulu-settings","version":1,"preferences":[1,2]}`, false},
		{"not json", `hello`, false},
	}
	for _, tt := range tests {
		_, err := readSettings(write(tt.name+".json", tt.body))
		if (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v1.1.0", "1.0.0", true}, {"1.0.0", "1.0.0", false}, {"v0.9.9", "1.0.0", false},
		{"v1.0.0", "1.0.0-beta", true}, {"v1.0.0-rc1", "1.0.0", false}, {"v2", "1.9.9", true}, {"v1.0.10", "1.0.9", true},
	}
	for _, tt := range tests {
		if got := newerVersion(tt.latest, tt.current); got != tt.want {
			t.Errorf("newerVersion(%q, %q) = %v", tt.latest, tt.current, got)
		}
	}
}
