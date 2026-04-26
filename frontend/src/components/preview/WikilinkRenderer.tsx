import { useState } from 'react'
import { useNotes } from '../../contexts/NotesContext'
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
  const { notes, setCurrentNote } = useNotes()
  const [showPopup, setShowPopup] = useState(false)

  // Find linked note by title or path
  const linkedNote = notes.find(
    (note) =>
      note.title.toLowerCase() === target.toLowerCase() ||
      note.path.toLowerCase() === target.toLowerCase() ||
      note.path.toLowerCase().endsWith(`/${target.toLowerCase()}.md`)
  )

  // Debug logging
  console.log('Wikilink target:', target)
  console.log('Available notes:', notes.length)
  console.log('Linked note found:', linkedNote ? linkedNote.title : 'NOT FOUND')

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault()

    if (linkedNote) {
      setCurrentNote(linkedNote)
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
