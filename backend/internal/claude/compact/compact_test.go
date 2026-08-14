package compact

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// Synthetic transcript covering the patterns Compact must preserve:
//   - ai-title metadata line (no message body)
//   - assistant turn with text + tool_use
//   - user turn with tool_result (long content; must be evicted)
//   - recent user turn with tool_result that must STAY
//   - tool_use_id pairing across line boundaries
//
// Build by hand to keep the test hermetic — no dependency on real
// transcripts.
func writeFixture(t *testing.T, dir string, lines []map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, "fixture.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range lines {
		if err := enc.Encode(m); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	return path
}

func TestCompact_PreservesRecentEvictsOld(t *testing.T) {
	dir := t.TempDir()
	bigOld := strings.Repeat("A", 5000)
	bigRecent := strings.Repeat("B", 5000)

	fixture := []map[string]any{
		{"type": "ai-title", "aiTitle": "test", "sessionId": "s1"},
		{
			"type":      "assistant",
			"uuid":      "u1",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": "reading file"},
					map[string]any{"type": "tool_use", "id": "tool_old", "name": "Read", "input": map[string]any{"file_path": "/a.go"}},
				},
			},
		},
		{
			"type":      "user",
			"uuid":      "u2",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "tool_old", "content": bigOld},
				},
			},
			"toolUseResult": map[string]any{"summary": "huge mirror"},
		},
		{
			"type":      "assistant",
			"uuid":      "u3",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "tool_use", "id": "tool_recent", "name": "Bash", "input": map[string]any{"command": "ls"}},
				},
			},
		},
		{
			"type":      "user",
			"uuid":      "u4",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "tool_recent", "content": bigRecent},
				},
			},
		},
	}

	path := writeFixture(t, dir, fixture)
	var ledger bytes.Buffer
	res, err := Compact(path, Options{KeepRecentToolResults: 1, MaxStubBytes: 50, DropToolUseResultMirror: true}, &ledger)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if res.Stats.ToolResults != 2 {
		t.Errorf("ToolResults: got %d, want 2", res.Stats.ToolResults)
	}
	if res.Stats.ToolResultsEvicted != 1 {
		t.Errorf("ToolResultsEvicted: got %d, want 1", res.Stats.ToolResultsEvicted)
	}
	if res.Stats.BytesAfter >= res.Stats.BytesBefore {
		t.Errorf("BytesAfter (%d) should be < BytesBefore (%d)", res.Stats.BytesAfter, res.Stats.BytesBefore)
	}
	if _, err := os.Stat(res.Stats.ArchivePath); err != nil {
		t.Errorf("archive not created at %s: %v", res.Stats.ArchivePath, err)
	}

	// Re-read compacted file and check invariants.
	out, err := readLines(path)
	if err != nil {
		t.Fatalf("read compacted: %v", err)
	}
	if len(out) != len(fixture) {
		t.Fatalf("line count changed from %d to %d", len(fixture), len(out))
	}

	// Line 2 (old tool_result) — content must be evicted to a stub,
	// tool_use_id intact, and toolUseResult mirror gone.
	var line2 map[string]any
	if err := json.Unmarshal([]byte(out[2]), &line2); err != nil {
		t.Fatalf("unmarshal line2: %v", err)
	}
	if _, ok := line2["toolUseResult"]; ok {
		t.Errorf("toolUseResult should have been dropped on evicted line")
	}
	c := line2["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if c["tool_use_id"] != "tool_old" {
		t.Errorf("tool_use_id lost: %v", c["tool_use_id"])
	}
	body := c["content"].(string)
	if strings.Contains(body, bigOld) {
		t.Errorf("evicted body still contains original payload")
	}
	if !strings.Contains(body, "evicted by grimoire compact") {
		t.Errorf("evicted body missing stub marker: %s", body)
	}

	// Line 4 (recent tool_result) — must be untouched.
	var line4 map[string]any
	if err := json.Unmarshal([]byte(out[4]), &line4); err != nil {
		t.Fatalf("unmarshal line4: %v", err)
	}
	c4 := line4["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if c4["content"].(string) != bigRecent {
		t.Errorf("recent tool_result modified (should stay verbatim)")
	}

	// Line 0 (ai-title) — must be untouched.
	var line0 map[string]any
	if err := json.Unmarshal([]byte(out[0]), &line0); err != nil {
		t.Fatalf("unmarshal line0: %v", err)
	}
	if line0["aiTitle"] != "test" {
		t.Errorf("ai-title line was modified")
	}

	// Ledger sanity: should mention both tool calls by name + target.
	led := ledger.String()
	if !strings.Contains(led, "Read") || !strings.Contains(led, "/a.go") {
		t.Errorf("ledger missing Read entry: %s", led)
	}
	if !strings.Contains(led, "Bash") || !strings.Contains(led, "ls") {
		t.Errorf("ledger missing Bash entry: %s", led)
	}
}

func TestCompact_NoEvictionWhenUnderWindow(t *testing.T) {
	dir := t.TempDir()
	fixture := []map[string]any{
		{
			"type":      "assistant",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "/x"}},
				},
			},
		},
		{
			"type":      "user",
			"sessionId": "s1",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "small"},
				},
			},
		},
	}
	path := writeFixture(t, dir, fixture)
	res, err := Compact(path, Options{KeepRecentToolResults: 10}, nil)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Stats.ToolResultsEvicted != 0 {
		t.Errorf("nothing should be evicted; got %d", res.Stats.ToolResultsEvicted)
	}
}

// Dropping a whole event that sits IN the parentUuid chain must splice
// it out and re-point its child at the nearest surviving ancestor, not
// leave a dangling parentUuid. A broken chain makes claude --resume lose
// the conversation ("session started fresh"). Regression guard for the
// incident where compacting a 35MB session dropped 115 chain links and
// the resumed worker had no context.
func TestCompact_ChainRepairOnDrop(t *testing.T) {
	dir := t.TempDir()
	fixture := []map[string]any{
		{"type": "assistant", "uuid": "A", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "hi"},
		}}},
		// droppable meta event sitting between the two turns
		{"type": "mode", "uuid": "M", "parentUuid": "A", "mode": "default"},
		{"type": "user", "uuid": "U", "parentUuid": "M", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "continue"},
		}}},
	}
	path := writeFixture(t, dir, fixture)

	res, err := Compact(path, Options{DropMetaSidecar: true, KeepRecentToolResults: 10}, nil)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Stats.MetaSidecarDropped != 1 {
		t.Fatalf("expected 1 meta-sidecar dropped, got %d", res.Stats.MetaSidecarDropped)
	}

	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	parentByUUID := map[string]string{}
	for _, line := range lines {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		u, _ := m["uuid"].(string)
		if u != "" {
			ids[u] = true
		}
		if p, ok := m["parentUuid"].(string); ok {
			parentByUUID[u] = p
		}
	}
	if ids["M"] {
		t.Fatal("meta event M must be dropped")
	}
	if parentByUUID["U"] != "A" {
		t.Fatalf("U.parentUuid must be spliced to A (was M), got %q", parentByUUID["U"])
	}
	for u, p := range parentByUUID {
		if p != "" && !ids[p] {
			t.Fatalf("dangling parentUuid %q on %q (chain break)", p, u)
		}
	}
}

// A compact that evicts/drops nothing must be a TRUE no-op: no archive
// sidecar created, source left byte-identical, Stats.NoChange set.
// Regression guard for repeated "Compact" clicks piling up identical
// multi-MB archives on an already-minimal transcript.
func TestCompact_NoOpWhenNothingToEvict(t *testing.T) {
	dir := t.TempDir()
	fixture := []map[string]any{
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "hello, nothing evictable here"},
		}}},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "small"},
		}}},
	}
	path := writeFixture(t, dir, fixture)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Compact(path, Options{KeepRecentToolResults: 10}, nil)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !res.Stats.NoChange {
		t.Errorf("expected NoChange=true on a no-op compact")
	}
	if res.Stats.ArchivePath != "" {
		t.Errorf("no-op compact must not set ArchivePath, got %q", res.Stats.ArchivePath)
	}
	if archives, _ := filepath.Glob(filepath.Join(dir, "*.archive.*")); len(archives) != 0 {
		t.Errorf("no-op compact must not create archive sidecars, found %v", archives)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("no-op compact must leave source byte-identical")
	}
}

// Write tool_use with a large `content` field must be evicted to a
// sentinel-tagged stub, while small fields (file_path) stay verbatim.
// This is the symmetry fix for the original gap where only tool_result
// was shrunk.
func TestCompact_EvictsLargeToolUseInput(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("X", 5000)

	fixture := []map[string]any{
		{
			"type": "assistant", "sessionId": "s1",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use", "id": "t_old", "name": "Write",
						"input": map[string]any{"file_path": "/foo.go", "content": huge},
					},
				},
			},
		},
		{
			"type": "user", "sessionId": "s1",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "t_old", "content": "ok"},
				},
			},
		},
		// recent tool_use that must stay
		{
			"type": "assistant", "sessionId": "s1",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use", "id": "t_recent", "name": "Write",
						"input": map[string]any{"file_path": "/bar.go", "content": huge},
					},
				},
			},
		},
		{
			"type": "user", "sessionId": "s1",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "t_recent", "content": "ok"},
				},
			},
		},
	}
	path := writeFixture(t, dir, fixture)
	res, err := Compact(path, Options{KeepRecentToolUses: 1, LargeFieldBytes: 100}, nil)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Stats.ToolUsesEvicted != 1 {
		t.Errorf("ToolUsesEvicted: got %d, want 1", res.Stats.ToolUsesEvicted)
	}

	out, _ := readLines(path)
	var line0 map[string]any
	if err := json.Unmarshal([]byte(out[0]), &line0); err != nil {
		t.Fatalf("unmarshal line0: %v", err)
	}
	input := line0["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["input"].(map[string]any)
	if input["file_path"] != "/foo.go" {
		t.Errorf("file_path lost: %v", input["file_path"])
	}
	contentStr := input["content"].(string)
	if strings.Contains(contentStr, huge) {
		t.Errorf("content field not evicted, still contains payload")
	}
	if !strings.HasPrefix(contentStr, EvictedMarker) {
		t.Errorf("content stub missing sentinel marker: %s", contentStr)
	}

	// Recent tool_use untouched.
	var line2 map[string]any
	if err := json.Unmarshal([]byte(out[2]), &line2); err != nil {
		t.Fatalf("unmarshal line2: %v", err)
	}
	recentInput := line2["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["input"].(map[string]any)
	if recentInput["content"].(string) != huge {
		t.Errorf("recent tool_use input was modified")
	}
}

// tool_result with an array-of-blocks content (text + image) must
// produce a stub that captures both text and a placeholder for the
// image source data without dragging the base64 along.
func TestCompact_EvictsImageInToolResult(t *testing.T) {
	dir := t.TempDir()
	b64 := strings.Repeat("Z", 4000)

	fixture := []map[string]any{
		// OLD pair with image — should be evicted.
		{
			"type": "assistant", "sessionId": "s1",
			"message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "/img.png"}},
			}},
		},
		{
			"type": "user", "sessionId": "s1",
			"message": map[string]any{"role": "user", "content": []any{
				map[string]any{
					"type": "tool_result", "tool_use_id": "t1",
					"content": []any{
						map[string]any{"type": "text", "text": "screenshot follows"},
						map[string]any{"type": "image", "source": map[string]any{
							"type": "base64", "media_type": "image/png", "data": b64,
						}},
					},
				},
			}},
		},
		// RECENT pair — keeps the window full so the old one gets evicted.
		{
			"type": "assistant", "sessionId": "s1",
			"message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "t2", "name": "Read", "input": map[string]any{"file_path": "/x.txt"}},
			}},
		},
		{
			"type": "user", "sessionId": "s1",
			"message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t2", "content": "ok"},
			}},
		},
	}
	path := writeFixture(t, dir, fixture)
	res, err := Compact(path, Options{KeepRecentToolResults: 1, MaxStubBytes: 60}, nil)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Stats.ToolResultsEvicted != 1 {
		t.Errorf("ToolResultsEvicted: got %d, want 1", res.Stats.ToolResultsEvicted)
	}

	out, _ := readLines(path)
	if strings.Contains(out[1], b64) {
		t.Errorf("base64 payload still present after eviction")
	}
	var line1 map[string]any
	if err := json.Unmarshal([]byte(out[1]), &line1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	body := line1["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.HasPrefix(body, EvictedMarker) {
		t.Errorf("missing sentinel: %s", body)
	}
	// Original size in the stub message should reflect the base64
	// bytes we accounted for, not just the text length.
	if !strings.Contains(body, "4000") && !strings.Contains(body, "original 4") {
		t.Errorf("stub should mention original size including image: %s", body)
	}
}

// Compacting a file that's already been compacted must be a no-op for
// the evicted blocks — no "stub inside stub". Counters reflect that
// nothing was re-evicted.
func TestCompact_IsIdempotentOnEvictedBlocks(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("A", 5000)
	fixture := []map[string]any{
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "/x"}},
		}}},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": big},
		}}},
	}
	// Add a second pair so KeepRecentToolResults=1 has something to keep.
	fixture = append(fixture,
		map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t2", "name": "Read", "input": map[string]any{"file_path": "/y"}},
		}}},
		map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t2", "content": "small"},
		}}},
	)
	path := writeFixture(t, dir, fixture)
	r1, err := Compact(path, Options{KeepRecentToolResults: 1, MaxStubBytes: 50, KeepArchives: 5}, nil)
	if err != nil {
		t.Fatalf("first compact: %v", err)
	}
	if r1.Stats.ToolResultsEvicted != 1 {
		t.Errorf("first pass should evict 1, got %d", r1.Stats.ToolResultsEvicted)
	}
	beforeSize, _ := os.Stat(path)

	r2, err := Compact(path, Options{KeepRecentToolResults: 1, MaxStubBytes: 50, KeepArchives: 5}, nil)
	if err != nil {
		t.Fatalf("second compact: %v", err)
	}
	if r2.Stats.ToolResultsEvicted != 0 {
		t.Errorf("second pass must not re-evict; got %d", r2.Stats.ToolResultsEvicted)
	}
	if r2.Stats.AlreadyEvictedSkipped == 0 {
		t.Errorf("second pass should report AlreadyEvictedSkipped > 0")
	}
	afterSize, _ := os.Stat(path)
	// File should not grow on a no-op compact (sentinel-on-sentinel
	// would have inflated it).
	if afterSize.Size() > beforeSize.Size() {
		t.Errorf("idempotent compact inflated file: %d to %d", beforeSize.Size(), afterSize.Size())
	}
}

// utf8SafeTail must never return a string with a broken multibyte
// prefix. Pick a Russian (Cyrillic) text where every char is 2 bytes,
// then request a tail whose natural cut would land inside a rune.
func TestUTF8SafeTail(t *testing.T) {
	// "Привет!" = П(2) р(2) и(2) в(2) е(2) т(2) !(1) = 13 bytes
	s := "Привет!"
	for n := 1; n <= len(s); n++ {
		got := utf8SafeTail(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("utf8SafeTail(%q, %d) = %q (invalid UTF-8)", s, n, got)
		}
	}
}

// Concurrent Compact calls on the same path must serialise — otherwise
// they'd race on archive creation and rename(). Race detector run will
// catch any unsynchronised mutation.
func TestCompact_PathLockSerialises(t *testing.T) {
	dir := t.TempDir()
	fixture := []map[string]any{
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": "/x"}},
		}}},
		{"type": "user", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": strings.Repeat("Q", 2000)},
		}}},
	}
	path := writeFixture(t, dir, fixture)

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Compact(path, Options{KeepRecentToolResults: 1, KeepArchives: 10}, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent compact returned err: %v", err)
		}
	}
	// File must still be valid JSONL.
	out, err := readLines(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for i, line := range out {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d corrupted by race: %v", i, err)
		}
	}
}

// Archive rotation keeps only the newest N archives. Older ones with
// earlier timestamps in their filename get deleted.
func TestCompact_PrunesOldArchives(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(source, []byte("{\"type\":\"x\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Plant five fake archive siblings with sortable timestamps.
	stamps := []string{"20260101T000001Z", "20260102T000001Z", "20260103T000001Z", "20260104T000001Z", "20260105T000001Z"}
	for _, ts := range stamps {
		fp := source + ".archive." + ts + ".jsonl"
		if err := os.WriteFile(fp, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n := pruneArchives(source, 2)
	if n != 3 {
		t.Errorf("pruned %d, want 3", n)
	}
	matches, _ := filepath.Glob(source + ".archive.*.jsonl")
	if len(matches) != 2 {
		t.Errorf("after prune: %d archives left, want 2", len(matches))
	}
	// The TWO surviving must be the latest timestamps.
	sort.Strings(matches)
	if !strings.Contains(matches[0], "20260104") || !strings.Contains(matches[1], "20260105") {
		t.Errorf("wrong archives survived: %v", matches)
	}
}

// Ledger must include a Conversation turns section with user/assistant
// text — that's the highest-signal content for restoring context after
// a compact, and the original ledger only captured tool calls.
func TestCompact_LedgerIncludesConversationTurns(t *testing.T) {
	dir := t.TempDir()
	fixture := []map[string]any{
		{
			"type": "user", "timestamp": "2026-06-14T10:00:00Z",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Please fix the off-by-one in handler.go"},
				},
			},
		},
		{
			"type": "assistant", "timestamp": "2026-06-14T10:00:05Z",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": "Found it, applying the patch now."},
					map[string]any{"type": "tool_use", "id": "t1", "name": "Edit", "input": map[string]any{
						"file_path": "/handler.go", "old_string": "i <= n", "new_string": "i < n",
					}},
				},
			},
		},
	}
	path := writeFixture(t, dir, fixture)
	var ledger strings.Builder
	if _, err := Compact(path, Options{}, &ledger); err != nil {
		t.Fatal(err)
	}
	led := ledger.String()
	if !strings.Contains(led, "Conversation turns") {
		t.Errorf("ledger missing turns section: %s", led)
	}
	if !strings.Contains(led, "off-by-one") {
		t.Errorf("ledger missing user text: %s", led)
	}
	if !strings.Contains(led, "applying the patch") {
		t.Errorf("ledger missing assistant text: %s", led)
	}
}

func TestCompact_HandlesMalformedLineGracefully(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.jsonl")
	// Mix valid and invalid lines.
	content := `{"type":"ai-title","sessionId":"s1"}
not valid json at all
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + strings.Repeat("X", 1000) + `"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Compact(path, Options{KeepRecentToolResults: 0}, nil)
	if err != nil {
		t.Fatalf("Compact should tolerate malformed lines: %v", err)
	}
	// Confirm the malformed line survived verbatim.
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if !strings.Contains(string(out), "not valid json at all") {
		t.Errorf("malformed line was dropped")
	}
}
