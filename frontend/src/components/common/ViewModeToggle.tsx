import { FileEdit, SplitSquareHorizontal, Eye } from 'lucide-react'
import type { ViewMode } from '../../types/ui'
import type { ReactNode } from 'react'

interface ViewModeToggleProps {
  viewMode: ViewMode
  onChange: (mode: ViewMode) => void
}

function ViewModeToggle({ viewMode, onChange }: ViewModeToggleProps) {
  const modes: Array<{ value: ViewMode; label: string; icon: ReactNode }> = [
    {
      value: 'editor',
      label: 'Editor',
      icon: <FileEdit className="w-4 h-4" />,
    },
    {
      value: 'split',
      label: 'Split',
      icon: <SplitSquareHorizontal className="w-4 h-4" />,
    },
    {
      value: 'preview',
      label: 'Preview',
      icon: <Eye className="w-4 h-4" />,
    },
  ]

  return (
    <div className="flex items-center gap-1 bg-gray-100 dark:bg-gray-700 p-1 rounded-lg">
      {modes.map((mode) => (
        <button
          key={mode.value}
          onClick={() => onChange(mode.value)}
          className={`flex items-center gap-2 px-3 py-1.5 rounded transition ${
            viewMode === mode.value
              ? 'bg-white dark:bg-gray-600 shadow text-purple-600 dark:text-purple-400'
              : 'text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white'
          }`}
          title={mode.label}
        >
          {mode.icon}
          <span className="text-sm font-medium">{mode.label}</span>
        </button>
      ))}
    </div>
  )
}

export default ViewModeToggle
