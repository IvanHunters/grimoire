package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// ListSessions returns list of active Claude sessions from memory (not DB)
// This ensures only live sessions are shown, preventing issues after backend restart
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	// Get live sessions from SessionManager instead of DB
	sessions := h.sessionManager.ListActiveSessions()

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

	// Close session in manager (stops subprocess) - runs in background
	if h.sessionManager != nil {
		go func() {
			if err := h.sessionManager.Close(sessionID); err != nil {
				h.logger.Warn("failed to close session in manager", "session_id", sessionID, "error", err)
			}
		}()
	}

	// Update session status in database with fresh context
	// (not tied to request context which might be cancelled)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionStorage := storage.NewSessionStorage(h.db)
	if err := sessionStorage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
		h.logger.Error("failed to update session status", "session_id", sessionID, "error", err)
		http.Error(w, "Failed to terminate session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
