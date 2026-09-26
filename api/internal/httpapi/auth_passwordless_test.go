// Issue #303: password is optional at signup. Integration tests that verify
// a blank password creates an account with a random UUID password that is
// argon2id-hashed (never stored plaintext), while explicitly provided weak
// passwords are still rejected. Skipped unless JOMFORM_TEST_DATABASE_URL +
// JOMFORM_TEST_VALKEY_URL are set (mirrors auth_hardening_test.go).
package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
)

// TestRegisterBlankPasswordAssignsHashedUUID: signup without a password must
// succeed (201) and store an argon2id hash of a random UUID — never the empty
// string and never plaintext — so the account still has a credential and can
// later set a real password.
func TestRegisterBlankPasswordAssignsHashedUUID(t *testing.T) {
	ip := testIP(t)
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping passwordless signup integration test")
	}
	h, users, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("pwless-%d@example.test", time.Now().UnixNano())
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "",
	}, ip)
	if rec.Code != http.StatusCreated {
		t.Fatalf("blank-password register status = %d (body %s)", rec.Code, rec.Body.String())
	}

	u, err := users.ByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("fetch created user: %v", err)
	}
	if u.PasswordHash == "" {
		t.Fatal("expected a non-empty password hash for passwordless signup")
	}
	// It must be a valid argon2id PHC hash, never a plaintext value.
	ok, vErr := auth.VerifyPassword("", u.PasswordHash)
	if vErr == nil && ok {
		t.Fatal("empty plaintext should not verify against the assigned hash")
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		t.Fatalf("password hash is not argon2id-encoded: %q", u.PasswordHash)
	}
}

// TestRegisterStillRejectsShortPassword: providing a too-short explicit
// password is still rejected at signup (backward-compatible with #306-era
// validation).
func TestRegisterStillRejectsShortPassword(t *testing.T) {
	ip := testIP(t)
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping passwordless signup integration test")
	}
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := fmt.Sprintf("shortpw-%d@example.test", time.Now().UnixNano())
	rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": email, "password": "short",
	}, ip)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short-password register status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
