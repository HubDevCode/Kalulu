// Package share is Kalulu's shared listening on the local network. The
// Owner runs a small HTTP server; guests join with the room code, follow
// the Owner's playback (each plays the same source itself, so every client
// buffers on its own) and, by role, control it or request tracks.
//
// Endpoints (all but /join and /time need the member token):
//
//	POST /join      {code, name, token?}  → {token, role, room, owner}
//	GET  /time                             → server clock, for sync
//	GET  /events                           → Server-Sent Events: state, members, requests
//	POST /cmd       {op, ...}              → play, pause, seek, next, prev, queue, request, role, kick, approve, deny
//	GET  /media/{id}                       → an Owner-local file, with Range support
//
// The server listens on the LAN only: requests from public addresses are
// refused, and the room code works as the password.
package share

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Roles of a member.
const (
	RoleOwner     = "owner"
	RoleEditor    = "editor"    // controls playback and the queue
	RoleRequester = "requester" // asks for tracks; the Owner approves
	RoleListener  = "listener"  // listens only
)

// Track is a track as the room shares it.
type Track struct {
	Path     string `json:"path"`
	Title    string `json:"title,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
	Art      string `json:"art,omitempty"`
	Duration int    `json:"duration,omitempty"`
	Stream   bool   `json:"stream,omitempty"`
	Live     bool   `json:"live,omitempty"`
}

// State is the playback the guests follow.
type State struct {
	Track    *Track  `json:"track,omitempty"`
	Playing  bool    `json:"playing"`
	Position float64 `json:"position"` // seconds at At
	At       int64   `json:"at"`       // server clock, unix milliseconds
	Seq      uint64  `json:"seq"`
}

// Member is a person in the room.
type Member struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Online   bool   `json:"online"`
	Address  string `json:"address,omitempty"`
	JoinedAt int64  `json:"joined_at"`
	token    string
}

// Request is a track a guest asked for.
type Request struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	Track Track  `json:"track"`
}

// Event is one Server-Sent Event.
type Event struct {
	Type     string    `json:"type"` // state, members, requests, bye
	State    *State    `json:"state,omitempty"`
	Members  []Member  `json:"members,omitempty"`
	Requests []Request `json:"requests,omitempty"`
	Room     string    `json:"room,omitempty"`
	Message  string    `json:"message,omitempty"`
}

// Command is a control request of a member.
type Command struct {
	Op       string  `json:"op"`
	Position float64 `json:"position,omitempty"`
	Track    *Track  `json:"track,omitempty"`
	Member   string  `json:"member,omitempty"`
	Role     string  `json:"role,omitempty"`
	Request  string  `json:"request,omitempty"`
}

// Hooks connect the server to the Owner's player.
type Hooks struct {
	// Control runs a playback command from an authorized member.
	Control func(cmd Command) error
	// Requested tells the Owner that a guest asked for a track.
	Requested func(Request)
	// Changed tells the Owner that the members changed.
	Changed func()
}

// Server is the Owner side of a room.
type Server struct {
	Room  string
	Code  string
	Owner string
	hooks Hooks

	mu       sync.Mutex
	members  map[string]*Member // by token
	requests []Request
	state    State
	media    map[string]string // media id → local path
	subs     map[chan Event]string
	ln       net.Listener
	srv      *http.Server
	seq      uint64
	allowAll bool // tests only: accept loopback and public addresses
}

// NewServer creates a room. The code has 6 characters without the ones
// that look alike.
func NewServer(room, owner string, hooks Hooks) *Server {
	return &Server{
		Room: room, Owner: owner, Code: newCode(), hooks: hooks,
		members: map[string]*Member{}, media: map[string]string{}, subs: map[chan Event]string{},
	}
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	var b strings.Builder
	for range 6 {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b.WriteByte(codeAlphabet[n.Int64()])
	}
	return b.String()
}

func newToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Start listens on addr, such as ":47420".
func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /join", s.handleJoin)
	mux.HandleFunc("GET /time", s.handleTime)
	mux.HandleFunc("GET /events", s.auth(s.handleEvents))
	mux.HandleFunc("POST /cmd", s.auth(s.handleCmd))
	mux.HandleFunc("GET /media/{id}", s.auth(s.handleMedia))
	s.srv = &http.Server{Handler: s.lanOnly(mux), ReadHeaderTimeout: 10 * time.Second}
	s.ln = ln
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

// Port returns the listening port.
func (s *Server) Port() int {
	if s.ln == nil {
		return 0
	}
	return s.ln.Addr().(*net.TCPAddr).Port
}

// Stop ends the room and tells the guests.
func (s *Server) Stop() {
	s.broadcast(Event{Type: "bye", Message: "the room was closed"})
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

// isLAN accepts private, loopback and link-local addresses.
func isLAN(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func (s *Server) lanOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); !s.allowAll && (ip == nil || !isLAN(ip)) {
			http.Error(w, "LAN only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next func(http.ResponseWriter, *http.Request, *Member)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Kalulu-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		s.mu.Lock()
		m, ok := s.members[token]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "not a member", http.StatusUnauthorized)
			return
		}
		next(w, r, m)
	}
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func cleanName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, name))
	if r := []rune(name); len(r) > 32 {
		name = string(r[:32])
	}
	if name == "" {
		name = "Guest"
	}
	return name
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Code, Name, Token string }
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(req.Code), s.Code) {
		time.Sleep(300 * time.Millisecond) // slow down guessing
		http.Error(w, "wrong room code", http.StatusForbidden)
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.mu.Lock()
	m, ok := s.members[req.Token]
	if !ok {
		if len(s.members) >= 32 {
			s.mu.Unlock()
			http.Error(w, "the room is full", http.StatusConflict)
			return
		}
		m = &Member{ID: newToken()[:8], Name: cleanName(req.Name), Role: RoleListener, JoinedAt: time.Now().Unix(), token: newToken()}
		s.members[m.token] = m
	}
	m.Address = host
	s.mu.Unlock()
	s.membersChanged()
	writeJSON(w, map[string]string{"token": m.token, "role": m.Role, "id": m.ID, "room": s.Room, "owner": s.Owner})
}

func (s *Server) handleTime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]int64{"now": time.Now().UnixMilli()})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, m *Member) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan Event, 16)
	s.mu.Lock()
	s.subs[ch] = m.token
	m.Online = true
	state := s.state
	s.mu.Unlock()
	s.membersChanged()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		still := false
		for _, tok := range s.subs {
			if tok == m.token {
				still = true
			}
		}
		m.Online = still
		s.mu.Unlock()
		s.membersChanged()
	}()
	send := func(ev Event) bool {
		data, _ := json.Marshal(ev)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	send(Event{Type: "state", State: &state, Room: s.Room})
	send(Event{Type: "members", Members: s.Members()})
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if !send(ev) || ev.Type == "bye" {
				return
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// allowed reports whether role may run op.
func allowed(role, op string) bool {
	switch op {
	case "play", "pause", "seek", "next", "prev", "queue":
		return role == RoleOwner || role == RoleEditor
	case "request":
		return role == RoleOwner || role == RoleEditor || role == RoleRequester
	case "role", "kick", "approve", "deny":
		return role == RoleOwner
	}
	return false
}

func validTrack(t *Track) bool {
	if t == nil || len(t.Path) > 2048 || len(t.Title) > 512 {
		return false
	}
	return strings.HasPrefix(t.Path, "https://") || strings.HasPrefix(t.Path, "http://")
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request, m *Member) {
	var cmd Command
	if err := readJSON(r, &cmd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.run(m, cmd); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// Run executes a command as the Owner, for the Owner's own window.
func (s *Server) Run(cmd Command) error {
	return s.run(&Member{Role: RoleOwner, Name: s.Owner}, cmd)
}

func (s *Server) run(m *Member, cmd Command) error {
	if !allowed(m.Role, cmd.Op) {
		return errors.New("your role does not allow this")
	}
	switch cmd.Op {
	case "queue", "request":
		if !validTrack(cmd.Track) {
			return errors.New("only links to online tracks can be shared")
		}
	}
	switch cmd.Op {
	case "request":
		if m.Role == RoleRequester {
			req := Request{ID: newToken()[:10], From: m.Name, Track: *cmd.Track}
			s.mu.Lock()
			if len(s.requests) >= 50 {
				s.mu.Unlock()
				return errors.New("too many pending requests")
			}
			s.requests = append(s.requests, req)
			s.mu.Unlock()
			s.requestsChanged()
			if s.hooks.Requested != nil {
				s.hooks.Requested(req)
			}
			return nil
		}
		cmd.Op = "queue"
	case "role":
		switch cmd.Role {
		case RoleEditor, RoleRequester, RoleListener:
		default:
			return errors.New("unknown role")
		}
		s.mu.Lock()
		for _, mem := range s.members {
			if mem.ID == cmd.Member {
				mem.Role = cmd.Role
			}
		}
		s.mu.Unlock()
		s.membersChanged()
		return nil
	case "kick":
		s.mu.Lock()
		for tok, mem := range s.members {
			if mem.ID == cmd.Member {
				delete(s.members, tok)
				for ch, t := range s.subs {
					if t == tok {
						select {
						case ch <- Event{Type: "bye", Message: "you were removed from the room"}:
						default:
						}
					}
				}
			}
		}
		s.mu.Unlock()
		s.membersChanged()
		return nil
	case "approve", "deny":
		s.mu.Lock()
		var found *Request
		left := s.requests[:0]
		for _, req := range s.requests {
			if req.ID == cmd.Request {
				r := req
				found = &r
				continue
			}
			left = append(left, req)
		}
		s.requests = left
		s.mu.Unlock()
		s.requestsChanged()
		if found == nil || cmd.Op == "deny" {
			return nil
		}
		cmd = Command{Op: "queue", Track: &found.Track}
	}
	if s.hooks.Control == nil {
		return errors.New("the owner cannot be controlled")
	}
	return s.hooks.Control(cmd)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request, m *Member) {
	s.mu.Lock()
	path, ok := s.media[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

// ShareLocal makes an Owner-local file playable for the guests and
// returns its media id.
func (s *Server) ShareLocal(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.media {
		if p == path {
			return id
		}
	}
	id := newToken()[:12]
	s.media[id] = path
	return id
}

// SetState publishes the Owner's playback.
func (s *Server) SetState(st State) {
	s.mu.Lock()
	s.seq++
	st.Seq = s.seq
	st.At = time.Now().UnixMilli()
	s.state = st
	s.mu.Unlock()
	s.broadcast(Event{Type: "state", State: &st})
}

// Members lists the members, the Owner first.
func (s *Server) Members() []Member {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Member{{ID: "owner", Name: s.Owner, Role: RoleOwner, Online: true}}
	var guests []Member
	for _, m := range s.members {
		guests = append(guests, *m)
	}
	sort.Slice(guests, func(i, j int) bool { return guests[i].JoinedAt < guests[j].JoinedAt })
	return append(out, guests...)
}

// Requests lists the pending requests.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) membersChanged() {
	s.broadcast(Event{Type: "members", Members: s.Members()})
	if s.hooks.Changed != nil {
		s.hooks.Changed()
	}
}

func (s *Server) requestsChanged() {
	s.broadcast(Event{Type: "requests", Requests: s.Requests()})
	if s.hooks.Changed != nil {
		s.hooks.Changed()
	}
}

func (s *Server) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default: // a stuck client drops events; the next state catches it up
		}
	}
}

// LANAddresses lists the private IPv4 addresses of this machine.
func LANAddresses() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && ipn.IP.IsPrivate() {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}
