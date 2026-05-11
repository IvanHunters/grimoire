package events

import (
	"sync"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// EventType represents type of event
type EventType string

const (
	EventNoteCreated   EventType = "note_created"
	EventNoteUpdated   EventType = "note_updated"
	EventNoteDeleted   EventType = "note_deleted"
	EventFolderCreated EventType = "folder_created"
	EventFolderDeleted EventType = "folder_deleted"
	EventTaskCreated   EventType = "task_created"
	EventTaskUpdated   EventType = "task_updated"
	EventTaskDeleted   EventType = "task_deleted"
)

// Event represents a system event
type Event struct {
	Type   EventType      `json:"type"`
	Note   *models.Note   `json:"note,omitempty"`
	Folder *models.Folder `json:"folder,omitempty"`
	Task   *models.Task   `json:"task,omitempty"`
	NoteID string         `json:"noteId,omitempty"`
	TaskID string         `json:"taskId,omitempty"`
	Path   string         `json:"path,omitempty"`
}

// EventBus manages event subscriptions and publishing
type EventBus struct {
	subscribers []chan Event
	mu          sync.RWMutex
}

var globalBus *EventBus
var busOnce sync.Once

// GetEventBus returns the global event bus instance
func GetEventBus() *EventBus {
	busOnce.Do(func() {
		globalBus = &EventBus{
			subscribers: make([]chan Event, 0),
		}
	})
	return globalBus
}

// Subscribe creates a new subscription channel for events
func (b *EventBus) Subscribe() chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 10) // Buffered channel
	b.subscribers = append(b.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscription channel
func (b *EventBus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, sub := range b.subscribers {
		if sub == ch {
			close(ch)
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			break
		}
	}
}

// Publish sends an event to all subscribers
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		select {
		case sub <- event:
			// Event sent
		default:
			// Channel full, skip (non-blocking)
		}
	}
}
