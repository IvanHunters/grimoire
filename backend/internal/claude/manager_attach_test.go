package claude

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// nopPTY is a non-nil io.ReadWriteCloser standing in for a live PTY /
// daemon AttachConn — lookupLiveAttach only checks PTY != nil, never
// reads from it.
type nopPTY struct{}

func (nopPTY) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopPTY) Write(p []byte) (int, error) { return len(p), nil }
func (nopPTY) Close() error                { return nil }

// Regression guard for the attach-to-wrong-session bug: quick terminals
// share a cwd and collide on grimoireID, so GetOrAttach's fast-path
// lookup must reuse a cached session ONLY when its DaemonUUID matches the
// worker being attached to. Before the fix, clicking session A (whose
// grimoireID entry was cached for worker B) handed back B's live PTY.
func TestLookupLiveAttach_UUIDGuard(t *testing.T) {
	mgr := &SessionManager{
		sessions: make(map[string]*ClaudeSession),
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	cached := &ClaudeSession{ID: "gid", DaemonUUID: "uuid-A", PTY: nopPTY{}}
	mgr.sessions["gid"] = cached

	// Matching UUID → reuse the cached session.
	if got := mgr.lookupLiveAttach("gid", "uuid-A"); got != cached {
		t.Fatalf("matching UUID: expected cached session, got %v", got)
	}

	// Mismatched UUID → must NOT reuse it (this is the bug).
	if got := mgr.lookupLiveAttach("gid", "uuid-B"); got != nil {
		t.Fatalf("mismatched UUID: expected nil (fresh attach), got the wrong cached session %v", got)
	}

	// Dead PTY → not reusable even with a matching UUID.
	mgr.sessions["dead"] = &ClaudeSession{ID: "dead", DaemonUUID: "uuid-C", PTY: nil}
	if got := mgr.lookupLiveAttach("dead", "uuid-C"); got != nil {
		t.Fatalf("dead PTY: expected nil, got %v", got)
	}

	// Unknown grimoireID → nil.
	if got := mgr.lookupLiveAttach("nope", "uuid-A"); got != nil {
		t.Fatalf("unknown id: expected nil, got %v", got)
	}
}
