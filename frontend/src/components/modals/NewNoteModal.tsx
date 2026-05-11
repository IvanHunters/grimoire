import { useState, useEffect } from 'react'
import { X, Plus } from 'lucide-react'

interface NewNoteModalProps {
  visible: boolean
  onClose: () => void
  folders: Array<{ path: string; name: string }>
  defaultFolder?: string
  onCreate: (name: string, folder: string) => void
}

function NewNoteModal({ visible, onClose, folders, defaultFolder = '', onCreate }: NewNoteModalProps) {
  const [noteName, setNoteName] = useState('')
  const [selectedFolder, setSelectedFolder] = useState(defaultFolder)
  const [showNewFolder, setShowNewFolder] = useState(false)
  const [newFolderPath, setNewFolderPath] = useState('')

  useEffect(() => {
    if (visible) {
      setSelectedFolder(defaultFolder || '')
    }
  }, [visible, defaultFolder])

  if (!visible) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!noteName.trim()) return

    const targetFolder = showNewFolder && newFolderPath.trim()
      ? newFolderPath.trim()
      : selectedFolder

    onCreate(noteName.trim(), targetFolder)
    setNoteName('')
    setSelectedFolder('')
    setShowNewFolder(false)
    setNewFolderPath('')
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
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono font-semibold tracking-widest text-cyan-500 uppercase">
              ● NEW NOTE
            </span>
          </div>
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
          <div className="px-5 py-4 space-y-4">
            <div>
              <label className={labelCls}>Note name</label>
              <input
                type="text"
                value={noteName}
                onChange={(e) => setNoteName(e.target.value)}
                placeholder="my-awesome-note"
                className={inputCls}
                autoFocus
              />
            </div>

            {!showNewFolder && (
              <div>
                <label className={labelCls}>Folder</label>
                <select
                  value={selectedFolder}
                  onChange={(e) => setSelectedFolder(e.target.value)}
                  className={inputCls}
                >
                  <option value="">/ root</option>
                  {folders.map((folder) => (
                    <option key={folder.path} value={folder.path}>
                      {folder.path.includes('/')
                        ? `  ${folder.path.replace('/', ' / ')}`
                        : folder.path
                      }
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  onClick={() => setShowNewFolder(true)}
                  className="mt-2 text-[11px] font-mono text-cyan-600 hover:text-cyan-400 flex items-center gap-1 transition-colors"
                >
                  <Plus className="w-3 h-3" />
                  new folder
                </button>
              </div>
            )}

            {showNewFolder && (
              <div>
                <label className={labelCls}>New folder path</label>
                <input
                  type="text"
                  value={newFolderPath}
                  onChange={(e) => setNewFolderPath(e.target.value)}
                  placeholder="projects/new-folder"
                  className={inputCls}
                />
                <button
                  type="button"
                  onClick={() => { setShowNewFolder(false); setNewFolderPath('') }}
                  className="mt-2 text-[11px] font-mono text-slate-600 hover:text-slate-400 transition-colors"
                >
                  cancel
                </button>
              </div>
            )}
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
              disabled={!noteName.trim()}
              className="px-4 py-1.5 text-xs font-mono bg-cyan-500/15 text-cyan-400 border border-cyan-500/25 rounded hover:bg-cyan-500/25 hover:border-cyan-500/40 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              create →
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default NewNoteModal
