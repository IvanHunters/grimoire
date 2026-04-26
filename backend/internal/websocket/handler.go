package websocket

import (
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

		h.logger.Info("received websocket message",
			slog.String("type", msg.Type),
			slog.String("session_id", msg.SessionID),
		)

		// Handle message based on type
		switch msg.Type {
		case "init":
			h.handleInit(conn, &msg)
		case "message":
			h.handleMessage(conn, &msg)
		case "terminal_input":
			h.handleTerminalInput(conn, &msg)
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

	// Send success response
	conn.WriteJSON(WSResponse{
		Type:      "session_started",
		SessionID: msg.SessionID,
	})

	// Replay terminal output buffer for reconnects
	buffer := session.GetOutputBuffer()
	if len(buffer) > 0 {
		h.logger.Info("replaying terminal output buffer",
			slog.String("session_id", session.ID),
			slog.Int("bytes", len(buffer)),
		)
		conn.WriteJSON(WSResponse{
			Type:      "terminal_output",
			SessionID: session.ID,
			Content:   string(buffer),
		})
	}

	// Subscribe to PTY output for this WebSocket connection
	go h.streamPTYOutput(session, conn)
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

	// Save messages to MongoDB
	if err := h.manager.SaveSessionMessages(msg.SessionID); err != nil {
		h.logger.Warn("failed to save session messages", slog.Any("error", err))
	}

	// Notify frontend that message was sent
	conn.WriteJSON(WSResponse{
		Type:      "message_start",
		SessionID: msg.SessionID,
	})
}

// handleTerminalInput sends raw keystrokes to PTY
func (h *Handler) handleTerminalInput(conn *websocket.Conn, msg *WSMessage) {
	session, err := h.manager.Get(msg.SessionID)
	if err != nil {
		h.logger.Error("session not found", slog.String("session_id", msg.SessionID))
		return
	}

	// Write raw input to PTY
	_, err = session.PTY.Write([]byte(msg.Content))
	if err != nil {
		h.logger.Error("failed to write to PTY", slog.Any("error", err))
	}
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

// streamPTYOutput subscribes to PTY output channel and sends to WebSocket
func (h *Handler) streamPTYOutput(session *claude.ClaudeSession, conn *websocket.Conn) {
	h.logger.Info("subscribing to PTY output", slog.String("session_id", session.ID))

	outputChan := session.SubscribeToOutput()

	for data := range outputChan {
		// Send to WebSocket
		err := conn.WriteJSON(WSResponse{
			Type:      "terminal_output",
			SessionID: session.ID,
			Content:   string(data),
		})
		if err != nil {
			h.logger.Error("failed to send terminal output to WebSocket", slog.Any("error", err))
			return
		}

		preview := string(data)
		if len(preview) > 20 {
			preview = preview[:20] + "..."
		}
		h.logger.Debug("sent terminal output",
			slog.String("session_id", session.ID),
			slog.Int("bytes", len(data)),
			slog.String("preview", preview),
		)
	}

	h.logger.Info("PTY output subscription ended", slog.String("session_id", session.ID))
}

// isTUIGarbage checks if a line is TUI garbage that should be filtered
func (h *Handler) isTUIGarbage(line string) bool {
	// Empty or whitespace only
	if len(line) == 0 {
		return true
	}

	// UI borders and decorations
	if strings.ContainsAny(line, "│╭╰─┤├┬┴┼╔╗╚╝║═╠╣╦╩╬") {
		return true
	}

	// Prompt indicators
	if strings.HasPrefix(line, "❯") || strings.HasPrefix(line, "⏵") {
		return true
	}

	// Remove all spaces for matching (ANSI stripping causes words to merge)
	noSpaces := strings.ReplaceAll(strings.ToLower(line), " ", "")

	// Status messages (without spaces to catch merged words)
	filters := []string{
		"updateavailable",
		"brewupgrade",
		"claude-code",
		"ctrl+gtoedit",
		"ctrl+g",
		"pressctrl-c",
		"bypasspermissions",
		"shift+tab",
		"claudecode",
		"welcomeback",
		"tipsfor",
		"askclaude",
		"recentactivity",
		"norecentactivity",
		"sonnet4",
		"claudeteam",
		"sessions/",
		".claude/",
	}

	for _, filter := range filters {
		if strings.Contains(noSpaces, filter) {
			return true
		}
	}

	// Also check original line with spaces
	lowerLine := strings.ToLower(line)
	spacedFilters := []string{
		"update available",
		"brew upgrade",
		"press ctrl-c",
	}

	for _, filter := range spacedFilters {
		if strings.Contains(lowerLine, filter) {
			return true
		}
	}

	return false
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
