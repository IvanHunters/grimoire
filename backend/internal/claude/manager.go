package claude

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// lookupHistoricalNameByShort scans every JSONL filename for one
// starting with `prefix` and returns its display name (ai-title or
// first-prompt fallback). Used to translate "grimoire-resume-<short>"
// daemon names back into human-readable labels for listing endpoints.
// Returns "" if nothing matches; caller falls back gracefully.
func lookupHistoricalNameByShort(prefix string) string {
	if len(prefix) != 8 {
		return ""
	}
	root, err := discovery.ProjectsRoot()
	if err != nil {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", prefix+"*.jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	hdr, err := discovery.ReadHeader(matches[0])
	if err != nil {
		return ""
	}
	return hdr.Name
}

// isUUIDLike reports whether s has the canonical 8-4-4-4-12 UUID shape.
// We treat such sessionIDs as "potentially identifying a claude daemon
// session" — clickers from SessionsModal / Sidebar carry full UUIDs.
// Grimoire-managed sessions usually have prefixes like "note-" / "global-",
// so this filter avoids false positives for our own keys.
func isUUIDLike(s string) bool {
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
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// useDaemonBackend reports whether new sessions should be hosted by the
// claude daemon instead of an in-process subprocess. Toggled by the
// USE_DAEMON_BACKEND env var (any non-empty value enables).
//
// This is intentionally an env-only switch for now — daemon-backend is
// dev-validated, not yet a user-facing setting. When we're confident,
// we'll flip the default and the flag becomes inverted (USE_SUBPROCESS).
func useDaemonBackend() bool {
	return os.Getenv("USE_DAEMON_BACKEND") != ""
}

// SessionStorage interface for persisting sessions
type SessionStorage interface {
	SaveSession(ctx context.Context, session *models.ClaudeSession) error
	GetSession(ctx context.Context, sessionID string) (*models.ClaudeSession, error)
	UpdateSessionStatus(ctx context.Context, sessionID string, status string) error
	UpdateSessionActivity(ctx context.Context, sessionID string) error
	UpdateSessionName(ctx context.Context, sessionID string, name string) error
	UpdateSessionMessages(ctx context.Context, sessionID string, messages []models.ClaudeMessage) error
	ListActiveSessions(ctx context.Context) ([]*models.ClaudeSession, error)
}

// SessionManager manages all Claude sessions
type SessionManager struct {
	sessions      map[string]*ClaudeSession
	storage       SessionStorage
	mongoURI      string
	mongoDatabase string
	mu            sync.RWMutex
	logger        *slog.Logger
}

// Global session manager instance
var globalManager *SessionManager
var managerOnce sync.Once

// SweepOrphanWorkers kills daemon workers that look orphan: name
// starts with our structured "grimoire-" prefix AND no on-disk JSONL
// exists for the worker's session UUID (so it never wrote anything,
// can't be resumed, has no chat history to preserve). These leak
// across backend restarts when claude --bg dispatches were never
// typed into. Call once at startup so the sidebar doesn't show
// useless "···<short>" placeholders.
func SweepOrphanWorkers(logger *slog.Logger) {
	client := &daemon.Client{Logger: logger}
	jobs, err := client.ListSessions()
	if err != nil {
		logger.Warn("orphan sweep: list failed", slog.Any("error", err))
		return
	}
	killed := 0
	for _, j := range jobs {
		if !strings.HasPrefix(j.Name, "grimoire-") {
			continue // someone else's worker; leave alone
		}
		// Check if a JSONL exists for this worker. If yes, it had real
		// activity — preserve it.
		if path, err := discovery.SessionPath(j.SessionID); err == nil && path != "" {
			continue
		}
		// Empty grimoire-* worker. Kill it.
		if err := client.Remove(j.Short); err != nil {
			logger.Debug("orphan kill failed",
				slog.String("short", j.Short),
				slog.String("name", j.Name),
				slog.Any("error", err),
			)
			continue
		}
		killed++
	}
	if killed > 0 {
		logger.Info("orphan workers swept on startup",
			slog.Int("killed", killed),
			slog.Int("total_jobs", len(jobs)),
		)
	}
}

// GetSessionManager returns the global session manager instance
func GetSessionManager(logger *slog.Logger, storage SessionStorage, mongoURI string, mongoDatabase string) *SessionManager {
	managerOnce.Do(func() {
		globalManager = &SessionManager{
			sessions:      make(map[string]*ClaudeSession),
			storage:       storage,
			mongoURI:      mongoURI,
			mongoDatabase: mongoDatabase,
			logger:        logger,
		}
		// Hook the listing package so it can mark sessions that are
		// alive in our manager as "live" even when the daemon worker
		// has paused. Closes the consistency gap between SessionsModal
		// "Active now" and what the user has actually open.
		SetManagedLiveProvider(func() map[string]bool {
			globalManager.mu.RLock()
			defer globalManager.mu.RUnlock()
			out := make(map[string]bool, len(globalManager.sessions))
			for id := range globalManager.sessions {
				out[id] = true
			}
			return out
		})
		// Daemon UUIDs of every managed session. Listing dedups its
		// daemon-side rows against this so the live worker of an open
		// chat doesn't appear as a separate "···<short>" orphan row
		// (deleting which would kill the active chat).
		SetManagedDaemonUUIDProvider(func() map[string]bool {
			globalManager.mu.RLock()
			defer globalManager.mu.RUnlock()
			out := make(map[string]bool, len(globalManager.sessions))
			for _, s := range globalManager.sessions {
				if s.DaemonUUID != "" {
					out[s.DaemonUUID] = true
				}
			}
			return out
		})
		// daemonUUID → {grimoireID, Name}. Listing rewrites its
		// daemon-side rows to use the grimoire-side ID (so click
		// routes through our manager) and the user-given name (so
		// fresh forks show "MyForkName" instead of "···short").
		SetManagedSessionInfoProvider(func() map[string]ManagedSessionInfo {
			globalManager.mu.RLock()
			defer globalManager.mu.RUnlock()
			out := make(map[string]ManagedSessionInfo, len(globalManager.sessions))
			for _, s := range globalManager.sessions {
				if s.DaemonUUID == "" {
					continue
				}
				out[s.DaemonUUID] = ManagedSessionInfo{
					GrimoireID: s.ID,
					Name:       s.Name,
				}
			}
			return out
		})
	})
	return globalManager
}

// GetOrAttach attaches to an EXISTING daemon worker by its full UUID,
// without spawning a new session. The daemon must already host this
// session — typically because the user (or another grimoire instance)
// dispatched it earlier and it's still running in op:list.
//
// Unlike GetOrResume (which uses claude --resume to start a fresh
// process from an on-disk transcript), this just opens an attach
// bridge to the live PTY. No new process, no new conversation —
// continuing the same one in real time, possibly alongside another
// attacher viewing the same TUI.
//
// Returns a clear error when the session isn't found in the daemon's
// live registry (caller should fall back to GetOrResume in that case).
// Only works with USE_DAEMON_BACKEND=1.
func (m *SessionManager) GetOrAttach(grimoireID string, daemonSessionUUID string) (*ClaudeSession, error) {
	if !useDaemonBackend() {
		return nil, fmt.Errorf("attach requires USE_DAEMON_BACKEND=1")
	}
	if daemonSessionUUID == "" {
		return nil, fmt.Errorf("attach needs daemonSessionUUID")
	}

	// If we already have this session in our map, return it.
	m.mu.RLock()
	if session, ok := m.sessions[grimoireID]; ok {
		m.mu.RUnlock()
		session.UpdateActivity()
		return session, nil
	}
	m.mu.RUnlock()

	newSession, err := startDaemonSessionAttach(grimoireID, daemonSessionUUID, m.logger)
	if err != nil {
		return nil, fmt.Errorf("daemon attach: %w", err)
	}

	m.mu.Lock()
	if existing, raced := m.sessions[grimoireID]; raced {
		m.mu.Unlock()
		go shutdownSession(newSession, m.logger)
		return existing, nil
	}
	m.sessions[grimoireID] = newSession
	m.mu.Unlock()
	return newSession, nil
}

// GetOrResume gets or spawns a session that continues a historical claude
// conversation. Unlike GetOrCreate which always starts fresh, this passes
// --resume <resumeFromUUID> to claude so the daemon-backed session has
// the full prior transcript in context.
//
// resumeFromUUID is the historical session's UUID — same as the JSONL
// filename under ~/.claude/projects/. workingDir MUST be the cwd that
// historical session ran in (claude resolves the transcript via cwd +
// session id). The caller is responsible for looking that up via
// discovery.SessionPath + discovery.ReadHeader.
//
// When fork is true, --fork-session is added so the new chat branches
// off a copy of the transcript and gets its own UUID; the original
// session stays untouched.
//
// Only works with USE_DAEMON_BACKEND=1. Subprocess backend doesn't
// support resume in this code path yet; returns an explicit error.
func (m *SessionManager) GetOrResume(grimoireID string, resumeFromUUID string, workingDir string, sessionName string, fork bool) (*ClaudeSession, error) {
	if !useDaemonBackend() {
		return nil, fmt.Errorf("resume requires USE_DAEMON_BACKEND=1")
	}
	if resumeFromUUID == "" || workingDir == "" {
		return nil, fmt.Errorf("resume needs both resumeFromUUID and workingDir")
	}

	// Fast path: already in memory.
	m.mu.RLock()
	if session, ok := m.sessions[grimoireID]; ok {
		m.mu.RUnlock()
		// Self-heal: earlier code paths stuffed the structured
		// "grimoire-resume-<short>" token into session.Name, which
		// shows up in Sidebar as gibberish. If we land on a fast-path
		// session that still has that token, refresh from the resume
		// caller's nicer name (or leave alone if no better option).
		if strings.HasPrefix(session.Name, "grimoire-resume-") || strings.HasPrefix(session.Name, "grimoire-fork-") {
			if sessionName != "" && !strings.HasPrefix(sessionName, "grimoire-") {
				session.Name = sessionName
			}
		}
		session.UpdateActivity()
		return session, nil
	}
	m.mu.RUnlock()

	newSession, err := startDaemonSessionResume(grimoireID, resumeFromUUID, workingDir, m.mongoURI, m.mongoDatabase, m.logger, sessionName, fork)
	if err != nil {
		return nil, fmt.Errorf("daemon resume: %w", err)
	}

	m.mu.Lock()
	if existing, raced := m.sessions[grimoireID]; raced {
		m.mu.Unlock()
		go shutdownSession(newSession, m.logger)
		return existing, nil
	}
	m.sessions[grimoireID] = newSession
	m.mu.Unlock()

	return newSession, nil
}

// GetOrCreate gets an existing session or creates a new one.
// Lock is held only for map reads/writes; slow operations (subprocess start,
// MongoDB queries) run outside the lock to prevent blocking ListActiveSessions.
func (m *SessionManager) GetOrCreate(sessionID string, dangerousMode bool, workingDir string, sessionName string, systemPrompt string) (*ClaudeSession, error) {
	ctx := context.Background()

	// Fast path: session already in memory.
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if exists {
		if session.DangerousMode == dangerousMode {
			session.UpdateActivity()
			go func() {
				if m.storage != nil {
					m.storage.UpdateSessionActivity(ctx, sessionID)
				}
			}()
			return session, nil
		}

		// Dangerous mode mismatch. For daemon-backed sessions this flag
		// is purely a UI marker — the actual `claude` process inside the
		// daemon worker doesn't observe it. Killing and respawning the
		// daemon worker just because the UI flag flipped destroys the
		// live conversation (and the JSONL scrollback) for no benefit.
		// Just align the flag in-memory and keep going.
		//
		// For subprocess sessions the flag actually changes the spawned
		// process's CLI args (--dangerously-skip-permissions), so we keep
		// the old shutdown-and-recreate behaviour there.
		if session.IsDaemonBacked() {
			m.logger.Info("dangerous_mode flag changed for daemon session, syncing in-memory only",
				slog.String("session_id", sessionID),
				slog.Bool("was", session.DangerousMode),
				slog.Bool("now", dangerousMode),
			)
			session.DangerousMode = dangerousMode
			session.UpdateActivity()
			go func() {
				if m.storage != nil {
					m.storage.UpdateSessionActivity(ctx, sessionID)
				}
			}()
			return session, nil
		}

		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		if err := shutdownSession(session, m.logger); err != nil {
			m.logger.Error("failed to shutdown session for restart", slog.Any("error", err))
		}
		if m.storage != nil {
			m.storage.UpdateSessionStatus(ctx, sessionID, "inactive")
		}
	}

	// Slow path: DB lookup + subprocess creation — all outside the lock.
	var dbSessionName string
	var mcpConfigPath string
	var restoredMessages []models.ClaudeMessage

	if m.storage != nil {
		dbSession, err := m.storage.GetSession(ctx, sessionID)
		if err != nil {
			m.logger.Error("failed to get session from DB", slog.Any("error", err))
		} else if dbSession != nil {
			dbSessionName = dbSession.Name
			mcpConfigPath = dbSession.MCPConfigPath
			restoredMessages = dbSession.Messages
			m.logger.Info("restoring session metadata from DB",
				slog.String("session_id", sessionID),
				slog.String("name", dbSessionName),
				slog.Int("messages_count", len(restoredMessages)),
			)
		}
	}

	var newSession *ClaudeSession
	var spawnErr error
	if useDaemonBackend() {
		// Smart-attach: if sessionID is a UUID (matches an existing
		// daemon worker) we attach to that existing PTY instead of
		// spawning a new one. Covers two cases:
		//   1. External daemon sessions (kvaps spawned via claude --bg)
		//      that we surface in the Sidebar
		//   2. Sessions we own but that fell out of our manager.sessions
		//      map (e.g. a previous resume that's still alive in daemon)
		// Without this, click→open spawns a fresh chat under the same
		// id which looks to the user like "different session, same name".
		if isUUIDLike(sessionID) {
			client := &daemon.Client{Logger: m.logger}
			if jobs, err := client.ListSessions(); err == nil {
				for _, j := range jobs {
					if j.SessionID == sessionID {
						newSession, spawnErr = startDaemonSessionAttach(sessionID, sessionID, m.logger)
						break
					}
				}
				// Also probe for resumed-child by name pattern — the
				// child carries a different daemon UUID, but its name
				// encodes the original short. Reuse that worker rather
				// than spawning yet another resume.
				if newSession == nil && spawnErr == nil {
					expectedChildName := "grimoire-resume-" + sessionID[:8]
					for _, j := range jobs {
						if j.Name == expectedChildName {
							newSession, spawnErr = startDaemonSessionAttach(sessionID, j.SessionID, m.logger)
							break
						}
					}
				}
			}
			// Smart-attach didn't yield a live worker (no live record, or
			// the worker we found exited between op:list and op:attach with
			// ENOJOB). For a UUID-shaped sessionID the historical JSONL is
			// still on disk — spawn a daemon resume so the new chat keeps
			// the conversation context instead of starting blank.
			//
			// CRITICAL: cwd MUST come from the historical JSONL header,
			// not from the caller's workingDir. The caller is the WS
			// handler which derives workingDir from the CURRENTLY-OPEN
			// note's project path — that's the wrong cwd for resuming an
			// unrelated session. `claude --resume <uuid>` locates the
			// transcript via (cwd, sessionId); if cwd doesn't match the
			// one in the JSONL header, claude silently starts a fresh
			// chat with no history.
			if newSession == nil {
				resumeCwd := ""
				if hdrPath, hdrErr := getHistoricalPath(sessionID); hdrErr == nil {
					if hdr, hErr := readHistoricalHeader(hdrPath); hErr == nil {
						resumeCwd = hdr.Cwd
					}
				}
				if resumeCwd != "" {
					m.logger.Info("smart-attach miss, resuming via daemon",
						slog.String("session_id", sessionID),
						slog.String("cwd", resumeCwd),
						slog.Any("prior_attach_err", spawnErr),
					)
					spawnErr = nil
					newSession, spawnErr = startDaemonSessionResume(sessionID, sessionID, resumeCwd, m.mongoURI, m.mongoDatabase, m.logger, sessionName, false)
				}
			}
		}
		if newSession == nil && spawnErr == nil {
			newSession, spawnErr = startDaemonSession(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
		}
		if spawnErr != nil {
			m.logger.Warn("daemon backend failed, falling back to subprocess",
				slog.String("session_id", sessionID),
				slog.Any("error", spawnErr),
			)
			newSession, spawnErr = startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
		}
	} else {
		newSession, spawnErr = startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
	}
	if spawnErr != nil {
		return nil, fmt.Errorf("failed to start claude session: %w", spawnErr)
	}

	if dbSessionName != "" {
		newSession.Name = dbSessionName
	} else if sessionName != "" {
		newSession.Name = sessionName
	} else {
		newSession.Name = "Terminal Session"
	}

	if mcpConfigPath != "" {
		newSession.MCPConfigPath = mcpConfigPath
	}

	if len(restoredMessages) > 0 {
		newSession.Messages = restoredMessages
		m.logger.Info("restored messages from DB",
			slog.String("session_id", sessionID),
			slog.Int("count", len(restoredMessages)),
		)
	}

	// Register in the map (brief lock only for map write).
	m.mu.Lock()
	// Double-check: another goroutine may have created it while we were starting the subprocess.
	if existing, raced := m.sessions[sessionID]; raced {
		m.mu.Unlock()
		// Discard the subprocess we just started.
		go shutdownSession(newSession, m.logger)
		return existing, nil
	}
	m.sessions[sessionID] = newSession
	m.mu.Unlock()

	// Persist to DB asynchronously — doesn't block the caller.
	go func() {
		if m.storage == nil {
			return
		}
		dbRecord := &models.ClaudeSession{
			ID:            newSession.ID,
			Name:          newSession.Name,
			DangerousMode: newSession.DangerousMode,
			WorkingDir:    newSession.WorkingDir,
			MCPConfigPath: newSession.MCPConfigPath,
			Status:        "active",
			Messages:      []models.ClaudeMessage{},
			CreatedAt:     newSession.CreatedAt,
			UpdatedAt:     time.Now(),
			LastActivity:  newSession.LastActivity,
		}
		if err := m.storage.SaveSession(ctx, dbRecord); err != nil {
			m.logger.Error("failed to save session to DB", slog.Any("error", err))
		} else {
			m.logger.Info("session saved to DB", slog.String("session_id", sessionID))
		}
	}()

	return newSession, nil
}

// Get retrieves an existing session
func (m *SessionManager) Get(sessionID string) (*ClaudeSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return session, nil
}

// Close closes a specific session
func (m *SessionManager) Close(sessionID string) error {
	// Hold lock only for map operation — release before slow ops.
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := shutdownSession(session, m.logger); err != nil {
		return fmt.Errorf("failed to shutdown session: %w", err)
	}

	if m.storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.storage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
			m.logger.Error("failed to update session status in DB", slog.Any("error", err))
		}
	}

	return nil
}

// CloseInactive closes sessions that have been inactive for the given timeout
func (m *SessionManager) CloseInactive(timeout time.Duration) {
	// Collect inactive sessions and remove from map under lock — release before slow ops.
	m.mu.Lock()
	type toClose struct {
		id      string
		session *ClaudeSession
	}
	var inactive []toClose
	for id, session := range m.sessions {
		if session.IsInactive(timeout) {
			inactive = append(inactive, toClose{id, session})
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, item := range inactive {
		m.logger.Info("detaching inactive session",
			slog.String("session_id", item.id),
			slog.Duration("inactive_for", time.Since(item.session.LastActivity)),
		)

		// Detach only — same reasoning as CloseAll. Inactivity in
		// grimoire ≠ user wants the daemon worker dead. Worker keeps
		// running in daemon; user can re-attach via sidebar / Sessions
		// Modal when they come back.
		if err := detachSession(item.session, m.logger); err != nil {
			m.logger.Error("failed to detach inactive session",
				slog.String("session_id", item.id),
				slog.Any("error", err),
			)
		}

		if m.storage != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.storage.UpdateSessionStatus(ctx, item.id, "inactive"); err != nil {
				m.logger.Error("failed to update session status in DB", slog.Any("error", err))
			}
			cancel()
		}
	}
}

// CloseAll detaches all sessions (for graceful grimoire shutdown).
// For daemon-backed sessions this DOES NOT kill the daemon worker —
// it just closes our local PTY attach so the worker survives our
// restart. User can re-attach next time grimoire comes up. Subprocess
// sessions die regardless (they're our child processes, can't survive
// our exit). This is the whole point of the daemon backend.
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	old := m.sessions
	m.sessions = make(map[string]*ClaudeSession)
	m.mu.Unlock()

	m.logger.Info("detaching all claude sessions on shutdown", slog.Int("count", len(old)))

	for id, session := range old {
		if err := detachSession(session, m.logger); err != nil {
			m.logger.Error("failed to detach session",
				slog.String("session_id", id),
				slog.Any("error", err),
			)
		}
	}
}

// MonitorInactiveSessions starts a background goroutine to cleanup inactive sessions
func (m *SessionManager) MonitorInactiveSessions(timeout time.Duration, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			m.CloseInactive(timeout)
			m.saveAllSessionMessages()
		}
	}()
}

// saveAllSessionMessages saves messages for all active sessions
func (m *SessionManager) saveAllSessionMessages() {
	m.mu.RLock()
	sessionIDs := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range sessionIDs {
		if err := m.SaveSessionMessages(id); err != nil {
			m.logger.Debug("failed to save messages for session",
				slog.String("session_id", id),
				slog.Any("error", err),
			)
		}
	}
}

// SaveSessionMessages saves session messages to storage
func (m *SessionManager) SaveSessionMessages(sessionID string) error {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if m.storage == nil {
		return nil // Storage not configured
	}

	ctx := context.Background()
	messages := session.GetMessages()

	if err := m.storage.UpdateSessionMessages(ctx, sessionID, messages); err != nil {
		m.logger.Error("failed to save session messages",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		return err
	}

	m.logger.Debug("saved session messages to DB",
		slog.String("session_id", sessionID),
		slog.Int("count", len(messages)),
	)

	return nil
}

// ListActiveSessions returns list of all active sessions
func (m *SessionManager) ListActiveSessions() []*models.ClaudeSession {
	// Pre-fetch daemon records once so each session lookup is O(1). We
	// don't want to hold m.mu while making a daemon RPC, so this happens
	// outside the lock below.
	var jobsByShort map[string]daemon.Record
	var jobsByUUID map[string]daemon.Record
	var jobs []daemon.Record
	if useDaemonBackend() {
		client := &daemon.Client{}
		if list, err := client.ListSessions(); err == nil {
			jobs = list
			jobsByShort = make(map[string]daemon.Record, len(list))
			jobsByUUID = make(map[string]daemon.Record, len(list))
			for _, j := range list {
				jobsByShort[j.Short] = j
				jobsByUUID[j.SessionID] = j
			}
		}
	}

	m.mu.RLock()
	sessions := make([]*models.ClaudeSession, 0, len(m.sessions))
	known := make(map[string]bool, len(m.sessions))
	for _, session := range m.sessions {
		known[session.ID] = true
		// Track daemon UUID too so we don't double-list our own sessions
		// when also pulling from the daemon below.
		if session.DaemonUUID != "" {
			known[session.DaemonUUID] = true
		}
		// Pick the most user-friendly name. Generic "Terminal Session"
		// and "grimoire-*" tokens are useless for the UI — for
		// UUID-style session IDs we can recover the JSONL ai-title
		// instead. This catches sessions spawned before the resume
		// name-lookup fix landed.
		name := session.Name
		if (name == "Terminal Session" || name == "" || strings.HasPrefix(name, "grimoire-")) && isUUIDLike(session.ID) {
			if friendly := lookupHistoricalNameByShort(session.ID[:8]); friendly != "" {
				name = friendly
			}
		}
		out := &models.ClaudeSession{
			ID:            session.ID,
			Name:          name,
			DangerousMode: session.DangerousMode,
			WorkingDir:    session.WorkingDir,
			MCPConfigPath: session.MCPConfigPath,
			Status:        "active",
			Messages:      []models.ClaudeMessage{}, // Don't include full messages in list
			CreatedAt:     session.CreatedAt,
			UpdatedAt:     time.Now(),
			LastActivity:  session.LastActivity,
		}
		// Live state for the sidebar status badge. For daemon-backed
		// sessions we look up by short id (cheap, just hashtable). For
		// subprocess sessions, synthesize "active" so the UI knows the
		// PTY is alive even though we don't have richer info.
		if session.IsDaemonBacked() {
			if rec, ok := jobsByShort[session.DaemonShort]; ok {
				out.Tempo = rec.Tempo
				out.State = rec.State
				out.Detail = rec.Detail
				out.Needs = rec.Needs
			} else {
				out.Tempo = "idle"
				out.State = "running"
				out.Detail = "in grimoire memory"
			}
		} else {
			out.Tempo = "active"
			out.State = "running"
		}
		sessions = append(sessions, out)
	}
	m.mu.RUnlock()

	// Also include live daemon sessions not in our manager — e.g. ones
	// kvaps spawned via `claude --bg` directly, or sessions that
	// survived a backend restart but haven't been reattached yet.
	// Sidebar uses this list to surface "all open terminals", not just
	// what grimoire spawned. Resume-children get their daemon name
	// rewritten to their parent's display name so Sidebar shows
	// human-readable labels instead of the structured token.
	if useDaemonBackend() {
		now := time.Now()
		for _, j := range jobs {
			if known[j.SessionID] {
				continue
			}
			name := j.Name
			if strings.HasPrefix(name, "grimoire-resume-") {
				// Pull the readable name from the parent historical
				// JSONL. parentShort is encoded right in the name.
				parentShort := strings.TrimPrefix(name, "grimoire-resume-")
				if friendly := lookupHistoricalNameByShort(parentShort); friendly != "" {
					name = friendly
				}
			}
			sessions = append(sessions, &models.ClaudeSession{
				ID:           j.SessionID,
				Name:         name,
				Status:       "active",
				WorkingDir:   j.Cwd,
				Messages:     []models.ClaudeMessage{},
				CreatedAt:    time.UnixMilli(j.StartedAt),
				UpdatedAt:    now,
				LastActivity: time.UnixMilli(j.StartedAt),
				Tempo:        j.Tempo,
				State:        j.State,
				Detail:       j.Detail,
				Needs:        j.Needs,
			})
			_ = jobsByUUID // keep map referenced even if unused; avoids lint warning
		}
	}

	// Sort by CreatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions
}

// SessionStatus is the snapshot returned by GetSessionStatus. Fields map
// 1:1 with daemon.Record for daemon-backed sessions; subprocess sessions
// get synthesized values (tempo=active, state=running) since the local
// PTY doesn't expose richer info.
type SessionStatus struct {
	SessionID    string `json:"sessionId"`
	DaemonBacked bool   `json:"daemonBacked"`
	DaemonShort  string `json:"daemonShort,omitempty"`
	DaemonUUID   string `json:"daemonUuid,omitempty"`
	Tempo        string `json:"tempo"`  // idle | active | blocked | unknown
	State        string `json:"state"`  // working | blocked | done | failed | stopped | running | unknown
	Detail       string `json:"detail"` // human-readable, e.g. "Running tests…"
	Needs        string `json:"needs"`  // when blocked, the question waiting on user
}

// GetSessionStatus reports the current activity state of a session.
//
// For daemon-backed sessions it queries op:list and finds the matching
// short id. If the daemon doesn't know about the session (it died or was
// removed externally), returns tempo=unknown state=unknown so the UI can
// show "?" instead of stale data.
//
// For subprocess sessions it returns a synthetic alive/dead status — the
// local PTY layer doesn't expose Haiku-summarised "what is it doing".
func (m *SessionManager) GetSessionStatus(sessionID string) (SessionStatus, error) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return SessionStatus{
			SessionID: sessionID,
			Tempo:     "unknown",
			State:     "unknown",
			Detail:    "session not in memory",
		}, nil
	}

	if !session.IsDaemonBacked() {
		// Subprocess: alive if Cmd hasn't exited yet.
		state := "running"
		tempo := "active"
		if session.Cmd != nil && session.Cmd.ProcessState != nil && session.Cmd.ProcessState.Exited() {
			state = "done"
			tempo = "idle"
		}
		return SessionStatus{
			SessionID:    sessionID,
			DaemonBacked: false,
			Tempo:        tempo,
			State:        state,
			Detail:       "subprocess",
		}, nil
	}

	// Daemon-backed: ask the daemon for the live record.
	jobs, err := session.DaemonClient.ListSessions()
	if err != nil {
		return SessionStatus{
			SessionID:    sessionID,
			DaemonBacked: true,
			DaemonShort:  session.DaemonShort,
			DaemonUUID:   session.DaemonUUID,
			Tempo:        "unknown",
			State:        "unknown",
			Detail:       fmt.Sprintf("daemon error: %v", err),
		}, nil
	}
	for _, j := range jobs {
		if j.Short == session.DaemonShort {
			return SessionStatus{
				SessionID:    sessionID,
				DaemonBacked: true,
				DaemonShort:  j.Short,
				DaemonUUID:   j.SessionID,
				Tempo:        j.Tempo,
				State:        j.State,
				Detail:       j.Detail,
				Needs:        j.Needs,
			}, nil
		}
	}
	// Not found in daemon list — likely removed externally.
	return SessionStatus{
		SessionID:    sessionID,
		DaemonBacked: true,
		DaemonShort:  session.DaemonShort,
		DaemonUUID:   session.DaemonUUID,
		Tempo:        "unknown",
		State:        "unknown",
		Detail:       "daemon does not have this session",
	}, nil
}

// RenameSession updates the display name of an in-memory session
func (m *SessionManager) RenameSession(sessionID string, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[sessionID]; ok {
		session.Name = name
	}
}
