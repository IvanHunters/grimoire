/**
 * Force-directed graph layout
 *
 * Simple physics simulation for graph visualization:
 * - Repulsion force between all nodes (prevents overlap)
 * - Attraction force between connected nodes (keeps links short)
 * - Center force (pulls nodes toward center)
 * - Damping (reduces oscillation)
 */

export interface ForceNode {
  id: string
  x: number
  y: number
  vx: number // velocity x
  vy: number // velocity y
  fixed?: boolean // don't move this node
}

export interface ForceLink {
  source: string
  target: string
}

export interface ForceLayoutConfig {
  width: number
  height: number
  repulsionStrength?: number // default: 5000
  attractionStrength?: number // default: 0.01
  centerStrength?: number // default: 0.05
  damping?: number // default: 0.8
  iterations?: number // default: 300
}

/**
 * Run force-directed layout simulation
 *
 * Returns nodes with updated positions
 */
export function forceLayout(
  nodes: ForceNode[],
  links: ForceLink[],
  config: ForceLayoutConfig
): ForceNode[] {
  const {
    width,
    height,
    repulsionStrength = 5000,
    attractionStrength = 0.01,
    centerStrength = 0.05,
    damping = 0.8,
    iterations = 300,
  } = config

  const centerX = width / 2
  const centerY = height / 2

  // Clone nodes to avoid mutation
  const nodesCopy = nodes.map(n => ({ ...n }))

  // Build map for fast lookup
  const nodeMap = new Map<string, ForceNode>()
  nodesCopy.forEach(node => nodeMap.set(node.id, node))

  // Run simulation iterations
  for (let iter = 0; iter < iterations; iter++) {
    // Apply repulsion force (all pairs)
    for (let i = 0; i < nodesCopy.length; i++) {
      for (let j = i + 1; j < nodesCopy.length; j++) {
        const nodeA = nodesCopy[i]
        const nodeB = nodesCopy[j]

        const dx = nodeB.x - nodeA.x
        const dy = nodeB.y - nodeA.y
        const distance = Math.sqrt(dx * dx + dy * dy) || 1 // prevent division by zero

        // Repulsion force (inverse square law)
        const force = repulsionStrength / (distance * distance)
        const fx = (dx / distance) * force
        const fy = (dy / distance) * force

        if (!nodeA.fixed) {
          nodeA.vx -= fx
          nodeA.vy -= fy
        }

        if (!nodeB.fixed) {
          nodeB.vx += fx
          nodeB.vy += fy
        }
      }
    }

    // Apply attraction force (linked nodes)
    links.forEach(link => {
      const source = nodeMap.get(link.source)
      const target = nodeMap.get(link.target)

      if (!source || !target) return

      const dx = target.x - source.x
      const dy = target.y - source.y
      const distance = Math.sqrt(dx * dx + dy * dy) || 1

      // Spring force (Hooke's law)
      const force = attractionStrength * distance
      const fx = (dx / distance) * force
      const fy = (dy / distance) * force

      if (!source.fixed) {
        source.vx += fx
        source.vy += fy
      }

      if (!target.fixed) {
        target.vx -= fx
        target.vy -= fy
      }
    })

    // Apply center force (pull toward center)
    nodesCopy.forEach(node => {
      if (!node.fixed) {
        const dx = centerX - node.x
        const dy = centerY - node.y

        node.vx += dx * centerStrength
        node.vy += dy * centerStrength
      }
    })

    // Update positions and apply damping
    nodesCopy.forEach(node => {
      if (!node.fixed) {
        node.x += node.vx
        node.y += node.vy

        // Apply damping
        node.vx *= damping
        node.vy *= damping

        // Keep nodes within bounds (with padding)
        const padding = 50
        node.x = Math.max(padding, Math.min(width - padding, node.x))
        node.y = Math.max(padding, Math.min(height - padding, node.y))
      }
    })
  }

  return nodesCopy
}

/**
 * Initialize nodes with random positions
 */
export function initializeNodes(
  nodeIds: string[],
  width: number,
  height: number,
  currentNodeId?: string
): ForceNode[] {
  const centerX = width / 2
  const centerY = height / 2
  const radius = Math.min(width, height) * 0.3

  return nodeIds.map((id, index) => {
    // Place current node in center (fixed)
    if (id === currentNodeId) {
      return {
        id,
        x: centerX,
        y: centerY,
        vx: 0,
        vy: 0,
        fixed: true,
      }
    }

    // Place other nodes in a circle around center
    const angle = (index / nodeIds.length) * 2 * Math.PI
    const x = centerX + radius * Math.cos(angle) + (Math.random() - 0.5) * 100
    const y = centerY + radius * Math.sin(angle) + (Math.random() - 0.5) * 100

    return {
      id,
      x,
      y,
      vx: 0,
      vy: 0,
    }
  })
}
