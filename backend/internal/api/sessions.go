package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// SessionStats returns aggregate stats for all sessions.
func (h *Handler) SessionStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	ss := storage.NewSessionStorage(h.db)
	stats, err := ss.GetSessionsStats(ctx)
	if err != nil {
		h.logger.Error("failed to get session stats", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ListAllSessions returns metadata for all sessions (no message bodies).
func (h *Handler) ListAllSessions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	ss := storage.NewSessionStorage(h.db)
	sessions, err := ss.ListSessionsMeta(ctx, 200)
	if err != nil {
		h.logger.Error("failed to list sessions", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []*models.SessionMeta{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// RotateSessions clears message history for sessions idle longer than the given threshold.
func (h *Handler) RotateSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OlderThanDays int `json:"older_than_days"`
	}
	req.OlderThanDays = 2 // default
	json.NewDecoder(r.Body).Decode(&req)
	if req.OlderThanDays <= 0 {
		req.OlderThanDays = 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ss := storage.NewSessionStorage(h.db)
	n, err := ss.RotateOldSessions(ctx, time.Duration(req.OlderThanDays)*24*time.Hour)
	if err != nil {
		h.logger.Error("failed to rotate sessions", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"rotated": n})
}

// ClearSessionHistory removes all messages from a single session.
func (h *Handler) ClearSessionHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ss := storage.NewSessionStorage(h.db)
	if err := ss.ClearSessionMessages(ctx, sessionID); err != nil {
		h.logger.Error("failed to clear session history", "session_id", sessionID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RenameSession updates the display name of a session
func (h *Handler) RenameSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ss := storage.NewSessionStorage(h.db)
	if err := ss.UpdateSessionName(ctx, sessionID, body.Name); err != nil {
		h.logger.Error("failed to rename session", "session_id", sessionID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Also update in-memory session name if it's live
	h.sessionManager.RenameSession(sessionID, body.Name)

	w.WriteHeader(http.StatusNoContent)
}

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
