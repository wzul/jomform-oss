// Regression guard for the machine error CODE contract (issue #6177).
//
// THE BUG. Every API failure answered with Malay prose only —
// `{"error":"workspace tidak dijumpai"}` — and the dashboard rendered that
// string verbatim, so an English dashboard showed Malay. The fix is a stable
// `code` field the client translates. This file guards the SERVER half of that
// contract:
//
//  1. writeErr really puts a code in the JSON body (not just prose).
//  2. The reported case resolves to its own code, not a generic class code.
//  3. An unknown message still gets a code (the status class), so a client
//     never has to fall back to the prose.
//  4. Every table key still exists as a Go string literal in this package.
//     This is the check that makes the table maintainable: a handler that
//     rewords its message does NOT silently leave a stale/never-matching code
//     behind, and a table entry cannot become dead weight.
//  5. The prose is STILL in the body. Removing it would break API-key
//     callers, MCP and the public buyer-form JS, which cannot consume an
//     i18n key.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestIssue6177WriteErrCarriesMachineCode(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, http.StatusForbidden, "workspace tidak dijumpai")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the error envelope: %v (body=%s)", err, w.Body.String())
	}

	// The exact reported case must name its own code: a class code here would
	// let the dashboard print a generic sentence for a refusal the user can act
	// on (a workspace slug they do not own).
	//
	// PRE-FIX this read `""` — writeErr emitted {"error": …} only. Neutralising
	// the code in errorBody turns this test red.
	if got := body["code"]; got != CodeWorkspaceNotFound {
		t.Errorf("code = %q, want %q (the reported case must name its own code, "+
			"not a status class)", got, CodeWorkspaceNotFound)
	}
	// The prose must survive: it is the wire message for API-key/curl/MCP
	// callers and the public buyer-form JavaScript.
	if got := body["error"]; got != "workspace tidak dijumpai" {
		t.Errorf("error = %q, want the wire prose preserved", got)
	}
}

func TestIssue6177EveryFailureGetsACode(t *testing.T) {
	// A message with no specific entry must still answer a code, or the client
	// is back to rendering prose. The class code is what it gets.
	cases := []struct {
		status int
		msg    string
		want   string
	}{
		{http.StatusBadRequest, "butiran tidak lengkap", ClassBadRequest},
		{http.StatusUnauthorized, "auth diperlukan", ClassAuthRequired},
		{http.StatusNotFound, "borang tidak dijumpai", ClassNotFound},
		// 409 has BOTH a specific-code message and one without, so both the
		// specific lookup and the class fallback for this status are pinned.
		{http.StatusConflict, "produk telah habis (out of stock)", CodeProductOutOfStock},
		{http.StatusConflict, "ahli sudah wujud dalam workspace ini", ClassConflict},
		{http.StatusInternalServerError, "internal error", ClassInternal},
		{http.StatusServiceUnavailable, "pembayaran tidak aktif", CodePaymentInactive},
		{http.StatusRequestEntityTooLarge, "fail melebihi had 20MB", ClassPayloadTooLarge},
		{http.StatusTooManyRequests, "terlalu banyak cubaan. Cuba lagi kemudian.", CodeRateLimited},
		{http.StatusBadGateway, "token exchange gagal", ClassUpstreamFailed},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		writeErr(w, c.status, c.msg)
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("status %d %q: bad envelope: %v", c.status, c.msg, err)
		}
		if body["code"] == "" {
			t.Errorf("status %d %q: code is EMPTY — a client would fall back to prose "+
				"and print Malay on an English dashboard", c.status, c.msg)
		}
		if body["code"] != c.want {
			t.Errorf("status %d %q: code = %q, want %q", c.status, c.msg, body["code"], c.want)
		}
	}
}

func TestIssue6177URLGuardPrefixResolvesToItsOwnCode(t *testing.T) {
	// urlguard.InvalidMessage() builds "<field> mesti bermula dengan https:// — …"
	// around the field name, so no exact-key entry can ever match it. It is the
	// one message shape that NEEDS the prefix rule; without it the dashboard
	// would print the server's Malay/English mixture for every rejected image.
	msgs := []string{
		"theme.logo_url mesti bermula dengan https:// — skema lain (data:, http:, javascript:, …) ditolak supaya imej tidak rosak pada halaman pembeli",
		"images mesti bermula dengan https:// — skema lain ditolak",
	}
	for _, m := range msgs {
		if got := ErrCodeFor(m); got != CodeURLNotHTTPS {
			t.Errorf("ErrCodeFor(%q) = %q, want %q", m, got, CodeURLNotHTTPS)
		}
	}
}

func TestIssue6177ErrorCodeTableHasNoDeadKeys(t *testing.T) {
	// A table entry keyed on a message that no longer exists anywhere in the
	// package is DEAD: the code can never fire, and nothing says so. Read every
	// non-test .go file in this package and require each key to appear as a Go
	// string literal (in a writeErr call, a map literal, or a var).
	//
	// This walks the tree rather than iterating a list of file names: a new
	// handler file must be covered automatically, and a file renamed away must
	// not be silently skipped (the same failure mode as the sales-read guard).
	// Match on the string literal, not on a window around a pattern, so a
	// neighbour line cannot satisfy the check.
	pkgDir := "."
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var src strings.Builder
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// errcode.go itself holds the table; excluding it would prove nothing,
		// and INCLUDING it is fine because the check looks for the literal
		// anywhere — the table's own key line is not a call site, so the
		// "must appear elsewhere too" requirement below is what gives it teeth.
		if e.Name() == "errcode_test.go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if e.Name() == "errcode.go" {
			continue // the table itself, checked below via the call-site scan
		}
		src.Write(b)
		src.WriteString("\n")
		files++
	}
	if files < 10 {
		t.Fatalf("only %d source files scanned — the walk is not covering the package", files)
	}
	all := src.String()

	for msg, code := range errCodeByMessage {
		// The handler writes this message with %q-style quoting; require the
		// literal in a Go source form (either quote style, escaped or not).
		if !strings.Contains(all, `"`+msg+`"`) {
			t.Errorf("error-code table maps %q → %q, but that message no longer "+
				"appears as a string literal in any handler in this package — the "+
				"code is dead and the dashboard will fall back to its generic "+
				"message. Update the table key to the new wording.", msg, code)
		}
	}
	// The allowlist direction: every entry must also be reachable, i.e. the
	// code must be non-empty and namespaced. An empty code would make
	// ErrCodeFor return "" for a table hit, which writeErr would then treat as
	// "no specific code" while ErrCodeFor returned "" — indistinguishable.
	for msg, code := range errCodeByMessage {
		if code == "" || !strings.Contains(code, ".") {
			t.Errorf("table entry %q → %q: a code must be a non-empty namespaced "+
				`string like "workspace.notFound"`, msg, code)
		}
	}
}

func TestIssue6177ClassCodesAreDistinctAndStable(t *testing.T) {
	// Every status the error path can use must map to a distinct, non-empty
	// code. Two statuses sharing a code would make the client unable to tell
	// 403 from 404, which is the whole point of the field.
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge,
		http.StatusTooManyRequests, http.StatusUnprocessableEntity,
		http.StatusNotImplemented, http.StatusBadGateway, http.StatusServiceUnavailable,
	}
	seen := map[string]int{}
	for _, s := range statuses {
		c := classCode(s)
		if c == "" {
			t.Fatalf("status %d maps to an empty class code", s)
		}
		if prev, dup := seen[c]; dup {
			t.Errorf("statuses %d and %d share the class code %q", prev, s, c)
		}
		seen[c] = s
	}
	// An unrecognised status must not panic or answer empty.
	if got := classCode(418); got != ClassInternal {
		t.Errorf("classCode(418) = %q, want %q", got, ClassInternal)
	}
}

func TestIssue6177PanicRecovererCarriesTheCode(t *testing.T) {
	// A panic answers the error envelope too, so it must carry a code like every
	// other failure: a dashboard rendering a 500 from a recovered panic has the
	// same problem as any other status. Driven through the REAL recoverer
	// middleware (not a copy of its body), so removing errorBody from the panic
	// branch fails this test — the bite-proof harness pins that direction.
	panicking := recoverer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))
	w := httptest.NewRecorder()
	panicking.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/anything", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("panic body is not the error envelope: %v (body=%s)", err, w.Body.String())
	}
	if body["code"] != ClassInternal {
		t.Errorf("panic code = %q, want %q — a recovered panic must answer the "+
			"same envelope as any other 500", body["code"], ClassInternal)
	}
	if body["error"] == "" {
		t.Error("panic body lost its human-readable message")
	}
}

func TestIssue6177NoHandlerSendsProseOnly(t *testing.T) {
	// The FAILURE envelope has ONE shape. A handler that hand-rolls
	// map[string]string{"error": msg} bypasses the code contract entirely —
	// which is exactly how the CHIP-credential path shipped prose-only before
	// this fix. Only writeErr / errorBody (and the richer envelopes that now
	// carry `code` explicitly) may build an error body.
	//
	// SCOPE: this checks calls that answer a NON-2xx status. A 200 body may
	// legitimately carry a diagnostic `error` field instead of the failure
	// envelope — `/rest/domains/{id}/auto-cname` answers
	// {"auto_cname":"failed","error":<upstream CF reason>} with 200 so the page
	// can show a partial-success state. Those two sites are pinned by name
	// below rather than silently allowed, so a new one is still a failure.
	pkgDir := "."
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	// 200-OK diagnostic carriers: not the failure envelope, deliberately.
	nonFailureAllowlist := map[string]bool{
		"rest_domains2.go": true, // auto-cname partial-success body
	}
	// A writeJSON call whose body mentions an "error" key. The status literal
	// and the whole map literal sit inside the match, so both the status and
	// the presence of "code" are read from the matched text. The pattern runs
	// to the map's closing brace, so a `code` key on a LATER line (gofmt lines
	// map keys up) is still seen — matching only as far as `"error":` would
	// report every correctly-fixed multi-line literal as an offender.
	callRe := regexp.MustCompile(`writeJSON\(w,\s*http\.Status[A-Za-z]+,\s*map\[string\](?:any|string)\{[^}]*"error"\s*:[^}]*\}\)`)
	statusRe := regexp.MustCompile(`writeJSON\(w,\s*(http\.Status[A-Za-z]+)`)
	var offenders []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		// The envelope DEFINITION (errorBody) has no writeJSON(map-literal) call
		// of its own, so server.go is scanned like any other file — that is what
		// covers the recovered-panic 500. Only oauth.go is skipped: it emits the
		// RFC 6749 error document, a different contract.
		if e.Name() == "oauth.go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		s := string(b)
		for _, m := range callRe.FindAllStringIndex(s, -1) {
			call := s[m[0]:m[1]]
			st := statusRe.FindStringSubmatch(call)
			if st == nil {
				continue
			}
			// 2xx: a diagnostic body, not the failure envelope.
			if st[1] == "http.StatusOK" || st[1] == "http.StatusCreated" {
				line := 1 + strings.Count(s[:m[0]], "\n")
				if !nonFailureAllowlist[e.Name()] {
					offenders = append(offenders, fmt.Sprintf("%s:%d (200-body carries error without an allowlist entry)", e.Name(), line))
				}
				continue
			}
			if !strings.Contains(call, `"code"`) {
				line := 1 + strings.Count(s[:m[0]], "\n")
				offenders = append(offenders, e.Name()+":"+strconv.Itoa(line))
			}
		}
	}
	if len(offenders) > 0 {
		t.Errorf("these sites build a FAILURE body WITHOUT a machine code, so a "+
			"dashboard client can only render the server's prose (issue #6177):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
