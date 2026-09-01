// Issue #39 regression tests: 2FA setup must be pending-only until a valid
// code is confirmed; login must challenge TOTP only when totp_enabled=true
// (never on secret presence); handleMe/report must read the enabled column.
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) and Valkey.
package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// totpTestEnv returns a live Valkey + Postgres-backed AuthHandlers, skipping
// the test when the integration environment is unavailable.
func totpTestEnv(t *testing.T) (*AuthHandlers, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping TOTP integration test")
	}
	pool := bundleTestPool(t)
	sessions, err := auth.NewSessions("valkey://localhost:6379/0")
	if err != nil {
		t.Skipf("valkey not available for TOTP test: %v", err)
	}
	a := &AuthHandlers{
		Sessions:     sessions,
		Users:        dbstore.NewUserStore(pool),
		Accounts:     dbstore.NewAccountStore(pool),
		BaseURL:      "http://localhost",
		VerifyKey:    testVerifyKey,
		LoginLimiter: auth.NewLoginLimiter(sessions.Client()),
	}
	// Flush pending keys so tests start clean (only this project's keyspace).
	return a, pool, ""
}

// totpServe routes one request through the auth mux and returns the recorder.
func totpServe(t *testing.T, a *AuthHandlers, method, path string, body any, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// totpRegister creates a fresh email-verified user and returns the user ID
// plus a live session cookie. Since issue #42, registration no longer starts a
// session: the account must be email-verified, then a login establishes the
// session. This helper reproduces exactly that (register → /verify → login).
func totpRegister(t *testing.T, a *AuthHandlers) (userID, sessionCookie string) {
	t.Helper()
	email := "totp-it-" + strings.ReplaceAll(randSuffix(), "-", "") + "@example.test"
	rec := totpServe(t, a, "POST", "/v1/auth/register", map[string]string{
		"email": email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Confirm the email via the signed /verify link (issue #42).
	tok, err := auth.SignVerifyToken(a.VerifyKey, email)
	if err != nil {
		t.Fatalf("sign verify token: %v", err)
	}
	if rec := totpServe(t, a, "GET", "/v1/auth/verify?token="+tok, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Establish the session with a real login (this is what sets jf_session).
	rec = totpServe(t, a, "POST", "/v1/auth/login", map[string]string{
		"email": email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login after verify = %d body=%s", rec.Code, rec.Body.String())
	}
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "jf_session" {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("login did not set session cookie")
	}
	return me.ID, cookie
}

// randSuffix returns a short random hex suffix (non-crypto; test-only).
func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func totpCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}
	return code
}

func Test2FASetupIsPendingAndLoginNotLocked(t *testing.T) {
	a, pool, base := totpTestEnv(t)
	ctx := context.Background()
	userID, cookie := totpRegister(t, a)

	// Setup: secret + URL returned, DB secret column must stay empty and
	// totp_enabled must stay false.
	rec := totpServe(t, a, "POST", base+"/v1/auth/2fa/setup", map[string]any{}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	secret, url := out["secret"], out["otpauth_url"]
	if secret == "" || !strings.Contains(url, "otpauth://totp/") {
		t.Fatalf("setup response wrong: %v", out)
	}
	var dbSecret string
	var dbEnabled bool
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(totp_secret,''), totp_enabled FROM users WHERE id=$1`, userID).
		Scan(&dbSecret, &dbEnabled); err != nil {
		t.Fatalf("read db: %v", err)
	}
	if dbSecret != "" {
		t.Fatalf("setup wrote active secret to DB; want pending-only: %q", dbSecret)
	}
	if dbEnabled {
		t.Fatal("setup enabled 2FA prematurely")
	}

	// Login with password only must SUCCEED (mirrors wan@jomform.com — secret
	// present but enabled=false must NOT lock the user out).
	rec = totpServe(t, a, "POST", base+"/v1/auth/login", map[string]string{
		"email": meByID(t, a, userID).Email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("password-only login after abandoned setup = %d, want 200 (no lockout): %s", rec.Code, rec.Body.String())
	}
}

func Test2FAEnableValidatesPendingThenActivates(t *testing.T) {
	a, pool, base := totpTestEnv(t)
	ctx := context.Background()
	userID, cookie := totpRegister(t, a)

	rec := totpServe(t, a, "POST", base+"/v1/auth/2fa/setup", map[string]any{}, cookie)
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	secret := out["secret"]

	// Wrong code → 401, still not enabled.
	rec = totpServe(t, a, "POST", base+"/v1/auth/2fa/enable", map[string]string{"code": "000000"}, cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("enable with wrong code = %d, want 401", rec.Code)
	}

	// Valid code → enabled.
	code := totpCode(t, secret)
	rec = totpServe(t, a, "POST", base+"/v1/auth/2fa/enable", map[string]string{"code": code}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable valid code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var dbSecret string
	var dbEnabled bool
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(totp_secret,''), totp_enabled FROM users WHERE id=$1`, userID).
		Scan(&dbSecret, &dbEnabled); err != nil {
		t.Fatalf("read db: %v", err)
	}
	if dbSecret != secret || !dbEnabled {
		t.Fatalf("after enable: secret=%q enabled=%v, want secret present + enabled", dbSecret, dbEnabled)
	}

	// me reports totp_enabled = true.
	rec = totpServe(t, a, "GET", base+"/v1/auth/me", nil, cookie)
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if !me.TOTPEnabled {
		t.Fatal("me reports totp_enabled=false after enable")
	}

	// Pending key is consumed: enabling again without a fresh setup must fail.
	rec = totpServe(t, a, "POST", base+"/v1/auth/2fa/enable", map[string]string{"code": code}, cookie)
	if rec.Code == http.StatusOK {
		t.Fatal("enable succeeded with no pending secret (should have been consumed)")
	}

	// Now login demands TOTP.
	email := meByID(t, a, userID).Email
	rec = totpServe(t, a, "POST", base+"/v1/auth/login", map[string]string{
		"email": email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("password-only login when enabled = %d, want 401 totp_required", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["next_step"] != "totp_required" {
		t.Fatalf("expected next_step=totp_required, got %v", body)
	}
	rec = totpServe(t, a, "POST", base+"/v1/auth/login", map[string]string{
		"email": email, "password": "correct-horse-battery-staple", "totp_code": code,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login with code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func Test2FAEnableRequiresPendingSetup(t *testing.T) {
	a, _, base := totpTestEnv(t)
	_, cookie := totpRegister(t, a)
	rec := totpServe(t, a, "POST", base+"/v1/auth/2fa/enable", map[string]string{"code": "123456"}, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("enable without pending secret = %d, want 400", rec.Code)
	}
}

// Test2FALoginIgnoresSecretWithoutEnabled reproduces the prod lockout state:
// a user with a stale totp_secret but totp_enabled=false (abandoned setup)
// must log in with password alone. This is the wan@jomform.com scenario from
// issue #39 where the old secret-presence gate armed an unproducible TOTP
// challenge.
func Test2FALoginIgnoresSecretWithoutEnabled(t *testing.T) {
	a, pool, base := totpTestEnv(t)
	ctx := context.Background()
	userID, _ := totpRegister(t, a)

	// Simulate legacy split-brain: secret present, totp_enabled false.
	secret, _, err := auth.GenerateTOTPSecret("legacy@example.test")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if err := a.Users.SetTOTPSecret(ctx, userID, secret); err != nil {
		t.Fatalf("set legacy secret: %v", err)
	}
	if err := pool.QueryRow(ctx, `UPDATE users SET totp_enabled=$2 WHERE id=$1 RETURNING totp_enabled`,
		userID, false).Scan(new(bool)); err != nil {
		t.Fatalf("ensure enabled=false: %v", err)
	}

	// me must report not-enabled despite secret presence.
	rec := totpServe(t, a, "GET", base+"/v1/auth/me", nil, "jf_session="+sessionForUser(t, a, userID))
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.TOTPEnabled {
		t.Fatal("me reports totp_enabled=true for secret-without-enabled (split-brain)")
	}

	// Password-only login must succeed — no totp_required lockout.
	u := meByID(t, a, userID)
	rec = totpServe(t, a, "POST", base+"/v1/auth/login", map[string]string{
		"email": u.Email, "password": "correct-horse-battery-staple",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("password-only login with enabled=false = %d, want 200 (no lockout): %s", rec.Code, rec.Body.String())
	}
}

// sessionForUser mints a real session for the user via Valkey.
func sessionForUser(t *testing.T, a *AuthHandlers, userID string) string {
	t.Helper()
	tok, err := a.Sessions.Create(context.Background(), userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return tok
}

func Test2FADisableClearsBoth(t *testing.T) {
	a, pool, base := totpTestEnv(t)
	ctx := context.Background()
	userID, cookie := totpRegister(t, a)

	// Fully enable.
	rec := totpServe(t, a, "POST", base+"/v1/auth/2fa/setup", map[string]any{}, cookie)
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	code := totpCode(t, out["secret"])
	rec = totpServe(t, a, "POST", base+"/v1/auth/2fa/enable", map[string]string{"code": code}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable failed: %s", rec.Body.String())
	}
	_ = a.Users.SetTOTPSecret(ctx, userID, out["secret"]) // ensure a secret exists

	// Disable with correct password clears both columns.
	rec = totpServe(t, a, "POST", base+"/v1/auth/2fa/disable", map[string]string{
		"password": "correct-horse-battery-staple",
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var dbSecret string
	var dbEnabled bool
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(totp_secret,''), totp_enabled FROM users WHERE id=$1`, userID).
		Scan(&dbSecret, &dbEnabled); err != nil {
		t.Fatalf("read db: %v", err)
	}
	if dbSecret != "" || dbEnabled {
		t.Fatalf("after disable: secret=%q enabled=%v, want both cleared", dbSecret, dbEnabled)
	}
}

// meByID returns the user's current email (helpers can't access the struct
// directly across the register boundary, so re-read via me/email lookup).
func meByID(t *testing.T, a *AuthHandlers, userID string) *dbstore.User {
	t.Helper()
	u, err := a.Users.ByID(context.Background(), userID)
	if err != nil || u == nil {
		t.Fatalf("load user: %v", err)
	}
	return u
}
