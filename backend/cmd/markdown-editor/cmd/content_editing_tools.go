package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/events"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerContentEditingTools(s *server.MCPServer, ctx *MCPContext) {
	// replace_text - Replace specific text fragment
	s.AddTool(
		mcp.NewTool("replace_text",
			mcp.WithDescription("Replace specific text fragment in note (first occurrence or all)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("old_text",
				mcp.Required(),
				mcp.Description("Text to replace (exact match)"),
			),
			mcp.WithString("new_text",
				mcp.Required(),
				mcp.Description("Replacement text"),
			),
			mcp.WithBoolean("replace_all",
				mcp.Description("Replace all occurrences (default: false, only first)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			oldText := request.GetString("old_text", "")
			newText := request.GetString("new_text", "")
			replaceAll := request.GetString("replace_all", "") == "true"

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

			// Check if old text exists
			if !strings.Contains(note.Content, oldText) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Text not found in note: %s", note.Title)),
					},
				}, nil
			}

			// Replace
			if replaceAll {
				note.Content = strings.ReplaceAll(note.Content, oldText, newText)
			} else {
				note.Content = strings.Replace(note.Content, oldText, newText, 1)
			}

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

			action := "replaced first occurrence"
			if replaceAll {
				action = "replaced all occurrences"
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Text %s in note: %s", action, note.Title)),
				},
			}, nil
		},
	)

	// insert_after_heading - Insert text after a specific heading
	s.AddTool(
		mcp.NewTool("insert_after_heading",
			mcp.WithDescription("Insert text immediately after a specific heading (e.g., '## Section')"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("heading",
				mcp.Required(),
				mcp.Description("Heading text (with or without #, e.g., 'Section' or '## Section')"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to insert after heading"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			heading := request.GetString("heading", "")
			text := request.GetString("text", "")

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

			// Normalize heading - remove leading # and spaces
			normalizedHeading := strings.TrimLeft(heading, "# ")

			// Try different heading levels
			patterns := []string{
				fmt.Sprintf("\n# %s\n", normalizedHeading),
				fmt.Sprintf("\n## %s\n", normalizedHeading),
				fmt.Sprintf("\n### %s\n", normalizedHeading),
				fmt.Sprintf("\n#### %s\n", normalizedHeading),
			}

			// Also try at start of document
			startPatterns := []string{
				fmt.Sprintf("# %s\n", normalizedHeading),
				fmt.Sprintf("## %s\n", normalizedHeading),
				fmt.Sprintf("### %s\n", normalizedHeading),
				fmt.Sprintf("#### %s\n", normalizedHeading),
			}

			found := false
			for _, pattern := range patterns {
				if strings.Contains(note.Content, pattern) {
					replacement := pattern + "\n" + text + "\n"
					note.Content = strings.Replace(note.Content, pattern, replacement, 1)
					found = true
					break
				}
			}

			if !found {
				for _, pattern := range startPatterns {
					if strings.HasPrefix(note.Content, pattern) {
						replacement := pattern + "\n" + text + "\n"
						note.Content = replacement + strings.TrimPrefix(note.Content, pattern)
						found = true
						break
					}
				}
			}

			if !found {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Heading not found: %s", heading)),
					},
				}, nil
			}

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
					mcp.NewTextContent(fmt.Sprintf("Text inserted after heading '%s' in note: %s", heading, note.Title)),
				},
			}, nil
		},
	)

	// append_to_section - Append text to the end of a section
	s.AddTool(
		mcp.NewTool("append_to_section",
			mcp.WithDescription("Append text to the end of a section (before next heading of same or higher level)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("section",
				mcp.Required(),
				mcp.Description("Section heading (e.g., 'Features' or '## Features')"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to append to section"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			section := request.GetString("section", "")
			text := request.GetString("text", "")

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

			// Normalize section name
			normalizedSection := strings.TrimLeft(section, "# ")

			// Find section heading and determine its level
			headingRegex := regexp.MustCompile(`(?m)^(#{1,6})\s+` + regexp.QuoteMeta(normalizedSection) + `\s*$`)
			matches := headingRegex.FindStringSubmatchIndex(note.Content)

			if matches == nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Section not found: %s", section)),
					},
				}, nil
			}

			headingEnd := matches[1]
			headingLevel := len(note.Content[matches[2]:matches[3]]) // Count #'s

			// Find next heading of same or higher level (fewer #'s)
			remainingContent := note.Content[headingEnd:]
			nextHeadingPattern := fmt.Sprintf(`(?m)^#{1,%d}\s+`, headingLevel)
			nextHeadingRegex := regexp.MustCompile(nextHeadingPattern)
			nextHeadingMatch := nextHeadingRegex.FindStringIndex(remainingContent)

			var insertPosition int
			if nextHeadingMatch != nil {
				// Insert before next heading
				insertPosition = headingEnd + nextHeadingMatch[0]
			} else {
				// No next heading, insert at end
				insertPosition = len(note.Content)
			}

			// Insert text with proper spacing
			beforeInsert := note.Content[:insertPosition]
			afterInsert := note.Content[insertPosition:]

			// Ensure proper spacing
			if !strings.HasSuffix(beforeInsert, "\n\n") && !strings.HasSuffix(beforeInsert, "\n") {
				beforeInsert += "\n\n"
			} else if !strings.HasSuffix(beforeInsert, "\n\n") {
				beforeInsert += "\n"
			}

			note.Content = beforeInsert + text + "\n\n" + afterInsert

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
					mcp.NewTextContent(fmt.Sprintf("Text appended to section '%s' in note: %s", section, note.Title)),
				},
			}, nil
		},
	)

	// prepend_to_note - Add text at the beginning of note
	s.AddTool(
		mcp.NewTool("prepend_to_note",
			mcp.WithDescription("Add text at the very beginning of note (before any content)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to prepend"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			text := request.GetString("text", "")

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

			// Prepend with proper spacing
			note.Content = text + "\n\n" + note.Content

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
					mcp.NewTextContent(fmt.Sprintf("Text prepended to note: %s", note.Title)),
				},
			}, nil
		},
	)

	// append_to_note - Add text at the end of note
	s.AddTool(
		mcp.NewTool("append_to_note",
			mcp.WithDescription("Add text at the very end of note (after all content)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to append"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			text := request.GetString("text", "")

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

			// Append with proper spacing
			if !strings.HasSuffix(note.Content, "\n") {
				note.Content += "\n\n"
			} else if !strings.HasSuffix(note.Content, "\n\n") {
				note.Content += "\n"
			}
			note.Content += text

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
					mcp.NewTextContent(fmt.Sprintf("Text appended to note: %s", note.Title)),
				},
			}, nil
		},
	)

	// delete_text - Remove specific text fragment
	s.AddTool(
		mcp.NewTool("delete_text",
			mcp.WithDescription("Remove specific text fragment from note (first occurrence or all)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to delete (exact match)"),
			),
			mcp.WithBoolean("delete_all",
				mcp.Description("Delete all occurrences (default: false, only first)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			text := request.GetString("text", "")
			deleteAll := request.GetString("delete_all", "") == "true"

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

			// Check if text exists
			if !strings.Contains(note.Content, text) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Text not found in note: %s", note.Title)),
					},
				}, nil
			}

			// Delete
			if deleteAll {
				note.Content = strings.ReplaceAll(note.Content, text, "")
			} else {
				note.Content = strings.Replace(note.Content, text, "", 1)
			}

			// Clean up excessive newlines
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

			action := "deleted first occurrence"
			if deleteAll {
				action = "deleted all occurrences"
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Text %s from note: %s", action, note.Title)),
				},
			}, nil
		},
	)

	// insert_at_line - Insert text at specific line number
	s.AddTool(
		mcp.NewTool("insert_at_line",
			mcp.WithDescription("Insert text at specific line number (1-based)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithNumber("line",
				mcp.Required(),
				mcp.Description("Line number (1-based) where to insert"),
			),
			mcp.WithString("text",
				mcp.Required(),
				mcp.Description("Text to insert"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			lineStr := request.GetString("line", "")
			text := request.GetString("text", "")

			lineNum, err := strconv.Atoi(lineStr)
			if err != nil || lineNum < 1 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent("Line number must be a valid integer >= 1"),
					},
				}, nil
			}

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

			// Split into lines
			lines := strings.Split(note.Content, "\n")

			if lineNum > len(lines)+1 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Line number %d exceeds note length (%d lines)", lineNum, len(lines))),
					},
				}, nil
			}

			// Insert text at line
			insertIndex := lineNum - 1
			newLines := append(lines[:insertIndex], append([]string{text}, lines[insertIndex:]...)...)
			note.Content = strings.Join(newLines, "\n")

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
					mcp.NewTextContent(fmt.Sprintf("Text inserted at line %d in note: %s", lineNum, note.Title)),
				},
			}, nil
		},
	)
}
