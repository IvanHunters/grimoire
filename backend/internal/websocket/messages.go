package websocket

import (
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// TaskContext carries task data for Claude sessions opened from the task tracker.
type TaskContext struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Description string `json:"description,omitempty"`
	FolderPath  string `json:"folderPath,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`
}

// WSMessage represents a WebSocket message from client
type WSMessage struct {
	Type          string              `json:"type"` // "init", "message", "stop", "switch_session", "restart_session", "close_session", "terminal_resize"
	SessionID     string              `json:"sessionId"`
	Content       string              `json:"content,omitempty"`
	DangerousMode bool                `json:"dangerousMode,omitempty"`
	CurrentNote   *claude.CurrentNote `json:"currentNote,omitempty"`
	TaskContext   *TaskContext         `json:"taskContext,omitempty"`
	Cols          int                 `json:"cols,omitempty"`
	Rows          int                 `json:"rows,omitempty"`

	// ResumeFromSessionID, when set on an "init" message, tells the
	// backend to spawn the claude session via --resume <uuid> instead
	// of starting fresh. The cwd is resolved from the historical
	// session's JSONL header, ignoring the usual DetermineWorkingDir
	// chain. Used by the "Continue this session" button on a historical
	// transcript view.
	ResumeFromSessionID string `json:"resumeFromSessionId,omitempty"`

	// ResumeFork, when paired with ResumeFromSessionID, passes
	// --fork-session to claude so the new session branches off a copy
	// of the transcript instead of continuing the original. The new
	// session gets its own UUID assigned by the daemon. Used by the
	// "Fork" button alongside "Continue".
	ResumeFork bool `json:"resumeFork,omitempty"`

	// AttachToSessionID, when set, tells the backend to attach to a
	// live daemon worker by its UUID instead of spawning or resuming.
	// Used when the user clicks an active session in the sidebar — we
	// connect to the existing PTY rather than dispatching a new
	// process. Falls through to error if the daemon doesn't know that
	// UUID. Cwd from the daemon record is used; CurrentNote is ignored.
	AttachToSessionID string `json:"attachToSessionId,omitempty"`

	// SessionName, when set, is the user-given display name for the
	// session being spawned/resumed/forked. Persisted to the Mongo
	// overlay so the sidebar listing shows it instead of the daemon's
	// structured token ("grimoire-fork-…", etc). Only honored when
	// non-empty and not a "grimoire-" structured token itself.
	SessionName string `json:"sessionName,omitempty"`

	// SkipContextPrompt is an internal flag set by handleRestartSession
	// when calling handleInit. It tells the init code path to mark the
	// new session's ContextPromptSent=true upfront so the SESSION
	// CONTEXT wall of text doesn't get re-pasted into the terminal on
	// every restart. Not exposed via JSON — only the handler sets it.
	SkipContextPrompt bool `json:"-"`
}

// WSResponse represents a WebSocket message to client
type WSResponse struct {
	Type      string                 `json:"type"` // "message_start", "content_delta", "tool_use", "message_complete", "error", "stopped", "session_history"
	SessionID string                 `json:"sessionId,omitempty"`
	Content   string                 `json:"content,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolArgs  string                 `json:"tool_args,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Messages  []models.ClaudeMessage `json:"messages,omitempty"`
}
