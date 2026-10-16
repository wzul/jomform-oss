// The chat's per-message COST is arithmetic on two constants: the message rate
// (chatRatePerHour, rest_chat.go) times the MODEL CALLS one message can make.
//
// That second number is MaxSteps + 1, and it is MEASURED here rather than read off
// the loop, because prose turns on it: rest_chat.go's COST comment multiplies the
// message rate by that count, and the /docs/chat "Limits and cost" section states
// the same count as "up to five". The trap is a UNIT one — MaxSteps caps tool
// ROUND-TRIPS, while the runner calls the model once per round-trip AND once more
// with no tools, so the user gets an answer built from what was gathered (the loop
// at chat.go:378, and chat.go:494 on its last step).
//
// TestStepBudgetEndsWithAnAnswer next door covers the END of that budget (the final
// round-trip is offered no tools, and the user still gets an answer). This covers
// its SIZE: how many ChatWithTools calls one message that loops on tools costs.
package chat

import (
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
)

func TestModelCallsPerMessageIsMaxStepsPlusOne(t *testing.T) {
	caller := &fakeCaller{answer: "{}"}
	// Exactly MaxSteps+1 replies: the script is exhausted the moment the runner
	// asks one time too many, so this test cannot pass by the runner quietly
	// asking a time it did not count.
	replies := make([]llm.ChatReply, 0, MaxSteps+1)
	for i := 0; i < MaxSteps; i++ {
		replies = append(replies, llm.ChatReply{
			ToolCalls: []llm.ToolCall{toolCall("c", "list_forms", `{}`)},
		})
	}
	replies = append(replies, llm.ChatReply{Content: "Ringkasan daripada apa yang saya jumpa."})
	model := &scriptedLLM{replies: replies}
	r := newTestRunner(t, caller, model, nil)

	if _, err := r.Run(chatCtx(), nil, "senaraikan semuanya"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(model.prompts); got != MaxSteps+1 {
		t.Fatalf("a message that calls a tool on EVERY round-trip cost %d model calls, want %d "+
			"(MaxSteps %d tool round-trips, plus the final tool-less call that answers). "+
			"rest_chat.go's COST comment and the /docs/chat \"Limits and cost\" section both "+
			"state this count as the per-message cost", got, MaxSteps+1, MaxSteps)
	}
	if got := len(caller.calls); got != MaxSteps {
		t.Fatalf("the same message dispatched %d tool calls, want exactly MaxSteps (%d): the +1 "+
			"above is a MODEL call, not another round-trip — the two units differ, which is the "+
			"whole reason the per-message cost is %d and not %d", got, MaxSteps, MaxSteps+1, MaxSteps)
	}
}
