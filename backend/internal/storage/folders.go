package storage

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const foldersCollection = "folders"

// ListFolders retrieves all folders
func (s *MongoStorage) ListFolders(ctx context.Context) ([]*models.Folder, error) {
	collection := s.db.Collection(foldersCollection)

	opts := options.Find().SetSort(bson.D{{Key: "path", Value: 1}})

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}
	defer cursor.Close(ctx)

	var folders []*models.Folder
	if err := cursor.All(ctx, &folders); err != nil {
		return nil, fmt.Errorf("failed to decode folders: %w", err)
	}

	return folders, nil
}

// CreateFolder creates a new folder
func (s *MongoStorage) CreateFolder(ctx context.Context, folder *models.Folder) error {
	collection := s.db.Collection(foldersCollection)

	folder.CreatedAt = time.Now()

	_, err := collection.InsertOne(ctx, folder)
	if err != nil {
		// Check if folder already exists
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("folder already exists: %s", folder.Path)
		}
		return fmt.Errorf("failed to create folder: %w", err)
	}

	return nil
}

// DeleteFolder deletes a folder and all its notes
func (s *MongoStorage) DeleteFolder(ctx context.Context, path string) error {
	// First, delete all notes in this folder and subfolders
	notesCollection := s.db.Collection(notesCollection)

	// Find all notes that belong to this folder or its subfolders
	filter := bson.M{
		"$or": []bson.M{
			{"folder": path},
			{"folder": bson.M{"$regex": "^" + regexp.QuoteMeta(path) + "/"}},
		},
	}

	_, err := notesCollection.DeleteMany(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete notes in folder: %w", err)
	}

	// Delete the folder itself
	foldersCollection := s.db.Collection(foldersCollection)

	result, err := foldersCollection.DeleteOne(ctx, bson.M{"path": path})
	if err != nil {
		return fmt.Errorf("failed to delete folder: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("folder not found: %s", path)
	}

	// Delete all subfolders
	subfoldersFilter := bson.M{"path": bson.M{"$regex": "^" + regexp.QuoteMeta(path) + "/"}}
	_, err = foldersCollection.DeleteMany(ctx, subfoldersFilter)
	if err != nil {
		return fmt.Errorf("failed to delete subfolders: %w", err)
	}

	return nil
}

// BuildFolderTree converts flat folder list to tree structure
func BuildFolderTree(folders []*models.Folder) *models.FolderNode {
	root := &models.FolderNode{
		Name:     "",
		Path:     "",
		Children: make([]*models.FolderNode, 0),
	}

	// Create a map for quick lookup
	nodeMap := make(map[string]*models.FolderNode)
	nodeMap[""] = root

	// Sort folders by path depth to ensure parents are processed before children
	sort.Slice(folders, func(i, j int) bool {
		return strings.Count(folders[i].Path, "/") < strings.Count(folders[j].Path, "/")
	})

	for _, folder := range folders {
		// Create node for this folder
		parts := strings.Split(folder.Path, "/")
		name := parts[len(parts)-1]

		node := &models.FolderNode{
			Name:     name,
			Path:     folder.Path,
			Children: make([]*models.FolderNode, 0),
		}

		nodeMap[folder.Path] = node

		// Find parent
		parentPath := ""
		if len(parts) > 1 {
			parentPath = strings.Join(parts[:len(parts)-1], "/")
		}

		if parent, exists := nodeMap[parentPath]; exists {
			parent.Children = append(parent.Children, node)
		}
	}

	return root
}

// EnsureFolderIndexes creates required MongoDB indexes for folders collection
func (s *MongoStorage) EnsureFolderIndexes(ctx context.Context) error {
	collection := s.db.Collection(foldersCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "path", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("failed to create folder indexes: %w", err)
	}

	return nil
}
