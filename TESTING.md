# Testing Guide

## Prerequisites

- MongoDB running (`docker compose up -d`)
- Backend running (`make backend` or `go run cmd/markdown-editor/main.go serve`)
- Frontend running (`make frontend` or `npm run dev` in `frontend/`)
- Claude CLI installed (`claude` in PATH)

## Health Check

```bash
curl http://localhost:8080/health
# {"status":"ok","mongodb":"connected","time":"..."}
```

## API Tests

### Notes CRUD

```bash
# Create
curl -X POST http://localhost:8080/api/notes \
  -H "Content-Type: application/json" \
  -d '{"title":"Test","content":"# Hello","folder":""}'

# List
curl http://localhost:8080/api/notes

# Search
curl "http://localhost:8080/api/search?q=hello"
```

### Tags

```bash
# All tags with counts
curl http://localhost:8080/api/tags

# Search by tags
curl "http://localhost:8080/api/tags/search?tags=kubernetes,networking"
```

### Tasks

```bash
# Create task
curl -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"title":"Fix bug","status":"todo","priority":"high"}'

# List tasks
curl http://localhost:8080/api/tasks
```

### Upload

```bash
curl -X POST http://localhost:8080/api/upload \
  -F "file=@/path/to/image.png"
```

## WebSocket — Claude Terminal

Connect to `ws://localhost:8080/claude-chat` with a `session_id` query param.

Send a message:
```json
{"type":"user_message","content":"list my notes","session_id":"test-123"}
```

The terminal streams PTY output back as binary WebSocket frames.

## Real-time Events

Connect to `ws://localhost:8080/api/events`.

Events are emitted when Claude MCP tools modify notes/folders:
```json
{"type":"note_updated","payload":{"id":"...","title":"..."}}
```

## Manual UI Checklist

- [ ] Create note, edit in split view, verify live preview
- [ ] Upload image via drag & drop and via clipboard paste
- [ ] Add `[[wikilink]]`, hover to see popup, click to navigate
- [ ] Open Graph View, verify nodes and edges
- [ ] Open Claude terminal, run `list_notes`, verify MCP tool response
- [ ] Export note to PDF, Word, and ZIP
- [ ] Open Task board, create story, add tasks
- [ ] Mobile layout at 375px width
