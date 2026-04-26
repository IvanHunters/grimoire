package claude

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SessionManager manages all Claude sessions
type SessionManager struct {
	sessions map[string]*ClaudeSession
	mu       sync.RWMutex
	logger   *slog.Logger
}

// Global session manager instance
var globalManager *SessionManager
var managerOnce sync.Once

// GetSessionManager returns the global session manager instance
func GetSessionManager(logger *slog.Logger) *SessionManager {
	managerOnce.Do(func() {
		globalManager = &SessionManager{
			sessions: make(map[string]*ClaudeSession),
			logger:   logger,
		}
	})
	return globalManager
}

// GetOrCreate gets an existing session or creates a new one
func (m *SessionManager) GetOrCreate(sessionID string, dangerousMode bool, workingDir string) (*ClaudeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if session exists
	if session, exists := m.sessions[sessionID]; exists {
		// Update dangerous mode if it changed
		if session.DangerousMode != dangerousMode {
			// Need to restart session with new mode
			if err := shutdownSession(session, m.logger); err != nil {
				m.logger.Error("failed to shutdown session for restart", slog.Any("error", err))
			}
			delete(m.sessions, sessionID)
		} else {
			// Session exists and mode is the same
			session.UpdateActivity()
			return session, nil
		}
	}

	// Create new session
	session, err := startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start claude subprocess: %w", err)
	}

	m.sessions[sessionID] = session
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

	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := shutdownSession(session, m.logger); err != nil {
		return fmt.Errorf("failed to shutdown session: %w", err)
	}

	delete(m.sessions, sessionID)
	return nil
}

// CloseInactive closes sessions that have been inactive for the given timeout
func (m *SessionManager) CloseInactive(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

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
		}
	}()
}
