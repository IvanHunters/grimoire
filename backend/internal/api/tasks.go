package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/scheduler"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// ── Projects ──────────────────────────────────────────────────────────────────

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	projects, err := store.ListProjects(ctx)
	if err != nil {
		h.logger.Error("list projects", "error", err)
		http.Error(w, "failed to list projects", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := h.validator.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	color := req.Color
	if color == "" {
		color = "#6366f1"
	}

	now := time.Now()
	p := &models.Project{
		ID:               uuid.New().String(),
		Title:            req.Title,
		Description:      req.Description,
		Color:            color,
		LinkedFolderPath: req.LinkedFolderPath,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.CreateProject(ctx, p); err != nil {
		h.logger.Error("create project", "error", err)
		http.Error(w, "failed to create project", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	p, err := store.GetProject(ctx, id)
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	var req models.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Title != "" {
		p.Title = req.Title
	}
	if req.Description != "" {
		p.Description = req.Description
	}
	if req.Color != "" {
		p.Color = req.Color
	}
	p.LinkedFolderPath = req.LinkedFolderPath // allow clearing

	if err := store.UpdateProject(ctx, p); err != nil {
		h.logger.Error("update project", "error", err)
		http.Error(w, "failed to update project", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.DeleteProject(ctx, id); err != nil {
		h.logger.Error("delete project", "error", err)
		http.Error(w, "failed to delete project", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (h *Handler) SearchTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}
	projectID := r.URL.Query().Get("projectId")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	tasks, err := store.SearchTasks(ctx, q, projectID)
	if err != nil {
		h.logger.Error("search tasks", "error", err)
		http.Error(w, "failed to search tasks", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	status := r.URL.Query().Get("status")
	folderPath := r.URL.Query().Get("folderPath")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	tasks, err := store.ListTasks(ctx, projectID, status, folderPath)
	if err != nil {
		h.logger.Error("list tasks", "error", err)
		http.Error(w, "failed to list tasks", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	task, err := store.GetTask(ctx, id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := h.validator.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	status := req.Status
	if status == "" {
		status = models.TaskStatusBacklog
	}
	priority := req.Priority
	if priority == "" {
		priority = models.TaskPriorityMedium
	}

	now := time.Now()
	t := &models.Task{
		ID:                uuid.New().String(),
		Type:              req.Type,
		ParentID:          req.ParentID,
		Title:             req.Title,
		Description:       req.Description,
		ProjectID:         req.ProjectID,
		Status:            status,
		Priority:          priority,
		LinkedNoteIDs:     orEmptyStrings(req.LinkedNoteIDs),
		LinkedFolderPaths: orEmptyStrings(req.LinkedFolderPaths),
		LinkedTaskIDs:     orEmptyStrings(req.LinkedTaskIDs),
		Tags:              orEmptyStrings(req.Tags),
		Comments:          []models.TaskComment{},
		DueDate:           req.DueDate,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.CreateTask(ctx, t); err != nil {
		h.logger.Error("create task", "error", err)
		http.Error(w, "failed to create task", http.StatusInternalServerError)
		return
	}
	events.GetEventBus().Publish(events.Event{Type: events.EventTaskCreated, Task: t})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	t, err := store.GetTask(ctx, id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	var req models.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Type != "" {
		t.Type = req.Type
	}
	if req.ClearParentID {
		t.ParentID = ""
	} else if req.ParentID != nil {
		t.ParentID = *req.ParentID
	}
	if req.Title != "" {
		t.Title = req.Title
	}
	if req.Description != "" {
		t.Description = req.Description
	}
	if req.ProjectID != nil {
		t.ProjectID = *req.ProjectID
	}
	if req.Status != "" {
		t.Status = req.Status
	}
	if req.Priority != "" {
		t.Priority = req.Priority
	}
	if req.LinkedNoteIDs != nil {
		t.LinkedNoteIDs = *req.LinkedNoteIDs
	}
	if req.LinkedFolderPaths != nil {
		t.LinkedFolderPaths = *req.LinkedFolderPaths
	}
	if req.LinkedTaskIDs != nil {
		t.LinkedTaskIDs = *req.LinkedTaskIDs
	}
	if req.Tags != nil {
		t.Tags = *req.Tags
	}
	if req.ClearDueDate {
		t.DueDate = nil
	} else if req.DueDate != nil {
		t.DueDate = req.DueDate
	}
	if req.ClearRecurring {
		t.Recurring = nil
	} else if req.SetCronExpr != "" {
		if err := scheduler.ValidateCronExpr(req.SetCronExpr); err != nil {
			http.Error(w, "invalid cron expression: "+err.Error(), http.StatusBadRequest)
			return
		}
		if t.Recurring == nil {
			t.Recurring = &models.RecurringConfig{}
		}
		t.Recurring.CronExpr = req.SetCronExpr
		t.Recurring.Enabled = true
		next, _ := scheduler.ComputeNextRun(t.Recurring, time.Now())
		t.Recurring.NextRunAt = next
	} else if req.Recurring != nil {
		if t.Recurring == nil {
			t.Recurring = &models.RecurringConfig{}
		}
		t.Recurring.Enabled = req.Recurring.Enabled
		if req.Recurring.CronExpr != "" {
			if err := scheduler.ValidateCronExpr(req.Recurring.CronExpr); err != nil {
				http.Error(w, "invalid cron expression: "+err.Error(), http.StatusBadRequest)
				return
			}
			t.Recurring.CronExpr = req.Recurring.CronExpr
			t.Recurring.IntervalMinutes = 0
		} else {
			t.Recurring.IntervalMinutes = req.Recurring.IntervalMinutes
			t.Recurring.CronExpr = ""
		}
		// Recompute nextRunAt when enabling
		if t.Recurring.Enabled && t.Recurring.NextRunAt == nil {
			next, _ := scheduler.ComputeNextRun(t.Recurring, time.Now())
			t.Recurring.NextRunAt = next
		}
	}

	if err := store.UpdateTask(ctx, t); err != nil {
		h.logger.Error("update task", "error", err)
		http.Error(w, "failed to update task", http.StatusInternalServerError)
		return
	}
	events.GetEventBus().Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.DeleteTask(ctx, id); err != nil {
		h.logger.Error("delete task", "error", err)
		http.Error(w, "failed to delete task", http.StatusInternalServerError)
		return
	}
	events.GetEventBus().Publish(events.Event{Type: events.EventTaskDeleted, TaskID: id})
	w.WriteHeader(http.StatusNoContent)
}

// ── Comments ──────────────────────────────────────────────────────────────────

func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := h.validator.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	t, err := store.GetTask(ctx, id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	now := time.Now()
	comment := models.TaskComment{
		ID:        uuid.New().String(),
		Content:   req.Content,
		CreatedAt: now,
		UpdatedAt: now,
	}
	t.Comments = append(t.Comments, comment)

	if err := store.UpdateTask(ctx, t); err != nil {
		http.Error(w, "failed to add comment", http.StatusInternalServerError)
		return
	}
	events.GetEventBus().Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(comment)
}

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentId")

	var req models.UpdateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	t, err := store.GetTask(ctx, taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	var updated *models.TaskComment
	for i := range t.Comments {
		if t.Comments[i].ID == commentID {
			t.Comments[i].Content = req.Content
			t.Comments[i].UpdatedAt = time.Now()
			updated = &t.Comments[i]
			break
		}
	}
	if updated == nil {
		http.Error(w, "comment not found", http.StatusNotFound)
		return
	}

	if err := store.UpdateTask(ctx, t); err != nil {
		http.Error(w, "failed to update comment", http.StatusInternalServerError)
		return
	}
	events.GetEventBus().Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentId")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	t, err := store.GetTask(ctx, taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	filtered := t.Comments[:0]
	for _, c := range t.Comments {
		if c.ID != commentID {
			filtered = append(filtered, c)
		}
	}
	t.Comments = filtered

	if err := store.UpdateTask(ctx, t); err != nil {
		http.Error(w, "failed to delete comment", http.StatusInternalServerError)
		return
	}
	events.GetEventBus().Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
	w.WriteHeader(http.StatusNoContent)
}

// ── Kanban Columns ────────────────────────────────────────────────────────────

var defaultKanbanColumns = []models.KanbanColumn{
	{ID: "backlog",     Label: "Backlog",     TextColor: "#94a3b8", DotColor: "#64748b", Order: 0},
	{ID: "todo",        Label: "To Do",       TextColor: "#60a5fa", DotColor: "#3b82f6", Order: 1},
	{ID: "in_progress", Label: "In Progress", TextColor: "#fbbf24", DotColor: "#f59e0b", Order: 2},
	{ID: "done",        Label: "Done",        TextColor: "#4ade80", DotColor: "#22c55e", Order: 3},
}

func (h *Handler) GetTaskColumns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	cols, err := store.GetKanbanColumns(ctx)
	if err != nil {
		h.logger.Error("get kanban columns", "error", err)
		http.Error(w, "failed to get columns", http.StatusInternalServerError)
		return
	}
	if cols == nil {
		cols = defaultKanbanColumns
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cols)
}

func (h *Handler) SetTaskColumns(w http.ResponseWriter, r *http.Request) {
	var cols []models.KanbanColumn
	if err := json.NewDecoder(r.Body).Decode(&cols); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if len(cols) == 0 {
		http.Error(w, "at least one column required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.SetKanbanColumns(ctx, cols); err != nil {
		h.logger.Error("set kanban columns", "error", err)
		http.Error(w, "failed to save columns", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cols)
}

func (h *Handler) GetTaskProjectFolders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	folders, err := store.GetTaskProjectFolders(ctx)
	if err != nil {
		h.logger.Error("get task project folders", "error", err)
		http.Error(w, "failed to get folders", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(folders)
}

func (h *Handler) SetTaskProjectFolders(w http.ResponseWriter, r *http.Request) {
	var folders []string
	if err := json.NewDecoder(r.Body).Decode(&folders); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if folders == nil {
		folders = []string{}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	store := storage.NewMongoStorage(h.db)
	if err := store.SetTaskProjectFolders(ctx, folders); err != nil {
		h.logger.Error("set task project folders", "error", err)
		http.Error(w, "failed to save folders", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(folders)
}

// RunTaskNow triggers a scheduled task immediately.
func (h *Handler) RunTaskNow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.taskRunner == nil {
		http.Error(w, "scheduler not available", http.StatusServiceUnavailable)
		return
	}
	if err := h.taskRunner.RunNow(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// CancelTaskNow kills a currently running scheduled task.
func (h *Handler) CancelTaskNow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.taskRunner == nil {
		http.Error(w, "scheduler not available", http.StatusServiceUnavailable)
		return
	}
	if err := h.taskRunner.CancelNow(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
