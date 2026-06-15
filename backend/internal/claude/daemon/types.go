// Package daemon is a Go client for the claude-code background session
// supervisor. The supervisor (started lazily by `claude --bg` or `claude
// agents`) listens on a per-user unix socket and exposes a newline-delimited
// JSON protocol for listing, dispatching, subscribing to, and managing
// background sessions.
//
// This package wraps that protocol so grimoire can host its chat panels on
// top of claude-supervised sessions instead of owning the PTY processes
// directly. The benefits, in one line: sessions survive grimoire restart,
// the supervisor handles respawn, and the same sessions are visible from
// the native `claude agents` TUI.
//
// The wire protocol is private and versioned (proto: 1 today); we pin to a
// known cli version range via Ping() and fall back to CLI invocation if the
// daemon answers with an unexpected proto.
package daemon

import (
	"encoding/json"
)

// Record is a single session as reported by the daemon (op:list and
// op:subscribe's snapshot frame).
//
// Tempo / State separation: Tempo is the activity level (idle / active /
// blocked), State is the lifecycle phase (working / blocked / done / failed /
// stopped). They overlap but are independently set. UI should consult both.
type Record struct {
	Short     string `json:"short"`     // 8-hex short id (used by attach/kill/etc)
	Nonce     string `json:"nonce"`     // dispatch nonce, for ack matching
	SessionID string `json:"sessionId"` // full UUID, matches the on-disk JSONL filename
	PID       int    `json:"pid"`
	Cwd       string `json:"cwd"`
	Backend   string `json:"backend"` // "daemon" or "peer"
	Tempo     string `json:"tempo"`   // idle | active | blocked
	State     string `json:"state"`   // working | blocked | done | failed | stopped
	Detail    string `json:"detail"`  // Haiku-generated activity summary
	Intent    string `json:"intent"`  // user's initial prompt
	Name      string `json:"name"`    // display name from --name
	Agent     string `json:"agent"`   // subagent if dispatched with --agent
	Needs     string `json:"needs"`   // when blocked, the question waiting for input
	StartedAt int64  `json:"startedAt"`
}

// PingReply is the response to op:ping.
type PingReply struct {
	Version string // claude CLI version hosting the daemon
	Proto   int    // wire-protocol version, always 1 on current builds
}

// DispatchOpts describes a session to spawn via op:dispatch.
// Either Prompt or ResumeSessionID should be set. Empty Prompt creates an
// idle session waiting for the first reply.
type DispatchOpts struct {
	Cwd       string            // absolute working directory, required
	Name      string            // display name shown in agent view
	Prompt    string            // initial user message; empty creates an idle session
	SessionID string            // pre-assigned UUID; auto-generated if empty
	Agent     string            // optional subagent name (passed as --agent)
	Env       map[string]string // extra env vars to inject into the session

	Cols int // initial PTY columns (default 80)
	Rows int // initial PTY rows (default 24)

	// Resume an existing on-disk session.
	ResumeSessionID string
	Fork            bool // pass --fork-session when resuming
}

// DispatchResult is what the daemon returns on a successful op:dispatch.
type DispatchResult struct {
	Short         string `json:"short"`
	SessionID     string `json:"sessionId"`
	PID           int    `json:"pid"`
	MessagingSock string `json:"messagingSock"`
	Via           string `json:"via"` // "spare" (warm worker) or "cold" (fresh spawn)
}

// HasReply is the response to op:has. Alive and Present are independent —
// Present can stay true briefly after a session exits while the daemon's
// graveyard cleanup catches up.
type HasReply struct {
	Alive   bool // session process is currently running
	Present bool // session is in the daemon's live registry
}

// SubFrame is a frame from op:subscribe. The first frame for any subscription
// is always a snapshot (Record + recent PTY tail); subsequent frames are
// live updates.
type SubFrame struct {
	Type       string          `json:"type"`
	Record     *Record         `json:"record"`
	StreamTail []string        `json:"streamTail"`
	Raw        json.RawMessage `json:"-"` // original bytes, available for callers who want to forward
}
