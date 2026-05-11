# grimoire

Personal knowledge base with Claude AI integration. Markdown editor, knowledge graph, embedded AI terminal, and task tracker — all in one dark-themed web app.

![Stack](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)
![Stack](https://img.shields.io/badge/React-18-61DAFB?style=flat&logo=react)
![Stack](https://img.shields.io/badge/MongoDB-7-47A248?style=flat&logo=mongodb)

## Features

### Editor
- Split-view markdown editor (CodeMirror 6) with live preview
- Syntax highlighting for 200+ languages via Prism.js
- GitHub Flavored Markdown + Mermaid diagrams
- Search & replace with regex (Cmd+F / Cmd+H)
- Image upload via drag & drop or clipboard paste
- Wikilinks (`[[note-title]]`) with hover popup preview

### Knowledge Graph
- Force-directed graph of all wikilink connections
- Backlinks panel per note
- In-memory tag index with instant search (microseconds vs MongoDB milliseconds)

### AI Terminal
- Embedded Claude terminal (PTY subprocess per session)
- Multiple independent sessions with persistent history
- Project mode: link a note to a local git repo, Claude runs in that directory
- Auto-discovery of git repos under `~/git/`
- 66 MCP tools for reading, editing, and organizing notes
- Dangerous mode toggle (passes `--dangerously-skip-permissions` to Claude CLI)

### Task Tracker
- Kanban board with customizable columns
- Stories (epics) with child tasks and subtasks
- Link tasks to notes and folders
- Comments, priority, due dates

### Export
- PDF (smart page breaks, inline code chips, Mermaid diagrams)
- Word (.docx)
- HTML preview
- Markdown
- ZIP archive of all notes

### Other
- Mobile-responsive layout
- Dark theme throughout
- Folder tree with drag & drop reorganization
- Full-text search across all notes

## Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+
- Docker (for MongoDB)
- [Claude CLI](https://claude.ai/download) (`claude` in PATH)

### Start

```bash
# 1. Start MongoDB
docker compose up -d

# 2. Start backend + frontend
make up
```

Backend: http://localhost:8080  
Frontend: http://localhost:5173

Or start separately:

```bash
# Backend
cd backend
go run cmd/markdown-editor/main.go serve

# Frontend (in another terminal)
cd frontend
npm install
npm run dev
```

## Project Structure

```
grimoire/
├── backend/
│   ├── cmd/markdown-editor/
│   │   ├── main.go                      # Entry point
│   │   └── cmd/
│   │       ├── serve.go                 # HTTP server
│   │       ├── mcp.go                   # MCP server (66 tools)
│   │       ├── content_editing_tools.go
│   │       ├── content_structure_tools.go
│   │       ├── attachment_tools.go
│   │       ├── folder_tools.go
│   │       ├── graph_tools.go
│   │       └── task_tools.go
│   └── internal/
│       ├── api/          # HTTP handlers
│       ├── claude/       # PTY subprocess management
│       ├── events/       # Event bus (MCP -> WebSocket -> clients)
│       ├── index/        # In-memory tag index
│       ├── middleware/   # CORS, logging, recovery
│       ├── models/       # Note, Folder, Task, Session
│       ├── scheduler/    # Session cleanup
│       ├── storage/      # MongoDB operations
│       └── websocket/    # Claude terminal WebSocket handler
│
├── frontend/src/
│   ├── components/
│   │   ├── chat/         # Claude terminal (TerminalChat, ChatPanel)
│   │   ├── editor/       # CodeMirror editor, toolbar, tag picker
│   │   ├── graph/        # Force-directed knowledge graph
│   │   ├── layout/       # Header, Sidebar
│   │   ├── modals/       # New note, rename, search, upload, ...
│   │   ├── preview/      # Markdown preview, Mermaid, wikilink popup
│   │   └── tasks/        # Kanban board, task detail, story card
│   ├── contexts/         # NotesContext, ClaudeContext, ThemeContext
│   ├── hooks/            # useWebSocket, useTerminalWebSocket, useEventsWebSocket
│   ├── pages/            # HomePage, TasksPage
│   └── utils/            # export, wikilinks, forceLayout
│
├── data/
│   ├── mongodb/          # MongoDB data (gitignored)
│   └── uploads/          # Uploaded images
│
├── docker-compose.yml
└── Makefile
```

## Architecture

Single Go binary (`./markdown-editor`) with two subcommands:

- **`serve`** — HTTP REST API + WebSocket terminal server on `:8080`
- **`mcp`** — MCP server (launched automatically by Claude CLI as a subprocess)

### MCP Integration

When a Claude terminal session starts, the backend writes `.claude/mcp_servers.json` into the working directory:

```json
{
  "markdown-editor": {
    "command": "/path/to/markdown-editor",
    "args": ["mcp"],
    "env": {
      "MONGODB_URI": "mongodb://localhost:27017",
      "MONGODB_DATABASE": "markdown_editor"
    }
  }
}
```

Claude CLI picks this up automatically and gets access to all 66 tools.

### Event Bus

MCP operations (note create/update/delete) publish events that flow to all connected WebSocket clients, keeping the frontend in sync without polling:

```
Claude MCP → Event Bus → WebSocket Handler → Frontend
     ↓
  MongoDB
```

### WebSocket Endpoints

- `WS /claude-chat` — Claude terminal multiplexer (one endpoint, multiple sessions via session ID)
- `WS /api/events` — Real-time note/folder change events

## API Reference

### Notes
```
GET    /api/notes           list all notes
GET    /api/notes/:id       get note
POST   /api/notes           create note
PUT    /api/notes/:id       update note
DELETE /api/notes/:id       delete note
GET    /api/search?q=...    full-text search
```

### Folders
```
GET    /api/folders               folder tree
POST   /api/folders               create folder
DELETE /api/folders?path=...      delete folder
PUT    /api/folders/move          move note or folder
```

### Tags
```
GET /api/tags               all tags with counts
GET /api/tags/search?tags=  notes matching tags
```

### Tasks
```
GET    /api/tasks            list tasks
POST   /api/tasks            create task
GET    /api/tasks/:id        get task
PUT    /api/tasks/:id        update task
DELETE /api/tasks/:id        delete task
```

### Sessions
```
GET    /api/sessions         list active Claude sessions
DELETE /api/sessions/:id     terminate session
```

### Files
```
POST /api/upload             upload image
GET  /uploads/:year/:month/  serve uploaded file
GET  /health                 health check
```

## Configuration

`backend/.env` (copy from `.env.example`):

```bash
HTTP_PORT=8080
MONGODB_URI=mongodb://localhost:27017
MONGODB_DATABASE=markdown_editor
DATA_DIR=./data
ALLOWED_ORIGINS=http://localhost:5173
SESSION_TIMEOUT=300   # seconds; inactive Claude sessions are terminated
LOG_LEVEL=info
```

`frontend/.env`:

```bash
VITE_API_URL=http://localhost:8080
```

## Build

```bash
# Backend binary
cd backend
go build -o markdown-editor cmd/markdown-editor/main.go

# Run
./markdown-editor serve

# Frontend production build
cd frontend
npm run build
# Serve dist/ with any static server
```

## MCP Tools

Claude has access to 66 tools organized into groups:

**Notes:** `list_notes`, `list_notes_summary`, `read_note`, `get_note_by_path`, `create_note`, `update_note`, `delete_note`, `rename_note`, `move_note`, `find_recent_notes`, `find_related_notes`, `get_note_metadata`, `get_note_headings`, `get_note_connections`, `get_note_backlinks`, `get_note_wikilinks`, `get_note_tags`, `get_notes_by_tag`, `search_notes`

**Content editing:** `append_to_note`, `prepend_to_note`, `insert_at_line`, `insert_after_heading`, `append_to_section`, `get_section_content`, `replace_text`, `delete_text`

**Tags:** `get_all_tags`, `set_tags`, `add_tags`, `remove_tags`, `search_by_tags`

**Wikilinks:** `add_wikilink`, `remove_wikilink`

**Folders:** `list_folders`, `create_folder`, `delete_folder`, `rename_folder`, `move_folder`

**Attachments:** `upload_image`, `upload_file_from_path`, `list_note_attachments`, `delete_attachment`

**Graph:** `get_graph_data`, `get_note_connections`

**Tasks:** `list_tasks`, `search_tasks`, `get_task`, `create_task`, `update_task`, `delete_task`, `move_task`, `set_task_parent`, `link_note_to_task`, `link_folder_to_task`, `link_tasks`, `add_task_comment`, `get_kanban_columns`, `get_task_board`

**Projects:** `get_current_project`, `set_project_path`, `set_note_type`, `list_projects`, `create_project`, `update_project`, `delete_project`

## License

MIT
