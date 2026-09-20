package mcpsrv

// The in-app chat's tool exposure (phase 1), declared HERE rather than in the
// chat package so the class guard in scope_gate_guard_test.go can police it with
// the same call-graph it uses for every registered tool.
//
// THE ALLOWLIST IS THE FIRST OF THREE LAYERS that keep the chat read-only:
//
//	1. only these names are offered to the model, and only these names are
//	   dispatched — an invented or injected tool name is refused in the chat
//	   runner (internal/chat) before any call is made;
//	2. every dispatched call runs under an identity whose scopes are READONLY,
//	   so a tool that does reach a store write is refused by requireWriteScope
//	   (issue #645);
//	3. the server re-checks workspace role and ownership per call.
//
// Layer 1 is a JUDGEMENT list, not a computed one. "Every tool that reaches no
// write sink" is a predicate the guard can check (and does — see
// TestChatReadAllowlistContainsNoWriter), but it cannot tell whether a tool is
// SENSIBLE to put in front of a model. list_api_keys and list_passkeys are
// perfect reads and are deliberately absent: a chat question about "my account"
// never needs the credential list, and a tool that returns secret material is
// one prompt-injection away from being read out loud. So the list is written by
// hand and the guard proves the property that IS checkable.

// ChatSubmitFeedbackTool is the ONE write the chat has. It is registered as an
// MCP tool for parity (tools.go) but the chat executes it in-process through
// internal/feedbacksvc, so its model-facing definition does not come from
// tools/list (see internal/chat.toolDefs).
const ChatSubmitFeedbackTool = "submit_feedback"

// chatReadTools is the READ allowlist: tools about the caller's own account, each
// of which reaches no store write. The names must be REGISTERED tools — the guard
// fails on a typo, which is how a silently-absent tool would otherwise become a
// model that keeps answering "I have no tool for that".
var chatReadTools = []string{
	"get_account",
	"list_workspaces",
	"list_forms",
	"get_form",
	"get_form_analytics",
	"list_form_submissions",
	"list_products",
	"list_product_variations",
	"get_product_analytics",
	"list_sales",
	"get_sales_summary",
	"get_sale_email_status",
	"list_subscriptions",
	"list_email_deliveries",
	"list_spam_sales",
	"list_spam_submissions",
	"list_media",
	"list_events",
	"list_templates",
	"list_own_templates",
	"list_feedback",
	"get_feedback_thread",
}

// ChatReadTools returns the chat's read allowlist. A copy, so a caller cannot
// widen the list by mutating the slice it received.
func ChatReadTools() []string {
	out := make([]string, len(chatReadTools))
	copy(out, chatReadTools)
	return out
}

// ChatToolIsAllowed reports whether a name may be dispatched by the chat. It is
// the single predicate both the tool exposure and the dispatch path consult, so
// the offered set and the reachable set cannot diverge.
func ChatToolIsAllowed(name string) bool {
	if name == ChatSubmitFeedbackTool {
		return true
	}
	for _, n := range chatReadTools {
		if n == name {
			return true
		}
	}
	return false
}
