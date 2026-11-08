package httpapi

// The NO-WORKSPACE confirm refusal must reach the wire as a CODE and a status the
// dashboard can act on, and every failed confirm must leave one WARN line that
// names the tool and the cause (card t_bf4feea2).
//
// THE DEFECT, measured on staging (7 Nov). An account created by a magic link never
// had a workspace (`accounts.workspace_id` NULL). The assistant prepared a
// form-creation card, the user pressed Confirm, and:
//
//	INFO http_request method=POST path=/v1/rest/chat/confirm status=400 duration_ms=113
//	UI: "The change could not be applied. Please try again."
//
// The token was already spent — the refusal happens after the atomic claim — so
// "try again" was advice that could never work, and the ACCESS LOG was the entire
// server-side record: no tool, no reason, no error. The refusal itself was
// mcpsrv.requireLiveWorkspace's ("this account has no workspace yet — create a
// workspace before creating a form"), wrapped by the chat lane as
// `server refused: %s`, which matched no arm of chatConfirmStatusFor and therefore
// shipped as a 400 with the untranslatable class code `request.badRequest`.
//
// WHAT THIS FILE PINS (the edge's half; the classification itself is exercised
// end-to-end over the real MCP server and the real gate in internal/chat,
// TestConfirmFormCreationOnAnAccountWithNoWorkspaceNamesItsCause):
//
//  1. the status is 409, the prose is the GATE'S OWN sentence, and the code is
//     `workspace.required` — the code the form-CREATE door already answers this
//     state with, so the page can name the cause;
//  2. the WARN line carries request_id, tool, stage and err, so the next report is
//     answerable from the log rather than from a forensic read of the database;
//  3. an UNCLASSIFIED post-claim refusal still answers 400 with the class code —
//     the new branch is not a wildcard.
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// workspaceRequiredConfirmErr is the error internal/chat returns when the confirmed
// creation is refused by the workspace gate: the sentinel, the gate's own sentence,
// and the wrapper that carries the tool for the log line.
func workspaceRequiredConfirmErr() error {
	return &chat.ConfirmFailure{
		Tool:  "upsert_form",
		Stage: "refused",
		Err:   fmt.Errorf("%w: %s", chat.ErrProposalWorkspaceRequired, mcpsrv.WorkspaceRequiredMessage),
	}
}

// TestConfirmNoWorkspaceAnswersItsOwnCodeOnTheWire drives the REAL handler with the
// sentinel the chat lane returns and asserts the whole edge: 409, the gate's prose,
// and `workspace.required`.
func TestConfirmNoWorkspaceAnswersItsOwnCodeOnTheWire(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)
	e.runner.confirmErr = workspaceRequiredConfirmErr()

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm?workspace="+slugs[0],
		`{"token":"tok-1","args_hash":"hash-1"}`)

	// 409 and not 400: the request is well formed; what cannot be carried out is
	// the change, and the card can never be used again (its token was claimed
	// before the tool ran). It is also the status the form-CREATE door answers this
	// state with.
	if rec.Code != http.StatusConflict {
		t.Fatalf("no-workspace confirm = %d, want 409 (body %s) — a 400 is the generic arm that told "+
			"this user to \"try again\" on a spent token", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal body is not the error envelope: %v (body=%s)", err, rec.Body.String())
	}
	// THE CODE. Without it the page sees `request.badRequest`, which it
	// deliberately does not translate (#6177), so it falls back to the generic
	// failure copy and names no cause.
	if got := body["code"]; got != CodeWorkspaceRequired {
		t.Errorf("code = %q, want %q — the page can only name a cause it can tell apart from every other "+
			"failure on this route", got, CodeWorkspaceRequired)
	}
	// THE PROSE is the gate's own sentence, unchanged: it is what API-key/curl/MCP
	// callers read, and it is what errcode.go looks the code up BY (see the test
	// below).
	if got := body["error"]; got != mcpsrv.WorkspaceRequiredMessage {
		t.Errorf("error = %q, want the gate's own refusal %q (surfaced verbatim)",
			got, mcpsrv.WorkspaceRequiredMessage)
	}
	// It must not have become the ROLE refusal, which is the other classified
	// post-claim refusal: the two have different remedies (create a workspace vs
	// ask for a role) and the page renders different sentences for them.
	if body["code"] == CodeRoleManageRequired {
		t.Error("the no-workspace refusal reports the ROLE code — the page would tell the user to ask for " +
			"a role when what they need is a workspace")
	}
}

// TestNoWorkspaceWireMessageIsTheTableKey pins the two spellings together: the
// literal the handler writes and the key errcode.go maps onto the code. errcode.go
// looks its code up BY MESSAGE, so a reworded refusal silently loses the code while
// still answering 409 — and nothing else in the suite would notice.
func TestNoWorkspaceWireMessageIsTheTableKey(t *testing.T) {
	if workspaceRequiredMessage == "" {
		t.Fatal("rest_handlers.go's workspaceRequiredMessage is empty — the refusal would answer an empty body")
	}
	// The confirm path now surfaces this exact sentence, so it must resolve to the
	// code from the form-CREATE door as well.
	if got := ErrCodeFor(workspaceRequiredMessage); got != CodeWorkspaceRequired {
		t.Errorf("ErrCodeFor(%q) = %q, want %q — the confirm route's no-workspace refusal would ship with "+
			"the untranslatable class code and the dashboard could not tell it from any other failure",
			workspaceRequiredMessage, got, CodeWorkspaceRequired)
	}
	if _, ok := errCodeByMessage[workspaceRequiredMessage]; !ok {
		t.Errorf("the table has no exact entry for %q — it resolves only through a prefix rule, which would "+
			"also claim unrelated messages containing the sentence", workspaceRequiredMessage)
	}
	// One sentence on both doors: mcpsrv's gate writes it and this package keys it.
	if workspaceRequiredMessage != mcpsrv.WorkspaceRequiredMessage {
		t.Errorf("the confirm path surfaces mcpsrv's sentence %q but this package keys %q — the code would "+
			"be lost the moment the two are spelled differently",
			mcpsrv.WorkspaceRequiredMessage, workspaceRequiredMessage)
	}
}

// TestEveryFailedConfirmLeavesOneWarnLineWithRequestIDToolAndCause is the card's
// server-side requirement: "satu baris WARN pada setiap confirm yang gagal, dengan
// request_id, nama tool, dan err".
//
// THE DEFECT this pins shut: the staging log held the status and nothing else, so
// the only way to explain the user's screenshot was to read
// chat_write_proposals.args out of the database by hand.
func TestEveryFailedConfirmLeavesOneWarnLineWithRequestIDToolAndCause(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)
	e.runner.confirmErr = workspaceRequiredConfirmErr()

	// Capture the log. slog.SetDefault is process-wide, so it is restored.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Through the production middleware chain, because request_id is the field that
	// ties the line to the access log's entry and to the client's own report.
	handler := middleware.RequestID(e.mux)
	r := httptest.NewRequest(http.MethodPost, "/v1/rest/chat/confirm?workspace="+slugs[0],
		strings.NewReader(`{"token":"tok-1","args_hash":"hash-1"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusConflict {
		t.Fatalf("confirm = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	requestID := rec.Header().Get("X-Request-Id")
	if requestID == "" {
		t.Fatal("the middleware echoed no request id — this test cannot prove the log line carries one")
	}

	line := warnLineFor(&buf, "chat_confirm_failed")
	if line == "" {
		t.Fatalf("no WARN chat_confirm_failed line was written for a failed confirm. Log was:\n%s\n"+
			"Without it every user report is a forensic read of chat_write_proposals (the measured defect).",
			buf.String())
	}
	for _, want := range []struct{ key, value, why string }{
		{"request_id", requestID, "the line must correlate with the access log and the user's report"},
		{"tool", "upsert_form", "the first question of every report is WHICH tool the card named"},
		{"stage", "refused", "the stage says WHERE the lane stopped, without parsing error text"},
		{"reason", "workspace_required", "the classification is the one-word cause"},
		{"account_id", "", ""}, // presence only: checked below
		{"err", "", ""},        // presence + text checked below
	} {
		if want.value == "" {
			if !strings.Contains(line, want.key+"=") {
				t.Errorf("the WARN line carries no %s: %s", want.key, line)
			}
			continue
		}
		if !strings.Contains(line, want.key+"="+want.value) {
			t.Errorf("the WARN line does not carry %s=%s (%s): %s", want.key, want.value, want.why, line)
		}
	}
	// The cause itself: the gate's own sentence, so an operator reads the refusal
	// the tool made rather than a paraphrase. (The text handler quotes a value that
	// contains spaces, so the check is on the sentence, not on `err=`.)
	if !strings.Contains(line, "no workspace yet") || !strings.Contains(line, "create a workspace before creating a form") {
		t.Errorf("the WARN line does not quote the cause: %s", line)
	}
	// The token must never appear: it is an authorisation, not a diagnostic. The
	// fingerprint is what correlates the line with the proposal row.
	if strings.Contains(line, "tok-1") {
		t.Errorf("the WARN line contains the RAW proposal token — the log would hold a live authorisation: %s", line)
	}
	if !strings.Contains(line, "token_fp=") {
		t.Errorf("the WARN line carries no token fingerprint, so it cannot be matched to the proposal row: %s", line)
	}
}

// TestAnUnclassifiedConfirmFailureStaysTheClassCode is the negative half: the new
// branch must be EARNED by the classification, not sprayed over every 400 the route
// answers — otherwise the page would name a workspace problem for a role problem,
// which is the mirror image of the defect this card fixes.
func TestAnUnclassifiedConfirmFailureStaysTheClassCode(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)
	// A post-claim refusal the lane could NOT classify (the pre-fix wrap).
	e.runner.confirmErr = fmt.Errorf("server refused: title is required")

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm?workspace="+slugs[0],
		`{"token":"tok-1","args_hash":"hash-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unclassified post-claim refusal = %d, want 400 — this branch is 409 only for the "+
			"refusals the lane can name (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad envelope: %v (body=%s)", err, rec.Body.String())
	}
	if body["code"] == CodeWorkspaceRequired {
		t.Errorf("an unclassified refusal claimed `workspace.required` — the page would tell a user whose "+
			"card failed for another reason to go and create a workspace (body %s)", rec.Body.String())
	}
	if body["error"] == workspaceRequiredMessage {
		t.Errorf("an unclassified refusal claimed the no-workspace sentence (body %s)", rec.Body.String())
	}
}

// warnLineFor returns the first log line for event, or "".
func warnLineFor(buf *bytes.Buffer, event string) string {
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, "msg="+event) {
			return line
		}
	}
	return ""
}
