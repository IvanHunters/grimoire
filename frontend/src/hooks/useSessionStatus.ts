import { useEffect, useState } from 'react'
import { sessionsAPI, type SessionStatus } from '../api/sessions'

interface Options {
  /** Polling interval in ms. Default 2000. */
  intervalMs?: number
  /** Set to false to pause polling (e.g. when chat panel is closed). */
  enabled?: boolean
}

/**
 * useSessionStatus polls GET /sessions/:id/status while enabled.
 *
 * Returns the latest status snapshot plus a `loading` flag for the first
 * fetch and an `error` field when the daemon or backend hiccups. Stops
 * polling on unmount or when `enabled` toggles false.
 *
 * Don't drive critical UI logic off `error`: a transient blip should not
 * close the chat. Just degrade the indicator to "unknown".
 */
export function useSessionStatus(sessionId: string | undefined, opts: Options = {}) {
  const { intervalMs = 2000, enabled = true } = opts
  const [status, setStatus] = useState<SessionStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    if (!sessionId || !enabled) {
      setLoading(false)
      return
    }
    let cancelled = false

    const tick = async () => {
      try {
        const s = await sessionsAPI.getStatus(sessionId)
        if (!cancelled) {
          setStatus(s)
          setError(null)
        }
      } catch (e) {
        if (!cancelled) setError(e)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    tick()
    const id = setInterval(tick, intervalMs)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [sessionId, enabled, intervalMs])

  return { status, loading, error }
}
