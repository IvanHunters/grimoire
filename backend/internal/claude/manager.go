package claude

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

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
	})
	return globalManager
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

		// Dangerous mode changed — shut down and recreate.
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

	newSession, err := startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to start claude subprocess: %w", err)
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
		m.logger.Info("closing inactive session",
			slog.String("session_id", item.id),
			slog.Duration("inactive_for", time.Since(item.session.LastActivity)),
		)

		if err := shutdownSession(item.session, m.logger); err != nil {
			m.logger.Error("failed to shutdown inactive session",
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

// CloseAll closes all sessions (for graceful shutdown)
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	old := m.sessions
	m.sessions = make(map[string]*ClaudeSession)
	m.mu.Unlock()

	m.logger.Info("closing all claude sessions", slog.Int("count", len(old)))

	for id, session := range old {
		if err := shutdownSession(session, m.logger); err != nil {
			m.logger.Error("failed to shutdown session",
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
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*models.ClaudeSession, 0, len(m.sessions))

	for _, session := range m.sessions {
		sessions = append(sessions, &models.ClaudeSession{
			ID:            session.ID,
			Name:          session.Name,
			DangerousMode: session.DangerousMode,
			WorkingDir:    session.WorkingDir,
			MCPConfigPath: session.MCPConfigPath,
			Status:        "active",
			Messages:      []models.ClaudeMessage{}, // Don't include full messages in list
			CreatedAt:     session.CreatedAt,
			UpdatedAt:     time.Now(),
			LastActivity:  session.LastActivity,
		})
	}

	// Sort by CreatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions
}

// RenameSession updates the display name of an in-memory session
func (m *SessionManager) RenameSession(sessionID string, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[sessionID]; ok {
		session.Name = name
	}
}
