package storage

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const notesCollection = "notes"

// MongoStorage implements Storage interface using MongoDB
type MongoStorage struct {
	db *mongo.Database
}

// NewMongoStorage creates a new MongoDB storage
func NewMongoStorage(db *mongo.Database) *MongoStorage {
	return &MongoStorage{db: db}
}

// ListNotes retrieves all notes, optionally filtered by folder
func (s *MongoStorage) ListNotes(ctx context.Context, folder string) ([]*models.Note, error) {
	collection := s.db.Collection(notesCollection)

	filter := bson.M{}
	if folder != "" {
		filter["folder"] = folder
	}

	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list notes: %w", err)
	}
	defer cursor.Close(ctx)

	var notes []*models.Note
	if err := cursor.All(ctx, &notes); err != nil {
		return nil, fmt.Errorf("failed to decode notes: %w", err)
	}

	return notes, nil
}

// GetNote retrieves a note by ID
func (s *MongoStorage) GetNote(ctx context.Context, id string) (*models.Note, error) {
	collection := s.db.Collection(notesCollection)

	var note models.Note
	err := collection.FindOne(ctx, bson.M{"id": id}).Decode(&note)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("note not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get note: %w", err)
	}

	return &note, nil
}

// GetNoteByPath retrieves a note by path
func (s *MongoStorage) GetNoteByPath(ctx context.Context, path string) (*models.Note, error) {
	collection := s.db.Collection(notesCollection)

	var note models.Note
	err := collection.FindOne(ctx, bson.M{"path": path}).Decode(&note)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("note not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get note by path: %w", err)
	}

	return &note, nil
}

// CreateNote creates a new note
func (s *MongoStorage) CreateNote(ctx context.Context, note *models.Note) error {
	collection := s.db.Collection(notesCollection)

	note.CreatedAt = time.Now()
	note.UpdatedAt = time.Now()

	// Parse wikilinks from content
	note.OutgoingLinks = parseWikilinks(note.Content, note.ID)

	_, err := collection.InsertOne(ctx, note)
	if err != nil {
		return fmt.Errorf("failed to create note: %w", err)
	}

	// Update backlinks for linked notes
	if err := s.UpdateBacklinks(ctx, note.ID, note.OutgoingLinks); err != nil {
		return fmt.Errorf("failed to update backlinks: %w", err)
	}

	return nil
}

// UpdateNote updates an existing note
func (s *MongoStorage) UpdateNote(ctx context.Context, note *models.Note) error {
	collection := s.db.Collection(notesCollection)

	note.UpdatedAt = time.Now()

	// Parse wikilinks from content
	note.OutgoingLinks = parseWikilinks(note.Content, note.ID)

	update := bson.M{
		"$set": bson.M{
			"title":          note.Title,
			"path":           note.Path,
			"folder":         note.Folder,
			"content":        note.Content,
			"type":           note.Type,
			"project_path":   note.ProjectPath,
			"updated_at":     note.UpdatedAt,
			"outgoing_links": note.OutgoingLinks,
		},
	}

	result, err := collection.UpdateOne(ctx, bson.M{"id": note.ID}, update)
	if err != nil {
		return fmt.Errorf("failed to update note: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("note not found: %s", note.ID)
	}

	// Update backlinks for linked notes
	if err := s.UpdateBacklinks(ctx, note.ID, note.OutgoingLinks); err != nil {
		return fmt.Errorf("failed to update backlinks: %w", err)
	}

	return nil
}

// DeleteNote deletes a note by ID
func (s *MongoStorage) DeleteNote(ctx context.Context, id string) error {
	collection := s.db.Collection(notesCollection)

	result, err := collection.DeleteOne(ctx, bson.M{"id": id})
	if err != nil {
		return fmt.Errorf("failed to delete note: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("note not found: %s", id)
	}

	// Remove this note from backlinks of all notes it linked to
	if err := s.UpdateBacklinks(ctx, id, []string{}); err != nil {
		return fmt.Errorf("failed to update backlinks after deletion: %w", err)
	}

	// Remove this note from backlinks of all notes that linked to it
	_, err = collection.UpdateMany(
		ctx,
		bson.M{"backlinks": id},
		bson.M{"$pull": bson.M{"backlinks": id}},
	)
	if err != nil {
		return fmt.Errorf("failed to remove from other notes' backlinks: %w", err)
	}

	return nil
}

// SearchNotes performs full-text search on notes
func (s *MongoStorage) SearchNotes(ctx context.Context, query string) ([]*models.Note, error) {
	collection := s.db.Collection(notesCollection)

	filter := bson.M{
		"$text": bson.M{
			"$search": query,
		},
	}

	opts := options.Find().SetProjection(bson.M{
		"score": bson.M{"$meta": "textScore"},
	}).SetSort(bson.D{{Key: "score", Value: bson.M{"$meta": "textScore"}}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to search notes: %w", err)
	}
	defer cursor.Close(ctx)

	var notes []*models.Note
	if err := cursor.All(ctx, &notes); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %w", err)
	}

	return notes, nil
}

// UpdateBacklinks updates backlinks for notes that are referenced
func (s *MongoStorage) UpdateBacklinks(ctx context.Context, noteID string, outgoingLinks []string) error {
	collection := s.db.Collection(notesCollection)

	// Get current note to find previous outgoing links
	currentNote, err := s.GetNote(ctx, noteID)
	if err != nil {
		// If note doesn't exist yet (during creation), skip getting previous links
		if err.Error() != fmt.Sprintf("note not found: %s", noteID) {
			return err
		}
	}

	var previousLinks []string
	if currentNote != nil {
		previousLinks = currentNote.OutgoingLinks
	}

	// Find links that were removed
	removedLinks := difference(previousLinks, outgoingLinks)

	// Find links that were added
	addedLinks := difference(outgoingLinks, previousLinks)

	// Remove noteID from backlinks of notes that are no longer linked
	if len(removedLinks) > 0 {
		_, err = collection.UpdateMany(
			ctx,
			bson.M{"id": bson.M{"$in": removedLinks}},
			bson.M{"$pull": bson.M{"backlinks": noteID}},
		)
		if err != nil {
			return fmt.Errorf("failed to remove backlinks: %w", err)
		}
	}

	// Add noteID to backlinks of newly linked notes
	if len(addedLinks) > 0 {
		_, err = collection.UpdateMany(
			ctx,
			bson.M{"id": bson.M{"$in": addedLinks}},
			bson.M{"$addToSet": bson.M{"backlinks": noteID}},
		)
		if err != nil {
			return fmt.Errorf("failed to add backlinks: %w", err)
		}
	}

	return nil
}

// parseWikilinks extracts wikilinks from markdown content
// Format: [[note-title]] or [[note-title|alias]]
func parseWikilinks(content string, currentNoteID string) []string {
	re := regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	matches := re.FindAllStringSubmatch(content, -1)

	links := make(map[string]bool) // Use map to deduplicate
	for _, match := range matches {
		if len(match) > 1 {
			targetTitle := match[1]
			// TODO: resolve title to note ID (requires lookup by title)
			// For now, store the title as-is
			if targetTitle != "" && targetTitle != currentNoteID {
				links[targetTitle] = true
			}
		}
	}

	result := make([]string, 0, len(links))
	for link := range links {
		result = append(result, link)
	}

	return result
}

// difference returns elements in a that are not in b
func difference(a, b []string) []string {
	mb := make(map[string]bool, len(b))
	for _, x := range b {
		mb[x] = true
	}

	var diff []string
	for _, x := range a {
		if !mb[x] {
			diff = append(diff, x)
		}
	}

	return diff
}

// EnsureIndexes creates required MongoDB indexes
func (s *MongoStorage) EnsureIndexes(ctx context.Context) error {
	collection := s.db.Collection(notesCollection)

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "path", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "folder", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "updated_at", Value: -1}},
		},
		{
			Keys: bson.D{
				{Key: "title", Value: "text"},
				{Key: "content", Value: "text"},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	return nil
}
