// Package discovery enumerates historical Claude sessions by reading the
// JSONL transcripts that the claude CLI writes to ~/.claude/projects/.
//
// Every claude session — interactive, --resume, or background — writes a
// full transcript to ~/.claude/projects/<sanitized-cwd>/<sessionId>.jsonl
// in newline-delimited Anthropic Messages format. This package treats those
// files as the source of truth for "what sessions have ever existed", and
// surfaces them as Session structs that grimoire's UI can list, preview,
// and resume.
//
// Scanning is cheap by design: for a list view, only the first ~30 lines
// of each JSONL are read (enough to extract sessionId, cwd, name, first
// prompt). For a full transcript view, the caller reads the whole file
// separately.
package discovery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Session is the catalog-level view of a JSONL transcript. It does NOT
// contain the full message list — for that, read the file directly.
type Session struct {
	SessionID      string    // UUID, matches the JSONL filename
	JSONLPath      string    // absolute path to the transcript on disk
	Cwd            string    // working directory the session ran in
	Name           string    // ai-title if present, else first user prompt (truncated)
	FirstPrompt    string    // first user message content, truncated to 200 chars
	GitBranch      string    // git branch at session start, if any
	ClaudeVersion  string    // claude CLI version that produced the transcript
	StartedAt      time.Time // first event's timestamp
	LastActivityAt time.Time // file's mtime
	SizeBytes      int64     // transcript file size
}

// ProjectsRoot returns the canonical claude projects directory
// (~/.claude/projects/). Override via CLAUDE_PROJECTS_DIR for tests.
func ProjectsRoot() (string, error) {
	if override := os.Getenv("CLAUDE_PROJECTS_DIR"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// SanitizeCwd encodes an absolute path the same way claude does for its
// projects/ subdirectory names: every '/' becomes '-', so
// "/private/tmp/foo" becomes "-private-tmp-foo". Use this to find the
// project-dir for a given cwd.
func SanitizeCwd(cwd string) string {
	cwd = filepath.Clean(cwd)
	return strings.ReplaceAll(cwd, "/", "-")
}

// ScanAll walks the entire projects directory and returns one Session per
// JSONL file found. Sorted by LastActivityAt descending.
//
// Sessions with malformed headers are skipped (logged via the optional
// onSkip callback). A daemon-side write race that produces an empty file
// during scan returns a skip, not an error.
func ScanAll(onSkip func(path string, err error)) ([]Session, error) {
	root, err := ProjectsRoot()
	if err != nil {
		return nil, err
	}
	return scanRoot(root, "", onSkip)
}

// ScanCwd returns Sessions for a single cwd. Equivalent to ScanAll then
// filter, but reads only one subdirectory.
func ScanCwd(cwd string, onSkip func(path string, err error)) ([]Session, error) {
	root, err := ProjectsRoot()
	if err != nil {
		return nil, err
	}
	subdir := filepath.Join(root, SanitizeCwd(cwd))
	if _, err := os.Stat(subdir); os.IsNotExist(err) {
		return nil, nil // no sessions for this cwd yet
	}
	return scanRoot(root, SanitizeCwd(cwd), onSkip)
}

func scanRoot(root, subFilter string, onSkip func(string, error)) ([]Session, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", root, err)
	}

	var sessions []Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if subFilter != "" && entry.Name() != subFilter {
			continue
		}
		subdir := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(subdir)
		if err != nil {
			if onSkip != nil {
				onSkip(subdir, err)
			}
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(subdir, f.Name())
			s, err := ReadHeader(path)
			if err != nil {
				if onSkip != nil {
					onSkip(path, err)
				}
				continue
			}
			sessions = append(sessions, s)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivityAt.After(sessions[j].LastActivityAt)
	})
	return sessions, nil
}

// ReadHeader reads just enough of a JSONL transcript to populate a Session.
// It scans up to maxHeaderLines events looking for the first user message,
// ai-title, and cwd-bearing event. Cheap (one mmap-equivalent + a few
// json.Unmarshals); safe to call across all 1000+ transcripts on a list
// view.
//
// SessionID is derived from the filename (stem before .jsonl), NOT from
// any event — that's authoritative because the filename is how claude
// addresses the session.
//
// maxHeaderLines is generous (250) because slash-command sessions can
// have 40+ tool_use+tool_result lines before any ai-title appears.
// Scanning 250 short JSONL lines is still cheap (~few ms per file).
func ReadHeader(path string) (Session, error) {
	const maxHeaderLines = 250

	stat, err := os.Stat(path)
	if err != nil {
		return Session{}, fmt.Errorf("stat: %w", err)
	}
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

	f, err := os.Open(path)
	if err != nil {
		return Session{}, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	s := Session{
		SessionID:      sessionID,
		JSONLPath:      path,
		LastActivityAt: stat.ModTime(),
		SizeBytes:      stat.Size(),
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; i < maxHeaderLines && scanner.Scan(); i++ {
		applyHeaderEvent(&s, scanner.Bytes())
		// Early exit: we have everything we need.
		if s.Cwd != "" && s.FirstPrompt != "" && s.Name != "" && s.GitBranch != "" {
			break
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return Session{}, fmt.Errorf("scan: %w", err)
	}

	// Fallback name: try to extract a meaningful label from the first
	// prompt. Slash-command-driven sessions (/cozy-review, /tidy-notes,
	// etc) have an XML-ish payload as their first message — the bare
	// truncated string looks like "<command-message>cozy-review</…>"
	// which is useless as a title. Parse out the command name instead.
	if s.Name == "" {
		s.Name = niceNameFromPrompt(s.FirstPrompt)
	}
	if s.Name == "" {
		s.Name = "(unnamed)"
	}
	return s, nil
}

var slashCmdRe = regexp.MustCompile(`<command-message>([^<]+)</command-message>`)
var slashArgsRe = regexp.MustCompile(`<command-args>([^<]+)</command-args>`)

// niceNameFromPrompt extracts a human-readable label from a session's
// first user prompt. For slash-command sessions we surface "command-name
// · arg" (e.g. "cozy-review · …pull/2610"); for ordinary prompts we
// just truncate at 60 chars.
func niceNameFromPrompt(prompt string) string {
	if prompt == "" {
		return ""
	}
	if m := slashCmdRe.FindStringSubmatch(prompt); m != nil {
		name := strings.TrimSpace(m[1])
		if args := slashArgsRe.FindStringSubmatch(prompt); args != nil {
			arg := strings.TrimSpace(args[1])
			// Trim the arg to keep the title readable; URLs in
			// particular are long.
			if len(arg) > 40 {
				arg = "…" + arg[len(arg)-40:]
			}
			return "/" + name + " " + arg
		}
		return "/" + name
	}
	return truncate(prompt, 60)
}

// applyHeaderEvent extracts one event's interesting fields into the Session
// being built. The JSONL is heterogeneous — different event types have
// different field sets — so we use a permissive struct and only set fields
// when they're populated AND we haven't seen them yet.
func applyHeaderEvent(s *Session, line []byte) {
	var ev struct {
		Type      string          `json:"type"`
		AITitle   string          `json:"aiTitle"`
		Cwd       string          `json:"cwd"`
		GitBranch string          `json:"gitBranch"`
		Version   string          `json:"version"`
		Timestamp string          `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return // skip un-parseable lines silently
	}

	if s.Cwd == "" && ev.Cwd != "" {
		s.Cwd = ev.Cwd
	}
	if s.GitBranch == "" && ev.GitBranch != "" {
		s.GitBranch = ev.GitBranch
	}
	if s.ClaudeVersion == "" && ev.Version != "" {
		s.ClaudeVersion = ev.Version
	}
	if s.StartedAt.IsZero() && ev.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			s.StartedAt = t
		}
	}
	if s.Name == "" && ev.Type == "ai-title" && ev.AITitle != "" {
		s.Name = ev.AITitle
	}
	if s.FirstPrompt == "" && ev.Type == "user" && len(ev.Message) > 0 {
		s.FirstPrompt = extractUserText(ev.Message)
	}
}

// extractUserText pulls plain text out of a user message's content field.
// Content can be either a bare string ("hello") or an array of blocks
// (tool_result, text, etc). We return the first text-y bit, truncated.
func extractUserText(messageRaw json.RawMessage) string {
	// Try string content first.
	var withStringContent struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(messageRaw, &withStringContent); err == nil && withStringContent.Content != "" {
		return truncate(withStringContent.Content, 200)
	}
	// Fall back to array-of-blocks.
	var withArrayContent struct {
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content string `json:"content"`
		} `json:"content"`
	}
	if err := json.Unmarshal(messageRaw, &withArrayContent); err == nil {
		for _, block := range withArrayContent.Content {
			if block.Type == "text" && block.Text != "" {
				return truncate(block.Text, 200)
			}
			// tool_result content is typically not what the user "said"
			// but a previous tool's output. Skip.
		}
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
