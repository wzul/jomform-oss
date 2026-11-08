// The streamed half of POST /v1/rest/chat (card t_148d7100).
//
// WHAT THIS FILE OWNS. The runner's own streaming test (internal/chat) proves the
// events are produced in the right order; this one proves they reach the WIRE in
// an order and a framing a browser can read, and — just as important — that the
// route did not change shape for anybody who did not ask for a stream:
//
//  1. `?stream=1` answers text/event-stream, one `data:` frame per event, with
//     the answer's text arriving in SEVERAL frames (the acceptance criterion is
//     "the reply grew at least three times": with one `delta` frame the page can
//     only paint it once, whatever the browser does).
//  2. An ordinary POST still answers application/json, byte-shape unchanged.
//  3. A deployment whose runner cannot stream still answers a `?stream=1` caller
//     with the one-shot JSON body — the client's fallback, exercised server-side.
//  4. A REFUSAL BEFORE THE STREAM STILL GETS ITS STATUS. This is the arm that
//     catches the classic streaming regression: writing the 200 header before the
//     availability / rate-limit / auth checks, after which a 503 or a 429 becomes
//     an empty 200 stream the page renders as a silent failure.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
)

// streamingRunner is the fake runner with the optional streaming capability: it
// replays a fixed event script through emit, then answers with reply.
type streamingRunner struct {
	recordingRunner
	events []chat.Event
}

func (r *streamingRunner) Run(ctx context.Context, history []chat.Turn, message string) (*chat.Result, error) {
	return r.recordingRunner.Run(ctx, history, message)
}

func (r *streamingRunner) RunStream(ctx context.Context, history []chat.Turn, message string, emit chat.EventFunc) (*chat.Result, error) {
	for _, ev := range r.events {
		if err := emit(ev); err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gotRuns++
	if r.err != nil {
		return nil, r.err
	}
	if r.reply != nil {
		return r.reply, nil
	}
	return &chat.Result{Reply: "ok"}, nil
}

// framesOf decodes the `data:` frames of an SSE body, in order.
func framesOf(t *testing.T, body string) []chatStreamEvent {
	t.Helper()
	var out []chatStreamEvent
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev chatStreamEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("frame is not valid JSON (%q): %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func TestChatStreamDeliversTheAnswerInSeveralFrames(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{
			ready: true,
			reply: &chat.Result{Reply: "Three live forms.", Steps: []chat.Step{{Tool: "list_forms", Status: chat.StepCalled}}},
		},
		events: []chat.Event{
			{Type: chat.EventStep, Tool: "list_forms"},
			{Type: chat.EventDelta, Text: "Three "},
			{Type: chat.EventDelta, Text: "live "},
			{Type: chat.EventDelta, Text: "forms."},
		},
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?stream=1", `{"message":"which forms are live?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	frames := framesOf(t, rec.Body.String())

	var deltas int
	var sawStep bool
	for i, f := range frames {
		if f.Type == chat.EventStep {
			sawStep = true
			if i != 0 {
				t.Errorf("the step frame is at position %d; it must be the FIRST frame so the page can show\n"+
					"the work while it happens rather than after the answer", i)
			}
		}
		if f.Type == chat.EventDelta {
			deltas++
		}
	}
	if !sawStep {
		t.Error("no step frame: the tool call is invisible while it runs, which is the defect this card fixes")
	}
	if deltas < 3 {
		t.Fatalf("delta frames = %d, want at least 3 — the page can only grow the bubble as many times as it\n"+
			"receives text, so one frame is a stream that looks exactly like the non-streaming answer", deltas)
	}
	last := frames[len(frames)-1]
	if last.Type != "done" || last.Result == nil {
		t.Fatalf("last frame = %+v, want a done frame carrying the result", last)
	}
	if last.Result.Reply != "Three live forms." {
		t.Errorf("done.result.reply = %q", last.Result.Reply)
	}
	if len(last.Result.Steps) != 1 || last.Result.Steps[0].Tool != "list_forms" {
		t.Errorf("done.result.steps = %+v, want the finished step list (the frames are progress; this is the record)", last.Result.Steps)
	}
	if last.Result.Limits.MessagesPerHour == 0 {
		t.Error("done.result.limits is empty — the page uses these to explain a 429 and to cap history")
	}
}

func TestChatWithoutTheStreamFlagIsStillOneJSONBody(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{ready: true, reply: &chat.Result{Reply: "Three live forms."}},
		events:          []chat.Event{{Type: chat.EventDelta, Text: "ignored"}},
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"hi"}`)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json for a request that did not ask to stream", ct)
	}
	var out chatResponseOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not the JSON answer: %v (%s)", err, rec.Body.String())
	}
	if out.Reply != "Three live forms." {
		t.Errorf("reply = %q", out.Reply)
	}
}

func TestChatStreamFallsBackToJSONWhenTheRunnerCannotStream(t *testing.T) {
	// recordingRunner does NOT implement RunStream: the capability is optional, and
	// this is the deployment shape that does not have it.
	runner := &recordingRunner{ready: true, reply: &chat.Result{Reply: "Three live forms."}}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?stream=1", `{"message":"hi"}`)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q — a runner without the streaming capability must answer the one-shot body\n"+
			"(the page reads either), never an empty stream", ct)
	}
	var out chatResponseOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not the JSON answer: %v", err)
	}
	if out.Reply != "Three live forms." {
		t.Errorf("reply = %q", out.Reply)
	}
}

func TestChatStreamRefusalHappensBeforeTheStreamOpens(t *testing.T) {
	// available:false is a pre-stream refusal (no model credential). It must keep
	// its 503: if the handler wrote the SSE header first, the page would receive a
	// 200 with no frames and render an empty answer instead of "the assistant is
	// not available".
	runner := &streamingRunner{recordingRunner: recordingRunner{ready: false}}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?stream=1", `{"message":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; a stream header written before the availability check turns this into\n"+
			"an empty 200 stream. body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q — the stream must not be opened for a refusal", ct)
	}
	if runner.gotRuns != 0 {
		t.Errorf("the runner was asked to answer %d times for an unavailable deployment", runner.gotRuns)
	}
}

// TestChatStreamAnswersOnTheAcceptHeaderToo keeps the standard HTTP semantics: a
// client that cannot put a query string on it (curl -H 'Accept: text/event-stream')
// still gets a stream.
func TestChatStreamAnswersOnTheAcceptHeaderToo(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{ready: true, reply: &chat.Result{Reply: "ok"}},
		events:          []chat.Event{{Type: chat.EventDelta, Text: "o"}, {Type: chat.EventDelta, Text: "k"}},
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	r := httptest.NewRequest(http.MethodPost, "/v1/rest/chat", strings.NewReader(`{"message":"hi","history":[]}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.AddCookie(&http.Cookie{Name: "jf_session", Value: token})
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, r)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream for an Accept: text/event-stream request", ct)
	}
	if got := len(framesOf(t, rec.Body.String())); got != 3 {
		t.Errorf("frames = %d, want 2 deltas + 1 done", got)
	}
}
