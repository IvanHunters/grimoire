import { useState } from 'react'
import { Search, FileDown, FilePlus } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'
import NewNoteModal from '../modals/NewNoteModal'

interface HeaderProps {
  onNoteSelect?: (note: any) => void
}

function Header({ onNoteSelect }: HeaderProps) {
  const { folders, createNote } = useNotes()
  const [showNewNoteModal, setShowNewNoteModal] = useState(false)

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
    // Will be implemented with modal
    console.log('Global search')
  }

  const handleExport = () => {
    // Will be implemented with dropdown
    console.log('Export')
  }

  return (
    <>
      <header className="h-14 border-b border-gray-200 bg-white flex items-center justify-between px-4 shadow-sm">
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

          {/* Export */}
          <button
            onClick={handleExport}
            className="p-2 text-gray-600 hover:bg-gray-100 rounded transition"
            title="Export"
          >
            <FileDown className="w-5 h-5" />
          </button>
        </div>
      </header>

      {/* New Note Modal */}
      <NewNoteModal
        visible={showNewNoteModal}
        onClose={() => setShowNewNoteModal(false)}
        folders={folders.map((f) => ({ path: f.path, name: f.name }))}
        onCreate={handleCreateNote}
      />
    </>
  )
}

export default Header
