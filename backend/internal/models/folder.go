package models

import (
	"time"
)

// FolderSource identifies where the folder is mirrored from. Empty for
// normal in-database folders. "skills" means the folder mirrors
// ~/.claude/skills/ on disk and writes go through the skills syncer.
type FolderSource string

const (
	FolderSourceSkills FolderSource = "skills"
)

// Folder represents a folder in the file system
type Folder struct {
	Path        string       `json:"path" bson:"path"`
	ProjectPath string       `json:"projectPath,omitempty" bson:"project_path,omitempty"`
	IsSystem    bool         `json:"isSystem,omitempty" bson:"is_system,omitempty"`
	Source      FolderSource `json:"source,omitempty" bson:"source,omitempty"`
	CreatedAt   time.Time    `json:"createdAt" bson:"created_at"`
}

// FolderNode represents a node in the folder tree
type FolderNode struct {
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	ProjectPath string        `json:"projectPath,omitempty"`
	IsSystem    bool          `json:"isSystem,omitempty"`
	Source      FolderSource  `json:"source,omitempty"`
	Children    []*FolderNode `json:"children,omitempty"`
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
