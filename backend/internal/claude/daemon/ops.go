package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Ping checks the daemon is alive and returns its CLI + protocol version.
// Call this on backend startup to refuse to talk to a daemon with an
// incompatible proto version.
func (c *Client) Ping() (PingReply, error) {
	line, err := c.request(`{"proto":1,"op":"ping"}`)
	if err != nil {
		return PingReply{}, err
	}
	var resp struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Version string `json:"version"`
		Proto   int    `json:"proto"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return PingReply{}, fmt.Errorf("parse ping reply: %w", err)
	}
	if !resp.OK {
		return PingReply{}, fmt.Errorf("daemon error: %s", resp.Error)
	}
	return PingReply{Version: resp.Version, Proto: resp.Proto}, nil
}

// ListSessions returns every background session the daemon knows about,
// across all projects. Filter by Cwd or other Record fields client-side.
func (c *Client) ListSessions() ([]Record, error) {
	line, err := c.request(`{"proto":1,"op":"list"}`)
	if err != nil {
		return nil, err
	}
	var resp struct {
		OK    bool     `json:"ok"`
		Error string   `json:"error"`
		Jobs  []Record `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("parse list reply: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}
	return resp.Jobs, nil
}

// Dispatch spawns a new background session. The daemon may serve it from a
// warm spare worker (Result.Via=="spare", nearly instant) or cold-spawn a
// fresh claude process. Either way the session keeps running after this
// call returns; reach it later via Subscribe / Reply / Attach.
func (c *Client) Dispatch(opts DispatchOpts) (DispatchResult, error) {
	if opts.Cwd == "" {
		return DispatchResult{}, fmt.Errorf("dispatch: Cwd is required")
	}
	if opts.SessionID == "" {
		opts.SessionID = genUUID()
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	short := opts.SessionID[:8]

	nonceBytes := make([]byte, 4)
	_, _ = rand.Read(nonceBytes)
	nonce := hex.EncodeToString(nonceBytes)

	// Build launch sub-object. The daemon picks the mode from this shape.
	var launch map[string]any
	if opts.ResumeSessionID != "" {
		launch = map[string]any{
			"mode":      "resume",
			"sessionId": opts.ResumeSessionID,
			"fork":      opts.Fork,
			"flagArgs":  []string{},
		}
	} else {
		args := []string{"--session-id", opts.SessionID}
		if opts.Agent != "" {
			args = append(args, "--agent", opts.Agent)
		}
		if opts.Prompt != "" {
			args = append(args, "--", opts.Prompt)
		}
		launch = map[string]any{"mode": "prompt", "args": args}
	}

	if opts.Env == nil {
		opts.Env = map[string]string{}
	}
	// Fields the daemon validates strictly: omit when unset rather than
	// sending null. The daemon rejects null for `agent` / `routine`.
	spec := map[string]any{
		"proto":        1,
		"short":        short,
		"sessionId":    opts.SessionID,
		"createdAt":    time.Now().UnixMilli(),
		"source":       "shell",
		"cwd":          opts.Cwd,
		"launch":       launch,
		"respawnFlags": []string{},
		"env":          opts.Env,
		"isolation":    "none",
		"seed":         map[string]string{"intent": opts.Prompt, "name": opts.Name},
		"cols":         opts.Cols,
		"rows":         opts.Rows,
		"nonce":        nonce,
	}
	if opts.Agent != "" {
		spec["agent"] = opts.Agent
	}
	req := map[string]any{
		"proto":     1,
		"op":        "dispatch",
		"d":         spec,
		"timeoutMs": 5000,
	}
	if key, err := readControlKey(); err == nil && key != "" {
		req["auth"] = key
	}
	body, err := json.Marshal(req)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("marshal dispatch: %w", err)
	}

	line, err := c.request(string(body))
	if err != nil {
		return DispatchResult{}, err
	}
	var resp struct {
		OK            bool   `json:"ok"`
		Error         string `json:"error"`
		Code          string `json:"code"`
		Short         string `json:"short"`
		PID           int    `json:"pid"`
		MessagingSock string `json:"messagingSock"`
		Via           string `json:"via"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return DispatchResult{}, fmt.Errorf("parse dispatch reply: %w (raw=%s)", err, line)
	}
	if !resp.OK {
		return DispatchResult{}, fmt.Errorf("daemon error (%s): %s", resp.Code, resp.Error)
	}
	return DispatchResult{
		Short:         resp.Short,
		SessionID:     opts.SessionID,
		PID:           resp.PID,
		MessagingSock: resp.MessagingSock,
		Via:           resp.Via,
	}, nil
}

// Kill terminates a session via op:kill. On v2.1.153+ this is a final stop
// — the session does NOT respawn (an earlier release would; see kvaps'
// research). The on-disk jobdir at ~/.claude/jobs/<short>/ may persist
// briefly; use Remove() for a clean wipe.
func (c *Client) Kill(short string) error {
	line, err := c.request(fmt.Sprintf(`{"proto":1,"op":"kill","short":%q}`, short))
	if err != nil {
		return err
	}
	return unwrapAck(line, "kill")
}

// Remove performs a full session deletion: Kill + remove the on-disk
// jobdir. The conversation transcript at
// ~/.claude/projects/<sanitized-cwd>/<uuid>.jsonl is left alone — that's
// history, not session state.
func (c *Client) Remove(short string) error {
	if err := c.Kill(short); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	jobDir := filepath.Join(home, ".claude", "jobs", short)
	if err := os.RemoveAll(jobDir); err != nil {
		return fmt.Errorf("rm jobdir %s: %w", jobDir, err)
	}
	return nil
}

// Has reports whether a session exists in the daemon's registry. Present
// can be true while Alive is false during respawn windows or just after a
// session exits.
func (c *Client) Has(short string) (HasReply, error) {
	line, err := c.request(fmt.Sprintf(`{"proto":1,"op":"has","short":%q}`, short))
	if err != nil {
		return HasReply{}, err
	}
	var resp struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Alive   bool   `json:"alive"`
		Present bool   `json:"present"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return HasReply{}, fmt.Errorf("parse has reply: %w", err)
	}
	if !resp.OK {
		return HasReply{}, fmt.Errorf("daemon error: %s", resp.Error)
	}
	return HasReply{Alive: resp.Alive, Present: resp.Present}, nil
}

// Reply sends a user message to a running session without attaching a PTY.
// This is the primitive grimoire's chat panel uses for the user's side of
// the conversation: Subscribe for output, Reply for input. The daemon
// delivers the text to the session's stdin layer.
func (c *Client) Reply(short, text string) error {
	payload := map[string]any{"proto": 1, "op": "reply", "short": short, "text": text}
	if key, err := readControlKey(); err == nil && key != "" {
		payload["auth"] = key
	}
	body, _ := json.Marshal(payload)
	line, err := c.request(string(body))
	if err != nil {
		return err
	}
	return unwrapAck(line, "reply")
}

// unwrapAck parses a generic ack-style reply and returns an error if !ok.
func unwrapAck(line, op string) error {
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return fmt.Errorf("parse %s reply: %w", op, err)
	}
	if !resp.OK {
		return fmt.Errorf("daemon error (%s): %s", resp.Code, resp.Error)
	}
	return nil
}

// genUUID returns an RFC 4122 v4 UUID.
func genUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// readControlKey returns the daemon control key from ~/.claude/daemon/control.key.
// Since claude 2.1.16x the daemon requires this key in the "auth" field of
// dispatch/reply/permission-response requests (EAUTH otherwise). Returns ""
// with no error when the file is absent (older daemon versions that don't
// need auth) — callers should omit the field in that case.
func readControlKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}
	path := filepath.Join(home, ".claude", "daemon", "control.key")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read control.key: %w", err)
	}
	if len(b) > 4096 {
		return "", nil
	}
	// daemon strips whitespace; trim newline and surrounding spaces
	key := string(b)
	for len(key) > 0 && (key[len(key)-1] == '\n' || key[len(key)-1] == '\r' || key[len(key)-1] == ' ' || key[len(key)-1] == '\t') {
		key = key[:len(key)-1]
	}
	return key, nil
}
