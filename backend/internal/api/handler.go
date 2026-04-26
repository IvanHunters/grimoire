package api

import (
	"log/slog"

	"github.com/go-playground/validator/v10"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	"go.mongodb.org/mongo-driver/mongo"
)

// Handler handles HTTP requests
type Handler struct {
	cfg       *config.Config
	db        *mongo.Database
	validator *validator.Validate
	logger    *slog.Logger
}

// NewHandler creates a new HTTP handler
func NewHandler(cfg *config.Config, db *mongo.Database, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:       cfg,
		db:        db,
		validator: validator.New(),
		logger:    logger,
	}
}
