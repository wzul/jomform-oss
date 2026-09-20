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
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/chat"
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
		Reply: res.Reply, Steps: res.Steps, Filed: res.Filed, Limits: chatLimits(),
	})
}

// chatLimits echoes the caps so one place states them (the docs page reads the
// same numbers, and the UI explains a 429 with them).
func chatLimits() chatLimitsOut {
	return chatLimitsOut{
		MessagesPerHour: int(chatRatePerHour),
		Burst:           int(chatBurst),
		MaxMessageChars: chat.MaxMessageChars,
		MaxHistoryTurns: chat.MaxTurns,
		MaxToolSteps:    chat.MaxSteps,
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
