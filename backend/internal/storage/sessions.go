package storage

import (
	"context"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const sessionsCollection = "sessions"

// SessionStorage handles session persistence
type SessionStorage struct {
	db *mongo.Database
}

// NewSessionStorage creates a new session storage
func NewSessionStorage(db *mongo.Database) *SessionStorage {
	return &SessionStorage{db: db}
}

// SaveSession saves or updates a session in MongoDB
func (s *SessionStorage) SaveSession(ctx context.Context, session *models.ClaudeSession) error {
	collection := s.db.Collection(sessionsCollection)

	session.UpdatedAt = time.Now()

	opts := options.Replace().SetUpsert(true)
	_, err := collection.ReplaceOne(
		ctx,
		bson.M{"_id": session.ID},
		session,
		opts,
	)

	return err
}

// GetSession retrieves a session by ID
func (s *SessionStorage) GetSession(ctx context.Context, sessionID string) (*models.ClaudeSession, error) {
	collection := s.db.Collection(sessionsCollection)

	var session models.ClaudeSession
	err := collection.FindOne(ctx, bson.M{"_id": sessionID}).Decode(&session)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return &session, nil
}

// ListActiveSessions returns all active sessions
func (s *SessionStorage) ListActiveSessions(ctx context.Context) ([]*models.ClaudeSession, error) {
	collection := s.db.Collection(sessionsCollection)

	cursor, err := collection.Find(ctx, bson.M{"status": "active"})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var sessions []*models.ClaudeSession
	if err := cursor.All(ctx, &sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

// ListAllSessions returns all sessions sorted by last activity
func (s *SessionStorage) ListAllSessions(ctx context.Context, limit int) ([]*models.ClaudeSession, error) {
	collection := s.db.Collection(sessionsCollection)

	opts := options.Find().
		SetSort(bson.D{{Key: "last_activity", Value: -1}})

	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var sessions []*models.ClaudeSession
	if err := cursor.All(ctx, &sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

// UpdateSessionStatus updates session status
func (s *SessionStorage) UpdateSessionStatus(ctx context.Context, sessionID string, status string) error {
	collection := s.db.Collection(sessionsCollection)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"status":        status,
				"updated_at":    time.Now(),
				"last_activity": time.Now(),
			},
		},
	)

	return err
}

// UpdateSessionActivity updates last activity timestamp
func (s *SessionStorage) UpdateSessionActivity(ctx context.Context, sessionID string) error {
	collection := s.db.Collection(sessionsCollection)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"last_activity": time.Now(),
				"updated_at":    time.Now(),
			},
		},
	)

	return err
}

// UpdateSessionName updates session name
func (s *SessionStorage) UpdateSessionName(ctx context.Context, sessionID string, name string) error {
	collection := s.db.Collection(sessionsCollection)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"name":       name,
				"updated_at": time.Now(),
			},
		},
	)

	return err
}

// UpdateSessionMessages updates session messages
func (s *SessionStorage) UpdateSessionMessages(ctx context.Context, sessionID string, messages []models.ClaudeMessage) error {
	collection := s.db.Collection(sessionsCollection)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"messages":      messages,
				"last_activity": time.Now(),
				"updated_at":    time.Now(),
			},
		},
	)

	return err
}

// DeleteSession removes a session from MongoDB
func (s *SessionStorage) DeleteSession(ctx context.Context, sessionID string) error {
	collection := s.db.Collection(sessionsCollection)

	_, err := collection.DeleteOne(ctx, bson.M{"_id": sessionID})
	return err
}

// CleanupInactiveSessions marks sessions as inactive if they haven't been active for the specified duration
func (s *SessionStorage) CleanupInactiveSessions(ctx context.Context, inactiveDuration time.Duration) (int, error) {
	collection := s.db.Collection(sessionsCollection)

	threshold := time.Now().Add(-inactiveDuration)

	result, err := collection.UpdateMany(
		ctx,
		bson.M{
			"status":        "active",
			"last_activity": bson.M{"$lt": threshold},
		},
		bson.M{
			"$set": bson.M{
				"status":     "inactive",
				"updated_at": time.Now(),
			},
		},
	)

	if err != nil {
		return 0, err
	}

	return int(result.ModifiedCount), nil
}

// CreateSessionsIndexes creates indexes for sessions collection
func (s *SessionStorage) CreateSessionsIndexes(ctx context.Context) error {
	collection := s.db.Collection(sessionsCollection)

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "last_activity", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	return err
}
