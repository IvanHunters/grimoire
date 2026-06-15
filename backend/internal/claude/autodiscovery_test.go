package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetermineWorkingDir_NoProjectPath_UsesPersistentDir(t *testing.T) {
	// Redirect the persistent dir to a temp location so we don't pollute
	// the developer's real ~/.markdown-editor while testing.
	dataDir := t.TempDir()
	t.Setenv("MARKDOWN_EDITOR_DATA_DIR", dataDir)

	note := &CurrentNote{Folder: "foo", Name: "bar.md"}
	dir, err := DetermineWorkingDir(note, "", "")
	if err != nil {
		t.Fatalf("DetermineWorkingDir: %v", err)
	}

	wantPrefix := filepath.Join(dataDir, "sessions")
	if !strings.HasPrefix(dir, wantPrefix) {
		t.Errorf("expected cwd under %s, got %s", wantPrefix, dir)
	}

	// Directory must exist (mkdir -p) for claude to chdir into it.
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("expected %s to exist as a directory, stat err=%v", dir, err)
	}
}

func TestDetermineWorkingDir_DeterministicForSameNote(t *testing.T) {
	// Same note should produce the same directory across calls — that's
	// how claude finds its old transcripts.
	t.Setenv("MARKDOWN_EDITOR_DATA_DIR", t.TempDir())
	note := &CurrentNote{Folder: "foo", Name: "bar.md"}

	dir1, err := DetermineWorkingDir(note, "", "")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	dir2, err := DetermineWorkingDir(note, "", "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if dir1 != dir2 {
		t.Errorf("non-deterministic cwd: %s vs %s", dir1, dir2)
	}
}

func TestDetermineWorkingDir_ProjectPathWins(t *testing.T) {
	// Note with an explicit ProjectPath should use it, not the persistent
	// fallback. Use an existing dir (the temp dir itself) so the stat passes.
	tmp := t.TempDir()
	t.Setenv("MARKDOWN_EDITOR_DATA_DIR", t.TempDir())

	note := &CurrentNote{Folder: "foo", Name: "bar.md", ProjectPath: tmp}
	dir, err := DetermineWorkingDir(note, "", "")
	if err != nil {
		t.Fatalf("DetermineWorkingDir: %v", err)
	}
	if dir != tmp {
		t.Errorf("expected projectPath %s to win, got %s", tmp, dir)
	}
}

func TestDetermineWorkingDir_FolderProjectPathFallsThroughWhenMissing(t *testing.T) {
	// If folderProjectPath doesn't exist on disk we should fall through
	// to the persistent dir, not the missing path.
	t.Setenv("MARKDOWN_EDITOR_DATA_DIR", t.TempDir())
	note := &CurrentNote{Folder: "foo", Name: "bar.md"}

	dir, err := DetermineWorkingDir(note, "/nonexistent/folder/path", "")
	if err != nil {
		t.Fatalf("DetermineWorkingDir: %v", err)
	}
	if strings.HasPrefix(dir, "/nonexistent") {
		t.Errorf("should not have used nonexistent folder path, got %s", dir)
	}
	if !strings.Contains(dir, "sessions") {
		t.Errorf("expected fall-through to persistent sessions/ dir, got %s", dir)
	}
}
