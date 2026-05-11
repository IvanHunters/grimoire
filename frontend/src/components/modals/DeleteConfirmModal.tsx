import { X } from 'lucide-react'

interface DeleteConfirmModalProps {
  visible: boolean
  onClose: () => void
  type: 'note' | 'folder'
  itemName: string
  onConfirm: () => void
}

function DeleteConfirmModal({ visible, onClose, type, itemName, onConfirm }: DeleteConfirmModalProps) {
  if (!visible) return null

  const handleConfirm = () => {
    onConfirm()
    onClose()
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose()
  }

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-[2000]"
      onClick={handleOverlayClick}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-md mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
        style={{ boxShadow: '0 0 0 1px rgba(239,68,68,0.1), 0 25px 50px rgba(0,0,0,0.6)' }}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.06]">
          <span className="text-[10px] font-mono font-semibold tracking-widest text-red-500 uppercase">
            ⚠ DELETE {type === 'note' ? 'NOTE' : 'FOLDER'}
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
        <div className="px-5 py-5 space-y-3">
          <p className="text-sm text-slate-300 font-mono">
            delete <span className="text-slate-100 font-semibold">{itemName}</span>?
          </p>
          {type === 'folder' && (
            <p className="text-xs font-mono text-red-400/80 bg-red-500/[0.06] border border-red-500/10 rounded px-3 py-2">
              all notes inside this folder will also be deleted
            </p>
          )}
          <p className="text-[11px] font-mono text-slate-700">
            this action cannot be undone
          </p>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-white/[0.06]">
          <button
            onClick={onClose}
            className="px-4 py-1.5 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors"
          >
            cancel
          </button>
          <button
            onClick={handleConfirm}
            className="px-4 py-1.5 text-xs font-mono bg-red-500/15 text-red-400 border border-red-500/25 rounded hover:bg-red-500/25 hover:border-red-500/40 transition-colors"
          >
            delete →
          </button>
        </div>
      </div>
    </div>
  )
}

export default DeleteConfirmModal
