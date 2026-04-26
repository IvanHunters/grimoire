package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ivanohotnikov/markdown-editor/internal/api"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	mw "github.com/ivanohotnikov/markdown-editor/internal/middleware"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	logger.Info("starting server",
		slog.Int("port", cfg.HTTPPort),
		slog.String("mongodb_uri", cfg.MongoDBURI),
	)

	// Connect to MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoDBURI))
	if err != nil {
		logger.Error("failed to connect to mongodb", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			logger.Error("failed to disconnect from mongodb", slog.Any("error", err))
		}
	}()

	// Ping MongoDB to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		logger.Error("failed to ping mongodb", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("connected to mongodb")

	db := client.Database(cfg.MongoDBDatabase)

	// Ensure indexes
	store := storage.NewMongoStorage(db)
	if err := store.EnsureIndexes(ctx); err != nil {
		logger.Error("failed to ensure indexes", slog.Any("error", err))
		os.Exit(1)
	}
	if err := store.EnsureFolderIndexes(ctx); err != nil {
		logger.Error("failed to ensure folder indexes", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("indexes created")

	// Create handler
	handler := api.NewHandler(cfg, db, logger)

	// Setup router
	r := chi.NewRouter()

	// Apply middlewares
	r.Use(mw.Recovery(logger))
	r.Use(mw.Logging(logger))
	r.Use(mw.CORS(cfg))
	r.Use(middleware.Compress(5))

	// Routes
	r.Get("/health", handler.Health)

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Notes endpoints
		r.Get("/notes", handler.ListNotes)
		r.Get("/notes/project-suggestions", handler.ProjectSuggestions)
		r.Get("/notes/{id}", handler.GetNote)
		r.Post("/notes", handler.CreateNote)
		r.Put("/notes/{id}", handler.UpdateNote)
		r.Delete("/notes/{id}", handler.DeleteNote)

		// Folders endpoints
		r.Get("/folders", handler.ListFolders)
		r.Post("/folders", handler.CreateFolder)
		r.Delete("/folders", handler.DeleteFolder)
		r.Put("/folders/move", handler.MoveFolder)

		// Search endpoint
		r.Get("/search", handler.Search)

		// Upload endpoint
		r.Post("/upload", handler.Upload)
	})

	// Serve static files (uploads)
	fileServer := http.FileServer(http.Dir(cfg.UploadsDir))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.HTTPPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	// Graceful shutdown
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("server stopped")
}

// parseLogLevel converts string log level to slog.Level
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
