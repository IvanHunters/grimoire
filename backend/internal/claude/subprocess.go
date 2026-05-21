package claude

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// startPTYReader reads from PTY and broadcasts to all subscribers
func startPTYReader(session *ClaudeSession, logger *slog.Logger) {
	logger.Info("starting PTY reader", slog.String("session_id", session.ID))
	buf := make([]byte, 4096)

	for {
		n, err := session.PTY.Read(buf)
		if err != nil {
			if err.Error() != "EOF" && err.Error() != "read /dev/ptmx: input/output error" {
				logger.Error("PTY read error", slog.Any("error", err))
			}
			logger.Info("PTY reader stopped", slog.String("session_id", session.ID))
			return
		}

		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			// Save to buffer
			session.AppendOutput(data)

			// Broadcast to all active WebSocket subscribers (fan-out).
			session.BroadcastOutput(data)
			logger.Debug("broadcast PTY output",
				slog.String("session_id", session.ID),
				slog.Int("bytes", n),
			)

			session.UpdateActivity()
		}
	}
}

// startClaudeSubprocess starts a new Claude subprocess using PTY
func startClaudeSubprocess(sessionID string, dangerousMode bool, workingDir string, mongoURI string, mongoDatabase string, logger *slog.Logger, systemPrompt string) (*ClaudeSession, error) {
	// Setup MCP configuration automatically
	mcpConfigPath, err := setupMCPConfig(sessionID, workingDir, mongoURI, mongoDatabase, logger)
	if err != nil {
		logger.Warn("failed to setup MCP config, continuing without it", slog.Any("error", err))
	}

	// Build command arguments
	args := []string{}
	if dangerousMode {
		args = append(args, "--dangerously-skip-permissions")
	}
	if systemPrompt != "" {
		// Pass context as system prompt — avoids PTY echo of the context text in the terminal.
		// --append-system-prompt keeps Claude's built-in system prompt intact.
		args = append(args, "--append-system-prompt", systemPrompt)
	}

	// Create command
	cmd := exec.Command("claude", args...)
	cmd.Dir = workingDir

	// Set environment variables
	env := os.Environ()

	// Remove CLAUDECODE env to allow nested Claude sessions
	filteredEnv := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filteredEnv = append(filteredEnv, e)
		}
	}

	// Ensure UTF-8 encoding for proper display of non-ASCII characters
	filteredEnv = append(filteredEnv, "LANG=en_US.UTF-8")
	filteredEnv = append(filteredEnv, "LC_ALL=en_US.UTF-8")
	// Inject session ID so MCP tools can self-identify
	filteredEnv = append(filteredEnv, "GRIMOIRE_SESSION_ID="+sessionID)

	if mcpConfigPath != "" {
		// Claude CLI will read MCP config from .claude directory in working dir
		logger.Info("mcp config created", slog.String("path", mcpConfigPath))
	}
	cmd.Env = filteredEnv

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start claude subprocess: %w", err)
	}

	// Create session
	now := time.Now()

	session := &ClaudeSession{
		ID:            sessionID,
		Name:          "Terminal Session", // Default name, can be updated later
		Cmd:           cmd,
		PTY:           ptmx,
		DangerousMode: dangerousMode,
		WorkingDir:    workingDir,
		MCPConfigPath: mcpConfigPath,
		CreatedAt:     now,
		LastActivity:  now,
		Messages:      make([]models.ClaudeMessage, 0),
		OutputBuffer:  make([]byte, 0, 1024), // Initialize with small capacity
	}

	// Start PTY reader goroutine (runs once for the session lifetime)
	go startPTYReader(session, logger)

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
		// Close all subscriber channels when process exits.
		session.CloseAllSubscriptions()
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

// SetupMCPConfig creates MCP configuration for Claude CLI in the given working directory.
func SetupMCPConfig(workingDir string, mongoURI string, mongoDatabase string) (string, error) {
	return setupMCPConfig("", workingDir, mongoURI, mongoDatabase, slog.Default())
}

// setupMCPConfig writes an HTTP MCP config for the subprocess so it calls the backend
// at /mcp?session_id=<id>. The backend injects the session ID into the request context,
// making all session-aware MCP tools work without relying on env var inheritance.
func setupMCPConfig(sessionID string, workingDir string, mongoURI string, mongoDatabase string, logger *slog.Logger) (string, error) { //nolint:unparam // logger may be used for debugging
	// Create .claude directory in working dir
	claudeDir := filepath.Join(workingDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .claude directory: %w", err)
	}

	mcpURL := "http://localhost:8080/mcp"
	if sessionID != "" {
		mcpURL += "?session_id=" + sessionID
	}

	// Claude Code reads project-level MCP config from .claude/settings.json under "mcpServers".
	// Read existing settings first so we don't clobber other keys.
	configPath := filepath.Join(claudeDir, "settings.json")
	existing := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	existing["mcpServers"] = map[string]any{
		"markdown-editor": map[string]any{
			"url": mcpURL,
		},
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal mcp config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return configPath, nil
}
