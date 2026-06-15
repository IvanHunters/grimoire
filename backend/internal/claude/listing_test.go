package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeListingFixture writes a minimal JSONL transcript for listing tests.
func writeListingFixture(t *testing.T, dir, sessionID, cwd, title string) {
	t.Helper()
	path := filepath.Join(dir, sessionID+".jsonl")
	lines := []string{
		`{"type":"permission-mode","mode":"bypassPermissions","sessionId":"` + sessionID + `"}`,
		`{"type":"ai-title","aiTitle":"` + title + `","sessionId":"` + sessionID + `"}`,
		`{"type":"user","cwd":"` + cwd + `","gitBranch":"main","timestamp":"2026-06-10T12:00:00Z","message":{"role":"user","content":"some prompt"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestListSessionsByCwd_HistoricalOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	cwd := "/tmp/grimoire-listing"
	subdir := filepath.Join(root, "-tmp-grimoire-listing")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	writeListingFixture(t, subdir, "11111111-1111-1111-1111-111111111111", cwd, "older session")
	older := filepath.Join(subdir, "11111111-1111-1111-1111-111111111111.jsonl")
	writeListingFixture(t, subdir, "22222222-2222-2222-2222-222222222222", cwd, "newer session")
	newer := filepath.Join(subdir, "22222222-2222-2222-2222-222222222222.jsonl")

	// Differentiate mtime so sort is deterministic.
	old := time.Now().Add(-1 * time.Hour)
	now := time.Now()
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatal(err)
	}

	items, err := ListSessionsByCwd(cwd)
	if err != nil {
		t.Fatalf("ListSessionsByCwd: %v", err)
	}
	// May include extras from the live daemon — filter to ours.
	mine := filterByCwd(items, cwd)
	if len(mine) != 2 {
		t.Fatalf("expected exactly 2 historical sessions for our cwd, got %d", len(mine))
	}
	if mine[0].Name != "newer session" {
		t.Errorf("expected newer session first, got %q", mine[0].Name)
	}
	// Historical-only: Live should be nil, DaemonShort empty.
	for _, m := range mine {
		if m.Live != nil {
			t.Errorf("expected nil Live for historical session %s", m.SessionID)
		}
		if m.DaemonShort != "" {
			t.Errorf("expected empty DaemonShort for historical session, got %s", m.DaemonShort)
		}
	}
}

func TestListSessionsByCwd_EmptyForUnknownCwd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	items, err := ListSessionsByCwd("/nope/never/existed")
	if err != nil {
		t.Fatalf("expected nil error for missing cwd, got %v", err)
	}
	// Filter out live (anything from the actual user's daemon) since
	// the test process can't control that.
	mine := filterByCwd(items, "/nope/never/existed")
	if len(mine) != 0 {
		t.Errorf("expected 0 sessions for unknown cwd, got %d", len(mine))
	}
}

// filterByCwd keeps only items whose cwd matches exactly. The live
// daemon may add sessions from other cwds when our SanitizeCwd lookup
// is loose, so we narrow client-side for assertions.
func filterByCwd(items []SessionListItem, cwd string) []SessionListItem {
	out := items[:0]
	for _, it := range items {
		if it.Cwd == cwd {
			out = append(out, it)
		}
	}
	return out
}
