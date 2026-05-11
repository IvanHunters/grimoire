package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// SearchByTags handles GET /api/search/tags
func (h *Handler) SearchByTags(w http.ResponseWriter, r *http.Request) {
	tagsParam := r.URL.Query().Get("tags")
	if tagsParam == "" {
		http.Error(w, "Query parameter 'tags' is required", http.StatusBadRequest)
		return
	}

	limitParam := r.URL.Query().Get("limit")
	limit := 50 // default
	if limitParam != "" {
		if l, err := strconv.Atoi(limitParam); err == nil && l > 0 {
			limit = l
		}
	}

	// Parse tags
	tags := []string{}
	for _, tag := range strings.Split(tagsParam, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}

	if len(tags) == 0 {
		http.Error(w, "No valid tags provided", http.StatusBadRequest)
		return
	}

	store := storage.NewMongoStorage(h.db)
	notes := store.SearchByTags(tags, limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

// GetAllTags handles GET /api/tags
func (h *Handler) GetAllTags(w http.ResponseWriter, r *http.Request) {
	store := storage.NewMongoStorage(h.db)
	tagCounts := store.GetAllTags()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tagCounts)
}
