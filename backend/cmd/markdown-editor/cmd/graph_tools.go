package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerGraphTools(s *server.MCPServer, ctx *MCPContext) {
	// get_graph_data - Get graph connections for all notes
	s.AddTool(
		mcp.NewTool("get_graph_data",
			mcp.WithDescription("Get graph connections between all notes (wikilinks and backlinks)"),
			mcp.WithString("folder",
				mcp.Description("Optional: filter by folder path"),
			),
			mcp.WithNumber("max_notes",
				mcp.Description("Optional: limit number of notes (default: 100)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			folder := request.GetString("folder", "")
			maxNotes := request.GetInt("max_notes", 100)

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			notes, err := ctx.store.ListNotes(timeoutCtx, folder)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error: %v", err)),
					},
				}, nil
			}

			// Limit notes
			if len(notes) > maxNotes {
				notes = notes[:maxNotes]
			}

			// Build graph data
			result := fmt.Sprintf("Graph with %d notes:\n\n", len(notes))

			// Nodes
			result += "## Nodes\n\n"
			for _, note := range notes {
				result += fmt.Sprintf("- **%s** (ID: `%s`, Path: `%s`)\n",
					note.Title, note.ID, note.Path)
			}

			// Edges (connections)
			result += "\n## Connections\n\n"
			edgeCount := 0
			for _, note := range notes {
				if len(note.OutgoingLinks) > 0 {
					result += fmt.Sprintf("**%s** links to:\n", note.Title)
					for _, targetID := range note.OutgoingLinks {
						// Find target note title
						targetTitle := targetID
						for _, n := range notes {
							if n.ID == targetID {
								targetTitle = n.Title
								break
							}
						}
						result += fmt.Sprintf("  → %s\n", targetTitle)
						edgeCount++
					}
					result += "\n"
				}
			}

			result += fmt.Sprintf("\nTotal connections: %d\n", edgeCount)

			// Mermaid diagram
			result += "\n## Mermaid Diagram\n\n```mermaid\ngraph TD\n"
			for _, note := range notes {
				nodeID := sanitizeForMermaid(note.ID)
				nodeLabel := sanitizeForMermaid(note.Title)
				result += fmt.Sprintf("  %s[\"%s\"]\n", nodeID, nodeLabel)
			}
			for _, note := range notes {
				nodeID := sanitizeForMermaid(note.ID)
				for _, targetID := range note.OutgoingLinks {
					targetNodeID := sanitizeForMermaid(targetID)
					result += fmt.Sprintf("  %s --> %s\n", nodeID, targetNodeID)
				}
			}
			result += "```\n"

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// get_note_connections - Get connections for a specific note
	s.AddTool(
		mcp.NewTool("get_note_connections",
			mcp.WithDescription("Get all connections (outgoing and incoming) for a specific note"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithNumber("depth",
				mcp.Description("Optional: connection depth (1-3, default: 1)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			depth := request.GetInt("depth", 1)
			if depth < 1 {
				depth = 1
			}
			if depth > 3 {
				depth = 3
			}

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			// Get main note
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

			result := fmt.Sprintf("# Connections for: %s\n\n", note.Title)
			result += fmt.Sprintf("Path: `%s`\n", note.Path)
			result += fmt.Sprintf("ID: `%s`\n\n", note.ID)

			// Outgoing links
			result += fmt.Sprintf("## Outgoing Links (%d)\n\n", len(note.OutgoingLinks))
			if len(note.OutgoingLinks) > 0 {
				for _, targetID := range note.OutgoingLinks {
					targetNote, err := ctx.store.GetNote(timeoutCtx, targetID)
					if err == nil {
						result += fmt.Sprintf("- **%s** (`%s`)\n", targetNote.Title, targetNote.Path)
					} else {
						result += fmt.Sprintf("- `%s` (not found)\n", targetID)
					}
				}
			} else {
				result += "No outgoing links.\n"
			}

			// Backlinks
			result += fmt.Sprintf("\n## Backlinks (%d)\n\n", len(note.Backlinks))
			if len(note.Backlinks) > 0 {
				for _, sourceID := range note.Backlinks {
					sourceNote, err := ctx.store.GetNote(timeoutCtx, sourceID)
					if err == nil {
						result += fmt.Sprintf("- **%s** (`%s`)\n", sourceNote.Title, sourceNote.Path)
					} else {
						result += fmt.Sprintf("- `%s` (not found)\n", sourceID)
					}
				}
			} else {
				result += "No backlinks.\n"
			}

			// Network stats
			totalConnections := len(note.OutgoingLinks) + len(note.Backlinks)
			result += fmt.Sprintf("\n**Total connections:** %d\n", totalConnections)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)

	// find_related_notes - Find notes related by connections
	s.AddTool(
		mcp.NewTool("find_related_notes",
			mcp.WithDescription("Find notes most connected to a given note (by shared links)"),
			mcp.WithString("note_id",
				mcp.Required(),
				mcp.Description("Note ID or path"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max number of results (default: 10)"),
			),
		),
		func(reqCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			noteID := request.GetString("note_id", "")
			limit := request.GetInt("limit", 10)

			timeoutCtx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
			defer cancel()

			// Get main note
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

			// Get all directly connected notes
			connectedIDs := make(map[string]bool)
			for _, id := range note.OutgoingLinks {
				connectedIDs[id] = true
			}
			for _, id := range note.Backlinks {
				connectedIDs[id] = true
			}

			result := fmt.Sprintf("# Related Notes for: %s\n\n", note.Title)

			if len(connectedIDs) == 0 {
				result += "No connected notes found.\n"
			} else {
				result += fmt.Sprintf("Found %d directly connected notes:\n\n", len(connectedIDs))
				count := 0
				for id := range connectedIDs {
					if count >= limit {
						break
					}
					relatedNote, err := ctx.store.GetNote(timeoutCtx, id)
					if err == nil {
						// Calculate connection strength
						sharedLinks := 0
						for _, link := range relatedNote.OutgoingLinks {
							if connectedIDs[link] {
								sharedLinks++
							}
						}

						result += fmt.Sprintf("- **%s** (`%s`)\n", relatedNote.Title, relatedNote.Path)
						result += fmt.Sprintf("  - Shared connections: %d\n", sharedLinks)
						result += fmt.Sprintf("  - Total links: %d out, %d in\n\n",
							len(relatedNote.OutgoingLinks), len(relatedNote.Backlinks))
						count++
					}
				}
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(result),
				},
			}, nil
		},
	)
}

// sanitizeForMermaid removes special characters for Mermaid diagram compatibility
func sanitizeForMermaid(s string) string {
	// Replace special characters
	result := ""
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result += string(r)
		} else {
			result += "_"
		}
	}
	return result
}
