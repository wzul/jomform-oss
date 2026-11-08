// The streamed half of POST /v1/rest/chat, over the chain production serves.
//
// WHY THESE TESTS EXIST. chat_stream_test.go drives the handler through a bare
// httptest.ResponseRecorder, and a ResponseRecorder IS an http.Flusher itself.
// So that file passed while the streamed answer was dead in production: under
// middleware.Logger the writer is a *statusRecorderMiddleware, which embeds
// http.ResponseWriter (promoting only that interface's methods) and exposes just
// Unwrap() — the issue-#615 fix. `fl, ok := w.(http.Flusher)` therefore failed on
// every real request and the handler silently answered one JSON body instead.
//
// These tests mount Server.Handler() (RequestID → Logger → recoverer → Secure)
// over a real listener and speak HTTP over the wire, which is the only shape that
// can see the wrapper at all. They are the regression the issue asks for.
//
// Integration: needs JOMFORM_TEST_DATABASE_URL (scratch schema) and
// JOMFORM_TEST_VALKEY_URL; skips otherwise (via newChatEnv).
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/config"
)

// chatStreamServer wires the same RESTHandlers the chat tests use behind the
// FULL production middleware stack and returns a live base URL plus a session
// cookie for a freshly created user. writeTimeout is applied to the test server
// (0 leaves the default), so a test can make the server's absolute write
// deadline short enough to observe.
func chatStreamServer(t *testing.T, runner ChatRunner, writeTimeout time.Duration) (baseURL, token string) {
	t.Helper()
	e := newChatEnv(t, runner)
	_, _, token = e.newUser(t)

	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{REST: e.h})

	ts := httptest.NewUnstartedServer(srv.Handler())
	if writeTimeout > 0 {
		ts.Config.WriteTimeout = writeTimeout
	}
	ts.Start()
	t.Cleanup(ts.Close)

	return ts.URL, token
}

// postChatStream issues the streamed POST over the wire.
func postChatStream(t *testing.T, ctx context.Context, baseURL, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/rest/chat?stream=1", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/rest/chat?stream=1: %v", err)
	}
	return resp
}

// TestChatStreamIsSSEThroughTheFullMiddlewareChain is the regression test the
// card asks for: `?stream=1` must commit Content-Type text/event-stream and
// deliver SEVERAL `data:` frames over the real chain.
//
// Pre-fix this fails on the first assertion: the logging wrapper is not a
// http.Flusher, so streamChat took the JSON fallback and the response is
// `application/json; charset=utf-8` in one body — exactly the production
// symptom (200 JSON, one 1.7 KB chunk, zero `data:` frames).
func TestChatStreamIsSSEThroughTheFullMiddlewareChain(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{
			ready: true,
			reply: &chat.Result{
				Reply: "Three live forms.",
				Steps: []chat.Step{{Tool: "list_forms", Status: chat.StepCalled}},
			},
		},
		events: []chat.Event{
			{Type: chat.EventStep, Tool: "list_forms"},
			{Type: chat.EventDelta, Text: "Three "},
			{Type: chat.EventDelta, Text: "live "},
			{Type: chat.EventDelta, Text: "forms."},
		},
	}
	baseURL, token := chatStreamServer(t, runner, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp := postChatStream(t, ctx, baseURL, token, `{"message":"which forms are live?"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readAll(t, resp.Body))
	}
	// The header must be on the COMMITTED response, not set after a flush.
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream. An application/json answer here means the "+
			"handler took the no-flush fallback: the middleware wrapper hides http.Flusher, so the "+
			"streamed chat is dead in production (the defect this test exists to catch)", ct)
	}

	br := bufio.NewReader(resp.Body)
	var frames []chatStreamEvent
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev chatStreamEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("frame is not valid JSON (%q): %v", line, err)
		}
		frames = append(frames, ev)
	}

	if len(frames) < 2 {
		t.Fatalf("data frames on the wire = %d, want the whole exchange (step, 3 deltas, done). "+
			"Fewer than 2 means the answer was not streamed at all", len(frames))
	}
	var deltas int
	for _, f := range frames {
		if f.Type == chat.EventDelta {
			deltas++
		}
	}
	if deltas < 3 {
		t.Fatalf("delta frames = %d, want at least 3 — one frame is a stream the page can only paint "+
			"once, which looks exactly like the non-streaming answer", deltas)
	}
	last := frames[len(frames)-1]
	if last.Type != "done" || last.Result == nil {
		t.Fatalf("last frame = %+v, want a done frame carrying the result", last)
	}
	if last.Result.Reply != "Three live forms." {
		t.Errorf("done.result.reply = %q", last.Result.Reply)
	}
}

// slowStreamingRunner emits one delta, waits past the server's WriteTimeout, then
// emits the rest. It is the shape a real answer has when the model pauses (a
// tool round-trip, a slow provider) and it is what proves the frames are flushed
// as they happen AND that the absolute write deadline was cleared.
type slowStreamingRunner struct {
	streamingRunner
	pause time.Duration
}

func (r *slowStreamingRunner) RunStream(ctx context.Context, history []chat.Turn, message string, emit chat.EventFunc) (*chat.Result, error) {
	if err := emit(chat.Event{Type: chat.EventDelta, Text: "first "}); err != nil {
		return nil, err
	}
	time.Sleep(r.pause)
	if err := emit(chat.Event{Type: chat.EventDelta, Text: "after the timeout"}); err != nil {
		return nil, err
	}
	return &chat.Result{Reply: "first after the timeout"}, nil
}

// TestChatStreamSurvivesTheWriteTimeout pins BOTH halves of "it really streams":
//
//   - the first frame arrives BEFORE the runner pauses, so the frame was flushed
//     on the wire rather than buffered until the handler returned;
//   - the frame emitted after the server's WriteTimeout still arrives, so the
//     absolute 60s deadline (cmd/jomform-api/main.go) — set per connection, not
//     per write — was cleared for this stream. Without the clear the connection
//     is torn down at the deadline and "streamed" means "streams for a minute".
func TestChatStreamSurvivesTheWriteTimeout(t *testing.T) {
	const writeTimeout = 1200 * time.Millisecond
	const pause = 2400 * time.Millisecond

	runner := &slowStreamingRunner{
		streamingRunner: streamingRunner{
			recordingRunner: recordingRunner{ready: true, reply: &chat.Result{Reply: "first after the timeout"}},
		},
		pause: pause,
	}
	baseURL, token := chatStreamServer(t, runner, writeTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	start := time.Now()
	resp := postChatStream(t, ctx, baseURL, token, `{"message":"summary for this month"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, readAll(t, resp.Body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	br := bufio.NewReader(resp.Body)
	var frames []chatStreamEvent
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev chatStreamEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("frame is not valid JSON (%q): %v", line, err)
		}
		if len(frames) == 0 && time.Since(start) > pause/2 {
			t.Fatalf("the first frame arrived %s after the request, but the runner emitted it immediately: "+
				"the frame sat in a buffer instead of being flushed, so the answer paints all at once",
				time.Since(start))
		}
		frames = append(frames, ev)
	}

	var sawLate bool
	for _, f := range frames {
		if f.Type == chat.EventDelta && f.Text == "after the timeout" {
			sawLate = true
		}
	}
	if !sawLate {
		t.Fatalf("the delta emitted after the %s WriteTimeout never arrived (%d frames): the stream "+
			"inherits the server's absolute write deadline and dies mid-answer", writeTimeout, len(frames))
	}
	if len(frames) == 0 || frames[len(frames)-1].Type != "done" {
		t.Fatalf("frames = %+v, want a done frame at the end", frames)
	}
}

// noFlushWriter is a response writer with NO Flush method and NO Unwrap: the
// chain genuinely cannot flush. The handler must say so by answering the
// one-shot JSON body — never an SSE Content-Type over a buffered stream the
// browser would receive in one go at the end.
type noFlushWriter struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (w *noFlushWriter) Header() http.Header         { return w.h }
func (w *noFlushWriter) WriteHeader(code int)        { w.code = code }
func (w *noFlushWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

// TestChatStreamFallsBackToJSONWhenTheChainCannotFlush keeps the honest-negative
// arm: a deployment whose writer chain cannot flush still answers the caller
// (the page reads either shape), with the JSON content type — not an SSE header
// over a body that arrives all at once.
func TestChatStreamFallsBackToJSONWhenTheChainCannotFlush(t *testing.T) {
	runner := &streamingRunner{
		recordingRunner: recordingRunner{ready: true, reply: &chat.Result{Reply: "Three live forms."}},
		events:          []chat.Event{{Type: chat.EventDelta, Text: "ignored"}},
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/rest/chat?stream=1", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})

	w := &noFlushWriter{h: http.Header{}}
	e.mux.ServeHTTP(w, req)

	if ct := w.h.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json when nothing in the chain can flush", ct)
	}
	if w.h.Get("X-Accel-Buffering") != "" || strings.Contains(w.h.Get("Cache-Control"), "no-store") {
		t.Errorf("stream-only headers leaked into the JSON fallback: %v", w.h)
	}
	var out chatResponseOut
	if err := json.Unmarshal(w.buf.Bytes(), &out); err != nil {
		t.Fatalf("body is not the JSON answer: %v (%s)", err, w.buf.String())
	}
	if out.Reply != "Three live forms." {
		t.Errorf("reply = %q", out.Reply)
	}
}

// readAll drains a (short) body for a failure message.
func readAll(t *testing.T, body io.Reader) string {
	t.Helper()
	b, _ := io.ReadAll(io.LimitReader(body, 4096))
	return string(b)
}
