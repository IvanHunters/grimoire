package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Subscribe opens an op:subscribe stream for one session and delivers frames
// to onFrame until ctx is cancelled or the connection closes. The first
// frame is always a snapshot (Record + recent PTY tail), subsequent frames
// are live updates.
//
// The connection stays open for the lifetime of ctx; closing ctx is the
// only way to cleanly disconnect.
func (c *Client) Subscribe(ctx context.Context, short string, tail int, onFrame func(SubFrame)) error {
	sock, err := c.resolveSock()
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", sock, 3*time.Second)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	defer conn.Close()

	req := fmt.Sprintf(`{"proto":1,"op":"subscribe","short":%q,"tail":%d}`, short, tail)
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		return fmt.Errorf("write subscribe: %w", err)
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var f SubFrame
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			continue
		}
		f.Raw = append([]byte(nil), sc.Bytes()...)
		onFrame(f)
	}
	// When ctx is cancelled, the conn-close goroutine closes the socket
	// out from under the scanner — Scan returns false with a "use of closed
	// network connection" error. Prefer reporting the cancellation so
	// callers can distinguish "we asked to stop" from "daemon went away".
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return sc.Err()
}

// SubscribeWithReconnect wraps Subscribe with automatic re-dial on socket
// close or daemon restart. The daemon may rotate its socket path after
// auto-update; we re-discover it via FindSock() on each reconnect.
//
// onFrame receives every frame. onReconnect (optional) is called each time
// we successfully reattach, so the caller can log or rerender.
//
// This returns nil only when the stream ends cleanly (session done) or ctx
// is cancelled. It will loop forever otherwise, backing off exponentially.
func (c *Client) SubscribeWithReconnect(
	ctx context.Context,
	short string,
	tail int,
	onFrame func(SubFrame),
	onReconnect func(),
) error {
	backoff := 500 * time.Millisecond
	const maxBackoff = 10 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Re-discover the socket each round in case the daemon restarted.
		c.Sock = ""
		subErr := c.Subscribe(ctx, short, tail, onFrame)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if subErr == nil {
			return nil // session done, stream ended cleanly
		}
		if c.Logger != nil {
			c.Logger.Debug("subscribe disconnected, will reconnect",
				"short", short, "err", subErr, "backoff", backoff)
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
		if onReconnect != nil {
			onReconnect()
		}
	}
}

// ansiRe matches ANSI/control sequences for CleanTerminal stripping.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|[\x00-\x08\x0b-\x1f\x7f]`)

// CleanTerminal strips ANSI / control-byte noise from raw PTY chunks into
// readable lines. Use it for plain-text previews in a list UI; for a real
// terminal renderer (xterm.js, etc), keep the raw bytes.
func CleanTerminal(chunks []string) []string {
	joined := strings.Join(chunks, "")
	joined = strings.ReplaceAll(joined, "\r\n", "\n")
	joined = strings.ReplaceAll(joined, "\r", "\n")
	joined = ansiRe.ReplaceAllString(joined, "")
	var out []string
	for _, ln := range strings.Split(joined, "\n") {
		ln = strings.TrimRight(ln, " \t")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, ln)
	}
	return out
}
