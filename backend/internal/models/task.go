package models

import "time"

type TaskStatus string
type TaskPriority string

// KanbanColumn is a user-configurable kanban column stored in MongoDB.
type KanbanColumn struct {
	ID        string `json:"id" bson:"id"`
	Label     string `json:"label" bson:"label"`
	TextColor string `json:"textColor" bson:"text_color"`
	DotColor  string `json:"dotColor" bson:"dot_color"`
	Order     int    `json:"order" bson:"order"`
}

const (
	TaskStatusBacklog    TaskStatus = "backlog"
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusDone       TaskStatus = "done"
)

const (
	TaskPriorityLow    TaskPriority = "low"
	TaskPriorityMedium TaskPriority = "medium"
	TaskPriorityHigh   TaskPriority = "high"
	TaskPriorityUrgent TaskPriority = "urgent"
)

type Project struct {
	ID               string    `json:"id" bson:"id"`
	Title            string    `json:"title" bson:"title"`
	Description      string    `json:"description,omitempty" bson:"description,omitempty"`
	Color            string    `json:"color,omitempty" bson:"color,omitempty"`
	LinkedFolderPath string    `json:"linkedFolderPath,omitempty" bson:"linked_folder_path,omitempty"`
	CreatedAt        time.Time `json:"createdAt" bson:"created_at"`
	UpdatedAt        time.Time `json:"updatedAt" bson:"updated_at"`
}

type TaskComment struct {
	ID        string    `json:"id" bson:"id"`
	Content   string    `json:"content" bson:"content"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
}

type RecurringConfig struct {
	Enabled         bool       `json:"enabled" bson:"enabled"`
	IntervalMinutes int        `json:"intervalMinutes" bson:"interval_minutes"` // used when CronExpr is empty
	CronExpr        string     `json:"cronExpr,omitempty" bson:"cron_expr,omitempty"` // e.g. "30 10 * * *"
	LastRunAt       *time.Time `json:"lastRunAt,omitempty" bson:"last_run_at,omitempty"`
	NextRunAt       *time.Time `json:"nextRunAt,omitempty" bson:"next_run_at,omitempty"`
}

type Task struct {
	ID                string          `json:"id" bson:"id"`
	Type              string          `json:"type,omitempty" bson:"type,omitempty"` // "task" (default) | "story"
	ParentID          string          `json:"parentId,omitempty" bson:"parent_id,omitempty"`
	Title             string          `json:"title" bson:"title"`
	Description       string          `json:"description,omitempty" bson:"description,omitempty"`
	ProjectID         string          `json:"projectId,omitempty" bson:"project_id,omitempty"`
	Status            TaskStatus      `json:"status" bson:"status"`
	Priority          TaskPriority    `json:"priority" bson:"priority"`
	LinkedNoteIDs     []string        `json:"linkedNoteIds,omitempty" bson:"linked_note_ids,omitempty"`
	LinkedFolderPaths []string        `json:"linkedFolderPaths,omitempty" bson:"linked_folder_paths,omitempty"`
	LinkedTaskIDs     []string        `json:"linkedTaskIds,omitempty" bson:"linked_task_ids,omitempty"`
	Tags              []string        `json:"tags,omitempty" bson:"tags,omitempty"`
	Comments          []TaskComment   `json:"comments,omitempty" bson:"comments,omitempty"`
	DueDate           *time.Time      `json:"dueDate,omitempty" bson:"due_date,omitempty"`
	Recurring         *RecurringConfig `json:"recurring,omitempty" bson:"recurring,omitempty"`
	CreatedAt         time.Time       `json:"createdAt" bson:"created_at"`
	UpdatedAt         time.Time       `json:"updatedAt" bson:"updated_at"`
}

type CreateProjectRequest struct {
	Title            string `json:"title" validate:"required"`
	Description      string `json:"description,omitempty"`
	Color            string `json:"color,omitempty"`
	LinkedFolderPath string `json:"linkedFolderPath,omitempty"`
}

type UpdateProjectRequest struct {
	Title            string `json:"title,omitempty"`
	Description      string `json:"description,omitempty"`
	Color            string `json:"color,omitempty"`
	LinkedFolderPath string `json:"linkedFolderPath,omitempty"`
}

type CreateTaskRequest struct {
	Type              string       `json:"type,omitempty"`
	ParentID          string       `json:"parentId,omitempty"`
	Title             string       `json:"title" validate:"required"`
	Description       string       `json:"description,omitempty"`
	ProjectID         string       `json:"projectId,omitempty"`
	Status            TaskStatus   `json:"status,omitempty"`
	Priority          TaskPriority `json:"priority,omitempty"`
	LinkedNoteIDs     []string     `json:"linkedNoteIds,omitempty"`
	LinkedFolderPaths []string     `json:"linkedFolderPaths,omitempty"`
	LinkedTaskIDs     []string     `json:"linkedTaskIds,omitempty"`
	Tags              []string     `json:"tags,omitempty"`
	DueDate           *time.Time   `json:"dueDate,omitempty"`
}

type UpdateTaskRequest struct {
	Type              string           `json:"type,omitempty"`
	ParentID          *string          `json:"parentId,omitempty"`
	ClearParentID     bool             `json:"clearParentId,omitempty"`
	Title             string           `json:"title,omitempty"`
	Description       string           `json:"description,omitempty"`
	ProjectID         *string          `json:"projectId,omitempty"`
	Status            TaskStatus       `json:"status,omitempty"`
	Priority          TaskPriority     `json:"priority,omitempty"`
	LinkedNoteIDs     *[]string        `json:"linkedNoteIds,omitempty"`
	LinkedFolderPaths *[]string        `json:"linkedFolderPaths,omitempty"`
	LinkedTaskIDs     *[]string        `json:"linkedTaskIds,omitempty"`
	Tags              *[]string        `json:"tags,omitempty"`
	DueDate           *time.Time       `json:"dueDate,omitempty"`
	ClearDueDate      bool             `json:"clearDueDate,omitempty"`
	Recurring         *RecurringConfig `json:"recurring,omitempty"`
	ClearRecurring    bool             `json:"clearRecurring,omitempty"`
	// CronExpr shortcut: sets recurring.cronExpr + recomputes nextRunAt
	SetCronExpr string `json:"setCronExpr,omitempty"`
}

type AddCommentRequest struct {
	Content string `json:"content" validate:"required"`
}

type UpdateCommentRequest struct {
	Content string `json:"content" validate:"required"`
}
