package gui

import (
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/share"
)

const (
	prefShareName = "share.name"
	prefSharePort = "share.port"
)

// together is the state of shared listening.
type together struct {
	mu       sync.Mutex
	server   *share.Server
	announce context.CancelFunc
	client   *share.Client
	guest    share.State // the last state of the room, for a guest
	members  []share.Member
	lastPub  string
	lastSent time.Time
	drift    float64
	speedSet bool
	status   string
}

func shareTrack(t *ipc.TrackInfo) *share.Track {
	if t == nil {
		return nil
	}
	title, sub := trackTitle(*t)
	return &share.Track{Path: t.Path, Title: title, Artist: sub, Album: t.Album, Art: t.AlbumArtURL,
		Duration: t.DurationSecs, Stream: t.Stream, Live: t.Realtime}
}

// publishShare sends the Owner's playback to the room when it changes, and
// every few seconds so late joiners and drifting guests stay aligned.
func (a *App) publishShare(s ipc.RuntimeSnapshot) {
	a.share.mu.Lock()
	srv := a.share.server
	a.share.mu.Unlock()
	if srv == nil {
		return
	}
	tr := shareTrack(s.Track)
	if tr != nil && isLocalPath(tr.Path) {
		tr.Path = "media:" + srv.ShareLocal(tr.Path)
		tr.Art = ""
	}
	key := ""
	if tr != nil {
		key = tr.Path
	}
	key += fmt.Sprintf("|%v", s.State == "playing")
	a.share.mu.Lock()
	changed := key != a.share.lastPub || time.Since(a.share.lastSent) > 5*time.Second
	if changed {
		a.share.lastPub = key
		a.share.lastSent = time.Now()
	}
	a.share.mu.Unlock()
	if changed {
		srv.SetState(share.State{Track: tr, Playing: s.State == "playing", Position: s.Position})
	}
}

// followRoom aligns a guest's player with the room.
func (a *App) followRoom(s ipc.RuntimeSnapshot) {
	a.share.mu.Lock()
	c := a.share.client
	st := a.share.guest
	a.share.mu.Unlock()
	if c == nil || st.Track == nil {
		return
	}
	path := st.Track.Path
	if id, ok := strings.CutPrefix(path, "media:"); ok {
		path = c.MediaURL(id)
	}
	expected := c.Expected(st)
	if s.Track == nil || s.Track.Path != path {
		t := ipc.TrackInfo{Path: path, Title: st.Track.Title, Artist: st.Track.Artist, Album: st.Track.Album,
			AlbumArtURL: st.Track.Art, DurationSecs: st.Track.Duration, Stream: true, Realtime: st.Track.Live}
		a.share.mu.Lock()
		a.share.status = T("Loading the room's track…")
		a.share.mu.Unlock()
		a.playTrackLocal(t)
		return
	}
	switch {
	case st.Playing && s.State != "playing":
		a.do("play", nil, nil)
	case !st.Playing && s.State == "playing":
		a.do("pause", nil, nil)
	}
	if st.Track.Live || !s.Seekable {
		return
	}
	drift := s.Position - expected
	a.share.mu.Lock()
	a.share.drift = drift
	a.share.status = fmt.Sprintf(T("In sync (%+.0f ms)"), drift*1000)
	speedSet := a.share.speedSet
	a.share.mu.Unlock()
	switch {
	case math.Abs(drift) > 1.5:
		// Far off, after a pause or a buffering stall: jump.
		a.do("seek.absolute", P{"value": expected + 0.2}, nil)
	case math.Abs(drift) > 0.15 && st.Playing:
		// Close: nudge the speed by up to 3 % so the correction is not heard.
		speed := 1 - math.Max(-0.03, math.Min(0.03, drift*0.05))
		a.do("speed", P{"value": speed}, nil)
		a.share.mu.Lock()
		a.share.speedSet = true
		a.share.mu.Unlock()
	case speedSet && math.Abs(drift) < 0.06:
		a.do("speed", P{"value": 1.0}, nil)
		a.share.mu.Lock()
		a.share.speedSet = false
		a.share.mu.Unlock()
	}
}

// playTrackLocal plays a track on this computer only.
func (a *App) playTrackLocal(t ipc.TrackInfo) {
	a.do("track.play", P{"track": t}, nil)
}

// guestControl sends a transport command to the room. It reports whether
// the command was for the room, so the caller skips the local action.
func (a *App) guestControl(op string, position float64) bool {
	a.share.mu.Lock()
	c := a.share.client
	a.share.mu.Unlock()
	if c == nil {
		return false
	}
	if c.Role != share.RoleEditor {
		a.toast(T("Only the owner and the editors control the room"))
		return true
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Send(ctx, share.Command{Op: op, Position: position}); err != nil {
			a.toastError(err)
		}
	}()
	return true
}

// roomMenuItem lets a guest request or queue a track in the room.
func (a *App) roomMenuItem(t ipc.TrackInfo) *fyne.MenuItem {
	a.share.mu.Lock()
	c := a.share.client
	srv := a.share.server
	a.share.mu.Unlock()
	if c == nil && srv == nil {
		return nil
	}
	if !strings.HasPrefix(t.Path, "http") {
		return nil
	}
	label := T("Add to the room's queue")
	if c != nil && c.Role == share.RoleRequester {
		label = T("Ask the owner to play it")
	}
	if c != nil && c.Role == share.RoleListener {
		return nil
	}
	return fyne.NewMenuItemWithIcon(label, iconGroup, func() {
		tr := shareTrack(&t)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var err error
			if c != nil {
				err = c.Send(ctx, share.Command{Op: "request", Track: tr})
			} else {
				err = srv.Run(share.Command{Op: "queue", Track: tr})
			}
			if err != nil {
				a.toastError(err)
			} else {
				a.toast(T("Sent to the room"))
			}
		}()
	})
}

// shareControl runs a command of an authorized guest on the Owner's player.
func (a *App) shareControl(cmd share.Command) error {
	runOnUI(func() {
		switch cmd.Op {
		case "play":
			a.do("play", nil, nil)
		case "pause":
			a.do("pause", nil, nil)
		case "next":
			a.do("next", nil, nil)
		case "prev":
			a.do("prev", nil, nil)
		case "seek":
			a.do("seek.absolute", P{"value": cmd.Position}, nil)
		case "queue":
			if cmd.Track != nil {
				t := ipc.TrackInfo{Path: cmd.Track.Path, Title: cmd.Track.Title, Artist: cmd.Track.Artist,
					Album: cmd.Track.Album, AlbumArtURL: cmd.Track.Art, DurationSecs: cmd.Track.Duration, Stream: true}
				a.do("track.queue", P{"track": t}, func(Result) { a.toast(T("A guest added: ") + cmd.Track.Title) })
			}
		}
	})
	return nil
}

func defaultUserName() string {
	if u, err := user.Current(); err == nil {
		if name := strings.TrimSpace(strings.Split(u.Name, ",")[0]); name != "" {
			return name
		}
		return u.Username
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "Kalulu"
}

// shareView is the Together page.
type shareView struct {
	app  *App
	root *fyne.Container
}

func newShareView(a *App) *shareView {
	v := &shareView{app: a, root: container.NewStack()}
	v.render()
	return v
}

func (v *shareView) Object() fyne.CanvasObject { return v.root }
func (v *shareView) Shown()                    { v.render() }

func (v *shareView) render() {
	a := v.app
	a.share.mu.Lock()
	srv, c := a.share.server, a.share.client
	a.share.mu.Unlock()
	var body fyne.CanvasObject
	switch {
	case srv != nil:
		body = v.ownerPage(srv)
	case c != nil:
		body = v.guestPage(c)
	default:
		body = v.startPage()
	}
	header := pageHeader(heading(T("Listen together")), wrapLabel(T("Share what you play with people on your local network, in sync.")))
	v.root.Objects = []fyne.CanvasObject{container.NewBorder(header, nil, nil, nil, scrollPage(body))}
	v.root.Refresh()
}

func (v *shareView) startPage() fyne.CanvasObject {
	a := v.app
	name := widget.NewEntry()
	name.SetText(a.prefs.StringWithFallback(prefShareName, defaultUserName()))
	room := widget.NewEntry()
	room.SetText(fmt.Sprintf(T("%s's room"), name.Text))
	port := widget.NewEntry()
	port.SetText(strconv.Itoa(a.prefs.IntWithFallback(prefSharePort, share.DefaultPort)))
	create := primaryButton(T("Create the room"), theme.ContentAddIcon(), func() {
		p, err := strconv.Atoi(strings.TrimSpace(port.Text))
		if err != nil || p < 1024 || p > 65535 {
			a.toastError(fmt.Errorf("%s", T("The port must be between 1024 and 65535")))
			return
		}
		a.prefs.SetString(prefShareName, name.Text)
		a.prefs.SetInt(prefSharePort, p)
		srv := share.NewServer(strings.TrimSpace(room.Text), strings.TrimSpace(name.Text), share.Hooks{
			Control: a.shareControl,
			Requested: func(r share.Request) {
				a.toast(fmt.Sprintf(T("%s asks to play %s"), r.From, r.Track.Title))
				a.notify(prefNotify, "Kalulu", fmt.Sprintf(T("%s asks to play %s"), r.From, r.Track.Title))
			},
			Changed: func() { runOnUI(v.renderIfShown) },
		})
		if err := srv.Start(fmt.Sprintf(":%d", p)); err != nil {
			a.toastError(err)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		if err := srv.Announce(ctx); err != nil {
			a.toast(T("Discovery is unavailable; guests can join with IP and port"))
		}
		a.share.mu.Lock()
		a.share.server, a.share.announce = srv, cancel
		a.share.lastPub = ""
		a.share.mu.Unlock()
		a.publishShare(a.state)
		v.render()
	})
	ownerCard := settingsCard(T("Create a room"),
		wrapLabel(T("You become the Owner: guests hear what you play and you decide what they may do.")),
		setRow(T("Your name"), "", name), setRow(T("Room name"), "", room),
		setRow(T("Port"), T("TCP, and the next port in UDP for discovery"), port),
		container.NewHBox(create))

	code := widget.NewEntry()
	code.SetPlaceHolder("ABC123")
	guestName := widget.NewEntry()
	guestName.SetText(name.Text)
	host := widget.NewEntry()
	host.SetPlaceHolder("192.168.1.20")
	hostPort := widget.NewEntry()
	hostPort.SetText(strconv.Itoa(share.DefaultPort))
	status := captionLabel("")
	join := func(useIP bool) {
		cd := strings.ToUpper(strings.TrimSpace(code.Text))
		if len(cd) != 6 {
			status.SetText(T("The room code has 6 characters"))
			return
		}
		status.SetText(T("Looking for the room…"))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			h, p := strings.TrimSpace(host.Text), share.DefaultPort
			if useIP {
				n, err := strconv.Atoi(strings.TrimSpace(hostPort.Text))
				if err != nil || h == "" {
					runOnUI(func() { status.SetText(T("Enter the IP address and the port of the owner")) })
					return
				}
				p = n
			} else {
				room, err := share.FindRoom(ctx, cd)
				if err != nil {
					runOnUI(func() { status.SetText(err.Error()) })
					return
				}
				h, p = room.Host, room.Port
			}
			c, err := share.Join(ctx, h, p, cd, strings.TrimSpace(guestName.Text), "")
			if err != nil {
				runOnUI(func() { status.SetText(err.Error()) })
				return
			}
			runOnUI(func() { a.enterRoom(c); v.render() })
		}()
	}
	byCode := container.NewVBox(setRow(T("Room code"), "", code), setRow(T("Your name"), "", guestName),
		container.NewHBox(primaryButton(T("Find and join"), theme.SearchIcon(), func() { join(false) })))
	byIP := container.NewVBox(setRow(T("Owner IP address"), "", host), setRow(T("Port"), "", hostPort),
		wrapLabel(T("Enter the room code above too: it is the password of the room.")),
		container.NewHBox(flatButton(T("Connect"), theme.ComputerIcon(), func() { join(true) })))
	guestCard := settingsCard(T("Join a room"), byCode, widget.NewSeparator(), boldLabel(T("Or connect directly")), byIP, status)
	note := wrapLabel(T("The room works on your local network only and is never exposed to the Internet. Everyone plays the same source on their own computer, so online tracks (YouTube, radio, podcasts) need each guest to reach them; your local files are streamed to the guests by Kalulu. If guests cannot find you, allow TCP and UDP on the room ports in your firewall."))
	return container.NewVBox(container.NewGridWithColumns(2, ownerCard, guestCard), note)
}

func (v *shareView) renderIfShown() {
	if v.app.current == "share" {
		v.render()
	}
}

func roleLabel(role string) string {
	return map[string]string{
		share.RoleOwner: T("Owner"), share.RoleEditor: T("Editor"),
		share.RoleRequester: T("On request"), share.RoleListener: T("Listener"),
	}[role]
}

func (v *shareView) ownerPage(srv *share.Server) fyne.CanvasObject {
	a := v.app
	code := label(srv.Code)
	code.SizeName = theme.SizeNameHeadingText
	code.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	var addrs []string
	for _, ip := range share.LANAddresses() {
		addrs = append(addrs, fmt.Sprintf("%s:%d", ip, srv.Port()))
	}
	if len(addrs) == 0 {
		addrs = []string{fmt.Sprintf("localhost:%d", srv.Port())}
	}
	stop := flatButton(T("Close the room"), theme.CancelIcon(), func() {
		a.share.mu.Lock()
		if a.share.announce != nil {
			a.share.announce()
		}
		a.share.server.Stop()
		a.share.server = nil
		a.share.mu.Unlock()
		v.render()
	})
	info := settingsCard(srv.Room,
		setRow(T("Room code"), T("Guests type it to find the room"), code),
		setRow(T("Address"), T("For a direct connection"), wrapLabel(strings.Join(addrs, "  ·  "))),
		container.NewHBox(stop))
	roles := []string{T("Editor"), T("On request"), T("Listener")}
	roleValue := map[string]string{T("Editor"): share.RoleEditor, T("On request"): share.RoleRequester, T("Listener"): share.RoleListener}
	people := container.NewVBox()
	for _, m := range srv.Members() {
		m := m
		dot := "○"
		if m.Online {
			dot = "●"
		}
		name := label(dot + " " + m.Name)
		if m.Role == share.RoleOwner {
			people.Add(container.NewBorder(nil, nil, nil, dimLabel(roleLabel(m.Role)), name))
			continue
		}
		sel := widget.NewSelect(roles, func(r string) {
			if err := srv.Run(share.Command{Op: "role", Member: m.ID, Role: roleValue[r]}); err != nil {
				a.toastError(err)
			}
		})
		sel.Selected = roleLabel(m.Role)
		kick := iconButton(theme.ContentClearIcon(), func() { _ = srv.Run(share.Command{Op: "kick", Member: m.ID}) })
		people.Add(container.NewBorder(nil, nil, nil, container.NewHBox(fixed(150, 0, sel), kick), name))
	}
	requests := container.NewVBox()
	for _, r := range srv.Requests() {
		r := r
		approve := flatButton(T("Play next"), theme.ConfirmIcon(), func() { _ = srv.Run(share.Command{Op: "approve", Request: r.ID}) })
		deny := iconButton(theme.CancelIcon(), func() { _ = srv.Run(share.Command{Op: "deny", Request: r.ID}) })
		requests.Add(container.NewBorder(nil, nil, nil, container.NewHBox(approve, deny),
			trunc(label(fmt.Sprintf("%s — %s", r.From, r.Track.Title)))))
	}
	if len(srv.Requests()) == 0 {
		requests.Add(dimLabel(T("No pending requests")))
	}
	roleHelp := wrapLabel(T("Editor: controls playback and the queue · On request: asks for tracks, you approve · Listener: listens only."))
	return container.NewVBox(info,
		settingsCard(T("People"), roleHelp, people),
		settingsCard(T("Requests"), requests))
}

func (v *shareView) guestPage(c *share.Client) fyne.CanvasObject {
	a := v.app
	a.share.mu.Lock()
	status := a.share.status
	members := a.share.members
	a.share.mu.Unlock()
	leave := flatButton(T("Leave the room"), theme.LogoutIcon(), func() {
		a.leaveRoom()
		v.render()
	})
	people := container.NewVBox()
	for _, m := range members {
		dot := "○"
		if m.Online {
			dot = "●"
		}
		people.Add(container.NewBorder(nil, nil, nil, dimLabel(roleLabel(m.Role)), label(dot+" "+m.Name)))
	}
	return container.NewVBox(
		settingsCard(c.Room,
			setRow(T("Owner"), "", label(c.Owner)),
			setRow(T("Your role"), "", label(roleLabel(c.Role))),
			setRow(T("Sync"), "", label(status)),
			container.NewHBox(leave)),
		settingsCard(T("People"), people))
}

// enterRoom starts following a room as a guest.
func (a *App) enterRoom(c *share.Client) {
	a.share.mu.Lock()
	a.share.client = c
	a.share.status = T("Connected")
	a.share.mu.Unlock()
	c.Listen(func(ev share.Event) {
		switch ev.Type {
		case "state":
			if ev.State != nil {
				a.share.mu.Lock()
				a.share.guest = *ev.State
				a.share.mu.Unlock()
				runOnUI(func() { a.followRoom(a.state) })
			}
		case "members":
			a.share.mu.Lock()
			a.share.members = ev.Members
			for _, m := range ev.Members {
				if m.ID == c.ID {
					c.Role = m.Role
				}
			}
			a.share.mu.Unlock()
			runOnUI(func() {
				if v, ok := a.views["share"].(*shareView); ok {
					v.renderIfShown()
				}
			})
		case "offline":
			a.share.mu.Lock()
			a.share.status = T("Reconnecting…")
			a.share.mu.Unlock()
		case "bye":
			a.toast(T("You left the room: ") + ev.Message)
			runOnUI(func() {
				a.leaveRoom()
				if v, ok := a.views["share"].(*shareView); ok {
					v.render()
				}
			})
		}
	})
}

func (a *App) leaveRoom() {
	a.share.mu.Lock()
	c := a.share.client
	a.share.client = nil
	speed := a.share.speedSet
	a.share.speedSet = false
	a.share.mu.Unlock()
	if c != nil {
		c.Close()
	}
	if speed {
		a.do("speed", P{"value": 1.0}, nil)
	}
}

// startRoomFromOptions creates or joins a room from the command line.
func (a *App) startRoomFromOptions() {
	if name := strings.TrimSpace(a.opts.RoomCreate); name != "" {
		srv := share.NewServer(name, a.prefs.StringWithFallback(prefShareName, defaultUserName()), share.Hooks{
			Control: a.shareControl,
			Requested: func(r share.Request) {
				a.toast(fmt.Sprintf(T("%s asks to play %s"), r.From, r.Track.Title))
			},
			Changed: func() {
				runOnUI(func() {
					if v, ok := a.views["share"].(*shareView); ok {
						v.renderIfShown()
					}
				})
			},
		})
		if err := srv.Start(fmt.Sprintf(":%d", a.prefs.IntWithFallback(prefSharePort, share.DefaultPort))); err != nil {
			a.toastError(err)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		_ = srv.Announce(ctx)
		a.share.mu.Lock()
		a.share.server, a.share.announce = srv, cancel
		a.share.mu.Unlock()
		applogInfo("share: room %q open on port %d with code %s", name, srv.Port(), srv.Code)
		a.toast(fmt.Sprintf(T("Room open · code %s"), srv.Code))
		return
	}
	target := strings.TrimSpace(a.opts.RoomJoin)
	if target == "" {
		return
	}
	addr, code, ok := strings.Cut(target, "/")
	host, portText, err := netSplit(addr)
	port, perr := strconv.Atoi(portText)
	if !ok || err != nil || perr != nil {
		a.toastError(fmt.Errorf("--join wants host:port/CODE"))
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := share.Join(ctx, host, port, code, a.prefs.StringWithFallback(prefShareName, defaultUserName()), "")
		if err != nil {
			a.toastError(err)
			return
		}
		applogInfo("share: joined room %q as %s", c.Room, c.Role)
		runOnUI(func() { a.enterRoom(c) })
	}()
}

func netSplit(addr string) (string, string, error) { return net.SplitHostPort(addr) }

func applogInfo(format string, args ...any) { applog.Info(format, args...) }
