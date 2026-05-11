import type { Note } from './note'
import type { Folder } from './folder'

// Real-time event types from backend
export type EventType =
  | 'note_created'
  | 'note_updated'
  | 'note_deleted'
  | 'folder_created'
  | 'folder_deleted'

// Real-time event from WebSocket
export interface RealtimeEvent {
  type: EventType
  note?: Note
  folder?: Folder
  noteId?: string
  path?: string
}

// Event handler type
export type EventHandler = (event: RealtimeEvent) => void
