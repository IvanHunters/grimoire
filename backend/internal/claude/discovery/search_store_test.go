package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// storeSeeded writes a session in the given cwd, then relocates it to a
// shelf of the store, and returns nothing but a t.Fatal on failure.
func storeSeeded(t *testing.T, root, cwd, uuid, text string, kind StoreKind, nano int64) {
	t.Helper()
	dir := filepath.Join(root, SanitizeCwd(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, uuid+".jsonl")
	line := `{"type":"user","cwd":"` + cwd + `","sessionId":"` + uuid + `","message":{"role":"user","content":"` + text + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveTranscriptToStore(path, uuid, kind, nano); err != nil {
		t.Fatal(err)
	}
}

// Searching must reach archived and deleted sessions too, each hit
// labelled with where it lives. Without this, a session that was put
// away becomes unfindable and the only way back is digging through the
// filesystem by hand.
func TestSearch_CoversArchiveAndTrash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const cwd = "/Users/ivan/gitops/thing"
	live := filepath.Join(root, SanitizeCwd(cwd))
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSearchFixture(t, live, "aaaaaaaa-0000-4000-8000-000000000001", "flokinet talos boot", "ok")
	storeSeeded(t, root, cwd, "bbbbbbbb-0000-4000-8000-000000000002", "flokinet ipmi console", StoreArchive, 1735689600000000000)
	storeSeeded(t, root, cwd, "cccccccc-0000-4000-8000-000000000003", "flokinet whmcs plugin", StoreTrash, 1735689600000000001)

	hits, err := Search(context.Background(), "flokinet", "", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	states := map[string]string{}
	for _, h := range hits {
		states[h.SessionID] = h.State
	}
	want := map[string]string{
		"aaaaaaaa-0000-4000-8000-000000000001": SessionStateActive,
		"bbbbbbbb-0000-4000-8000-000000000002": SessionStateArchived,
		"cccccccc-0000-4000-8000-000000000003": SessionStateDeleted,
	}
	for id, state := range want {
		got, ok := states[id]
		if !ok {
			t.Errorf("session %s missing from results, want state %q", id, state)
			continue
		}
		if got != state {
			t.Errorf("session %s state = %q, want %q", id, got, state)
		}
	}
}

// The cwd filter has to apply to stored sessions as well, otherwise
// scoping a search to one project leaks results from every other one.
func TestSearch_CwdFilterAppliesToStoredSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	storeSeeded(t, root, "/Users/ivan/gitops/wanted", "dddddddd-0000-4000-8000-000000000004", "shared keyword", StoreArchive, 1735689600000000000)
	storeSeeded(t, root, "/Users/ivan/gitops/other", "eeeeeeee-0000-4000-8000-000000000005", "shared keyword", StoreArchive, 1735689600000000001)

	hits, err := Search(context.Background(), "shared keyword", "/Users/ivan/gitops/wanted", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected the archived session in the filtered cwd to match")
	}
	for _, h := range hits {
		if h.SessionID != "dddddddd-0000-4000-8000-000000000004" {
			t.Errorf("cwd filter leaked session %s from another project", h.SessionID)
		}
	}
}

// Live sessions keep the active label so existing callers that don't
// know about the store still read sensibly.
func TestSearch_LiveHitsAreLabelledActive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	dir := filepath.Join(root, "-tmp-x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSearchFixture(t, dir, "ffffffff-0000-4000-8000-000000000006", "deploy to kubernetes", "done")

	hits, err := Search(context.Background(), "kubernetes", "", 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	for _, h := range hits {
		if h.State != SessionStateActive {
			t.Errorf("live hit state = %q, want %q", h.State, SessionStateActive)
		}
	}
}
