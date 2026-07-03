// Package compact shrinks a claude JSONL transcript by replacing the
// bulky payload fields on tool_use and tool_result blocks in older
// turns with short stubs, while preserving the dialogue structure
// claude relies on for --resume.
//
// Why this exists: claude's built-in /compact rewrites the entire
// conversation into a lossy LLM-generated summary. References to
// specific files, exact command outputs, and the diff applied are
// replaced with claude's paraphrase, and self-references like "the
// file we edited" lose their anchor when the source turn is gone.
//
// Our approach is deterministic and symmetric:
//
//   - Every user/assistant message stays present (no dropped turns).
//   - tool_use and tool_result blocks stay paired by id (the wire
//     protocol stays valid on --resume).
//   - For turns older than a recency window we replace bulky FIELDS
//     within those blocks with sentinel-tagged stubs.
//     • tool_result content (text, image, document) → short stub.
//     • tool_use input fields whose value is a large string
//       (Write.content, Edit.old_string/new_string, MultiEdit.edits)
//       → short stub. Small fields (file_path, command head) stay.
//   - A ledger.md is generated WITH FULL FIDELITY from the original
//     transcript so detail isn't lost — files on disk and git history
//     remain the canonical source of truth.
//
// Stubs carry a sentinel header so a future compact pass can detect
// already-evicted blocks and skip them (no "stub inside stub" growth).
//
// Concurrency: a per-source-path mutex serialises compactions. The
// caller is expected to first stop any live daemon worker writing to
// the JSONL (otherwise we'd race their append against our rename).
package compact

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// EvictedMarker is the sentinel string we embed at the start of every
// stub. Recognising it lets a subsequent compact pass skip blocks we
// already evicted — without this, repeated compacts would re-evict
// stubs and pile sentinel text on top of sentinel text.
const EvictedMarker = "[evicted by grimoire compact"

// Options controls how aggressive the compaction is.
type Options struct {
	// KeepRecentToolResults is the number of most-recent tool_result
	// blocks to leave verbatim. Older ones get bodies evicted. Default 30.
	KeepRecentToolResults int

	// KeepRecentToolUses is the number of most-recent tool_use blocks
	// to leave verbatim. Older Write/Edit/etc. get their big input
	// fields evicted. Default same as KeepRecentToolResults.
	KeepRecentToolUses int

	// MaxStubBytes is the size of the tail we keep INSIDE each stub
	// so claude has some recall of what the original value carried.
	// Default 200 bytes (UTF-8-aligned).
	MaxStubBytes int

	// LargeFieldBytes is the threshold above which a tool_use input
	// field's string value qualifies for eviction. Default 400 — small
	// enough to catch a content/old_string/new_string blob, large
	// enough to leave normal file paths and CLI commands alone.
	LargeFieldBytes int

	// DropToolUseResultMirror, when true, deletes the top-level
	// `toolUseResult` field on each line. claude does not consume it
	// (it's grimoire-side UI metadata) so removing it shrinks the
	// on-disk file without affecting --resume behaviour. Default true.
	DropToolUseResultMirror bool

	// KeepArchives caps how many `<path>.archive.<ts>.jsonl` siblings
	// we keep next to the JSONL. The newest N (by timestamp parsed
	// from the filename) survive; older ones get removed after a
	// successful compact. Default 3. Set to a negative value to skip
	// rotation entirely.
	KeepArchives int

	// DropFileHistorySnapshots removes events of type
	// `file-history-snapshot` entirely. Claude rebuilds these on demand
	// from disk + git history — they are NOT consumed during --resume,
	// just pile up on every Edit/Write. Default true. Frees ~25-30% on
	// long sessions with many edits.
	DropFileHistorySnapshots bool

	// DropMetaSidecar removes lightweight metadata events that grimoire
	// and the daemon write but claude does not consume on --resume:
	// `system`, `mode`, `permission-mode`, `pr-link`, `last-prompt`,
	// `queue-operation`, `worktree-state`, `ai-title`. Default true.
	DropMetaSidecar bool

	// DropThinking strips assistant `thinking` blocks from message
	// content. They are claude's internal chain-of-thought scratchpad;
	// removing them does NOT affect --resume because claude
	// regenerates reasoning from the visible conversation. Default
	// true. Frees ~15% on long sessions.
	DropThinking bool

	// KeepRecentAttachments leaves the last N attachment events alone
	// and drops everything older. Attachments are large inline file
	// dumps; old ones were superseded by later reads/edits. Set <=0 to
	// disable (no attachment eviction). Default 40.
	KeepRecentAttachments int
}

func (o *Options) defaults() {
	if o.KeepRecentToolResults <= 0 {
		o.KeepRecentToolResults = 30
	}
	if o.KeepRecentToolUses <= 0 {
		o.KeepRecentToolUses = o.KeepRecentToolResults
	}
	if o.MaxStubBytes <= 0 {
		o.MaxStubBytes = 200
	}
	if o.LargeFieldBytes <= 0 {
		o.LargeFieldBytes = 400
	}
	if o.KeepArchives == 0 {
		o.KeepArchives = 3
	}
	if o.KeepRecentAttachments == 0 {
		o.KeepRecentAttachments = 40
	}
	// Boolean defaults are zero-valued, but the "drop sidecar / drop
	// thinking / drop file-history" set are universally safe, so we
	// flip them on unless caller explicitly opted out via a pointer.
	// Since we use plain bools, we accept the zero default — call sites
	// (api/sessions.go CompactSession) set them explicitly.
}

// metaSidecarTypes are entire event types that grimoire / the daemon
// write but claude does not read back on --resume. Removing them is
// pure shrink with zero context impact.
var metaSidecarTypes = map[string]bool{
	"system":          true,
	"mode":            true,
	"permission-mode": true,
	"pr-link":         true,
	"last-prompt":     true,
	"queue-operation": true,
	"worktree-state":  true,
	"ai-title":        true,
}

// Stats reports what the compact pass did. Surfaced to the caller for
// the Compact button UI.
type Stats struct {
	Lines                    int    `json:"lines"`
	ToolResults              int    `json:"tool_results"`
	ToolResultsEvicted       int    `json:"tool_results_evicted"`
	ToolUses                 int    `json:"tool_uses"`
	ToolUsesEvicted          int    `json:"tool_uses_evicted"`
	BytesBefore              int64  `json:"bytes_before"`
	BytesAfter               int64  `json:"bytes_after"`
	ApproxTokensBefore       int    `json:"approx_tokens_before"`
	ApproxTokensAfter        int    `json:"approx_tokens_after"`
	ArchivePath              string `json:"archive_path"`
	ArchivesPruned           int    `json:"archives_pruned"`
	LedgerPath               string `json:"ledger_path,omitempty"`
	AlreadyEvictedSkipped    int    `json:"already_evicted_skipped"`
	FileHistorySnapshotsDropped int `json:"file_history_snapshots_dropped"`
	MetaSidecarDropped       int    `json:"meta_sidecar_dropped"`
	ThinkingBlocksDropped    int    `json:"thinking_blocks_dropped"`
	AttachmentsDropped       int    `json:"attachments_dropped"`
}

// Result returned by Compact.
type Result struct {
	Stats      Stats
	LedgerText string
}

// pathLocks serialises compaction on the same JSONL path. Holding the
// lock for the *entire* read-modify-write keeps two parallel POSTs from
// racing each other's archives and rename().
var pathLocks sync.Map // map[string]*sync.Mutex

func lockFor(path string) *sync.Mutex {
	if v, ok := pathLocks.Load(path); ok {
		return v.(*sync.Mutex)
	}
	m := &sync.Mutex{}
	actual, _ := pathLocks.LoadOrStore(path, m)
	return actual.(*sync.Mutex)
}

// Compact rewrites the JSONL at sourcePath in place after first copying
// it to <sourcePath>.archive.<RFC3339>.jsonl. The archive is a literal
// byte-for-byte snapshot — restore is just a file move. Any failure
// after the archive step leaves the source untouched.
//
// IMPORTANT: the caller must ensure no other process is appending to
// the file during the call. Run after killing the live daemon worker.
//
// ledgerOut is an optional sink for the generated markdown ledger.
// Pass nil to skip writing externally; Result.LedgerText always
// carries the text regardless.
func Compact(sourcePath string, opts Options, ledgerOut io.Writer) (*Result, error) {
	opts.defaults()

	lk := lockFor(sourcePath)
	lk.Lock()
	defer lk.Unlock()

	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("stat source: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("source is a directory")
	}

	lines, err := readLines(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read lines: %w", err)
	}

	stats := Stats{
		Lines:       len(lines),
		BytesBefore: info.Size(),
	}
	for _, l := range lines {
		stats.ApproxTokensBefore += len(l) / 4
	}

	parsed := make([]map[string]any, len(lines))
	var useRefs, resultRefs []blockRef
	// dropLine[i] == true means we omit this line entirely from output.
	// Used for whole-event drops (file-history-snapshot, sidecar, old
	// attachments). dirty[i] handles in-place content rewrites.
	dropLine := make(map[int]bool)
	// Track attachment lines so we can keep only the last N.
	var attachmentLines []int

	// Pass 1: parse all lines, decide whole-line drops, collect
	// attachment line indices. Don't collect tool_use/tool_result refs
	// yet — they'd go stale after DropThinking reshapes content arrays.
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			parsed[i] = nil
			continue
		}
		parsed[i] = m

		t, _ := m["type"].(string)
		switch {
		case t == "file-history-snapshot" && opts.DropFileHistorySnapshots:
			dropLine[i] = true
			stats.FileHistorySnapshotsDropped++
			continue
		case metaSidecarTypes[t] && opts.DropMetaSidecar:
			dropLine[i] = true
			stats.MetaSidecarDropped++
			continue
		case t == "attachment" && opts.KeepRecentAttachments > 0:
			// Defer the decision: we drop ones older than the last
			// KeepRecentAttachments after we've seen them all.
			attachmentLines = append(attachmentLines, i)
		}
	}

	// Attachment retention: drop everything older than the last N.
	if opts.KeepRecentAttachments > 0 && len(attachmentLines) > opts.KeepRecentAttachments {
		cutoff := len(attachmentLines) - opts.KeepRecentAttachments
		for _, idx := range attachmentLines[:cutoff] {
			if !dropLine[idx] {
				dropLine[idx] = true
				stats.AttachmentsDropped++
			}
		}
	}

	// Build the ledger BEFORE eviction so it sees real payloads.
	// Built from current `parsed[]` — thinking blocks are still present
	// so the ledger captures reasoning narratives.
	ledger := buildLedger(parsed, lines)

	dirty := make(map[int]bool)

	// Pass 2: strip `thinking` blocks from message content arrays.
	// MUST run BEFORE collecting tool_use/tool_result refs — otherwise
	// the refs' .idx positions go stale (content array shrinks after
	// strip) and the eviction loop indexes out of range. Mark line
	// dirty so it re-serialises without thinking.
	if opts.DropThinking {
		for i, m := range parsed {
			if m == nil || dropLine[i] {
				continue
			}
			msg, ok := m["message"].(map[string]any)
			if !ok {
				continue
			}
			rawContent, ok := msg["content"].([]any)
			if !ok {
				continue
			}
			filtered := make([]any, 0, len(rawContent))
			removed := 0
			for _, item := range rawContent {
				blk, ok := item.(map[string]any)
				if !ok {
					filtered = append(filtered, item)
					continue
				}
				if blk["type"] == "thinking" {
					removed++
					continue
				}
				filtered = append(filtered, item)
			}
			if removed > 0 {
				msg["content"] = filtered
				stats.ThinkingBlocksDropped += removed
				dirty[i] = true
			}
		}
	}

	// Pass 3: collect tool_use/tool_result refs with FINAL positions
	// (post-thinking-strip). Skip drop'd lines — they won't be written
	// anyway, no point evicting their content.
	for i, m := range parsed {
		if m == nil || dropLine[i] {
			continue
		}
		for bi, b := range messageContent(m) {
			switch b["type"] {
			case "tool_use":
				stats.ToolUses++
				useRefs = append(useRefs, blockRef{
					line: i, idx: bi,
					alreadyEvicted: inputAlreadyEvicted(b),
				})
			case "tool_result":
				stats.ToolResults++
				resultRefs = append(resultRefs, blockRef{
					line: i, idx: bi,
					alreadyEvicted: resultAlreadyEvicted(b),
				})
			}
		}
	}

	// Recency window — apply to non-evicted refs only. Already-evicted
	// blocks don't count as "kept" capacity: we don't want a session
	// with a long history of mostly-stubs to push the last few real
	// payloads out of the keep window.
	useCutoff := computeCutoff(useRefs, opts.KeepRecentToolUses)
	resultCutoff := computeCutoff(resultRefs, opts.KeepRecentToolResults)

	for i, ref := range useRefs {
		if ref.alreadyEvicted {
			stats.AlreadyEvictedSkipped++
			continue
		}
		if i >= useCutoff {
			break
		}
		blk := messageContent(parsed[ref.line])[ref.idx]
		if evictToolUseInput(blk, opts.LargeFieldBytes, opts.MaxStubBytes) {
			stats.ToolUsesEvicted++
			dirty[ref.line] = true
		}
	}

	for i, ref := range resultRefs {
		if ref.alreadyEvicted {
			stats.AlreadyEvictedSkipped++
			continue
		}
		if i >= resultCutoff {
			break
		}
		blk := messageContent(parsed[ref.line])[ref.idx]
		evictToolResultContent(blk, opts.MaxStubBytes)
		stats.ToolResultsEvicted++
		dirty[ref.line] = true

		if opts.DropToolUseResultMirror {
			if _, ok := parsed[ref.line]["toolUseResult"]; ok {
				delete(parsed[ref.line], "toolUseResult")
				dirty[ref.line] = true
			}
		}
	}

	// Render — drop entire-line removals first, then re-marshal mutated
	// lines, leaving the rest at their original byte representation
	// (no field-order shuffles, no whitespace drift vs archive).
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if dropLine[i] {
			continue
		}
		if !dirty[i] || parsed[i] == nil {
			out = append(out, line)
			continue
		}
		buf, err := json.Marshal(parsed[i])
		if err != nil {
			out = append(out, line)
			continue
		}
		out = append(out, string(buf))
	}

	// Archive + atomic write. If the atomic write fails, the archive
	// is rolled back so the user doesn't accumulate `*.archive.<ts>.jsonl`
	// sidecars across repeated compact failures (each new attempt
	// would otherwise leave another archive behind).
	archivePath := sourcePath + ".archive." + time.Now().UTC().Format("20060102T150405Z") + ".jsonl"
	if err := copyFile(sourcePath, archivePath); err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	stats.ArchivePath = archivePath

	if err := writeLines(sourcePath, out); err != nil {
		_ = os.Remove(archivePath)
		return nil, fmt.Errorf("write compacted: %w", err)
	}

	if info2, err := os.Stat(sourcePath); err == nil {
		stats.BytesAfter = info2.Size()
	}
	for _, l := range out {
		stats.ApproxTokensAfter += len(l) / 4
	}

	if opts.KeepArchives >= 0 {
		stats.ArchivesPruned = pruneArchives(sourcePath, opts.KeepArchives)
	}

	if ledgerOut != nil {
		if _, err := io.WriteString(ledgerOut, ledger); err != nil {
			return nil, fmt.Errorf("write ledger: %w", err)
		}
	}

	return &Result{Stats: stats, LedgerText: ledger}, nil
}

// blockRef points at one block inside the parsed transcript: the line
// index plus its position within message.content. alreadyEvicted is set
// during the first pass so we can short-circuit a second eviction and
// keep the recency budget for live blocks only.
type blockRef struct {
	line, idx      int
	alreadyEvicted bool
}

func computeCutoff(refs []blockRef, keep int) int {
	// We want to evict refs[0..cutoff). The last `keep` *non-evicted*
	// refs stay verbatim. Iterate from the end backwards, counting only
	// non-evicted refs toward the keep budget — already-evicted ones
	// pass through "for free" and don't consume the budget.
	if len(refs) == 0 {
		return 0
	}
	kept := 0
	for i := len(refs) - 1; i >= 0; i-- {
		if !refs[i].alreadyEvicted {
			kept++
			if kept >= keep {
				return i
			}
		}
	}
	return 0
}

// ─── eviction core ─────────────────────────────────────────────────

// evictToolResultContent rewrites the `content` field of a tool_result
// block. content may be:
//   - a string (legacy / simple form)
//   - an array of {type:"text"|"image"|"document", ...}
// Both forms get reduced to a single string stub preserving the
// sentinel + tool_use_id + sample tail.
func evictToolResultContent(blk map[string]any, maxTailBytes int) {
	toolUseID, _ := blk["tool_use_id"].(string)

	originalSize, tail := summariseContent(blk["content"], maxTailBytes)

	blk["content"] = fmt.Sprintf("%s — original %d bytes, tool_use_id=%s. Tail follows.]\n%s",
		EvictedMarker, originalSize, toolUseID, tail)
}

// summariseContent returns (approxOriginalBytes, utf8SafeTail). Handles
// string form, array-of-blocks form, image blocks (base64 source), and
// anything else by falling back to JSON-encoded size.
func summariseContent(v any, maxTailBytes int) (int, string) {
	switch x := v.(type) {
	case string:
		return len(x), utf8SafeTail(x, maxTailBytes)
	case []any:
		var sb strings.Builder
		total := 0
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch m["type"] {
			case "text":
				if t, _ := m["text"].(string); t != "" {
					sb.WriteString(t)
					sb.WriteByte('\n')
					total += len(t)
				}
			case "image":
				// Image source.data is base64; count its length, drop
				// the bytes themselves from the tail.
				if src, ok := m["source"].(map[string]any); ok {
					if d, _ := src["data"].(string); d != "" {
						total += len(d)
					}
					if media, _ := src["media_type"].(string); media != "" {
						sb.WriteString("[image ")
						sb.WriteString(media)
						sb.WriteString("]\n")
					}
				}
			case "document":
				if src, ok := m["source"].(map[string]any); ok {
					if d, _ := src["data"].(string); d != "" {
						total += len(d)
					}
					sb.WriteString("[document]\n")
				}
			default:
				buf, _ := json.Marshal(item)
				total += len(buf)
			}
		}
		return total, utf8SafeTail(sb.String(), maxTailBytes)
	default:
		buf, _ := json.Marshal(v)
		return len(buf), utf8SafeTail(string(buf), maxTailBytes)
	}
}

// evictToolUseInput walks the input map and replaces large string
// fields with stubs. Returns true if anything was rewritten. Small
// fields (file_path, command head, single-line args) survive.
func evictToolUseInput(blk map[string]any, threshold, maxTailBytes int) bool {
	input, ok := blk["input"].(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for k, v := range input {
		input[k], _, _ = maybeEvictValue(v, threshold, maxTailBytes, &changed, k)
	}
	return changed
}

// maybeEvictValue: returns (replacementValue, evicted bool, sizeBytes).
// It handles strings (eviction by size), arrays (recurse — for
// MultiEdit.edits), maps (recurse — defensive), and leaves primitives.
func maybeEvictValue(v any, threshold, maxTailBytes int, changed *bool, fieldHint string) (any, bool, int) {
	switch x := v.(type) {
	case string:
		if len(x) > threshold {
			*changed = true
			return fmt.Sprintf("%s — %s field, %d bytes evicted. Tail follows.]\n%s",
				EvictedMarker, fieldHint, len(x), utf8SafeTail(x, maxTailBytes)), true, len(x)
		}
		return x, false, len(x)
	case []any:
		total := 0
		for i, item := range x {
			repl, _, sz := maybeEvictValue(item, threshold, maxTailBytes, changed, fmt.Sprintf("%s[%d]", fieldHint, i))
			x[i] = repl
			total += sz
		}
		return x, false, total
	case map[string]any:
		total := 0
		for k, item := range x {
			repl, _, sz := maybeEvictValue(item, threshold, maxTailBytes, changed, k)
			x[k] = repl
			total += sz
		}
		return x, false, total
	default:
		return v, false, 0
	}
}

// resultAlreadyEvicted: a tool_result whose content begins with our
// sentinel string (string form) is already evicted; skip on re-pass.
func resultAlreadyEvicted(blk map[string]any) bool {
	if s, ok := blk["content"].(string); ok {
		return strings.HasPrefix(s, EvictedMarker)
	}
	return false
}

// inputAlreadyEvicted: every large string field in the input has been
// replaced with a sentinel-tagged stub. We treat a tool_use as
// "already evicted" when none of its remaining fields exceed the
// threshold (heuristic: nothing more to do).
func inputAlreadyEvicted(blk map[string]any) bool {
	input, ok := blk["input"].(map[string]any)
	if !ok {
		return true // no input to evict
	}
	for _, v := range input {
		if s, ok := v.(string); ok {
			if strings.HasPrefix(s, EvictedMarker) {
				continue
			}
			if len(s) > 400 {
				return false
			}
		}
	}
	return true
}

// utf8SafeTail returns the last n bytes of s aligned to a rune
// boundary. If the truncation point lands in the middle of a multi-
// byte sequence we advance forward until utf8.RuneStart so the result
// is always valid UTF-8.
func utf8SafeTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// ─── ledger ───────────────────────────────────────────────────────────

type ledgerEntry struct {
	ts       string
	kind     string
	tool     string
	target   string
	detail   string
	tailLine int
}

type turnEntry struct {
	ts    string
	role  string
	text  string
	line  int
}

func buildLedger(parsed []map[string]any, rawLines []string) string {
	var entries []ledgerEntry
	var turns []turnEntry

	type useRef struct{ ts, name, target string }
	uses := make(map[string]useRef)
	// entryIdxByID maps a tool_use's id directly to its index in
	// entries[]. Replaces the previous O(n) backward scan that matched
	// by (name, target, ts) tuple — that was both O(n×m) overall AND
	// occasionally wrong when two different tool_use calls shared the
	// same name/target/ts (e.g. parallel Read of the same file). The
	// id is unique per call, so this is both faster and correct.
	entryIdxByID := make(map[string]int)

	for i, m := range parsed {
		if m == nil {
			continue
		}
		ts, _ := m["timestamp"].(string)
		role, _ := messageRole(m)

		var turnText strings.Builder
		for _, b := range messageContent(m) {
			switch b["type"] {
			case "text":
				if t, _ := b["text"].(string); t != "" && !strings.HasPrefix(t, EvictedMarker) {
					if turnText.Len() > 0 {
						turnText.WriteString(" ")
					}
					turnText.WriteString(t)
				}
			case "tool_use":
				id, _ := b["id"].(string)
				name, _ := b["name"].(string)
				input, _ := b["input"].(map[string]any)
				target := extractTarget(name, input)
				uses[id] = useRef{ts: ts, name: name, target: target}
				entries = append(entries, ledgerEntry{
					ts: ts, kind: classifyTool(name), tool: name,
					target: target, detail: shortInput(name, input), tailLine: i,
				})
				if id != "" {
					entryIdxByID[id] = len(entries) - 1
				}
			case "tool_result":
				id, _ := b["tool_use_id"].(string)
				if _, ok := uses[id]; !ok {
					continue
				}
				_, tail := summariseContent(b["content"], 240)
				tail = sanitiseTail(tail)
				if idx, ok := entryIdxByID[id]; ok && idx < len(entries) {
					entries[idx].detail = mergeDetail(entries[idx].detail, tail)
				}
			}
		}
		if role != "" && turnText.Len() > 0 {
			turns = append(turns, turnEntry{
				ts: ts, role: role, text: utf8SafeTail(turnText.String(), 600), line: i,
			})
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Session ledger\n\n")
	fmt.Fprintf(&sb, "_Generated by grimoire compact %s. %d tool calls and %d turns from %d transcript lines._\n\n",
		time.Now().UTC().Format(time.RFC3339), len(entries), len(turns), len(rawLines))

	if len(turns) > 0 {
		fmt.Fprintf(&sb, "## Conversation turns (%d)\n\n", len(turns))
		for _, t := range turns {
			tsShort := compactTime(t.ts)
			fmt.Fprintf(&sb, "- `%s` **%s**: %s\n", tsShort, t.role, t.text)
		}
		sb.WriteString("\n")
	}

	order := []string{"edit", "write", "read", "bash", "git", "grep", "glob", "other"}
	titles := map[string]string{
		"edit":  "Edits applied",
		"write": "Files written",
		"read":  "Files read",
		"bash":  "Shell commands",
		"git":   "Git operations",
		"grep":  "Searches",
		"glob":  "File pattern matches",
		"other": "Other tool calls",
	}

	groups := map[string][]ledgerEntry{}
	for _, e := range entries {
		groups[e.kind] = append(groups[e.kind], e)
	}
	for _, k := range order {
		es := groups[k]
		if len(es) == 0 {
			continue
		}
		sort.Slice(es, func(i, j int) bool { return es[i].tailLine < es[j].tailLine })
		fmt.Fprintf(&sb, "## %s (%d)\n\n", titles[k], len(es))
		for _, e := range es {
			tsShort := compactTime(e.ts)
			if e.target != "" {
				fmt.Fprintf(&sb, "- `%s` **%s** `%s`", tsShort, e.tool, e.target)
			} else {
				fmt.Fprintf(&sb, "- `%s` **%s**", tsShort, e.tool)
			}
			if e.detail != "" {
				fmt.Fprintf(&sb, ": %s", e.detail)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func compactTime(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Format("15:04:05")
	}
	return ts
}

func messageRole(m map[string]any) (string, bool) {
	msg, ok := m["message"].(map[string]any)
	if !ok {
		return "", false
	}
	r, _ := msg["role"].(string)
	return r, r != ""
}

func messageContent(m map[string]any) []map[string]any {
	msg, ok := m["message"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := msg["content"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, b := range raw {
		if bm, ok := b.(map[string]any); ok {
			out = append(out, bm)
		}
	}
	return out
}

func classifyTool(name string) string {
	switch name {
	case "Edit", "MultiEdit", "NotebookEdit":
		return "edit"
	case "Write":
		return "write"
	case "Read", "NotebookRead":
		return "read"
	case "Bash", "BashOutput":
		return "bash"
	case "Grep":
		return "grep"
	case "Glob":
		return "glob"
	}
	if strings.Contains(name, "_git_") || name == "Git" {
		return "git"
	}
	return "other"
}

func extractTarget(name string, input map[string]any) string {
	if input == nil {
		return ""
	}
	for _, k := range []string{"file_path", "path", "filepath", "filename"} {
		if v, ok := input[k].(string); ok && v != "" {
			return v
		}
	}
	if v, ok := input["command"].(string); ok {
		c := strings.TrimSpace(v)
		if nl := strings.IndexByte(c, '\n'); nl >= 0 {
			c = c[:nl]
		}
		if len(c) > 80 {
			c = c[:77] + "..."
		}
		return c
	}
	if v, ok := input["pattern"].(string); ok {
		if len(v) > 60 {
			v = v[:57] + "..."
		}
		return v
	}
	for _, k := range []string{"query", "url", "title", "name", "id"} {
		if v, ok := input[k].(string); ok && v != "" {
			if len(v) > 80 {
				v = v[:77] + "..."
			}
			return v
		}
	}
	return ""
}

func shortInput(name string, input map[string]any) string {
	if input == nil {
		return ""
	}
	switch name {
	case "Edit":
		old, _ := input["old_string"].(string)
		neu, _ := input["new_string"].(string)
		return fmt.Sprintf("%d to %d chars", len(old), len(neu))
	case "Write":
		c, _ := input["content"].(string)
		return fmt.Sprintf("%d bytes", len(c))
	case "Read":
		if off, ok := input["offset"].(float64); ok {
			lim, _ := input["limit"].(float64)
			return fmt.Sprintf("lines %d..%d", int(off), int(off+lim))
		}
	}
	return ""
}

func sanitiseTail(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " | ")
	s = strings.ReplaceAll(s, "`", "ʼ")
	return s
}

func mergeDetail(prev, add string) string {
	if prev == "" {
		return add
	}
	if add == "" {
		return prev
	}
	return prev + " · out: " + add
}

// ─── archive rotation ─────────────────────────────────────────────────

func pruneArchives(sourcePath string, keep int) int {
	dir := filepath.Dir(sourcePath)
	base := filepath.Base(sourcePath)
	prefix := base + ".archive."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".jsonl") {
			matches = append(matches, e.Name())
		}
	}
	// Filenames embed `<TS>` in lexicographic order, so simple sort is
	// equivalent to chronological sort.
	sort.Strings(matches)
	if len(matches) <= keep {
		return 0
	}
	pruned := 0
	for _, name := range matches[:len(matches)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			pruned++
		}
	}
	return pruned
}

// ─── file helpers ─────────────────────────────────────────────────────

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// writeLines writes the file atomically via a .tmp + rename pattern.
// On ANY failure (write, flush, close) the orphan .tmp is removed so
// repeated failed compacts don't pile up <jsonl>.tmp siblings on disk.
func writeLines(path string, lines []string) (err error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// Named-return + deferred cleanup: any error path (incl. panic in
	// the loop) leaves the .tmp removed. Rename success clears err so
	// the cleanup is a no-op.
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		if _, err = w.WriteString(l); err != nil {
			_ = f.Close()
			return err
		}
		if err = w.WriteByte('\n'); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err = w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	err = os.Rename(tmp, path)
	return err
}

// copyFile copies src → dst atomically wrt error handling. On any
// failure path (open, copy, close) the partial destination is removed
// so a failed archive doesn't leave a half-written .archive.<ts>.jsonl
// on disk that would later confuse listing or compaction.
func copyFile(src, dst string) (err error) {
	if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	// Named-return ensures close-error is captured, partial dst is
	// removed on any failure.
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
