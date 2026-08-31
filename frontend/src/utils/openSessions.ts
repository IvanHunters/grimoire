// openSessions is a tiny in-memory registry of session ids currently
// open in a live UI surface — a resume/attach chat modal, a chat panel,
// etc. The sidebar subscribes to it so it can highlight "what's open"
// beyond the Quick Terminal tabs (tracked separately in localStorage)
// and the note chat (tracked via activeSessionId).
//
// Deliberately not localStorage-backed: these are per-window runtime
// states, not something to persist across reloads.

type Listener = () => void

const open = new Set<string>()
const listeners = new Set<Listener>()

function emit() {
  listeners.forEach((l) => l())
}

/** Mark a session as open in some UI surface. No-op for empty ids. */
export function markSessionOpen(id: string | null | undefined) {
  if (!id) return
  if (!open.has(id)) {
    open.add(id)
    emit()
  }
}

/** Mark a session as no longer open. */
export function markSessionClosed(id: string | null | undefined) {
  if (!id) return
  if (open.delete(id)) emit()
}

/** Snapshot of currently-open session ids. */
export function getOpenSessions(): Set<string> {
  return new Set(open)
}

/** Subscribe to changes; returns an unsubscribe fn. */
export function subscribeOpenSessions(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
