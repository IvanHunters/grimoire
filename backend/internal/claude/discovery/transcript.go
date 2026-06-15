package discovery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TranscriptMessage is one user-visible message extracted from a JSONL
// transcript. We collapse the rich Anthropic event stream (thinking +
// tool_use + tool_result + text blocks) into a simple role + text view
// suitable for a chat-bubble UI.
//
// HasTools indicates the underlying assistant message also performed
// tool calls — the UI can show a small indicator. ToolUses is the list
// of tool names invoked, for at-a-glance context.
//
// LineNumber refers to the source JSONL line; pair it with search hit
// lineNumbers to scroll-to-match.
type TranscriptMessage struct {
	UUID       string    `json:"uuid"`
	LineNumber int       `json:"lineNumber"`
	Role       string    `json:"role"` // "user" | "assistant"
	Timestamp  time.Time `json:"timestamp"`
	Text       string    `json:"text"`
	HasTools   bool      `json:"hasTools"`
	ToolUses   []string  `json:"toolUses,omitempty"`
	IsError    bool      `json:"isError,omitempty"`
}

// TranscriptHeader is metadata about the session as a whole — first line
// the UI shows when opening a transcript.
type TranscriptHeader struct {
	SessionID     string    `json:"sessionId"`
	Name          string    `json:"name"`
	Cwd           string    `json:"cwd"`
	GitBranch     string    `json:"gitBranch,omitempty"`
	ClaudeVersion string    `json:"claudeVersion,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	MessageCount  int       `json:"messageCount"`
}

// Transcript is the full payload returned by ReadTranscript.
type Transcript struct {
	Header   TranscriptHeader    `json:"header"`
	Messages []TranscriptMessage `json:"messages"`
}

// ReadTranscript reads a JSONL transcript and produces a clean
// TranscriptMessage list. Metadata events (permission-mode, ai-title,
// attachments, etc) are filtered out — only user/assistant content
// remains.
//
// SessionID is the UUID part of the filename (we don't look it up by
// path). path must be the absolute JSONL location, e.g. as returned by
// SessionListItem.JSONLPath.
func ReadTranscript(path string) (Transcript, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Transcript{}, fmt.Errorf("stat: %w", err)
	}
	_ = stat // used implicitly via os.Open below

	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	tr := Transcript{
		Header: TranscriptHeader{
			SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		},
		Messages: []TranscriptMessage{},
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		applyTranscriptEvent(&tr, scanner.Bytes(), lineNum)
	}
	if err := scanner.Err(); err != nil {
		return Transcript{}, fmt.Errorf("scan: %w", err)
	}

	tr.Header.MessageCount = len(tr.Messages)
	if tr.Header.Name == "" && len(tr.Messages) > 0 {
		tr.Header.Name = truncate(tr.Messages[0].Text, 80)
	}
	if tr.Header.Name == "" {
		tr.Header.Name = "(unnamed)"
	}
	return tr, nil
}

func applyTranscriptEvent(tr *Transcript, line []byte, lineNum int) {
	var ev struct {
		Type      string          `json:"type"`
		UUID      string          `json:"uuid"`
		AITitle   string          `json:"aiTitle"`
		Cwd       string          `json:"cwd"`
		GitBranch string          `json:"gitBranch"`
		Version   string          `json:"version"`
		Timestamp string          `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}

	// Populate header fields from any event that has them (first wins).
	if tr.Header.Cwd == "" && ev.Cwd != "" {
		tr.Header.Cwd = ev.Cwd
	}
	if tr.Header.GitBranch == "" && ev.GitBranch != "" {
		tr.Header.GitBranch = ev.GitBranch
	}
	if tr.Header.ClaudeVersion == "" && ev.Version != "" {
		tr.Header.ClaudeVersion = ev.Version
	}
	if tr.Header.StartedAt.IsZero() && ev.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			tr.Header.StartedAt = t
		}
	}
	if tr.Header.Name == "" && ev.Type == "ai-title" && ev.AITitle != "" {
		tr.Header.Name = ev.AITitle
	}

	// Only user/assistant events become messages.
	if ev.Type != "user" && ev.Type != "assistant" {
		return
	}

	text, tools, isError := extractMessageRich(ev.Message)
	if text == "" && len(tools) == 0 {
		// Skip empty messages (e.g. thinking-only events with no text).
		return
	}

	var ts time.Time
	if ev.Timestamp != "" {
		ts, _ = time.Parse(time.RFC3339Nano, ev.Timestamp)
	}
	tr.Messages = append(tr.Messages, TranscriptMessage{
		UUID:       ev.UUID,
		LineNumber: lineNum,
		Role:       ev.Type,
		Timestamp:  ts,
		Text:       text,
		HasTools:   len(tools) > 0,
		ToolUses:   tools,
		IsError:    isError,
	})
}

// extractMessageRich pulls text + tool names + error flag from a
// user/assistant message body. Distinguishes from the simpler version
// in search.go by also collecting tool_use names and detecting
// is_error on tool_result blocks.
func extractMessageRich(raw json.RawMessage) (text string, tools []string, isError bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	// String-content shape (typed user message).
	var withStr struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &withStr); err == nil && withStr.Content != "" {
		return withStr.Content, nil, false
	}
	// Array-of-blocks shape (assistant, or user with tool_result).
	var withArr struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Content json.RawMessage `json:"content"`
			IsError bool            `json:"is_error"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &withArr); err != nil {
		return "", nil, false
	}
	var parts []string
	for _, b := range withArr.Content {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_use":
			if b.Name != "" {
				tools = append(tools, b.Name)
			}
		case "tool_result":
			// Tool results in user-messages: include the text but mark error.
			if b.IsError {
				isError = true
			}
			if s := extractToolResultText(b.Content); s != "" {
				parts = append(parts, s)
			}
		case "thinking":
			// Skip thinking blocks — they're internal and look noisy in
			// a chat bubble view. Could be a toggle in the UI later.
		}
	}
	return strings.Join(parts, "\n"), tools, isError
}

// extractToolResultText handles the polymorphic tool_result.content
// field: it can be a bare string or an array of {type:"text",text:...}
// blocks.
func extractToolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var asArr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &asArr); err == nil {
		var parts []string
		for _, b := range asArr {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// SessionPath maps a sessionID to its on-disk JSONL by globbing
// ~/.claude/projects/*/<sessionID>.jsonl. Returns the first match (each
// session appears in exactly one project dir under normal circumstances).
func SessionPath(sessionID string) (string, error) {
	root, err := ProjectsRoot()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", sessionID+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no transcript found for session %s", sessionID)
	}
	return matches[0], nil
}
