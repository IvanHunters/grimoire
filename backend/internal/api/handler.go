package api

import (
	"log/slog"

	"github.com/go-playground/validator/v10"
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
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
