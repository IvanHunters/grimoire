package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerContentStructureTools(s *server.MCPServer, ctx *MCPContext) {
	// get_note_headings - Get outline of all headings
	s.AddTool(
		mcp.NewTool("get_note_headings",
			mcp.WithDescription("Get outline of all headings in a note. Useful for understanding note structure without reading full content."),
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

			// Extract all headings
			headingRegex := regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
			matches := headingRegex.FindAllStringSubmatch(note.Content, -1)

			if len(matches) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Note '%s' has no headings", note.Title)),
					},
				}, nil
			}

			result := fmt.Sprintf("# Outline for: %s\n\n", note.Title)
			result += fmt.Sprintf("Found %d headings:\n\n", len(matches))

			for i, match := range matches {
				level := len(match[1]) // Count #'s
				heading := match[2]
				indent := strings.Repeat("  ", level-1)
				result += fmt.Sprintf("%d. %s%s (level %d)\n", i+1, indent, heading, level)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_section_content - Get content of specific section
	s.AddTool(
		mcp.NewTool("get_section_content",
			mcp.WithDescription("Get content of a specific section (text under a heading until next heading of same or higher level). Useful for reading large notes section by section."),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithString("heading",
				mcp.Required(),
				mcp.Description("Heading text (with or without #, e.g., 'Features' or '## Features')"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			heading := request.GetString("heading", "")

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

			// Find the heading
			headingRegex := regexp.MustCompile(`(?m)^(#{1,6})\s+` + regexp.QuoteMeta(normalizedHeading) + `\s*$`)
			matches := headingRegex.FindStringSubmatchIndex(note.Content)

			if matches == nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Heading not found: %s", heading)),
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

			var sectionContent string
			if nextHeadingMatch != nil {
				// Section ends before next heading
				sectionContent = remainingContent[:nextHeadingMatch[0]]
			} else {
				// No next heading, section extends to end
				sectionContent = remainingContent
			}

			// Clean up leading/trailing whitespace
			sectionContent = strings.TrimSpace(sectionContent)

			if sectionContent == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Section '%s' is empty", heading)),
					},
				}, nil
			}

			result := fmt.Sprintf("# Section: %s\n\n", normalizedHeading)
			result += fmt.Sprintf("From note: %s\n\n", note.Title)
			result += fmt.Sprintf("---\n\n%s", sectionContent)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)
}
