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

  // Update selected folder when modal opens with new defaultFolder
  useEffect(() => {
    if (visible) {
      setSelectedFolder(defaultFolder || '')
    }
  }, [visible, defaultFolder])

  if (!visible) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!noteName.trim()) {
      alert('Please enter note name')
      return
    }

    // If creating new folder, use that path
    const targetFolder = showNewFolder && newFolderPath.trim()
      ? newFolderPath.trim()
      : selectedFolder

    onCreate(noteName.trim(), targetFolder)

    // Reset form
    setNoteName('')
    setSelectedFolder('')
    setShowNewFolder(false)
    setNewFolderPath('')
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
          <h2 className="text-lg font-semibold text-gray-900">Create New Note</h2>
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
          <div className="p-4 space-y-4">
            {/* Note name */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Note Name
              </label>
              <input
                type="text"
                value={noteName}
                onChange={(e) => setNoteName(e.target.value)}
                placeholder="my-awesome-note"
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                autoFocus
              />
            </div>

            {/* Folder select */}
            {!showNewFolder && (
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Folder
                </label>
                <select
                  value={selectedFolder}
                  onChange={(e) => setSelectedFolder(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                >
                  <option value="">📁 Root (no folder)</option>
                  {folders.map((folder) => (
                    <option key={folder.path} value={folder.path}>
                      {folder.path.includes('/')
                        ? `\u00A0\u00A0\u00A0📁 ${folder.path.replace('/', ' / ')}`
                        : `📁 ${folder.name}`
                      }
                    </option>
                  ))}
                </select>

                {/* Create new folder button */}
                <button
                  type="button"
                  onClick={() => setShowNewFolder(true)}
                  className="mt-2 text-sm text-purple-600 hover:text-purple-700 flex items-center gap-1"
                >
                  <Plus className="w-4 h-4" />
                  Create New Folder
                </button>
              </div>
            )}

            {/* New folder input */}
            {showNewFolder && (
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  New Folder Path
                </label>
                <input
                  type="text"
                  value={newFolderPath}
                  onChange={(e) => setNewFolderPath(e.target.value)}
                  placeholder="projects/new-folder"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                />
                <button
                  type="button"
                  onClick={() => {
                    setShowNewFolder(false)
                    setNewFolderPath('')
                  }}
                  className="mt-2 text-sm text-gray-600 hover:text-gray-700"
                >
                  Cancel new folder
                </button>
              </div>
            )}
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
              Create Note
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default NewNoteModal
