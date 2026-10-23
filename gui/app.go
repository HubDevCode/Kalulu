package gui

import (
	"context"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/discover"
	"github.com/bjarneo/cliamp/ipc"
)

// AppID is the Fyne application ID; it names the preferences directory.
const AppID = "io.kalulu.Kalulu"

// legacyAppID is the ID of Kalulu, whose preferences Kalulu takes over.
const legacyAppID = "stream.guiamp.GuiAmp"

// view is one page of the main area.
type view interface {
	Object() fyne.CanvasObject
	// Shown runs each time the page becomes visible.
	Shown()
}

// App is the Kalulu window and its state.
type App struct {
	prefetchedFor   string
	prefetchedStart bool
	notifyMu        sync.Mutex
	lastNotify      string
	lastNotifyAt    time.Time
	engine          Engine
	opts            Options
	fa              fyne.App
	win             fyne.Window
	prefs           fyne.Preferences
	theme           *guiTheme

	// state is the last runtime snapshot. It is read and written on the
	// UI thread only.
	state     ipc.RuntimeSnapshot
	listeners []func(ipc.RuntimeSnapshot)
	poke      chan struct{}
	// playing mirrors state for the visualizer goroutines.
	playing atomic.Bool
	// cover is the dominant color of the playing cover.
	coverMu  sync.Mutex
	cover    color.NRGBA
	hasCover bool
	// rightShown is true while the now playing panel is on screen.
	rightShown atomic.Bool
	// winVisible is false while the window is hidden in the tray.
	winVisible atomic.Bool

	providers   []ipc.ProviderInfo
	providersMu sync.Mutex

	nav            map[string]*navItem
	navCompactHide []fyne.CanvasObject
	compactNav     bool
	navBox         *fyne.Container
	navCards       []fyne.CanvasObject
	navSources     *widget.Label
	navExtra       []*navItem // footer rows outside a.nav
	views          map[string]view
	current        string
	content        *fyne.Container
	rightPane      *fyne.Container
	toastBox       *fyne.Container
	toastText      *widget.Label
	toastSeq       int

	playerBar  *playerBar
	nowPlaying *nowPlayingPanel
	queue      *queueView
	lyrics     *lyricsView
	eq         *eqPanel

	lastNotified string
	quitting     bool

	ytm        *discover.YTMusic
	paletteSeq int
	share      together
	lastFollow time.Time
	tray       *trayMenu
	splash     *splash
	// immersive hides everything but the now playing page, full screen.
	immersive  bool
	shell      *shellLayout
	bottom     fyne.CanvasObject
	restarting bool
	// windowed is the window size before full screen, restored after.
	windowed   fyne.Size
	lastMix    time.Time
	mixHistory []mixRecord
	auto       autoplay
	// stack holds the pages opened inside the current view, for Back.
	stack []fyne.CanvasObject
}

// Run opens the window and blocks until it closes. It reports whether the
// user asked for a restart.
func Run(engine Engine, opts Options) (restart bool) {
	migratePreferences()
	// Every widget call goes through runOnUI (fyne.Do), so Fyne can skip
	// its per-Refresh thread check, which captures a stack trace each time.
	app.SetMetadata(fyne.AppMetadata{ID: AppID, Name: "Kalulu", Migrations: map[string]bool{"fyneDo": true}})
	fa := app.NewWithID(AppID)
	a := newApp(fa, engine, opts)
	a.installShortcuts()
	a.installTray()
	a.win.SetOnDropped(a.onDropped)
	a.win.SetCloseIntercept(a.onClose)

	go a.pollState()
	go a.watchEvents()
	go a.watchAuthURLs()
	go a.loadProviders()
	a.autoCheckUpdates()
	a.checkTools()
	a.startRoomFromOptions()
	go func() {
		<-engine.Done()
		runOnUI(func() {
			if !a.quitting {
				a.quitting = true
				fa.Quit()
			}
		})
	}()
	go func() {
		for args := range opts.Activate {
			args := args
			runOnUI(func() {
				a.showWindow()
				if len(args) > 0 {
					a.openPaths(args)
				}
			})
		}
	}()
	fa.Lifecycle().SetOnStopped(func() { uiStopped.Store(true) })
	if opts.Background && a.trayHostAvailable() {
		a.fa.Run()
	} else {
		a.winVisible.Store(true)
		a.win.ShowAndRun()
	}
	uiStopped.Store(true)
	return a.restarting
}

// restart quits and asks the caller to start Kalulu again.
func (a *App) restart() {
	a.restarting = true
	a.quit()
}

// newApp builds the window on fa without starting the background work.
func newApp(fa fyne.App, engine Engine, opts Options) *App {
	fa.SetIcon(LogoResource)
	a := &App{
		engine: engine,
		opts:   opts,
		fa:     fa,
		prefs:  fa.Preferences(),
		poke:   make(chan struct{}, 1),
		nav:    map[string]*navItem{},
		views:  map[string]view{},
	}
	a.theme = a.makeTheme(PaletteByName(a.prefs.StringWithFallback(prefPalette, paletteDark)))
	fa.Settings().SetTheme(a.theme)
	if lang := a.prefs.String(prefLanguage); lang == "it" || lang == "en" {
		uiLang = lang
	}
	a.ytm = discover.NewYTMusic(a.country(), langCode())
	a.setDJSettings(a.djSettings())
	a.applyPlaybackPrefs()

	a.win = fa.NewWindow("Kalulu")
	a.win.SetIcon(LogoResource)
	a.win.SetMaster()
	a.build()
	a.win.Resize(fyne.NewSize(
		float32(a.prefs.FloatWithFallback(prefWinW, 1280)),
		float32(a.prefs.FloatWithFallback(prefWinH, 800)),
	))
	a.show(a.prefs.StringWithFallback(prefLastSection, "home"))
	if a.current == "" {
		a.show("home")
	}
	return a
}

// Preference keys.
const (
	prefPalette     = "palette"
	prefVisStyle    = "visualizer"
	prefWinW        = "window.width"
	prefWinH        = "window.height"
	prefRightPane   = "now_playing_panel"
	prefCloseToTray = "close_to_tray"
	prefNotify      = "notifications"
	prefNotifyMix   = "notifications.mix"
	prefNotifyErr   = "notifications.errors"
	prefLastSection = "last_view"
)

func (a *App) build() {
	resetThemed(a.theme.p)
	a.playerBar = newPlayerBar(a)
	a.queue = newQueueView(a)
	a.lyrics = newLyricsView(a)
	a.nowPlaying = newNowPlayingPanel(a)
	a.rightPane = fixed(340, 0, a.nowPlaying.Object())

	a.eq = newEQPanel(a)

	a.views["queue"] = a.queue
	a.views["home"] = newHomeView(a)
	a.views["radio"] = newRadioView(a)
	a.views["podcasts"] = newPodcastsView(a)
	a.views["search"] = newSearchView(a)
	a.views["history"] = newHistoryView(a)
	a.views["settings"] = newSettingsView(a)
	a.views["eq"] = a.eq
	a.views["dj"] = newDJView(a)
	a.views["player"] = newFullPlayer(a)
	a.views["share"] = newShareView(a)

	a.content = container.NewStack()

	a.toastText = label("")
	a.toastText.Wrapping = fyne.TextWrapWord
	a.toastText.Truncation = fyne.TextTruncateOff
	toastCard := tPanel(roleOverlay, 10, padded(4, 10, 4, 10, a.toastText))
	a.toastBox = container.NewVBox(spacer(), container.NewHBox(spacer(), fixed(420, 0, toastCard), spacer()), fixed(0, 16))
	a.toastBox.Hide()

	sidebar := a.buildSidebar()
	divider := func() fyne.CanvasObject {
		r := tRect(roleBorder)
		r.SetMinSize(fyne.NewSize(1, 1))
		return r
	}
	contentBG := tRect(roleBG)
	a.shell = &shellLayout{a: a}
	shell := container.New(a.shell,
		sidebar, divider(),
		container.NewStack(contentBG, a.content, a.toastBox),
		divider(), a.rightPane)
	a.bottom = container.NewVBox(divider(), a.playerBar.Object())
	body := container.NewBorder(nil, a.bottom, nil, nil, shell)
	if a.splash == nil {
		a.splash = newSplash(a)
	}
	if a.splash.done() {
		a.win.SetContent(body)
	} else {
		a.win.SetContent(container.NewStack(body, a.splash.root))
	}
}

// Widths at which the window drops the now playing panel and compacts the
// sidebar to icons.
const (
	wideEnoughForPanel = 1180
	wideEnoughForNav   = 900
	navWide            = 232
	navCompact         = 60
)

// shellLayout arranges sidebar, content and the now playing panel, and
// adapts them to the window width, so the window works in a tiling window
// manager tile as well as maximized.
type shellLayout struct {
	a       *App
	compact bool
}

func (l *shellLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	m := objects[2].MinSize()
	return fyne.NewSize(navCompact+1+m.Width, max(m.Height, objects[0].MinSize().Height))
}

func (l *shellLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	sidebar, div1, main, div2, right := objects[0], objects[1], objects[2], objects[3], objects[4]
	compact := size.Width < wideEnoughForNav
	if compact != l.compact {
		l.compact = compact
		l.a.setNavCompact(compact)
	}
	navW := float32(navWide)
	if compact {
		navW = navCompact
	}
	showRight := l.a.prefs.BoolWithFallback(prefRightPane, true) && size.Width >= wideEnoughForPanel && l.a.current != "player"
	if l.a.immersive {
		navW = 0
		showRight = false
		sidebar.Hide()
		div1.Hide()
	} else {
		sidebar.Show()
		div1.Show()
	}
	rightW := float32(0)
	l.a.rightShown.Store(showRight)
	if showRight {
		rightW = 340
		right.Show()
		div2.Show()
	} else {
		right.Hide()
		div2.Hide()
	}
	sidebar.Move(fyne.NewPos(0, 0))
	sidebar.Resize(fyne.NewSize(navW, size.Height))
	div1.Move(fyne.NewPos(navW, 0))
	div1.Resize(fyne.NewSize(1, size.Height))
	mainW := size.Width - navW - 1 - rightW
	if showRight {
		mainW--
	}
	main.Move(fyne.NewPos(navW+1, 0))
	main.Resize(fyne.NewSize(mainW, size.Height))
	if showRight {
		div2.Move(fyne.NewPos(navW+1+mainW, 0))
		div2.Resize(fyne.NewSize(1, size.Height))
		right.Move(fyne.NewPos(size.Width-rightW, 0))
		right.Resize(fyne.NewSize(rightW, size.Height))
	}
}

func (a *App) navCompact() bool { return a.compactNav }

// setNavCompact shows the sidebar as icons only.
func (a *App) setNavCompact(compact bool) {
	a.compactNav = compact
	for _, n := range a.nav {
		n.setCompact(compact)
	}
	for _, n := range a.navExtra {
		n.setCompact(compact)
	}
	for _, o := range a.navCompactHide {
		if compact {
			o.Hide()
		} else {
			o.Show()
		}
	}
}

// setProviders fills the Library section of the sidebar.
func (a *App) setProviders(list []ipc.ProviderInfo) {
	a.providersMu.Lock()
	a.providers = list
	a.providersMu.Unlock()
	// Drop the "Loading" label, the last object.
	objects := a.navBox.Objects
	a.navBox.Objects = objects[:len(objects)-1]
	shown := 0
	for _, p := range list {
		p := p
		// Radio and podcasts have their own pages in Discover.
		if p.Key == "radio" || p.Key == "podcast" || p.Key == "podcasts" {
			continue
		}
		key := "provider:" + p.Key
		a.views[key] = newBrowseView(a, p)
		n := newNavItem(key, providerIcon(p.Key), p.Name, func() { a.show(key) })
		a.nav[key] = n
		n.setCompact(a.compactNav)
		a.navBox.Add(n)
		shown++
	}
	a.setSidebarSources(shown)
	a.navBox.Refresh()
	a.applyNavVisibility()
	if a.splash != nil {
		a.splash.step("sources")
	}
	a.views["search"].(*searchView).setProviders(list)
	if last := a.prefs.String(prefLastSection); strings.HasPrefix(last, "provider:") && a.current == "queue" {
		if _, ok := a.views[last]; ok && a.state.Total == 0 {
			a.show(last)
		}
	}
}

func (a *App) loadProviders() {
	for attempt := 0; attempt < 20; attempt++ {
		result, err := a.call("provider.list", nil)
		if err == nil {
			list := result.Response.Providers
			runOnUI(func() { a.setProviders(list) })
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	a.toastError(fmt.Errorf("could not list the music sources"))
}

// Providers returns the provider list.
func (a *App) Providers() []ipc.ProviderInfo {
	a.providersMu.Lock()
	defer a.providersMu.Unlock()
	return append([]ipc.ProviderInfo(nil), a.providers...)
}

// show switches the main area to the view key.
func (a *App) show(key string) {
	v, ok := a.views[key]
	if !ok {
		return
	}
	if old, ok := a.nav[a.current]; ok {
		old.setSelected(false)
	}
	if prev, ok := a.views[a.current].(interface{ Hidden() }); ok && a.current != key {
		prev.Hidden()
	}
	a.current = key
	a.stack = nil
	if n, ok := a.nav[key]; ok {
		n.setSelected(true)
	}
	a.content.Objects = []fyne.CanvasObject{v.Object()}
	a.content.Refresh()
	if a.win.Content() != nil {
		// The shell hides the side panel on some pages.
		a.win.Content().Refresh()
	}
	v.Shown()
	if key != "settings" && key != "player" {
		a.prefs.SetString(prefLastSection, key)
	}
}

// push opens a page inside the current view; back returns from it.
func (a *App) push(page fyne.CanvasObject) {
	a.stack = append(a.stack, page)
	a.content.Objects = []fyne.CanvasObject{page}
	a.content.Refresh()
}

func (a *App) back() {
	if len(a.stack) == 0 {
		return
	}
	a.stack = a.stack[:len(a.stack)-1]
	if n := len(a.stack); n > 0 {
		a.content.Objects = []fyne.CanvasObject{a.stack[n-1]}
	} else if v, ok := a.views[a.current]; ok {
		a.content.Objects = []fyne.CanvasObject{v.Object()}
	}
	a.content.Refresh()
}

// onState registers fn to run on the UI thread with every snapshot.
func (a *App) onState(fn func(ipc.RuntimeSnapshot)) {
	a.listeners = append(a.listeners, fn)
}

// refreshState asks the poller for a snapshot now.
func (a *App) refreshState() {
	select {
	case a.poke <- struct{}{}:
	default:
	}
}

func (a *App) pollState() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	for {
		select {
		case <-a.engine.Done():
			return
		case <-ticker.C:
			// Hidden in the tray, a snapshot a second is plenty.
			tick++
			if !a.winVisible.Load() && tick%4 != 0 {
				continue
			}
		case <-a.poke:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		snapshot, err := a.engine.State(ctx)
		cancel()
		if err != nil {
			continue
		}
		runOnUI(func() { a.applyState(snapshot) })
	}
}

func (a *App) applyState(s ipc.RuntimeSnapshot) {
	prev := a.state
	a.state = s
	a.playing.Store(s.State == "playing")
	if a.splash != nil {
		a.splash.step("engine")
	}
	for _, fn := range a.listeners {
		fn(s)
	}
	a.checkAutoplay(s)
	a.prefetchUpNext(s)
	a.djTick(s)
	a.publishShare(s)
	if time.Since(a.lastFollow) > time.Second {
		a.lastFollow = time.Now()
		a.followRoom(s)
	}
	a.updateTitle(prev, s)
}

func (a *App) updateTitle(prev, s ipc.RuntimeSnapshot) {
	title := "Kalulu"
	key := ""
	if s.Track != nil {
		name, sub := trackTitle(*s.Track)
		key = s.Track.Path + "|" + name
		if sub != "" {
			title = name + " — " + sub + " · Kalulu"
		} else {
			title = name + " · Kalulu"
		}
	}
	if a.win.Title() != title {
		a.win.SetTitle(title)
	}
	if key != "" && key != a.lastNotified && s.State == "playing" {
		first := a.lastNotified == ""
		a.lastNotified = key
		if !first && a.prefs.BoolWithFallback(prefNotify, false) && s.Track != nil {
			name, sub := trackTitle(*s.Track)
			a.fa.SendNotification(fyne.NewNotification(name, sub))
		}
	}
}

// watchEvents refreshes the snapshot as soon as the engine publishes one.
func (a *App) watchEvents() {
	for {
		sub, err := a.engine.Subscribe([]string{"runtime.state", "runtime.job"})
		if err != nil {
			return
		}
		for range sub.Events() {
			a.refreshState()
		}
		sub.Close()
		select {
		case <-a.engine.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (a *App) watchAuthURLs() {
	if a.opts.AuthURLs == nil {
		return
	}
	for auth := range a.opts.AuthURLs {
		auth := auth
		runOnUI(func() { a.showAuthURL(auth) })
	}
}

func (a *App) showAuthURL(auth AuthURL) {
	u, err := url.Parse(auth.URL)
	link := widget.NewLabel(auth.URL)
	link.Wrapping = fyne.TextWrapBreak
	link.Selectable = true
	content := container.NewVBox(
		widget.NewLabel(auth.Provider+" needs you to sign in. A browser window should open; if it did not, open this address:"),
		link,
	)
	d := dialog.NewCustomConfirm("Sign in to "+auth.Provider, "Open browser", "Close", content, func(ok bool) {
		if ok && err == nil {
			_ = a.fa.OpenURL(u)
		}
	}, a.win)
	d.Resize(fyne.NewSize(560, 240))
	d.Show()
}

// toast shows a short message over the content.
func (a *App) toast(msg string) {
	runOnUI(func() {
		a.toastSeq++
		seq := a.toastSeq
		a.toastText.SetText(msg)
		a.toastBox.Show()
		go func() {
			time.Sleep(3500 * time.Millisecond)
			runOnUI(func() {
				if a.toastSeq == seq {
					a.toastBox.Hide()
				}
			})
		}()
	})
}

// notify sends a desktop notification when the event kind is enabled.
func (a *App) notify(pref, title, body string) {
	if !a.prefs.BoolWithFallback(pref, false) {
		return
	}
	// The same message within a minute is a duplicate: a retried error or a
	// track announced twice.
	key := title + "\x00" + body
	now := time.Now()
	a.notifyMu.Lock()
	dup := key == a.lastNotify && now.Sub(a.lastNotifyAt) < time.Minute
	a.lastNotify, a.lastNotifyAt = key, now
	a.notifyMu.Unlock()
	if !dup {
		a.fa.SendNotification(fyne.NewNotification(title, body))
	}
}

func (a *App) toastError(err error) {
	if err == nil {
		return
	}
	if !a.winVisible.Load() {
		a.notify(prefNotifyErr, "Kalulu", err.Error())
	}
	msg := err.Error()
	if len(msg) > 0 {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	a.toast("⚠ " + msg)
}

// toggleRightPane shows or hides the now playing panel.
func (a *App) toggleRightPane() {
	on := !a.prefs.BoolWithFallback(prefRightPane, true)
	a.prefs.SetBool(prefRightPane, on)
	if on && a.win.Canvas().Size().Width < wideEnoughForPanel {
		a.toast("The window is too narrow for the now playing panel")
	}
	a.win.Content().Refresh()
}

// setPalette switches the color palette of the whole window.
func (a *App) setPalette(name string) {
	a.prefs.SetString(prefPalette, name)
	a.theme = a.makeTheme(PaletteByName(name))
	a.fa.Settings().SetTheme(a.theme)
	// Surfaces that hold a palette color directly are rebuilt.
	if a.nowPlaying != nil {
		a.nowPlaying.vis.Close()
	}
	current := a.current
	a.views = map[string]view{}
	a.nav = map[string]*navItem{}
	a.listeners = nil
	a.build()
	providers := a.Providers()
	if len(providers) > 0 {
		a.setProviders(providers)
	}
	a.show(current)
	a.applyState(a.state)
	a.followCover()
}

func (a *App) onClose() {
	a.prefs.SetFloat(prefWinW, float64(a.win.Canvas().Size().Width))
	a.prefs.SetFloat(prefWinH, float64(a.win.Canvas().Size().Height))
	if a.prefs.BoolWithFallback(prefCloseToTray, a.trayHostAvailable()) && a.hasTray() && a.trayHostAvailable() {
		a.win.Hide()
		a.winVisible.Store(false)
		// In the tray only the player runs: drop the decoded covers and
		// give the freed memory back to the system.
		go func() {
			time.Sleep(2 * time.Second)
			dropArtCache()
			debug.FreeOSMemory()
		}()
		return
	}
	a.quit()
}

func (a *App) quit() {
	if a.quitting {
		return
	}
	a.share.mu.Lock()
	if a.share.server != nil {
		if a.share.announce != nil {
			a.share.announce()
		}
		a.share.server.Stop()
	}
	a.share.mu.Unlock()
	a.leaveRoom()
	a.quitting = true
	a.prefs.SetFloat(prefWinW, float64(a.win.Canvas().Size().Width))
	a.prefs.SetFloat(prefWinH, float64(a.win.Canvas().Size().Height))
	a.engine.Quit()
	a.fa.Quit()
}

func (a *App) hasTray() bool {
	_, ok := a.fa.(desktop.App)
	return ok
}

// appMenu is the hamburger menu of the sidebar.
func (a *App) appMenu() *fyne.Menu {
	themes := fyne.NewMenuItem(T("Theme"), nil)
	var items []*fyne.MenuItem
	for _, name := range PaletteNames() {
		name := name
		it := fyne.NewMenuItem(name, func() { a.setPalette(name) })
		it.Checked = name == a.theme.p.Name
		items = append(items, it)
	}
	themes.ChildMenu = fyne.NewMenu("", items...)

	vis := fyne.NewMenuItem(T("Visualizer"), nil)
	var visItems []*fyne.MenuItem
	for _, style := range visStyles {
		style := style
		it := fyne.NewMenuItem(style, func() { a.nowPlaying.vis.SetStyle(style) })
		it.Checked = a.nowPlaying.vis.Style() == style
		visItems = append(visItems, it)
	}
	vis.ChildMenu = fyne.NewMenu("", visItems...)

	return fyne.NewMenu("",
		fyne.NewMenuItem(T("Open files…"), a.openFiles),
		fyne.NewMenuItem(T("Open folder…"), a.openFolder),
		fyne.NewMenuItem(T("Open URL / stream…"), a.openURL),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(T("Save queue as playlist…"), a.queue.saveAsPlaylist),
		fyne.NewMenuItem(T("Download current track"), a.saveCurrent),
		fyne.NewMenuItemSeparator(),
		themes,
		vis,
		fyne.NewMenuItem(T("Toggle now playing panel"), a.toggleRightPane),
		fyne.NewMenuItem(T("Full screen"), func() { a.win.SetFullScreen(!a.win.FullScreen()) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(T("Keyboard shortcuts"), a.showShortcuts),
		fyne.NewMenuItem(T("About Kalulu"), a.showAbout),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(T("Quit"), a.quit),
	)
}

// Playback verbs shared by the buttons, the shortcuts and the tray.

func (a *App) togglePlay() {
	if a.state.State == "playing" && a.guestControl("pause", 0) {
		return
	}
	if a.state.State != "playing" && a.guestControl("play", 0) {
		return
	}
	if a.state.State == "stopped" && a.state.Total > 0 {
		a.do("play", nil, nil)
		return
	}
	a.do("toggle", nil, nil)
}

func (a *App) next() {
	if !a.guestControl("next", 0) {
		a.do("next", nil, nil)
	}
}

func (a *App) prev() {
	if !a.guestControl("prev", 0) {
		a.do("prev", nil, nil)
	}
}

func (a *App) seekBy(secs float64) {
	if !a.state.Seekable {
		return
	}
	a.do("seek", P{"value": secs}, nil)
}

func (a *App) seekTo(secs float64) {
	if !a.state.Seekable || a.guestControl("seek", secs) {
		return
	}
	a.do("seek.absolute", P{"value": secs}, nil)
}

func (a *App) setVolume(db float64) {
	a.do("volume", P{"value": db}, nil)
}

func (a *App) adjustVolume(delta float64) {
	a.do("volume.adjust", P{"value": delta}, nil)
}

func (a *App) toggleShuffle() {
	a.do("shuffle", P{"name": "toggle"}, func(Result) {
		if a.state.Shuffle != nil && *a.state.Shuffle {
			a.toast(T("Shuffle on"))
		} else {
			a.toast(T("Shuffle off"))
		}
	})
}

func (a *App) cycleRepeat() {
	a.do("repeat", P{"name": "cycle"}, func(Result) {
		a.toast("Repeat: " + a.state.Repeat)
	})
}

func (a *App) toggleMono() {
	a.do("mono", P{"name": "toggle"}, func(Result) {
		if a.state.Mono != nil && *a.state.Mono {
			a.toast(T("Mono output"))
		} else {
			a.toast(T("Stereo output"))
		}
	})
}

func (a *App) setSpeed(ratio float64) {
	a.do("speed", P{"value": ratio}, nil)
}

func (a *App) saveCurrent() {
	a.toast(T("Downloading the current track…"))
	a.do("save", nil, func(r Result) {
		if r.Response.Output != "" {
			a.toast("Saved to " + r.Response.Output)
		}
	})
}

// addTracks appends tracks to the queue; play starts the first of them.
func (a *App) addTracks(tracks []ipc.TrackInfo, play bool) {
	if len(tracks) == 0 {
		return
	}
	a.do("tracks.add", P{"tracks": tracks, "play": play}, func(Result) {
		if len(tracks) == 1 {
			title, _ := trackTitle(tracks[0])
			a.toast("Added " + title)
		} else {
			a.toast(fmt.Sprintf(T("Added %d tracks"), len(tracks)))
		}
	})
}

// playTrack plays a provider or search track at once.
func (a *App) playTrack(t ipc.TrackInfo) {
	a.do("track.play", P{"track": t}, nil)
}

// queueNext puts a track at the head of play-next.
func (a *App) queueNext(t ipc.TrackInfo) {
	a.do("track.queue", P{"track": t}, func(Result) {
		title, _ := trackTitle(t)
		a.toast("Playing next: " + title)
	})
}

// trackMenu is the context menu of a track that is not in the queue.
func (a *App) trackMenu(t ipc.TrackInfo) []*fyne.MenuItem {
	items := []*fyne.MenuItem{
		fyne.NewMenuItemWithIcon(T("Play now"), theme.MediaPlayIcon(), func() { a.playTrack(t) }),
		fyne.NewMenuItemWithIcon(T("Play next"), iconPlayNext, func() { a.queueNext(t) }),
		fyne.NewMenuItemWithIcon(T("Add to queue"), theme.ContentAddIcon(), func() { a.addTracks([]ipc.TrackInfo{t}, false) }),
		fyne.NewMenuItemWithIcon(T("Start radio"), iconRadio, func() { a.startRadio(t) }),
		a.addToPlaylistItem([]ipc.TrackInfo{t}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItemWithIcon(T("Track info"), theme.InfoIcon(), func() { a.showTrackInfo(t) }),
	}
	if it := a.roomMenuItem(t); it != nil {
		items = append(items, fyne.NewMenuItemSeparator(), it)
	}
	return items
}

// addToPlaylistItem is a submenu that adds tracks to a saved playlist of the
// local provider, or to a new one.
func (a *App) addToPlaylistItem(tracks []ipc.TrackInfo) *fyne.MenuItem {
	item := fyne.NewMenuItemWithIcon(T("Add to playlist"), iconPlaylistAdd, nil)
	children := []*fyne.MenuItem{
		fyne.NewMenuItem(T("New playlist…"), func() { a.newPlaylistWith(tracks) }),
		fyne.NewMenuItemSeparator(),
	}
	for _, name := range a.localPlaylistNames() {
		name := name
		children = append(children, fyne.NewMenuItem(name, func() { a.addToPlaylist(name, tracks) }))
	}
	item.ChildMenu = fyne.NewMenu("", children...)
	return item
}

// localPlaylists caches the saved playlist names for the menus.
var (
	localPlaylistsMu    sync.Mutex
	localPlaylistsCache []string
)

func (a *App) localPlaylistNames() []string {
	localPlaylistsMu.Lock()
	names := append([]string(nil), localPlaylistsCache...)
	localPlaylistsMu.Unlock()
	go a.reloadLocalPlaylists()
	return names
}

func (a *App) reloadLocalPlaylists() {
	result, err := a.call("provider.playlists", P{"provider": "local", "limit": 1000})
	if err != nil {
		return
	}
	var names []string
	for _, p := range result.Response.Playlists {
		if p.Section == "" || strings.EqualFold(p.Section, "playlists") {
			names = append(names, p.Name)
		}
	}
	sort.Strings(names)
	localPlaylistsMu.Lock()
	localPlaylistsCache = names
	localPlaylistsMu.Unlock()
}

func (a *App) addToPlaylist(name string, tracks []ipc.TrackInfo) {
	a.do("playlist.add_many", P{"provider": "local", "playlist": name, "tracks": tracks}, func(r Result) {
		a.toast(fmt.Sprintf("Added %d track(s) to %s", r.Response.Total, name))
		go a.reloadLocalPlaylists()
	})
}

func (a *App) newPlaylistWith(tracks []ipc.TrackInfo) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Playlist name")
	dialog.ShowForm("New playlist", "Create", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Name", entry)},
		func(ok bool) {
			name := strings.TrimSpace(entry.Text)
			if !ok || name == "" {
				return
			}
			go func() {
				if _, err := a.call("playlist.create", P{"provider": "local", "playlist": name}); err != nil && !strings.Contains(err.Error(), "exists") {
					a.toastError(err)
					return
				}
				if len(tracks) > 0 {
					a.addToPlaylist(name, tracks)
				} else {
					a.toast("Created " + name)
					a.reloadLocalPlaylists()
				}
			}()
		}, a.win)
	a.win.Canvas().Focus(entry)
}

// migratePreferences copies the GuiAmp preferences to Kalulu once, so the
// rename keeps the theme, the window size and every other choice.
func migratePreferences() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	newFile := filepath.Join(dir, "fyne", AppID, "preferences.json")
	if _, err := os.Stat(newFile); err == nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "fyne", legacyAppID, "preferences.json"))
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(newFile), 0o755) == nil {
		_ = os.WriteFile(newFile, data, 0o600)
	}
}

// setImmersive switches the full screen now playing mode: only the page of
// the track, with its cover, lyrics and controls.
func (a *App) setImmersive(on bool) {
	if a.immersive == on {
		return
	}
	a.immersive = on
	if f, ok := a.views["player"].(*fullPlayer); ok {
		f.exitFull.Hidden = !on
		f.exitFull.Refresh()
	}
	if on {
		a.windowed = a.win.Canvas().Size()
		a.bottom.Hide()
		a.win.SetFullScreen(true)
		a.win.Content().Refresh()
		return
	}
	a.bottom.Show()
	a.win.SetFullScreen(false)
	// The window manager gives the window back its size a moment later;
	// until then the canvas kept the full screen size and the edges were
	// cut. Resize to the old size and lay out again once it settles.
	size := a.windowed
	go func() {
		for _, wait := range []time.Duration{60, 250, 600} {
			time.Sleep(wait * time.Millisecond)
			runOnUI(func() {
				if a.immersive {
					return
				}
				if size.Width > 0 && size.Height > 0 {
					a.win.Resize(size)
				}
				a.win.Content().Refresh()
			})
		}
	}()
	a.win.Content().Refresh()
}
