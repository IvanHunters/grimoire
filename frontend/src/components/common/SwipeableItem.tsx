import { useRef, useState, useEffect } from 'react'

export interface SwipeAction {
  icon: string
  label: string
  danger?: boolean
  onTap: () => void
}

interface SwipeableItemProps {
  children: React.ReactNode
  actions: SwipeAction[]
  disabled?: boolean
}

const ACTION_W = 64

export function SwipeableItem({ children, actions, disabled }: SwipeableItemProps) {
  const [offset, setOffset] = useState(0)
  const revealW = actions.length * ACTION_W
  const containerRef = useRef<HTMLDivElement>(null)
  const stateRef = useRef({ startX: 0, startY: 0, initOffset: 0, active: false, locked: false })

  useEffect(() => {
    if (offset === 0) return
    const close = (e: MouseEvent | TouchEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOffset(0)
    }
    document.addEventListener('mousedown', close)
    document.addEventListener('touchstart', close, { passive: true })
    return () => {
      document.removeEventListener('mousedown', close)
      document.removeEventListener('touchstart', close)
    }
  }, [offset])

  if (disabled || actions.length === 0) return <>{children}</>

  const onTouchStart = (e: React.TouchEvent) => {
    const s = stateRef.current
    s.startX = e.touches[0].clientX
    s.startY = e.touches[0].clientY
    s.initOffset = offset
    s.active = false
    s.locked = false
  }

  const onTouchMove = (e: React.TouchEvent) => {
    const s = stateRef.current
    const dx = e.touches[0].clientX - s.startX
    const dy = Math.abs(e.touches[0].clientY - s.startY)

    if (s.locked) return
    if (!s.active) {
      if (dy > 8 && dy > Math.abs(dx)) { s.locked = true; return }
      if (Math.abs(dx) > 4) s.active = true
    }
    if (!s.active) return

    const next = Math.min(0, Math.max(-revealW, s.initOffset + dx))
    setOffset(next)
    e.preventDefault()
  }

  const onTouchEnd = () => {
    const threshold = revealW * 0.35
    setOffset(Math.abs(offset) >= threshold ? -revealW : 0)
    stateRef.current.active = false
  }

  return (
    <div ref={containerRef} className="relative overflow-hidden">
      {/* Action buttons */}
      <div className="absolute right-0 top-0 bottom-0 flex" style={{ width: revealW }}>
        {actions.map((a, i) => (
          <button
            key={i}
            onPointerDown={(e) => { e.stopPropagation(); setOffset(0); a.onTap() }}
            className={`flex-1 flex flex-col items-center justify-center gap-0.5 text-[10px] font-mono active:brightness-75 ${
              a.danger
                ? 'bg-red-600/80 text-white'
                : 'bg-slate-700 text-slate-200'
            }`}
          >
            <i className={`fas ${a.icon} text-sm`} />
            <span>{a.label}</span>
          </button>
        ))}
      </div>

      {/* Content */}
      <div
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={onTouchEnd}
        style={{
          transform: `translateX(${offset}px)`,
          transition: stateRef.current.active ? 'none' : 'transform 0.2s ease',
          position: 'relative',
          zIndex: 1,
        }}
      >
        {children}
      </div>
    </div>
  )
}
