package models

import (
	"time"
)

// Folder represents a folder in the file system
type Folder struct {
	Path      string    `json:"path" bson:"path"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
}

// FolderNode represents a node in the folder tree
type FolderNode struct {
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Children []*FolderNode `json:"children,omitempty"`
}

// CreateFolderRequest represents a request to create a new folder
type CreateFolderRequest struct {
	Path string `json:"path" validate:"required"`
}

// MoveFolderRequest represents a request to move a folder
type MoveFolderRequest struct {
	From string `json:"from" validate:"required"`
	To   string `json:"to" validate:"required"`
}
