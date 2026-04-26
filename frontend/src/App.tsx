import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { NotesProvider, useNotes } from './contexts/NotesContext'
import { ClaudeProvider } from './contexts/ClaudeContext'
import HomePage from './pages/HomePage'
import type { RealtimeEvent } from './types/events'

function AppContent() {
  const { fetchNotes, fetchFolders } = useNotes()

  const handleRealtimeEvent = (event: RealtimeEvent) => {
    console.log('Real-time event:', event)

    // Refresh data when changes occur via Claude
    if (event.type === 'note_created' || event.type === 'note_updated' || event.type === 'note_deleted') {
      fetchNotes()
    }

    if (event.type === 'folder_created' || event.type === 'folder_deleted') {
      fetchFolders()
    }
  }

  return (
    <ClaudeProvider onRealtimeEvent={handleRealtimeEvent}>
      <div className="w-full h-screen overflow-hidden">
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/notes/:noteId" element={<HomePage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </ClaudeProvider>
  )
}

function App() {
  return (
    <BrowserRouter>
      <NotesProvider>
        <AppContent />
      </NotesProvider>
    </BrowserRouter>
  )
}

export default App
