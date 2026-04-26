package claude

import (
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// ClaudeSession represents an active Claude subprocess session
type ClaudeSession struct {
	ID            string
	Cmd           *exec.Cmd
	PTY           *os.File // Pseudo-terminal for interactive control
	DangerousMode bool
	WorkingDir    string
	LastActivity  time.Time
	Messages      []models.ClaudeMessage // History stored on backend
	mu            sync.Mutex
}

// SendMessage sends a message to the Claude subprocess
func (s *ClaudeSession) SendMessage(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Write message to PTY
	_, err := s.PTY.Write([]byte(content + "\n"))
	if err != nil {
		return err
	}

	// Update last activity
	s.LastActivity = time.Now()

	// Add message to history
	s.Messages = append(s.Messages, models.ClaudeMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})

	return nil
}

// Stop sends Ctrl+C to the Claude subprocess (interrupts generation)
func (s *ClaudeSession) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Send Ctrl+C (ASCII 3) to PTY
	_, err := s.PTY.Write([]byte{3})
	return err
}

// AddAssistantMessage adds an assistant message to history
func (s *ClaudeSession) AddAssistantMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = append(s.Messages, models.ClaudeMessage{
		Role:      "assistant",
		Content:   content,
		Timestamp: time.Now(),
	})

	s.LastActivity = time.Now()
}

// GetMessages returns a copy of message history
func (s *ClaudeSession) GetMessages() []models.ClaudeMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := make([]models.ClaudeMessage, len(s.Messages))
	copy(messages, s.Messages)
	return messages
}

// UpdateActivity updates the last activity timestamp
func (s *ClaudeSession) UpdateActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.LastActivity = time.Now()
}

// IsInactive checks if the session has been inactive for the given duration
func (s *ClaudeSession) IsInactive(timeout time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return time.Since(s.LastActivity) > timeout
}
