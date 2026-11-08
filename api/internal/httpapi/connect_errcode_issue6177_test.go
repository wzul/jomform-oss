// Issue #6177 follow-up: the /connect failure envelope must carry a machine
// code, and the CODE must be one the dashboard can turn into a sentence.
//
// WHY THIS TEST EXISTS. The dashboard /connect page used to render the raw
// failure body (`await r.text()`), so a user saw
// {"code":"request.badRequest","error":"pautan penghubung tidak sah"} — the
// whole JSON envelope, in the server's Malay prose, even on lang=en.
//
// The page fix (web/src/app/connect/page.tsx) routes the body through
// apiErr(body, t("connect.fail"), t). That only produces a useful sentence when
// the code the handler answers is a code the web dictionary maps — so the two
// halves are pinned together HERE, at the handler, rather than trusting the two
// sides to drift in step:
//
//   - socialConnect answers connect.invalid / connect.expired for a bad or
//     expired link, not the shared request.badRequest class code;
//   - connect.invalid and connect.expired are in web/src/lib/apiErr.ts
//     API_ERROR_KEYS, and the web-side guard (apiErr.test.ts) fails the build
//     when a server code has no dictionary entry.
//
// The token is signed with the REAL auth.SignConnectToken, so this exercises
// the production verify path rather than a stub. No database is needed: every
// branch below answers before the user lookup.
package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
)

// connectTestKey is a valid 32-byte VerifyKey. socialConnect refuses to run at
// all with anything else, so a short key would make every assertion below pass
// for the wrong reason (a 500 internal error).
func connectTestKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

// postConnectToken drives the REAL socialConnect handler with a JSON body.
func postConnectToken(t *testing.T, key []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	a := &AuthHandlers{VerifyKey: key, BaseURL: "http://localhost:8080"}
	raw, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/social/connect", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.socialConnect(rec, req)
	return rec
}

// connectEnvelope decodes the failure body and fails on a non-JSON one, since
// "the dashboard prints the raw body" is exactly what a non-JSON answer would
// recreate.
func connectEnvelope(t *testing.T, rec *httptest.ResponseRecorder, what string) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: envelope is not JSON: %v (body %s)", what, err, rec.Body.String())
	}
	return body
}

// TestConnectErrorCodesAreTranslatable pins the reported case: the bad link Wan
// saw answered {"code":"request.badRequest", ...}, a class code the dashboard
// deliberately does not translate — so the page could only fall back to its own
// generic sentence. It must now answer a SPECIFIC code.
func TestConnectErrorCodesAreTranslatable(t *testing.T) {
	rec := postConnectToken(t, connectTestKey(), "bogus-token-123")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad token: status = %d, want %d (body %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	body := connectEnvelope(t, rec, "bad token")
	if body["code"] != CodeConnectInvalid {
		t.Errorf("bad token: code = %q, want %q — a class code (request.badRequest) "+
			"leaves the /connect page with only its generic sentence", body["code"], CodeConnectInvalid)
	}
	// The prose stays on the wire for API-key/curl callers; only the dashboard
	// stops rendering it. Removing it would break live callers.
	if body["error"] == "" {
		t.Error("bad token: the wire `error` prose was dropped — API-key callers consume it")
	}
}

// TestConnectExpiredTokenHasItsOwnCode proves the two refusals are
// distinguishable: "the link was forged" and "the link timed out" need
// different sentences, and a single shared code could not express that.
//
// The expired token is minted in-test with the same scheme connecttoken.go
// uses. That duplication is SELF-CHECKING: if the scheme ever changes, the HMAC
// no longer matches, the handler answers CodeConnectInvalid, and the assertion
// below fails loudly instead of quietly testing nothing.
func TestConnectExpiredTokenHasItsOwnCode(t *testing.T) {
	key := connectTestKey()
	tok := expiredConnectToken(key, "google", "uid-1", "expired@example.test")

	rec := postConnectToken(t, key, tok)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expired token: status = %d, want %d (body %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	body := connectEnvelope(t, rec, "expired token")
	if body["code"] != CodeConnectExpired {
		t.Errorf("expired token: code = %q, want %q (a forged-link code here means the "+
			"in-test signing scheme no longer matches auth/connecttoken.go, or the "+
			"expiry branch stopped being reported separately)", body["code"], CodeConnectExpired)
	}
}

// TestConnectMissingTokenAnswersItsOwnCode covers the pre-verify branch.
//
// This test used to assert the CLASS code on purpose, because "token diperlukan"
// was one literal shared with the email-verify and recovery handlers, so a code
// naming it would have changed those endpoints' wire contract too. That is no
// longer true: each surface now answers its own literal (issue #6177 follow-up),
// so this branch must name its OWN code — a class code here would put the
// /connect page back on its generic sentence.
func TestConnectMissingTokenAnswersItsOwnCode(t *testing.T) {
	rec := postConnectToken(t, connectTestKey(), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing token: status = %d, want %d (body %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	body := connectEnvelope(t, rec, "missing token")
	if body["code"] == "" {
		t.Error("missing token: code is EMPTY — the dashboard would fall back to the server prose")
	}
	if body["code"] != CodeConnectTokenMissing {
		t.Errorf("missing token: code = %q, want %q — the old shared literal %q is gone, "+
			"so this branch is no longer forced onto a class code", body["code"],
			CodeConnectTokenMissing, "token diperlukan")
	}
	// The prose is still on the wire for API-key/curl callers.
	if body["error"] == "" {
		t.Error("missing token: the wire `error` prose was dropped — API-key callers consume it")
	}
}

// TestConnectSpecificCodesReachErrCodeFor checks the lookup table directly, so
// a code that is declared but unreachable (the shape the web guard's dead-key
// check also hunts) fails here at the table level.
func TestConnectSpecificCodesReachErrCodeFor(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"invalid connect link", CodeConnectInvalid},
		{"the connect link has expired", CodeConnectExpired},
		{"an account for this address no longer exists", CodeConnectNoAccount},
		{"identity is already linked to another account", CodeConnectOtherLinked},
		{"connect token is required", CodeConnectTokenMissing},
		// The wire `error` string, not the class code: ErrCodeFor answers "" for a
		// message with no specific entry and the caller applies the HTTP class, so
		// "" here is the proof that this literal was NOT claimed by a connect code.
		{"account not found", ""},
	}
	for _, c := range cases {
		if got := ErrCodeFor(c.msg); got != c.want {
			t.Errorf("ErrCodeFor(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
	// The pre-verify branch must not borrow the "account is gone" code: they are
	// different failures and the /connect page renders a different sentence.
	rec := postConnectToken(t, connectTestKey(), "")
	if body := connectEnvelope(t, rec, "pre-verify"); body["code"] == CodeConnectNoAccount {
		t.Error(`the missing-token branch resolved to connect.accountMissing — the ` +
			"lookup table must not collapse two different refusals onto one code")
	}
}

// TestConnectCodesAreMappedInTheWebDictionary reads the dashboard mapping and
// requires every connect code this handler can answer to be present. The web
// guard checks the same property from its side by parsing errcode.go; this
// direction exists so a code added here without a dashboard entry fails in the
// package that OWNS the code, where the person adding it is looking.
func TestConnectCodesAreMappedInTheWebDictionary(t *testing.T) {
	// From api/ (go test's cwd), the repo root is one level up — but this file
	// lives at api/internal/httpapi, and readRepoFile's two-level join is
	// calibrated for a package at api/internal/*. Resolve the repo root from the
	// package's own location instead of guessing a depth, so the check cannot
	// silently skip when the file is there.
	path := filepath.Join("..", "..", "..", "web", "src", "lib", "apiErr.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v (this check cannot pass vacuously)", path, err)
	}
	for _, code := range []string{CodeConnectInvalid, CodeConnectExpired, CodeConnectNoAccount, CodeConnectOtherLinked, CodeConnectTokenMissing} {
		if !strings.Contains(string(raw), `"`+code+`"`) {
			t.Errorf("%q is answered by socialConnect but is not in web/src/lib/apiErr.ts "+
				"API_ERROR_KEYS — the /connect page would render its generic sentence "+
				"instead of the reason", code)
		}
	}
}

// expiredConnectToken mints a correctly-signed token whose expiry is in the
// past. auth.SignConnectToken cannot express that (it always uses now+TTL, and
// the clock is package-private), so the token is assembled with the same scheme
// connecttoken.go documents: "<provider>|<uid>|<email>|<expiryUnix>|<hmac>",
// base64 RawURL, HMAC-SHA256 over the payload.
func expiredConnectToken(key []byte, provider, uid, email string) string {
	const expiry = 1 // 1970 — unambiguously past

	// The format assumption is checked against the REAL verifier before the
	// expiry is patched, so a scheme change fails this helper immediately.
	if valid, err := auth.SignConnectToken(key, provider, uid, email); err == nil {
		if _, _, _, verr := auth.VerifyConnectToken(key, valid); verr != nil {
			panic(fmt.Sprintf("in-test token scheme no longer matches auth/connecttoken.go: %v", verr))
		}
	} else {
		panic(fmt.Sprintf("SignConnectToken: %v", err))
	}

	payload := provider + "|" + uid + "|" + email + "|" + strconv.Itoa(expiry)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(payload))
	mac := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + mac))
}
