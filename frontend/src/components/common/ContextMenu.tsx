import { useEffect, useRef } from 'react'

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
      style={{ left: `${x}px`, top: `${y}px` }}
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
