// One GET, one snapshot: the sidebar count and the transcript it is rendered
// with must describe the same instant.
//
// WHY THIS FILE EXISTS (card t_21caaf69). GET /v1/rest/chat/sessions/{id}
// answers with `session.message_count` and `messages` in ONE body, and the page
// draws both: the transcript as the thread, the count as the sidebar's badge.
// The handler used to read them as two independent statements, so under READ
// COMMITTED each took its own snapshot and the body could describe two
// different instants — a turn committing in between was in the transcript and
// not in the count.
//
// That is not a theoretical window. Stopping an answer persists the question and
// the partial AFTER the client has disconnected (chatPersistStoppedExchange), so
// the reload the user triggers by coming back is precisely the read that races
// the write. Measured on pristine main: TestChatStopMidAnswerStoresThePartialAndTheQuestion
// red with `message_count = 1` (and 0) against a two-turn transcript, 3 runs in
// 100. It reddened the merge train's whole-suite gate twice on unrelated PRs.
//
// The test below makes the race deterministic instead of hoping for it: one
// goroutine appends turns through the same store the answer path uses while the
// reader hits the route in a loop, and EVERY response must agree with itself.
// Reading two statements instead of one snapshot fails this within a few
// samples; a single REPEATABLE READ snapshot cannot fail it at all.
//
// Integration against a real Postgres + Valkey, skipped without the test env.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
)

// sessionReadSamples is how many GETs the reader takes while the writer is
// appending. The window this guards is the gap between two statements of one
// request, so a handful of samples would only catch it by luck; a few hundred
// makes a two-statement read fail essentially every run while costing this
// package well under a second.
const sessionReadSamples = 200

// TestChatSessionGetAnswersFromOneSnapshot is the regression the card asks for.
//
// Pre-fix it fails with the exact symptom the flake reported — a body whose
// message_count is lower than the number of turns in the same body. It cannot
// pass by accident: the writer keeps a commit running for the whole read loop,
// so the only way every sample agrees with itself is for both halves of the
// response to come from one database snapshot.
func TestChatSessionGetAnswersFromOneSnapshot(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	userID, workspaceID, token := e.newUserInWorkspace(t)
	s := e.createSession(t, token, "", "")

	// The writer is the ANSWER path's store call, not a second HTTP call: what
	// this test is about is the READ, and running the persist through the chat
	// route would drag the rate limiter into the loop for no gain.
	stop := make(chan struct{})
	writerDone := make(chan struct{})
	var (
		wErr     error
		mu       sync.Mutex
		appended int
	)
	go func() {
		defer close(writerDone)
		ctx := context.Background()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			// keep = chat.MaxTurns, exactly as chatPersistTurn does it, so the
			// conversation is trimmed the way production trims it.
			if _, err := e.h.ChatSessions.Append(ctx, userID, workspaceID, s.ID, role, "turn", nil, chat.MaxTurns); err != nil {
				mu.Lock()
				wErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			appended++
			mu.Unlock()
		}
	}()
	halt := func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		<-writerDone
	}
	defer halt()

	sawTurns := 0
	for i := 0; i < sessionReadSamples; i++ {
		rec := e.do(t, token, http.MethodGet, "/v1/rest/chat/sessions/"+s.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("sample %d: GET conversation = %d, want 200 (body %s)", rec.Code, i, rec.Body.String())
		}
		var detail chatSessionDetailWire
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("sample %d: decode conversation: %v (body %s)", i, err, rec.Body.String())
		}
		if n := len(detail.Messages); n > 0 {
			sawTurns++
		}
		if detail.Session.MessageCount != len(detail.Messages) {
			t.Fatalf("sample %d: message_count = %d but the response's own transcript holds %d turn(s) — "+
				"one GET described two different instants, so the sidebar would tell the user this "+
				"conversation is shorter than the thread they are reading (the count and the turns "+
				"must come from ONE database snapshot)",
				i, detail.Session.MessageCount, len(detail.Messages))
		}
	}
	halt()
	mu.Lock()
	err, wrote := wErr, appended
	mu.Unlock()
	if err != nil {
		t.Fatalf("the concurrent writer failed after %d append(s): %v — the test did not exercise the race", wrote, err)
	}
	if wrote == 0 || sawTurns == 0 {
		t.Fatalf("the test read %d response(s) with turns while %d turn(s) were appended, so it never "+
			"overlapped the writes and proves nothing", sawTurns, wrote)
	}
}
