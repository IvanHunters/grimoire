import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { NotesProvider } from './contexts/NotesContext'
import HomePage from './pages/HomePage'

function App() {
  return (
    <BrowserRouter>
      <NotesProvider>
        <div className="w-full h-screen overflow-hidden">
          <Routes>
            <Route path="/" element={<HomePage />} />
            <Route path="/notes/:noteId" element={<HomePage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
      </NotesProvider>
    </BrowserRouter>
  )
}

export default App
