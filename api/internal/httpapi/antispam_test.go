package httpapi

// Issue #244: deterministic anti-spam layers on public form submissions.
// These unit tests exercise the pure helpers (honeypot, time-based check,
// per-form rate limit, email validation) without a database. The integration
// paths (renderer injection + submit rejection) are covered by the existing
// response/variation submit tests against a real Postgres.

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// newSubmitInput builds a minimal payment-mode submit body with the given
// fields map (so tests can inject honeypot/timestamp values).
func newSubmitInput(fields map[string]string) *submitInput {
	in := &submitInput{
		Client: struct {
			FullName        string `json:"full_name"`
			Email           string `json:"email"`
			Phone           string `json:"phone"`
			StreetAddress   string `json:"street_address"`
			City            string `json:"city"`
			State           string `json:"state"`
			ZipCode         string `json:"zip_code"`
			Country         string `json:"country"`
			LegalName       string `json:"legal_name"`
			RegistrationNum string `json:"registration_number"`
			TaxNumber       string `json:"tax_number"`
		}{Email: "buyer@example.com"},
		Items:         []submitItem{{ProductID: "p1", Quantity: 1}},
		PaymentMethod: "chip",
		Fields:        fields,
	}
	return in
}

func TestAntiSpamHoneypotRejectsFilled(t *testing.T) {
	h := &PublicHandlers{}
	// Empty honeypot = human, not spam.
	if h.antiSpamCheckHoneypot(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{antiSpamHoneypotField: ""})) {
		t.Fatalf("empty honeypot should not be flagged as spam")
	}
	// Filled honeypot = bot.
	if !h.antiSpamCheckHoneypot(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{antiSpamHoneypotField: "http://spam.example"})) {
		t.Fatalf("filled honeypot should be flagged as spam")
	}
	// Whitespace-only honeypot = not spam.
	if h.antiSpamCheckHoneypot(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{antiSpamHoneypotField: "   "})) {
		t.Fatalf("whitespace honeypot should not be flagged as spam")
	}
}

func TestAntiSpamTimeRejectsTooFast(t *testing.T) {
	h := &PublicHandlers{}
	// A submit with no render timestamp = nothing to measure → skip (not spam).
	// API clients and legacy callers may omit it; they are not necessarily bots.
	if h.antiSpamCheckTime(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{})) {
		t.Fatalf("missing render timestamp should not be flagged as spam")
	}
	// A submit arriving immediately after render (< 3s) = bot.
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if !h.antiSpamCheckTime(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{antiSpamRenderField: now})) {
		t.Fatalf("too-fast submit should be flagged as spam")
	}
	// A submit arriving well after render (>= 3s) = human.
	old := strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	if h.antiSpamCheckTime(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{antiSpamRenderField: old})) {
		t.Fatalf("slow submit should not be flagged as spam")
	}
	// A malformed timestamp = nothing to measure → skip (not spam).
	if h.antiSpamCheckTime(httptest.NewRequest("POST", "/", nil), newSubmitInput(map[string]string{antiSpamRenderField: "not-a-number"})) {
		t.Fatalf("malformed render timestamp should not be flagged as spam")
	}
}

func TestAntiSpamRateLimitBlocksAfterLimit(t *testing.T) {
	// 15/form/IP/hour: burst 15, then refill 15/hour.
	h := &PublicHandlers{FormLimiter: middleware.NewRateLimiter(15.0/3600.0, 15)}
	req := httptest.NewRequest("POST", "/", nil)
	// The test's own address (testIP), never a literal: the limiter keys per
	// IP, and a hand-written address is one no run can reclaim.
	req.RemoteAddr = testIP(t) + ":1234"
	form1 := &dbstore.Form{ID: "form-1"}
	form2 := &dbstore.Form{ID: "form-2"}
	// First 15 are allowed.
	for i := 0; i < 15; i++ {
		if h.antiSpamCheckRateLimit(req, form1) {
			t.Fatalf("submit %d should be allowed (under limit)", i+1)
		}
	}
	// The 16th is over the limit.
	if !h.antiSpamCheckRateLimit(req, form1) {
		t.Fatalf("16th submit should be over the limit")
	}
	// A different form is not throttled by this form's counter.
	if h.antiSpamCheckRateLimit(req, form2) {
		t.Fatalf("different form should not be throttled")
	}
	// A nil limiter (bare-mode tests) never throttles.
	h2 := &PublicHandlers{}
	if h2.antiSpamCheckRateLimit(req, form1) {
		t.Fatalf("nil limiter should never throttle")
	}
}

func TestAntiSpamEmailValidation(t *testing.T) {
	h := &PublicHandlers{}
	// Format-only (no MX checker wired): valid email passes, invalid fails.
	if h.antiSpamCheckEmail(httptest.NewRequest("POST", "/", nil), "buyer@example.com") {
		t.Fatalf("valid email should pass format check")
	}
	if !h.antiSpamCheckEmail(httptest.NewRequest("POST", "/", nil), "not-an-email") {
		t.Fatalf("invalid email should fail format check")
	}
	if !h.antiSpamCheckEmail(httptest.NewRequest("POST", "/", nil), "") {
		t.Fatalf("empty email should fail format check")
	}
}
