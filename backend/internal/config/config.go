package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

// Config holds application configuration
type Config struct {
	// HTTP Server
	HTTPPort int `envconfig:"HTTP_PORT" default:"8080"`

	// WebSocket Server
	WSPort int `envconfig:"WS_PORT" default:"3000"`

	// MongoDB
	MongoDBURI      string `envconfig:"MONGODB_URI" default:"mongodb://localhost:27017"`
	MongoDBDatabase string `envconfig:"MONGODB_DATABASE" default:"markdown_editor"`

	// Storage
	DataDir         string `envconfig:"DATA_DIR" default:"./data"`
	MaxUploadSize   int64  `envconfig:"MAX_UPLOAD_SIZE" default:"10485760"` // 10MB
	UploadsDir      string // Computed from DataDir

	// CORS
	AllowedOrigins []string `envconfig:"ALLOWED_ORIGINS" default:"http://localhost:5173"`

	// Logging
	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`

	// Claude
	SessionTimeout int `envconfig:"SESSION_TIMEOUT" default:"300"` // 5 minutes in seconds
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process env config: %w", err)
	}

	// Compute uploads directory
	cfg.UploadsDir = cfg.DataDir + "/uploads"

	// Ensure data directories exist
	if err := os.MkdirAll(cfg.UploadsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create uploads directory: %w", err)
	}

	return &cfg, nil
}

// OriginsContain checks if the given origin is in the allowed origins list
func (c *Config) OriginsContain(origin string) bool {
	for _, o := range c.AllowedOrigins {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}
