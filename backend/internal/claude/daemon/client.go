package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Client is a stateless wrapper around a daemon control socket path. Every
// op opens a fresh unix-socket connection — the daemon's request/reply
// pattern is one-shot, with the exception of Subscribe which keeps the
// connection open for streaming.
//
// Discovery is lazy: when Sock is empty the first call invokes findSock()
// and caches the result. Pass an explicit Sock to pin a specific daemon
// instance (useful in tests).
type Client struct {
	Sock   string
	Logger *slog.Logger
}

// FindSock globs for the active daemon control socket for the current user.
// Returns the newest match by mtime — on a single-user machine there's
// always exactly one socket, but the daemon may rotate paths after restart.
func FindSock() (string, error) {
	uid := os.Getuid()
	pattern := fmt.Sprintf("/tmp/cc-daemon-%d/*/control.sock", uid)
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return "", fmt.Errorf("no control.sock matching %s (is `claude agents` running?)", pattern)
	}
	sort.Slice(matches, func(i, j int) bool {
		fi, ei := os.Stat(matches[i])
		fj, ej := os.Stat(matches[j])
		if ei != nil || ej != nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})
	return matches[0], nil
}

// resolveSock returns c.Sock, discovering it lazily on the first call.
// If no socket is found, attempts to start the daemon lazily — claude
// supervisor self-exits when idle (no live sessions for ~hour), and
// our client has to be able to spin it back up on the next request.
func (c *Client) resolveSock() (string, error) {
	if c.Sock != "" {
		// Verify the cached path still exists — daemon may have rotated.
		if _, err := os.Stat(c.Sock); err == nil {
			return c.Sock, nil
		}
		c.Sock = ""
	}
	if s, err := FindSock(); err == nil {
		c.Sock = s
		return s, nil
	}

	// No socket → try to start the daemon by running `claude agents --json`.
	// That command lazily spawns the supervisor and returns immediately;
	// the supervisor stays up after the CLI exits.
	if err := startDaemonLazily(c.Logger); err != nil {
		return "", fmt.Errorf("daemon not running and lazy start failed: %w", err)
	}

	// Poll for the socket to appear (usually under 500ms on warm systems).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := FindSock(); err == nil {
			c.Sock = s
			return s, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", fmt.Errorf("daemon spawn started but socket never appeared")
}

// StartKeepAlive holds a persistent connection to the daemon control
// socket and sends op:ping every `interval`. The daemon supervisor
// (per `claude daemon --help`) exits when its last client disconnects,
// taking all worker sessions with it. By keeping one socket open and
// active for the lifetime of grimoire, we guarantee the daemon sees
// us as a client and never shuts down.
//
// Returns a cancel func — call on graceful shutdown to release the
// connection. Background goroutine self-heals: if the socket dies
// (daemon got restarted some other way), it reconnects on the next
// tick using the regular discovery flow.
func (c *Client) StartKeepAlive(interval time.Duration) (cancel func()) {
	done := make(chan struct{})
	go func() {
		var conn net.Conn
		dial := func() net.Conn {
			sock, err := c.resolveSock()
			if err != nil {
				if c.Logger != nil {
					c.Logger.Warn("keepalive: socket resolve failed", slog.Any("error", err))
				}
				return nil
			}
			n, err := net.Dial("unix", sock)
			if err != nil {
				if c.Logger != nil {
					c.Logger.Warn("keepalive: dial failed", slog.Any("error", err))
				}
				return nil
			}
			return n
		}
		conn = dial()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				if conn != nil {
					_ = conn.Close()
				}
				return
			case <-ticker.C:
				if conn == nil {
					conn = dial()
					if conn == nil {
						continue
					}
				}
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err := conn.Write([]byte(`{"proto":1,"op":"ping"}` + "\n")); err != nil {
					_ = conn.Close()
					conn = nil
					continue
				}
				// Drain the reply so the daemon can write the next one.
				br := bufio.NewReader(conn)
				if _, err := br.ReadString('\n'); err != nil {
					_ = conn.Close()
					conn = nil
				}
			}
		}
	}()
	return func() { close(done) }
}

// startDaemonLazily spawns the claude supervisor in the background via
// `claude daemon run --origin transient`. That's the supervisor's own
// foreground entry point — it runs until killed, so we detach from it
// and let it live. `claude agents --json` does NOT trigger a daemon
// spawn when there are no sessions, so we use the direct `daemon run`
// command instead.
//
// On macOS the supervisor socket appears in /tmp/cc-daemon-<uid>/
// within ~300ms of process start. We don't wait here — caller polls
// the socket separately.
func startDaemonLazily(logger *slog.Logger) error {
	if logger != nil {
		logger.Info("daemon socket not found, spawning supervisor via claude daemon run")
	}
	cmd := exec.Command("claude", "daemon", "run", "--origin", "transient")
	// Detach stdio so the supervisor doesn't inherit our file
	// descriptors. We never reap this process — it lives on past the
	// caller's lifetime.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude daemon: %w", err)
	}
	// Don't Wait() — supervisor is meant to run indefinitely. Releasing
	// the Process so its zombie state doesn't accumulate.
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

// request sends one newline-framed JSON request and returns the first reply
// line. On transient daemon-startup errors (ESTARTING / ENOCONN) it retries
// up to 10 times with 200ms backoff, matching the claude-side behaviour.
//
// Non-transient errors (ENOJOB, EALIVE, EUNKNOWN, etc) are returned as-is
// in the reply line — caller decides whether to surface them.
func (c *Client) request(payload string) (string, error) {
	const maxRetries = 10
	const backoff = 200 * time.Millisecond

	sock, err := c.resolveSock()
	if err != nil {
		return "", err
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		line, err := requestOnce(sock, payload)
		if err != nil {
			return "", err
		}
		var probe struct {
			OK   bool   `json:"ok"`
			Code string `json:"code"`
		}
		_ = json.Unmarshal([]byte(line), &probe)
		if probe.OK || (probe.Code != "ESTARTING" && probe.Code != "ENOCONN") {
			return line, nil
		}
		if attempt < maxRetries {
			if c.Logger != nil {
				c.Logger.Debug("daemon transient, retrying",
					slog.String("code", probe.Code),
					slog.Int("attempt", attempt+1),
				)
			}
			time.Sleep(backoff)
		}
	}
	// Exhausted retries — return the last reply so caller sees the error code.
	return requestOnce(sock, payload)
}

// requestOnce is the un-retried one-shot.
func requestOnce(sock, payload string) (string, error) {
	conn, err := net.DialTimeout("unix", sock, 3*time.Second)
	if err != nil {
		return "", fmt.Errorf("dial daemon: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(payload + "\n")); err != nil {
		return "", fmt.Errorf("write request: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("read reply: %w", err)
	}
	return line, nil
}
