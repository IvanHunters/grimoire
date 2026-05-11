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
			Name:        name,
			Path:        folder.Path,
			ProjectPath: folder.ProjectPath,
			Children:    make([]*models.FolderNode, 0),
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

// MoveFolder renames/moves a folder and updates all affected notes and subfolders
func (s *MongoStorage) MoveFolder(ctx context.Context, fromPath, toPath string) error {
	// Validate paths
	if fromPath == "" || toPath == "" {
		return fmt.Errorf("from and to paths are required")
	}
	if fromPath == toPath {
		return fmt.Errorf("from and to paths are the same")
	}

	// Check if destination folder already exists
	foldersCollection := s.db.Collection(foldersCollection)
	count, err := foldersCollection.CountDocuments(ctx, bson.M{"path": toPath})
	if err != nil {
		return fmt.Errorf("failed to check destination folder: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("destination folder already exists: %s", toPath)
	}

	// Check if source folder exists
	count, err = foldersCollection.CountDocuments(ctx, bson.M{"path": fromPath})
	if err != nil {
		return fmt.Errorf("failed to check source folder: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("source folder not found: %s", fromPath)
	}

	// 1. Update all notes in this folder and subfolders
	notesCollection := s.db.Collection(notesCollection)

	// Find all notes that need updating
	notesFilter := bson.M{
		"$or": []bson.M{
			{"folder": fromPath},
			{"folder": bson.M{"$regex": "^" + regexp.QuoteMeta(fromPath) + "/"}},
		},
	}

	cursor, err := notesCollection.Find(ctx, notesFilter)
	if err != nil {
		return fmt.Errorf("failed to find notes in folder: %w", err)
	}
	defer cursor.Close(ctx)

	var notes []*models.Note
	if err := cursor.All(ctx, &notes); err != nil {
		return fmt.Errorf("failed to decode notes: %w", err)
	}

	// Update each note's folder and path
	for _, note := range notes {
		oldFolder := note.Folder
		oldPath := note.Path

		// Replace folder prefix
		newFolder := strings.Replace(oldFolder, fromPath, toPath, 1)
		newPath := strings.Replace(oldPath, fromPath, toPath, 1)

		// Update note
		update := bson.M{
			"$set": bson.M{
				"folder":     newFolder,
				"path":       newPath,
				"updated_at": time.Now(),
			},
		}

		_, err := notesCollection.UpdateOne(ctx, bson.M{"id": note.ID}, update)
		if err != nil {
			return fmt.Errorf("failed to update note %s: %w", note.ID, err)
		}
	}

	// 2. Update the folder itself
	update := bson.M{"$set": bson.M{"path": toPath}}
	_, err = foldersCollection.UpdateOne(ctx, bson.M{"path": fromPath}, update)
	if err != nil {
		return fmt.Errorf("failed to update folder path: %w", err)
	}

	// 3. Update all subfolders
	subfoldersFilter := bson.M{"path": bson.M{"$regex": "^" + regexp.QuoteMeta(fromPath) + "/"}}

	cursor, err = foldersCollection.Find(ctx, subfoldersFilter)
	if err != nil {
		return fmt.Errorf("failed to find subfolders: %w", err)
	}
	defer cursor.Close(ctx)

	var subfolders []*models.Folder
	if err := cursor.All(ctx, &subfolders); err != nil {
		return fmt.Errorf("failed to decode subfolders: %w", err)
	}

	for _, subfolder := range subfolders {
		oldPath := subfolder.Path
		newPath := strings.Replace(oldPath, fromPath, toPath, 1)

		update := bson.M{"$set": bson.M{"path": newPath}}
		_, err := foldersCollection.UpdateOne(ctx, bson.M{"path": oldPath}, update)
		if err != nil {
			return fmt.Errorf("failed to update subfolder %s: %w", oldPath, err)
		}
	}

	return nil
}

// GetFolder retrieves a folder by path
func (s *MongoStorage) GetFolder(ctx context.Context, path string) (*models.Folder, error) {
	collection := s.db.Collection(foldersCollection)

	var folder models.Folder
	err := collection.FindOne(ctx, bson.M{"path": path}).Decode(&folder)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("folder not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get folder: %w", err)
	}

	return &folder, nil
}

// UpdateFolder updates a folder's metadata
func (s *MongoStorage) UpdateFolder(ctx context.Context, folder *models.Folder) error {
	collection := s.db.Collection(foldersCollection)

	update := bson.M{
		"$set": bson.M{
			"project_path": folder.ProjectPath,
		},
	}

	result, err := collection.UpdateOne(ctx, bson.M{"path": folder.Path}, update)
	if err != nil {
		return fmt.Errorf("failed to update folder: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("folder not found: %s", folder.Path)
	}

	return nil
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
