import { useEffect, useRef, useState } from 'react'
import mermaid from 'mermaid'

interface MermaidDiagramProps {
  code: string
}

let diagramCounter = 0
let mermaidInitialized = false

function MermaidDiagram({ code }: MermaidDiagramProps) {
  const mermaidRef = useRef<HTMLDivElement>(null)
  const [, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!mermaidInitialized) {
      mermaid.initialize({
        startOnLoad: false,
        theme: 'default',
        securityLevel: 'loose',
      })
      mermaidInitialized = true
    }

    if (mermaidRef.current && code) {
      setError(null)

      const newId = `mermaid-${++diagramCounter}-${Date.now()}`

      mermaid
        .render(newId, code)
        .then(({ svg }) => {
          if (mermaidRef.current) {
            mermaidRef.current.innerHTML = svg
          }
        })
        .catch((error) => {
          console.error('Mermaid render error:', error)
          setError(error.message || 'Invalid diagram syntax')
          if (mermaidRef.current) {
            mermaidRef.current.innerHTML = `
              <div style="color: #dc2626; padding: 20px; text-align: center;">
                <i class="fas fa-exclamation-triangle" style="font-size: 32px; margin-bottom: 10px;"></i>
                <div>Invalid Mermaid syntax</div>
                <div style="font-size: 12px; margin-top: 8px; color: #6b7280;">${error.message || 'Check your code'}</div>
              </div>
            `
          }
        })
    }
  }, [code])

  return (
    <div className="mermaid-wrapper my-4 rounded-lg overflow-auto bg-white p-4 shadow-sm border border-gray-100 dark:border-gray-700">
      <div ref={mermaidRef} className="mermaid" data-source={code} />
    </div>
  )
}

export default MermaidDiagram
