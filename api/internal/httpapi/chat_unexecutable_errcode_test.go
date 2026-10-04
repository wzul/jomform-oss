package httpapi

// The chat lane's "this card cannot be carried out" refusal carries its OWN code
// (card t_623676d1).
//
// THE DEFECT. A proposal whose arguments the target tool would never accept — the
// model keyed the object `id` while update_product requires `product_id`, which is
// exactly what the chat's READ tools return (mcpsrv.handlers.go's productOut is
// `json:"id"`) — used to render a card naming NO object, and Confirm then answered
// a bare 400 `{"error":"cadangan tidak dapat dilaksanakan"}` AFTER spending the
// token. The page could only say "The change could not be applied. Please try
// again." — a retry hint for a card that can never succeed, over a token that was
// already gone.
//
// WHAT THIS FILE PINS:
//   1. The refusal answers 409 (the request is well formed; it is the PROPOSAL
//      that is unfit) AND carries its own code, on the real handler → writeErr
//      path, so the code really reaches the wire.
//   2. The wire literal chatConfirmMessageFor writes is the errcode.go table key —
//      errcode.go looks the code up BY MESSAGE, so a reworded refusal would keep
//      answering 409 and silently lose its code, and the page would fall back to
//      its spent/expired sentence.
//   3. It does NOT borrow the spent/expired code, and the spent/expired refusals
//      do not borrow it: the two 409s must stay distinguishable, because their
//      remedies are the same but their REASONS are not (one token is live, one is
//      dead) and the page's copy depends on knowing which.
//
// The BEHAVIOUR of the mapping is executed in internal/chat
// (proposal_argshape_test.go), which drives the real store and the real schema.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestChatConfirmMapsAnUnexecutableProposalToItsOwnCode drives the PRODUCTION
// handler with the runner's own refusal and reads the envelope it produces.
func TestChatConfirmMapsAnUnexecutableProposalToItsOwnCode(t *testing.T) {
	runner := &recordingRunner{ready: true, confirmErr: chat.ErrProposalArgsInvalid}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm",
		`{"token":"tok-u","args_hash":"hash-u"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("confirm with an unacceptable proposal = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal body is not the error envelope: %v (body=%s)", err, rec.Body.String())
	}
	if got := body["code"]; got != CodeProposalArgsInvalid {
		t.Errorf("code = %q, want %q — without it the page reads the 409 class code and prints its "+
			"spent/expired sentence (\"expired or was already used\") for a card that was NEITHER, and "+
			"the user never learns their token is still good and a fresh card is all they need",
			got, CodeProposalArgsInvalid)
	}
	// The prose survives (API-key/curl/MCP callers cannot look up an i18n key) and
	// it must NOT be the spent/expired sentence: those describe a different state.
	if body["error"] == "" {
		t.Error("the refusal body carries no wire prose")
	}
	if body["error"] == chatConfirmMessageFor(dbstore.ErrChatProposalExpired) {
		t.Error("the unexecutable refusal reuses the EXPIRED prose — the token is unspent and the " +
			"user would be told the card expired, which is false")
	}
	// Nothing internal leaks to the browser.
	if s := rec.Body.String(); containsAny(s, "pgx", "SQLSTATE", "jsonschema") {
		t.Errorf("the refusal leaks a server detail: %s", s)
	}
}

// TestChatConfirmUnexecutableWireMessageIsTheTableKey pins the two spellings
// together, the same way the scope refusal's pair is pinned.
func TestChatConfirmUnexecutableWireMessageIsTheTableKey(t *testing.T) {
	msg := chatConfirmMessageFor(chat.ErrProposalArgsInvalid)
	if msg == "" {
		t.Fatal("chatConfirmMessageFor(ErrProposalArgsInvalid) is empty — the refusal would answer an empty body")
	}
	if got := ErrCodeFor(msg); got != CodeProposalArgsInvalid {
		t.Errorf("ErrCodeFor(%q) = %q, want %q — the handler's literal and the message→code table have "+
			"drifted, so the refusal ships with the untranslatable 409 class code", msg, got, CodeProposalArgsInvalid)
	}
	if _, ok := errCodeByMessage[msg]; !ok {
		t.Errorf("the table has no EXACT entry for %q — it would resolve only through a prefix rule, "+
			"which claims any future message containing this text", msg)
	}
}

// TestChatConfirmTwoConflictsStayDistinguishable is the negative half: the two
// 409s on this route must not collapse onto one code, in either direction.
func TestChatConfirmTwoConflictsStayDistinguishable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"expired", dbstore.ErrChatProposalExpired, ClassConflict},
		{"consumed", dbstore.ErrChatProposalConsumed, ClassConflict},
		{"args changed", chat.ErrProposalArgsChanged, ClassConflict},
		{"unexecutable", chat.ErrProposalArgsInvalid, CodeProposalArgsInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &recordingRunner{ready: true, confirmErr: c.err}
			e := newChatEnv(t, runner)
			_, _, token := e.newUser(t)
			rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm",
				`{"token":"tok-x","args_hash":"hash-x"}`)
			if rec.Code != http.StatusConflict {
				t.Fatalf("%s = %d, want 409 (body %s)", c.name, rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: bad envelope: %v", c.name, err)
			}
			if body["code"] != c.want {
				t.Errorf("%s: code = %q, want %q — the two 409 states have different REASONS (one token "+
					"is live, one is dead) and one sentence cannot be right for both", c.name, body["code"], c.want)
			}
		})
	}
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// A compile-time guard: the sentinel the page mirrors must exist and be mapped,
// so a rename on the Go side cannot leave the REST layer comparing nothing.
var _ = errors.Is(chat.ErrProposalArgsInvalid, chat.ErrProposalArgsInvalid)
