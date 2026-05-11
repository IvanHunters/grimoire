import type { Task } from '../../types/task'
import { PRIORITY_CONFIG } from '../../types/task'
import { FileText, Folder, MessageSquare, Calendar, Link2, RefreshCw, Pin } from 'lucide-react'

interface TaskCardProps {
  task: Task
  onClick: () => void
  isChild?: boolean
  parentLabel?: string
  pinned?: boolean
}

export function TaskCard({ task, onClick, isChild, parentLabel, pinned }: TaskCardProps) {
  const priority = PRIORITY_CONFIG[task.priority] ?? { label: task.priority, color: 'text-slate-400' }
  const isOverdue = task.dueDate && new Date(task.dueDate) < new Date() && task.status !== 'done'
  const rec = task.recurring
  const hasRecurring = !!rec
  const recurringActive = rec?.enabled ?? false

  if (isChild) {
    return (
      <div
        onClick={onClick}
        className="group flex items-start gap-2 rounded-lg px-2 py-1.5 cursor-pointer transition-all"
        style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.04)' }}
      >
        <span className={`text-[9px] font-mono font-bold mt-0.5 shrink-0 ${priority.color}`}>
          {priority.label.slice(0, 3).toUpperCase()}
        </span>
        <span className="text-xs font-mono text-slate-300 leading-snug flex-1 group-hover:text-slate-100 transition-colors line-clamp-2">
          {task.title}
        </span>
        {hasRecurring && (
          <RefreshCw className="w-2.5 h-2.5 mt-0.5 shrink-0"
            style={{ color: recurringActive ? 'rgba(6,182,212,0.6)' : 'rgba(100,116,139,0.4)' }} />
        )}
        {isOverdue && <span className="text-[9px] font-mono text-red-400 shrink-0 mt-0.5">!</span>}
      </div>
    )
  }

  return (
    <div
      onClick={onClick}
      className="group relative rounded-lg p-3 cursor-pointer transition-all"
      style={{
        background: recurringActive ? 'rgba(6,182,212,0.03)' : 'rgba(15,17,23,0.9)',
        border: pinned
          ? '1px solid rgba(251,191,36,0.2)'
          : recurringActive
            ? '1px solid rgba(6,182,212,0.18)'
            : hasRecurring
              ? '1px solid rgba(100,116,139,0.15)'
              : '1px solid rgba(255,255,255,0.06)',
        borderTopColor: pinned ? 'rgba(251,191,36,0.5)' : undefined,
        borderTopWidth: pinned ? '2px' : undefined,
        borderLeftWidth: !pinned && hasRecurring ? '2px' : undefined,
        borderLeftColor: !pinned && hasRecurring
          ? (recurringActive ? 'rgba(6,182,212,0.45)' : 'rgba(100,116,139,0.3)')
          : undefined,
      }}
      onMouseEnter={e => {
        (e.currentTarget as HTMLDivElement).style.borderColor = recurringActive ? 'rgba(6,182,212,0.4)' : 'rgba(6,182,212,0.25)'
        ;(e.currentTarget as HTMLDivElement).style.background = recurringActive ? 'rgba(6,182,212,0.06)' : 'rgba(15,17,23,1)'
      }}
      onMouseLeave={e => {
        (e.currentTarget as HTMLDivElement).style.borderColor = recurringActive ? 'rgba(6,182,212,0.18)' : hasRecurring ? 'rgba(100,116,139,0.15)' : 'rgba(255,255,255,0.06)'
        ;(e.currentTarget as HTMLDivElement).style.background = recurringActive ? 'rgba(6,182,212,0.03)' : 'rgba(15,17,23,0.9)'
      }}
    >
      {pinned && (
        <Pin className="absolute top-2 right-2 w-3 h-3 text-amber-400/50" />
      )}

      {/* Parent group indicator */}
      {parentLabel && (
        <div className="flex items-center gap-1 mb-1.5 text-[9px] font-mono text-slate-700 truncate">
          <span className="opacity-50">↳</span>
          <span className="truncate">{parentLabel}</span>
        </div>
      )}

      {/* Priority + title */}
      <div className="flex items-start gap-2 mb-2">
        <span className={`text-[10px] font-mono font-semibold mt-0.5 shrink-0 ${priority.color}`}>
          {priority.label.toUpperCase()}
        </span>
        <span className="text-sm font-mono text-slate-200 leading-snug line-clamp-2 group-hover:text-white transition-colors flex-1">
          {task.title}
        </span>
      </div>

      {/* Tags */}
      {task.tags && task.tags.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-2">
          {task.tags.slice(0, 3).map(tag => (
            <span key={tag} className="text-[10px] font-mono bg-white/[0.05] text-slate-500 px-1.5 py-0.5 rounded">
              #{tag}
            </span>
          ))}
        </div>
      )}

      {/* Recurring badge */}
      {hasRecurring && (
        <div className="flex items-center gap-1.5 mb-2">
          <span
            className="flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-mono"
            style={recurringActive
              ? { background: 'rgba(6,182,212,0.1)', color: 'rgba(6,182,212,0.8)', border: '1px solid rgba(6,182,212,0.2)' }
              : { background: 'rgba(100,116,139,0.08)', color: 'rgba(100,116,139,0.5)', border: '1px solid rgba(100,116,139,0.15)' }
            }
          >
            <RefreshCw className="w-2.5 h-2.5" />
            {recurringActive ? 'scheduled' : 'paused'}
          </span>
          {recurringActive && rec?.nextRunAt && (
            <span className="text-[9px] font-mono" style={{ color: 'rgba(6,182,212,0.4)' }}>
              {formatRecurringNext(rec.nextRunAt)}
            </span>
          )}
        </div>
      )}

      {/* Meta row */}
      <div className="flex items-center gap-3 text-[10px] font-mono text-slate-600">
        {task.linkedNoteIds && task.linkedNoteIds.length > 0 && (
          <span className="flex items-center gap-1">
            <FileText className="w-3 h-3" />{task.linkedNoteIds.length}
          </span>
        )}
        {task.linkedFolderPaths && task.linkedFolderPaths.length > 0 && (
          <span className="flex items-center gap-1">
            <Folder className="w-3 h-3" />{task.linkedFolderPaths.length}
          </span>
        )}
        {task.linkedTaskIds && task.linkedTaskIds.length > 0 && (
          <span className="flex items-center gap-1">
            <Link2 className="w-3 h-3" />{task.linkedTaskIds.length}
          </span>
        )}
        {task.comments && task.comments.length > 0 && (
          <span className="flex items-center gap-1">
            <MessageSquare className="w-3 h-3" />{task.comments.length}
          </span>
        )}
        {task.dueDate && (
          <span className={`flex items-center gap-1 ml-auto ${isOverdue ? 'text-red-400' : ''}`}>
            <Calendar className="w-3 h-3" />
            {new Date(task.dueDate).toLocaleDateString('en-GB', { day: '2-digit', month: 'short' })}
          </span>
        )}
      </div>
    </div>
  )
}

function formatRecurringNext(iso: string) {
  const d = new Date(iso), now = new Date()
  const diff = d.getTime() - now.getTime()
  if (diff < 0) return 'due'
  const mins = Math.round(diff / 60000)
  if (mins < 60) return `${mins}m`
  if (mins < 1440) return `${Math.round(mins / 60)}h`
  return `${Math.round(mins / 1440)}d`
}
