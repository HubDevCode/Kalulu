package share

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultPort is the TCP port of a room; discovery uses the next one.
const DefaultPort = 47420

// discoveryPort is the UDP port of the room announcements.
const discoveryPort = DefaultPort + 1

// Room is a room found on the network.
type Room struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Code  string `json:"-"`
}

// Announce answers discovery probes with the room until ctx ends. A probe
// carries the room code, so only people who know it find the room.
func (s *Server) Announce(ctx context.Context) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: discoveryPort})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	go func() {
		buf := make([]byte, 256)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if !isLAN(from.IP) {
				continue
			}
			msg := strings.TrimSpace(string(buf[:n]))
			code, ok := strings.CutPrefix(msg, "KALULU?")
			if !ok || !strings.EqualFold(code, s.Code) {
				continue
			}
			reply, _ := json.Marshal(Room{Name: s.Room, Owner: s.Owner, Port: s.Port()})
			_, _ = conn.WriteToUDP(reply, from)
		}
	}()
	return nil
}

// FindRoom broadcasts the code on the LAN and returns the room that answers.
func FindRoom(ctx context.Context, code string) (Room, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return Room{}, err
	}
	defer conn.Close()
	msg := []byte("KALULU?" + strings.ToUpper(strings.TrimSpace(code)))
	targets := []*net.UDPAddr{{IP: net.IPv4bcast, Port: discoveryPort}, {IP: net.IPv4(127, 0, 0, 1), Port: discoveryPort}}
	// Directed broadcasts reach networks whose router drops 255.255.255.255.
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && ipn.IP.IsPrivate() {
				ip := ipn.IP.To4()
				b := make(net.IP, 4)
				for i := range 4 {
					b[i] = ip[i] | ^ipn.Mask[i]
				}
				targets = append(targets, &net.UDPAddr{IP: b, Port: discoveryPort})
			}
		}
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(3 * time.Second)
	}
	buf := make([]byte, 512)
	for time.Now().Before(deadline) {
		for _, t := range targets {
			_, _ = conn.WriteToUDP(msg, t)
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var room Room
		if json.Unmarshal(buf[:n], &room) == nil && room.Port > 0 {
			room.Host = from.IP.String()
			room.Code = strings.ToUpper(strings.TrimSpace(code))
			return room, nil
		}
	}
	return Room{}, errors.New("no room with this code answered on the network")
}

// Client is a guest in a room.
type Client struct {
	base   string
	Name   string
	Token  string
	ID     string
	Role   string
	Room   string
	Owner  string
	http   *http.Client
	offset time.Duration // server clock minus local clock

	mu     sync.Mutex
	cancel context.CancelFunc
}

// Join enters the room at host:port with the code.
func Join(ctx context.Context, host string, port int, code, name, token string) (*Client, error) {
	c := &Client{
		base: fmt.Sprintf("http://%s", net.JoinHostPort(host, fmt.Sprint(port))),
		Name: name, http: &http.Client{Timeout: 15 * time.Second},
	}
	body, _ := json.Marshal(map[string]string{"code": code, "name": name, "token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/join", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the room: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, errors.New(strings.TrimSpace(string(msg)))
	}
	var r struct{ Token, Role, ID, Room, Owner string }
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	c.Token, c.Role, c.ID, c.Room, c.Owner = r.Token, r.Role, r.ID, r.Room, r.Owner
	c.SyncClock(ctx)
	return c, nil
}

// SyncClock estimates the offset to the Owner's clock from a few round
// trips, keeping the fastest one, as NTP does.
func (c *Client) SyncClock(ctx context.Context) {
	best := time.Duration(1 << 62)
	for range 5 {
		t0 := time.Now()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/time", nil)
		resp, err := c.http.Do(req)
		if err != nil {
			continue
		}
		var r struct{ Now int64 }
		err = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		rtt := time.Since(t0)
		if err != nil || rtt >= best {
			continue
		}
		best = rtt
		server := time.UnixMilli(r.Now)
		c.offset = server.Sub(t0.Add(rtt / 2))
	}
}

// ServerNow is the Owner's clock now.
func (c *Client) ServerNow() time.Time { return time.Now().Add(c.offset) }

// Expected returns where playback of st should be now, in seconds.
func (c *Client) Expected(st State) float64 {
	if !st.Playing {
		return st.Position
	}
	return st.Position + c.ServerNow().Sub(time.UnixMilli(st.At)).Seconds()
}

// MediaURL is the address of an Owner-local file.
func (c *Client) MediaURL(id string) string {
	return c.base + "/media/" + id + "?token=" + c.Token
}

// Send runs a command in the room.
func (c *Client) Send(ctx context.Context, cmd Command) error {
	body, _ := json.Marshal(cmd)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/cmd", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Kalulu-Token", c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return errors.New(strings.TrimSpace(string(msg)))
	}
	return nil
}

// Listen delivers the room events until Close, reconnecting after drops
// with the same token, so the guest keeps its place and role.
func (c *Client) Listen(events func(Event)) {
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			err := c.stream(ctx, events)
			if errors.Is(err, errBye) || ctx.Err() != nil {
				return
			}
			events(Event{Type: "offline", Message: fmt.Sprint(err)})
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 15*time.Second)
			c.SyncClock(ctx)
		}
	}()
}

var errBye = errors.New("room closed")

func (c *Client) stream(ctx context.Context, events func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Kalulu-Token", c.Token)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			events(Event{Type: "bye", Message: "you are no longer in the room"})
			return errBye
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		events(ev)
		if ev.Type == "bye" {
			return errBye
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

// Close leaves the room.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}
