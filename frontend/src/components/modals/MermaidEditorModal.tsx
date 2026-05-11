import { useState, useEffect } from 'react'
import MermaidDiagram from '../preview/MermaidDiagram'

interface MermaidEditorModalProps {
  visible: boolean
  onClose: () => void
  onInsert: (code: string) => void
}

const templates = {
  flowchart: `graph TD
    A[Start] --> B[Process]
    B --> C{Decision}
    C -->|Yes| D[Result 1]
    C -->|No| E[Result 2]`,
  sequence: `sequenceDiagram
    participant Alice
    participant Bob
    Alice->>Bob: Hello Bob!
    Bob->>Alice: Hello Alice!`,
  gantt: `gantt
    title Project Timeline
    dateFormat YYYY-MM-DD
    section Planning
    Task 1           :2024-01-01, 7d
    Task 2           :2024-01-08, 5d
    section Development
    Task 3           :2024-01-13, 10d`,
  pie: `pie title Distribution
    "Category A" : 45
    "Category B" : 30
    "Category C" : 25`
}

function MermaidEditorModal({ visible, onClose, onInsert }: MermaidEditorModalProps) {
  const [code, setCode] = useState(templates.flowchart)
  const [renderKey, setRenderKey] = useState(0)

  // Live update with debounce
  useEffect(() => {
    const timer = setTimeout(() => {
      setRenderKey(prev => prev + 1)
    }, 500) // 500ms debounce

    return () => clearTimeout(timer)
  }, [code])

  const handleInsert = () => {
    onInsert(code)
    // Reset for next time
    setCode(templates.flowchart)
    setRenderKey(prev => prev + 1)
    onClose()
  }

  const handleClose = () => {
    // Reset code when closing without inserting
    setCode(templates.flowchart)
    setRenderKey(prev => prev + 1)
    onClose()
  }

  const handleTemplateSelect = (template: keyof typeof templates) => {
    setCode(templates[template])
    setRenderKey(prev => prev + 1)
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      handleClose()
    }
  }

  // Close on Escape
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && visible) {
        handleClose()
      }
    }

    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [visible])

  if (!visible) return null

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      onClick={handleOverlayClick}
    >
      <div
        className="bg-white dark:bg-gray-800 rounded-lg shadow-xl"
        style={{ maxWidth: '900px', width: '90%', maxHeight: '85vh' }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Mermaid Diagram Editor</h2>
          <button
            onClick={handleClose}
            className="p-2 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition"
            title="Close"
          >
            <i className="fas fa-times text-gray-600 dark:text-gray-400"></i>
          </button>
        </div>

        {/* Body */}
        <div className="flex" style={{ height: '500px' }}>
          {/* Editor side */}
          <div className="flex-1 flex flex-col border-r border-gray-200 dark:border-gray-700">
            <div className="px-4 py-3 bg-gray-50 dark:bg-gray-700 border-b border-gray-200 dark:border-gray-600">
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">Mermaid Code</label>
            </div>

            <textarea
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="flex-1 p-4 border-0 font-mono text-sm resize-none outline-none bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100"
              style={{ fontFamily: "'Monaco', 'Menlo', 'Courier New', monospace" }}
            />

            {/* Quick templates */}
            <div className="px-4 py-3 bg-gray-50 dark:bg-gray-700 border-t border-gray-200 dark:border-gray-600">
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 block">Quick Templates:</label>
              <div className="flex gap-2 flex-wrap">
                <button
                  onClick={() => handleTemplateSelect('flowchart')}
                  className="px-3 py-1 text-xs bg-white dark:bg-gray-600 border border-gray-300 dark:border-gray-500 rounded hover:bg-gray-50 dark:hover:bg-gray-500 text-gray-700 dark:text-gray-200 transition"
                >
                  Flowchart
                </button>
                <button
                  onClick={() => handleTemplateSelect('sequence')}
                  className="px-3 py-1 text-xs bg-white dark:bg-gray-600 border border-gray-300 dark:border-gray-500 rounded hover:bg-gray-50 dark:hover:bg-gray-500 text-gray-700 dark:text-gray-200 transition"
                >
                  Sequence
                </button>
                <button
                  onClick={() => handleTemplateSelect('gantt')}
                  className="px-3 py-1 text-xs bg-white dark:bg-gray-600 border border-gray-300 dark:border-gray-500 rounded hover:bg-gray-50 dark:hover:bg-gray-500 text-gray-700 dark:text-gray-200 transition"
                >
                  Gantt
                </button>
                <button
                  onClick={() => handleTemplateSelect('pie')}
                  className="px-3 py-1 text-xs bg-white dark:bg-gray-600 border border-gray-300 dark:border-gray-500 rounded hover:bg-gray-50 dark:hover:bg-gray-500 text-gray-700 dark:text-gray-200 transition"
                >
                  Pie Chart
                </button>
              </div>
            </div>
          </div>

          {/* Preview side */}
          <div className="flex-1 flex flex-col">
            <div className="px-4 py-3 bg-gray-50 dark:bg-gray-700 border-b border-gray-200 dark:border-gray-600">
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300">Live Preview</label>
            </div>
            <div className="flex-1 p-6 overflow-auto flex items-center justify-center bg-white dark:bg-gray-800">
              <MermaidDiagram key={renderKey} code={code} />
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700">
          <button
            onClick={handleClose}
            className="px-4 py-2 text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded hover:bg-gray-50 dark:hover:bg-gray-600 transition"
          >
            Cancel
          </button>
          <button
            onClick={handleInsert}
            className="px-4 py-2 text-white bg-purple-600 rounded hover:bg-purple-700 transition"
          >
            Insert Diagram
          </button>
        </div>
      </div>
    </div>
  )
}

export default MermaidEditorModal
