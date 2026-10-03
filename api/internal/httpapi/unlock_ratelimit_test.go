package httpapi

// Card t_fc5cecbd — every POST .../unlock route is a PASSWORD ORACLE and must
// carry the same per-IP limiter as the /order routes beside it.
//
// WHY THIS GUARD EXISTS
//
// A private form's unlock takes an attacker-supplied string, runs an argon2id
// verification against the form's real access hash and answers 403 or 200. Left
// unwrapped it is an unlimited online guessing surface, reachable from any
// merchant page that frames the embed. PR #789 — the sibling superseded by the
// embed work — wrapped its embed unlock in middleware.RateLimit; that wrapper
// was dropped when the route was re-registered, and the two canonical unlock
// routes never had one at all. Measured on that branch: 12 wrong passwords ->
// 403 x12 on all three, while the RateLimit-wrapped /order answered 429.
//
// HOW THE THREE PROPERTIES ARE PINNED
//
//  1. THE BITE. A burst of wrong passwords from one address must produce 429s.
//     Removing the wrapper restores 403-for-everything, which this catches.
//  2. THE PRECEDENT. The /order route is the working reference, so the same
//     burst shape must throttle there too — the routes are asserted as one
//     table, so a future change that wraps one and not the others fails here.
//  3. THE LEGITIMATE CASE. A buyer who mistypes twice and then succeeds is not
//     throttled, and one buyer's burst does not touch a DIFFERENT buyer's
//     bucket — the per-IP keying is what makes the cross-site case safe, so it
//     is asserted rather than assumed.
//
// WHY THE BURST IS CONCURRENT, AND WHY "429 AT REQUEST 11" IS NOT THE ASSERTION
//
// middleware.RateLimit consumes the token BEFORE the handler runs, and the
// unlock handler then spends ~134ms in argon2id (measured: BenchmarkArgon2Cost,
// api/internal/auth). Over a bucket that refills at 1 token/sec, that cost is
// larger than the refill, so a SEQUENTIAL loop never drains burst 10: by the
// time request 11 arrives the bucket has a token back and it is served. The
// threshold /order exhibits at request 11 is therefore NOT reproducible
// sequentially on these routes — the handler's own cost refunds the bucket.
//
// A concurrent fan-out is what a brute-force burst actually looks like, and it
// is deterministic: every request's Allow() call lands before any argon2 verify
// completes, so the bucket drains to zero and the requests past the burst are
// refused. Asserting "429 somewhere in a burst of 12" is the property that both
// bites on a missing wrapper and matches the real attack; asserting a
// wall-clock request ordinal would be a flake.
//
// DRAINING FIRST (card t_80c62c1c). Concurrency alone was not enough: the bucket
// is burst 10 at 1 token/sec, so 12 requests only trip it while they land inside
// ~2s, and on a loaded host they do not — 12 concurrent requests through the full
// handler can outlive the refill, every one gets served, and a route that IS
// throttled reads as an unthrottled oracle (`403 x12`, no 429, measured on a host
// at load ~25 while passing in isolation on the same bytes). Spending the
// capacity with a synchronous burst first leaves the bucket empty, so the
// measured burst is over the limit for ~11.5s instead of ~2s and the result no
// longer depends on how fast the box happens to be. That is a fix to WHICH
// condition is measured, not to what is asserted: the "no 400" check below is
// deliberately unchanged.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// unlockPost drives one unlock POST through the full server chain from a given
// address, the way a buyer's browser reaches the API for a form on that host.
// No *testing.T: callers run it from goroutines, where t.Fatalf is not allowed.
func unlockPost(hnd http.Handler, host, path, ip, password string) int {
	body := fmt.Sprintf(`{"password":%q}`, password)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// middleware.ClientIP reads CF-Connecting-IP first (else X-Real-IP, else the
	// socket peer). Setting it is how a test plays "this request came from
	// address X" — production gets the same value from the Cloudflare edge.
	req.Header.Set("CF-Connecting-IP", ip)
	req.Host = host
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	return rec.Code
}

// concurrentUnlockBurst fires n unlock requests from ONE address at once and
// returns the status codes it observed, in any order.
func concurrentUnlockBurst(hnd http.Handler, host, path, ip, password string, n int) []int {
	codes := make([]int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together: the burst must land inside one refill window
			codes[i] = unlockPost(hnd, host, path, ip, password)
		}(i)
	}
	close(start)
	wg.Wait()
	return codes
}

func countCode(codes []int, want int) int {
	n := 0
	for _, c := range codes {
		if c == want {
			n++
		}
	}
	return n
}

func summarise(codes []int) string {
	seen := map[int]int{}
	order := []int{}
	for _, c := range codes {
		if _, ok := seen[c]; !ok {
			order = append(order, c)
		}
		seen[c]++
	}
	var b strings.Builder
	for _, c := range order {
		fmt.Fprintf(&b, "%d x%d  ", c, seen[c])
	}
	return strings.TrimSpace(b.String())
}

// unlockRouteCase is one unlock route plus the host it must be addressed with.
type unlockRouteCase struct {
	name string
	path string
	host string
}

// gateUnlockRouteCases enumerates the private-form gate's unlock routes, using
// the fixture's real slugs, plus the /order route on the canonical scope as the
// rate-limit PRECEDENT every one of them is matched against.
func gateUnlockRouteCases(ws, slug string) []unlockRouteCase {
	host := ws + ".jomform.com"
	return []unlockRouteCase{
		{"canonical /{slug}/unlock", "/" + slug + "/unlock", host},
		{"canonical /f/{slug}/unlock", "/f/" + slug + "/unlock", host},
		{"embed /embed/f/{wsslug}/{slug}/unlock", "/embed/f/" + ws + "/" + slug + "/unlock", host},
		{"precedent /{slug}/order", "/" + slug + "/order", host},
	}
}

// TestEveryUnlockRouteIsRateLimited is the bite: a wrong-password burst from one
// address must be throttled on EVERY unlock route, exactly as the /order route
// beside them already is.
func TestEveryUnlockRouteIsRateLimited(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unlock rate-limit guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)

	for _, tc := range gateUnlockRouteCases(ws, slug) {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// A fresh address per case: the three unlock routes hold separate
			// limiters, so this also keeps the cases independent.
			ip := uniqueTestIP()

			// DRAIN THE BUCKET FIRST (card t_80c62c1c). The limiter is burst 10 with
			// a 1 token/sec refill, so a burst of 12 only trips it while it lands
			// inside ~2s. Under suite load that is not guaranteed: 12 concurrent
			// requests through a loaded handler take longer than the refill, every
			// one is served, and a route that IS throttled reads as an unthrottled
			// password oracle — the reported red was `403 x12` with no 429 on a
			// loaded host, while the same bytes passed in isolation. Spending the
			// capacity first with a synchronous burst leaves the bucket EMPTY, so a
			// single further request is over the limit for ~11.5s instead of ~2s and
			// the measurement stops depending on how fast the box is.
			//
			// The drain itself must be exactly the capacity, or the limiter would
			// answer 429 here and the assertion below could not tell that from the
			// limiter never having engaged.
			drain := concurrentUnlockBurst(hnd, tc.host, tc.path, ip, "definitely-not-the-password",
				middleware.RateLimitBurst)
			if n := countCode(drain, http.StatusTooManyRequests); n > 0 {
				t.Fatalf("POST %s : %d of the first %d requests were already throttled — the drain "+
					"must consume exactly the bucket's capacity and nothing more (codes: %s)",
					tc.path, n, middleware.RateLimitBurst, summarise(drain))
			}

			codes := concurrentUnlockBurst(hnd, tc.host, tc.path, ip, "definitely-not-the-password", 12)

			// With the bucket drained this is the ONLY assertion that decides
			// whether the route is limited, and it must not be loosened to make a
			// loaded host pass: the 400 check below stays exactly as it was, because
			// relaxing it is how a guard starts lying about the limiter. If this
			// ever cannot be measured reliably the fix belongs in the limiter or in
			// the budget, not in the assertion.
			if countCode(codes, http.StatusTooManyRequests) == 0 {
				t.Fatalf("POST %s : a burst of 12 wrong passwords from one address produced NO 429 — "+
					"this route is an unthrottled password oracle. codes: %s",
					tc.path, summarise(codes))
			}
			// It must be the LIMITER answering, not the handler refusing the
			// empty-ish body: a throttled request never reaches argon2, so the
			// refused ones are 429 and the served ones are the wrong-password
			// refusal, never a 400.
			if n := countCode(codes, http.StatusBadRequest); n > 0 {
				t.Fatalf("POST %s : %d of the burst answered 400 — the body should reach the handler "+
					"well-formed, so a 400 means this test is not measuring the limiter. codes: %s",
					tc.path, n, summarise(codes))
			}
			t.Logf("POST %s burst of 12 from %s -> %s", tc.path, ip, summarise(codes))
		})
	}
}

// TestUnlockRateLimitSparesTheFirstBuyers pins the threshold: the limiter's own
// burst (10) must be served, so several buyers behind ONE merchant page — each
// unlocking once — are never throttled. It is the same burst /order allows.
func TestUnlockRateLimitSparesTheFirstBuyers(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unlock rate-limit guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)

	for _, tc := range gateUnlockRouteCases(ws, slug) {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ip := uniqueTestIP()
			// Exactly the burst, fired together: every one must reach the
			// handler. A limiter tighter than the /order precedent would answer
			// 429 here and this catches it.
			codes := concurrentUnlockBurst(hnd, tc.host, tc.path, ip, "definitely-not-the-password", 10)
			if n := countCode(codes, http.StatusTooManyRequests); n > 0 {
				t.Fatalf("POST %s : %d of the first 10 requests from one address were throttled — "+
					"that is tighter than the /order precedent (burst 10) and would throttle real "+
					"buyers sharing an egress. codes: %s", tc.path, n, summarise(codes))
			}
			if n := countCode(codes, http.StatusForbidden); n != 10 {
				t.Fatalf("POST %s : only %d/10 burst requests got the handler's wrong-password "+
					"refusal (403). codes: %s", tc.path, n, summarise(codes))
			}
		})
	}
}

// TestUnlockRateLimitIsKeyedPerBuyer pins the KEY, which is the whole reason
// this wrapper is safe on a route reached from a third-party frame: the bucket
// belongs to the buyer's own address, not to the form and not to the merchant
// page. One buyer exhausting their bucket must leave every other buyer — on the
// same form, framed by the same merchant — untouched.
func TestUnlockRateLimitIsKeyedPerBuyer(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unlock rate-limit guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"
	path := "/embed/f/" + ws + "/" + slug + "/unlock"

	// Buyer A (an attacker, here) spends their whole burst from their address.
	attacker := uniqueTestIP()
	codes := concurrentUnlockBurst(hnd, host, path, attacker, "wrong-password", 16)
	if countCode(codes, http.StatusTooManyRequests) == 0 {
		t.Fatalf("the attacker's own burst was not throttled at all (codes: %s) — nothing to "+
			"discriminate below", summarise(codes))
	}

	// Buyer B, a different address on the SAME form, must be untouched: two
	// buyers on one merchant page share the page, not the bucket.
	buyer := uniqueTestIP()
	for i := 1; i <= 3; i++ {
		if got := unlockPost(hnd, host, path, buyer, gateFixturePassword); got != http.StatusOK {
			t.Fatalf("a second buyer's unlock #%d on the same form returned %d, want 200 — the limiter "+
				"is keyed on something shared (the form or the embedding page) instead of the buyer's "+
				"own address, so one visitor can lock out every other visitor on the page", i, got)
		}
	}
}

// TestUnlockStillWorksAfterAFewMistakes is the card's acceptance criterion for
// the legitimate case, measured rather than argued: a buyer who mistypes twice
// and then enters the right password is unlocked, not throttled.
func TestUnlockStillWorksAfterAFewMistakes(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unlock rate-limit guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)

	for _, tc := range gateUnlockRouteCases(ws, slug) {
		tc := tc
		// /order is not an unlock: it takes a submission, not a password.
		if strings.HasSuffix(tc.path, "/order") {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			ip := uniqueTestIP()
			for i := 0; i < 2; i++ {
				if got := unlockPost(hnd, tc.host, tc.path, ip, "mistyped-"+fmt.Sprint(i)); got != http.StatusForbidden {
					t.Fatalf("mistyped attempt #%d -> %d, want 403 (a wrong password is refused, not "+
						"throttled, at this point in the bucket)", i+1, got)
				}
			}
			if got := unlockPost(hnd, tc.host, tc.path, ip, gateFixturePassword); got != http.StatusOK {
				t.Fatalf("the CORRECT password after two mistypes -> %d, want 200 — the limiter is "+
					"throttling a buyer who is simply fallible", got)
			}
		})
	}
}
