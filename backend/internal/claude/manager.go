package claude

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// histNameCache caches the result of lookupHistoricalNameByShort.
// Sidebar polls /api/sessions every 3s; without this every poll did
// filepath.Glob + ReadHeader per session — multi-millisecond syscall
// storm. JSONL ai-titles are effectively immutable once set, so a
// 30s TTL is safe; rename via UpsertSessionName flows through Mongo
// overlay (different code path) and is not affected.
type histNameCacheEntry struct {
	name     string
	cachedAt time.Time
}

var (
	histNameCache    sync.Map // map[prefix8]histNameCacheEntry
	histNameCacheTTL = 30 * time.Second
)

// jsonlMtimeCache caches discovery.SessionPath + os.Stat results per
// daemon UUID. ListActiveSessions stat's every daemon-backed session's
// JSONL on every poll to compute "stale" tempo override; without cache
// that's N syscalls every 3s.
type jsonlMtimeCacheEntry struct {
	mtime    time.Time
	cachedAt time.Time
	ok       bool // false ⇒ SessionPath miss; don't re-glob for 10s
}

var (
	jsonlMtimeCache    sync.Map // map[uuid]jsonlMtimeCacheEntry
	jsonlMtimeCacheTTL = 10 * time.Second
)

// daemonJobsCache caches a recent op:list response across SessionStatus
// calls. Sidebar polling fans out N=open-sessions calls every few
// seconds — without this, every poll opens a unix socket, sends an
// op:list, and parses the daemon's full job listing. Cache TTL is
// short (~1s) so a job finishing remains visible quickly.
type daemonJobsCacheEntry struct {
	jobs    []daemon.Record
	fetched time.Time
}

var (
	daemonJobsCacheMu  sync.Mutex
	daemonJobsCacheVal daemonJobsCacheEntry
	daemonJobsCacheTTL = 1 * time.Second
)

// listSessionsCached returns the daemon's op:list result, refreshing
// from the daemon only when the cached snapshot is older than
// daemonJobsCacheTTL. A single inflight refresh is serialised by the
// mutex so a burst of N concurrent SessionStatus callers triggers
// exactly one op:list.
func listSessionsCached(client *daemon.Client) ([]daemon.Record, error) {
	daemonJobsCacheMu.Lock()
	defer daemonJobsCacheMu.Unlock()
	if time.Since(daemonJobsCacheVal.fetched) < daemonJobsCacheTTL && daemonJobsCacheVal.jobs != nil {
		return daemonJobsCacheVal.jobs, nil
	}
	jobs, err := client.ListSessions()
	if err != nil {
		return nil, err
	}
	daemonJobsCacheVal = daemonJobsCacheEntry{jobs: jobs, fetched: time.Now()}
	return jobs, nil
}

// lookupHistoricalNameByShort scans every JSONL filename for one
// starting with `prefix` and returns its display name (ai-title or
// first-prompt fallback). Cached for histNameCacheTTL.
func lookupHistoricalNameByShort(prefix string) string {
	if len(prefix) < 8 {
		return ""
	}
	if v, ok := histNameCache.Load(prefix); ok {
		entry := v.(histNameCacheEntry)
		if time.Since(entry.cachedAt) < histNameCacheTTL {
			return entry.name
		}
	}
	name := lookupHistoricalNameByShortUncached(prefix)
	histNameCache.Store(prefix, histNameCacheEntry{name: name, cachedAt: time.Now()})
	return name
}

func lookupHistoricalNameByShortUncached(prefix string) string {
	root, err := discovery.ProjectsRoot()
	if err != nil {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", prefix+"*.jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	hdr, err := discovery.ReadHeader(matches[0])
	if err != nil {
		return ""
	}
	return hdr.Name
}

// jsonlMtimeFor returns the JSONL mtime for the given daemon UUID,
// using a short TTL cache. ok=false means the JSONL doesn't exist
// (or path lookup failed); callers should treat that as "no stale
// timer running" rather than retry.
func jsonlMtimeFor(daemonUUID string) (mtime time.Time, ok bool) {
	if daemonUUID == "" {
		return time.Time{}, false
	}
	if v, ok := jsonlMtimeCache.Load(daemonUUID); ok {
		entry := v.(jsonlMtimeCacheEntry)
		if time.Since(entry.cachedAt) < jsonlMtimeCacheTTL {
			return entry.mtime, entry.ok
		}
	}
	var entry jsonlMtimeCacheEntry
	entry.cachedAt = time.Now()
	if path, err := discovery.SessionPath(daemonUUID); err == nil {
		if info, statErr := os.Stat(path); statErr == nil {
			entry.mtime = info.ModTime()
			entry.ok = true
		}
	}
	jsonlMtimeCache.Store(daemonUUID, entry)
	return entry.mtime, entry.ok
}

// isUUIDLike reports whether s has the canonical 8-4-4-4-12 UUID shape.
// We treat such sessionIDs as "potentially identifying a claude daemon
// session" — clickers from SessionsModal / Sidebar carry full UUIDs.
// Grimoire-managed sessions usually have prefixes like "note-" / "global-",
// so this filter avoids false positives for our own keys.
func isUUIDLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// useDaemonBackend reports whether new sessions should be hosted by the
// claude daemon instead of an in-process subprocess. Toggled by the
// USE_DAEMON_BACKEND env var (any non-empty value enables).
//
// This is intentionally an env-only switch for now — daemon-backend is
// dev-validated, not yet a user-facing setting. When we're confident,
// we'll flip the default and the flag becomes inverted (USE_SUBPROCESS).
func useDaemonBackend() bool {
	return os.Getenv("USE_DAEMON_BACKEND") != ""
}

// SessionStorage interface for persisting sessions
type SessionStorage interface {
	SaveSession(ctx context.Context, session *models.ClaudeSession) error
	GetSession(ctx context.Context, sessionID string) (*models.ClaudeSession, error)
	UpdateSessionStatus(ctx context.Context, sessionID string, status string) error
	UpdateSessionActivity(ctx context.Context, sessionID string) error
	UpdateSessionName(ctx context.Context, sessionID string, name string) error
	UpdateSessionMessages(ctx context.Context, sessionID string, messages []models.ClaudeMessage) error
	ListActiveSessions(ctx context.Context) ([]*models.ClaudeSession, error)
	// ListNameOverrides returns user-chosen display names keyed by
	// sessionID. Used by rehydrate / GetOrCreate to apply the user's
	// preferred name over claude's auto-generated continuation titles.
	ListNameOverrides(ctx context.Context) (map[string]string, error)
}

// SessionManager manages all Claude sessions
type SessionManager struct {
	sessions      map[string]*ClaudeSession
	storage       SessionStorage
	mongoURI      string
	mongoDatabase string
	mu            sync.RWMutex
	logger        *slog.Logger
}

// Global session manager instance
var globalManager *SessionManager
var managerOnce sync.Once

// SweepOrphanWorkers kills daemon workers that look orphan: name
// starts with our structured "grimoire-" prefix AND no on-disk JSONL
// exists for the worker's session UUID (so it never wrote anything,
// can't be resumed, has no chat history to preserve). These leak
// across backend restarts when claude --bg dispatches were never
// typed into. Call once at startup so the sidebar doesn't show
// useless "···<short>" placeholders.
func SweepOrphanWorkers(logger *slog.Logger) {
	client := &daemon.Client{Logger: logger}
	jobs, err := client.ListSessions()
	if err != nil {
		logger.Warn("orphan sweep: list failed", slog.Any("error", err))
		return
	}
	killed := 0
	for _, j := range jobs {
		if !strings.HasPrefix(j.Name, "grimoire-") {
			continue // someone else's worker; leave alone
		}
		// Check if a JSONL exists for this worker. If yes, it had real
		// activity — preserve it.
		if path, err := discovery.SessionPath(j.SessionID); err == nil && path != "" {
			continue
		}
		// Empty grimoire-* worker. Kill it.
		if err := client.Remove(j.Short); err != nil {
			logger.Debug("orphan kill failed",
				slog.String("short", j.Short),
				slog.String("name", j.Name),
				slog.Any("error", err),
			)
			continue
		}
		killed++
	}
	if killed > 0 {
		logger.Info("orphan workers swept on startup",
			slog.Int("killed", killed),
			slog.Int("total_jobs", len(jobs)),
		)
	}
}

// RehydrateFromDaemon scans the live daemon worker list and creates
// in-memory ClaudeSession entries for each grimoire-* worker. Called
// once at backend startup so the sidebar shows existing live workers
// immediately, instead of waiting for the user to click each one
// before the in-memory manager learns about it.
//
// What it covers:
//   - `grimoire-<grimoireID>` — recovers full grimoire-side identity
//     (e.g. `grimoire-note-task-bcc3100a-...` → manager keyed by
//     `note-task-bcc3100a-...`). Click routes through ChatPanel
//     correctly.
//   - `grimoire-resume-<short>` / `grimoire-fork-<short>` — we don't
//     know the original grimoire id, so we key by the worker's session
//     UUID. Sidebar shows it under its daemon name or user-overlay.
//
// Skipped: workers without a `grimoire-*` name (foreign claude
// processes) and workers in terminal state (failed/stopped/done with
// no live PTY).
//
// The attach connection opens immediately so PTY output keeps flowing
// from the moment the user reconnects. Cheap: one socket + reader
// goroutine per session, dwarfed by daemon's own memory footprint.
func (m *SessionManager) RehydrateFromDaemon(logger *slog.Logger) {
	if m == nil {
		return
	}
	client := &daemon.Client{Logger: logger}
	jobs, err := client.ListSessions()
	if err != nil {
		logger.Warn("rehydrate: daemon list failed (skipping)",
			slog.Any("error", err))
		return
	}

	// Pre-fetch Mongo name overlay so the manager session shows the
	// user-given name (e.g. "Bonding + TestFlight deploy") instead of
	// the claude-auto-generated JSONL ai-title which often defaults to
	// "This session is being continued..." after compact cycles.
	nameOverlay := map[string]string{}
	if m.storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if m, err := m.storage.ListNameOverrides(ctx); err == nil {
			nameOverlay = m
		}
	}

	var rehydrated, skipped int
	for i := range jobs {
		j := jobs[i]

		// Skip foreign workers (other claude clients on this machine).
		if !strings.HasPrefix(j.Name, "grimoire-") {
			skipped++
			continue
		}
		// Skip terminal-state workers — attach would refuse them.
		if j.State == "failed" || j.State == "stopped" || j.State == "done" {
			skipped++
			continue
		}

		// SKIP continuation-children workers (grimoire-resume-* /
		// grimoire-fork-*). These are claude --resume / --fork
		// sub-workers whose presence here would race with the
		// canonical entry the user opens via UI. The smart-attach
		// path in GetOrCreate finds them lazily on demand by name
		// pattern — so they appear in the sidebar with the SAME
		// canonical grimoire id (no duplicate row).
		//
		// Without this skip: each backend bounce + each compact cycle
		// surfaces continuation-child workers as separate manager
		// entries named "This session is being continued from a
		// previous conversation..." — the persistent dup bug.
		if strings.HasPrefix(j.Name, "grimoire-resume-") || strings.HasPrefix(j.Name, "grimoire-fork-") {
			skipped++
			continue
		}
		grimoireID := j.SessionID
		if strings.HasPrefix(j.Name, "grimoire-") {
			derived := strings.TrimPrefix(j.Name, "grimoire-")
			if derived != "" {
				grimoireID = derived
			}
		}

		// Don't double-add. Two ways an entry might already exist:
		//   1. Same grimoireID — second rehydrate or WS-init race
		//   2. Different grimoireID but SAME DaemonShort — happens
		//      when a user MCP-resumed a session (manager keyed by
		//      canonical resume-source UUID), AND the daemon worker
		//      shows up here under its daemon-assigned UUID. Without
		//      this check, both end up in the sidebar as duplicates.
		m.mu.RLock()
		alreadyPresent := false
		if _, ok := m.sessions[grimoireID]; ok {
			alreadyPresent = true
		} else {
			for _, existing := range m.sessions {
				if existing.DaemonShort != "" && existing.DaemonShort == j.Short {
					alreadyPresent = true
					break
				}
			}
		}
		m.mu.RUnlock()
		if alreadyPresent {
			skipped++
			continue
		}

		// Display name: prefer the daemon's name when it's NOT one of
		// our structured tokens. Otherwise leave empty and let the
		// frontend / Mongo overlay paint a better label.
		displayName := j.Name
		if strings.HasPrefix(displayName, "grimoire-resume-") ||
			strings.HasPrefix(displayName, "grimoire-fork-") ||
			displayName == fmt.Sprintf("grimoire-%s", grimoireID) {
			displayName = ""
		}

		sess, attachErr := attachDaemonSession(client, grimoireID, displayName, j, j.Cwd, "", false, logger)
		if attachErr != nil {
			logger.Warn("rehydrate: attach failed",
				slog.String("daemon_short", j.Short),
				slog.String("daemon_uuid", j.SessionID),
				slog.Any("error", attachErr),
			)
			skipped++
			continue
		}
		// CRITICAL: recover canonical DaemonUUID for resume/fork workers.
		// Daemon recycles worker UUIDs across restarts — rec.SessionID is
		// the LIVE worker's UUID, NOT the resume-source JSONL UUID. Without
		// this, restart's "DaemonUUID drift detected, recovered via
		// newest-jsonl-in-cwd" fallback kicks in and (when cwd contains
		// multiple historical JSONLs) silently picks the WRONG transcript,
		// leading to identity confusion in the sidebar:
		//   user kills bonding session → backend rehydrate → restart picks
		//   linstor.jsonl because both share cozystack/ cwd → manager
		//   relabels session as "Linstor". User opens "Bonding" — sees
		//   Linstor.
		// The worker's name carries the original SHORT (`grimoire-resume-XX`).
		// Resolve that short to a full canonical UUID via disk lookup.
		if strings.HasPrefix(j.Name, "grimoire-resume-") || strings.HasPrefix(j.Name, "grimoire-fork-") {
			short := j.Name
			short = strings.TrimPrefix(short, "grimoire-resume-")
			short = strings.TrimPrefix(short, "grimoire-fork-")
			if canonicalUUID := resolveJSONLByShort(j.Cwd, short, logger); canonicalUUID != "" {
				sess.DaemonUUID = canonicalUUID
				logger.Info("rehydrate: canonical DaemonUUID recovered from short",
					slog.String("grimoire_id", grimoireID),
					slog.String("worker_session_id", j.SessionID),
					slog.String("canonical_jsonl_uuid", canonicalUUID),
					slog.String("short", short),
				)
			}
		}
		// Mark ContextPromptSent so a downstream WS reconnect doesn't
		// re-paste the SESSION CONTEXT wall into the PTY. The worker
		// has been running with its own context for a while — we're
		// just re-grabbing the reader.
		sess.ContextPromptSent = true

		// LastActivity baseline: when daemon's worker started.
		// attachDaemonSession sets it to "now" (the rehydrate moment),
		// which would mask the stale-active downgrade (sessions seem
		// "fresh" even when the underlying worker has been idle for
		// hours). Use startedAt instead. The PTY reader will bump it
		// to real time on the next byte emitted.
		if j.StartedAt > 0 {
			sess.LastActivity = time.UnixMilli(j.StartedAt)
		}
		// Apply Mongo name overlay — keyed by grimoireID first, then
		// daemon UUID. The user's chosen name beats anything claude
		// auto-wrote into JSONL (e.g. continuation titles).
		if name, ok := nameOverlay[grimoireID]; ok && name != "" {
			sess.Name = name
		} else if name, ok := nameOverlay[j.SessionID]; ok && name != "" {
			sess.Name = name
		}

		m.mu.Lock()
		m.sessions[grimoireID] = sess
		m.mu.Unlock()
		rehydrated++
	}

	logger.Info("rehydrate from daemon complete",
		slog.Int("rehydrated", rehydrated),
		slog.Int("skipped", skipped),
		slog.Int("total_workers", len(jobs)),
	)
}

// resolveJSONLByShort scans the cwd's sanitized projects/ directory
// for a JSONL whose filename starts with `short` (8-char prefix from
// the daemon's `grimoire-resume-<short>` worker name). Returns the
// full UUID basename or "". Used by rehydrate to map a recycled
// worker UUID back to its canonical transcript on disk.
//
// Multi-match resolution: when more than one JSONL starts with the
// short (collision is statistically possible after a few thousand
// sessions), the most-recently-modified wins.
func resolveJSONLByShort(cwd, short string, logger *slog.Logger) string {
	if cwd == "" || short == "" {
		return ""
	}
	root, err := discovery.ProjectsRoot()
	if err != nil {
		return ""
	}
	dir := root + "/" + discovery.SanitizeCwd(cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestMtime time.Time
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, short) {
			continue
		}
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if strings.Contains(name, ".archive.") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestMtime) {
			newestMtime = info.ModTime()
			newest = strings.TrimSuffix(name, ".jsonl")
		}
	}
	if newest != "" {
		logger.Debug("resolveJSONLByShort: hit",
			slog.String("short", short),
			slog.String("cwd", cwd),
			slog.String("uuid", newest),
		)
	}
	return newest
}

// GetSessionManager returns the global session manager instance
func GetSessionManager(logger *slog.Logger, storage SessionStorage, mongoURI string, mongoDatabase string) *SessionManager {
	managerOnce.Do(func() {
		globalManager = &SessionManager{
			sessions:      make(map[string]*ClaudeSession),
			storage:       storage,
			mongoURI:      mongoURI,
			mongoDatabase: mongoDatabase,
			logger:        logger,
		}
		// Hook the listing package so it can mark sessions that are
		// alive in our manager as "live" even when the daemon worker
		// has paused. Closes the consistency gap between SessionsModal
		// "Active now" and what the user has actually open.
		SetManagedLiveProvider(func() map[string]bool {
			globalManager.mu.RLock()
			defer globalManager.mu.RUnlock()
			out := make(map[string]bool, len(globalManager.sessions))
			for id := range globalManager.sessions {
				out[id] = true
			}
			return out
		})
		// Daemon UUIDs of every managed session. Listing dedups its
		// daemon-side rows against this so the live worker of an open
		// chat doesn't appear as a separate "···<short>" orphan row
		// (deleting which would kill the active chat).
		SetManagedDaemonUUIDProvider(func() map[string]bool {
			globalManager.mu.RLock()
			defer globalManager.mu.RUnlock()
			out := make(map[string]bool, len(globalManager.sessions))
			for _, s := range globalManager.sessions {
				if s.DaemonUUID != "" {
					out[s.DaemonUUID] = true
				}
			}
			return out
		})
		// daemonUUID → {grimoireID, Name}. Listing rewrites its
		// daemon-side rows to use the grimoire-side ID (so click
		// routes through our manager) and the user-given name (so
		// fresh forks show "MyForkName" instead of "···short").
		SetManagedSessionInfoProvider(func() map[string]ManagedSessionInfo {
			globalManager.mu.RLock()
			defer globalManager.mu.RUnlock()
			out := make(map[string]ManagedSessionInfo, len(globalManager.sessions))
			for _, s := range globalManager.sessions {
				if s.DaemonUUID == "" {
					continue
				}
				out[s.DaemonUUID] = ManagedSessionInfo{
					GrimoireID: s.ID,
					Name:       s.Name,
				}
			}
			return out
		})
	})
	return globalManager
}

// GetOrAttach attaches to an EXISTING daemon worker by its full UUID,
// without spawning a new session. The daemon must already host this
// session — typically because the user (or another grimoire instance)
// dispatched it earlier and it's still running in op:list.
//
// Unlike GetOrResume (which uses claude --resume to start a fresh
// process from an on-disk transcript), this just opens an attach
// bridge to the live PTY. No new process, no new conversation —
// continuing the same one in real time, possibly alongside another
// attacher viewing the same TUI.
//
// Returns a clear error when the session isn't found in the daemon's
// live registry (caller should fall back to GetOrResume in that case).
// Only works with USE_DAEMON_BACKEND=1.
// lookupLiveAttach returns the cached session for grimoireID only when it
// is alive (PTY present) AND bound to the requested daemon worker. The
// UUID check is essential: quick terminals share one cwd
// (~/.markdown-editor/sessions/default) and collide on grimoireID, so a
// bare grimoireID hit could hand back a DIFFERENT worker's live PTY —
// that is why clicking session A opened session B's conversation. Returns
// nil when there is no usable cached entry (missing, dead PTY, or a
// different UUID), signalling the caller to do a fresh daemon attach.
func (m *SessionManager) lookupLiveAttach(grimoireID, daemonSessionUUID string) *ClaudeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[grimoireID]; ok && s.PTY != nil && s.DaemonUUID == daemonSessionUUID {
		return s
	}
	return nil
}

func (m *SessionManager) GetOrAttach(grimoireID string, daemonSessionUUID string) (*ClaudeSession, error) {
	if !useDaemonBackend() {
		return nil, fmt.Errorf("attach requires USE_DAEMON_BACKEND=1")
	}
	if daemonSessionUUID == "" {
		return nil, fmt.Errorf("attach needs daemonSessionUUID")
	}

	// Fast path: entry in manager AND its PTY is still live. PTY==nil
	// means startPTYReader exited (daemon supervisor killed / network
	// broke / worker died) and the manager entry is a stub — re-attach
	// instead of handing the caller a zombie session whose subscribers
	// never receive PTY output. Without this check the browser appears
	// frozen after every daemon-restart cycle and only "restart
	// session" fixes it.
	if session := m.lookupLiveAttach(grimoireID, daemonSessionUUID); session != nil {
		session.UpdateActivity()
		return session, nil
	}

	newSession, err := startDaemonSessionAttach(grimoireID, daemonSessionUUID, m.logger)
	if err != nil {
		return nil, fmt.Errorf("daemon attach: %w", err)
	}

	m.mu.Lock()
	if existing, raced := m.sessions[grimoireID]; raced && existing.PTY != nil && existing.DaemonUUID == daemonSessionUUID {
		m.mu.Unlock()
		// Race-loser path: both attaches target the same daemon worker
		// (same UUID), they only differ in AttachConn. shutdownSession
		// would op:kill the shared worker and tear down the race-winner's
		// session too — use detachSession so only our local AttachConn
		// is closed. PTY!=nil check ensures we only defer to the existing
		// entry when it's actually alive — if it's a stub we WANT to
		// replace it with our fresh attach.
		go detachSession(newSession, m.logger)
		return existing, nil
	}
	m.sessions[grimoireID] = newSession
	m.mu.Unlock()
	return newSession, nil
}

// GetOrResume gets or spawns a session that continues a historical claude
// conversation. Unlike GetOrCreate which always starts fresh, this passes
// --resume <resumeFromUUID> to claude so the daemon-backed session has
// the full prior transcript in context.
//
// resumeFromUUID is the historical session's UUID — same as the JSONL
// filename under ~/.claude/projects/. workingDir MUST be the cwd that
// historical session ran in (claude resolves the transcript via cwd +
// session id). The caller is responsible for looking that up via
// discovery.SessionPath + discovery.ReadHeader.
//
// When fork is true, --fork-session is added so the new chat branches
// off a copy of the transcript and gets its own UUID; the original
// session stays untouched.
//
// Only works with USE_DAEMON_BACKEND=1. Subprocess backend doesn't
// support resume in this code path yet; returns an explicit error.
func (m *SessionManager) GetOrResume(grimoireID string, resumeFromUUID string, workingDir string, sessionName string, fork bool) (*ClaudeSession, error) {
	if !useDaemonBackend() {
		return nil, fmt.Errorf("resume requires USE_DAEMON_BACKEND=1")
	}
	if resumeFromUUID == "" || workingDir == "" {
		return nil, fmt.Errorf("resume needs both resumeFromUUID and workingDir")
	}

	// Fast path: already in memory AND has a live PTY. ShutdownWorker
	// keeps the entry but nulls PTY/Cmd/DaemonShort to mark "killed
	// worker, awaiting respawn"; returning that without re-attach
	// gives the caller a dead session whose writes fail silently.
	m.mu.RLock()
	if session, ok := m.sessions[grimoireID]; ok {
		alive := session.PTY != nil
		m.mu.RUnlock()
		if alive {
			// Self-heal: earlier code paths stuffed the structured
			// "grimoire-resume-<short>" token into session.Name, which
			// shows up in Sidebar as gibberish. If we land on a fast-path
			// session that still has that token, refresh from the resume
			// caller's nicer name (or leave alone if no better option).
			current := session.GetName()
			if strings.HasPrefix(current, "grimoire-resume-") || strings.HasPrefix(current, "grimoire-fork-") {
				if sessionName != "" && !strings.HasPrefix(sessionName, "grimoire-") {
					session.SetName(sessionName)
				}
			}
			session.UpdateActivity()
			return session, nil
		}
		// PTY is nil → entry is in "killed worker, awaiting respawn"
		// state (e.g. post-compact ShutdownWorker). Fall through and
		// dispatch a fresh worker, then REPLACE the stub entry below.
		m.logger.Info("GetOrResume: stub entry detected (post-shutdown), respawning",
			slog.String("grimoire_id", grimoireID),
			slog.String("resume_from", resumeFromUUID),
		)
	} else {
		m.mu.RUnlock()
	}

	// If the session is ALREADY running as a live daemon worker (a bg
	// agent), attach to it instead of spawning `claude --resume`. claude
	// refuses to resume a session that's already an active bg agent
	// ("currently running as a background agent (bg)") and the worker
	// exits with that message, which the supervisor then respawns in a
	// tight crash loop. GetOrAttach resolves the live record by UUID
	// (incl. resume-child mapping) and errors cleanly when there's no
	// live worker — in which case we fall through to a normal resume.
	// Forks must always branch a fresh copy, so skip this for them.
	if !fork {
		if attached, aerr := m.GetOrAttach(grimoireID, resumeFromUUID); aerr == nil {
			if sessionName != "" && !strings.HasPrefix(sessionName, "grimoire-") {
				attached.SetName(sessionName)
			}
			m.logger.Info("GetOrResume: session already live as bg agent, attached instead of resuming",
				slog.String("grimoire_id", grimoireID),
				slog.String("resume_from", resumeFromUUID),
			)
			return attached, nil
		}
	}

	newSession, err := startDaemonSessionResume(grimoireID, resumeFromUUID, workingDir, m.mongoURI, m.mongoDatabase, m.logger, sessionName, fork)
	if err != nil {
		return nil, fmt.Errorf("daemon resume: %w", err)
	}

	// Mongo name overlay wins over JSONL ai-title / claude's
	// auto-generated "This session is being continued..." title.
	// Same path GetOrCreate uses — kept in sync so compact+restart
	// (which goes through this resume path) preserves the user's name.
	if m.storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if overlay, err := m.storage.ListNameOverrides(ctx); err == nil {
			if name, ok := overlay[grimoireID]; ok && name != "" {
				newSession.Name = name
			} else if newSession.DaemonUUID != "" {
				if name, ok := overlay[newSession.DaemonUUID]; ok && name != "" {
					newSession.Name = name
				}
			}
		}
		cancel()
	}

	m.mu.Lock()
	// Replace any stub (post-ShutdownWorker) entry with the freshly
	// spawned session. The raced-create branch (another GetOr* call
	// raced us and won) checks for an entry with a live PTY — if
	// theirs is also a stub, we still prefer the fresh one.
	if existing, raced := m.sessions[grimoireID]; raced && existing.PTY != nil {
		m.mu.Unlock()
		go shutdownSession(newSession, m.logger)
		return existing, nil
	}
	m.sessions[grimoireID] = newSession
	m.mu.Unlock()

	return newSession, nil
}

// GetOrCreate gets an existing session or creates a new one.
// Lock is held only for map reads/writes; slow operations (subprocess start,
// MongoDB queries) run outside the lock to prevent blocking ListActiveSessions.
func (m *SessionManager) GetOrCreate(sessionID string, dangerousMode bool, workingDir string, sessionName string, systemPrompt string) (*ClaudeSession, error) {
	ctx := context.Background()

	// Fast path: session already in memory.
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if exists {
		currentDangerous := session.GetDangerousMode()
		if currentDangerous == dangerousMode {
			session.UpdateActivity()
			go func() {
				if m.storage != nil {
					m.storage.UpdateSessionActivity(ctx, sessionID)
				}
			}()
			return session, nil
		}

		// Dangerous mode mismatch. For daemon-backed sessions this flag
		// is purely a UI marker — the actual `claude` process inside the
		// daemon worker doesn't observe it. Killing and respawning the
		// daemon worker just because the UI flag flipped destroys the
		// live conversation (and the JSONL scrollback) for no benefit.
		// Just align the flag in-memory and keep going.
		//
		// For subprocess sessions the flag actually changes the spawned
		// process's CLI args (--dangerously-skip-permissions), so we keep
		// the old shutdown-and-recreate behaviour there.
		if session.IsDaemonBacked() {
			m.logger.Info("dangerous_mode flag changed for daemon session, syncing in-memory only",
				slog.String("session_id", sessionID),
				slog.Bool("was", currentDangerous),
				slog.Bool("now", dangerousMode),
			)
			session.SetDangerousMode(dangerousMode)
			session.UpdateActivity()
			go func() {
				if m.storage != nil {
					m.storage.UpdateSessionActivity(ctx, sessionID)
				}
			}()
			return session, nil
		}

		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		if err := shutdownSession(session, m.logger); err != nil {
			m.logger.Error("failed to shutdown session for restart", slog.Any("error", err))
		}
		if m.storage != nil {
			m.storage.UpdateSessionStatus(ctx, sessionID, "inactive")
		}
	}

	// Slow path: DB lookup + subprocess creation — all outside the lock.
	var dbSessionName string
	var mcpConfigPath string
	var restoredMessages []models.ClaudeMessage

	if m.storage != nil {
		dbSession, err := m.storage.GetSession(ctx, sessionID)
		if err != nil {
			m.logger.Error("failed to get session from DB", slog.Any("error", err))
		} else if dbSession != nil {
			dbSessionName = dbSession.Name
			mcpConfigPath = dbSession.MCPConfigPath
			restoredMessages = dbSession.Messages
			m.logger.Info("restoring session metadata from DB",
				slog.String("session_id", sessionID),
				slog.String("name", dbSessionName),
				slog.Int("messages_count", len(restoredMessages)),
			)
		}
	}

	var newSession *ClaudeSession
	var spawnErr error
	if useDaemonBackend() {
		// Smart-attach: if sessionID is a UUID (matches an existing
		// daemon worker) we attach to that existing PTY instead of
		// spawning a new one. Covers two cases:
		//   1. External daemon sessions (kvaps spawned via claude --bg)
		//      that we surface in the Sidebar
		//   2. Sessions we own but that fell out of our manager.sessions
		//      map (e.g. a previous resume that's still alive in daemon)
		// Without this, click→open spawns a fresh chat under the same
		// id which looks to the user like "different session, same name".
		if isUUIDLike(sessionID) {
			client := &daemon.Client{Logger: m.logger}
			if jobs, err := client.ListSessions(); err == nil {
				for _, j := range jobs {
					if j.SessionID == sessionID {
						// Worker is a resume/fork child? Then its session
						// UUID (= sessionID we got here) is NOT the
						// canonical grimoire identity — the canonical
						// JSONL UUID is encoded in the worker's name.
						// Redirect to that canonical: if manager already
						// has an entry there, REUSE it (avoids
						// double-entry like "Bonding" + "This session is
						// being continued..." dup).
						if strings.HasPrefix(j.Name, "grimoire-resume-") || strings.HasPrefix(j.Name, "grimoire-fork-") {
							short := strings.TrimPrefix(j.Name, "grimoire-resume-")
							short = strings.TrimPrefix(short, "grimoire-fork-")
							if canonical := resolveJSONLByShort(j.Cwd, short, m.logger); canonical != "" {
								m.mu.RLock()
								if existing, ok := m.sessions[canonical]; ok && existing.PTY != nil {
									m.mu.RUnlock()
									m.logger.Info("smart-attach: redirecting worker UUID to canonical grimoire id",
										slog.String("requested_id", sessionID),
										slog.String("canonical_id", canonical),
										slog.String("worker_name", j.Name),
									)
									return existing, nil
								}
								m.mu.RUnlock()
							}
						}
						newSession, spawnErr = startDaemonSessionAttach(sessionID, sessionID, m.logger)
						break
					}
				}
				// Also probe for resumed-child by name pattern — the
				// child carries a different daemon UUID, but its name
				// encodes the original short. Reuse that worker rather
				// than spawning yet another resume.
				if newSession == nil && spawnErr == nil {
					expectedChildName := "grimoire-resume-" + sessionID
					for _, j := range jobs {
						if j.Name == expectedChildName {
							newSession, spawnErr = startDaemonSessionAttach(sessionID, j.SessionID, m.logger)
							break
						}
					}
				}
			}
			// Smart-attach didn't yield a live worker (no live record, or
			// the worker we found exited between op:list and op:attach with
			// ENOJOB). For a UUID-shaped sessionID the historical JSONL is
			// still on disk — spawn a daemon resume so the new chat keeps
			// the conversation context instead of starting blank.
			//
			// CRITICAL: cwd MUST come from the historical JSONL header,
			// not from the caller's workingDir. The caller is the WS
			// handler which derives workingDir from the CURRENTLY-OPEN
			// note's project path — that's the wrong cwd for resuming an
			// unrelated session. `claude --resume <uuid>` locates the
			// transcript via (cwd, sessionId); if cwd doesn't match the
			// one in the JSONL header, claude silently starts a fresh
			// chat with no history.
			if newSession == nil {
				resumeCwd := ""
				if hdrPath, hdrErr := getHistoricalPath(sessionID); hdrErr == nil {
					if hdr, hErr := readHistoricalHeader(hdrPath); hErr == nil {
						resumeCwd = hdr.Cwd
					}
				}
				if resumeCwd != "" {
					m.logger.Info("smart-attach miss, resuming via daemon",
						slog.String("session_id", sessionID),
						slog.String("cwd", resumeCwd),
						slog.Any("prior_attach_err", spawnErr),
					)
					spawnErr = nil
					newSession, spawnErr = startDaemonSessionResume(sessionID, sessionID, resumeCwd, m.mongoURI, m.mongoDatabase, m.logger, sessionName, false)
				}
			}
		}
		// Reconnect fallback for grimoire-handle ids (global-*/note-*,
		// which aren't UUIDs so the smart-attach above skipped them): an
		// existing daemon worker for this handle is named
		// "grimoire-<sessionID>". Attach to it instead of spawning a
		// second worker — otherwise every reconnect / backend restart
		// piles up another "grimoire-global-*" worker for the same tab,
		// which is the recurring duplicate-terminal-row bug. Falls through
		// to a fresh spawn when no live worker is found.
		if newSession == nil && spawnErr == nil && useDaemonBackend() && !isUUIDLike(sessionID) {
			client := &daemon.Client{Logger: m.logger}
			if jobs, err := client.ListSessions(); err == nil {
				want := "grimoire-" + sessionID
				for _, j := range jobs {
					if j.Name == want {
						if att, aerr := startDaemonSessionAttach(sessionID, j.SessionID, m.logger); aerr == nil {
							newSession = att
							m.logger.Info("GetOrCreate: attached to existing bg worker for handle instead of spawning",
								slog.String("session_id", sessionID),
								slog.String("worker_uuid", j.SessionID),
								slog.String("worker_short", j.Short),
							)
						}
						break
					}
				}
			}
		}
		if newSession == nil && spawnErr == nil {
			newSession, spawnErr = startDaemonSession(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
		}
		if spawnErr != nil {
			m.logger.Warn("daemon backend failed, falling back to subprocess",
				slog.String("session_id", sessionID),
				slog.Any("error", spawnErr),
			)
			newSession, spawnErr = startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
		}
	} else {
		newSession, spawnErr = startClaudeSubprocess(sessionID, dangerousMode, workingDir, m.mongoURI, m.mongoDatabase, m.logger, systemPrompt)
	}
	if spawnErr != nil {
		return nil, fmt.Errorf("failed to start claude session: %w", spawnErr)
	}

	if dbSessionName != "" {
		newSession.Name = dbSessionName
	} else if sessionName != "" {
		newSession.Name = sessionName
	} else {
		newSession.Name = "Terminal Session"
	}
	// Mongo name overlay wins over JSONL ai-title / DB-stored name —
	// it's the user's explicit choice. Check by grimoireID AND
	// DaemonUUID (resume sessions get overlaid under canonical).
	if m.storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if overlay, err := m.storage.ListNameOverrides(ctx); err == nil {
			if name, ok := overlay[sessionID]; ok && name != "" {
				newSession.Name = name
			} else if newSession.DaemonUUID != "" {
				if name, ok := overlay[newSession.DaemonUUID]; ok && name != "" {
					newSession.Name = name
				}
			}
		}
		cancel()
	}

	if mcpConfigPath != "" {
		newSession.MCPConfigPath = mcpConfigPath
	}

	if len(restoredMessages) > 0 {
		newSession.Messages = restoredMessages
		m.logger.Info("restored messages from DB",
			slog.String("session_id", sessionID),
			slog.Int("count", len(restoredMessages)),
		)
	}

	// Register in the map (brief lock only for map write).
	m.mu.Lock()
	// Double-check: another goroutine may have created it while we were starting the subprocess.
	if existing, raced := m.sessions[sessionID]; raced {
		m.mu.Unlock()
		// Discard the subprocess we just started.
		go shutdownSession(newSession, m.logger)
		return existing, nil
	}
	m.sessions[sessionID] = newSession
	m.mu.Unlock()

	// Persist to DB asynchronously — doesn't block the caller.
	go func() {
		if m.storage == nil {
			return
		}
		dbRecord := &models.ClaudeSession{
			ID:            newSession.ID,
			Name:          newSession.Name,
			DangerousMode: newSession.DangerousMode,
			WorkingDir:    newSession.WorkingDir,
			MCPConfigPath: newSession.MCPConfigPath,
			Status:        "active",
			Messages:      []models.ClaudeMessage{},
			CreatedAt:     newSession.CreatedAt,
			UpdatedAt:     time.Now(),
			LastActivity:  newSession.LastActivity,
		}
		if err := m.storage.SaveSession(ctx, dbRecord); err != nil {
			m.logger.Error("failed to save session to DB", slog.Any("error", err))
		} else {
			m.logger.Info("session saved to DB", slog.String("session_id", sessionID))
		}
	}()

	return newSession, nil
}

// Get retrieves an existing session
func (m *SessionManager) Get(sessionID string) (*ClaudeSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return session, nil
}

// Close closes a specific session
// ShutdownWorker kills the live daemon/subprocess worker for a session
// but PRESERVES the manager entry's metadata (ID, DaemonUUID,
// WorkingDir, Name). Callers that intend a "restart in place" — like
// the Compact endpoint — use this so a subsequent restart can read
// DaemonUUID + WorkingDir to spawn a fresh worker on the correct
// transcript. Using Close() instead would destroy the entry and force
// restart down the dangerous newest-jsonl-in-cwd fallback path.
//
// After ShutdownWorker: session.PTY is nil, DaemonShort is cleared,
// status is "stopped". The entry can be re-attached / resumed by the
// next user action.
func (m *SessionManager) ShutdownWorker(sessionID string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := shutdownSession(session, m.logger); err != nil {
		return fmt.Errorf("failed to shutdown worker: %w", err)
	}

	// Wait for the PTY reader goroutine to exit before nil-ing PTY/Cmd,
	// otherwise it can race a final Read against our clear and panic on
	// the nil deref. Bounded by a short timeout so a stuck reader can't
	// hang shutdown forever.
	select {
	case <-session.ReaderDoneSignal():
	case <-time.After(2 * time.Second):
		m.logger.Warn("PTY reader did not exit within timeout; clearing anyway",
			slog.String("session_id", sessionID))
	}

	// Clear live-process fields, but keep DaemonUUID + WorkingDir +
	// Name + ID so the entry is recognizable on the next restart.
	m.mu.Lock()
	session.PTY = nil
	session.Cmd = nil
	session.DaemonShort = ""
	// DaemonClient may still be needed for status checks, leave intact.
	m.mu.Unlock()

	return nil
}

func (m *SessionManager) Close(sessionID string) error {
	// Hold lock only for map operation — release before slow ops.
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := shutdownSession(session, m.logger); err != nil {
		return fmt.Errorf("failed to shutdown session: %w", err)
	}

	if m.storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.storage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
			m.logger.Error("failed to update session status in DB", slog.Any("error", err))
		}
	}

	return nil
}

// CloseInactive closes sessions that have been inactive for the given timeout
func (m *SessionManager) CloseInactive(timeout time.Duration) {
	// Collect inactive sessions and remove from map under lock — release before slow ops.
	m.mu.Lock()
	type toClose struct {
		id      string
		session *ClaudeSession
	}
	var inactive []toClose
	for id, session := range m.sessions {
		if session.IsInactive(timeout) {
			inactive = append(inactive, toClose{id, session})
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, item := range inactive {
		m.logger.Info("detaching inactive session",
			slog.String("session_id", item.id),
			slog.Duration("inactive_for", time.Since(item.session.LastActivity)),
		)

		// Detach only — same reasoning as CloseAll. Inactivity in
		// grimoire ≠ user wants the daemon worker dead. Worker keeps
		// running in daemon; user can re-attach via sidebar / Sessions
		// Modal when they come back.
		if err := detachSession(item.session, m.logger); err != nil {
			m.logger.Error("failed to detach inactive session",
				slog.String("session_id", item.id),
				slog.Any("error", err),
			)
		}

		if m.storage != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.storage.UpdateSessionStatus(ctx, item.id, "inactive"); err != nil {
				m.logger.Error("failed to update session status in DB", slog.Any("error", err))
			}
			cancel()
		}
	}
}

// CloseAll detaches all sessions (for graceful grimoire shutdown).
// For daemon-backed sessions this DOES NOT kill the daemon worker —
// it just closes our local PTY attach so the worker survives our
// restart. User can re-attach next time grimoire comes up. Subprocess
// sessions die regardless (they're our child processes, can't survive
// our exit). This is the whole point of the daemon backend.
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	old := m.sessions
	m.sessions = make(map[string]*ClaudeSession)
	m.mu.Unlock()

	m.logger.Info("detaching all claude sessions on shutdown", slog.Int("count", len(old)))

	for id, session := range old {
		if err := detachSession(session, m.logger); err != nil {
			m.logger.Error("failed to detach session",
				slog.String("session_id", id),
				slog.Any("error", err),
			)
		}
	}
}

// MonitorInactiveSessions starts a background goroutine to cleanup inactive sessions
func (m *SessionManager) MonitorInactiveSessions(timeout time.Duration, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			m.CloseInactive(timeout)
			m.saveAllSessionMessages()
		}
	}()
}

// saveAllSessionMessages saves messages for all active sessions
func (m *SessionManager) saveAllSessionMessages() {
	m.mu.RLock()
	sessionIDs := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range sessionIDs {
		if err := m.SaveSessionMessages(id); err != nil {
			m.logger.Debug("failed to save messages for session",
				slog.String("session_id", id),
				slog.Any("error", err),
			)
		}
	}
}

// SaveSessionMessages saves session messages to storage
func (m *SessionManager) SaveSessionMessages(sessionID string) error {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if m.storage == nil {
		return nil // Storage not configured
	}

	ctx := context.Background()
	messages := session.GetMessages()

	if err := m.storage.UpdateSessionMessages(ctx, sessionID, messages); err != nil {
		m.logger.Error("failed to save session messages",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		return err
	}

	m.logger.Debug("saved session messages to DB",
		slog.String("session_id", sessionID),
		slog.Int("count", len(messages)),
	)

	return nil
}

// ListActiveSessions returns list of all active sessions
func (m *SessionManager) ListActiveSessions() []*models.ClaudeSession {
	// Pre-fetch daemon records once so each session lookup is O(1). We
	// don't want to hold m.mu while making a daemon RPC, so this happens
	// outside the lock below.
	var jobsByShort map[string]daemon.Record
	var jobsByUUID map[string]daemon.Record
	var jobs []daemon.Record
	daemonReachable := false
	if useDaemonBackend() {
		client := &daemon.Client{}
		if list, err := client.ListSessions(); err == nil {
			daemonReachable = true
			jobs = list
			jobsByShort = make(map[string]daemon.Record, len(list))
			jobsByUUID = make(map[string]daemon.Record, len(list))
			for _, j := range list {
				jobsByShort[j.Short] = j
				jobsByUUID[j.SessionID] = j
			}
		}
	}

	// SNAPSHOT phase: hold m.mu.RLock for the minimum time needed to
	// copy the per-session metadata we care about. NO filesystem or
	// daemon I/O happens under the lock — those go in the next phase
	// against the snapshot. Without this, sidebar polling (every 3s)
	// held the RLock for the duration of N glob+stat+open syscalls,
	// blocking every writer (GetOrCreate, Close, RenameSession) for
	// hundreds of ms on a cold cache.
	type sessSnap struct {
		id              string
		name            string
		dangerousMode   bool
		workingDir      string
		mcpConfigPath   string
		createdAt       time.Time
		lastActivity    time.Time
		daemonBacked    bool
		daemonShort     string
		daemonUUID      string
		lastUserInputAt time.Time
	}
	m.mu.RLock()
	snaps := make([]sessSnap, 0, len(m.sessions))
	known := make(map[string]bool, len(m.sessions))
	for _, session := range m.sessions {
		known[session.ID] = true
		if session.DaemonUUID != "" {
			known[session.DaemonUUID] = true
		}
		// Read session fields under session-local lock — not m.mu —
		// because handler-side mutations (MarkUserInput, SetName,
		// MarkContextPromptSent) acquire only s.mu. Holding m.mu does
		// NOT protect session fields.
		session.mu.Lock()
		snaps = append(snaps, sessSnap{
			id:              session.ID,
			name:            session.Name,
			dangerousMode:   session.DangerousMode,
			workingDir:      session.WorkingDir,
			mcpConfigPath:   session.MCPConfigPath,
			createdAt:       session.CreatedAt,
			lastActivity:    session.LastActivity,
			daemonBacked:    session.IsDaemonBacked(),
			daemonShort:     session.DaemonShort,
			daemonUUID:      session.DaemonUUID,
			lastUserInputAt: session.LastUserInputAt,
		})
		session.mu.Unlock()
	}
	m.mu.RUnlock()

	// RESOLVE phase (lock-free): translate snapshots into output rows.
	// Uses histNameCache for ai-title lookups and jsonlMtimeCache for
	// stat'ing JSONLs. Both are TTL'd so back-to-back polls are O(1)
	// after warmup.
	sessions := make([]*models.ClaudeSession, 0, len(snaps))
	// seenUUID maps a live daemon worker's UUID to its index in `sessions`.
	// One worker can be registered in the manager under BOTH its grimoire
	// handle (e.g. "global-qz7pow") and its own UUID, which otherwise
	// surfaced as two identical "active" rows in the sidebar. Dedupe to one
	// row per worker.
	seenUUID := make(map[string]int)
	now := time.Now()
	for _, s := range snaps {
		// Drop entries whose daemon worker is gone. The manager keeps a
		// session in its map until Close, but a worker can die out-of-band
		// (killed via daemon op:kill, crashed, or swept), leaving a stale
		// entry that surfaced as a duplicate "active" row in the sidebar.
		// Only filter when the daemon list was actually fetched — on a
		// daemon RPC failure we keep everything rather than blank the list.
		if daemonReachable && s.daemonBacked {
			_, byShort := jobsByShort[s.daemonShort]
			_, byUUID := jobsByUUID[s.daemonUUID]
			if !byShort && !byUUID {
				continue
			}
		}
		name := s.name
		isGenericName := name == "" ||
			name == "Terminal Session" ||
			name == "Quick Terminal" ||
			name == "(unnamed)" ||
			strings.HasPrefix(name, "grimoire-")
		if isGenericName {
			if s.daemonShort != "" {
				if friendly := lookupHistoricalNameByShort(s.daemonShort); friendly != "" {
					name = friendly
				}
			}
			if name == s.name && isUUIDLike(s.id) {
				if friendly := lookupHistoricalNameByShort(s.id[:8]); friendly != "" {
					name = friendly
				}
			}
		}
		out := &models.ClaudeSession{
			ID:            s.id,
			Name:          name,
			DangerousMode: s.dangerousMode,
			WorkingDir:    s.workingDir,
			MCPConfigPath: s.mcpConfigPath,
			Status:        "active",
			Messages:      []models.ClaudeMessage{},
			CreatedAt:     s.createdAt,
			UpdatedAt:     now,
			LastActivity:  s.lastActivity,
		}
		if s.daemonBacked {
			if rec, ok := jobsByShort[s.daemonShort]; ok {
				out.Tempo = rec.Tempo
				out.State = rec.State
				out.Detail = rec.Detail
				out.Needs = rec.Needs
				if out.Tempo == "active" {
					userHasTyped := !s.lastUserInputAt.IsZero()
					jsonlStale := false
					if mtime, found := jsonlMtimeFor(s.daemonUUID); found {
						jsonlStale = time.Since(mtime) > 30*time.Second
					}
					switch {
					case !userHasTyped:
						out.Tempo = "idle"
					case jsonlStale:
						out.Tempo = "idle"
					}
				}
			} else {
				out.Tempo = "idle"
				out.State = "running"
				out.Detail = "in grimoire memory"
			}
		} else {
			out.Tempo = "active"
			out.State = "running"
		}
		// Dedupe by daemon worker identity: one worker can be registered
		// under multiple manager keys (its UUID entry AND a grimoire
		// handle / a resumed-into alias), which surfaced as duplicate
		// rows with different names. Key by the worker's DaemonShort — the
		// daemon's own handle, which is reliably populated even when a
		// session entry's DaemonUUID field is blank (the case that let
		// two aliases of one worker slip through). Fall back to DaemonUUID
		// then the entry's own id. Prefer the canonical (id == key) row.
		if s.daemonBacked {
			key := s.daemonShort
			if key == "" {
				key = s.daemonUUID
			}
			if key == "" {
				key = s.id
			}
			if idx, ok := seenUUID[key]; ok {
				// Same daemon worker already has a row — keep one. This
				// collapses resume aliases (a parent session resumed into
				// a fresh worker shows under both its own id and the
				// worker's uuid). Prefer the canonical entry whose id is
				// the worker's own identity (id == key) so the live row
				// wins over the alias.
				if s.id == key {
					sessions[idx] = out
				}
				continue
			}
			seenUUID[key] = len(sessions)
		}
		sessions = append(sessions, out)
	}

	// Also include live daemon sessions not in our manager — e.g. ones
	// kvaps spawned via `claude --bg` directly, or sessions that
	// survived a backend restart but haven't been reattached yet.
	// Sidebar uses this list to surface "all open terminals", not just
	// what grimoire spawned. Resume-children get their daemon name
	// rewritten to their parent's display name so Sidebar shows
	// human-readable labels instead of the structured token.
	if useDaemonBackend() {
		now := time.Now()
		for _, j := range jobs {
			if known[j.SessionID] {
				continue
			}
			// SKIP continuation-children entirely — they belong to a
			// chain whose canonical entry is (or should be) already in
			// manager. Surfacing them here is the persistent dup bug:
			// "Bonding + TestFlight deploy" + "This session is being
			// continued from a previous conversation..." for the
			// SAME conversation. Same logic as RehydrateFromDaemon
			// skip — kept in sync intentionally.
			if strings.HasPrefix(j.Name, "grimoire-resume-") || strings.HasPrefix(j.Name, "grimoire-fork-") {
				continue
			}
			name := j.Name
			// Try JSONL ai-title for daemon-only sessions (Quick Terminal
			// tabs not in manager, kvaps-spawned workers, etc). The short
			// id maps to the JSONL filename via lookupHistoricalNameByShort.
			isGeneric := name == "" ||
				name == "Terminal Session" ||
				name == "Quick Terminal" ||
				name == "(unnamed)" ||
				strings.HasPrefix(name, "grimoire-")
			if isGeneric && j.Short != "" {
				if friendly := lookupHistoricalNameByShort(j.Short); friendly != "" {
					name = friendly
				}
			}
			sessions = append(sessions, &models.ClaudeSession{
				ID:           j.SessionID,
				Name:         name,
				Status:       "active",
				WorkingDir:   j.Cwd,
				Messages:     []models.ClaudeMessage{},
				CreatedAt:    time.UnixMilli(j.StartedAt),
				UpdatedAt:    now,
				LastActivity: time.UnixMilli(j.StartedAt),
				Tempo:        j.Tempo,
				State:        j.State,
				Detail:       j.Detail,
				Needs:        j.Needs,
			})
			_ = jobsByUUID // keep map referenced even if unused; avoids lint warning
		}
	}

	// Sort by CreatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions
}

// SessionStatus is the snapshot returned by GetSessionStatus. Fields map
// 1:1 with daemon.Record for daemon-backed sessions; subprocess sessions
// get synthesized values (tempo=active, state=running) since the local
// PTY doesn't expose richer info.
type SessionStatus struct {
	SessionID    string `json:"sessionId"`
	Name         string `json:"name,omitempty"` // user-visible display name (matches listing/by-project)
	DaemonBacked bool   `json:"daemonBacked"`
	DaemonShort  string `json:"daemonShort,omitempty"`
	DaemonUUID   string `json:"daemonUuid,omitempty"`
	Tempo        string `json:"tempo"`  // idle | active | blocked | unknown
	State        string `json:"state"`  // working | blocked | done | failed | stopped | running | unknown
	Detail       string `json:"detail"` // human-readable, e.g. "Running tests…"
	Needs        string `json:"needs"`  // when blocked, the question waiting on user
}

// GetSessionStatus reports the current activity state of a session.
//
// For daemon-backed sessions it queries op:list and finds the matching
// short id. If the daemon doesn't know about the session (it died or was
// removed externally), returns tempo=unknown state=unknown so the UI can
// show "?" instead of stale data.
//
// For subprocess sessions it returns a synthetic alive/dead status — the
// local PTY layer doesn't expose Haiku-summarised "what is it doing".
func (m *SessionManager) GetSessionStatus(sessionID string) (SessionStatus, error) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		// Try to surface the canonical display name from Mongo
		// overlay → JSONL header → daemon prefix lookup. Without this
		// the frontend, polling /status to refresh runtime, would lose
		// the name when the manager-entry has been GC'd (e.g. after a
		// recent backend restart but before rehydrate picked it up).
		fallbackName := ""
		if m.storage != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			if overlay, err := m.storage.ListNameOverrides(ctx); err == nil {
				if n := overlay[sessionID]; n != "" {
					fallbackName = n
				}
			}
			cancel()
		}
		if fallbackName == "" && len(sessionID) >= 8 {
			if n := lookupHistoricalNameByShort(sessionID[:8]); n != "" {
				fallbackName = n
			}
		}
		return SessionStatus{
			SessionID: sessionID,
			Name:      fallbackName,
			Tempo:     "unknown",
			State:     "unknown",
			Detail:    "session not in memory",
		}, nil
	}

	// Snapshot the display name under lock. Without this the status
	// response carries no name → frontend that polls /status to refresh
	// runtime fields ends up dropping the human title (it had no way to
	// re-derive the canonical name from the daemon UUID alone, which can
	// differ from the grimoireID after resume/fork).
	name := session.GetName()

	if !session.IsDaemonBacked() {
		// Subprocess: alive if Cmd hasn't exited yet. Use the race-free
		// IsProcessDone() channel check instead of cmd.ProcessState
		// (which races against cmd.Wait writes).
		state := "running"
		tempo := "active"
		if session.Cmd != nil && session.IsProcessDone() {
			state = "done"
			tempo = "idle"
		}
		return SessionStatus{
			SessionID:    sessionID,
			Name:         name,
			DaemonBacked: false,
			Tempo:        tempo,
			State:        state,
			Detail:       "subprocess",
		}, nil
	}

	// Daemon-backed: ask the daemon for the live record. Cached briefly
	// to coalesce sidebar-polling bursts across N sessions.
	jobs, err := listSessionsCached(session.DaemonClient)
	if err != nil {
		return SessionStatus{
			SessionID:    sessionID,
			Name:         name,
			DaemonBacked: true,
			DaemonShort:  session.DaemonShort,
			DaemonUUID:   session.DaemonUUID,
			Tempo:        "unknown",
			State:        "unknown",
			Detail:       fmt.Sprintf("daemon error: %v", err),
		}, nil
	}
	for _, j := range jobs {
		if j.Short == session.DaemonShort {
			return SessionStatus{
				SessionID:    sessionID,
				Name:         name,
				DaemonBacked: true,
				DaemonShort:  j.Short,
				DaemonUUID:   j.SessionID,
				Tempo:        j.Tempo,
				State:        j.State,
				Detail:       j.Detail,
				Needs:        j.Needs,
			}, nil
		}
	}
	// Not found in daemon list — likely removed externally.
	return SessionStatus{
		SessionID:    sessionID,
		Name:         name,
		DaemonBacked: true,
		DaemonShort:  session.DaemonShort,
		DaemonUUID:   session.DaemonUUID,
		Tempo:        "unknown",
		State:        "unknown",
		Detail:       "daemon does not have this session",
	}, nil
}

// RenameSession updates the display name of an in-memory session.
// Uses the session's own lock (SetName) — m.mu only protects the map
// lookup; mutating session.Name under m.mu would race with code that
// reads session.Name while holding only s.mu.
func (m *SessionManager) RenameSession(sessionID string, name string) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if ok {
		session.SetName(name)
	}
}
