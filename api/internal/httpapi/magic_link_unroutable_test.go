package httpapi

// Unroutable-recipient refusal on the magic-link request (card t_641d62d5).
//
// A prod deploy verification asked for a magic link to
// nonexistent-t_c161c7d2@example.com. The address cannot receive mail —
// example.com publishes a null MX (RFC 7505) — so SES recorded a HARD BOUNCE,
// auto-suppressed the address, and mailed the bounce notification to the owner.
// The fix refuses such a recipient before the mailer is called; these tests
// prove (a) the send never happens, (b) the response is still the generic 200
// in every case, and (c) the refusal leaves no address in the log.
//
// The three requirements are asserted with three different oracles:
//   - a mailer that PANICS if called — the strongest proof a send did not
//     happen (a mock that merely records a call would pass a bug that reached
//     a different sender);
//   - byte-identical response bodies across existing / nonexistent / refused;
//   - the captured slog output.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/email"
)

// addressMailer is a VerifyMailer that records every recipient it is asked to
// send to, so a test can assert both "a send happened" and "no send went to
// this address".
type addressMailer struct {
	mu  sync.Mutex
	tos []string
}

func (m *addressMailer) Send(to, subject, body, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tos = append(m.tos, to)
	return nil
}

func (m *addressMailer) recipients() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.tos...)
}

// panicMailer panics on ANY send. Wiring it is how a test says "reaching the
// mailer is a failure"; a request that returns normally therefore never
// reached it.
type panicMailer struct{}

func (panicMailer) Send(to, subject, body, lang string) error {
	panic("mailer called for recipient " + to)
}

// TestMagicLinkRequestResponseIsIdenticalForRefusedAndAccepted pins the
// anti-enumeration contract: the same status and the same bytes whether the
// address exists, does not exist, or is refused by the unroutable guard.
func TestMagicLinkRequestResponseIsIdenticalForRefusedAndAccepted(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mailer := &addressMailer{}
	h.VerifyMailer = mailer
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// One verified account on a domain that CAN receive mail, and one on a
	// reserved domain. The second is the "refused but existing" case: without
	// the guard it would be mailed, and the bounce would be real.
	routableExisting := "unroutable-ok-" + randSuffix() + "@qa.jomform.com"
	reservedExisting := "unroutable-existing-" + randSuffix() + "@example.com"
	registerAndVerify(t, h, routableExisting, "correct-horse-battery-staple", "")
	registerAndVerify(t, h, reservedExisting, "correct-horse-battery-staple", "")

	// registerAndVerify's own verification mail is not what this test counts.
	before := len(mailer.recipients())

	type req struct {
		name  string
		email string
	}
	reqs := []req{
		{"existing routable", routableExisting},
		{"nonexistent routable", "unroutable-new-" + randSuffix() + "@qa.jomform.com"},
		{"existing reserved", reservedExisting},
		{"nonexistent reserved", "nonexistent-t_c161c7d2@example.com"},
	}

	var bodies []string
	for _, r := range reqs {
		// A fresh address per request: the route allows 2 requests per IP per
		// hour, and every case must actually reach the handler.
		rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": r.email}, uniqueTestIP())
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body=%s, want the generic 200", r.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
			t.Fatalf("%s: body is not the generic ok response: %q", r.name, rec.Body.String())
		}
		bodies = append(bodies, rec.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("response body differs between %q and %q — the guard has become an enumeration oracle:\n %q\n %q",
				reqs[0].name, reqs[i].name, bodies[0], bodies[i])
		}
	}

	sent := mailer.recipients()[before:]
	if len(sent) != 2 {
		t.Fatalf("mailer was asked to send %d magic links (%v), want exactly the 2 routable recipients — "+
			"a reserved domain must never reach the mailer", len(sent), sent)
	}
	for _, to := range sent {
		if _, refused := email.UnroutableDomain(to); refused {
			t.Fatalf("a magic link was sent to %q, a domain that cannot receive mail", to)
		}
	}
}

// TestMagicLinkRefusedRecipientNeverReachesTheMailer is the planted control.
//
// The mailer panics if it is called at all. Both reserved-domain cases (an
// address with no account and a verified account) must return the generic 200
// without the panic firing. The last subtest runs the SAME panicking mailer
// against a routable address and REQUIRES the panic: without it, "no panic"
// could be caused by anything that stopped the request before sending, and the
// test would prove nothing.
func TestMagicLinkRefusedRecipientNeverReachesTheMailer(t *testing.T) {
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	reservedExisting := "unroutable-panic-" + randSuffix() + "@example.com"
	registerAndVerify(t, h, reservedExisting, "correct-horse-battery-staple", "")

	// From here on, any send is a test failure — including the verification
	// mail a registration would emit, which is why the account above was
	// created before swapping the mailer.
	h.VerifyMailer = panicMailer{}

	for _, tc := range []struct {
		name  string
		email string
	}{
		{"nonexistent address on a reserved domain", "nonexistent-t_c161c7d2@example.com"},
		{"existing account on a reserved domain", reservedExisting},
		{"disposable domain", "bounce-" + randSuffix() + "@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": tc.email}, uniqueTestIP())
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s, want the generic 200", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
				t.Fatalf("body is not the generic ok response: %q", rec.Body.String())
			}
		})
	}

	// Control: the same wired mailer, a routable recipient. It MUST panic, or
	// the subtests above are vacuous.
	t.Run("control: routable recipient does reach the mailer", func(t *testing.T) {
		reached := false
		func() {
			defer func() {
				if recover() != nil {
					reached = true
				}
			}()
			postJSON(mux, "/v1/auth/magic-link/request",
				map[string]any{"email": "routable-" + randSuffix() + "@qa.jomform.com"}, uniqueTestIP())
		}()
		if !reached {
			t.Fatal("a routable recipient never reached the panicking mailer — the oracle does not observe sends, " +
				"so the 'no panic' cases above prove nothing")
		}
	})
}

// TestMagicLinkRefusalIsLoggedAsAReasonNotAnAddress pins requirement 4: the
// refusal is visible as a counter-shaped log line carrying the reason, and the
// recipient (or any part of it) is not written to the log.
func TestMagicLinkRefusalIsLoggedAsAReasonNotAnAddress(t *testing.T) {
	logs := capturingLogs(t)
	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := fmt.Sprintf("refused-%s@example.com", randSuffix())
	rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{"email": addr}, uniqueTestIP())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want the generic 200", rec.Code, rec.Body.String())
	}

	logged := logs.String()
	if !strings.Contains(logged, "magiclink_unroutable_domain_skipped") {
		t.Fatalf("the refusal was not logged; log:\n%s", logged)
	}
	if !strings.Contains(logged, "reason=reserved") {
		t.Fatalf("the refusal log does not carry the reason; log:\n%s", logged)
	}
	local := strings.SplitN(addr, "@", 2)[0]
	for _, leak := range []string{addr, local, "example.com"} {
		if strings.Contains(logged, leak) {
			t.Fatalf("the refusal log leaked %q (requirement: counter, not an address); log:\n%s", leak, logged)
		}
	}
}

// TestMagicLinkRefusalCountsAsAnUnroutableSkip proves the refusal increments
// the dedicated counter rather than being invisible: a silent no-op and a
// working guard look identical from the response.
func TestMagicLinkRefusalCountsAsAnUnroutableSkip(t *testing.T) {
	srv := newTestServer()
	before := scrapeUnroutable(t, srv)

	h, _, _ := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	if rec := postJSON(mux, "/v1/auth/magic-link/request",
		map[string]any{"email": "counted-" + randSuffix() + "@example.com"}, uniqueTestIP()); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	after := scrapeUnroutable(t, srv)
	if after <= before {
		t.Fatalf("jomform_ses_unroutable_total did not increase after a refused send (before=%d after=%d)", before, after)
	}
}

// scrapeUnroutable reads the current value of
// jomform_ses_unroutable_total{reason="reserved"} out of the server's /metrics.
func scrapeUnroutable(t *testing.T, srv *Server) int {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", rec.Code)
	}
	total := 0
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, `jomform_ses_unroutable_total{reason="reserved"}`) {
			var v int
			if _, err := fmt.Sscanf(strings.Fields(line)[1], "%d", &v); err != nil {
				t.Fatalf("parse metric line %q: %v", line, err)
			}
			total += v
		}
	}
	return total
}
