package websocket

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// wsWriter serializes concurrent WebSocket writes.
// gorilla/websocket allows one concurrent writer — without this, streamPTYOutput
// and handleEvents racing on the same conn cause random connection closes.
type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

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
	storage *storage.MongoStorage
	logger  *slog.Logger
}

// NewHandler creates a new WebSocket handler
func NewHandler(cfg *config.Config, manager *claude.SessionManager, store *storage.MongoStorage, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:     cfg,
		manager: manager,
		storage: store,
		logger:  logger,
	}
}

// HandleWebSocket handles WebSocket connections
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection to WebSocket
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade websocket connection", slog.Any("error", err))
		return
	}
	defer rawConn.Close()
	conn := &wsWriter{conn: rawConn}

	h.logger.Info("websocket connection established",
		slog.String("remote", r.RemoteAddr),
	)

	// Context cancelled when the connection's message loop exits, stopping streamPTYOutput.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

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
		if err := rawConn.ReadJSON(&msg); err != nil {
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
			h.handleInit(ctx, conn, &msg)
		case "message":
			h.handleMessage(conn, &msg)
		case "terminal_input":
			h.handleTerminalInput(rawConn, &msg)
		case "terminal_resize":
			h.handleTerminalResize(rawConn, &msg)
		case "stop":
			h.handleStop(conn, &msg)
		case "switch_session":
			h.handleSwitchSession(conn, &msg)
		case "restart_session":
			h.handleRestartSession(ctx, conn, &msg)
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
func (h *Handler) handleInit(ctx context.Context, conn *wsWriter, msg *WSMessage) {
	// Find project path from folder hierarchy if note doesn't have one
	var folderProjectPath string
	if msg.CurrentNote != nil && msg.CurrentNote.Folder != "" {
		folderProjectPath = h.getFolderProjectPath(msg.CurrentNote.Folder)
	}

	// For task context: resolve project path from linked folder hierarchy
	if msg.TaskContext != nil {
		if msg.TaskContext.ProjectPath != "" {
			folderProjectPath = msg.TaskContext.ProjectPath
		} else if msg.TaskContext.FolderPath != "" {
			if fp := h.getFolderProjectPath(msg.TaskContext.FolderPath); fp != "" {
				folderProjectPath = fp
				msg.TaskContext.ProjectPath = fp // propagate into prompt
			}
		}
	}

	// Determine working directory
	workingDir, err := claude.DetermineWorkingDir(msg.CurrentNote, folderProjectPath, msg.SessionID)
	if err != nil {
		h.logger.Error("failed to determine working directory", slog.Any("error", err))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to determine working directory",
		})
		return
	}

	// Determine session name from current note or task
	sessionName := ""
	if msg.CurrentNote != nil && msg.CurrentNote.Name != "" {
		sessionName = msg.CurrentNote.Name
	} else if msg.TaskContext != nil && msg.TaskContext.Title != "" {
		title := msg.TaskContext.Title
		runes := []rune(title)
		if len(runes) > 60 {
			title = string(runes[:57]) + "..."
		}
		sessionName = title
	}

	// Build system prompt for task sessions — passed via --append-system-prompt flag so
	// it never appears as PTY-echoed text in the terminal (long titles were garbling it).
	var systemPrompt string
	if msg.TaskContext != nil {
		systemPrompt = buildTaskSystemPrompt(msg.TaskContext, folderProjectPath)
	}

	// Get or create session
	session, err := h.manager.GetOrCreate(msg.SessionID, msg.DangerousMode, workingDir, sessionName, systemPrompt)
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

	// Send chat history if exists, otherwise kick off the task with a short initial message.
	// Context (MCP tools, rules, task details) is already in the system prompt via
	// --append-system-prompt, so we only need one short line here — avoids PTY echo garble.
	messages := session.GetMessages()
	if len(messages) == 0 && msg.TaskContext != nil {
		tc := msg.TaskContext
		initialMsg := "Загрузи детали задачи через get_task(\"" + tc.ID + "\") и скажи что за задача. Потом спроси: «Что делаем?»"
		go func() {
			time.Sleep(2 * time.Second)
			if err := session.SendMessage(initialMsg); err != nil {
				h.logger.Warn("failed to send task initial message", slog.Any("error", err))
			}
		}()
	}

	if len(messages) > 0 {
		h.logger.Info("sending chat history",
			slog.String("session_id", session.ID),
			slog.Int("messages", len(messages)),
		)
		conn.WriteJSON(WSResponse{
			Type:      "chat_history",
			SessionID: session.ID,
			Messages:  messages,
		})
	} else if msg.CurrentNote != nil {
		// Send automatic context prompt for new sessions
		var contextParts []string

		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "SESSION CONTEXT")
		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "You are Claude Code working inside a markdown knowledge base editor.")
		contextParts = append(contextParts, "MCP server 'markdown-editor' gives you direct access to notes and project files.")
		contextParts = append(contextParts, "")

		if msg.CurrentNote.Name != "" {
			contextParts = append(contextParts, "CURRENT NOTE: "+msg.CurrentNote.Name)
		}
		if msg.CurrentNote.Folder != "" {
			contextParts = append(contextParts, "Folder: "+msg.CurrentNote.Folder)
		}
		contextParts = append(contextParts, "")

		// Determine folder hierarchy for context discovery
		var folderHierarchy []string
		if msg.CurrentNote.Folder != "" {
			parts := strings.Split(msg.CurrentNote.Folder, "/")
			for i := len(parts); i >= 1; i-- {
				folderHierarchy = append(folderHierarchy, strings.Join(parts[:i], "/"))
			}
		}

		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "STARTUP SEQUENCE — DO THIS FIRST, EVERY SESSION")
		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "Goal: understand the project context in minimum tokens before asking 'что делаем?'")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "STEP 1 — Scan folder for context notes (one call):")
		if msg.CurrentNote.Folder != "" {
			contextParts = append(contextParts, "  list_notes_summary(\""+msg.CurrentNote.Folder+"\")")
			contextParts = append(contextParts, "  Look in the result for notes with names like:")
			contextParts = append(contextParts, "    *-project-rules*, *-project-overview*, overview, _context, rules, readme")
		} else {
			contextParts = append(contextParts, "  list_notes_summary(\"\")  — scan all notes")
		}
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "STEP 2 — If no context notes in current folder, check parent folders:")
		if len(folderHierarchy) > 1 {
			for _, f := range folderHierarchy[1:] {
				contextParts = append(contextParts, "  list_notes_summary(\""+f+"\")")
			}
		} else {
			contextParts = append(contextParts, "  (note is at root — no parent folders to check)")
		}
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "STEP 3 — Read found context notes (rules first, then overview):")
		contextParts = append(contextParts, "  read_note(path_or_id)  — read rules note")
		contextParts = append(contextParts, "  read_note(path_or_id)  — read overview if needed")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "STEP 4 — Check recent activity (optional, only if context is unclear):")
		contextParts = append(contextParts, "  find_recent_notes(days=3, limit=5)")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "AFTER STARTUP:")
		contextParts = append(contextParts, "  - If rules/context found: say 'Контекст загружен: [одна строка что за проект]'. Ask 'Что делаем?'")
		contextParts = append(contextParts, "  - If nothing found: say 'Контекст не найден. Расскажи что делаем — создам заметку с правилами проекта'")
		contextParts = append(contextParts, "  - After session with important decisions/discoveries: update or create rules note")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "RULES NOTE FORMAT (e.g. '{project}-project-rules.md'):")
		contextParts = append(contextParts, "  ## Стек и архитектура")
		contextParts = append(contextParts, "  ## Правила git (коммиты, ветки, GPG, push политика)")
		contextParts = append(contextParts, "  ## Запрещено")
		contextParts = append(contextParts, "  ## Важные пути и файлы")
		contextParts = append(contextParts, "  ## Нерешённые вопросы / TODO")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "MCP TOOLS — TOKEN EFFICIENCY ORDER (cheapest first)")
		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "CURRENT NOTE (free — already in context):")
		contextParts = append(contextParts, "  - read_current_note()                  — read note open in editor")
		contextParts = append(contextParts, "  - update_current_note(content)         — overwrite it")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "FAST LOOKUP (cheap):")
		contextParts = append(contextParts, "  - get_note_by_path(path)               — direct access when path is known, e.g. 'Projects/Aenix/rules.md'")
		contextParts = append(contextParts, "  - search_by_tags(tags, limit)          — microsecond in-memory search, e.g. 'kubernetes,networking'")
		contextParts = append(contextParts, "  - get_all_tags()                        — all tags with counts")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "SEARCH (medium cost — use limit!):")
		contextParts = append(contextParts, "  - search_notes(query, summary_only=true, limit=10)  — full-text, ranked by relevance")
		contextParts = append(contextParts, "  - list_notes_summary(folder)           — scan folder, titles + first line only")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "READ (expensive — only specific notes):")
		contextParts = append(contextParts, "  - read_note(path)                      — full content of one note")
		contextParts = append(contextParts, "  - list_notes(folder)                   — all notes with full content (avoid)")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "WRITE:")
		contextParts = append(contextParts, "  - create_note(path, content)           — new note")
		contextParts = append(contextParts, "  - create_folder(path)                  — new folder")
		contextParts = append(contextParts, "")

		if msg.CurrentNote.ProjectPath != "" {
			contextParts = append(contextParts, "PROJECT (path: "+msg.CurrentNote.ProjectPath+"):")
			contextParts = append(contextParts, "  - list_project_files(pattern)          — list files")
			contextParts = append(contextParts, "  - read_project_file(path)              — read file")
			contextParts = append(contextParts, "  - write_project_file(path, content)    — edit file")
			contextParts = append(contextParts, "  - search_project(query, pattern)       — search in code")
			contextParts = append(contextParts, "  - git_status(), git_diff(), git_pull() — git operations")
			contextParts = append(contextParts, "  - run_tests(command)                   — run tests")
			contextParts = append(contextParts, "")
		}

		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "MANDATORY RULES")
		contextParts = append(contextParts, "===========================================================")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "Token efficiency:")
		contextParts = append(contextParts, "  - Always try get_note_by_path or search_by_tags BEFORE search_notes")
		contextParts = append(contextParts, "  - search_notes: always pass limit=5..20, never omit it")
		contextParts = append(contextParts, "  - search_notes: summary_only=true unless you need full content")
		contextParts = append(contextParts, "  - read_note only for the specific note you actually need")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "Notes and documentation:")
		contextParts = append(contextParts, "  - Documentation lives IN NOTES — do not duplicate it into code/configs")
		contextParts = append(contextParts, "  - Reference notes via wikilinks [[Note Name]]")
		contextParts = append(contextParts, "  - Write documentation in RUSSIAN language")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "Git workflow:")
		contextParts = append(contextParts, "  - ALWAYS ask before commit and push")
		contextParts = append(contextParts, "  - Commit after each logical block")
		contextParts = append(contextParts, "  - Format: type(scope): description")
		contextParts = append(contextParts, "  - REQUIRED: git commit --signoff")
		contextParts = append(contextParts, "  - FORBIDDEN: --no-gpg-sign (financial penalty for unsigned commits)")
		contextParts = append(contextParts, "  - NEVER push to main/master directly")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "Communication:")
		contextParts = append(contextParts, "  - Chat in RUSSIAN, code/commits/PRs in ENGLISH")
		contextParts = append(contextParts, "  - Be direct and critical — challenge wrong decisions")
		contextParts = append(contextParts, "")
		contextParts = append(contextParts, "Tech:")
		contextParts = append(contextParts, "  - FORBIDDEN: Bitnami images, --no-gpg-sign, push to main")
		contextParts = append(contextParts, "  - Prefer: OpenTofu over Terraform, full flag names (--assume-yes not -y)")
		contextParts = append(contextParts, "===========================================================")

		contextPrompt := strings.Join(contextParts, "\n")

		// Send context prompt after delay to ensure terminal is ready
		go func() {
			time.Sleep(2 * time.Second)
			h.logger.Info("sending automatic context prompt",
				slog.String("session_id", session.ID),
			)
			if err := session.SendMessage(contextPrompt); err != nil {
				h.logger.Warn("failed to send context prompt", slog.Any("error", err))
			}
		}()
	}

	// Replay terminal output buffer for reconnects.
	// Buffer must be base64-encoded for safe JSON transport of raw PTY bytes.
	buffer := session.GetOutputBuffer()
	if len(buffer) > 0 {
		h.logger.Info("replaying terminal output buffer",
			slog.String("session_id", session.ID),
			slog.Int("bytes", len(buffer)),
		)
		conn.WriteJSON(WSResponse{
			Type:      "terminal_output",
			SessionID: session.ID,
			Content:   base64.StdEncoding.EncodeToString(buffer),
		})
	}

	// Subscribe to PTY output for this WebSocket connection.
	// Uses a dedicated channel per connection (fan-out) to prevent data splitting.
	go h.streamPTYOutput(ctx, session, conn)
}

// handleMessage sends a message to Claude subprocess
func (h *Handler) handleMessage(conn *wsWriter, msg *WSMessage) {
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
func (h *Handler) handleStop(conn *wsWriter, msg *WSMessage) {
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
func (h *Handler) handleSwitchSession(conn *wsWriter, msg *WSMessage) {
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
func (h *Handler) handleRestartSession(ctx context.Context, conn *wsWriter, msg *WSMessage) {
	// Close existing session
	if err := h.manager.Close(msg.SessionID); err != nil {
		h.logger.Warn("failed to close session for restart",
			slog.String("session_id", msg.SessionID),
			slog.Any("error", err),
		)
	}

	// Initialize new session
	h.handleInit(ctx, conn, msg)
}

// handleTerminalResize updates the PTY window size to match xterm.js dimensions
func (h *Handler) handleTerminalResize(conn *websocket.Conn, msg *WSMessage) {
	if msg.Cols <= 0 || msg.Rows <= 0 {
		return
	}

	session, err := h.manager.Get(msg.SessionID)
	if err != nil {
		return
	}

	if err := pty.Setsize(session.PTY, &pty.Winsize{
		Rows: uint16(msg.Rows),
		Cols: uint16(msg.Cols),
	}); err != nil {
		h.logger.Error("failed to resize PTY",
			slog.String("session_id", msg.SessionID),
			slog.Any("error", err),
		)
	}
}

// handleCloseSession gracefully closes a session
func (h *Handler) handleCloseSession(conn *wsWriter, msg *WSMessage) {
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

// streamPTYOutput subscribes to PTY output and forwards it to the WebSocket connection.
// Each call creates a dedicated subscriber channel (fan-out), so multiple concurrent
// connections for the same session each receive the full, unshared byte stream.
// The goroutine exits when: the context is cancelled (connection closed), the subscriber
// channel is closed (process exited), or the WebSocket write fails.
func (h *Handler) streamPTYOutput(ctx context.Context, session *claude.ClaudeSession, conn *wsWriter) {
	h.logger.Info("subscribing to PTY output", slog.String("session_id", session.ID))

	ch := session.SubscribeToOutput()
	defer session.UnsubscribeFromOutput(ch)

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				h.logger.Info("PTY output subscription ended", slog.String("session_id", session.ID))
				return
			}
			// Base64-encode raw PTY bytes so they survive JSON transport without corruption.
			// Raw PTY data may contain arbitrary byte values; Go's json.Marshal replaces
			// non-UTF-8 bytes with U+FFFD, which would corrupt ANSI escape sequences.
			err := conn.WriteJSON(WSResponse{
				Type:      "terminal_output",
				SessionID: session.ID,
				Content:   base64.StdEncoding.EncodeToString(data),
			})
			if err != nil {
				h.logger.Error("failed to send terminal output to WebSocket", slog.Any("error", err))
				return
			}
		case <-ctx.Done():
			h.logger.Info("PTY stream cancelled (connection closed)", slog.String("session_id", session.ID))
			return
		}
	}
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
func (h *Handler) handleEvents(conn *wsWriter, eventChan chan events.Event, done chan struct{}) {
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
			case events.EventTaskCreated:
				wsType = "task_created"
			case events.EventTaskUpdated:
				wsType = "task_updated"
			case events.EventTaskDeleted:
				wsType = "task_deleted"
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
			if event.Task != nil {
				response["task"] = event.Task
			}
			if event.NoteID != "" {
				response["noteId"] = event.NoteID
			}
			if event.TaskID != "" {
				response["taskId"] = event.TaskID
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

// sanitizePromptText strips characters that can cause issues when passed as CLI args:
// box-drawing (U+2500–U+257F), block elements (U+2580–U+259F), and other
// non-printable/control characters outside normal text ranges.
// Cyrillic and standard Latin are kept as-is.
func sanitizePromptText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0x2500 && r <= 0x259F: // Box Drawing + Block Elements
			b.WriteRune(' ')
		case r >= 0x2580 && r <= 0x27FF: // Misc symbols, arrows, etc.
			b.WriteRune(' ')
		case r < 0x20 && r != '\n' && r != '\t': // control chars except newline/tab
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// buildTaskSystemPrompt builds the --append-system-prompt string for task sessions.
// It is passed as a CLI flag, so it never appears as PTY-echoed text in the terminal.
func buildTaskSystemPrompt(tc *TaskContext, projectPath string) string {
	var parts []string
	parts = append(parts, "===========================================================")
	parts = append(parts, "TASK CONTEXT")
	parts = append(parts, "===========================================================")
	parts = append(parts, "")
	parts = append(parts, "You are Claude Code assisting with a task from the markdown knowledge base task tracker.")
	parts = append(parts, "MCP server 'markdown-editor' gives you access to notes, folders and tasks.")
	parts = append(parts, "")
	parts = append(parts, "TASK ID: "+tc.ID)
	parts = append(parts, "TITLE: "+sanitizePromptText(tc.Title))
	parts = append(parts, "Status: "+tc.Status+" | Priority: "+tc.Priority)
	if tc.Description != "" {
		parts = append(parts, "Description: "+sanitizePromptText(tc.Description))
	}
	if tc.FolderPath != "" {
		parts = append(parts, "Project folder: "+tc.FolderPath)
	}
	if projectPath != "" {
		parts = append(parts, "")
		parts = append(parts, "Project files ("+projectPath+"):")
		parts = append(parts, "  - list_project_files(pattern), read_project_file(path)")
		parts = append(parts, "  - search_project(query, pattern)")
		parts = append(parts, "  - git_status(), git_diff(), run_tests(command)")
	}
	parts = append(parts, "")
	parts = append(parts, "===========================================================")
	parts = append(parts, "AVAILABLE MCP TOOLS")
	parts = append(parts, "===========================================================")
	parts = append(parts, "")
	parts = append(parts, "Task management:")
	parts = append(parts, "  - get_task(id)                         — get full task details")
	parts = append(parts, "  - update_task(id, ...)                 — update status/priority/description")
	parts = append(parts, "  - move_task(id, status)                — change column")
	parts = append(parts, "  - add_task_comment(id, content)        — add a comment")
	parts = append(parts, "  - list_tasks(project_id, status)       — list tasks")
	parts = append(parts, "  - search_tasks(query)                  — search tasks and comments")
	parts = append(parts, "")
	parts = append(parts, "Notes (for context):")
	parts = append(parts, "  - search_notes(query, summary_only=true, limit=10)")
	parts = append(parts, "  - read_note(path)")
	if tc.FolderPath != "" {
		parts = append(parts, "  - list_notes_summary(\""+tc.FolderPath+"\")")
	}
	parts = append(parts, "")
	parts = append(parts, "===========================================================")
	parts = append(parts, "RULES")
	parts = append(parts, "===========================================================")
	parts = append(parts, "  - Chat in RUSSIAN, code/commits in ENGLISH")
	parts = append(parts, "  - Be direct, challenge wrong decisions")
	parts = append(parts, "  - FORBIDDEN: --no-gpg-sign, push to main")
	parts = append(parts, "  - REQUIRED: git commit --signoff")
	parts = append(parts, "===========================================================")
	return strings.Join(parts, "\n")
}

// getFolderProjectPath searches for projectPath up the folder hierarchy
// Returns empty string if no projectPath found
func (h *Handler) getFolderProjectPath(folderPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Split folder path into parts
	parts := strings.Split(folderPath, "/")

	// Search from most specific to least specific (child to parent)
	for i := len(parts); i > 0; i-- {
		currentPath := strings.Join(parts[:i], "/")

		// Query folder using storage
		folder, err := h.storage.GetFolder(ctx, currentPath)
		if err == nil && folder.ProjectPath != "" {
			h.logger.Info("found project path from folder",
				slog.String("folder", currentPath),
				slog.String("projectPath", folder.ProjectPath),
			)
			return folder.ProjectPath
		}
	}

	return ""
}
