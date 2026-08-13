package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
)

// Verifies that deleting a session's transcript is a recoverable MOVE
// into a trash dir (not a destructive os.Remove), and that it carries
// along the sidecar dir (subagents/tool-results) + archive/ledger
// siblings so nothing is orphaned or lost. This is the regression guard
// for the incident where a Trash click permanently destroyed session
// 3315's history and orphaned its subagents/ sidecar.
func TestMoveTranscriptToTrash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "86ac30af-8485-4325-870a-d4102af0eb1e"
	proj := filepath.Join(root, "-Users-ivan--markdown-editor-sessions-default")
	if err := os.MkdirAll(filepath.Join(proj, uuid, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, uuid+".jsonl")
	sidecarFile := filepath.Join(proj, uuid, "subagents", "agent-x.jsonl")
	archive := filepath.Join(proj, uuid+".jsonl.archive.20260101T000000Z.jsonl")
	ledger := filepath.Join(proj, uuid+".jsonl.ledger.md")
	for _, f := range []string{jsonl, sidecarFile, archive, ledger} {
		if err := os.WriteFile(f, []byte("data\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	trashRoot, err := transcriptTrashRoot()
	if err != nil {
		t.Fatal(err)
	}

	// Trash must live OUTSIDE the projects root, else scanRoot re-lists
	// the deleted session and it reappears in the sidebar.
	if strings.HasPrefix(filepath.Clean(trashRoot)+string(os.PathSeparator), filepath.Clean(root)+string(os.PathSeparator)) {
		t.Fatalf("trash root %q must not be inside projects root %q", trashRoot, root)
	}

	dest, err := moveTranscriptToTrash(jsonl, uuid, trashRoot, 1735689600000000000)
	if err != nil {
		t.Fatalf("moveTranscriptToTrash: %v", err)
	}

	// Originals are gone from the live project dir.
	for _, f := range []string{jsonl, archive, ledger} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("expected %s moved out of project dir, still present", filepath.Base(f))
		}
	}
	if _, err := os.Stat(filepath.Join(proj, uuid)); !os.IsNotExist(err) {
		t.Fatal("expected sidecar dir moved out of project dir, still present")
	}

	// Everything landed in the trash dest — nothing lost.
	for _, name := range []string{
		uuid + ".jsonl",
		uuid + ".jsonl.archive.20260101T000000Z.jsonl",
		uuid + ".jsonl.ledger.md",
		filepath.Join(uuid, "subagents", "agent-x.jsonl"),
	} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Fatalf("expected %s recoverable in trash, got: %v", name, err)
		}
	}

	// SessionPath no longer resolves it (removed from the live listing).
	if _, err := discovery.SessionPath(uuid); err == nil {
		t.Fatal("expected SessionPath to miss after trashing")
	}
}
