package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerAttachmentTools(s *server.MCPServer, ctx *MCPContext) {
	// upload_image - Upload an image and insert markdown link into note
	s.AddTool(
		mcp.NewTool("upload_image",
			mcp.WithDescription("Upload an image and insert markdown link into note"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path where to insert the image"),
			),
			mcp.WithString("image_data",
				mcp.Required(),
				mcp.Description("Base64-encoded image data"),
			),
			mcp.WithString("alt_text",
				mcp.Description("Alt text for the image (optional)"),
			),
			mcp.WithString("filename",
				mcp.Description("Original filename (optional, used to detect extension)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			imageData := request.GetString("image_data", "")
			altText := request.GetString("alt_text", "image")
			filename := request.GetString("filename", "image.png")

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
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

			// Decode base64 image data
			imageBytes, err := base64.StdEncoding.DecodeString(imageData)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Failed to decode image data: %v", err)),
					},
				}, nil
			}

			// Validate file size (10MB max)
			maxSize := int64(10485760)
			if int64(len(imageBytes)) > maxSize {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent("Image too large (max 10MB)"),
					},
				}, nil
			}

			// Detect extension from filename
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".png"
			}

			// Generate unique filename
			uniqueFilename := uuid.New().String() + ext

			// Create directory structure: YYYY/MM/
			now := time.Now()
			yearMonth := now.Format("2006/01")

			// Get uploads directory from config (default: ./data/uploads)
			uploadsDir := "./data/uploads"
			if ctx.config != nil {
				uploadsDir = ctx.config.UploadsDir
			}

			uploadDir := filepath.Join(uploadsDir, yearMonth)

			if err := os.MkdirAll(uploadDir, 0755); err != nil {
				ctx.logger.Error("failed to create upload directory", "error", err)
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Failed to create directory: %v", err)),
					},
				}, nil
			}

			// Save file
			destPath := filepath.Join(uploadDir, uniqueFilename)
			if err := os.WriteFile(destPath, imageBytes, 0644); err != nil {
				ctx.logger.Error("failed to save file", "error", err)
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Failed to save file: %v", err)),
					},
				}, nil
			}

			// Generate URL
			url := fmt.Sprintf("/uploads/%s/%s", yearMonth, uniqueFilename)

			// Insert markdown image link at the end of note
			imageMarkdown := fmt.Sprintf("\n\n![%s](%s)", altText, url)
			note.Content += imageMarkdown

			// Update note
			if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Failed to update note: %v", err)),
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
					mcp.NewTextContent(fmt.Sprintf("Image uploaded and inserted into note!\nURL: %s\nMarkdown: %s", url, imageMarkdown)),
				},
			}, nil
		},
	)

	// list_note_attachments - List all images/attachments in a note
	s.AddTool(
		mcp.NewTool("list_note_attachments",
			mcp.WithDescription("List all images/attachments referenced in a note"),
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

			// Extract all markdown images: ![alt](url)
			imageRegex := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
			matches := imageRegex.FindAllStringSubmatch(note.Content, -1)

			if len(matches) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("No attachments found in note: %s", note.Title)),
					},
				}, nil
			}

			result := fmt.Sprintf("Found %d attachment(s) in note '%s':\n\n", len(matches), note.Title)
			for i, match := range matches {
				altText := match[1]
				url := match[2]
				result += fmt.Sprintf("%d. ![%s](%s)\n", i+1, altText, url)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// delete_attachment - Delete an attachment and remove from note
	s.AddTool(
		mcp.NewTool("delete_attachment",
			mcp.WithDescription("Delete an attachment file and remove reference from note"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("attachment_path",
				mcp.Required(),
				mcp.Description("Attachment URL path (e.g., /uploads/2024/04/image.png)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			attachmentPath := request.GetString("attachment_path", "")

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

			// Get uploads directory from config
			uploadsDir := "./data/uploads"
			if ctx.config != nil {
				uploadsDir = ctx.config.UploadsDir
			}

			// Convert URL path to filesystem path
			// /uploads/2024/04/image.png -> ./data/uploads/2024/04/image.png
			attachmentPath = strings.TrimPrefix(attachmentPath, "/uploads/")
			filePath := filepath.Join(uploadsDir, attachmentPath)

			// Delete physical file
			if err := os.Remove(filePath); err != nil {
				if !os.IsNotExist(err) {
					ctx.logger.Error("failed to delete file", "path", filePath, "error", err)
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Warning: Failed to delete file %s: %v", filePath, err)),
						},
					}, nil
				}
			}

			// Remove all markdown references to this attachment
			urlPattern := "/uploads/" + attachmentPath
			imageRegex := regexp.MustCompile(fmt.Sprintf(`!\[[^\]]*\]\(%s\)\s*`, regexp.QuoteMeta(urlPattern)))
			note.Content = imageRegex.ReplaceAllString(note.Content, "")

			// Clean up multiple consecutive newlines
			note.Content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(note.Content, "\n\n")

			// Update note
			if err := ctx.store.UpdateNote(timeoutCtx, note); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Failed to update note: %v", err)),
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
					mcp.NewTextContent(fmt.Sprintf("Attachment deleted: %s\nReferences removed from note: %s", urlPattern, note.Title)),
				},
			}, nil
		},
	)
}
