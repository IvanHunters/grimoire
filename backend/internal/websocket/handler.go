package websocket

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// resumeCwdFromArchive looks for *.archive.* sidecars next to the live
// JSONL and reads cwd from the most recent one. After compact the live
// file gets truncated and may lose its cwd-bearing header event — the
// archive sibling still has the full history.
func resumeCwdFromArchive(sessionUUID string) string {
	livePath, err := discovery.SessionPath(sessionUUID)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(livePath)
	matches, err := filepath.Glob(filepath.Join(dir, sessionUUID+".jsonl.archive.*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		fi, ei := os.Stat(matches[i])
		fj, ej := os.Stat(matches[j])
		if ei != nil || ej != nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})
	hdr, err := discovery.ReadHeader(matches[0])
	if err != nil {
		return ""
	}
	return hdr.Cwd
}

// isStubJSONL reports whether a transcript file contains zero
// user/assistant events — only metadata (mode, permission-mode,
// last-prompt, queue-operation, etc.). Post-compact stubs land in this
// state when compact aggressively trims the conversation but leaves
// the housekeeping events behind. `claude --resume` on such a file
// fails with "No conversation found", so we detect and route to a
// fresh spawn instead.
func isStubJSONL(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	// Scan generously — slash-command sessions stack up dozens of
	// metadata lines before the first real message, and we'd rather
	// false-negative (treat as resume-able) than false-positive.
	const scanLimit = 400
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; i < scanLimit && scanner.Scan(); i++ {
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type == "user" || ev.Type == "assistant" {
			return false
		}
	}
	return true
}

// resumeCwdFromDaemonRoster asks the cc-daemon for the live worker
// record matching this sessionUUID and returns its cwd. Works for
// sessions where (a) the JSONL header is truncated AND (b) no archive
// sidecar exists yet, but the worker is still running and the daemon
// remembers where it was started.
func resumeCwdFromDaemonRoster(sessionUUID string) string {
	client := &daemon.Client{}
	jobs, err := client.ListSessions()
	if err != nil {
		return ""
	}
	for _, j := range jobs {
		if j.SessionID == sessionUUID && j.Cwd != "" {
			return j.Cwd
		}
	}
	return ""
}

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
	cfg            *config.Config
	manager        *claude.SessionManager
	storage        *storage.MongoStorage
	sessionStorage *storage.SessionStorage
	logger         *slog.Logger
}

// NewHandler creates a new WebSocket handler
func NewHandler(cfg *config.Config, manager *claude.SessionManager, store *storage.MongoStorage, sessionStorage *storage.SessionStorage, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:            cfg,
		manager:        manager,
		storage:        store,
		sessionStorage: sessionStorage,
		logger:         logger,
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

	// Smart routing: when the WebSocket sends a UUID-shaped sessionId
	// AND there's a JSONL on disk for it AND no explicit attach/resume
	// already requested → auto-route through resume. This catches the
	// "sidebar click on a session whose grimoire id drifted to a
	// daemon-assigned UUID" case, where the cached manager entry may
	// point at an unrelated worker (wrong cwd, wrong content). Resume
	// uses the JSONL's actual cwd from its header — always correct
	// regardless of what manager cached.
	//
	// MUST run BEFORE the workingDir resolution below so the resume
	// branch picks the right cwd from JSONL header instead of the
	// fallback DetermineWorkingDir.
	if msg.AttachToSessionID == "" && msg.ResumeFromSessionID == "" && isLikelyUUID(msg.SessionID) {
		// Skip auto-resume when the manager already has a live entry for
		// this grimoireID (typical when MCP start_session was just used
		// to set it up). Routing back through resume would re-parse the
		// possibly-truncated post-compact JSONL header — which lacks cwd
		// — and surface "resume target has no cwd in header" even though
		// the worker is fine. Just hit the GetOrCreate fast-path.
		if _, alive := h.manager.Get(msg.SessionID); alive == nil {
			h.logger.Info("init: session already live in manager, skipping auto-resume",
				slog.String("session_id", msg.SessionID),
			)
		} else if _, pathErr := discovery.SessionPath(msg.SessionID); pathErr == nil {
			msg.ResumeFromSessionID = msg.SessionID
			h.logger.Info("init: auto-routing UUID sessionId through resume",
				slog.String("session_id", msg.SessionID),
			)
		}
	}

	// Determine working directory. For resume flows we override with the
	// historical session's cwd — claude resolves the transcript via
	// (cwd, sessionId), so the cwd must match. For attach flows the
	// daemon's record carries the cwd, we pick it up in GetOrAttach
	// itself; here we just leave workingDir blank for that path.
	var workingDir string
	if msg.AttachToSessionID != "" {
		// Cwd comes from the daemon record; no lookup needed.
	} else if msg.ResumeFromSessionID != "" {
		path, lookupErr := discovery.SessionPath(msg.ResumeFromSessionID)
		if lookupErr != nil {
			h.logger.Error("resume target not found",
				slog.String("resume_from", msg.ResumeFromSessionID),
				slog.Any("error", lookupErr),
			)
			conn.WriteJSON(WSResponse{
				Type:  "error",
				Error: "Historical session not found on disk",
			})
			return
		}
		header, hdrErr := discovery.ReadHeader(path)
		workingDir = header.Cwd
		if hdrErr != nil || workingDir == "" {
			// JSONL header empty (most often: compact truncated the file
			// so the cwd-bearing event got rotated to the .archive.*
			// sidecar). Fall back to (a) the archive sibling, then (b)
			// daemon roster's record for this UUID. Either path beats
			// surfacing "could not determine cwd" to the user when the
			// information is right there in adjacent state.
			if archiveCwd := resumeCwdFromArchive(msg.ResumeFromSessionID); archiveCwd != "" {
				workingDir = archiveCwd
				h.logger.Info("resume cwd resolved from archive sibling",
					slog.String("resume_from", msg.ResumeFromSessionID),
					slog.String("cwd", workingDir),
				)
			} else if rosterCwd := resumeCwdFromDaemonRoster(msg.ResumeFromSessionID); rosterCwd != "" {
				workingDir = rosterCwd
				h.logger.Info("resume cwd resolved from daemon roster",
					slog.String("resume_from", msg.ResumeFromSessionID),
					slog.String("cwd", workingDir),
				)
			} else {
				h.logger.Error("resume target has no cwd in header (no archive or roster fallback)",
					slog.String("resume_from", msg.ResumeFromSessionID),
					slog.Any("error", hdrErr),
				)
				conn.WriteJSON(WSResponse{
					Type:  "error",
					Error: "Could not determine cwd for resume",
				})
				return
			}
		} else {
			h.logger.Info("resume cwd resolved",
				slog.String("resume_from", msg.ResumeFromSessionID),
				slog.String("cwd", workingDir),
			)
		}

		// Compact-stub guard: live JSONL may have valid header (cwd
		// resolved above from archive/roster) but zero user/assistant
		// events — that's a post-compact stub. `claude --resume` on
		// such a file exits with "No conversation found" and the
		// worker keeps respawning. Downgrade to a fresh spawn in the
		// same cwd + same UUID — the historical archive sidecar stays
		// on disk for investigation, conversation continues forward.
		if isStubJSONL(path) {
			h.logger.Info("resume target is a compact stub, downgrading to fresh spawn (same UUID)",
				slog.String("resume_from", msg.ResumeFromSessionID),
				slog.String("cwd", workingDir),
			)
			msg.ResumeFromSessionID = ""
		}
	} else {
		var ddErr error
		workingDir, ddErr = claude.DetermineWorkingDir(msg.CurrentNote, folderProjectPath, msg.SessionID)
		if ddErr != nil {
			h.logger.Error("failed to determine working directory", slog.Any("error", ddErr))
			conn.WriteJSON(WSResponse{
				Type:  "error",
				Error: "Failed to determine working directory",
			})
			return
		}
	}

	// Determine session name from current note or task.
	// Only apply the note's title to genuinely note-bound sessions
	// (sessionId starting with "note-"). For global-* tabs, daemon
	// UUIDs, or attach/resume flows, the note in the editor is
	// incidental — the session has its own identity (daemon name,
	// JSONL ai-title). Naming an unrelated chat after whatever note
	// happened to be open created the "every session shows the same
	// title" confusion users reported.
	sessionName := ""
	isNoteBound := strings.HasPrefix(msg.SessionID, "note-") && !strings.HasPrefix(msg.SessionID, "note-task-")
	// Explicit name from the WS payload wins — fork-from-kebab,
	// rename-on-the-fly, or anything that lets the user pick a label.
	// Reject "grimoire-…" tokens (only sent in error / by integration
	// tests that forwarded the structured name verbatim).
	if msg.SessionName != "" && !strings.HasPrefix(msg.SessionName, "grimoire-") {
		sessionName = msg.SessionName
	} else if isNoteBound && msg.CurrentNote != nil && msg.CurrentNote.Name != "" {
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

	// Mongo overlay (= explicit user rename) wins over any other name
	// source. Without this, opening a session via WS init / restart /
	// MCP start_session would use the structured name passed by the
	// caller (or empty) and the chat panel would show "(unnamed)" /
	// the first prompt / a fork token — even though the user already
	// renamed it. We re-check here right before spawn so the manager
	// entry gets the canonical name and `session.GetName()` everywhere
	// downstream returns the right string.
	if h.sessionStorage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		if overlay, err := h.sessionStorage.ListNameOverrides(ctx); err == nil {
			if n := overlay[msg.SessionID]; n != "" {
				sessionName = n
			} else if msg.ResumeFromSessionID != "" {
				if n := overlay[msg.ResumeFromSessionID]; n != "" {
					sessionName = n
				}
			} else if msg.AttachToSessionID != "" {
				if n := overlay[msg.AttachToSessionID]; n != "" {
					sessionName = n
				}
			}
		}
		cancel()
	}

	// Stash frontend cols/rows for the immediate daemon Dispatch/Attach.
	// Without this, claude renders initial scrollback at the daemon
	// default 80x24, and the subsequent terminal_resize SIGWINCH only
	// repaints the current screen — scrollback retains 80-col wrap,
	// xterm shows misaligned text until claude redraws.
	if msg.Cols > 0 && msg.Rows > 0 {
		claude.SetPendingDims(msg.SessionID, msg.Cols, msg.Rows)
	}

	// Write the user-given name into the overlay keyed by grimoireID
	// BEFORE spawn so the very next sidebar poll picks it up — even
	// if it lands before manager.sessions[grimoireID] is populated.
	// Listing.go's managedInfo also feeds the name, but only after
	// the spawn goroutine finishes. Belt and braces.
	if msg.SessionName != "" && !strings.HasPrefix(msg.SessionName, "grimoire-") && h.sessionStorage != nil {
		go func(id, name string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.sessionStorage.UpsertSessionName(ctx, id, name); err != nil {
				h.logger.Warn("preflight overlay write (non-fatal)",
					slog.String("session_id", id), slog.Any("error", err))
			}
		}(msg.SessionID, msg.SessionName)
	}

	// Spawn (or attach to existing) session. Three branches:
	//   AttachToSessionID → daemon.Attach on existing live worker
	//   ResumeFromSessionID → claude --resume from on-disk JSONL
	//   default → fresh spawn (subprocess or daemon dispatch)
	var session *claude.ClaudeSession
	var spawnErr error
	switch {
	case msg.AttachToSessionID != "":
		session, spawnErr = h.manager.GetOrAttach(msg.SessionID, msg.AttachToSessionID)
	case msg.ResumeFromSessionID != "":
		session, spawnErr = h.manager.GetOrResume(msg.SessionID, msg.ResumeFromSessionID, workingDir, sessionName, msg.ResumeFork)
	default:
		session, spawnErr = h.manager.GetOrCreate(msg.SessionID, msg.DangerousMode, workingDir, sessionName, systemPrompt)
	}
	if spawnErr != nil {
		h.logger.Error("failed to create session", slog.Any("error", spawnErr))
		conn.WriteJSON(WSResponse{
			Type:  "error",
			Error: "Failed to create Claude session",
		})
		return
	}

	// Persist the user-given name to the Mongo overlay keyed by the
	// daemon's session UUID. The listing code reads overlay first, so
	// after this write the sidebar shows the human name instead of the
	// daemon's "grimoire-fork-…" structured token. Skip for note-bound
	// sessions: they don't have a separate daemon UUID exposed in the
	// listing the same way, and the listing already pulls the right
	// name from the note path. Best-effort — log on failure.
	if msg.SessionName != "" && !strings.HasPrefix(msg.SessionName, "grimoire-") && h.sessionStorage != nil && session.DaemonUUID != "" {
		go func(uuid, name string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Upsert (not Update) — for fresh forks/resumes the Mongo
			// record keyed by daemon UUID doesn't exist yet (it's
			// keyed by our grimoireID), so a strict Update fails with
			// MatchedCount=0 and the overlay name is never persisted.
			// Listing then falls back to the daemon's "grimoire-fork-…"
			// token which we sanitize to "···<short>" — exactly what
			// the user reported as "хрен знает с каким именем".
			if err := h.sessionStorage.UpsertSessionName(ctx, uuid, name); err != nil {
				h.logger.Warn("save overlay name (non-fatal)",
					slog.String("session_uuid", uuid), slog.Any("error", err))
			}
		}(session.DaemonUUID, msg.SessionName)
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
	} else if msg.SkipContextPrompt {
		// Handler-internal hint (e.g. from restart) says don't inject
		// context. Mark sent so future reconnects also skip.
		session.MarkContextPromptSent()
	} else if msg.CurrentNote != nil && !session.HasContextPromptSent() && msg.ResumeFromSessionID == "" {
		// Send automatic context prompt — once per session lifetime,
		// AND only on fresh spawns. When the session was resumed via
		// `claude --bg --resume`, the JSONL already carries the prior
		// SESSION CONTEXT in claude's model state, so re-pasting it
		// just spams the visible terminal with duplicate text.
		session.MarkContextPromptSent()
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

	// Mark "real user activity" for sidebar pill heuristics. Set under
	// session lock — direct field assignment would race with
	// ListActiveSessions and ShutdownWorker.
	session.MarkUserInput()

	// WriteInput snapshots session.PTY under lock, returns
	// ErrWorkerShutdown if the worker has been torn down (compact /
	// restart), and serialises against SendMessage/Stop via writeMu.
	// Crashes-on-nil and concurrent-write tears can no longer happen.
	if err := session.WriteInput([]byte(msg.Content)); err != nil {
		if claude.IsShutdownErr(err) {
			// Worker is down — quietly drop the keystroke. ChatPanel
			// will surface "session restarting" via its status poll;
			// no point spamming logs.
			return
		}
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

// handleRestartSession restarts a session with preserved history.
//
// Capture the prior daemon worker's UUID BEFORE Close kills it — that
// UUID is the JSONL file claude has been writing to, and we want the
// new process to continue from it via `claude --bg --resume <uuid>`.
// Otherwise restart spawns a blank claude that has no memory of the
// conversation and re-injects the entire SESSION CONTEXT wall.
func (h *Handler) handleRestartSession(ctx context.Context, conn *wsWriter, msg *WSMessage) {
	// Restart never wants the SESSION CONTEXT wall re-pasted into the
	// terminal — the user explicitly asked to restart, not be re-onboarded.
	// Flag the init code path to mark ContextPromptSent=true upfront.
	msg.SkipContextPrompt = true

	// Capture both the current DaemonUUID (preferred) and the cwd so
	// we can fall back to scanning the cwd's project dir for the
	// newest JSONL if the daemon-assigned UUID drifted (which happens
	// when the daemon hands back a fresh sessionId on resume).
	resumeFromUUID := ""
	workingDir := ""
	if existing, err := h.manager.Get(msg.SessionID); err == nil && existing != nil {
		resumeFromUUID = existing.DaemonUUID
		workingDir = existing.WorkingDir
	}

	if err := h.manager.Close(msg.SessionID); err != nil {
		h.logger.Warn("failed to close session for restart",
			slog.String("session_id", msg.SessionID),
			slog.Any("error", err),
		)
	}

	// Resolve a JSONL to resume from. Three-step lookup:
	//   1. DaemonUUID matches a transcript on disk (happy path).
	//   2. msg.SessionID itself IS a UUID with a transcript anywhere
	//      under ~/.claude/projects/. Catches the case where the
	//      grimoire id is a raw UUID (sidebar attach / fork sessions)
	//      and the JSONL lives in the ORIGINAL fork-source cwd, not
	//      manager.WorkingDir which may have drifted to default after
	//      a restart cycle.
	//   3. Scan the cwd's project directory for the most-recently-
	//      touched JSONL — last resort when nothing matches by id.
	resolvedResumeUUID := ""
	if resumeFromUUID != "" {
		if _, pathErr := discovery.SessionPath(resumeFromUUID); pathErr == nil {
			resolvedResumeUUID = resumeFromUUID
		}
	}
	if resolvedResumeUUID == "" && isLikelyUUID(msg.SessionID) {
		if _, pathErr := discovery.SessionPath(msg.SessionID); pathErr == nil {
			resolvedResumeUUID = msg.SessionID
			h.logger.Info("restart: recovered via sessionID-as-UUID global lookup",
				slog.String("session_id", msg.SessionID),
				slog.String("stale_daemon_uuid", resumeFromUUID),
				slog.String("cwd_searched_was_wrong", workingDir),
			)
		}
	}
	if resolvedResumeUUID == "" && workingDir != "" {
		uuid, candidateCount := newestJSONLInCwdSafe(workingDir)
		if uuid != "" {
			// Take the newest JSONL in cwd as best-effort resume target.
			// Cross-session cwd-sharing risk: bonding → linstor swap
			// happens only when manager.WorkingDir is wrong (drifted to
			// a shared cwd like cozystack-work/cozystack). With the
			// MCP→Manager wiring fix the manager.WorkingDir now comes
			// from the JSONL header at resume time and stays accurate,
			// so newest-in-cwd is correct ≥99% of the time. The 1% risk
			// is acceptable vs. the current "restart silently opens
			// blank terminal" failure that this fallback prevents.
			resolvedResumeUUID = uuid
			level := slog.LevelInfo
			msgTxt := "restart: DaemonUUID drift detected, recovered via newest-jsonl-in-cwd"
			if candidateCount > 1 {
				level = slog.LevelWarn
				msgTxt += " (multiple JSONLs in cwd — verify the picked one is the right session)"
			}
			h.logger.Log(ctx, level, msgTxt,
				slog.String("session_id", msg.SessionID),
				slog.String("stale_daemon_uuid", resumeFromUUID),
				slog.String("resolved_uuid", uuid),
				slog.String("cwd", workingDir),
				slog.Int("candidates", candidateCount),
			)
		}
	}

	if resolvedResumeUUID != "" {
		msg.ResumeFromSessionID = resolvedResumeUUID
		msg.ResumeFork = false
		h.logger.Info("restart with resume",
			slog.String("session_id", msg.SessionID),
			slog.String("resume_from", resolvedResumeUUID),
		)
	} else {
		h.logger.Info("restart without resume (no transcript on disk)",
			slog.String("session_id", msg.SessionID),
			slog.String("daemon_uuid", resumeFromUUID),
			slog.String("cwd", workingDir),
		)
	}

	h.handleInit(ctx, conn, msg)
}

// isLikelyUUID quickly tells whether a string is shaped like a v4 UUID.
// We use this to gate calling discovery.SessionPath with the
// grimoire-side sessionId — note-task-* / global-* / note-* ids would
// never match a JSONL filename and the glob would be wasted work.
func isLikelyUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
	}
	return true
}

// newestJSONLInCwdSafe is a local thin wrapper preserved for call
// sites; the implementation lives in discovery.NewestJSONLInCwd so
// the api/ package can share it without duplicating the I/O logic.
func newestJSONLInCwdSafe(cwd string) (string, int) {
	return discovery.NewestJSONLInCwd(cwd)
}

// handleTerminalResize updates the PTY window size to match xterm.js dimensions
func (h *Handler) handleTerminalResize(conn *websocket.Conn, msg *WSMessage) {
	if msg.Cols <= 0 || msg.Rows <= 0 {
		return
	}
	h.logger.Debug("resize",
		slog.String("session_id", msg.SessionID),
		slog.Int("cols", msg.Cols),
		slog.Int("rows", msg.Rows),
	)

	session, err := h.manager.Get(msg.SessionID)
	if err != nil {
		return
	}

	// session.Resize routes to creack/pty for subprocess sessions or
	// daemon.AttachConn.Resize for daemon-backed ones.
	if err := session.Resize(msg.Cols, msg.Rows); err != nil {
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
