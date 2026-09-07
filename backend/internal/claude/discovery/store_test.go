package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedSession writes a transcript plus every sidecar a real session
// owns, and returns the transcript path.
func seedSession(t *testing.T, projectsRoot, slug, uuid, cwd string) string {
	t.Helper()
	proj := filepath.Join(projectsRoot, slug)
	if err := os.MkdirAll(filepath.Join(proj, uuid, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, uuid+".jsonl")
	body := `{"type":"user","cwd":"` + cwd + `","sessionId":"` + uuid + `","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(jsonl, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		filepath.Join(proj, uuid+".jsonl.ledger.md"),
		filepath.Join(proj, uuid+".jsonl.archive.20260101T000000Z.jsonl"),
		filepath.Join(proj, uuid, "subagents", "agent-x.jsonl"),
	} {
		if err := os.WriteFile(f, []byte("data\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return jsonl
}

// The store must sit OUTSIDE ~/.claude/projects, otherwise ScanAll
// re-lists archived and deleted sessions and they reappear in the
// sidebar as if nothing happened.
func TestStoreRootsLiveOutsideProjectsRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	store, err := StoreRoot()
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Clean(root) + string(os.PathSeparator)
	if strings.HasPrefix(filepath.Clean(store)+string(os.PathSeparator), inside) {
		t.Fatalf("store root %q must not be inside projects root %q", store, root)
	}

	arc, err := StoreKindRoot(StoreArchive)
	if err != nil {
		t.Fatal(err)
	}
	trash, err := StoreKindRoot(StoreTrash)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(arc) != store || filepath.Dir(trash) != store {
		t.Fatalf("archive %q and trash %q must both live under one store root %q", arc, trash, store)
	}
	if arc == trash {
		t.Fatal("archive and trash must be distinct dirs")
	}
}

// Archiving relocates the transcript and every sidecar, and records a
// manifest so restore is deterministic instead of guessing the project
// dir from the transcript body.
func TestMoveTranscriptToStoreWritesManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "86ac30af-8485-4325-870a-d4102af0eb1e"
	const cwd = "/Users/ivan/gitops/thing"
	jsonl := seedSession(t, root, SanitizeCwd(cwd), uuid, cwd)

	dest, err := MoveTranscriptToStore(jsonl, uuid, StoreArchive, 1735689600000000000)
	if err != nil {
		t.Fatalf("MoveTranscriptToStore: %v", err)
	}

	for _, name := range []string{
		uuid + ".jsonl",
		uuid + ".jsonl.ledger.md",
		uuid + ".jsonl.archive.20260101T000000Z.jsonl",
		filepath.Join(uuid, "subagents", "agent-x.jsonl"),
	} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Fatalf("expected %s carried into the store, got: %v", name, err)
		}
	}
	if _, err := SessionPath(uuid); err == nil {
		t.Fatal("expected SessionPath to miss once the session is archived")
	}

	raw, err := os.ReadFile(filepath.Join(dest, manifestName))
	if err != nil {
		t.Fatalf("manifest must be written next to the transcript: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest must be valid json: %v", err)
	}
	if m.SessionID != uuid {
		t.Errorf("manifest session id = %q, want %q", m.SessionID, uuid)
	}
	if m.Kind != StoreArchive {
		t.Errorf("manifest kind = %q, want %q", m.Kind, StoreArchive)
	}
	if m.OriginPath != jsonl {
		t.Errorf("manifest origin = %q, want %q", m.OriginPath, jsonl)
	}
	if m.Cwd != cwd {
		t.Errorf("manifest cwd = %q, want %q", m.Cwd, cwd)
	}
}

// Restore is the inverse of the move: every file goes back to the exact
// project dir it came from, and the store folder is cleaned up so a
// session is never listed as both live and archived.
func TestRestoreFromStoreReturnsEverythingToOrigin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "0d2f4a51-1c0e-4a8b-9f2d-2b7b7cf3a111"
	const cwd = "/Users/ivan/gitops/thing"
	jsonl := seedSession(t, root, SanitizeCwd(cwd), uuid, cwd)

	dest, err := MoveTranscriptToStore(jsonl, uuid, StoreTrash, 1735689600000000000)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := RestoreFromStore(uuid)
	if err != nil {
		t.Fatalf("RestoreFromStore: %v", err)
	}
	if restored != jsonl {
		t.Errorf("restored to %q, want the original path %q", restored, jsonl)
	}
	proj := filepath.Dir(jsonl)
	for _, name := range []string{
		uuid + ".jsonl",
		uuid + ".jsonl.ledger.md",
		uuid + ".jsonl.archive.20260101T000000Z.jsonl",
		filepath.Join(uuid, "subagents", "agent-x.jsonl"),
	} {
		if _, err := os.Stat(filepath.Join(proj, name)); err != nil {
			t.Fatalf("expected %s back in the project dir, got: %v", name, err)
		}
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("expected the store folder to be removed after a restore")
	}
	if _, err := SessionPath(uuid); err != nil {
		t.Errorf("expected SessionPath to resolve again after restore: %v", err)
	}
}

// A restore must never clobber a live transcript. The stub the delete
// path leaves behind is the newer file, so a naive "skip if exists"
// keeps the 7-line stub and silently loses the real history.
func TestRestoreFromStoreKeepsExistingFileAsBackup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "b1a0c9d4-7e6f-4a1b-8c3d-9e0f1a2b3c4d"
	const cwd = "/Users/ivan/gitops/thing"
	jsonl := seedSession(t, root, SanitizeCwd(cwd), uuid, cwd)
	if _, err := MoveTranscriptToStore(jsonl, uuid, StoreTrash, 1735689600000000000); err != nil {
		t.Fatal(err)
	}
	// Grimoire leaves a metadata stub behind after a delete.
	if err := os.WriteFile(jsonl, []byte(`{"type":"ai-title","aiTitle":"stub"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreFromStore(uuid); err != nil {
		t.Fatalf("RestoreFromStore: %v", err)
	}

	got, err := os.ReadFile(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"role":"user"`) {
		t.Error("expected the full transcript to win over the stub")
	}
	if _, err := os.Stat(jsonl + stubBackupSuffix); err != nil {
		t.Errorf("expected the displaced stub kept as a backup: %v", err)
	}
}

// Folders written by the old delete path carry no manifest. They must
// still be restorable, falling back to the cwd recorded inside the
// transcript itself.
func TestRestoreFromStoreFallsBackToTranscriptCwd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "9f8e7d6c-5b4a-4392-8172-0a1b2c3d4e5f"
	const cwd = "/Users/ivan/gitops/legacy"
	trashRoot, err := StoreKindRoot(StoreTrash)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(trashRoot, uuid+"-1735689600000000000")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","cwd":"` + cwd + `","sessionId":"` + uuid + `","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(legacy, uuid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	restored, err := RestoreFromStore(uuid)
	if err != nil {
		t.Fatalf("RestoreFromStore without manifest: %v", err)
	}
	want := filepath.Join(root, SanitizeCwd(cwd), uuid+".jsonl")
	if restored != want {
		t.Errorf("restored to %q, want %q derived from the transcript cwd", restored, want)
	}
}

// ListStore is what search and the UI read to show archived and deleted
// sessions alongside live ones.
func TestListStoreReportsBothKinds(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const archived = "11111111-1111-4111-8111-111111111111"
	const deleted = "22222222-2222-4222-8222-222222222222"
	const cwd = "/Users/ivan/gitops/thing"
	a := seedSession(t, root, SanitizeCwd(cwd), archived, cwd)
	d := seedSession(t, root, SanitizeCwd(cwd), deleted, cwd)
	if _, err := MoveTranscriptToStore(a, archived, StoreArchive, 1735689600000000000); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveTranscriptToStore(d, deleted, StoreTrash, 1735689600000000001); err != nil {
		t.Fatal(err)
	}

	all, err := ListStore("")
	if err != nil {
		t.Fatalf("ListStore: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListStore() returned %d entries, want 2", len(all))
	}
	byID := map[string]StoreEntry{}
	for _, e := range all {
		byID[e.SessionID] = e
	}
	if byID[archived].Kind != StoreArchive {
		t.Errorf("%s kind = %q, want archive", archived, byID[archived].Kind)
	}
	if byID[deleted].Kind != StoreTrash {
		t.Errorf("%s kind = %q, want trash", deleted, byID[deleted].Kind)
	}
	if byID[archived].Cwd != cwd {
		t.Errorf("entry cwd = %q, want %q", byID[archived].Cwd, cwd)
	}
	if byID[archived].SizeBytes == 0 {
		t.Error("entry must carry the transcript size for the UI")
	}

	only, err := ListStore(StoreArchive)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].SessionID != archived {
		t.Fatalf("ListStore(archive) = %+v, want just the archived session", only)
	}
}

// EnsureRestored is what makes "open this session" work straight from a
// search result: a live session is left alone, an archived or deleted
// one is brought back first, because claude --resume cannot see a
// transcript that is not in the project dir.
func TestEnsureRestoredBringsBackStoredSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "7c9a1b2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"
	const cwd = "/Users/ivan/gitops/thing"
	origin := seedSession(t, root, SanitizeCwd(cwd), uuid, cwd)
	if _, err := MoveTranscriptToStore(origin, uuid, StoreArchive, 1735689600000000000); err != nil {
		t.Fatal(err)
	}

	path, restored, err := EnsureRestored(uuid)
	if err != nil {
		t.Fatalf("EnsureRestored: %v", err)
	}
	if !restored {
		t.Error("expected the archived session to be reported as restored")
	}
	if path != origin {
		t.Errorf("path = %q, want %q", path, origin)
	}

	// Second call is a no-op: the session is live now.
	path, restored, err = EnsureRestored(uuid)
	if err != nil {
		t.Fatalf("EnsureRestored on a live session: %v", err)
	}
	if restored {
		t.Error("a live session must not be reported as restored")
	}
	if path != origin {
		t.Errorf("path = %q, want %q", path, origin)
	}
}

// A session that exists nowhere is an error, not a silent empty path.
func TestEnsureRestoredUnknownSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	if _, _, err := EnsureRestored("00000000-0000-4000-8000-00000000dead"); err == nil {
		t.Fatal("expected an error for a session that is neither live nor stored")
	}
}

// Sessions deleted by older builds live in the pre-store trash dir.
// They must stay visible and restorable, otherwise upgrading the app
// silently hides everything that was deleted before it.
func TestListStoreIncludesLegacyTrash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "3f2a1b0c-9d8e-4f7a-8b6c-5d4e3f2a1b0c"
	const cwd = "/Users/ivan/gitops/legacy"
	legacyRoot, err := LegacyTrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(legacyRoot, uuid+"-1735689600000000000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","cwd":"` + cwd + `","sessionId":"` + uuid + `","message":{"role":"user","content":"legacy"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, uuid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ListStore("")
	if err != nil {
		t.Fatalf("ListStore: %v", err)
	}
	if len(entries) != 1 || entries[0].SessionID != uuid {
		t.Fatalf("ListStore() = %+v, want the legacy session %s", entries, uuid)
	}
	if entries[0].Kind != StoreTrash {
		t.Errorf("legacy entry kind = %q, want trash", entries[0].Kind)
	}

	restored, err := RestoreFromStore(uuid)
	if err != nil {
		t.Fatalf("RestoreFromStore from legacy trash: %v", err)
	}
	if want := filepath.Join(root, SanitizeCwd(cwd), uuid+".jsonl"); restored != want {
		t.Errorf("restored to %q, want %q", restored, want)
	}
}
