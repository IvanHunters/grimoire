import { useEffect, useRef } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Note } from '../../types/note'

interface WikilinkPopupProps {
  note: Note
  onClose: () => void
  onMouseEnter?: () => void
  onMouseLeave?: () => void
}

function WikilinkPopup({ note, onClose, onMouseEnter, onMouseLeave }: WikilinkPopupProps) {
  const popupRef = useRef<HTMLDivElement>(null)

  // Remove frontmatter from preview
  const contentPreview = note.content
    .replace(/^---\n[\s\S]*?\n---\n/, '') // Remove frontmatter
    .substring(0, 500) // Limit to 500 chars for preview

  // Close on click outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (popupRef.current && !popupRef.current.contains(event.target as Node)) {
        onClose()
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [onClose])

  return (
    <div
      ref={popupRef}
      className="preview-popup show"
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      <h3>{note.title}</h3>
      <div className="preview-popup-content">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>
          {contentPreview}
        </ReactMarkdown>

        {note.content.length > 500 && (
          <div className="wikilink-popup-more">...</div>
        )}
      </div>
    </div>
  )
}

export default WikilinkPopup
