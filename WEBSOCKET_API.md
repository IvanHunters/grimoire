# WebSocket API для Claude Chat

Backend должен предоставлять WebSocket endpoint для взаимодействия с Claude CLI подпроцессом.

## Endpoint

```
ws://localhost:3000/claude-chat
```

## Архитектура

```
Frontend (browser) <--WebSocket--> Backend (Go) <--stdin/stdout--> Claude CLI subprocess
```

Backend запускает `claude` CLI как подпроцесс и проксирует сообщения между frontend и Claude.

**КРИТИЧНО:** Взаимодействие с Claude CLI происходит ТОЛЬКО через stdin/stdout:
- Сообщения пользователя → записываются в stdin процесса
- Ответы Claude → читаются из stdout процесса
- Stop (ESC) → специальная команда в stdin (НЕ убийство процесса!)
- Процесс Claude остаётся живым всю сессию

**Каждая сессия запускается в своей директории:**
- Сессия привязана к текущей открытой заметке
- Если заметка имеет `type: project` и `project_path` → claude запускается в project_path
- Если `project_path` пустой → autodiscovery по названию заметки
- Если обычная заметка → claude запускается в директории notes (~/notes)

## Сообщения от Frontend → Backend

### 1. Инициализация сессии

```json
{
  "type": "init",
  "sessionId": "main",
  "dangerousMode": false,
  "currentNote": {
    "name": "getting-started.md",
    "content": "---\ntitle: Getting Started\ntype: project\nproject_path:\n---\n# Getting Started\n...",
    "type": "project",
    "projectPath": "",
    "title": "Getting Started"
  }
}
```

**Frontend парсит frontmatter и извлекает:**
- `type` - из frontmatter
- `projectPath` - из frontmatter (может быть пустым!)
- `title` - из frontmatter или первого H1

**Действие бэкенда:**
- Запустить `claude` подпроцесс с флагами:
  - `--dangerous-mode-i-know-what-i-am-doing` (если dangerousMode = true)
  - Рабочая директория: проверить frontmatter заметки на `project_path`, если есть - переключиться в эту директорию
- Сохранить mapping sessionId → subprocess

### 2. Отправка сообщения

```json
{
  "type": "message",
  "sessionId": "main",
  "content": "Прочитай заметку и предложи улучшения",
  "currentNote": {
    "name": "my-go-app.md",
    "content": "---\ntitle: My Go App\ntype: project\nproject_path:\n---\n...",
    "type": "project",
    "projectPath": "",
    "title": "My Go App"
  },
  "dangerousMode": false
}
```

**Примеры autodiscovery:**

1. **projectPath пустой, title="markdown-editor"**
   - Ищем: `~/git/github.com/ivanohotnikov/markdown-editor`
   - Если найдена → запускаем там

2. **projectPath пустой, title="My Go App"**
   - Нормализация: "my-go-app"
   - Ищем: `~/git/github.com/ivanohotnikov/my-go-app`
   - Или: любая папка содержащая "my-go-app"

3. **projectPath явно указан**
   - Используем как есть, autodiscovery не нужен

**Действие бэкенда:**
- Найти подпроцесс по sessionId
- **ВАЖНО:** Использовать ТОТ ЖЕ подпроцесс для всех сообщений в сессии
- Просто записать сообщение в stdin: `stdin.Write([]byte(content + "\n"))`
- Читать stdout и парсить ответы Claude в real-time
- НЕ перезапускать процесс при каждом сообщении!

**Смена директории:**
- Если `projectPath` изменился с предыдущего сообщения - нужно либо:
  - Создать НОВУЮ сессию в новой директории
  - Или использовать команду `cd` внутри Claude (если поддерживается)
- По умолчанию: сессия остаётся в той директории, где была запущена

### 3. Остановка генерации (ESC)

```json
{
  "type": "stop",
  "sessionId": "main"
}
```

**Действие бэкенда:**
- Найти подпроцесс по sessionId
- **ВАЖНО:** НЕ убивать процесс! Claude CLI поддерживает остановку через stdin
- Отправить ESC sequence в stdin: `\x1b` (или Ctrl+C через PTY)
- Claude прервёт текущую генерацию, но останется в интерактивном режиме
- Готов принимать следующие сообщения

**Почему НЕ SIGINT:**
- SIGINT убьёт процесс → потеря контекста сессии
- Нужен PTY (pseudo-terminal) для корректной работы
- Claude CLI остаётся в той же директории и с тем же контекстом

### 4. Переключение сессии

```json
{
  "type": "switch_session",
  "sessionId": "session_1735123456789"
}
```

**Действие бэкенда:**
- Найти сессию в map
- Если не существует - создать новую (см. init)
- **Отправить всю историю сообщений** этой сессии frontend:

```json
{
  "type": "session_history",
  "sessionId": "session_1735123456789",
  "dangerousMode": true,
  "messages": [
    {"role": "user", "content": "Прочитай заметку", "time": "..."},
    {"role": "assistant", "content": "Вот что я нашёл...", "time": "..."},
    {"role": "system", "content": "🔧 read_file(getting-started.md)", "time": "..."}
  ]
}
```

**Frontend действия:**
- Очистить текущий чат
- Отрендерить всю историю из `messages`
- Обновить dangerous mode checkbox
- Обновить currentSessionId

**Backend логика при switch_session:**

```go
func handleSwitchSession(sessionID string) {
    // 1. Проверить: процесс запущен?
    session := sessions[sessionID]

    if session != nil {
        // Процесс активен - отправить текущую историю
        ws.WriteJSON(SessionHistory{
            Type:         "session_history",
            SessionID:    sessionID,
            DangerousMode: session.DangerousMode,
            WorkingDir:   session.WorkingDir,
            Messages:     session.Messages,
        })
        return
    }

    // 2. Процесса нет - проверить MongoDB
    sessionInfo, err := findInMongoDB(sessionID)

    if err == nil {
        // Есть в базе - предложить восстановление
        ws.WriteJSON(SessionNotFound{
            Type:       "session_not_found",
            SessionID:  sessionID,
            InDatabase: true,
            SessionInfo: SessionInfo{
                SessionID:    sessionID,
                WorkingDir:   sessionInfo.WorkingDir,
                DangerousMode: sessionInfo.DangerousMode,
                MessageCount: len(sessionInfo.Messages),
                ClosedAt:     sessionInfo.ClosedAt,
            },
        })
        return
    }

    // 3. Нигде нет - сообщить что нужно создать новую
    ws.WriteJSON(SessionNotFound{
        Type:       "session_not_found",
        SessionID:  sessionID,
        InDatabase: false,
    })
}
```

**ВАЖНО:** История хранится на backend, не на frontend!
- Frontend только отображает
- При переключении сессии - backend отдаёт полную историю
- При перезагрузке страницы - история восстанавливается

### 5. Перезапуск сессии с новыми настройками

```json
{
  "type": "restart_session",
  "sessionId": "main",
  "dangerousMode": true
}
```

**Действие бэкенда:**
- **Сохранить историю сообщений** текущей сессии
- Выполнить graceful shutdown текущего подпроцесса
- Запустить новый подпроцесс с новыми флагами
- **Восстановить историю сообщений** (отправить frontend)
- Обновить `session.DangerousMode` в map

**Когда используется:**
- Пользователь изменил dangerous mode для уже запущенной сессии
- Frontend показывает confirm: "Перезапустить сессию?"

**ВАЖНО:** Dangerous mode можно изменить ТОЛЬКО при запуске!
- Нельзя изменить флаги уже запущенного процесса Claude
- Требуется полный перезапуск подпроцесса

### 6. Восстановление архивной сессии

```json
{
  "type": "restore_session",
  "sessionId": "session_1234567890"
}
```

**Действие бэкенда:**
1. Загрузить сессию из MongoDB
2. Создать новый процесс Claude в том же `workingDir`
3. **Replay контекста** - воспроизвести историю сообщений
4. Добавить восстановленную сессию в активные (map sessions)
5. Отправить `session_history` frontend

**ВАЖНО: Директория должна быть та же!**
- Если сессия была в `/tmp/claude-my-app` → запустить там
- Если директория не существует → создать или использовать fallback
- Сохранить тот же `dangerousMode`

**Пример:**
```go
func restoreSessionFromMongoDB(sessionID string) (*ClaudeSession, error) {
    // Загрузить из базы
    var dbSession DBSession
    err := db.Collection("claude_sessions").
        FindOne(ctx, bson.M{"session_id": sessionID}).
        Decode(&dbSession)

    if err != nil {
        return nil, err
    }

    // Создать новую активную сессию
    session := &ClaudeSession{
        ID:            sessionID,
        WorkingDir:    dbSession.WorkingDir,
        DangerousMode: dbSession.DangerousMode,
        Messages:      dbSession.Messages,
        LastActivity:  time.Now(),
    }

    // Запустить Claude с восстановлением контекста
    err = startClaudeWithHistory(session)
    if err != nil {
        return nil, err
    }

    // Добавить в активные сессии
    sessions[sessionID] = session

    return session, nil
}
```

### 7. Удаление архивной сессии

```json
{
  "type": "delete_archived_session",
  "sessionId": "session_1234567890"
}
```

**Действие бэкенда:**
- Удалить из MongoDB
- Отправить подтверждение frontend

```go
func deleteArchivedSession(sessionID string) error {
    _, err := db.Collection("claude_sessions").
        DeleteOne(ctx, bson.M{"session_id": sessionID})
    return err
}
```

### 8. Закрытие сессии

```json
{
  "type": "close_session",
  "sessionId": "session_1234567890"
}
```

**Действие бэкенда:**
- Найти подпроцесс по sessionId
- Выполнить graceful shutdown (см. секцию ниже)
- Удалить сессию из map
- **ВАЖНО:** Очистить временную директорию `/tmp/claude-*` если она была создана
- Отправить подтверждение frontend

**Graceful shutdown последовательность:**
1. Отправить Ctrl+D (EOF) в PTY: `ptmx.Write([]byte{0x04})`
2. Подождать 5 секунд
3. Если не завершился - SIGTERM: `cmd.Process.Signal(syscall.SIGTERM)`
4. Подождать 3 секунды
5. Если всё ещё жив - SIGKILL: `cmd.Process.Kill()`
6. Очистить zombie: `cmd.Wait()`
7. Удалить временную директорию (если начинается с `/tmp/claude-`)

**Сохранение истории перед закрытием (опционально):**

```go
func closeSession(sessionID string) {
    session := sessions[sessionID]
    if session == nil {
        return
    }

    // Опционально: Сохранить историю в MongoDB для долгосрочного хранения
    if shouldPersist(session) {
        saveSessionToMongoDB(session)
    }

    // Graceful shutdown
    shutdownSession(sessionID)

    // Cleanup временных файлов
    cleanupSessionDir(session.WorkingDir)

    delete(sessions, sessionID)
}

func saveSessionToMongoDB(session *ClaudeSession) error {
    // Сохранить в MongoDB коллекцию "claude_sessions"
    doc := bson.M{
        "session_id":    session.ID,
        "working_dir":   session.WorkingDir,
        "dangerous_mode": session.DangerousMode,
        "messages":      session.Messages,
        "created_at":    session.CreatedAt,
        "closed_at":     time.Now(),
    }

    _, err := db.Collection("claude_sessions").InsertOne(ctx, doc)
    return err
}

// Восстановление старой сессии из MongoDB
func restoreSessionFromMongoDB(sessionID string) (*ClaudeSession, error) {
    var doc struct {
        SessionID    string    `bson:"session_id"`
        WorkingDir   string    `bson:"working_dir"`
        DangerousMode bool     `bson:"dangerous_mode"`
        Messages     []Message `bson:"messages"`
    }

    err := db.Collection("claude_sessions").
        FindOne(ctx, bson.M{"session_id": sessionID}).
        Decode(&doc)

    if err != nil {
        return nil, err
    }

    // Создать новую сессию с восстановленной историей
    session := &ClaudeSession{
        ID:            sessionID,
        WorkingDir:    doc.WorkingDir,
        DangerousMode: doc.DangerousMode,
        Messages:      doc.Messages,
    }

    // Запустить Claude с восстановлением контекста
    startClaudeWithHistory(session)

    return session, nil
}
```

**Cleanup временных файлов:**
```go
func cleanupSessionDir(workingDir string) {
    // Удалять только наши временные директории
    if strings.HasPrefix(workingDir, "/tmp/claude-") {
        os.RemoveAll(workingDir)
        log.Printf("Cleaned up temporary directory: %s", workingDir)
    }
}
```

## Сообщения от Backend → Frontend

### 1. Tool Use (Claude вызывает инструмент)

```json
{
  "type": "tool_use",
  "tool_name": "read_file",
  "tool_args": "handlers.go"
}
```

**Frontend действие:** Показать индикатор использования инструмента

### 2. Начало сообщения

```json
{
  "type": "message_start"
}
```

**Frontend действие:**
- Показать статус "Generating..."
- Показать кнопку Stop
- Создать пустое сообщение для стриминга

### 3. Стриминг контента (delta)

```json
{
  "type": "content_delta",
  "content": "Вот что я нашёл в заметке..."
}
```

**Frontend действие:** Добавлять content к последнему сообщению assistant

### 4. Завершение сообщения

```json
{
  "type": "message_complete"
}
```

**Frontend действие:**
- Скрыть кнопку Stop
- Статус "Ready"
- Обработать очередь сообщений (если были отправлены новые пока генерировал)

### 5. Ошибка

```json
{
  "type": "error",
  "error": "Failed to read file: permission denied"
}
```

**Frontend действие:** Показать ошибку в чате

### 6. Остановлено пользователем

```json
{
  "type": "stopped"
}
```

**Frontend действие:** Показать "⏹️ Generation stopped by user"

### 7. Сессия не найдена (в памяти, но может быть в базе)

```json
{
  "type": "session_not_found",
  "sessionId": "session_123",
  "inDatabase": true,
  "sessionInfo": {
    "sessionId": "session_123",
    "workingDir": "/tmp/claude-my-app",
    "dangerousMode": true,
    "messageCount": 15,
    "closedAt": "2024-01-15T10:00:00Z"
  }
}
```

**Когда отправляется:**
- При `switch_session` если процесс не запущен
- Backend проверил MongoDB и нашёл архивную сессию

**Frontend действие:**
1. Показать диалог с информацией о сессии
2. Предложить выбор:
   - "Восстановить" → отправить `restore_session`
   - "Удалить" → отправить `delete_archived_session`
   - "Отмена" → вернуться к текущей сессии

**Если `inDatabase: false`:**
- Сессии нет нигде
- Frontend создаёт новую (`init`)

### 8. История сессии

```json
{
  "type": "session_history",
  "sessionId": "session_123",
  "dangerousMode": true,
  "workingDir": "/tmp/claude-my-app",
  "messages": [
    {
      "role": "user",
      "content": "Прочитай заметку",
      "time": "2024-01-15T10:30:00Z"
    },
    {
      "role": "system",
      "content": "🔧 read_current_note()",
      "time": "2024-01-15T10:30:01Z"
    },
    {
      "role": "assistant",
      "content": "Прочитал заметку. Вот краткое содержание...",
      "time": "2024-01-15T10:30:05Z"
    }
  ]
}
```

**Когда отправляется:**
- При `switch_session` - отправить историю новой сессии
- При `restart_session` - отправить восстановленную историю
- При переподключении WebSocket - отправить историю текущей сессии

**Frontend действие:**
1. Очистить UI чата
2. Отрендерить все сообщения из `messages` по порядку
3. Обновить `dangerous mode` checkbox
4. Установить `currentSessionId`

**Backend должен сохранять:**
- Все user сообщения
- Все assistant ответы
- Все system уведомления (tool use, errors)
- НЕ сохранять: content_delta (только финальный результат)

## Управление подпроцессами

### Singleton подпроцесс на сессию

- **Один `claude` подпроцесс на sessionId**
- **Процесс остаётся живым всю сессию** - все сообщения идут через stdin/stdout
- При закрытии WebSocket соединения - **НЕ** убивать подпроцесс сразу
- Таймаут: 5 минут без активности → graceful shutdown подпроцесса
- При переподключении WebSocket - переиспользовать существующий подпроцесс

### Восстановление контекста Claude при перезапуске

**Проблема:** Claude CLI не сохраняет историю между запусками процесса.

**Решение:** Replay сообщений - воспроизвести всю историю при запуске нового процесса.

**Когда нужно:**
- При `restart_session` (изменение dangerous mode)
- При переподключении после таймаута (процесс был убит, но история сохранена)
- При ошибке процесса (crash и перезапуск)

**Механизм восстановления:**

```go
func startClaudeWithHistory(session *ClaudeSession) error {
    // 1. Запустить новый процесс Claude
    cmd := exec.Command("claude", args...)
    ptmx, err := pty.Start(cmd)

    // 2. Подождать готовности (Claude показывает приветствие)
    time.Sleep(1 * time.Second)

    // 3. Replay истории - отправить все предыдущие сообщения
    if len(session.Messages) > 0 {
        // Добавить system prompt с контекстом
        contextPrompt := buildContextPrompt(session.Messages)
        ptmx.Write([]byte(contextPrompt + "\n"))

        // Подождать ответа Claude
        time.Sleep(2 * time.Second)
    }

    return nil
}

func buildContextPrompt(messages []Message) string {
    var context strings.Builder

    context.WriteString("Восстанавливаю контекст предыдущего разговора:\n\n")

    // Добавить последние N сообщений (не все, чтобы не переполнить context)
    maxMessages := 10
    start := len(messages) - maxMessages
    if start < 0 {
        start = 0
    }

    for i := start; i < len(messages); i++ {
        msg := messages[i]
        switch msg.Role {
        case "user":
            context.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
        case "assistant":
            context.WriteString(fmt.Sprintf("Assistant: %s\n", msg.Content))
        }
    }

    context.WriteString("\nПродолжаем с того же контекста.")
    return context.String()
}
```

**Альтернатива: Claude CLI session files**

Claude CLI может поддерживать сохранение сессий:

```bash
# Запуск с session file
claude --session /tmp/claude-sessions/session-123.json

# Claude CLI автоматически:
# - Сохраняет историю в файл
# - Восстанавливает при следующем запуске
```

**ВАЖНО:** Проверить документацию `claude --help` на наличие флага `--session`

### Автоматическое закрытие неактивных сессий

Backend должен отслеживать активность и автоматически закрывать неактивные сессии:

```go
type ClaudeSession struct {
    ID            string
    Cmd           *exec.Cmd
    PTY           *os.File
    DangerousMode bool
    WorkingDir    string
    LastActivity  time.Time
    Messages      []Message  // История сообщений сессии (хранится на backend!)
}

type Message struct {
    Role    string    // "user", "assistant", "system"
    Content string    // Текст сообщения
    ToolUse string    // Если был tool use
    Time    time.Time // Время сообщения
}

// Background goroutine для мониторинга сессий
func monitorInactiveSessions() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        now := time.Now()
        for sessionID, session := range sessions {
            // Проверка timeout (5 минут)
            if now.Sub(session.LastActivity) > 5*time.Minute {
                log.Printf("Session %s inactive for 5 minutes, closing", sessionID)
                closeSession(sessionID)
            }
        }
    }
}

func closeSession(sessionID string) {
    session := sessions[sessionID]
    if session == nil {
        return
    }

    // Graceful shutdown
    shutdownSession(sessionID)

    // Cleanup временных файлов
    cleanupSessionDir(session.WorkingDir)

    // Удалить из map
    delete(sessions, sessionID)
}
```

**Обновление LastActivity:**
- При получении `message` от frontend
- При отправке `content_delta` от Claude
- НЕ обновлять при `stop` (это не активность, а прерывание)

### PTY (Pseudo-Terminal) - ОБЯЗАТЕЛЬНО!

**Claude CLI требует PTY для корректной работы:**

```go
import (
    "github.com/creack/pty"
    "os/exec"
)

// Запуск с PTY
cmd := exec.Command("claude")
cmd.Dir = projectPath // Директория проекта

ptmx, err := pty.Start(cmd)
if err != nil {
    return err
}

// Читать/писать через ptmx (это и stdin, и stdout)
go io.Copy(os.Stdout, ptmx)  // stdout
ptmx.Write([]byte("message\n"))  // stdin
```

**Почему PTY:**
- Позволяет отправлять Ctrl+C (ESC) без убийства процесса
- Claude CLI корректно обрабатывает интерактивный ввод
- Поддержка цветного вывода и форматирования

### Отправка Stop (ESC) через PTY

```go
// ПРАВИЛЬНО: Отправить Ctrl+C через PTY
ptmx.Write([]byte{0x03})  // ASCII ETX (Ctrl+C)

// Claude прервёт генерацию, но останется живым:
// > Interrupted by user
// > (ready for next message)
```

### Отправка новых сообщений

```go
// Просто пишем в ptmx
message := "Прочитай заметку\n"
ptmx.Write([]byte(message))

// Claude обработает и ответит через тот же ptmx (stdout)
```

### Graceful shutdown

При завершении приложения или удалении сессии:

```bash
# 1. Отправить Ctrl+D (EOF) в stdin
ptmx.Write([]byte{0x04})  # ASCII EOT (Ctrl+D)

# 2. Подождать 5 секунд
time.Sleep(5 * time.Second)

# 3. Если не завершился - SIGTERM
cmd.Process.Signal(syscall.SIGTERM)

# 4. Ещё 3 секунды - SIGKILL
time.Sleep(3 * time.Second)
cmd.Process.Kill()
```

### Zombie процессы

**КРИТИЧНО:** Всегда вызывать `cmd.Wait()` после завершения подпроцесса для очистки zombie.

```go
go func() {
    cmd.Wait() // Очистка zombie процесса
    log.Printf("Claude subprocess exited for session: %s", sessionId)
    delete(sessions, sessionId) // Удалить из map
}()
```

### Чтение stdout в real-time

```go
scanner := bufio.NewScanner(ptmx)
for scanner.Scan() {
    line := scanner.Text()

    // Отправить в WebSocket
    ws.WriteJSON(Message{
        Type: "content_delta",
        Content: line + "\n",
    })
}
```

## Сессии в своих директориях

**ВАЖНО:** Каждая сессия Claude запускается в директории текущей открытой заметки!

### Логика определения директории

При получении `init` или `message`:

1. **Парсить frontmatter заметки** (`currentNote.content`)
2. **Извлечь `type` и `project_path`**

**Сценарий 1: Заметка с явным project_path**

```yaml
---
type: project
project_path: ~/git/github.com/ivanohotnikov/my-go-app
---
```

→ Запустить `claude` в директории `~/git/github.com/ivanohotnikov/my-go-app`

**Сценарий 2: Заметка с пустым project_path (autodiscovery)**

```yaml
---
title: markdown-editor
type: project
project_path:
---
# Markdown Editor

Описание проекта...
```

**Frontend отправляет:**
```json
{
  "currentNote": {
    "type": "project",
    "projectPath": "",
    "title": "markdown-editor"
  }
}
```

**Backend автопоиск:**
1. `projectPath` пустой → включается autodiscovery
2. Нормализация title: "markdown-editor" → "markdown-editor"
3. Поиск в `~/git/github.com/ivanohotnikov/markdown-editor`
4. Если найдена → запустить там
5. Если не найдена → fallback в `~/notes`

**Примеры autodiscovery:**
- title: "My Go App" → нормализация: "my-go-app" → `~/git/.../my-go-app`
- title: "Web Application" → нормализация: "web-application" → поиск папок содержащих "web-application"
- title: "API" → нормализация: "api" → поиск папок содержащих "api" (осторожно, много совпадений!)

**Сценарий 3: Обычная заметка (не проект)**

```yaml
---
title: My Ideas
---
```

→ Запустить `claude` в `~/notes` (или другой дефолтной директории заметок)

### Fallback Chain (что если директории не существует?)

Backend проверяет существование директорий и использует fallback:

**1. Явный project_path:**
```go
path := expandPath(note.ProjectPath)
if exists(path) {
    return path  // ✅ Используем
}
// ⚠️ Путь не существует - логировать warning, идти дальше
```

**2. Autodiscovery:**
```go
discovered := findProjectByTitle(note.Title)
if discovered != "" {
    return discovered  // ✅ Нашли проект
}
// Не нашли - идём к fallback
```

**3. Fallback: ~/notes**
```go
notesDir := "$HOME/notes"
if exists(notesDir) {
    return notesDir  // ✅ Директория существует
}

// Попытаться создать
if mkdir(notesDir) == nil {
    log.Printf("Created notes directory: %s", notesDir)
    return notesDir  // ✅ Создали
}
// Не удалось создать - идём к /tmp
```

**4. Fallback: /tmp/claude-<normalized-title>**
```go
// Нормализация title для безопасного имени директории
normalized := strings.ToLower(note.Title)
normalized = strings.ReplaceAll(normalized, " ", "-")
normalized = regexp.ReplaceAll(`[^a-z0-9\-_]`, "", normalized)

tmpDir := "/tmp/claude-" + normalized
// Если title нет - использовать sessionID
if note.Title == "" {
    tmpDir = "/tmp/claude-session-" + sessionID
}

if mkdir(tmpDir) == nil {
    log.Printf("Warning: Using temporary directory: %s", tmpDir)
    return tmpDir  // ✅ Создали в /tmp
}
```

**5. Последний fallback: /tmp**
```go
// Всё провалилось - используем /tmp напрямую
log.Printf("Error: Cannot create tmp directory, using /tmp")
return "/tmp"  // Всегда существует, но без изоляции
```

**Примеры:**

- `project_path: /nonexistent/path` → Warning → Autodiscovery → ~/notes → /tmp/claude-my-app
- `project_path: ""`, title="My App" не найден → ~/notes → /tmp/claude-my-app
- `~/notes` не существует и не создаётся → /tmp/claude-my-app ✅
- title="" (нет заголовка) → /tmp/claude-session-main ✅
- Обычная заметка, `~/notes` есть → ~/notes ✅

**Почему /tmp вместо $HOME:**
- ✅ Изоляция: не замусоривает $HOME
- ✅ Автоочистка: /tmp очищается системой при перезагрузке
- ✅ Уникальность: каждая заметка в своей директории
- ✅ Безопасность: временные файлы не остаются навсегда

### Смена директории при смене заметки

**Проблема:** Пользователь переключился на другую заметку с другим `project_path`

**Решение 1 (простое):** Создать НОВУЮ сессию
```javascript
// Frontend: При смене заметки создаём новую сессию
if (currentNote.projectPath !== previousNote.projectPath) {
    createNewSession(); // Новая сессия в новой директории
}
```

**Решение 2 (сложное):** Использовать команду `cd` внутри Claude
- НЕ рекомендуется, так как Claude может не поддерживать смену cwd
- Лучше всегда создавать новую сессию для нового проекта

### Пример mapping сессий

```
Session ID          | Directory                                    | Note
--------------------|----------------------------------------------|------------------
main                | ~/notes                                      | getting-started.md
session_1234        | ~/git/github.com/user/my-go-app             | my-go-app.md
session_5678        | ~/git/github.com/user/web-app               | web-app.md
```

Каждая сессия независима и работает в своей директории!

## Очередь сообщений (Message Queue)

Frontend реализует очередь:
- Если Claude генерирует - новые сообщения добавляются в queue
- После `message_complete` - отправляется следующее из очереди
- Backend НЕ нужно реализовывать очередь - один запрос обрабатывается до конца

## Пример Go кода с PTY

```go
import (
    "bufio"
    "os"
    "os/exec"
    "time"

    "github.com/creack/pty"
    "github.com/gorilla/websocket"
)

type ClaudeSession struct {
    ID            string
    Cmd           *exec.Cmd
    PTY           *os.File           // PTY для stdin/stdout
    DangerousMode bool
    WorkingDir    string             // Директория запуска
    LastActivity  time.Time
}

var sessions = make(map[string]*ClaudeSession)

func handleWebSocket(ws *websocket.Conn) {
    for {
        var msg Message
        ws.ReadJSON(&msg)

        switch msg.Type {
        case "init":
            // Определить директорию из currentNote
            workingDir := determineWorkingDir(msg.CurrentNote, msg.SessionID)

            session := startClaudeSubprocess(
                msg.SessionID,
                msg.DangerousMode,
                workingDir,
            )
            sessions[msg.SessionID] = session
            go streamClaudeOutput(session, ws)

        case "message":
            session := sessions[msg.SessionID]
            // Писать в PTY (это и stdin, и stdout)
            session.PTY.Write([]byte(msg.Content + "\n"))
            session.LastActivity = time.Now()

        case "stop":
            session := sessions[msg.SessionID]
            // Отправить Ctrl+C через PTY (НЕ убивать процесс!)
            session.PTY.Write([]byte{0x03})  // ASCII ETX (Ctrl+C)
        }
    }
}

func determineWorkingDir(note CurrentNote, sessionID string) string {
    if note.Type == "project" {
        if note.ProjectPath != "" {
            // Явный путь из frontmatter
            path := expandPath(note.ProjectPath)
            if _, err := os.Stat(path); err == nil {
                return path
            }
            // Путь не существует - логировать предупреждение
            log.Printf("Warning: project_path does not exist: %s", path)
        } else if note.Title != "" {
            // Autodiscovery: projectPath пустой, ищем по title
            discovered := findProjectByTitle(note.Title)
            if discovered != "" {
                return discovered
            }
        }
    }

    // Fallback 1: ~/notes
    notesDir := os.ExpandEnv("$HOME/notes")
    if _, err := os.Stat(notesDir); err == nil {
        return notesDir
    }

    // Попытка создать ~/notes
    if err := os.MkdirAll(notesDir, 0755); err == nil {
        log.Printf("Created notes directory: %s", notesDir)
        return notesDir
    }

    // Fallback 2: /tmp/<normalized-title>
    var tmpDir string
    if note.Title != "" {
        // Нормализовать title для имени директории
        normalized := strings.ToLower(note.Title)
        normalized = strings.ReplaceAll(normalized, " ", "-")
        // Удалить небезопасные символы
        normalized = regexp.MustCompile(`[^a-z0-9\-_]`).ReplaceAllString(normalized, "")
        tmpDir = filepath.Join("/tmp", "claude-"+normalized)
    } else {
        // Если title нет - использовать sessionID
        tmpDir = filepath.Join("/tmp", "claude-session-"+sessionID)
    }

    // Создать /tmp директорию
    if err := os.MkdirAll(tmpDir, 0755); err == nil {
        log.Printf("Warning: Using temporary directory: %s", tmpDir)
        return tmpDir
    }

    // Последний fallback: /tmp (всегда существует)
    log.Printf("Error: Cannot create tmp directory, using /tmp")
    return "/tmp"
}

// Autodiscovery проектов по title заметки
func findProjectByTitle(title string) string {
    // Базовая директория проектов
    username := os.Getenv("USER")
    baseDir := filepath.Join(os.Getenv("HOME"), "git", "github.com", username)

    // Нормализовать title для поиска
    normalized := strings.ToLower(title)
    normalized = strings.ReplaceAll(normalized, " ", "-")

    // Попытка 1: Точное совпадение
    exactPath := filepath.Join(baseDir, normalized)
    if _, err := os.Stat(exactPath); err == nil {
        return exactPath
    }

    // Попытка 2: Поиск по паттерну (содержит title)
    entries, err := os.ReadDir(baseDir)
    if err != nil {
        return ""
    }

    for _, entry := range entries {
        if entry.IsDir() {
            dirName := strings.ToLower(entry.Name())
            // Если название папки содержит normalized title
            if strings.Contains(dirName, normalized) {
                return filepath.Join(baseDir, entry.Name())
            }
        }
    }

    // Не найдено
    return ""
}

// Expand ~ в пути
func expandPath(path string) string {
    if strings.HasPrefix(path, "~/") {
        return filepath.Join(os.Getenv("HOME"), path[2:])
    }
    return path
}

func startClaudeSubprocess(sessionID string, dangerousMode bool, workingDir string) *ClaudeSession {
    // Подготовить команду
    args := []string{}
    if dangerousMode {
        args = append(args, "--dangerous-mode-i-know-what-i-am-doing")
    }

    cmd := exec.Command("claude", args...)
    cmd.Dir = workingDir  // ВАЖНО: Установить рабочую директорию!

    // Запустить с PTY
    ptmx, err := pty.Start(cmd)
    if err != nil {
        log.Printf("Failed to start Claude: %v", err)
        return nil
    }

    session := &ClaudeSession{
        ID:            sessionID,
        Cmd:           cmd,
        PTY:           ptmx,
        DangerousMode: dangerousMode,
        WorkingDir:    workingDir,
        LastActivity:  time.Now(),
    }

    // Мониторинг завершения процесса
    go func() {
        cmd.Wait() // КРИТИЧНО: очистка zombie
        log.Printf("Claude subprocess exited for session: %s", sessionID)
        ptmx.Close()
        delete(sessions, sessionID)
    }()

    return session
}

func streamClaudeOutput(session *ClaudeSession, ws *websocket.Conn) {
    scanner := bufio.NewScanner(session.PTY)

    ws.WriteJSON(Message{Type: "message_start"})

    for scanner.Scan() {
        line := scanner.Text()

        // Парсить вывод Claude
        if strings.Contains(line, "tool_use") {
            // Извлечь имя инструмента
            toolName := extractToolName(line)
            ws.WriteJSON(Message{
                Type:     "tool_use",
                ToolName: toolName,
            })
        } else if strings.Contains(line, "Interrupted") {
            // Claude прервал генерацию
            ws.WriteJSON(Message{Type: "stopped"})
        } else {
            // Обычный вывод - streaming content
            ws.WriteJSON(Message{
                Type:    "content_delta",
                Content: line + "\n",
            })
        }
    }

    ws.WriteJSON(Message{Type: "message_complete"})
}

// Graceful shutdown сессии
func shutdownSession(sessionID string) {
    session := sessions[sessionID]
    if session == nil {
        return
    }

    // 1. Ctrl+D (EOF)
    session.PTY.Write([]byte{0x04})

    // 2. Подождать 5 секунд
    done := make(chan bool)
    go func() {
        session.Cmd.Wait()
        done <- true
    }()

    select {
    case <-done:
        log.Printf("Session %s exited gracefully", sessionID)
    case <-time.After(5 * time.Second):
        // 3. SIGTERM
        session.Cmd.Process.Signal(syscall.SIGTERM)

        select {
        case <-done:
            log.Printf("Session %s terminated", sessionID)
        case <-time.After(3 * time.Second):
            // 4. SIGKILL
            session.Cmd.Process.Kill()
            log.Printf("Session %s killed", sessionID)
        }
    }

    session.PTY.Close()
    delete(sessions, sessionID)
}
```

**Зависимости:**

```bash
go get github.com/creack/pty
go get github.com/gorilla/websocket
```

## MCP Tools для работы с заметками

Backend должен предоставить Claude MCP tools через stdio:

### Доступные инструменты

1. **read_current_note()** - прочитать текущую открытую заметку
2. **update_current_note(content)** - обновить текущую заметку
3. **search_notes(query)** - поиск по всем заметкам
4. **create_note(path, content)** - создать новую заметку
5. **list_notes()** - список всех заметок
6. **get_note_connections(note_name)** - найти связи заметки

### WebSocket обновления заметок

Когда Claude вызывает `update_current_note()`:

1. Backend обновляет файл заметки
2. Backend отправляет WebSocket сообщение:

```json
{
  "type": "note_updated",
  "noteName": "getting-started.md",
  "content": "новое содержимое..."
}
```

3. Frontend обновляет редактор в real-time

## Безопасность

- **Dangerous mode:** Разрешает деструктивные операции (rm, git push --force, etc.)
- **Без dangerous mode:** Claude может только читать, создавать файлы, безопасные git операции
- Пользователь видит явный индикатор dangerous mode (⚠️)

## Reconnect логика

Frontend автоматически переподключается при обрыве:
- Попытки: максимум 5
- Интервал: 2, 4, 6, 8, 10 секунд (exponential backoff)
- Backend сохраняет подпроцессы живыми при разрыве соединения
