import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useNotes } from '../../contexts/NotesContext'
import { resolveWikilinkTarget } from '../../utils/wikilinks'
import WikilinkPopup from './WikilinkPopup'

interface WikilinkRendererProps {
  target: string // Note title or path
  children: React.ReactNode // Display text
}

/**
 * Custom renderer for wikilinks in markdown preview
 * Handles [[link]] and [[link|alias]] syntax
 * Shows hover popup with note preview
 */
function WikilinkRenderer({ target, children }: WikilinkRendererProps) {
  const { notes } = useNotes()
  const navigate = useNavigate()
  const [showPopup, setShowPopup] = useState(false)

  // Find linked note using the same resolution logic as graph/other components
  const linkedNoteId = resolveWikilinkTarget(target, notes)
  const linkedNote = linkedNoteId ? notes.find(n => n.id === linkedNoteId) : null

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault()

    if (linkedNote) {
      // Navigate to note URL - setCurrentNote will be called by useEffect in HomePage
      navigate(`/notes/${linkedNote.id}`)
    } else {
      console.warn(`Wikilink target not found: ${target}`)
    }
  }

  const handleMouseEnter = () => {
    if (!linkedNote) return
    setShowPopup(true)
  }

  const handleMouseLeave = () => {
    setShowPopup(false)
  }

  // If note not found, render as broken link
  if (!linkedNote) {
    return (
      <a
        href="#"
        className="wikilink wikilink-broken"
        onClick={handleClick}
        title={`Note not found: ${target}`}
      >
        {children}
      </a>
    )
  }

  return (
    <>
      <a
        href="#"
        className="wikilink"
        onClick={handleClick}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
        title={linkedNote.title}
      >
        {children}
      </a>

      {showPopup && (
        <WikilinkPopup
          note={linkedNote}
          onClose={() => setShowPopup(false)}
        />
      )}
    </>
  )
}

export default WikilinkRenderer
