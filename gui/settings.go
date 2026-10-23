package gui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/djmix"
	"github.com/bjarneo/cliamp/ipc"
)

// settingsView holds every setting: the window's own preferences and all
// of config.toml, grouped in sections with a jump bar.
type settingsView struct {
	app      *App
	root     fyne.CanvasObject
	scroll   *container.Scroll
	body     *fyne.Container
	sections map[string]fyne.CanvasObject
	restart  *fyne.Container
	plugins  *widget.Select
	args     *widget.Entry
	output   *widget.Label
	values   map[string]string
}

func newSettingsView(a *App) *settingsView {
	s := &settingsView{app: a, sections: map[string]fyne.CanvasObject{}}
	s.body = container.NewVBox()
	s.scroll = container.NewVScroll(padded(0, 24, 24, 24, s.body))

	restartBtn := primaryButton(T("Restart now"), theme.ViewRefreshIcon(), a.restart)
	s.restart = padded(0, 24, 8, 24, panel(withAlpha(a.theme.p.Accent, 0x30), 10, padded(6, 12, 6, 12,
		container.NewBorder(nil, nil, nil, restartBtn, container.NewVBox(spacer(), boldLabel(T("Some changes apply after a restart")), spacer())))))
	s.restart.Hide()

	names := []string{T("General"), T("Personalization"), T("Home and services"), T("Notifications"), T("Playback"), T("Transitions"), T("Audio"),
		T("Lyrics"), T("Visualizer"), T("Network"), T("Downloads"), T("Performance"), T("Plugins"), T("Advanced"), T("Updates"), T("About")}
	var chips []fyne.CanvasObject
	for _, n := range names {
		n := n
		chips = append(chips, newChip(n, func() { s.jump(n) }))
	}
	header := container.NewVBox(padded(16, 24, 0, 24, heading(T("Settings"))), padded(6, 24, 8, 24, chipRow(chips...)), s.restart)
	s.root = container.NewBorder(header, nil, nil, nil, s.scroll)
	s.build()
	return s
}

func (s *settingsView) Object() fyne.CanvasObject { return s.root }

func (s *settingsView) Shown() {
	s.loadPlugins()
}

func (s *settingsView) jump(name string) {
	if obj, ok := s.sections[name]; ok {
		s.scroll.ScrollToOffset(fyne.NewPos(0, obj.Position().Y))
	}
}

func (s *settingsView) needsRestart() {
	s.restart.Show()
	s.root.Refresh()
}

func (s *settingsView) section(name string, objs ...fyne.CanvasObject) {
	box := container.NewVBox(sectionHeading(name))
	for _, o := range objs {
		box.Add(o)
	}
	s.sections[name] = box
	s.body.Add(box)
}

func (s *settingsView) build() {
	a := s.app
	s.values = configValues(a.opts.ConfigPath)

	// General: the window's own preferences.
	var names []string
	byName := map[string]string{}
	for _, code := range countryCodes {
		n := countryName(code)
		names = append(names, n)
		byName[n] = code
	}
	sort.Strings(names)
	countrySel := widget.NewSelect(names, func(n string) {
		if code := byName[n]; code != "" {
			a.setCountry(code)
		}
	})
	countrySel.Selected = countryName(a.country())
	langSel := widget.NewSelect([]string{"Italiano", "English"}, func(v string) {
		code := map[string]string{"Italiano": "it", "English": "en"}[v]
		if code == "" || code == uiLang {
			return
		}
		a.prefs.SetString(prefLanguage, code)
		uiLang = code
		a.ytm.SetLocale(a.country(), code)
		go runOnUI(func() { a.setPalette(a.theme.p.Name) })
	})
	langSel.Selected = map[string]string{"it": "Italiano", "en": "English"}[uiLang]
	themeSel := widget.NewSelect(PaletteNames(), func(name string) {
		if name != a.theme.p.Name {
			go runOnUI(func() { a.setPalette(name) })
		}
	})
	themeSel.Selected = a.theme.p.Name
	coverNames := map[string]string{coverStyleCover: T("Cover art"), coverStyleVinyl: T("Vinyl record")}
	coverSel := widget.NewSelect([]string{coverNames[coverStyleCover], coverNames[coverStyleVinyl]}, func(v string) {
		style := coverStyleCover
		if v == coverNames[coverStyleVinyl] {
			style = coverStyleVinyl
		}
		a.prefs.SetString(prefCoverStyle, style)
		if f, ok := a.views["player"].(*fullPlayer); ok {
			f.applyCoverStyle()
		}
	})
	coverSel.Selected = coverNames[a.coverStyle()]
	toggle := func(key string, def bool, after func(bool)) *widget.Check {
		c := widget.NewCheck("", func(on bool) {
			a.prefs.SetBool(key, on)
			if after != nil {
				after(on)
			}
		})
		c.Checked = a.prefs.BoolWithFallback(key, def)
		return c
	}
	desktop := []fyne.CanvasObject{
		setRow(T("Keep playing in the tray"), T("Closing the window leaves Kalulu in the system tray"), toggle(prefCloseToTray, a.trayHostAvailable(), nil)),
	}
	// The Windows installer adds the Start menu entry itself.
	if runtime.GOOS == "linux" {
		desktop = append(desktop, container.NewHBox(flatButton(T("Add Kalulu to the application menu"), theme.ComputerIcon(), s.installDesktopEntry)))
	}
	s.section(T("General"), settingsCard(T("Location and language"),
		wrapLabel(T("Recommendations, charts, radio and podcasts follow this country.")),
		setRow(T("Country"), "", countrySel),
		setRow(T("Language"), "", langSel),
	), settingsCard(T("Desktop"), desktop...))

	s.section(T("Personalization"), settingsCard(T("Appearance"),
		setRow(T("Theme"), "", themeSel),
		setRow(T("Dynamic colors"), T("The colors of the whole window follow the cover of the playing track"), toggle(prefDynamicColors, true, func(bool) { a.followCover() })),
		setRow(T("Now playing panel"), T("Shown on windows wider than 1180 px"), toggle(prefRightPane, true, func(bool) { a.win.Content().Refresh() })),
		setRow(T("Player cover"), T("The player page shows the cover, or a record that turns while the track plays"), coverSel),
	), a.customizeCard())

	// Home and services.
	items := []*widget.AccordionItem{}
	for _, g := range providerGroups() {
		items = append(items, widget.NewAccordionItem(g.Title, s.groupBody(g)))
	}
	s.section(T("Home and services"), a.homeServicesCard(), settingsCard(T("Services"),
		wrapLabel(T("Fill in a service and restart Kalulu; it then shows in the Library. Credentials stay in config.toml, shared with cliamp.")),
		widget.NewAccordion(items...),
	))

	s.section(T("Notifications"), settingsCard(T("Desktop notifications"),
		wrapLabel(T("All off by default. Kalulu never shows the same notification twice in a row.")),
		setRow(T("Notify track changes"), T("A desktop notification for each new track"), toggle(prefNotify, false, nil)),
		setRow(T("Notify DJ transitions"), T("A notification when the DJ Mixer starts a mix"), toggle(prefNotifyMix, false, nil)),
		setRow(T("Notify errors"), T("A notification when a track fails while the window is hidden"), toggle(prefNotifyErr, false, nil)),
	))

	// Playback: live controls and the startup defaults of config.toml.
	repeat := widget.NewSelect([]string{T("Off"), T("All"), T("One")}, nil)
	repeatVals := map[string]string{T("Off"): "off", T("All"): "all", T("One"): "one"}
	repeatShown := map[string]string{"Off": T("Off"), "All": T("All"), "One": T("One")}
	repeat.Selected = repeatShown[a.state.Repeat]
	repeat.OnChanged = func(v string) {
		if repeatShown[a.state.Repeat] != v {
			a.do("repeat", P{"name": repeatVals[v]}, nil)
		}
	}
	shuffle := widget.NewCheck("", func(on bool) {
		if a.state.Shuffle != nil && *a.state.Shuffle != on {
			a.do("shuffle", P{"name": map[bool]string{true: "on", false: "off"}[on]}, nil)
		}
	})
	if a.state.Shuffle != nil {
		shuffle.Checked = *a.state.Shuffle
	}
	a.onState(func(st ipc.RuntimeSnapshot) {
		if v := repeatShown[st.Repeat]; repeat.Selected != v {
			repeat.Selected = v
			repeat.Refresh()
		}
		if st.Shuffle != nil && shuffle.Checked != *st.Shuffle {
			shuffle.Checked = *st.Shuffle
			shuffle.Refresh()
		}
	})
	playbackObjs := []fyne.CanvasObject{settingsCard(T("Now"),
		setRow(T("Repeat"), "", repeat),
		setRow(T("Shuffle"), "", shuffle),
		setRow(T("Autoplay similar songs"), T("When the queue ends, keep going with songs like the last one"), toggle(prefAutoplay, true, nil)),
	)}
	playbackObjs = append(playbackObjs, a.playbackSettingsCard())
	for _, g := range configGroups()[:1] {
		playbackObjs = append(playbackObjs, s.groupCard(g))
	}
	s.section(T("Playback"), playbackObjs...)

	// Transitions.
	st := a.djSettings()
	mode := map[djmix.Mode]string{djmix.ModeOff: T("Gapless"), djmix.ModeCrossfade: T("Crossfade"), djmix.ModeDJ: "DJ Mixer"}[st.Mode]
	modeSel := widget.NewSelect([]string{T("Gapless"), T("Crossfade"), "DJ Mixer"}, func(v string) {
		m := map[string]djmix.Mode{T("Gapless"): djmix.ModeOff, T("Crossfade"): djmix.ModeCrossfade, "DJ Mixer": djmix.ModeDJ}[v]
		cur := a.djSettings()
		if cur.Mode != m {
			cur.Mode = m
			a.setDJSettings(cur)
		}
	})
	modeSel.Selected = mode
	s.section(T("Transitions"), settingsCard(T("Between tracks"),
		setRow(T("Mode"), T("Crossfade blends tracks; DJ Mixer beatmatches them"), modeSel),
		setRow(T("Preload"), T("Open the next track this many seconds before the end"), s.preloadSlider()),
		container.NewHBox(flatButton(T("Open DJ Mixer"), iconDJ, func() { a.show("dj") })),
	))

	// Audio.
	s.section(T("Audio"), settingsCard(T("Equalizer, balance and normalization"),
		wrapLabel(T("The equalizer, the L/R balance, the volume normalization and the mono output are on the Equalizer page.")),
		container.NewHBox(flatButton(T("Equalizer & audio…"), iconEqualizer, func() { a.show("eq") })),
	), s.groupCard(configGroups()[1]))

	lyricsGroup := configGroups()[2]
	s.section(T("Lyrics"), a.lyricsSettingsCard(), s.groupCard(cfgGroup{Title: T("Timing"), Fields: lyricsGroup.Fields[:1]}))
	s.section(T("Visualizer"), a.visualizerSettingsCard())
	s.section(T("Network"), a.qualitySettingsCard())
	s.section(T("Downloads"), settingsCard(T("Saved tracks"), a.downloadFormatRow()),
		s.groupCard(cfgGroup{Title: T("Folders"), Fields: lyricsGroup.Fields[1:]}))
	s.section(T("Performance"), a.performanceCard())

	// Plugins.
	s.plugins = widget.NewSelect(nil, nil)
	s.plugins.PlaceHolder = T("No plugin commands")
	s.args = widget.NewEntry()
	s.args.SetPlaceHolder(T("arguments (optional)"))
	s.output = label("")
	s.output.Wrapping = fyne.TextWrapWord
	s.output.Selectable = true
	run := widget.NewButtonWithIcon(T("Run"), theme.MediaPlayIcon(), s.runPlugin)
	pluginDir := filepath.Join(filepath.Dir(a.opts.ConfigPath), "plugins")
	s.section(T("Plugins"), settingsCard(T("Lua plugins"),
		wrapLabel(T("cliamp plugins work unchanged. Install one with: kalulu plugins install <source>")),
		setRow(T("Command"), "", container.NewBorder(nil, nil, nil, run, s.plugins)),
		setRow(T("Arguments"), "", s.args),
		s.output,
		container.NewHBox(flatButton(T("Open plugin folder"), theme.FolderOpenIcon(), func() { a.openPath(pluginDir) })),
	))

	// Advanced.
	adv := []fyne.CanvasObject{s.groupCard(configGroups()[4])}
	term := configGroups()[3]
	adv = append(adv, settingsCard(term.Title, wrapLabel(term.Help), widget.NewAccordion(widget.NewAccordionItem(T("Show"), s.groupBody(term)))))
	cfgPath := trunc(dimLabel(a.opts.ConfigPath))
	adv = append(adv, settingsCard(T("Configuration file"),
		cfgPath,
		container.NewHBox(
			flatButton(T("Edit config.toml…"), theme.DocumentCreateIcon(), s.editConfig),
			flatButton(T("Open config folder"), theme.FolderOpenIcon(), func() { a.openPath(filepath.Dir(a.opts.ConfigPath)) }),
		),
	))
	adv = append(adv, a.backupCard())
	s.section(T("Advanced"), adv...)

	s.section(T("Updates"), a.updatesCard())

	s.section(T("About"), settingsCard("Kalulu (ex GuiAmp) "+KaluluVersion,
		wrapLabel(T("A graphical fork of cliamp by Bjarne Øverli. Same engine, same providers, same plugins.")),
		container.NewHBox(
			widget.NewHyperlink("cliamp.stream", mustURL("https://cliamp.stream")),
			widget.NewHyperlink("github.com/bjarneo/cliamp", mustURL("https://github.com/bjarneo/cliamp")),
		),
		container.NewHBox(flatButton(T("Keyboard shortcuts"), theme.HelpIcon(), a.showShortcuts)),
	))
	s.body.Add(fixed(0, 24))
}

func (s *settingsView) preloadSlider() fyne.CanvasObject {
	a := s.app
	sl := widget.NewSlider(15, 120)
	sl.Step = 5
	sl.Value = a.preloadLead()
	l := label(fmt.Sprintf("%.0f s", sl.Value))
	sl.OnChanged = func(x float64) { l.SetText(fmt.Sprintf("%.0f s", x)) }
	sl.OnChangeEnded = func(x float64) {
		a.prefs.SetFloat(prefPreload, x)
		a.updateLead()
	}
	return container.NewBorder(nil, nil, nil, fixed(56, 0, l), sl)
}

// groupCard renders a config group as a card.
func (s *settingsView) groupCard(g cfgGroup) fyne.CanvasObject {
	objs := []fyne.CanvasObject{}
	if g.Help != "" {
		objs = append(objs, wrapLabel(g.Help))
	}
	objs = append(objs, s.groupBody(g))
	return settingsCard(g.Title, objs...)
}

func (s *settingsView) groupBody(g cfgGroup) fyne.CanvasObject {
	box := container.NewVBox()
	if g.Help != "" && len(g.Fields) > 0 && g.Fields[0].Section != "" {
		box.Add(wrapLabel(g.Help))
	}
	for _, f := range g.Fields {
		box.Add(s.fieldRow(f))
	}
	return box
}

// fieldRow builds the input of one config key. Every change is saved.
func (s *settingsView) fieldRow(f cfgField) fyne.CanvasObject {
	key := f.Key
	if f.Section != "" {
		key = f.Section + "." + f.Key
	}
	raw, set := s.values[key]
	value := f.Default
	if set {
		value = displayValue(raw, f.Kind)
	}
	save := func(v string) {
		if err := saveField(f, v); err != nil {
			s.app.toastError(err)
			return
		}
		s.values[key] = v
		if !f.Live {
			s.needsRestart()
		}
	}
	var input fyne.CanvasObject
	switch f.Kind {
	case kindBool:
		c := widget.NewCheck("", func(on bool) { save(strconv.FormatBool(on)) })
		c.Checked = value == "true"
		input = c
	case kindSelect:
		labels := f.Labels
		if len(labels) != len(f.Options) {
			labels = make([]string, len(f.Options))
			for i, o := range f.Options {
				labels[i] = o
				if o == "" {
					labels[i] = T("None")
				}
			}
		}
		sel := widget.NewSelect(labels, nil)
		for i, o := range f.Options {
			if o == value {
				sel.Selected = labels[i]
			}
		}
		sel.OnChanged = func(l string) {
			for i, lb := range labels {
				if lb == l {
					save(f.Options[i])
				}
			}
		}
		input = sel
	case kindInt, kindFloat:
		v, _ := strconv.ParseFloat(value, 64)
		sl := widget.NewSlider(f.Min, f.Max)
		sl.Step = 1
		if f.Kind == kindFloat {
			sl.Step = 0.5
		}
		sl.Value = v
		format := func(x float64) string {
			if f.Kind == kindInt {
				return strconv.Itoa(int(x))
			}
			return strconv.FormatFloat(x, 'f', 1, 64)
		}
		l := label(format(v))
		sl.OnChanged = func(x float64) { l.SetText(format(x)) }
		sl.OnChangeEnded = func(x float64) { save(format(x)) }
		input = container.NewBorder(nil, nil, nil, fixed(64, 0, l), sl)
	default:
		var e *widget.Entry
		if f.Kind == kindSecret {
			e = widget.NewPasswordEntry()
		} else {
			e = widget.NewEntry()
		}
		e.SetText(value)
		if f.Help != "" && f.Kind != kindPath {
			e.SetPlaceHolder(f.Help)
		}
		var timer *time.Timer
		e.OnChanged = func(v string) {
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(900*time.Millisecond, func() { runOnUI(func() { save(strings.TrimSpace(v)) }) })
		}
		input = e
		if f.Kind == kindPath {
			browse := iconButton(theme.FolderOpenIcon(), func() {
				go func() {
					if paths, ok := nativePicker(true); ok && len(paths) > 0 {
						runOnUI(func() { e.SetText(paths[0]) })
						return
					}
					runOnUI(func() {
						dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
							if err == nil && u != nil {
								e.SetText(u.Path())
							}
						}, s.app.win)
					})
				}()
			})
			input = container.NewBorder(nil, nil, nil, browse, e)
		}
	}
	help := f.Help
	if f.Kind != kindPath && (f.Kind == kindString || f.Kind == kindSecret) {
		help = ""
	}
	return setRow(f.Label, help, input)
}

// setRow is a settings line: the name and its help on the left, the input
// on the right.
func setRow(name, help string, input fyne.CanvasObject) fyne.CanvasObject {
	left := container.NewVBox(label(name))
	if help != "" {
		h := captionLabel(help)
		h.Wrapping = fyne.TextWrapWord
		left.Add(h)
	}
	if c, ok := input.(*widget.Check); ok && c.Text == "" {
		input = container.NewHBox(spacer(), c)
	}
	return container.New(&splitLayout{left: 0.45, gap: 16}, left, container.NewVBox(spacer(), input, spacer()))
}

func (s *settingsView) loadPlugins() {
	s.app.do("plugin.commands", nil, func(r Result) {
		items := append([]string(nil), r.Response.Items...)
		sort.Strings(items)
		s.plugins.Options = items
		if len(items) > 0 {
			s.plugins.PlaceHolder = T("Pick a command")
		}
		s.plugins.Refresh()
	})
}

func (s *settingsView) runPlugin() {
	parts := strings.SplitN(s.plugins.Selected, " ", 2)
	if len(parts) != 2 {
		return
	}
	args := strings.Fields(s.args.Text)
	s.output.SetText(T("Running…"))
	go func() {
		r, err := s.app.call("plugin.call", P{"name": parts[0], "sub": parts[1], "args": args})
		runOnUI(func() {
			if err != nil {
				s.output.SetText("⚠ " + err.Error())
				return
			}
			out := strings.TrimSpace(r.Response.Output)
			if out == "" {
				out = T("Done.")
			}
			s.output.SetText(out)
		})
	}()
}

func (s *settingsView) editConfig() {
	path := s.app.opts.ConfigPath
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		s.app.toastError(err)
		return
	}
	editor := widget.NewMultiLineEntry()
	editor.TextStyle = fyne.TextStyle{Monospace: true}
	editor.SetText(string(data))
	editor.Wrapping = fyne.TextWrapOff
	d := dialog.NewCustomConfirm("config.toml", T("Save"), T("Cancel"), container.NewBorder(
		dimLabel(T("Changes apply the next time Kalulu starts.")), nil, nil, nil, editor),
		func(ok bool) {
			if !ok {
				return
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				s.app.toastError(err)
				return
			}
			if err := os.WriteFile(path, []byte(editor.Text), 0o600); err != nil {
				s.app.toastError(err)
				return
			}
			s.needsRestart()
		}, s.app.win)
	d.Resize(fyne.NewSize(860, 620))
	d.Show()
}

// installDesktopEntry writes a .desktop file and the icon into the user's
// XDG data directory, so Kalulu shows in the application menu of every
// desktop environment.
func (s *settingsView) installDesktopEntry() {
	exe, err := os.Executable()
	if err != nil {
		s.app.toastError(err)
		return
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	iconDir := filepath.Join(dataHome, "icons", "hicolor", "512x512", "apps")
	appDir := filepath.Join(dataHome, "applications")
	for _, dir := range []string{iconDir, appDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.app.toastError(err)
			return
		}
	}
	if err := os.WriteFile(filepath.Join(iconDir, "kalulu.png"), logoPNG, 0o644); err != nil {
		s.app.toastError(err)
		return
	}
	if err := os.WriteFile(filepath.Join(appDir, "kalulu.desktop"), []byte(DesktopEntry(exe)), 0o644); err != nil {
		s.app.toastError(err)
		return
	}
	s.app.toast(T("Kalulu is now in the application menu"))
}

// DesktopEntry returns the freedesktop.org launcher of the binary at exe.
func DesktopEntry(exe string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Kalulu
GenericName=Music Player
Comment=Modern graphical music player — local files, streams, radio, podcasts and streaming services
Exec=%s %%U
TryExec=%s
Icon=kalulu
Terminal=false
Categories=AudioVideo;Audio;Player;Music;
Keywords=music;audio;player;radio;podcast;spotify;winamp;cliamp;dj;
MimeType=audio/mpeg;audio/flac;audio/x-flac;audio/ogg;audio/x-vorbis+ogg;audio/opus;audio/wav;audio/x-wav;audio/aac;audio/mp4;audio/x-m4a;audio/x-mpegurl;audio/x-scpls;application/vnd.apple.mpegurl;
StartupWMClass=Kalulu
StartupNotify=true
`, quoteExec(exe), exe)
}

func quoteExec(path string) string {
	if strings.ContainsAny(path, " \t\"'\\") {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}

// wrapLabel is a dim paragraph that wraps to the card width.
func wrapLabel(text string) *widget.Label {
	l := dimLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}

func settingsCard(title string, objects ...fyne.CanvasObject) fyne.CanvasObject {
	box := container.NewVBox(boldLabel(title))
	for _, o := range objects {
		box.Add(o)
	}
	return padded(0, 0, 12, 0, tPanel(roleCard, 14, padded(12, 16, 12, 16, box)))
}

func row(name string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, fixed(110, 0, dimLabel(name)), nil, obj)
}

func mustURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}
