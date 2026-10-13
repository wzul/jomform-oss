// Regression guard for the shared "token diperlukan" literal (issue #6177
// follow-up, card t_74c46aaa).
//
// THE DEFECT. FOUR handlers answered the SAME wire literal for "this request
// carried no token":
//
//	social.go        POST /v1/auth/social/connect
//	verify.go        POST /v1/auth/verify
//	auth_ux.go       POST /v1/auth/reset-password
//	auth_recovery.go POST /v1/auth/magic-link/verify
//
// The line each of them writes its OWN literal on is deliberately NOT written
// here. This header used to carry a typed line number for every one of those
// four handlers, and all four were stale — they named, in this order, a
// `json:"token"` field, an `internal error`, a `NewPassword` field and a
// comment about the localized failure page, with one of them off by 48 lines.
// Nothing asserted those numbers and a comment cannot re-check itself, so
// nothing ever red — the same class t_9ad7676b fixed in
// typed_nil_mailer_guard_test.go.
//
// THE FIX, for the pins as for the wire. Each site is named by the LITERAL the
// handler writes (the msg constants below), its line is resolved from the real
// source when a failure message is built, and a site the resolver cannot find
// exactly once fails BY NAME rather than quoting a number that may point
// somewhere else. This file uses the resolver that fix already added
// (derivedLineAnchor, derived_line_anchor_test.go) instead of a second one, and
// all four anchors sit in the package's pin list, so
// TestDerivedLineAnchorTracksTheSource re-resolves them on every run and fails
// if a literal is no longer written on exactly one line of the file named here.
//
// errCodeByMessage deliberately did not claim the shared literal, because one
// code keyed on it would necessarily have been applied to all four endpoints at
// once — changing four wire contracts to say one thing. The observable result
// was that all four answered the indistinguishable class code
// `request.badRequest`, so any client (or dashboard page) had no way to tell
// which link the caller had to reopen.
//
// THE WIRE FIX. Each endpoint now writes its OWN literal and so carries its own
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

// The wording each handler writes when the request carries no token — ONE
// constant per surface, because two things must agree on the exact string: the
// case that asserts what reached the wire, and the anchor that derives the line
// it is written on. Spelling it twice is how the pins in this file's header
// drifted in the first place.
const (
	msgConnectTokenMissing = "token penghubung diperlukan"
	msgVerifyTokenMissing  = "token pengesahan diperlukan"
	msgResetTokenMissing   = "token set semula diperlukan"
	msgMagicTokenMissing   = "token log masuk diperlukan"
)

// The four sites, named by the literal each handler writes so that a failure
// message can quote their lines without any of them being typed. Each is scoped
// to the handler that writes it (derivedLineAnchor.Within) — that is the site a
// reader has to open, and in auth_recovery.go the same file holds other
// missing-token-ish wording that a file-wide search would have to disambiguate.
var (
	anchorConnectTokenMissing = derivedLineAnchor{
		File:    "social.go",
		Display: "social.go",
		Name:    "the missing-token answer for POST /v1/auth/social/connect (`token penghubung diperlukan`)",
		Any:     []string{msgConnectTokenMissing},
		Within:  "func (a *AuthHandlers) socialConnect(",
	}
	anchorVerifyTokenMissing = derivedLineAnchor{
		File:    "verify.go",
		Display: "verify.go",
		Name:    "the missing-token answer for POST /v1/auth/verify (`token pengesahan diperlukan`)",
		Any:     []string{msgVerifyTokenMissing},
		Within:  "func (a *AuthHandlers) handleVerify(",
	}
	anchorResetTokenMissing = derivedLineAnchor{
		File:    "auth_ux.go",
		Display: "auth_ux.go",
		Name:    "the missing-token answer for POST /v1/auth/reset-password (`token set semula diperlukan`)",
		Any:     []string{msgResetTokenMissing},
		Within:  "func (a *AuthHandlers) handleResetPassword(",
	}
	anchorMagicTokenMissing = derivedLineAnchor{
		File:    "auth_recovery.go",
		Display: "auth_recovery.go",
		Name:    "the missing-token answer for POST /v1/auth/magic-link/verify (`token log masuk diperlukan`)",
		Any:     []string{msgMagicTokenMissing},
		Within:  "func (a *AuthHandlers) handleMagicLinkVerify(",
	}
)

// tokenMissingCase is one of the four surfaces under test.
type tokenMissingCase struct {
	name string
	code string
	// msg is the wording this surface must put on the wire when the request
	// carries no token. It is also what identifies the pin: pin's anchor is
	// named by this same constant.
	msg string
	// pin locates the production line that writes msg, so a message about a
	// literal that is wrong or missing can say WHERE the one this surface is
	// supposed to write lives — derived at test time, never typed.
	pin derivedLineAnchor
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
			msg:  msgConnectTokenMissing,
			pin:  anchorConnectTokenMissing,
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
			// POST (not GET): the machine envelope is the APP/API surface.
			// A GET renders the localized failure PAGE for a human who
			// clicked the emailed link (verify.go writeVerifyFailPage), so
			// the JSON contract this card pins lives on POST for this
			// endpoint. Same handler and same literal either way.
			name: "POST /v1/auth/verify",
			code: CodeVerifyTokenMissing,
			msg:  msgVerifyTokenMissing,
			pin:  anchorVerifyTokenMissing,
			call: func(t *testing.T) *httptest.ResponseRecorder {
				t.Helper()
				a := &AuthHandlers{VerifyKey: connectTestKey()}
				req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil)
				rec := httptest.NewRecorder()
				a.handleVerify(rec, req)
				return rec
			},
		},
		{
			name: "POST /v1/auth/reset-password",
			code: CodeResetTokenMissing,
			msg:  msgResetTokenMissing,
			pin:  anchorResetTokenMissing,
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
			msg:  msgMagicTokenMissing,
			pin:  anchorMagicTokenMissing,
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
				"surfaces indistinguishable (this surface's wording is written by %s)",
				c.name, got, c.code, c.pin.Line(t))
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
//
// The expected wording comes from the case itself (one constant per surface, the
// same one its anchor is named by), so this test has no second list of literals
// to drift against, and a mismatch quotes the DERIVED location of the line the
// surface is supposed to write — the header no longer carries that location.
func TestTokenMissingLiteralsArePerSurface(t *testing.T) {
	for _, c := range tokenMissingCases() {
		var body map[string]string
		if err := json.Unmarshal(c.call(t).Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if body["error"] != c.msg {
			t.Errorf("%s: error = %q, want %q — the per-surface wording is written by %s",
				c.name, body["error"], c.msg, c.pin.Line(t))
		}
		// And the literal is what resolves the code, not a coincidence.
		if got := ErrCodeFor(c.msg); got != c.code {
			t.Errorf("ErrCodeFor(%q) = %q, want %q — the code keyed on that wording is %s",
				c.msg, got, c.code, c.pin.Line(t))
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
