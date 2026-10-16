package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/bjarneo/cliamp/playlist"
)

// instanceRequest is what a second launch sends to the running Kalulu.
type instanceRequest struct {
	Args []string `json:"args,omitempty"`
}

// instanceSocket is the per-user socket that makes Kalulu a single
// instance. It lives in the runtime directory, which only the user can read.
func instanceSocket() string {
	// KALULU_INSTANCE_SOCKET runs a separate instance, such as a second
	// profile with its own HOME.
	if p := os.Getenv("KALULU_INSTANCE_SOCKET"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
		// Windows has no uid, and its temporary folder is per user.
		if os.Getuid() < 0 {
			return filepath.Join(dir, "kalulu.sock")
		}
		return filepath.Join(dir, fmt.Sprintf("kalulu-%d.sock", os.Getuid()))
	}
	return filepath.Join(dir, "kalulu.sock")
}

// absArgs turns relative paths into absolute ones, since the running
// instance has another working directory.
func absArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if !playlist.IsURL(a) {
			if abs, err := filepath.Abs(a); err == nil {
				a = abs
			}
		}
		out = append(out, a)
	}
	return out
}

// activateRunning hands args to a running Kalulu. It reports false when
// none answers.
func activateRunning(args []string) bool {
	conn, err := net.DialTimeout("unix", instanceSocket(), 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	data, _ := json.Marshal(instanceRequest{Args: absArgs(args)})
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return false
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	return err == nil && reply == "ok\n"
}

// errAlreadyRunning is returned when another Kalulu holds the socket.
var errAlreadyRunning = errors.New("kalulu is already running")

// listenInstance claims the single-instance socket. Each request delivers
// its arguments on the returned channel; an empty list means "show".
func listenInstance() (<-chan []string, func(), error) {
	path := instanceSocket()
	// A socket left by a crash answers nobody; remove it.
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return nil, nil, errAlreadyRunning
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, err
	}
	_ = os.Chmod(path, 0o600)
	ch := make(chan []string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				var req instanceRequest
				if json.Unmarshal([]byte(line), &req) != nil {
					return
				}
				select {
				case ch <- req.Args:
				default:
				}
				_, _ = c.Write([]byte("ok\n"))
			}(conn)
		}
	}()
	return ch, func() { ln.Close(); _ = os.Remove(path) }, nil
}
