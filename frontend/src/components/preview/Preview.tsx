import { forwardRef, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkBreaks from 'remark-breaks'
import rehypePrism from 'rehype-prism-plus'
import WikilinkRenderer from './WikilinkRenderer'
import MermaidDiagram from './MermaidDiagram'

// Helper to generate heading ID from text (same as GFM)
function generateHeadingId(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, '') // Remove punctuation but keep letters, numbers, spaces, dashes
    .replace(/\s+/g, '-') // Replace spaces with dashes
    .replace(/-+/g, '-') // Replace multiple dashes with single
    .replace(/^-|-$/g, '') // Remove leading/trailing dashes
}

// Prism.js окраска через rehype-prism-plus
// Стили okaidia темы в index.css

interface PreviewProps {
  className?: string
  content?: string
}

const Preview = forwardRef<HTMLDivElement, PreviewProps>(({ className = '', content = '' }, ref) => {
  if (!content) {
    return (
      <div ref={ref} className={`flex items-center justify-center ${className}`}>
        <p className="text-gray-500">No content to preview</p>
      </div>
    )
  }

  // Remove frontmatter from content for preview
  const processedContent = useMemo(() => {
    let text = content.replace(/^---\n[\s\S]*?\n---\n/, '')

    // Process wikilinks: convert [[link]] or [[link|alias]] to custom markdown
    // [[target]] → [target](wikilink:target)
    // [[target|alias]] → [alias](wikilink:target)
    text = text.replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_match, target, alias) => {
      const displayText = alias?.trim() || target.trim()
      const targetPath = target.trim()
      return `[${displayText}](wikilink:${targetPath})`
    })

    return text
  }, [content])

  return (
    <div ref={ref} className={`h-full overflow-auto ${className}`}>
      <div className="markdown-preview p-8 max-w-4xl mx-auto">
        <ReactMarkdown
          remarkPlugins={[remarkGfm, remarkBreaks]}
          rehypePlugins={[rehypePrism]}
          urlTransform={(url) => {
            // Preserve wikilink: protocol (ReactMarkdown removes it by default)
            return url
          }}
          components={{
            // Custom heading renderers with ID generation
            h1: ({ node, children, ...props }) => {
              const text = children?.toString() || ''
              const id = generateHeadingId(text)
              return <h1 id={id} {...props}>{children}</h1>
            },
            h2: ({ node, children, ...props }) => {
              const text = children?.toString() || ''
              const id = generateHeadingId(text)
              return <h2 id={id} {...props}>{children}</h2>
            },
            h3: ({ node, children, ...props }) => {
              const text = children?.toString() || ''
              const id = generateHeadingId(text)
              return <h3 id={id} {...props}>{children}</h3>
            },
            h4: ({ node, children, ...props }) => {
              const text = children?.toString() || ''
              const id = generateHeadingId(text)
              return <h4 id={id} {...props}>{children}</h4>
            },
            h5: ({ node, children, ...props }) => {
              const text = children?.toString() || ''
              const id = generateHeadingId(text)
              return <h5 id={id} {...props}>{children}</h5>
            },
            h6: ({ node, children, ...props }) => {
              const text = children?.toString() || ''
              const id = generateHeadingId(text)
              return <h6 id={id} {...props}>{children}</h6>
            },

            // Custom link renderer - handles wikilinks and anchor links
            a: ({ node, href, children, ...props }) => {
              // Check if this is a wikilink (wikilink: protocol)
              if (href?.startsWith('wikilink:')) {
                const target = href.replace('wikilink:', '')
                return (
                  <WikilinkRenderer target={target}>
                    {children}
                  </WikilinkRenderer>
                )
              }

              // Check if this is an anchor link (same page navigation)
              if (href?.startsWith('#')) {
                return (
                  <a
                    {...props}
                    href={href}
                    onClick={(e) => {
                      e.preventDefault()
                      // Decode URL-encoded characters (for Cyrillic, etc.)
                      let targetId = href.slice(1) // Remove #
                      try {
                        targetId = decodeURIComponent(targetId)
                      } catch (err) {
                        // If decoding fails, use as-is
                      }

                      const targetElement = document.getElementById(targetId)
                      if (targetElement) {
                        targetElement.scrollIntoView({ behavior: 'smooth', block: 'start' })
                        // Add temporary highlight
                        targetElement.classList.add('anchor-highlight')
                        setTimeout(() => targetElement.classList.remove('anchor-highlight'), 2000)
                      } else {
                        console.log('Anchor target not found:', targetId)
                      }
                    }}
                  >
                    {children}
                  </a>
                )
              }

              // Regular link - open in new tab
              return (
                <a
                  {...props}
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {children}
                </a>
              )
            },

            // Pre renderer - handle mermaid diagrams and code blocks
            pre: ({ node, children, ...props }) => {
              // Check if this is a mermaid code block
              const codeElement = children as any
              const className = codeElement?.props?.className || ''
              const match = /language-(\w+)/.exec(className)
              const language = match ? match[1] : ''

              // Mermaid diagram - render as div, not pre
              if (language === 'mermaid') {
                // Extract text from React children recursively
                const extractText = (child: any): string => {
                  if (typeof child === 'string') return child
                  if (Array.isArray(child)) return child.map(extractText).join('')
                  if (child?.props?.children) return extractText(child.props.children)
                  return ''
                }

                const code = extractText(codeElement?.props?.children).replace(/\n$/, '')
                return <MermaidDiagram code={code} />
              }

              // Regular code block with copy button
              return (
                <pre tabIndex={0} {...props}>
                  {children}
                  {/* Copy button - показывается при hover */}
                  <button
                    className="copy-button"
                    onClick={(e) => {
                      const codeElement = e.currentTarget.previousElementSibling as HTMLElement
                      const codeText = codeElement?.innerText || ''
                      navigator.clipboard.writeText(codeText)
                      e.currentTarget.classList.add('copied')
                      e.currentTarget.innerHTML = '<i class="fas fa-check"></i> Copied!'
                      setTimeout(() => {
                        e.currentTarget.classList.remove('copied')
                        e.currentTarget.innerHTML = '<i class="fas fa-copy"></i> Copy'
                      }, 2000)
                    }}
                  >
                    <i className="fas fa-copy"></i> Copy
                  </button>
                </pre>
              )
            },
          }}
        >
          {processedContent}
        </ReactMarkdown>
      </div>
    </div>
  )
})

Preview.displayName = 'Preview'

export default Preview
