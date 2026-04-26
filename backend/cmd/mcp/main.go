package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MCPContext struct {
	store  *storage.MongoStorage
	logger *slog.Logger
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
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
		logger.Error("failed to connect to mongodb", slog.Any("error", err))
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	db := client.Database(cfg.MongoDBDatabase)
	store := storage.NewMongoStorage(db)

	mcpCtx := &MCPContext{
		store:  store,
		logger: logger,
	}

	// Create MCP server
	s := server.NewMCPServer(
		"Markdown Editor",
		"1.0.0",
		server.WithLogging(),
	)

	// Register tools
	registerNoteTools(s, mcpCtx)

	logger.Info("mcp server starting")

	// Start server (stdio mode)
	if err := server.ServeStdio(s); err != nil {
		logger.Error("server error", slog.Any("error", err))
		os.Exit(1)
	}
}

func registerNoteTools(s *server.MCPServer, ctx *MCPContext) {
	// list_notes - List all notes in a folder
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

	// read_note - Read a note by path or ID
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

			// Try to get by ID first
			note, err := ctx.store.GetNote(timeoutCtx, id)
			if err != nil {
				// Try by path
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

	// create_note - Create a new note
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

			// Generate note path
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

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note created: %s (ID: %s)", note.Path, note.ID)),
				},
			}, nil
		},
	)

	// search_notes - Full-text search
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

	// create_folder - Create a folder
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

// normalizeTitle converts title to lowercase slug
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
	// Remove consecutive hyphens
	for i := 0; i < len(result)-1; i++ {
		if result[i] == '-' && result[i+1] == '-' {
			result = result[:i] + result[i+1:]
			i--
		}
	}
	return result
}
