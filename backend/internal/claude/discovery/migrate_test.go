package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func legacyBundle(t *testing.T, legacyRoot, uuid, cwd string) string {
	t.Helper()
	dir := filepath.Join(legacyRoot, uuid+"-1735689600000000000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","cwd":"` + cwd + `","sessionId":"` + uuid + `","message":{"role":"user","content":"old"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, uuid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Folders deleted by older builds sit in a second directory. Migration
// folds them into the trash shelf of the central store so there is one
// place to search, and backfills the manifest they were written without.
func TestMigrateLegacyTrash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	legacyRoot, err := LegacyTrashRoot()
	if err != nil {
		t.Fatal(err)
	}
	const uuid = "5a4b3c2d-1e0f-4a9b-8c7d-6e5f4a3b2c1d"
	const cwd = "/Users/ivan/gitops/legacy"
	legacyDir := legacyBundle(t, legacyRoot, uuid, cwd)

	moved, err := MigrateLegacyTrash()
	if err != nil {
		t.Fatalf("MigrateLegacyTrash: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved = %d, want 1", moved)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Error("expected the legacy folder to be gone after migration")
	}

	shelf, err := StoreKindRoot(StoreTrash)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(shelf, filepath.Base(legacyDir))
	if _, err := os.Stat(filepath.Join(dest, uuid+".jsonl")); err != nil {
		t.Fatalf("expected the bundle on the trash shelf: %v", err)
	}
	m, err := ReadManifest(dest)
	if err != nil {
		t.Fatalf("expected a backfilled manifest: %v", err)
	}
	if m.SessionID != uuid || m.Kind != StoreTrash {
		t.Errorf("manifest = %+v, want session %s on the trash shelf", m, uuid)
	}
	if m.Cwd != cwd {
		t.Errorf("manifest cwd = %q, want %q read back from the transcript", m.Cwd, cwd)
	}
}

// Running twice must be safe: the second pass has nothing to do and
// must not touch what the first one moved.
func TestMigrateLegacyTrashIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	legacyRoot, _ := LegacyTrashRoot()
	legacyBundle(t, legacyRoot, "6b5c4d3e-2f1a-4b0c-9d8e-7f6a5b4c3d2e", "/Users/ivan/gitops/legacy")

	if _, err := MigrateLegacyTrash(); err != nil {
		t.Fatal(err)
	}
	before, err := ListStore(StoreTrash)
	if err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateLegacyTrash()
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if moved != 0 {
		t.Errorf("second pass moved %d folders, want 0", moved)
	}
	after, err := ListStore(StoreTrash)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("shelf holds %d entries after a second pass, want %d", len(after), len(before))
	}
}

// A name collision on the shelf must never overwrite what is already
// there — the legacy folder is left in place for a human to look at.
func TestMigrateLegacyTrashSkipsCollisions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "7c6d5e4f-3a2b-4c1d-8e9f-0a1b2c3d4e5f"
	legacyRoot, _ := LegacyTrashRoot()
	legacyDir := legacyBundle(t, legacyRoot, uuid, "/Users/ivan/gitops/legacy")

	shelf, _ := StoreKindRoot(StoreTrash)
	occupied := filepath.Join(shelf, filepath.Base(legacyDir))
	if err := os.MkdirAll(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(occupied, "keep.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateLegacyTrash()
	if err != nil {
		t.Fatalf("MigrateLegacyTrash: %v", err)
	}
	if moved != 0 {
		t.Errorf("moved = %d, want 0 for a colliding folder", moved)
	}
	if _, err := os.Stat(filepath.Join(occupied, "keep.txt")); err != nil {
		t.Error("expected the existing shelf folder untouched")
	}
	if _, err := os.Stat(legacyDir); err != nil {
		t.Error("expected the legacy folder left in place when it cannot move")
	}
}
