package claude

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
)

// isImported reports whether the transcript was brought in via the
// ImportSession flow. Two signals, either one is enough:
//   - JSONL path is under the "-imported" sanitized-cwd bucket (source
//     transcript had no cwd metadata)
//   - A sidecar "<uuid>.imported" file exists next to the JSONL (the
//     import handler writes this for all imports regardless of cwd).
func isImported(jsonlPath string) bool {
	if strings.Contains(jsonlPath, "/-imported/") {
		return true
	}
	if jsonlPath == "" {
		return false
	}
	marker := strings.TrimSuffix(jsonlPath, ".jsonl") + ".imported"
	if _, err := os.Stat(marker); err == nil {
		return true
	}
	return false
}

// isFork reports whether the session was created via --fork-session.
// Detected by a sidecar "<uuid>.fork" file written by
// startDaemonSessionResume when fork=true.
func isFork(jsonlPath string) bool {
	if jsonlPath == "" {
		return false
	}
	marker := strings.TrimSuffix(jsonlPath, ".jsonl") + ".fork"
	if _, err := os.Stat(marker); err == nil {
		return true
	}
	return false
}

// SessionListItem is the merged view of a session for the SessionsPanel.
// One row covers both historical (JSONL on disk, no live process) and
// live (daemon-hosted, possibly with running TUI). Use Live to decide
// what UI state to show: attach button, status badge, etc.
type SessionListItem struct {
	// SessionID is the full UUID — stable identifier across restarts.
	// Matches the JSONL filename and daemon.Record.SessionID.
	SessionID string `json:"sessionId"`

	// DaemonShort is set only when the session is currently live in the
	// daemon. Empty string for historical-only entries.
	DaemonShort string `json:"daemonShort,omitempty"`

	Name         string    `json:"name"`
	FirstPrompt  string    `json:"firstPrompt"`
	Cwd          string    `json:"cwd"`
	GitBranch    string    `json:"gitBranch,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	LastActivity time.Time `json:"lastActivity"`
	SizeBytes    int64     `json:"sizeBytes"`

	// Live mirrors the daemon's live state when available. Nil for
	// historical-only entries.
	Live *LiveState `json:"live,omitempty"`

	// JSONLPath is the on-disk transcript. Frontend uses it for "view
	// transcript" / delete operations.
	JSONLPath string `json:"jsonlPath"`

	// Imported is true for sessions that landed in the "-imported"
	// sanitized-cwd bucket — i.e. they came from a .jsonl upload
	// (ImportSession handler) rather than a claude --bg dispatch.
	// Imports with a recognisable cwd land in the regular project
	// dir and are NOT flagged — they're effectively native then.
	Imported bool `json:"imported,omitempty"`

	// Forked is true for sessions created via claude --fork-session
	// (Fork button in kebab, MCP start_session with fork=true).
	// Marked via sidecar "<uuid>.fork" next to the JSONL when the
	// fork is dispatched.
	Forked bool `json:"forked,omitempty"`
}

// LiveState is the subset of daemon.Record fields useful for badges.
type LiveState struct {
	Tempo  string `json:"tempo"`  // idle | active | blocked
	State  string `json:"state"`  // working | blocked | done | failed | stopped
	Detail string `json:"detail"` // Haiku-generated activity summary
	Needs  string `json:"needs"`  // when blocked, the question waiting on user
}

// NameOverlay returns user-chosen display names keyed by sessionId.
// Used by ListSessionsByCwd to apply Mongo overrides on top of JSONL
// ai-titles. nil overlay = no overrides applied.
type NameOverlay interface {
	Lookup(sessionID string) string
}

// MapOverlay is a tiny NameOverlay backed by a plain map. Build it once
// from storage.ListNameOverrides() and pass to ListSessionsByCwd.
type MapOverlay map[string]string

// Lookup returns the override for sessionID, or "" when none is set.
func (m MapOverlay) Lookup(sessionID string) string { return m[sessionID] }

// ListSessionsByCwd returns every session associated with the given
// working directory: live sessions from the daemon plus historical
// transcripts on disk. The two sources are merged by sessionId, so a
// live session whose transcript also exists on disk shows up once with
// Live populated.
//
// When cwd is empty, returns sessions across every project (full
// discovery.ScanAll). Useful for a global "all sessions" view.
//
// Sorted by LastActivity descending — most recently touched first.
func ListSessionsByCwd(cwd string) ([]SessionListItem, error) {
	return listSessionsByCwd(cwd, nil, nil)
}

// managedLiveSessionIDs is queried by listing to decide whether a
// historical session is also "in grimoire memory" — even if the daemon
// worker has gone idle. The manager package implements this; listing
// stays decoupled by accepting it as a function arg.
var managedLiveSessionIDs func() map[string]bool

// managedDaemonUUIDs returns the set of daemon-side UUIDs we own (i.e.
// our m.sessions entries that are daemon-backed). Listing uses this to
// SKIP duplicates: if the daemon's op:list returns a worker UUID that
// our manager already tracks under a grimoireID (e.g. "note-XXX"),
// don't surface a second "orphan" row keyed by the daemon UUID — that
// row tricks the user into deleting the live worker of an open chat.
var managedDaemonUUIDs func() map[string]bool

// ManagedSessionInfo carries the user-visible name + grimoire-side ID
// of a manager session. Listing uses it to display a freshly-forked
// session under its given name BEFORE claude writes the JSONL —
// without it the daemon-side row gets either skipped (managedDaemon
// dedup) or shown with the sanitized "···<short>" placeholder.
type ManagedSessionInfo struct {
	GrimoireID string
	Name       string
}

// managedSessionByDaemonUUID lets listing look up the manager session
// for a given daemon UUID. Returns map[daemonUUID]ManagedSessionInfo.
var managedSessionByDaemonUUID func() map[string]ManagedSessionInfo

// SetManagedLiveProvider lets the manager package inject a callback
// for listing to query in-memory session IDs. Wires through Init.
func SetManagedLiveProvider(fn func() map[string]bool) {
	managedLiveSessionIDs = fn
}

// SetManagedDaemonUUIDProvider injects a callback returning daemon
// UUIDs of managed sessions, used to dedup listing rows against
// orphan-looking daemon-side entries.
func SetManagedDaemonUUIDProvider(fn func() map[string]bool) {
	managedDaemonUUIDs = fn
}

// SetManagedSessionInfoProvider injects a callback returning the
// daemon-UUID → ManagedSessionInfo map. Used by listing to display
// the proper user-given name for fresh forks/resumes before the
// JSONL is written.
func SetManagedSessionInfoProvider(fn func() map[string]ManagedSessionInfo) {
	managedSessionByDaemonUUID = fn
}

// ListSessionsByCwdOverlay is the same as ListSessionsByCwd but applies
// a user-name overlay on top: if overlay.Lookup(sessionId) is non-empty
// it wins over the JSONL ai-title and the daemon's own name.
func ListSessionsByCwdOverlay(cwd string, overlay NameOverlay) ([]SessionListItem, error) {
	var managed map[string]bool
	if managedLiveSessionIDs != nil {
		managed = managedLiveSessionIDs()
	}
	return listSessionsByCwd(cwd, overlay, managed)
}

// splitResumeChildren separates live daemon records into two buckets:
//   - "regular" live sessions (fresh spawns, attaches, etc)
//   - resume-spawn sessions whose name encodes the original historical
//     short id (our startDaemonSessionResume sets name to
//     "grimoire-resume-<full-uuid>" — that uuid identifies the
//     historical session being resumed).
//
// The resume-spawn bucket is keyed by the original short so the merge
// pass can attach the live state to its historical counterpart rather
// than surfacing both as separate rows.
func splitResumeChildren(live []daemon.Record) (regular []daemon.Record, resumeOf map[string]daemon.Record) {
	resumeOf = make(map[string]daemon.Record)
	regular = live[:0]
	for _, j := range live {
		switch {
		case strings.HasPrefix(j.Name, "grimoire-resume-"):
			originalShort := strings.TrimPrefix(j.Name, "grimoire-resume-")
			resumeOf[originalShort] = j
		case strings.HasPrefix(j.Name, "grimoire-fork-"):
			// Forks branch off with a new UUID — they're their own
			// session, not a continuation. Treat as regular.
			regular = append(regular, j)
		default:
			regular = append(regular, j)
		}
	}
	return regular, resumeOf
}

func listSessionsByCwd(cwd string, overlay NameOverlay, managedLive map[string]bool) ([]SessionListItem, error) {
	if managedLive == nil {
		managedLive = map[string]bool{}
	}
	// Daemon UUIDs of sessions our manager owns. Used to dedup AND
	// to surface the freshly-spawned ones under their grimoire-side
	// name (before claude writes a JSONL). Maps daemonUUID →
	// ManagedSessionInfo{grimoireID, name}.
	var managedInfo map[string]ManagedSessionInfo
	if managedSessionByDaemonUUID != nil {
		managedInfo = managedSessionByDaemonUUID()
	}
	if managedInfo == nil {
		managedInfo = map[string]ManagedSessionInfo{}
	}
	_ = managedDaemonUUIDs // kept around for callers that still need just the set
	var historical []discovery.Session
	var err error
	if cwd == "" {
		historical, err = discovery.ScanAll(nil)
	} else {
		historical, err = discovery.ScanCwd(cwd, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("scan transcripts: %w", err)
	}

	live, liveErr := listLiveByCwd(cwd)
	// If the daemon is unreachable, return historical-only with a note in
	// the error log — but don't fail the API call. UI should still show
	// what's on disk.
	if liveErr != nil {
		live = nil
	}

	// Split live into regular records vs resume-spawn children.
	// Resume children are merged into their historical parent so the
	// user doesn't see "session X" twice (once as historical, once as
	// the freshly-resumed daemon UUID).
	live, resumeOf := splitResumeChildren(live)

	// Index regular live by SessionID for O(1) join.
	liveByID := make(map[string]daemon.Record, len(live))
	for _, j := range live {
		liveByID[j.SessionID] = j
	}

	items := make([]SessionListItem, 0, len(historical)+len(live))
	seen := make(map[string]bool, len(historical))

	for _, h := range historical {
		seen[h.SessionID] = true
		item := SessionListItem{
			SessionID:    h.SessionID,
			Name:         h.Name,
			FirstPrompt:  h.FirstPrompt,
			Cwd:          h.Cwd,
			GitBranch:    h.GitBranch,
			StartedAt:    h.StartedAt,
			LastActivity: h.LastActivityAt,
			SizeBytes:    h.SizeBytes,
			JSONLPath:    h.JSONLPath,
			Imported:     isImported(h.JSONLPath),
			Forked:       isFork(h.JSONLPath),
		}
		if r, ok := liveByID[h.SessionID]; ok {
			item.DaemonShort = r.Short
			item.Live = &LiveState{
				Tempo:  r.Tempo,
				State:  r.State,
				Detail: r.Detail,
				Needs:  r.Needs,
			}
			// Prefer the daemon's display name ONLY when it's a real
			// human-readable name (e.g. kvaps renamed via Ctrl+R in
			// the agent TUI). The daemon's structured tokens
			// ("grimoire-fork-XX", "grimoire-global-XX", "grimoire-<uuid>")
			// are wire-format only — fall back to the JSONL ai-title
			// instead. Overlay (Mongo rename) takes precedence over
			// both below.
			if r.Name != "" && !strings.HasPrefix(r.Name, "grimoire-") {
				item.Name = r.Name
			}
		} else if r, ok := resumeOf[h.SessionID]; ok {
			// Historical session has been resumed — that live record is
			// actually a child of this row, surface it as our live state
			// rather than letting it appear as a separate "grimoire-resume-*"
			// entry. UI sees one merged row with both transcript history
			// and current activity.
			item.DaemonShort = r.Short
			item.Live = &LiveState{
				Tempo:  r.Tempo,
				State:  r.State,
				Detail: r.Detail,
				Needs:  r.Needs,
			}
			delete(resumeOf, h.SessionID) // mark consumed
		} else if managedLive[h.SessionID] {
			// Session is alive in our manager (chat panel open in
			// browser) even if its daemon worker has paused. UI still
			// wants to surface it as "active now".
			item.Live = &LiveState{
				Tempo:  "idle",
				State:  "running",
				Detail: "in grimoire memory",
			}
		}
		// User-chosen overlay name beats everything else (JSONL ai-title,
		// daemon name) — it's the explicit "rename" action.
		if overlay != nil {
			if n := overlay.Lookup(item.SessionID); n != "" {
				item.Name = n
			}
		}
		items = append(items, item)
	}

	// Live sessions without a transcript yet (just spawned, haven't
	// written first line) — surface them too.
	for _, r := range live {
		if seen[r.SessionID] {
			continue
		}
		// Determine the row's SessionID + Name. If this daemon worker
		// belongs to a managed session, use the GRIMOIRE-side ID so
		// the row's click handler routes through our manager (and
		// delete kills the right thing). Otherwise this is an
		// external daemon session — keep the daemon UUID.
		rowID := r.SessionID
		name := r.Name
		if info, ok := managedInfo[r.SessionID]; ok {
			rowID = info.GrimoireID
			if info.Name != "" {
				name = info.Name
			}
		} else if strings.HasPrefix(r.Name, "grimoire-resume-") || strings.HasPrefix(r.Name, "grimoire-fork-") {
			// Worker is a continuation child whose session UUID
			// (r.SessionID) doesn't match any managed entry, but the
			// CANONICAL parent in the worker's name does. Resolve via
			// disk lookup and rewrite so the row maps to the parent's
			// grimoire identity instead of appearing as a separate
			// "···<short>" duplicate next to the canonical entry.
			short := strings.TrimPrefix(r.Name, "grimoire-resume-")
			short = strings.TrimPrefix(short, "grimoire-fork-")
			if canonical := resolveJSONLByShortForListing(r.Cwd, short); canonical != "" {
				if info, ok := managedInfo[canonical]; ok {
					rowID = info.GrimoireID
					if info.Name != "" {
						name = info.Name
					}
				}
			}
		}
		// Second-pass dedup: after possibly rewriting rowID to a
		// grimoireID, ensure we don't add a row that the historical
		// loop already produced under THAT id (e.g. fork's claude
		// JSONL happened to be written under our grimoireID).
		if seen[rowID] {
			continue
		}
		seen[rowID] = true
		// Strip our structured tokens for any leftover external rows.
		if strings.HasPrefix(name, "grimoire-") {
			name = "···" + r.Short
		}
		// Overlay (Mongo rename) wins, keyed by the row's effective ID.
		if overlay != nil {
			if n := overlay.Lookup(rowID); n != "" {
				name = n
			} else if n := overlay.Lookup(r.SessionID); n != "" {
				// Also check daemon UUID overlay key — that's where
				// handler.go writes after fork/resume spawn.
				name = n
			}
		}
		items = append(items, SessionListItem{
			SessionID:   rowID,
			DaemonShort: r.Short,
			Name:        name,
			Cwd:         r.Cwd,
			Live: &LiveState{
				Tempo: r.Tempo, State: r.State, Detail: r.Detail, Needs: r.Needs,
			},
			StartedAt:    time.UnixMilli(r.StartedAt),
			LastActivity: time.UnixMilli(r.StartedAt),
		})
	}

	// Orphan resume-children — daemon sessions named "grimoire-resume-*"
	// whose historical parent we didn't find (parent JSONL missing,
	// outside the cwd filter, etc). Surface them as their own rows so
	// the user never has a live worker invisible to the UI.
	for _, r := range resumeOf {
		if seen[r.SessionID] {
			continue
		}
		name := r.Name
		// Same structured-token sanitization as above.
		if strings.HasPrefix(name, "grimoire-") {
			parentShort := strings.TrimPrefix(name, "grimoire-resume-")
			if friendly := lookupHistoricalNameByShort(parentShort); friendly != "" {
				name = friendly
			} else {
				name = "···" + r.Short
			}
		}
		if overlay != nil {
			if n := overlay.Lookup(r.SessionID); n != "" {
				name = n
			}
		}
		items = append(items, SessionListItem{
			SessionID:   r.SessionID,
			DaemonShort: r.Short,
			Name:        name,
			Cwd:         r.Cwd,
			Live: &LiveState{
				Tempo: r.Tempo, State: r.State, Detail: r.Detail, Needs: r.Needs,
			},
			StartedAt:    time.UnixMilli(r.StartedAt),
			LastActivity: time.UnixMilli(r.StartedAt),
		})
	}

	// Sort: live sessions first (pinned to top regardless of activity),
	// then everything by LastActivity desc within each group. The user
	// wants to glance at the modal and see "what's running right now"
	// without scrolling past old transcripts.
	sort.SliceStable(items, func(i, j int) bool {
		iLive := items[i].Live != nil
		jLive := items[j].Live != nil
		if iLive != jLive {
			return iLive
		}
		return items[i].LastActivity.After(items[j].LastActivity)
	})
	return items, nil
}

// listLiveByCwd asks the daemon for all sessions, optionally filtered by
// cwd. Empty cwd returns all live sessions. Returns nil + error if the
// daemon socket isn't reachable.
func listLiveByCwd(cwd string) ([]daemon.Record, error) {
	client := &daemon.Client{}
	jobs, err := client.ListSessions()
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		return jobs, nil
	}
	out := jobs[:0]
	for _, j := range jobs {
		if j.Cwd == cwd {
			out = append(out, j)
		}
	}
	return out, nil
}

// resolveJSONLByShortForListing mirrors manager.resolveJSONLByShort —
// duplicated here to avoid the package cycle (manager already imports
// listing's providers). Given a daemon worker's "grimoire-resume-<short>"
// name component, returns the full UUID of the on-disk JSONL whose
// filename starts with <short>. Returns "" if no match in cwd.
func resolveJSONLByShortForListing(cwd, short string) string {
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
		if !strings.HasPrefix(name, short) || !strings.HasSuffix(name, ".jsonl") {
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
	return newest
}
