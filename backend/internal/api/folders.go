package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// ListFolders handles GET /api/folders
func (h *Handler) ListFolders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	folders, err := store.ListFolders(ctx)
	if err != nil {
		h.logger.Error("failed to list folders", "error", err)
		http.Error(w, "Failed to list folders", http.StatusInternalServerError)
		return
	}

	// Build folder tree
	tree := storage.BuildFolderTree(folders)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

// CreateFolder handles POST /api/folders
func (h *Handler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	var req models.CreateFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.validator.Struct(req); err != nil {
		http.Error(w, "Validation error", http.StatusBadRequest)
		return
	}

	folder := &models.Folder{
		Path: req.Path,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.CreateFolder(ctx, folder); err != nil {
		h.logger.Error("failed to create folder", "error", err)
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(folder)
}

// DeleteFolder handles DELETE /api/folders
func (h *Handler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Folder path is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.DeleteFolder(ctx, path); err != nil {
		h.logger.Error("failed to delete folder", "path", path, "error", err)
		http.Error(w, "Failed to delete folder", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// MoveFolder handles PUT /api/folders/move
func (h *Handler) MoveFolder(w http.ResponseWriter, r *http.Request) {
	var req models.MoveFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.validator.Struct(req); err != nil {
		http.Error(w, "Validation error", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)

	// Move folder and update all affected notes and subfolders
	if err := store.MoveFolder(ctx, req.From, req.To); err != nil {
		h.logger.Error("failed to move folder", "from", req.From, "to", req.To, "error", err)
		http.Error(w, fmt.Sprintf("Failed to move folder: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
