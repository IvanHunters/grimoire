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
	subscribers   []chan []byte          // Fan-out: each WebSocket connection gets its own channel
	subMu         sync.Mutex
	mu            sync.Mutex
}

// SendMessage sends a message to the Claude subprocess
func (s *ClaudeSession) SendMessage(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Write entire message to PTY at once (UTF-8 safe)
	_, err := s.PTY.Write([]byte(content))
	if err != nil {
		return err
	}

	// Send Enter key (CR in terminal)
	_, err = s.PTY.Write([]byte{'\r'})
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

// SubscribeToOutput creates a dedicated channel for a WebSocket connection.
// Each subscriber gets all PTY output independently (fan-out).
func (s *ClaudeSession) SubscribeToOutput() chan []byte {
	ch := make(chan []byte, 256)
	s.subMu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.subMu.Unlock()
	return ch
}

// UnsubscribeFromOutput removes a subscriber channel and closes it.
func (s *ClaudeSession) UnsubscribeFromOutput(ch chan []byte) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for i, sub := range s.subscribers {
		if sub == ch {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// BroadcastOutput sends PTY data to all active subscriber channels.
func (s *ClaudeSession) BroadcastOutput(data []byte) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, sub := range s.subscribers {
		select {
		case sub <- data:
		default:
			// Subscriber is too slow; drop this chunk for it rather than stalling the PTY reader.
		}
	}
}

// CloseAllSubscriptions closes every subscriber channel (called when process exits).
func (s *ClaudeSession) CloseAllSubscriptions() {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, sub := range s.subscribers {
		close(sub)
	}
	s.subscribers = nil
}
