import { useState, useEffect } from 'react'
import { X } from 'lucide-react'

interface RenameModalProps {
  visible: boolean
  onClose: () => void
  type: 'note' | 'folder'
  currentName: string
  onRename: (newName: string) => void
}

function RenameModal({ visible, onClose, type, currentName, onRename }: RenameModalProps) {
  const [newName, setNewName] = useState(currentName)

  useEffect(() => {
    if (visible) {
      setNewName(currentName)
    }
  }, [visible, currentName])

  if (!visible) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!newName.trim()) return
    if (newName.trim() === currentName) {
      onClose()
      return
    }
    onRename(newName.trim())
    onClose()
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose()
  }

  const inputCls = "w-full px-3 py-2 bg-white/[0.04] border border-white/[0.09] rounded text-sm text-slate-200 font-mono placeholder-slate-600 focus:outline-none focus:border-cyan-500/50 focus:bg-white/[0.06] transition-colors"
  const labelCls = "block text-[10px] font-semibold tracking-widest text-slate-600 font-mono uppercase mb-1.5"

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-[2000]"
      onClick={handleOverlayClick}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-md mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
        style={{ boxShadow: '0 0 0 1px rgba(6,182,212,0.08), 0 25px 50px rgba(0,0,0,0.6)' }}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.06]">
          <span className="text-[10px] font-mono font-semibold tracking-widest text-cyan-500 uppercase">
            ● RENAME {type === 'note' ? 'NOTE' : 'FOLDER'}
          </span>
          <button
            onClick={onClose}
            className="p-1 text-slate-600 hover:text-slate-400 transition-colors rounded"
            type="button"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Body */}
        <form onSubmit={handleSubmit}>
          <div className="px-5 py-4 space-y-3">
            <div>
              <label className={labelCls}>New {type === 'note' ? 'note' : 'folder'} name</label>
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder={type === 'note' ? 'my-awesome-note' : 'my-folder'}
                className={inputCls}
                autoFocus
              />
            </div>
            <p className="text-[11px] font-mono text-slate-700">
              current: <span className="text-slate-600">{currentName}</span>
            </p>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-white/[0.06]">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-1.5 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors"
            >
              cancel
            </button>
            <button
              type="submit"
              disabled={!newName.trim()}
              className="px-4 py-1.5 text-xs font-mono bg-cyan-500/15 text-cyan-400 border border-cyan-500/25 rounded hover:bg-cyan-500/25 hover:border-cyan-500/40 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              rename →
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default RenameModal
