package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "markdown-editor",
	Short: "Markdown Editor - Backend server with Claude AI integration",
	Long: `Markdown Editor backend provides:
  - HTTP API server for notes and folders management
  - WebSocket server for real-time Claude AI chat
  - MCP server for Claude Code integration`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(mcpCmd)
}
