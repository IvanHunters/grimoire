package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MCPContext struct {
	store    *storage.MongoStorage
	logger   *slog.Logger
	eventBus *events.EventBus
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server for Claude Code integration",
	Long:  `Starts Model Context Protocol server using stdio transport for Claude Code`,
	RunE:  runMCP,
}

func runMCP(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Connect to MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoDBURI))
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}
	defer client.Disconnect(context.Background())

	db := client.Database(cfg.MongoDBDatabase)
	store := storage.NewMongoStorage(db)

	mcpCtx := &MCPContext{
		store:    store,
		logger:   logger,
		eventBus: events.GetEventBus(),
	}

	// Create MCP server
	s := server.NewMCPServer(
		"Markdown Editor",
		"1.0.0",
		server.WithLogging(),
	)

	// Register tools
	registerNoteTools(s, mcpCtx)
	registerGraphTools(s, mcpCtx)
	registerFolderTools(s, mcpCtx)
	registerNoteManagementTools(s, mcpCtx)

	logger.Info("mcp server starting")

	// Start server (stdio mode)
	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func registerNoteTools(s *server.MCPServer, ctx *MCPContext) {
	// list_notes
	s.AddTool(
		mcp.NewTool("list_notes",
			mcp.WithDescription("List all notes, optionally filtered by folder"),
			mcp.WithString("folder",
				mcp.Description("Folder path to filter notes (empty string for all notes)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			folder := request.GetString("folder", "")
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			notes, err := ctx.store.ListNotes(timeoutCtx, folder)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes:\n\n", len(notes))
			for _, note := range notes {
				result += fmt.Sprintf("- [%s] %s (%s)\n", note.ID, note.Title, note.Path)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// read_note
	s.AddTool(
		mcp.NewTool("read_note",
			mcp.WithDescription("Read a note by its path or ID"),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id := request.GetString("id", "")
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			note, err := ctx.store.GetNote(timeoutCtx, id)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, id)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", id)),
						},
					}, nil
				}
			}

			result := fmt.Sprintf("# %s\n\nPath: %s\nFolder: %s\n\n---\n\n%s",
				note.Title, note.Path, note.Folder, note.Content)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// create_note
	s.AddTool(
		mcp.NewTool("create_note",
			mcp.WithDescription("Create a new note"),
			mcp.WithString("title",
				mcp.Required(),
				mcp.Description("Note title"),
			),
			mcp.WithString("content",
				mcp.Required(),
				mcp.Description("Note content in markdown"),
			),
			mcp.WithString("folder",
				mcp.Description("Folder path (optional)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			title := request.GetString("title", "")
			content := request.GetString("content", "")
			folder := request.GetString("folder", "")

			fileName := normalizeTitle(title) + ".md"
			path := fileName
			if folder != "" {
				path = folder + "/" + fileName
			}

			note := &models.Note{
				ID:      uuid.New().String(),
				Path:    path,
				Title:   title,
				Folder:  folder,
				Content: content,
			}

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			if err := ctx.store.CreateNote(timeoutCtx, note); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error creating note: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type: events.EventNoteCreated,
				Note: note,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note created: %s (ID: %s)", note.Path, note.ID)),
				},
			}, nil
		},
	)

	// search_notes
	s.AddTool(
		mcp.NewTool("search_notes",
			mcp.WithDescription("Search notes by content"),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Search query"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query := request.GetString("query", "")
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			notes, err := ctx.store.SearchNotes(timeoutCtx, query)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes matching '%s':\n\n", len(notes), query)
			for _, note := range notes {
				result += fmt.Sprintf("- [%s] %s (%s)\n", note.ID, note.Title, note.Path)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// update_note - Update note content
	s.AddTool(
		mcp.NewTool("update_note",
			mcp.WithDescription("Update note content (wikilinks will be automatically parsed)"),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("content",
				mcp.Required(),
				mcp.Description("New note content in markdown"),
			),
			mcp.WithString("title",
				mcp.Description("Optional: update note title"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id := request.GetString("id", "")
			content := request.GetString("content", "")
			title := request.GetString("title", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get existing note
			note, err := ctx.store.GetNote(timeoutCtx, id)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, id)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", id)),
						},
					}, nil
				}
			}

			// Update fields
			note.Content = content
			if title != "" {
				note.Title = title
			}

			// Update note (wikilinks will be automatically parsed)
			if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error updating note: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type: events.EventNoteUpdated,
				Note: note,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note updated: %s\nWikilinks parsed: %d", note.Path, len(note.OutgoingLinks))),
				},
			}, nil
		},
	)

	// delete_note - Delete a note
	s.AddTool(
		mcp.NewTool("delete_note",
			mcp.WithDescription("Delete a note (will also remove from other notes' backlinks)"),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id := request.GetString("id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note first to get its data for event
			note, err := ctx.store.GetNote(timeoutCtx, id)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, id)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", id)),
						},
					}, nil
				}
			}

			// Delete note
			if err := ctx.store.DeleteNote(timeoutCtx, note.ID); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error deleting note: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type:   events.EventNoteDeleted,
				NoteID: note.ID,
				Path:   note.Path,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note deleted: %s", note.Path)),
				},
			}, nil
		},
	)

	// add_wikilink - Helper to add wikilink to a note
	s.AddTool(
		mcp.NewTool("add_wikilink",
			mcp.WithDescription("Add a wikilink from one note to another (automatically updates content)"),
			mcp.WithString("source_note_id",
				mcp.Required(),
				mcp.Description("Source note ID or path"),
			),
			mcp.WithString("target_note_id",
				mcp.Required(),
				mcp.Description("Target note ID or path (to link to)"),
			),
			mcp.WithString("section",
				mcp.Description("Optional: section name where to add link (default: 'Related Notes')"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sourceID := request.GetString("source_note_id", "")
			targetID := request.GetString("target_note_id", "")
			section := request.GetString("section", "Related Notes")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get source note
			sourceNote, err := ctx.store.GetNote(timeoutCtx, sourceID)
			if err != nil {
				sourceNote, err = ctx.store.GetNoteByPath(timeoutCtx, sourceID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Source note not found: %s", sourceID)),
						},
					}, nil
				}
			}

			// Get target note
			targetNote, err := ctx.store.GetNote(timeoutCtx, targetID)
			if err != nil {
				targetNote, err = ctx.store.GetNoteByPath(timeoutCtx, targetID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Target note not found: %s", targetID)),
						},
					}, nil
				}
			}

			// Add wikilink to content
			wikilink := fmt.Sprintf("[[%s]]", targetNote.Title)

			// Check if section exists
			sectionHeader := fmt.Sprintf("\n## %s\n", section)
			if !contains(sourceNote.Content, sectionHeader) {
				// Add section at the end
				sourceNote.Content += fmt.Sprintf("\n\n## %s\n\n- %s\n", section, wikilink)
			} else {
				// Add to existing section
				sourceNote.Content += fmt.Sprintf("\n- %s", wikilink)
			}

			// Update note
			if err := ctx.store.UpdateNote(timeoutCtx, sourceNote); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error updating note: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type: events.EventNoteUpdated,
				Note: sourceNote,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Wikilink added: %s → %s\nBacklink automatically created!", sourceNote.Title, targetNote.Title)),
				},
			}, nil
		},
	)

	// remove_wikilink - Helper to remove wikilink from a note
	s.AddTool(
		mcp.NewTool("remove_wikilink",
			mcp.WithDescription("Remove a wikilink from a note (automatically updates content and backlinks)"),
			mcp.WithString("source_note_id",
				mcp.Required(),
				mcp.Description("Source note ID or path"),
			),
			mcp.WithString("target_note_id",
				mcp.Required(),
				mcp.Description("Target note ID or path (to unlink from)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sourceID := request.GetString("source_note_id", "")
			targetID := request.GetString("target_note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get source note
			sourceNote, err := ctx.store.GetNote(timeoutCtx, sourceID)
			if err != nil {
				sourceNote, err = ctx.store.GetNoteByPath(timeoutCtx, sourceID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Source note not found: %s", sourceID)),
						},
					}, nil
				}
			}

			// Get target note
			targetNote, err := ctx.store.GetNote(timeoutCtx, targetID)
			if err != nil {
				targetNote, err = ctx.store.GetNoteByPath(timeoutCtx, targetID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Target note not found: %s", targetID)),
						},
					}, nil
				}
			}

			// Remove wikilink from content
			wikilink := fmt.Sprintf("[[%s]]", targetNote.Title)
			sourceNote.Content = replaceAll(sourceNote.Content, wikilink, "")

			// Also remove with alias format
			wikilinkAlias := fmt.Sprintf("[[%s|", targetNote.Title)
			sourceNote.Content = removeAliasWikilinks(sourceNote.Content, wikilinkAlias)

			// Clean up empty lines
			sourceNote.Content = cleanEmptyLines(sourceNote.Content)

			// Update note
			if err := ctx.store.UpdateNote(timeoutCtx, sourceNote); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error updating note: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type: events.EventNoteUpdated,
				Note: sourceNote,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Wikilink removed: %s ⛔ %s\nBacklink automatically removed!", sourceNote.Title, targetNote.Title)),
				},
			}, nil
		},
	)

	// create_folder
	s.AddTool(
		mcp.NewTool("create_folder",
			mcp.WithDescription("Create a new folder"),
			mcp.WithString("path",
				mcp.Required(),
				mcp.Description("Folder path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			path := request.GetString("path", "")

			folder := &models.Folder{
				Path: path,
			}

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			if err := ctx.store.CreateFolder(timeoutCtx, folder); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Folder created: %s", path)),
				},
			}, nil
		},
	)
}

func normalizeTitle(title string) string {
	result := ""
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result += string(r)
		} else if r >= 'A' && r <= 'Z' {
			result += string(r + 32)
		} else if r == ' ' || r == '-' {
			result += "-"
		}
	}
	for i := 0; i < len(result)-1; i++ {
		if result[i] == '-' && result[i+1] == '-' {
			result = result[:i] + result[i+1:]
			i--
		}
	}
	return result
}

// Helper functions for wikilink manipulation

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); {
		if i <= len(s)-len(old) && s[i:i+len(old)] == old {
			result += new
			i += len(old)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}

func removeAliasWikilinks(s, prefix string) string {
	result := ""
	i := 0
	for i < len(s) {
		if i <= len(s)-len(prefix) && s[i:i+len(prefix)] == prefix {
			// Find closing ]]
			j := i + len(prefix)
			for j < len(s) && !(j > 0 && s[j-1:j+1] == "]]") {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		result += string(s[i])
		i++
	}
	return result
}

func cleanEmptyLines(s string) string {
	lines := splitLines(s)
	result := ""
	prevEmpty := false
	for _, line := range lines {
		isEmpty := trim(line) == ""
		if isEmpty && prevEmpty {
			continue
		}
		result += line + "\n"
		prevEmpty = isEmpty
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
