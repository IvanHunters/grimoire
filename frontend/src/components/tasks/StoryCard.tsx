import { ChevronDown, ChevronRight, Plus, Calendar, MessageSquare } from 'lucide-react'
import type { Task } from '../../types/task'
import { PRIORITY_CONFIG } from '../../types/task'
import { TaskCard } from './TaskCard'

interface StoryCardProps {
  story: Task
  allChildren: Task[]      // across all columns — for progress bar
  columnChildren: Task[]   // in same column — shown below
  collapsed: boolean
  onToggle: () => void
  onClick: () => void
  onChildClick: (task: Task) => void
  onAddChild: () => void
  isDragging: boolean
  draggable: boolean
  onDragStart: (e: React.DragEvent) => void
  onDragEnd: () => void
  creatingChild?: boolean
  childInput?: React.ReactNode
  isStory?: boolean        // explicit story type vs auto-grouped parent
  draggingTaskId?: string | null
  onChildDragStart?: (e: React.DragEvent, taskId: string) => void
  onChildDragEnd?: () => void
}

export function StoryCard({
  story, allChildren, columnChildren, collapsed, onToggle, onClick,
  onChildClick, onAddChild, isDragging, draggable, onDragStart, onDragEnd,
  creatingChild, childInput, isStory = false,
  draggingTaskId, onChildDragStart, onChildDragEnd,
}: StoryCardProps) {
  const priority = PRIORITY_CONFIG[story.priority] ?? { label: story.priority, color: 'text-slate-400' }
  const total = allChildren.length
  const done = allChildren.filter(t => t.status === 'done').length
  const progress = total > 0 ? Math.round((done / total) * 100) : 0
  const isOverdue = story.dueDate && new Date(story.dueDate) < new Date() && story.status !== 'done'

  return (
    <div
      draggable={draggable}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      className="rounded-xl overflow-hidden transition-all duration-150"
      style={{
        opacity: isDragging ? 0.4 : 1,
        background: isStory ? 'rgba(245,158,11,0.03)' : 'rgba(255,255,255,0.02)',
        border: isStory ? '1px solid rgba(245,158,11,0.12)' : '1px solid rgba(255,255,255,0.07)',
        borderLeftWidth: '2px',
        borderLeftColor: isStory ? 'rgba(245,158,11,0.5)' : 'rgba(100,116,139,0.4)',
      }}
    >
      {/* Story header */}
      <div
        onClick={onClick}
        className={`p-3 cursor-pointer transition-colors ${isStory ? 'hover:bg-amber-500/[0.04]' : 'hover:bg-white/[0.03]'}`}
      >
        {/* Type + priority row */}
        <div className="flex items-center gap-2 mb-1.5">
          {isStory ? (
            <span className="text-[9px] font-mono font-bold tracking-widest uppercase px-1.5 py-0.5 rounded"
              style={{ color: 'rgba(245,158,11,0.9)', background: 'rgba(245,158,11,0.1)' }}>
              story
            </span>
          ) : (
            <span className="text-[9px] font-mono font-bold tracking-widest uppercase px-1.5 py-0.5 rounded"
              style={{ color: 'rgba(148,163,184,0.7)', background: 'rgba(148,163,184,0.07)' }}>
              group
            </span>
          )}
          <span className={`text-[10px] font-mono font-semibold ${priority.color}`}>
            {priority.label.toUpperCase()}
          </span>
          {story.dueDate && (
            <span className={`flex items-center gap-1 ml-auto text-[10px] font-mono ${isOverdue ? 'text-red-400' : 'text-slate-700'}`}>
              <Calendar className="w-3 h-3" />
              {new Date(story.dueDate).toLocaleDateString('en-GB', { day: '2-digit', month: 'short' })}
            </span>
          )}
        </div>

        {/* Title */}
        <p className="text-sm font-mono text-slate-100 leading-snug mb-2">{story.title}</p>

        {/* Progress bar */}
        {total > 0 && (
          <div className="mb-2">
            <div className="h-1 rounded-full overflow-hidden" style={{ background: 'rgba(255,255,255,0.06)' }}>
              <div
                className="h-full rounded-full transition-all duration-500"
                style={{ width: `${progress}%`, background: progress === 100 ? '#22c55e' : 'rgba(245,158,11,0.7)' }}
              />
            </div>
            <div className="flex items-center justify-between mt-1">
              <span className="text-[9px] font-mono text-slate-700">{done}/{total} done</span>
              {progress === 100 && <span className="text-[9px] font-mono text-green-500/70">complete</span>}
            </div>
          </div>
        )}

        {/* Tags */}
        {story.tags && story.tags.length > 0 && (
          <div className="flex flex-wrap gap-1 mb-1.5">
            {story.tags.slice(0, 3).map(tag => (
              <span key={tag} className="text-[10px] font-mono px-1.5 py-0.5 rounded"
                style={{ background: 'rgba(245,158,11,0.08)', color: 'rgba(245,158,11,0.6)' }}>
                {tag}
              </span>
            ))}
          </div>
        )}

        {/* Meta */}
        {(story.comments?.length ?? 0) > 0 && (
          <div className="flex items-center gap-1 text-[10px] font-mono text-slate-700">
            <MessageSquare className="w-3 h-3" />{story.comments!.length}
          </div>
        )}
      </div>

      {/* Children section */}
      <div style={{ borderTop: isStory ? '1px solid rgba(245,158,11,0.08)' : '1px solid rgba(255,255,255,0.05)' }}>
        {/* Expand toggle */}
        <button
          onClick={onToggle}
          className={`w-full flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-mono transition-colors ${isStory ? 'hover:bg-amber-500/[0.05]' : 'hover:bg-white/[0.03]'}`}
          style={{ color: isStory ? 'rgba(245,158,11,0.5)' : 'rgba(100,116,139,0.5)' }}
        >
          {collapsed
            ? <ChevronRight className="w-3 h-3" />
            : <ChevronDown className="w-3 h-3" />}
          {columnChildren.length} task{columnChildren.length !== 1 ? 's' : ''} in column
          {allChildren.length > columnChildren.length && (
            <span className="ml-1 text-slate-700">({allChildren.length} total)</span>
          )}
        </button>

        {/* Child tasks */}
        {!collapsed && (
          <div className="px-2 pb-2 space-y-1.5"
            style={{ borderLeft: isStory ? '2px solid rgba(245,158,11,0.15)' : '2px solid rgba(100,116,139,0.15)', marginLeft: '10px' }}>
            {columnChildren.map(child => (
              <div
                key={child.id}
                draggable
                onDragStart={e => onChildDragStart?.(e, child.id)}
                onDragEnd={() => onChildDragEnd?.()}
                className={`transition-opacity duration-150 ${draggingTaskId === child.id ? 'opacity-40' : 'opacity-100'}`}
              >
                <TaskCard
                  task={child}
                  onClick={() => onChildClick(child)}
                  isChild
                />
              </div>
            ))}
            {creatingChild && childInput}
            {!creatingChild && (
              <button
                onClick={e => { e.stopPropagation(); onAddChild() }}
                className="w-full flex items-center gap-1.5 px-2 py-1 text-[10px] font-mono transition-colors rounded"
                style={{ color: 'rgba(245,158,11,0.4)' }}
              >
                <Plus className="w-3 h-3" />Add subtask
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
