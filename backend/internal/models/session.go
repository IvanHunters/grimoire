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

// SessionMeta is a lightweight session view without message bodies.
type SessionMeta struct {
	ID           string    `json:"id" bson:"_id"`
	Name         string    `json:"name" bson:"name"`
	Status       string    `json:"status" bson:"status"`
	WorkingDir   string    `json:"workingDir" bson:"working_dir"`
	LastActivity time.Time `json:"lastActivity" bson:"last_activity"`
	CreatedAt    time.Time `json:"createdAt" bson:"created_at"`
	MessageCount int       `json:"messageCount" bson:"message_count"`
	SizeBytes    int       `json:"sizeBytes" bson:"size_bytes"`
}

// SessionStats holds aggregate statistics for the sessions collection.
type SessionStats struct {
	TotalSessions  int     `json:"totalSessions"`
	ActiveSessions int     `json:"activeSessions"`
	TotalMessages  int     `json:"totalMessages"`
	TotalSizeMB    float64 `json:"totalSizeMb"`
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
}
