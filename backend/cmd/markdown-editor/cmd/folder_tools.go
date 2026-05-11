package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerFolderTools(s *server.MCPServer, ctx *MCPContext) {
	// list_folders - Get folder tree
	s.AddTool(
		mcp.NewTool("list_folders",
			mcp.WithDescription("List all folders as a tree structure"),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			timeoutCtx, cancel := context.WithTimeout(reqCtx, 5*time.Second)
			defer cancel()

			folders, err := ctx.store.ListFolders(timeoutCtx)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d folders:\n\n", len(folders))
			for _, folder := range folders {
				indent := strings.Repeat("  ", strings.Count(folder.Path, "/"))
				name := folder.Path
				if strings.Contains(folder.Path, "/") {
					parts := strings.Split(folder.Path, "/")
					name = parts[len(parts)-1]
				}
				result += fmt.Sprintf("%s📁 %s (`%s`)\n", indent, name, folder.Path)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// delete_folder - Delete folder and all its contents
	s.AddTool(
		mcp.NewTool("delete_folder",
			mcp.WithDescription("Delete a folder and all its notes and subfolders"),
			mcp.WithString("path",
				mcp.Required(),
				mcp.Description("Folder path to delete"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			path := request.GetString("path", "")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			// Delete folder
			if err := ctx.store.DeleteFolder(timeoutCtx, path); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type: events.EventFolderDeleted,
				Path: path,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Folder deleted: %s (including all notes and subfolders)", path)),
				},
			}, nil
		},
	)

	// rename_folder - Rename a folder
	s.AddTool(
		mcp.NewTool("rename_folder",
			mcp.WithDescription("Rename a folder (moves all notes and subfolders)"),
			mcp.WithString("old_path",
				mcp.Required(),
				mcp.Description("Current folder path"),
			),
			mcp.WithString("new_name",
				mcp.Required(),
				mcp.Description("New folder name (just the name, not full path)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			oldPath := request.GetString("old_path", "")
			newName := request.GetString("new_name", "")

			// Calculate new path
			var newPath string
			if strings.Contains(oldPath, "/") {
				parts := strings.Split(oldPath, "/")
				parts[len(parts)-1] = newName
				newPath = strings.Join(parts, "/")
			} else {
				newPath = newName
			}

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			// Move all notes in folder
			notes, err := ctx.store.ListNotes(timeoutCtx, oldPath)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			for _, note := range notes {
				// Update note's folder and path
				note.Folder = newPath
				oldNotePath := note.Path
				// Replace folder part in path
				if strings.HasPrefix(oldNotePath, oldPath+"/") {
					note.Path = strings.Replace(oldNotePath, oldPath+"/", newPath+"/", 1)
				}

				if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Error updating note %s: %v", note.Title, err)),
						},
					}, nil
				}
			}

			// Delete old folder
			if err := ctx.store.DeleteFolder(timeoutCtx, oldPath); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error deleting old folder: %v", err)),
					},
				}, nil
			}

			// Create new folder
			newFolder := &models.Folder{Path: newPath}
			if err := ctx.store.CreateFolder(timeoutCtx, newFolder); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error creating new folder: %v", err)),
					},
				}, nil
			}

			// Publish events
			ctx.eventBus.Publish(events.Event{
				Type: events.EventFolderDeleted,
				Path: oldPath,
			})
			ctx.eventBus.Publish(events.Event{
				Type:   events.EventFolderCreated,
				Folder: newFolder,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Folder renamed: %s → %s (%d notes moved)", oldPath, newPath, len(notes))),
				},
			}, nil
		},
	)

	// move_folder - Move folder to different parent
	s.AddTool(
		mcp.NewTool("move_folder",
			mcp.WithDescription("Move a folder to a different parent folder"),
			mcp.WithString("source_path",
				mcp.Required(),
				mcp.Description("Folder path to move"),
			),
			mcp.WithString("target_parent",
				mcp.Required(),
				mcp.Description("Target parent folder path (empty string for root)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			sourcePath := request.GetString("source_path", "")
			targetParent := request.GetString("target_parent", "")

			// Calculate new path
			folderName := sourcePath
			if strings.Contains(sourcePath, "/") {
				parts := strings.Split(sourcePath, "/")
				folderName = parts[len(parts)-1]
			}

			var newPath string
			if targetParent == "" {
				newPath = folderName
			} else {
				newPath = targetParent + "/" + folderName
			}

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			// Move all notes
			notes, err := ctx.store.ListNotes(timeoutCtx, sourcePath)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			for _, note := range notes {
				note.Folder = newPath
				oldNotePath := note.Path
				if strings.HasPrefix(oldNotePath, sourcePath+"/") {
					note.Path = strings.Replace(oldNotePath, sourcePath+"/", newPath+"/", 1)
				}

				if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Error updating note %s: %v", note.Title, err)),
						},
					}, nil
				}
			}

			// Delete old folder
			if err := ctx.store.DeleteFolder(timeoutCtx, sourcePath); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error deleting old folder: %v", err)),
					},
				}, nil
			}

			// Create new folder
			newFolder := &models.Folder{Path: newPath}
			if err := ctx.store.CreateFolder(timeoutCtx, newFolder); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error creating new folder: %v", err)),
					},
				}, nil
			}

			// Publish events
			ctx.eventBus.Publish(events.Event{
				Type: events.EventFolderDeleted,
				Path: sourcePath,
			})
			ctx.eventBus.Publish(events.Event{
				Type:   events.EventFolderCreated,
				Folder: newFolder,
			})

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Folder moved: %s → %s (%d notes moved)", sourcePath, newPath, len(notes))),
				},
			}, nil
		},
	)
}

// registerNoteManagementTools adds rename and move tools for notes
func registerNoteManagementTools(s *server.MCPServer, ctx *MCPContext) {
	// rename_note - Rename a note
	s.AddTool(
		mcp.NewTool("rename_note",
			mcp.WithDescription("Rename a note (updates title and path)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("new_title",
				mcp.Required(),
				mcp.Description("New note title"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			newTitle := request.GetString("new_title", "")

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

			oldTitle := note.Title
			oldPath := note.Path

			// Update title and path
			note.Title = newTitle
			newFileName := normalizeTitle(newTitle) + ".md"
			if note.Folder != "" {
				note.Path = note.Folder + "/" + newFileName
			} else {
				note.Path = newFileName
			}

			// Update note
			if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
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
					mcp.NewTextContent(fmt.Sprintf("Note renamed:\n- Title: %s → %s\n- Path: %s → %s", oldTitle, newTitle, oldPath, note.Path)),
				},
			}, nil
		},
	)

	// move_note - Move note to different folder
	s.AddTool(
		mcp.NewTool("move_note",
			mcp.WithDescription("Move a note to a different folder"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("target_folder",
				mcp.Required(),
				mcp.Description("Target folder path (empty string for root)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			targetFolder := request.GetString("target_folder", "")

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

			oldFolder := note.Folder
			oldPath := note.Path

			// Update folder and path
			note.Folder = targetFolder
			fileName := oldPath
			if strings.Contains(oldPath, "/") {
				parts := strings.Split(oldPath, "/")
				fileName = parts[len(parts)-1]
			}

			if targetFolder != "" {
				note.Path = targetFolder + "/" + fileName
			} else {
				note.Path = fileName
			}

			// Update note
			if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			// Publish event
			ctx.eventBus.Publish(events.Event{
				Type: events.EventNoteUpdated,
				Note: note,
			})

			targetDisplay := targetFolder
			if targetDisplay == "" {
				targetDisplay = "root"
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Note moved:\n- From: %s\n- To: %s\n- New path: %s", oldFolder, targetDisplay, note.Path)),
				},
			}, nil
		},
	)
}
