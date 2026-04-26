package claude

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// startClaudeSubprocess starts a new Claude subprocess using PTY
func startClaudeSubprocess(sessionID string, dangerousMode bool, workingDir string, logger *slog.Logger) (*ClaudeSession, error) {
	// Setup MCP configuration automatically
	mcpConfigPath, err := setupMCPConfig(workingDir, logger)
	if err != nil {
		logger.Warn("failed to setup MCP config, continuing without it", slog.Any("error", err))
	}

	// Build command arguments
	args := []string{}
	if dangerousMode {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Create command
	cmd := exec.Command("claude", args...)
	cmd.Dir = workingDir

	// Set environment variables
	env := os.Environ()
	if mcpConfigPath != "" {
		// Claude CLI will read MCP config from .claude directory in working dir
		logger.Info("mcp config created", slog.String("path", mcpConfigPath))
	}
	cmd.Env = env

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start claude subprocess: %w", err)
	}

	// Create session
	session := &ClaudeSession{
		ID:            sessionID,
		Cmd:           cmd,
		PTY:           ptmx,
		DangerousMode: dangerousMode,
		WorkingDir:    workingDir,
		LastActivity:  time.Now(),
		Messages:      make([]models.ClaudeMessage, 0),
	}

	// Start background goroutine to wait for process exit
	// This prevents zombie processes
	go func() {
		if err := cmd.Wait(); err != nil {
			logger.Error("claude subprocess exited",
				slog.String("session_id", sessionID),
				slog.Any("error", err),
			)
		} else {
			logger.Info("claude subprocess exited normally",
				slog.String("session_id", sessionID),
			)
		}
	}()

	logger.Info("claude subprocess started",
		slog.String("session_id", sessionID),
		slog.Bool("dangerous_mode", dangerousMode),
		slog.String("working_dir", workingDir),
	)

	return session, nil
}

// shutdownSession gracefully shuts down a Claude session
func shutdownSession(session *ClaudeSession, logger *slog.Logger) error {
	logger.Info("shutting down claude session",
		slog.String("session_id", session.ID),
	)

	// Step 1: Send Ctrl+D (EOF) to PTY
	if session.PTY != nil {
		_, _ = session.PTY.Write([]byte{4}) // ASCII 4 = Ctrl+D
		time.Sleep(2 * time.Second)

		// Check if process exited
		if session.Cmd.ProcessState == nil || !session.Cmd.ProcessState.Exited() {
			// Step 2: Send SIGTERM
			logger.Info("sending SIGTERM to claude subprocess",
				slog.String("session_id", session.ID),
			)
			if err := session.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
				logger.Error("failed to send SIGTERM", slog.Any("error", err))
			}
			time.Sleep(3 * time.Second)

			// Check again
			if session.Cmd.ProcessState == nil || !session.Cmd.ProcessState.Exited() {
				// Step 3: Send SIGKILL
				logger.Warn("force killing claude subprocess",
					slog.String("session_id", session.ID),
				)
				if err := session.Cmd.Process.Kill(); err != nil {
					logger.Error("failed to kill process", slog.Any("error", err))
				}
			}
		}

		// Close PTY
		if err := session.PTY.Close(); err != nil {
			logger.Error("failed to close PTY", slog.Any("error", err))
		}
	}

	logger.Info("claude session shutdown complete",
		slog.String("session_id", session.ID),
	)

	return nil
}

// setupMCPConfig creates MCP configuration for Claude CLI
func setupMCPConfig(workingDir string, logger *slog.Logger) (string, error) {
	// Get absolute path to markdown-editor binary
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}
	execDir := filepath.Dir(execPath)
	mcpBinary := filepath.Join(execDir, "markdown-editor")

	// Verify MCP binary exists
	if _, err := os.Stat(mcpBinary); os.IsNotExist(err) {
		return "", fmt.Errorf("mcp binary not found at %s", mcpBinary)
	}

	// Create .claude directory in working dir
	claudeDir := filepath.Join(workingDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Create MCP config
	mcpConfig := map[string]interface{}{
		"markdown-editor": map[string]interface{}{
			"command": mcpBinary,
			"args":    []string{"mcp"},
			"env": map[string]string{
				"MONGODB_URI":      os.Getenv("MONGODB_URI"),
				"MONGODB_DATABASE": os.Getenv("MONGODB_DATABASE"),
			},
		},
	}

	configPath := filepath.Join(claudeDir, "mcp_servers.json")
	configFile, err := os.Create(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to create mcp config file: %w", err)
	}
	defer configFile.Close()

	encoder := json.NewEncoder(configFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(mcpConfig); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return configPath, nil
}
