import { useEffect, useState } from 'react'

const features = [
  { num: '01', label: 'Rich editing', desc: 'CodeMirror 6 · syntax highlighting · vim keybindings' },
  { num: '02', label: 'Live preview', desc: 'GFM · Mermaid diagrams · wikilinks' },
  { num: '03', label: 'AI assistant', desc: 'Claude integration · MCP tools · project context' },
  { num: '04', label: 'Knowledge graph', desc: 'Wikilinks · backlinks · connections' },
  { num: '05', label: 'Search & replace', desc: 'Regex-powered · Cmd+F / Cmd+H' },
]

function WelcomeScreen() {
  const [cursor, setCursor] = useState(true)
  const [visible, setVisible] = useState(0)

  useEffect(() => {
    const iv = setInterval(() => setCursor(c => !c), 530)
    return () => clearInterval(iv)
  }, [])

  useEffect(() => {
    const timers = features.map((_, i) =>
      setTimeout(() => setVisible(v => Math.max(v, i + 1)), 200 + i * 110)
    )
    return () => timers.forEach(clearTimeout)
  }, [])

  return (
    <div className="welcome-root">
      {/* Grid background */}
      <div className="welcome-grid" aria-hidden />

      {/* Watermark */}
      <div className="welcome-watermark" aria-hidden>MD</div>

      {/* Content */}
      <div className="welcome-content">
        {/* Header */}
        <div className="welcome-header">
          <div className="welcome-label">
            <span className="welcome-dot" />
            MARKDOWN EDITOR
          </div>
          <h1 className="welcome-title">
            Your notes,<br />
            <span className="welcome-title-accent">intelligently</span>
            <span className="welcome-cursor" style={{ opacity: cursor ? 1 : 0 }}>_</span>
          </h1>
          <p className="welcome-subtitle">
            Professional markdown editing with AI assistance
          </p>
        </div>

        {/* Divider */}
        <div className="welcome-divider">
          <span className="welcome-divider-label">FEATURES</span>
        </div>

        {/* Feature list */}
        <ul className="welcome-features">
          {features.map((f, i) => (
            <li
              key={f.num}
              className="welcome-feature"
              style={{
                opacity: visible > i ? 1 : 0,
                transform: visible > i ? 'translateY(0)' : 'translateY(8px)',
                transition: 'opacity 0.35s ease, transform 0.35s ease',
              }}
            >
              <span className="welcome-feature-num">{f.num}</span>
              <span className="welcome-feature-label">{f.label}</span>
              <span className="welcome-feature-desc">{f.desc}</span>
            </li>
          ))}
        </ul>

        {/* CTA — desktop */}
        <p className="welcome-cta welcome-cta-desktop">
          <span className="welcome-arrow">←</span>
          select a note from the sidebar to begin
        </p>

        {/* CTA — mobile */}
        <p className="welcome-cta welcome-cta-mobile">
          <span className="welcome-arrow-mobile">↑</span>
          tap ☰ to browse your notes
        </p>
      </div>
    </div>
  )
}

export default WelcomeScreen
