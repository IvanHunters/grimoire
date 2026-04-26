import { useEffect, useRef, useCallback } from 'react'
import type { Note } from '../types/note'

const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:3000/claude-chat'

interface UseTerminalWebSocketProps {
  sessionId: string
  dangerousMode?: boolean
  currentNote?: Note | null
  onOutput: (data: string) => void
}

export function useTerminalWebSocket({ sessionId, dangerousMode = true, currentNote, onOutput }: UseTerminalWebSocketProps) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const onOutputRef = useRef(onOutput)
  const isConnectingRef = useRef(false)

  // Keep onOutput callback ref up to date
  useEffect(() => {
    onOutputRef.current = onOutput
  }, [onOutput])

  // Connect to WebSocket
  useEffect(() => {
    // Prevent multiple simultaneous connections
    if (isConnectingRef.current) {
      return
    }

    const connect = () => {
      // Prevent reconnect if already connecting
      if (isConnectingRef.current) {
        return
      }
      isConnectingRef.current = true

      console.log('Connecting to WebSocket...', sessionId)
      const ws = new WebSocket(WS_URL)

      ws.onopen = () => {
        console.log('Terminal WebSocket connected')
        isConnectingRef.current = false

        // Send init message with currentNote
        ws.send(JSON.stringify({
          type: 'init',
          sessionId,
          dangerousMode,
          currentNote: currentNote ? {
            name: currentNote.title,
            folder: currentNote.folder || '',
            content: currentNote.content,
            type: currentNote.type || '',
            projectPath: currentNote.project_path || '',
          } : null,
        }))
      }

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data)

          if (message.type === 'terminal_output' && message.content) {
            // Write raw output to terminal using ref to avoid dependency
            onOutputRef.current(message.content)
          }
        } catch (error) {
          console.error('Failed to parse WebSocket message:', error)
        }
      }

      ws.onerror = (error) => {
        console.error('Terminal WebSocket error:', error)
        isConnectingRef.current = false
      }

      ws.onclose = () => {
        console.log('Terminal WebSocket closed, reconnecting...')
        wsRef.current = null
        isConnectingRef.current = false

        // Reconnect after 1 second
        reconnectTimeoutRef.current = window.setTimeout(() => {
          connect()
        }, 1000)
      }

      wsRef.current = ws
    }

    connect()

    return () => {
      console.log('Cleaning up WebSocket connection')
      isConnectingRef.current = false
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [sessionId, dangerousMode, currentNote])

  // Send input to terminal
  const sendInput = useCallback((data: string) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'terminal_input',
        sessionId,
        content: data,
      }))
    }
  }, [sessionId])

  return { sendInput }
}
