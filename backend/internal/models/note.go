package models

import (
	"time"
)

// Note represents a markdown note
type Note struct {
	ID             string    `json:"id" bson:"id"`
	Path           string    `json:"path" bson:"path"`
	Title          string    `json:"title" bson:"title"`
	Folder         string    `json:"folder" bson:"folder"`
	Content        string    `json:"content" bson:"content"`
	Type           string    `json:"type,omitempty" bson:"type,omitempty"`         // "project" or empty
	ProjectPath    string    `json:"projectPath,omitempty" bson:"project_path,omitempty"`
	CreatedAt      time.Time `json:"createdAt" bson:"created_at"`
	UpdatedAt      time.Time `json:"updatedAt" bson:"updated_at"`
	OutgoingLinks  []string  `json:"outgoingLinks,omitempty" bson:"outgoing_links,omitempty"`
	Backlinks      []string  `json:"backlinks,omitempty" bson:"backlinks,omitempty"`
}

// CreateNoteRequest represents a request to create a new note
type CreateNoteRequest struct {
	Title       string `json:"title" validate:"required"`
	Folder      string `json:"folder"`
	Content     string `json:"content"`
	Type        string `json:"type,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`
}

// UpdateNoteRequest represents a request to update an existing note
type UpdateNoteRequest struct {
	Title       string `json:"title,omitempty"`
	Content     string `json:"content,omitempty"`
	Type        string `json:"type,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`
}

// MoveNoteRequest represents a request to move a note
type MoveNoteRequest struct {
	NoteID    string `json:"noteId" validate:"required"`
	NewFolder string `json:"newFolder" validate:"required"`
}
