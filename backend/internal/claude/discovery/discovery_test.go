package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureJSONL writes a synthetic transcript to disk. The content is the
// minimum claude-format header (a permission-mode event, an ai-title, and
// the first user message) plus the trailing lines if any.
func fixtureJSONL(t *testing.T, dir, sessionID string, extraLines ...string) string {
	t.Helper()
	path := filepath.Join(dir, sessionID+".jsonl")
	lines := []string{
		`{"type":"permission-mode","mode":"bypassPermissions","sessionId":"` + sessionID + `"}`,
		`{"type":"ai-title","aiTitle":"Test session title","sessionId":"` + sessionID + `"}`,
		`{"type":"user","sessionId":"` + sessionID + `","cwd":"/tmp/fake-cwd","gitBranch":"main","version":"2.1.153","timestamp":"2026-06-10T12:00:00Z","message":{"role":"user","content":"hello world this is the first prompt"}}`,
	}
	lines = append(lines, extraLines...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestSanitizeCwd(t *testing.T) {
	cases := map[string]string{
		"/private/tmp/foo":     "-private-tmp-foo",
		"/Users/me/git/proj":   "-Users-me-git-proj",
		"/tmp/claude-abc":      "-tmp-claude-abc",
		"/":                    "-",
		"relative/path":        "relative-path",
	}
	for input, want := range cases {
		got := SanitizeCwd(input)
		if got != want {
			t.Errorf("SanitizeCwd(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReadHeader_MinimalFixture(t *testing.T) {
	tmp := t.TempDir()
	path := fixtureJSONL(t, tmp, "11111111-2222-3333-4444-555555555555")

	s, err := ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if s.SessionID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("SessionID: got %q", s.SessionID)
	}
	if s.Name != "Test session title" {
		t.Errorf("Name: got %q, want 'Test session title'", s.Name)
	}
	if s.Cwd != "/tmp/fake-cwd" {
		t.Errorf("Cwd: got %q", s.Cwd)
	}
	if s.GitBranch != "main" {
		t.Errorf("GitBranch: got %q", s.GitBranch)
	}
	if s.ClaudeVersion != "2.1.153" {
		t.Errorf("ClaudeVersion: got %q", s.ClaudeVersion)
	}
	if s.FirstPrompt != "hello world this is the first prompt" {
		t.Errorf("FirstPrompt: got %q", s.FirstPrompt)
	}
	if s.StartedAt.IsZero() {
		t.Errorf("StartedAt should be parsed, got zero")
	}
	if s.SizeBytes == 0 {
		t.Errorf("SizeBytes should be > 0")
	}
}

func TestReadHeader_FallsBackToFirstPromptWhenNoAITitle(t *testing.T) {
	tmp := t.TempDir()
	// Same fixture but with no ai-title event.
	path := filepath.Join(tmp, "abc.jsonl")
	content := strings.Join([]string{
		`{"type":"permission-mode","sessionId":"abc"}`,
		`{"type":"user","sessionId":"abc","cwd":"/x","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"a short prompt"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if s.Name != "a short prompt" {
		t.Errorf("expected Name to fall back to first prompt, got %q", s.Name)
	}
}

func TestReadHeader_HandlesUserMessageWithBlockArrayContent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "abc.jsonl")
	// Real claude transcripts often have user messages with array-of-blocks
	// content (tool_result + text). Make sure we don't choke on those.
	content := `{"type":"user","cwd":"/x","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"output"},{"type":"text","text":"the actual user text"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if s.FirstPrompt != "the actual user text" {
		t.Errorf("expected text-block extraction, got %q", s.FirstPrompt)
	}
}

func TestReadHeader_EmptyFileReturnsUnnamed(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := ReadHeader(path)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if s.Name != "(unnamed)" {
		t.Errorf("expected fallback name '(unnamed)', got %q", s.Name)
	}
	if s.SessionID != "empty" {
		t.Errorf("SessionID from filename: got %q", s.SessionID)
	}
}

func TestScanAll_FindsAndSortsByMtime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	subdir := filepath.Join(root, "-tmp-test")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	older := fixtureJSONL(t, subdir, "aaaaaaaa-1111-1111-1111-111111111111")
	newer := fixtureJSONL(t, subdir, "bbbbbbbb-2222-2222-2222-222222222222")

	// Set mtimes explicitly so the sort is deterministic.
	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(older, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	sessions, err := ScanAll(nil)
	if err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != "bbbbbbbb-2222-2222-2222-222222222222" {
		t.Errorf("expected newer session first, got %s first", sessions[0].SessionID)
	}
}

func TestScanCwd_FiltersByDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	// Two projects, one session each.
	wantedDir := filepath.Join(root, "-tmp-wanted")
	otherDir := filepath.Join(root, "-tmp-other")
	os.MkdirAll(wantedDir, 0755)
	os.MkdirAll(otherDir, 0755)
	fixtureJSONL(t, wantedDir, "wanted-uuid-1")
	fixtureJSONL(t, otherDir, "other-uuid-2")

	sessions, err := ScanCwd("/tmp/wanted", nil)
	if err != nil {
		t.Fatalf("ScanCwd: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected exactly 1 matching session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "wanted-uuid-1" {
		t.Errorf("expected wanted-uuid-1, got %s", sessions[0].SessionID)
	}
}

func TestScanCwd_NonexistentCwdReturnsNilNotError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	sessions, err := ScanCwd("/nope/never/existed", nil)
	if err != nil {
		t.Fatalf("expected no error for missing dir, got %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil sessions, got %d", len(sessions))
	}
}

func TestScanAll_SkipCallbackInvokedOnMalformed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	subdir := filepath.Join(root, "-mixed")
	os.MkdirAll(subdir, 0755)
	// One good fixture
	fixtureJSONL(t, subdir, "good-uuid")
	// One unreadable file (perm 0000 on macOS still allows the owner;
	// instead create a directory named .jsonl which can't be opened as file)
	bogus := filepath.Join(subdir, "bogus.jsonl")
	if err := os.Mkdir(bogus, 0755); err != nil {
		t.Fatal(err)
	}

	var skips []string
	sessions, err := ScanAll(func(path string, err error) {
		skips = append(skips, path)
	})
	if err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 valid session, got %d", len(sessions))
	}
	if len(skips) != 1 {
		t.Errorf("expected 1 skip callback, got %d", len(skips))
	}
}
