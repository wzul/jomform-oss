// Regression guard for the shared "token diperlukan" literal (issue #6177
// follow-up, card t_74c46aaa).
//
// THE DEFECT. FOUR handlers answered the SAME wire literal for "this request
// carried no token":
//
//	social.go:425          POST /v1/auth/social/connect
//	verify.go:99           GET/POST /v1/auth/verify
//	auth_ux.go:132         POST /v1/auth/reset-password
//	auth_recovery.go:411   POST /v1/auth/magic-link/verify
//
// errCodeByMessage deliberately did not claim that literal, because one code
// keyed on it would necessarily have been applied to all four endpoints at
// once — changing four wire contracts to say one thing. The observable result
// was that all four answered the indistinguishable class code
// `request.badRequest`, so any client (or dashboard page) had no way to tell
// which link the caller had to reopen.
//
// THE FIX. Each endpoint now writes its OWN literal and so carries its own
// code. This file pins both halves, driven through the REAL handlers:
//
//  1. Each of the four answers its own code, never the class code.
//  2. The four codes are DISTINCT — a lookup table that collapsed two surfaces
//     onto one code would satisfy (1) while restoring the original problem.
//  3. The prose is still on the wire (API-key/curl/MCP callers consume it).
//  4. The old shared literal is claimed by NO code — that is what proves the
//     split is complete rather than the table merely growing entries beside it.
//
// Every branch below answers before it touches Postgres or Valkey, so this test
// needs no database. The handlers are driven directly (not through a mux) so a
// routing change cannot make it pass for the wrong reason.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tokenMissingCase is one of the four surfaces under test.
type tokenMissingCase struct {
	name string
	code string
	// call drives the real handler with a request that carries no token and
	// returns the recorder. It must not need a database.
	call func(t *testing.T) *httptest.ResponseRecorder
}

func tokenMissingCases() []tokenMissingCase {
	return []tokenMissingCase{
		{
			// socialConnect refuses anything but a 32-byte key before it reads
			// the body, so the key is supplied — with a short key this case
			// would pass on a 500 and prove nothing.
			name: "POST /v1/auth/social/connect",
			code: CodeConnectTokenMissing,
			call: func(t *testing.T) *httptest.ResponseRecorder {
				t.Helper()
				a := &AuthHandlers{VerifyKey: connectTestKey()}
				req := httptest.NewRequest(http.MethodPost, "/v1/auth/social/connect", strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				a.socialConnect(rec, req)
				return rec
			},
		},
		{
			name: "GET /v1/auth/verify",
			code: CodeVerifyTokenMissing,
			call: func(t *testing.T) *httptest.ResponseRecorder {
				t.Helper()
				a := &AuthHandlers{VerifyKey: connectTestKey()}
				req := httptest.NewRequest(http.MethodGet, "/v1/auth/verify", nil)
				rec := httptest.NewRecorder()
				a.handleVerify(rec, req)
				return rec
			},
		},
		{
			name: "POST /v1/auth/reset-password",
			code: CodeResetTokenMissing,
			call: func(t *testing.T) *httptest.ResponseRecorder {
				t.Helper()
				a := &AuthHandlers{}
				req := httptest.NewRequest(http.MethodPost, "/v1/auth/reset-password",
					strings.NewReader(`{"token":"","new_password":"correct-horse-battery-staple"}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				a.handleResetPassword(rec, req)
				return rec
			},
		},
		{
			name: "POST /v1/auth/magic-link/verify",
			code: CodeMagicTokenMissing,
			call: func(t *testing.T) *httptest.ResponseRecorder {
				t.Helper()
				a := &AuthHandlers{}
				req := httptest.NewRequest(http.MethodPost, "/v1/auth/magic-link/verify",
					strings.NewReader(`{"token":""}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				a.handleMagicLinkVerify(rec, req)
				return rec
			},
		},
	}
}

// TestTokenMissingIsFourDistinctCodes is the whole card in one assertion: every
// surface that requires a token names its OWN code, so a client can tell them
// apart. PRE-FIX all four answered `request.badRequest` and this test was RED on
// the first subtest.
func TestTokenMissingIsFourDistinctCodes(t *testing.T) {
	seen := map[string]string{}
	for _, c := range tokenMissingCases() {
		rec := c.call(t)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d (body %s)", c.name, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body is not the error envelope: %v (body %s)", c.name, err, rec.Body.String())
		}
		if got := body["code"]; got != c.code {
			t.Errorf("%s: code = %q, want %q — a class code here is what made all four "+
				"surfaces indistinguishable", c.name, got, c.code)
		}
		// The prose must survive the split: it is the wire message for API-key,
		// curl and MCP callers, which cannot look up an i18n key.
		if body["error"] == "" {
			t.Errorf("%s: the wire `error` prose was dropped — API-key callers consume it", c.name)
		}
		if prev, dup := seen[body["code"]]; dup {
			t.Errorf("%s and %s share the code %q — the point of the split is that a "+
				"client can tell the surfaces apart", prev, c.name, body["code"])
		}
		seen[body["code"]] = c.name
	}
	if len(seen) != len(tokenMissingCases()) {
		t.Fatalf("expected %d distinct codes, got %d (%v)", len(tokenMissingCases()), len(seen), seen)
	}
}

// TestTokenMissingLiteralsArePerSurface pins the LITERAL half. The code table is
// keyed on the exact wire string, so the codes above can only be reached if each
// handler really writes its own wording. Asserting the literals directly is also
// what makes the "shared literal is gone" claim checkable rather than inferred
// from a code value.
func TestTokenMissingLiteralsArePerSurface(t *testing.T) {
	want := []struct {
		name string
		msg  string
	}{
		{"POST /v1/auth/social/connect", "token penghubung diperlukan"},
		{"GET /v1/auth/verify", "token pengesahan diperlukan"},
		{"POST /v1/auth/reset-password", "token set semula diperlukan"},
		{"POST /v1/auth/magic-link/verify", "token log masuk diperlukan"},
	}
	cases := tokenMissingCases()
	if len(want) != len(cases) {
		t.Fatalf("test table drifted from tokenMissingCases(): %d vs %d", len(want), len(cases))
	}
	for i, w := range want {
		var body map[string]string
		if err := json.Unmarshal(cases[i].call(t).Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
		if body["error"] != w.msg {
			t.Errorf("%s: error = %q, want %q", w.name, body["error"], w.msg)
		}
		// And the literal is what resolves the code, not a coincidence.
		if got := ErrCodeFor(w.msg); got != cases[i].code {
			t.Errorf("ErrCodeFor(%q) = %q, want %q", w.msg, got, cases[i].code)
		}
	}
}

// TestOldSharedTokenLiteralIsClaimedByNoCode is the completion check: the ONE
// literal that used to be written by all four handlers must resolve to no
// specific code. If a table entry for it ever comes back, one code is again
// being applied to four surfaces — the defect, restored.
func TestOldSharedTokenLiteralIsClaimedByNoCode(t *testing.T) {
	if got := ErrCodeFor("token diperlukan"); got != "" {
		t.Errorf(`ErrCodeFor("token diperlukan") = %q, want "" — that literal was shared `+
			`by four handlers, so any code claiming it is applied to all four surfaces at `+
			`once. Each handler must write its own wording instead.`, got)
	}
}
