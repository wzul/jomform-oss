// Issue #56 SES reputation hardening integration tests.
//
// These exercise the suppression list, verification breaker, and SNS
// notification endpoint against real Postgres + Valkey. Skipped unless
// JOMFORM_TEST_DATABASE_URL and JOMFORM_TEST_VALKEY_URL are set (CI doesn't set
// them, mirroring auth_hardening_test.go).
package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
)

// sesTestFixture wires a full AuthHandlers over real Postgres + Valkey plus the
// suppression store, breaker, and MX checker (issue #56).
func sesTestFixture(t *testing.T) (*AuthHandlers, *dbstore.UserStore, *dbstore.SuppressionStore, *email.Breaker, *recorderMailer) {
	t.Helper()
	users := authUsers(t)
	sess := authTestValkey(t)
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	pool, err := db.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, testLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	supps := dbstore.NewSuppressionStore(pool)
	// Clear any breaker/MX/jf:* counters from a previous run so ratio assertions
	// in these tests are self-contained (they share one test Valkey DB).
	keys, err := sess.Client().Keys(context.Background(), "jf:bnc:*").Result()
	if err == nil && len(keys) > 0 {
		_ = sess.Client().Del(context.Background(), keys...).Err()
	}
	mxKeys, _ := sess.Client().Keys(context.Background(), "jf:mx:*").Result()
	if len(mxKeys) > 0 {
		_ = sess.Client().Del(context.Background(), mxKeys...).Err()
	}

	mailer := &recorderMailer{}
	breaker := email.NewBreaker(sess.Client(), 0, 0) // defaults 20 / 0.05
	h := &AuthHandlers{
		Sessions:     sess,
		Users:        users,
		BaseURL:      "http://localhost:8080",
		LoginLimiter: auth.NewLoginLimiter(sess.Client()),
		VerifyKey:    testVerifyKey,
		VerifyMailer: mailer,
		Suppressions: supps,
		Breaker:      breaker,
		MXChecker:    email.NewMXChecker(sess.Client()),
	}
	return h, users, supps, breaker, mailer
}

// TestSESSuppressedEmailNotResent: once an address is suppressed (a permanent
// bounce was recorded), a subsequent registration with that address still
// creates no verification email — the send is skipped.
func TestSESSuppressedEmailNotResent(t *testing.T) {
	h, users, supps, _, mailer := sesTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	emailAddr := "suppressed-" + randSuffix() + "@example.com"
	// Simulate an SNS permanent-bounce feedback landing in the suppression list.
	if _, err := supps.Upsert(context.Background(), emailAddr, "permanent bounce (Permanent)", dbstore.SuppressionSourceBounce); err != nil {
		t.Fatalf("upsert suppression: %v", err)
	}
	// Also flag any user row unverified (as the endpoint does).
	_ = users.MarkUnverified(context.Background(), emailAddr)

	if rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": emailAddr, "password": "password123",
	}, "10.20.0.1"); rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if mailer.wasCalled() {
		t.Fatal("verification email was sent to a suppressed address — must be skipped")
	}
}

// TestSESBreakerGatesRegister: when the verification breaker trips (bounce
// ratio > 5% with >= 20 sends), registration returns 503 instead of sending.
func TestSESBreakerGatesRegister(t *testing.T) {
	h, _, _, breaker, _ := sesTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	ctx := context.Background()
	// 25 sends + 5 bounces = 20% > 5% (>= 20 sends) → tripped.
	for i := 0; i < 25; i++ {
		if err := breaker.RecordSend(ctx); err != nil {
			t.Fatalf("record send: %v", err)
		}
	}
	if err := breaker.RecordBounceCount(ctx, 5); err != nil {
		t.Fatalf("record bounce: %v", err)
	}

	if rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": "breaker-" + randSuffix() + "@example.com", "password": "password123",
	}, "10.20.0.2"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("register under breaker status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestSESBreakerDoesNotGateReceipts is a contract test for the breaker wiring:
// the breaker has no receipt path, and receipts consult suppression only. Here
// we assert the breaker alone does not block a (clean) address's registration
// when a *single* bounce is within a low-bounce sample — i.e. small samples and
// low ratios don't trip.
func TestSESBreakerDoesNotGateLowBounce(t *testing.T) {
	h, _, _, breaker, _ := sesTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	ctx := context.Background()
	for i := 0; i < 25; i++ {
		_ = breaker.RecordSend(ctx)
	}
	_ = breaker.RecordBounceCount(ctx, 1) // 1/25 = 4% < 5%

	if rec := postJSON(mux, "/v1/auth/register", map[string]any{
		"email": "clean-" + randSuffix() + "@example.com", "password": "password123",
	}, "10.20.0.3"); rec.Code != http.StatusCreated {
		t.Fatalf("register under low-bounce status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestSESNotificationsRejectUnverifiedWrites: an unauthorised (unsigned / bad
// signature) POST to the SNS endpoint returns 401 and records NO suppression.
func TestSESNotificationsRejectUnverifiedWrites(t *testing.T) {
	h, _, supps, _, _ := sesTestFixture(t)
	// Wire the full server (which registers the internal endpoint only when
	// the suppression store + SNS-notif env flag are set).
	srv := NewServer(issue56TestConfig(), testLogger(), &Deps{
		Suppressions: supps,
		Users:        h.Users,
		Breaker:      h.Breaker,
		MXChecker:    h.MXChecker,
	})
	route := srv.Handler()

	// An attacker POSTs a spoofed bounce notification with no SNS signature.
	body := []byte(`{"Type":"Notification","MessageId":"m","TopicArn":"arn:aws:sns:us-east-1:1:t",
		"Message":"{\"eventType\":\"Bounce\",\"bounce\":{\"bounceType\":\"Permanent\",\"bouncedRecipients\":[{\"emailAddress\":\"victim@example.com\"}]},\"mail\":{\"destination\":[\"victim@example.com\"]}}",
		"Timestamp":"2026-01-01T00:00:00Z","SignatureVersion":"1"}`)
	req := httptest.NewRequest("POST", "/v1/internal/ses/notifications", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	route.ServeHTTP(rec, req)

	// No signature → 401, and nothing written to the suppression list.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unverified SNS POST status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	if sup, err := supps.IsSuppressed(context.Background(), "victim@example.com"); err == nil && sup {
		t.Fatal("unauthenticated SNS request must NOT write a suppression")
	}
}

// issue56TestConfig returns a dev config with SNS notifications enabled so the
// /v1/internal/ses/notifications route is registered by NewServer.
func issue56TestConfig() *config.Config {
	return &config.Config{
		Port:            "0",
		DatabaseURL:     "postgres://localhost/jomform_test",
		AppBaseURL:      "http://localhost:8080",
		Env:             "dev",
		SNSNotifEnabled: true,
	}
}
