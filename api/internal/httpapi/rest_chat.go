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
// to ChatMaxSteps of them. A message limit alone would understate the cost, so
// the limit below is stated alongside the step cap it multiplies with. A rate
// limit is the right guard here for the same reason it is on the feedback lane:
// it delays a loud user rather than misjudging one.
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
	// The COST bound is this times chat.MaxSteps tool round-trips per message,
	// i.e. at most 240 model calls per user per hour, each capped at
	// chat.MaxToolResultChars of context per tool. Both numbers are in the docs
	// page next to the feature so the limit is not a surprise.
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
	Reply string      `json:"reply"`
	Steps []chat.Step `json:"steps"`
	Filed *chat.Filed `json:"filed,omitempty"`
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
func (h *RESTHandlers) handleChatStatus(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if h.sessionOwnerID(r) == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": h.chatAvailable(),
		"limits":    chatLimits(),
		"read_only": true,
	})
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
			"had chat dicapai (60 mesej sejam). Sila cuba sebentar lagi.")
		return
	}

	// The identity the tool calls run under. THIS is what makes the chat
	// read-only: whatever the session could do itself, the tools it can reach
	// are dispatched with a readonly scope, so every write refuses.
	ctx := chatContextFor(r.Context(), actx.Account.ID, uid)

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
	h.recordChatEvent(r, res)
	writeJSON(w, http.StatusOK, chatResponseOut{
		Reply: res.Reply, Steps: res.Steps, Filed: res.Filed,
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
	// read-only identity every message runs under.
	writeCtx := chatConfirmContextFor(r.Context(), actx.Account.ID, uid)

	res, err := h.Chat.Confirm(r.Context(), writeCtx, uid, in.Token, in.ArgsHash)
	if err != nil {
		h.recordChatConfirmEvent(r, uid, in.Token, "", false, err)
		writeErr(w, chatConfirmStatusFor(err), chatConfirmMessageFor(err))
		return
	}
	h.recordChatConfirmEvent(r, uid, in.Token, res.Tool, true, nil)
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
func (h *RESTHandlers) handleChatCancel(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized,
			"membatalkan cadangan memerlukan sesi pengguna — log masuk untuk menggunakannya")
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
		h.recordChatConfirmEvent(r, uid, in.Token, "", false, err)
		writeErr(w, chatConfirmStatusFor(err), chatConfirmMessageFor(err))
		return
	}
	h.recordChatConfirmEvent(r, uid, in.Token, "", true, nil)
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
	case errors.Is(err, dbstore.ErrChatProposalNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

// chatConfirmMessageFor turns a confirm failure into copy the user can act on.
// Nothing internal is returned: the details are the server's business.
func chatConfirmMessageFor(err error) string {
	switch {
	case errors.Is(err, dbstore.ErrChatProposalConsumed):
		return "cadangan itu sudah digunakan atau dibatalkan — minta pembantu menyediakan semula"
	case errors.Is(err, dbstore.ErrChatProposalExpired):
		return "cadangan itu telah luput — minta pembantu menyediakan semula"
	case errors.Is(err, chat.ErrProposalArgsChanged):
		return "cadangan berubah sejak dipaparkan — sahkan semula dari kad asal"
	case errors.Is(err, dbstore.ErrChatProposalNotFound):
		return "cadangan tidak dijumpai"
	default:
		return "cadangan tidak dapat dilaksanakan"
	}
}

// recordChatConfirmEvent logs the SHAPE of a confirmation attempt: which tool,
// whether it succeeded. It never records the argument values — a confirmed edit is
// the merchant's own business, and the audit table is read by operators.
func (h *RESTHandlers) recordChatConfirmEvent(r *http.Request, uid, token, tool string, ok bool, err error) {
	if h.Events == nil {
		return
	}
	accountID := ""
	if h.Accounts != nil {
		if accs, aerr := h.Accounts.ByOwner(r.Context(), uid); aerr == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
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
func (h *RESTHandlers) recordChatEvent(r *http.Request, res *chat.Result) {
	if h.Events == nil || res == nil {
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return
	}
	accountID := ""
	if h.Accounts != nil {
		if accs, err := h.Accounts.ByOwner(r.Context(), uid); err == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
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

// chatContextFor builds the identity ctx the chat's tools run under. It exists
// as a named function so the read-only scope is set in ONE place: the refusal
// tests assert on it, and a future edit has a single line to get wrong.
func chatContextFor(ctx context.Context, accountID, userID string) context.Context {
	return mcpsrv.WithAuthIdentity(ctx, &mcpsrv.AuthIdentity{
		AccountID: accountID, UserID: userID,
		Scopes: chat.ReadOnlyScopes(), Agent: "in-app-chat",
	})
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
func chatConfirmContextFor(ctx context.Context, accountID, userID string) context.Context {
	return mcpsrv.WithAuthIdentity(ctx, &mcpsrv.AuthIdentity{
		AccountID: accountID, UserID: userID,
		Scopes: chat.WriteScopes(), Agent: "in-app-chat-confirm",
	})
}

// NewChatConfirmLimiter builds the per-user confirmation bucket used by
// NewServer. Separate from NewChatLimiter on purpose: see ChatConfirmLimiter.
func NewChatConfirmLimiter() *middleware.RateLimiter {
	return middleware.NewRateLimiter(chatConfirmPerHour/3600.0, chatConfirmBurst)
}
