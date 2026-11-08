package httpapi

// The chat lane's scope refusal must carry its OWN machine code (t_fb393abe).
//
// THE DEFECT. POST /chat/confirm answers 400 for two unrelated conditions:
//
//   - the caller sent no usable workspace scope (rest_scope.go's scopeAmbiguous
//     arm), refused BEFORE the proposal token is read, and
//   - a tool the server refused to execute AFTER the token was claimed
//     (chat/proposal.go's `fmt.Errorf("server refused: %s", …)`, which matches none
//     of consumed/expired/argsChanged/notFound and therefore reaches
//     chatConfirmStatusFor's `default:` arm).
//
// Both carried the per-status class code `request.badRequest`, which the dashboard
// deliberately does not translate (#6177), so web/src/app/chat/page.tsx could not
// tell them apart and rendered "Choose a workspace first" for BOTH — including for
// a read_only member whose request carried their own ?workspace= and whose real
// refusal was the role.
//
// WHAT THIS FILE PINS:
//   1. The scope refusal answers 400 AND carries `workspace.scopeRequired`, on the
//      real handler (not on ErrCodeFor directly), so the code really reaches the
//      wire through writeErr's envelope.
//   2. The scope refusal is reachable through the PRODUCTION path — an ambiguous
//      unscoped call from a two-workspace session — rather than through a body
//      hand-built to contain the code. A test that fed the code in itself would
//      prove nothing about whether the server ever emits it.
//   3. The message→code table and the handler's own literal cannot drift: the
//      table key must equal the constant rest_scope.go writes.
//   4. The two OTHER 400s on this lane are untouched: an unscoped message POST and
//      a malformed confirm body keep whatever code they had, and must not be
//      relabelled as scope refusals.
//
// WHAT IT DOES NOT COVER (stated honestly): it does not drive the post-claim tool
// refusal end to end (that needs a read_only workspace member and a real tool
// dispatch, and is covered by internal/mcpsrv's role-gate tests); what it pins here
// is that the two conditions are distinguishable on the wire, which is the property
// the client branches on.
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestScopeRequiredRefusalCarriesItsOwnCode is the criterion: "the scope-required
// refusal has its own machine code, and the page can tell it from a non-scope 400".
func TestScopeRequiredRefusalCarriesItsOwnCode(t *testing.T) {
	e := newScopeEnv(t)
	_, token, _ := e.userWithWorkspaces(t, 2)

	// The PRODUCTION shape of the refusal: a session that can reach two workspaces
	// sends no ?workspace=. handleChatConfirm answers 400 from the scopeAmbiguous arm.
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm",
		`{"token":"tok-1","args_hash":"hash-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unscoped /chat/confirm = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal body is not the error envelope: %v (body=%s)", err, rec.Body.String())
	}
	// THE CODE. Without it the page sees `request.badRequest`, which it does not
	// translate, and falls back to the scope copy for every 400 on this route.
	if got := body["code"]; got != CodeWorkspaceScopeRequired {
		t.Errorf("code = %q, want %q — a scope refusal that carries only the class code "+
			"cannot be told apart from any other 400 this route answers (including a tool refusal "+
			"that arrives AFTER the token is claimed), so the page tells a user who already "+
			"selected a workspace to go and choose one", got, CodeWorkspaceScopeRequired)
	}
	// The prose must survive: it is the wire message for API-key/curl/MCP callers,
	// none of which can look up an i18n key.
	if got := body["error"]; got != scopeRequiredMessage {
		t.Errorf("error = %q, want %q (the wire prose must be preserved)", got, scopeRequiredMessage)
	}
	// Neutralising the code in errorBody (e.g. forcing classCode) turns this red —
	// that is the mutation this test exists for.
	//
	// And the refusal must NOT claim the code of a role refusal: the two are the
	// whole reason this card exists.
	if body["code"] == CodeRoleManageRequired {
		t.Error("the scope refusal reports the ROLE code — the page would show the role sentence for a " +
			"missing workspace, which is the mirror image of the defect")
	}
}

// TestScopeRequiredWireMessageIsTheTableKey pins the two spellings together: the
// literal rest_scope.go writes and the key errcode.go maps onto the code. errcode.go
// looks its code up BY MESSAGE, so a reworded refusal silently loses the code while
// still answering 400 — and nothing else would notice.
func TestScopeRequiredWireMessageIsTheTableKey(t *testing.T) {
	if scopeRequiredMessage == "" {
		t.Fatal("rest_scope.go's scopeRequiredMessage is empty — the refusal would answer an empty body")
	}
	if got := ErrCodeFor(scopeRequiredMessage); got != CodeWorkspaceScopeRequired {
		t.Errorf("ErrCodeFor(%q) = %q, want %q — the handler's literal and the message→code table have "+
			"drifted, so the refusal ships with the untranslatable class code and the dashboard cannot "+
			"tell it from any other 400", scopeRequiredMessage, got, CodeWorkspaceScopeRequired)
	}
	// The table entry must exist as an EXACT key, not as a prefix rule: a prefix
	// rule would claim any future message that happens to contain this text, and
	// the code is a public contract (errcode.go's header).
	if _, ok := errCodeByMessage[scopeRequiredMessage]; !ok {
		t.Errorf("the table has no exact entry for %q — it resolves only through errCodeByPrefix, which "+
			"would also claim unrelated messages that contain the sentence", scopeRequiredMessage)
	}
}

// TestScopeRefusalDoesNotRelabelTheLane'sOtherFourHundreds is the negative half: the
// scope code must be earned, not sprayed over every 400 the chat lane answers. Three
// other shapes are driven here and none may answer it.
func TestScopeRefusalDoesNotRelabelTheLaneOtherFourHundreds(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 2)

	cases := []struct {
		name string
		path string
		body string
	}{
		// A SCOPED confirm with a malformed body: a 400, and nothing to do with the
		// scope (the scope resolved fine).
		{"scoped confirm, bad JSON", "/v1/rest/chat/confirm?workspace=" + slugs[1], `{"token":`},
		// A SCOPED confirm with no args_hash: the token binding is missing.
		{"scoped confirm, no args_hash", "/v1/rest/chat/confirm?workspace=" + slugs[1], `{"token":"tok-1"}`},
	}
	for _, c := range cases {
		rec := e.do(t, token, http.MethodPost, c.path, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", c.name, rec.Code, rec.Body.String())
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s: bad envelope: %v", c.name, err)
			continue
		}
		if body["code"] == CodeWorkspaceScopeRequired {
			t.Errorf("%s: answered the SCOPE code for a refusal that is not about the scope — the code "+
				"would become a wildcard for every 400 and the page would tell users to choose a "+
				"workspace they already have (body %s)", c.name, rec.Body.String())
		}
	}

	// And an UNSCOPED message POST — the same ambiguous-scope condition on the
	// message route — must carry the code too: the refusal is one rule, and the page
	// reads it on the message path as well. This is the positive control that keeps
	// the loop above from passing simply because nothing ever answers the code.
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"berapa produk saya?"}`)
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("message refusal body is not the envelope: %v (body=%s)", err, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest || body["code"] != CodeWorkspaceScopeRequired {
		t.Errorf("unscoped message POST = %d code %q, want 400 %q — the scope refusal is one condition "+
			"across the chat lane, and the page renders its copy from the code on both paths (body %s)",
			rec.Code, body["code"], CodeWorkspaceScopeRequired, rec.Body.String())
	}
}
