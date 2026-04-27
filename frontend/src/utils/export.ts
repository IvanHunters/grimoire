import jsPDF from 'jspdf'
import html2canvas from 'html2canvas'
import JSZip from 'jszip'
import { Document, Packer, Paragraph, TextRun, HeadingLevel } from 'docx'
import type { Note } from '../types/note'

/**
 * Export note to PDF
 * Renders the preview panel to canvas and converts to PDF
 */
export async function exportToPDF(note: Note, previewElement: HTMLElement): Promise<void> {
  try {
    // Create a temporary container with proper styling
    const container = document.createElement('div')
    container.style.cssText = `
      position: absolute;
      left: -9999px;
      top: 0;
      width: 800px;
      padding: 40px;
      background: white;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    `

    // Clone preview content
    const clone = previewElement.cloneNode(true) as HTMLElement
    container.appendChild(clone)
    document.body.appendChild(container)

    // Wait for images and diagrams to load
    await waitForImagesToLoad(container)

    // Render to canvas with high quality
    const canvas = await html2canvas(container, {
      scale: 2,
      useCORS: true,
      allowTaint: true,
      backgroundColor: '#ffffff',
      logging: false,
    })

    // Remove temporary container
    document.body.removeChild(container)

    // Create PDF
    const imgWidth = 210 // A4 width in mm
    const imgHeight = (canvas.height * imgWidth) / canvas.width

    const pdf = new jsPDF({
      orientation: imgHeight > imgWidth ? 'portrait' : 'landscape',
      unit: 'mm',
      format: 'a4',
    })

    const imgData = canvas.toDataURL('image/png')

    // If content is longer than one page, split it
    let heightLeft = imgHeight
    let position = 0
    const pageHeight = 297 // A4 height in mm

    pdf.addImage(imgData, 'PNG', 0, position, imgWidth, imgHeight)
    heightLeft -= pageHeight

    while (heightLeft > 0) {
      position = heightLeft - imgHeight
      pdf.addPage()
      pdf.addImage(imgData, 'PNG', 0, position, imgWidth, imgHeight)
      heightLeft -= pageHeight
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
    const blob = await zip.generateAsync({ type: 'blob' })
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
