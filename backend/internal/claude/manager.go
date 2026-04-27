package claude

import (
	"context"
	"fmt"
	"log/slog"
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

// GetOrCreate gets an existing session or creates a new one
func (m *SessionManager) GetOrCreate(sessionID string, dangerousMode bool, workingDir string) (*ClaudeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx := context.Background()

	// Check if session exists in memory
	if session, exists := m.sessions[sessionID]; exists {
		// Update dangerous mode if it changed
		if session.DangerousMode != dangerousMode {
			// Need to restart session with new mode
			if err := shutdownSession(session, m.logger); err != nil {
				m.logger.Error("failed to shutdown session for restart", slog.Any("error", err))
			}
			delete(m.sessions, sessionID)

			// Update status in DB
			if m.storage != nil {
				m.storage.UpdateSessionStatus(ctx, sessionID, "inactive")
			}
		} else {
			// Session exists and mode is the same
			session.UpdateActivity()

			// Update activity in DB
			if m.storage != nil {
				m.storage.UpdateSessionActivity(ctx, sessionID)
			}

			return session, nil
		}
	}

	// Check if session exists in DB (from previous run)
	var sessionName string
	var mcpConfigPath string
	var restoredMessages []models.ClaudeMessage

	if m.storage != nil {
		dbSession, err := m.storage.GetSession(ctx, sessionID)
		if err != nil {
			m.logger.Error("failed to get session from DB", slog.Any("error", err))
		} else if dbSession != nil {
			sessionName = dbSession.Name
			mcpConfigPath = dbSession.MCPConfigPath
			restoredMessages = dbSession.Messages
			m.logger.Info("restoring session metadata from DB",
				slog.String("session_id", sessionID),
				slog.String("name", sessionName),
				slog.Int("messages_count", len(restoredMessages)),
			)
		}
	}

	// Create new subprocess
	session, err := startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start claude subprocess: %w", err)
	}

	// Set name and MCP path if restored from DB
	if sessionName != "" {
		session.Name = sessionName
	} else {
		session.Name = "Terminal Session"
	}

	if mcpConfigPath != "" {
		session.MCPConfigPath = mcpConfigPath
	}

	// Restore messages from DB
	if len(restoredMessages) > 0 {
		session.Messages = restoredMessages
		m.logger.Info("restored messages from DB",
			slog.String("session_id", sessionID),
			slog.Int("count", len(restoredMessages)),
		)
	}

	m.sessions[sessionID] = session

	// Save to DB
	if m.storage != nil {
		dbSession := &models.ClaudeSession{
			ID:            session.ID,
			Name:          session.Name,
			DangerousMode: session.DangerousMode,
			WorkingDir:    session.WorkingDir,
			MCPConfigPath: session.MCPConfigPath,
			Status:        "active",
			Messages:      []models.ClaudeMessage{},
			CreatedAt:     session.CreatedAt,
			UpdatedAt:     time.Now(),
			LastActivity:  session.LastActivity,
		}

		if err := m.storage.SaveSession(ctx, dbSession); err != nil {
			m.logger.Error("failed to save session to DB", slog.Any("error", err))
		} else {
			m.logger.Info("session saved to DB", slog.String("session_id", sessionID))
		}
	}

	return session, nil
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
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx := context.Background()

	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := shutdownSession(session, m.logger); err != nil {
		return fmt.Errorf("failed to shutdown session: %w", err)
	}

	delete(m.sessions, sessionID)

	// Update status in DB
	if m.storage != nil {
		if err := m.storage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
			m.logger.Error("failed to update session status in DB", slog.Any("error", err))
		}
	}

	return nil
}

// CloseInactive closes sessions that have been inactive for the given timeout
func (m *SessionManager) CloseInactive(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx := context.Background()

	for id, session := range m.sessions {
		if session.IsInactive(timeout) {
			m.logger.Info("closing inactive session",
				slog.String("session_id", id),
				slog.Duration("inactive_for", time.Since(session.LastActivity)),
			)

			if err := shutdownSession(session, m.logger); err != nil {
				m.logger.Error("failed to shutdown inactive session",
					slog.String("session_id", id),
					slog.Any("error", err),
				)
			}

			delete(m.sessions, id)

			// Update status in DB
			if m.storage != nil {
				if err := m.storage.UpdateSessionStatus(ctx, id, "inactive"); err != nil {
					m.logger.Error("failed to update session status in DB", slog.Any("error", err))
				}
			}
		}
	}
}

// CloseAll closes all sessions (for graceful shutdown)
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("closing all claude sessions",
		slog.Int("count", len(m.sessions)),
	)

	for id, session := range m.sessions {
		if err := shutdownSession(session, m.logger); err != nil {
			m.logger.Error("failed to shutdown session",
				slog.String("session_id", id),
				slog.Any("error", err),
			)
		}
	}

	m.sessions = make(map[string]*ClaudeSession)
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

	return sessions
}
