import { useEffect, useRef } from 'react'
import type { Task } from '../types/task'

const WS_URL = import.meta.env.VITE_WS_URL ||
  `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/claude-chat`

export interface TaskEvent {
  type: 'task_created' | 'task_updated' | 'task_deleted'
  task?: Task
  taskId?: string
}

interface UseEventsWebSocketProps {
  onTaskEvent?: (event: TaskEvent) => void
  enabled?: boolean
}

export function useEventsWebSocket({ onTaskEvent, enabled = true }: UseEventsWebSocketProps) {
  const wsRef = useRef<WebSocket | null>(null)
  const onTaskEventRef = useRef(onTaskEvent)
  const reconnectRef = useRef<number | null>(null)
  const unmountedRef = useRef(false)

  useEffect(() => {
    onTaskEventRef.current = onTaskEvent
  }, [onTaskEvent])

  useEffect(() => {
    if (!enabled) return

    unmountedRef.current = false

    const connect = () => {
      if (unmountedRef.current) return
      if (wsRef.current?.readyState === WebSocket.OPEN || wsRef.current?.readyState === WebSocket.CONNECTING) return

      const ws = new WebSocket(WS_URL)
      wsRef.current = ws

      ws.onmessage = (e) => {
        try {
          const msg = JSON.parse(e.data)
          if (msg.type === 'task_created' || msg.type === 'task_updated' || msg.type === 'task_deleted') {
            onTaskEventRef.current?.(msg as TaskEvent)
          }
        } catch {
          // ignore parse errors
        }
      }

      ws.onclose = () => {
        if (!unmountedRef.current) {
          reconnectRef.current = window.setTimeout(connect, 3000)
        }
      }

      ws.onerror = () => {
        ws.close()
      }
    }

    connect()

    return () => {
      unmountedRef.current = true
      if (reconnectRef.current) clearTimeout(reconnectRef.current)
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [enabled])
}
