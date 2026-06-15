package discovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSearchFixture writes a JSONL with user/assistant messages plus
// some metadata events to verify filtering.
func writeSearchFixture(t *testing.T, dir, sessionID string, userText, assistantText string) string {
	t.Helper()
	path := filepath.Join(dir, sessionID+".jsonl")
	lines := []string{
		`{"type":"permission-mode","mode":"bypassPermissions"}`,
		`{"type":"file-history-snapshot","content":"some metadata noise"}`,
		`{"type":"user","cwd":"/tmp/x","timestamp":"2026-06-10T12:00:00Z","message":{"role":"user","content":"` + userText + `"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + assistantText + `"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestSearch_FindsUserAndAssistantText(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	dir := filepath.Join(root, "-tmp-x")
	os.MkdirAll(dir, 0755)
	writeSearchFixture(t, dir, "uuid-a", "deploy to kubernetes", "kubernetes deploy completed")

	hits, err := Search(context.Background(), "kubernetes", "", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits (one user, one assistant), got %d: %#v", len(hits), hits)
	}
	roles := map[string]int{}
	for _, h := range hits {
		roles[h.Role]++
	}
	if roles["user"] != 1 || roles["assistant"] != 1 {
		t.Errorf("expected 1 user + 1 assistant hit, got %v", roles)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	dir := filepath.Join(root, "-x")
	os.MkdirAll(dir, 0755)
	writeSearchFixture(t, dir, "u", "Hello WORLD", "")

	hits, err := Search(context.Background(), "hello world", "", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit (case-insensitive), got %d", len(hits))
	}
}

func TestSearch_IgnoresMetadataEvents(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	dir := filepath.Join(root, "-x")
	os.MkdirAll(dir, 0755)

	// The metadata noise contains "noise" — search MUST NOT match it,
	// because the user only cares about conversation text.
	writeSearchFixture(t, dir, "u", "no match here", "also no match")

	hits, err := Search(context.Background(), "noise", "", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits (metadata not searchable), got %d: %#v", len(hits), hits)
	}
}

func TestSearch_FilterByCwd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	dir1 := filepath.Join(root, "-tmp-a")
	dir2 := filepath.Join(root, "-tmp-b")
	os.MkdirAll(dir1, 0755)
	os.MkdirAll(dir2, 0755)
	writeSearchFixture(t, dir1, "u1", "match keyword in A", "")
	writeSearchFixture(t, dir2, "u2", "match keyword in B", "")

	hits, err := Search(context.Background(), "keyword", "/tmp/a", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit (cwd filter), got %d", len(hits))
	}
	if !strings.Contains(hits[0].Snippet, "A") {
		t.Errorf("expected hit from cwd A, got snippet %q", hits[0].Snippet)
	}
}

func TestSearch_LimitCap(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	dir := filepath.Join(root, "-x")
	os.MkdirAll(dir, 0755)
	for i := 0; i < 5; i++ {
		writeSearchFixture(t, dir, "u"+string(rune('a'+i)), "common match text", "common match text")
	}

	hits, err := Search(context.Background(), "common", "", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 3 {
		t.Errorf("limit not enforced: got %d hits", len(hits))
	}
}

func TestSearch_SnippetWindowsAroundMatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	dir := filepath.Join(root, "-x")
	os.MkdirAll(dir, 0755)

	long := strings.Repeat("abc ", 500) + "NEEDLE " + strings.Repeat("xyz ", 500)
	writeSearchFixture(t, dir, "u", long, "")

	hits, err := Search(context.Background(), "NEEDLE", "", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if !strings.Contains(hits[0].Snippet, "NEEDLE") {
		t.Errorf("snippet should contain match: %q", hits[0].Snippet)
	}
	if len(hits[0].Snippet) > 250 {
		t.Errorf("snippet too long: %d chars", len(hits[0].Snippet))
	}
}

func TestSearch_ContextCancel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	dir := filepath.Join(root, "-x")
	os.MkdirAll(dir, 0755)
	writeSearchFixture(t, dir, "u", "any text", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Search starts

	hits, err := Search(ctx, "any", "", 100)
	// Either it returns 0 hits and ctx.Err, or it manages to scan
	// nothing — both fine. Just make sure we don't panic / hang.
	if err != nil && err != context.Canceled {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) > 0 {
		t.Logf("got %d hits despite early cancel — acceptable race", len(hits))
	}
}
