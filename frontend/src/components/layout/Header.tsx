import { useState, useRef, useEffect } from 'react'
import { Search, FileDown, FilePlus, ChevronDown } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'
import NewNoteModal from '../modals/NewNoteModal'
import SearchModal from '../modals/SearchModal'
import { exportToPDF, exportToWord, exportAllNotesToZip } from '../../utils/export'

interface HeaderProps {
  onNoteSelect?: (note: any) => void
  previewRef?: React.RefObject<HTMLDivElement | null>
}

function Header({ onNoteSelect, previewRef }: HeaderProps) {
  const { folders, createNote, notes, currentNote } = useNotes()
  const [showNewNoteModal, setShowNewNoteModal] = useState(false)
  const [showSearchModal, setShowSearchModal] = useState(false)
  const [showExportMenu, setShowExportMenu] = useState(false)
  const exportMenuRef = useRef<HTMLDivElement>(null)

  const handleNewNote = () => {
    setShowNewNoteModal(true)
  }

  const handleCreateNote = async (name: string, folder: string) => {
    try {
      const newNote = await createNote(name, folder)
      onNoteSelect?.(newNote)
      setShowNewNoteModal(false)
    } catch (error) {
      console.error('Failed to create note:', error)
    }
  }

  const handleSearch = () => {
    setShowSearchModal(true)
  }

  const handleSearchNoteSelect = (noteId: string) => {
    const note = notes.find(n => n.id === noteId)
    if (note) {
      onNoteSelect?.(note)
    }
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

    const content = currentNote.content
    const filename = `${currentNote.title.replace(/[^a-z0-9]/gi, '-').toLowerCase()}.md`

    const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  // Close export menu when clicking outside
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

  return (
    <>
      <header className="h-14 border-b border-gray-200 bg-white flex items-center justify-between px-4 shadow-sm relative z-50">
        {/* Left: Logo */}
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-bold bg-gradient-to-r from-purple-600 to-blue-600 bg-clip-text text-transparent">
            Markdown Editor
          </h1>
        </div>

        {/* Center: Breadcrumb / Current note title */}
        <div className="flex-1 px-8 text-center">
          <p className="text-sm text-gray-600">
            {/* Current note path will be shown here */}
          </p>
        </div>

        {/* Right: Actions */}
        <div className="flex items-center gap-2">
          {/* New Note */}
          <button
            onClick={handleNewNote}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-purple-600 text-white rounded hover:bg-purple-700 transition"
            title="New Note (Cmd+N)"
          >
            <FilePlus className="w-4 h-4" />
            <span>New</span>
          </button>

          {/* Global Search */}
          <button
            onClick={handleSearch}
            className="p-2 text-gray-600 hover:bg-gray-100 rounded transition"
            title="Search All Notes (Cmd+Shift+F)"
          >
            <Search className="w-5 h-5" />
          </button>

          {/* Export Dropdown */}
          <div className="relative" ref={exportMenuRef}>
            <button
              onClick={() => setShowExportMenu(!showExportMenu)}
              className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-100 rounded transition"
              title="Export"
            >
              <FileDown className="w-4 h-4" />
              <span>Export</span>
              <ChevronDown className="w-3 h-3" />
            </button>

            {/* Export Menu */}
            {showExportMenu && (
              <div className="absolute right-0 mt-1 w-56 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-[100]">
                <button
                  onClick={() => handleExport('md')}
                  className="w-full flex items-center gap-3 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 transition"
                >
                  <i className="fas fa-file-code w-4"></i>
                  <span>Export to Markdown (.md)</span>
                </button>
                <button
                  onClick={() => handleExport('pdf')}
                  className="w-full flex items-center gap-3 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 transition"
                >
                  <i className="fas fa-file-pdf w-4"></i>
                  <span>Export to PDF</span>
                </button>
                <button
                  onClick={() => handleExport('docx')}
                  className="w-full flex items-center gap-3 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 transition"
                >
                  <i className="fas fa-file-word w-4"></i>
                  <span>Export to Word (.docx)</span>
                </button>
                <div className="my-1 border-t border-gray-200"></div>
                <button
                  onClick={() => handleExport('zip')}
                  className="w-full flex items-center gap-3 px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 transition"
                >
                  <i className="fas fa-file-archive w-4"></i>
                  <span>Export all notes as ZIP</span>
                </button>
              </div>
            )}
          </div>
        </div>
      </header>

      {/* New Note Modal */}
      <NewNoteModal
        visible={showNewNoteModal}
        onClose={() => setShowNewNoteModal(false)}
        folders={folders.map((f) => ({ path: f.path, name: f.name }))}
        onCreate={handleCreateNote}
      />

      {/* Search Modal */}
      <SearchModal
        visible={showSearchModal}
        onClose={() => setShowSearchModal(false)}
        onNoteSelect={handleSearchNoteSelect}
      />
    </>
  )
}

export default Header
