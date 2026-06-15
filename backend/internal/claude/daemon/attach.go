package daemon

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// AttachConn is a bidirectional PTY bridge to a running background session.
// After the op:attach handshake succeeds the underlying unix socket carries
// raw PTY bytes: Read pulls session output (claude's TUI), Write pushes
// keystrokes / paste content as if typed at the terminal.
//
// Multiple AttachConns can target the same session simultaneously — the
// daemon merges I/O across them. Close one attacher and the others keep
// streaming.
//
// Lifetime: when the session ends (process exits, op:kill, etc) the
// underlying conn closes and subsequent Read returns EOF.
type AttachConn struct {
	short    string
	attachID string
	sock     string

	conn net.Conn      // raw stream after the ack, used for Write/Close
	br   *bufio.Reader // wraps conn — Reads come through here to preserve
	// any bytes that were buffered while we parsed the ack line
}

// Attach opens an op:attach bridge to the named session. cols/rows define
// the initial PTY size — pass the client's terminal size or some sane
// default (e.g. 120x40) for a web view.
//
// The session must be in a running/idle state. Attach to a 'done',
// 'stopped', 'failed', or 'blocked' session is rejected by the daemon with
// an error.
func (c *Client) Attach(short string, cols, rows int) (*AttachConn, error) {
	sock, err := c.resolveSock()
	if err != nil {
		return nil, err
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	conn, err := net.DialTimeout("unix", sock, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial daemon: %w", err)
	}

	attachID := genHexID(8)

	// caps is required and validated by the daemon — mux must be one of
	// "tmux"/"screen"/"zellij" or null. We're a headless web backend so
	// mux is always null, ssh false. terminal must be a non-empty string.
	caps := `{"terminal":"xterm-256color","mux":null,"ssh":false}`
	req := fmt.Sprintf(
		`{"proto":1,"op":"attach","short":%q,"cols":%d,"rows":%d,"attachId":%q,"caps":%s}`,
		short, cols, rows, attachID, caps,
	)
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("write attach: %w", err)
	}

	// Read the ack line. Use a bufio.Reader so we can keep any post-ack
	// bytes that were already buffered alongside the ack.
	br := bufio.NewReader(conn)
	ackLine, err := br.ReadString('\n')
	if err != nil && len(ackLine) == 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("read attach ack: %w", err)
	}
	var ack struct {
		OK    bool   `json:"ok"`
		Op    string `json:"op"`
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(ackLine), &ack); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("parse attach ack: %w (raw=%s)", err, ackLine)
	}
	if !ack.OK {
		_ = conn.Close()
		return nil, fmt.Errorf("attach rejected (%s): %s", ack.Code, ack.Error)
	}

	// Force an initial repaint at our size so the TUI renders into the
	// right dimensions instead of the daemon's default.
	a := &AttachConn{short: short, attachID: attachID, sock: sock, conn: conn, br: br}
	_ = a.Resize(cols, rows)
	return a, nil
}

// Read returns raw PTY output from the session. Bytes are exactly what
// xterm.js (or any terminal emulator) would render: ANSI escapes, redraw
// sequences, etc — strip with daemon.CleanTerminal if you need plain text.
func (a *AttachConn) Read(p []byte) (int, error) {
	return a.br.Read(p)
}

// Write sends keystrokes to the session's PTY. The daemon translates these
// into stdin for the claude process. Use the same bytes a real terminal
// would send: "\r" (0x0d) for Enter, ESC sequences for arrow keys, etc.
//
// To submit a user message, write the text followed by "\r" — matches our
// WebSocket terminal_input handler in handler.go.
func (a *AttachConn) Write(p []byte) (int, error) {
	return a.conn.Write(p)
}

// Resize tells the daemon the attacher's PTY dimensions changed. Send this
// on SIGWINCH (CLI) or on container resize (web). The daemon will repaint
// the TUI at the new size.
//
// Note: this opens a fresh socket connection — the daemon expects resize
// to come over a separate channel, not in-band with the attach stream.
func (a *AttachConn) Resize(cols, rows int) error {
	conn, err := net.DialTimeout("unix", a.sock, 3*time.Second)
	if err != nil {
		return fmt.Errorf("dial daemon for resize: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	req := fmt.Sprintf(
		`{"proto":1,"op":"resize","short":%q,"cols":%d,"rows":%d,"attachId":%q}`,
		a.short, cols, rows, a.attachID,
	)
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		return fmt.Errorf("write resize: %w", err)
	}
	// Drain the reply to be polite — we don't care about its content.
	_, _ = bufio.NewReader(conn).ReadString('\n')
	return nil
}

// Close detaches from the session and releases socket resources. The
// session itself keeps running (other clients can still be attached); use
// Client.Kill/Remove to actually stop it.
func (a *AttachConn) Close() error {
	return a.conn.Close()
}

// AttachID returns the random ID the daemon associates with this attacher.
// Useful for logging and correlating with daemon-side activity.
func (a *AttachConn) AttachID() string { return a.attachID }

// genHexID returns n random bytes hex-encoded. Used for attachId tokens.
func genHexID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
