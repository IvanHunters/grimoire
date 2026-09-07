package discovery

import (
	"os"
	"path/filepath"
	"time"
)

// MigrateLegacyTrash folds folders written by the pre-store delete path
// into the trash shelf of the central store, and backfills the manifest
// those folders were written without. It returns how many folders moved.
//
// Safe to call on every start: folders that already moved are gone from
// the legacy root, so a second pass has nothing to do. A name collision
// on the shelf is skipped rather than resolved — overwriting is how
// history gets lost, and leaving the folder in place keeps it readable
// (ListStore covers the legacy root too).
func MigrateLegacyTrash() (int, error) {
	legacyRoot, err := LegacyTrashRoot()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // nothing was ever deleted by an older build
		}
		return 0, err
	}
	shelf, err := StoreKindRoot(StoreTrash)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(shelf, 0o755); err != nil {
		return 0, err
	}

	moved := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src := filepath.Join(legacyRoot, e.Name())
		dst := filepath.Join(shelf, e.Name())
		if _, statErr := os.Stat(dst); statErr == nil {
			continue // occupied — leave the legacy copy alone
		}
		if err := os.Rename(src, dst); err != nil {
			continue // best-effort: a stuck folder must not abort the rest
		}
		moved++
		backfillManifest(dst, e.Name())
	}
	return moved, nil
}

// backfillManifest writes the breadcrumb a legacy folder never had,
// reading identity back out of the transcript it holds. Best-effort:
// without it, restore still works through the cwd fallback.
func backfillManifest(dir, folderName string) {
	if _, err := ReadManifest(dir); err == nil {
		return
	}
	entry, ok := readStoreDir(dir, StoreTrash)
	if !ok {
		return
	}
	_ = writeManifest(dir, Manifest{
		SessionID:   sessionIDFromStoreDir(folderName),
		Kind:        StoreTrash,
		Cwd:         entry.Cwd,
		Name:        entry.Name,
		MovedAtNano: modTimeNano(entry.JSONLPath),
	})
}

func modTimeNano(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return time.Now().UnixNano()
	}
	return info.ModTime().UnixNano()
}
