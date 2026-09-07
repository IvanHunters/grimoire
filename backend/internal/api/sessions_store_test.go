package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/discovery"
)

func testHandler() *Handler {
	return &Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// requestWithID builds a request carrying a chi URL param, which is how
// the session handlers read the session id.
func requestWithID(method, target, id string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func seedTranscript(t *testing.T, root, cwd, uuid string) string {
	t.Helper()
	dir := filepath.Join(root, discovery.SanitizeCwd(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, uuid+".jsonl")
	line := `{"type":"user","cwd":"` + cwd + `","sessionId":"` + uuid + `","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Archiving puts a session away without destroying it: the transcript
// leaves the projects dir, so the sidebar stops listing it, and lands on
// the archive shelf where search can still reach it.
func TestArchiveSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "11111111-1111-4111-8111-111111111111"
	seedTranscript(t, root, "/Users/ivan/gitops/thing", uuid)

	rec := httptest.NewRecorder()
	testHandler().ArchiveSession(rec, requestWithID(http.MethodPost, "/api/sessions/"+uuid+"/archive", uuid))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		SessionID string `json:"sessionId"`
		State     string `json:"state"`
		Dir       string `json:"dir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.State != discovery.SessionStateArchived {
		t.Errorf("state = %q, want %q", body.State, discovery.SessionStateArchived)
	}
	if _, err := discovery.SessionPath(uuid); err == nil {
		t.Error("expected the transcript to leave the projects dir")
	}
	entries, err := discovery.ListStore(discovery.StoreArchive)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].SessionID != uuid {
		t.Fatalf("archive shelf = %+v, want just %s", entries, uuid)
	}
}

// Restore is what makes an archived or deleted session runnable again:
// the transcript has to be back in the project dir before claude
// --resume can see it, so the handler reports the path it restored to.
func TestRestoreSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "22222222-2222-4222-8222-222222222222"
	const cwd = "/Users/ivan/gitops/thing"
	origin := seedTranscript(t, root, cwd, uuid)
	if _, err := discovery.MoveTranscriptToStore(origin, uuid, discovery.StoreTrash, 1735689600000000000); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	testHandler().RestoreSession(rec, requestWithID(http.MethodPost, "/api/sessions/"+uuid+"/restore", uuid))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		SessionID string `json:"sessionId"`
		State     string `json:"state"`
		Path      string `json:"path"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Path != origin {
		t.Errorf("path = %q, want %q", body.Path, origin)
	}
	if body.Cwd != cwd {
		t.Errorf("cwd = %q, want %q — the caller needs it to start the session", body.Cwd, cwd)
	}
	if body.State != discovery.SessionStateActive {
		t.Errorf("state = %q, want %q", body.State, discovery.SessionStateActive)
	}
	if _, err := discovery.SessionPath(uuid); err != nil {
		t.Errorf("expected the session resolvable again: %v", err)
	}
}

// Restoring something that was never put away is a 404, not a 500.
func TestRestoreSessionNotStored(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	rec := httptest.NewRecorder()
	testHandler().RestoreSession(rec, requestWithID(http.MethodPost, "/api/sessions/x/restore", "33333333-3333-4333-8333-333333333333"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

// The UI lists both shelves in one call, each row labelled, so a user
// browsing "put away" sessions sees archived and deleted together.
func TestListStoredSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const archived = "44444444-4444-4444-8444-444444444444"
	const deleted = "55555555-5555-4555-8555-555555555555"
	a := seedTranscript(t, root, "/Users/ivan/gitops/thing", archived)
	d := seedTranscript(t, root, "/Users/ivan/gitops/thing", deleted)
	if _, err := discovery.MoveTranscriptToStore(a, archived, discovery.StoreArchive, 1735689600000000000); err != nil {
		t.Fatal(err)
	}
	if _, err := discovery.MoveTranscriptToStore(d, deleted, discovery.StoreTrash, 1735689600000000001); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	testHandler().ListStoredSessions(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/store", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []struct {
			SessionID string `json:"sessionId"`
			Kind      string `json:"kind"`
			State     string `json:"state"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("got %d entries, want 2: %s", len(body.Entries), rec.Body.String())
	}
	states := map[string]string{}
	for _, e := range body.Entries {
		states[e.SessionID] = e.State
	}
	if states[archived] != discovery.SessionStateArchived {
		t.Errorf("%s state = %q, want archived", archived, states[archived])
	}
	if states[deleted] != discovery.SessionStateDeleted {
		t.Errorf("%s state = %q, want deleted", deleted, states[deleted])
	}
}

// A delete must land on the trash shelf of the central store, not the
// legacy directory, so restore has one place to look.
func TestDropTranscriptGoesToStoreTrash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", root)

	const uuid = "66666666-6666-4666-8666-666666666666"
	seedTranscript(t, root, "/Users/ivan/gitops/thing", uuid)

	if _, err := testHandler().dropTranscript(uuid, discovery.StoreTrash); err != nil {
		t.Fatalf("dropTranscript: %v", err)
	}
	entries, err := discovery.ListStore(discovery.StoreTrash)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].SessionID != uuid {
		t.Fatalf("trash shelf = %+v, want just %s", entries, uuid)
	}
	shelf, _ := discovery.StoreKindRoot(discovery.StoreTrash)
	if !filepath.HasPrefix(entries[0].Dir, shelf) {
		t.Errorf("bundle landed in %q, want it under %q", entries[0].Dir, shelf)
	}
}
