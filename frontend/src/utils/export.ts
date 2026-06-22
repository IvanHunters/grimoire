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
  /* Hanging indent for wrapped code-block lines so continuation doesn't
     overflow into / past the line-number gutter. padding-left reserves
     the gutter width; text-indent pulls the first-line content (and the
     ::before line-number) back to flush-left. Wrapped lines stay indented
     at padding-left, lining up with where the first line's text starts. */
  .markdown-preview pre code .code-line {
    padding-left: 3.5em !important;
    text-indent: -3.5em !important;
  }
  /* CRITICAL: image cap forced via !important. Without this an image
     taller than one PDF page slices mid-pixel. We deliberately use
     "max-height" (not "height") so portrait aspect ratio is preserved:
     image scales down proportionally to fit. The 918px ceiling is
     PDF_CONTENT_HEIGHT*96/25.4 minus 80px margin for surrounding <p>
     paragraph margins + line-height. */
  .markdown-preview img,
  .markdown-preview picture,
  .markdown-preview svg {
    max-height: 918px !important;
    height: auto !important;
    max-width: 100% !important;
    object-fit: contain !important;
    display: block !important;
  }
  /* Tighten wrapping <p> so the forbidden-zone math (which assumes
     image height + ~50px wrapper) doesn't underflow. */
  .markdown-preview p:has(> img:only-child),
  .markdown-preview p:has(> picture:only-child) {
    margin-top: 8px !important;
    margin-bottom: 8px !important;
  }
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

/**
 * Bombproof image sizing: after images have loaded, compute and set
 * EXPLICIT width/height in CSS pixels so the rasteriser can't slice an
 * image bigger than one PDF content area. CSS max-height alone has lost
 * specificity races in the past (rehype-prism / prose plugin styling
 * can override). This is the second layer of defense — the geometry is
 * baked into element.style.{width,height,maxWidth,maxHeight}.
 *
 * pageContentPx = available vertical space on one PDF page in container
 * coordinates (PDF_CONTENT_HEIGHT mm @ 96dpi minus a safety margin for
 * surrounding <p> margins + line-height).
 */
function clampImagesToFitPage(container: HTMLElement): void {
  const pageContentPx = Math.floor(PDF_CONTENT_HEIGHT * (96 / 25.4)) - 80
  const maxWidthPx = container.clientWidth || PDF_CONTAINER_WIDTH
  container.querySelectorAll('img').forEach(node => {
    const el = node as HTMLImageElement
    const natW = el.naturalWidth || el.offsetWidth
    const natH = el.naturalHeight || el.offsetHeight
    if (!natW || !natH) return
    const aspect = natW / natH
    let w = Math.min(maxWidthPx, natW)
    let h = w / aspect
    if (h > pageContentPx) {
      h = pageContentPx
      w = h * aspect
    }
    // Round down to avoid sub-pixel rounding pushing past the cap.
    w = Math.floor(w)
    h = Math.floor(h)
    el.style.width = w + 'px'
    el.style.height = h + 'px'
    el.style.maxWidth = w + 'px'
    el.style.maxHeight = h + 'px'
    el.style.display = 'block'
    // Tighten wrapping <p> so wrapper margins don't push the block past
    // pageContentPx and trigger the slicer's mid-image cut.
    const parent = el.parentElement
    if (parent && parent.tagName.toLowerCase() === 'p' && parent.children.length === 1) {
      parent.style.marginTop = '8px'
      parent.style.marginBottom = '8px'
    }
  })
  // Force a synchronous layout pass so subsequent getBoundingClientRect
  // calls (in collectBreakPointsAndForbidden / collectTextSpans) see the
  // clamped dimensions.
  void container.offsetHeight
}

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
    // Code blocks contain long unbreakable tokens (URLs, hostnames like
    // `postgres-postgres-ht-rw.tenant-ktj-htdev.svc.cozy.local`) that MUST
    // break at any char or they overflow the page width. break-all is the
    // right tool here. Inline `<code>` tags (handled below) use a softer
    // strategy that protects short identifiers like `dwh`.
    pre.style.whiteSpace = 'pre-wrap'
    pre.style.wordBreak = 'break-all'
    pre.style.overflowX = 'hidden'
    pre.style.maxWidth = '100%'
    // Also constrain the <code> inside — rehype-prism sets it display:block
    // with intrinsic width which can ignore parent wrapping rules.
    pre.querySelectorAll('code').forEach(code => {
      const el = code as HTMLElement
      el.style.whiteSpace = 'pre-wrap'
      el.style.wordBreak = 'break-all'
      ;(el.style as CSSStyleDeclaration & { overflowWrap: string }).overflowWrap = 'anywhere'
      el.style.maxWidth = '100%'
    })
  })

  // Inline `<code>` tags (NOT inside <pre>) — keep word boundaries, only
  // break long unbreakable tokens. `wordBreak: break-all` was the cause of
  // mid-character splits like `dwh` → `dw` + line + `h`. Filter out code
  // tags that live inside <pre> (handled above with break-all).
  clone.querySelectorAll('code').forEach(code => {
    if (code.closest('pre')) return
    const el = code as HTMLElement
    el.style.wordBreak = 'normal'
    ;(el.style as CSSStyleDeclaration & { overflowWrap: string }).overflowWrap = 'break-word'
  })

  clone.querySelectorAll('table').forEach(table => {
    table.style.tableLayout = 'fixed'
    table.style.wordBreak = 'break-word'
  })

  // Cap image height so a single image+wrapper never exceeds one PDF page.
  // Markdown renders `![](...)` as `<p><img></p>`. The wrapping <p> adds
  // top+bottom margins (~16px each), and the surrounding paragraph
  // line-height adds another ~8-12px. So the effective height of the
  // image-block in the canvas is imgHeight + ~50-60px. We subtract a
  // generous safety margin from page-content-px so even with all these
  // wrapping affordances the block fits in one page slice — leaving the
  // forbidden-zone snap as a safety net for any edge case the math misses.
  const pageContentPx = Math.floor(PDF_CONTENT_HEIGHT * (96 / 25.4)) - 80
  clone.querySelectorAll('img, picture, svg').forEach(node => {
    const el = node as HTMLElement
    el.style.maxHeight = pageContentPx + 'px'
    el.style.height = 'auto'
    el.style.objectFit = 'contain'
    el.style.display = 'block'
    // Strip default img margins from any wrapping <p> too — collapsing
    // those margins lets the forbidden-zone math be exact.
    const parent = el.parentElement
    if (parent && parent.tagName.toLowerCase() === 'p' && parent.children.length === 1) {
      parent.style.marginTop = '8px'
      parent.style.marginBottom = '8px'
    }
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

    // BOMBPROOF image clamping — after natural dimensions are known, set
    // EXPLICIT width/height in pixels so html-to-image rasterises at the
    // clamped size. CSS max-height in PDF_OVERRIDE_CSS is the first layer
    // of defense; this is the second so even when CSS specificity races
    // (rehype-prism, prose, etc.) the image still fits inside one page.
    clampImagesToFitPage(container)

    const { breakPoints, forbiddenRanges } = collectBreakPointsAndForbidden(previewWrap, container)

    const canvas = await htmlToCanvas(container, {
      pixelRatio: PDF_RENDER_SCALE,
      backgroundColor: '#ffffff',
      cacheBust: true,
    })

    const pxPerMm = canvas.width / PDF_CONTENT_WIDTH
    const contentHeightPx = PDF_CONTENT_HEIGHT * pxPerMm
    const scaledBreaks = breakPoints.map(bp => bp * PDF_RENDER_SCALE)
    const scaledForbidden = forbiddenRanges.map(([t, b]) =>
      [t * PDF_RENDER_SCALE, b * PDF_RENDER_SCALE] as [number, number]
    )
    const pageSlices = computePageSlices(scaledBreaks, contentHeightPx, canvas.height, scaledForbidden)

    // Extract text spans for the invisible copy-text layer. Each entry holds
    // the text content + its bounding rect in CANVAS coordinates (i.e. already
    // multiplied by PDF_RENDER_SCALE). We project these onto each page slice
    // and emit invisible pdf.text() so the canvas-rendered image is backed by
    // selectable, copyable text.
    const textSpans = collectTextSpans(previewWrap, container)

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

      // Invisible text overlay — selectable / copyable. For every text span
      // whose top falls inside this slice's Y range, place pdf.text() at the
      // corresponding PDF coordinate with renderingMode:'invisible' so the
      // visible image stays the only paint, but the text is in the PDF stream.
      paintInvisibleTextLayer(pdf, textSpans, startY, endY, pxPerMm)
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
/**
 * Returns BOTH break-point candidates and forbidden ranges. Forbidden ranges
 * are pixel ranges where a page break would visually split a single visual
 * unit (image, picture, svg, mermaid diagram). computePageSlices snaps any
 * candidate that falls inside a forbidden range up to the range's top.
 */
// A single text span — text content + its rendered bounding rect in CANVAS
// coordinates (DOM px multiplied by PDF_RENDER_SCALE).
interface TextSpan {
  text: string
  /** Top edge of the text line in canvas pixels. */
  yCanvas: number
  /** Left edge of the text line in canvas pixels. */
  xCanvas: number
  /** Width in canvas pixels — used as maxWidth for pdf.text() so jsPDF
   *  wraps the invisible text the same way the rendered image does. */
  widthCanvas: number
  /** Line height in canvas pixels — drives the vertical offset jsPDF
   *  uses for its text baseline. */
  heightCanvas: number
}

// Tags whose text content should NOT be paired with their own invisible
// layer. Their children (when text-bearing) already provide the text.
const SKIP_TEXT_OF: ReadonlySet<string> = new Set([
  'ul', 'ol', 'dl',          // lists — items emit text
  'table', 'thead', 'tbody', 'tr',  // tables — cells emit
  'pre',                      // <pre><code> — code emits via .code-line spans
  'div',                      // generic — children emit
  'figure', 'article', 'section', 'main', 'nav', 'aside', 'header', 'footer',
  'picture', 'svg',           // images / vector — no text we need to copy
])

// Tags that emit text. We collect the leaf-most text-bearing nodes so
// nested paragraphs / list items don't duplicate.
const TEXT_LEAVES: ReadonlySet<string> = new Set([
  'p', 'li', 'dd', 'dt',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'td', 'th',
  'blockquote',
  'a',                        // standalone links
  'span',                     // .code-line / token spans inside <pre>
])

/**
 * Walk preview DOM and collect text spans suitable for an invisible PDF
 * overlay. Spans are kept per visual line (via Range.getClientRects) so
 * selection rectangles align with what the user sees on the image.
 */
function collectTextSpans(preview: HTMLElement, container: HTMLElement): TextSpan[] {
  const containerTop = container.getBoundingClientRect().top
  const containerLeft = container.getBoundingClientRect().left
  const spans: TextSpan[] = []

  // Walk all nodes; capture text-bearing leaves and any code-line spans.
  const walker = document.createTreeWalker(preview, NodeFilter.SHOW_ELEMENT)
  let node: Node | null = walker.currentNode
  while ((node = walker.nextNode())) {
    const el = node as HTMLElement
    const tag = el.tagName.toLowerCase()
    if (SKIP_TEXT_OF.has(tag)) continue
    const isCodeLine = el.classList.contains('code-line')
    if (!TEXT_LEAVES.has(tag) && !isCodeLine) continue

    // .code-line is a TERMINAL leaf even though it has <span class="token">
    // children — those are prism syntax-highlight tokens, and selecting
    // them individually breaks user copy (spaces between tokens lose, line
    // numbers double up). Emit one invisible-text span for the whole line.
    if (!isCodeLine) {
      // If this element contains a TEXT_LEAVES descendant, the descendant
      // will be visited later — skip the parent so we don't duplicate.
      let hasTextChild = false
      for (const child of Array.from(el.children)) {
        const ct = child.tagName.toLowerCase()
        if (TEXT_LEAVES.has(ct) || child.classList.contains('code-line')) {
          hasTextChild = true
          break
        }
      }
      if (hasTextChild) continue
    }

    // Per-visual-line rectangles via Range so wrapped text gets one entry
    // per line. Falls back to single bounding rect if range fails.
    const range = document.createRange()
    try {
      range.selectNodeContents(el)
    } catch {
      continue
    }
    const lineRects = Array.from(range.getClientRects())
    // Preserve indentation/whitespace inside code lines — copy of a code
    // block with collapsed spaces is useless. For prose, collapse runs of
    // whitespace so multi-line markdown like list items render as single
    // logical strings on copy.
    const fullText = isCodeLine
      ? (el.textContent || '').replace(/\s+$/, '')
      : (el.textContent || '').replace(/\s+/g, ' ').trim()
    if (!fullText) continue

    if (lineRects.length === 0) {
      const r = el.getBoundingClientRect()
      if (r.height <= 0 || r.width <= 0) continue
      spans.push({
        text: fullText,
        yCanvas: (r.top - containerTop) * PDF_RENDER_SCALE,
        xCanvas: (r.left - containerLeft) * PDF_RENDER_SCALE,
        widthCanvas: r.width * PDF_RENDER_SCALE,
        heightCanvas: r.height * PDF_RENDER_SCALE,
      })
      continue
    }

    // One pdf.text() per visual line. We don't try to split the text by
    // exact character offsets per rect (that needs binary-searching
    // ranges); instead we attach the full element text to the FIRST line
    // with maxWidth = first line's width. jsPDF will wrap the invisible
    // text to match. Selection across wrapped lines stays correct because
    // copy reads the text stream regardless of position.
    const first = lineRects[0]
    if (first.height <= 0 || first.width <= 0) continue
    spans.push({
      text: fullText,
      yCanvas: (first.top - containerTop) * PDF_RENDER_SCALE,
      xCanvas: (first.left - containerLeft) * PDF_RENDER_SCALE,
      widthCanvas: first.width * PDF_RENDER_SCALE,
      heightCanvas: first.height * PDF_RENDER_SCALE,
    })
  }

  return spans
}

/**
 * Emit invisible pdf.text() calls for every TextSpan that falls inside
 * the current page slice [startCanvasY, endCanvasY).
 *
 * Selection-rect alignment math:
 *   visible line box = [topMm, topMm + lineMm]
 *   jsPDF text(x, y) anchors the glyph BASELINE at y. Glyph box around
 *   baseline is roughly [baseline - ascent, baseline + descent], where
 *   ascent ≈ 0.78 × fontSize_mm and descent ≈ 0.22 × fontSize_mm for
 *   the default Helvetica family.
 *   To make selection rect ≈ visible line box:
 *     fontSize_mm ≈ lineMm / 1.20 (typical line-height multiplier)
 *     baseline   ≈ topMm + lineMm × 0.80
 *   At fontSize ~lineMm/1.2 the glyph box top = baseline - 0.78 × fontSize
 *   ≈ topMm + lineMm × 0.80 - lineMm × 0.65 = topMm + lineMm × 0.15,
 *   close to the visible line top. The slight gap at the very top is
 *   imperceptible in PDF readers (selection extends to text bounds).
 */
function paintInvisibleTextLayer(
  pdf: jsPDF,
  spans: TextSpan[],
  startCanvasY: number,
  endCanvasY: number,
  pxPerMm: number,
): void {
  const prevTextColor = pdf.getTextColor()
  const prevFontSize = pdf.getFontSize()
  pdf.setTextColor(0, 0, 0)

  const MM_PER_PT = 0.3527777
  const LINE_HEIGHT_MULTIPLIER = 1.2
  const BASELINE_RATIO = 0.80

  for (const span of spans) {
    if (span.yCanvas < startCanvasY || span.yCanvas >= endCanvasY) continue

    const xMm = PDF_MARGIN_LEFT + span.xCanvas / pxPerMm
    const yMmFromSliceTop = (span.yCanvas - startCanvasY) / pxPerMm
    const lineMm = span.heightCanvas / pxPerMm
    // Set fontSize so glyph box height ≈ visible line height.
    const fontSizePt = (lineMm / LINE_HEIGHT_MULTIPLIER) / MM_PER_PT
    pdf.setFontSize(Math.max(4, Math.min(72, fontSizePt)))
    const yBaselineMm = PDF_MARGIN_TOP + yMmFromSliceTop + lineMm * BASELINE_RATIO
    const maxWidthMm = Math.max(1, span.widthCanvas / pxPerMm)
    try {
      pdf.text(span.text, xMm, yBaselineMm, {
        renderingMode: 'invisible',
        maxWidth: maxWidthMm,
      })
    } catch {
      // jsPDF can choke on certain glyphs (cyrillic in default font).
      // Best-effort: silently drop that span.
    }
  }

  pdf.setTextColor(prevTextColor)
  pdf.setFontSize(prevFontSize)
}

function collectBreakPointsAndForbidden(
  preview: HTMLElement,
  container: HTMLElement,
): { breakPoints: number[]; forbiddenRanges: Array<[number, number]> } {
  const containerTop = container.getBoundingClientRect().top
  const points = new Set<number>()
  points.add(0)
  const forbiddenRanges: Array<[number, number]> = []

  // Line height approximation for generating sub-break points inside tall blocks
  const LINE_HEIGHT_PX = 20

  // Image-aware breaks: collect top+bottom of every <img> / <picture> / <svg> /
  // .mermaid as both BREAK candidates (so the slicer prefers them over
  // mid-image cuts) AND FORBIDDEN ranges (so a break inside the image is
  // snapped to image-top). Without forbidden ranges, an image taller than the
  // remaining page space could still get cut mid-pixel.
  const imageNodes = preview.querySelectorAll('img, picture, svg, .mermaid-wrapper')
  imageNodes.forEach(node => {
    const el = node as HTMLElement
    const r = el.getBoundingClientRect()
    if (!r || r.height <= 0) return
    const top = r.top - containerTop
    const bottom = r.bottom - containerTop
    if (top > 0) points.add(Math.round(top))
    if (bottom > 0) points.add(Math.round(bottom))
    // Forbidden zone: avoid cutting inside the image (small epsilon at edges
    // so adjacent break candidates at the exact boundary still match).
    forbiddenRanges.push([Math.round(top) + 1, Math.round(bottom) - 1])
  })

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

  return {
    breakPoints: Array.from(points).sort((a, b) => a - b),
    forbiddenRanges,
  }
}

/**
 * Returns the top of any forbidden range that strictly contains `y`, or
 * null when `y` is safe. Used by computePageSlices to snap a chosen break
 * up past an image whose middle would otherwise be cut.
 */
function forbiddenContainingTop(
  y: number,
  forbidden: Array<[number, number]>,
): number | null {
  for (const [top, bottom] of forbidden) {
    if (y > top && y < bottom) return top
  }
  return null
}

/**
 * Compute optimal page slices given break points and page height.
 * Tries to break at element boundaries; falls back to hard cut if an element
 * is taller than a full page. forbiddenRanges (e.g. image rects) repel any
 * candidate that lands inside them — the candidate snaps to the range's top
 * instead.
 */
function computePageSlices(
  breakPoints: number[],
  pageHeightPx: number,
  totalHeightPx: number,
  forbiddenRanges: Array<[number, number]> = [],
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

    // If our chosen break lands INSIDE a forbidden image range, snap to the
    // image's top. This leaves the image entirely on the NEXT page (where it
    // either fits or gets handled as a full-page element on its own).
    const snapTop = forbiddenContainingTop(bestBreak, forbiddenRanges)
    if (snapTop !== null && snapTop > currentStart) {
      bestBreak = snapTop
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
