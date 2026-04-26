import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useNotes } from '../../contexts/NotesContext'
import { X, RefreshCw } from 'lucide-react'
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
}

interface GraphLink {
  source: string
  target: string
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
  const [transform, setTransform] = useState({ x: 0, y: 0, scale: 1 })
  const [isPanning, setIsPanning] = useState(false)
  const [panStart, setPanStart] = useState({ x: 0, y: 0 })

  useEffect(() => {
    if (visible && svgRef.current) {
      renderGraph()
    }
  }, [visible, currentNote, notes, showAll])

  // Update transform without re-rendering the entire graph
  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.setAttribute(
        'transform',
        `translate(${transform.x}, ${transform.y}) scale(${transform.scale})`
      )
    }
  }, [transform])

  const renderGraph = () => {
    const svg = svgRef.current
    if (!svg) return

    const width = svg.clientWidth
    const height = svg.clientHeight

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

    // Build graph connections from wikilinks
    const connections = buildGraphConnections(notes)

    // Get nodes to display
    let nodeIds: string[]
    if (showAll) {
      // Show all notes
      nodeIds = notes.map(n => n.id)
    } else {
      // Show only connected notes (current + connected)
      const connectedIds = getConnectedNotes(currentNoteId, connections, 2)
      nodeIds = [currentNoteId, ...Array.from(connectedIds)]
    }

    // Build links from connections
    const links: GraphLink[] = []
    nodeIds.forEach(noteId => {
      const targets = connections.get(noteId) || []
      targets.forEach(targetId => {
        if (nodeIds.includes(targetId)) {
          links.push({
            source: noteId,
            target: targetId,
          })
        }
      })
    })

    // Initialize nodes with random positions
    const forceNodes: ForceNode[] = initializeNodes(nodeIds, width, height, currentNoteId)

    // Run force-directed layout with stronger repulsion to prevent overlap
    const layoutedNodes = forceLayout(forceNodes, links, {
      width,
      height,
      repulsionStrength: 15000, // Increased to prevent node overlap
      attractionStrength: 0.01,
      centerStrength: 0.02,
      damping: 0.85,
      iterations: 300,
    })

    // Convert to GraphNode format
    const nodes: GraphNode[] = layoutedNodes.map(node => {
      const noteData = notes.find(n => n.id === node.id)
      return {
        id: node.id,
        label: noteData?.title || 'Unknown',
        x: node.x,
        y: node.y,
        isCurrent: node.id === currentNoteId,
      }
    })

    // Draw links
    links.forEach(link => {
      const sourceNode = nodes.find(n => n.id === link.source)
      const targetNode = nodes.find(n => n.id === link.target)

      if (!sourceNode || !targetNode) return

      const line = document.createElementNS('http://www.w3.org/2000/svg', 'line')
      line.setAttribute('x1', String(sourceNode.x))
      line.setAttribute('y1', String(sourceNode.y))
      line.setAttribute('x2', String(targetNode.x))
      line.setAttribute('y2', String(targetNode.y))
      line.setAttribute('stroke', '#cbd5e1')
      line.setAttribute('stroke-width', '2')
      g.appendChild(line)
    })

    // Draw nodes
    nodes.forEach(node => {
      // Circle
      const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
      circle.setAttribute('cx', String(node.x))
      circle.setAttribute('cy', String(node.y))
      circle.setAttribute('r', '30')
      circle.setAttribute('fill', node.isCurrent ? '#f59e0b' : '#8b5cf6') // Orange for current, purple for others
      circle.setAttribute('stroke', 'white')
      circle.setAttribute('stroke-width', node.isCurrent ? '4' : '3') // Thicker border for current
      circle.style.cursor = 'pointer'
      circle.style.transition = 'all 0.2s ease'

      // Hover effect
      circle.addEventListener('mouseenter', () => {
        circle.setAttribute('stroke-width', '5')
        circle.style.filter = 'brightness(1.2)'
      })
      circle.addEventListener('mouseleave', () => {
        circle.setAttribute('stroke-width', '3')
        circle.style.filter = 'brightness(1)'
      })

      // Click to navigate
      circle.addEventListener('click', () => {
        const noteToOpen = notes.find(n => n.id === node.id)
        if (noteToOpen) {
          navigate(`/notes/${noteToOpen.id}`)
          onClose()
        }
      })

      g.appendChild(circle)

      // Label
      const text = document.createElementNS('http://www.w3.org/2000/svg', 'text')
      text.setAttribute('x', String(node.x))
      text.setAttribute('y', String(node.y + 50))
      text.setAttribute('text-anchor', 'middle')
      text.setAttribute('fill', '#374151')
      text.setAttribute('font-size', '14')
      text.setAttribute('font-weight', '500')
      text.textContent = node.label
      g.appendChild(text)
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
    // Only pan with left mouse button and not clicking on nodes (circle or text)
    if (e.button === 0 && target.tagName !== 'circle' && target.tagName !== 'text') {
      setIsPanning(true)
      setPanStart({ x: e.clientX - transform.x, y: e.clientY - transform.y })
      e.preventDefault()
    }
  }

  const handleMouseMove = (e: React.MouseEvent<SVGSVGElement>) => {
    if (isPanning) {
      setTransform(prev => ({
        ...prev,
        x: e.clientX - panStart.x,
        y: e.clientY - panStart.y,
      }))
    }
  }

  const handleMouseUp = () => {
    setIsPanning(false)
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
        className="w-[90%] h-[85%] bg-white rounded-xl flex flex-col shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="p-4 border-b border-gray-200 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold flex items-center gap-2">
              <i className="fas fa-project-diagram text-blue-600"></i>
              Graph View - Note Connections
            </h2>
            <p className="text-sm text-gray-600 mt-1">
              {showAll
                ? `Showing all ${notes.length} notes`
                : 'Showing current note and connected notes'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setTransform({ x: 0, y: 0, scale: 1 })}
              className="px-3 py-1.5 text-sm bg-gray-100 hover:bg-gray-200 rounded-lg transition"
              title="Reset zoom and pan"
            >
              Reset View
            </button>
            <button
              onClick={() => setShowAll(!showAll)}
              className="px-3 py-1.5 text-sm bg-gray-100 hover:bg-gray-200 rounded-lg transition flex items-center gap-2"
            >
              <RefreshCw className="w-4 h-4" />
              {showAll ? 'Show Connected' : 'Show All'}
            </button>
            <button
              onClick={onClose}
              className="p-2 hover:bg-gray-100 rounded-lg transition"
            >
              <X className="w-5 h-5 text-gray-600" />
            </button>
          </div>
        </div>

        {/* Graph canvas */}
        <div className="flex-1 relative bg-gradient-to-br from-gray-50 to-white">
          <svg
            ref={svgRef}
            className="w-full h-full"
            onWheel={handleWheel}
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseUp}
            style={{ cursor: isPanning ? 'grabbing' : 'grab' }}
          />

          {/* Legend */}
          <div className="absolute bottom-4 left-4 bg-white bg-opacity-90 backdrop-blur-sm rounded-lg p-3 shadow-lg border border-gray-200">
            <div className="font-semibold text-sm mb-2">Legend:</div>
            <div className="flex items-center gap-2 mb-1 text-sm">
              <div className="w-4 h-4 rounded-full bg-amber-500 border-2 border-white"></div>
              <span className="font-medium">Current Note</span>
            </div>
            <div className="flex items-center gap-2 text-sm">
              <div className="w-3 h-3 rounded-full bg-purple-600"></div>
              <span>Connected Notes</span>
            </div>
          </div>

          {/* Instructions */}
          <div className="absolute top-4 left-1/2 -translate-x-1/2 bg-white bg-opacity-90 backdrop-blur-sm rounded-lg px-4 py-2 shadow-lg border border-gray-200">
            <div className="text-sm text-gray-600 space-y-1">
              <p>
                <i className="fas fa-mouse-pointer mr-2"></i>
                Click node to navigate • Drag to pan • Scroll to zoom
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default GraphView
