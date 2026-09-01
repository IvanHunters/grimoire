package claude

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// startPTYReader reads from the session PTY and broadcasts to all
// subscribers. session.PTY is an io.ReadWriteCloser backed by the daemon
// AttachConn. On exit it always closes subscribers and clears the PTY so a
// dead worker is detected on the next attach.
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
			// "use of closed network connection" is the daemon AttachConn's
			// close error on a clean shutdown; the ptmx variants are kept for
			// robustness against any local-PTY error strings surfacing.
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

// shutdownSession gracefully shuts down a Claude session. All sessions are
// daemon-backed, so this goes through op:kill on the daemon — the daemon
// owns the worker's process lifecycle.
func shutdownSession(session *ClaudeSession, logger *slog.Logger) error {
	logger.Info("shutting down claude session",
		slog.String("session_id", session.ID),
	)
	return shutdownDaemonSession(session, logger, true)
}

// detachSession releases our local hold on a session without killing the
// underlying daemon worker, so it survives a grimoire restart and the user
// can re-attach next time.
func detachSession(session *ClaudeSession, logger *slog.Logger) error {
	return shutdownDaemonSession(session, logger, false)
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
