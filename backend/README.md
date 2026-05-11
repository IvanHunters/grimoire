# Backend

Go backend for grimoire. Single binary with two subcommands: `serve` (HTTP + WebSocket) and `mcp` (MCP server for Claude).

## Commands

```bash
# Start HTTP + WebSocket server on :8080
go run cmd/markdown-editor/main.go serve

# Start MCP server (invoked automatically by Claude CLI)
go run cmd/markdown-editor/main.go mcp
```

## Build

```bash
go build -o markdown-editor cmd/markdown-editor/main.go
./markdown-editor serve
```

## Environment

Copy `.env.example` to `.env`:

```bash
HTTP_PORT=8080
MONGODB_URI=mongodb://localhost:27017
MONGODB_DATABASE=markdown_editor
DATA_DIR=./data
ALLOWED_ORIGINS=http://localhost:5173
SESSION_TIMEOUT=300
LOG_LEVEL=info
```

## Package Structure

```
internal/
├── api/         HTTP handlers (notes, folders, tasks, tags, sessions, upload)
├── claude/      PTY subprocess management, session manager, auto-discovery
├── events/      In-process event bus (MCP writes -> WebSocket clients)
├── index/       In-memory inverted tag index (microsecond tag search)
├── middleware/  CORS, structured logging, panic recovery
├── models/      Note, Folder, Task, ClaudeSession
├── scheduler/   Background goroutine for inactive session cleanup
├── storage/     MongoDB CRUD for all models
└── websocket/   Claude terminal WebSocket handler (fan-out to subscribers)

cmd/markdown-editor/cmd/
├── serve.go                   Sets up Chi router, starts servers
├── mcp.go                     Registers all 66 MCP tools
├── content_editing_tools.go   append, prepend, insert, replace, delete
├── content_structure_tools.go headings, sections
├── attachment_tools.go        upload_image, list_attachments, delete_attachment
├── folder_tools.go            list, create, delete, rename, move folders
├── graph_tools.go             get_graph_data, find_related_notes
└── task_tools.go              full task/story/project CRUD + linking
```

## MCP Auto-configuration

When `serve` starts a Claude subprocess, it writes `.claude/mcp_servers.json` in the
working directory pointing to the same binary with the `mcp` subcommand. Claude CLI
reads this on startup and connects automatically.

## Session Lifecycle

1. Frontend connects via `WS /claude-chat?session_id=<id>`
2. Backend starts `claude` as a PTY subprocess in the note's working directory
3. PTY output is broadcast to all WebSocket subscribers of that session
4. On inactivity (default 5 min), the scheduler sends Ctrl+D then SIGTERM then SIGKILL
5. Session state (name, messages) is persisted to MongoDB
