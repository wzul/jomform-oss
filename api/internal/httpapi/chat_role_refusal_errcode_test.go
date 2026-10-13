package httpapi

// The chat lane's ROLE refusal must reach the confirm wire as a CODE, and the
// confirm route must keep answering it as a 400 (card t_1900adef).
//
// THE DEFECT, measured on live prod (v1.19.1). A read_only workspace member
// confirmed a prepared change. The workspace role gate refused the write AFTER the
// token was claimed, and the wire answered:
//
//	400 {"code":"request.badRequest","error":"cadangan tidak dapat dilaksanakan"}
//
// — the untranslatable class code, because chatConfirmMessageFor's `default:` arm
// discarded the tool's refusal and wrote a generic sentence whose text is in no
// table row. The dashboard can only branch on a code, so the page could not name
// the cause and printed its generic "try again" failure copy over a token that was
// already spent.
//
// THE ROOT CAUSE WAS ONE WORD. errcode.go's message→code table keyed the refusal
// as the EXACT literal "perlu peranan manage atau admin" (the REST edge's spelling)
// while the MCP write gate writes "… dalam workspace ini" — one word longer — so
// ErrCodeFor returned "" for the confirm path's own wording. This file pins BOTH
// halves of the pairing, so neither can drift silently again:
//
//  1. the GATE'S OWN message resolves to CodeRoleManageRequired (the row that was
//     missing), and
//  2. the confirm path's wire body carries it — driven through the REAL handler,
//     not by calling ErrCodeFor directly, so the code really reaches the client
//     through writeErr's envelope.
//
// WHAT IT DOES NOT COVER (stated honestly): the classification of a refused tool
// RESULT into chat.ErrProposalRoleRefused is exercised end to end over the real
// MCP server and the real role gate in internal/chat
// (TestConfirmRefusedByWorkspaceRoleKeepsTheCause). What this package owns is the
// edge: that the sentinel the lane returns becomes this status, this prose and this
// code.
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// TestRoleRefusalWireMessageResolvesToItsCode pins the missing table row: the
// sentence the write gates actually emit must answer the ROLE code, in BOTH
// spellings the codebase writes it in.
//
// THE BITE: deleting the prefix row in errcode.go (or re-wording the gate without
// touching the table) turns the first two assertions red — and with them the
// confirm path's refusal silently returns to `request.badRequest`.
func TestRoleRefusalWireMessageResolvesToItsCode(t *testing.T) {
	if mcpsrv.RoleManageRequiredMessage == "" || mcpsrv.RoleManageRequiredLeadIn == "" {
		t.Fatal("mcpsrv's role-refusal wording is empty — the gates would refuse with an empty body")
	}
	// The confirm path's wording: the gate's own sentence.
	if got := ErrCodeFor(mcpsrv.RoleManageRequiredMessage); got != CodeRoleManageRequired {
		t.Errorf("ErrCodeFor(%q) = %q, want %q — this is the message the confirmed tool's own write gate "+
			"emits, and the confirm path surfaces it VERBATIM: without the row the refusal ships as the "+
			"class code `request.badRequest`, which the dashboard deliberately does not translate, so the "+
			"page cannot tell it from any other 400 and prints its generic failure copy (the measured "+
			"defect)", mcpsrv.RoleManageRequiredMessage, got, CodeRoleManageRequired)
	}
	// The REST edge's spelling, which the table keyed exactly.
	if got := ErrCodeFor("perlu peranan manage atau admin"); got != CodeRoleManageRequired {
		t.Errorf("ErrCodeFor(the REST edge's spelling) = %q, want %q — the two spellings of one refusal "+
			"must not carry two different codes", got, CodeRoleManageRequired)
	}
	// And the resolution is by the SHARED lead-in, so a future suffix on the gate's
	// sentence cannot detach the code again (that is exactly how the defect arose).
	if got := ErrCodeFor(mcpsrv.RoleManageRequiredLeadIn + " pada masa hadapan"); got != CodeRoleManageRequired {
		t.Errorf("a re-worded role refusal = %q, want %q — the lead-in is the declared constant the gate "+
			"builds its sentence from, so the code must not depend on the rest of the wording",
			got, CodeRoleManageRequired)
	}
	// Not a wildcard: a message that is NOT a role refusal keeps the class code.
	if got := ErrCodeFor("cadangan tidak dapat dilaksanakan"); got != "" {
		t.Errorf("the generic confirm failure resolves to %q — the role rule has become a wildcard and "+
			"every post-claim refusal would claim the role sentence", got)
	}
}

// TestConfirmRoleRefusalAnswersItsOwnCodeOnTheWire drives the real handler with the
// sentinel the chat lane returns for a post-claim role refusal, and asserts the
// whole edge: 400, the gate's own prose, and `role.manageRequired`.
func TestConfirmRoleRefusalAnswersItsOwnCodeOnTheWire(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)
	// The exact error internal/chat returns when the workspace role gate declines a
	// confirmed write (see ErrProposalRoleRefused).
	e.runner.confirmErr = fmt.Errorf("%w: %s", chat.ErrProposalRoleRefused, mcpsrv.RoleManageRequiredMessage)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm?workspace="+slugs[0],
		`{"token":"tok-1","args_hash":"hash-1"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("role-refused confirm = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal body is not the error envelope: %v (body=%s)", err, rec.Body.String())
	}
	// THE CODE. Without it the page sees `request.badRequest`, which it does not
	// translate, and tells a member whose card was refused by their ROLE to "try
	// again" on a token that is already spent.
	if got := body["code"]; got != CodeRoleManageRequired {
		t.Errorf("code = %q, want %q — the page can only name the cause it can tell apart from any other "+
			"400 on this route", got, CodeRoleManageRequired)
	}
	// The prose names the CAUSE now, and it is the gate's own sentence: this is the
	// message API-key/curl/MCP callers read, none of which can look up an i18n key.
	if got := body["error"]; got != mcpsrv.RoleManageRequiredMessage {
		t.Errorf("error = %q, want %q (the gate's own refusal, surfaced verbatim)", got, mcpsrv.RoleManageRequiredMessage)
	}
	// It must not be the generic sentence: an answer that names the cause AND
	// carries a code has to be one or the other, never the pre-fix text with a code
	// that would then name a different thing than the prose says.
	if body["error"] == "cadangan tidak dapat dilaksanakan" {
		t.Error("the refusal still answers the generic sentence — the cause the server knows is being thrown away")
	}
	// And it must not claim the SCOPE code: the two 400s on this route are the
	// whole reason this lane has codes at all.
	if body["code"] == CodeWorkspaceScopeRequired {
		t.Error("the role refusal claims the workspace-scope code — the page would tell a user who already " +
			"selected a workspace to go and choose one")
	}
}

// TestConfirmRoleRefusalStaysAFourHundred pins the STATUS, deliberately.
//
// A permission refusal "should" be a 403, and making it one is the obvious-looking
// change — it would break the dashboard: web/src/lib/wsfetch.ts reads 401/403 as
// "the session is no longer valid" and navigates to /login, so a member refused by
// their role would be signed out of the page they were reading. The distinction
// this route needs is the CODE (see the test above), not the status.
func TestConfirmRoleRefusalStaysAFourHundred(t *testing.T) {
	err := fmt.Errorf("%w: %s", chat.ErrProposalRoleRefused, mcpsrv.RoleManageRequiredMessage)
	if got := chatConfirmStatusFor(err); got != http.StatusBadRequest {
		t.Errorf("chatConfirmStatusFor(role refusal) = %d, want 400 — a 403 here is read by the dashboard "+
			"as a dead session and redirects the user to /login mid-page", got)
	}
	if got := chatConfirmMessageFor(err); got != mcpsrv.RoleManageRequiredMessage {
		t.Errorf("chatConfirmMessageFor(role refusal) = %q, want %q — the handler's literal and the "+
			"message→code table must not drift", got, mcpsrv.RoleManageRequiredMessage)
	}
}
