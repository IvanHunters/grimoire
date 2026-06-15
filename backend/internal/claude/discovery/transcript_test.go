package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTranscript_FiltersMetadataEvents(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "abc-def-1234.jsonl")
	content := strings.Join([]string{
		// All of these should be filtered out — they're metadata, not chat.
		`{"type":"permission-mode","mode":"bypassPermissions"}`,
		`{"type":"ai-title","aiTitle":"My Title"}`,
		`{"type":"attachment","content":"some attachment"}`,
		`{"type":"file-history-snapshot","content":"snapshot data"}`,
		`{"type":"last-prompt","content":"last prompt"}`,
		`{"type":"mode","mode":"normal"}`,
		// These survive.
		`{"type":"user","uuid":"u1","timestamp":"2026-06-10T12:00:00Z","cwd":"/x","gitBranch":"main","version":"2.1.153","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-06-10T12:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"hi there"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tr, err := ReadTranscript(path)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(tr.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(tr.Messages))
	}
	if tr.Messages[0].Role != "user" || tr.Messages[0].Text != "hello" {
		t.Errorf("first message wrong: %+v", tr.Messages[0])
	}
	if tr.Messages[1].Role != "assistant" || tr.Messages[1].Text != "hi there" {
		t.Errorf("second message wrong: %+v", tr.Messages[1])
	}
	if tr.Header.Name != "My Title" {
		t.Errorf("header Name from ai-title: got %q", tr.Header.Name)
	}
	if tr.Header.Cwd != "/x" {
		t.Errorf("header Cwd: got %q", tr.Header.Cwd)
	}
	if tr.Header.GitBranch != "main" {
		t.Errorf("header GitBranch: got %q", tr.Header.GitBranch)
	}
	if tr.Header.MessageCount != 2 {
		t.Errorf("header MessageCount: got %d", tr.Header.MessageCount)
	}
}

func TestReadTranscript_CollectsToolNames(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "tools.jsonl")
	content := `{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"let me check"},` +
		`{"type":"tool_use","name":"Bash","input":{}},` +
		`{"type":"tool_use","name":"Read","input":{}}` +
		`]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tr, err := ReadTranscript(path)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	if len(tr.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(tr.Messages))
	}
	m := tr.Messages[0]
	if !m.HasTools {
		t.Error("HasTools should be true")
	}
	if len(m.ToolUses) != 2 || m.ToolUses[0] != "Bash" || m.ToolUses[1] != "Read" {
		t.Errorf("ToolUses: got %v", m.ToolUses)
	}
}

func TestReadTranscript_HandlesToolResultErrorFlag(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "err.jsonl")
	content := `{"type":"user","uuid":"u1","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"x","content":"command failed","is_error":true}` +
		`]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tr, err := ReadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(tr.Messages))
	}
	if !tr.Messages[0].IsError {
		t.Error("expected IsError=true for failed tool_result")
	}
	if tr.Messages[0].Text != "command failed" {
		t.Errorf("text: got %q", tr.Messages[0].Text)
	}
}

func TestReadTranscript_HandlesArrayContentToolResult(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "arr.jsonl")
	// Some tool_result.content fields are array of {type:"text",text}
	// rather than a bare string. Extractor must handle both.
	content := `{"type":"user","uuid":"u1","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"x","content":[{"type":"text","text":"output line 1"},{"type":"text","text":"line 2"}]}` +
		`]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tr, err := ReadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(tr.Messages))
	}
	if !strings.Contains(tr.Messages[0].Text, "output line 1") ||
		!strings.Contains(tr.Messages[0].Text, "line 2") {
		t.Errorf("missing array-content text: %q", tr.Messages[0].Text)
	}
}

func TestReadTranscript_LineNumbersAreSourceJSONLLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "lines.jsonl")
	content := strings.Join([]string{
		`{"type":"permission-mode"}`,                                                                 // line 1 (skipped)
		`{"type":"ai-title","aiTitle":"T"}`,                                                          // line 2 (skipped)
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"q"}}`,                        // line 3
		`{"type":"attachment"}`,                                                                      // line 4 (skipped)
		`{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"text","text":"a"}]}}`, // line 5
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tr, err := ReadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(tr.Messages))
	}
	if tr.Messages[0].LineNumber != 3 {
		t.Errorf("first message line: want 3, got %d", tr.Messages[0].LineNumber)
	}
	if tr.Messages[1].LineNumber != 5 {
		t.Errorf("second message line: want 5, got %d", tr.Messages[1].LineNumber)
	}
}

func TestSessionPath_FindsTranscriptInProjectsTree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	subdir := filepath.Join(root, "-some-cwd")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(subdir, "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(target, []byte(`{"type":"user"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := SessionPath("11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("SessionPath: %v", err)
	}
	if got != target {
		t.Errorf("want %s, got %s", target, got)
	}
}

func TestSessionPath_MissingSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)
	_, err := SessionPath("does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}
