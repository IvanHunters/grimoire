import { createContext, useContext, useState, useEffect, useMemo } from 'react'
import type { ReactNode } from 'react'
import type { Note } from '../types/note'
import type { FolderNode } from '../types/folder'
import { notesAPI } from '../api/notes'
import { foldersAPI } from '../api/folders'

// Helper function to flatten folder tree into array
function flattenFolderTree(node: FolderNode): FolderNode[] {
  const result: FolderNode[] = []

  function traverse(n: FolderNode) {
    if (n.path) { // Skip root node
      result.push(n)
    }
    if (n.children) {
      n.children.forEach(traverse)
    }
  }

  traverse(node)
  return result
}

interface NotesContextValue {
  notes: Note[]
  currentNote: Note | null
  folderTree: FolderNode
  folders: FolderNode[] // Flattened array for legacy components
  loading: boolean
  error: string | null

  // Notes operations
  fetchNotes: () => Promise<void>
  createNote: (title: string, folder?: string, content?: string) => Promise<Note>
  updateNote: (id: string, content: string) => Promise<void>
  deleteNote: (id: string) => Promise<void>
  setCurrentNote: (note: Note | null) => void

  // Folders operations
  fetchFolders: () => Promise<void>
  createFolder: (path: string) => Promise<void>
  deleteFolder: (path: string) => Promise<void>
}

const NotesContext = createContext<NotesContextValue | undefined>(undefined)

export function NotesProvider({ children }: { children: ReactNode }) {
  const [notes, setNotes] = useState<Note[]>([])
  const [currentNote, setCurrentNote] = useState<Note | null>(null)
  const [folderTree, setFolderTree] = useState<FolderNode>({ name: '', path: '', children: [] })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Flatten folder tree for legacy components
  const folders = useMemo(() => flattenFolderTree(folderTree), [folderTree])

  // Fetch notes on mount
  useEffect(() => {
    fetchNotes()
    fetchFolders()
  }, [])

  const fetchNotes = async () => {
    try {
      setLoading(true)
      setError(null)
      const data = await notesAPI.listNotes()
      setNotes(data)
    } catch (err) {
      setError('Failed to fetch notes')
      console.error('Error fetching notes:', err)
      // Use mock data for development
      setNotes(getMockNotes())
    } finally {
      setLoading(false)
    }
  }

  const fetchFolders = async () => {
    try {
      const data = await foldersAPI.getFolders()
      setFolderTree(data)
    } catch (err) {
      console.error('Error fetching folders:', err)
      // Use mock data for development
      setFolderTree(getMockFolders())
    }
  }

  const createNote = async (
    title: string,
    folder?: string,
    content?: string
  ): Promise<Note> => {
    try {
      setError(null)
      const newNote = await notesAPI.createNote({
        title,
        folder,
        content,
      })
      setNotes((prev) => [...(prev || []), newNote])
      return newNote
    } catch (err) {
      setError('Failed to create note')
      console.error('Error creating note:', err)
      throw err
    }
  }

  const updateNote = async (id: string, content: string) => {
    try {
      setError(null)
      const updated = await notesAPI.updateNote(id, { content })

      setNotes((prev) =>
        (prev || []).map((note) => (note.id === id ? updated : note))
      )

      if (currentNote?.id === id) {
        setCurrentNote(updated)
      }
    } catch (err) {
      setError('Failed to update note')
      console.error('Error updating note:', err)
      throw err
    }
  }

  const deleteNote = async (id: string) => {
    try {
      setError(null)
      await notesAPI.deleteNote(id)
      setNotes((prev) => (prev || []).filter((note) => note.id !== id))

      if (currentNote?.id === id) {
        setCurrentNote(null)
      }
    } catch (err) {
      setError('Failed to delete note')
      console.error('Error deleting note:', err)
      throw err
    }
  }

  const createFolder = async (path: string) => {
    try {
      setError(null)
      await foldersAPI.createFolder({ path })
      await fetchFolders()
    } catch (err) {
      setError('Failed to create folder')
      console.error('Error creating folder:', err)
      throw err
    }
  }

  const deleteFolder = async (path: string) => {
    try {
      setError(null)
      await foldersAPI.deleteFolder(path)
      await fetchFolders()
    } catch (err) {
      setError('Failed to delete folder')
      console.error('Error deleting folder:', err)
      throw err
    }
  }

  const value: NotesContextValue = {
    notes,
    currentNote,
    folderTree,
    folders,
    loading,
    error,
    fetchNotes,
    createNote,
    updateNote,
    deleteNote,
    setCurrentNote,
    fetchFolders,
    createFolder,
    deleteFolder,
  }

  return <NotesContext.Provider value={value}>{children}</NotesContext.Provider>
}

export function useNotes() {
  const context = useContext(NotesContext)
  if (context === undefined) {
    throw new Error('useNotes must be used within NotesProvider')
  }
  return context
}

// Mock data for development (until backend is ready)
function getMockNotes(): Note[] {
  return [
    {
      id: '1',
      title: 'Getting Started',
      path: 'projects/getting-started.md',
      folder: 'projects',
      content: `---
id: 1
title: Getting Started
type: project
project_path:
created_at: 2024-01-15T10:00:00Z
updated_at: 2024-01-15T10:00:00Z
---

# Getting Started

Welcome to Markdown Editor!

## Features

- **Rich Editing**: CodeMirror 6 with syntax highlighting
- **Live Preview**: Real-time markdown rendering
- **AI Assistant**: Claude integration
- **Graph View**: Visualize note connections

## Quick Start

1. Create a new note
2. Start writing in markdown
3. Use wikilinks to connect notes: [[TODO List]] or [[Personal Notes|my notes]]
4. Ask Claude for help

Check out the [[Wikilinks Guide]] for more examples!

## Example Code

\`\`\`go
func main() {
    fmt.Println("Hello, Markdown!")
}
\`\`\`

## Table Example

| Feature | Status |
|---------|--------|
| Editor  | ✅     |
| Preview | ✅     |
| Claude  | 🚧     |
`,
      createdAt: '2024-01-15T10:00:00Z',
      updatedAt: '2024-01-15T10:00:00Z',
    },
    {
      id: '2',
      title: 'TODO List',
      path: 'projects/todo.md',
      folder: 'projects',
      content: `---
id: 2
title: TODO List
type: project
project_path:
created_at: 2024-01-15T11:00:00Z
updated_at: 2024-01-15T11:00:00Z
---

# TODO List

## Frontend
- [x] Setup Vite + React + TypeScript
- [x] Install dependencies
- [x] Create layout components
- [ ] Implement Editor (CodeMirror)
- [ ] Implement Preview
- [ ] Implement Claude Chat
- [ ] Implement Graph View

## Backend
- [ ] Setup Go server
- [ ] MongoDB integration
- [ ] API endpoints
- [ ] Claude MCP server
- [ ] WebSocket for chat

## Testing
- [ ] Unit tests
- [ ] Integration tests
- [ ] E2E tests
`,
      createdAt: '2024-01-15T11:00:00Z',
      updatedAt: '2024-01-15T11:00:00Z',
    },
    {
      id: '3',
      title: 'Personal Notes',
      path: 'personal/notes.md',
      folder: 'personal',
      content: `---
id: 3
title: Personal Notes
type: regular
created_at: 2024-01-15T12:00:00Z
updated_at: 2024-01-15T12:00:00Z
---

# Personal Notes

Random thoughts and ideas.

See also: [[TODO List]] for project tasks.
`,
      createdAt: '2024-01-15T12:00:00Z',
      updatedAt: '2024-01-15T12:00:00Z',
    },
    {
      id: '4',
      title: 'Wikilinks Guide',
      path: 'projects/wikilinks-guide.md',
      folder: 'projects',
      content: `---
id: 4
title: Wikilinks Guide
type: project
created_at: 2024-01-16T10:00:00Z
updated_at: 2024-01-16T10:00:00Z
---

# Wikilinks Guide

Wikilinks allow you to create connections between notes using \`[[double brackets]]\` syntax.

## Basic Syntax

**Simple link:**
\`[[Getting Started]]\` → [[Getting Started]]

**Link with alias:**
\`[[TODO List|My TODO]]\` → [[TODO List|My TODO]]

**Path-based link:**
\`[[personal/notes]]\` → [[personal/notes]]

## Features

When you hover over a wikilink, you'll see a popup preview of the linked note.

Click on a wikilink to navigate to that note instantly.

## Example Connections

This guide connects to:
- [[Getting Started]] - Introduction and quick start
- [[TODO List]] - Project tasks and roadmap
- [[Personal Notes|My personal notes]] - Random thoughts

## Broken Links

Links to non-existent notes appear differently:
[[Non-existent Note]] (this note doesn't exist)

## Use Cases

1. **Knowledge Management**: Create a web of interconnected notes
2. **Project Documentation**: Link related project notes
3. **Daily Notes**: Reference previous entries
4. **Research**: Build connections between sources

## See Also

- [[Getting Started]] - Main introduction
- [[TODO List]] - What's next
`,
      createdAt: '2024-01-16T10:00:00Z',
      updatedAt: '2024-01-16T10:00:00Z',
    },
  ]
}

function getMockFolders(): FolderNode {
  return {
    name: '',
    path: '',
    children: [
      {
        name: 'projects',
        path: 'projects',
        isCollapsed: false,
      },
      {
        name: 'personal',
        path: 'personal',
        isCollapsed: true,
      },
      {
        name: 'archive',
        path: 'archive',
        isCollapsed: true,
      },
    ],
  }
}
