package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── fixtures ───────────────────────────────────────────────────────────────
//
// The tokens below are built at run time rather than written as literals: they
// are 64 hex characters, which is exactly the shape a secret scanner (and this
// package's own "nothing token-looking reached stdout" test) looks for. Building
// them here keeps a fixture from being mistaken for a leaked credential while
// still exercising the real shape.
func fixtureToken(seed string) string { return strings.Repeat(seed, 8) }

func fixtureAccessToken() string  { return fixtureToken("a1b2c3d4") }
func fixtureRefreshToken() string { return fixtureToken("e5f6a7b8") }
func fixtureDeviceCode() string   { return fixtureToken("c3d4e5f6") }

// fakeClock is the injected clock: sleeping advances it, so the poll loop's
// deadline and its intervals are exercised without real waiting.
type fakeClock struct {
	t      time.Time
	sleeps []time.Duration
}

func (f *fakeClock) now() time.Time { return f.t }
func (f *fakeClock) sleep(d time.Duration) {
	f.sleeps = append(f.sleeps, d)
	f.t = f.t.Add(d)
}

// fakeTokenReply is one scripted /oauth/token answer.
type fakeTokenReply struct {
	status int
	body   string
}

// fakeMCPCall is one request the proxy relayed to the MCP endpoint.
type fakeMCPCall struct {
	authorization string
	contentType   string
	accept        string
	body          []byte
}

// fakeMCPReply is one scripted answer from the MCP endpoint.
type fakeMCPReply struct {
	status      int
	contentType string
	body        string
}

// fakeAS is a stand-in authorization server: the four endpoints the CLI talks
// to, scripted per test. No real network, no real account, no real token.
type fakeAS struct {
	srv *httptest.Server

	mu            sync.Mutex
	device        deviceGrant
	tokenScript   []fakeTokenReply
	tokenCalls    int
	deviceCalls   int
	registerCalls int
	deviceForm    url.Values
	tokenForms    []url.Values
	revokeForm    url.Values

	// refreshScript answers the refresh_token grant. With no script the endpoint
	// issues a distinct pair per call, so a test can tell renewals apart.
	refreshScript []fakeTokenReply
	refreshForms  []url.Values

	// The MCP endpoint the proxy relays to.
	mcpCalls     []fakeMCPCall
	mcpScript    []fakeMCPReply
	acceptTokens map[string]bool
}

func newFakeAS(t *testing.T) *fakeAS {
	t.Helper()
	code := "BCDF-GHJK"
	a := &fakeAS{device: deviceGrant{
		DeviceCode:              fixtureDeviceCode(),
		UserCode:                code,
		VerificationURI:         "https://as.example.test/profile?tab=mcp",
		VerificationURIComplete: "https://as.example.test/profile?tab=mcp&user_code=" + url.QueryEscape(code),
		ExpiresIn:               600,
		Interval:                1,
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		a.mu.Lock()
		a.registerCalls++
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"jfc_cli_fixture","token_endpoint_auth_method":"none"}`))
	})
	mux.HandleFunc("/oauth/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		a.mu.Lock()
		a.deviceCalls++
		a.deviceForm = r.PostForm
		device := a.device
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(device)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		a.mu.Lock()
		isRefresh := r.PostForm.Get("grant_type") == "refresh_token"
		var reply fakeTokenReply
		if isRefresh {
			a.refreshForms = append(a.refreshForms, r.PostForm)
			idx := len(a.refreshForms) - 1
			reply = refreshedPair(len(a.refreshForms))
			if len(a.refreshScript) > 0 {
				if idx >= len(a.refreshScript) {
					idx = len(a.refreshScript) - 1
				}
				reply = a.refreshScript[idx]
			}
		} else {
			idx := a.tokenCalls
			a.tokenCalls++
			a.tokenForms = append(a.tokenForms, r.PostForm)
			reply = fakeTokenReply{status: http.StatusBadRequest, body: `{"error":"authorization_pending"}`}
			if len(a.tokenScript) > 0 {
				if idx >= len(a.tokenScript) {
					idx = len(a.tokenScript) - 1
				}
				reply = a.tokenScript[idx]
			}
		}
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	})
	// The remote MCP endpoint. Like the real one it answers with an SSE stream
	// carrying one JSON-RPC message per event, rejects a credential it does not
	// accept, and answers a notification with 202 and no body.
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		a.mu.Lock()
		a.mcpCalls = append(a.mcpCalls, fakeMCPCall{
			authorization: r.Header.Get("Authorization"),
			contentType:   r.Header.Get("Content-Type"),
			accept:        r.Header.Get("Accept"),
			body:          raw,
		})
		idx := len(a.mcpCalls) - 1
		var scripted *fakeMCPReply
		if len(a.mcpScript) > 0 {
			if idx >= len(a.mcpScript) {
				idx = len(a.mcpScript) - 1
			}
			s := a.mcpScript[idx]
			scripted = &s
		}
		accepted := a.acceptTokens
		a.mu.Unlock()

		if scripted != nil {
			if scripted.contentType != "" {
				w.Header().Set("Content-Type", scripted.contentType)
			}
			w.WriteHeader(scripted.status)
			_, _ = w.Write([]byte(scripted.body))
			return
		}
		if accepted != nil && !accepted[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
			return
		}
		id := idFromRequest(raw)
		if len(id) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"ok\":true}}\n\n", id)
	})
	mux.HandleFunc("/oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		a.mu.Lock()
		a.revokeForm = r.PostForm
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"revoked"}`))
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeAS) setTokenScript(replies ...fakeTokenReply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenScript = replies
}

func tokenOK(scope string) fakeTokenReply {
	body, _ := json.Marshal(map[string]any{
		"access_token":  fixtureAccessToken(),
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": fixtureRefreshToken(),
		"scope":         scope,
	})
	return fakeTokenReply{status: http.StatusOK, body: string(body)}
}

func tokenErr(code string) fakeTokenReply {
	return fakeTokenReply{status: http.StatusBadRequest, body: `{"error":"` + code + `","error_description":"scripted"}`}
}

// refreshedPair is the pair a refresh_token grant issues. Each call is distinct,
// so a test can tell a renewal from the pair the login stored, and the rotation
// (a NEW refresh token) is observable.
func refreshedPair(n int) fakeTokenReply {
	body, _ := json.Marshal(map[string]any{
		"access_token":  strings.Repeat(fmt.Sprintf("%02d", n), 32),
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": strings.Repeat(fmt.Sprintf("%02d", n+40), 32),
		"scope":         "read",
	})
	return fakeTokenReply{status: http.StatusOK, body: string(body)}
}

// refreshedAccess is the access token refreshedPair(n) issues.
func refreshedAccess(n int) string { return strings.Repeat(fmt.Sprintf("%02d", n), 32) }

// refreshedRefresh is the rotated refresh token refreshedPair(n) issues.
func refreshedRefresh(n int) string { return strings.Repeat(fmt.Sprintf("%02d", n+40), 32) }

// idFromRequest reads a JSON-RPC id out of a message, without borrowing the
// production helper (so a blind spot there cannot make the fake agree with it).
func idFromRequest(raw []byte) json.RawMessage {
	var env struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	if len(env.ID) == 0 || string(env.ID) == "null" {
		return nil
	}
	return env.ID
}

// acceptOnly makes the MCP endpoint reject every credential except these.
func (a *fakeAS) acceptOnly(tokens ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acceptTokens = map[string]bool{}
	for _, t := range tokens {
		a.acceptTokens[t] = true
	}
}

// scriptMCP scripts the MCP endpoint's answers in call order.
func (a *fakeAS) scriptMCP(replies ...fakeMCPReply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mcpScript = replies
}

func (a *fakeAS) setRefreshScript(replies ...fakeTokenReply) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshScript = replies
}

func (a *fakeAS) mcpSnapshot() ([]fakeMCPCall, []url.Values) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]fakeMCPCall(nil), a.mcpCalls...), append([]url.Values(nil), a.refreshForms...)
}

// newTestCLI wires a CLI to a temp home, the fake server's HTTP client, a fake
// clock and a stubbed account lookup.
func newTestCLI(t *testing.T, as *fakeAS) (*cli, *bytes.Buffer, *bytes.Buffer, *fakeClock) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	c := &cli{
		stdout: stdout,
		stderr: stderr,
		stdin:  strings.NewReader(""),
		home:   t.TempDir(),
		http:   as.srv.Client(),
		stream: as.srv.Client(),
		// A harness launcher entry carries an absolute path; the tests pin the
		// one they expect rather than the path of the test binary.
		executable: func() (string, error) { return "/usr/local/bin/jomform", nil },
		lookupAccount: func(context.Context, string, string) (accountRef, error) {
			return accountRef{Name: "Warung ABC", Slug: "warung-abc"}, nil
		},
	}
	clock := &fakeClock{t: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
	c.now, c.sleep = clock.now, clock.sleep
	return c, stdout, stderr, clock
}

func cursorConfig(t *testing.T, c *cli) string {
	t.Helper()
	return filepath.Join(c.home, ".cursor", "mcp.json")
}

func loginArgs(as *fakeAS, out string) []string {
	return []string{
		"mcp", "login", "--headless",
		"--server", as.srv.URL,
		"--mcp-url", "https://mcp.example.test/mcp",
		"--out", out,
	}
}

func outputLines(buf *bytes.Buffer) []string {
	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
}

// ─── login: the polling handshake ────────────────────────────────────────────

func TestLoginPollsThroughPendingAndSlowDownThenSucceeds(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(
		tokenErr("authorization_pending"),
		tokenErr("slow_down"),
		tokenOK("read"),
	)
	c, stdout, stderr, clock := newTestCLI(t, as)
	out := cursorConfig(t, c)

	if err := c.run(loginArgs(as, out)); err != nil {
		t.Fatalf("login: %v (stderr %q)", err, stderr.String())
	}

	// interval=1 from the device response; slow_down adds 5s PERMANENTLY, so the
	// third wait is 6s. Removing that bump changes sleeps[2] to 1s.
	want := []time.Duration{1 * time.Second, 1 * time.Second, 6 * time.Second}
	if len(clock.sleeps) != len(want) {
		t.Fatalf("waits between polls = %v, want %v", clock.sleeps, want)
	}
	for i := range want {
		if clock.sleeps[i] != want[i] {
			t.Fatalf("wait %d = %s, want %s (slow_down must add 5s for the rest of the grant); all waits %v",
				i, clock.sleeps[i], want[i], clock.sleeps)
		}
	}

	// stdout: the URL, the code, the expiry in words, then only the path.
	lines := outputLines(stdout)
	if len(lines) != 4 {
		t.Fatalf("stdout has %d line(s), want exactly 4 (url, code, instruction, path): %q", len(lines), lines)
	}
	if lines[0] != as.device.VerificationURIComplete {
		t.Fatalf("line 1 = %q, want the verification_uri_complete %q", lines[0], as.device.VerificationURIComplete)
	}
	if lines[1] != as.device.UserCode {
		t.Fatalf("line 2 = %q, want the user_code %q on its own line", lines[1], as.device.UserCode)
	}
	if !strings.Contains(lines[2], "Approve") || !strings.Contains(lines[2], "10 minutes") {
		t.Fatalf("line 3 = %q, want the expiry in plain words", lines[2])
	}
	if lines[3] != out {
		t.Fatalf("line 4 = %q, want the config path %q and nothing else", lines[3], out)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr on a successful login = %q, want empty", stderr.String())
	}

	// The token request must be the RFC 8628 device grant.
	first := as.tokenForms[0]
	if got := first.Get("grant_type"); got != deviceGrantType {
		t.Fatalf("grant_type = %q, want %q", got, deviceGrantType)
	}
	if got := first.Get("device_code"); got != as.device.DeviceCode {
		t.Fatalf("device_code in the poll = %q, want the one from the device response", got)
	}
	if got := first.Get("client_id"); got != "jfc_cli_fixture" {
		t.Fatalf("client_id in the poll = %q, want the registered client", got)
	}
	if got := as.deviceForm.Get("scope"); got != "read" {
		t.Fatalf("scope requested = %q, want read", got)
	}
	if as.registerCalls != 1 {
		t.Fatalf("registration calls = %d, want 1", as.registerCalls)
	}

	// The harness config: a LAUNCHER entry, mode 0600, and no secret in it. This
	// is the card's core invariant: the file that the harness reads must not
	// carry a credential, because a harness has no way to renew one.
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat harness config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("harness config mode = %o, want 600", perm)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read harness config: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("harness config is not JSON: %v\n%s", err, raw)
	}
	entry, ok := doc.MCPServers["jomform"]
	if !ok {
		t.Fatalf("harness config has no jomform server: %s", raw)
	}
	if entry.URL != "" || len(entry.Headers) != 0 {
		t.Fatalf("the entry still carries url=%q headers=%v — a static header cannot be renewed and goes 401 after an hour",
			entry.URL, entry.Headers)
	}
	if entry.Command != "/usr/local/bin/jomform" {
		t.Fatalf("entry command = %q, want the absolute path of the installed binary", entry.Command)
	}
	if got, want := strings.Join(entry.Args, " "), "mcp proxy --mcp-url https://mcp.example.test/mcp"; got != want {
		t.Fatalf("entry args = %q, want %q", got, want)
	}
	for name, secret := range map[string]string{
		"access token":  fixtureAccessToken(),
		"refresh token": fixtureRefreshToken(),
	} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("the %s was written into the harness config — the config must never carry a credential:\n%s", name, raw)
		}
	}

	// The CLI's own copy, 0600, with the refresh token for logout.
	tokInfo, err := os.Stat(c.tokenPath())
	if err != nil {
		t.Fatalf("stat token.json: %v", err)
	}
	if perm := tokInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token.json mode = %o, want 600", perm)
	}
	tok, err := c.loadToken()
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if tok.AccessToken != fixtureAccessToken() || tok.RefreshToken != fixtureRefreshToken() {
		t.Fatalf("token.json does not hold the issued pair")
	}
	if tok.Scope != "read" {
		t.Fatalf("token.json scope = %q, want read", tok.Scope)
	}
	if tok.AccountName != "Warung ABC" || tok.AccountSlug != "warung-abc" {
		t.Fatalf("token.json account = %q/%q, want the MCP lookup result", tok.AccountName, tok.AccountSlug)
	}
}

func TestLoginDeniedStopsWithoutStoringAnything(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenErr("access_denied"))
	c, stdout, _, _ := newTestCLI(t, as)
	out := cursorConfig(t, c)

	err := c.run(loginArgs(as, out))
	if err == nil {
		t.Fatal("login succeeded after access_denied, want a non-zero failure")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Fatalf("error %q does not say the request was denied", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("harness config was written after a denial: %v", statErr)
	}
	if _, statErr := os.Stat(c.tokenPath()); !os.IsNotExist(statErr) {
		t.Fatalf("token.json was written after a denial: %v", statErr)
	}
	if as.tokenCalls != 1 {
		t.Fatalf("polls after a denial = %d, want 1 (denial is terminal)", as.tokenCalls)
	}
	// The user still saw where to go; only the path line is absent.
	if n := len(outputLines(stdout)); n != 3 {
		t.Fatalf("stdout lines = %d, want 3 before the failure", n)
	}
}

func TestLoginExpiredSaysSoAndOffersARerun(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenErr("expired_token"))
	c, _, _, _ := newTestCLI(t, as)

	err := c.run(loginArgs(as, cursorConfig(t, c)))
	if err == nil {
		t.Fatal("login succeeded after expired_token, want a failure")
	}
	if !strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "again") {
		t.Fatalf("error %q does not say it expired and offer a re-run", err)
	}
	if as.tokenCalls != 1 {
		t.Fatalf("polls after expiry = %d, want 1", as.tokenCalls)
	}
}

func TestLoginInvalidGrantStops(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenErr("invalid_grant"))
	c, _, _, _ := newTestCLI(t, as)

	err := c.run(loginArgs(as, cursorConfig(t, c)))
	if err == nil || !strings.Contains(err.Error(), "rejected the device code") {
		t.Fatalf("error = %v, want invalid_grant reported as a rejected code", err)
	}
	if as.tokenCalls != 1 {
		t.Fatalf("polls after invalid_grant = %d, want 1", as.tokenCalls)
	}
}

func TestLoginRetriesTransientServerFailures(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(
		fakeTokenReply{status: http.StatusInternalServerError, body: "<html>502</html>"},
		fakeTokenReply{status: http.StatusConflict, body: `{"error":"conflict"}`},
		tokenOK("read"),
	)
	c, _, _, clock := newTestCLI(t, as)

	if err := c.run(loginArgs(as, cursorConfig(t, c))); err != nil {
		t.Fatalf("login should survive a 500 and a 409: %v", err)
	}
	if as.tokenCalls != 3 {
		t.Fatalf("polls = %d, want 3 (two transient answers then success)", as.tokenCalls)
	}
	if len(clock.sleeps) != 3 {
		t.Fatalf("waits = %v, want one per poll", clock.sleeps)
	}
}

func TestLoginGivesUpOnPersistentServerFailure(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(fakeTokenReply{status: http.StatusInternalServerError, body: "boom"})
	c, _, _, _ := newTestCLI(t, as)

	err := c.run(loginArgs(as, cursorConfig(t, c)))
	if err == nil {
		t.Fatal("login succeeded against a server that only answers 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %q does not name the status", err)
	}
	if as.tokenCalls != maxTransientPolls+1 {
		t.Fatalf("polls = %d, want %d before giving up", as.tokenCalls, maxTransientPolls+1)
	}
}

func TestLoginMalformedTokenBodyIsAnErrorNotAToken(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(fakeTokenReply{status: http.StatusOK, body: `{"access_token":`})
	c, _, _, _ := newTestCLI(t, as)
	out := cursorConfig(t, c)

	err := c.run(loginArgs(as, out))
	if err == nil {
		t.Fatal("login accepted a truncated token response")
	}
	if !strings.Contains(err.Error(), "could not read") {
		t.Fatalf("error %q does not say the response was unreadable", err)
	}
	if _, statErr := os.Stat(c.tokenPath()); !os.IsNotExist(statErr) {
		t.Fatalf("token.json was written from a malformed response: %v", statErr)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("harness config was written from a malformed response: %v", statErr)
	}
}

func TestLoginMalformedDeviceAuthorizationIsAnError(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenOK("read"))
	c, _, _, _ := newTestCLI(t, as)
	// A device response that is valid JSON but carries no code pair must not be
	// printed as if it were one.
	as.mu.Lock()
	as.device = deviceGrant{ExpiresIn: 600, Interval: 1}
	as.mu.Unlock()

	if err := c.run(loginArgs(as, cursorConfig(t, c))); err == nil {
		t.Fatal("login accepted a device response with no device_code/user_code")
	}
	if as.tokenCalls != 0 {
		t.Fatalf("polls = %d, want 0 — there was no code to poll", as.tokenCalls)
	}
}

// TestLoginPrintsNoTokenLookingString is the leak guard: the only things stdout
// may carry before the path are the URL, the code and the expiry sentence.
func TestLoginPrintsNoTokenLookingString(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenErr("authorization_pending"), tokenOK("readwrite"))
	c, stdout, stderr, _ := newTestCLI(t, as)

	if err := c.run(loginArgs(as, cursorConfig(t, c))); err != nil {
		t.Fatalf("login: %v", err)
	}
	lines := outputLines(stdout)
	if len(lines) < 3 {
		t.Fatalf("stdout = %q, want at least the three handshake lines", lines)
	}
	// Long opaque runs are what a token looks like. The path line is allowed to
	// be long (it is a filesystem path); the handshake lines are not.
	opaque := regexp.MustCompile(`[A-Za-z0-9_\-+/=]{32,}`)
	for i, line := range lines[:3] {
		if m := opaque.FindString(line); m != "" {
			t.Fatalf("line %d of the handshake output carries a token-looking run (%d chars): %q", i+1, len(m), m)
		}
	}
	all := stdout.String() + stderr.String()
	for name, secret := range map[string]string{
		"access token":  fixtureAccessToken(),
		"refresh token": fixtureRefreshToken(),
		"device code":   fixtureDeviceCode(),
	} {
		if strings.Contains(all, secret) {
			t.Fatalf("the %s reached stdout/stderr", name)
		}
	}
}

func TestLoginNeedsTheHeadlessFlag(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, _ := newTestCLI(t, as)

	err := c.run([]string{"mcp", "login", "--server", as.srv.URL})
	if err == nil {
		t.Fatal("login without --headless succeeded")
	}
	if as.registerCalls != 0 || as.deviceCalls != 0 {
		t.Fatalf("a rejected command line still talked to the server (%d/%d calls)", as.registerCalls, as.deviceCalls)
	}
}

func TestLoginRefusesAPathOutsideTheHarnessList(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenOK("read"))
	c, _, _, _ := newTestCLI(t, as)
	// Inside the CLI's own home, but not a config any harness reads.
	elsewhere := filepath.Join(c.home, "notes.json")

	err := c.run(loginArgs(as, elsewhere))
	if err == nil {
		t.Fatal("login wrote to an unlisted path without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error %q does not point at --force", err)
	}
	// The refusal must come before a code is minted.
	if as.deviceCalls != 0 {
		t.Fatalf("device authorisations = %d, want 0 — the path is checked first", as.deviceCalls)
	}

	as.setTokenScript(tokenOK("read"))
	args := append(loginArgs(as, elsewhere), "--force")
	if err := c.run(args); err != nil {
		t.Fatalf("--force should allow the path: %v", err)
	}
	if _, statErr := os.Stat(elsewhere); statErr != nil {
		t.Fatalf("--force did not write %s: %v", elsewhere, statErr)
	}
}

func TestLoginReusesARegisteredClient(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenOK("read"))
	c, _, _, _ := newTestCLI(t, as)

	if err := c.run(loginArgs(as, cursorConfig(t, c))); err != nil {
		t.Fatalf("first login: %v", err)
	}
	as.setTokenScript(tokenOK("read"))
	if err := c.run(loginArgs(as, cursorConfig(t, c))); err != nil {
		t.Fatalf("second login: %v", err)
	}
	if as.registerCalls != 1 {
		t.Fatalf("registrations = %d, want 1 (the client_id is reused)", as.registerCalls)
	}
}

// ─── status ─────────────────────────────────────────────────────────────────

func storedFixture(t *testing.T, as *fakeAS, c *cli, expires time.Time, account string) {
	t.Helper()
	if err := c.saveToken(storedToken{
		Server: as.srv.URL, MCPURL: "https://mcp.example.test/mcp",
		ClientID: "jfc_cli_fixture", AccessToken: fixtureAccessToken(), RefreshToken: fixtureRefreshToken(),
		TokenType: "Bearer", Scope: "read readwrite", ExpiresAt: expires,
		AccountName: account, AccountSlug: "warung-abc",
		HarnessPath: filepath.Join(c.home, ".cursor", "mcp.json"), CreatedAt: expires.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
}

func TestStatusPrintsExpiryScopesAndAccount(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")

	if err := c.run([]string{"mcp", "status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	lines := outputLines(stdout)
	if len(lines) != 3 {
		t.Fatalf("status printed %d line(s), want expiry/scopes/account: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "expires: ") {
		t.Fatalf("line 1 = %q, want the expiry", lines[0])
	}
	if lines[1] != "scopes: read readwrite" {
		t.Fatalf("line 2 = %q, want the granted scopes", lines[1])
	}
	if lines[2] != "account: Warung ABC (warung-abc)" {
		t.Fatalf("line 3 = %q, want the account", lines[2])
	}
}

// TestStatusWithOutReportsTheHarnessEntry covers the `--out` half of status: it
// says whether the file the harness will read actually carries our entry.
func TestStatusWithOutReportsTheHarnessEntry(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	out := cursorConfig(t, c)
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(out, []byte(`{"mcpServers": {}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := c.run([]string{"mcp", "status", "--out", out}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout.String(), "harness: "+out+" (no jomform entry)") {
		t.Fatalf("status printed %q, want the missing entry reported", stdout.String())
	}

	merged, err := mergeServer([]byte(`{"mcpServers": {}}`), serverName, entryFor(t, "https://mcp.example.test/mcp", fixtureAccessToken()))
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := os.WriteFile(out, merged, 0o600); err != nil {
		t.Fatalf("seed merged: %v", err)
	}
	stdout.Reset()
	if err := c.run([]string{"mcp", "status", "--out", out}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout.String(), "harness: "+out+" (jomform entry present)") {
		t.Fatalf("status printed %q, want the entry reported", stdout.String())
	}
}

// TestLogoutRefusesAnUnlistedOutPath checks the check happens BEFORE the revoke:
// a refused path must not leave a revoked-but-still-stored token behind.
func TestLogoutRefusesAnUnlistedOutPath(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	tok, err := c.loadToken()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tok.HarnessPath = cursorConfig(t, c)
	if err := c.saveToken(tok); err != nil {
		t.Fatalf("seed: %v", err)
	}
	elsewhere := filepath.Join(c.home, "other.json")
	if err := os.WriteFile(elsewhere, []byte(`{"mcpServers": {"jomform": {"url": "u"}}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err = c.run([]string{"mcp", "logout", "--out", elsewhere})
	if err == nil {
		t.Fatal("logout edited a path outside the harness list without --force")
	}
	as.mu.Lock()
	revoked := as.revokeForm
	as.mu.Unlock()
	if revoked != nil {
		t.Fatal("logout revoked the token before it knew whether it could finish the job")
	}
	if _, statErr := os.Stat(c.tokenPath()); statErr != nil {
		t.Fatalf("token.json was removed although nothing was revoked: %v", statErr)
	}
}

// TestLogoutFollowsThePathALoginWasForcedToWrite covers the one path logout may
// edit without --force: the one the user already approved at login.
func TestLogoutFollowsThePathALoginWasForcedToWrite(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenOK("read"))
	c, _, _, clock := newTestCLI(t, as)
	unusual := filepath.Join(c.home, "custom", "mcp.json")

	args := append(loginArgs(as, unusual), "--force")
	if err := c.run(args); err != nil {
		t.Fatalf("forced login: %v", err)
	}
	clock.t = clock.t.Add(time.Minute)
	if err := c.run([]string{"mcp", "logout"}); err != nil {
		t.Fatalf("logout of a forced path: %v", err)
	}
	raw, err := os.ReadFile(unusual)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if bytes.Contains(raw, []byte(`"jomform"`)) {
		t.Fatalf("the entry survived logout:\n%s", raw)
	}
}

// TestStatusFailsWithoutAToken keeps the failure honest when there is nothing to
// renew from.
func TestStatusFailsWithoutAToken(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, _ := newTestCLI(t, as)

	if err := c.run([]string{"mcp", "status"}); err == nil {
		t.Fatal("status succeeded with no stored token")
	}
}

// TestStatusRenewsAnExpiredAccessToken is this card's symptom, fixed.
//
// An expired access token used to end `status` with a non-zero exit and "run
// login again", while a perfectly good 30-day refresh token sat on disk unused.
// It now renews, persists the rotated pair and reports the expiry the harness's
// launcher entry will actually see.
func TestStatusRenewsAnExpiredAccessToken(t *testing.T) {
	as := newFakeAS(t)
	c, stdout, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(-time.Minute), "Warung ABC")

	if err := c.run([]string{"mcp", "status"}); err != nil {
		t.Fatalf("status on an expired access token: %v (a refresh token is stored, so this is renewable)", err)
	}
	_, refreshes := as.mcpSnapshot()
	if len(refreshes) != 1 {
		t.Fatalf("renewals = %d, want exactly 1", len(refreshes))
	}
	if got := refreshes[0].Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q, want refresh_token", got)
	}
	if got := refreshes[0].Get("refresh_token"); got != fixtureRefreshToken() {
		t.Fatalf("the renewal presented %q, want the stored refresh token", got)
	}
	if !strings.Contains(stdout.String(), "account: Warung ABC") {
		t.Fatalf("status printed %q, want the account line", stdout.String())
	}
	tok, err := c.loadToken()
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if tok.AccessToken != refreshedAccess(1) {
		t.Fatalf("the stored access token was not replaced by the renewal")
	}
	if tok.RefreshToken != refreshedRefresh(1) {
		t.Fatalf("the ROTATED refresh token was not persisted — the next renewal would present a dead one")
	}
	if tok.AccountName != "Warung ABC" {
		t.Fatalf("the renewal dropped the account name (%q)", tok.AccountName)
	}
}

// TestStatusReportsADeadGrant: when the server refuses the renewal, status is a
// failure and says the one thing that helps.
func TestStatusReportsADeadGrant(t *testing.T) {
	as := newFakeAS(t)
	as.setRefreshScript(tokenErr("invalid_grant"))
	c, stdout, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(-time.Minute), "Warung ABC")

	err := c.run([]string{"mcp", "status"})
	if err == nil {
		t.Fatal("status succeeded although the grant can no longer be renewed")
	}
	if !strings.Contains(err.Error(), "login") {
		t.Fatalf("error %q does not point at a fresh login", err)
	}
	if !strings.Contains(stdout.String(), "account: Warung ABC") {
		t.Fatalf("status printed %q, want the lines it does know before the failure", stdout.String())
	}
}

// ─── logout ─────────────────────────────────────────────────────────────────

func TestLogoutRevokesAndRemovesOnlyOurEntry(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenOK("read"))
	c, stdout, _, _ := newTestCLI(t, as)
	out := cursorConfig(t, c)

	otherEntry := `"github":   { "command": "npx", "args": [ "-y", "server-github" ] }`
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := "{\n  \"mcpServers\": {\n    " + otherEntry + "\n  }\n}\n"
	if err := os.WriteFile(out, []byte(original), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := c.run(loginArgs(as, out)); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := c.run([]string{"mcp", "logout"}); err != nil {
		t.Fatalf("logout: %v", err)
	}

	as.mu.Lock()
	revoke := as.revokeForm
	as.mu.Unlock()
	if revoke == nil {
		t.Fatal("logout did not call /oauth/revoke")
	}
	if got := revoke.Get("token"); got != fixtureRefreshToken() {
		t.Fatalf("revoked token = %q, want the refresh token", got)
	}
	if got := revoke.Get("token_type_hint"); got != "refresh_token" {
		t.Fatalf("token_type_hint = %q, want refresh_token", got)
	}
	if got := revoke.Get("client_id"); got != "jfc_cli_fixture" {
		t.Fatalf("client_id = %q, want the registered client", got)
	}
	if _, err := os.Stat(c.tokenPath()); !os.IsNotExist(err) {
		t.Fatalf("token.json survived logout: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if bytes.Contains(raw, []byte(`"jomform"`)) {
		t.Fatalf("the jomform entry survived logout:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte(otherEntry)) {
		t.Fatalf("logout damaged the other server's bytes:\n%s", raw)
	}
	if !strings.Contains(stdout.String(), "removed the jomform entry from "+out) {
		t.Fatalf("stdout = %q, want the removal reported", stdout.String())
	}
}

func TestLogoutWithoutAStoredTokenFails(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, _ := newTestCLI(t, as)
	if err := c.run([]string{"mcp", "logout"}); err == nil {
		t.Fatal("logout succeeded with nothing stored")
	}
	if as.revokeForm != nil {
		t.Fatal("logout called /oauth/revoke with no token")
	}
}

func TestLogoutKeepsTheLocalCopyWhenRevocationFails(t *testing.T) {
	as := newFakeAS(t)
	c, _, _, clock := newTestCLI(t, as)
	storedFixture(t, as, c, clock.now().Add(time.Hour), "Warung ABC")
	// Point the stored server at a port nothing listens on.
	tok, _ := c.loadToken()
	tok.Server = "http://127.0.0.1:1"
	tok.HarnessPath = filepath.Join(c.home, ".cursor", "mcp.json")
	if err := c.saveToken(tok); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := c.run([]string{"mcp", "logout"}); err == nil {
		t.Fatal("logout reported success although the server could not be reached")
	}
	if _, err := os.Stat(c.tokenPath()); err != nil {
		t.Fatalf("token.json was deleted after a failed revocation: %v", err)
	}
}

// TestExpiryWords pins the human phrase the third line uses.
func TestExpiryWords(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{600, "10 minutes"},
		{60, "1 minute"},
		{61, "2 minutes"},
		{0, "a moment"},
	}
	for _, tc := range cases {
		if got := expiryWords(tc.in); got != tc.want {
			t.Fatalf("expiryWords(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSlowDownBumpIsObservable is the narrow reading of the bite proof: the
// interval the loop waits after slow_down is the bumped one, taken from the
// same slice the polling test asserts on.
func TestSlowDownBumpIsObservable(t *testing.T) {
	as := newFakeAS(t)
	as.setTokenScript(tokenErr("slow_down"), tokenOK("read"))
	c, _, _, clock := newTestCLI(t, as)
	if err := c.run(loginArgs(as, cursorConfig(t, c))); err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(clock.sleeps) != 2 || clock.sleeps[1] != 6*time.Second {
		t.Fatalf("waits = %v, want [1s 6s] (base 1s + the 5s slow_down step)", clock.sleeps)
	}
	if clock.sleeps[1]-clock.sleeps[0] != slowDownStep {
		t.Fatalf("the slow_down bump = %s, want %s", clock.sleeps[1]-clock.sleeps[0], slowDownStep)
	}
}
