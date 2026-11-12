package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// The stdio proxy.
//
// A harness entry that names a `url` and a static `headers.Authorization` has no
// way to renew: the token in it dies an hour after login and every call after
// that is answered 401. So `mcp login` writes a launcher entry instead —
// `{"command": "<jomform>", "args": ["mcp", "proxy", "--mcp-url", ...]}` — and
// this process sits between the harness's stdio transport and the remote
// streamable-HTTP endpoint, attaching a live bearer token to every request.
//
// It is a relay, not a reimplementation: the message bytes the harness writes are
// forwarded unchanged, and the bytes the server answers are forwarded unchanged
// (unwrapped from SSE to the newline-delimited JSON that stdio uses). No tool
// call, notification or protocol detail is interpreted, so a tool the server adds
// tomorrow works without touching this file.

const (
	// maxInflight bounds how many MCP messages may be in flight at once. A client
	// may pipeline (a cancellation must be able to overtake the call it cancels),
	// but it must not be able to spray the endpoint unboundedly.
	maxInflight = 8
	// proxyErrorCode is the JSON-RPC error a relay failure is reported as. -32000
	// through -32099 is the implementation-defined server-error range.
	proxyErrorCode = -32000
)

// cmdProxy parses `jomform mcp proxy` and runs the relay until stdin closes.
func (c *cli) cmdProxy(args []string) error {
	var mcpURL string
	fs := flag.NewFlagSet("jomform mcp proxy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&mcpURL, "mcp-url", "", "remote MCP endpoint to relay to (default: the one in the stored token)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	tok, err := c.loadToken()
	if err != nil {
		return errors.New("not signed in on this machine — run `jomform mcp login --headless` first")
	}
	if strings.TrimSpace(mcpURL) == "" {
		mcpURL = tok.MCPURL
	}
	if strings.TrimSpace(mcpURL) == "" {
		return errors.New("the stored token does not name an MCP endpoint — run `jomform mcp login --headless` again")
	}
	// One renewal before the first call: a harness started on an expired token
	// never sees a 401, and a dead grant is reported once at startup instead of
	// once per tool call.
	if _, err := c.accessToken(context.Background(), ""); err != nil {
		return err
	}
	return c.serveMCP(context.Background(), mcpURL)
}

// serveMCP reads newline-delimited JSON-RPC from stdin and writes the answers to
// stdout until stdin reaches EOF, then waits for the calls still in flight.
func (c *cli) serveMCP(ctx context.Context, mcpURL string) error {
	dec := json.NewDecoder(c.stdin)
	out := &lockedWriter{w: c.stdout}
	sem := make(chan struct{}, maxInflight)
	var wg sync.WaitGroup

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("could not read an MCP message from stdin: %w", err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(msg json.RawMessage) {
			defer wg.Done()
			defer func() { <-sem }()
			c.relay(ctx, mcpURL, msg, out)
		}(raw)
	}
	wg.Wait()
	return nil
}

// relay forwards one message and writes whatever the server answers.
func (c *cli) relay(ctx context.Context, mcpURL string, msg json.RawMessage, out *lockedWriter) {
	id := requestID(msg)
	tok, err := c.accessToken(ctx, "")
	if err != nil {
		c.relayFailed(out, id, err)
		return
	}
	body, contentType, status, err := c.postMCP(ctx, mcpURL, tok.AccessToken, msg)
	if err == nil && status == http.StatusUnauthorized {
		// The clock said the token was live and the server disagrees (revoked, or
		// a renewal that did not land). Renew once and retry once — a loop here
		// would turn a dead grant into a request storm.
		tok, err = c.accessToken(ctx, tok.AccessToken)
		if err != nil {
			c.relayFailed(out, id, err)
			return
		}
		body, contentType, status, err = c.postMCP(ctx, mcpURL, tok.AccessToken, msg)
	}
	if err != nil {
		c.relayFailed(out, id, err)
		return
	}
	if status < 200 || status > 299 {
		c.relayFailed(out, id, fmt.Errorf("the MCP server answered HTTP %d%s", status, bodyHint(body)))
		return
	}
	sent, err := out.writeResponses(contentType, body)
	if err != nil {
		c.warnf("could not write to the harness: %v", err)
		return
	}
	if sent == 0 && len(id) > 0 {
		// A request the server accepted and never answered would leave the
		// harness waiting forever; say so instead of hanging.
		c.relayFailed(out, id, fmt.Errorf("the MCP server answered HTTP %d with no message", status))
	}
}

// postMCP performs one streamable-HTTP POST. The response is returned whole: a
// stateless server ends the stream when the request it carries is answered, so
// reading it to EOF is the same as waiting for that answer.
func (c *cli) postMCP(ctx context.Context, mcpURL, accessToken string, msg json.RawMessage) (body []byte, contentType string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(msg))
	if err != nil {
		return nil, "", 0, err
	}
	// The spec requires both media types on Accept; the server picks. This
	// deployment is stateless, so it answers with an SSE stream (one `data:`
	// event per message) and closes it when the request completes.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	// MCP-Protocol-Version is deliberately not set. The server accepts an absent
	// header (the version is simply unknown), and inventing one here would be a
	// guess about a session this relay does not own.

	client := c.stream
	if client == nil {
		client = c.http
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("could not reach %s: %w", mcpURL, err)
	}
	defer resp.Body.Close()
	// No read cap: a tool result is data the harness asked for, and truncating it
	// would hand the harness broken JSON.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("could not read the MCP answer: %w", err)
	}
	return raw, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

// relayFailed reports a relay failure to both audiences: the harness's log (a
// line on stderr, which never carries a token) and, when the message was a
// request, the caller as a JSON-RPC error. A notification has nobody to answer.
func (c *cli) relayFailed(out *lockedWriter, id json.RawMessage, err error) {
	c.warnf("%v", err)
	if len(id) == 0 {
		return
	}
	payload, merr := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    proxyErrorCode,
			"message": err.Error(),
		},
	})
	if merr != nil {
		return
	}
	if werr := out.writeLine(payload); werr != nil {
		c.warnf("could not write to the harness: %v", werr)
	}
}

// requestID returns the message's id, or nil when it is a notification (or is not
// a JSON-RPC message at all). `id: null` is treated as absent: there is no caller
// to answer.
func requestID(msg json.RawMessage) json.RawMessage {
	var env struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(msg, &env); err != nil {
		return nil
	}
	if len(env.ID) == 0 || string(env.ID) == "null" {
		return nil
	}
	return env.ID
}

// bodyHint adds the server's own words to an HTTP failure, bounded so a proxy's
// HTML error page cannot flood a log line.
func bodyHint(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return ": " + e.Error
	}
	if len(trimmed) > 80 {
		trimmed = trimmed[:80] + "…"
	}
	return ": " + strings.Join(strings.Fields(trimmed), " ")
}

// lockedWriter is stdout for the relay: several requests may be answered
// concurrently, and the stdio transport must not interleave their bytes.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// writeResponses turns one HTTP answer into stdio messages and reports how many
// it wrote.
func (w *lockedWriter) writeResponses(contentType string, body []byte) (int, error) {
	var msgs [][]byte
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		msgs = ssePayloads(body)
	} else if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 {
		msgs = [][]byte{trimmed}
	}
	for _, m := range msgs {
		if err := w.writeLine(m); err != nil {
			return 0, err
		}
	}
	return len(msgs), nil
}

// writeLine writes one JSON-RPC message as one line of newline-delimited JSON.
// Interior newlines are insignificant JSON whitespace (a newline inside a string
// is escaped by definition), so folding them to a space keeps the framing intact
// without touching the message.
func (w *lockedWriter) writeLine(msg []byte) error {
	line := bytes.ReplaceAll(msg, []byte("\r"), nil)
	line = bytes.ReplaceAll(line, []byte("\n"), []byte(" "))
	line = append(bytes.TrimSpace(line), '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.w.Write(line)
	return err
}

// ssePayloads extracts the `data` payloads from a text/event-stream body. Only
// data that is valid JSON is returned: a payload the harness cannot parse would
// be a protocol error invented by this relay.
func ssePayloads(body []byte) [][]byte {
	var out [][]byte
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		payload := strings.Join(data, "\n")
		data = nil
		if json.Valid([]byte(payload)) {
			out = append(out, []byte(payload))
		}
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // a comment/keep-alive
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok || field != "data" {
			continue
		}
		data = append(data, strings.TrimPrefix(value, " "))
	}
	flush()
	return out
}
