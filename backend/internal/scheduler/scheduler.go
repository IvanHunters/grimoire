package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/robfig/cron/v3"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// Storage interface for scheduler operations.
type Storage interface {
	ListRecurringDueTasks(ctx context.Context) ([]models.Task, error)
	GetTask(ctx context.Context, id string) (*models.Task, error)
	UpdateTask(ctx context.Context, t *models.Task) error
	GetProject(ctx context.Context, id string) (*models.Project, error)
}

// Scheduler polls MongoDB for recurring tasks that are due and runs them.
type Scheduler struct {
	store    Storage
	mongoURI string
	mongoDB  string
	logger   *slog.Logger
	running  sync.Map // taskID → bool
	cancels  sync.Map // taskID → context.CancelFunc
}

func New(store Storage, mongoURI, mongoDB string, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		store:    store,
		mongoURI: mongoURI,
		mongoDB:  mongoDB,
		logger:   logger,
	}
}

// Start runs the scheduler loop until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	s.tick(ctx) // run immediately on startup

	for {
		select {
		case <-ticker.C:
			s.tick(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	tasks, err := s.store.ListRecurringDueTasks(ctx)
	if err != nil {
		s.logger.Error("scheduler: list due tasks", slog.Any("error", err))
		return
	}
	for _, task := range tasks {
		if _, running := s.running.Load(task.ID); running {
			continue
		}
		s.running.Store(task.ID, true)
		go s.runTask(task)
	}
}

// RunNow triggers a task immediately, regardless of schedule. Used by the API.
func (s *Scheduler) RunNow(taskID string) error {
	task, err := s.store.GetTask(context.Background(), taskID)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if _, running := s.running.Load(taskID); running {
		return fmt.Errorf("task is already running")
	}
	s.running.Store(taskID, true)
	go s.runTask(*task)
	return nil
}

// CancelNow cancels a currently running task by its ID.
func (s *Scheduler) CancelNow(taskID string) error {
	val, ok := s.cancels.Load(taskID)
	if !ok {
		return fmt.Errorf("task is not running")
	}
	val.(context.CancelFunc)()
	return nil
}

func (s *Scheduler) runTask(task models.Task) {
	defer s.running.Delete(task.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s.cancels.Store(task.ID, cancel)
	defer func() {
		cancel()
		s.cancels.Delete(task.ID)
	}()

	s.logger.Info("scheduler: starting task", slog.String("id", task.ID), slog.String("title", task.Title))

	// Look up project for richer prompt context.
	var project *models.Project
	if task.ProjectID != "" {
		if p, err := s.store.GetProject(ctx, task.ProjectID); err == nil {
			project = p
		}
	}

	workDir, err := os.MkdirTemp("", "claude-scheduler-*")
	if err != nil {
		s.logger.Error("scheduler: mkdirtemp", slog.Any("error", err))
		s.saveResult(task, fmt.Sprintf("❌ Failed to create working directory: %v", err))
		return
	}
	defer os.RemoveAll(workDir)

	if _, err := claude.SetupMCPConfig(workDir, s.mongoURI, s.mongoDB); err != nil {
		s.logger.Warn("scheduler: mcp config setup failed, continuing without MCP", slog.Any("error", err))
	}

	prompt := buildPrompt(task, project)

	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions", "--output-format", "text")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = workDir
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = append(filtered, "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")

	out, err := cmd.Output()

	var result string
	if err != nil {
		s.logger.Error("scheduler: claude run failed", slog.String("id", task.ID), slog.Any("error", err))
		result = fmt.Sprintf("❌ Run failed: %v\n\n%s", err, string(out))
	} else {
		s.logger.Info("scheduler: task done", slog.String("id", task.ID))
		result = strings.TrimSpace(string(out))
	}

	s.saveResult(task, result)
}

func (s *Scheduler) saveResult(task models.Task, result string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Reload fresh task to avoid overwriting concurrent edits.
	fresh, err := s.store.GetTask(ctx, task.ID)
	if err != nil {
		s.logger.Error("scheduler: reload task", slog.Any("error", err))
		return
	}

	now := time.Now()
	comment := models.TaskComment{
		ID:        uuid.New().String(),
		Content:   fmt.Sprintf("🤖 Scheduled run at %s:\n\n%s", now.Format("02 Jan 15:04"), result),
		CreatedAt: now,
		UpdatedAt: now,
	}
	fresh.Comments = append(fresh.Comments, comment)

	if fresh.Recurring != nil {
		fresh.Recurring.LastRunAt = &now
		next, err := ComputeNextRun(fresh.Recurring, now)
		if err != nil || next == nil {
			fresh.Recurring.Enabled = false
		} else {
			fresh.Recurring.NextRunAt = next
		}
	}

	if err := s.store.UpdateTask(ctx, fresh); err != nil {
		s.logger.Error("scheduler: update task after run", slog.Any("error", err))
	}
}

func buildPrompt(task models.Task, project *models.Project) string {
	var sb strings.Builder

	sb.WriteString("You are an automated Claude agent executing a scheduled task.\n")
	sb.WriteString("Use the MCP tools below to complete the work. Respond only in English.\n\n")

	sb.WriteString("═══════════════════════════════════════════\n")
	sb.WriteString("TASK CONTEXT\n")
	sb.WriteString("═══════════════════════════════════════════\n\n")

	sb.WriteString("Task ID    : " + task.ID + "\n")
	sb.WriteString("Task title : " + task.Title + "\n")

	if project != nil {
		sb.WriteString("Project    : " + project.Title + "\n")
		sb.WriteString("Project ID : " + project.ID + "\n")
		if project.LinkedFolderPath != "" {
			sb.WriteString("Project folder: " + project.LinkedFolderPath + "\n")
		}
	} else if task.ProjectID != "" {
		sb.WriteString("Project ID : " + task.ProjectID + "\n")
	}

	if len(task.LinkedFolderPaths) > 0 {
		sb.WriteString("Linked folders: " + strings.Join(task.LinkedFolderPaths, ", ") + "\n")
	}
	if len(task.Tags) > 0 {
		sb.WriteString("Tags       : " + strings.Join(task.Tags, ", ") + "\n")
	}

	sb.WriteString("\n")

	if task.Description != "" {
		sb.WriteString("═══════════════════════════════════════════\n")
		sb.WriteString("INSTRUCTIONS\n")
		sb.WriteString("═══════════════════════════════════════════\n\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	sb.WriteString("═══════════════════════════════════════════\n")
	sb.WriteString("AVAILABLE MCP TOOLS\n")
	sb.WriteString("═══════════════════════════════════════════\n\n")
	sb.WriteString("Task management:\n")
	sb.WriteString("  create_task(title, description?, projectId?, status?, priority?, linkedFolderPaths?, tags?)\n")
	sb.WriteString("  list_tasks(projectId?, status?)      — list tasks in a project\n")
	sb.WriteString("  get_task(id)                         — get full task details\n")
	sb.WriteString("  update_task(id, ...)                 — update task fields\n")
	sb.WriteString("  move_task(id, status)                — change kanban column\n")
	sb.WriteString("  add_task_comment(id, content)        — add comment to a task\n")
	sb.WriteString("  search_tasks(query, projectId?)      — search tasks by text\n")
	sb.WriteString("\nNotes:\n")
	sb.WriteString("  search_notes(query, summary_only=true, limit=10)\n")
	sb.WriteString("  read_note(path)\n")
	sb.WriteString("  list_notes_summary(folder)\n")

	sb.WriteString("\n═══════════════════════════════════════════\n")
	sb.WriteString("MANDATORY RULES\n")
	sb.WriteString("═══════════════════════════════════════════\n\n")

	if project != nil {
		sb.WriteString(fmt.Sprintf("- ALWAYS set projectId=\"%s\" when calling create_task\n", project.ID))
		sb.WriteString("- NEVER create tasks in other projects or without a projectId\n")
	} else if task.ProjectID != "" {
		sb.WriteString(fmt.Sprintf("- ALWAYS set projectId=\"%s\" when calling create_task\n", task.ProjectID))
	}

	if len(task.LinkedFolderPaths) > 0 {
		sb.WriteString(fmt.Sprintf("- When linking tasks to folders use: %s\n", strings.Join(task.LinkedFolderPaths, ", ")))
	}

	sb.WriteString("- Do not open interactive sessions or request user input — complete the work autonomously\n")
	sb.WriteString("- When done, output a brief summary (3–10 lines) of exactly what was done\n")

	return sb.String()
}

// ComputeNextRun calculates the next scheduled time after `from` for a RecurringConfig.
// Returns nil if the config has no valid schedule.
func ComputeNextRun(r *models.RecurringConfig, from time.Time) (*time.Time, error) {
	if r == nil {
		return nil, nil
	}
	if r.CronExpr != "" {
		sched, err := cron.ParseStandard(r.CronExpr)
		if err != nil {
			return nil, fmt.Errorf("invalid cron expression %q: %w", r.CronExpr, err)
		}
		next := sched.Next(from)
		return &next, nil
	}
	if r.IntervalMinutes > 0 {
		next := from.Add(time.Duration(r.IntervalMinutes) * time.Minute)
		return &next, nil
	}
	return nil, nil
}

// ValidateCronExpr returns an error if the cron expression is invalid.
func ValidateCronExpr(expr string) error {
	_, err := cron.ParseStandard(expr)
	return err
}

// Verify MongoStorage satisfies Storage at compile time.
var _ Storage = (*storage.MongoStorage)(nil)
