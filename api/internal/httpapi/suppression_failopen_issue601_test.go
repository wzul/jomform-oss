// Fail-open behaviour of the pre-render suppression checks (issue #601).
//
// Until this change, four auth senders (verify, email 2FA code, 2FA activated,
// magic link) returned the store error from their suppression check, which
// meant a database blip silently stopped that mail. The pre-send choke point at
// (*email.Sender).SendReceipt now fails OPEN and logs
// email_suppression_check_error, and these callers were aligned with it so the
// whole stack behaves the same way: an infrastructure error never blocks mail to
// a healthy address, and a real suppression always does.
//
// The store is not stubbed — AuthHandlers.Suppressions is a concrete
// *dbstore.SuppressionStore, and it is what the senders actually call. The
// failure is produced the way production produces it: a pool that cannot reach
// Postgres (a closed listener), so the query really errors.
package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// unreachableSuppressionStore returns a store whose every lookup fails, by
// pointing a pool at a port nothing listens on.
func unreachableSuppressionStore(t *testing.T) *dbstore.SuppressionStore {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://nobody:nobody@127.0.0.1:1/jomform?sslmode=disable&connect_timeout=2")
	if err != nil {
		t.Fatalf("build unreachable pool: %v", err)
	}
	t.Cleanup(pool.Close)
	// Prove the fixture really is broken before trusting any fail-open result.
	if _, err := dbstore.NewSuppressionStore(pool).IsSuppressed(context.Background(), "x@example.com"); err == nil {
		t.Fatal("fixture is not broken: the unreachable pool answered a query successfully")
	}
	return dbstore.NewSuppressionStore(pool)
}

// captureHTTPAPILogs routes slog into a buffer for one test.
func captureHTTPAPILogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestSendVerifyEmailFailsOpenOnSuppressionStoreError: a store error must not
// fail the send; it is logged and the verification email still goes out.
func TestSendVerifyEmailFailsOpenOnSuppressionStoreError(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	h := &AuthHandlers{
		BaseURL:      "http://localhost:8080",
		VerifyKey:    testVerifyKey,
		VerifyMailer: mailer,
		Suppressions: unreachableSuppressionStore(t),
	}

	if err := h.sendVerifyEmail(context.Background(), "user-1", "reg@example.com", "en"); err != nil {
		t.Fatalf("sendVerifyEmail err = %v, want nil (fail open on a store error)", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a store error must not stop the verification email (fail open)")
	}
	logged := logs.String()
	if !strings.Contains(logged, "email_suppression_check_error") {
		t.Fatalf("store error was not logged as email_suppression_check_error; log:\n%s", logged)
	}
}

// TestSendEmail2FACodeFailsOpenOnSuppressionStoreError: same for the 2FA backup
// code, which is a login-recovery path where a false block is worst.
func TestSendEmail2FACodeFailsOpenOnSuppressionStoreError(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	h := &AuthHandlers{
		VerifyMailer: mailer,
		Suppressions: unreachableSuppressionStore(t),
	}

	if err := h.sendEmail2FACode(context.Background(), "reg@example.com", "123456"); err != nil {
		t.Fatalf("sendEmail2FACode err = %v, want nil (fail open)", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a store error must not stop the 2FA code email")
	}
	if !strings.Contains(logs.String(), "email_suppression_check_error") {
		t.Fatalf("store error was not logged; log:\n%s", logs.String())
	}
}

// TestSendMagicLinkFailsOpenOnSuppressionStoreError: same for the magic link.
func TestSendMagicLinkFailsOpenOnSuppressionStoreError(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	h := &AuthHandlers{
		BaseURL:      "http://localhost:8080",
		VerifyMailer: mailer,
		Suppressions: unreachableSuppressionStore(t),
	}

	if err := h.sendMagicLinkEmail(context.Background(), "reg@example.com", "tok", false, "en"); err != nil {
		t.Fatalf("sendMagicLinkEmail err = %v, want nil (fail open)", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a store error must not stop the magic link email")
	}
	if !strings.Contains(logs.String(), "email_suppression_check_error") {
		t.Fatalf("store error was not logged; log:\n%s", logs.String())
	}
}

// TestSendPasswordResetFailsOpenOnSuppressionStoreError: the reset path, which
// had NO suppression check at all before this change, behaves like the others.
func TestSendPasswordResetFailsOpenOnSuppressionStoreError(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	h := &AuthHandlers{
		BaseURL:      "http://localhost:8080",
		VerifyMailer: mailer,
		Suppressions: unreachableSuppressionStore(t),
	}

	if err := h.sendPasswordResetEmail(context.Background(), "reg@example.com", "tok"); err != nil {
		t.Fatalf("sendPasswordResetEmail err = %v, want nil (fail open)", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a store error must not stop the password reset email")
	}
	if !strings.Contains(logs.String(), "email_suppression_check_error") {
		t.Fatalf("store error was not logged; log:\n%s", logs.String())
	}
}

// TestSendConnectEmailFailsOpenOnSuppressionStoreError: a store error must not
// become a connect-flow failure either.
func TestSendConnectEmailFailsOpenOnSuppressionStoreError(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	h := &AuthHandlers{
		BaseURL:      "http://localhost:8080",
		VerifyMailer: mailer,
		Suppressions: unreachableSuppressionStore(t),
	}

	if err := h.sendConnectEmail(context.Background(), "ok@example.com", "http://x/y"); err != nil {
		t.Fatalf("sendConnectEmail err = %v, want nil (fail open)", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("a store error must not stop the connect email")
	}
	if !strings.Contains(logs.String(), "email_suppression_check_error") {
		t.Fatalf("store error was not logged; log:\n%s", logs.String())
	}
}

// TestSendConnectEmailSuppressedIsNotAnError: a suppressed address used to make
// sendConnectEmail return an error, which surfaced as a broken connect flow. It
// must now be a silent, logged skip — and no mail may be sent.
func TestSendConnectEmailSuppressedIsNotAnError(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	supps := dbstore.NewSuppressionStore(bundleTestPool(t))
	ctx := context.Background()
	addr := "connect-suppressed-" + randSuffix() + "@example.com"
	if _, err := supps.Add(ctx, addr, dbstore.SuppressionReasonHardBounce,
		dbstore.SuppressionSourceBounce, "Permanent/NoEmail", ""); err != nil {
		t.Fatalf("seed suppression: %v", err)
	}

	h := &AuthHandlers{
		BaseURL:      "http://localhost:8080",
		VerifyMailer: mailer,
		Suppressions: supps,
	}
	if err := h.sendConnectEmail(ctx, addr, "http://x/y"); err != nil {
		t.Fatalf("sendConnectEmail err = %v, want nil for a suppressed address", err)
	}
	if mailer.wasCalled() {
		t.Fatal("a suppressed address must not receive a connect email")
	}
	logged := logs.String()
	if !strings.Contains(logged, "connect_email_suppressed_skipped") {
		t.Fatalf("connect skip was not logged; log:\n%s", logged)
	}
	if strings.Contains(logged, "connect_email_suppressed_skipped") &&
		strings.Contains(logged, addr) {
		t.Fatalf("skip log leaked the full address; log:\n%s", logged)
	}
}

// TestSendPasswordResetSuppressedIsASilentSkip: a suppressed address gets no
// reset mail and no error — the enumeration-safe response must not change.
func TestSendPasswordResetSuppressedIsASilentSkip(t *testing.T) {
	logs := captureHTTPAPILogs(t)
	mailer := &recorderMailer{}
	supps := dbstore.NewSuppressionStore(bundleTestPool(t))
	ctx := context.Background()
	addr := "reset-suppressed-" + randSuffix() + "@example.com"
	if _, err := supps.Add(ctx, addr, dbstore.SuppressionReasonHardBounce,
		dbstore.SuppressionSourceBounce, "Permanent/NoEmail", ""); err != nil {
		t.Fatalf("seed suppression: %v", err)
	}

	h := &AuthHandlers{
		BaseURL:      "http://localhost:8080",
		VerifyMailer: mailer,
		Suppressions: supps,
	}
	if err := h.sendPasswordResetEmail(ctx, addr, "tok"); err != nil {
		t.Fatalf("sendPasswordResetEmail err = %v, want nil for a suppressed address", err)
	}
	if mailer.wasCalled() {
		t.Fatal("a suppressed address must not receive a password reset email")
	}
	if !strings.Contains(logs.String(), "pwreset_email_suppressed_skipped") {
		t.Fatalf("reset skip was not logged; log:\n%s", logs.String())
	}
}
