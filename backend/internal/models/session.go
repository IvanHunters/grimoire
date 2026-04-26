package models

import (
	"time"
)

// ClaudeMessage represents a message in Claude conversation
type ClaudeMessage struct {
	Role      string    `json:"role" bson:"role"`           // "user" or "assistant"
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
	Messages      []ClaudeMessage `json:"messages" bson:"messages"`
	CreatedAt     time.Time       `json:"createdAt" bson:"created_at"`
	UpdatedAt     time.Time       `json:"updatedAt" bson:"updated_at"`
	LastActivity  time.Time       `json:"lastActivity" bson:"last_activity"`
}
