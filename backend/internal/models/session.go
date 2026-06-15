package models

import (
	"time"
)

// ClaudeMessage represents a message in Claude conversation
type ClaudeMessage struct {
	Role      string    `json:"role" bson:"role"`
	Content   string    `json:"content" bson:"content"`
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
}

// ClaudeSession represents a Claude chat session (for persistence)
type ClaudeSession struct {
	ID            string          `json:"id" bson:"_id"`
	Name          string          `json:"name" bson:"name"`
	DangerousMode bool            `json:"dangerousMode" bson:"dangerous_mode"`
	WorkingDir    string          `json:"workingDir" bson:"working_dir"`
	MCPConfigPath string          `json:"mcpConfigPath,omitempty" bson:"mcp_config_path,omitempty"`
	Status        string          `json:"status" bson:"status"` // "active", "inactive", "terminated"
	Notes         string          `json:"notes,omitempty" bson:"notes,omitempty"`
	Messages      []ClaudeMessage `json:"messages" bson:"messages"`
	CreatedAt     time.Time       `json:"createdAt" bson:"created_at"`
	UpdatedAt     time.Time       `json:"updatedAt" bson:"updated_at"`
	LastActivity  time.Time       `json:"lastActivity" bson:"last_activity"`

	// Transient live state, populated only by ListActiveSessions for
	// the sessions list endpoint — never persisted to Mongo. Empty for
	// historical / persisted sessions.
	Tempo  string `json:"tempo,omitempty" bson:"-"`
	State  string `json:"state,omitempty" bson:"-"`
	Detail string `json:"detail,omitempty" bson:"-"`
	Needs  string `json:"needs,omitempty" bson:"-"`
}
