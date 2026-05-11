import { useState, useRef } from 'react'
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
  const hideTimerRef = useRef<number | null>(null)

  // Find linked note using the same resolution logic as graph/other components
  const linkedNoteId = resolveWikilinkTarget(target, notes)
  const linkedNote = linkedNoteId ? notes.find(n => n.id === linkedNoteId) : null

  const cancelHide = () => {
    if (hideTimerRef.current !== null) {
      clearTimeout(hideTimerRef.current)
      hideTimerRef.current = null
    }
  }

  const scheduleHide = () => {
    cancelHide()
    hideTimerRef.current = window.setTimeout(() => {
      setShowPopup(false)
      hideTimerRef.current = null
    }, 200)
  }

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault()
    cancelHide()
    setShowPopup(false)
    if (linkedNote) {
      navigate(`/notes/${linkedNote.id}`)
    } else {
      console.warn(`Wikilink target not found: ${target}`)
    }
  }

  const handleMouseEnter = () => {
    if (!linkedNote) return
    cancelHide()
    setShowPopup(true)
  }

  const handleMouseLeave = () => {
    scheduleHide()
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
          onMouseEnter={cancelHide}
          onMouseLeave={scheduleHide}
        />
      )}
    </>
  )
}

export default WikilinkRenderer
