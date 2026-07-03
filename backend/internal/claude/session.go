package claude

import (
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/daemon"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

const (
	maxOutputBufferSize = 500 * 1024 // 500KB terminal output buffer
	// maxMessageHistory caps ClaudeSession.Messages so a long-running
	// session can't grow the in-memory + persisted slice forever. Older
	// turns are trimmed from the head when this length is exceeded.
	maxMessageHistory = 1000
	// maxMessageContent caps a single message's Content length before it
	// goes into Messages. Prevents a pasted log of hundreds of KB from
	// blowing past the per-doc Mongo limit on UpdateSessionMessages.
	maxMessageContent = 64 * 1024
)

// ErrWorkerShutdown is returned by writes to a session whose worker has
// been torn down (PTY nil-ed by ShutdownWorker). Handlers should surface
// a "session restarting" message rather than crashing.
var ErrWorkerShutdown = errors.New("session worker has been shut down")

// IsShutdownErr reports whether err is ErrWorkerShutdown. Used by
// handler code to decide between "log and surface error" vs "quietly
// drop input" when a worker has been intentionally killed (compact,
// restart) but the WS connection is still alive forwarding keystrokes.
func IsShutdownErr(err error) bool { return errors.Is(err, ErrWorkerShutdown) }

// ClaudeSession represents an active Claude session. Backed by either a
// local subprocess (Cmd != nil, PTY is a creack/pty *os.File) or by the
// claude daemon (Cmd == nil, PTY is a *daemon.AttachConn). Callers should
// not assume which — use the methods, not the underlying type.
type ClaudeSession struct {
	ID            string
	Name          string
	Cmd           *exec.Cmd          // nil when daemon-backed
	PTY           io.ReadWriteCloser // PTY for subprocess; AttachConn for daemon
	DangerousMode bool
	WorkingDir    string
	MCPConfigPath string
	CreatedAt     time.Time
	LastActivity  time.Time
	// LastUserInputAt is updated every time the user types something
	// (terminal_input WS message). Used by ListActiveSessions to
	// distinguish "claude really working" (recent user input + daemon
	// active) from "stale active" (no user input for ages but daemon
	// still reports active). Zero value = no input ever.
	LastUserInputAt time.Time
	Messages        []models.ClaudeMessage // History stored on backend
	OutputBuffer    []byte                 // Circular buffer for terminal output (last 500KB)

	// Daemon-backend fields. All nil/empty for subprocess sessions.
	DaemonClient *daemon.Client // socket client; non-nil ↔ daemon-backed
	DaemonShort  string         // 8-hex short id used by daemon ops
	DaemonUUID   string         // full UUID claude assigned (differs from ID)

	// ContextPromptSent is set to true after the handler has injected
	// the "SESSION CONTEXT" prompt into the terminal once. Without this
	// flag, every WS reconnect within seconds of session creation would
	// re-paste the entire context block into a working terminal.
	ContextPromptSent bool

	// readerDone signals that the PTY reader goroutine has exited.
	// ShutdownWorker can wait on this before clearing PTY/Cmd so the
	// reader doesn't see torn-down state mid-Read. Closed exactly once
	// by SignalReaderDone. Lazy-init via readerDoneInit so callers can
	// take the channel before the reader actually starts.
	readerDone     chan struct{}
	readerDoneInit sync.Once
	readerDoneSig  sync.Once

	// procExit signals that the subprocess Cmd.Wait goroutine has
	// returned. Reading cmd.ProcessState directly races against the
	// Wait goroutine writing it (Go race detector flags it); checking
	// this channel via non-blocking select instead is race-free.
	// Subprocess-backend only; nil for daemon-backed sessions.
	procExit     chan struct{}
	procExitInit sync.Once
	procExitSig  sync.Once

	subscribers []chan []byte // Fan-out: each WebSocket connection gets its own channel
	subMu       sync.Mutex
	// mu guards mutable fields above (Name, LastActivity, LastUserInputAt,
	// ContextPromptSent, Messages, OutputBuffer, DangerousMode). Lock is
	// FINE-GRAINED — never held during PTY I/O. Use writeMu for that.
	mu sync.Mutex
	// writeMu serialises concurrent PTY writes between user input
	// (handleTerminalInput) and grimoire-internal messages (SendMessage,
	// Stop, context-prompt injection). The kernel doesn't atomically
	// interleave separate Write calls into a unix socket so we serialise
	// at the application level; this also stops a slow daemon-side
	// writer from blocking metadata mutations (those acquire mu, not
	// writeMu).
	writeMu sync.Mutex
}

// IsDaemonBacked reports whether this session is hosted by the claude
// daemon vs. a local subprocess we own.
func (s *ClaudeSession) IsDaemonBacked() bool { return s.DaemonClient != nil }

// Resize forwards a terminal-size change to the right backend: the daemon
// over op:resize, or the local PTY via creack/pty.Setsize. The latter is
// implemented in subprocess.go because it requires a *os.File.
func (s *ClaudeSession) Resize(cols, rows int) error {
	// Snapshot PTY under mu so we don't race ShutdownWorker nil-assigning it.
	s.mu.Lock()
	pty := s.PTY
	s.mu.Unlock()
	if pty == nil {
		return ErrWorkerShutdown
	}
	if s.IsDaemonBacked() {
		if ac, ok := pty.(*daemon.AttachConn); ok {
			return ac.Resize(cols, rows)
		}
	}
	return resizeSubprocessPTY(pty, cols, rows)
}

// WriteInput safely writes bytes to the PTY. Used by handleTerminalInput
// per-keystroke. Returns ErrWorkerShutdown if the worker has been torn
// down (PTY nil after ShutdownWorker). Holds writeMu to serialise
// against SendMessage / Stop / internal injectors.
func (s *ClaudeSession) WriteInput(data []byte) error {
	s.mu.Lock()
	pty := s.PTY
	s.mu.Unlock()
	if pty == nil {
		return ErrWorkerShutdown
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := pty.Write(data)
	return err
}

// SendMessage writes a user prompt + CR to the PTY and appends it to
// history. PTY I/O happens under writeMu (never under mu), so a stalled
// daemon socket can't block status reads / activity timestamps.
func (s *ClaudeSession) SendMessage(content string) error {
	// Snapshot PTY under mu — protects against PTY=nil race with ShutdownWorker.
	s.mu.Lock()
	pty := s.PTY
	s.mu.Unlock()
	if pty == nil {
		return ErrWorkerShutdown
	}

	// Serialise PTY writes; do NOT hold mu here — daemon socket writes
	// have no deadline and may block on a stalled remote, which would
	// deadlock the PTY reader goroutine trying to AppendOutput.
	s.writeMu.Lock()
	// Single Write of body+CR — separate Writes can interleave with a
	// concurrent WriteInput between two locked sections; one call is
	// kernel-atomic on a unix-socket up to PIPE_BUF.
	payload := make([]byte, 0, len(content)+1)
	payload = append(payload, content...)
	payload = append(payload, '\r')
	_, err := pty.Write(payload)
	s.writeMu.Unlock()
	if err != nil {
		return err
	}

	// Touch state under mu (fast — no I/O).
	s.appendUserMessage(content)
	return nil
}

// Stop sends Ctrl+C to the Claude subprocess (interrupts generation).
// Same PTY-snapshot + writeMu pattern as SendMessage.
func (s *ClaudeSession) Stop() error {
	s.mu.Lock()
	pty := s.PTY
	s.mu.Unlock()
	if pty == nil {
		return ErrWorkerShutdown
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := pty.Write([]byte{3})
	return err
}

// MarkUserInput records that the user just sent terminal input. Cheap
// lock-protected setter used by the WS handler so ListActiveSessions can
// distinguish real activity from daemon-side stale-active reports.
func (s *ClaudeSession) MarkUserInput() {
	s.mu.Lock()
	s.LastUserInputAt = time.Now()
	s.mu.Unlock()
}

// MarkContextPromptSent flips ContextPromptSent under mu so the WS init
// handler can't race with ListActiveSessions reading it for status.
func (s *ClaudeSession) MarkContextPromptSent() {
	s.mu.Lock()
	s.ContextPromptSent = true
	s.mu.Unlock()
}

// HasContextPromptSent returns the flag under lock so handler-side
// "skip if already sent" checks are race-free.
func (s *ClaudeSession) HasContextPromptSent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ContextPromptSent
}

// ClearPTY zero-es the PTY field under mu. Called from startPTYReader's
// defer so a dead reader leaves the manager entry in a "stub" state —
// next GetOrAttach/GetOrResume detects PTY==nil and re-spawns instead
// of handing the caller a zombie session whose Subscribe* calls return
// channels nobody ever broadcasts to. Also clears DaemonShort since
// the old short is meaningless once the AttachConn is dead.
func (s *ClaudeSession) ClearPTY() {
	s.mu.Lock()
	s.PTY = nil
	s.DaemonShort = ""
	s.mu.Unlock()
}

// SetName updates the display name under mu. Safe to call concurrently
// with ListActiveSessions / GetName.
func (s *ClaudeSession) SetName(name string) {
	s.mu.Lock()
	s.Name = name
	s.mu.Unlock()
}

// GetName returns the display name under lock.
func (s *ClaudeSession) GetName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Name
}

// SetDangerousMode updates the dangerous-mode flag under mu so it's
// safe to flip concurrently with any reader (ListActiveSessions /
// GetSessionStatus).
func (s *ClaudeSession) SetDangerousMode(v bool) {
	s.mu.Lock()
	s.DangerousMode = v
	s.mu.Unlock()
}

// GetDangerousMode returns the dangerous-mode flag under lock.
func (s *ClaudeSession) GetDangerousMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DangerousMode
}

// appendUserMessage adds a user message and trims history if needed.
// Must run under no lock (acquires mu internally).
func (s *ClaudeSession) appendUserMessage(content string) {
	if len(content) > maxMessageContent {
		content = content[:maxMessageContent]
	}
	s.mu.Lock()
	s.LastActivity = time.Now()
	s.Messages = append(s.Messages, models.ClaudeMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	s.trimMessagesLocked()
	s.mu.Unlock()
}

// AddAssistantMessage adds an assistant message and trims history if needed.
func (s *ClaudeSession) AddAssistantMessage(content string) {
	if len(content) > maxMessageContent {
		content = content[:maxMessageContent]
	}
	s.mu.Lock()
	s.Messages = append(s.Messages, models.ClaudeMessage{
		Role:      "assistant",
		Content:   content,
		Timestamp: time.Now(),
	})
	s.LastActivity = time.Now()
	s.trimMessagesLocked()
	s.mu.Unlock()
}

// trimMessagesLocked keeps the most recent maxMessageHistory entries.
// Caller must hold s.mu.
func (s *ClaudeSession) trimMessagesLocked() {
	if len(s.Messages) > maxMessageHistory {
		// Copy into a fresh slice to release the old backing array for GC.
		drop := len(s.Messages) - maxMessageHistory
		trimmed := make([]models.ClaudeMessage, maxMessageHistory)
		copy(trimmed, s.Messages[drop:])
		s.Messages = trimmed
	}
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

// GetLastActivity returns LastActivity under lock — for ListActiveSessions
// snapshot pass.
func (s *ClaudeSession) GetLastActivity() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastActivity
}

// GetLastUserInputAt returns LastUserInputAt under lock.
func (s *ClaudeSession) GetLastUserInputAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastUserInputAt
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
// Nils the removed slot before re-slicing so the closed channel becomes
// garbage-collectable immediately — without this, the backing array
// retains a reference until the next reallocation.
func (s *ClaudeSession) UnsubscribeFromOutput(ch chan []byte) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for i, sub := range s.subscribers {
		if sub == ch {
			s.subscribers[i] = nil
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			if len(s.subscribers) == 0 {
				s.subscribers = nil // release backing array
			}
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
	for i, sub := range s.subscribers {
		close(sub)
		s.subscribers[i] = nil
	}
	s.subscribers = nil
}

// ReaderDoneSignal returns the channel that's closed when the PTY
// reader goroutine exits. ShutdownWorker uses it to wait for the reader
// before nil-ing PTY. Lazy-initialised so the channel is safe to take
// at any point — close is performed exactly once by SignalReaderDone.
func (s *ClaudeSession) ReaderDoneSignal() <-chan struct{} {
	s.readerDoneInit.Do(func() {
		s.readerDone = make(chan struct{})
	})
	return s.readerDone
}

// SignalReaderDone closes the readerDone channel exactly once. Called
// from startPTYReader's defer to release any goroutine waiting on
// ReaderDoneSignal.
func (s *ClaudeSession) SignalReaderDone() {
	// Ensure the channel exists even if no waiter has called
	// ReaderDoneSignal yet (otherwise close-of-nil-chan panics).
	s.readerDoneInit.Do(func() {
		s.readerDone = make(chan struct{})
	})
	s.readerDoneSig.Do(func() {
		close(s.readerDone)
	})
}

// ProcExitSignal returns the channel that's closed when the subprocess
// Cmd.Wait goroutine returns. Use IsProcessDone for the common
// "did it exit yet" check; this accessor exists for callers that need
// to block until exit. Lazy-initialized.
func (s *ClaudeSession) ProcExitSignal() <-chan struct{} {
	s.procExitInit.Do(func() {
		s.procExit = make(chan struct{})
	})
	return s.procExit
}

// SignalProcExit closes the procExit channel exactly once. Called by
// the cmd.Wait goroutine in startClaudeSubprocess after Wait returns.
func (s *ClaudeSession) SignalProcExit() {
	s.procExitInit.Do(func() {
		s.procExit = make(chan struct{})
	})
	s.procExitSig.Do(func() {
		close(s.procExit)
	})
}

// IsProcessDone reports whether the subprocess Cmd has exited, as
// observed via the procExit channel. Race-free replacement for
// `cmd.ProcessState != nil && cmd.ProcessState.Exited()` which races
// against cmd.Wait writing ProcessState. Returns true for daemon-
// backed sessions (no subprocess to wait on).
func (s *ClaudeSession) IsProcessDone() bool {
	if s.procExit == nil {
		// Channel never initialized — for subprocess sessions this
		// means the Wait goroutine never started (unexpected) or the
		// session is daemon-backed. Treat as "done" so shutdown
		// doesn't try to signal a phantom process.
		return true
	}
	select {
	case <-s.procExit:
		return true
	default:
		return false
	}
}
