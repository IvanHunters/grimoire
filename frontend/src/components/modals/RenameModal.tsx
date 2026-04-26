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

  // Update name when modal opens with new item
  useEffect(() => {
    if (visible) {
      setNewName(currentName)
    }
  }, [visible, currentName])

  if (!visible) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!newName.trim()) {
      alert('Please enter a name')
      return
    }

    if (newName.trim() === currentName) {
      onClose()
      return
    }

    onRename(newName.trim())
    onClose()
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-[2000]"
      onClick={handleOverlayClick}
    >
      <div
        className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200">
          <h2 className="text-lg font-semibold text-gray-900">
            Rename {type === 'note' ? 'Note' : 'Folder'}
          </h2>
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-100 rounded transition"
            type="button"
          >
            <X className="w-5 h-5 text-gray-600" />
          </button>
        </div>

        {/* Body */}
        <form onSubmit={handleSubmit}>
          <div className="p-4">
            <label className="block text-sm font-medium text-gray-700 mb-1">
              {type === 'note' ? 'Note Name' : 'Folder Name'}
            </label>
            <input
              type="text"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={type === 'note' ? 'my-awesome-note' : 'my-folder'}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              autoFocus
            />
            <p className="mt-2 text-xs text-gray-500">
              Current: <span className="font-mono">{currentName}</span>
            </p>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-end gap-2 p-4 border-t border-gray-200">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-gray-700 hover:bg-gray-100 rounded-lg transition"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition"
            >
              Rename
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default RenameModal
