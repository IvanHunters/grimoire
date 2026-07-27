package websocket

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivanohotnikov/markdown-editor/internal/claude"
)

// fakeResumeLookup is a hand-rolled resumeSessionLookup so we can seed
// grimoire-id -> ClaudeSession mappings without spinning up a real
// SessionManager (whose sessions map is unexported and unreachable from
// this package).
type fakeResumeLookup map[string]*claude.ClaudeSession

func (f fakeResumeLookup) Get(id string) (*claude.ClaudeSession, error) {
	if s, ok := f[id]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("session not found: %s", id)
}

// writeTranscript drops an empty <uuid>.jsonl into a project dir under
// root so discovery.SessionPath can find it.
func writeTranscript(t *testing.T, root, project, uuid string) {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, uuid+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveResumeTarget(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const (
		liveUUID    = "11111111-1111-1111-1111-111111111111"
		grimoireOK  = "global-alive"
		mappedUUID  = "22222222-2222-2222-2222-222222222222"
		grimoireBad = "global-pqpr0l" // the reported regression
		deadUUID    = "86ac30af-8485-4325-870a-d4102af0eb1e"
	)

	// Only liveUUID and mappedUUID have transcripts on disk. deadUUID
	// deliberately has none (mirrors the "3315" session whose main
	// <uuid>.jsonl was gone, leaving only a sidecar dir).
	writeTranscript(t, root, "proj-a", liveUUID)
	writeTranscript(t, root, "proj-b", mappedUUID)

	lookup := fakeResumeLookup{
		grimoireOK:  {DaemonUUID: mappedUUID, WorkingDir: "/tmp/work-ok"},
		grimoireBad: {DaemonUUID: deadUUID, WorkingDir: "/tmp/work-dead"},
	}

	t.Run("bare uuid with transcript resolves to itself", func(t *testing.T) {
		uuid, _ := resolveResumeTarget(liveUUID, lookup)
		if uuid != liveUUID {
			t.Fatalf("want %s, got %q", liveUUID, uuid)
		}
	})

	t.Run("grimoire id maps to daemon uuid with transcript", func(t *testing.T) {
		uuid, cwd := resolveResumeTarget(grimoireOK, lookup)
		if uuid != mappedUUID {
			t.Fatalf("want resolved uuid %s, got %q", mappedUUID, uuid)
		}
		if cwd != "/tmp/work-ok" {
			t.Fatalf("want cwd /tmp/work-ok, got %q", cwd)
		}
	})

	t.Run("regression: grimoire id with no transcript yields empty uuid but keeps cwd", func(t *testing.T) {
		// This is the exact "форк виснет" case: SessionPath fails on the
		// grimoire id AND on the mapped-but-gone daemon uuid, so the
		// caller must downgrade to a fresh spawn. cwd survives so the
		// fresh worker starts in the right directory.
		uuid, cwd := resolveResumeTarget(grimoireBad, lookup)
		if uuid != "" {
			t.Fatalf("want empty uuid (forces downgrade), got %q", uuid)
		}
		if cwd != "/tmp/work-dead" {
			t.Fatalf("want cwd /tmp/work-dead preserved, got %q", cwd)
		}
	})

	t.Run("unknown id with no transcript and no mapping yields nothing", func(t *testing.T) {
		uuid, cwd := resolveResumeTarget("global-unknown", lookup)
		if uuid != "" || cwd != "" {
			t.Fatalf("want empty/empty, got uuid=%q cwd=%q", uuid, cwd)
		}
	})
}
