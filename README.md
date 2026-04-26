# Markdown Editor with Claude AI Integration

Полнофункциональный markdown редактор с интеграцией Claude AI через MCP (Model Context Protocol).

## Features

### Core Features
- 📝 **Markdown Editor** - CodeMirror 6 с real-time preview
- 📁 **Folder Organization** - древовидная структура с поддержкой вложенности
- 🔍 **Search & Replace** - с regex поддержкой (Cmd+F / Cmd+H)
- 🖼️ **Image Upload** - drag & drop и paste из clipboard
- 🔗 **Wikilinks** - `[[note-title]]` для связей между заметками
- 📊 **Graph View** - визуализация связей (Mermaid diagrams)
- 🎨 **Syntax Highlighting** - 200+ языков (Prism.js)

### AI Features (Claude Integration)
- 🤖 **Claude Chat** - полный доступ ко всем заметкам через MCP
- 💻 **Project Mode** - работа с локальными git репозиториями
- 🛠️ **MCP Tools** - 19 инструментов для управления заметками
- ⚡ **Real-time Events** - автообновление UI при изменениях через Claude
- 🔄 **Multiple Sessions** - независимые чат-сессии с историей
- ⚠️ **Dangerous Mode** - опциональный режим для деструктивных операций

## Tech Stack

### Backend (Go)
- **Chi** - HTTP router
- **MongoDB** - database
- **WebSocket** - real-time communication
- **PTY** - Claude subprocess management
- **MCP SDK** - github.com/mark3labs/mcp-go

### Frontend (React + TypeScript)
- **Vite** - build tool
- **CodeMirror 6** - editor
- **react-markdown** - preview
- **Tailwind CSS** - styling
- **WebSocket** - real-time sync

## Quick Start

### Prerequisites
- Go 1.21+
- Node.js 18+
- Docker (для MongoDB)
- Claude CLI (`claude` в PATH)

### 1. Start MongoDB

```bash
docker compose up -d
```

### 2. Start Backend

```bash
cd backend
go run cmd/markdown-editor/main.go serve
```

Backend запустится на:
- HTTP API: http://localhost:8080
- WebSocket: ws://localhost:3000

### 3. Start Frontend

```bash
cd frontend
npm install
npm run dev
```

Frontend откроется на http://localhost:5173

## Project Structure

```
markdown-editor/
├── backend/
│   ├── cmd/markdown-editor/     # Single binary with subcommands
│   │   ├── main.go              # Entry point
│   │   └── cmd/                 # Cobra commands
│   │       ├── serve.go         # HTTP + WebSocket servers
│   │       ├── mcp.go          # MCP server
│   │       ├── *_tools.go      # MCP tool implementations
│   ├── internal/
│   │   ├── api/                # HTTP handlers
│   │   ├── storage/            # MongoDB operations
│   │   ├── models/             # Data structures
│   │   ├── claude/             # Subprocess management
│   │   ├── websocket/          # WebSocket handler
│   │   ├── events/             # Event bus
│   │   ├── middleware/         # CORS, logging, etc.
│   │   └── config/             # Configuration
│
├── frontend/
│   ├── src/
│   │   ├── components/         # React components
│   │   ├── contexts/           # NotesContext, ClaudeContext
│   │   ├── hooks/              # useWebSocket
│   │   ├── api/                # API client
│   │   ├── types/              # TypeScript types
│   │   └── utils/              # Helpers
│
├── data/
│   ├── mongodb/                # MongoDB data (gitignored)
│   └── uploads/                # Uploaded images
│
├── docker-compose.yml          # MongoDB container
├── CLAUDE.md                   # Project instructions
└── TESTING.md                  # E2E testing guide
```

## Architecture

### Two-Server Pattern

1. **HTTP Server (:8080)** - REST API для CRUD операций
2. **WebSocket Server (:3000)** - Claude chat + real-time events

### MCP Integration

Backend автоматически создаёт `.claude/mcp_servers.json` при запуске Claude subprocess:

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

Claude CLI автоматически подключает MCP сервер и получает доступ к 19 инструментам.

### Event Bus Pattern

Real-time синхронизация между Claude MCP, WebSocket clients и frontend:

```
Claude MCP -> Event Bus -> WebSocket Handler -> All Clients
     |                                                |
     v                                                v
MongoDB <----------------------------------------- Frontend
```

## API Endpoints

### Notes
- `GET /api/notes` - list notes
- `GET /api/notes/:id` - get note
- `POST /api/notes` - create note
- `PUT /api/notes/:id` - update note
- `DELETE /api/notes/:id` - delete note

### Folders
- `GET /api/folders` - folder tree
- `POST /api/folders` - create folder
- `DELETE /api/folders?path=X` - delete folder
- `PUT /api/folders/move` - move note/folder

### Search & Upload
- `GET /api/search?q=query` - full-text search
- `POST /api/upload` - upload image

### WebSocket
- `WS /claude-chat` - Claude chat + events

See [TESTING.md](./TESTING.md) for complete E2E testing guide.

## MCP Tools

Claude имеет доступ к 19 инструментам:

### Notes Management
- `list_notes(folder)` - список заметок
- `read_note(path)` - прочитать заметку
- `create_note(path, content)` - создать
- `update_note(id, content)` - обновить
- `delete_note(id)` - удалить
- `rename_note(id, newPath)` - переименовать
- `move_note(id, newFolder)` - переместить
- `search_notes(query)` - поиск

### Wikilinks
- `get_note_connections(id)` - связи заметки
- `add_wikilink(fromId, toId)` - добавить связь
- `remove_wikilink(fromId, toId)` - удалить связь

### Graph Analysis
- `get_graph_data()` - полный граф с Mermaid
- `find_related_notes(id, depth)` - похожие заметки

### Folders
- `list_folders()` - список папок
- `create_folder(path)` - создать папку
- `delete_folder(path)` - удалить папку
- `rename_folder(path, newPath)` - переименовать
- `move_folder(from, to)` - переместить

### Special
- `read_current_note()` - заметка в editor
- `update_current_note(content)` - обновить текущую

## Configuration

### Backend (.env)

```bash
PORT=8080
WS_PORT=3000
MONGODB_URI=mongodb://localhost:27017
MONGODB_DATABASE=markdown_editor
DATA_DIR=./data
ALLOWED_ORIGINS=http://localhost:5173
```

### Frontend (.env)

```bash
VITE_API_URL=/api
VITE_WS_URL=ws://localhost:3000/claude-chat
```

## Development

### Build Backend

```bash
cd backend
go build -o markdown-editor cmd/markdown-editor/main.go
```

### Run Tests

```bash
# Backend
cd backend
go test ./...

# Frontend
cd frontend
npm test
```

### Single Binary Commands

```bash
# Start HTTP + WebSocket servers
./markdown-editor serve

# Start MCP server (used by Claude)
./markdown-editor mcp
```

## Deployment

### Backend

1. Build binary:
   ```bash
   cd backend
   go build -o markdown-editor cmd/markdown-editor/main.go
   ```

2. Set environment variables

3. Run:
   ```bash
   ./markdown-editor serve
   ```

### Frontend

1. Build:
   ```bash
   cd frontend
   npm run build
   ```

2. Serve `dist/` with any static server

## Testing

See [TESTING.md](./TESTING.md) for comprehensive testing guide including:
- HTTP API tests
- WebSocket chat tests
- Real-time events tests
- Project integration tests
- Dangerous mode tests

## License

MIT
