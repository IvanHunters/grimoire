package claude

import (
	"encoding/json"
	"fmt"
	"io"
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

// resizeSubprocessPTY changes the PTY size of a locally-owned subprocess
// session. Returns an error if rw isn't actually a *os.File (which is the
// case for daemon-backed sessions — those route resize through
// ClaudeSession.Resize → AttachConn.Resize instead).
func resizeSubprocessPTY(rw io.ReadWriteCloser, cols, rows int) error {
	f, ok := rw.(*os.File)
	if !ok {
		return fmt.Errorf("resize: PTY is not *os.File (backend mismatch)")
	}
	return pty.Setsize(f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// startPTYReader reads from PTY and broadcasts to all subscribers. Works
// for both backends — session.PTY is io.ReadWriteCloser. On exit it
// always closes subscribers; for subprocess sessions the cmd.Wait
// goroutine also calls CloseAllSubscriptions but it's idempotent.
func startPTYReader(session *ClaudeSession, logger *slog.Logger) {
	logger.Info("starting PTY reader",
		slog.String("session_id", session.ID),
		slog.Bool("daemon_backed", session.IsDaemonBacked()),
	)
	defer func() {
		session.CloseAllSubscriptions()
		// Clear PTY so future GetOrAttach/GetOrResume can detect the
		// dead state and re-spawn instead of handing the caller a stub
		// entry whose reader is gone. Without this, when a supervisor
		// dies (OOM, hung-kill from clearStaleDaemonLock, etc.), all
		// subsequent WS reconnects get a "live"-looking session that
		// silently swallows every input/output — the browser appears
		// frozen and only "restart session" fixes it.
		session.ClearPTY()
		// Signal ShutdownWorker (and any other waiter) that the reader
		// has fully exited and PTY/Cmd are safe to nil. Without this,
		// ShutdownWorker could clear session.PTY while this goroutine
		// was still mid-read, producing a nil-deref panic.
		session.SignalReaderDone()
	}()
	buf := make([]byte, 4096)

	for {
		n, err := session.PTY.Read(buf)
		if err != nil {
			msg := err.Error()
			// All three are expected on a clean session close:
			//   - "EOF" from creack/pty when subprocess exits
			//   - "input/output error" from creack/pty on macOS at slave-side close
			//   - "use of closed network connection" from daemon AttachConn on shutdown
			isExpected := msg == "EOF" ||
				msg == "read /dev/ptmx: input/output error" ||
				strings.Contains(msg, "use of closed network connection")
			if !isExpected {
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

	// Pre-allocate the procExit channel so a fast shutdown path that
	// races the goroutine startup still sees a live (un-closed) chan.
	_ = session.ProcExitSignal()

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
		// Signal exit BEFORE closing subscribers — race-free replacement
		// for reading cmd.ProcessState. shutdownSubprocessSession uses
		// IsProcessDone() to decide whether to escalate signals.
		session.SignalProcExit()
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

// shutdownSession gracefully shuts down a Claude session. Branches on the
// backend: daemon-backed sessions go through op:kill on the daemon (the
// daemon owns the process lifecycle); subprocess-backed sessions follow
// the classic Ctrl+D → SIGTERM → SIGKILL escalation.
func shutdownSession(session *ClaudeSession, logger *slog.Logger) error {
	logger.Info("shutting down claude session",
		slog.String("session_id", session.ID),
		slog.Bool("daemon_backed", session.IsDaemonBacked()),
	)

	if session.IsDaemonBacked() {
		return shutdownDaemonSession(session, logger, true)
	}
	return shutdownSubprocessSession(session, logger)
}

// detachSession releases our local hold on a session without killing
// the underlying worker. Use for graceful grimoire shutdown so daemon
// workers survive the restart — user can re-attach next time.
func detachSession(session *ClaudeSession, logger *slog.Logger) error {
	if session.IsDaemonBacked() {
		return shutdownDaemonSession(session, logger, false)
	}
	// Subprocess sessions can't survive grimoire restart — they're our
	// child processes. Full shutdown is the only option.
	return shutdownSubprocessSession(session, logger)
}

func shutdownSubprocessSession(session *ClaudeSession, logger *slog.Logger) error {
	// Snapshot PTY into a local — the reader's defer can clear
	// session.PTY (via ClearPTY) at any moment after we observe it
	// non-nil. Subsequent calls on session.PTY would nil-deref panic
	// without this snapshot.
	pty := session.PTY
	// Step 1: Send Ctrl+D (EOF) to PTY
	if pty != nil {
		_, _ = pty.Write([]byte{4}) // ASCII 4 = Ctrl+D
		time.Sleep(2 * time.Second)

		// Check if process exited. IsProcessDone is a non-blocking
		// channel check that's race-free against cmd.Wait writing
		// ProcessState.
		if session.Cmd != nil && !session.IsProcessDone() {
			// Step 2: Send SIGTERM
			logger.Info("sending SIGTERM to claude subprocess",
				slog.String("session_id", session.ID),
			)
			if err := session.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
				logger.Error("failed to send SIGTERM", slog.Any("error", err))
			}
			time.Sleep(3 * time.Second)

			// Check again
			if !session.IsProcessDone() {
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
		if err := pty.Close(); err != nil {
			logger.Error("failed to close PTY", slog.Any("error", err))
		}
	}

	logger.Info("claude session shutdown complete",
		slog.String("session_id", session.ID),
	)

	return nil
}

// shutdownDaemonSession releases this side of a daemon-hosted session.
// killWorker=true tells the daemon to fully terminate the worker
// (used when the user explicitly kills/restarts the session). false
// detaches only — the worker keeps running in the daemon so the user
// can re-attach later (used during grimoire graceful shutdown so a
// backend restart doesn't take user's live conversations with it).
func shutdownDaemonSession(session *ClaudeSession, logger *slog.Logger, killWorker bool) error {
	// 1. Close our local attach (detach from the daemon-hosted PTY).
	// Snapshot PTY first — the reader's defer can ClearPTY() at any
	// moment after we observe it non-nil.
	if pty := session.PTY; pty != nil {
		if err := pty.Close(); err != nil {
			logger.Debug("attach close error (often expected at shutdown)",
				slog.String("session_id", session.ID),
				slog.Any("error", err),
			)
		}
	}

	// 2. Optionally tell the daemon to kill the worker too. We use Remove
	// (kill + jobdir cleanup) so we don't leak ~/.claude/jobs/<short>/
	// entries when explicitly closing. On graceful shutdown we SKIP this:
	// the daemon worker stays alive across grimoire restarts, which is
	// the whole point of the daemon backend.
	if killWorker && session.DaemonClient != nil && session.DaemonShort != "" {
		if err := session.DaemonClient.Remove(session.DaemonShort); err != nil {
			logger.Error("daemon remove failed",
				slog.String("session_id", session.ID),
				slog.String("daemon_short", session.DaemonShort),
				slog.Any("error", err),
			)
			// Non-fatal: even if the daemon doesn't ack the kill, we've
			// already detached locally so our state is consistent.
		}
	}

	// 3. Close subscribers — the daemon-backed reader goroutine usually
	// does this on EOF, but if shutdown races ahead we close here too.
	session.CloseAllSubscriptions()

	logger.Info("daemon-backed session shutdown complete",
		slog.String("session_id", session.ID),
		slog.Bool("worker_killed", killWorker),
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
