package claude

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestBackendsSmoke spins manager.GetOrCreate once and verifies it returns
// a daemon-backed session. The in-process PTY subprocess backend has been
// removed — the daemon is the only backend. Skips when claude isn't
// installed or the opt-in env var isn't set.
func TestBackendsSmoke(t *testing.T) {
	// This is a LIVE integration smoke test: it spawns a real `claude`
	// worker on the shared per-user daemon (cc-daemon-<uid>). A routine
	// `go test ./...` must not do that — a leaked or slow-to-die worker
	// surfaces as a stray "smoke-daemon-*" session in the developer's real
	// session list. Gate behind an explicit opt-in.
	if os.Getenv("RUN_DAEMON_SMOKE") != "1" {
		t.Skip("live backend smoke test; set RUN_DAEMON_SMOKE=1 to run (spawns a real claude worker)")
	}
	if _, err := os.Stat("/opt/homebrew/bin/claude"); err != nil {
		t.Skip("claude CLI not found, skipping smoke test")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cwd := t.TempDir()

	// Construct a fresh manager directly — skips the global singleton so
	// tests don't share state.
	mgr := &SessionManager{
		sessions: make(map[string]*ClaudeSession),
		storage:  nil,
		logger:   logger,
	}

	daemonID := fmt.Sprintf("smoke-daemon-%d", time.Now().UnixNano())
	daemonSession, err := mgr.GetOrCreate(daemonID, false, cwd, "smoke-daemon", "")
	if err != nil {
		t.Fatalf("daemon GetOrCreate: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close(daemonID) })

	if !daemonSession.IsDaemonBacked() {
		t.Fatalf("session is not daemon-backed; daemon is the only backend")
	}
	if daemonSession.DaemonShort == "" {
		t.Errorf("daemon-backed session has empty DaemonShort")
	}
	if daemonSession.DaemonUUID == "" {
		t.Errorf("daemon-backed session has empty DaemonUUID")
	}
	if daemonSession.DaemonClient == nil {
		t.Errorf("daemon-backed session has nil DaemonClient")
	}
	t.Logf("daemon OK · short=%s uuid=%s",
		daemonSession.DaemonShort, daemonSession.DaemonUUID)

	// Give shutdown time to drain.
	time.Sleep(200 * time.Millisecond)
}
