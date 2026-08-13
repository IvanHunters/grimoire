package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/compact"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// newestJSONLInCwdAPI returns the UUID of the most recently modified
// transcript in the cwd's sanitized project dir, or "" if none. Used
// as a last-resort path resolver in CompactSession when the session's
// id doesn't directly match a JSONL filename. Thin wrapper over
// discovery.NewestJSONLInCwd so the implementation stays in one place.
func newestJSONLInCwdAPI(cwd string) string {
	uuid, _ := discovery.NewestJSONLInCwd(cwd)
	return uuid
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RenameSession updates the display name of a session
func (h *Handler) RenameSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ss := storage.NewSessionStorage(h.db)
	// UPSERT so historical sessions (JSONL on disk, no Mongo record yet)
	// can be renamed too. The override becomes the displayed name in
	// SessionsModal / TranscriptViewer / search results.
	if err := ss.UpsertSessionName(ctx, sessionID, body.Name); err != nil {
		h.logger.Error("failed to rename session", "session_id", sessionID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Also update in-memory session name if it's live
	if h.sessionManager != nil {
		h.sessionManager.RenameSession(sessionID, body.Name)
	}

	w.WriteHeader(http.StatusNoContent)
}

// SearchSessions does a case-insensitive substring search across every
// JSONL transcript under ~/.claude/projects/. Only user/assistant
// message bodies are searched — metadata events are filtered out.
//
// Query params:
//   - q (required): the search string
//   - cwd (optional): limit search to one project's transcripts
//   - limit (optional, default 100): cap on total hits returned
//
// First version uses a linear scan. Performance is acceptable up to
// ~1500 transcripts; if it slows the UI we'll add an index later.
func (h *Handler) SearchSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "q query param required", http.StatusBadRequest)
		return
	}
	cwd := r.URL.Query().Get("cwd")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	hits, err := discovery.Search(ctx, query, cwd, limit)
	if err != nil {
		h.logger.Error("search sessions failed", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if hits == nil {
		hits = []discovery.SearchHit{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(hits); err != nil {
		h.logger.Error("encode search hits", "error", err)
	}
}

// SessionsByCwd returns every session associated with a cwd: live ones
// from the daemon plus historical transcripts on disk, merged by
// sessionId. Used by the per-project sessions panel.
//
// Query: ?cwd=/absolute/path
//
// On daemon-unreachable, returns historical-only — the API stays useful
// when the user just wants to browse old chats.
func (h *Handler) SessionsByCwd(w http.ResponseWriter, r *http.Request) {
	// cwd is optional — when omitted we return ALL sessions across every
	// project. Useful for the global sessions overview.
	cwd := r.URL.Query().Get("cwd")

	// Pull user-chosen name overrides from Mongo so renamed sessions show
	// the right title in the list. Skipped silently if storage is down.
	var overlay claude.NameOverlay
	if h.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		ss := storage.NewSessionStorage(h.db)
		if m, err := ss.ListNameOverrides(ctx); err == nil {
			overlay = claude.MapOverlay(m)
		} else {
			h.logger.Warn("list name overrides failed (continuing without)", "error", err)
		}
	}

	items, err := claude.ListSessionsByCwdOverlay(cwd, overlay)
	if err != nil {
		h.logger.Error("list sessions by cwd failed", "cwd", cwd, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []claude.SessionListItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(items); err != nil {
		h.logger.Error("encode sessions by cwd", "error", err)
	}
}

// ImportSession accepts a multipart upload of one or more .jsonl
// transcripts and writes them under ~/.claude/projects/ so they show
// up in SessionsModal alongside native sessions. The cwd discovered in
// each transcript decides which project subdirectory it lands in —
// imported sessions therefore re-associate with the correct project
// automatically. Sessions with no cwd hint go to a "-imported" bucket.
//
// Returns a JSON array of ImportResult, one per file. Partial success
// is allowed: a bad file fails individually with its `error` set, the
// rest still import.
func (h *Handler) ImportSession(w http.ResponseWriter, r *http.Request) {
	// 32 MB max per upload; multipart parser buffers up to the limit
	// in memory, the rest spills to disk.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Spool files past the in-memory threshold get written to /tmp.
	// Without RemoveAll those temp files accumulate forever — bulk
	// imports of large transcripts would leak GBs of disk over time.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		http.Error(w, "no files provided (use field name 'file')", http.StatusBadRequest)
		return
	}

	type fileResult struct {
		Filename string                  `json:"filename"`
		Result   *discovery.ImportResult `json:"result,omitempty"`
		Error    string                  `json:"error,omitempty"`
	}
	results := make([]fileResult, 0, len(r.MultipartForm.File["file"]))

	for _, fh := range r.MultipartForm.File["file"] {
		fr := fileResult{Filename: fh.Filename}
		f, err := fh.Open()
		if err != nil {
			fr.Error = err.Error()
			results = append(results, fr)
			continue
		}
		res, err := discovery.ImportTranscript(f, fh.Filename)
		_ = f.Close()
		if err != nil {
			fr.Error = err.Error()
			h.logger.Warn("import transcript failed",
				"filename", fh.Filename, "error", err)
		} else {
			fr.Result = &res
			h.logger.Info("imported transcript",
				"filename", fh.Filename,
				"session_id", res.SessionID,
				"cwd", res.Cwd,
				"messages", res.Messages,
			)
		}
		results = append(results, fr)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		h.logger.Error("encode import results", "error", err)
	}
}

// SessionRawJSONL streams the raw transcript file for download. Unlike
// SessionTranscript (which returns parsed messages), this serves the
// untouched bytes so the recipient can import it back via
// POST /sessions/import for a lossless roundtrip.
//
// Use case: Alice clicks Download JSONL → ships file to Bob → Bob
// drags into SessionsModal → identical session appears in his grimoire.
func (h *Handler) SessionRawJSONL(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	path, err := discovery.SessionPath(sessionID)
	if err != nil {
		http.Error(w, "transcript not found", http.StatusNotFound)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		h.logger.Error("open transcript for download", "session_id", sessionID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sessionID+`.jsonl"`)
	// io.Copy streams without buffering everything in memory — keeps
	// the endpoint snappy even for big multi-MB transcripts.
	if _, err := io.Copy(w, f); err != nil {
		h.logger.Warn("stream jsonl", "session_id", sessionID, "error", err)
	}
}

// SessionTranscript reads a session's JSONL transcript from disk and
// returns the parsed user/assistant message stream. Metadata events
// (permission-mode, ai-title, attachments, file-history-snapshot, etc)
// are filtered out — the response is what a chat UI would naturally
// render.
//
// Used by TranscriptViewer when the user clicks a session in the
// SessionsModal or a hit in the GlobalSearchModal.
func (h *Handler) SessionTranscript(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	path, err := discovery.SessionPath(sessionID)
	if err != nil {
		http.Error(w, "transcript not found", http.StatusNotFound)
		return
	}

	tr, err := discovery.ReadTranscript(path)
	if err != nil {
		h.logger.Error("read transcript failed", "session_id", sessionID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Mongo overlay (user-chosen rename) wins over the JSONL first-prompt
	// fallback that ReadHeader picks. Without this, TranscriptViewer shows
	// the original first message as the title — even when the user
	// explicitly renamed the session in the sidebar.
	if h.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		ss := storage.NewSessionStorage(h.db)
		if overlay, err := ss.ListNameOverrides(ctx); err == nil {
			if n := overlay[sessionID]; n != "" {
				tr.Header.Name = n
			}
		}
		cancel()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tr); err != nil {
		h.logger.Error("encode transcript", "error", err)
	}
}

// SessionStatus returns the live tempo/state/detail/needs for one session.
// For daemon-backed sessions this is fresh data from the daemon's op:list;
// for subprocess sessions it's a synthetic alive/dead status.
//
// Frontend polls this every ~2s while a chat is open to render the status
// indicator next to the chat header and the "waiting for input" banner.
func (h *Handler) SessionStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}
	if h.sessionManager == nil {
		http.Error(w, "session manager not configured", http.StatusServiceUnavailable)
		return
	}

	status, err := h.sessionManager.GetSessionStatus(sessionID)
	if err != nil {
		h.logger.Error("failed to get session status",
			"session_id", sessionID,
			"error", err,
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		h.logger.Error("failed to encode session status", "error", err)
	}
}

// ListSessions returns list of active Claude sessions from memory (not DB)
// This ensures only live sessions are shown, preventing issues after backend restart
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	// Get live sessions from SessionManager instead of DB
	sessions := h.sessionManager.ListActiveSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sessions); err != nil {
		h.logger.Error("failed to encode sessions", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// transcriptTrashRoot returns the directory into which deleted session
// transcripts are moved. It lives OUTSIDE ~/.claude/projects (as a
// sibling), so discovery.scanRoot never re-lists a trashed session, yet
// it is on the same filesystem so os.Rename is atomic and never fails
// cross-device.
func transcriptTrashRoot() (string, error) {
	root, err := discovery.ProjectsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(root), ".md-editor-trash"), nil
}

// moveTranscriptToTrash relocates a session's JSONL transcript and every
// sidecar it owns — the <stem>/ dir (subagents + tool-results) and any
// <stem>.jsonl.* siblings (.archive.* / .ledger.md) — into a fresh
// per-delete folder under trashRoot instead of destroying them. Returns
// the trash folder so the caller can log where the data went. A delete
// thus becomes fully recoverable: move the folder's contents back into
// the project dir to restore the session.
func moveTranscriptToTrash(jsonlPath, sessionID, trashRoot string, nowNano int64) (string, error) {
	dir := filepath.Dir(jsonlPath)
	base := filepath.Base(jsonlPath)
	stem := strings.TrimSuffix(base, ".jsonl")

	dest := filepath.Join(trashRoot, sessionID+"-"+strconv.FormatInt(nowNano, 10))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}

	// Main transcript — the one move that must succeed.
	if err := os.Rename(jsonlPath, filepath.Join(dest, base)); err != nil {
		return "", err
	}

	// Sidecar dir <stem>/ (subagents, tool-results). Best-effort.
	sidecarDir := filepath.Join(dir, stem)
	if info, err := os.Stat(sidecarDir); err == nil && info.IsDir() {
		_ = os.Rename(sidecarDir, filepath.Join(dest, stem))
	}

	// Archive / ledger siblings (<stem>.jsonl.*). Best-effort.
	if siblings, _ := filepath.Glob(filepath.Join(dir, base+".*")); len(siblings) > 0 {
		for _, s := range siblings {
			_ = os.Rename(s, filepath.Join(dest, filepath.Base(s)))
		}
	}

	return dest, nil
}

// DeleteSession kills a Claude session and cleans up resources.
//
// Query param `transcript=true` ALSO removes the JSONL transcript from
// ~/.claude/projects/. Default false — we keep the history around so
// the user can browse the conversation later. The UI confirms before
// passing transcript=true.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}
	dropTranscript := r.URL.Query().Get("transcript") == "true"

	// Close session in manager (stops subprocess / daemon worker) — runs
	// in background so the HTTP response doesn't wait on shutdown.
	if h.sessionManager != nil {
		go func() {
			if err := h.sessionManager.Close(sessionID); err != nil {
				h.logger.Warn("failed to close session in manager", "session_id", sessionID, "error", err)
			}
		}()
	}

	// Also kill the daemon worker by its session UUID. This catches
	// external sessions (kvaps spawned, our orphans across restarts)
	// that aren't in manager.sessions — without this, manager.Close
	// is a no-op and the worker stays alive, so the row re-appears on
	// the next sidebar poll.
	go func() {
		client := &daemon.Client{Logger: h.logger}
		jobs, err := client.ListSessions()
		if err != nil {
			return
		}
		for _, j := range jobs {
			if j.SessionID == sessionID {
				if rmErr := client.Remove(j.Short); rmErr != nil {
					h.logger.Warn("daemon worker remove failed",
						"session_id", sessionID, "short", j.Short, "error", rmErr)
				}
				return
			}
			// Also match the "grimoire-resume-<short>" name pattern in
			// case the row's sessionID was the historical parent and the
			// live worker is the resume child.
			if strings.HasPrefix(j.Name, "grimoire-resume-") &&
				strings.TrimPrefix(j.Name, "grimoire-resume-") == sessionID[:min(8, len(sessionID))] {
				if rmErr := client.Remove(j.Short); rmErr != nil {
					h.logger.Warn("daemon resume-child remove failed",
						"session_id", sessionID, "short", j.Short, "error", rmErr)
				}
				return
			}
		}
	}()

	// Update Mongo status with a fresh ctx (not request-bound).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if h.db != nil {
		sessionStorage := storage.NewSessionStorage(h.db)
		// MatchedCount=0 is fine for historical sessions; ignore that error.
		if err := sessionStorage.UpdateSessionStatus(ctx, sessionID, "terminated"); err != nil {
			h.logger.Warn("update session status (non-fatal)", "session_id", sessionID, "error", err)
		}
	}

	// Optionally delete the JSONL transcript itself. The file lives
	// outside our app at ~/.claude/projects/. We do NOT os.Remove it:
	// a single Trash click used to PERMANENTLY destroy a session's
	// history and orphan its subagents/ sidecar dir (that is how
	// session 3315 was lost). Instead we MOVE the transcript and all
	// its sidecars into a trash dir outside the projects root, so an
	// accidental delete is always recoverable. Failure here is logged
	// but doesn't surface 500 — the session is already gone from memory
	// and Mongo.
	if dropTranscript {
		if path, err := discovery.SessionPath(sessionID); err == nil {
			trashRoot, trErr := transcriptTrashRoot()
			if trErr != nil {
				h.logger.Warn("resolve transcript trash root failed",
					"session_id", sessionID, "error", trErr)
			} else if dest, mvErr := moveTranscriptToTrash(path, sessionID, trashRoot, time.Now().UnixNano()); mvErr != nil {
				h.logger.Warn("move transcript to trash failed",
					"session_id", sessionID, "path", path, "error", mvErr)
			} else {
				h.logger.Info("transcript moved to trash (recoverable)",
					"session_id", sessionID, "path", path, "trash", dest)
			}
		} else {
			// Path lookup failed — likely the transcript was already gone.
			h.logger.Debug("transcript not found for delete",
				"session_id", sessionID, "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// CompactSession runs the deterministic compactor on a session's JSONL
// transcript: bulky tool_result payloads from older turns get replaced
// with a short stub (keeping tool_use_id pairing intact), and an
// optional ledger note is generated capturing every tool call so the
// detail isn't lost.
//
// POST /api/sessions/{id}/compact
//
//	{
//	  "keep_recent_tool_results": 30,
//	  "max_stub_bytes": 200,
//	  "drop_tool_use_result_mirror": true,
//	  "generate_ledger": true
//	}
//
// Response: stats + the inline ledger markdown (also written next to
// the JSONL as `<uuid>.jsonl.ledger.md`).
func (h *Handler) CompactSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Body optional — all fields have safe defaults.
	var body struct {
		KeepRecentToolResults    int   `json:"keep_recent_tool_results"`
		MaxStubBytes             int   `json:"max_stub_bytes"`
		DropToolUseResultMirror  *bool `json:"drop_tool_use_result_mirror"`
		GenerateLedger           *bool `json:"generate_ledger"`
		DropFileHistorySnapshots *bool `json:"drop_file_history_snapshots"`
		DropMetaSidecar          *bool `json:"drop_meta_sidecar"`
		DropThinking             *bool `json:"drop_thinking"`
		KeepRecentAttachments    int   `json:"keep_recent_attachments"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body) // best-effort
	}
	dropMirror := true
	if body.DropToolUseResultMirror != nil {
		dropMirror = *body.DropToolUseResultMirror
	}
	genLedger := true
	if body.GenerateLedger != nil {
		genLedger = *body.GenerateLedger
	}
	// New aggressive-but-safe drops — all default true. These remove
	// content claude does NOT consume on --resume (file-history is
	// rebuilt from disk; meta sidecar is grimoire-side; thinking is
	// internal scratchpad). Override via JSON body if you need to keep
	// any category.
	dropFileHistory := true
	if body.DropFileHistorySnapshots != nil {
		dropFileHistory = *body.DropFileHistorySnapshots
	}
	dropMeta := true
	if body.DropMetaSidecar != nil {
		dropMeta = *body.DropMetaSidecar
	}
	dropThinking := true
	if body.DropThinking != nil {
		dropThinking = *body.DropThinking
	}

	// Resolve transcript path with three-step lookup so compact works
	// even when the UI sends a worker UUID (drift after resume cycles)
	// instead of the canonical JSONL UUID:
	//   1. SessionPath(sessionID) — happy path (sessionID == JSONL).
	//   2. Manager.Get(sessionID).DaemonUUID — for resume sessions we
	//      pin DaemonUUID to the resume-source UUID, which IS the
	//      JSONL.
	//   3. newestJSONLInCwd(workingDir) — last resort.
	path, err := discovery.SessionPath(sessionID)
	if err != nil && h.sessionManager != nil {
		if sess, getErr := h.sessionManager.Get(sessionID); getErr == nil && sess != nil {
			if sess.DaemonUUID != "" && sess.DaemonUUID != sessionID {
				if p, e := discovery.SessionPath(sess.DaemonUUID); e == nil {
					path = p
					err = nil
					h.logger.Info("compact: resolved via DaemonUUID",
						"session_id", sessionID,
						"resolved_uuid", sess.DaemonUUID)
				}
			}
			if err != nil && sess.WorkingDir != "" {
				if uuid := newestJSONLInCwdAPI(sess.WorkingDir); uuid != "" {
					if p, e := discovery.SessionPath(uuid); e == nil {
						path = p
						err = nil
						h.logger.Info("compact: resolved via newest-jsonl-in-cwd",
							"session_id", sessionID,
							"resolved_uuid", uuid,
							"cwd", sess.WorkingDir)
					}
				}
			}
		}
	}
	if err != nil {
		http.Error(w, "transcript not found", http.StatusNotFound)
		return
	}

	// Kill the live daemon worker BEFORE touching the JSONL. Otherwise
	// the worker might append a new turn between our read-all-lines and
	// our atomic rename, and that turn would be lost. The UI is
	// expected to re-attach (the Sidebar Compact action dispatches a
	// claude-session-restart-request) which spawns a fresh worker that
	// --resume's from the now-compacted transcript.
	//
	// We use ShutdownWorker (NOT Close) so the manager entry KEEPS its
	// DaemonUUID + WorkingDir metadata. Without this, the immediate
	// restart_session that ChatPanel fires post-compact arrives at
	// handleRestartSession with an empty existing.DaemonUUID and ""
	// workingDir → no resume target → fresh empty terminal (the user's
	// "after compact I see new session" bug).
	if h.sessionManager != nil {
		if err := h.sessionManager.ShutdownWorker(sessionID); err != nil {
			h.logger.Debug("shutdown worker before compact", "session_id", sessionID, "error", err)
		}
	}

	opts := compact.Options{
		KeepRecentToolResults:    body.KeepRecentToolResults,
		MaxStubBytes:             body.MaxStubBytes,
		DropToolUseResultMirror:  dropMirror,
		DropFileHistorySnapshots: dropFileHistory,
		DropMetaSidecar:          dropMeta,
		DropThinking:             dropThinking,
		KeepRecentAttachments:    body.KeepRecentAttachments,
	}

	var ledgerSink io.Writer
	var ledgerBuf strings.Builder
	if genLedger {
		ledgerSink = &ledgerBuf
	}

	res, err := compact.Compact(path, opts, ledgerSink)
	if err != nil {
		h.logger.Error("compact failed", "session_id", sessionID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if genLedger && ledgerBuf.Len() > 0 {
		ledgerPath := path + ".ledger.md"
		if werr := os.WriteFile(ledgerPath, []byte(ledgerBuf.String()), 0o644); werr != nil {
			h.logger.Warn("write ledger sidecar", "path", ledgerPath, "error", werr)
		} else {
			res.Stats.LedgerPath = ledgerPath
		}
	}

	resp := struct {
		compact.Stats
		Ledger string `json:"ledger,omitempty"`
	}{
		Stats:  res.Stats,
		Ledger: ledgerBuf.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("encode compact response", "error", err)
	}
}
