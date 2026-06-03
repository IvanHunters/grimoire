package api

import (
	"log/slog"

	"github.com/go-playground/validator/v10"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"github.com/ivanohotnikov/markdown-editor/internal/skills"
	"go.mongodb.org/mongo-driver/mongo"
)

// TaskRunner triggers or cancels a scheduled task.
type TaskRunner interface {
	RunNow(taskID string) error
	CancelNow(taskID string) error
}

// Handler handles HTTP requests
type Handler struct {
	cfg            *config.Config
	db             *mongo.Database
	validator      *validator.Validate
	logger         *slog.Logger
	sessionManager *claude.SessionManager
	taskRunner     TaskRunner // optional — set after scheduler is created
	skills         *skills.Syncer
	skillSettings  *skills.SettingsStore
}

// NewHandler creates a new HTTP handler
func NewHandler(cfg *config.Config, db *mongo.Database, sessionManager *claude.SessionManager, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:            cfg,
		db:             db,
		validator:      validator.New(),
		logger:         logger,
		sessionManager: sessionManager,
	}
}

// SetTaskRunner wires the scheduler into the handler after startup.
func (h *Handler) SetTaskRunner(r TaskRunner) { h.taskRunner = r }

// SetSkills wires the skills syncer and settings store into the handler.
func (h *Handler) SetSkills(syncer *skills.Syncer, settings *skills.SettingsStore) {
	h.skills = syncer
	h.skillSettings = settings
}
