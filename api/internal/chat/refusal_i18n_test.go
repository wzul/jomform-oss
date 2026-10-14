package chat

// A REFUSED STEP CARRIES A MACHINE KEY, NOT A SENTENCE (card t_3839995c).
//
// THE DEFECT. The confirmation card's own wording became dictionary keys in
// t_30ffa28a (title_key / summary_key / label_key / value_key). The REFUSAL block
// on the same page was left behind: a refused step carried `detail`, a sentence the
// RUNNER wrote in Malay — the allowlist refusal ("alat %q tidak dibenarkan dalam
// chat ini"), "RALAT: " plus an internal error, the proposal lane's own message —
// and web/src/app/chat/page.tsx printed it verbatim under the one localized word
// (the `chat.refused` heading). On an ENGLISH dashboard the transcript therefore
// read:
//
//	assistant tried — update_product — alat "update_product" tidak dibenarkan dalam chat ini
//
// half English, half Malay, on a page whose every other failure sentence
// (chat.fail / chat.confirmFail / chat.proposalArgsInvalid) goes through the
// five-locale dictionary. The API cannot fix that by translating: it never receives
// the caller's language on a JSON call (issue #6177 — the same reason
// lib/chatConfirm.ts branches on a machine CODE rather than on the wire's prose).
//
// THE RULE THESE TESTS PIN. A refused step carries a `chat.refused.*` key plus the
// values its sentence interpolates, and `detail` does not exist on the wire AT ALL.
// The wording the MODEL needs is a different thing and is untouched: it still
// travels as the tool result, which is fenced into the prompt (prompt.go) and never
// reaches a browser. One refusal, two audiences, one of which is a person.
//
// WHAT IS ASSERTED, and why each half is needed:
//
//	1. EVERY REFUSAL CAUSE the runner can produce reports a key — the write-tool
//	   refusal, the not-allowed refusal, a tool that declined, the proposal lane,
//	   the per-answer proposal budget, and a call that failed before it reached the
//	   tool. A cause with no key would render as NOTHING on the page (the page has
//	   no prose fallback any more), so a silent failure here is a lost refusal.
//	2. NOTHING ON THE STEP IS A SENTENCE. The detector scans the marshalled step
//	   for a string value containing a space, which is the shape EVERY one of the
//	   old refusals had and no identifier or key has. This is the half that makes
//	   the regression unrepresentable rather than merely unlikely: the old shape
//	   put the Malay sentence in exactly one field, and a new prose field would
//	   have to look like a sentence to pass.
//	3. THE MODEL'S HALF IS NOT DEGRADED. The tool result the model reads back still
//	   names the tool, so a refusal stayed teachable when the sentence left the
//	   step — the fix must not cost the model the ability to correct itself.
//
// THE BITE. TestRefusalProseDetectorCatchesTheOldShape feeds the detector the
// PRE-FIX step (the card's own JSON, `detail` and all) and requires it to name that
// field, so a detector that matches nothing cannot be mistaken for a clean step.
// The dictionary half lives on the web side (web/src/lib/chatRefusal.test.ts): it
// reads THIS FILE, extracts every "chat.refused.*" literal and fails when the
// dictionary is missing one in any of the five locales — the half a Go test cannot
// see. Two halves of one block drifting apart is the defect, so they are pinned
// from both ends.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
)

// proseStepFields returns the names of the fields of a marshalled Step whose value
// is a SENTENCE rather than a machine value.
//
// The rule is deliberately narrow and total: an identifier (a tool name), a
// dictionary key, and a parameter value are all space-free in this protocol, while
// every refusal sentence the defect shipped for the browser contained one. A nested
// map (detail_params) is walked rather than skipped, so a sentence cannot hide one
// level down.
func proseStepFields(t *testing.T, stepJSON []byte) []string {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stepJSON, &top); err != nil {
		return []string{"<unparseable>"}
	}
	var out []string
	for name, raw := range top {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if strings.Contains(s, " ") {
				out = append(out, name)
			}
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err == nil {
			for k, v := range m {
				if strings.Contains(v, " ") {
					out = append(out, name+"."+k)
				}
			}
		}
	}
	return out
}

// stepJSON marshals one step the way the REST edge sends it.
func stepJSON(t *testing.T, st Step) []byte {
	t.Helper()
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal step: %v", err)
	}
	return b
}

// TestRefusalProseDetectorCatchesTheOldShape is the BITE PROOF for
// proseStepFields: the pre-fix step is the card's own measured JSON, and the
// detector must name the field the Malay sentence travelled in. Without this, a
// detector matching nothing would read as "the step is clean".
func TestRefusalProseDetectorCatchesTheOldShape(t *testing.T) {
	// Exactly the shape t_3839995c was filed on: the page printed `detail`
	// verbatim beside the (already localized) heading.
	preFix := []byte(`{"tool":"update_product","status":"refused",` +
		`"detail":"alat \"update_product\" tidak dibenarkan dalam chat ini"}`)
	got := proseStepFields(t, preFix)
	if len(got) != 1 || got[0] != "detail" {
		t.Fatalf("the detector reported %v for the pre-fix step, want [detail] — it cannot see the "+
			"defect it exists for", got)
	}
	// And it must stay quiet on the shape that replaced it, or a clean run below
	// would prove nothing either way.
	clean := []byte(`{"tool":"update_product","status":"refused","detail_key":"chat.refused.writeTool"}`)
	if got := proseStepFields(t, clean); len(got) != 0 {
		t.Fatalf("the detector reported %v for a keyed step, want nothing", got)
	}
}

// TestEveryRefusalCauseCarriesAKeyAndNoProse drives each refusal the runner can
// produce and requires the step to carry its machine key with no sentence beside
// it (the rule at the head of this file).
func TestEveryRefusalCauseCarriesAKeyAndNoProse(t *testing.T) {
	// A refusal sentence in the PROMPT is expected and required (assertion 3); a
	// refusal sentence on the STEP is the defect.
	cases := []struct {
		name string
		// wantKey is the dictionary key the dashboard resolves for this cause.
		wantKey string
		// wantParams is asserted verbatim when non-nil.
		wantParams map[string]string
		// run performs the cause and returns the one refused step and the model's
		// turns (its second round-trip carries the refusal text).
		run func(t *testing.T) (Step, [][]llm.ChatTurn)
	}{
		{
			name:    "a write tool the model called directly",
			wantKey: keyRefusedWriteTool,
			run:     refusalViaRun("update_product", `{"product_id":"x","name":"Nama baharu"}`, &fakeCaller{}),
		},
		{
			name:    "a tool name the chat may not dispatch at all",
			wantKey: keyRefusedNotAllowed,
			run:     refusalViaRun("list_api_keys", `{}`, &fakeCaller{}),
		},
		{
			name:    "a read tool the server itself declined",
			wantKey: keyRefusedTool,
			run: refusalViaRun("list_forms", `{}`,
				&fakeCaller{refuse: map[string]bool{"list_forms": true}}),
		},
		{
			name:    "a filing with no feedback service wired",
			wantKey: keyRefusedTool,
			run:     refusalViaRun(ToolSubmitFeedback, `{"subject":"x","body":"sesuatu yang panjang"}`, &fakeCaller{}),
		},
		{
			name:    "the proposal lane refusing what the model asked for",
			wantKey: keyRefusedProposal,
			run:     refusalViaRun(ToolProposeWrite, `{"tool":"update_product","arguments":{}}`, &fakeCaller{}),
		},
		{
			name:    "a call that failed before it reached the tool",
			wantKey: keyRefusedError,
			run:     refusalViaRun("list_forms", `{}`, &fakeCaller{err: errors.New("sesi alat terputus")}),
		},
		{
			name:       "this answer's proposal budget",
			wantKey:    keyRefusedProposalLimit,
			wantParams: map[string]string{"max": "3"},
			run: func(t *testing.T) (Step, [][]llm.ChatTurn) {
				// The budget is per MESSAGE and is checked before the lane runs, so
				// it is reached with the state a message would already be in —
				// through dispatchIn, the same entry point Run uses.
				r := newTestRunner(t, &fakeCaller{}, &scriptedLLM{}, nil)
				st := &runState{proposals: MaxProposalsPerRun}
				out, ref, err := r.dispatchIn(chatCtx(), &fakeCaller{},
					toolCall("p1", ToolProposeWrite, `{"tool":"update_product","arguments":{}}`), st)
				if err != nil {
					t.Fatalf("dispatchIn: %v", err)
				}
				if ref == nil {
					t.Fatalf("the proposal budget did not refuse: %q", out)
				}
				return Step{Tool: ToolProposeWrite, Status: StepRefused, DetailKey: ref.key,
					DetailParams: ref.params}, nil
			},
		},
	}

	if MaxProposalsPerRun != 3 {
		t.Fatalf("MaxProposalsPerRun is now %d; the budget case above asserts max=3 and must be "+
			"updated with it (the parameter is what the sentence interpolates)", MaxProposalsPerRun)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, turns := c.run(t)
			if st.Status != StepRefused {
				t.Fatalf("step = %+v, want a refusal", st)
			}
			if st.DetailKey != c.wantKey {
				t.Fatalf("detail_key = %q, want %q — the page has no prose fallback any more, so a "+
					"missing key renders as nothing on every dashboard", st.DetailKey, c.wantKey)
			}
			if c.wantParams != nil {
				if len(st.DetailParams) != len(c.wantParams) {
					t.Fatalf("detail_params = %v, want %v", st.DetailParams, c.wantParams)
				}
				for k, v := range c.wantParams {
					if st.DetailParams[k] != v {
						t.Fatalf("detail_params[%q] = %q, want %q", k, st.DetailParams[k], v)
					}
				}
			} else if len(st.DetailParams) != 0 {
				t.Errorf("detail_params = %v, want none — this sentence takes no values", st.DetailParams)
			}
			if prose := proseStepFields(t, stepJSON(t, st)); len(prose) != 0 {
				t.Fatalf("the refused step carries a sentence in %v: %s\nThe API does not know the "+
					"caller's language, so this reaches EVERY dashboard in one language (t_3839995c).",
					prose, stepJSON(t, st))
			}
			// Assertion 3: the model's half is not degraded by the fix. (turns is
			// nil for the budget case, which never reaches the model.) The fenced
			// tool result the model reads names the tool it asked for, so a
			// refusal stayed teachable when its sentence left the step.
			for _, prompt := range turns {
				for _, turn := range prompt {
					if turn.Role == "tool" && !strings.Contains(turn.Content, st.Tool) {
						t.Errorf("the tool result the model read back does not name %q: %q",
							st.Tool, turn.Content)
					}
				}
			}
		})
	}
}

// TestPreparedProposalIsStillReportedAsProposed is the other side of the rule: a
// propose_write that DID prepare a card must still be reported as "proposed" with
// the card's own title key.
//
// It is the control for the pair of facts that pull in opposite directions — the
// refusal above must be visible, and the success must not be relabelled a refusal
// — driven through Run with the REAL MCP session and the real proposal store, so
// the status is read off the production path rather than off a fake.
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the standard test env).
func TestPreparedProposalIsStillReportedAsProposed(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	r.llm = &scriptedLLM{replies: []llm.ChatReply{
		{ToolCalls: []llm.ToolCall{toolCall("p1", ToolProposeWrite,
			`{"tool":"update_product","arguments":{"product_id":`+jsonString(f.productID)+
				`,"name":"Nama Selepas Cadangan"},"reason":"pengguna minta tukar nama"}`)}},
		{Content: "Saya sudah sediakan kad untuk anda sahkan."},
	}}

	res, err := r.Run(f.proposalCtx(), nil, "tukar nama produk saya")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %+v, want exactly one", res.Steps)
	}
	st := res.Steps[0]
	if st.Status != StepProposed {
		t.Fatalf("a prepared card was reported as %q, want %q — the transcript would tell the "+
			"merchant nothing was prepared, or (worse) that a change happened",
			st.Status, StepProposed)
	}
	if st.DetailKey != keyTitleUpdateProduct {
		t.Fatalf("detail_key = %q, want the card's own title key %q", st.DetailKey, keyTitleUpdateProduct)
	}
	if len(res.Proposals) != 1 {
		t.Fatalf("proposals = %+v, want the one token-bearing card", res.Proposals)
	}
	if prose := proseStepFields(t, stepJSON(t, st)); len(prose) != 0 {
		t.Fatalf("the proposed step carries a sentence in %v: %s", prose, stepJSON(t, st))
	}
}

// refusalViaRun drives one tool call through Run — the path the REST edge uses —
// with a scripted model, and returns the refused step plus the model's turns.
func refusalViaRun(tool, args string, caller *fakeCaller) func(t *testing.T) (Step, [][]llm.ChatTurn) {
	return func(t *testing.T) (Step, [][]llm.ChatTurn) {
		t.Helper()
		model := &scriptedLLM{replies: []llm.ChatReply{
			{ToolCalls: []llm.ToolCall{toolCall("c1", tool, args)}},
			{Content: "Maaf, saya tidak boleh melakukan itu."},
		}}
		r := newTestRunner(t, caller, model, nil)
		// An unconfigured feedback service is its own cause (see the case list);
		// every other cause must not be masked by it.
		if tool != ToolSubmitFeedback {
			r.feedback = nil
		}
		res, err := r.Run(chatCtx(), nil, "tolong buat itu")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(res.Steps) != 1 {
			t.Fatalf("steps = %+v, want exactly one", res.Steps)
		}
		return res.Steps[0], model.prompts
	}
}
