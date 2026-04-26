package websocket

import (
	"bufio"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from configured origins
		// This will be configured in main.go
		return true // TODO: Check against allowed origins
	},
}

// Handler handles WebSocket connections for Claude chat
type Handler struct {
	cfg     *config.Config
	manager *claude.SessionManager
	logger  *slog.Logger
}

// NewHandler creates a new WebSocket handler
func NewHandler(cfg *config.Config, manager *claude.SessionManager, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:     cfg,
		manager: manager,
		logger:  logger,
	}
}

// HandleWebSocket handles WebSocket connections
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade websocket connection", slog.Any("error", err))
		return
	}
	defer conn.Close()

	h.logger.Info("websocket connection established",
		slog.String("remote", r.RemoteAddr),
	)

	// Subscribe to events
	eventBus := events.GetEventBus()
	eventChan := eventBus.Subscribe()
	defer eventBus.Unsubscribe(eventChan)

	// Start event listener goroutine
	done := make(chan struct{})
	go h.handleEvents(conn, eventChan, done)
	defer close(done)

	// Message loop
	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Error("websocket read error", slog.Any("error", err))
			}
			break
		}

		h.logger.Debug("received websocket message",
			slog.String("type", msg.Type),
			slog.String("session_id", msg.SessionID),
		)

		// Handle message based on type
		switch msg.Type {
		case "init":
			h.handleInit(conn, &msg)
		case "message":
			h.handleMessage(conn, &msg)
		case "stop":
			h.handleStop(conn, &msg)
		case "switch_session":
			h.handleSwitchSession(conn, &msg)
		case "restart_session":
			h.handleRestartSession(conn, &msg)
		case "close_session":
			h.handleCloseSession(conn, &msg)
		default:
			h.logger.Warn("unknown message type", slog.String("type", msg.Type))
			conn.WriteJSON(WSResponse{
				Type:  "error",
				Error: "Unknown message type",
			})
		}
	}

	h.logger.Info("websocket connection closed",
		slog.String("remote", r.RemoteAddr),
	)
}

// handleInit initializes a new Claude session
func (h *Handler) handleInit(conn *websocket.Conn, msg *WSMessage) {
	// Determine working directory
	workingDir, err := claude.DetermineWorkingDir(msg.CurrentNote, msg.SessionID)
	if err != nil {
		h.logger.Error("failed to determine working directory", slog.Any("error", err))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to determine working directory",
		})
		return
	}

	// Get or create session
	session, err := h.manager.GetOrCreate(msg.SessionID, msg.DangerousMode, workingDir)
	if err != nil {
		h.logger.Error("failed to create session", slog.Any("error", err))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to create Claude session",
		})
		return
	}

	// Start streaming output from Claude subprocess
	go h.streamClaudeOutput(session, conn)

	// Send success response
	conn.WriteJSON(WSResponse{
		Type:      "session_started",
		SessionID: msg.SessionID,
	})
}

// handleMessage sends a message to Claude subprocess
func (h *Handler) handleMessage(conn *websocket.Conn, msg *WSMessage) {
	session, err := h.manager.Get(msg.SessionID)
	if err != nil {
		h.logger.Error("session not found", slog.String("session_id", msg.SessionID))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Session not found. Please initialize first.",
		})
		return
	}

	// Send message to Claude subprocess
	if err := session.SendMessage(msg.Content); err != nil {
		h.logger.Error("failed to send message", slog.Any("error", err))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to send message to Claude",
		})
		return
	}

	// Notify frontend that message was sent
	conn.WriteJSON(WSResponse{
		Type:      "message_start",
		SessionID: msg.SessionID,
	})
}

// handleStop sends Ctrl+C to Claude subprocess
func (h *Handler) handleStop(conn *websocket.Conn, msg *WSMessage) {
	session, err := h.manager.Get(msg.SessionID)
	if err != nil {
		h.logger.Error("session not found", slog.String("session_id", msg.SessionID))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Session not found",
		})
		return
	}

	// Send Ctrl+C
	if err := session.Stop(); err != nil {
		h.logger.Error("failed to stop session", slog.Any("error", err))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to stop generation",
		})
		return
	}

	// Notify frontend
	conn.WriteJSON(WSResponse{
		Type:      "stopped",
		SessionID: msg.SessionID,
	})
}

// handleSwitchSession switches to a different session
func (h *Handler) handleSwitchSession(conn *websocket.Conn, msg *WSMessage) {
	session, err := h.manager.Get(msg.SessionID)
	if err != nil {
		// Session doesn't exist yet, client should init it
		conn.WriteJSON(WSResponse{
			Type:      "session_not_found",
			SessionID: msg.SessionID,
		})
		return
	}

	// Return session history
	messages := session.GetMessages()
	conn.WriteJSON(WSResponse{
		Type:      "session_history",
		SessionID: msg.SessionID,
		Messages:  messages,
	})
}

// handleRestartSession restarts a session with preserved history
func (h *Handler) handleRestartSession(conn *websocket.Conn, msg *WSMessage) {
	// Close existing session
	if err := h.manager.Close(msg.SessionID); err != nil {
		h.logger.Warn("failed to close session for restart",
			slog.String("session_id", msg.SessionID),
			slog.Any("error", err),
		)
	}

	// Initialize new session
	h.handleInit(conn, msg)
}

// handleCloseSession gracefully closes a session
func (h *Handler) handleCloseSession(conn *websocket.Conn, msg *WSMessage) {
	if err := h.manager.Close(msg.SessionID); err != nil {
		h.logger.Error("failed to close session", slog.Any("error", err))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to close session",
		})
		return
	}

	conn.WriteJSON(WSResponse{
		Type:      "session_closed",
		SessionID: msg.SessionID,
	})
}

// streamClaudeOutput streams Claude subprocess output to WebSocket
func (h *Handler) streamClaudeOutput(session *claude.ClaudeSession, conn *websocket.Conn) {
	reader := bufio.NewReader(session.PTY)
	var currentMessage strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				h.logger.Error("error reading from PTY", slog.Any("error", err))
			}
			break
		}

		// Strip ANSI codes
		line = claude.StripANSI(line)
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		h.logger.Debug("claude output", slog.String("line", line))

		// Check for tool use
		if toolUse := claude.ParseToolUse(line); toolUse != nil {
			conn.WriteJSON(WSResponse{
				Type:      "tool_use",
				SessionID: session.ID,
				ToolName:  toolUse.Name,
				ToolArgs:  toolUse.Args,
			})
			continue
		}

		// Check for interruption
		if claude.DetectInterrupted(line) {
			conn.WriteJSON(WSResponse{
				Type:      "stopped",
				SessionID: session.ID,
			})

			// Add accumulated message to history
			if currentMessage.Len() > 0 {
				session.AddAssistantMessage(currentMessage.String())
				currentMessage.Reset()
			}
			continue
		}

		// Check for message completion
		if claude.IsMessageComplete(line) {
			conn.WriteJSON(WSResponse{
				Type:      "message_complete",
				SessionID: session.ID,
			})

			// Add accumulated message to history
			if currentMessage.Len() > 0 {
				session.AddAssistantMessage(currentMessage.String())
				currentMessage.Reset()
			}
			continue
		}

		// Regular content - stream it
		conn.WriteJSON(WSResponse{
			Type:      "content_delta",
			SessionID: session.ID,
			Content:   line,
		})

		// Accumulate message content
		currentMessage.WriteString(line)
		currentMessage.WriteString("\n")

		// Update activity
		session.UpdateActivity()
	}
}

// SetCheckOrigin sets the origin checker for WebSocket upgrader
func (h *Handler) SetCheckOrigin(check func(*http.Request) bool) {
	upgrader.CheckOrigin = check
}

// handleEvents listens for events and sends them to WebSocket client
func (h *Handler) handleEvents(conn *websocket.Conn, eventChan chan events.Event, done chan struct{}) {
	for {
		select {
		case event := <-eventChan:
			// Convert event to WebSocket response
			var wsType string
			switch event.Type {
			case events.EventNoteCreated:
				wsType = "note_created"
			case events.EventNoteUpdated:
				wsType = "note_updated"
			case events.EventNoteDeleted:
				wsType = "note_deleted"
			case events.EventFolderCreated:
				wsType = "folder_created"
			case events.EventFolderDeleted:
				wsType = "folder_deleted"
			default:
				continue
			}

			// Send event to client
			response := map[string]interface{}{
				"type": wsType,
			}

			if event.Note != nil {
				response["note"] = event.Note
			}
			if event.Folder != nil {
				response["folder"] = event.Folder
			}
			if event.NoteID != "" {
				response["noteId"] = event.NoteID
			}
			if event.Path != "" {
				response["path"] = event.Path
			}

			if err := conn.WriteJSON(response); err != nil {
				h.logger.Error("failed to send event to client", slog.Any("error", err))
				return
			}

		case <-done:
			return
		}
	}
}
