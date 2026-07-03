package discovery

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestImportTranscript_PicksCwdFromContent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	jsonl := strings.Join([]string{
		`{"type":"permission-mode","mode":"bypassPermissions"}`,
		`{"type":"user","cwd":"/Users/me/proj","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`,
	}, "\n") + "\n"

	res, err := ImportTranscript(strings.NewReader(jsonl), "from-friend.jsonl")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Cwd != "/Users/me/proj" {
		t.Errorf("expected cwd from JSONL, got %q", res.Cwd)
	}
	if res.Messages != 2 {
		t.Errorf("expected 2 messages, got %d", res.Messages)
	}
	expectedDir := filepath.Join(root, "-Users-me-proj")
	if _, err := os.Stat(expectedDir); err != nil {
		t.Errorf("expected dir %s to exist: %v", expectedDir, err)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Errorf("expected file %s to exist: %v", res.Path, err)
	}
}

func TestImportTranscript_FallsBackToImportedBucket(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	// No cwd anywhere → "-imported" bucket.
	jsonl := `{"type":"user","message":{"role":"user","content":"orphan"}}` + "\n"

	res, err := ImportTranscript(strings.NewReader(jsonl), "orphan.jsonl")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Cwd != "" {
		t.Errorf("expected empty cwd, got %q", res.Cwd)
	}
	expectedDir := filepath.Join(root, "-imported")
	if _, err := os.Stat(expectedDir); err != nil {
		t.Errorf("expected %s to exist", expectedDir)
	}
}

func TestImportTranscript_AssignsFreshUUIDEvenForUUIDFilename(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	jsonl := `{"type":"user","cwd":"/x","message":{"role":"user","content":"x"}}` + "\n"
	uuid := "11111111-2222-3333-4444-555555555555"

	res, err := ImportTranscript(strings.NewReader(jsonl), uuid+".jsonl")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// Imports MUST get a fresh UUID, never the source filename's UUID:
	// re-importing the same file (or importing a transcript that already
	// has a live native session with that UUID) used to overwrite live
	// state. Fresh UUID is the safety guarantee.
	if res.SessionID == uuid {
		t.Errorf("expected fresh UUID, got source filename's UUID %q", res.SessionID)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(res.SessionID) {
		t.Errorf("expected canonical UUID, got %q", res.SessionID)
	}
}

func TestImportTranscript_GeneratesUUIDForNonUUIDFilename(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	jsonl := `{"type":"user","cwd":"/x","message":{"role":"user","content":"x"}}` + "\n"

	res, err := ImportTranscript(strings.NewReader(jsonl), "my-chat-yesterday.jsonl")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(res.SessionID) {
		t.Errorf("expected generated UUID, got %q", res.SessionID)
	}
}

func TestImportTranscript_RejectsInvalidJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	_, err := ImportTranscript(strings.NewReader("<html>not jsonl</html>\n"), "bad.jsonl")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestImportTranscript_RejectsMetadataOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	// All metadata events, no user/assistant — useless to import.
	jsonl := strings.Join([]string{
		`{"type":"permission-mode"}`,
		`{"type":"ai-title","aiTitle":"foo"}`,
	}, "\n") + "\n"
	_, err := ImportTranscript(strings.NewReader(jsonl), "meta.jsonl")
	if err == nil {
		t.Fatal("expected error for metadata-only JSONL")
	}
}

func TestImportTranscript_ReimportProducesDistinctSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	jsonl := `{"type":"user","cwd":"/x","message":{"role":"user","content":"x"}}` + "\n"
	uuid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	res1, err := ImportTranscript(strings.NewReader(jsonl), uuid+".jsonl")
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	res2, err := ImportTranscript(strings.NewReader(jsonl), uuid+".jsonl")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	// Each import gets a fresh UUID, so paths must differ even when the
	// source filename is identical — re-importing must not clobber the
	// earlier copy.
	if res1.Path == res2.Path {
		t.Errorf("expected distinct paths for re-imports, both went to %s", res1.Path)
	}
	if res1.SessionID == res2.SessionID {
		t.Errorf("expected distinct session ids, both got %s", res1.SessionID)
	}
}
