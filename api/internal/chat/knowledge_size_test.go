// The guide is paid for on EVERY model call of EVERY message.
//
// The cost arithmetic this pins is the one rest_chat.go states in prose
// (TestModelCallsPerMessageIsMaxStepsPlusOne measures it): a message that works
// costs up to MaxSteps + 1 model calls, and each call carries the whole system
// prompt plus the turns so far. So the guide's size is not a documentation
// quality issue — it is multiplied by five before it reaches a bill, and it
// arrives on every message a merchant sends.
//
// WHY A CAP AND NOT A "KEEP IT SHORT" COMMENT. A comment has no failure mode.
// The guide grew by 25 facts in one change; the next change will want to add
// three more, and without a number the additions are invisible until someone
// reads the bill. The caps below are the number, and the character count is the
// HARD one — the token figure is the conventional English approximation
// (about four characters per token), kept because a reviewer thinks in tokens.
// A model's own tokeniser is not available here and does not need to be: the
// approximation can only be wrong by a constant factor, and the character cap
// already bounds it.
package chat

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
)

const (
	// maxProductGuideChars is the hard budget for the rendered guide block,
	// markers included. Measured at the time of writing: the guide is ~7.0 KB.
	// Raising this is a deliberate, reviewable act (a fact has to be worth its
	// place in every model call), which is the point.
	maxProductGuideChars = 7200

	// maxProductGuideTokens is the same budget in estimated tokens.
	maxProductGuideTokens = 1800

	// maxSystemPromptChars bounds the WHOLE system prompt, which is what is
	// actually sent: the operating instructions (the bulk of it) plus the guide.
	// It must stay comfortably under a small model's context, with room for the
	// turns and the tool results the same request carries.
	maxSystemPromptChars = 16000

	// maxPromptCharsPerMessage is the ceiling on what ONE message may send in
	// system prompts alone: the cap above times the number of model calls one
	// message can make (MaxSteps round-trips plus the final answering call).
	// Measured worst case at the time of writing: ~75 KB (~19 K estimated
	// tokens). It is the number the guide's size has to be argued against.
	maxPromptCharsPerMessage = 80000
)

// estimatedTokens is the conventional English approximation: about four
// characters per token. It is an ESTIMATE — the character counts above and below
// are measurements — and it is used only to make the budget reviewable in the
// unit people reason about.
func estimatedTokens(s string) int {
	return (len(s) + 3) / 4
}

// TestTheProductGuideFitsItsBudget pins the guide's size, in characters (a
// measurement) and in estimated tokens (the readable unit).
func TestTheProductGuideFitsItsBudget(t *testing.T) {
	guide := productGuideText()
	chars := len([]rune(guide))
	tokens := estimatedTokens(guide)
	if chars > maxProductGuideChars {
		t.Errorf("the product guide is %d characters, over its %d-character budget by %d. It is sent on every "+
			"model call (up to MaxSteps+1 per message), so an addition has to be worth that cost — trim the "+
			"fact, or raise maxProductGuideChars deliberately and say why", chars, maxProductGuideChars,
			chars-maxProductGuideChars)
	}
	if tokens > maxProductGuideTokens {
		t.Errorf("the product guide is ~%d estimated tokens, over its %d budget", tokens, maxProductGuideTokens)
	}
	if chars < 1500 {
		t.Fatalf("the product guide renders %d characters — an empty or truncated guide would satisfy the caps "+
			"above while telling the model nothing", chars)
	}
	t.Logf("product guide: %d facts, %d chars, ~%d estimated tokens", len(productFacts), chars, tokens)
}

// TestTheSystemPromptFitsItsBudget bounds the text the model is actually sent.
//
// The prompt is assembled from a template plus the guide, so a cap on the guide
// alone does not bound the prompt: the instructions can grow too, and this is the
// number that decides what a message costs.
func TestTheSystemPromptFitsItsBudget(t *testing.T) {
	prompt := systemPromptText("")
	chars := len([]rune(prompt))
	if chars > maxSystemPromptChars {
		t.Errorf("the system prompt is %d characters, over its %d budget — every message carries it on every "+
			"model call", chars, maxSystemPromptChars)
	}
	if perMessage := chars * (MaxSteps + 1); perMessage > maxPromptCharsPerMessage {
		t.Errorf("the worst case for ONE message is %d characters of system prompt (%d × MaxSteps+1 %d), over "+
			"the %d ceiling. The guide's size is the part that is cheapest to argue with",
			perMessage, chars, MaxSteps+1, maxPromptCharsPerMessage)
	}
	if !strings.Contains(prompt, "HOW JOMFORM WORKS") {
		t.Error("the prompt no longer tells the model it answers product questions — the guide would then be " +
			"reference material the model was never told to use")
	}
	t.Logf("system prompt: %d chars, ~%d estimated tokens, worst case per message ~%d chars (~%d estimated tokens)",
		chars, estimatedTokens(prompt), chars*(MaxSteps+1), estimatedTokens(prompt)*(MaxSteps+1))
}

// TestTheGuideReachesTheModelOnEveryCall is the end-to-end half of the size
// question: the guide is assembled in one place (productGuideText) and injected in
// another (buildTurns), and a mis-wired call site is the classic way a feature is
// perfectly unit-tested and dead. It drives a real Run with a scripted model and
// reads the system turn of the FIRST call and of the call that carries a fenced
// tool result — both must carry the guide, because a model that is given the guide
// only on the first turn answers the follow-up without it.
func TestTheGuideReachesTheModelOnEveryCall(t *testing.T) {
	caller := &fakeCaller{answer: "{}"}
	model := &scriptedLLM{replies: []llm.ChatReply{
		{ToolCalls: []llm.ToolCall{toolCall("c1", "list_forms", `{}`)}},
		{Content: "Saya jumpa 2 borang."},
	}}
	r := newTestRunner(t, caller, model, nil)
	if _, err := r.Run(chatCtx(), nil, "macam mana nak tetapkan caj pemprosesan kepada pembeli?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(model.prompts) == 0 {
		t.Fatal("the model was never called")
	}
	guide := productGuideText()
	for i, turns := range model.prompts {
		if len(turns) == 0 || turns[0].Role != "system" {
			t.Fatalf("call %d did not start with a system turn", i)
		}
		if !strings.Contains(turns[0].Content, guide) {
			t.Fatalf("call %d's system turn does not carry the product guide verbatim. The guide is assembled "+
				"by productGuideText and injected by buildTurns; a mismatch here means the model answers "+
				"product questions without it", i)
		}
	}
}

// TestTheGuideIsTheOnlyCopyOfItsFacts guards the single-source rule the rest of
// this package follows: the facts live in productFacts, and the prompt template
// carries a PLACEHOLDER, not a second copy of the text. A copy in prompt.go would
// pass every provenance check (the copy is not checked) while being the version
// the model reads.
func TestTheGuideIsTheOnlyCopyOfItsFacts(t *testing.T) {
	if !strings.Contains(systemPromptTemplate, "{{GUIDE}}") {
		t.Fatal("the prompt template no longer carries the {{GUIDE}} placeholder")
	}
	for _, f := range productFacts {
		if strings.Contains(systemPromptTemplate, f.Text) {
			t.Errorf("the prompt template carries fact %s's text directly — it belongs in knowledge.go only, "+
				"where its provenance is checked", f.ID)
		}
	}
	// And the placeholder really is substituted: a literal {{GUIDE}} in the
	// rendered prompt would be read by the model as content.
	if strings.Contains(systemPromptText(""), "{{GUIDE}}") {
		t.Error("the rendered system prompt still contains the {{GUIDE}} placeholder")
	}
}
