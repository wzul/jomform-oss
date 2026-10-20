package chat

// A REFUSED PROPOSAL MUST NOT END THE TURN IN SILENCE (card t_7f1694cb).
//
// THE DEFECT, measured on prod v1.21.149 by the QA lane and reproduced below
// against THIS tree. A merchant typed, in English, on an English dashboard:
//
//	Create a form called My New Form with one text field for the customer's name.
//
// and got back `reply: ""` — no answer at all — plus a block reading
//
//	That action is not allowed here.
//	propose_write — The assistant could not prepare that change, so nothing was
//	                prepared for you to confirm.
//
// twice. The transcript's only content was the refusal, and its heading told the
// merchant the ACTION was not allowed when in fact the action is proposable and
// only the ARGUMENTS were refused.
//
// TWO HALVES, and this file owns the server half of both:
//
//  1. NO PROSE IS NOT AN ANSWER. The proposal lane hands its objection to the
//     MODEL (that is correct — the model is the one that can correct the call),
//     and the model is free to answer with nothing at all, which is exactly what
//     the live run did. The runner must not pass that silence through to the
//     person: when the model writes no prose, the result carries the KEY of the
//     sentence the dashboard renders in the caller's own language. It is a key
//     and not a sentence for the reason every refusal is (t_3839995c): the API
//     never receives the caller's language (issue #6177), so a sentence built
//     here reaches all five locales in one language.
//
//  2. THE MODEL IS TOLD TO RECOVER. The refusal is teachable only if the model
//     is told what to do with it, and "answer anyway" is part of that: the system
//     prompt now says that a refused proposal must still end with an answer in
//     the user's language, never with silence.
//
// WHY THE REPRODUCTION BUILDS THE REAL REGISTRY. The refusal is driven through the
// REAL MCP server and the REAL in-memory session (mcpsrv.New over empty Deps, the
// QA probe's own path), so the schema that refuses is the one a merchant's
// deployment would use, not a hand-written copy of it — a test that asserted
// against its own schema could pass while the registered tool accepted something
// else. The arguments below (`config` with no `title`) are refused by the
// registered upsert_form schema, which is the class the live failure belonged to:
// arguments that follow the model's reading of the tool and not the tool's
// declared contract.
//
// WHAT IS PINNED, and why each half is needed:
//
//	TestRefusedProposalStillAnswersTheUser   the reported turn: two refusals, an
//	  empty final message, and the result still carrying an answer for the person.
//	TestReplyProseIsPreferredOverTheFallback  the control: a model that DID answer
//	  keeps its own words and gets no fallback key, so the fix cannot overwrite a
//	  real answer with a canned sentence.
//	TestStepBudgetFinalAnswerIsNeverEmptyEither  the OTHER way a turn can end
//	  (the tool-step budget exhausted) obeys the same rule, because a fix applied
//	  at one exit point only is the shape this repo has been bitten by before.
//	TestSystemPromptTellsTheModelToAnswerAfterARefusal  the model half: the rule
//	  is in the prompt the model actually receives, not only in a comment.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// realRegistryRunner builds a Runner over the REAL MCP server and its in-memory
// session, with a scripted model — the production path for everything except the
// model itself. No DB is touched: the proposal lane refuses the arguments before
// it looks at the store.
func realRegistryRunner(t *testing.T, model *scriptedLLM) *Runner {
	t.Helper()
	r := &Runner{llm: model, mcp: mcpsrv.New(&mcpsrv.Deps{}, nil, nil, nil)}
	r.newSession = r.mcpSession
	return r
}

// TestRefusedProposalStillAnswersTheUser is the reported defect, end to end at the
// level the REST edge consumes: Result.Reply and Result.ReplyKey.
func TestRefusedProposalStillAnswersTheUser(t *testing.T) {
	// Arguments the registered upsert_form schema refuses: `config` is fine, but
	// `title` is required and absent. This is the natural-request shape — a model
	// that understood the request and under-specified the call.
	const refusedArgs = `{"tool":"upsert_form","arguments":{"config":{}},"reason":"cipta borang"}`
	model := &scriptedLLM{replies: []llm.ChatReply{
		{ToolCalls: []llm.ToolCall{toolCall("c1", ToolProposeWrite, refusedArgs)}},
		{ToolCalls: []llm.ToolCall{toolCall("c2", ToolProposeWrite, refusedArgs)}},
		// The live model's last round-trip: no tools, and nothing written. This
		// is the silence the card was filed on.
		{Content: ""},
	}}
	r := realRegistryRunner(t, model)

	res, err := r.Run(chatCtx(), nil, "Create a form called My New Form with one text field for the customer's name.")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The refusal itself is unchanged and still visible: two refused steps, each
	// naming the proposal lane's own key.
	if len(res.Steps) != 2 {
		t.Fatalf("steps = %+v, want the two refused propose_write calls", res.Steps)
	}
	for i, st := range res.Steps {
		if st.Status != StepRefused || st.DetailKey != keyRefusedProposal {
			t.Fatalf("step %d = %+v, want refused/%s", i, st, keyRefusedProposal)
		}
	}

	// THE FIX. The model wrote nothing, so the person must still get an answer:
	// prose, or the key of one. Both empty is the defect.
	if strings.TrimSpace(res.Reply) == "" && res.ReplyKey == "" {
		t.Fatalf("the turn ended with no answer for the user (reply=%q, reply_key=%q) — "+
			"two refused proposals and an empty final message is exactly the live shape: the "+
			"merchant saw only the refusal block (t_7f1694cb)", res.Reply, res.ReplyKey)
	}
	if res.ReplyKey != keyReplyNoAnswer {
		t.Fatalf("reply_key = %q, want %q — the dashboard resolves it in the caller's language; "+
			"the API does not know that language (issue #6177)", res.ReplyKey, keyReplyNoAnswer)
	}
	if res.Reply != "" {
		t.Fatalf("reply = %q, want empty: the model wrote nothing, so the sentence must come from "+
			"the dictionary rather than be invented here", res.Reply)
	}
	if !strings.HasPrefix(keyReplyNoAnswer, "chat.reply.") {
		t.Fatalf("%q is not a chat.reply.* key — web/src/lib/chatAnswer.test.ts extracts exactly that "+
			"prefix from this file to prove the dashboard has the sentence in all five locales",
			keyReplyNoAnswer)
	}

	// The MODEL's half is not degraded by the fix: the refusal it read back named
	// the tool and the schema's objection, so a model that wants to can correct
	// the call on the next step.
	if got := lastToolTurn(model); !strings.Contains(got, ToolProposeWrite) ||
		!strings.Contains(got, "title") {
		t.Fatalf("the model's last tool result does not carry the correction cue: %q", got)
	}
}

// TestReplyProseIsPreferredOverTheFallback is the control for the fix. A model that
// answered must keep its own words: a canned sentence in place of a real answer
// would be a worse product than the bug.
func TestReplyProseIsPreferredOverTheFallback(t *testing.T) {
	const prose = "Saya tidak dapat menyediakan borang itu. Beritahu saya nama borang dan medan yang anda mahu."
	model := &scriptedLLM{replies: []llm.ChatReply{
		{ToolCalls: []llm.ToolCall{toolCall("c1", ToolProposeWrite,
			`{"tool":"upsert_form","arguments":{"config":{}},"reason":"cipta borang"}`)}},
		{Content: prose},
	}}
	r := realRegistryRunner(t, model)

	res, err := r.Run(chatCtx(), nil, "Create a form called My New Form.")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reply != prose {
		t.Fatalf("reply = %q, want the model's own words %q", res.Reply, prose)
	}
	if res.ReplyKey != "" {
		t.Fatalf("reply_key = %q, want empty — a real answer must not be replaced by the fallback",
			res.ReplyKey)
	}
}

// TestStepBudgetFinalAnswerIsNeverEmptyEither covers the OTHER exit from Run: the
// model spends the tool-step budget and its final (tool-less) round-trip is empty.
func TestStepBudgetFinalAnswerIsNeverEmptyEither(t *testing.T) {
	caller := &fakeCaller{answer: "{}"}
	replies := make([]llm.ChatReply, 0, MaxSteps+1)
	for i := 0; i < MaxSteps; i++ {
		replies = append(replies, llm.ChatReply{
			ToolCalls: []llm.ToolCall{toolCall("c", "list_forms", `{}`)},
		})
	}
	replies = append(replies, llm.ChatReply{Content: "   "})
	model := &scriptedLLM{replies: replies}
	r := newTestRunner(t, caller, model, nil)

	res, err := r.Run(chatCtx(), nil, "senaraikan semuanya")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ReplyKey != keyReplyNoAnswer {
		t.Fatalf("reply_key = %q, want %q — the step-budget exit needs the same answer as the "+
			"normal one; a fix at one exit point only is the shape this repo keeps re-finding",
			res.ReplyKey, keyReplyNoAnswer)
	}
}

// TestSystemPromptTellsTheModelToAnswerAfterARefusal pins the model half: the
// refusal is a dead end only if the model does not know it must still answer.
func TestSystemPromptTellsTheModelToAnswerAfterARefusal(t *testing.T) {
	prompt := systemPromptText()
	if !strings.Contains(prompt, "propose_write is refused") {
		t.Fatalf("the system prompt says nothing about a refused propose_write — the model is left " +
			"to decide on its own, and on prod it decided to say nothing (t_7f1694cb)")
	}
	if !strings.Contains(prompt, "A refused proposal is never a reason to go quiet") {
		t.Fatalf("the system prompt does not tell the model that silence is not an answer:\n%s", prompt)
	}
	if !strings.Contains(prompt, "never leave the user with only a refusal") {
		t.Fatalf("the system prompt does not name the shape the defect took (a refusal and nothing "+
			"else):\n%s", prompt)
	}
}

// lastToolTurn returns the content of the last tool turn the model was handed in
// its final round-trip.
func lastToolTurn(model *scriptedLLM) string {
	if len(model.prompts) == 0 {
		return ""
	}
	turns := model.prompts[len(model.prompts)-1]
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "tool" {
			return turns[i].Content
		}
	}
	return ""
}
