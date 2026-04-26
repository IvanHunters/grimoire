package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// Search handles GET /api/search
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	notes, err := store.SearchNotes(ctx, query)
	if err != nil {
		h.logger.Error("failed to search notes", "query", query, "error", err)
		http.Error(w, "Failed to search notes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}
