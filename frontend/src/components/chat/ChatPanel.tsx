import { useState, useRef, useEffect } from 'react'
import { X, Send, User, Bot, AlertCircle, Plus, Trash2, Settings, FileText } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'

interface ChatPanelProps {
  visible: boolean
  onClose: () => void
  noteId?: string | null // Note ID for chat context
}

interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
  toolUse?: ToolUse
}

interface ToolUse {
  name: string
  args: Record<string, any>
  status: 'running' | 'completed' | 'error'
  result?: string
}

interface ChatSession {
  id: string
  name: string
  messages: Message[]
  createdAt: Date
  dangerousMode: boolean
}

/**
 * ChatPanel - AI assistant chat interface
 *
 * Features:
 * - Multiple sessions support
 * - Dangerous mode toggle
 * - Tool use visualization
 * - Session management (create, switch, delete)
 * - Note context support
 */
function ChatPanel({ visible, onClose, noteId }: ChatPanelProps) {
  const { notes } = useNotes()

  // Sessions state
  const [sessions, setSessions] = useState<ChatSession[]>([
    {
      id: 'main',
      name: 'Main Session',
      messages: [
        {
          id: '0',
          role: 'system',
          content: '🔓 Claude имеет полный доступ ко ВСЕМ заметкам через MCP tools (не только к текущей)',
          timestamp: new Date(),
        },
        {
          id: '1',
          role: 'assistant',
          content: `👋 Привет! Я Claude, твой AI ассистент для работы с заметками.

У меня есть доступ ко ВСЕМ твоим заметкам! Я могу:
• 📖 Читать и анализировать любую заметку
• 🔍 Искать по содержимому всех заметок
• 📝 Создавать новые заметки и папки
• 🔗 Находить связи между заметками (упоминания, теги, темы)
• 🗂️ Организовывать и структурировать весь контент
• 🌐 Создавать wikilinks между связанными заметками
• 📊 Анализировать всю базу знаний и предлагать улучшения

Примеры запросов (заметки):
• "Найди все заметки про Go и суммируй их"
• "Создай папку 'Архив' и предложи что туда переместить"
• "Какие заметки связаны с текущей?"
• "Проанализируй всю базу и предложи структуру"

💻 Работа с проектами кода:
Если открыта заметка с type: project, я переключаюсь в директорию проекта и могу:
• 📂 "Изучи структуру проекта"
• 🔍 "Найди все TODO в коде"
• ✍️ "Напиши тесты для handlers.go"
• 🔄 "Сделай git pull"
• 🔎 "Найди функции, работающие с MongoDB"

📝 Работа с заметками:
Я НЕ читаю заметки автоматически (экономия токенов!)
Явно попроси когда нужно:
• 📖 "Прочитай заметку" → read_current_note()
• ✏️ "Добавь секцию X в заметку" → update_current_note()
• ✅ "Посмотри TODO и сделай первую задачу"
• 🔍 "Найди заметки про Go" → search_notes()`,
          timestamp: new Date(),
        },
      ],
      createdAt: new Date(),
      dangerousMode: false,
    },
  ])
  const [currentSessionId, setCurrentSessionId] = useState('main')
  const [input, setInput] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const [showSessionManager, setShowSessionManager] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Get current session
  const currentSession = sessions.find(s => s.id === currentSessionId) || sessions[0]
  const messages = currentSession?.messages || []

  // Get current note (if noteId provided)
  const currentNote = noteId ? notes.find(n => n.id === noteId) : null

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Focus input when panel opens
  useEffect(() => {
    if (visible && inputRef.current) {
      inputRef.current.focus()
    }
  }, [visible])

  const handleSendMessage = async () => {
    const text = input.trim()
    if (!text || isGenerating) return

    // Add user message to current session
    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      content: text,
      timestamp: new Date(),
    }

    setSessions(prev =>
      prev.map(s =>
        s.id === currentSessionId
          ? { ...s, messages: [...s.messages, userMessage] }
          : s
      )
    )
    setInput('')
    setIsGenerating(true)

    // Mock AI response with tool use visualization
    setTimeout(() => {
      // Simulate tool use
      const toolMessage: Message = {
        id: (Date.now() + 1).toString(),
        role: 'assistant',
        content: '',
        timestamp: new Date(),
        toolUse: {
          name: 'search_notes',
          args: { query: text },
          status: 'running',
        },
      }

      setSessions(prev =>
        prev.map(s =>
          s.id === currentSessionId
            ? { ...s, messages: [...s.messages, toolMessage] }
            : s
        )
      )

      // Simulate tool completion
      setTimeout(() => {
        setSessions(prev =>
          prev.map(s => {
            if (s.id !== currentSessionId) return s
            const updatedMessages = s.messages.map(m =>
              m.id === toolMessage.id && m.toolUse
                ? {
                    ...m,
                    toolUse: {
                      ...m.toolUse,
                      status: 'completed' as const,
                      result: 'Found 3 notes matching your query',
                    },
                  }
                : m
            )
            return { ...s, messages: updatedMessages }
          })
        )

        // Add AI response
        setTimeout(() => {
          const aiMessage: Message = {
            id: (Date.now() + 2).toString(),
            role: 'assistant',
            content: `Нашёл 3 заметки, связанные с "${text}":\n\n1. Getting Started\n2. TODO List\n3. Wikilinks Guide\n\nВ будущем здесь будет реальный Claude API через WebSocket.`,
            timestamp: new Date(),
          }

          setSessions(prev =>
            prev.map(s =>
              s.id === currentSessionId
                ? { ...s, messages: [...s.messages, aiMessage] }
                : s
            )
          )
          setIsGenerating(false)
        }, 500)
      }, 800)
    }, 300)
  }

  const handleCreateSession = () => {
    const newSession: ChatSession = {
      id: `session_${Date.now()}`,
      name: `Session ${sessions.length + 1}`,
      messages: [
        {
          id: `${Date.now()}_system`,
          role: 'system',
          content: '🔓 Claude имеет полный доступ ко ВСЕМ заметкам через MCP tools',
          timestamp: new Date(),
        },
        {
          id: `${Date.now()}_welcome`,
          role: 'assistant',
          content: '👋 Новая сессия создана. Я готов помочь с заметками и проектами! Чем могу помочь?',
          timestamp: new Date(),
        },
      ],
      createdAt: new Date(),
      dangerousMode: false,
    }

    setSessions(prev => [...prev, newSession])
    setCurrentSessionId(newSession.id)
    setShowSessionManager(false)
  }

  const handleDeleteSession = (sessionId: string) => {
    if (sessionId === 'main') return // Can't delete main session
    if (sessions.length <= 1) return // Need at least one session

    setSessions(prev => prev.filter(s => s.id !== sessionId))

    if (currentSessionId === sessionId) {
      setCurrentSessionId('main')
    }
  }

  const handleToggleDangerousMode = () => {
    setSessions(prev =>
      prev.map(s =>
        s.id === currentSessionId
          ? { ...s, dangerousMode: !s.dangerousMode }
          : s
      )
    )
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSendMessage()
    }
  }

  return (
    <div
      className={`fixed right-0 top-0 h-screen w-[600px] bg-white border-l border-gray-200 flex flex-col shadow-2xl transition-transform duration-300 ease-in-out z-[1000] ${
        visible ? 'translate-x-0' : 'translate-x-full'
      }`}
    >
      {/* Header */}
      <div className="p-4 border-b border-gray-200 bg-gradient-to-r from-purple-600 to-purple-700 text-white flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Bot className="w-6 h-6" />
          <div>
            <h2 className="text-lg font-semibold">Chat with Claude</h2>
            {currentNote ? (
              <p className="text-sm text-purple-100 flex items-center gap-1">
                <FileText className="w-3 h-3" />
                Context: {currentNote.title}
              </p>
            ) : (
              <p className="text-sm text-purple-100">AI-powered assistant</p>
            )}
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-2 hover:bg-purple-800 hover:bg-opacity-50 rounded-lg transition"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Toolbar */}
      <div className="px-4 py-3 bg-gray-50 border-b border-gray-200 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <select
            value={currentSessionId}
            onChange={(e) => setCurrentSessionId(e.target.value)}
            className="px-3 py-1.5 text-sm border border-gray-300 rounded-lg bg-white"
          >
            {sessions.map(session => (
              <option key={session.id} value={session.id}>
                {session.name}
              </option>
            ))}
          </select>
          <button
            onClick={handleCreateSession}
            className="p-1.5 text-sm text-gray-600 hover:bg-gray-200 rounded"
            title="New Session"
          >
            <Plus className="w-4 h-4" />
          </button>
          <button
            onClick={() => setShowSessionManager(!showSessionManager)}
            className="p-1.5 text-sm text-gray-600 hover:bg-gray-200 rounded"
            title="Manage Sessions"
          >
            <Settings className="w-4 h-4" />
          </button>
        </div>

        <div className="flex items-center gap-2">
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              className="rounded"
              checked={currentSession?.dangerousMode}
              onChange={handleToggleDangerousMode}
            />
            <span className={currentSession?.dangerousMode ? 'text-orange-600 font-semibold' : 'text-gray-600'}>
              ⚠️ Dangerous
            </span>
          </label>
          <button
            className="px-3 py-1.5 text-sm bg-red-500 text-white rounded hover:bg-red-600 disabled:opacity-50 transition"
            disabled={!isGenerating}
          >
            Stop
          </button>
        </div>
      </div>

      {/* Session Manager */}
      {showSessionManager && (
        <div className="px-4 py-2 bg-yellow-50 border-b border-yellow-100">
          <div className="text-sm font-semibold mb-2">Sessions:</div>
          <div className="space-y-1">
            {sessions.map(session => (
              <div
                key={session.id}
                className="flex items-center justify-between p-2 bg-white rounded border border-gray-200"
              >
                <div>
                  <div className="text-sm font-medium">{session.name}</div>
                  <div className="text-xs text-gray-500">
                    {session.messages.length} messages
                    {session.dangerousMode && ' • ⚠️ Dangerous'}
                  </div>
                </div>
                {session.id !== 'main' && (
                  <button
                    onClick={() => handleDeleteSession(session.id)}
                    className="p-1 text-red-500 hover:bg-red-50 rounded"
                    title="Delete Session"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map(message => (
          <div key={message.id}>
            {/* Regular message */}
            {!message.toolUse && (
              <div className={`flex gap-3 ${message.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                {message.role === 'assistant' && (
                  <div className="flex-shrink-0">
                    <div className="w-8 h-8 rounded-full bg-purple-600 flex items-center justify-center">
                      <Bot className="w-5 h-5 text-white" />
                    </div>
                  </div>
                )}

                <div
                  className={`max-w-[80%] rounded-lg p-3 ${
                    message.role === 'user'
                      ? 'bg-blue-600 text-white'
                      : message.role === 'system'
                      ? 'bg-yellow-50 border border-yellow-200 text-yellow-900 text-sm'
                      : 'bg-gray-100 text-gray-900'
                  }`}
                >
                  <div className={`text-sm whitespace-pre-wrap ${message.role === 'system' ? 'font-medium' : ''}`}>
                    {message.content}
                  </div>
                  {message.role !== 'system' && (
                    <div
                      className={`text-xs mt-1 ${
                        message.role === 'user' ? 'text-blue-100' : 'text-gray-500'
                      }`}
                    >
                      {message.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                    </div>
                  )}
                </div>

                {message.role === 'user' && (
                  <div className="flex-shrink-0">
                    <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center">
                      <User className="w-5 h-5 text-white" />
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* Tool use visualization */}
            {message.toolUse && (
              <div className="flex gap-3 justify-start">
                <div className="flex-shrink-0">
                  <div className="w-8 h-8 rounded-full bg-purple-600 flex items-center justify-center">
                    <Bot className="w-5 h-5 text-white" />
                  </div>
                </div>

                <div className="bg-purple-50 border border-purple-200 rounded-lg p-3 max-w-[80%]">
                  <div className="flex items-center gap-2 mb-2">
                    <i className="fas fa-tools text-purple-600"></i>
                    <span className="text-sm font-semibold text-purple-900">
                      Using tool: {message.toolUse.name}
                    </span>
                    {message.toolUse.status === 'running' && (
                      <div className="flex gap-1">
                        <div className="w-1.5 h-1.5 bg-purple-600 rounded-full animate-bounce" style={{ animationDelay: '0ms' }}></div>
                        <div className="w-1.5 h-1.5 bg-purple-600 rounded-full animate-bounce" style={{ animationDelay: '150ms' }}></div>
                        <div className="w-1.5 h-1.5 bg-purple-600 rounded-full animate-bounce" style={{ animationDelay: '300ms' }}></div>
                      </div>
                    )}
                    {message.toolUse.status === 'completed' && (
                      <span className="text-xs text-green-600">✓ Completed</span>
                    )}
                  </div>

                  <div className="text-xs bg-purple-100 rounded p-2 mb-2">
                    <div className="font-mono text-purple-900">
                      {JSON.stringify(message.toolUse.args, null, 2)}
                    </div>
                  </div>

                  {message.toolUse.result && (
                    <div className="text-sm text-purple-900">
                      <strong>Result:</strong> {message.toolUse.result}
                    </div>
                  )}

                  <div className="text-xs text-purple-500 mt-1">
                    {message.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                  </div>
                </div>
              </div>
            )}
          </div>
        ))}

        {isGenerating && (
          <div className="flex gap-3 justify-start">
            <div className="flex-shrink-0">
              <div className="w-8 h-8 rounded-full bg-purple-600 flex items-center justify-center">
                <Bot className="w-5 h-5 text-white" />
              </div>
            </div>
            <div className="bg-gray-100 rounded-lg p-3">
              <div className="flex items-center gap-2 text-sm text-gray-600">
                <div className="flex gap-1">
                  <div className="w-2 h-2 bg-purple-600 rounded-full animate-bounce" style={{ animationDelay: '0ms' }}></div>
                  <div className="w-2 h-2 bg-purple-600 rounded-full animate-bounce" style={{ animationDelay: '150ms' }}></div>
                  <div className="w-2 h-2 bg-purple-600 rounded-full animate-bounce" style={{ animationDelay: '300ms' }}></div>
                </div>
                <span>Claude is thinking...</span>
              </div>
            </div>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Info banner */}
      <div className="px-4 py-2 bg-blue-50 border-t border-blue-100 flex items-start gap-2 text-sm">
        <AlertCircle className="w-4 h-4 text-blue-600 mt-0.5 flex-shrink-0" />
        <div className="text-blue-900">
          <p>
            <strong>Demo mode:</strong> Responses are mocked. Real Claude integration will be added via WebSocket.
          </p>
          {currentNote && (
            <p className="mt-1 text-xs">
              Claude has access to note: <strong>{currentNote.title}</strong>
            </p>
          )}
        </div>
      </div>

      {/* Input */}
      <div className="p-4 border-t border-gray-200 bg-white">
        <div className="flex items-center gap-2">
          <input
            ref={inputRef}
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Спроси Claude что-нибудь..."
            className="flex-1 px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
            disabled={isGenerating}
          />
          <button
            onClick={handleSendMessage}
            disabled={!input.trim() || isGenerating}
            className="p-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            <Send className="w-5 h-5" />
          </button>
        </div>
      </div>
    </div>
  )
}

export default ChatPanel
