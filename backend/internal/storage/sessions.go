package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const sessionsCollection = "sessions"

// nameOverrideCacheTTL is the lifetime of the in-memory ListNameOverrides
// result. Sidebar polling + every GetOrCreate/GetOrResume hits this
// function, so without a cache it's a Mongo collection scan per few
// hundred ms. Invalidated by UpsertSessionName / UpdateSessionName so
// renames remain instant.
const nameOverrideCacheTTL = 5 * time.Second

// SessionStorage handles session persistence
type SessionStorage struct {
	db *mongo.Database

	overlayMu     sync.RWMutex
	overlayCache  map[string]string
	overlayLoaded time.Time
}

// NewSessionStorage creates a new session storage
func NewSessionStorage(db *mongo.Database) *SessionStorage {
	return &SessionStorage{db: db}
}

// invalidateOverlay forces the next ListNameOverrides call to refresh
// from Mongo. Called from name-write paths so a rename shows up
// immediately in the sidebar instead of after up-to-TTL latency.
func (s *SessionStorage) invalidateOverlay() {
	s.overlayMu.Lock()
	s.overlayCache = nil
	s.overlayLoaded = time.Time{}
	s.overlayMu.Unlock()
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

// UpdateSessionName updates session name. Requires the session to
// already exist in Mongo — use UpsertSessionName if you want to rename
// a historical session that only has a JSONL transcript on disk.
func (s *SessionStorage) UpdateSessionName(ctx context.Context, sessionID string, name string) error {
	collection := s.db.Collection(sessionsCollection)

	res, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"name":       name,
				"updated_at": time.Now(),
			},
		},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("session not found in database: %s", sessionID)
	}
	s.invalidateOverlay()

	return nil
}

// UpsertSessionName sets the display name for a session, inserting a
// minimal record if it doesn't exist yet. This is how we rename
// historical sessions — they live only as JSONL on disk, but we keep
// the name override in Mongo so list/search queries can surface the
// user's preferred title.
func (s *SessionStorage) UpsertSessionName(ctx context.Context, sessionID string, name string) error {
	collection := s.db.Collection(sessionsCollection)
	opts := options.Update().SetUpsert(true)

	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{
			"$set": bson.M{
				"name":       name,
				"updated_at": time.Now(),
			},
			"$setOnInsert": bson.M{
				"created_at": time.Now(),
				"status":     "historical",
			},
		},
		opts,
	)
	if err == nil {
		s.invalidateOverlay()
	}
	return err
}

// ListNameOverrides returns the map of sessionId → user-chosen display
// name from Mongo. Used as an overlay when listing sessions so renames
// stick across daemon restarts and survive even if the JSONL ai-title
// says something else.
//
// Cached in-memory with nameOverrideCacheTTL. Renames invalidate via
// UpsertSessionName / UpdateSessionName so the user sees their change
// instantly rather than waiting for TTL to expire.
func (s *SessionStorage) ListNameOverrides(ctx context.Context) (map[string]string, error) {
	s.overlayMu.RLock()
	if s.overlayCache != nil && time.Since(s.overlayLoaded) < nameOverrideCacheTTL {
		// Copy under RLock; map is treated as read-only by callers but
		// we don't want them to mutate our cache by reference.
		out := make(map[string]string, len(s.overlayCache))
		for k, v := range s.overlayCache {
			out[k] = v
		}
		s.overlayMu.RUnlock()
		return out, nil
	}
	s.overlayMu.RUnlock()

	// Cache miss / expired — fetch from Mongo. Hold Lock around the
	// final store so concurrent readers see consistent state.
	collection := s.db.Collection(sessionsCollection)
	cur, err := collection.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1, "name": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	fresh := make(map[string]string)
	for cur.Next(ctx) {
		var row struct {
			ID   string `bson:"_id"`
			Name string `bson:"name"`
		}
		if err := cur.Decode(&row); err != nil {
			continue
		}
		// Skip generic / auto-assigned names that were stored as
		// defaults by our handler (not explicit user renames). These
		// would otherwise blanket-override the JSONL ai-title with
		// "Terminal Session" or "grimoire-*" tokens, hiding the real
		// session topic in list views.
		if row.Name == "" ||
			row.Name == "Terminal Session" ||
			row.Name == "(unnamed)" ||
			strings.HasPrefix(row.Name, "grimoire-") {
			continue
		}
		fresh[row.ID] = row.Name
	}
	s.overlayMu.Lock()
	s.overlayCache = fresh
	s.overlayLoaded = time.Now()
	s.overlayMu.Unlock()

	// Return a defensive copy so callers can't mutate the cache.
	out := make(map[string]string, len(fresh))
	for k, v := range fresh {
		out[k] = v
	}
	return out, nil
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
