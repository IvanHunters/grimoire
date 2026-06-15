// SessionStatusPill renders a coloured dot + label that mirrors the
// daemon's live state for a session. Shared by the main Sidebar and
// the Tasks page sidebar so both menus look identical.
//
// Logic priority (tempo > state because tempo reflects what claude
// is doing right NOW; state lags on lifecycle events):
//   - state=failed  → "failed" (red)
//   - state=stopped → "stopped" (slate)
//   - tempo=active  → "working" (cyan, pulse) — claude IS emitting
//   - tempo=blocked OR state=blocked → "needs you" (amber, pulse)
//   - state=done/working/running → "ready" (emerald)
//   - else          → raw state/tempo or nothing
import { JSX } from 'react'

export interface SessionStatusPillProps {
  state?: string
  tempo?: string
  detail?: string
  needs?: string
}

export function SessionStatusPill({ state, tempo, detail, needs }: SessionStatusPillProps): JSX.Element | null {
  if (!state && !tempo) return null
  let color = 'bg-slate-500'
  let label = ''
  if (state === 'failed') {
    color = 'bg-rose-500'
    label = 'failed'
  } else if (state === 'stopped') {
    color = 'bg-slate-600'
    label = 'stopped'
  } else if (tempo === 'active') {
    color = 'bg-cyan-400 animate-pulse'
    label = 'working'
  } else if (tempo === 'blocked' || state === 'blocked') {
    color = 'bg-amber-400 animate-pulse'
    label = 'needs you'
  } else if (state === 'done' || state === 'working' || state === 'running') {
    color = 'bg-emerald-600/80'
    label = 'ready'
  } else {
    label = state || tempo || ''
  }
  const tip = [detail, needs].filter(Boolean).join(' · ') || label
  return (
    <span
      className="flex items-center gap-1 flex-shrink-0 text-[9px] font-mono uppercase tracking-wider text-slate-500"
      title={tip}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${color}`} />
      <span className="truncate max-w-[60px]">{label}</span>
    </span>
  )
}

// formatSessionAge — same compact "5s / 3m / 2h / 4d / 2mo" format
// the main sidebar uses for timestamps.
export function formatSessionAge(iso: string): string {
  if (!iso) return ''
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d}d`
  return `${Math.floor(d / 30)}mo`
}
