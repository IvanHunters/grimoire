package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// ListSessions returns list of active Claude sessions from database
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	sessionStorage := storage.NewSessionStorage(h.db)
	sessions, err := sessionStorage.ListActiveSessions(ctx)
	if err != nil {
		h.logger.Error("failed to list sessions", "error", err)
		http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sessions); err != nil {
		h.logger.Error("failed to encode sessions", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// DeleteSession kills a Claude session and cleans up resources
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Close session in manager (stops subprocess)
	manager := claude.GetSessionManager(h.logger, nil, "", "")
	if err := manager.Close(sessionID); err != nil {
		h.logger.Warn("failed to close session in manager", "session_id", sessionID, "error", err)
		// Continue anyway to update DB status
	}

	// Update session status in database
	sessionStorage := storage.NewSessionStorage(h.db)
	if err := sessionStorage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
		h.logger.Error("failed to update session status", "session_id", sessionID, "error", err)
		http.Error(w, "Failed to terminate session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
