package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/compact"
	"github.com/spf13/cobra"
)

// The CLI is the only way to run a compaction against a copy of a
// transcript before touching the live one, so its defaults must match
// what the Compact button and the MCP tool do. Anything else would
// make a rehearsal misleading.
func TestCompactFlags_DefaultsMatchSharedOptions(t *testing.T) {
	got := newCompactFlags().options()
	want := compact.DefaultOptions()

	if got.RecompressStubs != want.RecompressStubs {
		t.Errorf("RecompressStubs = %v, want %v", got.RecompressStubs, want.RecompressStubs)
	}
	if got.RestubTailBytes != want.RestubTailBytes {
		t.Errorf("RestubTailBytes = %d, want %d", got.RestubTailBytes, want.RestubTailBytes)
	}
	if got.DropThinking != want.DropThinking {
		t.Errorf("DropThinking = %v, want %v", got.DropThinking, want.DropThinking)
	}
	if got.DropUsage != want.DropUsage {
		t.Errorf("DropUsage = %v, want %v", got.DropUsage, want.DropUsage)
	}
}

// Zero means "keep no tail", not "unset".
func TestCompactFlags_ZeroTailIsHonoured(t *testing.T) {
	f := newCompactFlags()
	f.restubTailBytes = 0
	if got := f.options().RestubTailBytes; got != 0 {
		t.Errorf("RestubTailBytes = %d, want 0", got)
	}
}

func TestCompactFlags_OptOut(t *testing.T) {
	f := newCompactFlags()
	f.recompressStubs = false
	f.dropUsage = true
	opts := f.options()
	if opts.RecompressStubs {
		t.Errorf("recompress opt-out ignored")
	}
	if !opts.DropUsage {
		t.Errorf("drop-usage opt-in ignored")
	}
}

// A typed nil handed to an io.Writer parameter is still a non-nil
// interface, so "no ledger" must be expressed by leaving the interface
// itself nil. Getting this wrong panics instead of skipping the ledger.
func TestRunCompact_WithoutLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	transcript := `{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a"}}]}}
{"type":"user","uuid":"u1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + strings.Repeat("Z", 5000) + `"}]}}
{"type":"assistant","uuid":"a2","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"/b"}}]}}
{"type":"user","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"` + strings.Repeat("Y", 5000) + `"}]}}
`
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	saved := compactFlagValues
	t.Cleanup(func() { compactFlagValues = saved })
	compactFlagValues = newCompactFlags()
	compactFlagValues.ledger = false
	compactFlagValues.keepRecentToolResults = 1

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runCompact(cmd, []string{path}); err != nil {
		t.Fatalf("runCompact: %v", err)
	}
	if _, err := os.Stat(path + ".ledger.md"); err == nil {
		t.Errorf("ledger written despite --ledger=false")
	}
	if !strings.Contains(out.String(), "Compacted") {
		t.Errorf("no summary printed: %s", out.String())
	}
}
