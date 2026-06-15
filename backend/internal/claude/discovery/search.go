package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SearchHit is one matching JSONL line found by Search.
type SearchHit struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"` // ai-title / first prompt, same source as TranscriptViewer header
	SessionDir  string `json:"sessionDir"`  // sanitized cwd dir name
	Cwd         string `json:"cwd"`         // derived from sessionDir (best-effort)
	Snippet     string `json:"snippet"`     // matched line, truncated
	Role        string `json:"role"`        // "user" / "assistant" / event type
	LineNumber  int    `json:"lineNumber"`
}

// Search scans every JSONL transcript under projectsRoot for `query` and
// returns matching hits. Substring search (case-insensitive), no regex.
// One JSONL line = one potential hit; the function inspects only
// user/assistant message bodies, ignoring metadata events like
// permission-mode or file-history-snapshot which would be noise.
//
// limit caps total hits to avoid returning megabytes when query is very
// common. Pass 0 for "no limit" — caller is responsible for sanity.
// cwdFilter narrows to one cwd (matched against the sanitized dir name);
// pass empty for global search.
//
// ctx is respected — long scans abort cleanly on cancel.
func Search(ctx context.Context, query, cwdFilter string, limit int) ([]SearchHit, error) {
	if query == "" {
		return nil, fmt.Errorf("query required")
	}
	root, err := ProjectsRoot()
	if err != nil {
		return nil, err
	}
	lowerQ := strings.ToLower(query)
	var wanted string
	if cwdFilter != "" {
		wanted = SanitizeCwd(cwdFilter)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", root, err)
	}

	var hits []SearchHit
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if wanted != "" && entry.Name() != wanted {
			continue
		}
		if ctx.Err() != nil {
			return hits, ctx.Err()
		}
		subdir := filepath.Join(root, entry.Name())
		dirHits, err := searchDir(ctx, subdir, entry.Name(), lowerQ, limit-len(hits))
		if err != nil {
			continue // bad file, skip
		}
		hits = append(hits, dirHits...)
		if limit > 0 && len(hits) >= limit {
			hits = hits[:limit]
			break
		}
	}

	// Sort by sessionId (deterministic), but within a session keep
	// line-number order so the snippets read naturally.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].SessionID != hits[j].SessionID {
			return hits[i].SessionID < hits[j].SessionID
		}
		return hits[i].LineNumber < hits[j].LineNumber
	})
	return hits, nil
}

func searchDir(ctx context.Context, dir, sanitizedDir, lowerQ string, capLeft int) ([]SearchHit, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		if ctx.Err() != nil {
			return hits, ctx.Err()
		}
		path := filepath.Join(dir, f.Name())
		sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
		fHits, _ := searchFile(ctx, path, sessionID, sanitizedDir, lowerQ, capLeft-len(hits))
		hits = append(hits, fHits...)
		if capLeft > 0 && len(hits) >= capLeft {
			break
		}
	}
	return hits, nil
}

func searchFile(ctx context.Context, path, sessionID, sanitizedDir, lowerQ string, capLeft int) ([]SearchHit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Lazy session-name lookup: only pay the ReadHeader cost when we
	// actually find a match in this file. Most scanned files have zero
	// hits, so this keeps the common path cheap.
	var sessionName string
	var nameLoaded bool

	var hits []SearchHit
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if ctx.Err() != nil {
			return hits, ctx.Err()
		}
		line := scanner.Bytes()
		// Cheap pre-filter: skip event lines that don't contain the query
		// at all (case-insensitive). Avoids the cost of JSON parsing on
		// the 99% of lines that aren't matches.
		if !containsFold(line, lowerQ) {
			continue
		}
		role, snippet, ok := extractMatchedTextLine(line, lowerQ)
		if !ok {
			continue
		}
		if !nameLoaded {
			// Same source of truth as TranscriptViewer: ReadHeader
			// returns the ai-title (or first-prompt fallback) that
			// TranscriptViewer's header displays. Keeps both views
			// consistent for the same session.
			if hdr, err := ReadHeader(path); err == nil {
				sessionName = hdr.Name
			}
			nameLoaded = true
		}
		hits = append(hits, SearchHit{
			SessionID:   sessionID,
			SessionName: sessionName,
			SessionDir:  sanitizedDir,
			Cwd:         unsanitizeCwd(sanitizedDir),
			Snippet:     snippet,
			Role:        role,
			LineNumber:  lineNum,
		})
		if capLeft > 0 && len(hits) >= capLeft {
			break
		}
	}
	return hits, scanner.Err()
}

// extractMatchedTextLine pulls the textual content from a user/assistant
// JSONL event and confirms the match is in the text body, not metadata.
// Returns role and the trimmed snippet (max ~200 chars centered around
// match). For event types we don't care about (permission-mode, etc),
// returns ok=false.
func extractMatchedTextLine(line []byte, lowerQ string) (role string, snippet string, ok bool) {
	var ev struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return "", "", false
	}
	switch ev.Type {
	case "user", "assistant":
		// fall through
	default:
		return "", "", false
	}

	text := extractMessageText(ev.Message)
	if text == "" {
		return "", "", false
	}
	lowerText := strings.ToLower(text)
	idx := strings.Index(lowerText, lowerQ)
	if idx < 0 {
		return "", "", false
	}
	return ev.Type, snippetAround(text, idx, len(lowerQ), 200), true
}

// extractMessageText handles both user-string and assistant-array
// content shapes, returning a flat string for matching/snippeting.
func extractMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string content (user message form).
	var withStr struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &withStr); err == nil && withStr.Content != "" {
		return withStr.Content
	}
	// Try array of blocks (assistant or user-with-tool-result).
	var withArr struct {
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content string `json:"content"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &withArr); err == nil {
		var parts []string
		for _, b := range withArr.Content {
			switch b.Type {
			case "text":
				if b.Text != "" {
					parts = append(parts, b.Text)
				}
			case "tool_result":
				if b.Content != "" {
					parts = append(parts, b.Content)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// snippetAround returns a window of text centered on a match position.
func snippetAround(text string, matchStart, matchLen, window int) string {
	if window <= 0 || len(text) <= window {
		return strings.TrimSpace(text)
	}
	half := (window - matchLen) / 2
	if half < 0 {
		half = 0
	}
	start := matchStart - half
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(text) {
		end = len(text)
		start = end - window
		if start < 0 {
			start = 0
		}
	}
	out := text[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out = out + "…"
	}
	return strings.TrimSpace(out)
}

// containsFold reports whether b contains needle (case-insensitive),
// without allocating. Used as a cheap pre-filter before JSON parsing.
func containsFold(b []byte, needle string) bool {
	if needle == "" {
		return true
	}
	if len(b) < len(needle) {
		return false
	}
	// Convert b to lowercase in a small reusable buf? Skip — strings.ToLower
	// on a 64KB line is fast enough that simplicity wins here.
	return strings.Contains(strings.ToLower(string(b)), needle)
}

// unsanitizeCwd is the inverse of SanitizeCwd. Best-effort: we can't
// distinguish an original "-" in the path from a directory separator.
// Returns "/" + dashes-as-slashes which works for typical mac/linux paths.
func unsanitizeCwd(sanitized string) string {
	return strings.ReplaceAll(sanitized, "-", "/")
}
