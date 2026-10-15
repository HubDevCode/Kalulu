package share

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu       sync.Mutex
	commands []Command
	requests []Request
}

func (r *recorder) hooks() Hooks {
	return Hooks{
		Control:   func(c Command) error { r.mu.Lock(); r.commands = append(r.commands, c); r.mu.Unlock(); return nil },
		Requested: func(q Request) { r.mu.Lock(); r.requests = append(r.requests, q); r.mu.Unlock() },
	}
}

func (r *recorder) count() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.commands), len(r.requests)
}

func startRoom(t *testing.T) (*Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := NewServer("Party", "Owner", rec.hooks())
	if err := s.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s, rec
}

func TestJoinNeedsTheCode(t *testing.T) {
	s, _ := startRoom(t)
	ctx := context.Background()
	if _, err := Join(ctx, "127.0.0.1", s.Port(), "WRONG1", "Ann", ""); err == nil {
		t.Fatal("joined with a wrong code")
	}
	c, err := Join(ctx, "127.0.0.1", s.Port(), strings.ToLower(s.Code), "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Role != RoleListener || c.Room != "Party" {
		t.Fatalf("role %q room %q", c.Role, c.Room)
	}
	// The token brings the guest back as the same member.
	again, err := Join(ctx, "127.0.0.1", s.Port(), s.Code, "Ann", c.Token)
	if err != nil || again.ID != c.ID {
		t.Fatalf("rejoin: %v id %s vs %s", err, again.ID, c.ID)
	}
	if n := len(s.Members()); n != 2 {
		t.Fatalf("members = %d, want owner + Ann", n)
	}
}

func TestRolesAndStateSync(t *testing.T) {
	s, rec := startRoom(t)
	ctx := context.Background()
	c, err := Join(ctx, "127.0.0.1", s.Port(), s.Code, "Bob", "")
	if err != nil {
		t.Fatal(err)
	}
	states := make(chan State, 8)
	byes := make(chan string, 1)
	c.Listen(func(ev Event) {
		switch ev.Type {
		case "state":
			if ev.State != nil && ev.State.Track != nil {
				states <- *ev.State
			}
		case "bye":
			byes <- ev.Message
		}
	})
	defer c.Close()
	time.Sleep(200 * time.Millisecond)
	s.SetState(State{Track: &Track{Path: "https://x/a", Title: "A"}, Playing: true, Position: 30})
	select {
	case st := <-states:
		if exp := c.Expected(st); exp < 29.9 || exp > 31 {
			t.Fatalf("expected position %.2f, want about 30", exp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no state event")
	}
	track := &Track{Path: "https://x/b"}
	if err := c.Send(ctx, Command{Op: "pause"}); err == nil {
		t.Fatal("a listener paused the room")
	}
	members := s.Members()
	if err := s.Run(Command{Op: "role", Member: members[1].ID, Role: RoleEditor}); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, Command{Op: "pause"}); err != nil {
		t.Fatalf("editor pause: %v", err)
	}
	_ = s.Run(Command{Op: "role", Member: members[1].ID, Role: RoleRequester})
	if err := c.Send(ctx, Command{Op: "request", Track: track}); err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := c.Send(ctx, Command{Op: "request", Track: &Track{Path: "/etc/passwd"}}); err == nil {
		t.Fatal("a local path was accepted")
	}
	cmds, reqs := rec.count()
	if cmds != 1 || reqs != 1 || len(s.Requests()) != 1 {
		t.Fatalf("commands %d requests %d pending %d", cmds, reqs, len(s.Requests()))
	}
	if err := s.Run(Command{Op: "approve", Request: s.Requests()[0].ID}); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := rec.count(); cmds != 2 || len(s.Requests()) != 0 {
		t.Fatalf("approve did not queue the track (commands %d)", cmds)
	}
	_ = s.Run(Command{Op: "kick", Member: members[1].ID})
	select {
	case <-byes:
	case <-time.After(3 * time.Second):
		t.Fatal("the kicked guest was not told")
	}
}

func TestSharedLocalFileSupportsRange(t *testing.T) {
	s, _ := startRoom(t)
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := s.ShareLocal(path)
	c, err := Join(context.Background(), "127.0.0.1", s.Port(), s.Code, "Cy", "")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, c.MediaURL(id), nil)
	req.Header.Set("Range", "bytes=3-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "345" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	resp, _ = http.Get(strings.Replace(c.MediaURL(id), c.Token, "bad", 1))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("media without a valid token: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestFindRoomByCode(t *testing.T) {
	s, _ := startRoom(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Announce(ctx); err != nil {
		t.Skipf("discovery port busy: %v", err)
	}
	fctx, fcancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer fcancel()
	room, err := FindRoom(fctx, s.Code)
	if err != nil {
		t.Fatal(err)
	}
	if room.Port != s.Port() || room.Name != "Party" {
		t.Fatalf("room = %+v", room)
	}
	wctx, wcancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer wcancel()
	if _, err := FindRoom(wctx, "ZZZZZZ"); err == nil {
		t.Fatal("found a room with the wrong code")
	}
}
