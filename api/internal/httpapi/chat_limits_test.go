// The chat's LIMITS are a contract, not a comment.
//
// # WHAT THIS FILE EXISTS FOR
//
// The `limits` block on GET /v1/rest/chat (and on every chat answer) is the
// client's only view of the caps: the UI renders max_message_chars as the
// message box's maxlength and explains a 429 with messages_per_hour. A cap
// reported here that is not the cap the server ENFORCES is worse than no cap at
// all — it tells the user a number the server will contradict.
//
// So there are three guards, and each covers a different way to get it wrong:
//
//  1. TestChatLimitsReportsEveryEnforcedCap — walks chatLimitsOut by reflection
//     and demands every declared field be nonzero AND equal to the constant that
//     enforces it, on the STRUCT and on both live HTTP bodies. The reflection
//     walk is the REVERSE guard the defect needed: the phase-2 fields were
//     declared on the struct and never assigned, so the endpoint advertised
//     proposal_ttl_seconds: 0 and confirms_per_hour: 0 and every test in this
//     package stayed green. A field left out of the struct literal now fails.
//  2. TestChatLimitsSourcesEveryCapFromItsConstant — reads rest_chat.go and
//     refuses a numeric literal in chatLimits()'s returned struct. A reported
//     cap must REFERENCE the constant that governs it, never restate the number,
//     or the two drift (and they drift silently, in the direction that lies to
//     the user). This is what makes the reported value follow the enforced one
//     with no second edit anywhere.
//  3. The 429 the limits explain: TestChatRateLimitIsPerUser and
//     TestChatConfirmEndpointMapsStaleTokensTo409 cover the behaviour; the
//     numbers those refusals quote are the ones asserted here.
package httpapi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chat"
)

// enforcedChatCaps is the one place this test states which constant governs
// which field. It is keyed by the GO field name on chatLimitsOut on purpose: a
// field RENAMED in the struct must be renamed here too, rather than silently
// falling out of the walk (the walk fails on an unknown field for the same
// reason — it must not be possible to add a limit and have the test skip it).
//
// Every value is an expression over the constant that ENFORCES the cap, so this
// table cannot itself become the stale second copy the file exists to prevent.
func enforcedChatCaps() map[string]int {
	return map[string]int{
		// Phase 1.
		"MessagesPerHour": int(chatRatePerHour),
		"Burst":           int(chatBurst),
		"MaxMessageChars": chat.MaxMessageChars,
		"MaxHistoryTurns": chat.MaxTurns,
		"MaxToolSteps":    chat.MaxSteps,
		// Phase 2 — the proposal lane's caps. Read from the package the runner
		// enforces them in (chat), and from the same rate-limit constants
		// handleChatConfirm/handleChatCancel check.
		"MaxProposalsPerMessage": chat.MaxProposalsPerRun,
		"ProposalTTLSeconds":     int(chat.ProposalTTL / time.Second),
		"ConfirmsPerHour":        int(chatConfirmPerHour),
		"ConfirmBurst":           int(chatConfirmBurst),
	}
}

// TestChatLimitsReportsEveryEnforcedCap is the regression test for the defect:
// four phase-2 fields declared on chatLimitsOut and never assigned, so the
// endpoint reported 0 for all four.
//
// It asserts on the LIVE response body of both routes the UI calls — GET
// /v1/rest/chat before it shows the box, and the `limits` block of a chat POST —
// because reading the source is exactly what failed to catch this.
func TestChatLimitsReportsEveryEnforcedCap(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	_, _, token := e.newUser(t)

	// ── the struct itself: every declared field wired to its enforcing cap ──
	// The reflection walk, not a hand-written field list, so a field added to
	// chatLimitsOut is covered the moment it exists.
	want := enforcedChatCaps()
	sv := reflect.ValueOf(chatLimits())
	st := sv.Type()
	if st.NumField() != len(want) {
		t.Errorf("chatLimitsOut declares %d fields but this test knows %d — a limit was "+
			"added or removed; wire it (and add the constant it enforces) rather than "+
			"letting the response report it unset", st.NumField(), len(want))
	}
	for i := 0; i < st.NumField(); i++ {
		sf := st.Field(i)
		exp, ok := want[sf.Name]
		if !ok {
			t.Errorf("chatLimitsOut.%s has no enforced cap in this test — a limit that is "+
				"declared and never assigned serialises as 0", sf.Name)
			continue
		}
		if exp == 0 {
			t.Errorf("the enforced cap for chatLimitsOut.%s is 0 — either the constant is "+
				"gone or this table is wrong; the endpoint must not advertise 0", sf.Name)
			continue
		}
		got := int(sv.Field(i).Int())
		if got != exp {
			t.Errorf("chatLimitsOut.%s = %d, want %d (the cap that is enforced)",
				sf.Name, got, exp)
		}
	}

	// ── the wire: both bodies the client reads ──
	getLimits := chatLimitsFromGet(t, e, token)
	postLimits := chatLimitsFromPost(t, e, token)

	for _, body := range []struct {
		name   string
		limits map[string]any
	}{
		{"GET /v1/rest/chat", getLimits},
		{"POST /v1/rest/chat", postLimits},
	} {
		// Exactly the declared field set: a dropped field is a MISSING key, and
		// an extra key would be a cap the caller cannot know about.
		if len(body.limits) != len(want) {
			t.Errorf("%s reports %d limit fields, want %d (%v)",
				body.name, len(body.limits), len(want), body.limits)
		}
		for i := 0; i < st.NumField(); i++ {
			sf := st.Field(i)
			jsonName := chatLimitJSONName(sf.Tag.Get("json"))
			raw, present := body.limits[jsonName]
			if !present {
				t.Errorf("%s has no %q — the client renders this cap; it must be present "+
					"and nonzero", body.name, jsonName)
				continue
			}
			n, isNum := raw.(float64)
			if !isNum {
				t.Errorf("%s %s = %v (%T), want a number", body.name, jsonName, raw, raw)
				continue
			}
			exp := want[sf.Name]
			if int(n) != exp {
				t.Errorf("%s reports %s = %d, want %d (the enforced cap)",
					body.name, jsonName, int(n), exp)
			}
			if n == 0 {
				t.Errorf("%s reports %s = 0 — the endpoint is advertising a cap of zero",
					body.name, jsonName)
			}
		}
	}

	// GET and the POST's limits block must not disagree: they are the same
	// contract read at two moments, and the UI keeps whichever it saw last.
	for name, got := range getLimits {
		if p, ok := postLimits[name]; ok && p != got {
			t.Errorf("limits disagree between GET (%v) and POST (%v) for %s — the UI keeps "+
				"the last one it saw, so the two routes must answer the same", got, p, name)
		}
	}
}

// TestChatLimitsSourcesEveryCapFromItsConstant walks chatLimits() in the source
// and refuses a numeric literal in the returned struct.
//
// WHY STRUCTURAL, NOT JUST A VALUE CHECK. "Report 300" and "report whatever
// chat.ProposalTTL is" are the same assertion today and different assertions the
// day someone changes the constant — and the whole point of the card is that
// chatLimits() must not hold a second copy of the number. A value assertion
// alone cannot tell those apart; this can. It also covers the NEXT limit: the
// mistake is writing the number, not writing it poorly.
func TestChatLimitsSourcesEveryCapFromItsConstant(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "rest_chat.go", nil, 0)
	if err != nil {
		// The test runs in the package directory, so the file is a sibling.
		t.Fatalf("parse rest_chat.go: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "chatLimits" {
			fn = fd
		}
	}
	if fn == nil || fn.Body == nil {
		// Retarget the guard, do not delete it: chatLimits() is where the client's
		// view of every cap is built.
		t.Fatal("chatLimits() is gone from rest_chat.go — this guard must be pointed at " +
			"whatever replaced it, not removed")
	}
	var lit *ast.CompositeLit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok && lit == nil {
			lit = cl
		}
		return true
	})
	if lit == nil {
		t.Fatal("chatLimits() returns no struct literal — this guard cannot see its field " +
			"values; if the shape changed, retarget the guard")
	}

	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("chatLimits() sets a limit positionally (%T) — name the field so a cap "+
				"cannot be wired to the wrong one", el)
		}
		field, _ := kv.Key.(*ast.Ident)
		ast.Inspect(kv.Value, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok {
				return true
			}
			switch bl.Kind {
			case token.INT, token.FLOAT:
				t.Errorf("chatLimits() sets %s to the literal %s — reference the constant that "+
					"enforces it (chat.* for the runner's caps, chatRatePerHour/chatBurst and "+
					"chatConfirmPerHour/chatConfirmBurst for the buckets) so the reported cap "+
					"cannot drift from the enforced one", identName(field), bl.Value)
			}
			return true
		})
	}
}

func identName(id *ast.Ident) string {
	if id == nil {
		return "<unnamed field>"
	}
	return id.Name
}

// chatLimitJSONName strips the json tag options ("max_tool_steps,omitempty").
func chatLimitJSONName(tag string) string {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i]
		}
	}
	return tag
}

// chatLimitsFromGet reads the `limits` object from GET /v1/rest/chat — the call
// the chat page makes before it shows the message box, so the numbers here are
// what a user's first interaction with the feature is told.
func chatLimitsFromGet(t *testing.T, e *chatEnv, token string) map[string]any {
	t.Helper()
	rec := e.do(t, token, http.MethodGet, "/v1/rest/chat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Limits map[string]any `json:"limits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode GET limits: %v (%s)", err, rec.Body.String())
	}
	if out.Limits == nil {
		t.Fatalf("GET /v1/rest/chat has no limits block (body %s)", rec.Body.String())
	}
	return out.Limits
}

// chatLimitsFromPost reads the `limits` block off a successful chat answer.
func chatLimitsFromPost(t *testing.T, e *chatEnv, token string) map[string]any {
	t.Helper()
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"berapa banyak borang saya?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Limits map[string]any `json:"limits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode POST limits: %v (%s)", err, rec.Body.String())
	}
	if out.Limits == nil {
		t.Fatalf("POST /v1/rest/chat has no limits block (body %s)", rec.Body.String())
	}
	return out.Limits
}
