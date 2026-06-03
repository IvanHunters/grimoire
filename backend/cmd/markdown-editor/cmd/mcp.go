package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/skills"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type sessionIDKey struct{}

// sessionIDFromCtx returns the session ID injected by the HTTP MCP middleware,
// falling back to the GRIMOIRE_SESSION_ID env var (set for stdio MCP).
func sessionIDFromCtx(ctx context.Context) string {
	if id, ok := ctx.Value(sessionIDKey{}).(string); ok && id != "" {
		return id
	}
	return os.Getenv("GRIMOIRE_SESSION_ID")
}

// resolveSessionID picks the session ID from explicit tool arg first, then context/env.
// Use this in all write tools so Claude can pass its ID explicitly when automatic detection fails.
func resolveSessionID(ctx context.Context, argsMap map[string]interface{}) string {
	if id, ok := argsMap["session_id"].(string); ok && id != "" {
		return id
	}
	return sessionIDFromCtx(ctx)
}

type MCPContext struct {
	store          *storage.MongoStorage
	sessionStorage *storage.SessionStorage
	logger         *slog.Logger
	eventBus       *events.EventBus
	config         *config.Config
	skills         *skills.Syncer
	skillSettings  *skills.SettingsStore
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server for Claude Code integration",
	Long:  `Starts Model Context Protocol server using stdio transport for Claude Code`,
	RunE:  runMCP,
}

// CreateMCPServer создаёт и настраивает MCP сервер
func CreateMCPServer(store *storage.MongoStorage, sessionStorage *storage.SessionStorage, logger *slog.Logger, cfg *config.Config) *server.MCPServer {
	return CreateMCPServerWithSkills(store, sessionStorage, logger, cfg, nil, nil)
}

// CreateMCPServerWithSkills creates the MCP server with an optional skills
// syncer + settings store so skill tools can be registered.
func CreateMCPServerWithSkills(
	store *storage.MongoStorage,
	sessionStorage *storage.SessionStorage,
	logger *slog.Logger,
	cfg *config.Config,
	skillsSyncer *skills.Syncer,
	skillsSettings *skills.SettingsStore,
) *server.MCPServer {
	mcpCtx := &MCPContext{
		store:          store,
		sessionStorage: sessionStorage,
		logger:         logger,
		eventBus:       events.GetEventBus(),
		config:         cfg,
		skills:         skillsSyncer,
		skillSettings:  skillsSettings,
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
	registerAttachmentTools(s, mcpCtx)
	registerContentEditingTools(s, mcpCtx)
	registerContentStructureTools(s, mcpCtx)
	registerProjectTools(s, mcpCtx)
	registerTaskTools(s, mcpCtx)
	registerSessionTools(s, mcpCtx)
	if mcpCtx.skills != nil {
		registerSkillTools(s, mcpCtx)
	}

	return s
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
	sessionStorage := storage.NewSessionStorage(db)

	s := CreateMCPServer(store, sessionStorage, logger, cfg)

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
			mcp.WithDescription("List notes with id, title, path, tags. Does NOT return content (use read_note for that). Prefer list_notes_summary for large folders."),
			mcp.WithString("folder",
				mcp.Description("Folder path to filter notes (empty for all notes)"),
			),
			mcp.WithBoolean("recursive",
				mcp.Description("Include notes in subfolders (default: false)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			folder := request.GetString("folder", "")
			recursive := request.GetString("recursive", "") == "true"
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			notes, err := ctx.store.ListNotesMeta(timeoutCtx, folder, recursive)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes:\n\n", len(notes))
			for _, note := range notes {
				tagsStr := ""
				if len(note.Tags) > 0 {
					tagsStr = fmt.Sprintf(" tags:[%s]", strings.Join(note.Tags, ","))
				}
				result += fmt.Sprintf("- [%s] %s (%s)%s\n", note.ID, note.Title, note.Path, tagsStr)
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

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Auto-create intermediate folders if they don't exist
			if folder != "" {
				if err := ensureFoldersExist(timeoutCtx, ctx.store, folder); err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Error creating folders: %v", err)),
						},
					}, nil
				}
			}

			note := &models.Note{
				ID:      uuid.New().String(),
				Path:    path,
				Title:   title,
				Folder:  folder,
				Content: content,
			}

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
			mcp.WithDescription("Full-text search in notes, ranked by relevance. Use summary_only=true (default) to save tokens. Optionally scope to a folder."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Search query"),
			),
			mcp.WithBoolean("summary_only",
				mcp.Description("Return only title + snippet (default: true)"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max results (default 20, max 100)"),
			),
			mcp.WithString("folder",
				mcp.Description("Limit search to this folder (empty = global search)"),
			),
			mcp.WithBoolean("recursive",
				mcp.Description("Include subfolders when folder is specified (default: true)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query := request.GetString("query", "")
			summaryOnly := request.GetBool("summary_only", true)
			limit := int(request.GetFloat("limit", 20))
			folder := request.GetString("folder", "")
			recursive := request.GetString("recursive", "true") != "false"
			if limit <= 0 || limit > 100 {
				limit = 20
			}
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			var notes []*models.Note
			var err error
			if folder != "" {
				notes, err = ctx.store.SearchNotesInFolder(timeoutCtx, query, folder, recursive, limit)
			} else {
				notes, err = ctx.store.SearchNotes(timeoutCtx, query, limit)
			}
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes matching '%s':\n\n", len(notes), query)
			for _, note := range notes {
				if summaryOnly {
					snippet := note.Content
					if len(snippet) > 150 {
						snippet = snippet[:150] + "..."
					}
					result += fmt.Sprintf("- [%s] %s\n  Path: %s\n  Snippet: %s\n\n", note.ID, note.Title, note.Path, snippet)
				} else {
					result += fmt.Sprintf("- [%s] %s (%s)\n  Content:\n%s\n\n", note.ID, note.Title, note.Path, note.Content)
				}
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_note_by_path — direct access without search, saves tokens when path is known
	s.AddTool(
		mcp.NewTool("get_note_by_path",
			mcp.WithDescription("Get a note directly by its path (e.g. 'Projects/Aenix/rules.md'). Much cheaper than search when you know the path. Returns full content by default."),
			mcp.WithString("path",
				mcp.Required(),
				mcp.Description("Note path, e.g. 'Projects/Aenix/Cozystack/overview.md'"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			path := request.GetString("path", "")
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			note, err := ctx.store.GetNoteByPath(timeoutCtx, path)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Note not found at path '%s': %v", path, err)),
					},
				}, nil
			}

			result := fmt.Sprintf("# %s\nID: %s\nPath: %s\n\n%s", note.Title, note.ID, note.Path, note.Content)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// list_notes_summary
	s.AddTool(
		mcp.NewTool("list_notes_summary",
			mcp.WithDescription("Fast overview of notes — returns only id, title, path, tags, modified date. No content fetched. Use before read_note for details."),
			mcp.WithString("folder",
				mcp.Description("Folder path to filter notes (empty for all notes)"),
			),
			mcp.WithBoolean("recursive",
				mcp.Description("Include notes in subfolders (default: false)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			folder := request.GetString("folder", "")
			recursive := request.GetString("recursive", "") == "true"
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			notes, err := ctx.store.ListNotesMeta(timeoutCtx, folder, recursive)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes:\n\n", len(notes))
			for _, note := range notes {
				modifiedDate := note.UpdatedAt.Format("2006-01-02 15:04")
				tagsStr := ""
				if len(note.Tags) > 0 {
					tagsStr = fmt.Sprintf(" [%s]", strings.Join(note.Tags, ", "))
				}
				result += fmt.Sprintf("- [%s] %s%s\n  Path: %s | Modified: %s\n\n",
					note.ID, note.Title, tagsStr, note.Path, modifiedDate)
			}

			result += "\nUse read_note(id) to read full content of specific notes."

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_notes_by_tag - Get all notes with a specific tag
	s.AddTool(
		mcp.NewTool("get_notes_by_tag",
			mcp.WithDescription("Get all notes that have a specific tag. Simpler alternative to search_by_tags for single-tag queries."),
			mcp.WithString("tag",
				mcp.Required(),
				mcp.Description("Tag to search for (e.g., 'kubernetes')"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results (default: 50)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tag := strings.ToLower(strings.TrimSpace(request.GetString("tag", "")))
			limit := int(request.GetFloat("limit", 50))

			if tag == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent("Error: tag parameter is required"),
					},
				}, nil
			}

			notes := ctx.store.SearchByTags([]string{tag}, limit)

			if len(notes) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("No notes found with tag '%s'", tag)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes with tag '%s':\n\n", len(notes), tag)
			for i, note := range notes {
				snippet := note.Content
				if len(snippet) > 150 {
					snippet = snippet[:150] + "..."
				}
				allTags := strings.Join(note.Tags, ", ")
				result += fmt.Sprintf("%d. [%s] %s\n   Tags: %s\n   Path: %s\n   Snippet: %s\n\n",
					i+1, note.ID, note.Title, allTags, note.Path, snippet)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// search_by_tags - Fast in-memory tag search
	s.AddTool(
		mcp.NewTool("search_by_tags",
			mcp.WithDescription("Fast in-memory search by tags. Ranks results by number of matching tags. Much faster than full-text search for tag-based queries."),
			mcp.WithString("tags",
				mcp.Required(),
				mcp.Description("Comma-separated tags to search for (e.g., 'kubernetes,networking,security')"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results (default: 50)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tagsStr := request.GetString("tags", "")
			limit := int(request.GetFloat("limit", 50))

			if tagsStr == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent("Error: tags parameter is required"),
					},
				}, nil
			}

			// Parse tags
			tags := []string{}
			for _, tag := range strings.Split(tagsStr, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tags = append(tags, tag)
				}
			}

			if len(tags) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent("Error: no valid tags provided"),
					},
				}, nil
			}

			notes := ctx.store.SearchByTags(tags, limit)

			result := fmt.Sprintf("Found %d notes matching tags [%s]:\n\n", len(notes), strings.Join(tags, ", "))
			for i, note := range notes {
				snippet := note.Content
				if len(snippet) > 150 {
					snippet = snippet[:150] + "..."
				}
				tagList := strings.Join(note.Tags, ", ")
				result += fmt.Sprintf("%d. [%s] %s\n   Tags: %s\n   Path: %s\n   Snippet: %s\n\n",
					i+1, note.ID, note.Title, tagList, note.Path, snippet)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_all_tags - Get all unique tags with counts
	s.AddTool(
		mcp.NewTool("get_all_tags",
			mcp.WithDescription("Get all unique tags with usage counts. Useful for tag exploration and autocomplete."),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			tagCounts := ctx.store.GetAllTags()

			// Sort by count (descending)
			type tagCount struct {
				tag   string
				count int
			}
			tags := make([]tagCount, 0, len(tagCounts))
			for tag, count := range tagCounts {
				tags = append(tags, tagCount{tag, count})
			}
			sort.Slice(tags, func(i, j int) bool {
				return tags[i].count > tags[j].count
			})

			result := fmt.Sprintf("Total unique tags: %d\n\n", len(tags))
			for _, tc := range tags {
				result += fmt.Sprintf("- %s (%d notes)\n", tc.tag, tc.count)
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
			mcp.WithDescription("Update note content (wikilinks and tags will be automatically parsed)"),
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
			mcp.WithArray("tags",
				mcp.Description("Optional: update note tags (array of strings)"),
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

			// Validate content
			if content == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent("Error: content cannot be empty. Use replace_text or delete_text to remove specific parts."),
					},
				}, nil
			}

			// Update fields
			note.Content = content
			if title != "" {
				note.Title = title
			}

			// Update tags if provided
			if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
				ctx.logger.Info("MCP update_note: parsing tags", "argsMap", argsMap)
				if tagsVal, exists := argsMap["tags"]; exists && tagsVal != nil {
					ctx.logger.Info("MCP update_note: tags field exists", "tagsVal", tagsVal, "type", fmt.Sprintf("%T", tagsVal))
					if tagsRaw, ok := tagsVal.([]interface{}); ok {
						tags := make([]string, 0, len(tagsRaw))
						for _, t := range tagsRaw {
							if tagStr, ok := t.(string); ok {
								// Normalize tag (lowercase, trimmed)
								normalized := strings.ToLower(strings.TrimSpace(tagStr))
								if normalized != "" {
									tags = append(tags, normalized)
								}
							}
						}
						ctx.logger.Info("MCP update_note: parsed tags", "tags", tags)
						note.Tags = tags
					} else {
						ctx.logger.Warn("MCP update_note: tags is not []interface{}", "type", fmt.Sprintf("%T", tagsVal))
					}
				} else {
					ctx.logger.Info("MCP update_note: tags field not provided or nil")
				}
			} else {
				ctx.logger.Warn("MCP update_note: Arguments is not map[string]interface{}")
			}

			// Update note (wikilinks and tags will be automatically parsed)
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

			tagsInfo := ""
			if len(note.Tags) > 0 {
				tagsInfo = fmt.Sprintf("\nTags: %v", note.Tags)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note updated: %s\nWikilinks parsed: %d%s", note.Path, len(note.OutgoingLinks), tagsInfo)),
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

	// get_note_metadata - Get note metadata without content
	s.AddTool(
		mcp.NewTool("get_note_metadata",
			mcp.WithDescription("Get note metadata (title, folder, path, type, projectPath, tags, dates) without loading full content. Fast and token-efficient."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			result := fmt.Sprintf("# Metadata for: %s\n\n", note.Title)
			result += fmt.Sprintf("**ID:** `%s`\n", note.ID)
			result += fmt.Sprintf("**Path:** `%s`\n", note.Path)
			result += fmt.Sprintf("**Folder:** `%s`\n", note.Folder)

			if note.Type != "" {
				result += fmt.Sprintf("**Type:** `%s`\n", note.Type)
			}
			if note.ProjectPath != "" {
				result += fmt.Sprintf("**Project Path:** `%s`\n", note.ProjectPath)
			}

			if len(note.Tags) > 0 {
				result += fmt.Sprintf("**Tags:** %v\n", note.Tags)
			}

			result += fmt.Sprintf("**Created:** %s\n", note.CreatedAt.Format("2006-01-02 15:04"))
			result += fmt.Sprintf("**Updated:** %s\n", note.UpdatedAt.Format("2006-01-02 15:04"))

			result += fmt.Sprintf("\n**Connections:**\n")
			result += fmt.Sprintf("- Outgoing links: %d\n", len(note.OutgoingLinks))
			result += fmt.Sprintf("- Backlinks: %d\n", len(note.Backlinks))

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_note_wikilinks - Get outgoing wikilinks
	s.AddTool(
		mcp.NewTool("get_note_wikilinks",
			mcp.WithDescription("Get all outgoing wikilinks from a note (notes this note links to). Simpler alternative to get_note_connections."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			if len(note.OutgoingLinks) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Note '%s' has no outgoing wikilinks", note.Title)),
					},
				}, nil
			}

			result := fmt.Sprintf("Outgoing wikilinks from '%s' (%d):\n\n", note.Title, len(note.OutgoingLinks))
			for i, targetID := range note.OutgoingLinks {
				targetNote, err := ctx.store.GetNote(timeoutCtx, targetID)
				if err == nil {
					result += fmt.Sprintf("%d. **%s** (`%s`)\n", i+1, targetNote.Title, targetNote.Path)
				} else {
					result += fmt.Sprintf("%d. `%s` (not found)\n", i+1, targetID)
				}
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_note_backlinks - Get incoming backlinks
	s.AddTool(
		mcp.NewTool("get_note_backlinks",
			mcp.WithDescription("Get all backlinks to a note (notes that link to this note). Simpler alternative to get_note_connections."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			if len(note.Backlinks) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Note '%s' has no backlinks", note.Title)),
					},
				}, nil
			}

			result := fmt.Sprintf("Backlinks to '%s' (%d):\n\n", note.Title, len(note.Backlinks))
			for i, sourceID := range note.Backlinks {
				sourceNote, err := ctx.store.GetNote(timeoutCtx, sourceID)
				if err == nil {
					result += fmt.Sprintf("%d. **%s** (`%s`)\n", i+1, sourceNote.Title, sourceNote.Path)
				} else {
					result += fmt.Sprintf("%d. `%s` (not found)\n", i+1, sourceID)
				}
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// find_recent_notes - Find recently modified notes
	s.AddTool(
		mcp.NewTool("find_recent_notes",
			mcp.WithDescription("Find notes modified within the last N days. Fast: no content fetched."),
			mcp.WithNumber("days",
				mcp.Description("Days to look back (default: 7)"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max results (default: 20)"),
			),
			mcp.WithString("folder",
				mcp.Description("Limit to a specific folder (empty = all folders)"),
			),
			mcp.WithBoolean("recursive",
				mcp.Description("Include subfolders (default: true)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			days := int(request.GetFloat("days", 7))
			limit := int(request.GetFloat("limit", 20))
			folder := request.GetString("folder", "")
			recursive := request.GetString("recursive", "true") != "false"

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			notes, err := ctx.store.ListNotesMeta(timeoutCtx, folder, recursive)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			cutoff := time.Now().AddDate(0, 0, -days)
			var recentNotes []*models.Note
			for _, note := range notes {
				if note.UpdatedAt.After(cutoff) {
					recentNotes = append(recentNotes, note)
				}
			}

			sort.Slice(recentNotes, func(i, j int) bool {
				return recentNotes[i].UpdatedAt.After(recentNotes[j].UpdatedAt)
			})
			if len(recentNotes) > limit {
				recentNotes = recentNotes[:limit]
			}

			if len(recentNotes) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("No notes modified in the last %d days", days)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d notes modified in the last %d days:\n\n", len(recentNotes), days)
			for i, note := range recentNotes {
				timeAgo := time.Since(note.UpdatedAt)
				var timeStr string
				if timeAgo.Hours() < 24 {
					timeStr = fmt.Sprintf("%dh ago", int(timeAgo.Hours()))
				} else {
					timeStr = fmt.Sprintf("%dd ago", int(timeAgo.Hours()/24))
				}
				result += fmt.Sprintf("%d. [%s] %s (%s)\n", i+1, note.ID, note.Title, timeStr)
				result += fmt.Sprintf("   Path: %s | Updated: %s\n", note.Path, note.UpdatedAt.Format("2006-01-02 15:04"))
				if len(note.Tags) > 0 {
					result += fmt.Sprintf("   Tags: %v\n", note.Tags)
				}
				result += "\n"
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_note_tags - Get tags of a specific note
	s.AddTool(
		mcp.NewTool("get_note_tags",
			mcp.WithDescription("Get tags of a specific note without loading full content. Fast and token-efficient way to check note tags."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			tags := note.Tags
			if tags == nil || len(tags) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Note '%s' has no tags", note.Title)),
					},
				}, nil
			}

			result := fmt.Sprintf("Tags for '%s':\n", note.Title)
			for i, tag := range tags {
				result += fmt.Sprintf("%d. %s\n", i+1, tag)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// set_tags - Replace all tags on a note
	s.AddTool(
		mcp.NewTool("set_tags",
			mcp.WithDescription("Set tags on a note (replaces all existing tags). Tags are automatically normalized to lowercase."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithArray("tags",
				mcp.Required(),
				mcp.Description("Array of tag strings (e.g., [\"kubernetes\", \"networking\"])"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get existing note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			// Parse tags
			tags := []string{}
			if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if tagsVal, exists := argsMap["tags"]; exists && tagsVal != nil {
					if tagsRaw, ok := tagsVal.([]interface{}); ok {
						for _, t := range tagsRaw {
							if tagStr, ok := t.(string); ok {
								normalized := strings.ToLower(strings.TrimSpace(tagStr))
								if normalized != "" {
									tags = append(tags, normalized)
								}
							}
						}
					}
				}
			}

			// Set tags (replace existing)
			note.Tags = tags

			// Update note
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
					mcp.NewTextContent(fmt.Sprintf("Tags set on '%s': %v", note.Title, tags)),
				},
			}, nil
		},
	)

	// add_tags - Add tags to existing tags
	s.AddTool(
		mcp.NewTool("add_tags",
			mcp.WithDescription("Add tags to a note (merges with existing tags, no duplicates). Tags are automatically normalized to lowercase."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithArray("tags",
				mcp.Required(),
				mcp.Description("Array of tag strings to add (e.g., [\"kubernetes\", \"networking\"])"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get existing note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			// Parse new tags
			newTags := []string{}
			if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if tagsVal, exists := argsMap["tags"]; exists && tagsVal != nil {
					if tagsRaw, ok := tagsVal.([]interface{}); ok {
						for _, t := range tagsRaw {
							if tagStr, ok := t.(string); ok {
								normalized := strings.ToLower(strings.TrimSpace(tagStr))
								if normalized != "" {
									newTags = append(newTags, normalized)
								}
							}
						}
					}
				}
			}

			// Merge tags (avoid duplicates)
			existingTags := note.Tags
			if existingTags == nil {
				existingTags = []string{}
			}

			tagSet := make(map[string]bool)
			for _, tag := range existingTags {
				tagSet[tag] = true
			}

			addedCount := 0
			for _, tag := range newTags {
				if !tagSet[tag] {
					existingTags = append(existingTags, tag)
					tagSet[tag] = true
					addedCount++
				}
			}

			note.Tags = existingTags

			// Update note
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
					mcp.NewTextContent(fmt.Sprintf("Added %d tag(s) to '%s'. Total tags: %v", addedCount, note.Title, note.Tags)),
				},
			}, nil
		},
	)

	// remove_tags - Remove specific tags
	s.AddTool(
		mcp.NewTool("remove_tags",
			mcp.WithDescription("Remove specific tags from a note. Tags are automatically normalized to lowercase for matching."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithArray("tags",
				mcp.Required(),
				mcp.Description("Array of tag strings to remove (e.g., [\"kubernetes\", \"networking\"])"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get existing note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			// Parse tags to remove
			tagsToRemove := make(map[string]bool)
			if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if tagsVal, exists := argsMap["tags"]; exists && tagsVal != nil {
					if tagsRaw, ok := tagsVal.([]interface{}); ok {
						for _, t := range tagsRaw {
							if tagStr, ok := t.(string); ok {
								normalized := strings.ToLower(strings.TrimSpace(tagStr))
								if normalized != "" {
									tagsToRemove[normalized] = true
								}
							}
						}
					}
				}
			}

			// Filter out tags to remove
			existingTags := note.Tags
			if existingTags == nil {
				existingTags = []string{}
			}

			filteredTags := []string{}
			removedCount := 0
			for _, tag := range existingTags {
				if !tagsToRemove[tag] {
					filteredTags = append(filteredTags, tag)
				} else {
					removedCount++
				}
			}

			note.Tags = filteredTags

			// Update note
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
					mcp.NewTextContent(fmt.Sprintf("Removed %d tag(s) from '%s'. Remaining tags: %v", removedCount, note.Title, note.Tags)),
				},
			}, nil
		},
	)

	// set_note_type - Set note type
	s.AddTool(
		mcp.NewTool("set_note_type",
			mcp.WithDescription("Set note type. Use 'project' for project-related notes or empty string for regular notes."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("type",
				mcp.Required(),
				mcp.Description("Note type ('project' or empty string)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			noteType := request.GetString("type", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			oldType := note.Type
			note.Type = noteType

			// Update note
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

			typeDisplay := noteType
			if typeDisplay == "" {
				typeDisplay = "(none)"
			}

			oldTypeDisplay := oldType
			if oldTypeDisplay == "" {
				oldTypeDisplay = "(none)"
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Type updated for '%s': %s → %s", note.Title, oldTypeDisplay, typeDisplay)),
				},
			}, nil
		},
	)

	// set_project_path - Link note to local project
	s.AddTool(
		mcp.NewTool("set_project_path",
			mcp.WithDescription("Link note to a local project directory. This enables project-specific MCP tools when working with this note."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("project_path",
				mcp.Required(),
				mcp.Description("Local filesystem path to project directory (or empty string to unlink)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			projectPath := request.GetString("project_path", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			// Get note
			note, err := ctx.store.GetNote(timeoutCtx, noteID)
			if err != nil {
				note, err = ctx.store.GetNoteByPath(timeoutCtx, noteID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Note not found: %s", noteID)),
						},
					}, nil
				}
			}

			oldPath := note.ProjectPath
			note.ProjectPath = projectPath

			// Auto-set type to "project" if linking to a project
			if projectPath != "" && note.Type == "" {
				note.Type = "project"
			}

			// Update note
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

			if projectPath == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Project link removed from '%s'\nPrevious: %s", note.Title, oldPath)),
					},
				}, nil
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note '%s' linked to project:\nPath: %s\nType: %s", note.Title, projectPath, note.Type)),
				},
			}, nil
		},
	)

	// copy_note — duplicate a note with a new title (and optionally a different folder)
	s.AddTool(
		mcp.NewTool("copy_note",
			mcp.WithDescription("Duplicate a note with a new title. Copies content, tags, and type. New note is placed in the same folder unless target_folder is specified."),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Source note ID or path"),
			),
			mcp.WithString("new_title",
				mcp.Required(),
				mcp.Description("Title for the new copy"),
			),
			mcp.WithString("target_folder",
				mcp.Description("Destination folder (default: same as source)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id := request.GetString("id", "")
			newTitle := request.GetString("new_title", "")
			targetFolder := request.GetString("target_folder", "")

			if newTitle == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent("Error: new_title is required")},
				}, nil
			}

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			src, err := ctx.store.GetNote(timeoutCtx, id)
			if err != nil {
				src, err = ctx.store.GetNoteByPath(timeoutCtx, id)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Note not found: %s", id))},
					}, nil
				}
			}

			folder := src.Folder
			if targetFolder != "" {
				folder = targetFolder
			}

			slug := normalizeTitle(newTitle)
			if slug == "" {
				slug = uuid.New().String()[:8]
			}
			newPath := slug + ".md"
			if folder != "" {
				newPath = folder + "/" + newPath
			}

			newNote := &models.Note{
				ID:      uuid.New().String(),
				Title:   newTitle,
				Path:    newPath,
				Folder:  folder,
				Content: src.Content,
				Tags:    append([]string(nil), src.Tags...),
				Type:    src.Type,
			}

			if err := ctx.store.CreateNote(timeoutCtx, newNote); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Error creating copy: %v", err))},
				}, nil
			}

			ctx.eventBus.Publish(events.Event{Type: events.EventNoteCreated, Note: newNote})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Copied '%s' → '%s'\nNew ID: %s\nPath: %s", src.Title, newNote.Title, newNote.ID, newNote.Path)),
				},
			}, nil
		},
	)

	// bulk_tag_update — add/remove tags from notes matching a filter
	s.AddTool(
		mcp.NewTool("bulk_tag_update",
			mcp.WithDescription("Add and/or remove tags from multiple notes at once. Filter by folder, existing tag, or title substring."),
			mcp.WithArray("add_tags",
				mcp.Description("Tags to add to matching notes"),
			),
			mcp.WithArray("remove_tags",
				mcp.Description("Tags to remove from matching notes"),
			),
			mcp.WithString("filter_folder",
				mcp.Description("Only affect notes in this folder (empty = all)"),
			),
			mcp.WithBoolean("filter_folder_recursive",
				mcp.Description("Include subfolders when filter_folder is set (default: false)"),
			),
			mcp.WithString("filter_tag",
				mcp.Description("Only affect notes that already have this tag"),
			),
			mcp.WithString("filter_title",
				mcp.Description("Only affect notes whose title contains this substring (case-insensitive)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			var addTags, removeTags []string
			if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
				for _, pair := range [][2]interface{}{{"add_tags", &addTags}, {"remove_tags", &removeTags}} {
					key, dest := pair[0].(string), pair[1].(*[]string)
					if v, exists := argsMap[key]; exists && v != nil {
						if arr, ok := v.([]interface{}); ok {
							for _, t := range arr {
								if s, ok := t.(string); ok {
									if n := strings.ToLower(strings.TrimSpace(s)); n != "" {
										*dest = append(*dest, n)
									}
								}
							}
						}
					}
				}
			}

			if len(addTags) == 0 && len(removeTags) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent("Error: provide add_tags or remove_tags")},
				}, nil
			}

			filterFolder := request.GetString("filter_folder", "")
			filterFolderRecursive := request.GetString("filter_folder_recursive", "") == "true"
			filterTag := request.GetString("filter_tag", "")
			filterTitle := request.GetString("filter_title", "")

			filter := bson.M{}
			if filterFolder != "" {
				if filterFolderRecursive {
					filter["folder"] = bson.M{"$regex": "^" + filterFolder + "(/|$)"}
				} else {
					filter["folder"] = filterFolder
				}
			}
			if filterTag != "" {
				filter["tags"] = filterTag
			}
			if filterTitle != "" {
				filter["title"] = bson.M{"$regex": filterTitle, "$options": "i"}
			}

			modified, err := ctx.store.BulkUpdateTags(timeoutCtx, filter, addTags, removeTags)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Error: %v", err))},
				}, nil
			}

			var parts []string
			if len(addTags) > 0 {
				parts = append(parts, fmt.Sprintf("added %v", addTags))
			}
			if len(removeTags) > 0 {
				parts = append(parts, fmt.Sprintf("removed %v", removeTags))
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Updated %d notes: %s", modified, strings.Join(parts, ", "))),
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

// ensureFoldersExist creates all intermediate folders if they don't exist
// For example: "Projects/Aenix/Cozystack/Architecture" will create:
// - Projects
// - Projects/Aenix
// - Projects/Aenix/Cozystack
// - Projects/Aenix/Cozystack/Architecture
func ensureFoldersExist(ctx context.Context, store *storage.MongoStorage, folderPath string) error {
	if folderPath == "" {
		return nil
	}

	parts := splitPath(folderPath)
	currentPath := ""

	for i, part := range parts {
		if i > 0 {
			currentPath += "/"
		}
		currentPath += part

		// Check if folder exists
		folders, err := store.ListFolders(ctx)
		if err != nil {
			return fmt.Errorf("failed to list folders: %w", err)
		}

		exists := false
		for _, f := range folders {
			if f.Path == currentPath {
				exists = true
				break
			}
		}

		// Create folder if it doesn't exist
		if !exists {
			folder := &models.Folder{Path: currentPath}
			if err := store.CreateFolder(ctx, folder); err != nil {
				// Ignore duplicate key errors (race condition)
				if !contains(err.Error(), "already exists") {
					return fmt.Errorf("failed to create folder %s: %w", currentPath, err)
				}
			}
		}
	}

	return nil
}

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, r := range path {
		if r == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func registerProjectTools(s *server.MCPServer, ctx *MCPContext) {
	// get_current_project - определяет текущий проект из переменных окружения
	s.AddTool(
		mcp.NewTool("get_current_project",
			mcp.WithDescription("Get information about the current project based on working directory. Returns project name and suggested folder path for notes."),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Попытка получить текущую директорию из переменных окружения
			// Claude Code обычно устанавливает PWD или можно использовать другие методы
			cwd := os.Getenv("PWD")
			if cwd == "" {
				cwd = os.Getenv("OLDPWD")
			}
			if cwd == "" {
				// Fallback - попробовать получить через os.Getwd()
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					cwd = ""
				}
			}

			var projectName string
			var projectPath string
			var noteFolder string

			if cwd != "" {
				// Извлекаем имя проекта из последнего сегмента пути
				parts := splitPath(cwd)
				if len(parts) > 0 {
					projectName = parts[len(parts)-1]
					projectPath = cwd
					noteFolder = fmt.Sprintf("Projects/%s", projectName)
				}
			}

			if projectName == "" {
				// Нет информации о проекте - используем General
				projectName = "General"
				projectPath = ""
				noteFolder = "General"
			}

			result := fmt.Sprintf(`Current project information:
- Project name: %s
- Project path: %s
- Suggested note folder: %s

Use this folder path when creating notes related to this project.
Example: create_note("path": "%s/architecture.md", "content": "...")`,
				projectName, projectPath, noteFolder, noteFolder)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)
}

func registerSessionTools(s *server.MCPServer, mcpCtx *MCPContext) {
	// list_sessions — list sessions sorted by last activity
	s.AddTool(
		mcp.NewTool("list_sessions",
			mcp.WithDescription("List Claude chat sessions sorted by last activity. Returns metadata without message bodies."),
			mcp.WithNumber("limit", mcp.Description("Maximum number of sessions to return (default 30)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			limit := 30
			if argsMap, ok := req.Params.Arguments.(map[string]interface{}); ok {
				if v, ok := argsMap["limit"].(float64); ok && v > 0 {
					limit = int(v)
				}
			}
			sessions, err := mcpCtx.sessionStorage.ListAllSessions(ctx, limit)
			if err != nil {
				return nil, fmt.Errorf("failed to list sessions: %w", err)
			}
			if len(sessions) == 0 {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("No sessions found")}}, nil
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Found %d sessions:\n\n", len(sessions))
			for _, sess := range sessions {
				msgCount := len(sess.Messages)
				fmt.Fprintf(&sb, "- **%s** (id: `%s`)\n  status: %s | messages: %d | last active: %s\n",
					sess.Name, sess.ID, sess.Status, msgCount, sess.LastActivity.Format("2006-01-02 15:04"))
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sb.String())}}, nil
		},
	)

	// get_session_history — read last N messages from a session
	s.AddTool(
		mcp.NewTool("get_session_history",
			mcp.WithDescription("Get message history for a specific session. Returns messages in chronological order."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Session ID")),
			mcp.WithNumber("limit", mcp.Description("Number of most recent messages to return (default 50)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID, _ := argsMap["session_id"].(string)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id is required")}}, nil
			}
			limit := 50
			if v, ok := argsMap["limit"].(float64); ok && v > 0 {
				limit = int(v)
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
			if sess == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session not found: " + sessionID)}}, nil
			}
			msgs := sess.Messages
			if len(msgs) > limit {
				msgs = msgs[len(msgs)-limit:]
			}
			if len(msgs) == 0 {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Session '%s' has no messages", sess.Name))}}, nil
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Session: **%s** (%d messages shown of %d total)\n\n", sess.Name, len(msgs), len(sess.Messages))
			for _, m := range msgs {
				content := m.Content
				if len(content) > 500 {
					content = content[:500] + "... [truncated]"
				}
				fmt.Fprintf(&sb, "**[%s]** %s\n%s\n\n", m.Role, m.Timestamp.Format("15:04 02 Jan"), content)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sb.String())}}, nil
		},
	)

	// clear_session_history — remove all messages from a session
	s.AddTool(
		mcp.NewTool("clear_session_history",
			mcp.WithDescription("Clear all messages from a session. The session metadata (name, status) is preserved."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Session ID to clear")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID, _ := argsMap["session_id"].(string)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id is required")}}, nil
			}
			if err := mcpCtx.sessionStorage.ClearSessionMessages(ctx, sessionID); err != nil {
				return nil, fmt.Errorf("failed to clear session: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session history cleared: " + sessionID)}}, nil
		},
	)

	// search_session_history — find sessions containing messages matching a query
	s.AddTool(
		mcp.NewTool("search_session_history",
			mcp.WithDescription("Search for sessions containing messages that match a query string (case-insensitive). Returns session metadata; use get_session_history to read full messages."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive substring match)")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of sessions to return (default 20)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			query, _ := argsMap["query"].(string)
			if query == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: query is required")}}, nil
			}
			limit := 20
			if v, ok := argsMap["limit"].(float64); ok && v > 0 {
				limit = int(v)
			}
			sessions, err := mcpCtx.sessionStorage.SearchSessionMessages(ctx, query, limit)
			if err != nil {
				return nil, fmt.Errorf("failed to search sessions: %w", err)
			}
			if len(sessions) == 0 {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("No sessions found matching: " + query)}}, nil
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Found %d sessions matching '%s':\n\n", len(sessions), query)
			for _, sess := range sessions {
				fmt.Fprintf(&sb, "- **%s** (id: `%s`) — last active: %s\n",
					sess.Name, sess.ID, sess.LastActivity.Format("2006-01-02 15:04"))
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sb.String())}}, nil
		},
	)

	// search_session_messages — grep with surrounding context
	s.AddTool(
		mcp.NewTool("search_session_messages",
			mcp.WithDescription("Search messages across all sessions and return matching messages with surrounding context. Shows which session, message index, role, timestamp, and N messages before/after each match."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive substring match)")),
			mcp.WithNumber("context_before", mcp.Description("Number of messages before each match to include (default 2)")),
			mcp.WithNumber("context_after", mcp.Description("Number of messages after each match to include (default 2)")),
			mcp.WithNumber("max_sessions", mcp.Description("Maximum number of sessions to search through (default 10)")),
			mcp.WithNumber("max_matches", mcp.Description("Maximum total matches to return across all sessions (default 20)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			query, _ := argsMap["query"].(string)
			if query == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: query is required")}}, nil
			}
			ctxBefore := 2
			if v, ok := argsMap["context_before"].(float64); ok && v >= 0 {
				ctxBefore = int(v)
			}
			ctxAfter := 2
			if v, ok := argsMap["context_after"].(float64); ok && v >= 0 {
				ctxAfter = int(v)
			}
			maxSessions := 10
			if v, ok := argsMap["max_sessions"].(float64); ok && v > 0 {
				maxSessions = int(v)
			}
			maxMatches := 20
			if v, ok := argsMap["max_matches"].(float64); ok && v > 0 {
				maxMatches = int(v)
			}

			sessions, err := mcpCtx.sessionStorage.SearchSessionMessagesWithContent(ctx, query, maxSessions)
			if err != nil {
				return nil, fmt.Errorf("failed to search sessions: %w", err)
			}
			if len(sessions) == 0 {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("No messages found matching: " + query)}}, nil
			}

			queryLower := strings.ToLower(query)
			var sb strings.Builder
			totalMatches := 0

			for _, sess := range sessions {
				if totalMatches >= maxMatches {
					break
				}
				msgs := sess.Messages
				sessHeader := false

				for i, msg := range msgs {
					if totalMatches >= maxMatches {
						break
					}
					if !strings.Contains(strings.ToLower(msg.Content), queryLower) {
						continue
					}

					if !sessHeader {
						fmt.Fprintf(&sb, "## Session: %s (id: `%s`)\n\n", sess.Name, sess.ID)
						sessHeader = true
					}

					start := i - ctxBefore
					if start < 0 {
						start = 0
					}
					end := i + ctxAfter + 1
					if end > len(msgs) {
						end = len(msgs)
					}

					fmt.Fprintf(&sb, "### Match at message %d/%d\n", i+1, len(msgs))
					for j := start; j < end; j++ {
						m := msgs[j]
						marker := "  "
						if j == i {
							marker := "**"
							content := m.Content
							if len(content) > 600 {
								content = content[:600] + "... [truncated]"
							}
							fmt.Fprintf(&sb, "%s[%s]** %s\n%s\n\n", marker, m.Role, m.Timestamp.Format("15:04 02 Jan"), content)
							continue
						}
						_ = marker
						content := m.Content
						if len(content) > 200 {
							content = content[:200] + "... [truncated]"
						}
						fmt.Fprintf(&sb, "  [%s] %s\n%s\n\n", m.Role, m.Timestamp.Format("15:04 02 Jan"), content)
					}
					fmt.Fprintln(&sb, "---")
					totalMatches++
				}
			}

			if totalMatches == 0 {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("No messages found matching: " + query)}}, nil
			}
			header := fmt.Sprintf("Found %d match(es) across %d session(s) for '%s':\n\n", totalMatches, len(sessions), query)
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(header + sb.String())}}, nil
		},
	)

	// get_my_session_id — returns the session ID of the calling Claude instance
	s.AddTool(
		mcp.NewTool("get_my_session_id",
			mcp.WithDescription("Returns the session ID and metadata of the current Claude session. Use this to identify yourself, rename your session, or access your own history."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sessionID := sessionIDFromCtx(ctx)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("GRIMOIRE_SESSION_ID not set — running outside a managed session")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("session_id: " + sessionID)}}, nil
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil || sess == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("session_id: " + sessionID + "\n(session not yet persisted)")}}, nil
			}
			result := fmt.Sprintf("session_id: %s\nname: %s\nstatus: %s\nworking_dir: %s\nlast_active: %s\nmessages: %d",
				sess.ID, sess.Name, sess.Status, sess.WorkingDir,
				sess.LastActivity.Format("2006-01-02 15:04:05"),
				len(sess.Messages),
			)
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(result)}}, nil
		},
	)

	// rename_my_session — lets Claude rename its own session
	s.AddTool(
		mcp.NewTool("rename_my_session",
			mcp.WithDescription("Rename the current session. Pass session_id explicitly if GRIMOIRE_SESSION_ID is not auto-detected (get it via: echo $GRIMOIRE_SESSION_ID)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("New name for the session (short, descriptive)")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			name, _ := argsMap["name"].(string)
			if name == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: name is required")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			if err := mcpCtx.sessionStorage.UpdateSessionName(ctx, sessionID, name); err != nil {
				return nil, fmt.Errorf("failed to rename session: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session renamed to: " + name)}}, nil
		},
	)

	// get_session_notes — read notes from any session (by ID) or own session
	s.AddTool(
		mcp.NewTool("get_session_notes",
			mcp.WithDescription("Read the persistent notes attached to a session. Pass session_id explicitly if auto-detection fails (get it via: echo $GRIMOIRE_SESSION_ID)."),
			mcp.WithString("session_id", mcp.Description("Session ID (omit to use own session)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id required. Pass it explicitly or run inside a managed session.")}}, nil
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
			if sess == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session not found: " + sessionID)}}, nil
			}
			if sess.Notes == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("(no notes for session " + sessionID + ")")}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sess.Notes)}}, nil
		},
	)

	// set_session_notes — replace all notes in the current session
	s.AddTool(
		mcp.NewTool("set_session_notes",
			mcp.WithDescription("Replace the entire notes field of a session. Pass session_id explicitly if auto-detection fails (get it via: echo $GRIMOIRE_SESSION_ID)."),
			mcp.WithString("notes", mcp.Required(), mcp.Description("New notes content (replaces existing)")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			notes, _ := argsMap["notes"].(string)
			if err := mcpCtx.sessionStorage.UpdateSessionNotes(ctx, sessionID, notes); err != nil {
				return nil, fmt.Errorf("failed to set notes: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session notes updated")}}, nil
		},
	)

	// append_to_session_notes — append text to current session notes
	s.AddTool(
		mcp.NewTool("append_to_session_notes",
			mcp.WithDescription("Append text to the end of session notes. Pass session_id explicitly if auto-detection fails (get it via: echo $GRIMOIRE_SESSION_ID)."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to append")),
			mcp.WithString("separator", mcp.Description("Separator between old and new content (default: newline)")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			text, _ := argsMap["text"].(string)
			if text == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: text is required")}}, nil
			}
			separator := "\n"
			if sep, ok := argsMap["separator"].(string); ok && sep != "" {
				separator = sep
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
			var newNotes string
			if sess != nil && sess.Notes != "" {
				newNotes = sess.Notes + separator + text
			} else {
				newNotes = text
			}
			if err := mcpCtx.sessionStorage.UpdateSessionNotes(ctx, sessionID, newNotes); err != nil {
				return nil, fmt.Errorf("failed to append notes: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Appended to session notes")}}, nil
		},
	)

	// prepend_to_session_notes — prepend text to current session notes
	s.AddTool(
		mcp.NewTool("prepend_to_session_notes",
			mcp.WithDescription("Prepend text to the beginning of session notes. Pass session_id explicitly if auto-detection fails (get it via: echo $GRIMOIRE_SESSION_ID)."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to prepend")),
			mcp.WithString("separator", mcp.Description("Separator between new and old content (default: newline)")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			text, _ := argsMap["text"].(string)
			if text == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: text is required")}}, nil
			}
			separator := "\n"
			if sep, ok := argsMap["separator"].(string); ok && sep != "" {
				separator = sep
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
			var newNotes string
			if sess != nil && sess.Notes != "" {
				newNotes = text + separator + sess.Notes
			} else {
				newNotes = text
			}
			if err := mcpCtx.sessionStorage.UpdateSessionNotes(ctx, sessionID, newNotes); err != nil {
				return nil, fmt.Errorf("failed to prepend notes: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Prepended to session notes")}}, nil
		},
	)

	// replace_in_session_notes — find and replace text within session notes
	s.AddTool(
		mcp.NewTool("replace_in_session_notes",
			mcp.WithDescription("Find and replace a substring in session notes. Pass session_id explicitly if auto-detection fails."),
			mcp.WithString("old_text", mcp.Required(), mcp.Description("Text to find (must match exactly)")),
			mcp.WithString("new_text", mcp.Required(), mcp.Description("Replacement text")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			oldText, _ := argsMap["old_text"].(string)
			newText, _ := argsMap["new_text"].(string)
			if oldText == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: old_text is required")}}, nil
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
			if sess == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session not found")}}, nil
			}
			if !strings.Contains(sess.Notes, oldText) {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: old_text not found in session notes")}}, nil
			}
			updated := strings.Replace(sess.Notes, oldText, newText, 1)
			if err := mcpCtx.sessionStorage.UpdateSessionNotes(ctx, sessionID, updated); err != nil {
				return nil, fmt.Errorf("failed to update notes: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session notes updated")}}, nil
		},
	)

	// delete_from_session_notes — remove a specific substring from session notes
	s.AddTool(
		mcp.NewTool("delete_from_session_notes",
			mcp.WithDescription("Remove a specific substring from session notes. Pass session_id explicitly if auto-detection fails."),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to remove (must match exactly)")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			text, _ := argsMap["text"].(string)
			if text == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: text is required")}}, nil
			}
			sess, err := mcpCtx.sessionStorage.GetSession(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
			if sess == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session not found")}}, nil
			}
			if !strings.Contains(sess.Notes, text) {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: text not found in session notes")}}, nil
			}
			updated := strings.Replace(sess.Notes, text, "", 1)
			if err := mcpCtx.sessionStorage.UpdateSessionNotes(ctx, sessionID, updated); err != nil {
				return nil, fmt.Errorf("failed to update notes: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Text removed from session notes")}}, nil
		},
	)

	// clear_session_notes — wipe all notes from the current session
	s.AddTool(
		mcp.NewTool("clear_session_notes",
			mcp.WithDescription("Remove all notes from a session. Pass session_id explicitly if auto-detection fails."),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			if err := mcpCtx.sessionStorage.UpdateSessionNotes(ctx, sessionID, ""); err != nil {
				return nil, fmt.Errorf("failed to clear notes: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session notes cleared")}}, nil
		},
	)

	// append_to_session_history — write a message into the current session's message history
	s.AddTool(
		mcp.NewTool("append_to_session_history",
			mcp.WithDescription("Append a message to a session's conversation history. Pass session_id explicitly if auto-detection fails (get it via: echo $GRIMOIRE_SESSION_ID)."),
			mcp.WithString("role", mcp.Required(), mcp.Description("Message role: 'user' or 'assistant'")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Message content to record")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			role, _ := argsMap["role"].(string)
			content, _ := argsMap["content"].(string)
			if role != "user" && role != "assistant" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: role must be 'user' or 'assistant'")}}, nil
			}
			if content == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: content is required")}}, nil
			}
			msg := models.ClaudeMessage{
				Role:      role,
				Content:   content,
				Timestamp: time.Now(),
			}
			if err := mcpCtx.sessionStorage.AppendSessionMessage(ctx, sessionID, msg); err != nil {
				return nil, fmt.Errorf("failed to append message: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Message appended to session history")}}, nil
		},
	)
}
