package api

import "github.com/ivanohotnikov/markdown-editor/internal/claude/compact"

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

// resolve layers the request on top of compact.DefaultOptions, the
// setting set shared with the compact_my_session MCP tool. Only fields
// the caller actually sent are overridden, so a plain "Compact" click
// (empty body) gets exactly the shared defaults.
func (r compactRequest) resolve() compactSettings {
	opts := compact.DefaultOptions()
	opts.KeepRecentToolResults = r.KeepRecentToolResults
	opts.MaxStubBytes = r.MaxStubBytes
	opts.KeepRecentAttachments = r.KeepRecentAttachments

	for _, o := range []struct {
		v   *bool
		dst *bool
	}{
		{r.DropToolUseResultMirror, &opts.DropToolUseResultMirror},
		{r.DropFileHistorySnapshots, &opts.DropFileHistorySnapshots},
		{r.DropMetaSidecar, &opts.DropMetaSidecar},
		{r.DropThinking, &opts.DropThinking},
		{r.RecompressStubs, &opts.RecompressStubs},
		{r.DropUsage, &opts.DropUsage},
	} {
		if o.v != nil {
			*o.dst = *o.v
		}
	}
	if r.RestubTailBytes != nil {
		opts.RestubTailBytes = *r.RestubTailBytes
	}

	generateLedger := true
	if r.GenerateLedger != nil {
		generateLedger = *r.GenerateLedger
	}
	return compactSettings{Options: opts, GenerateLedger: generateLedger}
}
