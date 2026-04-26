# Markdown Editor - Прогресс реализации Frontend

## ✅ Выполнено (Days 1-10)

### Day 1-5: Foundation & Layout ✅

**Tooling:**
- ✅ Vite + React + TypeScript проект
- ✅ Tailwind CSS 3.x (настроен и работает)
- ✅ Dependencies установлены:
  - Markdown: `react-markdown`, `remark-gfm`, `rehype-mermaid`, `rehype-prism-plus`
  - Syntax highlighting: `prismjs` (200+ языков, okaidia theme)
  - Utils: `axios`, `react-dropzone`, `react-hook-form`, `lucide-react`
  - ⚠️ **CodeMirror 6 удален** - используется plain textarea как в prototype

**Конфигурация:**
- ✅ `tailwind.config.ts` - кастомные цвета и ширины
- ✅ `postcss.config.js` - для Tailwind 3.x
- ✅ `vite.config.ts` - proxy для `/api`, `/uploads`, `/claude-chat` (WebSocket)
- ✅ `index.css` - **полные стили из design-prototype.html** (все классы)

**TypeScript типы:**
- ✅ `types/note.ts` - Note, Frontmatter, CRUD requests
- ✅ `types/folder.ts` - Folder, FolderTree
- ✅ `types/ui.ts` - ViewMode, UIState, ModalState
- ✅ `types/search.ts` - SearchOptions, SearchResult
- ✅ `types/claude.ts` - Message, ClaudeSession, WSMessage
- ✅ `types/graph.ts` - GraphNode, GraphEdge, GraphData

**API Client:**
- ✅ `api/client.ts` - Axios instance с interceptors
- ✅ `api/notes.ts` - CRUD endpoints для notes
- ✅ `api/folders.ts` - endpoints для folders

**State Management:**
- ✅ `contexts/NotesContext.tsx` - notes state с **mock данными** (3 заметки для dev)
- ✅ NotesProvider в App.tsx

**Layout компоненты:**
- ✅ `App.tsx` - root с NotesProvider
- ✅ `pages/HomePage.tsx` - main page с Editor/Preview/Sidebar
- ✅ `components/common/WelcomeScreen.tsx` - gradient welcome (стили из prototype)
- ✅ `components/common/ViewModeToggle.tsx` - Editor/Split/Preview toggle
- ✅ `components/layout/Header.tsx` - top bar (New Note, Search, Export)
- ✅ `components/layout/Sidebar.tsx` - collapsible folders, notes list

### Day 6-7: Editor & Preview Core ✅

**Editor:**
- ✅ `components/editor/EditorTextarea.tsx` - **Plain textarea** (не CodeMirror)
- ✅ Undo/Redo с cursor position tracking
- ✅ Auto-completion на Enter (tables, lists, tasks)
- ✅ Highlight overlay для search (positioned behind textarea)
- ✅ Auto-save on word boundaries
- ✅ forwardRef для sync scroll
- ⚠️ Поиск/замена будет добавлен позже (Days 13-14)

**Preview:**
- ✅ `components/preview/Preview.tsx` - React Markdown wrapper
- ✅ GitHub Flavored Markdown (remark-gfm) - tables, strikethrough
- ✅ Links открываются в new tab
- ✅ Code blocks с темной темой
- ✅ Inline code стили
- ✅ forwardRef для sync scroll

**View Mode:**
- ✅ Toggle между Editor Only / Split / Preview Only
- ✅ Работает корректно

### Day 8: Auto-Completion ✅

**Файл:** `components/editor/EditorTextarea.tsx` (встроено в компонент)

**Функционал:**
- ✅ Enter продолжает таблицы: `| col1 | col2 |` → новая строка с тем же кол-вом колонок
- ✅ Enter продолжает unordered lists: `- item` → `- `
- ✅ Enter продолжает ordered lists: `1. first` → `2. ` (инкремент)
- ✅ Enter продолжает task lists: `- [ ] task` → `- [ ] `
- ✅ Smart exit: пустой item (только маркер) → выход из списка
- ✅ Логика из design-prototype.html (lines 1796-1967)
- ✅ Regex patterns: table rows, unordered/ordered lists, task lists

### Day 9: Resize Handle ✅

**Файл:** `components/common/ResizeHandle.tsx`

**Функционал:**
- ✅ Drag handle между Editor и Preview
- ✅ requestAnimationFrame для smooth performance
- ✅ 20-80% width constraints для обеих панелей
- ✅ Fixed widths (не flex percentages)
- ✅ Hover highlight (w-1 → w-2)
- ✅ Cursor изменяется на col-resize
- ✅ Интегрировано в HomePage

### Day 10: Synchronized Scroll ✅

**Файл:** `hooks/useSyncScroll.ts`

**Функционал:**
- ✅ Editor textarea scroll → Preview scroll (синхронно по %)
- ✅ Preview scroll → Editor textarea scroll (синхронно по %)
- ✅ Debounce 100ms (предотвращает infinite loop)
- ✅ Flags для предотвращения циклов (isScrollingEditor/isScrollingPreview)
- ✅ Работает только в split view
- ✅ Находит textarea внутри editor wrapper div
- ✅ Логика из prototype (lines 2184-2203)

### Day 11-12: Wikilinks ✅

**Файлы:**
- `utils/wikilinks.ts` - parser для wikilinks
- `components/preview/WikilinkRenderer.tsx` - компонент для рендеринга wikilink
- `components/preview/WikilinkPopup.tsx` - hover popup с preview
- `components/preview/Preview.tsx` - интеграция wikilinks

**Функционал:**
- ✅ Парсинг `[[link]]` и `[[link|alias]]` синтаксиса
- ✅ Custom renderer для react-markdown (через wikilink: protocol)
- ✅ Hover popup с preview заметки (400px width, auto-position)
- ✅ Click navigation к связанной заметке
- ✅ Broken links визуально отличаются (серый цвет)
- ✅ CSS стили из prototype (wikilink, wikilink-broken, wikilink-popup)
- ✅ Mock данные с примерами wikilinks (4 заметки)
- ✅ Автоматическое позиционирование popup (если не влезает справа → слева)

### 🔄 Architectural Change: Plain Textarea (вместо CodeMirror) ✅

**Причина изменения:**
- Пользователь указал: "логика и стили редактора отличаются от прототипа который я утвердил"
- Решение: заменить CodeMirror на plain textarea как в `design-prototype.html`
- Результат: **bundle size уменьшился на 60%** (1017KB → 408KB)

**Изменения:**

**СОЗДАНО:**
- ✅ `components/editor/EditorTextarea.tsx` - новый редактор на plain textarea
  - textarea + highlight overlay (positioned behind)
  - Undo/Redo с cursor position tracking (limit 100 states)
  - Auto-completion (Enter key) - tables, lists, tasks
  - Auto-save on word boundaries (space, punctuation, newline)
  - Synchronized scroll с highlight overlay
  - Keyboard shortcuts (Cmd+Z, Cmd+Shift+Z, Cmd+Y)

**УДАЛЕНО:**
- ❌ `components/editor/Editor.tsx` - CodeMirror wrapper
- ❌ `hooks/useAutoComplete.ts` - CodeMirror-specific
- ❌ 27 CodeMirror packages из package.json:
  - @codemirror/lang-markdown
  - @codemirror/search
  - @codemirror/state
  - @codemirror/view
  - @uiw/react-codemirror
  - и др.

**ОБНОВЛЕНО:**
- ✅ `index.css` - **полностью переписан** с exact стилями из prototype:
  - `.editor-pane` - Monaco/Menlo/Courier font, 14px, line-height 1.5
  - `.editor-highlight` - overlay с transparent color, pointer-events: none
  - `.markdown-preview` - полные стили для всех markdown элементов
  - `.search-panel` - ACE-style floating panel
  - `.resize-handle` - col-resize cursor
  - `.wikilink-*` - все wikilink стили
  - `.welcome-screen` - gradient background
- ✅ `components/preview/Preview.tsx`:
  - Убраны Tailwind классы
  - Используются только классы из index.css
  - Добавлен rehype-prism-plus для syntax highlighting
  - Copy button для code blocks
  - Стили Prism.js okaidia theme в index.css
- ✅ `hooks/useSyncScroll.ts` - работает с textarea вместо `.cm-scroller`
- ✅ `pages/HomePage.tsx` - импорт EditorTextarea вместо Editor

**Новые пакеты:**
- ✅ `prismjs` - для syntax highlighting (200+ языков)
- ✅ `rehype-prism-plus` - Prism integration для react-markdown

**Логика:**
- Преобразование `[[target]]` → `[target](wikilink:target)` перед парсингом markdown
- Custom link renderer проверяет href и если начинается с `wikilink:` → рендерит WikilinkRenderer
- WikilinkRenderer ищет заметку по title/path и показывает popup при hover
- WikilinkPopup отображает первые 500 символов контента заметки

---

## 📋 Осталось сделать (Days 13-30)

### Days 13-15: Advanced Preview Features

**Day 13: Mermaid Diagrams** ⚠️ NEXT
- [ ] rehype-mermaid уже установлен, нужно протестировать
- [ ] `components/modals/MermaidEditorModal.tsx` - diagram editor с live preview
- [ ] Templates: flowchart, sequence, gantt, pie chart
- [ ] Insert diagram в editor

**Day 13: Mermaid Diagrams** ⚠️
- [ ] rehype-mermaid уже установлен, нужно протестировать
- [ ] `components/modals/MermaidEditorModal.tsx` - diagram editor с live preview
- [ ] Templates: flowchart, sequence, gantt, pie chart
- [ ] Insert diagram в editor

**Day 14: Syntax Highlighting** ⚠️
- [ ] rehype-prism-plus уже установлен, нужно протестировать
- [ ] `components/preview/CodeBlock.tsx` - code block с copy button
- [ ] Language labels (::before в CSS)
- [ ] Copy button (opacity 0 → 1 on hover)
- [ ] Проверить okaidia theme

**Day 15: Testing** ⚠️
- [ ] Протестировать все preview фичи
- [ ] Bug fixes

---

### Days 16-20: Modals & Context Menus

**Day 16-17: Modals Base** ⚠️
- [ ] `components/modals/Modal.tsx` - base modal компонент
- [ ] Slide-in анимация (уже в CSS)
- [ ] Overlay с backdrop-filter: blur(2px)
- [ ] ESC закрывает modal

**Day 18: CRUD Modals** ⚠️
- [ ] `components/modals/NewNoteModal.tsx` - create note с:
  - Folder selector
  - Create new folder опция
  - Project auto-discovery (API `/api/notes/project-suggestions`)
- [ ] `components/modals/SearchModal.tsx` - global search с:
  - Search input
  - Results list с highlight
  - Click → open note
- [ ] `components/modals/UploadModal.tsx` - file upload с:
  - Image preview
  - Filename editor
  - Enter → upload, ESC → cancel

**Day 19: Additional Modals** ⚠️
- [ ] `components/modals/DeleteConfirmModal.tsx` - delete confirmation
- [ ] `components/modals/AskClaudeModal.tsx` - AI text assistant (right-click selected text)

**Day 20: Context Menus** ⚠️
- [ ] `components/common/ContextMenu.tsx` - base context menu
- [ ] Right-click на folders: Rename, Delete, Move
- [ ] Right-click на notes: Rename, Delete, Move, Duplicate
- [ ] Right-click на editor (selected text): Ask Claude
- [ ] Submenu для Claude sessions

---

### Days 21-25: Claude WebSocket Integration ⭐ KILLER FEATURE

**Day 21: WebSocket Connection** ⚠️
- [ ] `hooks/useClaudeWebSocket.ts` - WebSocket connection logic
- [ ] Connection to `ws://localhost:3000/claude-chat`
- [ ] Exponential backoff reconnection: 2s, 4s, 6s, 8s, 10s (max 5 attempts)
- [ ] Connection status: Ready/Connecting/Generating/Error/Disconnected
- [ ] Message queue (отправка пока генерируется)

**Day 22: WebSocket Protocol** ⚠️
- [ ] Frontend → Backend messages:
  - `init` (sessionId, dangerousMode, currentNote)
  - `message` (content, sessionId)
  - `stop` (sessionId)
  - `switch_session` (sessionId)
- [ ] Backend → Frontend messages:
  - `message_start`
  - `content_delta` (streaming)
  - `tool_use` (tool_name, tool_args)
  - `message_complete`
  - `error`
  - `stopped`

**Day 23: Chat Panel UI** ⚠️
- [ ] `components/claude/ClaudeChatPanel.tsx` - 600px right panel
- [ ] Slide-in animation (right: -600px → 0)
- [ ] Purple gradient header (как в CSS)
- [ ] Messages container с scroll
- [ ] Markdown rendering в assistant messages (react-markdown)
- [ ] Toggle open/close

**Day 24: Chat Toolbar** ⚠️
- [ ] `components/claude/ClaudeToolbar.tsx` - toolbar с:
  - Session select dropdown
  - New session button
  - Close session button (X, красный)
  - Dangerous mode toggle (⚠️ checkbox)
  - Stop button (красный, ESC shortcut)
  - Status indicator (dot + text)
- [ ] Session management state

**Day 25: Messages & Tool Use** ⚠️
- [ ] `components/claude/ClaudeMessages.tsx` - message list
- [ ] `components/claude/ClaudeInput.tsx` - input field
- [ ] Message types:
  - `.claude-message.user` - серый фон, справа
  - `.claude-message.assistant` - фиолетовый фон, слева
  - `.claude-message.system` - желтый фон, центр, italic
  - `.claude-tool-use` - синий фон, monospace
- [ ] Message queue UI ("⏳ Message queued (2)")

---

### Days 26-30: Graph View & Polish

**Day 26-27: Graph View** ⚠️
- [ ] `components/graph/GraphView.tsx` - overlay
- [ ] `components/graph/GraphCanvas.tsx` - SVG rendering
- [ ] `components/graph/GraphNode.tsx` - circle node
- [ ] `components/graph/GraphEdge.tsx` - line
- [ ] API `GET /api/graph` → nodes + links
- [ ] Circular layout (simple MVP algorithm)
- [ ] Click node → navigate to note
- [ ] Current note highlighted (blue)
- [ ] Legend panel

**Day 28: Image Paste** ⚠️
- [ ] `hooks/useClipboard.ts` - clipboard paste detection
- [ ] Image paste → show UploadModal
- [ ] Preview image
- [ ] Edit filename
- [ ] Insert `![name](url)` в editor

**Day 29: Keyboard Shortcuts** ⚠️
- [ ] `hooks/useKeyboardShortcuts.ts` - global shortcuts
- [ ] Cmd+F → open search panel
- [ ] Cmd+H → open search + replace panel
- [ ] Cmd+B → bold
- [ ] Cmd+I → italic
- [ ] Cmd+K → link
- [ ] ESC → close modals/panels
- [ ] F3, Shift+F3 → next/prev search match

**Day 30: Polish & Bug Fixes** ⚠️
- [ ] Animations tuning
- [ ] Loading states everywhere
- [ ] Error handling улучшение
- [ ] Performance optimization (large files)
- [ ] Edge cases testing
- [ ] Final testing all features

---

## 🎯 Приоритеты для продолжения

**Рекомендуемый порядок:**

1. **Days 11-12: Wikilinks** - важная фича для knowledge graph
2. **Days 21-25: Claude WebSocket** - killer feature, главное отличие от других редакторов
3. **Days 16-20: Modals** - функциональность Create Note, Search, Upload
4. **Days 26-27: Graph View** - визуализация связей
5. **Days 13-15: Mermaid + Syntax** - улучшение preview
6. **Days 28-30: Final polish** - доработка и тестирование

**Альтернативный порядок (если хочется быстрее получить full-featured app):**

1. Claude WebSocket (Days 21-25) - сразу killer feature
2. Modals (Days 16-20) - базовый CRUD UI
3. Wikilinks (Days 11-12) - knowledge management
4. Polish (Days 28-30) - финализация

---

## 📝 Важные заметки

### Mock данные

В `NotesContext.tsx` используются mock данные для development:
- 3 заметки: "Getting Started", "TODO List", "Personal Notes"
- 3 папки: "projects", "personal", "archive"

Fallback срабатывает когда API недоступен (502 ECONNREFUSED).

### Backend

Backend пока **не реализован**. Спецификация есть в `CLAUDE.md`:
- Go + Chi router
- MongoDB для хранения
- Claude CLI subprocess для AI
- WebSocket для chat
- MCP server для tools

### CSS

Все стили из `design-prototype.html` полностью портированы в `index.css`:
- Welcome screen (gradient)
- Sidebar (folders, notes)
- Editor (CodeMirror overrides)
- Preview (markdown, tables, code)
- Search panel (ACE-style)
- Claude chat panel (600px, purple header)
- Modals (slide-in animation)
- Context menus
- И всё остальное

### Dev сервер

```bash
cd frontend
npm run dev
# http://localhost:5173
```

API ошибки 502 - это нормально (backend не запущен), приложение работает с mock данными.

---

## 🔧 Технические детали

### CodeMirror Extensions

Используются:
1. `markdown()` - syntax highlighting
2. `search({ top: true })` - ACE-style search panel
3. `EditorView.lineWrapping` - wrap long lines
4. `autoCompletion()` - custom extension для tables/lists

### React Context API

State management через 5 контекстов (план):
1. `NotesContext` - ✅ готов (notes, folders, CRUD)
2. `EditorContext` - ⚠️ TODO (content, cursor, viewMode)
3. `SearchContext` - ⚠️ TODO (query, matches, options)
4. `ClaudeContext` - ⚠️ TODO (sessions, ws, messages)
5. `UIContext` - ⚠️ TODO (sidebar, chatPanel states)

Пока только NotesContext реализован, остальные по мере надобности.

### Refs для Scroll Sync

HomePage передаёт refs в Editor и Preview через forwardRef:
- `editorRef` → Editor → `.cm-scroller`
- `previewRef` → Preview → root div
- `useSyncScroll` подписывается на scroll events и синхронизирует

---

## 📦 Созданные файлы (текущий список)

```
frontend/
├── tailwind.config.ts
├── postcss.config.js
├── vite.config.ts
├── src/
│   ├── index.css (полные стили из prototype)
│   ├── App.tsx
│   ├── types/
│   │   ├── note.ts
│   │   ├── folder.ts
│   │   ├── ui.ts
│   │   ├── search.ts
│   │   ├── claude.ts
│   │   └── graph.ts
│   ├── api/
│   │   ├── client.ts
│   │   ├── notes.ts
│   │   └── folders.ts
│   ├── contexts/
│   │   └── NotesContext.tsx
│   ├── hooks/
│   │   └── useSyncScroll.ts
│   ├── utils/
│   │   └── wikilinks.ts
│   ├── components/
│   │   ├── common/
│   │   │   ├── WelcomeScreen.tsx
│   │   │   ├── ViewModeToggle.tsx
│   │   │   └── ResizeHandle.tsx
│   │   ├── layout/
│   │   │   ├── Header.tsx
│   │   │   └── Sidebar.tsx
│   │   ├── editor/
│   │   │   └── EditorTextarea.tsx  ← НОВЫЙ (plain textarea, не CodeMirror)
│   │   └── preview/
│   │       ├── Preview.tsx
│   │       ├── WikilinkRenderer.tsx
│   │       └── WikilinkPopup.tsx
│   └── pages/
│       └── HomePage.tsx
```

**Итого:** ~22 файла (удален useAutoComplete.ts, создан EditorTextarea.tsx)

---

## 📊 Текущее состояние (после перехода на textarea)

**Что работает:**
- ✅ Plain textarea редактор с полной функциональностью из prototype
- ✅ Undo/Redo с cursor position tracking (100 states limit)
- ✅ Auto-completion (Enter key) - tables, lists, tasks
- ✅ Highlight overlay для search (пока не используется)
- ✅ Preview с react-markdown + remark-gfm
- ✅ Syntax highlighting (Prism.js, okaidia theme)
- ✅ Wikilinks с hover popup
- ✅ Synchronized scroll (Editor ↔ Preview)
- ✅ Resize handle (20-80% constraints)
- ✅ View mode toggle (Editor/Split/Preview)
- ✅ Welcome screen
- ✅ Bundle size: **408KB** (было 1017KB с CodeMirror)

**Что нужно портировать из prototype:**
- ❌ Search/Replace panel (floating, ACE-style)
  - Cmd+F / Cmd+H shortcuts
  - Regex/Case/Word toggles
  - Match counter и navigation
  - Highlight в overlay
- ❌ Toolbar с markdown formatting buttons
  - Bold, Italic, Heading, Link, Image, Code
  - Table picker (10x10 grid)
  - Mermaid editor modal
- ❌ Image paste from clipboard
- ❌ Context menu (right-click на folders/notes)
- ❌ Graph view visualization

**Следующие приоритеты:**
1. **Search/Replace panel** (Days 13-14) - критичная функция из prototype
2. **Toolbar** (Day 15) - markdown formatting кнопки
3. **Mermaid diagrams** (Day 13) - rehype-mermaid уже установлен
4. **Claude WebSocket** (Days 21-25) - killer feature для AI integration

---

## 🚀 Продолжение работы

Для продолжения:
1. Прочитать этот файл (PROGRESS.md)
2. Выбрать следующий Day из плана
3. Реализовать по порядку
4. Обновить PROGRESS.md (переместить ✅)

**Следующий шаг:** Search/Replace Panel (критичная функция, уже есть highlight overlay)
