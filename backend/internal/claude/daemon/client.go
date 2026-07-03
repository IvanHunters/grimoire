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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// daemonLockPath is where claude's supervisor writes its lock-file. A
// stale lock (referencing a defunct/dead pid) makes `claude daemon run`
// refuse to spawn, because the new supervisor sees "another is already
// running" and bails. clearStaleDaemonLock detects that and removes
// the lock so the spawn path can proceed.
func daemonLockPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "daemon.lock")
}

// isProcessAlive returns true only if pid points at a running process
// that is NOT a zombie. `kill(pid, 0)` returns success for zombies
// (defunct processes still occupy a pid slot until their parent
// reaps), which is exactly the trap we hit — supervisor exits, becomes
// defunct, but the lock-file pid still passes `kill(0)` so every
// subsequent spawn attempt refuses with "another is already running".
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// kill(0) — does the pid exist at all?
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	// Confirm it's not defunct. macOS exposes state via `ps -o stat=`;
	// zombies show "Z" or "Z+". Linux exposes /proc/<pid>/stat. We use
	// `ps` for portability since this only runs on the local user host.
	out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "stat=").Output()
	if err != nil {
		// `ps` failure means the pid likely vanished between kill(0)
		// and ps exec — race condition; treat as not alive.
		return false
	}
	state := strings.TrimSpace(string(out))
	return state != "" && !strings.HasPrefix(state, "Z")
}

// clearStaleDaemonLock removes ~/.claude/daemon.lock when its pid is
// defunct, absent, OR alive-but-hung (process exists but control.sock
// doesn't respond — supervisor stuck in some hot loop / deadlock).
// Idempotent — safe to call on every spawn attempt. Returns true if a
// stale lock was removed (caller can log it).
//
// The hung-process case is the trickier one: kill(pid, 0) returns
// success, ps shows the pid as running, so it LOOKS alive — but the
// supervisor isn't accepting connections. Without forcibly killing it
// and clearing the lock, every new supervisor candidate refuses to
// spawn ("another daemon is already running") and grimoire's PTY ops
// pile up 3-5s timeouts. Detection: any control.sock under
// /tmp/cc-daemon-<uid>/ that fails a 300ms dial is considered hung.
func clearStaleDaemonLock(logger *slog.Logger) bool {
	path := daemonLockPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false // no lock to clear
	}
	var lock struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return false
	}
	if lock.PID <= 0 {
		_ = os.Remove(path)
		return true
	}
	if !isProcessAlive(lock.PID) {
		if err := os.Remove(path); err != nil {
			if logger != nil {
				logger.Warn("failed to remove stale daemon lock",
					slog.Int("stale_pid", lock.PID), slog.Any("error", err))
			}
			return false
		}
		if logger != nil {
			logger.Info("removed stale daemon.lock (pid was defunct/dead)",
				slog.Int("stale_pid", lock.PID))
		}
		return true
	}
	// Process is alive — verify control.sock actually answers. A
	// supervisor stuck in a hot loop / livelock leaves the sock file
	// on disk but every dial returns ECONNREFUSED. SIGKILL it so the
	// freshly-spawned candidate can take over.
	if sockPath, err := FindSock(); err == nil {
		conn, derr := net.DialTimeout("unix", sockPath, 300*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			return false // genuinely alive AND responsive — keep lock
		}
		if logger != nil {
			logger.Warn("daemon process alive but control.sock unresponsive; SIGKILL'ing hung supervisor",
				slog.Int("hung_pid", lock.PID),
				slog.String("sock", sockPath),
				slog.Any("dial_err", derr))
		}
		_ = syscall.Kill(lock.PID, syscall.SIGKILL)
		// Give the kernel a brief moment to reap + release the lock owner.
		time.Sleep(150 * time.Millisecond)
		_ = os.Remove(sockPath)
	}
	if err := os.Remove(path); err != nil {
		if logger != nil {
			logger.Warn("failed to remove daemon lock after killing hung pid",
				slog.Int("hung_pid", lock.PID), slog.Any("error", err))
		}
		return false
	}
	return true
}

// daemonSpawnMu serialises lazy-spawn attempts across the process so a
// burst of N concurrent ops doesn't race to fork N supervisor candidates
// that all collide on the lock-file.
var daemonSpawnMu sync.Mutex

// daemonDownUntil holds a unix-millis after which retrying the lazy
// spawn is allowed again. While "down", request() short-circuits to
// an immediate error instead of paying a 3-5s dial timeout. The flag
// is cleared as soon as a spawn attempt succeeds.
var daemonDownUntil atomic.Int64

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
		// We don't dial-check here: per-op dial failures are surfaced as
		// regular errors and the keepalive loop self-heals by clearing
		// c.Sock + invoking startDaemonLazily after a short failure
		// streak. A dial-check on every resolveSock would add latency to
		// the hot path and serialise unrelated callers.
		if _, err := os.Stat(c.Sock); err == nil {
			return c.Sock, nil
		}
		c.Sock = ""
	}
	if s, err := FindSock(); err == nil {
		// Dial-check before caching — a sock-file lingering after a
		// supervisor crash would otherwise become a permanently cached
		// dead path and every op would 5s-timeout. The dial is cheap
		// (300ms) and only runs on cache-miss, not the hot path.
		conn, derr := net.DialTimeout("unix", s, 300*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			c.Sock = s
			return s, nil
		}
	}

	// Circuit breaker: if a recent spawn attempt failed, fail fast
	// instead of paying another multi-second timeout. Cleared on the
	// next successful spawn.
	if downUntil := daemonDownUntil.Load(); downUntil > 0 && time.Now().UnixMilli() < downUntil {
		return "", fmt.Errorf("daemon recently failed to spawn, retrying after %dms",
			downUntil-time.Now().UnixMilli())
	}

	// No socket → try to start the daemon. `claude daemon run` is the
	// supervisor's foreground entry point; we detach so it lives past us.
	if err := startDaemonLazily(c.Logger); err != nil {
		daemonDownUntil.Store(time.Now().Add(5 * time.Second).UnixMilli())
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
	daemonDownUntil.Store(time.Now().Add(5 * time.Second).UnixMilli())
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
		// consecutiveFailures triggers an active re-spawn attempt
		// instead of just passively warning and waiting. Without this
		// the daemon could stay down forever — keepalive would keep
		// dialing a dead sock, every other op would 5s-timeout, the
		// UI looks frozen.
		failures := 0
		const respawnThreshold = 3
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
				// Invalidate cached sock so next resolve re-discovers
				// or spawns a fresh supervisor instead of dialing the
				// dead one again.
				c.Sock = ""
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
						failures++
						if failures >= respawnThreshold {
							// Force a re-spawn attempt. resolveSock
							// already does this on the cold path, but
							// we explicitly call startDaemonLazily so
							// it bypasses the circuit-breaker window
							// after the first dial failure.
							daemonDownUntil.Store(0)
							if err := startDaemonLazily(c.Logger); err != nil && c.Logger != nil {
								c.Logger.Warn("keepalive: respawn attempt failed",
									slog.Int("failures", failures),
									slog.Any("error", err))
							}
							failures = 0
						}
						continue
					}
					failures = 0
				}
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err := conn.Write([]byte(`{"proto":1,"op":"ping"}` + "\n")); err != nil {
					_ = conn.Close()
					conn = nil
					c.Sock = ""
					continue
				}
				// Drain the reply so the daemon can write the next one.
				br := bufio.NewReader(conn)
				if _, err := br.ReadString('\n'); err != nil {
					_ = conn.Close()
					conn = nil
					c.Sock = ""
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
	// Serialise: a burst of concurrent ops would otherwise race to fork
	// N candidates that all collide on the lock-file.
	daemonSpawnMu.Lock()
	defer daemonSpawnMu.Unlock()

	// Another caller may have already spawned the supervisor while we
	// were waiting for the mutex — short-circuit if a fresh sock is
	// now visible AND dialable. A bare sock-file existence is not
	// enough: the file lingers on disk after a supervisor crash, and
	// FindSock would happily return that dead path; the dial-check
	// is what tells "supervisor actually listening".
	if sock, err := FindSock(); err == nil {
		conn, derr := net.DialTimeout("unix", sock, 300*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			return nil
		}
	}

	// Stale-lock heal: if a previous supervisor died ungracefully (OOM,
	// segfault, kill -9), its lock-file remains. `claude daemon run`
	// then refuses to spawn because kill(pid, 0) on the defunct/zombie
	// owner still returns success. Detect & remove before exec'ing.
	if clearStaleDaemonLock(logger) && logger != nil {
		logger.Info("daemon lock cleared, attempting fresh spawn")
	}

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
	// New session so the supervisor doesn't get our SIGINT / SIGTERM
	// when the parent shell exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude daemon: %w", err)
	}
	// Don't Wait() — supervisor is meant to run indefinitely. Releasing
	// the Process so its zombie state doesn't accumulate.
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	// Reset the "down" flag immediately so subsequent ops try to dial
	// the new supervisor instead of being short-circuited.
	daemonDownUntil.Store(0)
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

// requestOnce is the un-retried one-shot. Dial timeout is intentionally
// tight (800ms) so a dead supervisor surfaces sub-second to the caller
// instead of holding the goroutine for the full 3-5s ack window.
// Sidebar polling otherwise stacks a queue of 5s-deep requests and the
// whole UI appears frozen.
func requestOnce(sock, payload string) (string, error) {
	conn, err := net.DialTimeout("unix", sock, 800*time.Millisecond)
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
