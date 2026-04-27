import { useEffect, useRef } from 'react'

/**
 * Synchronized scroll between Editor and Preview
 *
 * Strategy:
 * - Track scroll percentage in one panel
 * - Apply same percentage to other panel
 * - Debounce to prevent infinite loop
 * - requestAnimationFrame for smooth performance
 */

interface UseSyncScrollProps {
  editorRef: React.RefObject<HTMLDivElement | null>
  previewRef: React.RefObject<HTMLDivElement | null>
  enabled: boolean // Only sync in split view
}

export function useSyncScroll({ editorRef, previewRef, enabled }: UseSyncScrollProps) {
  const isScrollingEditor = useRef(false)
  const isScrollingPreview = useRef(false)
  const editorScrollTimeoutRef = useRef<number | undefined>(undefined)
  const previewScrollTimeoutRef = useRef<number | undefined>(undefined)

  useEffect(() => {
    if (!enabled) return

    const editorElement = editorRef.current
    const previewElement = previewRef.current

    if (!editorElement || !previewElement) return

    // Find textarea element (plain textarea, not CodeMirror)
    const textarea = editorElement.querySelector('textarea') as HTMLTextAreaElement
    if (!textarea) return

    const syncEditorToPreview = () => {
      if (isScrollingPreview.current) return

      const scrollPercentage =
        textarea.scrollTop / (textarea.scrollHeight - textarea.clientHeight)

      if (!isNaN(scrollPercentage) && isFinite(scrollPercentage)) {
        isScrollingEditor.current = true

        const targetScroll =
          scrollPercentage * (previewElement.scrollHeight - previewElement.clientHeight)
        previewElement.scrollTop = targetScroll

        // Clear flag after debounce
        clearTimeout(editorScrollTimeoutRef.current)
        editorScrollTimeoutRef.current = window.setTimeout(() => {
          isScrollingEditor.current = false
        }, 100)
      }
    }

    const syncPreviewToEditor = () => {
      if (isScrollingEditor.current) return

      const scrollPercentage =
        previewElement.scrollTop / (previewElement.scrollHeight - previewElement.clientHeight)

      if (!isNaN(scrollPercentage) && isFinite(scrollPercentage)) {
        isScrollingPreview.current = true

        const targetScroll =
          scrollPercentage * (textarea.scrollHeight - textarea.clientHeight)
        textarea.scrollTop = targetScroll

        // Clear flag after debounce
        clearTimeout(previewScrollTimeoutRef.current)
        previewScrollTimeoutRef.current = window.setTimeout(() => {
          isScrollingPreview.current = false
        }, 100)
      }
    }

    textarea.addEventListener('scroll', syncEditorToPreview)
    previewElement.addEventListener('scroll', syncPreviewToEditor)

    return () => {
      textarea.removeEventListener('scroll', syncEditorToPreview)
      previewElement.removeEventListener('scroll', syncPreviewToEditor)
      clearTimeout(editorScrollTimeoutRef.current)
      clearTimeout(previewScrollTimeoutRef.current)
    }
  }, [editorRef, previewRef, enabled])
}
