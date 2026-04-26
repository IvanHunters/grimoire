import { useEffect, useRef, useCallback, useState } from 'react'
import type { WSMessage, ConnectionStatus } from '../types/claude'
import type { RealtimeEvent } from '../types/events'

interface UseWebSocketOptions {
  url: string
  onMessage?: (message: WSMessage) => void
  onRealtimeEvent?: (event: RealtimeEvent) => void
  onError?: (error: Event) => void
  onClose?: (event: CloseEvent) => void
  reconnect?: boolean
  reconnectInterval?: number
}

interface UseWebSocketReturn {
  connectionStatus: ConnectionStatus
  sendMessage: (message: WSMessage) => void
  disconnect: () => void
  connect: () => void
}

export function useWebSocket({
  url,
  onMessage,
  onRealtimeEvent,
  onError,
  onClose,
  reconnect = true,
  reconnectInterval = 3000,
}: UseWebSocketOptions): UseWebSocketReturn {
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('disconnected')
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const shouldReconnectRef = useRef<boolean>(reconnect)

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return
    }

    try {
      setConnectionStatus('connecting')
      const ws = new WebSocket(url)

      ws.onopen = () => {
        console.log('WebSocket connected')
        setConnectionStatus('ready')

        // Clear any pending reconnect
        if (reconnectTimeoutRef.current) {
          window.clearTimeout(reconnectTimeoutRef.current)
          reconnectTimeoutRef.current = null
        }
      }

      ws.onmessage = (event) => {
        try {
          const message: WSMessage = JSON.parse(event.data)

          // Handle real-time events separately
          if (
            message.type === 'note_created' ||
            message.type === 'note_updated' ||
            message.type === 'note_deleted' ||
            message.type === 'folder_created' ||
            message.type === 'folder_deleted'
          ) {
            if (onRealtimeEvent) {
              onRealtimeEvent({
                type: message.type,
                note: message.note,
                folder: message.folder,
                noteId: message.noteId,
                path: message.path,
              })
            }
          }

          // Call general message handler
          if (onMessage) {
            onMessage(message)
          }

          // Update connection status based on message type
          if (message.type === 'message_start') {
            setConnectionStatus('generating')
          } else if (message.type === 'message_complete' || message.type === 'error' || message.type === 'stopped') {
            setConnectionStatus('ready')
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err)
        }
      }

      ws.onerror = (event) => {
        console.error('WebSocket error:', event)
        setConnectionStatus('error')
        if (onError) {
          onError(event)
        }
      }

      ws.onclose = (event) => {
        console.log('WebSocket closed:', event.code, event.reason)
        setConnectionStatus('disconnected')
        wsRef.current = null

        if (onClose) {
          onClose(event)
        }

        // Attempt to reconnect if enabled
        if (shouldReconnectRef.current && !event.wasClean) {
          console.log(`Reconnecting in ${reconnectInterval}ms...`)
          reconnectTimeoutRef.current = window.setTimeout(() => {
            connect()
          }, reconnectInterval)
        }
      }

      wsRef.current = ws
    } catch (err) {
      console.error('Failed to create WebSocket connection:', err)
      setConnectionStatus('error')
    }
  }, [url, onMessage, onRealtimeEvent, onError, onClose, reconnectInterval])

  const disconnect = useCallback(() => {
    shouldReconnectRef.current = false

    if (reconnectTimeoutRef.current) {
      window.clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }

    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
  }, [])

  const sendMessage = useCallback((message: WSMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(message))
    } else {
      console.error('WebSocket is not connected')
    }
  }, [])

  useEffect(() => {
    connect()

    return () => {
      shouldReconnectRef.current = false
      if (reconnectTimeoutRef.current) {
        window.clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [connect])

  return {
    connectionStatus,
    sendMessage,
    disconnect,
    connect,
  }
}
