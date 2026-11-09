package chat

// The chat's half of card t_52429d6f: before it suggests a sale action, it must
// read the SERVER's answer (available_events), not infer the rule from status.
//
// The chat cannot execute a sale write — every money writer is outside both the
// read allowlist and the proposable set — so there is no dispatch to gate here.
// What it CAN do is advise, and the defect class this closes is advice that the
// server would refuse. The model reads sales through list_sales, which now
// carries available_events on every row; this test pins that the prompt tells it
// to use that list, and that the tool carrying it is still offered.

import (
	"slices"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/mcpsrv"
)

func TestThePromptPointsSaleAdviceAtTheServersAvailableEvents(t *testing.T) {
	prompt := systemPromptText("en")
	if !strings.Contains(prompt, "available_events") {
		t.Fatal("the system prompt never mentions available_events: the model is free to infer sale actions from status, " +
			"which is exactly the guess the lifecycle table replaces")
	}
	if !strings.Contains(prompt, "enabled=false") {
		t.Error("the prompt does not tell the model what enabled=false means, so a disabled action would read as possible")
	}

	// The tool that carries the list must still be readable by the chat, or the
	// instruction above would have no source of truth to read.
	if !slices.Contains(mcpsrv.ChatReadTools(), "list_sales") {
		t.Fatal("list_sales is no longer a chat read tool, so the prompt tells the model to read a field it cannot reach")
	}
	// And no sale writer may become dispatchable by the chat. The allowlist guard
	// already asserts this over the whole registry; this pins the specific names
	// so a future card cannot quietly widen the chat to a money action.
	for _, name := range []string{"confirm_sale", "capture_sale", "release_sale", "refund_sale"} {
		if mcpsrv.ChatToolIsAllowed(name) {
			t.Errorf("%s is dispatchable by the chat — a money action must never be", name)
		}
	}
}
