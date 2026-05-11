import { useState, useRef, useEffect } from 'react'
import { Search, ChevronDown, Menu, X as XIcon, Kanban } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useNotes } from '../../contexts/NotesContext'
import NewNoteModal from '../modals/NewNoteModal'
import SearchModal from '../modals/SearchModal'
import { exportToPDF, exportToWord, exportAllNotesToZip, exportToHTML } from '../../utils/export'

interface HeaderProps {
  onNoteSelect?: (note: any) => void
  previewRef?: React.RefObject<HTMLDivElement | null>
  onToggleMobileSidebar?: () => void
  mobileSidebarOpen?: boolean
}

function Header({ onNoteSelect, previewRef, onToggleMobileSidebar, mobileSidebarOpen }: HeaderProps) {
  const { folders, createNote, notes, currentNote } = useNotes()
  const navigate = useNavigate()
  const [showNewNoteModal, setShowNewNoteModal] = useState(false)
  const [showSearchModal, setShowSearchModal] = useState(false)
  const [showExportMenu, setShowExportMenu] = useState(false)
  const exportMenuRef = useRef<HTMLDivElement>(null)

  const handleNewNote = () => setShowNewNoteModal(true)

  const handleCreateNote = async (name: string, folder: string) => {
    try {
      const newNote = await createNote(name, folder)
      onNoteSelect?.(newNote)
      setShowNewNoteModal(false)
    } catch (error) {
      console.error('Failed to create note:', error)
    }
  }

  const handleSearch = () => setShowSearchModal(true)

  const handleSearchNoteSelect = (noteId: string) => {
    const note = notes.find(n => n.id === noteId)
    if (note) onNoteSelect?.(note)
  }

  const handleExport = async (format: 'md' | 'pdf' | 'docx' | 'zip') => {
    setShowExportMenu(false)

    if (!currentNote && format !== 'zip') {
      alert('Please select a note to export')
      return
    }

    try {
      switch (format) {
        case 'md':
          exportMarkdown()
          break
        case 'pdf':
          if (!previewRef?.current) {
            alert('Preview not available. Please switch to split or preview mode first.')
            return
          }
          await exportToPDF(currentNote!, previewRef.current)
          break
        case 'html':
          if (!previewRef?.current) {
            alert('Preview not available. Please switch to split or preview mode first.')
            return
          }
          await exportToHTML(currentNote!, previewRef.current)
          break
        case 'docx':
          await exportToWord(currentNote!)
          break
        case 'zip':
          await exportAllNotesToZip(notes)
          break
      }
    } catch (error) {
      console.error('Export failed:', error)
      alert(`Export failed: ${error instanceof Error ? error.message : 'Unknown error'}`)
    }
  }

  const exportMarkdown = () => {
    if (!currentNote) return
    const filename = `${currentNote.title.replace(/[^a-z0-9]/gi, '-').toLowerCase()}.md`
    const blob = new Blob([currentNote.content], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (exportMenuRef.current && !exportMenuRef.current.contains(e.target as Node)) {
        setShowExportMenu(false)
      }
    }
    if (showExportMenu) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [showExportMenu])

  const menuItemCls = "w-full flex items-center gap-3 px-4 py-2 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/[0.03] transition-colors text-left"

  return (
    <>
      <header
        className="h-14 flex items-center justify-between px-5 relative z-50 flex-shrink-0"
        style={{
          background: '#0a0b10',
          borderBottom: '1px solid rgba(6,182,212,0.1)',
        }}
      >
        {/* Left: Hamburger (mobile) + Logo */}
        <div className="flex items-center gap-2">
          {/* Hamburger — mobile only */}
          <button
            onClick={onToggleMobileSidebar}
            className="md:hidden p-2 -ml-1 text-slate-500 hover:text-cyan-400 hover:bg-white/5 rounded transition-colors"
            title="Toggle sidebar"
          >
            {mobileSidebarOpen ? <XIcon className="w-4 h-4" /> : <Menu className="w-4 h-4" />}
          </button>
          <span
            className="font-mono font-bold text-base tracking-tight select-none"
            style={{ fontFamily: "'JetBrains Mono', ui-monospace, monospace" }}
          >
            <span style={{ color: '#06b6d4' }}>md</span>
            <span style={{ color: 'rgba(100,116,139,0.5)' }}>/</span>
            <span style={{ color: 'rgba(100,116,139,0.35)', fontSize: '0.8em', marginLeft: '1px' }}>editor</span>
          </span>
        </div>

        {/* Right: Actions */}
        <div className="flex items-center gap-1">
          {/* Tasks */}
          <button
            onClick={() => navigate('/tasks')}
            className="p-2 text-slate-600 hover:text-cyan-400 hover:bg-white/5 rounded transition-colors"
            title="Task Tracker"
          >
            <Kanban className="w-4 h-4" />
          </button>

          {/* New Note */}
          <button
            onClick={handleNewNote}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono bg-cyan-500/15 text-cyan-400 border border-cyan-500/25 rounded hover:bg-cyan-500/25 hover:border-cyan-500/40 transition-colors"
            title="New Note (Cmd+N)"
          >
            <span className="text-cyan-600 text-[10px]">+</span>
            <span>new</span>
          </button>

          {/* Search */}
          <button
            onClick={handleSearch}
            className="p-2 text-slate-600 hover:text-cyan-400 hover:bg-white/5 rounded transition-colors"
            title="Search All Notes (Cmd+Shift+F)"
          >
            <Search className="w-4 h-4" />
          </button>

          {/* Export Dropdown — hidden on mobile */}
          <div className="relative hidden md:block" ref={exportMenuRef}>
            <button
              onClick={() => setShowExportMenu(!showExportMenu)}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded transition-colors"
              title="Export"
            >
              <i className="fas fa-download text-[10px]" />
              <span>export</span>
              <ChevronDown className={`w-3 h-3 transition-transform duration-150 ${showExportMenu ? 'rotate-180' : ''}`} />
            </button>

            {showExportMenu && (
              <div
                className="absolute right-0 mt-1.5 w-52 rounded-lg overflow-hidden z-[100]"
                style={{
                  background: '#0d0f17',
                  border: '1px solid rgba(255,255,255,0.08)',
                  boxShadow: '0 8px 32px rgba(0,0,0,0.6), 0 0 0 1px rgba(6,182,212,0.05)',
                }}
              >
                <div className="px-3 py-2 border-b border-white/[0.05]">
                  <span className="text-[9px] font-mono font-semibold tracking-widest text-slate-700 uppercase">
                    Export as
                  </span>
                </div>
                <div className="py-1">
                  <button onClick={() => handleExport('md')} className={menuItemCls}>
                    <span className="text-slate-700 w-4 text-center font-mono text-[10px]">.md</span>
                    <span>Markdown</span>
                  </button>
                  <button onClick={() => handleExport('pdf')} className={menuItemCls}>
                    <span className="text-slate-700 w-4 text-center font-mono text-[10px]">pdf</span>
                    <span>PDF</span>
                  </button>
                  <button onClick={() => handleExport('html')} className={menuItemCls}>
                    <span className="text-slate-700 w-4 text-center font-mono text-[10px]">htm</span>
                    <span>HTML preview</span>
                  </button>
                  <button onClick={() => handleExport('docx')} className={menuItemCls}>
                    <span className="text-slate-700 w-4 text-center font-mono text-[10px]">doc</span>
                    <span>Word (.docx)</span>
                  </button>
                  <div className="my-1 border-t border-white/[0.05]" />
                  <button onClick={() => handleExport('zip')} className={menuItemCls}>
                    <span className="text-slate-700 w-4 text-center font-mono text-[10px]">zip</span>
                    <span>All notes as ZIP</span>
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      </header>

      <NewNoteModal
        visible={showNewNoteModal}
        onClose={() => setShowNewNoteModal(false)}
        folders={folders.map((f) => ({ path: f.path, name: f.name }))}
        onCreate={handleCreateNote}
      />

      <SearchModal
        visible={showSearchModal}
        onClose={() => setShowSearchModal(false)}
        onNoteSelect={handleSearchNoteSelect}
      />
    </>
  )
}

export default Header
