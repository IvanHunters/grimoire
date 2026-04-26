package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// UploadResponse represents the upload response
type UploadResponse struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

// Upload handles POST /api/upload
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form with max memory 10MB
	if err := r.ParseMultipartForm(h.cfg.MaxUploadSize); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file from request", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file size
	if header.Size > h.cfg.MaxUploadSize {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	// Validate MIME type (only images)
	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "Only image files are allowed", http.StatusBadRequest)
		return
	}

	// Generate unique filename
	ext := filepath.Ext(header.Filename)
	filename := uuid.New().String() + ext

	// Create directory structure: YYYY/MM/
	now := time.Now()
	yearMonth := now.Format("2006/01")
	uploadDir := filepath.Join(h.cfg.UploadsDir, yearMonth)

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		h.logger.Error("failed to create upload directory", "error", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Save file
	destPath := filepath.Join(uploadDir, filename)
	dest, err := os.Create(destPath)
	if err != nil {
		h.logger.Error("failed to create destination file", "error", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		h.logger.Error("failed to save file", "error", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Return URL
	url := fmt.Sprintf("/uploads/%s/%s", yearMonth, filename)

	response := UploadResponse{
		URL:      url,
		Filename: filename,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
