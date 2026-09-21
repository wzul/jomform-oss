package mcpsrv

// The in-app chat's tool exposure (phase 1 read-only; phase 2 propose-only),
// declared HERE rather than in the chat package so the class guard in
// scope_gate_guard_test.go can police it with the same call-graph it uses for
// every registered tool.
//
// THE PHASE-2 MEANING OF "THE CHAT CANNOT WRITE".
//
// Phase 1 meant "no write tool is offered and none can be dispatched". Phase 2
// keeps that for EXECUTION and adds a proposal channel, so the claim is restated
// rather than relaxed:
//
//	the model may PROPOSE any tool in the proposal set;
//	the model may never EXECUTE a write — every dispatch of a write name is
//	refused in the chat runner before any call is made, `confirm=true` inside
//	the model's arguments included (a boolean the model typed proves nothing).
//
// Three layers, in the order a call meets them:
//
//	1. THE ALLOWLIST (ChatToolIsAllowed). Only these names may be dispatched:
//	   the read tools, submit_feedback (executed in-process through
//	   internal/feedbacksvc) and propose_write (which records intent and
//	   executes nothing). A registered tool that reaches a store write is
//	   refused here, so a prompt-injected "call delete_form" has nothing to
//	   call. The guard asserts that direction over the whole registry.
//	2. THE IDENTITY. Every dispatched call runs under an identity whose scopes
//	   are READONLY, whatever the caller's own credential can do. The only
//	   path that runs a tool with a write scope is the CONFIRM path, which
//	   requires an opaque server-issued token bound to (tool, arguments,
//	   caller, expiry) — see internal/chat/proposal.go.
//	3. THE SERVER'S OWN GATES (mcpsrv). Workspace role, ownership and scope are
//	   re-checked per call, exactly as for an external agent. The confirmed call
//	   runs under the HUMAN's identity, so a workspace member who may not write
//	   is refused at confirm time too.
//
// Layer 1 is a JUDGEMENT list, not a computed one. "Every tool that reaches no
// write sink" is a predicate the guard can check (and does — see
// TestChatReadAllowlistContainsNoWriter), but it cannot tell whether a tool is
// SENSIBLE to put in front of a model. list_api_keys and list_passkeys are
// perfect reads and are deliberately absent: a chat question about "my account"
// never needs the credential list, and a tool that returns secret material is
// one prompt-injection away from being read out loud. So the list is written by
// hand and the guard proves the property that IS checkable.

// ChatSubmitFeedbackTool is the chat's in-process filing tool. It is registered
// as an MCP tool for parity (tools.go) but the chat executes it in-process
// through internal/feedbacksvc, so its model-facing definition does not come
// from tools/list (see internal/chat.toolDefs).
const ChatSubmitFeedbackTool = "submit_feedback"

// ChatProposeWriteTool is the chat's in-process PROPOSAL tool (phase 2). The
// model calls it with a tool name and the arguments it wants; the server
// records the proposal and returns an opaque token. It executes NOTHING, which
// is why it is dispatchable while every real writer is not.
//
// It is deliberately NOT registered as an MCP tool. The confirm step is a
// session-only route (a chat is a person's conversation), so a token handed to
// an MCP caller could never be confirmed by one: registering it would ship a
// tool whose only outcome is a dead token. An agent that wants the write calls
// the write tool itself — its own client is the human's confirmation channel.
const ChatProposeWriteTool = "propose_write"

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

// chatProposableTools is the set of WRITE tools the model may PROPOSE. Started
// deliberately small and reversible (issue: phase 2). Each one is a FIELD
// EDIT on an object the caller already owns, and each is undoable by proposing
// the opposite edit — there is no new object to clean up and nothing that
// cannot be put back by the same tool that changed it.
//
//	update_product            name/description/price/stock/active/images/…
//	update_product_variation  the same for one variation
//	set_form_theme            styling/branding fields on one form
//
// WHAT IS EXCLUDED, AND WHY (the classes, not just the names):
//
//   - Destroy / archive / revoke / purge (delete_form, archive_product,
//     revoke_api_key, remove_*): no inverse through any tool the chat can
//     propose. A wrong confirm must be recoverable.
//   - Money movement (refund_sale, confirm_sale, capture_sale, release_sale):
//     moves real money in CHIP, and is not a field edit.
//   - Cancel / retry (cancel_subscription, retry_subscription): stops or
//     re-runs a customer's billing; the consequence lands on a third party.
//   - Credentials and access (create_api_key, revoke_api_key, set_form_password,
//     invite_workspace_member, update_workspace_member_role, *_cloudflare*,
//     webhook secrets): the proposal card cannot show a secret's effect, and a
//     mistaken confirm on an access change locks someone out.
//   - Email that leaves the building (set_email_settings, set_form_email_settings,
//     set_form_notifications, reply_feedback): a confirm would send real mail to
//     real customers, which no chat preview can undo.
//   - Publication state (publish_form, unpublish_form, set_form_restrictions,
//     set_workspace_landing_page, set_menu_page): changes what the public can
//     reach; the dashboard is where the consequence is visible.
//   - Bulk anything (bulk_products, bulk_forms, capture_all_pending_sales): the
//     blast radius is the point of the operation and the card cannot show it.
//   - Create a new object (create_product, create_webhook, upsert_form,
//     use_template): the chat has no delete, so a mistaken confirm leaves
//     litter the user must clean up by hand.
//
// The guard (chat_tools_guard_test.go) asserts this shape: every proposable
// name is a REGISTERED tool that reaches a write sink and the write-scope gate,
// no name carrying a destructive verb is proposable, and the set stays small.
var chatProposableTools = []string{
	"update_product",
	"update_product_variation",
	"set_form_theme",
}

// ChatReadTools returns the chat's read allowlist. A copy, so a caller cannot
// widen the list by mutating the slice it received.
func ChatReadTools() []string {
	out := make([]string, len(chatReadTools))
	copy(out, chatReadTools)
	return out
}

// ChatProposableTools returns the tools the model may propose a write for. A
// copy, for the same reason as ChatReadTools.
func ChatProposableTools() []string {
	out := make([]string, len(chatProposableTools))
	copy(out, chatProposableTools)
	return out
}

// ChatToolIsProposable reports whether a name may be PROPOSED by the chat. It is
// consulted twice on the proposal path — when the model asks for a proposal, and
// again when a token is confirmed — so a proposal that somehow named a tool
// outside this set cannot be executed even if it were somehow recorded.
func ChatToolIsProposable(name string) bool {
	for _, n := range chatProposableTools {
		if n == name {
			return true
		}
	}
	return false
}

// ChatToolIsAllowed reports whether a name may be DISPATCHED by the chat. It is
// the single predicate both the tool exposure and the dispatch path consult, so
// the offered set and the reachable set cannot diverge.
//
// Phase 2 note: the two in-process tools in this set (submit_feedback,
// propose_write) are the exceptions that are not reads, and neither one
// executes a tool the caller names. Nothing that reaches a store write is ever
// allowed here — the guard asserts that over every registered tool.
func ChatToolIsAllowed(name string) bool {
	if name == ChatSubmitFeedbackTool || name == ChatProposeWriteTool {
		return true
	}
	for _, n := range chatReadTools {
		if n == name {
			return true
		}
	}
	return false
}
