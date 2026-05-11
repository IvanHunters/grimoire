package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTaskTools(s *server.MCPServer, ctx *MCPContext) {
	// list_projects
	s.AddTool(
		mcp.NewTool("list_projects",
			mcp.WithDescription("List all projects in the task tracker"),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			projects, err := ctx.store.ListProjects(tCtx)
			if err != nil {
				return mcpError(err), nil
			}
			if len(projects) == 0 {
				return mcpText("No projects found. Create one with create_project."), nil
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Projects (%d):\n\n", len(projects))
			for _, p := range projects {
				fmt.Fprintf(&sb, "• [%s] %s", p.ID, p.Title)
				if p.Description != "" {
					fmt.Fprintf(&sb, " — %s", p.Description)
				}
				if p.LinkedFolderPath != "" {
					fmt.Fprintf(&sb, " (folder: %s)", p.LinkedFolderPath)
				}
				sb.WriteString("\n")
			}
			return mcpText(sb.String()), nil
		},
	)

	// create_project
	s.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Create a new project in the task tracker"),
			mcp.WithString("title", mcp.Required(), mcp.Description("Project title")),
			mcp.WithString("description", mcp.Description("Project description")),
			mcp.WithString("color", mcp.Description("Hex color, e.g. #6366f1")),
			mcp.WithString("linked_folder_path", mcp.Description("Knowledge base folder path to link to this project")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			color := req.GetString("color", "#6366f1")
			now := time.Now()
			p := &models.Project{
				ID:               uuid.New().String(),
				Title:            req.GetString("title", ""),
				Description:      req.GetString("description", ""),
				Color:            color,
				LinkedFolderPath: req.GetString("linked_folder_path", ""),
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := ctx.store.CreateProject(tCtx, p); err != nil {
				return mcpError(err), nil
			}
			return mcpText(fmt.Sprintf("Created project %q (id: %s)", p.Title, p.ID)), nil
		},
	)

	// update_project
	s.AddTool(
		mcp.NewTool("update_project",
			mcp.WithDescription("Update an existing project"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Project ID")),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("color", mcp.Description("New color")),
			mcp.WithString("linked_folder_path", mcp.Description("New linked folder path (empty to clear)")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			id := req.GetString("id", "")
			p, err := ctx.store.GetProject(tCtx, id)
			if err != nil {
				return mcpError(fmt.Errorf("project not found: %s", id)), nil
			}
			if t := req.GetString("title", ""); t != "" {
				p.Title = t
			}
			if d := req.GetString("description", ""); d != "" {
				p.Description = d
			}
			if c := req.GetString("color", ""); c != "" {
				p.Color = c
			}
			p.LinkedFolderPath = req.GetString("linked_folder_path", p.LinkedFolderPath)

			if err := ctx.store.UpdateProject(tCtx, p); err != nil {
				return mcpError(err), nil
			}
			return mcpText(fmt.Sprintf("Updated project %q", p.Title)), nil
		},
	)

	// delete_project
	s.AddTool(
		mcp.NewTool("delete_project",
			mcp.WithDescription("Delete a project (tasks are NOT deleted)"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Project ID")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()
			if err := ctx.store.DeleteProject(tCtx, req.GetString("id", "")); err != nil {
				return mcpError(err), nil
			}
			return mcpText("Project deleted"), nil
		},
	)

	// list_tasks
	s.AddTool(
		mcp.NewTool("list_tasks",
			mcp.WithDescription("List tasks, optionally filtered by project, status, or linked folder"),
			mcp.WithString("project_id", mcp.Description("Filter by project ID")),
			mcp.WithString("status", mcp.Description("Filter by status: backlog, todo, in_progress, done")),
			mcp.WithString("folder_path", mcp.Description("Filter by linked folder path")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			tasks, err := ctx.store.ListTasks(tCtx,
				req.GetString("project_id", ""),
				req.GetString("status", ""),
				req.GetString("folder_path", ""),
			)
			if err != nil {
				return mcpError(err), nil
			}
			if len(tasks) == 0 {
				return mcpText("No tasks found."), nil
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Tasks (%d):\n\n", len(tasks))
			for _, t := range tasks {
				due := ""
				if t.DueDate != nil {
					due = " due:" + t.DueDate.Format("2006-01-02")
				}
				fmt.Fprintf(&sb, "[%s] [%s] [%s] %s%s\n", t.ID, t.Status, t.Priority, t.Title, due)
				if len(t.LinkedNoteIDs) > 0 {
					fmt.Fprintf(&sb, "  notes: %s\n", strings.Join(t.LinkedNoteIDs, ", "))
				}
				if len(t.LinkedFolderPaths) > 0 {
					fmt.Fprintf(&sb, "  folders: %s\n", strings.Join(t.LinkedFolderPaths, ", "))
				}
				if len(t.Comments) > 0 {
					fmt.Fprintf(&sb, "  comments: %d\n", len(t.Comments))
				}
			}
			return mcpText(sb.String()), nil
		},
	)

	// get_task
	s.AddTool(
		mcp.NewTool("get_task",
			mcp.WithDescription("Get full details of a task including comments"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}

			var sb strings.Builder
			fmt.Fprintf(&sb, "# %s\n", t.Title)
			fmt.Fprintf(&sb, "ID: %s\n", t.ID)
			fmt.Fprintf(&sb, "Status: %s | Priority: %s\n", t.Status, t.Priority)
			if t.ProjectID != "" {
				fmt.Fprintf(&sb, "Project: %s\n", t.ProjectID)
			}
			if t.DueDate != nil {
				fmt.Fprintf(&sb, "Due: %s\n", t.DueDate.Format("2006-01-02"))
			}
			if t.Description != "" {
				fmt.Fprintf(&sb, "\nDescription:\n%s\n", t.Description)
			}
			if len(t.Tags) > 0 {
				fmt.Fprintf(&sb, "\nTags: %s\n", strings.Join(t.Tags, ", "))
			}
			if len(t.LinkedNoteIDs) > 0 {
				fmt.Fprintf(&sb, "\nLinked notes: %s\n", strings.Join(t.LinkedNoteIDs, ", "))
			}
			if len(t.LinkedFolderPaths) > 0 {
				fmt.Fprintf(&sb, "Linked folders: %s\n", strings.Join(t.LinkedFolderPaths, ", "))
			}
			if len(t.Comments) > 0 {
				fmt.Fprintf(&sb, "\nComments (%d):\n", len(t.Comments))
				for _, c := range t.Comments {
					fmt.Fprintf(&sb, "  [%s] %s — %s\n", c.ID[:8], c.CreatedAt.Format("2006-01-02 15:04"), c.Content)
				}
			}
			return mcpText(sb.String()), nil
		},
	)

	// create_task
	s.AddTool(
		mcp.NewTool("create_task",
			mcp.WithDescription("Create a new task. Use create_story for user stories."),
			mcp.WithString("title", mcp.Required(), mcp.Description("Task title")),
			mcp.WithString("parent_id", mcp.Description("User story ID to make this a subtask")),
			mcp.WithString("project_id", mcp.Description("Project ID to assign task to")),
			mcp.WithString("description", mcp.Description("Task description (markdown supported)")),
			mcp.WithString("status", mcp.Description("Status: backlog (default), todo, in_progress, done")),
			mcp.WithString("priority", mcp.Description("Priority: low, medium (default), high, urgent")),
			mcp.WithString("due_date", mcp.Description("Due date in YYYY-MM-DD format")),
			mcp.WithString("tags", mcp.Description("Comma-separated tags")),
			mcp.WithString("linked_note_ids", mcp.Description("Comma-separated note IDs to link")),
			mcp.WithString("linked_folder_paths", mcp.Description("Comma-separated folder paths to link")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			status := models.TaskStatus(req.GetString("status", ""))
			if status == "" {
				status = models.TaskStatusBacklog
			}
			priority := models.TaskPriority(req.GetString("priority", ""))
			if priority == "" {
				priority = models.TaskPriorityMedium
			}

			var dueDate *time.Time
			if ds := req.GetString("due_date", ""); ds != "" {
				if d, err := time.Parse("2006-01-02", ds); err == nil {
					dueDate = &d
				}
			}

			now := time.Now()
			t := &models.Task{
				ID:                uuid.New().String(),
				ParentID:          req.GetString("parent_id", ""),
				Title:             req.GetString("title", ""),
				Description:       req.GetString("description", ""),
				ProjectID:         req.GetString("project_id", ""),
				Status:            status,
				Priority:          priority,
				LinkedNoteIDs:     splitCSV(req.GetString("linked_note_ids", "")),
				LinkedFolderPaths: splitCSV(req.GetString("linked_folder_paths", "")),
				Tags:              splitCSV(req.GetString("tags", "")),
				Comments:          []models.TaskComment{},
				DueDate:           dueDate,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if err := ctx.store.CreateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskCreated, Task: t})
			return mcpText(fmt.Sprintf("Created task %q (id: %s, status: %s)", t.Title, t.ID, t.Status)), nil
		},
	)

	// update_task
	s.AddTool(
		mcp.NewTool("update_task",
			mcp.WithDescription("Update an existing task (move between statuses, change priority, etc.)"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
			mcp.WithString("title", mcp.Description("New title")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("status", mcp.Description("New status: backlog, todo, in_progress, done")),
			mcp.WithString("priority", mcp.Description("New priority: low, medium, high, urgent")),
			mcp.WithString("project_id", mcp.Description("Move to project (empty to remove from project)")),
			mcp.WithString("due_date", mcp.Description("New due date YYYY-MM-DD, or 'clear' to remove")),
			mcp.WithString("tags", mcp.Description("Replace tags (comma-separated)")),
			mcp.WithString("linked_note_ids", mcp.Description("Replace linked note IDs (comma-separated)")),
			mcp.WithString("linked_folder_paths", mcp.Description("Replace linked folder paths (comma-separated)")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}

			if v := req.GetString("title", ""); v != "" {
				t.Title = v
			}
			if v := req.GetString("description", ""); v != "" {
				t.Description = v
			}
			if v := req.GetString("status", ""); v != "" {
				t.Status = models.TaskStatus(v)
			}
			if v := req.GetString("priority", ""); v != "" {
				t.Priority = models.TaskPriority(v)
			}
			// project_id: allow setting to empty string (remove from project)
			if v := req.GetString("project_id", "\x00"); v != "\x00" {
				t.ProjectID = v
			}
			if v := req.GetString("tags", ""); v != "" {
				t.Tags = splitCSV(v)
			}
			if v := req.GetString("linked_note_ids", ""); v != "" {
				t.LinkedNoteIDs = splitCSV(v)
			}
			if v := req.GetString("linked_folder_paths", ""); v != "" {
				t.LinkedFolderPaths = splitCSV(v)
			}
			if ds := req.GetString("due_date", ""); ds != "" {
				if ds == "clear" {
					t.DueDate = nil
				} else if d, err2 := time.Parse("2006-01-02", ds); err2 == nil {
					t.DueDate = &d
				}
			}

			if err := ctx.store.UpdateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			return mcpText(fmt.Sprintf("Updated task %q (status: %s, priority: %s)", t.Title, t.Status, t.Priority)), nil
		},
	)

	// move_task (shorthand for status change)
	s.AddTool(
		mcp.NewTool("move_task",
			mcp.WithDescription("Move a task to a different status column (shorthand for update_task status)"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
			mcp.WithString("status", mcp.Required(), mcp.Description("Target status: backlog, todo, in_progress, done")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}
			oldStatus := t.Status
			t.Status = models.TaskStatus(req.GetString("status", ""))
			if err := ctx.store.UpdateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			return mcpText(fmt.Sprintf("Moved %q: %s → %s", t.Title, oldStatus, t.Status)), nil
		},
	)

	// delete_task
	s.AddTool(
		mcp.NewTool("delete_task",
			mcp.WithDescription("Delete a task permanently"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()
			taskID := req.GetString("id", "")
			if err := ctx.store.DeleteTask(tCtx, taskID); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskDeleted, TaskID: taskID})
			return mcpText("Task deleted"), nil
		},
	)

	// add_task_comment
	s.AddTool(
		mcp.NewTool("add_task_comment",
			mcp.WithDescription("Add a comment to a task"),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Comment text (markdown supported)")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("task_id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}
			now := time.Now()
			comment := models.TaskComment{
				ID:        uuid.New().String(),
				Content:   req.GetString("content", ""),
				CreatedAt: now,
				UpdatedAt: now,
			}
			t.Comments = append(t.Comments, comment)
			if err := ctx.store.UpdateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			return mcpText(fmt.Sprintf("Added comment to %q (comment id: %s)", t.Title, comment.ID)), nil
		},
	)

	// link_note_to_task
	s.AddTool(
		mcp.NewTool("link_note_to_task",
			mcp.WithDescription("Link a note to a task"),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID")),
			mcp.WithString("note_id", mcp.Required(), mcp.Description("Note ID")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("task_id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}
			noteID := req.GetString("note_id", "")
			for _, id := range t.LinkedNoteIDs {
				if id == noteID {
					return mcpText("Note already linked to this task"), nil
				}
			}
			t.LinkedNoteIDs = append(t.LinkedNoteIDs, noteID)
			if err := ctx.store.UpdateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			return mcpText(fmt.Sprintf("Linked note %s to task %q", noteID, t.Title)), nil
		},
	)

	// link_folder_to_task
	s.AddTool(
		mcp.NewTool("link_folder_to_task",
			mcp.WithDescription("Link a knowledge base folder to a task"),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID")),
			mcp.WithString("folder_path", mcp.Required(), mcp.Description("Folder path")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("task_id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}
			fp := req.GetString("folder_path", "")
			for _, p := range t.LinkedFolderPaths {
				if p == fp {
					return mcpText("Folder already linked to this task"), nil
				}
			}
			t.LinkedFolderPaths = append(t.LinkedFolderPaths, fp)
			if err := ctx.store.UpdateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			return mcpText(fmt.Sprintf("Linked folder %q to task %q", fp, t.Title)), nil
		},
	)

	// create_story
	s.AddTool(
		mcp.NewTool("create_story",
			mcp.WithDescription("Create a user story that groups related tasks. Stories appear as collapsible parent cards in the kanban board."),
			mcp.WithString("title", mcp.Required(), mcp.Description("Story title, e.g. 'User Authentication Flow'")),
			mcp.WithString("description", mcp.Description("Story description / acceptance criteria")),
			mcp.WithString("status", mcp.Description("Status: backlog (default), todo, in_progress, done")),
			mcp.WithString("priority", mcp.Description("Priority: low, medium (default), high, urgent")),
			mcp.WithString("linked_folder_paths", mcp.Description("Comma-separated folder paths to link")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			status := models.TaskStatus(req.GetString("status", ""))
			if status == "" {
				status = models.TaskStatusBacklog
			}
			priority := models.TaskPriority(req.GetString("priority", ""))
			if priority == "" {
				priority = models.TaskPriorityMedium
			}
			now := time.Now()
			t := &models.Task{
				ID:                uuid.New().String(),
				Type:              "story",
				Title:             req.GetString("title", ""),
				Description:       req.GetString("description", ""),
				Status:            status,
				Priority:          priority,
				LinkedFolderPaths: splitCSV(req.GetString("linked_folder_paths", "")),
				Comments:          []models.TaskComment{},
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if err := ctx.store.CreateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskCreated, Task: t})
			return mcpText(fmt.Sprintf("Created story %q (id: %s)", t.Title, t.ID)), nil
		},
	)

	// set_task_parent
	s.AddTool(
		mcp.NewTool("set_task_parent",
			mcp.WithDescription("Make a task a subtask of a user story. Pass empty story_id to detach from parent."),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to make a subtask")),
			mcp.WithString("story_id", mcp.Required(), mcp.Description("User story ID (parent). Pass empty string to detach.")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			t, err := ctx.store.GetTask(tCtx, req.GetString("task_id", ""))
			if err != nil {
				return mcpError(fmt.Errorf("task not found")), nil
			}
			storyID := req.GetString("story_id", "")
			if storyID == "" {
				t.ParentID = ""
				if err := ctx.store.UpdateTask(tCtx, t); err != nil {
					return mcpError(err), nil
				}
				ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
				return mcpText(fmt.Sprintf("Detached %q from parent story", t.Title)), nil
			}
			// Verify story exists
			story, err := ctx.store.GetTask(tCtx, storyID)
			if err != nil {
				return mcpError(fmt.Errorf("story not found: %s", storyID)), nil
			}
			t.ParentID = storyID
			if err := ctx.store.UpdateTask(tCtx, t); err != nil {
				return mcpError(err), nil
			}
			ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			return mcpText(fmt.Sprintf("Set %q as subtask of story %q", t.Title, story.Title)), nil
		},
	)

	// link_tasks
	s.AddTool(
		mcp.NewTool("link_tasks",
			mcp.WithDescription("Create a bidirectional link between two tasks (related tasks, dependencies, etc.)"),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("First task ID")),
			mcp.WithString("linked_task_id", mcp.Required(), mcp.Description("Second task ID to link to")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			taskID := req.GetString("task_id", "")
			linkedID := req.GetString("linked_task_id", "")

			t, err := ctx.store.GetTask(tCtx, taskID)
			if err != nil {
				return mcpError(fmt.Errorf("task not found: %s", taskID)), nil
			}
			linked, err := ctx.store.GetTask(tCtx, linkedID)
			if err != nil {
				return mcpError(fmt.Errorf("linked task not found: %s", linkedID)), nil
			}

			// Add to first task if not already linked
			alreadyLinked := false
			for _, id := range t.LinkedTaskIDs {
				if id == linkedID {
					alreadyLinked = true
					break
				}
			}
			if !alreadyLinked {
				t.LinkedTaskIDs = append(t.LinkedTaskIDs, linkedID)
				if err := ctx.store.UpdateTask(tCtx, t); err != nil {
					return mcpError(err), nil
				}
				ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: t})
			}

			// Add reverse link
			hasReverse := false
			for _, id := range linked.LinkedTaskIDs {
				if id == taskID {
					hasReverse = true
					break
				}
			}
			if !hasReverse {
				linked.LinkedTaskIDs = append(linked.LinkedTaskIDs, taskID)
				if err := ctx.store.UpdateTask(tCtx, linked); err != nil {
					return mcpError(err), nil
				}
				ctx.eventBus.Publish(events.Event{Type: events.EventTaskUpdated, Task: linked})
			}

			return mcpText(fmt.Sprintf("Linked %q ↔ %q", t.Title, linked.Title)), nil
		},
	)

	// search_tasks
	s.AddTool(
		mcp.NewTool("search_tasks",
			mcp.WithDescription("Search tasks by text (title, description, comments)"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithString("project_id", mcp.Description("Limit search to a specific project")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			tasks, err := ctx.store.SearchTasks(tCtx,
				req.GetString("query", ""),
				req.GetString("project_id", ""),
			)
			if err != nil {
				return mcpError(err), nil
			}
			if len(tasks) == 0 {
				return mcpText("No tasks found matching the query."), nil
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Found %d tasks:\n\n", len(tasks))
			for _, t := range tasks {
				fmt.Fprintf(&sb, "[%s] [%s] [%s] %s\n", t.ID, t.Status, t.Priority, t.Title)
				if t.Description != "" {
					desc := t.Description
					if len(desc) > 100 {
						desc = desc[:100] + "…"
					}
					fmt.Fprintf(&sb, "  %s\n", desc)
				}
			}
			return mcpText(sb.String()), nil
		},
	)

	// get_kanban_columns — returns current column configuration
	s.AddTool(
		mcp.NewTool("get_kanban_columns",
			mcp.WithDescription("Get the current kanban column configuration (id, label, order). Columns are dynamic and configurable."),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			cols, err := ctx.store.GetKanbanColumns(tCtx)
			if err != nil {
				return mcpError(err), nil
			}

			defaultCols := []struct{ id, label string }{
				{"backlog", "Backlog"},
				{"todo", "To Do"},
				{"in_progress", "In Progress"},
				{"done", "Done"},
			}

			var sb strings.Builder
			if len(cols) == 0 {
				sb.WriteString("Kanban columns (default, not yet customized):\n\n")
				for i, c := range defaultCols {
					fmt.Fprintf(&sb, "%d. %s (id: %s)\n", i+1, c.label, c.id)
				}
			} else {
				fmt.Fprintf(&sb, "Kanban columns (%d):\n\n", len(cols))
				for _, c := range cols {
					fmt.Fprintf(&sb, "%d. %s (id: %s)\n", c.Order+1, c.Label, c.ID)
				}
			}
			sb.WriteString("\nUse column id as the 'status' value when creating or moving tasks.")
			return mcpText(sb.String()), nil
		},
	)

	// get_task_board — board overview: all columns with task counts and lists
	s.AddTool(
		mcp.NewTool("get_task_board",
			mcp.WithDescription("Get a full board overview: all columns with their tasks. Optionally filter by folder path (project). Good for getting the big picture before working on tasks."),
			mcp.WithString("folder_path", mcp.Description("Filter by linked folder path (project)")),
		),
		func(reqCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			folderPath := req.GetString("folder_path", "")

			// Load columns
			cols, err := ctx.store.GetKanbanColumns(tCtx)
			if err != nil || len(cols) == 0 {
				cols = []models.KanbanColumn{
					{ID: "backlog", Label: "Backlog", Order: 0},
					{ID: "todo", Label: "To Do", Order: 1},
					{ID: "in_progress", Label: "In Progress", Order: 2},
					{ID: "done", Label: "Done", Order: 3},
				}
			}

			// Sort columns by order
			for i := 0; i < len(cols)-1; i++ {
				for j := i + 1; j < len(cols); j++ {
					if cols[j].Order < cols[i].Order {
						cols[i], cols[j] = cols[j], cols[i]
					}
				}
			}

			var sb strings.Builder
			if folderPath != "" {
				fmt.Fprintf(&sb, "Task board — folder: %s\n\n", folderPath)
			} else {
				sb.WriteString("Task board — all tasks\n\n")
			}

			totalTasks := 0
			for _, col := range cols {
				tasks, err := ctx.store.ListTasks(tCtx, "", string(col.ID), folderPath)
				if err != nil {
					continue
				}
				fmt.Fprintf(&sb, "── %s (%d) ──\n", col.Label, len(tasks))
				if len(tasks) == 0 {
					sb.WriteString("  (empty)\n")
				}
				for _, t := range tasks {
					due := ""
					if t.DueDate != nil {
						due = " [due:" + t.DueDate.Format("2006-01-02") + "]"
					}
					fmt.Fprintf(&sb, "  [%s] [%s] %s%s\n", t.ID, t.Priority, t.Title, due)
				}
				sb.WriteString("\n")
				totalTasks += len(tasks)
			}
			fmt.Fprintf(&sb, "Total: %d tasks across %d columns", totalTasks, len(cols))
			return mcpText(sb.String()), nil
		},
	)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func mcpText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(text)}}
}

func mcpError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: " + err.Error())}}
}

func splitCSV(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
