import { useState, useEffect, useCallback } from 'react'
import { X, Terminal, Trash2, RotateCcw, Database, AlertCircle } from 'lucide-react'
import { sessionsAPI, type SessionStats, type SessionMeta } from '../../api/sessions'

interface SessionManageModalProps {
  visible: boolean
  onClose: () => void
}

function fmtSize(mb: number): string {
  if (mb < 0.01) return '< 0.01 MB'
  if (mb < 1) return `${(mb * 1024).toFixed(0)} KB`
  return `${mb.toFixed(2)} MB`
}

function fmtSessionSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const h = Math.floor(diff / 3600000)
  if (h < 1) return 'just now'
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

export default function SessionManageModal({ visible, onClose }: SessionManageModalProps) {
  const [stats, setStats] = useState<SessionStats | null>(null)
  const [sessions, setSessions] = useState<SessionMeta[]>([])
  const [loading, setLoading] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [clearingId, setClearingId] = useState<string | null>(null)
  const [rotateResult, setRotateResult] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [s, all] = await Promise.all([sessionsAPI.getStats(), sessionsAPI.listAllSessions()])
      setStats(s)
      setSessions(all)
    } catch (e) {
      console.error('Failed to load session data', e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (visible) {
      setRotateResult(null)
      load()
    }
  }, [visible, load])

  const handleRotate = async () => {
    setRotating(true)
    setRotateResult(null)
    try {
      await Promise.all(sessions.map(s => sessionsAPI.clearHistory(s.id).catch(() => {})))
      setRotateResult(`Cleared history for ${sessions.length} session${sessions.length !== 1 ? 's' : ''}`)
      load()
    } catch {
      setRotateResult('Failed to clear sessions')
    } finally {
      setRotating(false)
    }
  }

  const handleClearHistory = async (id: string) => {
    setClearingId(id)
    try {
      await sessionsAPI.clearHistory(id)
      setSessions(prev => prev.map(s => s.id === id ? { ...s, messageCount: 0, sizeBytes: 0 } : s))
      setStats(prev => prev ? {
        ...prev,
        totalMessages: Math.max(0, prev.totalMessages - (sessions.find(s => s.id === id)?.messageCount ?? 0)),
      } : prev)
    } catch {
      console.error('Failed to clear session history')
    } finally {
      setClearingId(null)
    }
  }

  if (!visible) return null

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-[2000]"
      onClick={e => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-xl mx-4 overflow-hidden flex flex-col"
        style={{ maxHeight: '80vh', boxShadow: '0 0 0 1px rgba(168,85,247,0.08), 0 25px 50px rgba(0,0,0,0.6)' }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.06] flex-shrink-0">
          <div className="flex items-center gap-2">
            <Terminal className="w-3.5 h-3.5 text-purple-500" />
            <span className="text-[10px] font-mono font-semibold tracking-widest text-purple-500 uppercase">
              Session History
            </span>
          </div>
          <button onClick={onClose} className="p-1 text-slate-600 hover:text-slate-400 transition-colors rounded">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Stats bar */}
        <div className="px-5 py-3 border-b border-white/[0.06] flex-shrink-0">
          {loading && !stats ? (
            <div className="text-[11px] font-mono text-slate-600">Loading...</div>
          ) : stats ? (
            <div className="flex items-center gap-6">
              <div className="flex items-center gap-1.5">
                <Database className="w-3 h-3 text-slate-600" />
                <span className="text-[11px] font-mono text-slate-500">
                  <span className="text-slate-300">{stats.totalSessions}</span> sessions
                </span>
              </div>
              <div className="text-[11px] font-mono text-slate-500">
                <span className="text-slate-300">{stats.totalMessages}</span> messages
              </div>
              <div className="text-[11px] font-mono text-slate-500">
                <span className="text-slate-300">{fmtSize(stats.totalSizeMb)}</span> stored
              </div>
              <div className="text-[11px] font-mono text-slate-700">
                {stats.activeSessions} active
              </div>
            </div>
          ) : null}
        </div>

        {/* Rotate action */}
        <div className="px-5 py-3 border-b border-white/[0.06] flex items-center gap-3 flex-shrink-0">
          <button
            onClick={handleRotate}
            disabled={rotating}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono bg-purple-500/10 text-purple-400 border border-purple-500/20 rounded hover:bg-purple-500/20 hover:border-purple-500/35 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <RotateCcw className={`w-3 h-3 ${rotating ? 'animate-spin' : ''}`} />
            Clear All
          </button>
          {rotateResult && (
            <span className="text-[11px] font-mono text-slate-500">{rotateResult}</span>
          )}
        </div>

        {/* Session list */}
        <div className="overflow-y-auto flex-1 px-2 py-2">
          {loading && sessions.length === 0 ? (
            <div className="text-[11px] font-mono text-slate-700 px-3 py-4 text-center">Loading sessions...</div>
          ) : sessions.length === 0 ? (
            <div className="text-[11px] font-mono text-slate-700 px-3 py-4 text-center">No sessions found</div>
          ) : (
            <div className="space-y-0.5">
              {sessions.map(s => (
                <div
                  key={s.id}
                  className="flex items-center gap-2 px-3 py-2 rounded hover:bg-white/[0.02] group"
                >
                  <Terminal className="w-3 h-3 flex-shrink-0 text-purple-700" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-mono text-slate-400 truncate">{s.name || 'Session'}</span>
                      <span className={`text-[9px] font-mono px-1 rounded ${
                        s.status === 'active'
                          ? 'bg-green-500/10 text-green-600'
                          : 'bg-white/5 text-slate-700'
                      }`}>{s.status}</span>
                    </div>
                    <div className="flex items-center gap-3 mt-0.5">
                      <span className="text-[10px] font-mono text-slate-700">{s.messageCount} msgs</span>
                      <span className="text-[10px] font-mono text-slate-700">{fmtSessionSize(s.sizeBytes)}</span>
                      <span className="text-[10px] font-mono text-slate-700">{timeAgo(s.lastActivity)}</span>
                    </div>
                  </div>
                  {s.messageCount > 0 && (
                    <button
                      onClick={() => handleClearHistory(s.id)}
                      disabled={clearingId === s.id}
                      title="Clear message history"
                      className="opacity-0 group-hover:opacity-100 flex-shrink-0 p-1 text-slate-700 hover:text-red-400 transition-all disabled:opacity-30"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  )}
                  {s.messageCount === 0 && (
                    <span className="opacity-0 group-hover:opacity-100 flex-shrink-0 text-[9px] font-mono text-slate-800 px-1">empty</span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-5 py-3 border-t border-white/[0.06] flex-shrink-0">
          <div className="flex items-center gap-1.5 text-[10px] font-mono text-slate-700">
            <AlertCircle className="w-3 h-3" />
            clearing history does not affect active sessions
          </div>
          <button
            onClick={onClose}
            className="px-4 py-1.5 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors"
          >
            close
          </button>
        </div>
      </div>
    </div>
  )
}
