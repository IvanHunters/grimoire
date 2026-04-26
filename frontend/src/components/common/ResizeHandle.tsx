import { useEffect, useRef } from 'react'

interface ResizeHandleProps {
  onResize: (editorWidth: number, previewWidth: number) => void
  containerRef: React.RefObject<HTMLDivElement | null>
}

/**
 * Resize handle between Editor and Preview panels
 * Based on design-prototype.html logic
 *
 * Features:
 * - Smooth drag with requestAnimationFrame
 * - 20-80% width constraints
 * - Fixed widths (not flex percentages)
 * - Hover highlight
 */
function ResizeHandle({ onResize, containerRef }: ResizeHandleProps) {
  const handleRef = useRef<HTMLDivElement>(null)
  const isDraggingRef = useRef(false)
  const animationFrameRef = useRef<number | undefined>(undefined)

  // Store cached measurements in refs (like global vars in prototype)
  const containerRectRef = useRef<DOMRect | null>(null)
  const containerWidthRef = useRef<number>(0)
  const handleWidthRef = useRef<number>(4)

  useEffect(() => {
    const handle = handleRef.current
    if (!handle) return

    // Global mousemove handler (like prototype)
    const handleMouseMove = (e: MouseEvent) => {
      if (!isDraggingRef.current) return
      e.preventDefault()

      // Cancel previous animation frame
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
      }

      // Capture mouseX immediately (before requestAnimationFrame)
      const mouseX = e.clientX

      // Use requestAnimationFrame for smooth performance
      animationFrameRef.current = requestAnimationFrame(() => {
        const container = containerRef.current
        if (!container || !containerRectRef.current) return

        // Get editor and preview panels
        const editorPanel = container.querySelector('.editor-panel') as HTMLElement
        const previewPanel = container.querySelector('.preview-panel') as HTMLElement

        if (!editorPanel || !previewPanel) return

        // Calculate mouse position relative to container
        const x = mouseX - containerRectRef.current.left

        // Define min/max widths (20% to 80% of container)
        const minWidth = containerWidthRef.current * 0.2
        const maxWidth = containerWidthRef.current * 0.8

        // Clamp editor width to valid range
        let editorWidth = Math.max(minWidth, Math.min(maxWidth, x))

        // Calculate preview width
        let previewWidth = containerWidthRef.current - editorWidth - handleWidthRef.current

        // Ensure preview also respects minimum
        if (previewWidth < minWidth) {
          previewWidth = minWidth
          editorWidth = containerWidthRef.current - previewWidth - handleWidthRef.current
        }

        // Apply widths directly to DOM (like prototype) - no React re-render!
        editorPanel.style.width = `${editorWidth}px`
        editorPanel.style.flex = 'none'
        previewPanel.style.width = `${previewWidth}px`
        previewPanel.style.flex = 'none'

        // Also update via callback for state persistence (ONLY during drag)
        if (isDraggingRef.current) {
          onResize(editorWidth, previewWidth)
        }
      })
    }

    // Global mouseup handler (like prototype)
    const handleMouseUp = () => {
      if (!isDraggingRef.current) return

      isDraggingRef.current = false

      // Restore cursor
      document.body.style.cursor = ''
      document.body.style.userSelect = ''

      // Cancel animation frame
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
        animationFrameRef.current = undefined
      }
    }

    const handleMouseDown = (e: MouseEvent) => {
      e.preventDefault()

      const container = containerRef.current
      if (!container) return

      // Cache container measurements once
      containerRectRef.current = container.getBoundingClientRect()
      containerWidthRef.current = container.offsetWidth
      handleWidthRef.current = handle.offsetWidth || 4

      isDraggingRef.current = true

      // Add cursor style to body
      document.body.style.cursor = 'col-resize'
      document.body.style.userSelect = 'none'
    }

    // Attach listeners (like prototype)
    handle.addEventListener('mousedown', handleMouseDown)
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

    return () => {
      handle.removeEventListener('mousedown', handleMouseDown)
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)

      // Cleanup on unmount
      if (animationFrameRef.current) {
        cancelAnimationFrame(animationFrameRef.current)
      }
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
  }, [onResize, containerRef])

  return (
    <div
      ref={handleRef}
      className="resize-handle w-1"
    />
  )
}

export default ResizeHandle
