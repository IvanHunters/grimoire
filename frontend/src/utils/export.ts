import jsPDF from 'jspdf'
import { toCanvas as htmlToCanvas } from 'html-to-image'
import mermaid from 'mermaid'
import JSZip from 'jszip'
import { Document, Packer, Paragraph, TextRun, HeadingLevel } from 'docx'
import type { Note } from '../types/note'

// PDF layout constants (mm)
const PDF_PAGE_WIDTH = 210
const PDF_PAGE_HEIGHT = 297
const PDF_MARGIN_TOP = 15
const PDF_MARGIN_BOTTOM = 18 // space for footer
const PDF_MARGIN_LEFT = 15
const PDF_MARGIN_RIGHT = 15
const PDF_CONTENT_WIDTH = PDF_PAGE_WIDTH - PDF_MARGIN_LEFT - PDF_MARGIN_RIGHT
const PDF_CONTENT_HEIGHT = PDF_PAGE_HEIGHT - PDF_MARGIN_TOP - PDF_MARGIN_BOTTOM
const PDF_RENDER_SCALE = 2
// Container width matches content area proportionally
const PDF_CONTAINER_WIDTH = Math.round(PDF_CONTENT_WIDTH * (96 / 25.4)) // mm to px at 96dpi

/**
 * Export note to PDF with smart page breaks
 *
 * Approach:
 * 1. Render preview to a single tall canvas
 * 2. Collect Y positions of all block-level elements (paragraphs, headings, code blocks, etc.)
 * 3. Find optimal page break points that avoid cutting through elements
 * 4. Slice canvas at those break points and place each slice on a PDF page
 * 5. Add page numbers and title footer
 */
/**
 * Build the same off-screen container used for PDF and return it.
 * Caller is responsible for removing the container from DOM.
 */
// CSS overrides applied to every PDF-render container. Forces dark-on-light
// styling regardless of the app's current dark/light theme — bypasses needing
// to toggle the global `.dark` class (which would briefly flash the visible
// preview to light mode).
const PDF_OVERRIDE_CSS = `
  .markdown-preview { color: #1f2937 !important; background: white !important; }
  .markdown-preview h1 { color: #111827 !important; border-bottom-color: rgba(8,145,178,0.25) !important; }
  .markdown-preview h2 { color: #1f2937 !important; border-bottom-color: rgba(8,145,178,0.2) !important; }
  .markdown-preview h3 { color: #374151 !important; }
  .markdown-preview h4, .markdown-preview h5, .markdown-preview h6 { color: #4b5563 !important; }
  .markdown-preview p, .markdown-preview li, .markdown-preview td { color: #1f2937 !important; }
  .markdown-preview code:not([class*="language-"]) { color: #0e7490 !important; background-color: rgba(14,116,144,0.08) !important; border-color: rgba(14,116,144,0.2) !important; }
  .markdown-preview blockquote { color: #4b5563 !important; background: rgba(8,145,178,0.04) !important; border-left-color: rgba(8,145,178,0.4) !important; }
  .markdown-preview table { border-color: #d1d5db !important; }
  .markdown-preview th, .markdown-preview td { border-color: #e5e7eb !important; }
  .markdown-preview th { color: #374151 !important; background-color: rgba(8,145,178,0.06) !important; border-bottom-color: rgba(8,145,178,0.2) !important; }
  .markdown-preview tbody tr:nth-child(even) { background-color: rgba(0,0,0,0.02) !important; }
  .markdown-preview a { color: #0e7490 !important; }
  .markdown-preview a:visited { color: #6366f1 !important; }
`

function appendPdfOverrideStyle(container: HTMLElement): void {
  const style = document.createElement('style')
  style.textContent = PDF_OVERRIDE_CSS
  container.appendChild(style)
}

async function buildPDFContainer(note: Note, previewElement: HTMLElement): Promise<{ container: HTMLElement; clone: HTMLElement }> {
  const container = document.createElement('div')
  applyContainerStyles(container, PDF_CONTAINER_WIDTH)

  const clone = previewElement.cloneNode(true) as HTMLElement
  clone.style.overflow = 'visible'
  clone.style.height = 'auto'
  clone.style.maxHeight = 'none'
  clone.querySelectorAll('button').forEach(btn => btn.remove())

  appendPdfOverrideStyle(container)

  clone.querySelectorAll('a').forEach(link => {
    const el = link as HTMLElement
    el.style.textDecoration = 'none'
    el.style.borderBottom = 'none'
    el.style.borderBottomWidth = '0'
  })

  clone.querySelectorAll('pre').forEach(pre => {
    pre.style.whiteSpace = 'pre-wrap'
    pre.style.wordBreak = 'break-all'
    pre.style.overflowX = 'hidden'
  })

  clone.querySelectorAll('code').forEach(code => {
    code.style.wordBreak = 'break-all'
  })

  clone.querySelectorAll('table').forEach(table => {
    table.style.tableLayout = 'fixed'
    table.style.wordBreak = 'break-word'
  })

  container.appendChild(clone)
  document.body.appendChild(container)

  await waitForImagesToLoad(container)

  // Re-render mermaid for PDF inside the container — the container has a white
  // background so mermaid's default light theme renders correctly without us
  // having to toggle `dark` on the document root (which would flash the visible
  // preview to light mode for the duration of the render).
  await rerenderMermaidForPDF(clone)

  return { container, clone }
}

/**
 * Export the PDF-render container as a self-contained HTML file opened in a new tab.
 * Useful for debugging rendering issues before exporting to PDF.
 */
export async function exportToHTML(note: Note, previewElement: HTMLElement): Promise<void> {
  const { container } = await buildPDFContainer(note, previewElement)

  // Collect all stylesheets from the current document
  const styleSheets: string[] = []
  for (const sheet of Array.from(document.styleSheets)) {
    try {
      const rules = Array.from(sheet.cssRules).map(r => r.cssText).join('\n')
      styleSheets.push(rules)
    } catch {
      // cross-origin stylesheet — skip
    }
  }

  const html = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>PDF Preview: ${note.title}</title>
  <style>${styleSheets.join('\n')}
pre[class*="language-"], pre[class*="language-"] code { color: #f8f8f2 !important; }
</style>
</head>
<body style="background:#fff;margin:0;padding:24px">
  ${container.innerHTML}
</body>
</html>`

  document.body.removeChild(container)

  const win = window.open('', '_blank')
  if (win) {
    win.document.write(html)
    win.document.close()
  }
}

// Aspect ratio threshold: diagrams wider than this (relative to height) are
// rendered on their own landscape page. Anything below stays inline in the
// portrait flow.
const WIDE_DIAGRAM_ASPECT_RATIO = 1.8

function isWideMermaidDiagram(el: HTMLElement): boolean {
  if (!el.classList.contains('mermaid-wrapper')) return false
  const svg = el.querySelector('svg')
  if (!svg) return false
  const viewBox = svg.getAttribute('viewBox')
  if (!viewBox) return false
  const parts = viewBox.split(/\s+/).map(parseFloat)
  if (parts.length < 4) return false
  const [, , vbW, vbH] = parts
  if (!(vbW > 0) || !(vbH > 0)) return false
  return vbW / vbH >= WIDE_DIAGRAM_ASPECT_RATIO
}

function applyContainerStyles(el: HTMLElement, widthPx: number): void {
  el.style.cssText = `
    position: absolute;
    left: 0;
    top: 0;
    width: ${widthPx}px;
    padding: 0;
    background: white;
    color: #1f2937;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    pointer-events: none;
    z-index: -1;
  `
}

export async function exportToPDF(note: Note, previewElement: HTMLElement): Promise<void> {
  try {
    // buildPDFContainer already re-renders mermaid with the light theme.
    const { container: fullContainer, clone: fullClone } = await buildPDFContainer(note, previewElement)
    // We don't render fullContainer directly — we'll build per-segment containers below.
    document.body.removeChild(fullContainer)

    const markdownPreview = (fullClone.querySelector('.markdown-preview') as HTMLElement) || fullClone

    // Walk top-level children, splitting on wide mermaid diagrams.
    type Segment =
      | { type: 'portrait'; nodes: HTMLElement[] }
      | { type: 'landscape'; diagram: HTMLElement }
    const segments: Segment[] = []
    let portraitBuf: HTMLElement[] = []
    for (const child of Array.from(markdownPreview.children) as HTMLElement[]) {
      if (isWideMermaidDiagram(child)) {
        if (portraitBuf.length > 0) {
          segments.push({ type: 'portrait', nodes: portraitBuf })
          portraitBuf = []
        }
        segments.push({ type: 'landscape', diagram: child })
      } else {
        portraitBuf.push(child)
      }
    }
    if (portraitBuf.length > 0) {
      segments.push({ type: 'portrait', nodes: portraitBuf })
    }

    // jsPDF auto-creates page 1 in the constructor orientation. We always call
    // addPage() per output page so each page has its own explicit orientation,
    // then drop the blank page 1 at the end.
    const pdf = new jsPDF({ orientation: 'portrait', unit: 'mm', format: 'a4' })
    let pageCount = 0

    for (const segment of segments) {
      if (segment.type === 'portrait') {
        pageCount += await renderPortraitSegment(pdf, segment.nodes)
      } else {
        await renderLandscapePage(pdf, segment.diagram)
        pageCount += 1
      }
    }

    pdf.deletePage(1)

    addFooters(pdf, note, pageCount)

    pdf.save(sanitizeFilename(note.title) + '.pdf')
  } catch (error) {
    console.error('Failed to export PDF:', error)
    throw new Error('Failed to export to PDF')
  }
}

async function renderPortraitSegment(pdf: jsPDF, nodes: HTMLElement[]): Promise<number> {
  // Build a container holding only this segment's nodes inside a .markdown-preview wrapper.
  const container = document.createElement('div')
  applyContainerStyles(container, PDF_CONTAINER_WIDTH)
  appendPdfOverrideStyle(container)
  const previewWrap = document.createElement('div')
  previewWrap.className = 'markdown-preview'
  nodes.forEach(n => previewWrap.appendChild(n))
  container.appendChild(previewWrap)
  document.body.appendChild(container)

  try {
    await waitForImagesToLoad(container)

    const breakPoints = collectBreakPoints(previewWrap, container)

    const canvas = await htmlToCanvas(container, {
      pixelRatio: PDF_RENDER_SCALE,
      backgroundColor: '#ffffff',
      cacheBust: true,
    })

    const pxPerMm = canvas.width / PDF_CONTENT_WIDTH
    const contentHeightPx = PDF_CONTENT_HEIGHT * pxPerMm
    const scaledBreaks = breakPoints.map(bp => bp * PDF_RENDER_SCALE)
    const pageSlices = computePageSlices(scaledBreaks, contentHeightPx, canvas.height)

    for (let i = 0; i < pageSlices.length; i++) {
      pdf.addPage('a4', 'portrait')

      const { startY, endY } = pageSlices[i]
      const sliceHeight = endY - startY
      if (sliceHeight <= 0) continue

      const pageCanvas = document.createElement('canvas')
      pageCanvas.width = canvas.width
      pageCanvas.height = sliceHeight
      const ctx = pageCanvas.getContext('2d')!
      ctx.fillStyle = '#ffffff'
      ctx.fillRect(0, 0, pageCanvas.width, pageCanvas.height)
      ctx.drawImage(canvas, 0, startY, canvas.width, sliceHeight, 0, 0, canvas.width, sliceHeight)

      const sliceHeightMm = sliceHeight / pxPerMm
      const imgData = pageCanvas.toDataURL('image/png')
      pdf.addImage(imgData, 'PNG', PDF_MARGIN_LEFT, PDF_MARGIN_TOP, PDF_CONTENT_WIDTH, sliceHeightMm)
    }

    return pageSlices.length
  } finally {
    document.body.removeChild(container)
  }
}

async function renderLandscapePage(pdf: jsPDF, diagram: HTMLElement): Promise<void> {
  // A4 landscape content area in mm
  const landscapeContentWidth = PDF_PAGE_HEIGHT - PDF_MARGIN_LEFT - PDF_MARGIN_RIGHT
  const landscapeContentHeight = PDF_PAGE_WIDTH - PDF_MARGIN_TOP - PDF_MARGIN_BOTTOM
  const containerWidthPx = Math.round(landscapeContentWidth * (96 / 25.4))

  const container = document.createElement('div')
  applyContainerStyles(container, containerWidthPx)
  appendPdfOverrideStyle(container)
  const previewWrap = document.createElement('div')
  previewWrap.className = 'markdown-preview'
  const diagramClone = diagram.cloneNode(true) as HTMLElement
  previewWrap.appendChild(diagramClone)
  container.appendChild(previewWrap)
  document.body.appendChild(container)

  try {
    await waitForImagesToLoad(container)

    const canvas = await htmlToCanvas(container, {
      pixelRatio: PDF_RENDER_SCALE,
      backgroundColor: '#ffffff',
      cacheBust: true,
    })

    pdf.addPage('a4', 'landscape')

    // Fit the diagram into the landscape content area, centered, preserving aspect ratio.
    const imgAspect = canvas.width / canvas.height
    const boxAspect = landscapeContentWidth / landscapeContentHeight
    let drawW: number, drawH: number
    if (imgAspect > boxAspect) {
      drawW = landscapeContentWidth
      drawH = landscapeContentWidth / imgAspect
    } else {
      drawH = landscapeContentHeight
      drawW = landscapeContentHeight * imgAspect
    }
    const offsetX = PDF_MARGIN_LEFT + (landscapeContentWidth - drawW) / 2
    const offsetY = PDF_MARGIN_TOP + (landscapeContentHeight - drawH) / 2

    const imgData = canvas.toDataURL('image/png')
    pdf.addImage(imgData, 'PNG', offsetX, offsetY, drawW, drawH)
  } finally {
    document.body.removeChild(container)
  }
}

function addFooters(pdf: jsPDF, note: Note, totalPages: number): void {
  const exportDate = new Date().toLocaleDateString()
  pdf.setFontSize(8)
  pdf.setTextColor(160, 160, 160)
  for (let i = 1; i <= totalPages; i++) {
    pdf.setPage(i)
    const isLandscape = pdf.getPageWidth() > pdf.getPageHeight()
    const pageW = pdf.getPageWidth()
    const pageH = pdf.getPageHeight()
    const footerY = pageH - 8
    const leftX = isLandscape ? PDF_MARGIN_LEFT : PDF_MARGIN_LEFT
    const rightX = pageW - PDF_MARGIN_RIGHT
    pdf.text(note.path, leftX, footerY)
    pdf.text(`${i} / ${totalPages}`, pageW / 2, footerY, { align: 'center' })
    pdf.text(exportDate, rightX, footerY, { align: 'right' })
  }
}

/**
 * Collect Y positions of block-level elements for smart page breaking.
 * Returns sorted array of Y offsets (in CSS px, relative to container top).
 *
 * Rules:
 * - Each block boundary is a candidate break point
 * - Headings are paired with next sibling (break before heading, not after)
 * - Tall elements (pre, table) get sub-element break points so they can
 *   be split across pages instead of being cut arbitrarily
 */
function collectBreakPoints(preview: HTMLElement, container: HTMLElement): number[] {
  const containerTop = container.getBoundingClientRect().top
  const points = new Set<number>()
  points.add(0)

  // Line height approximation for generating sub-break points inside tall blocks
  const LINE_HEIGHT_PX = 20

  const children = preview.children
  let prevWasHeading = false

  for (let i = 0; i < children.length; i++) {
    const child = children[i] as HTMLElement
    if (!child.getBoundingClientRect) continue

    const rect = child.getBoundingClientRect()
    const top = rect.top - containerTop
    const bottom = rect.bottom - containerTop
    const height = rect.height
    const tagName = child.tagName.toLowerCase()
    const isHeading = /^h[1-6]$/.test(tagName)

    // Add top of each element as a break candidate.
    // Exception: if the previous sibling was a heading, skip this element's top —
    // this keeps heading + its first content block on the same page (avoids orphan headings).
    if (top > 0 && !prevWasHeading) {
      points.add(Math.round(top))
    }

    prevWasHeading = isHeading

    // For headings: do NOT add the bottom as break point
    if (isHeading) {
      continue
    }

    // Add bottom
    if (bottom > 0) {
      points.add(Math.round(bottom))
    }

    // For tall elements (code blocks, tables, blockquotes, lists, mermaid):
    // add sub-element break points so they can be split across pages
    const isMermaid = child.classList.contains('mermaid-wrapper')
    if (height > 200 && (tagName === 'pre' || tagName === 'table' || tagName === 'blockquote' || tagName === 'ul' || tagName === 'ol' || isMermaid)) {
      // For pre: add break points at each wrapped line boundary
      if (tagName === 'pre') {
        const codeEl = child.querySelector('code')
        if (codeEl) {
          // Use line elements if available, otherwise estimate by line height
          const lineHeight = parseFloat(getComputedStyle(codeEl).lineHeight) || LINE_HEIGHT_PX
          for (let y = top + lineHeight; y < bottom - lineHeight; y += lineHeight) {
            points.add(Math.round(y))
          }
        }
      }
      // For tables: add break points at each row
      else if (tagName === 'table') {
        const rows = child.querySelectorAll('tr')
        rows.forEach(row => {
          const rowRect = row.getBoundingClientRect()
          const rowTop = rowRect.top - containerTop
          if (rowTop > top && rowTop < bottom) {
            points.add(Math.round(rowTop))
          }
        })
      }
      // For mermaid: add break points every ~page-height so very tall diagrams
      // can be split across multiple pages rather than just at a single midpoint
      else if (isMermaid) {
        const approxPageHeightPx = Math.round(PDF_CONTENT_HEIGHT * PDF_CONTAINER_WIDTH / PDF_CONTENT_WIDTH)
        for (let y = top + approxPageHeightPx; y < bottom; y += approxPageHeightPx) {
          points.add(Math.round(y))
        }
      }
      // For lists and blockquotes: break at each child
      else {
        const items = child.children
        for (let j = 0; j < items.length; j++) {
          const itemRect = items[j].getBoundingClientRect()
          const itemTop = itemRect.top - containerTop
          if (itemTop > top && itemTop < bottom) {
            points.add(Math.round(itemTop))
          }
        }
      }
    }
  }

  return Array.from(points).sort((a, b) => a - b)
}

/**
 * Compute optimal page slices given break points and page height.
 * Tries to break at element boundaries; falls back to hard cut if an element
 * is taller than a full page.
 */
function computePageSlices(
  breakPoints: number[],
  pageHeightPx: number,
  totalHeightPx: number
): Array<{ startY: number; endY: number }> {
  const slices: Array<{ startY: number; endY: number }> = []
  let currentStart = 0

  while (currentStart < totalHeightPx) {
    const idealEnd = currentStart + pageHeightPx

    // If remaining content fits on this page
    if (idealEnd >= totalHeightPx) {
      slices.push({ startY: currentStart, endY: totalHeightPx })
      break
    }

    // Find the best break point: largest Y that doesn't exceed idealEnd
    let bestBreak = idealEnd
    for (const bp of breakPoints) {
      if (bp > currentStart && bp <= idealEnd) {
        bestBreak = bp
      }
    }

    // Edge case: no break point found between currentStart and idealEnd
    // (element taller than full page) — force break at idealEnd
    if (bestBreak <= currentStart) {
      bestBreak = idealEnd
    }

    slices.push({ startY: currentStart, endY: bestBreak })
    currentStart = bestBreak
  }

  return slices
}

/**
 * Export note to Word (DOCX)
 * Converts markdown to DOCX with basic formatting
 */
export async function exportToWord(note: Note): Promise<void> {
  try {
    const paragraphs: Paragraph[] = []

    // Split content into lines
    const lines = note.content.split('\n')

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i]

      // Skip empty lines
      if (!line.trim()) {
        paragraphs.push(new Paragraph({ text: '' }))
        continue
      }

      // Headings
      if (line.startsWith('# ')) {
        paragraphs.push(new Paragraph({
          text: line.substring(2),
          heading: HeadingLevel.HEADING_1,
        }))
      } else if (line.startsWith('## ')) {
        paragraphs.push(new Paragraph({
          text: line.substring(3),
          heading: HeadingLevel.HEADING_2,
        }))
      } else if (line.startsWith('### ')) {
        paragraphs.push(new Paragraph({
          text: line.substring(4),
          heading: HeadingLevel.HEADING_3,
        }))
      } else if (line.startsWith('#### ')) {
        paragraphs.push(new Paragraph({
          text: line.substring(5),
          heading: HeadingLevel.HEADING_4,
        }))
      }
      // Code blocks
      else if (line.startsWith('```')) {
        const codeLines: string[] = []
        i++ // Skip opening ```
        while (i < lines.length && !lines[i].startsWith('```')) {
          codeLines.push(lines[i])
          i++
        }
        paragraphs.push(new Paragraph({
          children: [
            new TextRun({
              text: codeLines.join('\n'),
              font: 'Courier New',
              size: 20,
            }),
          ],
        }))
      }
      // Regular paragraphs with inline formatting
      else {
        const runs = parseInlineFormatting(line)
        paragraphs.push(new Paragraph({ children: runs }))
      }
    }

    // Create document
    const doc = new Document({
      sections: [{
        properties: {},
        children: paragraphs,
      }],
    })

    // Generate and download
    const blob = await Packer.toBlob(doc)
    const filename = sanitizeFilename(note.title) + '.docx'
    downloadBlob(blob, filename)
  } catch (error) {
    console.error('Failed to export Word:', error)
    throw new Error('Failed to export to Word')
  }
}

/**
 * Export a single folder (and its subfolders) to ZIP
 */
export async function exportFolderToZip(
  folderPath: string,
  notes: Note[]
): Promise<void> {
  const folderName = folderPath.split('/').pop() || folderPath

  const folderNotes = notes.filter(
    n => n.folder === folderPath || n.folder.startsWith(folderPath + '/')
  )

  if (folderNotes.length === 0) {
    throw new Error('No notes found in this folder')
  }

  const zip = new JSZip()

  for (const note of folderNotes) {
    const filename = sanitizeFilename(note.title) + '.md'
    const relativeFolder = note.folder === folderPath
      ? ''
      : note.folder.slice(folderPath.length + 1)
    const path = relativeFolder ? `${relativeFolder}/${filename}` : filename
    zip.file(path, note.content)
  }

  const imageUrls = new Set<string>()
  const imageRegex = /!\[.*?\]\((\/uploads\/[^)]+)\)/g
  for (const note of folderNotes) {
    let match
    while ((match = imageRegex.exec(note.content)) !== null) {
      imageUrls.add(match[1])
    }
  }

  if (imageUrls.size > 0) {
    const uploadsFolder = zip.folder('uploads')
    for (const imageUrl of imageUrls) {
      try {
        const response = await fetch(imageUrl)
        const blob = await response.blob()
        uploadsFolder?.file(imageUrl.replace('/uploads/', ''), blob)
      } catch {
        // skip missing images
      }
    }
  }

  const blob = await zip.generateAsync({ type: 'blob', mimeType: 'application/zip' })
  downloadBlob(blob, `${sanitizeFilename(folderName)}.zip`)
}

/**
 * Export all notes to ZIP
 * Includes folder structure and uploaded images
 */
export async function exportAllNotesToZip(
  notes: Note[]
): Promise<void> {
  try {
    const zip = new JSZip()

    // Add notes with folder structure
    for (const note of notes) {
      const folder = note.folder || 'root'
      const filename = sanitizeFilename(note.title) + '.md'
      const path = folder === '' ? filename : `${folder}/${filename}`

      zip.file(path, note.content)
    }

    // Collect all image URLs from notes
    const imageUrls = new Set<string>()
    const imageRegex = /!\[.*?\]\((\/uploads\/[^)]+)\)/g

    for (const note of notes) {
      let match
      while ((match = imageRegex.exec(note.content)) !== null) {
        imageUrls.add(match[1])
      }
    }

    // Download and add images to ZIP
    if (imageUrls.size > 0) {
      const uploadsFolder = zip.folder('uploads')

      for (const imageUrl of imageUrls) {
        try {
          const response = await fetch(imageUrl)
          const blob = await response.blob()
          const imagePath = imageUrl.replace('/uploads/', '')
          uploadsFolder?.file(imagePath, blob)
        } catch (error) {
          console.warn(`Failed to fetch image: ${imageUrl}`, error)
        }
      }
    }

    // Generate and download ZIP
    const blob = await zip.generateAsync({ type: 'blob', mimeType: 'application/zip' })
    const timestamp = new Date().toISOString().split('T')[0]
    downloadBlob(blob, `notes-backup-${timestamp}.zip`)
  } catch (error) {
    console.error('Failed to export ZIP:', error)
    throw new Error('Failed to export to ZIP')
  }
}

// Helper functions

function sanitizeFilename(filename: string): string {
  return filename
    .replace(/[^a-z0-9_\-\.]/gi, '-')
    .replace(/--+/g, '-')
    .toLowerCase()
}

function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

let pdfDiagramCounter = 0

/**
 * Re-render all mermaid diagrams with light theme for PDF.
 * Uses data-source attribute saved by MermaidDiagram component.
 */
async function rerenderMermaidForPDF(container: HTMLElement): Promise<void> {
  const mermaidDivs = container.querySelectorAll('.mermaid[data-source]')
  if (mermaidDivs.length === 0) return

  mermaid.initialize({
    startOnLoad: false,
    theme: 'default',
    securityLevel: 'loose',
  })

  for (const div of mermaidDivs) {
    const source = div.getAttribute('data-source')
    if (!source) continue

    try {
      const id = `pdf-mermaid-${++pdfDiagramCounter}-${Date.now()}`
      const { svg } = await mermaid.render(id, source)
      div.innerHTML = svg
    } catch (error) {
      console.warn('Failed to re-render mermaid for PDF:', error)
      // Leave the existing SVG as-is
    }
  }
}

function waitForImagesToLoad(container: HTMLElement): Promise<void> {
  const images = container.querySelectorAll('img')
  const promises = Array.from(images).map(img => {
    if (img.complete) return Promise.resolve()
    return new Promise<void>((resolve) => {
      img.onload = () => resolve()
      img.onerror = () => resolve() // Continue even if image fails to load
      // Timeout after 5 seconds
      setTimeout(() => resolve(), 5000)
    })
  })
  return Promise.all(promises).then(() => {})
}

function parseInlineFormatting(text: string): TextRun[] {
  const runs: TextRun[] = []

  // Simple parser for **bold** and *italic*
  // This is a basic implementation - could be enhanced
  const parts = text.split(/(\*\*[^*]+\*\*|\*[^*]+\*|`[^`]+`)/)

  for (const part of parts) {
    if (!part) continue

    if (part.startsWith('**') && part.endsWith('**')) {
      runs.push(new TextRun({
        text: part.slice(2, -2),
        bold: true,
      }))
    } else if (part.startsWith('*') && part.endsWith('*')) {
      runs.push(new TextRun({
        text: part.slice(1, -1),
        italics: true,
      }))
    } else if (part.startsWith('`') && part.endsWith('`')) {
      runs.push(new TextRun({
        text: part.slice(1, -1),
        font: 'Courier New',
      }))
    } else {
      runs.push(new TextRun({ text: part }))
    }
  }

  return runs.length > 0 ? runs : [new TextRun({ text })]
}
