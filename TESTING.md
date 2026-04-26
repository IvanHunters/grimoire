# End-to-End Testing Guide

Полное руководство по тестированию интеграции frontend-backend.

## Prerequisites

1. **MongoDB** - должна быть запущена
2. **Go 1.21+** - для backend
3. **Node.js 18+** - для frontend
4. **Claude CLI** - установлен в системе

## Quick Start

### 1. Start MongoDB

```bash
docker compose up -d
```

Проверка:
```bash
docker compose ps
# Должен показать mongodb как running
```

### 2. Start Backend Server

```bash
cd backend
go run cmd/markdown-editor/main.go serve
```

Ожидаемый вывод:
```
INFO HTTP server started port=8080
INFO WebSocket server started port=3000
INFO MongoDB connected
INFO Created indexes
```

Проверка:
```bash
curl http://localhost:8080/health
# Ожидается: {"status":"ok","mongodb":"connected"}
```

### 3. Start Frontend

```bash
cd frontend
npm install  # если ещё не делали
npm run dev
```

Ожидаемый вывод:
```
VITE v8.0.10  ready in XXX ms

➜  Local:   http://localhost:5173/
➜  Network: use --host to expose
```

Откройте http://localhost:5173 в браузере.

## Testing Checklist

### ✅ HTTP API Tests

#### Notes CRUD

1. **Create Note**
   - Нажмите "+ New Note" в header
   - Введите название и выберите папку
   - Нажмите "Create"
   - Ожидается: заметка появилась в sidebar

2. **Edit Note**
   - Выберите заметку в sidebar
   - Введите текст в editor
   - Ожидается: preview обновляется автоматически
   - Ожидается: после паузы заметка сохраняется (проверить в MongoDB)

3. **Delete Note**
   - ПКМ на заметке в sidebar → Delete
   - Подтвердите удаление
   - Ожидается: заметка исчезла из sidebar

#### Folders CRUD

1. **Create Folder**
   - ПКМ в пустом месте sidebar → New Folder
   - Введите название (можно с путём: `projects/backend`)
   - Ожидается: папка появилась в sidebar с древовидной структурой

2. **Delete Folder**
   - ПКМ на пустой папке → Delete Folder
   - Подтвердите удаление
   - Ожидается: папка исчезла

### ✅ WebSocket Chat Tests

#### Connection

1. **Open Chat**
   - Нажмите иконку чата в header
   - Ожидается: ChatPanel открылся справа
   - Ожидается: Connection status = "Ready" (зелёная точка)

2. **Session Management**
   - Нажмите иконку Settings в ChatPanel
   - Нажмите "Create Session"
   - Введите название (опционально)
   - Выберите "Dangerous Mode" (опционально)
   - Нажмите "Create Session"
   - Ожидается: новая сессия создана и активна

#### Messaging

1. **Send Simple Message**
   - Введите "hello" в input
   - Нажмите Send или Enter
   - Ожидается: сообщение отправлено
   - Ожидается: статус изменился на "Generating..." (синяя пульсирующая точка)
   - Ожидается: получен ответ от Claude
   - Ожидается: статус вернулся к "Ready"

2. **Send Message with Note Context**
   - Откройте любую заметку
   - Откройте чат
   - Ожидается: баннер "Context: <note title>" вверху чата
   - Введите "summarize this note"
   - Ожидается: Claude получает содержимое заметки через MCP

3. **Tool Use Visualization**
   - Отправьте "list all notes"
   - Ожидается: в ответе видно "🔧 list_notes" перед текстом ответа

4. **Stop Generation**
   - Отправьте длинный запрос (например, "write a long essay about Go programming")
   - Пока Claude генерирует, нажмите кнопку Stop (красная X)
   - Ожидается: генерация остановлена
   - Ожидается: статус вернулся к "Ready"

### ✅ Real-Time Events Tests

#### Note Events

1. **Create Note via Claude**
   - В чате: "create a note called 'Test Note' with content 'Hello from Claude'"
   - Ожидается: заметка появилась в sidebar БЕЗ перезагрузки страницы

2. **Update Note via Claude**
   - В чате: "update Test Note, add a new line 'Updated by Claude'"
   - Откройте Test Note
   - Ожидается: контент обновлён автоматически

3. **Delete Note via Claude**
   - В чате: "delete Test Note"
   - Ожидается: заметка исчезла из sidebar автоматически

#### Folder Events

1. **Create Folder via Claude**
   - В чате: "create a folder called 'test-folder'"
   - Ожидается: папка появилась в sidebar автоматически

2. **Delete Folder via Claude**
   - В чате: "delete folder test-folder"
   - Ожидается: папка исчезла автоматически

### ✅ Project Integration Tests

#### Autodiscovery

1. **Create Project Note**
   - Создайте заметку с названием соответствующим проекту в `~/git/`
   - Например: "markdown-editor"
   - Ожидается: backend автоматически определяет projectPath

2. **Project Mode Tools**
   - Откройте project note
   - В чате: "list files in this project"
   - Ожидается: Claude показывает файлы проекта
   - В чате: "show me the main.go file"
   - Ожидается: Claude читает файл из проекта

### ✅ Graph View Tests

1. **Create Wikilinks**
   - В заметке A добавьте `[[Note B]]`
   - В заметке B добавьте `[[Note A]]` и `[[Note C]]`
   - Сохраните заметки

2. **Check Graph**
   - Откройте Graph View
   - Ожидается: видны связи между A ↔ B ↔ C
   - Ожидается: Mermaid diagram отображается корректно

3. **Via Claude**
   - В чате: "show connections for Note A"
   - Ожидается: Claude возвращает outgoing и incoming links

### ✅ Dangerous Mode Tests

⚠️ **ОСТОРОЖНО**: эти тесты выполняют реальные операции!

1. **Create Session with Dangerous Mode**
   - Создайте новую сессию с включённым Dangerous Mode
   - Ожидается: предупреждение внизу чата

2. **Destructive Operations**
   - В dangerous mode сессии: "delete all notes in folder test"
   - Ожидается: Claude выполняет операцию
   - В обычной сессии: то же самое
   - Ожидается: Claude ОТКАЗЫВАЕТСЯ выполнять без dangerous mode

## Troubleshooting

### Backend не запускается

```bash
# Проверить MongoDB
docker compose ps

# Проверить порты
lsof -i :8080  # HTTP server
lsof -i :3000  # WebSocket server

# Проверить логи
# Backend должен выводить structured logs в stdout
```

### Frontend не подключается к WebSocket

```bash
# Проверить WebSocket endpoint
wscat -c ws://localhost:3000/claude-chat

# Отправить init сообщение
{"type":"init","sessionId":"test","dangerousMode":false}
```

### MongoDB ошибки

```bash
# Проверить что MongoDB запущена
docker compose logs mongodb

# Пересоздать если нужно
docker compose down
docker compose up -d
```

### Claude subprocess ошибки

```bash
# Проверить что claude CLI установлен
which claude

# Проверить версию
claude --version

# Проверить MCP конфигурацию
ls -la ~/GoProjects/markdown-editor/.claude/mcp_servers.json
```

## Performance Metrics

### Expected Response Times

- **HTTP API**: < 100ms (local)
- **WebSocket init**: < 200ms
- **Claude first response**: 1-3s (зависит от API)
- **Claude streaming**: 50-100 tokens/sec
- **Real-time event propagation**: < 50ms

### Resource Usage

- **Backend memory**: ~50MB idle, ~100MB active
- **Frontend bundle size**: ~2MB gzipped
- **MongoDB storage**: ~1KB per note

## Success Criteria

✅ Все HTTP endpoints работают
✅ WebSocket подключается и остаётся стабильным
✅ Claude отвечает на сообщения
✅ MCP tools доступны Claude
✅ Real-time события обновляют UI
✅ Autodiscovery находит проекты
✅ Dangerous mode работает корректно
✅ Session cleanup работает (idle sessions закрываются через 5 минут)
✅ Graceful shutdown работает (Ctrl+C корректно завершает все subprocess)

## Known Issues

1. **Claude subprocess PIN prompt**: Первый раз может запросить PIN для GPG signing - это нормально
2. **MongoDB indexes**: При первом запуске создаются индексы, может занять 1-2 секунды
3. **Frontend hot reload**: При изменении backend может потребоваться refresh страницы

## Next Steps

После успешного прохождения всех тестов:

1. Create Pull Request
2. Code review
3. Merge to main
4. Deploy backend to production
5. Deploy frontend to production
6. Setup monitoring and alerting
