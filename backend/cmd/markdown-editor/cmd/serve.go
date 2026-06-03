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
	"github.com/ivanohotnikov/markdown-editor/internal/scheduler"
	"github.com/ivanohotnikov/markdown-editor/internal/skills"
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
	if err := store.EnsureTaskIndexes(ctx); err != nil {
		return fmt.Errorf("failed to ensure task indexes: %w", err)
	}
	logger.Info("mongodb indexes created")

	// Build in-memory tags index
	if err := store.BuildTagsIndex(ctx); err != nil {
		return fmt.Errorf("failed to build tags index: %w", err)
	}

	// Setup session storage
	sessionStorage := storage.NewSessionStorage(db)
	if err := sessionStorage.CreateSessionsIndexes(ctx); err != nil {
		return fmt.Errorf("failed to create session indexes: %w", err)
	}
	logger.Info("session indexes created")

	// Setup SessionManager (needed by both HTTP and WebSocket servers)
	manager := claude.GetSessionManager(logger, sessionStorage, cfg.MongoDBURI, cfg.MongoDBDatabase)
	// Automatic session cleanup disabled - user closes sessions manually
	// timeout := time.Duration(cfg.SessionTimeout) * time.Second
	// manager.MonitorInactiveSessions(timeout, 1*time.Minute)

	// Setup task scheduler
	sched := scheduler.New(store, cfg.MongoDBURI, cfg.MongoDBDatabase, logger)
	schedCtx, schedCancel := context.WithCancel(context.Background())
	go sched.Start(schedCtx)

	// Setup skills syncer: mirror ~/.claude/skills/ into Mongo and propagate edits to disk.
	skillsRoot, err := skills.DefaultRoot()
	if err != nil {
		return fmt.Errorf("resolve skills root: %w", err)
	}
	settingsPath, err := skills.DefaultSettingsPath()
	if err != nil {
		return fmt.Errorf("resolve settings path: %w", err)
	}
	skillSettings := skills.NewSettingsStore(settingsPath)
	skillSyncer := skills.NewSyncer(skillsRoot, store, skillSettings, logger)
	if err := skillSyncer.ImportAll(ctx); err != nil {
		logger.Warn("skills import failed", slog.Any("error", err))
	} else {
		logger.Info("skills imported", slog.String("root", skillsRoot))
	}
	if err := skillSyncer.Start(); err != nil {
		logger.Warn("skills watcher failed to start", slog.Any("error", err))
	}
	defer skillSyncer.Stop()

	// Setup HTTP server
	handler := api.NewHandler(cfg, db, manager, logger)
	handler.SetTaskRunner(sched)
	handler.SetSkills(skillSyncer, skillSettings)
	wsHandler := websocket.NewHandler(cfg, manager, store, logger)
	httpRouter := chi.NewRouter()
	httpRouter.Use(mw.Recovery(logger))
	httpRouter.Use(mw.Logging(logger))
	httpRouter.Use(mw.CORS(cfg))
	// Compress is applied per-route group, NOT globally:
	// WebSocket upgrade (http.Hijack) breaks if ResponseWriter is wrapped by compress.

	// Create MCP server
	mcpServer := CreateMCPServerWithSkills(store, sessionStorage, logger, cfg, skillSyncer, skillSettings)
	mcpHTTPServer := server.NewStreamableHTTPServer(mcpServer)

	// Routes
	httpRouter.Get("/health", handler.Health)
	httpRouter.Route("/api", func(r chi.Router) {
		r.Use(middleware.Compress(5))
		r.Get("/notes", handler.ListNotes)
		r.Get("/notes/project-suggestions", handler.ProjectSuggestions)
		r.Get("/notes/{id}", handler.GetNote)
		r.Post("/notes", handler.CreateNote)
		r.Put("/notes/{id}", handler.UpdateNote)
		r.Delete("/notes/{id}", handler.DeleteNote)

		r.Get("/folders", handler.ListFolders)
		r.Post("/folders", handler.CreateFolder)
		r.Put("/folders", handler.UpdateFolder)
		r.Delete("/folders", handler.DeleteFolder)
		r.Put("/folders/move", handler.MoveFolder)

		r.Get("/search", handler.Search)
		r.Get("/search/tags", handler.SearchByTags)
		r.Get("/tags", handler.GetAllTags)
		r.Post("/upload", handler.Upload)

		r.Get("/export/db", handler.ExportDB)
		r.Post("/import/db", handler.ImportDB)

		r.Get("/sessions", handler.ListSessions)
		r.Get("/sessions/stats", handler.SessionStats)
		r.Get("/sessions/all", handler.ListAllSessions)
		r.Post("/sessions/rotate", handler.RotateSessions)
		r.Delete("/sessions/{id}", handler.DeleteSession)
		r.Put("/sessions/{id}/name", handler.RenameSession)
		r.Delete("/sessions/{id}/history", handler.ClearSessionHistory)

		// Projects
		r.Get("/projects", handler.ListProjects)
		r.Post("/projects", handler.CreateProject)
		r.Put("/projects/{id}", handler.UpdateProject)
		r.Delete("/projects/{id}", handler.DeleteProject)

		// Tasks
		r.Get("/task-columns", handler.GetTaskColumns)
		r.Put("/task-columns", handler.SetTaskColumns)
		r.Get("/task-project-folders", handler.GetTaskProjectFolders)
		r.Put("/task-project-folders", handler.SetTaskProjectFolders)

		r.Get("/tasks/search", handler.SearchTasks)
		r.Get("/tasks", handler.ListTasks)
		r.Get("/tasks/{id}", handler.GetTask)
		r.Post("/tasks", handler.CreateTask)
		r.Put("/tasks/{id}", handler.UpdateTask)
		r.Delete("/tasks/{id}", handler.DeleteTask)
		r.Post("/tasks/{id}/run-now", handler.RunTaskNow)
		r.Post("/tasks/{id}/cancel", handler.CancelTaskNow)

		// Task comments
		r.Post("/tasks/{id}/comments", handler.AddComment)
		r.Put("/tasks/{id}/comments/{commentId}", handler.UpdateComment)
		r.Delete("/tasks/{id}/comments/{commentId}", handler.DeleteComment)

		// Skills (mirror of ~/.claude/skills/)
		r.Get("/skills", handler.ListSkills)
		r.Post("/skills", handler.CreateSkill)
		r.Delete("/skills/{name}", handler.DeleteSkill)
		r.Post("/skills/{name}/state", handler.SetSkillState)
		r.Post("/skills/refresh", handler.RefreshSkills)
	})

	// WebSocket endpoint (on same port as HTTP API, no separate server needed)
	httpRouter.HandleFunc("/claude-chat", wsHandler.HandleWebSocket)

	// MCP endpoint — injects session_id query param into context so tools can self-identify.
	// Subprocess Claude is configured to call /mcp?session_id=<id>.
	httpRouter.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		logger.Info("mcp http request", slog.String("method", r.Method), slog.String("path", r.URL.Path))
		if sid := r.URL.Query().Get("session_id"); sid != "" {
			r = r.WithContext(context.WithValue(r.Context(), sessionIDKey{}, sid))
		}
		mcpHTTPServer.ServeHTTP(w, r)
	})

	fileServer := http.FileServer(http.Dir(cfg.UploadsDir))
	httpRouter.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))

	httpSrv := &http.Server{
		Addr:        fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:     httpRouter,
		IdleTimeout: 60 * time.Second,
		// No ReadTimeout/WriteTimeout: WebSocket connections on /claude-chat are long-lived.
		// HTTP-level write timeouts kill active WebSocket connections.
	}

	// Start server in goroutine
	go func() {
		logger.Info("http+websocket server listening", slog.Int("port", cfg.HTTPPort))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", slog.Any("error", err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down servers...")

	// Stop scheduler
	schedCancel()

	// Close all Claude sessions
	manager.CloseAll()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server forced to shutdown", slog.Any("error", err))
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
