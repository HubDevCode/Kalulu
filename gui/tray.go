package gui

import (
	"fmt"
	"runtime"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/godbus/dbus/v5"

	"github.com/bjarneo/cliamp/djmix"
	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/ui/model"
)

var (
	trayHostOnce sync.Once
	trayHost     bool
)

// trayHostAvailable reports whether a status notifier host (Plasma, Waybar,
// GNOME with the AppIndicator extension, Xfce…) can show the tray icon. On
// other systems it is assumed.
func (a *App) trayHostAvailable() bool {
	trayHostOnce.Do(func() {
		if runtime.GOOS != "linux" {
			trayHost = true
			return
		}
		conn, err := dbus.SessionBus()
		if err != nil {
			return
		}
		var has bool
		err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
		trayHost = err == nil && has
	})
	return trayHost
}

// trayMenu is the menu of the tray icon. It is rebuilt when the state it
// shows changes.
type trayMenu struct {
	app  *App
	desk desktop.App
	key  string
}

func (a *App) installTray() {
	desk, ok := a.fa.(desktop.App)
	if !ok {
		return
	}
	a.tray = &trayMenu{app: a, desk: desk}
	desk.SetSystemTrayIcon(LogoResource)
	desk.SetSystemTrayWindow(a.win)
	a.tray.update(a.state)
	a.onState(a.tray.update)
}

func (t *trayMenu) update(s ipc.RuntimeSnapshot) {
	a := t.app
	title := T("Nothing playing")
	if s.Track != nil {
		name, sub := trackTitle(*s.Track)
		title = name
		if sub != "" {
			title += " — " + sub
		}
	}
	mode := djmix.ModeOff
	if a.opts.DJ != nil {
		mode = a.opts.DJ.Settings().Mode
	}
	shuffle := s.Shuffle != nil && *s.Shuffle
	fav := s.Track != nil && s.Track.Bookmark
	key := fmt.Sprintf("%s|%s|%v|%s|%d|%s|%v|%.0f|%s", title, s.State, shuffle, s.Repeat, mode, s.EQPreset, fav, s.Volume, uiLang)
	if key == t.key {
		return
	}
	t.key = key
	if len([]rune(title)) > 48 {
		title = string([]rune(title)[:47]) + "…"
	}
	now := fyne.NewMenuItem("♪ "+title, func() { a.showWindow() })
	play := T("Play")
	if s.State == "playing" {
		play = T("Pause")
	}
	shuffleItem := fyne.NewMenuItem(T("Shuffle"), a.toggleShuffle)
	shuffleItem.Checked = shuffle
	repeat := fyne.NewMenuItem(T("Repeat"), nil)
	var repeatItems []*fyne.MenuItem
	for _, r := range []struct{ name, value, label string }{{"Off", "off", T("Off")}, {"All", "all", T("All")}, {"One", "one", T("One")}} {
		r := r
		it := fyne.NewMenuItem(r.label, func() { a.do("repeat", P{"name": r.value}, nil) })
		it.Checked = s.Repeat == r.name
		repeatItems = append(repeatItems, it)
	}
	repeat.ChildMenu = fyne.NewMenu("", repeatItems...)

	volume := fyne.NewMenuItem(fmt.Sprintf("%s (%s)", T("Volume"), volumeText(s.Volume, a.opts.VolumeMin)), nil)
	volume.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem(T("Volume up"), func() { a.adjustVolume(3) }),
		fyne.NewMenuItem(T("Volume down"), func() { a.adjustVolume(-3) }),
		fyne.NewMenuItem(T("Mute"), func() { a.playerBar.toggleMute() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("0 dB", func() { a.setVolume(0) }),
		fyne.NewMenuItem("-10 dB", func() { a.setVolume(-10) }),
		fyne.NewMenuItem("-20 dB", func() { a.setVolume(-20) }),
	)

	transitions := fyne.NewMenuItem(T("Transitions"), nil)
	var modeItems []*fyne.MenuItem
	for _, m := range []struct {
		mode  djmix.Mode
		label string
	}{{djmix.ModeOff, T("Gapless")}, {djmix.ModeCrossfade, T("Crossfade")}, {djmix.ModeDJ, "DJ Mixer"}} {
		m := m
		it := fyne.NewMenuItem(m.label, func() {
			st := a.djSettings()
			st.Mode = m.mode
			a.setDJSettings(st)
			if v, ok := a.views["dj"].(*djView); ok {
				v.refreshModes()
			}
			t.key = ""
			t.update(a.state)
		})
		it.Checked = mode == m.mode
		modeItems = append(modeItems, it)
	}
	transitions.ChildMenu = fyne.NewMenu("", modeItems...)

	eq := fyne.NewMenuItem(T("Equalizer"), nil)
	var eqItems []*fyne.MenuItem
	for _, name := range model.EQPresetNames() {
		name := name
		it := fyne.NewMenuItem(name, func() { a.do("eq", P{"name": name}, nil) })
		it.Checked = s.EQPreset == name
		eqItems = append(eqItems, it)
	}
	eq.ChildMenu = fyne.NewMenu("", eqItems...)

	favLabel := T("Add to favorites")
	if fav {
		favLabel = T("Remove from favorites")
	}
	items := []*fyne.MenuItem{
		now,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(play, a.togglePlay),
		fyne.NewMenuItem(T("Next"), a.next),
		fyne.NewMenuItem(T("Previous"), a.prev),
		fyne.NewMenuItem(T("Stop"), func() { a.do("stop", nil, nil) }),
		fyne.NewMenuItemSeparator(),
		shuffleItem, repeat, volume, transitions, eq,
	}
	if s.Track != nil {
		items = append(items, fyne.NewMenuItem(favLabel, a.playerBar.toggleFavorite))
		if videoID(s.Track.Path) != "" || s.Track.Artist != "" {
			track := *s.Track
			items = append(items, fyne.NewMenuItem(T("Start radio"), func() { a.startRadio(track) }))
		}
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(T("Show Kalulu"), a.showWindow),
		fyne.NewMenuItem("DJ Mixer", func() { a.showWindow(); a.show("dj") }),
		fyne.NewMenuItem(T("Settings"), func() { a.showWindow(); a.show("settings") }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(T("Quit"), a.quit),
	)
	t.desk.SetSystemTrayMenu(fyne.NewMenu("Kalulu", items...))
}

func (a *App) showWindow() {
	a.winVisible.Store(true)
	a.win.Show()
	a.win.RequestFocus()
}
