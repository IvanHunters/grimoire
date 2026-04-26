package claude

import (
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

const maxOutputBufferSize = 500 * 1024 // 500KB terminal output buffer

// ClaudeSession represents an active Claude subprocess session
type ClaudeSession struct {
	ID            string
	Name          string
	Cmd           *exec.Cmd
	PTY           *os.File // Pseudo-terminal for interactive control
	DangerousMode bool
	WorkingDir    string
	MCPConfigPath string
	CreatedAt     time.Time
	LastActivity  time.Time
	Messages      []models.ClaudeMessage // History stored on backend
	OutputBuffer  []byte                 // Circular buffer for terminal output (last 500KB)
	outputChan    chan []byte            // Channel to broadcast PTY output to all WebSocket clients
	mu            sync.Mutex
}

// SendMessage sends a message to the Claude subprocess
func (s *ClaudeSession) SendMessage(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Write message to PTY byte by byte (emulate typing)
	for _, ch := range content {
		_, err := s.PTY.Write([]byte{byte(ch)})
		if err != nil {
			return err
		}
	}

	// Send Enter key (CR in terminal)
	_, err := s.PTY.Write([]byte{'\r'})
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

// AppendOutput appends terminal output to buffer (keeps last maxOutputBufferSize bytes)
func (s *ClaudeSession) AppendOutput(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.OutputBuffer = append(s.OutputBuffer, data...)

	// If buffer exceeds max size, keep only last maxOutputBufferSize bytes
	if len(s.OutputBuffer) > maxOutputBufferSize {
		// Keep last maxOutputBufferSize bytes
		s.OutputBuffer = s.OutputBuffer[len(s.OutputBuffer)-maxOutputBufferSize:]
	}
}

// GetOutputBuffer returns a copy of the output buffer
func (s *ClaudeSession) GetOutputBuffer() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	buffer := make([]byte, len(s.OutputBuffer))
	copy(buffer, s.OutputBuffer)
	return buffer
}

// SubscribeToOutput returns a channel that will receive all PTY output
func (s *ClaudeSession) SubscribeToOutput() <-chan []byte {
	return s.outputChan
}

// GetOutputChan returns the output channel (for internal use)
func (s *ClaudeSession) GetOutputChan() chan<- []byte {
	return s.outputChan
}
