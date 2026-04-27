package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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
