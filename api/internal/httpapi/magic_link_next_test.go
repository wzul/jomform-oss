// Card t_5e39dc9f — the magic link must carry the page the visitor was bounced
// from, so the DEFAULT (passwordless) sign-in returns them to it instead of the
// dashboard root.
//
// THE DEFECT (measured on prod 2026-10-11, fresh anonymous browser):
//
//	/profile → 307 /login?next=https%3A%2F%2Fjomform.com%2Fprofile
//	→ the magic request body carried no `next`
//	→ the received email's link was https://jomform.com/magic-link?token=… with
//	  no `next=` → clicking it landed on /workspace-picker.
//
// The two arms below are the two halves of the server's part in the fix:
// `safeMagicLinkNext` decides the SHAPE of a value that may be carried, and the
// emailed link must really carry it in both the login and the sign-up variants —
// with the token semantics (single-use, account created on click) untouched.
//
// The ORIGIN half of the rule cannot be tested here: this handler does not know
// which spelling of the app the visitor was on, and the browser applies the
// authoritative check (web/src/lib/nextTarget.ts, pinned by
// web/src/lib/nextTarget.test.ts and web/e2e/magic-link-next.spec.ts).
package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSafeMagicLinkNext is the shape rule, exercised directly: what travels on
// the emailed link, and what is refused so the link stays exactly as before.
func TestSafeMagicLinkNext(t *testing.T) {
	oversized := "https://jomform.com/settings?" + strings.Repeat("x", nextMaxLen)
	if len(oversized) <= nextMaxLen {
		t.Fatalf("fixture is wrong: %d bytes must exceed the %d cap", len(oversized), nextMaxLen)
	}

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"the page the visitor asked for", "https://jomform.com/profile", "https://jomform.com/profile"},
		{
			"a value carrying its own query survives the trip",
			"https://jomform.com/products/42/edit?tab=images",
			"https://jomform.com/products/42/edit?tab=images",
		},
		{
			"the app's other spelling (the emailed link is built on the API base URL)",
			"https://www.jomform.com/profile",
			"https://www.jomform.com/profile",
		},
		{
			"an http origin is a shape this rule allows (dev/e2e serve http)",
			"http://127.0.0.1:3100/profile",
			"http://127.0.0.1:3100/profile",
		},
		// Everything below must be dropped, so the link degrades to its pre-fix
		// bytes rather than carrying a value nobody can honour.
		{"relative", "/profile", ""},
		{"protocol-relative", "//evil.example/profile", ""},
		{"schemeless", "evil.example/profile", ""},
		{"javascript", "javascript:alert(1)", ""},
		{"other scheme", "ftp://jomform.com/x", ""},
		{"empty", "", ""},
		{"oversized", oversized, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := safeMagicLinkNext(c.raw); got != c.want {
				t.Fatalf("safeMagicLinkNext(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestMagicLinkLoginEmailCarriesTheReturnTarget: an existing verified account
// asks for a link from a guarded page; the LINK variant of the email must carry
// the return target, and the token must still be single-use.
func TestMagicLinkLoginEmailCarriesTheReturnTarget(t *testing.T) {
	ip := testIP(t)
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := "next-login-" + randSuffix() + "@example.test"
	registerAndVerify(t, h, email, "correct-horse-battery-staple", "")

	target := "https://jomform.com/products/42/edit?tab=images"
	rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{
		"email": email, "next": target,
	}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d body=%s (want the generic 200)", rec.Code, rec.Body.String())
	}

	body := mailer.lastBody()
	if want := "&next=" + url.QueryEscape(target); !strings.Contains(body, want) {
		t.Fatalf("the emailed link does not carry the return target %q — body=%q", want, body)
	}
	// The token must still be the first query parameter and still parse: the
	// link's own reader (and every existing consumer) finds it by position, not
	// by position in a map.
	mt := linkRe.FindStringSubmatch(body)
	if len(mt) < 2 {
		t.Fatalf("no magic link token in email body: %q", body)
	}

	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": mt[1]}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link verify status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": mt[1]}, ip)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reused magic-link token status = %d, want 400 (single-use must be unchanged)", rec.Code)
	}
}

// TestMagicLinkSignupEmailCarriesTheReturnTarget: a NEW address gets the
// sign-up variant of the link, which must carry the target too — and clicking it
// must still create the account and start the session.
func TestMagicLinkSignupEmailCarriesTheReturnTarget(t *testing.T) {
	ip := testIP(t)
	h, users, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	email := "next-signup-" + randSuffix() + "@example.test"
	target := "https://jomform.com/profile"

	rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{
		"email": email, "next": target,
	}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link request status = %d body=%s (want the generic 200)", rec.Code, rec.Body.String())
	}

	body := mailer.lastBody()
	if want := "&next=" + url.QueryEscape(target); !strings.Contains(body, want) {
		t.Fatalf("the signup link does not carry the return target %q — body=%q", want, body)
	}
	mt := linkRe.FindStringSubmatch(body)
	if len(mt) < 2 {
		t.Fatalf("no magic link token in signup email body: %q", body)
	}

	rec = postJSON(mux, "/v1/auth/magic-link/verify", map[string]any{"token": mt[1]}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup magic-link verify status = %d body=%s", rec.Code, rec.Body.String())
	}
	user, err := users.ByEmail(context.Background(), email)
	if err != nil || user == nil {
		t.Fatalf("account not created after the signup magic link: err=%v", err)
	}
	if !user.Verified {
		t.Fatal("signup magic-link account should be verified (email confirmed by click)")
	}
}

// TestMagicLinkEmailDropsAnUnusableReturnTarget: a value the browser would
// refuse must NOT be embedded in the email. The link then carries exactly what
// it carried before this change — no new error surface, no silently-ignored
// value looking deliberate.
//
// Two requests, because the route's per-IP limiter allows two in a burst.
func TestMagicLinkEmailDropsAnUnusableReturnTarget(t *testing.T) {
	ip := testIP(t)
	h, _, mailer := authTestFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	for _, raw := range []string{"javascript:alert(1)", "//evil.example/profile"} {
		email := "next-drop-" + randSuffix() + "@example.test"
		rec := postJSON(mux, "/v1/auth/magic-link/request", map[string]any{
			"email": email, "next": raw,
		}, ip)
		if rec.Code != http.StatusOK {
			t.Fatalf("magic-link request (%s) status = %d body=%s (want the generic 200)", raw, rec.Code, rec.Body.String())
		}
		body := mailer.lastBody()
		if !strings.Contains(body, "magic-link?token=") {
			t.Fatalf("no link was emailed for %s: body=%q", raw, body)
		}
		if strings.Contains(body, "next=") {
			t.Fatalf("the link carried a `next` the browser must refuse (%s): body=%q", raw, body)
		}
	}
}
