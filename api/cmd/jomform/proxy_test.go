package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// ─── the stdio proxy ─────────────────────────────────────────────────────────
//
// These tests drive the real relay: a stdio process on one side (a strings.Reader
// of newline-delimited JSON), an httptest server on the other that behaves like
// the deployed endpoint (SSE answers, 401 for a credential it does not accept,
// 202 for a notification) and a fake AS that rotates the refresh token. Nothing
// is stubbed between the two halves, so a mis-wired call site fails here.

// proxyFor seeds the CLI with the messages the harness would write on stdin.
func proxyFor(t *testing.T, as *fakeAS, messages ...string) (*cli, *bytes.Buffer, *bytes.Buffer, *fakeClock) {
	t.Helper()
	c, stdout, stderr, clock := newTestCLI(t, as)
	c.stdin = strings.NewReader(strings.Join(messages, "\n") + "\n")
	return c, stdout, stderr, clock
}

func callMessage(id int) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"list_sales"}}`, id)
}

// stdoutMessages parses what the proxy wrote: one JSON-RPC message per line.
func stdoutMessages(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(buf.String())
	if trimmed == "" {
		return nil
	}
	var out []map[string]any
	for _, line := range strings.Split(trimmed, "\n") {
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("the proxy wrote a line that is not JSON (%v): %q", err, line)
		}
		out = append(out, msg)
	}
	return out
}

// TestProxyRefreshesAnExpiredAccessTokenBeforeRelaying is the defect: the stored
// access token has passed, the harness calls a tool anyway, and the call must
// reach the server with a token it accepts instead of coming back 401.
func TestProxyRefreshesAnExpiredAccessTokenBeforeRelaying(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, stderr, clock := proxyFor(t, as, callMessage(1))
	storedFixture(t, as, c, clock.now().Add(-time.Minute), "Warung ABC")
	// The endpoint only accepts the credential the renewal issues. Without the
	// refresh this call is answered 401, which is exactly the reported symptom.
	as.acceptOnly(refreshedAccess(1))

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("proxy: %v (stderr %q)", err, stderr.String())
	}

	calls, refreshes := as.mcpSnapshot()
	if len(refreshes) != 1 {
		t.Fatalf("renewals = %d, want 1 before the first relayed call", len(refreshes))
	}
	if got := refreshes[0].Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q, want refresh_token", got)
	}
	if got := refreshes[0].Get("refresh_token"); got != fixtureRefreshToken() {
		t.Fatalf("the renewal presented %q, want the stored refresh token", got)
	}
	if len(calls) != 1 {
		t.Fatalf("relayed calls = %d, want 1", len(calls))
	}
	if want := "Bearer " + refreshedAccess(1); calls[0].authorization != want {
		t.Fatalf("the relayed call carried %q, want the renewed token", calls[0].authorization)
	}
	msgs := stdoutMessages(t, stdout)
	if len(msgs) != 1 || msgs[0]["id"].(float64) != 1 {
		t.Fatalf("stdout = %q, want the one response for id 1", stdout.String())
	}
	if _, ok := msgs[0]["result"]; !ok {
		t.Fatalf("the response carries no result: %q", stdout.String())
	}
	// The rotated refresh token lives on disk: a second renewal would otherwise
	// present a token the server has already burned.
	tok, err := c.loadToken()
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if tok.AccessToken != refreshedAccess(1) || tok.RefreshToken != refreshedRefresh(1) {
		t.Fatalf("token.json was not rotated (access %q, refresh %q)", tok.AccessToken, tok.RefreshToken)
	}
}

// TestProxyRenewsOnA401AndRetriesTheCall covers the other direction: the clock
// says the token is good, the server disagrees (revoked, or a renewal that never
// landed). One renewal, one retry — not a loop.
func TestProxyRenewsOnA401AndRetriesTheCall(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, stderr, clock := proxyFor(t, as, callMessage(7))
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	as.acceptOnly(refreshedAccess(1))

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("proxy: %v (stderr %q)", err, stderr.String())
	}

	calls, refreshes := as.mcpSnapshot()
	if len(calls) != 2 {
		t.Fatalf("relayed calls = %d, want 2 (the 401 and one retry)", len(calls))
	}
	if calls[0].authorization != "Bearer "+fixtureAccessToken() {
		t.Fatalf("the first call carried %q, want the stored token", calls[0].authorization)
	}
	if want := "Bearer " + refreshedAccess(1); calls[1].authorization != want {
		t.Fatalf("the retry carried %q, want the renewed token", calls[1].authorization)
	}
	if len(refreshes) != 1 {
		t.Fatalf("renewals = %d, want exactly 1 (a loop here would be a request storm)", len(refreshes))
	}
	msgs := stdoutMessages(t, stdout)
	if len(msgs) != 1 || msgs[0]["id"].(float64) != 7 {
		t.Fatalf("stdout = %q, want the one response for id 7", stdout.String())
	}
}

// TestProxyReportsADeadGrantAsAJSONRPCError: when the renewal is refused, the
// caller gets a JSON-RPC error (a harness has no HTTP status to read) and stderr
// names the one remedy. The call is not retried.
func TestProxyReportsADeadGrantAsAJSONRPCError(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, stderr, clock := proxyFor(t, as, callMessage(9))
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	as.acceptOnly("some-other-token") // the stored one is refused
	as.setRefreshScript(tokenErr("invalid_grant"))

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("the proxy exited on one dead call instead of answering it: %v", err)
	}

	calls, refreshes := as.mcpSnapshot()
	if len(calls) != 1 {
		t.Fatalf("relayed calls = %d, want 1 — a refused renewal must not be retried", len(calls))
	}
	if len(refreshes) != 1 {
		t.Fatalf("renewals = %d, want 1", len(refreshes))
	}
	msgs := stdoutMessages(t, stdout)
	if len(msgs) != 1 {
		t.Fatalf("stdout = %q, want one error response", stdout.String())
	}
	errObj, ok := msgs[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("the proxy answered a failed call with %q, want a JSON-RPC error", stdout.String())
	}
	if msgs[0]["id"].(float64) != 9 {
		t.Fatalf("the error is not addressed to the caller's id: %q", stdout.String())
	}
	if code := errObj["code"].(float64); code != proxyErrorCode {
		t.Fatalf("error code = %v, want %d", code, proxyErrorCode)
	}
	if !strings.Contains(stderr.String(), "login") {
		t.Fatalf("stderr = %q, want the remedy (run login again)", stderr.String())
	}
}

// TestProxyRefusesToStartOnADeadGrant: a harness that cannot authenticate should
// learn it at launch, once, with a message a person can act on.
func TestProxyRefusesToStartOnADeadGrant(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, clock := proxyFor(t, as, callMessage(1))
	storedFixture(t, as, c, clock.now().Add(-time.Minute), "Warung ABC")
	as.setRefreshScript(tokenErr("invalid_grant"))

	err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"})
	if err == nil {
		t.Fatal("the proxy started although the grant is dead")
	}
	if !strings.Contains(err.Error(), "login") {
		t.Fatalf("error %q does not point at a fresh login", err)
	}
	calls, _ := as.mcpSnapshot()
	if len(calls) != 0 {
		t.Fatalf("relayed calls = %d, want 0 before the credential was known good", len(calls))
	}
}

// TestProxyForwardsTheHarnessBytesUnchanged pins that this is a relay: what the
// harness wrote is what the server receives, byte for byte, with the two headers
// the spec requires on the transport.
func TestProxyForwardsTheHarnessBytesUnchanged(t *testing.T) {
	as := newFakeAS(t)
	msg := `{"jsonrpc":"2.0","id":"abc-1","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`
	c, _, _, clock := proxyFor(t, as, msg)
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	calls, _ := as.mcpSnapshot()
	if len(calls) != 1 {
		t.Fatalf("relayed calls = %d, want 1", len(calls))
	}
	if string(calls[0].body) != msg {
		t.Fatalf("the relay changed the harness's bytes:\n got %s\nwant %s", calls[0].body, msg)
	}
	if calls[0].contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", calls[0].contentType)
	}
	if !strings.Contains(calls[0].accept, "application/json") || !strings.Contains(calls[0].accept, "text/event-stream") {
		t.Fatalf("Accept = %q, want both media types the streamable-HTTP spec requires", calls[0].accept)
	}
}

// TestProxyRelaysAJsonAnswerToo: the deployment answers POSTs with SSE today, but
// the spec allows application/json and a future server option may use it.
func TestProxyRelaysAJsonAnswerToo(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, _, clock := proxyFor(t, as, callMessage(3))
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	as.scriptMCP(fakeMCPReply{
		status:      200,
		contentType: "application/json",
		body:        `{"jsonrpc":"2.0","id":3,"result":{"content":[]}}`,
	})

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	msgs := stdoutMessages(t, stdout)
	if len(msgs) != 1 || msgs[0]["id"].(float64) != 3 {
		t.Fatalf("stdout = %q, want the JSON answer relayed as one line", stdout.String())
	}
}

// TestProxyRelaysNothingForANotification: a notification has no id, the server
// answers 202 with no body, and the harness must not receive a fabricated
// message.
func TestProxyRelaysNothingForANotification(t *testing.T) {
	as := newFakeAS(t)
	note := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	c, stdout, stderr, clock := proxyFor(t, as, note)
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Fatalf("stdout = %q, want nothing for a notification", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want nothing for a successful notification", stderr.String())
	}
}

// TestProxyAnswersARequestTheServerLeftEmpty: a 200 with no message would leave
// the harness waiting forever, so the relay answers instead.
func TestProxyAnswersARequestTheServerLeftEmpty(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, _, clock := proxyFor(t, as, callMessage(5))
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	as.scriptMCP(fakeMCPReply{status: 200, contentType: "text/event-stream", body: ""})

	if err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"}); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	msgs := stdoutMessages(t, stdout)
	if len(msgs) != 1 {
		t.Fatalf("stdout = %q, want one error response", stdout.String())
	}
	if _, ok := msgs[0]["error"]; !ok {
		t.Fatalf("stdout = %q, want a JSON-RPC error instead of silence", stdout.String())
	}
	if msgs[0]["id"].(float64) != 5 {
		t.Fatalf("the error is not addressed to the caller's id: %q", stdout.String())
	}
}

// TestProxyNeedsAStoredGrant: run with no login, the proxy says what to run.
func TestProxyNeedsAStoredGrant(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, _ := proxyFor(t, as)
	err := c.run([]string{"mcp", "proxy", "--mcp-url", as.srv.URL + "/mcp"})
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("error = %v, want a pointer at mcp login", err)
	}
}

// ─── the pieces ──────────────────────────────────────────────────────────────

func TestSSEPayloads(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "one event per message",
			in:   "event: message\ndata: {\"id\":1}\n\nevent: message\ndata: {\"id\":2}\n\n",
			want: []string{`{"id":1}`, `{"id":2}`},
		},
		{
			name: "comments and other fields are ignored",
			in:   ": keep-alive\nevent: message\nid: 7\ndata: {\"id\":3}\nretry: 100\n\n",
			want: []string{`{"id":3}`},
		},
		{
			name: "CRLF framing",
			in:   "event: message\r\ndata: {\"id\":4}\r\n\r\n",
			want: []string{`{"id":4}`},
		},
		{
			name: "multi-line data is joined per the SSE spec",
			in:   "data: {\"id\":5,\ndata: \"ok\":true}\n\n",
			want: []string{"{\"id\":5,\n\"ok\":true}"},
		},
		{
			name: "a payload the harness could not parse is dropped",
			in:   "data: not json\n\ndata: {\"id\":6}\n\n",
			want: []string{`{"id":6}`},
		},
		{
			name: "no events at all",
			in:   "",
			want: nil,
		},
		{
			name: "a final event without a trailing blank line still counts",
			in:   "data: {\"id\":8}\n",
			want: []string{`{"id":8}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ssePayloads([]byte(tc.in))
			var asStrings []string
			for _, g := range got {
				asStrings = append(asStrings, string(g))
			}
			if strings.Join(asStrings, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("ssePayloads(%q) = %q, want %q", tc.in, asStrings, tc.want)
			}
		})
	}
}

func TestTokenDue(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	hour := storedToken{AccessToken: "a", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if tokenDue(hour, now) {
		t.Fatal("a token with an hour left was treated as due")
	}
	if tokenDue(hour, now.Add(58*time.Minute)) {
		t.Fatal("a token with two minutes left was treated as due")
	}
	if !tokenDue(hour, now.Add(59*time.Minute+30*time.Second)) {
		t.Fatal("a token inside the 60s window was not treated as due")
	}
	// A short-lived token (a self-hosted TTL, or a test) would otherwise renew on
	// every single request: the window shrinks to half its lifetime.
	short := storedToken{AccessToken: "a", CreatedAt: now, ExpiresAt: now.Add(2 * time.Second)}
	if tokenDue(short, now.Add(200*time.Millisecond)) {
		t.Fatal("a 2s token was treated as due 200ms in — the window must be half its lifetime")
	}
	if !tokenDue(short, now.Add(1500*time.Millisecond)) {
		t.Fatal("a 2s token was not treated as due 1.5s in")
	}
	if !tokenDue(storedToken{}, now) {
		t.Fatal("an empty token must be due")
	}
}

// TestRenewalDoesNotHappenTwice pins the shared-state half: once a renewal has
// written the pair, a later call sees it and does not spend the refresh token
// again (the server revokes the old one, so a second spend fails and the grant
// is lost).
func TestRenewalDoesNotHappenTwice(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(-time.Minute), "Warung ABC")

	first, err := c.accessToken(context.Background(), "")
	if err != nil {
		t.Fatalf("first renewal: %v", err)
	}
	second, err := c.accessToken(context.Background(), "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	_, refreshes := as.mcpSnapshot()
	if len(refreshes) != 1 {
		t.Fatalf("renewals = %d, want 1 — the second call spent the rotated token again", len(refreshes))
	}
	if second.AccessToken != first.AccessToken {
		t.Fatalf("the second call returned a different token (%q vs %q)", second.AccessToken, first.AccessToken)
	}
}

// TestTokenLockCoversACrashedProcess: a lock file left behind by a process that
// died mid-renewal must not wedge every later call.
func TestTokenLockCoversACrashedProcess(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, _ := newTestCLI(t, as)
	if err := os.MkdirAll(c.configDir(), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lock := c.configDir() + "/token.lock"
	if err := os.WriteFile(lock, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatalf("backdate lock: %v", err)
	}
	unlock, err := c.lockToken()
	if err != nil {
		t.Fatalf("a stale lock was not broken: %v", err)
	}
	unlock()
	if _, statErr := os.Stat(lock); !os.IsNotExist(statErr) {
		t.Fatalf("the lock survived release: %v", statErr)
	}
}
