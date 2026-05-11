import jsPDF from 'jspdf'
import html2canvas from 'html2canvas'
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
async function buildPDFContainer(note: Note, previewElement: HTMLElement): Promise<{ container: HTMLElement; clone: HTMLElement }> {
  const container = document.createElement('div')
  container.style.cssText = `
    position: absolute;
    left: -9999px;
    top: 0;
    width: ${PDF_CONTAINER_WIDTH}px;
    padding: 0;
    background: white;
    color: #1f2937;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  `

  const clone = previewElement.cloneNode(true) as HTMLElement
  clone.style.overflow = 'visible'
  clone.style.height = 'auto'
  clone.style.maxHeight = 'none'
  clone.querySelectorAll('button').forEach(btn => btn.remove())

  const pdfOverride = document.createElement('style')
  pdfOverride.textContent = `
    .markdown-preview h1 { color: #111827 !important; border-bottom-color: rgba(8,145,178,0.25) !important; }
    .markdown-preview h2 { color: #1f2937 !important; border-bottom-color: rgba(8,145,178,0.2) !important; }
    .markdown-preview h3 { color: #374151 !important; }
    .markdown-preview h4, .markdown-preview h5, .markdown-preview h6 { color: #4b5563 !important; }
    .markdown-preview code:not([class*="language-"]) { display: inline-block !important; line-height: 1 !important; vertical-align: 0.3em !important; color: #0e7490 !important; background-color: rgba(14,116,144,0.08) !important; border-color: rgba(14,116,144,0.2) !important; }
    .markdown-preview blockquote { color: #4b5563 !important; background: rgba(8,145,178,0.04) !important; border-left-color: rgba(8,145,178,0.4) !important; }
    .markdown-preview table { border-color: #d1d5db !important; }
    .markdown-preview th, .markdown-preview td { border-color: #e5e7eb !important; }
    .markdown-preview th { color: #374151 !important; background-color: rgba(8,145,178,0.06) !important; border-bottom-color: rgba(8,145,178,0.2) !important; }
    .markdown-preview tbody tr:nth-child(even) { background-color: rgba(0,0,0,0.02) !important; }
    .markdown-preview a { color: #0e7490 !important; }
    .markdown-preview a:visited { color: #6366f1 !important; }
  `
  container.appendChild(pdfOverride)

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

  const htmlElement = document.documentElement
  const wasDarkMode = htmlElement.classList.contains('dark')
  if (wasDarkMode) htmlElement.classList.remove('dark')

  await rerenderMermaidForPDF(clone)

  return { container, clone }
}

// Detach inline code backgrounds from text to work around html2canvas inline
// text positioning bug: html2canvas renders inline element text at the BOTTOM
// of the element's CSS rect (text_bottom ≈ rect.bottom - padding-bottom),
// regardless of how tall the rect is (parent line-height inflates rect height).
//
// Fix: create absolutely-positioned <div>s anchored to rect.bottom, sized to
// wrap the actual glyph height + padding. The code element keeps only its text
// (transparent background). html2canvas renders the bg div at the correct
// position and the transparent text on top.
//
// Call immediately before html2canvas — NOT for HTML export (coordinates are
// valid only at PDF_CONTAINER_WIDTH layout, not full viewport width).
function detachInlineCodeBackgrounds(container: HTMLElement, clone: HTMLElement): void {
  const containerRect = container.getBoundingClientRect()
  clone.querySelectorAll<HTMLElement>('code:not([class*="language-"])').forEach(code => {
    const rect = code.getBoundingClientRect()
    const s = window.getComputedStyle(code)
    const fontSize = parseFloat(s.fontSize)
    const paddingTop = parseFloat(s.paddingTop)
    const paddingBottom = parseFloat(s.paddingBottom)
    const borderWidth = parseFloat(s.borderTopWidth)

    // Anchor to rect.bottom: text content ends at rect.bottom - paddingBottom.
    // textHeight ≈ full em-square (safe upper bound, avoids glyph clipping).
    const textHeight = fontSize
    const bgTop = (rect.bottom - paddingBottom - textHeight - paddingTop - borderWidth) - containerRect.top
    const bgHeight = textHeight + paddingTop + paddingBottom + 2 * borderWidth

    const bg = document.createElement('div')
    bg.style.cssText = `
      position: absolute;
      left: ${rect.left - containerRect.left}px;
      top: ${bgTop}px;
      width: ${rect.width}px;
      height: ${bgHeight}px;
      background-color: ${s.backgroundColor};
      border: ${s.borderTopWidth} solid ${s.borderTopColor};
      border-radius: ${s.borderRadius};
      box-sizing: border-box;
      pointer-events: none;
    `
    container.insertBefore(bg, clone)

    // Use setProperty with important to override pdfOverride !important rules.
    code.style.setProperty('background-color', 'transparent', 'important')
    code.style.setProperty('border', 'none', 'important')
  })
}

/**
 * Export the PDF-render container as a self-contained HTML file opened in a new tab.
 * Useful for debugging rendering issues before exporting to PDF.
 */
export async function exportToHTML(note: Note, previewElement: HTMLElement): Promise<void> {
  const htmlElement = document.documentElement
  const wasDarkMode = htmlElement.classList.contains('dark')

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

  if (wasDarkMode) htmlElement.classList.add('dark')
  document.body.removeChild(container)

  const win = window.open('', '_blank')
  if (win) {
    win.document.write(html)
    win.document.close()
  }
}

export async function exportToPDF(note: Note, previewElement: HTMLElement): Promise<void> {
  try {
    const htmlElement = document.documentElement
    const wasDarkMode = htmlElement.classList.contains('dark')

    const { container, clone } = await buildPDFContainer(note, previewElement)

    // Collect break points from block-level elements
    const markdownPreview = clone.querySelector('.markdown-preview') || clone
    const breakPoints = collectBreakPoints(markdownPreview as HTMLElement, container)

    // Render to canvas
    const canvas = await html2canvas(container, {
      scale: PDF_RENDER_SCALE,
      useCORS: true,
      allowTaint: true,
      backgroundColor: '#ffffff',
      logging: false,
    })

    // Restore dark mode
    if (wasDarkMode) {
      htmlElement.classList.add('dark')
    }

    // Clean up
    document.body.removeChild(container)

    // Calculate dimensions
    // canvas pixels per mm = canvas.width / PDF_CONTENT_WIDTH
    const pxPerMm = canvas.width / PDF_CONTENT_WIDTH
    const contentHeightPx = PDF_CONTENT_HEIGHT * pxPerMm

    // Scale break points to canvas coordinates
    const scaledBreaks = breakPoints.map(bp => bp * PDF_RENDER_SCALE)

    // Find optimal page break positions
    const pageSlices = computePageSlices(scaledBreaks, contentHeightPx, canvas.height)

    // Build PDF
    const pdf = new jsPDF({ orientation: 'portrait', unit: 'mm', format: 'a4' })
    const totalPages = pageSlices.length
    const exportDate = new Date().toLocaleDateString()

    for (let i = 0; i < totalPages; i++) {
      if (i > 0) pdf.addPage()

      const { startY, endY } = pageSlices[i]
      const sliceHeight = endY - startY
      if (sliceHeight <= 0) continue

      // Slice canvas for this page
      const pageCanvas = document.createElement('canvas')
      pageCanvas.width = canvas.width
      pageCanvas.height = sliceHeight
      const ctx = pageCanvas.getContext('2d')!
      ctx.fillStyle = '#ffffff'
      ctx.fillRect(0, 0, pageCanvas.width, pageCanvas.height)
      ctx.drawImage(canvas, 0, startY, canvas.width, sliceHeight, 0, 0, canvas.width, sliceHeight)

      // Place slice on PDF page
      const sliceHeightMm = sliceHeight / pxPerMm
      const imgData = pageCanvas.toDataURL('image/png')
      pdf.addImage(imgData, 'PNG', PDF_MARGIN_LEFT, PDF_MARGIN_TOP, PDF_CONTENT_WIDTH, sliceHeightMm)

      // Footer: page number centered, title on the left, date on the right
      pdf.setFontSize(8)
      pdf.setTextColor(160, 160, 160)
      const footerY = PDF_PAGE_HEIGHT - 8
      pdf.text(note.path, PDF_MARGIN_LEFT, footerY)
      pdf.text(`${i + 1} / ${totalPages}`, PDF_PAGE_WIDTH / 2, footerY, { align: 'center' })
      pdf.text(exportDate, PDF_PAGE_WIDTH - PDF_MARGIN_RIGHT, footerY, { align: 'right' })
    }

    // Download
    const filename = sanitizeFilename(note.title) + '.pdf'
    pdf.save(filename)
  } catch (error) {
    console.error('Failed to export PDF:', error)
    throw new Error('Failed to export to PDF')
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

  // Initialize mermaid with light theme
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
