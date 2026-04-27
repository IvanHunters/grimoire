import { useRef, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useTerminalWebSocket } from '../../hooks/useTerminalWebSocket'
import { useEffect } from 'react'
import { useNotes } from '../../contexts/NotesContext'

interface TerminalChatProps {
  sessionId: string
  dangerousMode?: boolean
}

export function TerminalChat({ sessionId, dangerousMode = true }: TerminalChatProps) {
  const { currentNote } = useNotes()
  const terminalRef = useRef<HTMLDivElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)

  // Handle output from WebSocket
  const handleOutput = useCallback((data: string) => {
    if (xtermRef.current) {
      xtermRef.current.write(data)
    }
  }, [])

  // Setup WebSocket connection
  const { sendInput } = useTerminalWebSocket({
    sessionId,
    dangerousMode,
    currentNote,
    onOutput: handleOutput,
  })

  // Initialize terminal
  useEffect(() => {
    if (!terminalRef.current || xtermRef.current) return

    // Create terminal
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#ffffff',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#ffffff',
      },
      allowTransparency: false,
      convertEol: false,
    })

    // Create fit addon
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)

    // Open terminal
    term.open(terminalRef.current)
    fitAddon.fit()

    // Handle user input - send to WebSocket
    term.onData((data) => {
      sendInput(data)
    })

    // Handle resize with debounce
    let resizeTimeout: number | undefined
    const handleResize = () => {
      // Debounce resize to avoid too many fit calls
      if (resizeTimeout) {
        clearTimeout(resizeTimeout)
      }
      resizeTimeout = window.setTimeout(() => {
        try {
          fitAddon.fit()
        } catch (error) {
          console.error('Failed to fit terminal:', error)
        }
      }, 100)
    }

    // Listen to window resize
    window.addEventListener('resize', handleResize)

    // Watch for container size changes using ResizeObserver
    const resizeObserver = new ResizeObserver(() => {
      handleResize()
    })

    if (terminalRef.current) {
      resizeObserver.observe(terminalRef.current)
    }

    xtermRef.current = term
    fitAddonRef.current = fitAddon

    // Focus terminal
    term.focus()

    // Initial fit after a short delay to ensure container is rendered
    window.setTimeout(() => {
      try {
        fitAddon.fit()
      } catch (error) {
        console.error('Failed initial fit:', error)
      }
    }, 100)

    return () => {
      window.removeEventListener('resize', handleResize)
      if (resizeTimeout) {
        clearTimeout(resizeTimeout)
      }
      resizeObserver.disconnect()
      term.dispose()
      xtermRef.current = null
      fitAddonRef.current = null
    }
  }, [sessionId, sendInput])

  return (
    <div className="flex flex-col h-full bg-[#1e1e1e]">
      <div className="flex-shrink-0 px-4 py-2 bg-gray-900 border-b border-gray-700">
        <div className="text-xs text-gray-400">
          <span className="font-mono">Session: {sessionId.slice(0, 8)}...</span>
          {dangerousMode && (
            <span className="ml-4 text-yellow-500">⚠️ Dangerous Mode</span>
          )}
        </div>
      </div>
      <div
        ref={terminalRef}
        className="flex-1 p-2"
      />
    </div>
  )
}

export default TerminalChat
