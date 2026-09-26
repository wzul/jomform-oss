// Issue #454 integration tests: trusted-devices management list + revoke.
//   - GET /v1/auth/trust-devices lists each trusted device (label, ip,
//     first_seen, expiry) with the current device marked.
//   - DELETE /v1/auth/trust/{id} revokes ONE device, immediately (the very
//     next login on that device is challenged for TOTP again).
//   - DELETE /v1/auth/trust revokes ALL devices ("log out everywhere").
//   - A legacy trust entry with no #454 metadata still lists + revokes.
//
// These need a real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL) and are skipped in CI (which sets neither).
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
)

// loginOnceUA performs a full password login with an explicit User-Agent (so
// the #454 device label can be asserted) plus optional TOTP/remember/trust.
//
// The client address is the test's own (testIP), not httptest's default
// "192.0.2.1" — see loginOnce for why that default is a shared counter no
// process's scoped reclaim can clear.
func loginOnceUA(t *testing.T, h *AuthHandlers, email, password, totpCode string, totpRemember bool, trustValue, ua string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body := map[string]any{"email": email, "password": password}
	if totpCode != "" {
		body["totp_code"] = totpCode
	}
	if totpRemember {
		body["totp_remember"] = true
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(string(mustJSON(body))))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", testIP(t))
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if trustValue != "" {
		req.AddCookie(&http.Cookie{Name: trustCookie, Value: trustValue})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// trustDevicesGet calls GET /v1/auth/trust-devices with a session cookie and
// decodes the device rows.
func trustDevicesGet(t *testing.T, h *AuthHandlers, session string) []trustDeviceOut {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/trust-devices", nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list trusted devices status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out []trustDeviceOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode devices: %v (body %s)", err, rec.Body.String())
	}
	return out
}

// trustDevicesDelete calls DELETE /v1/auth/trust[/{id}] with a session cookie.
func trustDevicesDelete(t *testing.T, h *AuthHandlers, session, id string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	path := "/v1/auth/trust"
	if id != "" {
		path += "/" + id
	}
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// enable2FAAndSession sets up + enables 2FA on a fresh verified account and
// returns the TOTP secret plus a live session cookie.
func enable2FAAndSession(t *testing.T, h *AuthHandlers, email, ip string) (secret, session string) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	registerAndVerify(t, h, email, "password123", ip)
	session = cookieValueFrom(postJSON(mux, "/v1/auth/login", map[string]any{
		"email": email, "password": "password123",
	}, ip), sessionCookie)
	if session == "" {
		t.Fatal("no session cookie for 2FA setup")
	}
	setup := postJSONCookies(mux, "/v1/auth/2fa/setup", map[string]any{}, ip, session)
	var s map[string]string
	_ = json.Unmarshal(setup.Body.Bytes(), &s)
	secret = s["secret"]
	if secret == "" {
		t.Fatalf("2FA setup no secret (status %d body %s)", setup.Code, setup.Body.String())
	}
	if rec := postJSONCookies(mux, "/v1/auth/2fa/enable", map[string]any{"code": mustTOTP(secret)}, ip, session); rec.Code != http.StatusOK {
		t.Fatalf("enable 2FA status = %d body=%s", rec.Code, rec.Body.String())
	}
	return secret, session
}

// TestTrustedDevicesListAndRevokeOne is the issue #454 acceptance test:
// two trusted devices are listed with labels + expiry; revoking one is
// immediate (its next login is challenged) while the other keeps skipping.
func TestTrustedDevicesListAndRevokeOne(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)

	email := fmt.Sprintf("trustdev-%d@example.test", time.Now().UnixNano())
	secret, session := enable2FAAndSession(t, h, email, ip)

	const uaA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	const uaB = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

	// Trust device A, then device B — each mints a distinct trust cookie.
	loginA := loginOnceUA(t, h, email, "password123", mustTOTP(secret), true, "", uaA)
	if loginA.Code != http.StatusOK {
		t.Fatalf("trusted login A status = %d body=%s", loginA.Code, loginA.Body.String())
	}
	trustA := cookieValueFrom(loginA, trustCookie)
	if trustA == "" {
		t.Fatal("no trust cookie issued for device A")
	}
	// Device B is a new device (no trust cookie yet): it completes a fresh
	// TOTP login with remember=true, which mints B's own trust cookie.
	loginB := loginOnceUA(t, h, email, "password123", mustTOTP(secret), true, "", uaB)
	if loginB.Code != http.StatusOK {
		t.Fatalf("trusted login B status = %d body=%s", loginB.Code, loginB.Body.String())
	}
	trustB := cookieValueFrom(loginB, trustCookie)
	if trustB == "" {
		t.Fatal("no trust cookie issued for device B")
	}

	// Both devices are listed; labels come from the user-agent.
	devices := trustDevicesGet(t, h, session)
	if len(devices) != 2 {
		t.Fatalf("trusted device count = %d, want 2 (devices=%+v)", len(devices), devices)
	}
	labels := map[string]bool{}
	for _, d := range devices {
		if d.ExpiresAt == "" {
			t.Errorf("device %s has no expires_at", d.ID)
		}
		labels[d.Label] = true
	}
	if !labels["Chrome · macOS"] {
		t.Errorf("expected a 'Chrome · macOS' device, got labels %v", labels)
	}
	if !labels["Safari · iPhone"] {
		t.Errorf("expected a 'Safari · iPhone' device, got labels %v", labels)
	}

	// Revoke device A by its opaque id.
	idA := h.Sessions.TrustedDevices().HashFor(trustA)
	if rec := trustDevicesDelete(t, h, session, idA); rec.Code != http.StatusOK {
		t.Fatalf("revoke A status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Immediate invalidation: device A's trust no longer skips TOTP.
	after := loginOnceUA(t, h, email, "password123", "", false, trustA, uaA)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("revoked device A login status = %d, want 401 (challenge); body=%s", after.Code, after.Body.String())
	}
	// Device B still trusted.
	still := loginOnceUA(t, h, email, "password123", "", false, trustB, uaB)
	if still.Code != http.StatusOK {
		t.Fatalf("device B login after revoking A status = %d, want 200; body=%s", still.Code, still.Body.String())
	}

	// The list now holds only device B.
	devices = trustDevicesGet(t, h, session)
	if len(devices) != 1 || devices[0].Label != "Safari · iPhone" {
		t.Fatalf("after revoking A, devices = %+v, want only Safari · iPhone", devices)
	}

	// Revoke-all ("log out everywhere") empties the list and kills B's trust.
	if rec := trustDevicesDelete(t, h, session, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke all status = %d body=%s", rec.Code, rec.Body.String())
	}
	if devices = trustDevicesGet(t, h, session); len(devices) != 0 {
		t.Fatalf("after revoke-all, devices = %+v, want none", devices)
	}
	gone := loginOnceUA(t, h, email, "password123", "", false, trustB, uaB)
	if gone.Code != http.StatusUnauthorized {
		t.Fatalf("device B login after revoke-all status = %d, want 401 (challenge)", gone.Code)
	}
}

// TestTrustedDevicesLegacyEntryRevocable: a trust entry minted without #454
// metadata (the legacy Issue path) still lists and can be revoked.
func TestTrustedDevicesLegacyEntryRevocable(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	email := fmt.Sprintf("trustlegacy-%d@example.test", time.Now().UnixNano())
	_, session := enable2FAAndSession(t, h, email, ip)
	uid := requireUserID(t, h, email)

	// Mint a legacy-style trust token straight from the store (no metadata).
	legacy, err := h.Sessions.TrustedDevices().Issue(context.Background(), uid)
	if err != nil {
		t.Fatalf("legacy issue: %v", err)
	}
	devices := trustDevicesGet(t, h, session)
	if len(devices) != 1 {
		t.Fatalf("legacy device count = %d, want 1 (devices=%+v)", len(devices), devices)
	}
	if devices[0].Label != "" {
		t.Errorf("legacy device label = %q, want empty", devices[0].Label)
	}
	if devices[0].ExpiresAt == "" {
		t.Error("legacy device has no expires_at")
	}
	if rec := trustDevicesDelete(t, h, session, devices[0].ID); rec.Code != http.StatusOK {
		t.Fatalf("revoke legacy status = %d body=%s", rec.Code, rec.Body.String())
	}
	// The legacy cookie no longer maps to the user.
	if got := h.Sessions.TrustedDevices().UserIDFor(context.Background(), legacy); got != "" {
		t.Errorf("legacy token still maps to %q after revoke", got)
	}
	if devices = trustDevicesGet(t, h, session); len(devices) != 0 {
		t.Fatalf("after revoking legacy, devices = %+v, want none", devices)
	}
}

// TestTrustedDevicesCannotRevokeOtherUser: a device id belonging to another
// user is not revocable (RevokeHash only acts on the caller's own trust set).
func TestTrustedDevicesCannotRevokeOtherUser(t *testing.T) {
	ip := testIP(t)
	h, _ := requireIntegration(t)
	emailA := fmt.Sprintf("trusta-%d@example.test", time.Now().UnixNano())
	emailB := fmt.Sprintf("trustb-%d@example.test", time.Now().UnixNano())
	_, sessionB := enable2FAAndSession(t, h, emailB, ip)
	uidA := ""
	{
		registerAndVerify(t, h, emailA, "password123", ip)
		uidA = requireUserID(t, h, emailA)
	}
	victim, err := h.Sessions.TrustedDevices().Issue(context.Background(), uidA)
	if err != nil {
		t.Fatalf("victim issue: %v", err)
	}
	victimID := h.Sessions.TrustedDevices().HashFor(victim)

	// User B attempts to revoke user A's device.
	if rec := trustDevicesDelete(t, h, sessionB, victimID); rec.Code != http.StatusOK {
		t.Fatalf("cross-user revoke status = %d, want 200 (idempotent no-op)", rec.Code)
	}
	if got := h.Sessions.TrustedDevices().UserIDFor(context.Background(), victim); got != uidA {
		t.Fatalf("victim trust was revoked by another user (maps to %q, want %q)", got, uidA)
	}
}

// TestTrustedDevicesRequireAuth: the list/revoke endpoints reject anonymous
// callers.
func TestTrustedDevicesRequireAuth(t *testing.T) {
	h, _ := requireIntegration(t)
	if rec := trustDevicesGetRaw(h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list status = %d, want 401", rec.Code)
	}
	if rec := trustDevicesDelete(t, h, "", "abc"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous revoke status = %d, want 401", rec.Code)
	}
}

// trustDevicesGetRaw is trustDevicesGet without the status assertion.
func trustDevicesGetRaw(h *AuthHandlers, session string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/trust-devices", nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestDeviceLabel covers the user-agent → label mapping (issue #454).
func TestDeviceLabel(t *testing.T) {
	cases := []struct{ ua, want string }{
		{"", ""},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", "Chrome · macOS"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 Edg/120.0", "Edge · Windows"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "Safari · iPhone"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0", "Firefox · Linux"},
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36", "Chrome · Android"},
		{"curl/8.5.0", "curl"},
		{"SomeUnknownAgent/1.0", "SomeUnknownAgent/1.0"},
	}
	for _, c := range cases {
		if got := auth.DeviceLabel(c.ua); got != c.want {
			t.Errorf("DeviceLabel(%q) = %q, want %q", c.ua, got, c.want)
		}
	}
}
