// What Stop leaves behind (card t_babeaa84) — end to end, over the chain
// production serves, and into the stored conversation.
//
// WHY THIS IS NOT A HANDLER-LEVEL TEST. The defect was never inside
// chatPersistExchange: it was that on the CLIENT-CANCELLED path the stream
// handler returned BEFORE reaching chatAnswer, so the persist function was never
// called at all. A test that invokes the persist function directly would have
// stayed green through the whole of v1.27.0/v1.27.1 while every real Stop stored
// nothing. So these tests drive a real request over Server.Handler(), let the
// client walk away mid-answer exactly as the browser's AbortController does, and
// then read the conversation back through the same GET the page uses on reload.
//
// WHAT "FIXED" MEANS HERE, in the card's own words: after Stop, the stored
// conversation holds the user's question and the partial assistant text that had
// already been shown, marked incomplete — and the stored partial is byte-for-byte
// the text the client received before it disconnected, so a reload renders what
// the user was reading.
//
// Integration: needs JOMFORM_TEST_DATABASE_URL (scratch schema) and
// JOMFORM_TEST_VALKEY_URL; skips otherwise (via newChatEnv).
package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chat"
)

// stopAwareRunner streams a fixed set of fragments and then WAITS for the client
// to disappear, reporting exactly what the production runner reports when it does:
// the cancellation. That is the shape of a stopped answer — the runner has no way
// to know the browser is gone except through the write error or the context, and
// the handler's job is to keep the exchange either way.
type stopAwareRunner struct {
	recordingRunner
	fragments []string
	// emitted is closed once every fragment has been handed to emit.
	emitted chan struct{}
	// cancelled is closed when the runner observes the request context end, so a
	// test can assert the server really noticed the client leaving instead of
	// passing on some other failure.
	cancelled chan struct{}
}

func (r *stopAwareRunner) RunStream(ctx context.Context, history []chat.Turn, message string, emit chat.EventFunc) (*chat.Result, error) {
	for _, f := range r.fragments {
		if err := emit(chat.Event{Type: chat.EventDelta, Text: f}); err != nil {
			return nil, err
		}
	}
	close(r.emitted)
	select {
	case <-ctx.Done():
		close(r.cancelled)
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		return nil, errors.New("the request context was never cancelled: the server did not notice the client leaving")
	}
}

// chatSessionWireView is the two fields of a restored conversation this file
// asserts on: the turns (role, text, the partial marker) and the count the sidebar
// shows.
type chatSessionWireView struct {
	Session struct {
		ID           string `json:"id"`
		MessageCount int    `json:"message_count"`
	} `json:"session"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Stopped bool   `json:"stopped"`
	} `json:"messages"`
}

// createChatSessionOverHTTP starts a conversation through the route the page
// uses. It goes through the wire rather than the store on purpose: the scope the
// session is filed under must be the one the SAME cookie resolves for the chat
// POST and the GET, or the test could pass against a scope production never uses.
func createChatSessionOverHTTP(t *testing.T, baseURL, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/rest/chat/sessions", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build session request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/rest/chat/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/rest/chat/sessions = %d, want 201", resp.StatusCode)
	}
	var out struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Session.ID == "" {
		t.Fatalf("could not read the created session (err=%v)", err)
	}
	return out.Session.ID
}

// readChatSession is the reload: GET the conversation exactly as the page does
// when a stored thread is opened.
func readChatSession(t *testing.T, baseURL, token, id string) chatSessionWireView {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/rest/chat/sessions/"+id, nil)
	if err != nil {
		t.Fatalf("build read request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/rest/chat/sessions/%s: %v", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/rest/chat/sessions/%s = %d, want 200", id, resp.StatusCode)
	}
	var out chatSessionWireView
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode restored conversation: %v", err)
	}
	return out
}

// waitForTurns polls the stored conversation until it holds at least want turns.
// The write happens on the server AFTER the client has gone, so it is not
// synchronous with the aborted request — polling is the honest way to read it.
// It fails with the content it did see, so a regression names what was stored
// instead of only timing out.
func waitForTurns(t *testing.T, baseURL, token, id string, want int) chatSessionWireView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last chatSessionWireView
	for time.Now().Before(deadline) {
		last = readChatSession(t, baseURL, token, id)
		if len(last.Messages) >= want {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	var roles []string
	for _, m := range last.Messages {
		roles = append(roles, m.Role)
	}
	t.Fatalf("the stored conversation still holds %d message(s) %v after 10s, want %d — a stopped "+
		"exchange was not persisted (the user's question and the partial answer are lost)",
		len(last.Messages), roles, want)
	return last
}

// TestChatStopMidAnswerStoresThePartialAndTheQuestion is the regression the card
// asks for.
//
// Pre-fix this fails at waitForTurns: the handler's cancellation branch returned
// before chatAnswer, so chatPersistExchange never ran and the conversation stayed
// empty — measured on prod v1.27.1 as 10 sends / 3 stopped → chat_messages held
// exactly 14 rows for the 7 completed exchanges and zero rows for the stopped
// ones.
func TestChatStopMidAnswerStoresThePartialAndTheQuestion(t *testing.T) {
	const question = "design a loyalty programme for a small online store"
	// Two fragments: the client will have read both before it walks away, so the
	// stored partial has to equal their concatenation exactly.
	fragments := []string{"# Designing a loyalty programme\n\n", "## 1. Decide"}

	runner := &stopAwareRunner{
		recordingRunner: recordingRunner{ready: true},
		fragments:       fragments,
		emitted:         make(chan struct{}),
		cancelled:       make(chan struct{}),
	}
	baseURL, token := chatStreamServer(t, runner, 0)
	sessionID := createChatSessionOverHTTP(t, baseURL, token)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"message": question, "session_id": sessionID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/rest/chat?stream=1", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build chat request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/rest/chat?stream=1: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read what the USER was shown, frame by frame, and stop after the last
	// fragment — then close the connection, which is Stop.
	var shown strings.Builder
	br := bufio.NewReader(resp.Body)
	want := len(fragments)
	seen := 0
	for seen < want {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended before %d deltas (%d seen): the client never received the "+
				"fragment this test stops on: %v", want, seen, err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev chatStreamEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("frame is not valid JSON (%q): %v", line, err)
		}
		if ev.Type == chat.EventDelta {
			shown.WriteString(ev.Text)
			seen++
		}
	}
	wantShown := strings.Join(fragments, "")
	if shown.String() != wantShown {
		t.Fatalf("the client was shown %q, want %q", shown.String(), wantShown)
	}

	// STOP. The browser's abort closes the connection; the server learns of it
	// through the request context.
	cancel()
	_ = resp.Body.Close()

	select {
	case <-runner.cancelled:
	case <-time.After(10 * time.Second):
		t.Fatal("the runner never saw the request context end: the server did not notice the client " +
			"leaving, so this test would not be exercising the Stop path")
	}

	got := waitForTurns(t, baseURL, token, sessionID, 2)
	if len(got.Messages) != 2 {
		t.Fatalf("stored messages = %d, want exactly 2 (the question and the partial answer)", len(got.Messages))
	}
	userTurn, assistantTurn := got.Messages[0], got.Messages[1]

	if userTurn.Role != "user" || userTurn.Content != question {
		t.Fatalf("stored first turn = %q (%s), want the user's question %q — the question the user "+
			"typed is the half of the loss that is entirely theirs",
			userTurn.Content, userTurn.Role, question)
	}
	if assistantTurn.Role != "assistant" {
		t.Fatalf("stored second turn role = %q, want assistant", assistantTurn.Role)
	}
	if assistantTurn.Content != wantShown {
		t.Fatalf("stored partial = %q, want the text the client was shown %q — a reload must show "+
			"the same fragment the user was reading when they pressed Stop",
			assistantTurn.Content, wantShown)
	}
	if !assistantTurn.Stopped {
		t.Fatal("the restored assistant turn is not marked stopped: the page would render a sentence " +
			"that ends mid-word as a finished answer")
	}
	if got.Session.MessageCount != 2 {
		t.Errorf("message_count = %d, want 2 (the sidebar and the transcript must agree)",
			got.Session.MessageCount)
	}
}

// TestChatStopBeforeTheFirstFragmentStoresTheQuestionOnly covers the other end of
// Stop: pressed while the model is still thinking. The user's question must still
// be stored (it is what they typed, and what names the conversation), and NO empty
// assistant turn may be written — a blank bubble is indistinguishable from a
// broken answer.
func TestChatStopBeforeTheFirstFragmentStoresTheQuestionOnly(t *testing.T) {
	const question = "what were my sales last week?"

	runner := &stopAwareRunner{
		recordingRunner: recordingRunner{ready: true},
		emitted:         make(chan struct{}),
		cancelled:       make(chan struct{}),
	}
	baseURL, token := chatStreamServer(t, runner, 0)
	sessionID := createChatSessionOverHTTP(t, baseURL, token)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"message": question, "session_id": sessionID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/rest/chat?stream=1", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build chat request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/rest/chat?stream=1: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// The headers are committed and the runner is waiting on the context; nothing
	// has been shown yet. This is Stop during "thinking".
	cancel()
	_ = resp.Body.Close()

	select {
	case <-runner.cancelled:
	case <-time.After(10 * time.Second):
		t.Fatal("the runner never saw the request context end")
	}

	got := waitForTurns(t, baseURL, token, sessionID, 1)
	if len(got.Messages) != 1 {
		t.Fatalf("stored messages = %d, want exactly 1 (the question): an empty assistant turn is a "+
			"blank bubble the user cannot tell from a broken answer", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != question {
		t.Fatalf("stored turn = %q (%s), want the question %q", got.Messages[0].Content, got.Messages[0].Role, question)
	}
}

// TestChatCompletedStreamStoresAnUnmarkedAnswer is the INVERSE guard: the marker
// must never end up under a COMPLETE answer. That was v1.27.0's defect — a
// finished answer stored under an "incomplete" marker — and the cheapest way to
// re-introduce it is to mark every streamed exchange as stopped.
func TestChatCompletedStreamStoresAnUnmarkedAnswer(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{
			ready: true,
			reply: &chat.Result{Reply: "Three live forms."},
		},
		events: []chat.Event{
			{Type: chat.EventDelta, Text: "Three "},
			{Type: chat.EventDelta, Text: "live forms."},
		},
	}
	baseURL, token := chatStreamServer(t, runner, 0)
	sessionID := createChatSessionOverHTTP(t, baseURL, token)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"message": "which forms are live?", "session_id": sessionID})
	resp := postChatStream(t, ctx, baseURL, token, string(body))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Drain the stream so the handler finishes (and persists) before we read back.
	_, _ = io.Copy(io.Discard, resp.Body)

	got := waitForTurns(t, baseURL, token, sessionID, 2)
	if len(got.Messages) != 2 {
		t.Fatalf("stored messages = %d, want 2", len(got.Messages))
	}
	if got.Messages[1].Stopped {
		t.Fatal("a COMPLETE answer was stored marked stopped: the page would tell the user an answer " +
			"is incomplete when it is not (v1.27.0's defect, from the other side)")
	}
	if got.Messages[1].Content != "Three live forms." {
		t.Fatalf("stored answer = %q", got.Messages[1].Content)
	}
}
