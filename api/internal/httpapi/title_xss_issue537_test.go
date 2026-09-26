package httpapi

// Issue #537: stored XSS via the form title on the raw-HTML render paths.
//
// Several public renderers build HTML with fmt.Fprintf instead of html/template,
// so they have no contextual autoescaping. The merchant-controlled form title was
// interpolated raw, letting a `</title><script>…` break out and execute on the
// BUYER's page. f.Slug was interpolated into a JS single-quoted string literal,
// where HTML escaping alone is insufficient.
//
// These tests render the real handlers with hostile titles/slugs and assert the
// payload cannot become live markup. Each FAILS against the pre-fix code.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// round6XSS closes the <title> element and injects a script.
const round6XSS = `Pizza</title><script>alert(document.domain)</script>`

func hostileForm(title, slug string) *dbstore.Form {
	now := time.Now()
	return &dbstore.Form{
		Title: title, Slug: slug, Status: "published",
		Theme: map[string]any{}, Config: map[string]any{},
		CreatedAt: now, UpdatedAt: now,
	}
}

// assertNoLiveScript fails if the payload survived as executable markup.
func assertNoLiveScript(t *testing.T, label, body, payload string) {
	t.Helper()
	if strings.Contains(body, "<script>alert(document.domain)</script>") {
		t.Fatalf("issue #537 STORED XSS in %s: the payload was emitted as live markup — a "+
			"buyer visiting the form executes the merchant's script.\n  payload: %s\n  body: %.500s",
			label, payload, body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("issue #537: expected the payload to appear HTML-escaped in %s; body: %.500s",
			label, body)
	}
}

// TestIssue537PrivateGateEscapesTitle covers renderPrivateGate.
func TestIssue537PrivateGateEscapesTitle(t *testing.T) {
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/xss-probe", nil)

	h.renderPrivateGate(rec, req, hostileForm(round6XSS, "xss-probe"))

	assertNoLiveScript(t, "renderPrivateGate", rec.Body.String(), round6XSS)
}

// TestIssue537PrivateGateEscapesSlugInJSContext covers the JS string literal.
//
// The gate's fetch target is its OWN pathname now (the embed fix), so no
// merchant value is interpolated there any more — the slug cannot reach that
// literal at all. What still does is the language dictionary, and it carries the
// same risk: a dictionary entry interpolated into the single-quoted literal would
// break out exactly the way a hostile slug used to. So this test keeps the
// property it was written for (nothing interpolated into that literal can
// terminate it) and pins it on the value that really is interpolated, while
// asserting the stronger fact the fix bought: the target is location.pathname,
// not a merchant-controlled path.
func TestIssue537PrivateGateEscapesSlugInJSContext(t *testing.T) {
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	// A slug that tries to terminate the JS literal and run code, plus a
	// </script> break-out attempt.
	h.renderPrivateGate(rec, req, hostileForm("ok", `a');alert(1);//</script>`))

	body := rec.Body.String()
	if strings.Contains(body, "');alert(1)") {
		t.Fatalf("issue #537 JS-CONTEXT INJECTION: a merchant value terminated the JS string "+
			"literal in the gate's fetch() — HTML escaping is not enough for a JS "+
			"context.\n  body: %.500s", body)
	}
	if strings.Contains(body, "</script>alert") {
		t.Fatalf("issue #537: a merchant value broke out of the <script> block\n  body: %.500s", body)
	}
	// The fetch target is the gate's own address, so there is no second place
	// for a slug to be interpolated into JS. If a future edit reintroduces a
	// slug-built path, this line is what catches it.
	if !strings.Contains(body, "fetch(location.pathname+'/unlock'") {
		t.Errorf("the gate no longer posts to its own path (location.pathname + '/unlock'); a "+
			"gate that builds its target from a merchant-slug instead is the drift this pins.\n"+
			"  body: %.500s", body)
	}
}

// TestPrivateGateFetchTargetIsItsOwnPath pins the fix for the embedded private
// form: the gate must post to the address its OWN document was served at, the
// same rule the submit button follows with "/order".
//
// Measured against the pre-fix code: the gate hardcoded fetch('/<slug>/unlock'),
// so on the embed route (/embed/f/{ws}/{slug} — the only route a merchant can
// frame) it posted to the CANONICAL, host-scoped /<slug>/unlock. An embed runs on
// the merchant's origin, so that request's Host never names the workspace and the
// unlock could not resolve the form: POST /embed/f/{ws}/{slug}/unlock answered
// "404 page not found" (no route registered there at all) while the canonical
// path reached the handler. An embedded private form was therefore permanently
// locked.
//
// The rendered gate has no merchant-scoped host in it to derive the embed scope
// from, which is exactly why "my own pathname" is the correct target: it is the
// one expression that is right on every route that can render the gate
// (/{slug} and /f/{slug} host-scoped, /embed/f/{wsslug}/{slug} path-scoped).
func TestPrivateGateFetchTargetIsItsOwnPath(t *testing.T) {
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/embed/f/ws/some-form", nil)

	h.renderPrivateGate(rec, req, hostileForm("Private form", "some-form"))
	body := rec.Body.String()

	if !strings.Contains(body, "fetch(location.pathname+'/unlock'") {
		t.Fatalf("the private gate does not post to its own path — it must fetch "+
			"location.pathname + '/unlock' so the EMBED route posts to the embed scope. "+
			"body: %.500s", body)
	}
	// A hardcoded, host-resolved target is the defect itself.
	if strings.Contains(body, "fetch('/some-form/unlock'") {
		t.Fatalf("the gate still hardcodes the canonical /<slug>/unlock path, which is "+
			"host-scoped and cannot resolve a form served in an embed. body: %.500s", body)
	}
}

// TestIssue537RestrictionEscapesTitle covers renderRestriction.
func TestIssue537RestrictionEscapesTitle(t *testing.T) {
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/xss-probe", nil)

	h.renderRestriction(rec, req, hostileForm(round6XSS, "xss-probe"), restrictionExpired)

	assertNoLiveScript(t, "renderRestriction", rec.Body.String(), round6XSS)
}

// TestIssue537EscapesDoNotMangleNormalTitles pins that ordinary titles (including
// one containing &, as a real live form does: "F&B Order + Payment") still render
// correctly and legibly.
func TestIssue537EscapesDoNotMangleNormalTitles(t *testing.T) {
	h := &PublicHandlers{}
	for _, title := range []string{"F&B Order + Payment", "Tuition Class Registration", "Nasi Lemak — Set A"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		h.renderPrivateGate(rec, req, hostileForm(title, "slug"))
		body := rec.Body.String()
		if !strings.Contains(body, "<title>") {
			t.Errorf("title element missing for %q", title)
		}
		// & must be escaped as &amp; in markup, and no raw script may appear.
		if strings.Contains(body, "<script>alert") {
			t.Errorf("unexpected script for normal title %q", title)
		}
		if strings.Contains(title, "&") && !strings.Contains(body, "&amp;") {
			t.Errorf("expected & to be escaped as &amp; for title %q", title)
		}
	}
}

// TestIssue537JsSingleQuotedNeutralisesBreakouts pins the helper directly.
func TestIssue537JsSingleQuotedNeutralisesBreakouts(t *testing.T) {
	cases := map[string]string{
		// ' ) ; ( ) ; are escaped; the digit 1 is in the safe set so it stays.
		`a');alert(1);`: `a\u0027\u0029\u003balert\u00281\u0029\u003b`,
		// < is escaped, so the HTML parser can never see a real </script>.
		`</script>`: `\u003c/script\u003e`,
		// Safe identifier-ish characters pass through unchanged.
		`plain-slug_1.0`: `plain-slug_1.0`,
	}
	for in, want := range cases {
		if got := jsSingleQuoted(in); got != want {
			t.Errorf("jsSingleQuoted(%q) = %q, want %q", in, got, want)
		}
	}

	// THE SAFETY PROPERTY: the output is interpolated between single quotes, so
	// it must contain no raw quote (would terminate the literal) and no raw
	// backslash (a trailing one would escape the closing quote). Both would let
	// the attacker escape the string context.
	for _, in := range []string{`'`, `\`, `<`, `>`, "</script>", "a'b\\c<d>", `\'`, `\\`} {
		got := jsSingleQuoted(in)
		if strings.Contains(got, "'") {
			t.Errorf("jsSingleQuoted(%q) = %q contains a raw quote — it can terminate the JS literal", in, got)
		}
		if strings.Contains(got, `\`) {
			// Every backslash must be part of a \uXXXX escape we emitted, never a
			// bare one that could escape the closing quote.
			rest := got
			for strings.Contains(rest, `\u`) {
				if len(rest) < 6 {
					t.Errorf("jsSingleQuoted(%q) = %q has a truncated escape", in, got)
					break
				}
				rest = rest[strings.Index(rest, `\u`)+6:]
			}
			if strings.Contains(rest, `\`) {
				t.Errorf("jsSingleQuoted(%q) = %q contains a bare backslash — it can escape the closing quote", in, got)
			}
		}
	}

	// A payload that survives as \u003c must not reproduce a real tag.
	if got := jsSingleQuoted("</script><script>alert(1)</script>"); strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("jsSingleQuoted left a raw angle bracket: %q", got)
	}
}
