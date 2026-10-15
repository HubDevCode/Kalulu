package gui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/resolve"
)

// audioExtensions filters the Fyne file dialog.
var audioExtensions = []string{
	".mp3", ".flac", ".ogg", ".oga", ".opus", ".wav", ".m4a", ".m4b", ".aac", ".alac",
	".wma", ".aiff", ".aif", ".ape", ".wv", ".mka", ".webm", ".mp4", ".m3u", ".m3u8", ".pls",
}

// nativePicker runs the desktop's own file chooser when one is installed,
// which gives multi-select and the familiar dialog of the desktop. It
// reports false when none is available.
func nativePicker(folder bool) ([]string, bool) {
	type tool struct {
		name string
		args []string
	}
	var tools []tool
	if folder {
		tools = []tool{
			{"zenity", []string{"--file-selection", "--directory", "--title=Open folder"}},
			{"qarma", []string{"--file-selection", "--directory", "--title=Open folder"}},
			{"kdialog", []string{"--getexistingdirectory", "."}},
			{"yad", []string{"--file", "--directory", "--title=Open folder"}},
		}
	} else {
		filter := "Audio | *" + strings.Join(audioExtensions, " *")
		tools = []tool{
			{"zenity", []string{"--file-selection", "--multiple", "--separator=\n", "--title=Open files", "--file-filter=" + filter, "--file-filter=All files | *"}},
			{"qarma", []string{"--file-selection", "--multiple", "--separator=\n", "--title=Open files"}},
			{"kdialog", []string{"--getopenfilename", ".", "*" + strings.Join(audioExtensions, " *") + "|Audio files", "--multiple", "--separate-output"}},
			{"yad", []string{"--file", "--multiple", "--separator=\n", "--title=Open files"}},
		}
	}
	// Prefer kdialog on KDE, where zenity looks out of place.
	if strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "kde") {
		for i, t := range tools {
			if t.name == "kdialog" {
				tools[0], tools[i] = tools[i], tools[0]
			}
		}
	}
	for _, t := range tools {
		path, err := exec.LookPath(t.name)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, t.args...).Output()
		if err != nil {
			// Cancel exits non-zero; that is an answer, not a failure.
			if _, ok := err.(*exec.ExitError); ok {
				return nil, true
			}
			continue
		}
		var paths []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				paths = append(paths, line)
			}
		}
		return paths, true
	}
	return nil, false
}

func (a *App) openFiles() {
	go func() {
		if paths, ok := nativePicker(false); ok {
			a.openPaths(paths)
			return
		}
		runOnUI(func() {
			d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
				if err != nil || r == nil {
					return
				}
				path := r.URI().Path()
				r.Close()
				a.openPaths([]string{path})
			}, a.win)
			d.SetFilter(storage.NewExtensionFileFilter(audioExtensions))
			d.Resize(fyne.NewSize(860, 600))
			d.Show()
		})
	}()
}

func (a *App) openFolder() {
	go func() {
		if paths, ok := nativePicker(true); ok {
			a.openPaths(paths)
			return
		}
		runOnUI(func() {
			d := dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
				if err != nil || u == nil {
					return
				}
				a.openPaths([]string{u.Path()})
			}, a.win)
			d.Resize(fyne.NewSize(860, 600))
			d.Show()
		})
	}()
}

// openPaths scans files, folders and local playlists and adds them to the
// queue. The first of them plays when nothing plays.
func (a *App) openPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	go func() {
		var locals, remotes []string
		for _, p := range paths {
			if playlist.IsURL(p) {
				remotes = append(remotes, p)
			} else {
				locals = append(locals, p)
			}
		}
		if len(locals) > 0 {
			a.toast("Scanning…")
			res, err := resolve.Args(locals)
			if err != nil {
				a.toastError(err)
			}
			infos := make([]ipc.TrackInfo, 0, len(res.Tracks))
			for _, t := range res.Tracks {
				infos = append(infos, trackInfo(t))
			}
			remotes = append(remotes, res.Pending...)
			if len(infos) == 0 && len(remotes) == 0 {
				a.toast("No audio files found")
			}
			runOnUI(func() { a.addTracks(infos, a.state.State != "playing") })
		}
		for i, u := range remotes {
			a.loadURL(u, i == 0 && len(locals) == 0)
		}
	}()
}

func trackInfo(t playlist.Track) ipc.TrackInfo {
	return ipc.TrackInfo{
		Title: t.Title, Artist: t.Artist, Album: t.Album, Genre: t.Genre,
		Path: t.Path, AlbumArtURL: t.AlbumArtURL, Year: t.Year,
		TrackNumber: t.TrackNumber, DurationSecs: t.DurationSecs,
		Stream: t.Stream, Realtime: t.Realtime, Feed: t.Feed, Restricted: t.Restricted,
		ProviderMeta: t.ProviderMeta,
	}
}

// loadURL resolves a URL (stream, playlist, feed, YouTube, Bandcamp…) and
// appends it to the queue.
func (a *App) loadURL(u string, play bool) {
	a.toast("Opening " + u + "…")
	a.do("url.load", P{"path": u, "play": play}, func(r Result) {
		if r.Response.Total > 1 {
			a.toast(fmt.Sprintf(T("Added %d tracks"), r.Response.Total))
		} else if len(r.Response.Tracks) == 1 {
			title, _ := trackTitle(r.Response.Tracks[0])
			a.toast("Added " + title)
		}
	})
}

func (a *App) openURL() {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("https://… , ssh://host/path , podcast feed, YouTube, SoundCloud, Bandcamp…")
	if clip := a.fa.Clipboard().Content(); playlist.IsURL(strings.TrimSpace(clip)) {
		entry.SetText(strings.TrimSpace(clip))
	}
	playNow := widget.NewCheck(T("Play now"), nil)
	playNow.Checked = true
	hint := captionLabel("Streams, M3U/PLS, HLS, RSS feeds, and everything yt-dlp supports.")
	d := dialog.NewCustomConfirm("Open URL", T("Open"), "Cancel",
		container.NewVBox(entry, playNow, hint), func(ok bool) {
			u := strings.TrimSpace(entry.Text)
			if ok && u != "" {
				a.loadURL(u, playNow.Checked)
			}
		}, a.win)
	d.Resize(fyne.NewSize(620, 220))
	entry.OnSubmitted = func(string) { d.Confirm() }
	d.Show()
	a.win.Canvas().Focus(entry)
}

func (a *App) onDropped(_ fyne.Position, uris []fyne.URI) {
	var paths []string
	for _, u := range uris {
		if u.Scheme() == "file" {
			paths = append(paths, u.Path())
		} else {
			paths = append(paths, u.String())
		}
	}
	a.openPaths(paths)
}

func (a *App) openPath(path string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(path, 0o755)
	u := &url.URL{Scheme: "file", Path: path}
	if err := a.fa.OpenURL(u); err != nil {
		a.toastError(err)
	}
}

func (a *App) showTrackInfo(t ipc.TrackInfo) {
	title, _ := trackTitle(t)
	art := newArtView(a, 160, 10)
	art.Load(t)
	form := widget.NewForm()
	add := func(name, value string) {
		if value == "" || value == "0" {
			return
		}
		l := widget.NewLabel(value)
		l.Wrapping = fyne.TextWrapBreak
		l.Selectable = true
		form.Append(name, l)
	}
	add("Title", t.Title)
	add(T("Artist"), t.Artist)
	add("Album", t.Album)
	add("Genre", t.Genre)
	add("Year", fmt.Sprint(t.Year))
	add("Track", fmt.Sprint(t.TrackNumber))
	if t.DurationSecs > 0 {
		add("Duration", fmtDuration(float64(t.DurationSecs)))
	}
	add(T("Station"), t.Station)
	add("Now on air", t.StreamTitle)
	add(T("Location"), t.Path)
	var kinds []string
	if t.Stream {
		kinds = append(kinds, "stream")
	}
	if t.Realtime {
		kinds = append(kinds, "live")
	}
	if t.Feed {
		kinds = append(kinds, "feed")
	}
	if t.Bookmark {
		kinds = append(kinds, "favorite")
	}
	add("Kind", strings.Join(kinds, ", "))
	for k, v := range t.ProviderMeta {
		if strings.HasSuffix(k, "token") || strings.Contains(k, "secret") {
			continue
		}
		add(k, v)
	}
	buttons := container.NewHBox()
	if isLocalPath(t.Path) {
		buttons.Add(widget.NewButtonWithIcon("Show in folder", theme.FolderOpenIcon(), func() { a.openPath(filepath.Dir(t.Path)) }))
	}
	buttons.Add(widget.NewButtonWithIcon("Copy location", theme.ContentCopyIcon(), func() {
		a.fa.Clipboard().SetContent(t.Path)
		a.toast("Copied")
	}))
	content := container.NewBorder(nil, buttons, container.NewVBox(art.Object()), nil, container.NewVScroll(form))
	d := dialog.NewCustom(title, "Close", content, a.win)
	d.Resize(fyne.NewSize(720, 440))
	d.Show()
}

// shortcutHelp lists the keys, translated when the dialog opens.
func shortcutHelp() [][2]string {
	return [][2]string{
		{T("Space"), T("Play / pause")},
		{"N  /  P", T("Next / previous track")},
		{"→  /  ←", T("Seek 5 s forward / back")},
		{"Shift + →  /  ←", T("Seek 30 s forward / back")},
		{"↑  /  ↓  " + T("or") + "  +  /  -", T("Volume up / down")},
		{"M", T("Mute")},
		{"S", T("Toggle shuffle")},
		{"R", T("Cycle repeat (off, all, one)")},
		{"O", T("Toggle mono")},
		{"[  /  ]", T("Speed down / up")},
		{"V", T("Next visualizer style")},
		{"F  " + T("or") + "  F11", T("Full screen visualizer / window")},
		{T("Delete"), T("Remove the selected queue row")},
		{"Ctrl + F", T("Search")},
		{"Ctrl + O", T("Open files")},
		{"Ctrl + Shift + O", T("Open folder")},
		{"Ctrl + L", T("Open URL")},
		{"Ctrl + E", T("Equalizer")},
		{"Ctrl + 1 … 5", T("Queue, Search, Player and lyrics, Equalizer, History")},
		{"Ctrl + B", T("Toggle the now playing panel")},
		{"Ctrl + ,", T("Settings")},
		{"Ctrl + Q", T("Quit")},
		{"F1", T("This help")},
	}
}

func (a *App) showShortcuts() {
	grid := container.NewGridWithColumns(2)
	for _, s := range shortcutHelp() {
		k := boldLabel(s[0])
		k.Importance = widget.HighImportance
		grid.Add(k)
		grid.Add(widget.NewLabel(s[1]))
	}
	d := dialog.NewCustom(T("Keyboard shortcuts"), T("Close"), container.NewVScroll(grid), a.win)
	d.Resize(fyne.NewSize(560, 620))
	d.Show()
}

func (a *App) showAbout() {
	logo := canvas.NewImageFromResource(LogoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(96, 96))
	name := heading("Kalulu")
	name.Alignment = fyne.TextAlignCenter
	ver := dimLabel(T("Version") + " " + KaluluVersion + " · Kalulu (ex GuiAmp) · cliamp " + a.opts.Version)
	ver.Alignment = fyne.TextAlignCenter
	desc := widget.NewLabel(T("A modern graphical music player, forked from cliamp — the retro terminal player by Bjarne Øverli. Local files, streams, internet radio, podcasts, Spotify, Tidal, Qobuz, YouTube Music, SoundCloud, Navidrome, Jellyfin, Plex, Emby and more, with a 10-band EQ, synced lyrics, visualizers and Lua plugins."))
	desc.Wrapping = fyne.TextWrapWord
	desc.Alignment = fyne.TextAlignCenter
	links := container.NewHBox(spacer(),
		widget.NewHyperlink("cliamp.stream", mustURL("https://cliamp.stream")),
		widget.NewHyperlink("Kalulu", mustURL("https://github.com/HubDevCode/Kalulu")),
		widget.NewHyperlink("cliamp", mustURL("https://github.com/bjarneo/cliamp")),
		spacer())
	license := captionLabel("GPL-3.0-or-later · cliamp © Bjarne Øverli, MIT")
	license.Alignment = fyne.TextAlignCenter
	d := dialog.NewCustom(T("About"), T("Close"), container.NewVBox(container.NewCenter(logo), name, ver, desc, links, license), a.win)
	d.Resize(fyne.NewSize(480, 420))
	d.Show()
}

// showFullVisualizer opens a window with a large visualizer and the track.
func (a *App) showFullVisualizer() {
	w := a.fa.NewWindow("Kalulu — Visualizer")
	vis := newVisualizer(a, true)
	title := heading("")
	title.Alignment = fyne.TextAlignCenter
	sub := dimLabel("")
	sub.Alignment = fyne.TextAlignCenter
	hint := captionLabel(T("Double-click: next style · F: full screen · Esc: close"))
	hint.Alignment = fyne.TextAlignCenter
	update := func(s ipc.RuntimeSnapshot) {
		if s.Track == nil {
			title.SetText(T("Nothing playing"))
			sub.SetText("")
			return
		}
		t, st := trackTitle(*s.Track)
		if title.Text != t {
			title.SetText(t)
		}
		if sub.Text != st {
			sub.SetText(st)
		}
	}
	update(a.state)
	closed := false
	a.onState(func(s ipc.RuntimeSnapshot) {
		if !closed {
			update(s)
		}
	})
	bg := canvas.NewRectangle(a.theme.p.BG)
	w.SetContent(container.NewStack(bg, container.NewBorder(
		padded(24, 24, 0, 24, container.NewVBox(title, sub)), padded(0, 0, 12, 0, hint), nil, nil,
		padded(10, 30, 20, 30, vis))))
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyEscape:
			w.Close()
		case fyne.KeyF, fyne.KeyF11:
			w.SetFullScreen(!w.FullScreen())
		case fyne.KeySpace:
			a.togglePlay()
		case fyne.KeyRight:
			a.next()
		case fyne.KeyLeft:
			a.prev()
		case fyne.KeyV:
			vis.Cycle()
		}
	})
	w.SetOnClosed(func() {
		closed = true
		vis.Close()
	})
	w.Resize(fyne.NewSize(1000, 640))
	w.Show()
}
