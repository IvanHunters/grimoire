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
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/compact"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
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

// daemonGenUUIDForMCP returns a UUID-shaped string for fresh
// MCP-spawned sessions. We use uuid.New() to keep it simple — same
// helper the rest of the codebase uses.
func daemonGenUUIDForMCP() string {
	return uuid.New().String()
}

type MCPContext struct {
	store          *storage.MongoStorage
	sessionStorage *storage.SessionStorage
	logger         *slog.Logger
	eventBus       *events.EventBus
	config         *config.Config
	skills         *skills.Syncer
	skillSettings  *skills.SettingsStore
	// sessionManager is the live SessionManager singleton, set when
	// MCP runs in-process alongside the HTTP server (serve mode).
	// start_session uses this to register the dispatched session in
	// manager.sessions so the sidebar sees it immediately AND so
	// later WS init for that id hits the cached entry instead of
	// fresh-spawning another worker with the wrong cwd.
	sessionManager *claude.SessionManager
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server for Claude Code integration",
	Long:  `Starts Model Context Protocol server using stdio transport for Claude Code`,
	RunE:  runMCP,
}

// CreateMCPServer создаёт и настраивает MCP сервер
func CreateMCPServer(store *storage.MongoStorage, sessionStorage *storage.SessionStorage, logger *slog.Logger, cfg *config.Config) *server.MCPServer {
	return CreateMCPServerWithSkills(store, sessionStorage, logger, cfg, nil, nil, nil)
}

// CreateMCPServerWithSkills creates the MCP server with an optional skills
// syncer + settings store so skill tools can be registered. The optional
// sessionManager lets start_session register dispatched sessions in the
// live manager so they appear in the sidebar immediately and so later
// WS-init hits the cached entry rather than fresh-spawning a duplicate.
func CreateMCPServerWithSkills(
	store *storage.MongoStorage,
	sessionStorage *storage.SessionStorage,
	logger *slog.Logger,
	cfg *config.Config,
	skillsSyncer *skills.Syncer,
	skillsSettings *skills.SettingsStore,
	sessionManager *claude.SessionManager,
) *server.MCPServer {
	mcpCtx := &MCPContext{
		store:          store,
		sessionStorage: sessionStorage,
		logger:         logger,
		eventBus:       events.GetEventBus(),
		config:         cfg,
		skills:         skillsSyncer,
		skillSettings:  skillsSettings,
		sessionManager: sessionManager,
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

// registerSessionTools registers Claude-facing MCP tools for managing
// daemon-backed sessions. All legacy Mongo-message-history and session-notes
// tools were removed — they were unused by the UI after the daemon
// migration. What remains: self-identification, search across daemon
// + JSONL transcripts, spawn, rename, delete.
func registerSessionTools(s *server.MCPServer, mcpCtx *MCPContext) {
	// get_my_session_id — let claude identify itself inside grimoire.
	s.AddTool(
		mcp.NewTool("get_my_session_id",
			mcp.WithDescription("Returns the session ID of the current Claude session. Use this to identify yourself before calling rename_my_session or other self-targeting tools."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sessionID := sessionIDFromCtx(ctx)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("GRIMOIRE_SESSION_ID not set — running outside a managed session")}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("session_id: " + sessionID)}}, nil
		},
	)

	// rename_my_session — convenience: rename own session without
	// having to pass session_id (auto-detected from env/context).
	s.AddTool(
		mcp.NewTool("rename_my_session",
			mcp.WithDescription("Rename the current session. Pass session_id explicitly if GRIMOIRE_SESSION_ID is not auto-detected."),
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
			if err := mcpCtx.sessionStorage.UpsertSessionName(ctx, sessionID, name); err != nil {
				return nil, fmt.Errorf("failed to rename session: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session renamed to: " + name)}}, nil
		},
	)

	// search_sessions — combined name + JSONL content search. Each
	// session hit includes up to 5 surrounding-message snippets so
	// claude can confirm the match in context.
	s.AddTool(
		mcp.NewTool("search_sessions",
			mcp.WithDescription("Search Claude sessions by name OR by conversation content. Returns matches grouped by session, with snippet context around each content hit. Use this to locate a session before renaming/deleting/attaching."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Substring to match. Case-insensitive.")),
			mcp.WithString("cwd", mcp.Description("Optional working-directory filter — scopes to one project")),
			mcp.WithNumber("limit", mcp.Description("Max results per mode (default 20)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			query, _ := argsMap["query"].(string)
			cwd, _ := argsMap["cwd"].(string)
			if query == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: query is required")}}, nil
			}
			limit := 20
			if v, ok := argsMap["limit"].(float64); ok && v > 0 {
				limit = int(v)
			}

			items, err := claude.ListSessionsByCwd(cwd)
			if err != nil {
				return nil, fmt.Errorf("list sessions: %w", err)
			}
			lcq := strings.ToLower(query)
			var nameHits []claude.SessionListItem
			for _, it := range items {
				if strings.Contains(strings.ToLower(it.Name), lcq) ||
					strings.Contains(strings.ToLower(it.FirstPrompt), lcq) ||
					strings.Contains(strings.ToLower(it.Cwd), lcq) ||
					strings.Contains(strings.ToLower(it.SessionID), lcq) {
					nameHits = append(nameHits, it)
					if len(nameHits) >= limit {
						break
					}
				}
			}

			contentHits, err := discovery.Search(ctx, query, cwd, limit*3)
			if err != nil {
				mcpCtx.logger.Warn("transcript content search failed", slog.Any("error", err))
			}

			if len(nameHits) == 0 && len(contentHits) == 0 {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("No sessions match '" + query + "'")}}, nil
			}
			var sb strings.Builder
			// Quick lookup of SessionListItem by ID so content-hit rows
			// can show size/last-activity that discovery.SearchHit
			// doesn't carry on its own.
			byID := make(map[string]claude.SessionListItem, len(items))
			for _, it := range items {
				byID[it.SessionID] = it
			}
			if len(nameHits) > 0 {
				fmt.Fprintf(&sb, "## Matches by name/path (%d)\n\n", len(nameHits))
				for _, it := range nameHits {
					live := "historical"
					if it.Live != nil {
						live = "LIVE"
					}
					fmt.Fprintf(&sb, "- **%s** `%s`\n  cwd: `%s` · %s · %s · last: %s\n",
						it.Name, it.SessionID, it.Cwd, live, humanBytes(it.SizeBytes),
						it.LastActivity.Format("2006-01-02 15:04"))
				}
				sb.WriteString("\n")
			}
			if len(contentHits) > 0 {
				bySession := map[string][]discovery.SearchHit{}
				order := []string{}
				for _, h := range contentHits {
					if _, ok := bySession[h.SessionID]; !ok {
						order = append(order, h.SessionID)
					}
					bySession[h.SessionID] = append(bySession[h.SessionID], h)
				}
				fmt.Fprintf(&sb, "## Matches in conversation (%d sessions)\n\n", len(order))
				for _, id := range order {
					hs := bySession[id]
					first := hs[0]
					sizeStr := ""
					lastStr := ""
					if meta, ok := byID[id]; ok {
						sizeStr = " · " + humanBytes(meta.SizeBytes)
						lastStr = " · last: " + meta.LastActivity.Format("2006-01-02 15:04")
					}
					fmt.Fprintf(&sb, "- **%s** `%s` · cwd: `%s` · %d hits%s%s\n", first.SessionName, id, first.Cwd, len(hs), sizeStr, lastStr)
					perSessionShown := 5
					for i, h := range hs {
						if i >= perSessionShown {
							fmt.Fprintf(&sb, "  + %d more in this session\n", len(hs)-perSessionShown)
							break
						}
						snippet := h.Snippet
						if len(snippet) > 250 {
							snippet = snippet[:250] + "…"
						}
						fmt.Fprintf(&sb, "  · [%s line %d] %s\n", h.Role, h.LineNumber, snippet)
					}
				}
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sb.String())}}, nil
		},
	)

	// start_session — three modes, picked by which args are set:
	//   1. NEW (default): fresh `claude --bg` in `cwd`.
	//   2. RESUME: continue an existing session by passing `resume_from`
	//      — claude --bg --resume <uuid>. Cwd is inherited from the
	//      JSONL header; can be omitted.
	//   3. FORK: like resume but with --fork-session — creates a copy
	//      that branches off; original session stays untouched.
	s.AddTool(
		mcp.NewTool("start_session",
			mcp.WithDescription("Launch a background Claude session. Three modes:\n  - new: omit resume_from to spawn a fresh `claude --bg` in `cwd`.\n  - resume: pass `resume_from=<uuid>` to continue an existing transcript.\n  - fork: pass `resume_from=<uuid>` + `fork=true` to branch off a copy (original untouched).\nThe session appears in grimoire's sidebar within ~3 seconds."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Human-readable name for the session")),
			mcp.WithString("cwd", mcp.Description("Absolute working directory. Required for new mode; for resume/fork it's looked up from the source transcript when empty.")),
			mcp.WithString("prompt", mcp.Description("Optional initial user message (new mode only; ignored for resume/fork)")),
			mcp.WithString("resume_from", mcp.Description("UUID of an existing session to continue or fork. Find one via search_sessions.")),
			mcp.WithBoolean("fork", mcp.Description("When true alongside resume_from, --fork-session is added so the new session is a branch with its own UUID; the original chat stays untouched.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			name, _ := argsMap["name"].(string)
			cwd, _ := argsMap["cwd"].(string)
			prompt, _ := argsMap["prompt"].(string)
			resumeFrom, _ := argsMap["resume_from"].(string)
			fork, _ := argsMap["fork"].(bool)
			if name == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: name is required")}}, nil
			}

			// Resolve cwd for resume/fork from the source JSONL header
			// when caller didn't supply one. `claude --resume` locates
			// the transcript via (cwd, uuid) — must match.
			if resumeFrom != "" {
				if cwd == "" {
					if path, err := discovery.SessionPath(resumeFrom); err == nil {
						if tr, err := discovery.ReadTranscript(path); err == nil && tr.Header.Cwd != "" {
							cwd = tr.Header.Cwd
						}
					}
				}
				if cwd == "" {
					return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: cwd required (could not derive from resume_from transcript)")}}, nil
				}
			} else if cwd == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: cwd is required for new sessions")}}, nil
			}

			// Route through SessionManager so the dispatched session
			// is registered in m.sessions with correct DaemonUUID +
			// WorkingDir. Without this, MCP-spawned sessions appear in
			// the sidebar but clicking them fresh-spawns a NEW worker
			// (because handleInit's manager.GetOrCreate misses → falls
			// to startDaemonSession → no JSONL for the daemon-assigned
			// UUID → wrong cwd).
			mode := "new"
			var (
				disp    daemon.DispatchResult
				sess    *claude.ClaudeSession
				usedMgr bool
			)
			if mcpCtx.sessionManager != nil {
				usedMgr = true
				// Use grimoire-side ID = daemon-assigned UUID for new
				// sessions; for resume we still let the manager assign
				// its own grimoire id (it'll equal the resume target).
				if resumeFrom != "" {
					if fork {
						mode = "fork"
					} else {
						mode = "resume"
					}
					grimoireID := resumeFrom
					var mgrErr error
					sess, mgrErr = mcpCtx.sessionManager.GetOrResume(grimoireID, resumeFrom, cwd, name, fork)
					if mgrErr != nil {
						return nil, fmt.Errorf("manager resume: %w", mgrErr)
					}
				} else {
					mode = "new"
					grimoireID := daemonGenUUIDForMCP() // helper below
					var mgrErr error
					sess, mgrErr = mcpCtx.sessionManager.GetOrCreate(grimoireID, false, cwd, name, prompt)
					if mgrErr != nil {
						return nil, fmt.Errorf("manager create: %w", mgrErr)
					}
				}
				// Fill in disp so the reply is consistent with the
				// pre-manager dispatch shape.
				disp.SessionID = sess.ID
				disp.Short = sess.DaemonShort
				disp.Via = "manager"
			} else {
				// Fallback: bare daemon.Dispatch (no sessionManager
				// available — e.g. MCP running standalone outside the
				// HTTP server).
				client := &daemon.Client{Logger: mcpCtx.logger}
				opts := daemon.DispatchOpts{Cwd: cwd, Name: name}
				if resumeFrom != "" {
					opts.ResumeSessionID = resumeFrom
					opts.Fork = fork
					if fork {
						mode = "fork"
					} else {
						mode = "resume"
					}
				} else {
					opts.Prompt = prompt
				}
				var dispErr error
				disp, dispErr = client.Dispatch(opts)
				if dispErr != nil {
					return nil, fmt.Errorf("daemon dispatch: %w", dispErr)
				}
			}
			// Persist user-given name to overlay so it shows in the
			// sidebar instead of the daemon's structured token.
			if mcpCtx.sessionStorage != nil && !strings.HasPrefix(name, "grimoire-") {
				_ = mcpCtx.sessionStorage.UpsertSessionName(ctx, disp.SessionID, name)
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Session started (%s).\n  name: %s\n  cwd: %s\n  uuid: %s\n  short: %s\n  via: %s\n",
				mode, name, cwd, disp.SessionID, disp.Short, disp.Via)
			if resumeFrom != "" {
				fmt.Fprintf(&sb, "  resume_from: %s\n", resumeFrom)
			}
			if !usedMgr {
				sb.WriteString("\n[warn] session manager not available — entry will not appear in sidebar until WS init")
			}
			sb.WriteString("\nWill appear in sidebar within a few seconds.")
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sb.String())}}, nil
		},
	)

	// rename_session — rename ANY session by UUID.
	s.AddTool(
		mcp.NewTool("rename_session",
			mcp.WithDescription("Rename any session by ID. The name shows in grimoire's sidebar and Sessions Modal."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Full session UUID (find via search_sessions)")),
			mcp.WithString("name", mcp.Required(), mcp.Description("New display name")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID, _ := argsMap["session_id"].(string)
			name, _ := argsMap["name"].(string)
			if sessionID == "" || name == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id and name are required")}}, nil
			}
			if mcpCtx.sessionStorage == nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session storage not available")}}, nil
			}
			if err := mcpCtx.sessionStorage.UpsertSessionName(ctx, sessionID, name); err != nil {
				return nil, fmt.Errorf("rename: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Session renamed to: " + name)}}, nil
		},
	)

	// kill_session — stop the live daemon worker WITHOUT touching the
	// transcript. This is the safe "stop" (like Claude Code's Ctrl+X):
	// the JSONL history stays on disk and the session remains fully
	// resumable via `claude --resume <id>`. Use this to free a stuck or
	// runaway worker; use delete_session only when you actually want the
	// session gone.
	s.AddTool(
		mcp.NewTool("kill_session",
			mcp.WithDescription("Stop a session's live worker process WITHOUT deleting anything. The JSONL transcript stays on disk and the session remains resumable. This is the safe 'stop' — use it to kill a stuck/runaway worker. To actually remove a session, use delete_session instead."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Full session UUID whose worker to stop")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID, _ := argsMap["session_id"].(string)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id is required")}}, nil
			}

			killed, matchedShort := killLiveWorker(mcpCtx, sessionID)

			// Only touch Mongo when we actually killed something. Marking a
			// historical (never-live) session "terminated" would be a lie.
			// We KEEP the record — mark terminated, don't delete it — so the
			// sidebar shows a stopped-but-resumable session. (The manager
			// path already does this for manager-tracked sessions; this
			// covers the direct-daemon path.)
			if killed && mcpCtx.sessionStorage != nil {
				if err := mcpCtx.sessionStorage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
					mcpCtx.logger.Warn("kill_session: update status (non-fatal)",
						slog.String("session_id", sessionID), slog.Any("error", err))
				}
			}

			if !killed {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(
					fmt.Sprintf("No live worker running for %s — nothing to kill (already stopped / historical). The transcript is intact and resumable. To remove it from the session list, use delete_session (moves it to recoverable trash).", sessionID))}}, nil
			}
			workerLabel := "worker"
			if matchedShort != "" {
				workerLabel = "worker " + matchedShort
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(
				fmt.Sprintf("Killed %s for session %s\n  transcript preserved: yes (resume with `claude --resume %s`)", workerLabel, sessionID, sessionID))}}, nil
		},
	)

	// delete_session — kill daemon worker + MOVE the JSONL transcript
	// (and its sidecars) into the recoverable .md-editor-trash. This is
	// NOT a destructive os.Remove: an accidental delete can be restored
	// by moving the trash folder's contents back into the project dir.
	s.AddTool(
		mcp.NewTool("delete_session",
			mcp.WithDescription("Delete a session. Kills the daemon worker if live AND moves the JSONL transcript (with its subagent/archive/ledger sidecars) into a recoverable trash dir — it is NOT permanently destroyed. Use search_sessions to confirm the UUID first."),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("Full session UUID to delete")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID, _ := argsMap["session_id"].(string)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id is required")}}, nil
			}

			killed, _ := killLiveWorker(mcpCtx, sessionID)

			// Move the transcript (and every sidecar it owns) into the
			// recoverable trash instead of os.Remove. Same code path the
			// HTTP delete handler uses, so no delete route ever destroys
			// history outright (that is how session 3315 was lost).
			path, err := discovery.SessionPath(sessionID)
			transcriptTrashed := false
			trashDest := ""
			if err == nil && path != "" {
				if trashRoot, trErr := discovery.TrashRoot(); trErr == nil {
					if dest, mvErr := discovery.MoveTranscriptToTrash(path, sessionID, trashRoot, time.Now().UnixNano()); mvErr == nil {
						transcriptTrashed = true
						trashDest = dest
					} else {
						mcpCtx.logger.Warn("move transcript to trash failed",
							slog.String("session_id", sessionID), slog.String("path", path), slog.Any("error", mvErr))
					}
				}
			}

			dbCleared := false
			if mcpCtx.sessionStorage != nil {
				if err := mcpCtx.sessionStorage.DeleteSession(ctx, sessionID); err == nil {
					dbCleared = true
				}
			}

			if !killed && !transcriptTrashed && !dbCleared {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Nothing to delete — session not found in daemon, on disk, or in Mongo")}}, nil
			}
			msg := fmt.Sprintf("Deleted session %s\n  daemon worker killed: %v\n  transcript moved to trash (recoverable): %v\n  mongo record cleared: %v",
				sessionID, killed, transcriptTrashed, dbCleared)
			if trashDest != "" {
				msg += "\n  trash: " + trashDest
			}
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(msg)}}, nil
		},
	)

	// compact_my_session — claude-callable trigger for the deterministic
	// transcript compactor. Claude invokes this when it senses the
	// context is getting heavy (high tool_result count) and wants to
	// shed weight WITHOUT a lossy LLM-rewritten summary. We replace
	// bulky tool_result bodies on older turns with short stubs (keeping
	// tool_use_id pairing intact) and produce a deterministic ledger
	// of every tool call so detail isn't lost — the original transcript
	// is archived first so restore is one mv away.
	s.AddTool(
		mcp.NewTool("compact_my_session",
			mcp.WithDescription("Shrink the current session's JSONL transcript by evicting bulky tool_result payloads from older turns. Preserves recent context verbatim, generates a structured ledger of all tool calls so nothing is lost, archives the original. Use when context feels heavy AND you want to keep working without a lossy /compact rewrite. The actual context-window benefit lands on the next resume — the in-memory conversation isn't shortened."),
			mcp.WithNumber("keep_recent_tool_results", mcp.Description("Number of most-recent tool_result blocks to keep verbatim. Older ones get evicted to stubs. Default 30 (~ last 15 user/assistant exchanges).")),
			mcp.WithNumber("max_stub_bytes", mcp.Description("Bytes of the original tool_result tail to keep inside the stub for recall. Default 200.")),
			mcp.WithString("session_id", mcp.Description("Session ID (pass explicitly if auto-detection from GRIMOIRE_SESSION_ID fails)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			argsMap, _ := req.Params.Arguments.(map[string]interface{})
			sessionID := resolveSessionID(ctx, argsMap)
			if sessionID == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: session_id not set. Pass it explicitly or run inside a managed session.")}}, nil
			}
			path, err := discovery.SessionPath(sessionID)
			if err != nil {
				return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("Error: transcript not found for session " + sessionID)}}, nil
			}
			opts := compact.Options{DropToolUseResultMirror: true}
			if v, ok := argsMap["keep_recent_tool_results"].(float64); ok && v > 0 {
				opts.KeepRecentToolResults = int(v)
			}
			if v, ok := argsMap["max_stub_bytes"].(float64); ok && v > 0 {
				opts.MaxStubBytes = int(v)
			}

			var ledgerBuf strings.Builder
			res, err := compact.Compact(path, opts, &ledgerBuf)
			if err != nil {
				return nil, fmt.Errorf("compact: %w", err)
			}

			// Persist ledger as sidecar so it survives even if claude
			// drops the tool response from its context.
			ledgerPath := path + ".ledger.md"
			if werr := os.WriteFile(ledgerPath, []byte(ledgerBuf.String()), 0o644); werr != nil {
				mcpCtx.logger.Warn("write ledger sidecar", slog.String("path", ledgerPath), slog.Any("error", werr))
			} else {
				res.Stats.LedgerPath = ledgerPath
			}

			var sb strings.Builder
			fmt.Fprintf(&sb, "Compacted session %s\n", sessionID)
			fmt.Fprintf(&sb, "  lines: %d\n", res.Stats.Lines)
			fmt.Fprintf(&sb, "  tool_results total: %d  evicted: %d\n", res.Stats.ToolResults, res.Stats.ToolResultsEvicted)
			fmt.Fprintf(&sb, "  bytes: %s to %s\n", humanBytes(res.Stats.BytesBefore), humanBytes(res.Stats.BytesAfter))
			fmt.Fprintf(&sb, "  approx tokens: %d to %d (saved ~%d)\n",
				res.Stats.ApproxTokensBefore, res.Stats.ApproxTokensAfter,
				res.Stats.ApproxTokensBefore-res.Stats.ApproxTokensAfter)
			fmt.Fprintf(&sb, "  archive: %s\n", res.Stats.ArchivePath)
			if res.Stats.LedgerPath != "" {
				fmt.Fprintf(&sb, "  ledger:  %s\n", res.Stats.LedgerPath)
			}
			fmt.Fprintf(&sb, "\nThe context shrink takes effect on the NEXT resume of this session — your CURRENT in-memory conversation is unaffected. Consider restarting the terminal when convenient.\n")
			// Don't inline the ledger — that would pile fresh bytes into
			// claude's context window when the whole point of compact is
			// to LIGHTEN the context. The ledger is on disk; if claude
			// needs detail it can Read the file.
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent(sb.String())}}, nil
		},
	)
}

// killLiveWorker stops a session's live daemon worker, if one exists.
// It first asks the SessionManager, which knows the grimoireID → daemon
// worker mapping for manager-tracked sessions (global-* quick terminals,
// note-* chats, resume children) whose live worker's own SessionID does
// NOT equal the id the caller passes — a raw daemon SessionID scan misses
// all of those (that is why deleting a quick terminal only cleared Mongo
// but left the shell running). It then falls back to a direct daemon scan
// for external/orphan workers the manager does not track (kvaps-spawned,
// cross-restart orphans, resume children by name). The transcript is
// never touched. Returns whether a worker was killed and, when known, the
// daemon short id.
func killLiveWorker(mcpCtx *MCPContext, sessionID string) (bool, string) {
	// 1. Manager path — resolves grimoireID → daemon worker and kills it.
	if mcpCtx.sessionManager != nil {
		if err := mcpCtx.sessionManager.Close(sessionID); err == nil {
			return true, ""
		}
	}
	// 2. Direct daemon scan for workers the manager does not track.
	client := &daemon.Client{Logger: mcpCtx.logger}
	jobs, err := client.ListSessions()
	if err != nil {
		return false, ""
	}
	for _, j := range jobs {
		match := j.SessionID == sessionID ||
			(strings.HasPrefix(j.Name, "grimoire-resume-") &&
				strings.TrimPrefix(j.Name, "grimoire-resume-") == sessionID[:min(8, len(sessionID))])
		if match {
			if err := client.Remove(j.Short); err == nil {
				return true, j.Short
			}
			return false, ""
		}
	}
	return false, ""
}

// humanBytes formats a byte count as a short human string: 432 B,
// 18 KB, 3.4 MB, 1.2 GB. One decimal for KB+, none for bytes.
func humanBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.1f KB", float64(n)/kb)
	case n < gb:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	}
}
