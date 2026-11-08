// Chat history at the HTTP edge: the acceptance criteria that can only be seen
// through the routes.
//
// THE FOUR THINGS THIS FILE PROVES
//
//  1. A CONVERSATION STILL EXISTS AFTER THE CLIENT FORGETS IT. A message sent in
//     a stored session is appended server-side, and GET returns it — which is
//     what makes "log out and log back in and nothing is lost" true. The
//     browser's storage plays no part in this answer.
//  2. ANOTHER PERSON CANNOT READ IT. Every route refuses a session id that is not
//     the caller's with the same 404 an unknown id gets, so the routes are not an
//     existence oracle either.
//  3. A MESSAGE SENT INTO SOMEBODY ELSE'S SESSION IS REFUSED BEFORE THE MODEL IS
//     CALLED. That is the order that matters: the answer must never be appended
//     to a conversation the caller does not own, and a refused request must not
//     cost a round of model calls.
//  4. A WORKSPACE SWITCH IS A DIFFERENT LIST. Sessions are scoped to (user,
//     workspace), so the same person's other workspace does not inherit them.
//
// Integration against a real Postgres + Valkey, skipped without the test env.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
)

// newUserInWorkspace creates a user whose account belongs to a workspace, which
// is the normal shape for an existing merchant and the scope a stored
// conversation is filed under.
func (e *chatEnv) newUserInWorkspace(t *testing.T) (userID, workspaceID, token string) {
	t.Helper()
	ctx := context.Background()
	userID, _, token = e.newUser(t)
	var accountID string
	if err := e.pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE owner_user_id = $1 ORDER BY created_at DESC LIMIT 1`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("read the account just created: %v", err)
	}
	ws, err := e.h.Workspaces.CreateForAccount(ctx, userID,
		"chat ws", "chat-ws-"+userID[:8]+"-"+randHex(4), accountID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return userID, ws.ID, token
}

// secondWorkspaceFor adds a second workspace for the same user, so a test can
// switch between two scopes.
func (e *chatEnv) secondWorkspaceFor(t *testing.T, userID string) string {
	t.Helper()
	ctx := context.Background()
	ws, err := e.h.Workspaces.Create(ctx, userID, "chat ws 2", "chat-ws2-"+userID[:8]+"-"+randHex(4))
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	return ws.ID
}

type chatSessionWire struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Archived     bool   `json:"archived"`
	MessageCount int    `json:"message_count"`
}

type chatSessionListWire struct {
	Sessions []chatSessionWire `json:"sessions"`
}

type chatSessionDetailWire struct {
	Session  chatSessionWire `json:"session"`
	Messages []struct {
		Role    string      `json:"role"`
		Content string      `json:"content"`
		Steps   []chat.Step `json:"steps"`
	} `json:"messages"`
}

// createSession makes a conversation through the route under test.
func (e *chatEnv) createSession(t *testing.T, token, query, title string) chatSessionWire {
	t.Helper()
	body := "{}"
	if title != "" {
		raw, _ := json.Marshal(map[string]string{"title": title})
		body = string(raw)
	}
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/sessions"+query, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/chat/sessions = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Session chatSessionWire `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode created session: %v (%s)", err, rec.Body.String())
	}
	if out.Session.ID == "" {
		t.Fatalf("created session has no id: %s", rec.Body.String())
	}
	return out.Session
}

// TestChatHistoryRequiresASession: history is a person's own conversation, so an
// API key or an anonymous caller is refused on every route.
func TestChatHistoryRequiresASession(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	cases := []struct{ method, path string }{
		{http.MethodGet, "/v1/rest/chat/sessions"},
		{http.MethodPost, "/v1/rest/chat/sessions"},
		{http.MethodGet, "/v1/rest/chat/sessions/00000000-0000-0000-0000-000000000000"},
		{http.MethodPatch, "/v1/rest/chat/sessions/00000000-0000-0000-0000-000000000000"},
		{http.MethodDelete, "/v1/rest/chat/sessions/00000000-0000-0000-0000-000000000000"},
	}
	for _, c := range cases {
		if rec := e.do(t, "", c.method, c.path, "{}"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

// TestChatMessageIsStoredAndReadBack is criterion 1: the conversation survives
// the client. Nothing here touches browser storage — the transcript is read back
// from the server, which is what a new device sees too.
func TestChatMessageIsStoredAndReadBack(t *testing.T) {
	runner := &recordingRunner{
		ready: true,
		reply: &chat.Result{
			Reply: "You have 3 live forms.",
			Steps: []chat.Step{{Tool: "list_forms", Status: chat.StepCalled}},
		},
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUserInWorkspace(t)

	s := e.createSession(t, token, "", "")
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat",
		`{"message":"berapa banyak borang saya?","session_id":"`+s.ID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var answer struct {
		Reply     string `json:"reply"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if answer.SessionID != s.ID {
		t.Fatalf("answer session_id = %q, want the conversation it was stored in (%q)", answer.SessionID, s.ID)
	}

	rec = e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions/"+s.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET session = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var detail chatSessionDetailWire
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if len(detail.Messages) != 2 {
		t.Fatalf("stored %d messages, want the question and the answer (2) — body %s",
			len(detail.Messages), rec.Body.String())
	}
	if detail.Messages[0].Role != "user" || detail.Messages[0].Content != "berapa banyak borang saya?" {
		t.Fatalf("first stored turn = %+v, want the user's question", detail.Messages[0])
	}
	if detail.Messages[1].Role != "assistant" || detail.Messages[1].Content != "You have 3 live forms." {
		t.Fatalf("second stored turn = %+v, want the assistant's answer", detail.Messages[1])
	}
	// The tool trail survives the reload; the proposal token does not (there is
	// none here, and the stored meta carries only the steps).
	if len(detail.Messages[1].Steps) != 1 || detail.Messages[1].Steps[0].Tool != "list_forms" {
		t.Fatalf("restored answer carries no tool trail: %+v", detail.Messages[1].Steps)
	}
	// The first question named the conversation, so the sidebar has a label.
	if detail.Session.Title != "berapa banyak borang saya?" {
		t.Fatalf("session title = %q, want the first question", detail.Session.Title)
	}
	if detail.Session.MessageCount != 2 {
		t.Fatalf("message_count = %d, want 2", detail.Session.MessageCount)
	}
}

// TestChatStreamedAnswerIsStoredToo: the streamed path is the one the dashboard
// uses, so the exchange must be persisted there as well — a feature that only
// works on the fallback transport is a feature that does not work.
func TestChatStreamedAnswerIsStoredToo(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{
			ready: true,
			reply: &chat.Result{Reply: "Three live forms."},
		},
		events: []chat.Event{{Type: chat.EventDelta, Text: "Three live forms."}},
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUserInWorkspace(t)
	s := e.createSession(t, token, "", "")

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?stream=1",
		`{"message":"ada berapa borang?","session_id":"`+s.ID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("streamed POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	done := framesOf(t, rec.Body.String())
	var got *chatResponseOut
	for _, f := range done {
		if f.Type == "done" {
			got = f.Result
		}
	}
	if got == nil {
		t.Fatalf("the stream carried no done frame: %s", rec.Body.String())
	}
	if got.SessionID != s.ID {
		t.Fatalf("done frame session_id = %q, want %q", got.SessionID, s.ID)
	}
	detail := e.getSession(t, token, s.ID)
	if len(detail.Messages) != 2 {
		t.Fatalf("the streamed exchange stored %d messages, want 2", len(detail.Messages))
	}
}

func (e *chatEnv) getSession(t *testing.T, token, id string) chatSessionDetailWire {
	t.Helper()
	rec := e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET session %s = %d, want 200 (body %s)", id, rec.Code, rec.Body.String())
	}
	var detail chatSessionDetailWire
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return detail
}

// TestChatHistoryIsNotReadableByAnotherUser is criterion 4's negative half: user A
// cannot read user B's conversation, on ANY route, and cannot tell it exists.
func TestChatHistoryIsNotReadableByAnotherUser(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	_, _, tokenA := e.newUserInWorkspace(t)
	_, _, tokenB := e.newUserInWorkspace(t)

	s := e.createSession(t, tokenA, "", "A's private conversation")
	if rec := e.do(t, tokenA, http.MethodPost, "/v1/rest/chat",
		`{"message":"my secret question","session_id":"`+s.ID+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("A's message = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// B's list is theirs, and does not carry A's conversation.
	rec := e.do(t, tokenB, http.MethodGet, "/v1/rest/chat/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("B's list = %d, want 200", rec.Code)
	}
	var list chatSessionListWire
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode B's list: %v", err)
	}
	for _, row := range list.Sessions {
		if row.ID == s.ID {
			t.Fatal("user B's sidebar lists user A's conversation")
		}
	}

	// And B cannot reach it directly. 404 for every verb, identical to an id that
	// does not exist — so the routes cannot be used to probe for conversations.
	unknown := "00000000-0000-0000-0000-000000000000"
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/rest/chat/sessions/" + s.ID, ""},
		{http.MethodPatch, "/v1/rest/chat/sessions/" + s.ID, `{"title":"stolen"}`},
		{http.MethodDelete, "/v1/rest/chat/sessions/" + s.ID, ""},
	}
	for _, c := range cases {
		if rec := e.do(t, tokenB, c.method, c.path, c.body); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s as another user = %d, want 404", c.method, c.path, rec.Code)
		}
	}
	// The same refusal as an id that never existed: no existence oracle.
	for _, c := range cases {
		path := c.path[:len(c.path)-len(s.ID)] + unknown
		if rec := e.do(t, tokenB, c.method, path, c.body); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s (unknown id) = %d, want 404", c.method, path, rec.Code)
		}
	}

	// A's conversation is intact and still A's.
	detail := e.getSession(t, tokenA, s.ID)
	if len(detail.Messages) != 2 || detail.Session.Title != "A's private conversation" {
		t.Fatalf("A's conversation was altered by B's attempts: %+v", detail)
	}
}

// TestChatMessageIntoAnotherUsersSessionIsRefusedBeforeTheModel is criterion 3:
// the refusal happens BEFORE the model call, so the answer cannot be appended to
// a stranger's conversation and the request costs nothing.
func TestChatMessageIntoAnotherUsersSessionIsRefusedBeforeTheModel(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	_, _, tokenA := e.newUserInWorkspace(t)
	_, _, tokenB := e.newUserInWorkspace(t)
	s := e.createSession(t, tokenA, "", "")

	rec := e.do(t, tokenB, http.MethodPost, "/v1/rest/chat",
		`{"message":"hello","session_id":"`+s.ID+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("message into another user's session = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	runner.mu.Lock()
	runs := runner.gotRuns
	runner.mu.Unlock()
	if runs != 0 {
		t.Fatalf("the model was called %d time(s) for a request into somebody else's conversation — the "+
			"session must be checked BEFORE the model runs", runs)
	}
	// Nothing was written into A's conversation.
	detail := e.getSession(t, tokenA, s.ID)
	if len(detail.Messages) != 0 {
		t.Fatalf("%d message(s) landed in A's conversation from B's refused request", len(detail.Messages))
	}
}

// TestChatSessionListIsPerWorkspace is criterion 4's positive half: the same
// person's two workspaces have separate sidebars.
func TestChatSessionListIsPerWorkspace(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	userID, ws1, token := e.newUserInWorkspace(t)
	ws2 := e.secondWorkspaceFor(t, userID)

	// The workspace id is what the store scopes on; the routes resolve it from
	// ?workspace=<slug>, so the slug is what the test needs.
	var slug1, slug2 string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT slug FROM workspaces WHERE id IN ($1,$2) ORDER BY (id=$1) DESC`, ws1, ws2).
		Scan(&slug1); err != nil {
		t.Fatalf("read first slug: %v", err)
	}
	if err := e.pool.QueryRow(context.Background(),
		`SELECT slug FROM workspaces WHERE id = $1`, ws2).Scan(&slug2); err != nil {
		t.Fatalf("read second slug: %v", err)
	}

	first := e.createSession(t, token, "?workspace="+slug1, "in workspace one")

	// Workspace two's list does not carry it.
	rec := e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions?workspace="+slug2, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list in workspace two = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var list chatSessionListWire
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, row := range list.Sessions {
		if row.ID == first.ID {
			t.Fatal("a conversation held in workspace one is listed in workspace two")
		}
	}
	// And it is not readable through that scope either.
	if rec := e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions/"+first.ID+"?workspace="+slug2, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("reading workspace one's conversation through workspace two = %d, want 404", rec.Code)
	}
	// In its own workspace it is there.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions?workspace="+slug1, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list in workspace one = %d, want 200", rec.Code)
	}
	list = chatSessionListWire{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, row := range list.Sessions {
		if row.ID == first.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the conversation is missing from the workspace it was held in")
	}
}

// TestChatSessionRenameArchiveDelete covers the curation routes: a rename sticks,
// an archive hides it from the live list, and a delete removes it for good.
func TestChatSessionRenameArchiveDelete(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	_, _, token := e.newUserInWorkspace(t)
	s := e.createSession(t, token, "", "")

	rec := e.do(t, token, http.MethodPatch, "/v1/rest/chat/sessions/"+s.ID, `{"title":"Borang bulan lepas"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := e.getSession(t, token, s.ID); got.Session.Title != "Borang bulan lepas" {
		t.Fatalf("title after rename = %q", got.Session.Title)
	}

	// A PATCH with neither field is a malformed request, not a no-op.
	if rec := e.do(t, token, http.MethodPatch, "/v1/rest/chat/sessions/"+s.ID, `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty PATCH = %d, want 400", rec.Code)
	}

	if rec := e.do(t, token, http.MethodPatch, "/v1/rest/chat/sessions/"+s.ID, `{"archived":true}`); rec.Code != http.StatusOK {
		t.Fatalf("archive = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	rec = e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions", "")
	var list chatSessionListWire
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, row := range list.Sessions {
		if row.ID == s.ID {
			t.Fatal("an archived conversation is still in the live sidebar")
		}
	}
	// It is still there when the archive is asked for, and still readable: an
	// archive is a tidy-up, not a delete.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions?archived=1", "")
	list = chatSessionListWire{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode archive list: %v", err)
	}
	if len(list.Sessions) == 0 || list.Sessions[0].ID != s.ID {
		t.Fatalf("the archived conversation is not in the archive list: %s", rec.Body.String())
	}
	e.getSession(t, token, s.ID)

	if rec := e.do(t, token, http.MethodDelete, "/v1/rest/chat/sessions/"+s.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions/"+s.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", rec.Code)
	}
}

// TestChatSessionCreateIsCapped: an authenticated client must not be able to fill
// the table without limit. The cap is a product rule, so it is asserted, not
// assumed.
func TestChatSessionCreateIsCapped(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	userID, workspaceID, token := e.newUserInWorkspace(t)
	store := e.h.ChatSessions
	for i := 0; i < MaxChatSessionsPerScope; i++ {
		if _, err := store.Create(context.Background(), userID, workspaceID, ""); err != nil {
			t.Fatalf("seed session %d: %v", i, err)
		}
	}
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/sessions", "{}")
	if rec.Code != http.StatusConflict {
		t.Fatalf("create past the cap = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestChatHistoryRefusesAMalformedSessionID: an id the CLIENT typed that is not
// a UUID answers 404, not 500. The uuid comparison inside the store raises
// SQLSTATE 22P02 for such a value, and without the mapping in
// chatSessionWriteErr the route reported a server fault for what is a bad
// request — which a user reads as "the database is broken" and a caller reads as
// retryable.
func TestChatHistoryRefusesAMalformedSessionID(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	_, _, token := e.newUserInWorkspace(t)

	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/rest/chat/sessions/not-a-uuid", ""},
		{http.MethodPatch, "/v1/rest/chat/sessions/not-a-uuid", `{"title":"x"}`},
		{http.MethodDelete, "/v1/rest/chat/sessions/not-a-uuid", ""},
		{http.MethodPost, "/v1/rest/chat", `{"message":"hi","session_id":"not-a-uuid"}`},
	}
	for _, c := range cases {
		rec := e.do(t, token, c.method, c.path, c.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s with a malformed id = %d, want 404 (body %s)",
				c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}
