package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const settingsCollection = "settings"

type columnsDoc struct {
	ID      string               `bson:"_id"`
	Columns []models.KanbanColumn `bson:"columns"`
}

func (s *MongoStorage) GetKanbanColumns(ctx context.Context) ([]models.KanbanColumn, error) {
	var doc columnsDoc
	err := s.db.Collection(settingsCollection).FindOne(ctx, bson.M{"_id": "kanban_columns"}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return doc.Columns, err
}

func (s *MongoStorage) SetKanbanColumns(ctx context.Context, cols []models.KanbanColumn) error {
	_, err := s.db.Collection(settingsCollection).ReplaceOne(
		ctx,
		bson.M{"_id": "kanban_columns"},
		columnsDoc{ID: "kanban_columns", Columns: cols},
		options.Replace().SetUpsert(true),
	)
	return err
}

type projectFoldersDoc struct {
	ID      string   `bson:"_id"`
	Folders []string `bson:"folders"`
}

func (s *MongoStorage) GetTaskProjectFolders(ctx context.Context) ([]string, error) {
	var doc projectFoldersDoc
	err := s.db.Collection(settingsCollection).FindOne(ctx, bson.M{"_id": "task_project_folders"}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return []string{}, nil
	}
	return doc.Folders, err
}

func (s *MongoStorage) SetTaskProjectFolders(ctx context.Context, folders []string) error {
	_, err := s.db.Collection(settingsCollection).ReplaceOne(
		ctx,
		bson.M{"_id": "task_project_folders"},
		projectFoldersDoc{ID: "task_project_folders", Folders: folders},
		options.Replace().SetUpsert(true),
	)
	return err
}

const (
	projectsCollection = "projects"
	tasksCollection    = "tasks"
)

// EnsureTaskIndexes creates indexes for tasks and projects collections
func (s *MongoStorage) EnsureTaskIndexes(ctx context.Context) error {
	_, err := s.db.Collection(tasksCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "project_id", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "updated_at", Value: -1}}},
		{Keys: bson.D{{Key: "recurring.next_run_at", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("task indexes: %w", err)
	}
	_, err = s.db.Collection(projectsCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	if err != nil {
		return fmt.Errorf("project indexes: %w", err)
	}
	return nil
}

// ── Projects ──────────────────────────────────────────────────────────────────

func (s *MongoStorage) ListProjects(ctx context.Context) ([]*models.Project, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := s.db.Collection(projectsCollection).Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer cursor.Close(ctx)

	var projects []*models.Project
	if err := cursor.All(ctx, &projects); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}
	if projects == nil {
		projects = []*models.Project{}
	}
	return projects, nil
}

func (s *MongoStorage) GetProject(ctx context.Context, id string) (*models.Project, error) {
	var p models.Project
	err := s.db.Collection(projectsCollection).FindOne(ctx, bson.M{"id": id}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("project not found")
	}
	return &p, err
}

func (s *MongoStorage) CreateProject(ctx context.Context, p *models.Project) error {
	_, err := s.db.Collection(projectsCollection).InsertOne(ctx, p)
	return err
}

func (s *MongoStorage) UpdateProject(ctx context.Context, p *models.Project) error {
	p.UpdatedAt = time.Now()
	_, err := s.db.Collection(projectsCollection).ReplaceOne(ctx, bson.M{"id": p.ID}, p)
	return err
}

func (s *MongoStorage) DeleteProject(ctx context.Context, id string) error {
	_, err := s.db.Collection(projectsCollection).DeleteOne(ctx, bson.M{"id": id})
	return err
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (s *MongoStorage) ListTasks(ctx context.Context, projectID, status, folderPath string) ([]*models.Task, error) {
	filter := bson.M{}
	if projectID != "" {
		filter["project_id"] = projectID
	}
	if status != "" {
		filter["status"] = status
	}
	if folderPath != "" {
		filter["linked_folder_paths"] = folderPath
	}

	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}})
	cursor, err := s.db.Collection(tasksCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer cursor.Close(ctx)

	var tasks []*models.Task
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, fmt.Errorf("decode tasks: %w", err)
	}
	if tasks == nil {
		tasks = []*models.Task{}
	}
	return tasks, nil
}

// SearchTasks does a case-insensitive regex search across title, description, and comment content.
func (s *MongoStorage) SearchTasks(ctx context.Context, query string, projectID string) ([]*models.Task, error) {
	re := bson.M{"$regex": query, "$options": "i"}
	filter := bson.M{
		"$or": bson.A{
			bson.M{"title": re},
			bson.M{"description": re},
			bson.M{"comments.content": re},
		},
	}
	if projectID != "" {
		filter["project_id"] = projectID
	}
	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}}).SetLimit(50)
	cursor, err := s.db.Collection(tasksCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("search tasks: %w", err)
	}
	defer cursor.Close(ctx)
	var tasks []*models.Task
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}
	if tasks == nil {
		tasks = []*models.Task{}
	}
	return tasks, nil
}

func (s *MongoStorage) GetTask(ctx context.Context, id string) (*models.Task, error) {
	var t models.Task
	err := s.db.Collection(tasksCollection).FindOne(ctx, bson.M{"id": id}).Decode(&t)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("task not found")
	}
	return &t, err
}

func (s *MongoStorage) CreateTask(ctx context.Context, t *models.Task) error {
	_, err := s.db.Collection(tasksCollection).InsertOne(ctx, t)
	return err
}

func (s *MongoStorage) UpdateTask(ctx context.Context, t *models.Task) error {
	t.UpdatedAt = time.Now()
	_, err := s.db.Collection(tasksCollection).ReplaceOne(ctx, bson.M{"id": t.ID}, t)
	return err
}

func (s *MongoStorage) DeleteTask(ctx context.Context, id string) error {
	_, err := s.db.Collection(tasksCollection).DeleteOne(ctx, bson.M{"id": id})
	return err
}

func (s *MongoStorage) ListRecurringDueTasks(ctx context.Context) ([]models.Task, error) {
	now := time.Now()
	cursor, err := s.db.Collection(tasksCollection).Find(ctx, bson.M{
		"recurring.enabled":   true,
		"recurring.next_run_at": bson.M{"$lte": now},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var tasks []models.Task
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}
