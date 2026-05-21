package storage

import (
	"context"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// Storage defines the interface for data persistence
type Storage interface {
	// Notes
	ListNotes(ctx context.Context, folder string) ([]*models.Note, error)
	ListNotesMeta(ctx context.Context, folder string, recursive bool) ([]*models.Note, error)
	GetNote(ctx context.Context, id string) (*models.Note, error)
	GetNoteByPath(ctx context.Context, path string) (*models.Note, error)
	CreateNote(ctx context.Context, note *models.Note) error
	UpdateNote(ctx context.Context, note *models.Note) error
	DeleteNote(ctx context.Context, id string) error
	SearchNotes(ctx context.Context, query string, limit int) ([]*models.Note, error)
	SearchByTags(tags []string, limit int) []*models.Note
	GetAllTags() map[string]int
	BuildTagsIndex(ctx context.Context) error

	// Folders
	ListFolders(ctx context.Context) ([]*models.Folder, error)
	CreateFolder(ctx context.Context, folder *models.Folder) error
	DeleteFolder(ctx context.Context, path string) error

	// Backlinks
	UpdateBacklinks(ctx context.Context, noteID string, outgoingLinks []string) error

	// Initialize indexes
	EnsureIndexes(ctx context.Context) error
	EnsureFolderIndexes(ctx context.Context) error
}
