/**
 * Wikilinks parser - extracts [[link]] and [[link|alias]] from markdown
 * Based on design-prototype.html wikilink logic
 */

export interface Wikilink {
  target: string // Note title or path to link to
  displayText: string // Text to show (alias or target)
  startIndex: number // Position in original text
  endIndex: number // End position in original text
  raw: string // Original wikilink text: [[target|alias]]
}

/**
 * Regex for wikilinks:
 * [[target]] or [[target|alias]]
 */
const WIKILINK_REGEX = /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g

/**
 * Parse all wikilinks from markdown content
 */
export function parseWikilinks(content: string): Wikilink[] {
  const links: Wikilink[] = []
  let match: RegExpExecArray | null

  // Reset regex lastIndex
  WIKILINK_REGEX.lastIndex = 0

  while ((match = WIKILINK_REGEX.exec(content)) !== null) {
    const target = match[1].trim()
    const alias = match[2]?.trim()
    const displayText = alias || target

    links.push({
      target,
      displayText,
      startIndex: match.index,
      endIndex: match.index + match[0].length,
      raw: match[0],
    })
  }

  return links
}

/**
 * Replace wikilinks in content with HTML links
 * Used for preview rendering
 */
export function replaceWikilinksWithHTML(content: string): string {
  return content.replace(WIKILINK_REGEX, (_match, target, alias) => {
    const displayText = alias?.trim() || target.trim()
    const targetPath = target.trim()

    // Create HTML link with data attributes for React to handle
    return `<a href="#" class="wikilink" data-target="${targetPath}">${displayText}</a>`
  })
}

/**
 * Extract just the target paths from content (for building graph)
 */
export function extractWikilinkTargets(content: string): string[] {
  const links = parseWikilinks(content)
  return links.map((link) => link.target)
}

/**
 * Check if text contains any wikilinks
 */
export function hasWikilinks(content: string): boolean {
  WIKILINK_REGEX.lastIndex = 0
  return WIKILINK_REGEX.test(content)
}

/**
 * Resolve wikilink target to note ID
 *
 * Tries:
 * 1. Exact path match (e.g., "projects/web-app")
 * 2. Exact title match (case-insensitive)
 * 3. Partial title match (case-insensitive)
 */
export function resolveWikilinkTarget(
  target: string,
  notes: Array<{ id: string; title: string; path: string }>
): string | null {
  const targetLower = target.toLowerCase()

  // 1. Exact path match (without .md extension)
  const pathMatch = notes.find(note => {
    const pathWithoutExt = note.path.replace(/\.md$/, '')
    return pathWithoutExt.toLowerCase() === targetLower
  })
  if (pathMatch) return pathMatch.id

  // 2. Exact title match
  const titleMatch = notes.find(note => note.title.toLowerCase() === targetLower)
  if (titleMatch) return titleMatch.id

  // 3. Partial title match (contains)
  const partialMatch = notes.find(note =>
    note.title.toLowerCase().includes(targetLower)
  )
  if (partialMatch) return partialMatch.id

  return null
}

/**
 * Build graph connections from notes
 *
 * Returns map: noteId -> array of connected note IDs
 */
export function buildGraphConnections(
  notes: Array<{ id: string; title: string; path: string; content: string }>
): Map<string, string[]> {
  const connections = new Map<string, string[]>()

  // Initialize empty arrays for all notes
  notes.forEach(note => {
    connections.set(note.id, [])
  })

  // Parse wikilinks and build connections
  notes.forEach(note => {
    const wikilinks = parseWikilinks(note.content)
    const targets: string[] = []

    wikilinks.forEach(link => {
      const targetId = resolveWikilinkTarget(link.target, notes)
      if (targetId && targetId !== note.id) {
        targets.push(targetId)
      }
    })

    // Remove duplicates
    connections.set(note.id, [...new Set(targets)])
  })

  return connections
}

/**
 * Get all notes connected to a specific note (with depth limit)
 *
 * @param noteId - Starting note ID
 * @param connections - Graph connections map
 * @param maxDepth - Maximum connection depth (default: 2)
 */
export function getConnectedNotes(
  noteId: string,
  connections: Map<string, string[]>,
  maxDepth: number = 2
): Set<string> {
  const connected = new Set<string>()
  const queue: Array<{ id: string; depth: number }> = [{ id: noteId, depth: 0 }]
  const visited = new Set<string>()

  while (queue.length > 0) {
    const { id, depth } = queue.shift()!

    if (visited.has(id) || depth > maxDepth) continue
    visited.add(id)

    if (id !== noteId) {
      connected.add(id)
    }

    if (depth < maxDepth) {
      const targets = connections.get(id) || []
      targets.forEach(targetId => {
        if (!visited.has(targetId)) {
          queue.push({ id: targetId, depth: depth + 1 })
        }
      })
    }
  }

  return connected
}
