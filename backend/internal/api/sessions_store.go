package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
)

// storedSessionResponse is one row of the archive/trash browser.
type storedSessionResponse struct {
	SessionID string    `json:"sessionId"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	Name      string    `json:"name"`
	Cwd       string    `json:"cwd"`
	SizeBytes int64     `json:"sizeBytes"`
	MovedAt   time.Time `json:"movedAt"`
}

// stateForKind maps a shelf to the label the UI and MCP tools show.
func stateForKind(kind discovery.StoreKind) string {
	if kind == discovery.StoreTrash {
		return discovery.SessionStateDeleted
	}
	return discovery.SessionStateArchived
}

// dropTranscript relocates a session's transcript bundle onto one of the
// store's shelves. Shared by archive and delete so neither path can
// drift into destroying history.
func (h *Handler) dropTranscript(sessionID string, kind discovery.StoreKind) (string, error) {
	path, err := discovery.SessionPath(sessionID)
	if err != nil {
		return "", err
	}
	return discovery.MoveTranscriptToStore(path, sessionID, kind, time.Now().UnixNano())
}

// ArchiveSession puts a session away without deleting it: the worker is
// stopped and the transcript moves to the archive shelf, so the sidebar
// stops listing it while search still finds it and restore brings it
// back.
//
// POST /api/sessions/{id}/archive
func (h *Handler) ArchiveSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// A live worker keeps writing to the transcript it opened, so stop
	// it before the file moves out from under it.
	h.stopWorker(sessionID)

	dir, err := h.dropTranscript(sessionID, discovery.StoreArchive)
	if err != nil {
		h.logger.Warn("archive session failed", "session_id", sessionID, "error", err)
		http.Error(w, "No transcript found for this session", http.StatusNotFound)
		return
	}
	h.logger.Info("session archived", "session_id", sessionID, "dir", dir)

	writeJSON(w, map[string]any{
		"sessionId": sessionID,
		"state":     discovery.SessionStateArchived,
		"dir":       dir,
	})
}

// RestoreSession moves an archived or deleted session back into its
// project dir and reports where it landed. The path matters to the
// caller: `claude --resume` only sees a transcript that sits in the
// project dir for its cwd, so a restore is the required first half of
// "open this old session again".
//
// POST /api/sessions/{id}/restore
func (h *Handler) RestoreSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Read the bundle first — after the restore the manifest is gone and
	// with it the cwd the caller needs to start the session in.
	entry, err := discovery.FindStoreEntry(sessionID)
	if err != nil {
		http.Error(w, "Session is not archived or deleted", http.StatusNotFound)
		return
	}

	path, err := discovery.RestoreFromStore(sessionID)
	if err != nil {
		h.logger.Warn("restore session failed", "session_id", sessionID, "error", err)
		http.Error(w, "Restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.logger.Info("session restored", "session_id", sessionID, "path", path)

	writeJSON(w, map[string]any{
		"sessionId": sessionID,
		"state":     discovery.SessionStateActive,
		"path":      path,
		"cwd":       entry.Cwd,
		"from":      stateForKind(entry.Kind),
	})
}

// ListStoredSessions returns everything on the shelves, newest first.
// Pass ?kind=archive or ?kind=trash to narrow.
//
// GET /api/sessions/store
func (h *Handler) ListStoredSessions(w http.ResponseWriter, r *http.Request) {
	kind := discovery.StoreKind(r.URL.Query().Get("kind"))
	entries, err := discovery.ListStore(kind)
	if err != nil {
		http.Error(w, "Bad kind: "+err.Error(), http.StatusBadRequest)
		return
	}

	rows := make([]storedSessionResponse, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, storedSessionResponse{
			SessionID: e.SessionID,
			Kind:      string(e.Kind),
			State:     stateForKind(e.Kind),
			Name:      e.Name,
			Cwd:       e.Cwd,
			SizeBytes: e.SizeBytes,
			MovedAt:   e.MovedAt,
		})
	}
	writeJSON(w, map[string]any{"entries": rows})
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
