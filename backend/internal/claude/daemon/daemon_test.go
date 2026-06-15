package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// These tests require a running claude daemon (started by `claude agents` or
// `claude --bg`). They skip when no socket is found, so CI without claude
// installed stays green.

func testCwd(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "daemon-test-*")
	if err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// Resolve symlinks so the cwd we send matches what the daemon reports
	// back (macOS prefixes /tmp -> /private/tmp).
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	return resolved
}

func newClientOrSkip(t *testing.T) *Client {
	t.Helper()
	sock, err := FindSock()
	if err != nil {
		t.Skipf("skipping: no daemon socket (%v)", err)
	}
	return &Client{Sock: sock}
}

func TestPing(t *testing.T) {
	c := newClientOrSkip(t)
	reply, err := c.Ping()
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if reply.Proto != 1 {
		t.Errorf("expected proto=1, got %d", reply.Proto)
	}
	if reply.Version == "" {
		t.Error("expected non-empty version")
	}
	t.Logf("daemon · version=%s proto=%d", reply.Version, reply.Proto)
}

func TestList(t *testing.T) {
	c := newClientOrSkip(t)
	jobs, err := c.ListSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	t.Logf("daemon has %d session(s)", len(jobs))
	for _, j := range jobs {
		t.Logf("  · short=%s name=%q state=%s tempo=%s", j.Short, j.Name, j.State, j.Tempo)
	}
}

func TestHas_NonexistentSession(t *testing.T) {
	c := newClientOrSkip(t)
	r, err := c.Has("deadbeef")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if r.Alive || r.Present {
		t.Errorf("expected nonexistent session to be alive=false present=false, got %+v", r)
	}
}

func TestKill_NonexistentSession(t *testing.T) {
	c := newClientOrSkip(t)
	err := c.Kill("deadbeef")
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
	// Kill on a missing session returns ENOJOB — that's a well-formed error
	// from the daemon, not a transport failure.
	t.Logf("got expected error: %v", err)
}

func TestDispatchLifecycle(t *testing.T) {
	c := newClientOrSkip(t)
	cwd := testCwd(t)

	res, err := c.Dispatch(DispatchOpts{
		Cwd:    cwd,
		Name:   "grimoire-test-lifecycle",
		Prompt: "respond with the single word ACK",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.Short == "" || res.SessionID == "" {
		t.Fatalf("expected non-empty short/sessionId, got %+v", res)
	}
	t.Logf("dispatched · short=%s sessionId=%s via=%s", res.Short, res.SessionID, res.Via)

	// Always clean up, even on later failure.
	defer func() {
		if err := c.Remove(res.Short); err != nil {
			t.Logf("cleanup remove: %v", err)
		}
	}()

	// Wait briefly for the daemon to register the new session, then verify.
	time.Sleep(500 * time.Millisecond)
	has, err := c.Has(res.Short)
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has.Alive {
		t.Errorf("expected alive=true after dispatch, got %+v", has)
	}

	// Verify it appears in list with our cwd.
	jobs, err := c.ListSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *Record
	for i := range jobs {
		if jobs[i].Short == res.Short {
			found = &jobs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("dispatched session %s not in list", res.Short)
	}
	if found.Cwd != cwd {
		t.Errorf("expected cwd=%s in list, got %s", cwd, found.Cwd)
	}
	if found.Name != "grimoire-test-lifecycle" {
		t.Errorf("expected name=grimoire-test-lifecycle, got %q", found.Name)
	}
}

func TestReply_NonexistentSession(t *testing.T) {
	c := newClientOrSkip(t)
	err := c.Reply("deadbeef", "test")
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestSubscribeSnapshot(t *testing.T) {
	c := newClientOrSkip(t)
	cwd := testCwd(t)

	res, err := c.Dispatch(DispatchOpts{
		Cwd:    cwd,
		Name:   "grimoire-test-subscribe",
		Prompt: "just say OK",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	defer func() { _ = c.Remove(res.Short) }()

	// Give the daemon a moment to set up the rendezvous socket.
	time.Sleep(700 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var firstFrame SubFrame
	var got bool
	var mu sync.Mutex

	sErr := c.Subscribe(ctx, res.Short, 50, func(f SubFrame) {
		mu.Lock()
		if !got {
			firstFrame = f
			got = true
		}
		mu.Unlock()
		cancel() // we've seen at least one frame, that's enough
	})
	// Subscribe returns ctx.Canceled when we cancel after the first frame.
	if sErr != nil && !errors.Is(sErr, context.Canceled) && !errors.Is(sErr, context.DeadlineExceeded) {
		t.Fatalf("subscribe: %v", sErr)
	}
	if !got {
		t.Fatal("expected at least one subscribe frame")
	}
	if firstFrame.Type != "snapshot" {
		t.Errorf("expected first frame type=snapshot, got %q", firstFrame.Type)
	}
	if firstFrame.Record == nil || firstFrame.Record.Short != res.Short {
		t.Errorf("expected snapshot.Record.Short=%s, got %+v", res.Short, firstFrame.Record)
	}
}

func TestAttach_HandshakeReadWriteClose(t *testing.T) {
	c := newClientOrSkip(t)
	cwd := testCwd(t)

	// Use an empty prompt — claude treats this as "waiting for first user
	// input", so the session is idle and accepts attach instead of being
	// rejected as 'done'/'blocked'.
	res, err := c.Dispatch(DispatchOpts{
		Cwd:  cwd,
		Name: "grimoire-test-attach",
		// no Prompt → idle session waiting for the first reply
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	defer func() { _ = c.Remove(res.Short) }()

	// Give the daemon time to set up the rendezvous socket for the new
	// worker — attaching too early returns ESTARTING.
	time.Sleep(700 * time.Millisecond)

	a, err := c.Attach(res.Short, 120, 40)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer a.Close()

	if a.AttachID() == "" {
		t.Error("expected non-empty attachID")
	}

	// Read the welcome banner — claude's TUI paints immediately on attach.
	// Use a deadline so a stuck daemon doesn't hang the test.
	if err := setReadDeadlineIfSupported(a, 2*time.Second); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 8192)
	n, err := a.Read(buf)
	if err != nil {
		t.Fatalf("read welcome banner: %v", err)
	}
	if n == 0 {
		t.Fatal("expected some bytes from welcome banner, got 0")
	}
	t.Logf("read %d bytes from TUI banner (first 40: %q)", n, string(buf[:min(n, 40)]))

	// Write a no-op keystroke (just Ctrl-L for redraw, doesn't submit).
	if _, err := a.Write([]byte{0x0c}); err != nil {
		t.Fatalf("write keystroke: %v", err)
	}

	// Resize should succeed without error.
	if err := a.Resize(100, 30); err != nil {
		t.Errorf("resize: %v", err)
	}
}

// setReadDeadlineIfSupported tries to set a read deadline on the attach
// conn. AttachConn doesn't expose the underlying conn directly — but it
// embeds it; we reach in for testing only.
func setReadDeadlineIfSupported(a *AttachConn, d time.Duration) error {
	type deadliner interface {
		SetReadDeadline(time.Time) error
	}
	if dl, ok := a.conn.(deadliner); ok {
		return dl.SetReadDeadline(time.Now().Add(d))
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestCleanTerminal(t *testing.T) {
	// ANSI cursor moves, color codes, and bare CR characters should be
	// stripped; the underlying text content should remain.
	input := []string{
		"\x1b[?1049h\x1b[2J\x1b[H",
		"\x1b[1;31mHello\x1b[0m world\r\n",
		"   \x1b[2mtrim me  \x1b[0m\r",
		"\x1b]0;title\x07keep\n",
	}
	got := CleanTerminal(input)
	// CleanTerminal keeps leading whitespace (code-block indentation is
	// meaningful) and only strips trailing whitespace.
	want := []string{"Hello world", "   trim me", "keep"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: want %q, got %q", i, want[i], got[i])
		}
	}
}
