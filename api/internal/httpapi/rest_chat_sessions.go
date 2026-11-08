// Chat history at the REST edge: the conversations a person has had with the
// in-app assistant.
//
// WHY THESE ROUTES EXIST. The transcript used to live in the browser only and was
// deleted when the session ended, so logging out — or a session expiring, or a
// different device — lost every conversation the person had had. The transcript
// is the user's own work, so it is stored on the server, scoped to the person AND
// the workspace, and read back on the next visit.
//
// THE SAME LANE RULES AS EVERY OTHER CHAT ROUTE:
//
//   - SESSION ONLY. A conversation belongs to a person; an API key carries no
//     person (the same reason POST /v1/rest/chat refuses one).
//   - WORKSPACE SCOPED, through chatWorkspaceScopeOr403 — an unresolved scope is
//     a 400 ("choose a workspace") and a slug the caller cannot reach is a 403,
//     never a silent fall back to their first account.
//   - NOTHING IS STORED THAT CAN STILL BE SPENT. A proposal's token is a
//     single-use capability with a five-minute life; the transcript keeps the
//     words and the tool trail, so a restored conversation offers no button whose
//     token expired hours ago.
//
// A session id that is not the caller's reads EXACTLY like one that never
// existed (404), because the store scopes every query by (user, workspace) rather
// than looking the row up and then comparing owners.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// MaxChatSessionsPerScope bounds how many conversations one person keeps in one
// workspace. Without a bound, an authenticated client can create rows without
// limit; with one, a user who genuinely talks a lot still has room for years of
// conversations, and the refusal names the way out (delete old ones).
const MaxChatSessionsPerScope = 200

// chatSessionOut is one conversation as the sidebar sees it.
type chatSessionOut struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Archived     bool      `json:"archived"`
	MessageCount int       `json:"message_count"`
}

// chatSessionMessageOut is one turn. `steps` is the tool trail the assistant left
// when it produced this answer, so a restored conversation still shows what was
// read — the same shape POST /v1/rest/chat returns, minus anything spendable.
type chatSessionMessageOut struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	CreatedAt time.Time   `json:"created_at"`
	Steps     []chat.Step `json:"steps,omitempty"`
}

type chatSessionCreateIn struct {
	Title string `json:"title"`
}

type chatSessionPatchIn struct {
	// Pointers so "absent" and "set to empty/false" are different requests: a
	// rename to "" is a request to clear the name, and a PATCH that carries
	// neither field is a malformed request rather than a no-op.
	Title    *string `json:"title"`
	Archived *bool   `json:"archived"`
}

// requireChatSession resolves the caller and the workspace a history call is
// scoped to, writing the refusal itself. `ok=false` means the response is
// already written.
//
// It is named `require…` because it IS a gate — a session user is mandatory and
// an API key is refused, exactly like every other route that acts on a PERSON —
// and the OpenAPI access classifier reads contracted handlers by the gates they
// name (classifierGates). A history route published as `read` would promise that
// an API key can read somebody's conversations.
//
// The workspace id (not just the slug) is what the store filters on, so one
// person's conversations in workspace A are invisible from workspace B even
// though the same person owns both. "" is the no-workspace scope: accounts.
// workspace_id is nullable and a signup starts without one, so that state has to
// be storable rather than an error.
func (h *RESTHandlers) requireChatSession(w http.ResponseWriter, r *http.Request, actx *AccountCtx) (uid, workspaceID string, ok bool) {
	uid = h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized,
			"chat history needs a user session — sign in to use it")
		return "", "", false
	}
	if actx == nil || actx.Account == nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return "", "", false
	}
	if h.ChatSessions == nil {
		// A deployment with no session store (bare-mode tests, a partially wired
		// server) must say so rather than answering an empty list, which the UI
		// would render as "you have never talked to the assistant".
		writeErr(w, http.StatusServiceUnavailable, "chat history is not available")
		return "", "", false
	}
	scope, sok := h.chatWorkspaceScopeOr403(w, r, actx)
	if !sok {
		return "", "", false
	}
	workspaceID, err := h.chatSessionWorkspace(r, actx, scope)
	if err != nil {
		logError(r, "chat_session_workspace_resolve_failed", err)
		writeErr(w, http.StatusInternalServerError, "could not resolve the workspace")
		return "", "", false
	}
	return uid, workspaceID, true
}

// chatSessionWorkspace resolves the workspace id a chat request is scoped to:
// the resolved ?workspace= slug when there is one, otherwise the workspace the
// resolved account belongs to (empty when it belongs to none yet).
func (h *RESTHandlers) chatSessionWorkspace(r *http.Request, actx *AccountCtx, scope *scopeResult) (string, error) {
	if scope != nil && scope.Workspace != "" {
		if h.Workspaces == nil {
			return "", errors.New("workspace store not wired")
		}
		return h.Workspaces.IDBySlug(r.Context(), scope.Workspace)
	}
	if h.Workspaces == nil || actx == nil || actx.Account == nil {
		return "", nil
	}
	return h.Workspaces.WorkspaceIDByAccountOrEmpty(r.Context(), actx.Account.ID)
}

func chatSessionToOut(s *dbstore.ChatSession) chatSessionOut {
	return chatSessionOut{
		ID: s.ID, Title: s.Title, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Archived: s.ArchivedAt != nil, MessageCount: s.MessageCount,
	}
}

// handleChatSessionsList: GET /v1/rest/chat/sessions — the sidebar.
func (h *RESTHandlers) handleChatSessionsList(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid, workspaceID, ok := h.requireChatSession(w, r, actx)
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("archived") == "1"
	sessions, err := h.ChatSessions.List(r.Context(), uid, workspaceID, includeArchived)
	if err != nil {
		logError(r, "chat_sessions_list_failed", err)
		writeErr(w, http.StatusInternalServerError, "could not load your conversations")
		return
	}
	out := make([]chatSessionOut, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, chatSessionToOut(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// handleChatSessionsCreate: POST /v1/rest/chat/sessions — "New conversation".
func (h *RESTHandlers) handleChatSessionsCreate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid, workspaceID, ok := h.requireChatSession(w, r, actx)
	if !ok {
		return
	}
	var in chatSessionCreateIn
	// An empty body is a valid "start a conversation with no name yet": the
	// store names it from the first message.
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	count, err := h.ChatSessions.Count(r.Context(), uid, workspaceID)
	if err != nil {
		logError(r, "chat_sessions_count_failed", err)
		writeErr(w, http.StatusInternalServerError, "could not start a conversation")
		return
	}
	if count >= MaxChatSessionsPerScope {
		writeErr(w, http.StatusConflict,
			fmt.Sprintf("you have reached %d saved conversations in this workspace — delete some to start a new one",
				MaxChatSessionsPerScope))
		return
	}
	s, err := h.ChatSessions.Create(r.Context(), uid, workspaceID, in.Title)
	if err != nil {
		logError(r, "chat_session_create_failed", err)
		writeErr(w, http.StatusInternalServerError, "could not start a conversation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": chatSessionToOut(s)})
}

// handleChatSessionGet: GET /v1/rest/chat/sessions/{id} — one conversation with
// its messages.
func (h *RESTHandlers) handleChatSessionGet(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid, workspaceID, ok := h.requireChatSession(w, r, actx)
	if !ok {
		return
	}
	id := r.PathValue("id")
	s, err := h.ChatSessions.ByID(r.Context(), uid, workspaceID, id)
	if err != nil {
		h.writeChatSessionErr(w, r, err)
		return
	}
	// chat.MaxTurns is the window the assistant itself is given, and the store
	// trims to it on append; asking for it here is the belt to that braces, so a
	// conversation written before a cap change cannot be replayed in full.
	msgs, err := h.ChatSessions.Messages(r.Context(), uid, workspaceID, s.ID, chat.MaxTurns)
	if err != nil {
		h.writeChatSessionErr(w, r, err)
		return
	}
	out := make([]chatSessionMessageOut, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, chatSessionMessageOut{
			ID: m.ID, Role: m.Role, Content: m.Content, CreatedAt: m.CreatedAt,
			Steps: chatStepsFromMeta(m.Meta),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":  chatSessionToOut(s),
		"messages": out,
	})
}

// handleChatSessionPatch: PATCH /v1/rest/chat/sessions/{id} — rename and/or
// archive. Both are the user's own curation of their own list, so both live here
// rather than at two routes that would each need the same scope gate.
func (h *RESTHandlers) handleChatSessionPatch(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid, workspaceID, ok := h.requireChatSession(w, r, actx)
	if !ok {
		return
	}
	var in chatSessionPatchIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == nil && in.Archived == nil {
		writeErr(w, http.StatusBadRequest, "nothing to change (send title and/or archived)")
		return
	}
	id := r.PathValue("id")
	if in.Title != nil {
		if err := h.ChatSessions.Rename(r.Context(), uid, workspaceID, id, *in.Title); err != nil {
			h.writeChatSessionErr(w, r, err)
			return
		}
	}
	if in.Archived != nil {
		if err := h.ChatSessions.SetArchived(r.Context(), uid, workspaceID, id, *in.Archived); err != nil {
			h.writeChatSessionErr(w, r, err)
			return
		}
	}
	s, err := h.ChatSessions.ByID(r.Context(), uid, workspaceID, id)
	if err != nil {
		h.writeChatSessionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": chatSessionToOut(s)})
}

// handleChatSessionDelete: DELETE /v1/rest/chat/sessions/{id}.
//
// A delete is a real delete: the messages cascade. Archiving is the reversible
// action (PATCH archived:true), and it is what the UI offers first for a
// conversation the user merely wants out of the way.
func (h *RESTHandlers) handleChatSessionDelete(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid, workspaceID, ok := h.requireChatSession(w, r, actx)
	if !ok {
		return
	}
	if err := h.ChatSessions.Delete(r.Context(), uid, workspaceID, r.PathValue("id")); err != nil {
		h.writeChatSessionErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// writeChatSessionErr maps a store failure onto the wire. "Not found" is used for
// every refusal the store can name, including another person's session: whether a
// conversation exists is not something a stranger gets to learn.
func (h *RESTHandlers) writeChatSessionErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, dbstore.ErrChatSessionNotFound):
		writeErr(w, http.StatusNotFound, "conversation not found")
	default:
		logError(r, "chat_session_failed", err)
		writeErr(w, http.StatusInternalServerError, "could not complete that request")
	}
}

// chatStepsFromMeta reads the tool trail stored with an assistant turn. A meta
// value this build cannot decode yields no trail rather than an error: the
// message itself is still readable, and a transcript must not become unreadable
// because a future step shape was added.
func chatStepsFromMeta(meta map[string]any) []chat.Step {
	if len(meta) == 0 {
		return nil
	}
	raw, ok := meta["steps"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var steps []chat.Step
	if err := json.Unmarshal(b, &steps); err != nil {
		return nil
	}
	return steps
}

// chatStepsMeta builds the meta object stored with an assistant turn: the tool
// trail, and nothing that can still be spent. A proposal's token is deliberately
// absent (see this file's header) — the card it belonged to is gone.
func chatStepsMeta(steps []chat.Step) map[string]any {
	if len(steps) == 0 {
		return nil
	}
	return map[string]any{"steps": steps}
}

// chatPersistExchange records one answered exchange into its conversation.
//
// It runs AFTER the model has answered, so a failure here is logged and never
// turns a delivered answer into an error: the user has already paid for the
// answer with a round of model calls, and losing the copy is bad but losing the
// answer too would be worse. The session was validated before the model was
// called (chatSessionWorkspaceOr404), so the only ways this fails are a database
// fault and the session being deleted mid-answer.
func (h *RESTHandlers) chatPersistExchange(r *http.Request, uid, workspaceID, sessionID, msg string, res *chat.Result) {
	if h.ChatSessions == nil || strings.TrimSpace(sessionID) == "" || res == nil {
		return
	}
	// The user's turn first: it is what names an unnamed conversation, and it is
	// the part that must not be lost if the assistant's turn is refused.
	if _, err := h.ChatSessions.Append(r.Context(), uid, workspaceID, sessionID, "user", msg, nil, chat.MaxTurns); err != nil {
		logError(r, "chat_session_append_user_failed", err)
		return
	}
	if _, err := h.ChatSessions.Append(r.Context(), uid, workspaceID, sessionID, "assistant", res.Reply,
		chatStepsMeta(res.Steps), chat.MaxTurns); err != nil {
		logError(r, "chat_session_append_assistant_failed", err)
	}
}

// chatAnswer records the exchange and builds the wire response, so the one-shot
// and the streamed answer cannot disagree about what is stored, what is audited
// or what the response carries.
func (h *RESTHandlers) chatAnswer(r *http.Request, actx *AccountCtx, uid, workspaceID, sessionID, msg string, res *chat.Result) chatResponseOut {
	h.recordChatEvent(r, actx, res)
	h.chatPersistExchange(r, uid, workspaceID, sessionID, msg, res)
	return chatResponseOut{
		Reply: res.Reply, ReplyKey: res.ReplyKey, Steps: res.Steps, Filed: res.Filed,
		Proposals: res.Proposals, SessionID: sessionID, Limits: chatLimits(),
	}
}

// chatSessionWorkspaceOr404 validates the conversation a message claims to
// belong to and resolves the workspace it is scoped to.
//
// It answers ok=true with an empty workspace for a message that carries no
// session id: that is a caller keeping its own transcript, and the message is
// answered but not stored. Every other refusal is written here.
func (h *RESTHandlers) chatSessionWorkspaceOr404(w http.ResponseWriter, r *http.Request, actx *AccountCtx, scope *scopeResult, uid, sessionID string) (string, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return "", true
	}
	if h.ChatSessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "chat history is not available")
		return "", false
	}
	workspaceID, err := h.chatSessionWorkspace(r, actx, scope)
	if err != nil {
		logError(r, "chat_session_workspace_resolve_failed", err)
		writeErr(w, http.StatusInternalServerError, "could not resolve the workspace")
		return "", false
	}
	if _, err := h.ChatSessions.ByID(r.Context(), uid, workspaceID, sessionID); err != nil {
		h.writeChatSessionErr(w, r, err)
		return "", false
	}
	return workspaceID, true
}
