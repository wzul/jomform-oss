// The streamed half of POST /v1/rest/chat (card t_148d7100).
//
// WHY SSE ON THE SAME ROUTE. The chat already has one authenticated, rate-limited,
// workspace-scoped POST whose answer is an exchange of several model calls (one
// per tool round-trip, plus the one that answers). Adding a second route would
// duplicate that chain — the auth check, the availability check, the per-user
// bucket, the scope resolution — and any of the four could then drift between the
// two doors. So the SAME route answers in one of two ways, chosen by the caller:
//
//	POST /v1/rest/chat                      -> one JSON body (unchanged)
//	POST /v1/rest/chat?stream=1             -> text/event-stream
//	   (or `Accept: text/event-stream`)
//
// THE QUERY PARAM IS WHAT THE DASHBOARD USES, and that is not laziness: the
// browser's request passes through web/src/app/api/[...path]/route.ts, which
// forwards method, body, cookie, auth and the client address but NOT the Accept
// header. `Accept` is still honoured here for a caller that talks to the API
// directly (curl, a test), so the semantics are standard where they can be.
//
// FRAMES. Every frame is one `data:` line carrying a JSON object whose `type` is
// the discriminator — `step`, `delta`, `retract`, `done` or `error`. Putting the
// type inside the payload (rather than in an SSE `event:` line) means a client
// parser reads ONE field per frame and cannot half-implement the envelope:
//
//	data: {"type":"step","tool":"list_forms"}
//	data: {"type":"delta","text":"You have "}
//	data: {"type":"delta","text":"3 live forms."}
//	data: {"type":"done","result":{...the same body POST would have returned...}}
//
// AN ERROR AFTER THE FIRST BYTE CANNOT BE A STATUS CODE. The pre-stream refusals
// (401, 400, 403, 429, 503) all happen BEFORE the header is written, so every
// one of them keeps its exact status and body and the page's existing branches
// still fire. Once the stream is open the answer is 200 by construction, so a
// failure is delivered as an `error` frame with a machine code, and the page
// falls back to the copy it already has for an unavailable assistant.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/llm"
)

// streamingChatRunner is the optional capability a chat runner has when it can
// report progress while it works. The production value (*chat.Runner) implements
// it; the interface is separate from ChatRunner so a test fake that only knows
// `Run` keeps compiling and simply serves the JSON answer.
type streamingChatRunner interface {
	RunStream(ctx context.Context, history []chat.Turn, message string, emit chat.EventFunc) (*chat.Result, error)
}

// The production runner MUST be streamable. Without this assertion the type
// assertion in handleChat would silently fall back to JSON on every message and
// the feature would be dead in production with a green test suite — the exact
// "feature behind a mis-wired call site" shape.
var _ streamingChatRunner = (*chat.Runner)(nil)

// chatStreamEvent is one frame. Result is set only for `type:"done"`, and it is
// the SAME shape the non-streaming POST returns, so the page has one renderer
// for the finished answer rather than one per transport.
type chatStreamEvent struct {
	Type   string           `json:"type"`
	Tool   string           `json:"tool,omitempty"`
	Text   string           `json:"text,omitempty"`
	Result *chatResponseOut `json:"result,omitempty"`
	// Code names the failure on an `error` frame. It is a machine code, not a
	// sentence: the API does not know the visitor's language (issue #6177).
	Code string `json:"code,omitempty"`
}

// wantsChatStream reports whether this request asked for the streamed answer.
func wantsChatStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// streamChat answers one message as an SSE stream.
//
// It owns the frames and the flush; everything about WHICH answer is produced
// stays in the runner, so the streamed and one-shot paths cannot disagree about
// tool dispatch, refusals or proposals.
func (h *RESTHandlers) streamChat(
	w http.ResponseWriter,
	r *http.Request,
	runner streamingChatRunner,
	actx *AccountCtx,
	ctx context.Context,
	uid string,
	history []chat.Turn,
	msg string,
	sessionID string,
	sessionWorkspaceID string,
) {
	// DO NOT assert `w.(http.Flusher)` here. Under the real server chain the
	// response passes through middleware.Logger's statusRecorderMiddleware, which
	// embeds http.ResponseWriter and therefore promotes ONLY that interface's
	// methods — the wrapper is not a http.Flusher even though the writer
	// underneath is. That is deliberate: a forwarding Flush() on the wrapper would
	// make it ALWAYS satisfy the interface and hide a chain that cannot flush (see
	// the long comment in middleware/logging.go). The consequence here was that
	// the assertion failed on EVERY production request and this handler fell
	// quietly into chatJSONAnswer — the same issue-#615 shape, one level lower: a
	// dead feature with a green test suite, because the unit test's
	// httptest.ResponseRecorder is itself an http.Flusher.
	//
	// http.NewResponseController walks Unwrap() to the real writer and answers
	// honestly: nil when something in the chain can flush, ErrNotSupported when
	// nothing can. realtime.go solves the other SSE route the same way.
	rc := http.NewResponseController(w)

	head := w.Header()
	head.Set("Content-Type", "text/event-stream")
	// No cache anywhere on the path: a buffered or replayed transcript is both a
	// privacy problem and a wrong answer. X-Accel-Buffering disables nginx's
	// response buffering for a hop that has not been told to flush.
	head.Set("Cache-Control", "no-cache, no-store")
	head.Set("X-Accel-Buffering", "no")
	head.Set("Connection", "keep-alive")

	// Headers BEFORE the flush probe: a flush commits the status line and the
	// headers set at that moment (realtime.go carries the same note). On a chain
	// that cannot flush nothing has been committed, so the fallback below still
	// answers with its own status and Content-Type.
	//
	// No WriteHeader here: the probe itself commits 200, and an explicit call
	// after it would make net/http log "superfluous WriteHeader" on every stream.
	if err := rc.Flush(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			// No flush capability means every event would sit in a buffer until
			// the handler returned — a "stream" that arrives all at once, which
			// is the defect this feature exists to remove. Answer the one-shot
			// way instead of pretending. Nothing was committed by the failed
			// probe, so dropping the SSE headers keeps this response what the
			// non-streaming POST answers.
			head.Del("Content-Type")
			head.Del("Cache-Control")
			head.Del("X-Accel-Buffering")
			head.Del("Connection")
			h.chatJSONAnswer(w, r, actx, ctx, uid, history, msg, sessionID, sessionWorkspaceID)
			return
		}
		// Any other error came from the connection while committing the header:
		// the client is gone and there is nobody left to read either shape.
		return
	}

	// A long answer is not a broken connection. The server sets WriteTimeout:
	// 60s (cmd/jomform-api/main.go), an absolute per-connection deadline that a
	// model taking longer than a minute would hit mid-stream — "fixed" would mean
	// "streams for a minute". Clear it for this connection; a chain that cannot
	// set deadlines is not fatal, it just inherits the timeout (issue #615's
	// second half, as in realtime.go).
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("chat_stream_no_write_deadline", "err", err)
	}

	// THE PARTIAL ANSWER, KEPT (card t_babeaa84). emit is the ONLY place that sees
	// every fragment, so it is where the text the reader is looking at is
	// assembled — and on Stop that buffer is the whole of what survives, so it has
	// to be the same text the page shows. `retract` empties it, exactly as the
	// page's own accumulator does (web/src/lib/chatStream.ts): a round that ended
	// by calling a tool was not an answer, and storing "let me check your forms"
	// as the partial reply would file a discarded thought as the user's record of
	// what the assistant said. A `step` does NOT clear it — the runner emits an
	// explicit retract before the step when there is anything to withdraw.
	var partial strings.Builder
	emit := func(ev chat.Event) error {
		switch ev.Type {
		case chat.EventDelta:
			partial.WriteString(ev.Text)
		case chat.EventRetract:
			partial.Reset()
		}
		return writeChatFrame(w, rc, chatStreamEvent{
			Type: ev.Type, Tool: ev.Tool, Text: ev.Text,
		})
	}

	res, err := runner.RunStream(ctx, history, msg, emit)
	if err != nil {
		// A GONE CLIENT is not a failure to report: the write that failed is what
		// stopped the runner, and there is nobody left to read a frame.
		if r.Context().Err() != nil || errors.Is(err, context.Canceled) {
			// …but the EXCHANGE is not abandoned (card t_babeaa84). The user's
			// question and the fragment they had already read are the only copy of
			// that moment, and until this call they were stored nowhere: the next
			// load of the conversation showed neither, so Stop looked like it had
			// deleted a question the user had typed and an answer they had watched
			// arrive.
			//
			// This runs on a context detached from the (already cancelled)
			// request — see chatPersistCtx — because Stop IS that cancellation.
			h.chatPersistStoppedExchange(r, uid, sessionWorkspaceID, sessionID, msg, partial.String())
			slog.Info("chat_stream_client_gone",
				"session_id", sessionID,
				"partial_chars", len([]rune(partial.String())),
			)
			return
		}
		logStreamFailure(r, err)
		_ = writeChatFrame(w, rc, chatStreamEvent{Type: "error", Code: "unavailable"})
		return
	}

	out := h.chatAnswer(r, actx, uid, sessionWorkspaceID, sessionID, msg, res)
	_ = writeChatFrame(w, rc, chatStreamEvent{Type: "done", Result: &out})
}

// chatJSONAnswer is the one-shot answer, factored out so the no-flush fallback in
// streamChat produces byte-for-byte what POST /v1/rest/chat always produced.
func (h *RESTHandlers) chatJSONAnswer(w http.ResponseWriter, r *http.Request, actx *AccountCtx, ctx context.Context, uid string, history []chat.Turn, msg, sessionID, sessionWorkspaceID string) {
	res, err := h.Chat.Run(ctx, history, msg)
	if err != nil {
		respondAIErr(w, r, err, "the chat assistant is not available")
		return
	}
	writeJSON(w, http.StatusOK, h.chatAnswer(r, actx, uid, sessionWorkspaceID, sessionID, msg, res))
}

// writeChatFrame writes one `data:` frame and flushes it.
//
// The write error is returned, not logged: it is how a disconnected browser
// reaches the runner and cancels the model work behind it. A frame that cannot
// be encoded is a programming error and is returned the same way — the caller
// stops the exchange rather than emitting a partial stream.
//
// The flush goes through the ResponseController, not a stored http.Flusher: the
// controller is what knows how to reach the real writer through the middleware
// layers (see the note in streamChat). A flush error is returned like a write
// error — the client is gone and the runner should stop.
func writeChatFrame(w http.ResponseWriter, rc *http.ResponseController, ev chatStreamEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	return rc.Flush()
}

// logStreamFailure keeps the operator's half of a mid-stream failure distinct
// from a rejected credential or a retired model, which are the two deployment
// states the AI surfaces already know how to report.
func logStreamFailure(r *http.Request, err error) {
	switch {
	case errors.Is(err, llm.ErrCredential):
		logError(r, "llm_credential_rejected", err)
	case errors.Is(err, llm.ErrModelUnavailable):
		logError(r, "llm_model_unavailable", err)
	default:
		logError(r, "chat_stream_failed", err)
	}
}
