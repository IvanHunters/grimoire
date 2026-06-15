package discovery

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ImportResult is what ImportTranscript returns to the caller.
type ImportResult struct {
	SessionID string `json:"sessionId"` // UUID of the imported session
	Path      string `json:"path"`      // absolute path of the written JSONL
	Cwd       string `json:"cwd"`       // cwd discovered in the transcript (or "-imported")
	Messages  int    `json:"messages"`  // user+assistant event count
}

// ImportTranscript copies a JSONL transcript into the claude projects
// tree so grimoire's UI can list it alongside native sessions. The file
// is read line-by-line; first few lines are validated as JSON to reject
// obvious garbage. The first event with a `cwd` field decides which
// project subdirectory we write into — preserving the "this session
// belongs to project X" association. When the JSONL contains no cwd
// hint we fall back to a synthetic "-imported" directory.
//
// If suggestedName is a well-formed UUID it's reused as the session id
// (so re-importing the same file is idempotent). Otherwise a fresh
// UUID is generated. Collisions in the destination directory are
// avoided by appending "-2", "-3" etc to the filename.
func ImportTranscript(reader io.Reader, suggestedName string) (ImportResult, error) {
	root, err := ProjectsRoot()
	if err != nil {
		return ImportResult{}, err
	}

	// Read the whole stream — JSONL is line-oriented so we need it all
	// to count messages and discover cwd. Reasonable upper bound on
	// session size is a few MB; we cap at 32 MB to avoid pathological
	// uploads. Adjust if real workloads need more.
	limited := io.LimitReader(reader, 32*1024*1024)
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var (
		lines     []string
		cwd       string
		msgCount  int
		validJSON bool
	)
	for scanner.Scan() {
		raw := scanner.Bytes()
		lines = append(lines, string(raw))
		// Cheap validation: every non-empty line must parse. We bail on
		// the first parse error so an HTML page upload fails fast.
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var probe struct {
			Type string `json:"type"`
			Cwd  string `json:"cwd"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return ImportResult{}, fmt.Errorf("not a valid JSONL transcript (line %d not JSON): %w", len(lines), err)
		}
		validJSON = true
		if cwd == "" && probe.Cwd != "" {
			cwd = probe.Cwd
		}
		if probe.Type == "user" || probe.Type == "assistant" {
			msgCount++
		}
	}
	if err := scanner.Err(); err != nil {
		return ImportResult{}, fmt.Errorf("read: %w", err)
	}
	if !validJSON || len(lines) == 0 {
		return ImportResult{}, fmt.Errorf("empty or invalid JSONL transcript")
	}
	if msgCount == 0 {
		return ImportResult{}, fmt.Errorf("transcript has no user/assistant messages — looks like metadata-only")
	}

	// Always assign a fresh UUID to imports. Reusing the original
	// UUID from the file name caused collisions with native sessions
	// (re-import overwrites the live worker's transcript), and made
	// re-imports indistinguishable from the original — so the user
	// couldn't tell which row was the imported copy. Fresh UUID also
	// lets us rewrite the in-line `sessionId` references below so
	// every event in the transcript matches the new file name.
	sessionID := genUUID()
	_ = suggestedName // intentionally ignored — fresh UUID always

	// Pick destination directory. Sanitized-cwd matches what native
	// claude uses, so the imported transcript shows up under the right
	// project in our listing. No cwd → "-imported" bucket.
	subdir := "-imported"
	if cwd != "" {
		subdir = SanitizeCwd(cwd)
	}
	destDir := filepath.Join(root, subdir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return ImportResult{}, fmt.Errorf("mkdir %s: %w", destDir, err)
	}

	// Avoid clobbering existing transcripts. If the chosen name is
	// taken, suffix with -2, -3, … until we find a free slot.
	destPath := uniquePath(destDir, sessionID)

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return ImportResult{}, fmt.Errorf("create %s: %w", destPath, err)
	}
	w := bufio.NewWriter(out)
	// Find the source's original sessionId so we can rewrite every
	// in-line reference to it. We don't trust scanner.Text earlier
	// (it already parsed only `type` and `cwd`), so do a second pass.
	originalID := findOriginalSessionID(lines)
	for _, ln := range lines {
		out := ln
		if originalID != "" && originalID != sessionID {
			out = strings.ReplaceAll(out, originalID, sessionID)
		}
		_, _ = w.WriteString(out)
		_, _ = w.WriteString("\n")
	}
	if err := w.Flush(); err != nil {
		_ = out.Close()
		return ImportResult{}, fmt.Errorf("flush: %w", err)
	}
	if err := out.Close(); err != nil {
		return ImportResult{}, fmt.Errorf("close: %w", err)
	}

	// Write a sidecar marker so listing can flag this session as
	// "imported" even when the JSONL lives in a normal project dir
	// (i.e. the source carried a cwd field). Empty file — its
	// existence is the signal. No error handling: failing to mark
	// isn't fatal, listing just won't show the badge for this one.
	markerPath := strings.TrimSuffix(destPath, ".jsonl") + ".imported"
	if f, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644); err == nil {
		_ = f.Close()
	}

	// Derive the final sessionId from the filename — it may have been
	// suffixed for uniqueness.
	finalSessionID := strings.TrimSuffix(filepath.Base(destPath), ".jsonl")
	return ImportResult{
		SessionID: finalSessionID,
		Path:      destPath,
		Cwd:       cwd,
		Messages:  msgCount,
	}, nil
}

// findOriginalSessionID scans the first ~50 lines for the first
// "sessionId":"..." field. Claude writes the same UUID into every
// event so the first match is canonical. We use it to rewrite every
// event to the new UUID on import — otherwise re-attaching to the
// imported transcript via `claude --resume <new-uuid>` would fail
// because the events still claim the OLD id.
func findOriginalSessionID(lines []string) string {
	for i, ln := range lines {
		if i > 50 {
			break
		}
		var probe struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal([]byte(ln), &probe); err == nil && probe.SessionID != "" {
			return probe.SessionID
		}
	}
	return ""
}

// uniquePath returns a path inside destDir for the given base name,
// suffixing with -2, -3, … if the obvious filename is already taken.
func uniquePath(destDir, base string) string {
	candidate := filepath.Join(destDir, base+".jsonl")
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 2; i < 1000; i++ {
		candidate = filepath.Join(destDir, fmt.Sprintf("%s-%d.jsonl", base, i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	// Astronomical fallback — caller will get an O_EXCL error and bail.
	return candidate
}

// genUUID returns a v4 UUID (kept private to this package to avoid
// cross-package dependency on the daemon's genUUID).
func genUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
