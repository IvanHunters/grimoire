package cmd

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
	"github.com/ivanohotnikov/markdown-editor/internal/claude"
	"github.com/ivanohotnikov/markdown-editor/internal/config"
	mw "github.com/ivanohotnikov/markdown-editor/internal/middleware"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
	"github.com/ivanohotnikov/markdown-editor/internal/websocket"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start HTTP and WebSocket servers",
	Long:  `Starts both HTTP API server (:8080) and WebSocket server (:3000)`,
	RunE:  runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	logger.Info("starting servers",
		slog.Int("http_port", cfg.HTTPPort),
		slog.Int("ws_port", cfg.WSPort),
	)

	// Connect to MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoDBURI))
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping mongodb: %w", err)
	}
	logger.Info("connected to mongodb")

	db := client.Database(cfg.MongoDBDatabase)

	// Ensure indexes
	store := storage.NewMongoStorage(db)
	if err := store.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("failed to ensure indexes: %w", err)
	}
	if err := store.EnsureFolderIndexes(ctx); err != nil {
		return fmt.Errorf("failed to ensure folder indexes: %w", err)
	}

	// Setup session storage
	sessionStorage := storage.NewSessionStorage(db)
	if err := sessionStorage.CreateSessionsIndexes(ctx); err != nil {
		return fmt.Errorf("failed to create session indexes: %w", err)
	}
	logger.Info("indexes created")

	// Setup HTTP server
	handler := api.NewHandler(cfg, db, logger)
	httpRouter := chi.NewRouter()
	httpRouter.Use(mw.Recovery(logger))
	httpRouter.Use(mw.Logging(logger))
	httpRouter.Use(mw.CORS(cfg))
	httpRouter.Use(middleware.Compress(5))

	// Create MCP server
	mcpServer := CreateMCPServer(store, logger)
	mcpHTTPServer := server.NewStreamableHTTPServer(mcpServer)

	// Routes
	httpRouter.Get("/health", handler.Health)
	httpRouter.Route("/api", func(r chi.Router) {
		r.Get("/notes", handler.ListNotes)
		r.Get("/notes/project-suggestions", handler.ProjectSuggestions)
		r.Get("/notes/{id}", handler.GetNote)
		r.Post("/notes", handler.CreateNote)
		r.Put("/notes/{id}", handler.UpdateNote)
		r.Delete("/notes/{id}", handler.DeleteNote)

		r.Get("/folders", handler.ListFolders)
		r.Post("/folders", handler.CreateFolder)
		r.Delete("/folders", handler.DeleteFolder)
		r.Put("/folders/move", handler.MoveFolder)

		r.Get("/search", handler.Search)
		r.Post("/upload", handler.Upload)
	})

	// MCP endpoint (outside /api for simplicity)
	httpRouter.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		logger.Info("mcp http request", slog.String("method", r.Method), slog.String("path", r.URL.Path))
		mcpHTTPServer.ServeHTTP(w, r)
	})

	fileServer := http.FileServer(http.Dir(cfg.UploadsDir))
	httpRouter.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))

	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      httpRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Setup WebSocket server
	manager := claude.GetSessionManager(logger, sessionStorage, cfg.MongoDBURI, cfg.MongoDBDatabase)
	timeout := time.Duration(cfg.SessionTimeout) * time.Second
	manager.MonitorInactiveSessions(timeout, 1*time.Minute)

	wsHandler := websocket.NewHandler(cfg, manager, logger)
	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/claude-chat", wsHandler.HandleWebSocket)

	wsSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.WSPort),
		Handler:      wsMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start servers in goroutines
	go func() {
		logger.Info("http server listening", slog.Int("port", cfg.HTTPPort))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", slog.Any("error", err))
		}
	}()

	go func() {
		logger.Info("websocket server listening", slog.Int("port", cfg.WSPort))
		if err := wsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("websocket server error", slog.Any("error", err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down servers...")

	// Close all Claude sessions
	manager.CloseAll()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server forced to shutdown", slog.Any("error", err))
	}
	if err := wsSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("websocket server forced to shutdown", slog.Any("error", err))
	}

	logger.Info("servers stopped")
	return nil
}

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
