package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const maxImportSize = 512 << 20 // 512 MB

// ExportDB streams a ZIP backup: notes.json + folders.json + tasks.json + uploads/
func (h *Handler) ExportDB(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	store := storage.NewMongoStorage(h.db)

	notes, err := store.ListNotes(ctx, "")
	if err != nil {
		h.logger.Error("export: failed to list notes", "error", err)
		http.Error(w, "failed to list notes", http.StatusInternalServerError)
		return
	}

	folders, err := store.ListFolders(ctx)
	if err != nil {
		h.logger.Error("export: failed to list folders", "error", err)
		http.Error(w, "failed to list folders", http.StatusInternalServerError)
		return
	}

	tasks, err := listAllTasks(ctx, h)
	if err != nil {
		h.logger.Error("export: failed to list tasks", "error", err)
		http.Error(w, "failed to list tasks", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("markdown-backup-%s.zip", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)

	go func() {
		zw := zip.NewWriter(pw)
		defer func() {
			zw.Close()
			pw.Close()
		}()

		if err := addJSONToZip(zw, "notes.json", notes); err != nil {
			errCh <- err
			return
		}
		if err := addJSONToZip(zw, "folders.json", folders); err != nil {
			errCh <- err
			return
		}
		if err := addJSONToZip(zw, "tasks.json", tasks); err != nil {
			errCh <- err
			return
		}

		uploadsDir := h.cfg.UploadsDir
		if err := addDirToZip(zw, uploadsDir, "uploads"); err != nil {
			// Non-fatal: uploads dir might be empty
			h.logger.Warn("export: partial uploads error", "error", err)
		}
		errCh <- nil
	}()

	if _, err := io.Copy(w, pr); err != nil {
		h.logger.Error("export: stream error", "error", err)
	}
	<-errCh
}

// ImportDB accepts a ZIP backup and upserts notes, folders, tasks, and upload files.
func (h *Handler) ImportDB(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "request too large or invalid form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read upload", http.StatusInternalServerError)
		return
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		http.Error(w, "invalid zip file", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result := struct {
		Notes   int `json:"notes"`
		Folders int `json:"folders"`
		Tasks   int `json:"tasks"`
		Files   int `json:"files"`
	}{}

	for _, f := range zr.File {
		switch f.Name {
		case "notes.json":
			n, err := importNotes(ctx, h, zr, f)
			if err != nil {
				h.logger.Error("import: notes failed", "error", err)
				http.Error(w, "failed to import notes: "+err.Error(), http.StatusInternalServerError)
				return
			}
			result.Notes = n

		case "folders.json":
			n, err := importFolders(ctx, h, zr, f)
			if err != nil {
				h.logger.Error("import: folders failed", "error", err)
				http.Error(w, "failed to import folders: "+err.Error(), http.StatusInternalServerError)
				return
			}
			result.Folders = n

		case "tasks.json":
			n, err := importTasks(ctx, h, zr, f)
			if err != nil {
				h.logger.Error("import: tasks failed", "error", err)
				http.Error(w, "failed to import tasks: "+err.Error(), http.StatusInternalServerError)
				return
			}
			result.Tasks = n
		}

		if strings.HasPrefix(f.Name, "uploads/") && !f.FileInfo().IsDir() {
			if err := extractUpload(f, h.cfg.UploadsDir); err != nil {
				h.logger.Warn("import: failed to extract upload", "file", f.Name, "error", err)
			} else {
				result.Files++
			}
		}
	}

	// Rebuild tags index after bulk import
	store := storage.NewMongoStorage(h.db)
	if err := store.BuildTagsIndex(ctx); err != nil {
		h.logger.Warn("import: failed to rebuild tags index", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ---- helpers ----

func addJSONToZip(zw *zip.Writer, name string, v interface{}) error {
	fw, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(fw)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func addDirToZip(zw *zip.Writer, dir, prefix string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fw, err := zw.Create(prefix + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func importNotes(ctx context.Context, h *Handler, _ *zip.Reader, f *zip.File) (int, error) {
	data, err := readZipFile(f)
	if err != nil {
		return 0, err
	}
	var notes []*models.Note
	if err := json.Unmarshal(data, &notes); err != nil {
		return 0, fmt.Errorf("parse notes.json: %w", err)
	}
	col := h.db.Collection("notes")
	for _, note := range notes {
		opts := options.Replace().SetUpsert(true)
		_, err := col.ReplaceOne(ctx, bson.M{"id": note.ID}, note, opts)
		if err != nil {
			return 0, fmt.Errorf("upsert note %s: %w", note.ID, err)
		}
	}
	return len(notes), nil
}

func importFolders(ctx context.Context, h *Handler, _ *zip.Reader, f *zip.File) (int, error) {
	data, err := readZipFile(f)
	if err != nil {
		return 0, err
	}
	var folders []*models.Folder
	if err := json.Unmarshal(data, &folders); err != nil {
		return 0, fmt.Errorf("parse folders.json: %w", err)
	}
	col := h.db.Collection("folders")
	for _, folder := range folders {
		opts := options.Replace().SetUpsert(true)
		_, err := col.ReplaceOne(ctx, bson.M{"path": folder.Path}, folder, opts)
		if err != nil {
			return 0, fmt.Errorf("upsert folder %s: %w", folder.Path, err)
		}
	}
	return len(folders), nil
}

func importTasks(ctx context.Context, h *Handler, _ *zip.Reader, f *zip.File) (int, error) {
	data, err := readZipFile(f)
	if err != nil {
		return 0, err
	}
	var tasks []*models.Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return 0, fmt.Errorf("parse tasks.json: %w", err)
	}
	col := h.db.Collection("tasks")
	for _, task := range tasks {
		opts := options.Replace().SetUpsert(true)
		_, err := col.ReplaceOne(ctx, bson.M{"id": task.ID}, task, opts)
		if err != nil {
			return 0, fmt.Errorf("upsert task %s: %w", task.ID, err)
		}
	}
	return len(tasks), nil
}

func extractUpload(f *zip.File, uploadsDir string) error {
	// Strip "uploads/" prefix
	rel := strings.TrimPrefix(f.Name, "uploads/")
	if rel == "" {
		return nil
	}
	dest := filepath.Join(uploadsDir, filepath.FromSlash(rel))
	// Path traversal guard
	if !strings.HasPrefix(dest, uploadsDir) {
		return fmt.Errorf("path traversal detected: %s", f.Name)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func listAllTasks(ctx context.Context, h *Handler) ([]*models.Task, error) {
	col := h.db.Collection("tasks")
	cursor, err := col.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var tasks []*models.Task
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}
