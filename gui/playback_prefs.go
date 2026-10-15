package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/player"
	"github.com/bjarneo/cliamp/resolve"
)

const (
	prefSilenceStart = "silence.start"
	prefSilenceEnd   = "silence.end"
	prefSilenceDB    = "silence.threshold_db"
	prefSilenceMin   = "silence.min_end_sec"
	prefOnError      = "playback.on_error"
	prefQuality      = "stream.quality"
	prefBuffer       = "stream.buffer_sec"
	prefDownloadFmt  = "download.format"
)

func (a *App) silenceSettings() player.SilenceSettings {
	d := player.DefaultSilenceSettings()
	return player.SilenceSettings{
		Start:       a.prefs.BoolWithFallback(prefSilenceStart, d.Start),
		End:         a.prefs.BoolWithFallback(prefSilenceEnd, d.End),
		ThresholdDB: a.prefs.FloatWithFallback(prefSilenceDB, d.ThresholdDB),
		MinEnd:      time.Duration(a.prefs.FloatWithFallback(prefSilenceMin, d.MinEnd.Seconds()) * float64(time.Second)),
	}
}

// applyPlaybackPrefs pushes the playback preferences to the engine.
func (a *App) applyPlaybackPrefs() {
	if a.opts.Mixer != nil {
		a.opts.Mixer.SetSilenceSkip(a.silenceSettings())
		a.opts.Mixer.SetBalance(a.prefs.FloatWithFallback(prefBalance, 0))
		a.opts.Mixer.SetNormalize(a.prefs.BoolWithFallback(prefNormalize, false))
	}
	player.SetStreamQuality(player.StreamQuality(a.prefs.IntWithFallback(prefQuality, int(player.QualityAuto))))
	player.SetNetworkBuffer(time.Duration(a.prefs.FloatWithFallback(prefBuffer, 12) * float64(time.Second)))
	resolve.DownloadFormat.Store(a.prefs.StringWithFallback(prefDownloadFmt, "original"))
	policy := a.prefs.StringWithFallback(prefOnError, "skip")
	go func() { _, _ = a.call("on_error", P{"name": policy}) }()
}

// playbackSettingsCard holds silence skipping and error handling.
func (a *App) playbackSettingsCard() fyne.CanvasObject {
	toggle := func(key string, def bool) *widget.Check {
		c := widget.NewCheck("", func(on bool) {
			a.prefs.SetBool(key, on)
			a.applyPlaybackPrefs()
		})
		c.Checked = a.prefs.BoolWithFallback(key, def)
		return c
	}
	slider := func(key string, min, max, step, def float64, format string) fyne.CanvasObject {
		s := widget.NewSlider(min, max)
		s.Step = step
		s.Value = a.prefs.FloatWithFallback(key, def)
		l := label(fmt.Sprintf(format, s.Value))
		s.OnChanged = func(v float64) { l.SetText(fmt.Sprintf(format, v)) }
		s.OnChangeEnded = func(v float64) {
			a.prefs.SetFloat(key, v)
			a.applyPlaybackPrefs()
		}
		return containerBorderRight(s, fixed(70, 0, l))
	}
	d := player.DefaultSilenceSettings()
	labels := []string{T("Skip to the next track"), T("Retry once, then skip"), T("Stop")}
	values := []string{"skip", "retry", "stop"}
	onError := widget.NewSelect(labels, func(l string) {
		for i, x := range labels {
			if x == l {
				a.prefs.SetString(prefOnError, values[i])
				a.applyPlaybackPrefs()
			}
		}
	})
	cur := a.prefs.StringWithFallback(prefOnError, "skip")
	for i, v := range values {
		if v == cur {
			onError.Selected = labels[i]
		}
	}
	return settingsCard(T("Silence and errors"),
		setRow(T("Skip silence at the start"), T("Leading silence is cut before the track plays"), toggle(prefSilenceStart, d.Start)),
		setRow(T("Skip silence at the end"), T("The next track starts when the end goes silent"), toggle(prefSilenceEnd, d.End)),
		setRow(T("Silence threshold"), T("Lower values treat only true digital silence as silence; quiet intros are kept"), slider(prefSilenceDB, -75, -35, 1, d.ThresholdDB, "%.0f dB")),
		setRow(T("Silence at the end lasts at least"), T("Shorter gaps are intentional pauses"), slider(prefSilenceMin, 0.5, 5, 0.5, d.MinEnd.Seconds(), "%.1f s")),
		setRow(T("When a track fails"), "", onError),
	)
}

// qualitySettingsCard holds the streaming quality, the network buffer and
// the download format.
func (a *App) qualitySettingsCard() fyne.CanvasObject {
	qLabels := []string{T("Automatic (adapts to the connection)"), T("High · Opus ~160 kbps"), T("Balanced · Opus ~70 kbps"), T("Data saver · Opus ~50 kbps")}
	quality := widget.NewSelect(qLabels, func(l string) {
		for i, x := range qLabels {
			if x == l {
				a.prefs.SetInt(prefQuality, i)
				a.applyPlaybackPrefs()
			}
		}
	})
	quality.Selected = qLabels[min(len(qLabels)-1, max(0, a.prefs.IntWithFallback(prefQuality, 0)))]
	speed := captionLabel("")
	updateSpeed := func() {
		if r := player.NetworkSpeed(); r > 0 {
			speed.SetText(fmt.Sprintf(T("Measured speed: %.1f× real time"), r))
		} else {
			speed.SetText(T("The speed is measured while streaming"))
		}
	}
	updateSpeed()
	buffer := widget.NewSlider(4, 60)
	buffer.Step = 2
	buffer.Value = a.prefs.FloatWithFallback(prefBuffer, 12)
	bl := label(fmt.Sprintf("%.0f s", buffer.Value))
	buffer.OnChanged = func(v float64) { bl.SetText(fmt.Sprintf("%.0f s", v)) }
	buffer.OnChangeEnded = func(v float64) {
		a.prefs.SetFloat(prefBuffer, v)
		a.applyPlaybackPrefs()
	}
	refresh := iconButton(theme.ViewRefreshIcon(), updateSpeed)
	return settingsCard(T("Streaming quality and network"),
		setRow(T("YouTube quality"), T("Opus gives the same quality as AAC with much less data"), quality),
		containerBorderRight(speed, refresh),
		setRow(T("Read-ahead buffer"), T("More seconds keep slow or unstable connections playing"), containerBorderRight(buffer, fixed(56, 0, bl))),
	)
}

// downloadFormatRow picks the container of saved tracks.
func (a *App) downloadFormatRow() fyne.CanvasObject {
	fLabels := []string{T("Original (no conversion)"), "Opus", "M4A (AAC)", "MP3 320", "FLAC"}
	fValues := []string{"original", "opus", "m4a", "mp3", "flac"}
	format := widget.NewSelect(fLabels, func(l string) {
		for i, x := range fLabels {
			if x == l {
				a.prefs.SetString(prefDownloadFmt, fValues[i])
				a.applyPlaybackPrefs()
			}
		}
	})
	cur := a.prefs.StringWithFallback(prefDownloadFmt, "original")
	for i, v := range fValues {
		if v == cur {
			format.Selected = fLabels[i]
		}
	}
	return setRow(T("Download format"), T("MP3 and FLAC re-encode a lossy source and do not add quality"), format)
}

// lyricsSettingsCard holds the look of the synced lyrics.
func (a *App) lyricsSettingsCard() fyne.CanvasObject {
	refresh := func() {
		if fp, ok := a.views["player"].(*fullPlayer); ok {
			fp.karaoke.Refresh()
		}
	}
	size := widget.NewSlider(16, 44)
	size.Step = 1
	size.Value = a.prefs.FloatWithFallback(prefLyricsSize, 26)
	sl := label(fmt.Sprintf("%.0f pt", size.Value))
	size.OnChanged = func(v float64) { sl.SetText(fmt.Sprintf("%.0f pt", v)) }
	size.OnChangeEnded = func(v float64) { a.prefs.SetFloat(prefLyricsSize, v); refresh() }
	aligns := []string{T("Left"), T("Centered")}
	align := widget.NewSelect(aligns, func(v string) {
		val := "left"
		if v == aligns[1] {
			val = "center"
		}
		a.prefs.SetString(prefLyricsAlign, val)
		refresh()
	})
	align.Selected = aligns[0]
	if a.prefs.StringWithFallback(prefLyricsAlign, "left") == "center" {
		align.Selected = aligns[1]
	}
	speeds := []string{T("Gentle"), T("Normal"), T("Snappy")}
	values := []float64{0.08, 0.16, 0.3}
	smooth := widget.NewSelect(speeds, func(v string) {
		for i, x := range speeds {
			if x == v {
				a.prefs.SetFloat(prefLyricsSmooth, values[i])
			}
		}
	})
	cur := a.prefs.FloatWithFallback(prefLyricsSmooth, 0.16)
	smooth.Selected = speeds[1]
	for i, v := range values {
		if v == cur {
			smooth.Selected = speeds[i]
		}
	}
	offset := widget.NewSlider(-3000, 3000)
	offset.Step = 50
	offset.Value = float64(a.prefs.IntWithFallback(prefLyricsOffset, 0))
	ol := label(fmt.Sprintf("%+.2f s", offset.Value/1000))
	offset.OnChanged = func(v float64) { ol.SetText(fmt.Sprintf("%+.2f s", v/1000)) }
	offset.OnChangeEnded = func(v float64) { a.prefs.SetInt(prefLyricsOffset, int(v)); a.lyrics.updateOffsetLabel() }
	return settingsCard(T("Lyrics"),
		wrapLabel(T("Lyrics open by clicking the cover. Sources: tags of the file, the music service, LRCLIB (exact match by title, artist, album and duration) and NetEase.")),
		setRow(T("Text size"), "", containerBorderRight(size, fixed(64, 0, sl))),
		setRow(T("Alignment"), "", align),
		setRow(T("Scrolling"), T("How quickly the lyrics follow the song"), smooth),
		setRow(T("Timing offset"), T("Positive values show the lines earlier"), containerBorderRight(offset, fixed(64, 0, ol))),
	)
}

// visualizerSettingsCard holds the look of the visualizer.
func (a *App) visualizerSettingsCard() fyne.CanvasObject {
	style := widget.NewSelect(visStyles, func(v string) { a.nowPlaying.vis.SetStyle(v) })
	style.Selected = a.prefs.StringWithFallback(prefVisStyle, "NCS")
	slider := func(key string, def float64) fyne.CanvasObject {
		s := widget.NewSlider(25, 200)
		s.Step = 5
		s.Value = a.prefs.FloatWithFallback(key, def) * 100
		l := label(fmt.Sprintf("%.0f%%", s.Value))
		s.OnChanged = func(v float64) { l.SetText(fmt.Sprintf("%.0f%%", v)) }
		s.OnChangeEnded = func(v float64) { a.prefs.SetFloat(key, v/100) }
		return containerBorderRight(s, fixed(64, 0, l))
	}
	colors := []string{T("From the cover"), T("From the theme")}
	cs := widget.NewSelect(colors, func(v string) {
		val := "cover"
		if v == colors[1] {
			val = "theme"
		}
		a.prefs.SetString(prefVisColors, val)
	})
	cs.Selected = colors[0]
	if a.prefs.StringWithFallback(prefVisColors, "cover") == "theme" {
		cs.Selected = colors[1]
	}
	return settingsCard(T("Visualizer"),
		setRow(T("Style"), T("NCS is the sphere of the NCS music videos"), style),
		setRow(T("Sensitivity"), T("Raise it for quiet music"), slider(prefVisSensitivity, 1)),
		setRow(T("Intensity"), T("How strongly shapes and pulses react"), slider(prefVisIntensity, 1)),
		setRow(T("Colors"), "", cs),
		wrapLabel(T("The visualizer stops drawing while it is hidden, so it costs nothing in the tray.")),
	)
}
