import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

interface TerminalViewProps {
  sessionId: string
  onData: (data: string) => void
}

export function TerminalView({ sessionId, onData }: TerminalViewProps) {
  const terminalRef = useRef<HTMLDivElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)

  useEffect(() => {
    if (!terminalRef.current) return

    // Create terminal
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
      },
      rows: 30,
      cols: 100,
    })

    // Create fit addon
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)

    // Open terminal
    term.open(terminalRef.current)
    fitAddon.fit()

    // Handle user input
    term.onData((data) => {
      onData(data)
    })

    // Handle resize
    const handleResize = () => {
      fitAddon.fit()
    }
    window.addEventListener('resize', handleResize)

    xtermRef.current = term
    fitAddonRef.current = fitAddon

    return () => {
      window.removeEventListener('resize', handleResize)
      term.dispose()
    }
  }, [sessionId, onData])

  // Expose write method via ref
  useEffect(() => {
    if (xtermRef.current) {
      // Store reference for parent to call
      ;(terminalRef.current as any).write = (data: string) => {
        xtermRef.current?.write(data)
      }
    }
  }, [])

  return (
    <div
      ref={terminalRef}
      className="w-full h-full bg-[#1e1e1e]"
      style={{ minHeight: '500px' }}
    />
  )
}

export default TerminalView
