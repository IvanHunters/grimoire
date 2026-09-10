// Shared wording for what a compaction actually did.
//
// The three places that can trigger Compact used to phrase the result
// as "N/M tool results evicted". Once a session has been compacted a
// few times that number is zero even on a successful run — every
// payload is already a stub, and the work happens in re-compressing
// those stubs instead. Reporting only the eviction count made a run
// that freed hundreds of thousands of tokens read as "nothing
// happened", which is the complaint this wording exists to answer.

export interface CompactStats {
  no_change?: boolean
  bytes_before: number
  bytes_after: number
  tool_results?: number
  tool_results_evicted?: number
  tool_uses_evicted?: number
  stubs_recompressed?: number
  thinking_blocks_dropped?: number
  usage_blocks_dropped?: number
  attachments_dropped?: number
  ledger_path?: string
}

const mb = (b: number) => `${(b / 1e6).toFixed(2)} MB`

export function formatCompactResult(s: CompactStats): string {
  if (s.no_change) {
    return 'Nothing to compact: this session is already minimal. What is left is conversation text, not evictable tool output.'
  }

  const did: string[] = []
  const add = (n: number | undefined, one: string, many: string) => {
    if (n && n > 0) did.push(`${n} ${n === 1 ? one : many}`)
  }
  add(s.tool_results_evicted, 'tool result evicted', 'tool results evicted')
  add(s.tool_uses_evicted, 'tool call trimmed', 'tool calls trimmed')
  add(s.stubs_recompressed, 'stub re-compressed', 'stubs re-compressed')
  add(s.thinking_blocks_dropped, 'thinking block dropped', 'thinking blocks dropped')
  add(s.attachments_dropped, 'attachment dropped', 'attachments dropped')
  add(s.usage_blocks_dropped, 'usage record dropped', 'usage records dropped')

  const head = `Compacted: ${mb(s.bytes_before)} to ${mb(s.bytes_after)}`
  const detail = did.length > 0 ? ` (${did.join(', ')})` : ''
  const tail = '\n\nThe smaller context takes effect on the next resume of this session.'
  return `${head}${detail}.${tail}`
}
