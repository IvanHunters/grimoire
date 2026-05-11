import { useState, useEffect } from 'react'
import { X, FolderOpen } from 'lucide-react'

interface FolderProjectPathModalProps {
  visible: boolean
  folderPath: string
  currentProjectPath?: string
  onClose: () => void
  onSave: (projectPath: string) => void
}

function FolderProjectPathModal({
  visible,
  folderPath,
  currentProjectPath = '',
  onClose,
  onSave,
}: FolderProjectPathModalProps) {
  const [projectPath, setProjectPath] = useState(currentProjectPath)

  useEffect(() => {
    setProjectPath(currentProjectPath)
  }, [currentProjectPath, visible])

  if (!visible) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSave(projectPath)
    onClose()
  }

  const handleClear = () => {
    setProjectPath('')
    onSave('')
    onClose()
  }

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 p-6">
        {/* Header */}
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <FolderOpen className="w-5 h-5 text-purple-600 dark:text-purple-400" />
            <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Set Project Path</h2>
          </div>
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition"
          >
            <X className="w-5 h-5 text-gray-500 dark:text-gray-400" />
          </button>
        </div>

        {/* Folder path */}
        <div className="mb-4 p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
          <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Folder:</div>
          <div className="text-sm font-mono text-gray-900 dark:text-white">{folderPath}</div>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit}>
          <div className="mb-4">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Working Directory for Claude Terminal
            </label>
            <input
              type="text"
              value={projectPath}
              onChange={(e) => setProjectPath(e.target.value)}
              placeholder="/path/to/your/project или ~/projects/my-app"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500"
              autoFocus
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
              Все заметки в этой папке и вложенных папках будут наследовать этот путь.
              Можно переопределить для конкретной заметки.
            </p>
          </div>

          {/* Actions */}
          <div className="flex gap-2 justify-end">
            {currentProjectPath && (
              <button
                type="button"
                onClick={handleClear}
                className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition"
              >
                Clear
              </button>
            )}
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="px-4 py-2 text-sm bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition"
            >
              Save
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default FolderProjectPathModal
