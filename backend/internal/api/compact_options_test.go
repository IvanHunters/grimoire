package api

import (
	"encoding/json"
	"testing"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/compact"
)

// An empty body means "the Compact button was clicked". Those defaults
// decide what that button actually does, so they are asserted here
// rather than left implicit in the handler.
func TestCompactRequest_ResolveDefaults(t *testing.T) {
	var req compactRequest
	got := req.resolve()

	if !got.Options.RecompressStubs {
		t.Errorf("RecompressStubs must default on: a session whose tool output is already stubbed cannot shrink otherwise")
	}
	if got.Options.RestubTailBytes != compact.DefaultRestubTailBytes {
		t.Errorf("RestubTailBytes = %d, want %d", got.Options.RestubTailBytes, compact.DefaultRestubTailBytes)
	}
	if got.Options.DropUsage {
		t.Errorf("DropUsage must default off: it is the only on-disk record of context growth")
	}
	for name, v := range map[string]bool{
		"DropToolUseResultMirror":  got.Options.DropToolUseResultMirror,
		"DropFileHistorySnapshots": got.Options.DropFileHistorySnapshots,
		"DropMetaSidecar":          got.Options.DropMetaSidecar,
		"DropThinking":             got.Options.DropThinking,
	} {
		if !v {
			t.Errorf("%s must default on", name)
		}
	}
	if !got.GenerateLedger {
		t.Errorf("GenerateLedger must default on: the ledger is what makes tail trimming safe")
	}
}

func TestCompactRequest_ResolveHonoursOptOut(t *testing.T) {
	body := []byte(`{"recompress_stubs":false,"drop_thinking":false,"drop_usage":true}`)
	var req compactRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := req.resolve()

	if got.Options.RecompressStubs {
		t.Errorf("RecompressStubs=false was ignored")
	}
	if got.Options.DropThinking {
		t.Errorf("DropThinking=false was ignored")
	}
	if !got.Options.DropUsage {
		t.Errorf("DropUsage=true was ignored")
	}
}

// Zero is a meaningful tail size (drop the tail entirely), so it must
// be distinguishable from "field absent".
func TestCompactRequest_ResolveExplicitZeroTail(t *testing.T) {
	var req compactRequest
	if err := json.Unmarshal([]byte(`{"restub_tail_bytes":0}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := req.resolve(); got.Options.RestubTailBytes != 0 {
		t.Errorf("explicit zero tail became %d", got.Options.RestubTailBytes)
	}

	var absent compactRequest
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := absent.resolve(); got.Options.RestubTailBytes != compact.DefaultRestubTailBytes {
		t.Errorf("absent tail = %d, want %d", got.Options.RestubTailBytes, compact.DefaultRestubTailBytes)
	}
}
