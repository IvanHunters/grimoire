import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export interface ContextMenuItem {
  icon?: string
  text: string
  action?: () => void
  danger?: boolean
  divider?: boolean
  hasSubmenu?: boolean
  submenu?: ContextMenuItem[]
}

interface ContextMenuProps {
  visible: boolean
  x: number
  y: number
  items: ContextMenuItem[]
  onClose: () => void
}

function ContextMenu({ visible, x, y, items, onClose }: ContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)
  // Clamped position so the menu never overflows the viewport. When
  // user right-clicks near the bottom/right edge, the rendered menu
  // would otherwise extend past the visible area and items get cut.
  // Initial value mirrors the requested position; useLayoutEffect
  // measures actual dims after first paint and corrects.
  const [pos, setPos] = useState({ x, y })

  // Single useLayoutEffect handles both reset-on-anchor-change AND
  // clamp-to-viewport. The previous two-effect setup had a race:
  // a deferred useEffect reset pos to the raw (x, y) AFTER the
  // sync useLayoutEffect had already clamped it, so the menu
  // briefly landed off-screen for one paint. Fires before paint
  // when visible/x/y/items change.
  useLayoutEffect(() => {
    if (!visible || !menuRef.current) return
    const rect = menuRef.current.getBoundingClientRect()
    const margin = 8 // breathing room from viewport edge
    let nx = x
    let ny = y
    if (x + rect.width > window.innerWidth - margin) {
      nx = Math.max(margin, window.innerWidth - rect.width - margin)
    }
    if (y + rect.height > window.innerHeight - margin) {
      ny = Math.max(margin, window.innerHeight - rect.height - margin)
    }
    setPos({ x: nx, y: ny })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, x, y, items])

  // Close on click outside
  useEffect(() => {
    if (!visible) return

    const handleClickOutside = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        onClose()
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [visible, onClose])

  // Close on Escape
  useEffect(() => {
    if (!visible) return

    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose()
      }
    }

    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('keydown', handleEscape)
    }
  }, [visible, onClose])

  if (!visible) return null

  const handleItemClick = (item: ContextMenuItem) => {
    if (item.action) {
      item.action()
      onClose()
    }
  }

  return (
    <div
      ref={menuRef}
      className={`context-menu ${visible ? 'show' : ''}`}
      style={{ left: `${pos.x}px`, top: `${pos.y}px` }}
    >
      {items.map((item, index) => {
        if (item.divider) {
          return <div key={index} className="context-menu-divider" />
        }

        if (item.hasSubmenu && item.submenu) {
          return (
            <div key={index} className="context-menu-item context-menu-item-with-submenu">
              {item.icon && <i className={`fas ${item.icon}`} />}
              <span>{item.text}</span>
              <i className="fas fa-chevron-right ml-auto" style={{ fontSize: '10px' }} />

              {/* Submenu */}
              <div className="context-menu-submenu">
                {item.submenu.map((subItem, subIndex) => (
                  <div
                    key={subIndex}
                    className={`context-menu-item ${subItem.danger ? 'danger' : ''}`}
                    onClick={() => handleItemClick(subItem)}
                  >
                    {subItem.icon && <i className={`fas ${subItem.icon}`} />}
                    <span>{subItem.text}</span>
                  </div>
                ))}
              </div>
            </div>
          )
        }

        return (
          <div
            key={index}
            className={`context-menu-item ${item.danger ? 'danger' : ''}`}
            onClick={() => handleItemClick(item)}
          >
            {item.icon && <i className={`fas ${item.icon}`} />}
            <span>{item.text}</span>
          </div>
        )
      })}
    </div>
  )
}

export default ContextMenu
