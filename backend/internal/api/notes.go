package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/skills"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// ListNotes handles GET /api/notes
func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	notes, err := store.ListNotes(ctx, folder)
	if err != nil {
		h.logger.Error("failed to list notes", "error", err)
		http.Error(w, "Failed to list notes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

// GetNote handles GET /api/notes/{id}
func (h *Handler) GetNote(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Note ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	note, err := store.GetNote(ctx, id)
	if err != nil {
		h.logger.Error("failed to get note", "id", id, "error", err)
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(note)
}

// CreateNote handles POST /api/notes
func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	var req models.CreateNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.validator.Struct(req); err != nil {
		http.Error(w, fmt.Sprintf("Validation error: %v", err), http.StatusBadRequest)
		return
	}

	if skills.IsSkillPath(req.Folder) || skills.IsSkillPath(req.Folder+"/x") {
		http.Error(w, "Use POST /api/skills to create entries under Skills/", http.StatusBadRequest)
		return
	}

	// Generate note ID and path
	noteID := uuid.New().String()
	slug := slugify(req.Title)
	// Fallback to ID if slug is empty (e.g., non-Latin title)
	if slug == "" {
		slug = noteID
	}
	fileName := slug + ".md"

	var path string
	if req.Folder != "" {
		path = req.Folder + "/" + fileName
	} else {
		path = fileName
	}

	note := &models.Note{
		ID:          noteID,
		Path:        path,
		Title:       req.Title,
		Folder:      req.Folder,
		Content:     req.Content,
		Type:        req.Type,
		ProjectPath: req.ProjectPath,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)

	// Auto-create folder if specified
	if req.Folder != "" {
		// Create all parent folders
		parts := strings.Split(req.Folder, "/")
		currentPath := ""
		for _, part := range parts {
			if currentPath == "" {
				currentPath = part
			} else {
				currentPath = currentPath + "/" + part
			}

			folder := &models.Folder{
				Path: currentPath,
			}

			// Ignore error if folder already exists
			_ = store.CreateFolder(ctx, folder)
		}
	}

	if err := store.CreateNote(ctx, note); err != nil {
		h.logger.Error("failed to create note", "error", err)
		http.Error(w, "Failed to create note", http.StatusInternalServerError)
		return
	}

	// Publish event for real-time sync
	eventBus := events.GetEventBus()
	eventBus.Publish(events.Event{
		Type: events.EventNoteCreated,
		Note: note,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(note)
}

// UpdateNote handles PUT /api/notes/{id}
func (h *Handler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Note ID is required", http.StatusBadRequest)
		return
	}

	var req models.UpdateNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)

	// Get existing note
	note, err := store.GetNote(ctx, id)
	if err != nil {
		h.logger.Error("failed to get note", "id", id, "error", err)
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	// Update fields
	if req.Title != "" {
		note.Title = req.Title
	}
	if req.Content != "" {
		note.Content = req.Content
	}
	if req.Type != "" {
		note.Type = req.Type
	}
	if req.ProjectPath != nil {
		note.ProjectPath = *req.ProjectPath
	}
	if req.Tags != nil {
		note.Tags = req.Tags
	}

	// Update folder and recalculate path if folder changed
	if req.Folder != nil {
		oldFolder := note.Folder
		newFolder := *req.Folder

		if oldFolder != newFolder {
			note.Folder = newFolder

			// Recalculate path based on new folder
			slug := slugify(note.Title)
			// Fallback to ID if slug is empty (e.g., non-Latin title)
			if slug == "" {
				slug = note.ID
			}
			fileName := slug + ".md"

			if newFolder != "" {
				note.Path = newFolder + "/" + fileName
			} else {
				note.Path = fileName
			}
		}
	}

	if err := store.UpdateNote(ctx, note); err != nil {
		h.logger.Error("failed to update note", "id", id, "error", err)
		http.Error(w, "Failed to update note", http.StatusInternalServerError)
		return
	}

	if h.skills != nil && skills.IsSkillPath(note.Path) {
		if err := h.skills.WriteNote(note); err != nil {
			h.logger.Error("failed to write skill file", "path", note.Path, "error", err)
			http.Error(w, "Failed to write skill file: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Publish event for real-time sync
	eventBus := events.GetEventBus()
	eventBus.Publish(events.Event{
		Type: events.EventNoteUpdated,
		Note: note,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(note)
}

// DeleteNote handles DELETE /api/notes/{id}
func (h *Handler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Note ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)

	existing, err := store.GetNote(ctx, id)
	if err == nil && skills.IsSkillPath(existing.Path) && existing.Type == models.NoteTypeSkill {
		http.Error(w, "Use DELETE /api/skills/:name to delete a skill", http.StatusBadRequest)
		return
	}

	if err := store.DeleteNote(ctx, id); err != nil {
		h.logger.Error("failed to delete note", "id", id, "error", err)
		http.Error(w, "Failed to delete note", http.StatusInternalServerError)
		return
	}

	// Publish event for real-time sync
	eventBus := events.GetEventBus()
	eventBus.Publish(events.Event{
		Type:   events.EventNoteDeleted,
		NoteID: id,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ProjectSuggestion represents a suggested project path
type ProjectSuggestion struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// ProjectSuggestions handles GET /api/notes/project-suggestions
func (h *Handler) ProjectSuggestions(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	suggestions := findProjectsByTitle(title)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}

// findProjectsByTitle searches for projects in ~/git/github.com/$USER/
func findProjectsByTitle(title string) []ProjectSuggestion {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return []ProjectSuggestion{}
	}

	// Get current user from home directory
	parts := strings.Split(homeDir, "/")
	if len(parts) < 2 {
		return []ProjectSuggestion{}
	}
	user := parts[len(parts)-1]

	basePath := filepath.Join(homeDir, "git", "github.com", user)

	// Check if directory exists
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return []ProjectSuggestion{}
	}

	normalizedTitle := normalizeTitle(title)
	var suggestions []ProjectSuggestion

	// Read directories
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return []ProjectSuggestion{}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		normalizedDir := normalizeTitle(dirName)

		// Exact match
		if normalizedDir == normalizedTitle {
			suggestions = append(suggestions, ProjectSuggestion{
				Title: dirName,
				Path:  filepath.Join(basePath, dirName),
			})
		} else if strings.Contains(normalizedDir, normalizedTitle) {
			// Fuzzy match
			suggestions = append(suggestions, ProjectSuggestion{
				Title: dirName,
				Path:  filepath.Join(basePath, dirName),
			})
		}
	}

	return suggestions
}

// normalizeTitle converts title to lowercase slug
func normalizeTitle(title string) string {
	// Convert to lowercase
	s := strings.ToLower(title)
	// Replace spaces and special characters with hyphens
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	// Remove consecutive hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	// Trim hyphens from edges
	s = strings.Trim(s, "-")
	return s
}

// slugify converts a string to a URL-friendly slug
func slugify(s string) string {
	return normalizeTitle(s)
}
