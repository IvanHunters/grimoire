# Markdown Editor Backend

Go backend for markdown editor with Claude AI integration.

## Features

- **REST API** for notes and folders management
- **MongoDB** storage with full-text search
- **WebSocket** server for Claude AI chat
- **PTY-based subprocess** management for Claude CLI
- **Wikilinks** parsing and backlinks
- **Autodiscovery** for project paths
- **Image upload** with validation
- **Graceful shutdown** of all Claude sessions

## Architecture

### Single Binary with Multiple Modes

One binary `markdown-editor` with subcommands:
- **`serve`** - Runs HTTP (`:8080`) + WebSocket (`:3000`) servers together
- **`mcp`** - Runs MCP server for Claude Code integration (stdio)

### Project Structure

```
backend/
├── cmd/
│   └── markdown-editor/  # Single entry point with subcommands
│       ├── main.go
│       └── cmd/
│           ├── root.go   # Root command
│           ├── serve.go  # HTTP + WebSocket servers
│           └── mcp.go    # MCP server
├── internal/
│   ├── api/          # HTTP handlers
│   ├── storage/      # MongoDB operations
│   ├── models/       # Data structures
│   ├── config/       # Configuration
│   ├── middleware/   # CORS, logging, recovery
│   ├── claude/       # Subprocess management
│   └── websocket/    # WebSocket handler
└── go.mod
```

## Dependencies

- `github.com/go-chi/chi/v5` - HTTP router
- `go.mongodb.org/mongo-driver` - MongoDB client
- `github.com/gorilla/websocket` - WebSocket implementation
- `github.com/creack/pty` - PTY for Claude subprocess
- `github.com/google/uuid` - UUID generation
- `github.com/go-playground/validator/v10` - Validation
- `github.com/kelseyhightower/envconfig` - Config from environment

## Prerequisites

- Go 1.21+
- MongoDB 7.0+
- Claude CLI (`claude`) installed and in PATH

## Installation

```bash
# Install dependencies
go mod download

# Build HTTP server
go build -o server ./cmd/server

# Build WebSocket server
go build -o websocket ./cmd/websocket
```

## Configuration

Copy `.env.example` to `.env` and adjust settings:

```bash
cp .env.example .env
```

See `.env.example` for all available options.

## Running

### Start MongoDB

```bash
docker compose up -d
```

### Start HTTP Server

```bash
./server
# or
go run ./cmd/server
```

Server will start on `:8080`

### Start WebSocket Server

```bash
./websocket
# or
go run ./cmd/websocket
```

WebSocket server will start on `:3000`

## API Endpoints

### Health

- `GET /health` - Health check with MongoDB status

### Notes

- `GET /api/notes` - List all notes (query: `?folder=path`)
- `GET /api/notes/:id` - Get note by ID
- `POST /api/notes` - Create note
- `PUT /api/notes/:id` - Update note
- `DELETE /api/notes/:id` - Delete note
- `GET /api/notes/project-suggestions?title=X` - Autodiscovery suggestions

### Folders

- `GET /api/folders` - Get folder tree
- `POST /api/folders` - Create folder
- `DELETE /api/folders?path=X` - Delete folder
- `PUT /api/folders/move` - Move folder (not implemented)

### Search

- `GET /api/search?q=query` - Full-text search

### Upload

- `POST /api/upload` - Upload image (multipart/form-data)
- `GET /uploads/:year/:month/:filename` - Serve uploaded file

### WebSocket

- `WS /claude-chat` - Claude chat with real-time streaming

## WebSocket Protocol

### Client → Server

```json
{
  "type": "init",
  "sessionId": "session-1",
  "dangerousMode": false,
  "currentNote": {
    "name": "note.md",
    "content": "# Note",
    "projectPath": "~/git/github.com/user/project"
  }
}
```

Types: `init`, `message`, `stop`, `switch_session`, `restart_session`, `close_session`

### Server → Client

```json
{
  "type": "content_delta",
  "sessionId": "session-1",
  "content": "Response text"
}
```

Types: `session_started`, `message_start`, `content_delta`, `tool_use`, `message_complete`, `error`, `stopped`, `session_history`

## Features

### Wikilinks & Backlinks

Notes support wikilinks in format `[[note-title]]` or `[[note-title|alias]]`.

- Automatically parsed and stored in `outgoing_links`
- Backlinks automatically updated in linked notes
- Used for graph view connections

### Autodiscovery

When creating a project note, backend searches for matching projects in `~/git/github.com/$USER/`:

1. Exact match by normalized title
2. Fuzzy match (contains)

### PTY-based Subprocess

Claude CLI runs in PTY for interactive control:

- **Ctrl+C** sends interrupt without killing process
- **Graceful shutdown**: Ctrl+D → SIGTERM → SIGKILL
- **Background `cmd.Wait()`** prevents zombie processes
- **Thread-safe** operations with `sync.Mutex`

### Session Management

- Multiple concurrent sessions
- Per-session dangerous mode
- Automatic cleanup of inactive sessions (5 min timeout)
- Message history stored on backend
- Session switch without restart

## MongoDB Collections

### notes

```javascript
{
  id: "uuid",
  path: "folder/note.md",
  title: "Note Title",
  folder: "folder",
  content: "markdown content",
  type: "project",              // optional
  project_path: "~/git/...",    // optional
  created_at: ISODate,
  updated_at: ISODate,
  outgoing_links: ["id1", ...],
  backlinks: ["id2", ...]
}
```

**Indexes:**
- `path` (unique)
- `folder`
- `updated_at` (descending)
- `title` + `content` (text search)

### folders

```javascript
{
  path: "folder/subfolder",
  created_at: ISODate
}
```

**Index:** `path` (unique)

## Development

### Run tests

```bash
go test ./...
```

### Build

```bash
go build -o bin/server ./cmd/server
go build -o bin/websocket ./cmd/websocket
```

### Linting

```bash
golangci-lint run
```

## Troubleshooting

### MongoDB connection failed

Check that MongoDB is running:

```bash
docker compose ps
```

Restart if needed:

```bash
docker compose restart
```

### Claude subprocess not starting

Check that `claude` CLI is installed:

```bash
which claude
claude --version
```

### WebSocket connection refused

Ensure WebSocket server is running on `:3000`:

```bash
lsof -i :3000
```

## Production Deployment

1. Build binaries
2. Set environment variables
3. Run both servers with process manager (systemd, supervisor)
4. Use reverse proxy (nginx) for HTTPS and routing
5. Monitor logs and process health

## License

See LICENSE file in repository root.
