package claude

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestBackendsSmoke spins manager.GetOrCreate twice — once subprocess,
// once daemon — and verifies each returns a session of the right shape.
// Skips when claude isn't installed.
func TestBackendsSmoke(t *testing.T) {
	// This is a LIVE integration smoke test: it spawns real `claude`
	// workers, and the daemon path registers one on the shared per-user
	// daemon (cc-daemon-<uid>). A routine `go test ./...` must not do
	// that — a leaked or slow-to-die worker surfaces as a stray
	// "smoke-daemon-*" session in the developer's real session list.
	// Gate behind an explicit opt-in so only someone deliberately
	// exercising the backends pays that cost.
	if os.Getenv("RUN_DAEMON_SMOKE") != "1" {
		t.Skip("live backend smoke test; set RUN_DAEMON_SMOKE=1 to run (spawns real claude workers)")
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

	// --- Subprocess backend (default) ---
	t.Setenv("USE_DAEMON_BACKEND", "")

	subID := fmt.Sprintf("smoke-sub-%d", time.Now().UnixNano())
	subSession, err := mgr.GetOrCreate(subID, false, cwd, "smoke-sub", "")
	if err != nil {
		t.Fatalf("subprocess GetOrCreate: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close(subID) })

	if subSession.IsDaemonBacked() {
		t.Errorf("subprocess session unexpectedly daemon-backed")
	}
	if subSession.Cmd == nil {
		t.Errorf("subprocess session has nil Cmd")
	}
	if subSession.DaemonClient != nil {
		t.Errorf("subprocess session has non-nil DaemonClient")
	}
	t.Logf("subprocess OK · pid=%d", subSession.Cmd.Process.Pid)

	// --- Daemon backend ---
	t.Setenv("USE_DAEMON_BACKEND", "1")

	daemonID := fmt.Sprintf("smoke-daemon-%d", time.Now().UnixNano())
	daemonSession, err := mgr.GetOrCreate(daemonID, false, cwd, "smoke-daemon", "")
	if err != nil {
		t.Fatalf("daemon GetOrCreate: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close(daemonID) })

	// If the daemon was available, the session is daemon-backed. If it
	// wasn't, GetOrCreate logs a warning and falls back to subprocess —
	// that's the documented behavior and not a test failure.
	if daemonSession.IsDaemonBacked() {
		if daemonSession.DaemonShort == "" {
			t.Errorf("daemon-backed session has empty DaemonShort")
		}
		if daemonSession.DaemonUUID == "" {
			t.Errorf("daemon-backed session has empty DaemonUUID")
		}
		if daemonSession.Cmd != nil {
			t.Errorf("daemon-backed session has non-nil Cmd")
		}
		t.Logf("daemon OK · short=%s uuid=%s",
			daemonSession.DaemonShort, daemonSession.DaemonUUID)
	} else {
		t.Logf("daemon not available, fell back to subprocess (this is fine)")
	}

	// Give shutdown time to drain.
	time.Sleep(200 * time.Millisecond)
}
