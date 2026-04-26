# Markdown Editor - Project Instructions

## Project Overview

Web-based markdown editor with Go backend and React frontend.

**Key Features:**
- Markdown editing with synchronized scroll preview
- Folders/subfolders organization with collapsible sidebar
- Search & replace with regex (Cmd+F / Cmd+H)
- GitHub Flavored Markdown + Mermaid diagrams
- Syntax highlighting (200+ languages via Prism.js)
- Image upload and clipboard paste
- Wikilinks and graph view
- Claude AI chat integration with MCP tools
- Project integration (link notes to local code repositories)

## Technology Stack

### Backend (Go)
- **Framework**: `github.com/go-chi/chi/v5` - HTTP router
- **Database**: `go.mongodb.org/mongo-driver` - MongoDB
- **AI**: `github.com/anthropics/anthropic-sdk-go` - Claude API
- **Markdown**: `github.com/yuin/goldmark`
- **Validation**: `github.com/go-playground/validator/v10`
- **Logging**: `log/slog` (stdlib)

### Frontend (React + TypeScript)
- **Build**: Vite
- **Routing**: react-router-dom
- **Styling**: Tailwind CSS + shadcn/ui
- **Editor**: CodeMirror 6 with markdown mode
- **Search/Replace**: `@codemirror/search` (regex, case-sensitive)
- **Preview**: react-markdown + remark-gfm + rehype-mermaid + rehype-prism-plus
- **Upload**: react-dropzone
- **Icons**: lucide-react
- **State**: React Context

### MCP Server
- **Language**: Go
- **SDK**: `github.com/mark3labs/mcp-go`

## Project Structure

```
markdown-editor/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/         # HTTP handlers
│   │   ├── storage/     # DB operations
│   │   ├── models/      # data structures
│   │   └── config/
│   └── go.mod
│
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   │   ├── layout/     # Header, Sidebar
│   │   │   ├── editor/     # EditorTextarea, Toolbar
│   │   │   ├── preview/    # Preview
│   │   │   ├── graph/      # GraphView
│   │   │   ├── chat/       # ChatPanel
│   │   │   ├── modals/     # NewNote, Upload, etc.
│   │   │   └── common/     # WelcomeScreen, ResizeHandle
│   │   ├── contexts/       # NotesContext
│   │   ├── api/            # API client
│   │   ├── utils/          # wikilinks, forceLayout
│   │   └── pages/          # HomePage
│   ├── package.json
│   └── vite.config.ts
│
├── mcp/
│   ├── main.go          # MCP server
│   └── go.mod
│
├── data/
│   ├── mongodb/         # MongoDB data
│   └── uploads/         # Uploaded images
│
└── design-prototype.html  # Reference HTML prototype
```

## API Endpoints

### Notes
- `GET /api/notes` - list all notes
- `GET /api/notes/:id` - get note by ID
- `POST /api/notes` - create note
- `PUT /api/notes/:id` - update note
- `DELETE /api/notes/:id` - delete note
- `GET /api/search?q=query` - search notes

### Folders
- `GET /api/folders` - get folder tree
- `POST /api/folders` - create folder
- `DELETE /api/folders?path=...` - delete folder
- `PUT /api/folders/move` - move note/folder

### Files
- `POST /api/upload` - upload image
- `GET /uploads/:year/:month/:filename` - serve file

### AI (Claude)
- `POST /api/claude/assist` - text assistance
- `WS /api/claude/chat` - WebSocket chat

### Health
- `GET /health` - health check

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
  outgoing_links: ["id1", ...], // wikilinks
  backlinks: ["id2", ...]
}
```

**Indexes:** path (unique), folder, updated_at, text search (title + content)

### folders
```javascript
{
  path: "folder/subfolder",
  created_at: ISODate
}
```

**Index:** path (unique)

## Development Workflow

### Start MongoDB
```bash
docker compose up -d
```

### Run Backend
```bash
cd backend
go run cmd/server/main.go
# Listens on :8080
```

### Run Frontend
```bash
cd frontend
npm install  # first time only
npm run dev
# Runs on :5173, proxies API to :8080
```

## Code Conventions

### Go Backend
- Use `internal/` for private packages
- Always check errors, return early
- Use `slog` with structured fields
- Validate with struct tags
- MongoDB: always use context with timeout

### React Frontend
- TypeScript strict mode
- Functional components + hooks
- Centralized API calls in `api/`
- Tailwind utility classes
- PascalCase for components

### Git Workflow
- Feature branches: `feat/feature-name`
- Commit after each logical change
- Format: `type(scope): description`
- Types: feat, fix, docs, style, refactor, test, chore
- **ALWAYS use --signoff**: `git commit --signoff`

## MCP Tools for Claude

Claude has access to:
- `read_current_note()` - read note in editor (when user asks)
- `update_current_note(content)` - update note in editor
- `list_notes(folder)` - list all notes
- `search_notes(query)` - search by content
- `read_note(path)` - read specific note
- `create_note(path, content)` - create new note
- `create_folder(path)` - create folder
- `get_note_connections()` - get wikilinks for graph

**Project mode tools (when note has `project_path`):**
- `read_project_file(path)` - read file from project
- `write_project_file(path, content)` - edit project files
- `list_project_files(pattern)` - list files
- `search_project(query, pattern)` - search in code
- `git_status()`, `git_pull()`, `git_diff()` - Git operations
- `run_tests(command)` - run tests

## Environment Variables

### Backend
```bash
PORT=8080
MONGODB_URI=mongodb://localhost:27017
MONGODB_DATABASE=markdown_editor
DATA_DIR=./data
MAX_UPLOAD_SIZE=10485760
ALLOWED_ORIGINS=http://localhost:5173
ANTHROPIC_API_KEY=sk-ant-...  # if using API directly
```

### Frontend
```bash
VITE_API_URL=http://localhost:8080
```

## Key Features Implementation

### Wikilinks
- Syntax: `[[note-title]]` or `[[note-title|alias]]`
- Parsed with regex: `/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g`
- Resolution: exact path → exact title → fuzzy match
- Graph connections built from wikilinks

### Force-Directed Graph
- Physics simulation with forces: repulsion, attraction, center, damping
- Implementation in `utils/forceLayout.ts`
- 200 iterations for stable layout

### Chat Sessions
- Multiple independent sessions with separate histories
- Dangerous mode per-session
- WebSocket connection to Claude subprocess
- Tool use visualization

### Project Integration
- Link notes to local repositories via frontmatter
- Auto-discovery: searches `~/git/` for matching directories
- Claude subprocess runs in project directory
- Full file access + git operations

### Image Paste from Clipboard
- Detects pasted images via `ClipboardEvent.clipboardData.items`
- Shows upload modal with preview and name editing
- Inserts markdown at saved cursor position

## Development Commands

```bash
# Backend
cd backend
go mod tidy
go test ./...
go run cmd/server/main.go

# Frontend
cd frontend
npm install
npm run dev
npm run build
npm run lint

# MCP
cd mcp
go build -o markdown-mcp
```

## Security Notes

- Validate MIME types for uploads
- Use `filepath.Clean()` to prevent path traversal
- CORS: allow only frontend origin
- No authentication in MVP (localhost only)
- Validate all API inputs

## Future Enhancements

### Phase 2
- Export to PDF/Word/Markdown archive
- Import from Obsidian
- Tags in frontmatter
- Wikilink autocomplete

### Phase 3
- Mobile-responsive UI
- Dark mode
- Drag & drop notes between folders
- Note templates

### Phase 4
- Git integration for version control
- Real-time collaboration
- Multi-user authentication
- Plugins system
