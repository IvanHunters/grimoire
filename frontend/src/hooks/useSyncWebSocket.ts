import { useEffect, useRef } from 'react'
import type { Note } from '../types/note'

const WS_URL = import.meta.env.VITE_WS_URL ||
  `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/claude-chat`

interface SyncEvent {
  type: 'note_created' | 'note_updated' | 'note_deleted' | 'folder_created' | 'folder_deleted'
  note?: Note
  noteId?: string
  folder?: { path: string }
  path?: string
}

interface UseSyncWebSocketProps {
  onNoteCreated: (note: Note) => void
  onNoteUpdated: (note: Note) => void
  onNoteDeleted: (noteId: string) => void
  onFolderCreated: () => void
  onFolderDeleted: () => void
}

export function useSyncWebSocket({
  onNoteCreated,
  onNoteUpdated,
  onNoteDeleted,
  onFolderCreated,
  onFolderDeleted,
}: UseSyncWebSocketProps) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const intentionalCloseRef = useRef(false)
  const handlersRef = useRef({
    onNoteCreated,
    onNoteUpdated,
    onNoteDeleted,
    onFolderCreated,
    onFolderDeleted,
  })

  // Keep handlers up to date
  useEffect(() => {
    handlersRef.current = {
      onNoteCreated,
      onNoteUpdated,
      onNoteDeleted,
      onFolderCreated,
      onFolderDeleted,
    }
  }, [onNoteCreated, onNoteUpdated, onNoteDeleted, onFolderCreated, onFolderDeleted])

  // Connect to WebSocket
  useEffect(() => {
    const connect = () => {
      console.log('[Sync] Connecting to WebSocket...')
      const ws = new WebSocket(WS_URL)

      ws.onopen = () => {
        console.log('[Sync] WebSocket connected')
      }

      ws.onmessage = (event) => {
        try {
          const message: SyncEvent = JSON.parse(event.data)
          console.log('[Sync] Received event:', message.type)

          const handlers = handlersRef.current

          switch (message.type) {
            case 'note_created':
              if (message.note) {
                handlers.onNoteCreated(message.note)
              }
              break
            case 'note_updated':
              if (message.note) {
                handlers.onNoteUpdated(message.note)
              }
              break
            case 'note_deleted':
              if (message.noteId) {
                handlers.onNoteDeleted(message.noteId)
              }
              break
            case 'folder_created':
              handlers.onFolderCreated()
              break
            case 'folder_deleted':
              handlers.onFolderDeleted()
              break
          }
        } catch (error) {
          console.error('[Sync] Failed to parse WebSocket message:', error)
        }
      }

      ws.onerror = (error) => {
        console.error('[Sync] WebSocket error:', error)
      }

      ws.onclose = () => {
        wsRef.current = null

        // Only reconnect if not intentionally closed
        if (!intentionalCloseRef.current) {
          console.log('[Sync] WebSocket closed unexpectedly, reconnecting...')
          reconnectTimeoutRef.current = window.setTimeout(() => {
            connect()
          }, 2000) // Reconnect after 2 seconds
        } else {
          console.log('[Sync] WebSocket closed intentionally')
        }
      }

      wsRef.current = ws
    }

    // Reset intentional close flag and connect
    intentionalCloseRef.current = false
    connect()

    return () => {
      console.log('[Sync] Cleaning up WebSocket connection')
      // Mark as intentional close
      intentionalCloseRef.current = true

      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
        reconnectTimeoutRef.current = null
      }

      if (wsRef.current) {
        const ws = wsRef.current
        ws.onopen = null
        ws.onmessage = null
        ws.onerror = null
        ws.onclose = null
        ws.close()
        wsRef.current = null
      }
    }
  }, []) // Empty deps - only connect once

  return null // This hook doesn't return anything
}
