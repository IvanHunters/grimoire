import { useState, useRef, useEffect } from 'react'
import { X, Trash2, FileText, Folder, Plus, Calendar, Send, Edit2, Check, Terminal, Link2, BookOpen, ChevronRight, Pencil, User, ChevronDown, RefreshCw, Play } from 'lucide-react'
import type { Task, TaskStatus, TaskPriority, KanbanColumn } from '../../types/task'
import { TASK_STATUSES, PRIORITY_CONFIG } from '../../types/task'
import { tasksAPI } from '../../api/tasks'
import { useNotes } from '../../contexts/NotesContext'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// ── Schedule section ──────────────────────────────────────────────────────────

const INTERVAL_PRESETS = [
  { label: '15 min', minutes: 15 },
  { label: '1 hour', minutes: 60 },
  { label: '6 hours', minutes: 360 },
  { label: '1 day',  minutes: 1440 },
  { label: '1 week', minutes: 10080 },
]

const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

function buildCron(time: string, dayMode: 'daily' | 'weekdays' | 'custom', customDays: number[]): string {
  const [hh, mm] = time.split(':')
  const h = parseInt(hh ?? '0'), m = parseInt(mm ?? '0')
  if (dayMode === 'daily') return `${m} ${h} * * *`
  if (dayMode === 'weekdays') return `${m} ${h} * * 1-5`
  const days = [...customDays].sort().join(',')
  return `${m} ${h} * * ${days || '*'}`
}

function parseCronTime(expr: string): { time: string; dayMode: 'daily' | 'weekdays' | 'custom'; customDays: number[] } {
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return { time: '09:00', dayMode: 'daily', customDays: [] }
  const [min, hour, , , dow] = parts
  const h = parseInt(hour), m = parseInt(min)
  const time = `${String(isNaN(h) ? 9 : h).padStart(2, '0')}:${String(isNaN(m) ? 0 : m).padStart(2, '0')}`
  if (dow === '*' || dow === '*/1') return { time, dayMode: 'daily', customDays: [] }
  if (dow === '1-5') return { time, dayMode: 'weekdays', customDays: [] }
  const days = dow.split(',').map(Number).filter(n => !isNaN(n))
  return { time, dayMode: 'custom', customDays: days }
}

function formatNextRun(iso?: string) {
  if (!iso) return null
  const d = new Date(iso), now = new Date()
  const diff = d.getTime() - now.getTime()
  if (diff < 0) return 'overdue'
  const mins = Math.round(diff / 60000)
  if (mins < 60) return `in ${mins}m`
  if (mins < 1440) return `in ${Math.round(mins / 60)}h`
  return `in ${Math.round(mins / 1440)}d`
}

function formatLastRun(iso?: string) {
  if (!iso) return null
  return new Date(iso).toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })
}

interface ScheduleSectionProps {
  task: Task
  update: (data: Parameters<typeof tasksAPI.update>[1]) => Promise<void>
  runningNow: boolean
  setRunningNow: (v: boolean) => void
}

function ScheduleSection({ task, update, runningNow, setRunningNow }: ScheduleSectionProps) {
  const rec = task.recurring
  const enabled = rec?.enabled ?? false

  // mode: 'interval' | 'cron'
  const [mode, setMode] = useState<'interval' | 'cron'>(rec?.cronExpr ? 'cron' : 'interval')
  const [customMinutes, setCustomMinutes] = useState('')

  // cron builder state — initialised from existing cronExpr
  const parsed = rec?.cronExpr ? parseCronTime(rec.cronExpr) : { time: '09:00', dayMode: 'daily' as const, customDays: [] as number[] }
  const [cronTime, setCronTime] = useState(parsed.time)
  const [dayMode, setDayMode] = useState<'daily' | 'weekdays' | 'custom'>(parsed.dayMode)
  const [customDays, setCustomDays] = useState<number[]>(parsed.customDays)

  // sync cron fields when task prop changes (e.g. after save)
  useEffect(() => {
    if (rec?.cronExpr) {
      const p = parseCronTime(rec.cronExpr)
      setCronTime(p.time); setDayMode(p.dayMode); setCustomDays(p.customDays)
      setMode('cron')
    }
  }, [rec?.cronExpr])

  const applyCron = () => {
    const expr = buildCron(cronTime, dayMode, customDays)
    update({ setCronExpr: expr } as any)
  }

  const setIntervalPreset = (minutes: number) => {
    update({ recurring: { enabled: true, intervalMinutes: minutes } })
  }

  const toggleEnabled = () => {
    if (enabled) {
      update({ recurring: { enabled: false, intervalMinutes: rec?.intervalMinutes ?? 1440, cronExpr: rec?.cronExpr } as any })
    } else if (mode === 'cron' && rec?.cronExpr) {
      update({ setCronExpr: rec.cronExpr } as any)
    } else {
      update({ recurring: { enabled: true, intervalMinutes: rec?.intervalMinutes ?? 1440 } })
    }
  }

  const handleRunNow = async () => {
    setRunningNow(true)
    try { await tasksAPI.runNow(task.id) } catch {}
    setTimeout(() => setRunningNow(false), 2000)
  }

  const handleCancel = async () => {
    try { await tasksAPI.cancel(task.id) } catch {}
    setRunningNow(false)
  }

  const toggleDay = (d: number) => {
    setCustomDays(prev => prev.includes(d) ? prev.filter(x => x !== d) : [...prev, d])
  }

  const currentInterval = rec?.cronExpr ? 0 : (rec?.intervalMinutes ?? 0)
  const currentPreset = INTERVAL_PRESETS.find(p => p.minutes === currentInterval)

  // UI chip style helpers
  const chip = (active: boolean) => ({
    background: active ? 'rgba(6,182,212,0.12)' : 'rgba(255,255,255,0.03)',
    border: `1px solid ${active ? 'rgba(6,182,212,0.3)' : 'rgba(255,255,255,0.07)'}`,
    color: active ? '#06b6d4' : '#475569',
  })

  return (
    <div className="px-4 py-4">
      {/* Header row */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>Schedule</span>
          {enabled && (
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded"
              style={{ background: 'rgba(6,182,212,0.1)', color: '#06b6d4', border: '1px solid rgba(6,182,212,0.2)' }}>
              active
            </span>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {runningNow ? (
            <button onClick={handleCancel}
              className="flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-mono transition-colors"
              style={{ background: 'rgba(239,68,68,0.08)', color: 'rgba(239,68,68,0.7)', border: '1px solid rgba(239,68,68,0.2)' }}
              onMouseEnter={e => (e.currentTarget.style.background = 'rgba(239,68,68,0.18)')}
              onMouseLeave={e => (e.currentTarget.style.background = 'rgba(239,68,68,0.08)')}>
              <RefreshCw className="w-2.5 h-2.5 animate-spin" />
              Kill
            </button>
          ) : (
            <button onClick={handleRunNow}
              className="flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-mono transition-colors"
              style={{ background: 'rgba(6,182,212,0.08)', color: 'rgba(6,182,212,0.7)', border: '1px solid rgba(6,182,212,0.15)' }}
              onMouseEnter={e => (e.currentTarget.style.background = 'rgba(6,182,212,0.15)')}
              onMouseLeave={e => (e.currentTarget.style.background = 'rgba(6,182,212,0.08)')}>
              <Play className="w-2.5 h-2.5" />
              Run now
            </button>
          )}
          <button onClick={toggleEnabled}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-mono font-medium transition-all"
            style={{
              background: enabled ? 'rgba(6,182,212,0.1)' : 'rgba(255,255,255,0.04)',
              border: `1px solid ${enabled ? 'rgba(6,182,212,0.25)' : 'rgba(255,255,255,0.08)'}`,
              color: enabled ? '#06b6d4' : '#475569',
            }}
            onMouseEnter={e => (e.currentTarget.style.filter = 'brightness(1.2)')}
            onMouseLeave={e => (e.currentTarget.style.filter = '')}>
            <RefreshCw className={`w-3 h-3 ${enabled ? 'animate-spin' : ''}`} style={{ animationDuration: '3s' }} />
            {enabled ? 'Enabled' : 'Disabled'}
          </button>
        </div>
      </div>

      {/* Mode tabs */}
      <div className="flex gap-1 mb-3">
        {(['interval', 'cron'] as const).map(m => (
          <button key={m} onClick={() => setMode(m)}
            className="px-2.5 py-0.5 rounded text-[10px] font-mono transition-colors"
            style={chip(mode === m)}>
            {m === 'interval' ? 'Every N' : 'At time'}
          </button>
        ))}
      </div>

      {mode === 'interval' ? (
        <>
          <div className="flex flex-wrap gap-1.5 mb-2">
            {INTERVAL_PRESETS.map(p => (
              <button key={p.minutes} onClick={() => setIntervalPreset(p.minutes)}
                className="px-2 py-0.5 rounded text-[10px] font-mono transition-colors"
                style={chip(currentInterval === p.minutes && !rec?.cronExpr)}
                onMouseEnter={e => { if (currentInterval !== p.minutes) (e.currentTarget as HTMLElement).style.borderColor = 'rgba(6,182,212,0.2)' }}
                onMouseLeave={e => { if (currentInterval !== p.minutes) (e.currentTarget as HTMLElement).style.borderColor = 'rgba(255,255,255,0.07)' }}>
                {p.label}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono" style={{ color: '#475569' }}>every</span>
            <input type="number" min="1" value={customMinutes}
              onChange={e => setCustomMinutes(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && customMinutes) { setIntervalPreset(parseInt(customMinutes)); setCustomMinutes('') } }}
              placeholder="N"
              className="w-14 rounded px-2 py-0.5 text-[11px] font-mono outline-none text-center"
              style={{ background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#cbd5e1' }} />
            <span className="text-[10px] font-mono" style={{ color: '#475569' }}>min</span>
            {customMinutes && (
              <button onClick={() => { setIntervalPreset(parseInt(customMinutes)); setCustomMinutes('') }}
                className="text-[10px] font-mono px-2 py-0.5 rounded"
                style={{ background: 'rgba(6,182,212,0.1)', color: '#06b6d4', border: '1px solid rgba(6,182,212,0.2)' }}>
                Set
              </button>
            )}
          </div>
        </>
      ) : (
        /* ── Cron / at-time builder ── */
        <div className="space-y-2">
          {/* Time picker */}
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono w-8" style={{ color: '#475569' }}>at</span>
            <input type="time" value={cronTime} onChange={e => setCronTime(e.target.value)}
              className="rounded px-2 py-0.5 text-[11px] font-mono outline-none"
              style={{ background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#cbd5e1', colorScheme: 'dark' }} />
          </div>
          {/* Day mode */}
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono w-8" style={{ color: '#475569' }}>on</span>
            <div className="flex gap-1">
              {(['daily', 'weekdays', 'custom'] as const).map(dm => (
                <button key={dm} onClick={() => setDayMode(dm)}
                  className="px-2 py-0.5 rounded text-[10px] font-mono transition-colors"
                  style={chip(dayMode === dm)}>
                  {dm === 'daily' ? 'Every day' : dm === 'weekdays' ? 'Mon–Fri' : 'Custom'}
                </button>
              ))}
            </div>
          </div>
          {/* Custom day checkboxes */}
          {dayMode === 'custom' && (
            <div className="flex items-center gap-1 pl-10">
              {DAY_LABELS.map((label, i) => (
                <button key={i} onClick={() => toggleDay(i)}
                  className="w-7 h-6 rounded text-[9px] font-mono transition-colors"
                  style={chip(customDays.includes(i))}>
                  {label.slice(0, 2)}
                </button>
              ))}
            </div>
          )}
          {/* Preview + Save */}
          <div className="flex items-center gap-2 pl-10">
            <span className="text-[10px] font-mono" style={{ color: '#334155' }}>
              {buildCron(cronTime, dayMode, customDays)}
            </span>
            <button onClick={applyCron}
              className="flex items-center gap-1 px-2.5 py-0.5 rounded text-[10px] font-mono transition-colors"
              style={{ background: 'rgba(6,182,212,0.1)', color: '#06b6d4', border: '1px solid rgba(6,182,212,0.2)' }}
              onMouseEnter={e => (e.currentTarget.style.background = 'rgba(6,182,212,0.18)')}
              onMouseLeave={e => (e.currentTarget.style.background = 'rgba(6,182,212,0.1)')}>
              <Check className="w-3 h-3" />Apply
            </button>
          </div>
        </div>
      )}

      {/* Last / next run */}
      {rec && (
        <div className="flex items-center gap-3 text-[10px] font-mono mt-3" style={{ color: '#334155' }}>
          {rec.lastRunAt && <span>last: {formatLastRun(rec.lastRunAt)}</span>}
          {rec.nextRunAt && enabled && (
            <span style={{ color: new Date(rec.nextRunAt) < new Date() ? '#f87171' : '#334155' }}>
              next: {formatNextRun(rec.nextRunAt)}
            </span>
          )}
          {rec.cronExpr && <span style={{ color: '#1e293b' }}>cron: {rec.cronExpr}</span>}
          {!rec.cronExpr && currentPreset && <span style={{ color: '#1e293b' }}>({currentPreset.label})</span>}
          {!rec.cronExpr && !currentPreset && currentInterval > 0 && <span style={{ color: '#1e293b' }}>({currentInterval}m)</span>}
        </div>
      )}
    </div>
  )
}

interface TaskDetailProps {
  task: Task
  allTasks: Task[]
  columns?: KanbanColumn[]
  onClose: () => void
  onUpdated: (task: Task) => void
  onDeleted: (id: string) => void
  onOpenTerminal?: (task: Task) => void
  onOpenNote?: (noteId: string) => void
  onOpenTask?: (task: Task) => void
}

interface AutoResizeTextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  minRows?: number
}

function AutoResizeTextarea({ minRows = 3, style, onChange, value, ...props }: AutoResizeTextareaProps) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const minHeight = minRows * 20 + 16 // approx line-height 20px + padding

  const resize = () => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.max(el.scrollHeight, minHeight) + 'px'
  }

  useEffect(() => { resize() }, [value])

  return (
    <textarea
      ref={ref}
      value={value}
      style={{ ...style, minHeight, height: 'auto', overflow: 'hidden', resize: 'none' }}
      onChange={e => { onChange?.(e); resize() }}
      {...props}
    />
  )
}

function MarkdownContent({ children }: { children: string }) {
  return (
    <div className="break-words overflow-hidden min-w-0">
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        p: ({ children }) => <p className="text-xs font-mono text-slate-300 leading-relaxed mb-2 last:mb-0 break-words">{children}</p>,
        a: ({ href, children }) => <a href={href} target="_blank" rel="noopener noreferrer" onClick={e => e.stopPropagation()} className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2 transition-colors break-all">{children}</a>,
        code: ({ children, className }) => className
          ? <pre className="text-[11px] font-mono bg-black/30 rounded px-2 py-1.5 overflow-x-auto my-1.5 text-slate-300"><code>{children}</code></pre>
          : <code className="text-[11px] font-mono bg-black/30 rounded px-1 py-0.5 text-cyan-300">{children}</code>,
        ul: ({ children }) => <ul className="list-disc list-inside text-xs font-mono text-slate-300 space-y-0.5 my-1 pl-2">{children}</ul>,
        ol: ({ children }) => <ol className="list-decimal list-inside text-xs font-mono text-slate-300 space-y-0.5 my-1 pl-2">{children}</ol>,
        li: ({ children }) => <li className="leading-relaxed">{children}</li>,
        strong: ({ children }) => <strong className="text-slate-100 font-semibold">{children}</strong>,
        em: ({ children }) => <em className="text-slate-300 italic">{children}</em>,
        blockquote: ({ children }) => <blockquote className="border-l-2 border-slate-600 pl-2.5 my-1 text-slate-400 italic">{children}</blockquote>,
        h1: ({ children }) => <h1 className="text-sm font-mono font-bold text-slate-100 mt-2 mb-1">{children}</h1>,
        h2: ({ children }) => <h2 className="text-xs font-mono font-bold text-slate-200 mt-2 mb-1">{children}</h2>,
        h3: ({ children }) => <h3 className="text-xs font-mono font-semibold text-slate-300 mt-1.5 mb-0.5">{children}</h3>,
        hr: () => <hr className="border-white/[0.08] my-2" />,
      }}
    >
      {children}
    </ReactMarkdown>
    </div>
  )
}

const URL_RE = /(https?:\/\/[^\s]+)/g

function renderWithLinks(text: string) {
  const parts = text.split(URL_RE)
  return parts.map((part, i) =>
    URL_RE.test(part)
      ? <a key={i} href={part} target="_blank" rel="noopener noreferrer"
          onClick={e => e.stopPropagation()}
          className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2 transition-colors"
        >{part}</a>
      : part
  )
}

const PRIORITY_COLORS: Record<string, { bg: string; text: string; border: string }> = {
  low:    { bg: 'rgba(100,116,139,0.12)', text: '#94a3b8', border: 'rgba(100,116,139,0.25)' },
  medium: { bg: 'rgba(59,130,246,0.1)',   text: '#60a5fa', border: 'rgba(59,130,246,0.25)' },
  high:   { bg: 'rgba(245,158,11,0.1)',   text: '#fbbf24', border: 'rgba(245,158,11,0.25)' },
  urgent: { bg: 'rgba(239,68,68,0.1)',    text: '#f87171', border: 'rgba(239,68,68,0.25)' },
}

export function TaskDetail({ task, allTasks, columns, onClose, onUpdated, onDeleted, onOpenTerminal, onOpenNote, onOpenTask }: TaskDetailProps) {
  const { notes, folderTree } = useNotes()
  const [editingTitle, setEditingTitle] = useState(false)
  const [title, setTitle] = useState(task.title)
  const [description, setDescription] = useState(task.description || '')
  const [editingDesc, setEditingDesc] = useState(false)
  const [commentText, setCommentText] = useState('')
  const [editingCommentId, setEditingCommentId] = useState<string | null>(null)
  const [editingCommentText, setEditingCommentText] = useState('')
  const [showNoteSearch, setShowNoteSearch] = useState(false)
  const [showFolderPicker, setShowFolderPicker] = useState(false)
  const [showTaskSearch, setShowTaskSearch] = useState(false)
  const [showStorySearch, setShowStorySearch] = useState(false)
  const [noteQuery, setNoteQuery] = useState('')
  const [taskQuery, setTaskQuery] = useState('')
  const [showStatusMenu, setShowStatusMenu] = useState(false)
  const [showPriorityMenu, setShowPriorityMenu] = useState(false)
  const [tagInput, setTagInput] = useState('')
  const [addingTag, setAddingTag] = useState(false)
  const [runningNow, setRunningNow] = useState(false)

  const titleRef = useRef<HTMLInputElement>(null)
  const dateInputRef = useRef<HTMLInputElement>(null)
  const tagInputRef = useRef<HTMLInputElement>(null)
  const statusMenuRef = useRef<HTMLDivElement>(null)
  const priorityMenuRef = useRef<HTMLDivElement>(null)
  const taskRef = useRef(task)
  useEffect(() => { taskRef.current = task }, [task])
  useEffect(() => { setTitle(task.title); setDescription(task.description || '') }, [task.id])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (statusMenuRef.current && !statusMenuRef.current.contains(e.target as Node)) setShowStatusMenu(false)
      if (priorityMenuRef.current && !priorityMenuRef.current.contains(e.target as Node)) setShowPriorityMenu(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const update = async (data: Parameters<typeof tasksAPI.update>[1]) => {
    const updated = await tasksAPI.update(taskRef.current.id, data)
    onUpdated(updated)
  }

  const saveTitle = async () => {
    setEditingTitle(false)
    if (title !== taskRef.current.title) await update({ title })
  }

  const saveDesc = async () => {
    setEditingDesc(false)
    if (description !== taskRef.current.description) await update({ description })
  }

  const cancelDesc = () => {
    setEditingDesc(false)
    setDescription(taskRef.current.description || '')
  }

  const addComment = async () => {
    if (!commentText.trim()) return
    const t = taskRef.current
    const newComment = await tasksAPI.addComment(t.id, commentText.trim())
    onUpdated({
      ...taskRef.current,
      comments: [...(taskRef.current.comments || []).filter(c => c.id !== newComment.id), newComment],
    })
    setCommentText('')
  }

  const saveComment = async (commentId: string) => {
    await tasksAPI.updateComment(taskRef.current.id, commentId, editingCommentText)
    onUpdated({
      ...taskRef.current,
      comments: (taskRef.current.comments || []).map(c =>
        c.id === commentId ? { ...c, content: editingCommentText } : c
      ),
    })
    setEditingCommentId(null)
  }

  const deleteComment = async (commentId: string) => {
    await tasksAPI.deleteComment(taskRef.current.id, commentId)
    onUpdated({ ...taskRef.current, comments: (taskRef.current.comments || []).filter(c => c.id !== commentId) })
  }

  const unlinkNote = async (noteId: string) => {
    await update({ linkedNoteIds: (task.linkedNoteIds || []).filter(id => id !== noteId) })
  }
  const linkNote = async (noteId: string) => {
    if ((task.linkedNoteIds || []).includes(noteId)) return
    await update({ linkedNoteIds: [...(task.linkedNoteIds || []), noteId] })
    setShowNoteSearch(false); setNoteQuery('')
  }
  const unlinkFolder = async (path: string) => {
    await update({ linkedFolderPaths: (task.linkedFolderPaths || []).filter(p => p !== path) })
  }
  const linkFolder = async (path: string) => {
    if ((task.linkedFolderPaths || []).includes(path)) return
    await update({ linkedFolderPaths: [...(task.linkedFolderPaths || []), path] })
    setShowFolderPicker(false)
  }
  const linkTask = async (linkedTaskId: string) => {
    if ((taskRef.current.linkedTaskIds || []).includes(linkedTaskId)) return
    await update({ linkedTaskIds: [...(taskRef.current.linkedTaskIds || []), linkedTaskId] })
    setShowTaskSearch(false); setTaskQuery('')
  }
  const unlinkTask = async (linkedTaskId: string) => {
    await update({ linkedTaskIds: (taskRef.current.linkedTaskIds || []).filter(id => id !== linkedTaskId) })
  }
  const setParentStory = async (storyId: string) => {
    await update({ parentId: storyId }); setShowStorySearch(false)
  }
  const clearParentStory = async () => { await update({ clearParentId: true }) }

  const addTag = async (raw: string) => {
    const tag = raw.trim().replace(/^#/, '').toLowerCase().replace(/\s+/g, '-')
    if (!tag) return
    const current = task.tags || []
    if (current.includes(tag)) return
    await update({ tags: [...current, tag] })
  }

  const removeTag = async (tag: string) => {
    await update({ tags: (task.tags || []).filter(t => t !== tag) })
  }

  const commitTagInput = async () => {
    const tags = tagInput.split(',').map(t => t.trim()).filter(Boolean)
    for (const tag of tags) await addTag(tag)
    setTagInput('')
    setAddingTag(false)
  }

  const allFolders: string[] = []
  const collectFolders = (node: any) => { if (node.path) allFolders.push(node.path); node.children?.forEach(collectFolders) }
  if (folderTree) collectFolders(folderTree)

  const filteredNotes = noteQuery
    ? (notes || []).filter(n => n.title.toLowerCase().includes(noteQuery.toLowerCase())).slice(0, 8)
    : []

  const filteredTasksForLink = taskQuery
    ? allTasks.filter(t =>
        t.id !== task.id && t.type !== 'story' &&
        !(task.linkedTaskIds || []).includes(t.id) &&
        t.title.toLowerCase().includes(taskQuery.toLowerCase())
      ).slice(0, 8)
    : []

  const availableStories = allTasks.filter(t => t.type === 'story' && t.id !== task.id)
  const parentStory = task.parentId ? allTasks.find(t => t.id === task.parentId) : null
  const subtasks = task.type === 'story' ? allTasks.filter(t => t.parentId === task.id) : []
  const isStory = task.type === 'story'

  const statusOptions = columns && columns.length > 0
    ? columns.map(c => ({ value: c.id, label: c.label, textColor: c.textColor, dotColor: c.dotColor }))
    : TASK_STATUSES.map(s => ({ value: s.value, label: s.label, textColor: '#94a3b8', dotColor: '#64748b' }))
  const currentStatus = statusOptions.find(s => s.value === task.status) ?? { value: task.status, label: task.status, textColor: '#94a3b8', dotColor: '#64748b' }
  const pColors = PRIORITY_COLORS[task.priority] ?? PRIORITY_COLORS.low
  const isOverdue = task.dueDate && new Date(task.dueDate) < new Date() && task.status !== 'done'

  const Divider = () => <div style={{ height: '1px', background: 'rgba(255,255,255,0.05)', margin: '0 16px' }} />

  return (
    <div className="flex flex-col h-full" style={{ background: '#0a0b10', borderLeft: '1px solid rgba(255,255,255,0.06)' }}>

      {/* ── Top bar ── */}
      <div className="flex items-center gap-2 px-4 py-2.5 shrink-0" style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
        {/* Breadcrumb */}
        <div className="flex items-center gap-1.5 flex-1 min-w-0">
          {isStory ? (
            <span className="text-[10px] font-mono font-bold tracking-widest uppercase px-2 py-0.5 rounded shrink-0"
              style={{ color: 'rgba(245,158,11,0.95)', background: 'rgba(245,158,11,0.12)', border: '1px solid rgba(245,158,11,0.2)' }}>
              STORY
            </span>
          ) : (
            <span className="text-[10px] font-mono uppercase tracking-widest shrink-0" style={{ color: '#334155' }}>Task</span>
          )}
          {parentStory && (
            <>
              <ChevronRight className="w-3 h-3 shrink-0" style={{ color: '#334155' }} />
              <button
                onClick={() => onOpenTask?.(parentStory)}
                className="text-[10px] font-mono truncate min-w-0 transition-colors"
                style={{ color: 'rgba(245,158,11,0.55)' }}
                onMouseEnter={e => (e.currentTarget.style.color = 'rgba(245,158,11,0.9)')}
                onMouseLeave={e => (e.currentTarget.style.color = 'rgba(245,158,11,0.55)')}
              >{parentStory.title}</button>
            </>
          )}
        </div>
        {/* Actions */}
        <div className="flex items-center gap-0.5 shrink-0">
          {onOpenTerminal && (
            <button onClick={() => onOpenTerminal(task)} title="Open Terminal"
              className="p-1.5 rounded transition-colors" style={{ color: 'rgba(6,182,212,0.45)' }}
              onMouseEnter={e => { (e.currentTarget as HTMLElement).style.color = '#06b6d4'; (e.currentTarget as HTMLElement).style.background = 'rgba(6,182,212,0.08)' }}
              onMouseLeave={e => { (e.currentTarget as HTMLElement).style.color = 'rgba(6,182,212,0.45)'; (e.currentTarget as HTMLElement).style.background = '' }}>
              <Terminal className="w-3.5 h-3.5" />
            </button>
          )}
          <button onClick={() => { if (confirm('Delete this task?')) { tasksAPI.delete(task.id); onDeleted(task.id) } }}
            className="p-1.5 rounded transition-colors" style={{ color: 'rgba(239,68,68,0.4)' }}
            onMouseEnter={e => { (e.currentTarget as HTMLElement).style.color = '#f87171'; (e.currentTarget as HTMLElement).style.background = 'rgba(239,68,68,0.08)' }}
            onMouseLeave={e => { (e.currentTarget as HTMLElement).style.color = 'rgba(239,68,68,0.4)'; (e.currentTarget as HTMLElement).style.background = '' }}>
            <Trash2 className="w-3.5 h-3.5" />
          </button>
          <button onClick={onClose}
            className="p-1.5 rounded transition-colors" style={{ color: 'rgba(148,163,184,0.35)' }}
            onMouseEnter={e => { (e.currentTarget as HTMLElement).style.color = '#94a3b8'; (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.05)' }}
            onMouseLeave={e => { (e.currentTarget as HTMLElement).style.color = 'rgba(148,163,184,0.35)'; (e.currentTarget as HTMLElement).style.background = '' }}>
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">

        {/* ── Title ── */}
        <div className="px-4 pt-5 pb-3">
          {editingTitle ? (
            <input
              ref={titleRef}
              value={title}
              onChange={e => setTitle(e.target.value)}
              onBlur={saveTitle}
              onKeyDown={e => { if (e.key === 'Enter') saveTitle(); if (e.key === 'Escape') { setEditingTitle(false); setTitle(task.title) } }}
              className="w-full bg-transparent text-xl font-mono text-slate-100 outline-none pb-1"
              style={{ borderBottom: '2px solid rgba(6,182,212,0.4)' }}
              autoFocus
            />
          ) : (
            <div className="group flex items-start gap-2 cursor-text" onClick={() => setEditingTitle(true)}>
              <h2 className="flex-1 text-xl font-mono text-slate-100 leading-snug group-hover:text-white transition-colors">{task.title}</h2>
              <Pencil className="w-3.5 h-3.5 mt-1.5 shrink-0 transition-all opacity-0 group-hover:opacity-100" style={{ color: 'rgba(6,182,212,0.5)' }} />
            </div>
          )}
        </div>

        {/* ── Property chips ── */}
        <div className="px-4 pb-4 flex flex-wrap gap-2 items-center">

          {/* Status */}
          <div className="relative" ref={statusMenuRef}>
            <button
              onClick={() => { setShowStatusMenu(v => !v); setShowPriorityMenu(false) }}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-mono font-medium transition-all"
              style={{ background: `${currentStatus.textColor}1a`, border: `1px solid ${currentStatus.textColor}33`, color: currentStatus.textColor }}
              onMouseEnter={e => (e.currentTarget.style.background = `${currentStatus.textColor}2a`)}
              onMouseLeave={e => (e.currentTarget.style.background = `${currentStatus.textColor}1a`)}
            >
              <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ background: currentStatus.dotColor }} />
              {currentStatus.label}
              <ChevronDown className="w-3 h-3 opacity-50" />
            </button>
            {showStatusMenu && (
              <div className="absolute top-full left-0 mt-1 z-50 rounded-lg overflow-hidden shadow-2xl py-1"
                style={{ background: '#0d0f17', border: '1px solid rgba(255,255,255,0.1)', minWidth: '148px' }}>
                {statusOptions.map(opt => (
                  <button key={opt.value}
                    onClick={() => { update({ status: opt.value as TaskStatus }); setShowStatusMenu(false) }}
                    className="w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono transition-colors text-left"
                    style={{ color: opt.value === task.status ? opt.textColor : '#475569' }}
                    onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.05)')}
                    onMouseLeave={e => (e.currentTarget.style.background = '')}>
                    <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ background: opt.dotColor }} />
                    {opt.label}
                    {opt.value === task.status && <Check className="w-3 h-3 ml-auto" style={{ color: opt.textColor }} />}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Priority */}
          <div className="relative" ref={priorityMenuRef}>
            <button
              onClick={() => { setShowPriorityMenu(v => !v); setShowStatusMenu(false) }}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-mono font-medium transition-all"
              style={{ background: pColors.bg, border: `1px solid ${pColors.border}`, color: pColors.text }}
              onMouseEnter={e => (e.currentTarget.style.filter = 'brightness(1.2)')}
              onMouseLeave={e => (e.currentTarget.style.filter = '')}>
              {PRIORITY_CONFIG[task.priority]?.label ?? task.priority}
              <ChevronDown className="w-3 h-3 opacity-50" />
            </button>
            {showPriorityMenu && (
              <div className="absolute top-full left-0 mt-1 z-50 rounded-lg overflow-hidden shadow-2xl py-1"
                style={{ background: '#0d0f17', border: '1px solid rgba(255,255,255,0.1)', minWidth: '120px' }}>
                {Object.entries(PRIORITY_CONFIG).map(([v, c]) => {
                  const pc = PRIORITY_COLORS[v] ?? PRIORITY_COLORS.low
                  return (
                    <button key={v}
                      onClick={() => { update({ priority: v as TaskPriority }); setShowPriorityMenu(false) }}
                      className="w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono transition-colors text-left"
                      style={{ color: v === task.priority ? pc.text : '#475569' }}
                      onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.05)')}
                      onMouseLeave={e => (e.currentTarget.style.background = '')}>
                      {c.label}
                      {v === task.priority && <Check className="w-3 h-3 ml-auto" style={{ color: pc.text }} />}
                    </button>
                  )
                })}
              </div>
            )}
          </div>

          {/* Due date */}
          <div className="relative">
            <button
              onClick={() => dateInputRef.current?.click()}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-mono transition-all"
              style={{
                background: isOverdue ? 'rgba(239,68,68,0.1)' : 'rgba(255,255,255,0.04)',
                border: isOverdue ? '1px solid rgba(239,68,68,0.25)' : '1px solid rgba(255,255,255,0.08)',
                color: isOverdue ? '#f87171' : task.dueDate ? '#94a3b8' : '#475569',
              }}
              onMouseEnter={e => (e.currentTarget.style.background = isOverdue ? 'rgba(239,68,68,0.16)' : 'rgba(255,255,255,0.07)')}
              onMouseLeave={e => (e.currentTarget.style.background = isOverdue ? 'rgba(239,68,68,0.1)' : 'rgba(255,255,255,0.04)')}
            >
              <Calendar className="w-3 h-3" />
              {task.dueDate
                ? new Date(task.dueDate).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })
                : 'Due date'}
            </button>
            <input
              ref={dateInputRef}
              type="date"
              value={task.dueDate ? task.dueDate.split('T')[0] : ''}
              onChange={e => e.target.value ? update({ dueDate: e.target.value }) : update({ clearDueDate: true })}
              className="absolute opacity-0 w-0 h-0 pointer-events-none"
              tabIndex={-1}
            />
          </div>
        </div>

        {/* ── Tags ── */}
        <div className="px-4 pb-4 flex flex-wrap gap-1.5 items-center">
          {(task.tags || []).map(tag => (
            <span key={tag} className="group flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono transition-colors"
              style={{ background: 'rgba(6,182,212,0.08)', border: '1px solid rgba(6,182,212,0.18)', color: 'rgba(6,182,212,0.7)' }}>
              #{tag}
              <button onClick={() => removeTag(tag)}
                className="opacity-0 group-hover:opacity-100 transition-opacity ml-0.5 leading-none"
                style={{ color: 'rgba(239,68,68,0.6)' }}
                onMouseEnter={e => (e.currentTarget.style.color = '#f87171')}
                onMouseLeave={e => (e.currentTarget.style.color = 'rgba(239,68,68,0.6)')}>
                <X className="w-2.5 h-2.5" />
              </button>
            </span>
          ))}
          {addingTag ? (
            <input
              ref={tagInputRef}
              value={tagInput}
              onChange={e => setTagInput(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); commitTagInput() }
                if (e.key === 'Escape') { setTagInput(''); setAddingTag(false) }
              }}
              onBlur={commitTagInput}
              placeholder="tag, tag…"
              className="rounded px-2 py-0.5 text-[11px] font-mono outline-none"
              style={{ background: 'rgba(6,182,212,0.06)', border: '1px solid rgba(6,182,212,0.3)', color: '#06b6d4', width: '100px' }}
              autoFocus
            />
          ) : (
            <button onClick={() => { setAddingTag(true); setTimeout(() => tagInputRef.current?.focus(), 0) }}
              className="flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono transition-colors"
              style={{ border: '1px dashed rgba(255,255,255,0.1)', color: '#475569' }}
              onMouseEnter={e => { (e.currentTarget as HTMLElement).style.borderColor = 'rgba(6,182,212,0.3)'; (e.currentTarget as HTMLElement).style.color = 'rgba(6,182,212,0.6)' }}
              onMouseLeave={e => { (e.currentTarget as HTMLElement).style.borderColor = 'rgba(255,255,255,0.1)'; (e.currentTarget as HTMLElement).style.color = '#475569' }}>
              <Plus className="w-3 h-3" />Tag
            </button>
          )}
        </div>

        <Divider />

        {/* ── Schedule ── */}
        <ScheduleSection task={task} update={update} runningNow={runningNow} setRunningNow={setRunningNow} />

        <Divider />

        {/* ── Description ── */}
        <div className="px-4 py-4">
          <div className="flex items-center justify-between mb-2.5">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>Description</span>
            {!editingDesc && (
              <button onClick={() => setEditingDesc(true)}
                className="flex items-center gap-1 text-[10px] font-mono transition-colors"
                style={{ color: 'rgba(6,182,212,0.35)' }}
                onMouseEnter={e => (e.currentTarget.style.color = '#06b6d4')}
                onMouseLeave={e => (e.currentTarget.style.color = 'rgba(6,182,212,0.35)')}>
                <Edit2 className="w-3 h-3" />Edit
              </button>
            )}
          </div>
          {editingDesc ? (
            <div className="space-y-2">
              <AutoResizeTextarea
                value={description}
                onChange={e => setDescription(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) saveDesc()
                  if (e.key === 'Escape') cancelDesc()
                }}
                minRows={5}
                placeholder="Describe this task…"
                className="w-full rounded-lg px-3 py-2.5 text-xs font-mono text-slate-300 outline-none"
                style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(6,182,212,0.25)' }}
                autoFocus
              />
              <div className="flex gap-2">
                <button onClick={saveDesc}
                  className="flex items-center gap-1 px-3 py-1 rounded text-[11px] font-mono font-medium transition-colors"
                  style={{ background: 'rgba(34,197,94,0.1)', color: '#4ade80', border: '1px solid rgba(34,197,94,0.2)' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(34,197,94,0.18)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'rgba(34,197,94,0.1)')}>
                  <Check className="w-3 h-3" />Save
                </button>
                <button onClick={cancelDesc}
                  className="text-[11px] font-mono px-3 py-1 rounded transition-colors"
                  style={{ color: '#475569' }}
                  onMouseEnter={e => (e.currentTarget.style.color = '#94a3b8')}
                  onMouseLeave={e => (e.currentTarget.style.color = '#475569')}>
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div
              onClick={() => setEditingDesc(true)}
              className="cursor-text rounded-lg px-3 py-2.5 min-h-[72px] transition-colors"
              style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)' }}
              onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.035)')}
              onMouseLeave={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}
            >
              {description
                ? <MarkdownContent>{description}</MarkdownContent>
                : <span className="text-xs font-mono italic" style={{ color: '#2d3748' }}>Add a description…</span>
              }
            </div>
          )}
        </div>

        <Divider />

        {/* ── Subtasks (stories only) ── */}
        {isStory && (
          <>
            <div className="px-4 py-4">
              <div className="flex items-center gap-2 mb-3">
                <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>Subtasks</span>
                {subtasks.length > 0 && (
                  <span className="text-[10px] font-mono" style={{ color: 'rgba(245,158,11,0.5)' }}>
                    {subtasks.filter(t => t.status === 'done').length}/{subtasks.length} done
                  </span>
                )}
              </div>
              {subtasks.length > 0 ? (
                <div className="space-y-1">
                  {subtasks.map(t => {
                    const pc = PRIORITY_CONFIG[t.priority] ?? { label: t.priority }
                    const tpc = PRIORITY_COLORS[t.priority] ?? PRIORITY_COLORS.low
                    return (
                      <button key={t.id} onClick={() => onOpenTask?.(t)}
                        className="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-left group transition-all"
                        style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)' }}
                        onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.045)'; (e.currentTarget as HTMLElement).style.borderColor = 'rgba(6,182,212,0.15)' }}
                        onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.02)'; (e.currentTarget as HTMLElement).style.borderColor = 'rgba(255,255,255,0.04)' }}>
                        <span className="text-[9px] font-mono font-bold shrink-0" style={{ color: tpc.text }}>{pc.label.slice(0,3).toUpperCase()}</span>
                        <span className="text-xs font-mono text-slate-400 truncate flex-1 group-hover:text-slate-200 transition-colors">{t.title}</span>
                        <span className="text-[9px] font-mono shrink-0" style={{ color: t.status === 'done' ? '#4ade80' : '#334155' }}>{t.status}</span>
                        <ChevronRight className="w-3 h-3 shrink-0 opacity-0 group-hover:opacity-50 transition-opacity" style={{ color: '#06b6d4' }} />
                      </button>
                    )
                  })}
                </div>
              ) : (
                <p className="text-xs font-mono italic" style={{ color: '#2d3748' }}>No subtasks yet</p>
              )}
            </div>
            <Divider />
          </>
        )}

        {/* ── Parent Story (non-stories only) ── */}
        {!isStory && (
          <>
            <div className="px-4 py-4">
              <div className="flex items-center justify-between mb-2.5">
                <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>Parent Story</span>
                {!parentStory && (
                  <button onClick={() => setShowStorySearch(v => !v)}
                    className="flex items-center gap-1 text-[10px] font-mono transition-colors"
                    style={{ color: 'rgba(245,158,11,0.4)' }}
                    onMouseEnter={e => (e.currentTarget.style.color = 'rgba(245,158,11,0.9)')}
                    onMouseLeave={e => (e.currentTarget.style.color = 'rgba(245,158,11,0.4)')}>
                    <Plus className="w-3 h-3" />Link story
                  </button>
                )}
              </div>
              {showStorySearch && (
                <div className="mb-2 rounded-lg overflow-hidden" style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)' }}>
                  <div className="max-h-40 overflow-y-auto">
                    {availableStories.length === 0
                      ? <div className="px-3 py-2.5 text-[11px] font-mono" style={{ color: '#334155' }}>No stories available</div>
                      : availableStories.map(s => (
                          <button key={s.id} onClick={() => setParentStory(s.id)}
                            className="w-full flex items-center gap-2 px-3 py-2 text-xs font-mono text-left transition-colors"
                            style={{ color: '#64748b' }}
                            onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'rgba(245,158,11,0.05)'; (e.currentTarget as HTMLElement).style.color = 'rgba(245,158,11,0.9)' }}
                            onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; (e.currentTarget as HTMLElement).style.color = '#64748b' }}>
                            <BookOpen className="w-3 h-3 shrink-0" style={{ color: 'rgba(245,158,11,0.5)' }} />
                            {s.title}
                          </button>
                        ))}
                  </div>
                </div>
              )}
              {parentStory ? (
                <div className="flex items-center gap-2 px-3 py-2 rounded-lg group"
                  style={{ background: 'rgba(245,158,11,0.05)', border: '1px solid rgba(245,158,11,0.12)' }}>
                  <BookOpen className="w-3.5 h-3.5 shrink-0" style={{ color: 'rgba(245,158,11,0.6)' }} />
                  <button onClick={() => onOpenTask?.(parentStory)}
                    className="text-xs font-mono truncate flex-1 text-left transition-colors"
                    style={{ color: 'rgba(245,158,11,0.8)' }}
                    onMouseEnter={e => (e.currentTarget.style.color = 'rgba(245,158,11,1)')}
                    onMouseLeave={e => (e.currentTarget.style.color = 'rgba(245,158,11,0.8)')}>
                    {parentStory.title}
                  </button>
                  <button onClick={clearParentStory}
                    className="opacity-0 group-hover:opacity-100 transition-opacity p-0.5 rounded"
                    style={{ color: 'rgba(239,68,68,0.5)' }}
                    onMouseEnter={e => (e.currentTarget.style.color = '#f87171')}
                    onMouseLeave={e => (e.currentTarget.style.color = 'rgba(239,68,68,0.5)')}>
                    <X className="w-3 h-3" />
                  </button>
                </div>
              ) : !showStorySearch && (
                <p className="text-xs font-mono italic" style={{ color: '#2d3748' }}>No parent story</p>
              )}
            </div>
            <Divider />
          </>
        )}

        {/* ── Linked Notes ── */}
        <div className="px-4 py-4">
          <div className="flex items-center justify-between mb-2.5">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>
              Linked Notes{task.linkedNoteIds?.length ? ` (${task.linkedNoteIds.length})` : ''}
            </span>
            <button onClick={() => setShowNoteSearch(v => !v)}
              className="flex items-center gap-1 text-[10px] font-mono transition-colors"
              style={{ color: 'rgba(6,182,212,0.35)' }}
              onMouseEnter={e => (e.currentTarget.style.color = '#06b6d4')}
              onMouseLeave={e => (e.currentTarget.style.color = 'rgba(6,182,212,0.35)')}>
              <Plus className="w-3 h-3" />Link
            </button>
          </div>
          {showNoteSearch && (
            <div className="mb-2">
              <input value={noteQuery} onChange={e => setNoteQuery(e.target.value)} placeholder="Search notes…"
                className="w-full rounded-lg px-3 py-2 text-xs font-mono outline-none mb-1"
                style={{ background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#cbd5e1' }}
                autoFocus />
              {filteredNotes.map(n => (
                <button key={n.id} onClick={() => linkNote(n.id)}
                  className="w-full flex items-center gap-2 px-2.5 py-1.5 text-xs font-mono text-left rounded transition-colors"
                  style={{ color: '#64748b' }}
                  onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.04)'; (e.currentTarget as HTMLElement).style.color = '#06b6d4' }}
                  onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; (e.currentTarget as HTMLElement).style.color = '#64748b' }}>
                  <FileText className="w-3 h-3 shrink-0" />{n.title}
                </button>
              ))}
            </div>
          )}
          <div className="space-y-1">
            {!(task.linkedNoteIds?.length) && !showNoteSearch && (
              <p className="text-xs font-mono italic" style={{ color: '#2d3748' }}>No linked notes</p>
            )}
            {(task.linkedNoteIds || []).map(id => {
              const note = (notes || []).find(n => n.id === id)
              return (
                <div key={id} className="flex items-center gap-2 px-2.5 py-1.5 rounded-lg group transition-colors"
                  style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.04)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}>
                  <FileText className="w-3 h-3 shrink-0" style={{ color: 'rgba(6,182,212,0.45)' }} />
                  <button onClick={() => onOpenNote?.(id)} disabled={!onOpenNote}
                    className="text-xs font-mono truncate flex-1 text-left transition-colors"
                    style={{ color: '#64748b' }}
                    onMouseEnter={e => (e.currentTarget.style.color = '#06b6d4')}
                    onMouseLeave={e => (e.currentTarget.style.color = '#64748b')}>
                    {note?.title || id}
                  </button>
                  <button onClick={() => unlinkNote(id)}
                    className="opacity-0 group-hover:opacity-100 transition-opacity p-0.5"
                    style={{ color: 'rgba(239,68,68,0.45)' }}
                    onMouseEnter={e => (e.currentTarget.style.color = '#f87171')}
                    onMouseLeave={e => (e.currentTarget.style.color = 'rgba(239,68,68,0.45)')}>
                    <X className="w-3 h-3" />
                  </button>
                </div>
              )
            })}
          </div>
        </div>

        <Divider />

        {/* ── Linked Folders ── */}
        <div className="px-4 py-4">
          <div className="flex items-center justify-between mb-2.5">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>
              Linked Folders{task.linkedFolderPaths?.length ? ` (${task.linkedFolderPaths.length})` : ''}
            </span>
            <button onClick={() => setShowFolderPicker(v => !v)}
              className="flex items-center gap-1 text-[10px] font-mono transition-colors"
              style={{ color: 'rgba(6,182,212,0.35)' }}
              onMouseEnter={e => (e.currentTarget.style.color = '#06b6d4')}
              onMouseLeave={e => (e.currentTarget.style.color = 'rgba(6,182,212,0.35)')}>
              <Plus className="w-3 h-3" />Link
            </button>
          </div>
          {showFolderPicker && (
            <div className="mb-2 max-h-36 overflow-y-auto rounded-lg"
              style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)' }}>
              {allFolders.map(f => (
                <button key={f} onClick={() => linkFolder(f)}
                  className="w-full text-left px-3 py-1.5 text-xs font-mono transition-colors"
                  style={{ color: '#64748b' }}
                  onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.04)'; (e.currentTarget as HTMLElement).style.color = '#06b6d4' }}
                  onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; (e.currentTarget as HTMLElement).style.color = '#64748b' }}>
                  {f}
                </button>
              ))}
            </div>
          )}
          <div className="space-y-1">
            {!(task.linkedFolderPaths?.length) && !showFolderPicker && (
              <p className="text-xs font-mono italic" style={{ color: '#2d3748' }}>No linked folders</p>
            )}
            {(task.linkedFolderPaths || []).map(path => (
              <div key={path} className="flex items-center gap-2 px-2.5 py-1.5 rounded-lg group transition-colors"
                style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)' }}
                onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.04)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}>
                <Folder className="w-3 h-3 shrink-0" style={{ color: 'rgba(6,182,212,0.45)' }} />
                <span className="text-xs font-mono truncate flex-1" style={{ color: '#64748b' }}>{path}</span>
                <button onClick={() => unlinkFolder(path)}
                  className="opacity-0 group-hover:opacity-100 transition-opacity p-0.5"
                  style={{ color: 'rgba(239,68,68,0.45)' }}
                  onMouseEnter={e => (e.currentTarget.style.color = '#f87171')}
                  onMouseLeave={e => (e.currentTarget.style.color = 'rgba(239,68,68,0.45)')}>
                  <X className="w-3 h-3" />
                </button>
              </div>
            ))}
          </div>
        </div>

        <Divider />

        {/* ── Linked Tasks ── */}
        <div className="px-4 py-4">
          <div className="flex items-center justify-between mb-2.5">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>
              Linked Tasks{task.linkedTaskIds?.length ? ` (${task.linkedTaskIds.length})` : ''}
            </span>
            <button onClick={() => setShowTaskSearch(v => !v)}
              className="flex items-center gap-1 text-[10px] font-mono transition-colors"
              style={{ color: 'rgba(6,182,212,0.35)' }}
              onMouseEnter={e => (e.currentTarget.style.color = '#06b6d4')}
              onMouseLeave={e => (e.currentTarget.style.color = 'rgba(6,182,212,0.35)')}>
              <Plus className="w-3 h-3" />Link
            </button>
          </div>
          {showTaskSearch && (
            <div className="mb-2">
              <input value={taskQuery} onChange={e => setTaskQuery(e.target.value)} placeholder="Search tasks…"
                className="w-full rounded-lg px-3 py-2 text-xs font-mono outline-none mb-1"
                style={{ background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#cbd5e1' }}
                autoFocus />
              {filteredTasksForLink.map(t => {
                const pc = PRIORITY_CONFIG[t.priority] ?? { label: t.priority }
                const tpc = PRIORITY_COLORS[t.priority] ?? PRIORITY_COLORS.low
                return (
                  <button key={t.id} onClick={() => linkTask(t.id)}
                    className="w-full flex items-center gap-2 px-2.5 py-1.5 text-xs font-mono text-left rounded transition-colors"
                    style={{ color: '#64748b' }}
                    onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.04)'; (e.currentTarget as HTMLElement).style.color = '#06b6d4' }}
                    onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; (e.currentTarget as HTMLElement).style.color = '#64748b' }}>
                    <span className="text-[9px] font-mono font-bold shrink-0" style={{ color: tpc.text }}>{pc.label.slice(0,3).toUpperCase()}</span>
                    {t.title}
                  </button>
                )
              })}
            </div>
          )}
          <div className="space-y-1">
            {!(task.linkedTaskIds?.length) && !showTaskSearch && (
              <p className="text-xs font-mono italic" style={{ color: '#2d3748' }}>No linked tasks</p>
            )}
            {(task.linkedTaskIds || []).map(id => {
              const linked = allTasks.find(t => t.id === id)
              if (!linked) return null
              const pc = PRIORITY_CONFIG[linked.priority] ?? { label: linked.priority }
              const tpc = PRIORITY_COLORS[linked.priority] ?? PRIORITY_COLORS.low
              const lStatus = statusOptions.find(s => s.value === linked.status)
              return (
                <div key={id} className="flex items-center gap-2 px-2.5 py-1.5 rounded-lg group transition-colors"
                  style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.04)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'rgba(255,255,255,0.02)')}>
                  <Link2 className="w-3 h-3 shrink-0" style={{ color: '#334155' }} />
                  <span className="text-[9px] font-mono font-bold shrink-0" style={{ color: tpc.text }}>{pc.label.slice(0,3).toUpperCase()}</span>
                  <button onClick={() => onOpenTask?.(linked)}
                    className="text-xs font-mono truncate flex-1 text-left transition-colors"
                    style={{ color: '#64748b' }}
                    onMouseEnter={e => (e.currentTarget.style.color = '#06b6d4')}
                    onMouseLeave={e => (e.currentTarget.style.color = '#64748b')}>
                    {linked.title}
                  </button>
                  <span className="text-[9px] font-mono shrink-0" style={{ color: lStatus?.textColor ?? '#334155' }}>{linked.status}</span>
                  <button onClick={() => unlinkTask(id)}
                    className="opacity-0 group-hover:opacity-100 transition-opacity p-0.5"
                    style={{ color: 'rgba(239,68,68,0.45)' }}
                    onMouseEnter={e => (e.currentTarget.style.color = '#f87171')}
                    onMouseLeave={e => (e.currentTarget.style.color = 'rgba(239,68,68,0.45)')}>
                    <X className="w-3 h-3" />
                  </button>
                </div>
              )
            })}
          </div>
        </div>

        <Divider />

        {/* ── Activity / Comments ── */}
        <div className="px-4 py-4">
          <div className="flex items-center gap-2 mb-4">
            <span className="text-[11px] font-mono font-semibold uppercase tracking-wider" style={{ color: '#475569' }}>Activity</span>
            {!!task.comments?.length && (
              <span className="text-[10px] font-mono" style={{ color: '#334155' }}>
                {task.comments.length} comment{task.comments.length !== 1 ? 's' : ''}
              </span>
            )}
          </div>

          {/* Comments */}
          <div className="space-y-4 mb-5">
            {!task.comments?.length && (
              <p className="text-xs font-mono italic" style={{ color: '#2d3748' }}>No activity yet</p>
            )}
            {(task.comments || []).map(c => (
              <div key={c.id} className="flex items-start gap-2.5 group">
                <div className="w-6 h-6 rounded-full shrink-0 flex items-center justify-center mt-0.5"
                  style={{ background: 'rgba(6,182,212,0.08)', border: '1px solid rgba(6,182,212,0.18)' }}>
                  <User className="w-3 h-3" style={{ color: 'rgba(6,182,212,0.55)' }} />
                </div>
                <div className="flex-1 min-w-0">
                  {editingCommentId === c.id ? (
                    <div className="space-y-1.5">
                      <AutoResizeTextarea
                        value={editingCommentText}
                        onChange={e => setEditingCommentText(e.target.value)}
                        minRows={3}
                        className="w-full rounded-lg px-3 py-2 text-xs font-mono outline-none"
                        style={{ background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(6,182,212,0.25)', color: '#cbd5e1' }}
                        autoFocus
                      />
                      <div className="flex gap-2">
                        <button onClick={() => saveComment(c.id)}
                          className="flex items-center gap-1 px-2.5 py-0.5 rounded text-[10px] font-mono transition-colors"
                          style={{ background: 'rgba(34,197,94,0.1)', color: '#4ade80', border: '1px solid rgba(34,197,94,0.2)' }}>
                          <Check className="w-3 h-3" />Save
                        </button>
                        <button onClick={() => setEditingCommentId(null)}
                          className="text-[10px] font-mono px-2.5 py-0.5 rounded transition-colors"
                          style={{ color: '#475569' }}
                          onMouseEnter={e => (e.currentTarget.style.color = '#94a3b8')}
                          onMouseLeave={e => (e.currentTarget.style.color = '#475569')}>
                          Cancel
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="rounded-lg px-3 py-2.5"
                      style={{ background: 'rgba(255,255,255,0.025)', border: '1px solid rgba(255,255,255,0.05)' }}>
                      <MarkdownContent>{c.content}</MarkdownContent>
                      <div className="flex items-center justify-between mt-2">
                        <span className="text-[10px] font-mono" style={{ color: '#334155' }}>
                          {new Date(c.createdAt).toLocaleDateString('en-GB', { day: '2-digit', month: 'short' })}
                          {' · '}
                          {new Date(c.createdAt).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}
                        </span>
                        <div className="flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                          <button onClick={() => { setEditingCommentId(c.id); setEditingCommentText(c.content) }}
                            className="p-1 rounded transition-colors"
                            style={{ color: '#475569' }}
                            onMouseEnter={e => { (e.currentTarget as HTMLElement).style.color = '#94a3b8'; (e.currentTarget as HTMLElement).style.background = 'rgba(255,255,255,0.05)' }}
                            onMouseLeave={e => { (e.currentTarget as HTMLElement).style.color = '#475569'; (e.currentTarget as HTMLElement).style.background = '' }}>
                            <Edit2 className="w-3 h-3" />
                          </button>
                          <button onClick={() => deleteComment(c.id)}
                            className="p-1 rounded transition-colors"
                            style={{ color: 'rgba(239,68,68,0.35)' }}
                            onMouseEnter={e => { (e.currentTarget as HTMLElement).style.color = '#f87171'; (e.currentTarget as HTMLElement).style.background = 'rgba(239,68,68,0.08)' }}
                            onMouseLeave={e => { (e.currentTarget as HTMLElement).style.color = 'rgba(239,68,68,0.35)'; (e.currentTarget as HTMLElement).style.background = '' }}>
                            <X className="w-3 h-3" />
                          </button>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>

          {/* Add comment */}
          <div className="flex items-start gap-2.5">
            <div className="w-6 h-6 rounded-full shrink-0 flex items-center justify-center mt-0.5"
              style={{ background: 'rgba(6,182,212,0.05)', border: '1px solid rgba(6,182,212,0.1)' }}>
              <User className="w-3 h-3" style={{ color: 'rgba(6,182,212,0.3)' }} />
            </div>
            <div className="flex-1 space-y-2">
              <AutoResizeTextarea
                value={commentText}
                onChange={e => setCommentText(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) addComment() }}
                placeholder="Add a comment… (Cmd+Enter)"
                minRows={2}
                className="w-full rounded-lg px-3 py-2 text-xs font-mono outline-none transition-colors"
                style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.06)', color: '#cbd5e1' }}
                onFocus={e => (e.currentTarget.style.borderColor = 'rgba(6,182,212,0.22)')}
                onBlur={e => (e.currentTarget.style.borderColor = 'rgba(255,255,255,0.06)')}
              />
              {commentText.trim() && (
                <button onClick={addComment}
                  className="flex items-center gap-1.5 px-3 py-1 rounded-md text-[11px] font-mono font-medium transition-colors"
                  style={{ background: 'rgba(6,182,212,0.1)', color: '#06b6d4', border: '1px solid rgba(6,182,212,0.2)' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(6,182,212,0.18)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'rgba(6,182,212,0.1)')}>
                  <Send className="w-3 h-3" />Save comment
                </button>
              )}
            </div>
          </div>
        </div>

        <div className="h-8" />
      </div>
    </div>
  )
}
