import { useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { NotesProvider, useNotes } from './contexts/NotesContext'
import { ClaudeProvider } from './contexts/ClaudeContext'
import HomePage from './pages/HomePage'
import TasksPage from './pages/TasksPage'
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
      <div className="w-full h-screen overflow-hidden" style={{ height: '100dvh' }}>
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/notes/:noteId" element={<HomePage />} />
          <Route path="/tasks" element={<TasksPage />} />
          <Route path="/tasks/:taskId" element={<TasksPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </ClaudeProvider>
  )
}

function App() {
  // Always use dark theme
  useEffect(() => {
    document.documentElement.classList.add('dark')
  }, [])

  // Long tap (500ms) → contextmenu event for mobile right-click menus
  useEffect(() => {
    let timer: number | null = null
    let moved = false

    const onTouchStart = (e: TouchEvent) => {
      moved = false
      const touch = e.touches[0]
      timer = window.setTimeout(() => {
        if (!moved) {
          const evt = new MouseEvent('contextmenu', {
            bubbles: true,
            cancelable: true,
            clientX: touch.clientX,
            clientY: touch.clientY,
          })
          e.target?.dispatchEvent(evt)
        }
        timer = null
      }, 500)
    }
    const cancel = () => {
      if (timer) { clearTimeout(timer); timer = null }
    }
    const onMove = () => { moved = true; cancel() }

    document.addEventListener('touchstart', onTouchStart)
    document.addEventListener('touchend', cancel)
    document.addEventListener('touchmove', onMove)
    document.addEventListener('touchcancel', cancel)
    return () => {
      document.removeEventListener('touchstart', onTouchStart)
      document.removeEventListener('touchend', cancel)
      document.removeEventListener('touchmove', onMove)
      document.removeEventListener('touchcancel', cancel)
    }
  }, [])

  return (
    <BrowserRouter>
      <NotesProvider>
        <AppContent />
      </NotesProvider>
    </BrowserRouter>
  )
}

export default App
