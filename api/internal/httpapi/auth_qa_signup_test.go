package httpapi

// The QA test-account MARKING channel (card t_6ae7f586).
//
// Requirement: no public path may let an arbitrary visitor mark their own
// account as a disposable test account — otherwise `purge_test_accounts` becomes
// a way to delete a stranger's data. The marker is therefore reachable ONLY by
// presenting a server-held token in the X-JomForm-QA-Signup header, and an
// installation that never sets the token cannot mark anything at all.
//
// Each case below drives the REAL register handler against a real database and
// reads the persisted marker back, so a regression that moved the decision into
// the request body, or that honoured an unconfigured token, would red here.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// registerWithQAHeader posts a registration carrying the given QA header value
// ("" = no header) and returns the recorder.
func registerWithQAHeader(t *testing.T, h *AuthHandlers, email, headerValue string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body, err := json.Marshal(map[string]any{"email": email, "password": "correct-horse-battery"})
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if headerValue != "" {
		req.Header.Set(qaSignupHeader, headerValue)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// isTestColumn reads users.is_test back from the database — the persisted
// marker, not a value the handler returned.
func isTestColumn(t *testing.T, p *pgxpool.Pool, email string) (exists, isTest bool) {
	t.Helper()
	err := p.QueryRow(t.Context(), `SELECT is_test FROM users WHERE email = $1`, email).Scan(&isTest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false
		}
		t.Fatalf("read is_test for %s: %v", email, err)
	}
	return true, isTest
}

// uniqueTestEmail builds a fresh address; the exact shape does not matter,
// because the marker (not the shape) is what the purge uses.
func uniqueTestEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

func TestRegisterMarksATestAccountOnlyWithTheServerToken(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping QA signup marking integration test")
	}
	h, _, _ := authTestFixture(t)
	h.QASignupToken = "qa-secret-token"
	pool := bundleTestPool(t)

	// 1. No header: an ordinary visitor's signup is NOT marked.
	plain := uniqueTestEmail("jfqa-plain")
	if rec := registerWithQAHeader(t, h, plain, ""); rec.Code != http.StatusCreated {
		t.Fatalf("plain register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if exists, marked := isTestColumn(t, pool, plain); !exists || marked {
		t.Fatalf("a registration with no QA header wrote exists=%v is_test=%v — an unprivileged caller must "+
			"never be able to mark its own account", exists, marked)
	}

	// 2. WRONG token: still not marked (the channel is a secret, not a flag).
	wrong := uniqueTestEmail("jfqa-wrong")
	if rec := registerWithQAHeader(t, h, wrong, "not-the-token"); rec.Code != http.StatusCreated {
		t.Fatalf("wrong-token register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if exists, marked := isTestColumn(t, pool, wrong); !exists || marked {
		t.Fatalf("a registration with the WRONG QA token wrote exists=%v is_test=%v — the header must be "+
			"authenticated, not merely present", exists, marked)
	}

	// 3. Correct token: the account IS marked, which is what makes it findable
	// by the purge afterwards.
	markedEmail := uniqueTestEmail("jfqa-marked")
	if rec := registerWithQAHeader(t, h, markedEmail, "qa-secret-token"); rec.Code != http.StatusCreated {
		t.Fatalf("QA register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if exists, marked := isTestColumn(t, pool, markedEmail); !exists || !marked {
		t.Fatalf("a registration with the correct QA token wrote exists=%v is_test=%v — the marking channel "+
			"did not persist the marker, so the purge could never find the account", exists, marked)
	}
}

func TestRegisterIgnoresTheQAHeaderWhenNoTokenIsConfigured(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping QA signup marking integration test")
	}
	h, _, _ := authTestFixture(t)
	h.QASignupToken = "" // production's default: the channel is closed
	pool := bundleTestPool(t)

	email := uniqueTestEmail("jfqa-notoken")
	// The caller guesses the header name and sends something; with no configured
	// token there is nothing to match, so nothing is marked.
	if rec := registerWithQAHeader(t, h, email, "anything"); rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if exists, marked := isTestColumn(t, pool, email); !exists || marked {
		t.Fatalf("with no QA token configured the header still marked the account (exists=%v is_test=%v) — "+
			"the channel must fail closed", exists, marked)
	}
}

func TestRegisterRefusesToMarkAProtectedAddress(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping QA signup marking integration test")
	}
	h, _, _ := authTestFixture(t)
	h.QASignupToken = "qa-secret-token"
	pool := bundleTestPool(t)

	protected := dbstore.ProtectedAccountEmails[0]
	existsBefore, _ := isTestColumn(t, pool, protected)

	rec := registerWithQAHeader(t, h, protected, "qa-secret-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("registering the protected address through the QA channel: status = %d, want 400 "+
			"(body %s) — the ONE reused test login must never be marked disposable", rec.Code, rec.Body.String())
	}
	existsAfter, markedAfter := isTestColumn(t, pool, protected)
	if existsAfter && markedAfter {
		t.Fatalf("the protected address was marked is_test=true — a purge would delete the designated login")
	}
	if existsBefore != existsAfter {
		t.Fatalf("the refused registration changed the protected row's existence (before=%v after=%v)",
			existsBefore, existsAfter)
	}
}
