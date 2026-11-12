package httpapi

// RFC 8628 device grant + scope ladder — handler-level tests (card t_9a6cc85f).
//
// These drive the REAL HTTP handlers over an in-memory store, so they assert
// what a harness actually receives: the response shape, the RFC's error codes,
// the consent email's content, and the scope on the token that comes out the far
// end. The SQL properties behind them (single-use UPDATE, row lock, persisted
// slow_down penalty, rotation atomicity) are asserted against Postgres in
// dbstore/oauth_device_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// captureMailer records consent emails instead of sending them.
type captureMailer struct {
	mu   sync.Mutex
	sent []capturedMail
}

type capturedMail struct {
	To, Subject, Body, Lang string
}

func (m *captureMailer) Send(to, subject, body, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, capturedMail{To: to, Subject: subject, Body: body, Lang: lang})
	return nil
}

func (m *captureMailer) all() []capturedMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]capturedMail, len(m.sent))
	copy(out, m.sent)
	return out
}

// deviceTestHandlers builds a handler set whose user list contains one known
// address (it@example.test) and no super-admins.
func deviceTestHandlers(t *testing.T) (*OAuthHandlers, *fakeOAuthStore, *captureMailer) {
	t.Helper()
	store := newFakeOAuthStore()
	users := &fakeOAuthUsers{
		byID:    map[string]*dbstore.User{"user-1": {ID: "user-1", Email: "it@example.test", Verified: true, Lang: "en"}},
		byEmail: map[string]*dbstore.User{"it@example.test": {ID: "user-1", Email: "it@example.test", Verified: true, Lang: "en"}},
	}
	mailer := &captureMailer{}
	h := &OAuthHandlers{
		Store: store, Users: users,
		Accounts: &fakeOAuthAccounts{ownerAccounts: []*dbstore.Account{{ID: "acct-1", Slug: "merchant"}}},
		BaseURL:  "https://jomform.com",
		Mailer:   mailer,
		// Synchronous in tests: the production path hands the send to a
		// goroutine so "address exists" and "address does not" cost the same
		// wall-clock time, which would make this assertion a race.
		SendDeviceMailAsync: false,
		CodeTTL:             10 * time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	return h, store, mailer
}

func postDeviceAuth(t *testing.T, h *OAuthHandlers, form url.Values, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/oauth/device_authorization", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	h.deviceAuthorization(rec, req)
	return rec
}

func postToken(t *testing.T, h *OAuthHandlers, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.token(rec, req)
	return rec
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

// rewindPoll rewinds the fake row's last_polled_at past the interval, standing
// in for the five seconds a real client waits between polls. Without it the
// next exchange would (correctly) answer slow_down and the test would be
// asserting the pacing rule instead of the flow.
func rewindPoll(t *testing.T, store *fakeOAuthStore, deviceCode string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	d, ok := store.devices[hashToken(deviceCode)]
	if !ok {
		t.Fatalf("device row %q not found", deviceCode)
	}
	ts := time.Now().UTC().Add(-10 * time.Second)
	d.LastPolledAt = &ts
}

// deviceStart runs the authorization request and returns the parsed response.
func deviceStart(t *testing.T, h *OAuthHandlers, clientID, scope, hint string) deviceAuthResponse {
	t.Helper()
	form := url.Values{"client_id": {clientID}}
	if scope != "" {
		form.Set("scope", scope)
	}
	if hint != "" {
		form.Set("login_hint", hint)
	}
	rec := postDeviceAuth(t, h, form, "203.0.113.9:443")
	if rec.Code != http.StatusOK {
		t.Fatalf("device_authorization status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out deviceAuthResponse
	decodeInto(t, rec, &out)
	if out.DeviceCode == "" || out.UserCode == "" {
		t.Fatalf("empty device_code/user_code: %+v", out)
	}
	return out
}

// TestDeviceAuthorizationHappyPath is the whole onboarding, end to end: ask for
// a code, read the consent email, approve it, exchange the device_code for a
// token, and prove the token is the credential the MCP transport authenticates
// with.
func TestDeviceAuthorizationHappyPath(t *testing.T) {
	h, store, mailer := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")

	start := deviceStart(t, h, clientID, "read", "it@example.test")
	if start.ExpiresIn != 600 || start.Interval != 5 {
		t.Fatalf("expires_in/interval = %d/%d, want 600/5", start.ExpiresIn, start.Interval)
	}
	if !strings.Contains(start.VerificationURI, "/profile?tab=mcp") {
		t.Fatalf("verification_uri = %q, want the dashboard consent page", start.VerificationURI)
	}
	if !strings.Contains(start.VerificationURIComplete, url.QueryEscape(start.UserCode)) {
		t.Fatalf("verification_uri_complete %q does not carry the user_code", start.VerificationURIComplete)
	}

	// The email IS the consent screen: it must name the client, the scope in
	// plain words, where it came from, the code, and the warning line.
	mails := mailer.all()
	if len(mails) != 1 {
		t.Fatalf("consent emails sent = %d, want 1", len(mails))
	}
	mail := mails[0]
	if mail.To != "it@example.test" {
		t.Fatalf("consent email went to %q", mail.To)
	}
	// The rendered body is logged so the card's evidence quotes the real text
	// rather than a description of it.
	t.Logf("consent email:\n%s", mail.Body)
	for _, want := range []string{start.UserCode, "claude", "read-only", "203.0.113.9", "do not share this code"} {
		if !strings.Contains(mail.Body, want) {
			t.Fatalf("consent email missing %q:\n%s", want, mail.Body)
		}
	}
	if strings.Contains(mail.Body, "read and write") {
		t.Fatalf("a read authorisation described itself as readwrite:\n%s", mail.Body)
	}

	// Polling before approval is authorization_pending, not an error the client
	// should give up on.
	rec := postToken(t, h, url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {start.DeviceCode}, "client_id": {clientID}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pending poll status = %d", rec.Code)
	}
	var pending struct{ Error string }
	decodeInto(t, rec, &pending)
	if pending.Error != "authorization_pending" {
		t.Fatalf("pending error = %q, want authorization_pending", pending.Error)
	}

	// The person approves on the dashboard. (The consent-card handler is a thin
	// wrapper over this store call; its own behaviour is exercised in
	// dbstore/oauth_device_test.go through ApproveDeviceCode/DenyDeviceCode.)
	if _, err := store.ApproveDeviceCode(context.Background(), hashToken(start.UserCode), "user-1", "acct-1", []string{"read"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// Let the advertised 5s interval elapse as far as the store is concerned, so
	// the next poll is a legitimately timed one rather than a slow_down.
	rewindPoll(t, store, start.DeviceCode)

	rec = postToken(t, h, url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {start.DeviceCode}, "client_id": {clientID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("token status = %d body=%s", rec.Code, rec.Body.String())
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	decodeInto(t, rec, &tok)
	if tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 {
		t.Fatalf("token response = %+v", tok)
	}
	if tok.Scope != "read" {
		t.Fatalf("granted scope = %q, want read (the device default and the ladder's floor)", tok.Scope)
	}

	// This is exactly the lookup mcpsrv.HTTPHandler performs on a bearer token,
	// and the scope it then enforces on every tool call.
	got, err := store.AccessTokenByHash(context.Background(), hashToken(tok.AccessToken))
	if err != nil {
		t.Fatalf("access token did not resolve: %v", err)
	}
	if got.UserID != "user-1" || strings.Join(got.Scope, " ") != "read" {
		t.Fatalf("resolved token = user %q scope %v", got.UserID, got.Scope)
	}
	if dbstore.ScopeHasWrite(got.Scope) {
		t.Fatalf("a read grant reports write capability")
	}
}

// TestDevicePollingTooFastReturnsSlowDown: RFC 8628 §3.5 — a client that polls
// faster than `interval` gets slow_down, and the penalty sticks.
func TestDevicePollingTooFastReturnsSlowDown(t *testing.T) {
	h, store, _ := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	start := deviceStart(t, h, clientID, "read", "it@example.test")

	form := url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {start.DeviceCode}, "client_id": {clientID}}
	// First answer: the client just started polling.
	rec := postToken(t, h, form)
	var first struct{ Error string }
	decodeInto(t, rec, &first)
	if first.Error != "authorization_pending" {
		t.Fatalf("first poll = %q, want authorization_pending", first.Error)
	}
	// One second later: too fast for a 5s interval.
	time.Sleep(1100 * time.Millisecond)
	rec = postToken(t, h, form)
	var second struct{ Error string }
	decodeInto(t, rec, &second)
	if second.Error != "slow_down" {
		t.Fatalf("second poll = %q, want slow_down", second.Error)
	}
	// The advertised interval grew by exactly 5s on the stored row.
	rows, err := store.DeviceCodeByUserCode(context.Background(), hashToken(start.UserCode))
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if rows.IntervalS != 10 {
		t.Fatalf("interval_s = %d, want 10", rows.IntervalS)
	}
}

// TestDeviceCodeIsSingleUse: an approved code mints exactly one token pair.
func TestDeviceCodeIsSingleUse(t *testing.T) {
	h, store, _ := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	start := deviceStart(t, h, clientID, "read", "it@example.test")
	if _, err := store.ApproveDeviceCode(context.Background(), hashToken(start.UserCode), "user-1", "", []string{"read"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	form := url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {start.DeviceCode}, "client_id": {clientID}}
	if rec := postToken(t, h, form); rec.Code != http.StatusOK {
		t.Fatalf("first exchange = %d body=%s", rec.Code, rec.Body.String())
	}
	rec := postToken(t, h, form)
	var errOut struct{ Error string }
	decodeInto(t, rec, &errOut)
	if rec.Code != http.StatusBadRequest || errOut.Error != "invalid_grant" {
		t.Fatalf("second exchange = %d %q, want 400 invalid_grant", rec.Code, errOut.Error)
	}
}

// TestDeviceCodeExpiry: past expires_in the exchange answers expired_token.
func TestDeviceCodeExpiry(t *testing.T) {
	h, store, _ := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	// A row that already expired, exactly the state a 10-minute-old code is in.
	const deviceCode = "expired-device-code"
	if err := store.CreateDeviceCode(context.Background(), &dbstore.OAuthDeviceCode{
		DeviceHash: hashToken(deviceCode), UserCodeHash: hashToken("BCDF-GHJK"),
		ClientID: clientID, UserID: "user-1", Scope: []string{"read"},
		ExpiresAt: time.Now().UTC().Add(-time.Minute), IntervalS: 5, CreatedAt: time.Now().UTC().Add(-11 * time.Minute),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := postToken(t, h, url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {deviceCode}, "client_id": {clientID}})
	var errOut struct{ Error string }
	decodeInto(t, rec, &errOut)
	if rec.Code != http.StatusBadRequest || errOut.Error != "expired_token" {
		t.Fatalf("expired exchange = %d %q, want 400 expired_token", rec.Code, errOut.Error)
	}
}

// TestDeviceAuthorizationDoesNotEnumerateAccounts: the response for an address
// with no account must be indistinguishable from one for a real account.
//
// The secrets are random per request, so byte equality of the raw bodies is
// impossible by construction; the test masks the two values and requires the
// remaining bytes to be identical — which is the whole observable surface: same
// status, same keys in the same order, same constant fields, same lengths.
func TestDeviceAuthorizationDoesNotEnumerateAccounts(t *testing.T) {
	h, _, mailer := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")

	known := postDeviceAuth(t, h, url.Values{
		"client_id": {clientID}, "scope": {"read"}, "login_hint": {"it@example.test"},
	}, "203.0.113.10:443")
	unknown := postDeviceAuth(t, h, url.Values{
		"client_id": {clientID}, "scope": {"read"}, "login_hint": {"nobody@example.test"},
	}, "203.0.113.11:443")

	if known.Code != unknown.Code {
		t.Fatalf("status differs: %d vs %d", known.Code, unknown.Code)
	}
	var a, b deviceAuthResponse
	decodeInto(t, known, &a)
	decodeInto(t, unknown, &b)
	if len(a.DeviceCode) != len(b.DeviceCode) || len(a.UserCode) != len(b.UserCode) {
		t.Fatalf("secret lengths differ: device %d/%d user %d/%d",
			len(a.DeviceCode), len(b.DeviceCode), len(a.UserCode), len(b.UserCode))
	}
	masked := func(rec *httptest.ResponseRecorder, d deviceAuthResponse) []byte {
		s := rec.Body.String()
		s = strings.ReplaceAll(s, d.DeviceCode, strings.Repeat("d", len(d.DeviceCode)))
		s = strings.ReplaceAll(s, url.QueryEscape(d.UserCode), strings.Repeat("u", len(d.UserCode)))
		s = strings.ReplaceAll(s, d.UserCode, strings.Repeat("u", len(d.UserCode)))
		return []byte(s)
	}
	if !bytes.Equal(masked(known, a), masked(unknown, b)) {
		t.Fatalf("responses differ once secrets are masked:\nknown:   %s\nunknown: %s",
			masked(known, a), masked(unknown, b))
	}
	// The only difference is server-side and invisible to the caller: exactly
	// one mail went out, to the address that exists.
	if mails := mailer.all(); len(mails) != 1 || mails[0].To != "it@example.test" {
		t.Fatalf("mails = %+v, want exactly one to the known address", mails)
	}
}

// TestRefreshCannotWidenScope is the bite-proof test for the whole ladder: a
// refresh token may never be exchanged for a scope the grant did not already
// hold (RFC 6749 §6), even when the request asks for one.
func TestRefreshCannotWidenScope(t *testing.T) {
	h, store, _ := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	start := deviceStart(t, h, clientID, "read", "it@example.test")
	if _, err := store.ApproveDeviceCode(context.Background(), hashToken(start.UserCode), "user-1", "", []string{"read"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	rec := postToken(t, h, url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {start.DeviceCode}, "client_id": {clientID}})
	var first struct {
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	decodeInto(t, rec, &first)
	if first.Scope != "read" {
		t.Fatalf("initial scope = %q", first.Scope)
	}

	// Ask to come back wider. The `scope` parameter on a refresh is NOT an
	// upgrade path — it is ignored, and the rotated pair keeps the grant.
	rec = postToken(t, h, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {first.RefreshToken},
		"client_id":     {clientID},
		"scope":         {"readwrite"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", rec.Code, rec.Body.String())
	}
	var second struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	decodeInto(t, rec, &second)
	if second.Scope != "read" {
		t.Fatalf("refresh granted scope = %q, want read — widening through refresh must be impossible", second.Scope)
	}
	// And the credential on the wire is the same width, not merely the echo.
	got, err := store.AccessTokenByHash(context.Background(), hashToken(second.AccessToken))
	if err != nil {
		t.Fatalf("rotated access token: %v", err)
	}
	if dbstore.ScopeHasWrite(got.Scope) {
		t.Fatalf("rotated token holds write scope %v after asking for readwrite on refresh", got.Scope)
	}
}

// TestDeviceDeniedReturnsAccessDenied: a refusal stops the polling loop.
func TestDeviceDeniedReturnsAccessDenied(t *testing.T) {
	h, store, _ := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	start := deviceStart(t, h, clientID, "read", "it@example.test")
	if err := store.DenyDeviceCode(context.Background(), hashToken(start.UserCode), "user-1"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	rec := postToken(t, h, url.Values{"grant_type": {oauthmeta.DeviceGrantType}, "device_code": {start.DeviceCode}, "client_id": {clientID}})
	var errOut struct{ Error string }
	decodeInto(t, rec, &errOut)
	if rec.Code != http.StatusBadRequest || errOut.Error != "access_denied" {
		t.Fatalf("denied exchange = %d %q, want 400 access_denied", rec.Code, errOut.Error)
	}
}

// TestDeviceAuthorizationRateLimit: the email path must not become a mail bomb.
// The FOURTH request from one address in an hour is refused.
func TestDeviceAuthorizationRateLimit(t *testing.T) {
	h, _, mailer := deviceTestHandlers(t)
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	for i := 0; i < 3; i++ {
		if rec := postDeviceAuth(t, h, url.Values{
			"client_id": {clientID}, "scope": {"read"}, "login_hint": {"it@example.test"},
		}, "198.51.100.20:443"); rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d", i+1, rec.Code)
		}
	}
	rec := postDeviceAuth(t, h, url.Values{
		"client_id": {clientID}, "scope": {"read"}, "login_hint": {"it@example.test"},
	}, "198.51.100.20:443")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("fourth request = %d, want 429", rec.Code)
	}
	if len(mailer.all()) != 3 {
		t.Fatalf("mails sent = %d, want 3 (the limit is on the sends too)", len(mailer.all()))
	}
}

// TestDeviceAuthorizationNeverMailsSuperAdmin: an emailed code must not be the
// only thing between an attacker and a platform-admin token.
func TestDeviceAuthorizationNeverMailsSuperAdmin(t *testing.T) {
	h, _, mailer := deviceTestHandlers(t)
	h.SuperAdmin = func(email string) bool { return email == "it@example.test" }
	clientID := registerTestClient(t, h, "https://claude.example/callback")
	rec := postDeviceAuth(t, h, url.Values{
		"client_id": {clientID}, "scope": {"readwrite"}, "login_hint": {"it@example.test"},
	}, "192.0.2.5:443")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the same 200 (no oracle)", rec.Code)
	}
	if mails := mailer.all(); len(mails) != 0 {
		t.Fatalf("a consent email was sent to a platform admin: %+v", mails)
	}
}

// TestDeviceGrantIsAdvertised: a client discovers the device flow from the AS
// metadata, with no change on its side.
func TestDeviceGrantIsAdvertised(t *testing.T) {
	doc := authorizationServerDocument("mcp.jomform.com")
	if got := doc["device_authorization_endpoint"]; got != oauthmeta.Base()+"/oauth/device_authorization" {
		t.Fatalf("device_authorization_endpoint = %v", got)
	}
	grants, ok := doc["grant_types_supported"].([]string)
	if !ok {
		t.Fatalf("grant_types_supported is not a string list")
	}
	found := false
	for _, g := range grants {
		if g == oauthmeta.DeviceGrantType {
			found = true
		}
	}
	if !found {
		t.Fatalf("grant_types_supported = %v, missing %s", grants, oauthmeta.DeviceGrantType)
	}
}

// TestDeviceScopeDefaultIsReadAndCannotWiden: the device path defaults to read
// (unlike the browser path, which is untouched) and refuses an unadvertised
// scope instead of narrowing it.
func TestDeviceScopeDefaultIsReadAndCannotWiden(t *testing.T) {
	if got, ok := oauthmeta.DeviceScopes(""); !ok || strings.Join(got, " ") != "read" {
		t.Fatalf("device default scope = %v/%v, want read", got, ok)
	}
	if _, ok := oauthmeta.DeviceScopes("readwrite read"); !ok {
		t.Fatalf("'readwrite read' should resolve")
	}
	if _, ok := oauthmeta.DeviceScopes("admin"); ok {
		t.Fatalf("an unadvertised scope resolved")
	}
	// The browser flow's default is deliberately unchanged.
	if got := oauthmeta.DefaultScopes(); strings.Join(got, " ") != "readwrite" {
		t.Fatalf("authorize default changed to %v — this card must not touch it", got)
	}
}

// TestUserCodeNormalisation: a person types the code from an email, so case,
// spaces and a missing dash must all resolve to the same row.
func TestUserCodeNormalisation(t *testing.T) {
	for _, in := range []string{"BCDF-GHJK", "bcdfghjk", "bcdf ghjk", " BCDF-GHJK "} {
		if got := normalizeUserCode(in); got != "BCDF-GHJK" {
			t.Fatalf("normalizeUserCode(%q) = %q", in, got)
		}
	}
	for _, bad := range []string{"", "BCDF", "BCDF-GHJKX"} {
		if got := normalizeUserCode(bad); got != "" {
			t.Fatalf("normalizeUserCode(%q) = %q, want \"\" (wrong length)", bad, got)
		}
	}
	// A same-length value with a character outside the alphabet normalises fine
	// and then simply never matches a row — the store lookup is the rejection.
	if got := normalizeUserCode("BCDF-GHJ!"); got != "BCDF-GHJ!" {
		if got == "" {
			t.Fatalf("a same-length code was rejected before the store could")
		}
	}
}

// TestDeviceAuthorizationRejectsUnknownClient keeps an unregistered client from
// obtaining a code that no dashboard can ever revoke.
func TestDeviceAuthorizationRejectsUnknownClient(t *testing.T) {
	h, _, mailer := deviceTestHandlers(t)
	rec := postDeviceAuth(t, h, url.Values{"client_id": {"jfc_does_not_exist"}}, "192.0.2.9:443")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var out struct{ Error string }
	decodeInto(t, rec, &out)
	if out.Error != "invalid_client" {
		t.Fatalf("error = %q, want invalid_client", out.Error)
	}
	if len(mailer.all()) != 0 {
		t.Fatalf("mail sent for an unknown client")
	}
}

// TestDCRGrantTypesMatchASMetadata is the drift guard for the two documents
// that describe the same credential: the RFC 8414 `grant_types_supported` a
// client discovers, and the RFC 7591 registration response's `grant_types`.
//
// RFC 7591 §3.2.1 makes the registration response the authoritative statement
// of what the server will accept from THIS client, and RFC 8628 §5 lets a
// harness pick the device flow from it. /oauth/device_authorization only looks
// the client up by id and never consults grant_types, so a client registered
// with a narrower list could still use the device grant — the server would
// simply have under-described the credential it just issued.
func TestDCRGrantTypesMatchASMetadata(t *testing.T) {
	o, store, _ := testOAuthHandlers(t)
	body := strings.NewReader(`{"client_name":"harness","redirect_uris":["https://client.example.com/cb"]}`)
	req := httptest.NewRequest("POST", "/oauth/register", body)
	rec := httptest.NewRecorder()
	o.register(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}
	var reg struct {
		ClientID   string   `json:"client_id"`
		GrantTypes []string `json:"grant_types"`
	}
	decodeJSONBody(t, rec, &reg)
	if reg.ClientID == "" {
		t.Fatal("no client_id in the registration response")
	}

	advertised, ok := authorizationServerDocument("mcp.jomform.com")["grant_types_supported"].([]string)
	if !ok {
		t.Fatal("grant_types_supported is not a string list")
	}
	if !slices.Contains(advertised, oauthmeta.DeviceGrantType) {
		t.Fatalf("AS metadata advertises %v, missing %s — device discovery is broken", advertised, oauthmeta.DeviceGrantType)
	}
	if !slices.Equal(reg.GrantTypes, advertised) {
		t.Fatalf("DCR grant_types = %v, AS metadata grant_types_supported = %v — the two documents describe one credential and must be one list", reg.GrantTypes, advertised)
	}

	// The response must describe what was actually stored, not a narrower set.
	store.mu.Lock()
	client, ok := store.clients[reg.ClientID]
	var stored []string
	if ok {
		stored = client.GrantTypes
	}
	store.mu.Unlock()
	if !ok {
		t.Fatalf("registered client %s was not stored", reg.ClientID)
	}
	if !slices.Equal(stored, reg.GrantTypes) {
		t.Fatalf("stored oauth_clients.grant_types = %v, registration response said %v", stored, reg.GrantTypes)
	}
}

var _ = errors.Is
