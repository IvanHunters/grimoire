import { useRef, useState, useEffect } from 'react'
import type { ViewMode } from '../../types/ui'
import MermaidEditorModal from '../modals/MermaidEditorModal'

interface ToolbarProps {
  onInsertMarkdown: (type: string, value?: string) => void
  currentNotePath?: string
  onToggleGraph?: () => void
  onToggleChat?: () => void
  viewMode?: ViewMode
  onViewModeChange?: (mode: ViewMode) => void
}

/**
 * Editor Toolbar - ported from design-prototype.html
 *
 * Features:
 * - Markdown formatting buttons (Bold, Italic, Heading, Link, Image, Code)
 * - Table picker (10x10 grid)
 * - Mermaid editor
 * - Upload button
 * - Graph view toggle
 * - Chat toggle
 * - View mode toggle (inside toolbar as in prototype)
 */
function Toolbar({
  onInsertMarkdown,
  currentNotePath,
  onToggleGraph,
  onToggleChat,
  viewMode = 'split',
  onViewModeChange
}: ToolbarProps) {
  const [showTablePicker, setShowTablePicker] = useState(false)
  const [tableSize, setTableSize] = useState({ rows: 0, cols: 0 })
  const [showMermaidModal, setShowMermaidModal] = useState(false)
  const tablePickerRef = useRef<HTMLDivElement>(null)

  const handleTableCellHover = (row: number, col: number) => {
    setTableSize({ rows: row + 1, cols: col + 1 })
  }

  const handleMermaidInsert = (code: string) => {
    console.log('Toolbar - Received code:', code)
    console.log('Code length:', code.length)
    const markdown = `\n\`\`\`mermaid\n${code}\n\`\`\`\n\n`
    console.log('Toolbar - Generated markdown:', markdown)
    onInsertMarkdown('mermaid', markdown)
  }

  const handleTableCellClick = (rows: number, cols: number) => {
    // Generate table markdown (exactly like prototype)
    let table = '\n'

    // Header row
    table += '|'
    for (let c = 0; c < cols; c++) {
      table += ` Header ${c + 1} |`
    }
    table += '\n'

    // Separator row
    table += '|'
    for (let c = 0; c < cols; c++) {
      table += '----------|'
    }
    table += '\n'

    // Data rows (rows parameter = number of data rows, not including header)
    for (let r = 0; r < rows; r++) {
      table += '|'
      for (let c = 0; c < cols; c++) {
        table += ` Cell ${r + 1}-${c + 1} |`
      }
      table += '\n'
    }
    table += '\n'

    onInsertMarkdown('table', table)
    setShowTablePicker(false)
    setTableSize({ rows: 0, cols: 0 })
  }

  // Close table picker when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (tablePickerRef.current && !tablePickerRef.current.contains(e.target as Node)) {
        setShowTablePicker(false)
      }
    }

    if (showTablePicker) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [showTablePicker])

  return (
    <div className="bg-white border-b border-gray-200 p-3 flex items-center space-x-2">
      {/* Text formatting */}
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('bold')}
        title="Bold (Cmd+B)"
      >
        <i className="fas fa-bold"></i>
      </button>
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('italic')}
        title="Italic (Cmd+I)"
      >
        <i className="fas fa-italic"></i>
      </button>
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('heading')}
        title="Heading"
      >
        <i className="fas fa-heading"></i>
      </button>

      {/* Divider */}
      <div className="w-px h-6 bg-gray-300"></div>

      {/* Links & media */}
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('link')}
        title="Link (Cmd+K)"
      >
        <i className="fas fa-link"></i>
      </button>
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('image')}
        title="Image"
      >
        <i className="fas fa-image"></i>
      </button>
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('code')}
        title="Code Block"
      >
        <i className="fas fa-code"></i>
      </button>

      {/* Divider */}
      <div className="w-px h-6 bg-gray-300"></div>

      {/* Table picker */}
      <div className="relative table-picker-container" ref={tablePickerRef}>
        <button
          className="px-3 py-1 hover:bg-gray-100 rounded"
          onClick={() => setShowTablePicker(!showTablePicker)}
          title="Insert Table"
        >
          <i className="fas fa-table"></i>
        </button>
        {showTablePicker && (
          <div className="table-picker">
            <div className="table-picker-label">Select table size</div>
            <div className="table-grid">
              {Array.from({ length: 10 }, (_, row) => (
                <div key={row} style={{ display: 'flex' }}>
                  {Array.from({ length: 10 }, (_, col) => (
                    <div
                      key={`${row}-${col}`}
                      className="table-cell"
                      style={{
                        backgroundColor: row < tableSize.rows && col < tableSize.cols ? '#3b82f6' : '#e5e7eb',
                      }}
                      onMouseEnter={() => handleTableCellHover(row, col)}
                      onClick={() => handleTableCellClick(tableSize.rows, tableSize.cols)}
                    />
                  ))}
                </div>
              ))}
            </div>
            <div className="table-size-label">{tableSize.rows} x {tableSize.cols}</div>
          </div>
        )}
      </div>

      {/* Mermaid editor */}
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => setShowMermaidModal(true)}
        title="Mermaid Diagram Editor"
      >
        <i className="fas fa-project-diagram"></i>
      </button>

      {/* Divider */}
      <div className="w-px h-6 bg-gray-300"></div>

      {/* Upload */}
      <button
        className="px-3 py-1 hover:bg-gray-100 rounded"
        onClick={() => onInsertMarkdown('upload')}
        title="Upload File"
      >
        <i className="fas fa-upload"></i>
      </button>

      {/* Spacer */}
      <div className="flex-1"></div>

      {/* Graph view */}
      {onToggleGraph && (
        <button
          className="px-3 py-1 hover:bg-gray-100 rounded mr-2"
          onClick={onToggleGraph}
          title="Graph View - Note Connections"
        >
          <i className="fas fa-project-diagram" style={{ marginRight: 4 }}></i>
          <span className="text-sm">Graph</span>
        </button>
      )}

      {/* Chat */}
      {onToggleChat && (
        <button
          className="px-3 py-1 hover:bg-purple-100 rounded mr-4"
          onClick={onToggleChat}
          title="Chat with Claude AI"
        >
          <i className="fas fa-robot" style={{ marginRight: 4, color: '#8b5cf6' }}></i>
          <span className="text-sm" style={{ color: '#8b5cf6' }}>Chat</span>
        </button>
      )}

      {/* View mode toggle */}
      {onViewModeChange && (
        <div className="flex items-center bg-gray-100 rounded p-1 mr-4">
          <button
            className={`px-3 py-1 rounded text-sm ${viewMode === 'editor' ? 'bg-white shadow-sm' : ''}`}
            onClick={() => onViewModeChange('editor')}
            title="Editor Only"
          >
            <i className="fas fa-edit"></i>
          </button>
          <button
            className={`px-3 py-1 rounded text-sm ${viewMode === 'split' ? 'bg-white shadow-sm' : ''}`}
            onClick={() => onViewModeChange('split')}
            title="Split View"
          >
            <i className="fas fa-columns"></i>
          </button>
          <button
            className={`px-3 py-1 rounded text-sm ${viewMode === 'preview' ? 'bg-white shadow-sm' : ''}`}
            onClick={() => onViewModeChange('preview')}
            title="Preview Only"
          >
            <i className="fas fa-eye"></i>
          </button>
        </div>
      )}

      {/* Current note name */}
      {currentNotePath && (
        <span className="text-sm text-gray-500">{currentNotePath}</span>
      )}

      {/* Mermaid Editor Modal */}
      <MermaidEditorModal
        visible={showMermaidModal}
        onClose={() => setShowMermaidModal(false)}
        onInsert={handleMermaidInsert}
      />
    </div>
  )
}

export default Toolbar
