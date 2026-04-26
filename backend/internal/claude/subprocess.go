package claude

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// startClaudeSubprocess starts a new Claude subprocess using PTY
func startClaudeSubprocess(sessionID string, dangerousMode bool, workingDir string, logger *slog.Logger) (*ClaudeSession, error) {
	// Build command arguments
	args := []string{}
	if dangerousMode {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Create command
	cmd := exec.Command("claude", args...)
	cmd.Dir = workingDir

	// Set environment variables
	cmd.Env = os.Environ()

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
