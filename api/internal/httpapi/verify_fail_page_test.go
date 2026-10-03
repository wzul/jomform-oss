// Regression guard for the GET /v1/auth/verify FAILURE response type.
//
// THE DEFECT. The endpoint's own doc says "GET renders a small confirmation
// page; POST returns JSON", and the SUCCESS path honoured it — writeVerifyPage
// renders a self-contained localized page. Every FAILURE path answered
// `writeErr`, i.e. `application/json`, for GET and POST alike. So the ordinary
// outcome of an emailed verification link — a link that is stale past its 24h
// TTL — handed a browser the raw machine envelope:
//
//	$ curl -si 'https://api.jomform.com/v1/auth/verify?token=garbage' | head -3
//	HTTP/2 400
//	content-type: application/json; charset=utf-8
//	{"error":"pautan pengesahan tidak sah atau telah tamat tempoh"}
//
// On a production build that envelope carries Malay prose only, to a user whose
// dashboard language may be any of the five the platform ships. The caller here
// is a HUMAN who clicked a link in their mail client, not an API client — the
// one surface that reaches a person without a page that could translate a code.
//
// THE FIX. verifyFail renders a localized failure page for GET and keeps the
// JSON envelope for every other method. This file pins BOTH halves, plus the
// three properties that make the page actually work in a real browser:
//
//  1. GET answers text/html (not the envelope) for every failure class that is
//     reachable on a normal deployment, and the page carries the localized
//     sentence + the "get a new link" hint for EACH of the 5 locales.
//  2. POST is UNCHANGED: still 400 + the JSON error envelope with its code. The
//     app/API half of the contract is what auth_recovery_test.go:38 drives with
//     a valid token (still 200), and this file drives the failure half.
//  3. The page is longer than 512 bytes. Chrome (and Chromium-derived
//     browsers) REPLACE a 4xx body shorter than that with their own generic
//     error page, which would discard exactly the page this change is for.
//
// Every branch below answers before it touches Postgres or Valkey except the
// unknown-account leg, which drives the renderer directly, so this file needs
// no database.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// verifyFailLangCases are the 5 locales the verify surface renders in (issue
// #206), each with the sentence the failure page must carry in that language.
var verifyFailLangCases = []struct {
	lang string
	want string // the expired/forged sentence — the common real-world case
}{
	{"en", "This verification link is invalid or has expired."},
	{"ms", "Pautan pengesahan ini tidak sah atau telah tamat tempoh."},
	{"zh", "此验证链接无效或已过期。"},
	{"ta", "இந்த சரிபார்ப்பு இணைப்பு தவறானது அல்லது காலாவதியானது."},
	{"ar", "رابط التحقق هذا غير صالح أو منتهي الصلاحية."},
}

// TestVerifyGetFailureRendersLocalizedPage is the card's acceptance criterion:
// a GET with a bad token answers text/html in the caller's language, and the
// page tells the user to get a new link.
//
// PRE-FIX this was RED on the first subtest: the handler called writeErr, so
// Content-Type was application/json and the body was the envelope.
func TestVerifyGetFailureRendersLocalizedPage(t *testing.T) {
	for _, c := range verifyFailLangCases {
		t.Run(c.lang, func(t *testing.T) {
			a := &AuthHandlers{VerifyKey: connectTestKey(), BaseURL: "https://jomform.com"}
			req := httptest.NewRequest(http.MethodGet, "/v1/auth/verify?token=garbage&lang="+c.lang, nil)
			rec := httptest.NewRecorder()
			a.handleVerify(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("Content-Type = %q, want text/html — a browser was handed the "+
					"machine envelope (the defect this guard exists for)", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, c.want) {
				t.Errorf("page for lang %q is missing its localized sentence %q:\n%s", c.lang, c.want, body)
			}
			if !strings.Contains(body, `lang="`+c.lang+`"`) {
				t.Errorf("page for lang %q is missing the html lang attribute:\n%s", c.lang, body)
			}
			// Arabic is the one RTL locale; the dashboard sets dir=rtl for it
			// (web/src/components/i18n.tsx), so the page must too rather than
			// render right-to-left text in an LTR document.
			wantDir := `dir="ltr"`
			if c.lang == "ar" {
				wantDir = `dir="rtl"`
			}
			if !strings.Contains(body, wantDir) {
				t.Errorf("page for lang %q is missing %s:\n%s", c.lang, wantDir, body)
			}
			// The page must tell the user what to DO — a sentence about the
			// link being bad is not actionable on its own.
			if !strings.Contains(body, verifyFailText[c.lang].hint) {
				t.Errorf("page for lang %q is missing the \"get a new link\" hint %q:\n%s",
					c.lang, verifyFailText[c.lang].hint, body)
			}
			// …and offer the way there. The link must be the WEB login route,
			// never the POST-only /api/auth/login endpoint (issue #189).
			if !strings.Contains(body, `href="https://jomform.com/login"`) {
				t.Errorf("page for lang %q does not link to the web login page:\n%s", c.lang, body)
			}
			if strings.Contains(body, "/api/auth/login") {
				t.Errorf("page for lang %q links to the POST-only API endpoint:\n%s", c.lang, body)
			}
		})
	}
}

// TestVerifyGetFailurePageExceedsBrowserThreshold pins property 3. A 4xx HTML
// body under 512 bytes is DISCARDED by Chrome/Chromium in favour of its own
// error page (webhint's no-friendly-error-pages hint: 512 bytes for 400, 404,
// 500 …; 256 for 403, 405, 410) — the user would see the browser's error, which
// is the failure this change removes. The success page has always been long
// enough by accident; this is the assertion that keeps the failure page so on
// purpose, in every locale (the CJK/Tamil/Arabic sentences differ in UTF-8
// length from the English one).
func TestVerifyGetFailurePageExceedsBrowserThreshold(t *testing.T) {
	const chromeMinErrorBody = 512
	for _, lang := range []string{"en", "ms", "zh", "ta", "ar"} {
		t.Run(lang, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeVerifyFailPage(rec, lang, "https://jomform.com/login", verifyFailExpired)
			if n := rec.Body.Len(); n <= chromeMinErrorBody {
				t.Errorf("failure page for lang %q is %d bytes, must exceed %d or a "+
					"browser replaces it with its own error page:\n%s",
					lang, n, chromeMinErrorBody, rec.Body.String())
			}
		})
	}
}

// TestVerifyGetFailureSaysWhichClassItIs pins the per-class sentence: the three
// failure classes the card names — expired/forged, unknown account, and a URL
// with no token at all — must not collapse onto one generic sentence. They are
// distinct things to a human (a stale inbox link vs. a mangled URL).
func TestVerifyGetFailureSaysWhichClassItIs(t *testing.T) {
	cases := []struct {
		name  string
		class verifyFailClass
		want  string
	}{
		{"expired/forged", verifyFailExpired, verifyFailText["en"].expired},
		{"unknown account", verifyFailUnknown, verifyFailText["en"].unknown},
		{"no token", verifyFailMissing, verifyFailText["en"].missing},
	}
	seen := map[string]string{}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeVerifyFailPage(rec, "en", "/login", c.class)
		body := rec.Body.String()
		if !strings.Contains(body, c.want) {
			t.Errorf("%s: page missing %q:\n%s", c.name, c.want, body)
		}
		if prev, dup := seen[c.want]; dup {
			t.Errorf("%s and %s render the SAME sentence %q — the classes are "+
				"distinguishable to the caller and must read differently", prev, c.name, c.want)
		}
		seen[c.want] = c.name
	}
}

// TestVerifyGetFailureUnknownLangDefaultsToEnglish mirrors
// TestWriteVerifyPageUnknownLangDefaultsToEnglish for the failure page: an
// unsupported locale must not render an empty page or a raw map key.
func TestVerifyGetFailureUnknownLangDefaultsToEnglish(t *testing.T) {
	rec := httptest.NewRecorder()
	writeVerifyFailPage(rec, "fr", "/login", verifyFailExpired)
	body := rec.Body.String()
	if !strings.Contains(body, verifyFailText["en"].expired) {
		t.Errorf("unknown lang should fall back to English:\n%s", body)
	}
	if !strings.Contains(body, `lang="en"`) {
		t.Errorf("unknown lang should set html lang=en:\n%s", body)
	}
}

// TestVerifyGetNoTokenRendersPage pins the no-token leg THROUGH THE HANDLER: it
// answers before any dependency is dereferenced, so no database is needed.
func TestVerifyGetNoTokenRendersPage(t *testing.T) {
	a := &AuthHandlers{VerifyKey: connectTestKey()}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/verify", nil)
	rec := httptest.NewRecorder()
	a.handleVerify(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), verifyFailText["en"].missing) {
		t.Errorf("no-token page missing its sentence:\n%s", rec.Body.String())
	}
}

// TestWriteVerifyPageArabicIsRTL PINS THE FIX (card t_75ce0ac6).
//
// This test used to DOCUMENT A PRE-EXISTING GAP: `writeVerifyPage` emitted no `dir`
// attribute at all, so an Arabic confirmation rendered RTL prose inside an LTR document.
// It asserted `lang="ar"` and merely LOGGED that `dir` was absent, so the gap was visible
// here rather than discovered by an Arabic user. The gap is now closed — the success page
// carries its own direction, the same as its failure sibling — so the placeholder becomes
// the assertion, and a regression reds instead of logging.
func TestWriteVerifyPageArabicIsRTL(t *testing.T) {
	cases := []struct {
		lang string
		want string
	}{
		{"ar", "rtl"},
		{"en", "ltr"},
		{"ms", "ltr"},
		{"zh", "ltr"},
		{"ta", "ltr"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeVerifyPage(rec, c.lang, "https://jomform.com/login")
			body := rec.Body.String()
			if !strings.Contains(body, `lang="`+c.lang+`"`) {
				t.Fatalf("success page should render the %s locale:\n%s", c.lang, body)
			}
			// The page is the ONE surface reached straight from an email client, so no
			// dashboard JS (`document.documentElement.dir`) has run: the document must
			// carry the direction itself.
			if !strings.Contains(body, `dir="`+c.want+`"`) {
				t.Fatalf("success page for %s must carry dir=%q — an RTL locale renders in an "+
					"LTR document otherwise. body:\n%s", c.lang, c.want, body)
			}
		})
	}
}

// TestVerifyPostKeepsJSONEnvelope is the other half of the card's acceptance:
// POST behaviour and envelope are UNCHANGED. App/API callers complete a
// verification with a fetch, so they must keep getting the machine envelope
// with its code — the page is for the human clicking the emailed link.
func TestVerifyPostKeepsJSONEnvelope(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		key    []byte
		status int
		want   string // the `error` prose
	}{
		{"no token", "", connectTestKey(), http.StatusBadRequest, "token pengesahan diperlukan"},
		{"bad token", "garbage", connectTestKey(), http.StatusBadRequest, "pautan pengesahan tidak sah atau telah tamat tempoh"},
		{"no verify key", "garbage", nil, http.StatusInternalServerError, "internal error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &AuthHandlers{VerifyKey: c.key}
			target := "/v1/auth/verify"
			if c.token != "" {
				target += "?token=" + c.token
			}
			req := httptest.NewRequest(http.MethodPost, target, nil)
			rec := httptest.NewRecorder()
			a.handleVerify(rec, req)

			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d", rec.Code, c.status)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json — POST is the app/API "+
					"surface and must keep the machine envelope", ct)
			}
			// Must still be the {error, code} envelope, not prose in a string.
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("POST body is not the error envelope: %v (body %s)", err, rec.Body.String())
			}
			if body["error"] != c.want {
				t.Errorf("error = %q, want %q", body["error"], c.want)
			}
			if body["code"] == "" {
				t.Errorf("code is EMPTY — a client would fall back to prose")
			}
			if strings.Contains(rec.Body.String(), "<!doctype html>") {
				t.Errorf("POST rendered the HTML page:\n%s", rec.Body.String())
			}
		})
	}
}

// TestVerifyUnconfiguredKeyStaysJSONOnGet pins the ONE failure class that
// deliberately does NOT render a page. With no 32-byte VerifyKey the deployment
// cannot mint a verification link at all, so "request a new link" would promise
// something this server cannot do; the JSON envelope is the honest signal. That
// decision is stated in handleVerify's comment, and this test holds it.
func TestVerifyUnconfiguredKeyStaysJSONOnGet(t *testing.T) {
	a := &AuthHandlers{VerifyKey: nil}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/verify?token=garbage", nil)
	rec := httptest.NewRecorder()
	a.handleVerify(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json (a misconfigured deployment "+
			"cannot promise a new link)", ct)
	}
}
