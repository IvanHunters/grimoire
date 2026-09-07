package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StoreKind says which shelf of the session store a relocated
// transcript sits on. Both shelves behave identically on disk — the
// difference is intent, and what the UI shows.
type StoreKind string

const (
	// StoreArchive holds sessions the user put away deliberately.
	StoreArchive StoreKind = "archive"
	// StoreTrash holds sessions the user deleted.
	StoreTrash StoreKind = "trash"
)

const (
	manifestName     = "manifest.json"
	stubBackupSuffix = ".stub-before-restore"
)

// Manifest records where a bundle came from so a restore puts it back
// exactly where it was instead of reconstructing the project dir from
// the transcript body. Folders written before manifests existed restore
// through the cwd fallback in RestoreFromStore.
type Manifest struct {
	SessionID   string    `json:"session_id"`
	Kind        StoreKind `json:"kind"`
	OriginPath  string    `json:"origin_path"`
	Cwd         string    `json:"cwd"`
	Name        string    `json:"name"`
	MovedAtNano int64     `json:"moved_at_nano"`
}

// StoreEntry is one relocated session as the API, MCP tools and the UI
// see it.
type StoreEntry struct {
	SessionID  string    `json:"sessionId"`
	Kind       StoreKind `json:"kind"`
	Dir        string    `json:"dir"`
	JSONLPath  string    `json:"jsonlPath"`
	OriginPath string    `json:"originPath"`
	Cwd        string    `json:"cwd"`
	Name       string    `json:"name"`
	SizeBytes  int64     `json:"sizeBytes"`
	MovedAt    time.Time `json:"movedAt"`
}

// StoreRoot returns the single directory holding both archived and
// deleted sessions. It is a SIBLING of ~/.claude/projects, never a
// child: anything under the projects root gets re-listed by ScanAll and
// the session reappears in the sidebar as if it were never put away.
func StoreRoot() (string, error) {
	root, err := ProjectsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(root), ".md-editor-store"), nil
}

// StoreKindRoot returns the archive/ or trash/ shelf inside the store.
func StoreKindRoot(kind StoreKind) (string, error) {
	if kind != StoreArchive && kind != StoreTrash {
		return "", fmt.Errorf("unknown store kind %q", kind)
	}
	store, err := StoreRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(store, string(kind)), nil
}

// LegacyTrashRoot is the pre-store delete location. Reads still look
// here so sessions deleted by older builds stay findable and
// restorable; nothing new is ever written to it.
func LegacyTrashRoot() (string, error) {
	return TrashRoot()
}

// MoveTranscriptToStore relocates a session's transcript and every
// sidecar it owns into the store, then records a manifest describing
// where it came from. Returns the created folder.
func MoveTranscriptToStore(jsonlPath, sessionID string, kind StoreKind, nowNano int64) (string, error) {
	shelf, err := StoreKindRoot(kind)
	if err != nil {
		return "", err
	}
	// Read identity BEFORE the move, while the file is still in place.
	var cwd, name string
	if hdr, hdrErr := ReadHeader(jsonlPath); hdrErr == nil {
		cwd, name = hdr.Cwd, hdr.Name
	}

	dest := filepath.Join(shelf, sessionID+"-"+strconv.FormatInt(nowNano, 10))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if err := moveBundle(jsonlPath, dest); err != nil {
		return "", err
	}

	m := Manifest{
		SessionID:   sessionID,
		Kind:        kind,
		OriginPath:  jsonlPath,
		Cwd:         cwd,
		Name:        name,
		MovedAtNano: nowNano,
	}
	if err := writeManifest(dest, m); err != nil {
		// The data is safe in the store; only the breadcrumb failed, and
		// restore still works through the cwd fallback.
		return dest, nil
	}
	return dest, nil
}

// moveBundle relocates the transcript, its <stem>/ sidecar dir
// (subagents + tool-results) and any <stem>.jsonl.* siblings
// (.archive.* / .ledger.md) into destDir. Only the transcript itself
// must succeed; sidecars are best-effort so one stubborn file never
// aborts a move half-way.
func moveBundle(jsonlPath, destDir string) error {
	dir := filepath.Dir(jsonlPath)
	base := filepath.Base(jsonlPath)
	stem := strings.TrimSuffix(base, ".jsonl")

	if err := os.Rename(jsonlPath, filepath.Join(destDir, base)); err != nil {
		return err
	}
	sidecarDir := filepath.Join(dir, stem)
	if info, err := os.Stat(sidecarDir); err == nil && info.IsDir() {
		_ = os.Rename(sidecarDir, filepath.Join(destDir, stem))
	}
	if siblings, _ := filepath.Glob(filepath.Join(dir, base+".*")); len(siblings) > 0 {
		for _, s := range siblings {
			_ = os.Rename(s, filepath.Join(destDir, filepath.Base(s)))
		}
	}
	return nil
}

func writeManifest(dir string, m Manifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), append(raw, '\n'), 0o644)
}

// ReadManifest loads the breadcrumb a store folder was written with.
func ReadManifest(dir string) (Manifest, error) {
	var m Manifest
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	return m, nil
}

// RestoreFromStore moves a session's bundle back into its project dir
// and removes the now-empty store folder. It returns the restored
// transcript path, which is what `claude --resume` needs to see.
//
// A file already sitting at the target is never overwritten: it is kept
// alongside with a .stub-before-restore suffix. That matters because a
// delete leaves a short metadata stub behind, and the stub is the NEWER
// file — overwrite it blindly in the other direction and the real
// history is lost.
func RestoreFromStore(sessionID string) (string, error) {
	entry, err := findStoreEntry(sessionID)
	if err != nil {
		return "", err
	}

	destPath := entry.OriginPath
	if destPath == "" {
		if entry.Cwd == "" {
			return "", fmt.Errorf("session %s: no manifest and no cwd in transcript, cannot tell where it belongs", sessionID)
		}
		root, rootErr := ProjectsRoot()
		if rootErr != nil {
			return "", rootErr
		}
		destPath = filepath.Join(root, SanitizeCwd(entry.Cwd), sessionID+".jsonl")
	}
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}

	names, err := os.ReadDir(entry.Dir)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		if n.Name() == manifestName {
			continue
		}
		src := filepath.Join(entry.Dir, n.Name())
		dst := filepath.Join(destDir, n.Name())
		if _, statErr := os.Stat(dst); statErr == nil {
			if bakErr := displaceExisting(dst); bakErr != nil {
				return "", bakErr
			}
		}
		if err := os.Rename(src, dst); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(entry.Dir); err != nil {
		return "", err
	}
	return destPath, nil
}

// displaceExisting renames whatever occupies path out of the way,
// picking a free suffix so an earlier backup is never clobbered.
func displaceExisting(path string) error {
	bak := path + stubBackupSuffix
	for i := 2; ; i++ {
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			break
		}
		bak = path + stubBackupSuffix + "-" + strconv.Itoa(i)
	}
	return os.Rename(path, bak)
}

// ListStore returns relocated sessions. Pass an empty kind for
// everything, including folders written by the pre-store delete path.
func ListStore(kind StoreKind) ([]StoreEntry, error) {
	shelves, err := shelvesFor(kind)
	if err != nil {
		return nil, err
	}
	var out []StoreEntry
	for _, sh := range shelves {
		entries, err := os.ReadDir(sh.dir)
		if err != nil {
			continue // shelf not created yet
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if entry, ok := readStoreDir(filepath.Join(sh.dir, e.Name()), sh.kind); ok {
				out = append(out, entry)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].MovedAt.After(out[j].MovedAt) })
	return out, nil
}

type shelf struct {
	dir  string
	kind StoreKind
}

func shelvesFor(kind StoreKind) ([]shelf, error) {
	var kinds []StoreKind
	switch kind {
	case "":
		kinds = []StoreKind{StoreArchive, StoreTrash}
	case StoreArchive, StoreTrash:
		kinds = []StoreKind{kind}
	default:
		return nil, fmt.Errorf("unknown store kind %q", kind)
	}

	var out []shelf
	for _, k := range kinds {
		dir, err := StoreKindRoot(k)
		if err != nil {
			return nil, err
		}
		out = append(out, shelf{dir: dir, kind: k})
	}
	// Folders from the pre-store delete path read as trash.
	if kind == "" || kind == StoreTrash {
		if legacy, err := LegacyTrashRoot(); err == nil {
			out = append(out, shelf{dir: legacy, kind: StoreTrash})
		}
	}
	return out, nil
}

// readStoreDir turns one store folder into an entry, preferring the
// manifest and falling back to reading the transcript itself.
func readStoreDir(dir string, kind StoreKind) (StoreEntry, bool) {
	sessionID := sessionIDFromStoreDir(filepath.Base(dir))
	jsonl := filepath.Join(dir, sessionID+".jsonl")
	info, err := os.Stat(jsonl)
	if err != nil {
		// Older folders may hold only an archive sidecar or a stub.
		matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		found := ""
		for _, m := range matches {
			if !strings.Contains(filepath.Base(m), ".archive.") && !strings.Contains(filepath.Base(m), ".STUB") {
				found = m
				break
			}
		}
		if found == "" {
			return StoreEntry{}, false
		}
		jsonl = found
		if info, err = os.Stat(jsonl); err != nil {
			return StoreEntry{}, false
		}
	}

	entry := StoreEntry{
		SessionID: sessionID,
		Kind:      kind,
		Dir:       dir,
		JSONLPath: jsonl,
		SizeBytes: info.Size(),
		MovedAt:   info.ModTime(),
	}
	if m, err := ReadManifest(dir); err == nil {
		entry.OriginPath = m.OriginPath
		entry.Cwd = m.Cwd
		entry.Name = m.Name
		if m.Kind != "" {
			entry.Kind = m.Kind
		}
		if m.SessionID != "" {
			entry.SessionID = m.SessionID
		}
		if m.MovedAtNano > 0 {
			entry.MovedAt = time.Unix(0, m.MovedAtNano)
		}
	}
	if entry.Cwd == "" || entry.Name == "" {
		if hdr, err := ReadHeader(jsonl); err == nil {
			if entry.Cwd == "" {
				entry.Cwd = hdr.Cwd
			}
			if entry.Name == "" {
				entry.Name = hdr.Name
			}
		}
	}
	return entry, true
}

// findStoreEntry locates one session across both shelves and the legacy
// trash. When a session was put away more than once, the most recent
// folder wins.
func findStoreEntry(sessionID string) (StoreEntry, error) {
	all, err := ListStore("")
	if err != nil {
		return StoreEntry{}, err
	}
	for _, e := range all {
		if e.SessionID == sessionID {
			return e, nil
		}
	}
	return StoreEntry{}, fmt.Errorf("session %s is not in the archive or trash", sessionID)
}

// sessionIDFromStoreDir strips the "-<nanos>" suffix folders are named
// with. Names that carry no suffix (or a non-numeric one) are returned
// unchanged.
func sessionIDFromStoreDir(name string) string {
	idx := strings.LastIndex(name, "-")
	if idx < 0 {
		return name
	}
	if _, err := strconv.ParseInt(name[idx+1:], 10, 64); err != nil {
		return name
	}
	return name[:idx]
}

// FindStoreEntry locates a relocated session by id across both shelves.
// Callers need it before a restore to learn the cwd the session belongs
// to, which is what a subsequent start has to run in.
func FindStoreEntry(sessionID string) (StoreEntry, error) {
	return findStoreEntry(sessionID)
}

// EnsureRestored guarantees a session's transcript is sitting in its
// project dir, restoring it from the archive or trash when needed, and
// reports whether a restore actually happened. Callers that want to run
// a session found in search go through this: `claude --resume` only
// sees transcripts under the project dir for the session's cwd, so a
// stored session has to come back before it can start.
func EnsureRestored(sessionID string) (path string, restored bool, err error) {
	if live, liveErr := SessionPath(sessionID); liveErr == nil {
		return live, false, nil
	}
	restoredPath, err := RestoreFromStore(sessionID)
	if err != nil {
		return "", false, err
	}
	return restoredPath, true, nil
}
