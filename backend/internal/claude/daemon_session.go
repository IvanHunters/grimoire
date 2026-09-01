package claude

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// discoverForkJSONL finds the JSONL file claude wrote for a fork
// session. claude --fork-session generates its own UUID (different
// from the one we passed to daemon.Dispatch), so the JSONL filename
// is unknown until we look on disk. Polls the cwd's sanitized project
// directory for files with mtime ≥ dispatchedAt, returns the newest
// matching UUID (basename without .jsonl). Returns "" on timeout or
// if discovery.ProjectsRoot isn't resolvable.
//
// Bounded poll: 6 attempts × 250ms = ~1.5s total wait. Claude usually
// writes its first JSONL line within a few hundred ms of dispatch.
func discoverForkJSONL(workingDir string, dispatchedAt time.Time, logger *slog.Logger) string {
	root, err := discovery.ProjectsRoot()
	if err != nil {
		return ""
	}
	dir := root + "/" + discovery.SanitizeCwd(workingDir)
	// Account for filesystem mtime resolution — subtract a small
	// fudge so files written in the same second qualify.
	cutoff := dispatchedAt.Add(-500 * time.Millisecond)
	for attempt := 0; attempt < 6; attempt++ {
		time.Sleep(250 * time.Millisecond)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var newest string
		var newestMtime time.Time
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
			if info.ModTime().After(newestMtime) {
				newestMtime = info.ModTime()
				newest = strings.TrimSuffix(name, ".jsonl")
			}
		}
		if newest != "" {
			return newest
		}
	}
	if logger != nil {
		logger.Warn("fork JSONL not discovered within timeout",
			slog.String("dir", dir),
		)
	}
	return ""
}

// getHistoricalPath resolves a session UUID to its on-disk JSONL path
// (thin wrapper around discovery.SessionPath, kept local for symmetry
// with readHistoricalHeader).
func getHistoricalPath(uuid string) (string, error) {
	return discovery.SessionPath(uuid)
}

// readHistoricalHeader pulls the header (name, cwd, gitBranch, ...) of
// an on-disk transcript. Used during resume to inherit the historical
// session's display name into our manager-side session.Name.
func readHistoricalHeader(path string) (discovery.TranscriptHeader, error) {
	tr, err := discovery.ReadTranscript(path)
	if err != nil {
		return discovery.TranscriptHeader{}, err
	}
	return tr.Header, nil
}

// startDaemonSession spawns a new background session via the claude daemon
// and attaches to it, returning a ClaudeSession whose PTY field is the
// AttachConn bridge. This is the daemon-backed counterpart to
// startClaudeSubprocess.
//
// The session keeps running in the daemon after grimoire restarts —
// reconnecting on next GetOrCreate is a matter of calling op:list +
// op:attach, not op:dispatch. (That reconnect path is wired into
// GetOrCreate via session-name lookup.)
func startDaemonSession(
	sessionID string,
	dangerousMode bool,
	workingDir string,
	mongoURI string,
	mongoDatabase string,
	logger *slog.Logger,
	systemPrompt string,
) (*ClaudeSession, error) {
	client := &daemon.Client{Logger: logger}

	// Verify daemon is reachable before spawning — fail fast with a clear
	// message instead of a cryptic socket error.
	ping, err := client.Ping()
	if err != nil {
		return nil, fmt.Errorf("daemon ping failed: %w (is `claude agents` running?)", err)
	}
	logger.Info("daemon ready",
		slog.String("version", ping.Version),
		slog.Int("proto", ping.Proto),
	)

	// MCP config: same flow as subprocess. The daemon will read it from
	// {workingDir}/.claude/settings.json on session start.
	mcpConfigPath, err := setupMCPConfig(sessionID, workingDir, mongoURI, mongoDatabase, logger)
	if err != nil {
		logger.Warn("failed to setup MCP config for daemon session", slog.Any("error", err))
	}

	// First, look for an existing daemon session for this grimoire ID
	// (survives a backend restart). Name convention: grimoire-<sessionID>.
	displayName := fmt.Sprintf("grimoire-%s", sessionID)

	if existing := findExistingDaemonSession(client, workingDir, displayName, logger); existing != nil {
		logger.Info("reattaching to existing daemon session",
			slog.String("session_id", sessionID),
			slog.String("daemon_short", existing.Short),
		)
		sess, err := attachDaemonSession(client, sessionID, displayName, *existing, workingDir, mcpConfigPath, dangerousMode, logger)
		if err == nil && sess != nil {
			// Reattach to a live worker that survived backend restart.
			// The SESSION CONTEXT was already pasted into the terminal
			// during the worker's original spawn — don't re-paste on
			// reconnect.
			sess.ContextPromptSent = true
		}
		return sess, err
	}

	// No existing session — dispatch a fresh one.
	// Peek pending dims without consuming so attachDaemonSession below
	// also gets them.
	pendingAttachDims.mu.Lock()
	dCols, dRows := 80, 24
	if v, ok := pendingAttachDims.m[sessionID]; ok {
		dCols, dRows = v.cols, v.rows
	}
	pendingAttachDims.mu.Unlock()
	opts := daemon.DispatchOpts{
		Cwd:  workingDir,
		Name: displayName,
		Cols: dCols,
		Rows: dRows,
		// Empty prompt → idle session; the first user message comes
		// through op:reply or the PTY attach write path.
	}
	// Inject grimoire-side env so MCP tools can identify themselves.
	opts.Env = map[string]string{
		"GRIMOIRE_SESSION_ID": sessionID,
		// CLAUDECODE removal isn't needed in dispatch — the daemon spawns
		// outside grimoire's env entirely.
	}

	disp, err := client.Dispatch(opts)
	if err != nil {
		return nil, fmt.Errorf("daemon dispatch: %w", err)
	}
	logger.Info("daemon dispatched new session",
		slog.String("session_id", sessionID),
		slog.String("daemon_short", disp.Short),
		slog.String("via", disp.Via),
	)

	// Give the daemon a beat to set up the rendezvous socket; attaching
	// too early returns ESTARTING.
	time.Sleep(500 * time.Millisecond)

	rec := daemon.Record{
		Short:     disp.Short,
		SessionID: disp.SessionID,
		Cwd:       workingDir,
		Name:      displayName,
	}
	return attachDaemonSession(client, sessionID, displayName, rec, workingDir, mcpConfigPath, dangerousMode, logger)
}

// startDaemonSessionResume spawns a daemon-backed session that continues
// an existing on-disk transcript via claude --resume <uuid>. The cwd
// MUST be the historical session's original working directory — claude
// finds the transcript by combining cwd with sessionId.
//
// When fork is true, the new session branches off with --fork-session:
// it gets its own UUID, the original session stays untouched. Use this
// for "explore an alternative path without losing the original chat"
// scenarios. fork=false (the default for Continue) keeps the same UUID
// and just appends to the existing conversation.
//
// Use this for the "Continue this session" button on a historical
// transcript view; for fresh sessions use startDaemonSession instead.
func startDaemonSessionResume(
	grimoireID string,
	resumeFromUUID string,
	workingDir string,
	mongoURI string,
	mongoDatabase string,
	logger *slog.Logger,
	sessionName string,
	fork bool,
) (*ClaudeSession, error) {
	client := &daemon.Client{Logger: logger}
	ping, err := client.Ping()
	if err != nil {
		return nil, fmt.Errorf("daemon ping: %w", err)
	}
	logger.Info("daemon ready for resume",
		slog.String("version", ping.Version),
		slog.String("resume_from", resumeFromUUID),
	)

	// MCP config is still useful when resuming — the session keeps the
	// same MCP server config as a fresh chat would.
	mcpConfigPath, err := setupMCPConfig(grimoireID, workingDir, mongoURI, mongoDatabase, logger)
	if err != nil {
		logger.Warn("setup mcp for resume", slog.Any("error", err))
	}

	// Daemon name is a STRUCTURED token used by listing.go's
	// splitResumeChildren to merge this resumed worker back into its
	// historical parent. Always "grimoire-resume-<full-uuid>" — UI never
	// sees this string. The human-readable name (sessionName arg, or
	// the historical session's name looked up below) goes into the
	// in-manager session.Name for display.
	daemonName := fmt.Sprintf("grimoire-resume-%s", resumeFromUUID)
	if fork {
		daemonName = fmt.Sprintf("grimoire-fork-%s", resumeFromUUID)
	}

	// Pick the display name. Caller may have passed one explicitly
	// (rare); otherwise inherit from the historical transcript so the
	// resumed session shows up in lists as "Equeo Project Overview"
	// rather than the structured "grimoire-resume-…" token.
	displayName := sessionName
	if displayName == "" {
		if hdrPath, err := getHistoricalPath(resumeFromUUID); err == nil {
			if hdr, err := readHistoricalHeader(hdrPath); err == nil && hdr.Name != "" && hdr.Name != "(unnamed)" {
				displayName = hdr.Name
			}
		}
	}
	if displayName == "" {
		displayName = daemonName // last-ditch fallback
	}

	// Reuse an already-live daemon worker if one is bound to the
	// resume-target session UUID. Two cases this covers:
	//   (a) After a backend restart our manager.sessions is empty but
	//       the daemon still hosts a worker named "grimoire-resume-<X>".
	//   (b) The TARGET session itself is still alive in the daemon
	//       (e.g. another grimoire ChatPanel is attached). claude
	//       --resume <X> would refuse with "currently running as bg"
	//       in this case — we must reattach instead of redispatching.
	//
	// Skip for fork — forks intentionally create a new session UUID
	// so collision with the original is fine.
	if !fork {
		expectedChildName := fmt.Sprintf("grimoire-resume-%s", resumeFromUUID)
		jobs, _ := client.ListSessions()
		for i := range jobs {
			j := jobs[i]
			// Skip only TRULY dead workers. "done" means the previous
			// prompt finished but the bg PTY is still alive and ready
			// for a new prompt — attach is exactly the right move
			// there. Without this, we'd fall through to `claude --resume`
			// which fails on bg-template sessions with "currently
			// running as a background agent (bg)".
			if j.State == "failed" || j.State == "stopped" {
				continue
			}
			// Match by either: the structured grimoire name we use for
			// resume children, OR the live session UUID itself
			// (catches case b above where the target session is held
			// by some other worker).
			matchesName := j.Name == expectedChildName
			matchesUUID := j.SessionID == resumeFromUUID
			if !matchesName && !matchesUUID {
				continue
			}
			logger.Info("reusing live daemon worker for resume target",
				slog.String("grimoire_id", grimoireID),
				slog.String("resume_from", resumeFromUUID),
				slog.String("daemon_short", j.Short),
				slog.String("daemon_session_id", j.SessionID),
				slog.String("daemon_name", j.Name),
				slog.String("display_name", displayName),
				slog.Bool("matched_by_name", matchesName),
				slog.Bool("matched_by_uuid", matchesUUID),
			)
			// Pass the readable displayName (not daemon's structured
			// name) so the in-manager session shows the right label.
			sess, err := attachDaemonSession(client, grimoireID, displayName, j, j.Cwd, mcpConfigPath, false, logger)
			if err == nil && sess != nil {
				// Reusing a live worker — context is already painted
				// in its PTY; don't re-paste a "SESSION CONTEXT" wall.
				sess.ContextPromptSent = true
				return sess, nil
			}
			// Attach failed — most commonly ENOJOB because op:list
			// reported a worker that exited between our list and our
			// attach (zombie). Fall through to fresh dispatch below
			// instead of failing the whole resume. The user's intent
			// was "give me the session", and the JSONL is still on
			// disk, so we can still satisfy it via a cold spawn.
			logger.Warn("reuse-attach failed, falling through to fresh dispatch",
				slog.String("daemon_short", j.Short),
				slog.String("daemon_session_id", j.SessionID),
				slog.Any("error", err),
			)
			break // exit the loop so we drop to fresh-dispatch below
		}
	}

	pendingAttachDims.mu.Lock()
	rCols, rRows := 80, 24
	if v, ok := pendingAttachDims.m[grimoireID]; ok {
		rCols, rRows = v.cols, v.rows
	}
	pendingAttachDims.mu.Unlock()
	opts := daemon.DispatchOpts{
		Cwd:             workingDir,
		Name:            daemonName, // structured token for merge logic
		ResumeSessionID: resumeFromUUID,
		Fork:            fork,
		Cols:            rCols,
		Rows:            rRows,
		Env: map[string]string{
			"GRIMOIRE_SESSION_ID": grimoireID,
		},
	}

	dispatchedAt := time.Now()
	disp, err := client.Dispatch(opts)
	if err != nil {
		return nil, fmt.Errorf("dispatch resume: %w", err)
	}
	logger.Info("daemon resumed session",
		slog.String("grimoire_id", grimoireID),
		slog.String("resume_from", resumeFromUUID),
		slog.String("daemon_short", disp.Short),
		slog.String("new_session_id", disp.SessionID),
	)

	// For forks: claude --fork-session ignores our opts.SessionID and
	// generates its OWN UUID, writing JSONL at ~/.claude/projects/<cwd>/<real-uuid>.jsonl
	// — NOT under disp.SessionID. If we leave DaemonUUID=disp.SessionID,
	// the listing shows TWO rows for one conversation: the live
	// row keyed by disp.SessionID AND the historical row keyed by the
	// real UUID. Discover the real UUID by polling the cwd dir for
	// the freshly-written JSONL (mtime > dispatchedAt) and rewrite
	// disp.SessionID to that.
	if fork {
		if realUUID := discoverForkJSONL(workingDir, dispatchedAt, logger); realUUID != "" {
			logger.Info("fork real session UUID discovered",
				slog.String("dispatched_uuid", disp.SessionID),
				slog.String("real_uuid", realUUID),
			)
			disp.SessionID = realUUID
		}
		// Write a sidecar marker so listing can flag the row with a
		// "fork" badge — same pattern as `.imported`.
		if path, err := discovery.SessionPath(disp.SessionID); err == nil && path != "" {
			marker := strings.TrimSuffix(path, ".jsonl") + ".fork"
			if f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644); err == nil {
				_ = f.Close()
			}
		}
	}

	// Brief beat for daemon to set up the rendezvous socket — attaching
	// too early returns ESTARTING. We INTENTIONALLY don't wait for
	// claude to finish rendering the historical TUI: long waits mean
	// daemon buffers a big chunk of PTY bytes that gets dumped at
	// attach time in one shot. The bulk dump occasionally lands
	// mid-ANSI-escape and corrupts xterm display until subsequent
	// bytes "rescue" it. Short wait → smaller dump → cleaner paint.
	time.Sleep(400 * time.Millisecond)

	// For non-fork resume, claude --resume <X> appends to the ORIGINAL
	// X.jsonl — the session UUID does NOT change. But the daemon's
	// op:dispatch reply sometimes hands back a fresh daemon-assigned
	// UUID in `sessionId` that confuses our identity tracking. Force
	// rec.SessionID to the resumed-from UUID so the next restart can
	// still find X.jsonl via discovery.SessionPath. Without this, the
	// SECOND restart fails with "no transcript yet" and falls through
	// to fresh-spawn → empty terminal, even though X.jsonl is right
	// there on disk.
	canonicalUUID := disp.SessionID
	if !fork && resumeFromUUID != "" {
		canonicalUUID = resumeFromUUID
	}
	rec := daemon.Record{
		Short:     disp.Short,
		SessionID: canonicalUUID,
		Cwd:       workingDir,
		Name:      displayName,
	}
	return attachDaemonSession(client, grimoireID, displayName, rec, workingDir, mcpConfigPath, false, logger)
}

// startDaemonSessionAttach opens an attach bridge to a session already
// hosted by the daemon. Unlike spawn / resume, no dispatch happens —
// the daemon's existing worker keeps running and our AttachConn just
// joins its TUI alongside any other attachers.
//
// Looks up the daemon record by full UUID (must match the JSONL
// filename and the daemon's op:list reply). Returns an error when the
// session isn't currently alive in the daemon — caller should fall
// back to --resume against an on-disk JSONL if that path makes sense.
func startDaemonSessionAttach(
	grimoireID string,
	daemonSessionUUID string,
	logger *slog.Logger,
) (*ClaudeSession, error) {
	client := &daemon.Client{Logger: logger}

	// Find the live record so we have its short id and cwd.
	jobs, err := client.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list daemon sessions: %w", err)
	}
	var rec *daemon.Record
	for i := range jobs {
		if jobs[i].SessionID == daemonSessionUUID {
			rec = &jobs[i]
			break
		}
	}
	// Fallback: caller passed the HISTORICAL parent UUID, but the
	// live worker is its resume-child (named "grimoire-resume-<short>"
	// where short is the first 8 hex chars of the parent UUID). The
	// listing merges these as one row, so the frontend naturally uses
	// the parent UUID for attach. Map back to the child here.
	if rec == nil && len(daemonSessionUUID) >= 8 {
		want := "grimoire-resume-" + daemonSessionUUID
		for i := range jobs {
			if jobs[i].Name == want {
				logger.Info("attach resolved historical UUID to resume-child",
					slog.String("historical_uuid", daemonSessionUUID),
					slog.String("child_uuid", jobs[i].SessionID),
					slog.String("child_short", jobs[i].Short),
				)
				rec = &jobs[i]
				break
			}
		}
	}
	if rec == nil {
		return nil, fmt.Errorf("session %s not live in daemon", daemonSessionUUID)
	}

	logger.Info("attaching to existing daemon session",
		slog.String("grimoire_id", grimoireID),
		slog.String("daemon_uuid", daemonSessionUUID),
		slog.String("daemon_short", rec.Short),
		slog.String("name", rec.Name),
	)

	// Pick a readable display name. Three sources, in priority order:
	//   1. Daemon's name (only if it's not our structured "grimoire-*"
	//      token — those are useless for humans)
	//   2. Historical JSONL's ai-title (looked up by grimoireID, which
	//      for resume-flows equals the original session UUID)
	//   3. Fallback: "attach-<short>"
	displayName := rec.Name
	if strings.HasPrefix(displayName, "grimoire-") {
		displayName = ""
	}
	if displayName == "" {
		if hdrPath, err := getHistoricalPath(grimoireID); err == nil {
			if hdr, err := readHistoricalHeader(hdrPath); err == nil && hdr.Name != "" && hdr.Name != "(unnamed)" {
				displayName = hdr.Name
			}
		}
	}
	if displayName == "" {
		displayName = fmt.Sprintf("attach-%s", rec.Short)
	}
	sess, err := attachDaemonSession(client, grimoireID, displayName, *rec, rec.Cwd, "", false, logger)
	if err == nil && sess != nil {
		// We're attaching to a worker that already exists in the
		// daemon — its terminal already shows whatever context was
		// pasted at original spawn. Don't re-inject.
		sess.ContextPromptSent = true
	}
	return sess, err
}

// findExistingDaemonSession looks for a live session in our working dir
// whose name matches displayName. This is how a grimoire restart finds
// the chat that was running before the crash.
func findExistingDaemonSession(client *daemon.Client, workingDir, displayName string, logger *slog.Logger) *daemon.Record {
	jobs, err := client.ListSessions()
	if err != nil {
		logger.Warn("daemon list failed during reattach probe", slog.Any("error", err))
		return nil
	}
	for i := range jobs {
		j := jobs[i]
		if j.Name == displayName && j.Cwd == workingDir {
			// Make sure it's not failed/stopped — attach refuses those.
			if j.State == "failed" || j.State == "stopped" || j.State == "done" {
				logger.Info("found prior session but it's in terminal state, will respawn",
					slog.String("daemon_short", j.Short),
					slog.String("state", j.State),
				)
				return nil
			}
			return &j
		}
	}
	return nil
}

// pendingAttachDims is a per-grimoireID hint set by the handler from
// the WS init payload right before calling into the manager. The
// attach + dispatch paths read it to size the daemon worker correctly
// from the start, instead of using the static 80x24 default. We use a
// package-level map keyed by sessionID to avoid threading cols/rows
// through every manager/startDaemon* signature.
type pendingDimsEntry struct {
	cols, rows int
	stored     time.Time
}

var pendingAttachDims = struct {
	mu sync.Mutex
	m  map[string]pendingDimsEntry
}{m: map[string]pendingDimsEntry{}}

// pendingDimsTTL bounds how long a stashed (cols, rows) hint is kept
// when nothing ever consumes it. Without this, every WS connect that
// stashed dims but failed to proceed to attach (network drop between
// init and op:attach, frontend bail-out) would leak the entry forever.
const pendingDimsTTL = 60 * time.Second

// gcPendingDims drops entries older than pendingDimsTTL. Called
// opportunistically from SetPendingDims so the cleanup amortises over
// normal traffic — no background goroutine needed.
func gcPendingDims(now time.Time) {
	for id, v := range pendingAttachDims.m {
		if now.Sub(v.stored) > pendingDimsTTL {
			delete(pendingAttachDims.m, id)
		}
	}
}

// SetPendingDims stores cols/rows for the next session creation
// keyed by grimoireID. Cleared by ConsumePendingDims after use, or
// by the gcPendingDims sweep after pendingDimsTTL if never consumed.
func SetPendingDims(sessionID string, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	now := time.Now()
	pendingAttachDims.mu.Lock()
	gcPendingDims(now)
	pendingAttachDims.m[sessionID] = pendingDimsEntry{cols: cols, rows: rows, stored: now}
	pendingAttachDims.mu.Unlock()
}

// ConsumePendingDims returns the stored cols/rows for this sessionID
// (or 80x24 default if none), and removes the entry. Safe to call
// even when nothing was stored.
func ConsumePendingDims(sessionID string) (cols, rows int) {
	pendingAttachDims.mu.Lock()
	defer pendingAttachDims.mu.Unlock()
	if v, ok := pendingAttachDims.m[sessionID]; ok {
		delete(pendingAttachDims.m, sessionID)
		return v.cols, v.rows
	}
	return 80, 24
}

// attachDaemonSession opens an AttachConn against the daemon, builds the
// ClaudeSession, and starts the reader goroutine. Reusable for both
// fresh-dispatch and reattach flows.
func attachDaemonSession(
	client *daemon.Client,
	sessionID string,
	displayName string,
	rec daemon.Record,
	workingDir string,
	mcpConfigPath string,
	dangerousMode bool,
	logger *slog.Logger,
) (*ClaudeSession, error) {
	// Use frontend-supplied cols/rows when the handler stashed them
	// via SetPendingDims; otherwise fall back to daemon's 80x24
	// default. Matching the actual xterm size at attach prevents
	// SIGWINCH-driven repaints from invalidating scrollback that was
	// just written at the wrong width.
	cols, rows := ConsumePendingDims(sessionID)
	ac, err := client.Attach(rec.Short, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("daemon attach (short=%s): %w", rec.Short, err)
	}

	now := time.Now()
	session := &ClaudeSession{
		ID:            sessionID,
		Name:          displayName,
		Cmd:           nil, // not subprocess-owned
		PTY:           ac,  // *daemon.AttachConn satisfies io.ReadWriteCloser
		DangerousMode: dangerousMode,
		WorkingDir:    workingDir,
		MCPConfigPath: mcpConfigPath,
		CreatedAt:     now,
		LastActivity:  now,
		Messages:      make([]models.ClaudeMessage, 0),
		OutputBuffer:  make([]byte, 0, 1024),

		DaemonClient: client,
		DaemonShort:  rec.Short,
		DaemonUUID:   rec.SessionID,
	}

	// Same reader goroutine as subprocess sessions — reads from
	// session.PTY (which is the AttachConn) and broadcasts.
	go startPTYReader(session, logger)

	logger.Info("daemon-backed session ready",
		slog.String("session_id", sessionID),
		slog.String("daemon_short", rec.Short),
		slog.String("daemon_uuid", rec.SessionID),
	)
	return session, nil
}
