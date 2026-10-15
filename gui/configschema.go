package gui

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/ui/model"
)

// Kinds of config fields.
const (
	kindBool = iota
	kindInt
	kindFloat
	kindString
	kindSecret
	kindSelect
	kindPath
	kindList // a list of strings, entered comma-separated
)

// cfgField is one key of config.toml shown in Settings.
type cfgField struct {
	Section string // "" for top level
	Key     string
	Label   string
	Help    string
	Kind    int
	Options []string // values of a select; Labels may name them
	Labels  []string
	Min     float64
	Max     float64
	Default string
	// Live means the running player applies the value at once.
	Live bool
}

// cfgGroup is a titled block of fields.
type cfgGroup struct {
	Title  string
	Help   string
	Fields []cfgField
}

// configGroups lists every setting of config.toml that the window edits.
func configGroups() []cfgGroup {
	eq := append([]string{"Custom"}, model.EQPresetNames()...)
	return []cfgGroup{
		{Title: T("Playback at startup"), Fields: []cfgField{
			{Key: "volume", Label: T("Volume (dB)"), Kind: kindFloat, Min: -50, Max: 6, Default: "0"},
			{Key: "volume_min", Label: T("Volume floor (dB)"), Help: T("The quietest step of the volume control"), Kind: kindFloat, Min: -90, Max: 0, Default: "-50"},
			{Key: "repeat", Label: T("Repeat"), Kind: kindSelect, Options: []string{"off", "all", "one"}, Labels: []string{T("Off"), T("All"), T("One")}, Default: "off"},
			{Key: "shuffle", Label: T("Shuffle"), Kind: kindBool, Default: "false"},
			{Key: "mono", Label: T("Mono output"), Kind: kindBool, Default: "false"},
			{Key: "auto_play", Label: T("Start playing at launch"), Kind: kindBool, Default: "false"},
			{Key: "seek_large_step_sec", Label: T("Long seek step (s)"), Kind: kindInt, Min: 6, Max: 600, Default: "30"},
			{Key: "eq_preset", Label: T("Equalizer preset"), Kind: kindSelect, Options: eq, Default: "Flat"},
			{Key: "provider", Label: T("Source at startup (terminal mode)"), Kind: kindSelect,
				Options: []string{"cliamp", "radio", "podcast", "navidrome", "lyrion", "spotify", "qobuz", "tidal", "plex", "jellyfin", "emby", "audiobookshelf", "ytmusic", "soundcloud", "mixcloud", "netease", "yandex"}, Default: "cliamp"},
		}},
		{Title: T("Audio output"), Help: T("Changes here apply after a restart."), Fields: []cfgField{
			{Key: "audio_device", Label: T("Output device"), Help: T("Empty uses the system default; the Equalizer page lists the devices"), Kind: kindString},
			{Key: "sample_rate", Label: T("Sample rate"), Kind: kindSelect, Options: []string{"0", "22050", "44100", "48000", "96000", "192000"}, Labels: []string{T("Automatic"), "22.05 kHz", "44.1 kHz", "48 kHz", "96 kHz", "192 kHz"}, Default: "0"},
			{Key: "buffer_ms", Label: T("Buffer (ms)"), Help: T("Raise it for unstable radio streams"), Kind: kindInt, Min: 50, Max: 5000, Default: "250"},
			{Key: "resample_quality", Label: T("Resampling quality"), Kind: kindSelect, Options: []string{"1", "2", "3", "4"}, Labels: []string{"1", "2", "3", "4 (" + T("best") + ")"}, Default: "4"},
			{Key: "bit_depth", Label: T("Bit depth"), Help: T("32 is lossless for FFmpeg-decoded formats"), Kind: kindSelect, Options: []string{"16", "32"}, Default: "16"},
		}},
		{Title: T("Lyrics and files"), Fields: []cfgField{
			{Key: "lyrics_offset_ms", Label: T("Lyrics offset (ms)"), Kind: kindInt, Min: -10000, Max: 10000, Default: "0"},
			{Section: "downloads", Key: "directory", Label: T("Download folder"), Help: T("Empty uses ~/Music/cliamp"), Kind: kindPath},
			{Key: "initial_directory", Label: T("Start folder for files"), Kind: kindPath},
		}},
		{Title: T("Terminal interface (--tui)"), Help: T("These apply to kalulu --tui, the original cliamp interface."), Fields: []cfgField{
			{Key: "theme", Label: T("Theme"), Kind: kindString},
			{Key: "visualizer", Label: T("Visualizer"), Kind: kindString, Default: "Bars"},
			{Key: "vis_rows", Label: T("Visualizer rows"), Kind: kindInt, Min: 1, Max: 40, Default: "7"},
			{Key: "vis_volume_linked", Label: T("Visualizer follows the volume"), Kind: kindBool, Default: "true"},
			{Key: "simplified", Label: T("Simplified view"), Kind: kindBool, Default: "false"},
			{Key: "hide_help_bar", Label: T("Hide the key hints"), Kind: kindBool, Default: "false"},
			{Key: "hide_settings_pane", Label: T("Hide the settings pane"), Kind: kindBool, Default: "false"},
			{Key: "show_metadata", Label: T("Show metadata"), Kind: kindBool, Default: "false"},
			{Key: "expanded", Label: T("Expanded playlist"), Kind: kindBool, Default: "false"},
			{Key: "padding_horizontal", Label: T("Horizontal padding"), Kind: kindInt, Min: 0, Max: 10, Default: "3"},
			{Key: "padding_vertical", Label: T("Vertical padding"), Kind: kindInt, Min: 0, Max: 5, Default: "1"},
			{Key: "low_power", Label: T("Low power mode"), Kind: kindBool, Default: "false"},
		}},
		{Title: T("Advanced"), Fields: []cfgField{
			{Key: "log_level", Label: T("Log level"), Kind: kindSelect, Options: []string{"debug", "info", "warn", "error"}, Default: "info"},
		}},
	}
}

// providerGroups lists the settings of each service.
func providerGroups() []cfgGroup {
	cookies := []string{"", "chrome", "chromium", "firefox", "brave", "edge", "opera", "vivaldi", "safari"}
	url := func(section, example string) cfgField {
		return cfgField{Section: section, Key: "url", Label: "URL", Help: example, Kind: kindString}
	}
	user := func(section string) cfgField {
		return cfgField{Section: section, Key: "user", Label: T("User"), Kind: kindString}
	}
	pass := func(section string) cfgField {
		return cfgField{Section: section, Key: "password", Label: T("Password"), Kind: kindSecret}
	}
	token := func(section, label string) cfgField {
		return cfgField{Section: section, Key: "token", Label: label, Kind: kindSecret}
	}
	enabled := func(section string) cfgField {
		return cfgField{Section: section, Key: "enabled", Label: T("Enabled"), Kind: kindBool, Default: "false"}
	}
	return []cfgGroup{
		{Title: "Spotify", Help: T("Needs Spotify Premium. Sign in from the Spotify source after a restart."), Fields: []cfgField{
			{Section: "spotify", Key: "client_id", Label: "Client ID", Help: T("Optional: your own app from developer.spotify.com"), Kind: kindString},
			{Section: "spotify", Key: "bitrate", Label: T("Quality"), Kind: kindSelect, Options: []string{"96", "160", "320"}, Labels: []string{"96 kbps", "160 kbps", "320 kbps"}, Default: "320"},
		}},
		{Title: "Tidal", Help: T("Needs a paid Tidal plan."), Fields: []cfgField{
			enabled("tidal"),
			{Section: "tidal", Key: "quality", Label: T("Quality"), Kind: kindSelect, Options: []string{"low", "high", "lossless", "hires"}, Labels: []string{"AAC 96", "AAC 320", "FLAC", "Hi-Res"}, Default: "lossless"},
			{Section: "tidal", Key: "client_id", Label: "Client ID", Kind: kindString},
			{Section: "tidal", Key: "client_secret", Label: "Client secret", Kind: kindSecret},
		}},
		{Title: "Qobuz", Help: T("Needs a Qobuz subscription."), Fields: []cfgField{
			enabled("qobuz"),
			{Section: "qobuz", Key: "quality", Label: T("Quality"), Kind: kindSelect, Options: []string{"5", "6", "7", "27"}, Labels: []string{"MP3 320", "FLAC 16/44.1", "FLAC 24/96", "FLAC 24/192"}, Default: "6"},
		}},
		{Title: "YouTube Music", Help: T("Home, Search and the radio already work without an account; this adds your library."), Fields: []cfgField{
			{Section: "ytmusic", Key: "cookies_from", Label: T("Browser session"), Kind: kindSelect, Options: cookies},
			{Section: "ytmusic", Key: "client_id", Label: "Client ID", Kind: kindString},
			{Section: "ytmusic", Key: "client_secret", Label: "Client secret", Kind: kindSecret},
			{Section: "ytmusic", Key: "expand_playlist", Label: T("Open whole playlists from list= links"), Kind: kindBool, Default: "true"},
		}},
		{Title: "SoundCloud", Fields: []cfgField{
			enabled("soundcloud"), user("soundcloud"),
			{Section: "soundcloud", Key: "cookies_from", Label: T("Browser session"), Kind: kindSelect, Options: cookies},
		}},
		{Title: "Mixcloud", Fields: []cfgField{
			enabled("mixcloud"),
			{Section: "mixcloud", Key: "username", Label: T("User"), Kind: kindString},
			{Section: "mixcloud", Key: "access_token", Label: "Access token", Kind: kindSecret},
			{Section: "mixcloud", Key: "cookies_from", Label: T("Browser session"), Kind: kindSelect, Options: cookies},
			{Section: "mixcloud", Key: "max_items", Label: T("Items per list"), Kind: kindInt, Min: 10, Max: 500, Default: "100"},
		}},
		{Title: "Navidrome / Subsonic", Fields: []cfgField{
			url("navidrome", "https://music.example.com"), user("navidrome"), pass("navidrome"),
			{Section: "navidrome", Key: "browse_sort", Label: T("Album order"), Kind: kindSelect, Options: []string{"alphabeticalByName", "alphabeticalByArtist", "newest", "recent", "frequent", "starred", "byYear", "byGenre"}},
			{Section: "navidrome", Key: "format", Label: T("Stream format"), Help: T("raw always plays the original file"), Kind: kindString},
		}},
		{Title: "Jellyfin", Fields: []cfgField{
			url("jellyfin", "https://jellyfin.example.com"), user("jellyfin"), pass("jellyfin"), token("jellyfin", "API token"),
			{Section: "jellyfin", Key: "user_id", Label: "User ID", Kind: kindString},
		}},
		{Title: "Emby", Fields: []cfgField{
			url("emby", "https://emby.example.com"), user("emby"), pass("emby"), token("emby", "API key"),
			{Section: "emby", Key: "user_id", Label: "User ID", Kind: kindString},
		}},
		{Title: "Plex", Fields: []cfgField{
			url("plex", "http://192.168.1.10:32400"), token("plex", "Token"),
			{Section: "plex", Key: "libraries", Label: T("Libraries"), Help: T("Comma-separated; empty loads all"), Kind: kindList},
		}},
		{Title: "Lyrion (LMS)", Fields: []cfgField{
			url("lyrion", "http://nas.local:9000"), user("lyrion"), pass("lyrion"),
			{Section: "lyrion", Key: "show_unplayable", Label: T("Show plugin tracks it cannot play"), Kind: kindBool, Default: "false"},
		}},
		{Title: "Audiobookshelf", Fields: []cfgField{
			url("audiobookshelf", "https://abs.example.com"), token("audiobookshelf", "API key"), user("audiobookshelf"), pass("audiobookshelf"),
			{Section: "audiobookshelf", Key: "libraries", Label: T("Libraries"), Kind: kindList},
		}},
		{Title: "NetEase Cloud Music", Fields: []cfgField{
			enabled("netease"),
			{Section: "netease", Key: "cookies_from", Label: T("Browser session"), Kind: kindSelect, Options: cookies},
			{Section: "netease", Key: "user_id", Label: "User ID", Kind: kindString},
		}},
		{Title: "Yandex Music", Fields: []cfgField{
			enabled("yandex"), token("yandex", "OAuth token"),
		}},
		{Title: T("Podcasts (terminal mode)"), Fields: []cfgField{
			{Section: "podcast", Key: "country", Label: T("Chart country"), Help: T("Two letters, such as it or us"), Kind: kindString},
		}},
	}
}

// configValues reads the raw values of config.toml, keyed "section.key"
// ("key" for the top level). Comments and blank lines are skipped.
func configValues(path string) map[string]string {
	values := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return values
	}
	defer f.Close()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.Trim(line, "[]")))
			switch section {
			case "youtube", "yt":
				section = "ytmusic"
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if section != "" {
			key = section + "." + key
		}
		values[key] = stripComment(strings.TrimSpace(v))
	}
	return values
}

// stripComment drops a trailing # comment outside of quotes.
func stripComment(v string) string {
	inQuote := false
	for i, r := range v {
		switch r {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return strings.TrimSpace(v[:i])
			}
		}
	}
	return v
}

// displayValue turns a raw TOML value into the text of an input.
func displayValue(raw string, kind int) string {
	raw = strings.TrimSpace(raw)
	if kind == kindList {
		raw = strings.Trim(raw, "[]")
		var parts []string
		for _, p := range strings.Split(raw, ",") {
			if p = strings.Trim(strings.TrimSpace(p), `"`); p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, ", ")
	}
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2 {
		if s, err := strconv.Unquote(raw); err == nil {
			return s
		}
		return raw[1 : len(raw)-1]
	}
	return raw
}

// saveField writes one value; an empty value removes an optional key.
func saveField(f cfgField, value string) error {
	var raw string
	switch f.Kind {
	case kindBool, kindInt, kindFloat:
		raw = value
	case kindList:
		var parts []string
		for _, p := range strings.Split(value, ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, config.QuoteString(p))
			}
		}
		raw = "[" + strings.Join(parts, ", ") + "]"
	default:
		if f.Kind == kindSelect {
			if _, err := strconv.Atoi(value); err == nil && f.Key != "provider" && f.Key != "eq_preset" {
				raw = value
				break
			}
		}
		raw = config.QuoteString(value)
	}
	if f.Section == "" {
		switch f.Kind {
		case kindBool:
			return config.SaveBool(f.Key, value == "true")
		case kindInt, kindFloat:
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return err
			}
			return config.SaveFloat(f.Key, v, -1)
		case kindSelect:
			if raw == value {
				v, _ := strconv.ParseFloat(value, 64)
				return config.SaveFloat(f.Key, v, -1)
			}
		}
		return config.SaveString(f.Key, value)
	}
	if value == "" && f.Kind != kindBool {
		return config.SaveSection(f.Section, nil, []string{f.Key})
	}
	return config.SaveSection(f.Section, []config.KeyValue{{Key: f.Key, Value: raw}}, nil)
}
