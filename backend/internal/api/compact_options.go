package api

import "github.com/ivanohotnikov/markdown-editor/internal/claude/compact"

// defaultRestubTailBytes is how much of an already-evicted payload we
// keep when re-compressing a stub written by an older compact pass.
// Enough to recognise what the call returned, far below the ~115-byte
// header those stubs used to carry on their own.
const defaultRestubTailBytes = 40

// compactRequest is the optional JSON body of POST
// /api/sessions/{id}/compact. Pointer fields distinguish "absent" from
// an explicit zero value, which matters for every flag that defaults
// to on and for a tail size of zero (meaning: drop the tail).
type compactRequest struct {
	KeepRecentToolResults    int   `json:"keep_recent_tool_results"`
	MaxStubBytes             int   `json:"max_stub_bytes"`
	DropToolUseResultMirror  *bool `json:"drop_tool_use_result_mirror"`
	GenerateLedger           *bool `json:"generate_ledger"`
	DropFileHistorySnapshots *bool `json:"drop_file_history_snapshots"`
	DropMetaSidecar          *bool `json:"drop_meta_sidecar"`
	DropThinking             *bool `json:"drop_thinking"`
	KeepRecentAttachments    int   `json:"keep_recent_attachments"`
	RecompressStubs          *bool `json:"recompress_stubs"`
	RestubTailBytes          *int  `json:"restub_tail_bytes"`
	DropUsage                *bool `json:"drop_usage"`
}

// compactSettings is the effective configuration for one compact run.
type compactSettings struct {
	Options        compact.Options
	GenerateLedger bool
}

// resolve applies the defaults behind the Compact button.
//
// The drop-* family defaults on: none of it is content claude reads
// back on --resume (file history is rebuilt from disk, the meta
// sidecar is ours, thinking is an internal scratchpad).
//
// RecompressStubs defaults on too, and that is the difference between
// a Compact button that works and one that reports success while
// freeing nothing. Once every tool payload in a session is a stub,
// there is nothing left for ordinary eviction to take, and the stub
// boilerplate itself is what keeps the transcript from resuming.
//
// DropUsage defaults OFF: it never reaches the prompt, so dropping it
// buys file size only, and it is the sole on-disk record of how far
// the context grew.
func (r compactRequest) resolve() compactSettings {
	boolOr := func(v *bool, def bool) bool {
		if v == nil {
			return def
		}
		return *v
	}
	tail := defaultRestubTailBytes
	if r.RestubTailBytes != nil {
		tail = *r.RestubTailBytes
	}
	return compactSettings{
		Options: compact.Options{
			KeepRecentToolResults:    r.KeepRecentToolResults,
			MaxStubBytes:             r.MaxStubBytes,
			KeepRecentAttachments:    r.KeepRecentAttachments,
			DropToolUseResultMirror:  boolOr(r.DropToolUseResultMirror, true),
			DropFileHistorySnapshots: boolOr(r.DropFileHistorySnapshots, true),
			DropMetaSidecar:          boolOr(r.DropMetaSidecar, true),
			DropThinking:             boolOr(r.DropThinking, true),
			RecompressStubs:          boolOr(r.RecompressStubs, true),
			RestubTailBytes:          tail,
			DropUsage:                boolOr(r.DropUsage, false),
		},
		GenerateLedger: boolOr(r.GenerateLedger, true),
	}
}
