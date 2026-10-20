// In-app chat (phase 1): POST /v1/rest/chat.
//
// READ-ONLY, PLUS ONE WRITE. The endpoint answers questions about the caller's
// own account data and can file feedback. It cannot do anything else — and that
// is enforced by the identity it hands to the chat runner, not by the prompt:
//
//	Scopes: []string{dbstore.ScopeReadonly}
//
// The runner dispatches tool calls over the real MCP server, so every write tool
// answers "scope tidak mencukupi" (issue #645's gate) even though the caller's
// own session could do that write themselves on the dashboard. The point is not
// the caller's permission: it is that a sentence typed into a chat box must not
// be able to delete a form.
//
// SESSION-ONLY. An API key is refused: a chat conversation belongs to a PERSON,
// and the one write (file feedback) is filed on that person's behalf — the same
// reason the feedback tools refuse an API key (see mcpsrv/feedback_mcp.go).
//
// RATE LIMITED PER USER, because every message is at least one LLM call and up
// to chat.MaxSteps + 1 of them: one call per tool round-trip, plus the call that
// answers from what the round-trips gathered. A message limit alone would
// understate the cost, so the limit below is stated alongside the step cap it
// multiplies with. A rate limit is the right guard here for the same reason it
// is on the feedback lane: it delays a loud user rather than misjudging one.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

const (
	// chatRatePerHour / chatBurst bound ONE user's chat messages. 60/hour is a
	// minute-by-minute conversation with room to spare; the burst of 10 covers
	// the "ask, read, ask again" rhythm of a single sitting without making the
	// first messages of a conversation wait.
	//
	// The COST bound is this times (chat.MaxSteps + 1) model calls per message —
	// one call per tool round-trip, plus the one that answers from what they
	// gathered. So at most 300 model calls per user per hour, each capped at
	// chat.MaxToolResultChars of context per tool.
	//
	// WHAT THE DOCS PAGE CARRIES, next to the feature: the messages per hour
	// above, the model calls one message may take, and — since t_902ab8bb — the
	// per-tool cap on chat.MaxToolResultChars. So no limit a user meets is a
	// surprise. The per-hour MODEL-CALL TOTAL is not on that page: it is
	// arithmetic on the two figures the page does state, and this comment is the
	// only place the total is written down, so do not claim otherwise here.
	chatRatePerHour = 60.0
	chatBurst       = 10.0
	// chatMaxHistory is the number of prior turns the client may send. The
	// runner keeps the LAST chat.MaxTurns of them; the edge rejects an
	// implausibly long transcript rather than silently trimming a client's
	// memory of the conversation.
	chatMaxHistory = 40
	// chatConfirmPerHour / chatConfirmBurst bound ONE user's proposal
	// confirmations (phase 2). Confirming costs no model call, so the bound is
	// not about spend — it is about how fast a person can be walked through
	// decision cards, which is the failure mode the whole design exists to
	// prevent. 30/hour in bursts of 5 is a person deliberately approving changes
	// with room to spare, and slow enough that a script driving the UI is
	// throttled while a real merchant is not.
	chatConfirmPerHour = 30.0
	chatConfirmBurst   = 5.0
)

// chatLimiter is the per-user bucket. It is built once per server (see
// NewServer) so the limit is shared across requests; nil in bare-mode tests,
// where the limit is not exercised.
var _ = chatRatePerHour

type chatRequestIn struct {
	Message string      `json:"message"`
	History []chat.Turn `json:"history"`
}

type chatResponseOut struct {
	Reply string `json:"reply"`
	// ReplyKey names a dictionary sentence for the ANSWER when the model wrote none
	// of its own (t_7f1694cb) — the page resolves it in the caller's language, which
	// this handler does not know. Empty whenever Reply carries prose.
	ReplyKey string      `json:"reply_key,omitempty"`
	Steps    []chat.Step `json:"steps"`
	Filed    *chat.Filed `json:"filed,omitempty"`
	// Proposals are the confirmation cards this exchange produced (phase 2). The
	// TOKEN they carry goes to the browser and is never sent to the model; without
	// a click on one of these cards, no proposed write can happen.
	Proposals []chat.ProposalCard `json:"proposals,omitempty"`
	// Limits echoes the caps in force, so the UI can explain a 429 instead of
	// showing a bare error.
	Limits chatLimitsOut `json:"limits"`
}

type chatLimitsOut struct {
	MessagesPerHour int `json:"messages_per_hour"`
	Burst           int `json:"burst"`
	MaxMessageChars int `json:"max_message_chars"`
	MaxHistoryTurns int `json:"max_history_turns"`
	MaxToolSteps    int `json:"max_tool_steps"`
	// Phase 2: how many confirmation cards one answer may contain, how long a
	// card stays confirmable, and how many confirmations a user may make.
	MaxProposalsPerMessage int `json:"max_proposals_per_message"`
	ProposalTTLSeconds     int `json:"proposal_ttl_seconds"`
	ConfirmsPerHour        int `json:"confirms_per_hour"`
	ConfirmBurst           int `json:"confirm_burst"`
}

// chatConfirmRequestIn is the confirm request. It carries the token and the
// ARGUMENT HASH the card showed.
//
// There is no `confirm: true` field, and no argument the caller can set to make
// the write happen: the token IS the authorisation, and the hash is what binds the
// execution to the arguments the user was shown. A model that could reach this
// endpoint still could not complete it without a token it was never given.
type chatConfirmRequestIn struct {
	Token    string `json:"token"`
	ArgsHash string `json:"args_hash"`
}

type chatConfirmResponseOut struct {
	Status string `json:"status"`
	Tool   string `json:"tool"`
	Result string `json:"result"`
}

// chatAvailable reports whether the assistant can actually answer: a runner is
// wired AND it reports itself ready (a model with a credential).
//
// WHY NOT `h.Chat != nil`. A runner OBJECT exists even when no LLM key is
// configured, so the nil check answered "did we construct the struct", not "can
// this deployment answer a message". The UI hides the box on available:false,
// and that is the right behaviour for a deployment whose every message would
// otherwise die — so the flag has to mean the same thing the UI believes it
// means. ChatRunner.Ready is where the runner answers that question, and it is
// part of the interface so no implementation can skip it.
func (h *RESTHandlers) chatAvailable() bool {
	return h.Chat != nil && h.Chat.Ready()
}

// handleChatStatus: GET /v1/rest/chat — is the assistant available, and what are
// its limits? The UI asks before showing the box, so a deployment without a
// usable LLM credential hides the feature instead of offering a box that errors.
//
// The workspace scope must be resolvable here too (see chatWorkspaceScopeOr403):
// the availability answer is per deployment, but the endpoint belongs to the chat
// lane, and an unscoped call from a multi-workspace session is the same silent
// guess wherever it is made.
func (h *RESTHandlers) handleChatStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.sessionOwnerID(r) == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	if _, ok := h.chatWorkspaceScopeOr403(w, r, actx); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": h.chatAvailable(),
		"limits":    chatLimits(),
		"read_only": true,
	})
}

// chatWorkspaceScopeOr403 refuses a chat call whose account would be GUESSED, and
// hands back the account set the call resolved to.
//
// The scope is returned, not just the verdict, because the tools a chat message
// dispatches must run under EXACTLY the workspace this request resolved (see
// chatContextFor): the REST edge already resolved ?workspace= correctly, and
// handing the tool lane nothing but the identity's account id is what let every
// tool read and write the union of all the user's accounts (issue #747).
//
// WHY THE CHAT NEEDS THIS AND THE REST OF THE DASHBOARD DOES NOT. Every dashboard
// page resolves its account from ?workspace=; the chat used to send none, and the
// unscoped fallback in resolveSessionAccount picks Accounts.ByOwner(...)[0] — the
// user's FIRST owned account, not the one selected in the Shell. Phase 1 that read
// the wrong account; phase 2 CONFIRMS writes against it, so a merchant with two
// workspaces is shown a card naming one account's product and, by confirming it,
// edits that account. `roleAtLeast("")==true` makes it worse rather than better:
// an unscoped request has no role, so the write gate passes.
//
// The chat is SESSION-ONLY (an API key is refused by handleChat), so the documented
// "legacy unscoped callers / API-key path" exception in resolveSessionAccount does
// not apply to it — there is no legitimate unscoped chat caller to preserve.
//
// A session with exactly one accessible workspace is NOT refused: there the
// unscoped account and the scoped account are the same row, so refusing would be
// ceremony (and would break a single-workspace merchant on a stale client).
//
// It is applied to ALL FOUR chat routes — GET status, POST message, POST confirm,
// POST cancel — because they are one lane: a decision endpoint left unscoped would
// be the same defect with a narrower door.
func (h *RESTHandlers) chatWorkspaceScopeOr403(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (*scopeResult, bool) {
	return h.requireResolvedWorkspaceScope(w, r, actx)
}

// handleChat: POST /v1/rest/chat — answer one message.
func (h *RESTHandlers) handleChat(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	// Session only. A chat is a person's conversation, and the one write it has
	// (file feedback) is filed for that person — an API key carries no person
	// (see mcpsrv/feedbackScopeFor for the same argument).
	//
	// Auth comes BEFORE the availability check on purpose: whether this
	// deployment has a usable model is not an anonymous caller's business, and
	// the status endpoint that reports it requires a session too.
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized,
			"chat memerlukan sesi pengguna — log masuk untuk menggunakannya")
		return
	}
	if actx == nil || actx.Account == nil {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	// Refuse a GUESSED account before anything else: the message's tools read
	// under this identity, so an unresolved scope would answer questions about a
	// workspace the user is not looking at (see chatWorkspaceScopeOr403). The
	// scope it returns is the account set those tools must run under.
	scope, ok := h.chatWorkspaceScopeOr403(w, r, actx)
	if !ok {
		return
	}

	var in chatRequestIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
		return
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		writeErr(w, http.StatusBadRequest, "mesej wajib")
		return
	}
	if len([]rune(msg)) > chat.MaxMessageChars {
		writeErr(w, http.StatusBadRequest, "mesej terlalu panjang")
		return
	}
	if len(in.History) > chatMaxHistory {
		writeErr(w, http.StatusBadRequest, "sejarah perbualan terlalu panjang")
		return
	}

	// The same flag GET reports: no runner, or a runner whose model has no
	// credential, is "the assistant is unavailable". Checked AFTER the request
	// shape (a malformed request is a 400 whatever the model's state) and BEFORE
	// the limiter — a deployment that cannot answer must not spend a user's
	// allowance, and the message that comes back is the one the page already
	// handles (503 → setAvailable(false)).
	if !h.chatAvailable() {
		writeErr(w, http.StatusServiceUnavailable, "pembantu chat tidak tersedia")
		return
	}

	// Per-user limit, counted per message. Keyed on the USER (not the IP): a
	// household or office behind one address must not share an allowance, and a
	// user with several tabs must not multiply theirs.
	if h.ChatLimiter != nil && !h.ChatLimiter.Allow("chat|"+uid) {
		writeErr(w, http.StatusTooManyRequests,
			fmt.Sprintf("had chat dicapai (%.0f mesej sejam). Sila cuba sebentar lagi.", chatRatePerHour))
		return
	}

	// The identity the tool calls run under. THIS is what makes the chat
	// read-only: whatever the session could do itself, the tools it can reach
	// are dispatched with a readonly scope, so every write refuses. The scope is
	// the workspace this request resolved, so those tools also read and write the
	// workspace the user is looking at (issue #747).
	ctx := chatContextFor(r.Context(), scope, actx.Account.ID, uid)

	res, err := h.Chat.Run(ctx, in.History, msg)
	if err != nil {
		// A model/transport failure is not the user's fault and not fixable by
		// retyping: say the assistant is unavailable rather than "invalid
		// request". A credential the provider REJECTED is the same answer — the
		// deployment has no working model — so both land on 503 and the detail
		// is logged for the operator, never returned.
		respondAIErr(w, r, err, "pembantu chat tidak tersedia")
		return
	}
	h.recordChatEvent(r, actx, res)
	writeJSON(w, http.StatusOK, chatResponseOut{
		Reply: res.Reply, ReplyKey: res.ReplyKey, Steps: res.Steps, Filed: res.Filed,
		Proposals: res.Proposals, Limits: chatLimits(),
	})
}

// handleChatConfirm: POST /v1/rest/chat/confirm — the HUMAN's half of phase 2.
//
// This is the ONLY endpoint in the product where a chat can cause a write, and it
// is reachable only with a token the server issued to THIS user and bound to the
// arguments they were shown. Everything the model controls is on the other side of
// that token:
//
//   - the token is opaque and single use (chat.ChatWriteProposalStore.Claim),
//   - the arguments EXECUTED are the stored ones, and the request must echo the
//     args_hash the card carried (so a changed argument set is refused unused),
//   - the identity the tool runs under is the HUMAN's, with the write scope, and
//     the tool's own workspace-role/ownership gates apply unchanged,
//   - the per-user confirmation bucket bounds how fast a person can be walked
//     through cards.
//
// Session-only, like the rest of the chat: a proposal belongs to a person.
func (h *RESTHandlers) handleChatConfirm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized,
			"pengesahan memerlukan sesi pengguna — log masuk untuk menggunakannya")
		return
	}
	if actx == nil || actx.Account == nil {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	// The confirmation writes with THE ACCOUNT THIS REQUEST RESOLVED, so an
	// unresolved scope here is a write to the wrong workspace — the worst version
	// of the chat's scope defect. Refused before the token is even read. The
	// returned scope is what makes the confirmed call TARGET that workspace too:
	// the write gate was already scoped here, the write TARGET was not
	// (issue #747).
	scope, ok := h.chatWorkspaceScopeOr403(w, r, actx)
	if !ok {
		return
	}
	if h.Chat == nil {
		writeErr(w, http.StatusServiceUnavailable, "pembantu chat tidak tersedia")
		return
	}
	var in chatConfirmRequestIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
		return
	}
	if strings.TrimSpace(in.Token) == "" {
		writeErr(w, http.StatusBadRequest, "token cadangan diperlukan")
		return
	}
	if strings.TrimSpace(in.ArgsHash) == "" {
		// Without the hash there is nothing that binds the execution to what the
		// user read, so the request is refused as malformed rather than treated as
		// "no change requested".
		writeErr(w, http.StatusBadRequest, "args_hash diperlukan untuk mengikat cadangan")
		return
	}
	if h.ChatConfirmLimiter != nil && !h.ChatConfirmLimiter.Allow("chatconfirm|"+uid) {
		writeErr(w, http.StatusTooManyRequests,
			fmt.Sprintf("had pengesahan dicapai (%.0f sejam). Sila cuba sebentar lagi.", chatConfirmPerHour))
		return
	}

	// The identity the CONFIRMED tool runs under: the HUMAN's, with the write
	// scope. The confirmed call is the one place the chat gains a write, and it is
	// granted here — for a single bound proposal — rather than by loosening the
	// read-only identity every message runs under. The workspace scope rides with
	// it, so the write can only ever land in the workspace the user selected
	// (issue #747: the gate was scoped, the target was not).
	writeCtx := chatConfirmContextFor(r.Context(), scope, actx.Account.ID, uid)

	res, err := h.Chat.Confirm(r.Context(), writeCtx, uid, in.Token, in.ArgsHash)
	if err != nil {
		h.recordChatConfirmEvent(r, actx, uid, in.Token, "", false, err)
		writeErr(w, chatConfirmStatusFor(err), chatConfirmMessageFor(err))
		return
	}
	h.recordChatConfirmEvent(r, actx, uid, in.Token, res.Tool, true, nil)
	writeJSON(w, http.StatusOK, chatConfirmResponseOut{
		Status: "done", Tool: res.Tool, Result: res.Result,
	})
}

// handleChatCancel: POST /v1/rest/chat/cancel — the user's "no".
//
// A cancel is not the UI hiding a card. It SPENDS the server-side token (the same
// atomic claim a confirm uses), so:
//
//   - the proposal can never be confirmed afterwards, by anyone, and
//   - a cancelled token and a confirmed one are equally dead.
//
// Client-side-only cancelling would leave a live authorisation sitting in a
// browser tab that the user believes they declined, which is worse than not
// offering the button.
//
// Idempotent in effect: cancelling a spent proposal answers the same "already
// used" shape a second confirm would. That is deliberate — the user pressing the
// button twice must not look like a failure.
func (h *RESTHandlers) handleChatCancel(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized,
			"membatalkan cadangan memerlukan sesi pengguna — log masuk untuk menggunakannya")
		return
	}
	// The same scope rule as the confirm: a decision on a card belongs to the
	// workspace it was prepared in, and an unscoped request cannot say which one
	// that is. Refused for the SAME reason as a confirm so the three routes cannot
	// drift apart (see chatWorkspaceScopeOr403).
	if _, ok := h.chatWorkspaceScopeOr403(w, r, actx); !ok {
		return
	}
	if h.Chat == nil {
		writeErr(w, http.StatusServiceUnavailable, "pembantu chat tidak tersedia")
		return
	}
	var in chatConfirmRequestIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
		return
	}
	if strings.TrimSpace(in.Token) == "" {
		writeErr(w, http.StatusBadRequest, "token cadangan diperlukan")
		return
	}
	// The cancel counts against the same per-user bucket as a confirm: both are
	// decisions on a card, and a script driving the UI must be throttled either
	// way.
	if h.ChatConfirmLimiter != nil && !h.ChatConfirmLimiter.Allow("chatconfirm|"+uid) {
		writeErr(w, http.StatusTooManyRequests,
			fmt.Sprintf("had pengesahan dicapai (%.0f sejam). Sila cuba sebentar lagi.", chatConfirmPerHour))
		return
	}
	if err := h.Chat.Cancel(r.Context(), uid, in.Token); err != nil {
		h.recordChatConfirmEvent(r, actx, uid, in.Token, "", false, err)
		writeErr(w, chatConfirmStatusFor(err), chatConfirmMessageFor(err))
		return
	}
	h.recordChatConfirmEvent(r, actx, uid, in.Token, "", true, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// chatConfirmStatusFor maps a confirm failure onto an HTTP status. The
// distinction matters to the UI: a token that is spent or expired needs the card
// replaced (409 — the user must ask again), while a malformed request is a 400.
//
// A token that is not this caller's is deliberately a 404 that is IDENTICAL to a
// token that was never issued: the store scopes every read by user id, so this
// endpoint cannot be used to probe for other people's proposals.
func chatConfirmStatusFor(err error) int {
	switch {
	case errors.Is(err, dbstore.ErrChatProposalConsumed),
		errors.Is(err, dbstore.ErrChatProposalExpired),
		errors.Is(err, chat.ErrProposalArgsChanged):
		return http.StatusConflict
	case errors.Is(err, chat.ErrProposalArgsInvalid):
		// The stored arguments are not one the tool accepts, refused BEFORE the
		// claim (chat.Runner.Confirm). It is a CONFLICT and not a 400: the
		// request the user sent is well formed — what has changed is the
		// proposal's own fitness, which the user resolves by having the
		// assistant prepare a fresh card, exactly like a spent or expired token.
		return http.StatusConflict
	case errors.Is(err, dbstore.ErrChatProposalNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

// chatConfirmMessageFor turns a confirm failure into copy the user can act on.
// Nothing internal is returned: the details are the server's business.
//
// THE ROLE REFUSAL NAMES ITS CAUSE (card t_1900adef). It is the one post-claim
// refusal this lane can classify, and it answers the GATE'S OWN sentence
// (mcpsrv.RoleManageRequiredMessage, referenced by name so it cannot drift) rather
// than the generic line. Two consequences, both intended:
//
//   - the wire names the cause for the callers who read prose (curl, API keys,
//     MCP), and
//   - the message resolves to the code `role.manageRequired` through errcode.go's
//     prefix rule, which is what lets the dashboard render its OWN localized
//     sentence for the refusal instead of a generic failure — and, for a spent
//     token, one that does not advise a retry that cannot work.
//
// Nothing else is surfaced: a refusal this lane cannot classify keeps the generic
// sentence and the untranslatable class code, because naming a cause the server
// did not give would be worse than naming none.
func chatConfirmMessageFor(err error) string {
	switch {
	case errors.Is(err, dbstore.ErrChatProposalConsumed):
		return "cadangan itu sudah digunakan atau dibatalkan — minta pembantu menyediakan semula"
	case errors.Is(err, dbstore.ErrChatProposalExpired):
		return "cadangan itu telah luput — minta pembantu menyediakan semula"
	case errors.Is(err, chat.ErrProposalArgsChanged):
		return "cadangan berubah sejak dipaparkan — sahkan semula dari kad asal"
	case errors.Is(err, chat.ErrProposalArgsInvalid):
		// NOT "try again": the token is UNSPENT, and the same card can never
		// succeed. The remedy is a fresh proposal, and the copy says so.
		return "cadangan itu tidak boleh dilaksanakan — minta pembantu menyediakan kad baharu"
	case errors.Is(err, chat.ErrProposalRoleRefused):
		// The workspace ROLE gate refused the confirmed write, AFTER the token was
		// claimed. The gate's own wording, so the reason the card was refused is
		// the reason the user is shown.
		return mcpsrv.RoleManageRequiredMessage
	case errors.Is(err, dbstore.ErrChatProposalNotFound):
		return "cadangan tidak dijumpai"
	default:
		return "cadangan tidak dapat dilaksanakan"
	}
}

// chatAuditAccountID is the account a chat-lane audit row is filed on: the account
// THIS REQUEST RESOLVED (actx.Account), never Accounts.ByOwner(...)[0].
//
// THE DEFECT (card t_fdb74390). Both chat recorders asked
// Accounts.ByOwner(r.Context(), uid) and took [0], i.e. the user's FIRST owned
// account (ordering key accounts.created_at), while every other part of the lane
// had already resolved ?workspace= correctly — the identity the tools run under,
// the account a proposal is stored on and the account a confirmed write lands in.
// So a conversation held under ?workspace=<second> filed its chat.message and
// chat.proposal_confirmed rows inside the FIRST owned workspace's account:
// events.account_id is what the account-scoped Activity history is read through,
// so the second merchant could not see their own conversation at all and the
// first workspace's history showed a chat that never happened there.
//
// WHY THE RESOLVED ACCOUNT. ?workspace= is the user's own selection, resolved by
// resolveSessionAccount against the workspaces they can reach (owner OR member)
// and gated by requireResolvedWorkspaceScope before any row is written — an
// unresolvable or ambiguous scope is a 403/400 and nothing is recorded. That is
// the same source of truth every other REST audit row in this package already
// uses (recordEvent → actx), and the same workspace the chat's tools now run
// under (issue #747), so the audit row and the work it describes agree.
//
// WHY NOT mcpsrv.recordMCPEvent's ownsAccount/accountSurvives FILTER. That filter
// exists because an MCP call's account comes from its ARGUMENTS — caller-supplied,
// one hop from a MODEL — so "the call resolved to it" is evidence of reachability
// only, and a row filed there would sit in a history its owner reads and the
// caller cannot. Nothing here is caller-supplied: the account comes from the
// server's own resolution of the caller's ?workspace=, and the READER of that
// history is resolved by the same rule (Activity reads through resolveAccountScope
// → AccessBySlug, owner OR member, the same way this request resolved). So the
// caller can read back the rows they file — including a member of the workspace,
// for whom the MCP lane's "a stranger's account-scoped history" failure cannot
// arise. The SURVIVAL half has no analogue either: no chat route deletes an
// account, so this row cannot lose its foreign key between resolve and insert.
//
// AN EMPTY ACCOUNT means the request never resolved one (a bare-mode handler with
// no AccountCtx); the row is skipped, the way recordEventFor skips an empty
// account, rather than falling back to a guess — the guess is the bug.
func chatAuditAccountID(actx *AccountCtx) string {
	if actx == nil || actx.Account == nil {
		return ""
	}
	return actx.Account.ID
}

// recordChatConfirmEvent logs the SHAPE of a confirmation attempt: which tool,
// whether it succeeded. It never records the argument values — a confirmed edit is
// the merchant's own business, and the audit table is read by operators.
//
// The row is filed on the account the request RESOLVED (see chatAuditAccountID):
// a card approved in one workspace must not show up in another workspace's
// Activity history.
func (h *RESTHandlers) recordChatConfirmEvent(r *http.Request, actx *AccountCtx, uid, token, tool string, ok bool, err error) {
	if h.Events == nil {
		return
	}
	accountID := chatAuditAccountID(actx)
	if accountID == "" {
		return
	}
	props := map[string]any{
		"tool":    tool,
		"success": ok,
		// Only the token's FINGERPRINT, never the token: an operator can correlate
		// a refusal with a row, and nothing in the log can be replayed.
		"token_fp": chat.ProposalTokenFingerprint(token),
	}
	if err != nil {
		props["reason"] = chat.ProposeRefusalKind(err)
	}
	_ = h.Events.Record(r.Context(), accountID, uid, "chat.proposal_confirmed", props, "", "")
}

// chatLimits echoes the caps so one place states them (the docs page reads the
// same numbers, and the UI explains a 429 with them).
//
// EVERY field must reference the constant that ENFORCES it, never a literal.
// The response is the client's only view of the caps — the UI renders
// max_message_chars as the box's maxlength and explains a 429 with
// messages_per_hour — so a reported cap that is a second copy of the number is
// a cap that drifts from the one the server actually applies, silently and in
// the direction that lies to the user. A test walks this struct to keep it
// honest (TestChatLimitsReportsEveryEnforcedCap), because a field added here
// and left unset serialises as 0 and nothing else notices.
func chatLimits() chatLimitsOut {
	return chatLimitsOut{
		MessagesPerHour: int(chatRatePerHour),
		Burst:           int(chatBurst),
		MaxMessageChars: chat.MaxMessageChars,
		MaxHistoryTurns: chat.MaxTurns,
		MaxToolSteps:    chat.MaxSteps,
		// Phase 2 — the same constants the proposal lane enforces with:
		// chat.MaxProposalsPerRun bounds one answer's cards, chat.ProposalTTL is
		// the window Runner.Confirm will accept, and the chatConfirm* pair is
		// the bucket handleChatConfirm/handleChatCancel check.
		MaxProposalsPerMessage: chat.MaxProposalsPerRun,
		ProposalTTLSeconds:     int(chat.ProposalTTL / time.Second),
		ConfirmsPerHour:        int(chatConfirmPerHour),
		ConfirmBurst:           int(chatConfirmBurst),
	}
}

// recordChatEvent logs that a chat exchange happened, and whether it filed
// feedback. Best-effort: a nil Events store or a write error never fails the
// answer. It records the SHAPE of the exchange (how many tools, whether a filing
// happened), never the message or reply text — a chat about a merchant's own
// customers is not something to copy into an audit table.
//
// The row is filed on the account the request RESOLVED (see chatAuditAccountID):
// the workspace the user was chatting in is the workspace whose Activity history
// must show it, and no other.
func (h *RESTHandlers) recordChatEvent(r *http.Request, actx *AccountCtx, res *chat.Result) {
	if h.Events == nil || res == nil {
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return
	}
	accountID := chatAuditAccountID(actx)
	if accountID == "" {
		return
	}
	refused := 0
	for _, s := range res.Steps {
		if s.Status == "refused" {
			refused++
		}
	}
	props := map[string]any{
		"tool_calls":     len(res.Steps),
		"tool_refusals":  refused,
		"feedback_filed": res.Filed != nil,
	}
	if res.Filed != nil {
		props["thread_id"] = res.Filed.ThreadID
	}
	_ = h.Events.Record(r.Context(), accountID, uid, "chat.message", props, "", "")
}

// newChatLimiter builds the per-user chat bucket used by NewServer. Exported to
// the package because the route is registered in rest.go, where the wiring
// happens.
func NewChatLimiter() *middleware.RateLimiter {
	return middleware.NewRateLimiter(chatRatePerHour/3600.0, chatBurst)
}

// chatContextFor builds the identity ctx the chat's tools run under: the
// caller's account, the READ-ONLY scope, and the WORKSPACE SCOPE this request
// resolved to.
//
// It exists as a named function so the read-only scope is set in ONE place: the
// refusal tests assert on it, and a future edit has a single line to get wrong.
//
// WHY THE WORKSPACE SCOPE IS BOUND HERE (issue #747). The identity alone was
// never enough, and that is what shipped: the MCP lane re-derives a per-call
// scope from the tool call's OWN arguments (mcpsrv.addTool → resolveScope), the
// reserved `workspace_id`/`account_id` helper fields are optional, and the
// arguments of a chat tool call are the MODEL's — so every call resolved the
// UNSCOPED branch, i.e. EVERY account the user can reach. Under
// ?workspace=abc the assistant answered with another workspace's forms and a
// confirmed write reached a product abc does not own. Binding the resolved set
// here is what makes the tool lane read and write the workspace the user is
// looking at.
func chatContextFor(ctx context.Context, scope *scopeResult, accountID, userID string) context.Context {
	ctx = mcpsrv.WithAuthIdentity(ctx, &mcpsrv.AuthIdentity{
		AccountID: accountID, UserID: userID,
		Scopes: chat.ReadOnlyScopes(), Agent: "in-app-chat",
	})
	return mcpsrv.WithCallScope(ctx, chatCallScope(scope))
}

// chatCallScope turns the account set this request resolved to into the per-call
// MCP scope the tool lane runs under.
//
// WorkspaceID and Role stay empty on purpose: both describe a workspace the call
// named in its ARGUMENTS, and this scope did not come from an argument. Their
// only consumer (mcpsrv.accountAndRole) falls back to RoleByAccount for the
// workspace owning the account, which is the same answer and cannot go stale —
// writing a slug into a field documented as a workspace ID would be a lie in
// exchange for nothing.
//
// A nil scope — or one with no accounts, which is the same thing to the scope
// consumers (mcpsrv.accountScopes treats an empty set as "no scope" and falls
// through to the union) — binds nothing, so a caller that cannot name an account
// must never accidentally re-create the defect this function exists to fix.
func chatCallScope(scope *scopeResult) *mcpsrv.CallScope {
	if scope == nil || len(scope.Accounts) == 0 {
		return nil
	}
	return &mcpsrv.CallScope{Accounts: scope.Accounts}
}

// chatConfirmContextFor builds the identity the CONFIRMED tool runs under: the
// same person and account, with the WRITE scope (phase 2).
//
// It exists as its own named function, next to chatContextFor, so the two
// identities sit one line apart and the difference can only ever be the scope.
// That is the point: "a chat message is read-only; a confirmation is a human
// authorising one specific write" is a claim about these two functions, not about
// a prompt, and the guard test asserts both.
//
// The scope is granted HERE rather than to the conversation, so a confirmed write
// cannot enable a second one: the next message opens a new readonly session, and
// the next write needs its own token.
//
// It carries the request's WORKSPACE SCOPE for the same reason chatContextFor
// does, and it is the binding that matters most: this is the one identity the
// chat can write under, and without the scope the confirmed call targeted
// whatever account owned the object rather than the workspace the user was
// looking at (issue #747).
func chatConfirmContextFor(ctx context.Context, scope *scopeResult, accountID, userID string) context.Context {
	ctx = mcpsrv.WithAuthIdentity(ctx, &mcpsrv.AuthIdentity{
		AccountID: accountID, UserID: userID,
		Scopes: chat.WriteScopes(), Agent: "in-app-chat-confirm",
	})
	return mcpsrv.WithCallScope(ctx, chatCallScope(scope))
}

// NewChatConfirmLimiter builds the per-user confirmation bucket used by
// NewServer. Separate from NewChatLimiter on purpose: see ChatConfirmLimiter.
func NewChatConfirmLimiter() *middleware.RateLimiter {
	return middleware.NewRateLimiter(chatConfirmPerHour/3600.0, chatConfirmBurst)
}
