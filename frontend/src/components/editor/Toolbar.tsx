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

  const btnCls = "px-2.5 py-1.5 text-slate-500 hover:text-cyan-400 hover:bg-white/5 rounded transition-colors duration-150 dark:text-slate-500 dark:hover:text-cyan-400 dark:hover:bg-white/5"
  const divCls = "w-px h-5 bg-white/[0.07] mx-0.5"

  const viewToggle = onViewModeChange && (
    <div className="flex items-center bg-white/[0.04] border border-white/[0.07] rounded p-0.5">
      <button
        className={`px-2.5 py-1 rounded text-xs transition-colors duration-150 ${viewMode === 'editor' ? 'bg-cyan-500/15 text-cyan-400' : 'text-slate-500 hover:text-slate-300'}`}
        onClick={() => onViewModeChange('editor')}
        title="Editor Only"
      >
        <i className="fas fa-edit"></i>
      </button>
      <button
        className={`px-2.5 py-1 rounded text-xs transition-colors duration-150 ${viewMode === 'split' ? 'bg-cyan-500/15 text-cyan-400' : 'text-slate-500 hover:text-slate-300'}`}
        onClick={() => onViewModeChange('split')}
        title="Split View"
      >
        <i className="fas fa-columns"></i>
      </button>
      <button
        className={`px-2.5 py-1 rounded text-xs transition-colors duration-150 ${viewMode === 'preview' ? 'bg-cyan-500/15 text-cyan-400' : 'text-slate-500 hover:text-slate-300'}`}
        onClick={() => onViewModeChange('preview')}
        title="Preview Only"
      >
        <i className="fas fa-eye"></i>
      </button>
    </div>
  )

  return (
    <div className="bg-[#0a0b10] border-b border-[rgba(6,182,212,0.1)] dark:bg-[#0a0b10] flex flex-col md:flex-row">

      {/* ── Row 1: formatting (both mobile and desktop) ── */}
      <div className="flex items-center space-x-0.5 px-3 py-1.5">
        <button className={btnCls} onClick={() => onInsertMarkdown('bold')} title="Bold (Cmd+B)">
          <i className="fas fa-bold text-xs"></i>
        </button>
        <button className={btnCls} onClick={() => onInsertMarkdown('italic')} title="Italic (Cmd+I)">
          <i className="fas fa-italic text-xs"></i>
        </button>
        <button className={btnCls} onClick={() => onInsertMarkdown('heading')} title="Heading">
          <i className="fas fa-heading text-xs"></i>
        </button>

        <div className={divCls}></div>

        <button className={btnCls} onClick={() => onInsertMarkdown('link')} title="Link (Cmd+K)">
          <i className="fas fa-link text-xs"></i>
        </button>
        <button className={btnCls} onClick={() => onInsertMarkdown('code')} title="Code Block">
          <i className="fas fa-code text-xs"></i>
        </button>
        <button className={btnCls} onClick={() => onInsertMarkdown('image')} title="Image">
          <i className="fas fa-image text-xs"></i>
        </button>

        <div className={divCls}></div>

        {/* Table picker */}
        <div className="relative table-picker-container" ref={tablePickerRef}>
          <button className={btnCls} onClick={() => setShowTablePicker(!showTablePicker)} title="Insert Table">
            <i className="fas fa-table text-xs"></i>
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
                        style={{ backgroundColor: row < tableSize.rows && col < tableSize.cols ? '#3b82f6' : '#e5e7eb' }}
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

        <button className={btnCls} onClick={() => setShowMermaidModal(true)} title="Mermaid Diagram Editor">
          <i className="fas fa-project-diagram text-xs"></i>
        </button>

        <div className={divCls}></div>

        <button className={btnCls} onClick={() => onInsertMarkdown('upload')} title="Upload File">
          <i className="fas fa-upload text-xs"></i>
        </button>

        {/* On desktop: spacer + actions in same row */}
        <div className="flex-1 hidden md:block"></div>

        <div className="hidden md:flex items-center gap-1">
          {onToggleGraph && (
            <button
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors duration-150 text-xs font-mono"
              onClick={onToggleGraph}
              title="Graph View"
            >
              <i className="fas fa-project-diagram"></i>
              <span>Graph</span>
            </button>
          )}
          {onToggleChat && (
            <button
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-purple-500 hover:text-purple-300 hover:bg-purple-500/10 rounded transition-colors duration-150 text-xs font-mono mr-3"
              onClick={onToggleChat}
              title="Chat with Claude AI"
            >
              <i className="fas fa-robot"></i>
              <span>Chat</span>
            </button>
          )}
          {viewToggle}
          {currentNotePath && (
            <span className="ml-2 text-xs text-slate-600 font-mono truncate max-w-xs" title={currentNotePath}>
              {currentNotePath}
            </span>
          )}
        </div>
      </div>

      {/* ── Row 2 (mobile only): actions + view toggle ── */}
      <div className="flex md:hidden items-center px-3 py-1 border-t border-white/[0.04] gap-1">
        {onToggleGraph && (
          <button
            className="flex items-center gap-1.5 px-2.5 py-1.5 text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors duration-150 text-xs font-mono"
            onClick={onToggleGraph}
            title="Graph View"
          >
            <i className="fas fa-project-diagram"></i>
            <span>Graph</span>
          </button>
        )}
        {onToggleChat && (
          <button
            className="flex items-center gap-1.5 px-2.5 py-1.5 text-purple-500 hover:text-purple-300 hover:bg-purple-500/10 rounded transition-colors duration-150 text-xs font-mono"
            onClick={onToggleChat}
            title="Chat"
          >
            <i className="fas fa-robot"></i>
            <span>Chat</span>
          </button>
        )}
        <div className="flex-1"></div>
        {viewToggle}
      </div>

      <MermaidEditorModal
        visible={showMermaidModal}
        onClose={() => setShowMermaidModal(false)}
        onInsert={handleMermaidInsert}
      />
    </div>
  )
}

export default Toolbar
