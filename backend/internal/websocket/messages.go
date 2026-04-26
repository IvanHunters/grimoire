package websocket

import (
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// WSMessage represents a WebSocket message from client
type WSMessage struct {
	Type          string              `json:"type"` // "init", "message", "stop", "switch_session", "restart_session", "close_session"
	SessionID     string              `json:"sessionId"`
	Content       string              `json:"content,omitempty"`
	DangerousMode bool                `json:"dangerousMode,omitempty"`
	CurrentNote   *claude.CurrentNote `json:"currentNote,omitempty"`
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
