function WelcomeScreen() {
  return (
    <div className="welcome-screen">
      <h1>Markdown Editor</h1>
      <p>Professional markdown editing with AI assistance</p>

      <div className="instructions">
        <h3>Get Started</h3>
        <ul>
          <li>📝 Rich editing with CodeMirror 6</li>
          <li>👁️ Live preview with GFM support</li>
          <li>🤖 AI assistant powered by Claude</li>
          <li>🔗 Knowledge graph with wikilinks</li>
          <li>🔍 Advanced search & replace</li>
          <li>📊 Mermaid diagrams support</li>
        </ul>
        <p style={{ marginTop: '16px', fontSize: '14px', opacity: 0.8 }}>
          Select a note from the sidebar to begin
        </p>
      </div>
    </div>
  )
}

export default WelcomeScreen
