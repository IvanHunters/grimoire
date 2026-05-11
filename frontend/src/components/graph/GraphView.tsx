import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useNotes } from '../../contexts/NotesContext'
import { X, RefreshCw, ChevronDown } from 'lucide-react'
import { buildGraphConnections, getConnectedNotes } from '../../utils/wikilinks'
import { forceLayout, initializeNodes, type ForceNode } from '../../utils/forceLayout'

interface GraphViewProps {
  visible: boolean
  onClose: () => void
}

interface GraphNode {
  id: string
  label: string
  x: number
  y: number
  isCurrent: boolean
  connectionCount: number // Number of connections (for sizing)
}

interface GraphLink {
  source: string
  target: string
  type: 'wikilink' | 'tag'
  sharedTags?: string[]
}

/**
 * GraphView - Interactive visualization of note connections
 *
 * Features:
 * - Wikilinks parsing from note content
 * - Force-directed layout
 * - Interactive navigation
 */
function GraphView({ visible, onClose }: GraphViewProps) {
  const { currentNote, notes } = useNotes()
  const navigate = useNavigate()
  const svgRef = useRef<SVGSVGElement>(null)
  const containerRef = useRef<SVGGElement>(null)
  const [showAll, setShowAll] = useState(false)
  const [filterByFolder, setFilterByFolder] = useState(true) // Filter by current note's folder
  const [showWikilinks, setShowWikilinks] = useState(true) // Show wikilink connections
  const [showTagConnections, setShowTagConnections] = useState(false) // Show tag-based connections (default: off)
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set()) // Filter notes by tags (empty = show all)
  const [isTagFilterOpen, setIsTagFilterOpen] = useState(false) // Tag filter dropdown state
  const [transform, setTransform] = useState({ x: 0, y: 0, scale: 1 })
  const [isPanning, setIsPanning] = useState(false)
  const [panStart, setPanStart] = useState({ x: 0, y: 0 })
  const [draggedNode, setDraggedNode] = useState<string | null>(null)
  const dragStartPosRef = useRef<{ x: number; y: number } | null>(null) // Use ref for synchronous access
  const [nodePositions, setNodePositions] = useState<Map<string, { x: number; y: number }>>(new Map())
  const [graphNodes, setGraphNodes] = useState<GraphNode[]>([])
  const [graphLinks, setGraphLinks] = useState<GraphLink[]>([])
  const [allTags, setAllTags] = useState<string[]>([]) // All unique tags from filtered notes

  useEffect(() => {
    if (visible && svgRef.current) {
      // Reset custom positions when graph changes
      setNodePositions(new Map())
      renderGraph()
    }
  }, [visible, currentNote, notes, showAll, filterByFolder, showWikilinks, showTagConnections, selectedTags])

  // Update transform without re-rendering the entire graph
  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.setAttribute(
        'transform',
        `translate(${transform.x}, ${transform.y}) scale(${transform.scale})`
      )
    }
  }, [transform])

  // Close tag filter dropdown when clicking outside
  useEffect(() => {
    if (!isTagFilterOpen) return

    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (!target.closest('.tag-filter-dropdown')) {
        setIsTagFilterOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [isTagFilterOpen])

  const renderGraph = () => {
    const svg = svgRef.current
    if (!svg) return

    const width = svg.clientWidth
    const height = svg.clientHeight

    // Detect dark mode
    const isDarkMode = document.documentElement.classList.contains('dark')
    const textColor = isDarkMode ? '#e5e7eb' : '#1f2937' // gray-200 : gray-800
    const linkColor = isDarkMode ? '#6b7280' : '#cbd5e1' // gray-500 : gray-300

    // Clear previous graph
    svg.innerHTML = ''

    // Create container group for zoom/pan transform
    const g = document.createElementNS('http://www.w3.org/2000/svg', 'g')
    g.setAttribute('transform', `translate(${transform.x}, ${transform.y}) scale(${transform.scale})`)
    svg.appendChild(g)
    containerRef.current = g // Save ref for transform updates

    const currentNoteId = currentNote?.id || notes[0]?.id

    if (!currentNoteId) {
      // No notes, show empty state
      const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
      text.setAttribute('x', String(width / 2))
      text.setAttribute('y', String(height / 2))
      text.setAttribute('text-anchor', 'middle')
      text.setAttribute('fill', '#9ca3af')
      text.setAttribute('font-size', '16')
      text.textContent = 'No notes yet. Create your first note!'
      g.appendChild(text)
      return
    }

    // Filter notes by folder if enabled
    let filteredNotes = notes
    if (filterByFolder && currentNote) {
      // Get the top-level folder (e.g., "Projects" from "Projects/Aenix/Note.md")
      const currentFolder = currentNote.folder
      if (currentFolder) {
        const topLevelFolder = currentFolder.split('/')[0]
        // Show only notes from the same top-level folder
        filteredNotes = notes.filter(n => {
          if (!n.folder) return false
          return n.folder === currentFolder || n.folder.startsWith(currentFolder + '/') || n.folder.startsWith(topLevelFolder + '/')
        })
        // Always include current note
        if (!filteredNotes.find(n => n.id === currentNoteId)) {
          filteredNotes = [currentNote, ...filteredNotes]
        }
      }
    }

    // Extract all unique tags from notes BEFORE filtering by tags
    // This ensures selected tags remain visible in the dropdown
    const tags = Array.from(
      new Set(filteredNotes.flatMap(note => note.tags || []))
    ).sort()
    setAllTags(tags)

    // Filter notes by selected tags if any
    if (selectedTags.size > 0) {
      filteredNotes = filteredNotes.filter(note => {
        if (!note.tags || note.tags.length === 0) return false
        // Note must have ALL selected tags (AND logic)
        return Array.from(selectedTags).every(tag => note.tags!.includes(tag))
      })
    }

    // Build graph connections from wikilinks (using filtered notes)
    const connections = buildGraphConnections(filteredNotes)

    // Get nodes to display
    let nodeIds: string[]
    // If tags are selected or showAll is enabled, show all filtered notes
    // Otherwise show only connected notes (current + neighbors)
    if (showAll || selectedTags.size > 0) {
      // Show all filtered notes
      nodeIds = filteredNotes.map(n => n.id)
    } else {
      // Show only connected notes (current + connected)
      const connectedIds = getConnectedNotes(currentNoteId, connections, 2)
      nodeIds = [currentNoteId, ...Array.from(connectedIds)]
    }

    // Build links from connections
    const allLinks: GraphLink[] = []
    nodeIds.forEach(noteId => {
      const conns = connections.get(noteId) || []
      conns.forEach(conn => {
        if (nodeIds.includes(conn.targetId)) {
          allLinks.push({
            source: noteId,
            target: conn.targetId,
            type: conn.type,
            sharedTags: conn.sharedTags,
          })
        }
      })
    })

    // Filter links by type
    const links = allLinks.filter(link => {
      if (link.type === 'wikilink' && !showWikilinks) return false
      if (link.type === 'tag' && !showTagConnections) return false
      return true
    })

    // Initialize nodes with random positions
    const forceNodes: ForceNode[] = initializeNodes(nodeIds, width, height, currentNoteId)

    // Add connection count to each node for weighted center force
    forceNodes.forEach(node => {
      const connectionCount = links.filter(
        link => link.source === node.id || link.target === node.id
      ).length
      node.connectionCount = connectionCount
    })

    // Run force-directed layout with strong repulsion and collision detection
    // Hub nodes (high connection count) are pulled stronger to center
    // Peripheral nodes (low connection count) stay on edges
    // Graph can extend beyond viewport - use pan/zoom to navigate
    const layoutedNodes = forceLayout(forceNodes, links, {
      width,
      height,
      repulsionStrength: 80000, // Very strong repulsion for spacious layout
      attractionStrength: 0.004, // Weaker attraction for longer links
      centerStrength: 0.025, // Gentle center pull (weighted by connection count)
      damping: 0.9, // Higher damping for stable layout
      iterations: 600, // More iterations for convergence
    })

    // Convert to GraphNode format, using custom positions if available
    const nodes: GraphNode[] = layoutedNodes.map(node => {
      const noteData = notes.find(n => n.id === node.id)
      const customPos = nodePositions.get(node.id)

      // Count total connections (outgoing + incoming)
      const outgoing = connections.get(node.id)?.length || 0
      const incoming = links.filter(l => l.target === node.id).length
      const connectionCount = outgoing + incoming

      return {
        id: node.id,
        label: noteData?.title || 'Unknown',
        x: customPos?.x ?? node.x,
        y: customPos?.y ?? node.y,
        isCurrent: node.id === currentNoteId,
        connectionCount,
      }
    })

    // Save nodes and links to state for drag updates
    setGraphNodes(nodes)
    setGraphLinks(links)

    // Draw links
    links.forEach((link, index) => {
      const sourceNode = nodes.find(n => n.id === link.source)
      const targetNode = nodes.find(n => n.id === link.target)

      if (!sourceNode || !targetNode) return

      const line = document.createElementNS('http://www.w3.org/2000/svg', 'line')
      line.setAttribute('x1', String(sourceNode.x))
      line.setAttribute('y1', String(sourceNode.y))
      line.setAttribute('x2', String(targetNode.x))
      line.setAttribute('y2', String(targetNode.y))
      line.setAttribute('stroke', link.type === 'wikilink' ? linkColor : '#93c5fd') // Blue for tag-based
      line.setAttribute('stroke-width', '1.5')
      line.setAttribute('data-link-index', String(index))

      // Dashed line for tag-based connections
      if (link.type === 'tag') {
        line.setAttribute('stroke-dasharray', '4 2')
      }

      // Add title for tooltip
      const title = document.createElementNS('http://www.w3.org/2000/svg', 'title')
      if (link.type === 'wikilink') {
        title.textContent = 'Wikilink'
      } else if (link.sharedTags && link.sharedTags.length > 0) {
        title.textContent = `Tags: ${link.sharedTags.join(', ')}`
      }
      line.appendChild(title)

      g.appendChild(line)

      // Draw tag labels on tag-based connections
      if (link.type === 'tag' && link.sharedTags && link.sharedTags.length > 0) {
        const midX = (sourceNode.x + targetNode.x) / 2
        const midY = (sourceNode.y + targetNode.y) / 2

        const tagText = link.sharedTags.slice(0, 2).join(', ') // Show max 2 tags

        // Create text first to measure width
        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        text.setAttribute('x', String(midX))
        text.setAttribute('y', String(midY + 4))
        text.setAttribute('text-anchor', 'middle')
        text.setAttribute('fill', '#3b82f6')
        text.setAttribute('font-size', '10')
        text.setAttribute('font-weight', '600')
        text.setAttribute('class', 'tag-label-text')
        text.setAttribute('data-link-index', String(index))
        text.textContent = tagText
        text.style.pointerEvents = 'none'
        g.appendChild(text)

        // Calculate background width based on text length
        const textWidth = text.getComputedTextLength()
        const labelWidth = textWidth + 12 // Add padding
        const labelHeight = 18

        // Background rect for better readability
        const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
        rect.setAttribute('x', String(midX - labelWidth / 2))
        rect.setAttribute('y', String(midY - 10))
        rect.setAttribute('width', String(labelWidth))
        rect.setAttribute('height', String(labelHeight))
        rect.setAttribute('fill', isDarkMode ? '#1f2937' : 'white')
        rect.setAttribute('stroke', '#93c5fd')
        rect.setAttribute('stroke-width', '1')
        rect.setAttribute('rx', '3')
        rect.setAttribute('opacity', '0.9')
        rect.setAttribute('class', 'tag-label-bg')
        rect.setAttribute('data-link-index', String(index))
        // Insert rect before text
        g.insertBefore(rect, text)
      }
    })

    // Create hover tooltip group
    const tooltipGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g')
    tooltipGroup.setAttribute('id', 'graph-tooltip')
    tooltipGroup.style.pointerEvents = 'none'
    tooltipGroup.style.display = 'none'
    g.appendChild(tooltipGroup)

    // Draw nodes
    nodes.forEach(node => {
      // Dynamic radius based on connections (min 8, max 20)
      const baseRadius = 8
      const maxRadius = 20
      const radius = Math.min(maxRadius, baseRadius + Math.sqrt(node.connectionCount) * 2)

      // Circle
      const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
      circle.setAttribute('cx', String(node.x))
      circle.setAttribute('cy', String(node.y))
      circle.setAttribute('r', String(radius))
      circle.setAttribute('fill', node.isCurrent ? '#f59e0b' : '#8b5cf6') // Orange for current, purple for others
      circle.setAttribute('stroke', 'white')
      circle.setAttribute('stroke-width', node.isCurrent ? '2.5' : '1.5')
      circle.setAttribute('data-node-id', node.id)
      circle.style.cursor = 'move'
      circle.style.transition = 'all 0.2s ease'

      // Hover effect - show tooltip and highlight connections
      circle.addEventListener('mouseenter', () => {
        circle.setAttribute('stroke-width', '3')
        circle.style.filter = 'brightness(1.2)'

        // Highlight connected nodes and links
        const connectedNodeIds = new Set<string>()

        // Find all links connected to this node
        links.forEach((link, index) => {
          const isConnected = link.source === node.id || link.target === node.id

          if (isConnected) {
            // Highlight link
            const line = g.querySelector(`line[data-link-index="${index}"]`) as SVGLineElement
            if (line) {
              line.setAttribute('stroke-width', '3')
              line.setAttribute('opacity', '1')
            }

            // Mark connected nodes
            if (link.source === node.id) connectedNodeIds.add(link.target)
            if (link.target === node.id) connectedNodeIds.add(link.source)
          } else {
            // Dim other links
            const line = g.querySelector(`line[data-link-index="${index}"]`) as SVGLineElement
            if (line) {
              line.setAttribute('opacity', '0.2')
            }
          }
        })

        // Highlight connected nodes, dim others
        nodes.forEach(n => {
          if (n.id === node.id) return // Skip self

          const nodeCircle = g.querySelector(`circle[data-node-id="${n.id}"]`) as SVGCircleElement
          if (nodeCircle) {
            if (connectedNodeIds.has(n.id)) {
              nodeCircle.setAttribute('opacity', '1')
              nodeCircle.style.filter = 'brightness(1.1)'
            } else {
              nodeCircle.setAttribute('opacity', '0.3')
            }
          }
        })

        // Show tooltip
        tooltipGroup.innerHTML = ''
        tooltipGroup.style.display = 'block'

        // Get current position from circle (may be updated after drag)
        const currentX = parseFloat(circle.getAttribute('cx') || String(node.x))
        const currentY = parseFloat(circle.getAttribute('cy') || String(node.y))

        // Tooltip text (create first to measure width)
        const tooltipText = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        tooltipText.setAttribute('x', String(currentX))
        tooltipText.setAttribute('y', String(currentY - radius - 16))
        tooltipText.setAttribute('text-anchor', 'middle')
        tooltipText.setAttribute('fill', textColor)
        tooltipText.setAttribute('font-size', '12')
        tooltipText.setAttribute('font-weight', '600')
        tooltipText.textContent = node.label
        tooltipGroup.appendChild(tooltipText)

        // Calculate tooltip background width based on text length
        const textWidth = tooltipText.getComputedTextLength()
        const tooltipWidth = textWidth + 16 // Add padding
        const tooltipHeight = 24

        // Tooltip background
        const tooltipBg = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
        tooltipBg.setAttribute('x', String(currentX - tooltipWidth / 2))
        tooltipBg.setAttribute('y', String(currentY - radius - 32))
        tooltipBg.setAttribute('width', String(tooltipWidth))
        tooltipBg.setAttribute('height', String(tooltipHeight))
        tooltipBg.setAttribute('fill', isDarkMode ? '#1f2937' : 'white')
        tooltipBg.setAttribute('stroke', isDarkMode ? '#4b5563' : '#d1d5db')
        tooltipBg.setAttribute('stroke-width', '1')
        tooltipBg.setAttribute('rx', '4')
        tooltipBg.setAttribute('opacity', '0.95')
        // Insert background before text
        tooltipGroup.insertBefore(tooltipBg, tooltipText)
      })
      circle.addEventListener('mouseleave', () => {
        circle.setAttribute('stroke-width', node.isCurrent ? '2.5' : '1.5')
        circle.style.filter = 'brightness(1)'
        tooltipGroup.style.display = 'none'

        // Reset all highlights
        links.forEach((_, index) => {
          const line = g.querySelector(`line[data-link-index="${index}"]`) as SVGLineElement
          if (line) {
            line.setAttribute('stroke-width', '1.5')
            line.setAttribute('opacity', '1')
          }
        })

        nodes.forEach(n => {
          const nodeCircle = g.querySelector(`circle[data-node-id="${n.id}"]`) as SVGCircleElement
          if (nodeCircle) {
            nodeCircle.setAttribute('opacity', '1')
            nodeCircle.style.filter = ''
          }
        })
      })

      // Click to navigate
      circle.addEventListener('click', (e) => {
        // Check if mouse moved from original position (drag vs click)
        if (dragStartPosRef.current) {
          const dx = e.clientX - dragStartPosRef.current.x
          const dy = e.clientY - dragStartPosRef.current.y
          const distance = Math.sqrt(dx * dx + dy * dy)

          // Only navigate if movement was less than 5px (click, not drag)
          if (distance < 5) {
            const noteToOpen = notes.find(n => n.id === node.id)
            if (noteToOpen) {
              navigate(`/notes/${noteToOpen.id}`)
              onClose()
            }
          }
        }
      })

      // Drag handlers for moving nodes
      circle.addEventListener('mousedown', (e) => {
        e.stopPropagation() // Prevent pan
        setDraggedNode(node.id)
        dragStartPosRef.current = { x: e.clientX, y: e.clientY }
        // Disable transition during drag for immediate visual feedback
        circle.style.transition = 'none'
        // Tooltip will be updated during drag by updateNodeVisuals
      })

      g.appendChild(circle)

      // Label only for current note (always visible)
      if (node.isCurrent) {
        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        text.setAttribute('x', String(node.x))
        text.setAttribute('y', String(node.y + radius + 16))
        text.setAttribute('text-anchor', 'middle')
        text.setAttribute('fill', textColor)
        text.setAttribute('font-size', '12')
        text.setAttribute('font-weight', '700')
        text.setAttribute('data-node-id', node.id)
        text.textContent = node.label
        text.style.pointerEvents = 'none'
        g.appendChild(text)
      }
    })
  }

  // Zoom handler (mouse wheel)
  const handleWheel = (e: React.WheelEvent<SVGSVGElement>) => {
    e.preventDefault()

    const delta = e.deltaY > 0 ? 0.9 : 1.1 // Zoom out/in
    const newScale = Math.max(0.1, Math.min(5, transform.scale * delta))

    // Zoom towards mouse position
    const svg = svgRef.current
    if (!svg) return

    const rect = svg.getBoundingClientRect()
    const mouseX = e.clientX - rect.left
    const mouseY = e.clientY - rect.top

    // Adjust pan to zoom towards mouse
    const newX = mouseX - (mouseX - transform.x) * (newScale / transform.scale)
    const newY = mouseY - (mouseY - transform.y) * (newScale / transform.scale)

    setTransform({ x: newX, y: newY, scale: newScale })
  }

  // Pan handlers (mouse drag)
  const handleMouseDown = (e: React.MouseEvent<SVGSVGElement>) => {
    const target = e.target as SVGElement
    // Only pan with left mouse button and not clicking on nodes
    // Nodes handle their own mousedown for dragging
    if (e.button === 0 && target.tagName !== 'circle' && target.tagName !== 'text') {
      setIsPanning(true)
      setPanStart({ x: e.clientX - transform.x, y: e.clientY - transform.y })
      e.preventDefault()
    }
  }

  const handleMouseMove = (e: React.MouseEvent<SVGSVGElement>) => {
    if (draggedNode) {
      const svg = svgRef.current
      if (!svg) return

      const rect = svg.getBoundingClientRect()
      // Convert mouse position to SVG coordinates (accounting for transform)
      const x = (e.clientX - rect.left - transform.x) / transform.scale
      const y = (e.clientY - rect.top - transform.y) / transform.scale

      // Update node position
      setNodePositions(prev => {
        const updated = new Map(prev)
        updated.set(draggedNode, { x, y })
        return updated
      })

      // Update visual position immediately
      updateNodeVisuals(draggedNode, x, y)
    } else if (isPanning) {
      // Panning the canvas
      setTransform(prev => ({
        ...prev,
        x: e.clientX - panStart.x,
        y: e.clientY - panStart.y,
      }))
    }
  }

  const handleMouseUp = () => {
    // Re-enable transition on dragged node
    if (draggedNode && containerRef.current) {
      const circle = containerRef.current.querySelector(`circle[data-node-id="${draggedNode}"]`) as SVGCircleElement
      if (circle) {
        circle.style.transition = 'all 0.2s ease'
      }
    }

    setIsPanning(false)
    setDraggedNode(null)
    // Don't reset dragStartPosRef here - let click handler check it first
    // It will be reset on next mousedown
  }

  // Update node and connected links visually without re-rendering entire graph
  const updateNodeVisuals = (nodeId: string, x: number, y: number) => {
    if (!containerRef.current) return

    const g = containerRef.current

    // Update circle position
    const circle = g.querySelector(`circle[data-node-id="${nodeId}"]`) as SVGCircleElement
    if (circle) {
      circle.setAttribute('cx', String(x))
      circle.setAttribute('cy', String(y))
    }

    // Update text position (only for current note)
    const text = g.querySelector(`text[data-node-id="${nodeId}"]`) as SVGTextElement
    if (text && circle) {
      const radius = parseFloat(circle.getAttribute('r') || '8')
      text.setAttribute('x', String(x))
      text.setAttribute('y', String(y + radius + 16))
    }

    // Update tooltip position if visible (during drag)
    const tooltipGroup = g.querySelector('#graph-tooltip') as SVGGElement
    if (tooltipGroup && tooltipGroup.style.display === 'block' && circle) {
      const radius = parseFloat(circle.getAttribute('r') || '8')

      // Update tooltip text
      const tooltipText = tooltipGroup.querySelector('text') as SVGTextElement
      if (tooltipText) {
        tooltipText.setAttribute('x', String(x))
        tooltipText.setAttribute('y', String(y - radius - 16))

        // Recalculate background width and position
        const textWidth = tooltipText.getComputedTextLength()
        const tooltipWidth = textWidth + 16

        const tooltipBg = tooltipGroup.querySelector('rect') as SVGRectElement
        if (tooltipBg) {
          tooltipBg.setAttribute('x', String(x - tooltipWidth / 2))
          tooltipBg.setAttribute('y', String(y - radius - 32))
          tooltipBg.setAttribute('width', String(tooltipWidth))
        }
      }
    }

    // Update connected links
    graphLinks.forEach((link, index) => {
      if (link.source === nodeId || link.target === nodeId) {
        const sourceNode = graphNodes.find(n => n.id === link.source)
        const targetNode = graphNodes.find(n => n.id === link.target)

        if (sourceNode && targetNode) {
          const sourcePos = link.source === nodeId ? { x, y } : nodePositions.get(link.source) || { x: sourceNode.x, y: sourceNode.y }
          const targetPos = link.target === nodeId ? { x, y } : nodePositions.get(link.target) || { x: targetNode.x, y: targetNode.y }

          const line = g.querySelector(`line[data-link-index="${index}"]`) as SVGLineElement
          if (line) {
            line.setAttribute('x1', String(sourcePos.x))
            line.setAttribute('y1', String(sourcePos.y))
            line.setAttribute('x2', String(targetPos.x))
            line.setAttribute('y2', String(targetPos.y))
          }

          // Update tag label position for tag-based connections
          if (link.type === 'tag') {
            const midX = (sourcePos.x + targetPos.x) / 2
            const midY = (sourcePos.y + targetPos.y) / 2

            // Update text label position
            const textLabel = g.querySelector(`text.tag-label-text[data-link-index="${index}"]`) as SVGTextElement
            if (textLabel) {
              textLabel.setAttribute('x', String(midX))
              textLabel.setAttribute('y', String(midY + 4))

              // Recalculate background width based on text
              const textWidth = textLabel.getComputedTextLength()
              const labelWidth = textWidth + 12 // Add padding

              // Update rect background
              const rect = g.querySelector(`rect.tag-label-bg[data-link-index="${index}"]`) as SVGRectElement
              if (rect) {
                rect.setAttribute('x', String(midX - labelWidth / 2))
                rect.setAttribute('y', String(midY - 10))
                rect.setAttribute('width', String(labelWidth))
              }
            }
          }
        }
      }
    })
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  if (!visible) return null

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 z-[2000] flex items-center justify-center"
      onClick={handleOverlayClick}
    >
      <div
        className="w-[90%] h-[85%] bg-white dark:bg-gray-800 rounded-xl flex flex-col shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="p-4 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold flex items-center gap-2 text-gray-900 dark:text-white">
              <i className="fas fa-project-diagram text-blue-600 dark:text-blue-400"></i>
              Graph View - Note Connections
            </h2>
            <p className="text-sm text-gray-600 dark:text-gray-400 mt-1">
              {showAll
                ? `Showing all ${notes.length} notes`
                : 'Showing current note and connected notes'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setFilterByFolder(!filterByFolder)}
              className={`px-3 py-1.5 text-sm rounded-lg transition text-gray-700 dark:text-gray-300 ${
                filterByFolder
                  ? 'bg-purple-100 dark:bg-purple-900 hover:bg-purple-200 dark:hover:bg-purple-800'
                  : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
              }`}
              title={filterByFolder ? 'Showing notes from current folder' : 'Showing all folders'}
            >
              📁 {filterByFolder ? 'Current Folder' : 'All Folders'}
            </button>
            <button
              onClick={() => setShowWikilinks(!showWikilinks)}
              className={`px-3 py-1.5 text-sm rounded-lg transition text-gray-700 dark:text-gray-300 ${
                showWikilinks
                  ? 'bg-blue-100 dark:bg-blue-900 hover:bg-blue-200 dark:hover:bg-blue-800'
                  : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
              }`}
              title={showWikilinks ? 'Hiding wikilink connections' : 'Showing wikilink connections'}
            >
              🔗 Wikilinks
            </button>
            <button
              onClick={() => setShowTagConnections(!showTagConnections)}
              className={`px-3 py-1.5 text-sm rounded-lg transition text-gray-700 dark:text-gray-300 ${
                showTagConnections
                  ? 'bg-green-100 dark:bg-green-900 hover:bg-green-200 dark:hover:bg-green-800'
                  : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
              }`}
              title={showTagConnections ? 'Hiding tag-based connections' : 'Showing tag-based connections'}
            >
              🏷️ Tags
            </button>

            {/* Tag filter dropdown */}
            <div className="relative tag-filter-dropdown">
              <button
                onClick={() => setIsTagFilterOpen(!isTagFilterOpen)}
                className={`px-3 py-1.5 text-sm rounded-lg transition text-gray-700 dark:text-gray-300 flex items-center gap-1 ${
                  selectedTags.size > 0
                    ? 'bg-orange-100 dark:bg-orange-900 hover:bg-orange-200 dark:hover:bg-orange-800'
                    : 'bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600'
                }`}
                title="Filter by tags"
              >
                🎯 Filter Tags {selectedTags.size > 0 && `(${selectedTags.size})`}
                <ChevronDown className={`w-4 h-4 transition-transform ${isTagFilterOpen ? 'rotate-180' : ''}`} />
              </button>

              {isTagFilterOpen && allTags.length > 0 && (
                <div className="absolute top-full right-0 mt-2 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50 min-w-[200px] max-h-[400px] overflow-y-auto">
                  <div className="p-2 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
                    <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
                      {allTags.length} tags available
                    </span>
                    {selectedTags.size > 0 && (
                      <button
                        onClick={() => setSelectedTags(new Set())}
                        className="text-xs text-blue-600 dark:text-blue-400 hover:underline"
                      >
                        Clear all
                      </button>
                    )}
                  </div>
                  <div className="p-2">
                    {allTags.map(tag => (
                      <label
                        key={tag}
                        className="flex items-center gap-2 px-2 py-1.5 hover:bg-gray-100 dark:hover:bg-gray-700 rounded cursor-pointer"
                      >
                        <input
                          type="checkbox"
                          checked={selectedTags.has(tag)}
                          onChange={(e) => {
                            const newTags = new Set(selectedTags)
                            if (e.target.checked) {
                              newTags.add(tag)
                            } else {
                              newTags.delete(tag)
                            }
                            setSelectedTags(newTags)
                          }}
                          className="rounded border-gray-300 dark:border-gray-600"
                        />
                        <span className="text-sm text-gray-700 dark:text-gray-300">{tag}</span>
                      </label>
                    ))}
                  </div>
                </div>
              )}
            </div>

            <button
              onClick={() => setTransform({ x: 0, y: 0, scale: 1 })}
              className="px-3 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition text-gray-700 dark:text-gray-300"
              title="Reset zoom and pan"
            >
              Reset View
            </button>
            <button
              onClick={() => setShowAll(!showAll)}
              className="px-3 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition flex items-center gap-2 text-gray-700 dark:text-gray-300"
            >
              <RefreshCw className="w-4 h-4" />
              {showAll ? 'Show Connected' : 'Show All'}
            </button>
            <button
              onClick={onClose}
              className="p-2 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition"
            >
              <X className="w-5 h-5 text-gray-600 dark:text-gray-400" />
            </button>
          </div>
        </div>

        {/* Graph canvas */}
        <div className="flex-1 relative bg-gradient-to-br from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
          <svg
            ref={svgRef}
            className="w-full h-full"
            onWheel={handleWheel}
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseUp}
            style={{
              cursor: draggedNode ? 'grabbing' : isPanning ? 'grabbing' : 'grab',
              userSelect: 'none',
              WebkitUserSelect: 'none',
              MozUserSelect: 'none',
              msUserSelect: 'none'
            }}
          />

          {/* Legend */}
          <div className="absolute bottom-4 left-4 bg-white dark:bg-gray-800 bg-opacity-90 dark:bg-opacity-90 backdrop-blur-sm rounded-lg p-3 shadow-lg border border-gray-200 dark:border-gray-700">
            <div className="font-semibold text-sm mb-2 text-gray-900 dark:text-white">Legend:</div>

            {/* Nodes */}
            <div className="flex items-center gap-2 mb-1 text-xs text-gray-700 dark:text-gray-300">
              <div className="w-3.5 h-3.5 rounded-full bg-amber-500 border border-white dark:border-gray-600"></div>
              <span className="font-medium">Current Note</span>
            </div>
            <div className="flex items-center gap-2 mb-2 text-xs text-gray-700 dark:text-gray-300">
              <div className="w-3 h-3 rounded-full bg-purple-600"></div>
              <span>Connected Notes</span>
            </div>

            {/* Connections */}
            <div className="border-t border-gray-300 dark:border-gray-600 pt-2 mt-2">
              <div className="flex items-center gap-2 mb-1 text-xs text-gray-700 dark:text-gray-300">
                <div className="w-6 h-0.5 bg-gray-400 dark:bg-gray-500"></div>
                <span>Wikilink</span>
              </div>
              <div className="flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300">
                <div className="w-6 h-0.5 bg-blue-400 border-dashed border-t border-blue-400"></div>
                <span>Tag-based</span>
              </div>
            </div>
          </div>

          {/* Instructions */}
          <div className="absolute top-4 left-1/2 -translate-x-1/2 bg-white dark:bg-gray-800 bg-opacity-90 dark:bg-opacity-90 backdrop-blur-sm rounded-lg px-4 py-2 shadow-lg border border-gray-200 dark:border-gray-700">
            <div className="text-sm text-gray-600 dark:text-gray-400 space-y-1">
              <p>
                <i className="fas fa-mouse-pointer mr-2"></i>
                Click node to open • Drag node to move • Drag canvas to pan • Scroll to zoom
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default GraphView
