import { useState, useEffect } from 'react'
import { ChevronDown, ChevronRight, FolderOpen } from 'lucide-react'

interface NoteSettingsProps {
  projectPath?: string
  onProjectPathChange: (path: string) => void
}

function NoteSettings({ projectPath = '', onProjectPathChange }: NoteSettingsProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [localPath, setLocalPath] = useState(projectPath)

  // Sync localPath when projectPath changes (e.g., switching notes)
  useEffect(() => {
    setLocalPath(projectPath)
  }, [projectPath])

  const handleSave = () => {
    onProjectPathChange(localPath)
  }

  const handleClear = () => {
    setLocalPath('')
    onProjectPathChange('')
  }

  return (
    <div className="border-b border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      {/* Header */}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center gap-2 px-4 py-2 hover:bg-gray-50 transition text-left dark:hover:bg-gray-700"
      >
        {isExpanded ? (
          <ChevronDown className="w-4 h-4 text-gray-500 dark:text-gray-400" />
        ) : (
          <ChevronRight className="w-4 h-4 text-gray-500 dark:text-gray-400" />
        )}
        <FolderOpen className="w-4 h-4 text-purple-600 dark:text-purple-400" />
        <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
          Project Settings
        </span>
        {projectPath && !isExpanded && (
          <span className="text-xs text-gray-500 truncate ml-auto max-w-md dark:text-gray-400">
            {projectPath}
          </span>
        )}
      </button>

      {/* Content */}
      {isExpanded && (
        <div className="px-4 pb-3 space-y-3">
          <div>
            <label className="block text-xs font-medium text-gray-600 mb-1 dark:text-gray-400">
              Working Directory for Claude Terminal
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={localPath}
                onChange={(e) => setLocalPath(e.target.value)}
                placeholder="/path/to/your/project или ~/projects/my-app"
                className="flex-1 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent dark:bg-gray-700 dark:border-gray-600 dark:text-gray-100 dark:placeholder-gray-400"
              />
              <button
                onClick={handleSave}
                disabled={localPath === projectPath}
                className="px-3 py-1.5 text-sm bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition disabled:opacity-50 disabled:cursor-not-allowed dark:bg-purple-500 dark:hover:bg-purple-600"
              >
                Save
              </button>
              {projectPath && (
                <button
                  onClick={handleClear}
                  className="px-3 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 transition dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
                >
                  Clear
                </button>
              )}
            </div>
            <p className="text-xs text-gray-500 mt-1 dark:text-gray-400">
              Если указан путь, Claude терминал откроется в этой директории. Иначе - во временной папке.
            </p>
          </div>
        </div>
      )}
    </div>
  )
}

export default NoteSettings
