// Progress events for the in-app assistant (card t_148d7100).
//
// THE DEFECT. The chat answered in one POST whose body arrived only when the
// model had finished. Measured on the staging build: the transcript jumped from
// 45 to 361 characters in a single frame, and the only thing on screen while it
// worked was a static "Looking that up…" — identical whether the server was
// calling a tool or writing the answer. A tool step was rendered ONLY when it was
// refused, so the ordinary case (the assistant looked something up and answered)
// showed the user nothing at all.
//
// WHAT IS AND IS NOT ON THIS WIRE. An Event carries the TOOL NAME, never a
// sentence: the API does not know the visitor's language (issue #6177, the rule
// every refusal and every proposal title already follows), so the dashboard owns
// the wording and resolves it from its own dictionary. There is deliberately no
// prose field here for a call site to print by accident.
package chat

import (
	"context"

	"github.com/wzul/jomform/api/internal/llm"
)

// Event types. They are the values the SSE payload's `type` field carries.
const (
	// EventStep says a tool call has STARTED. Emitted BEFORE the dispatch, so a
	// UI can say "Reading your forms…" while the work happens instead of after.
	// The finished step list still travels in the Result; this is progress, not
	// the record.
	EventStep = "step"
	// EventDelta carries a fragment of the answer's prose, in the order the model
	// wrote it.
	EventDelta = "delta"
	// EventRetract tells the receiver to discard the prose streamed since the
	// previous retract/step. It exists because a model may write "let me look
	// that up" and THEN call a tool: that text is not the answer, and leaving it
	// on screen would make a discarded thought look like a reply.
	EventRetract = "retract"
)

// Event is one progress update. Text is set for EventDelta, Tool for EventStep.
type Event struct {
	Type string `json:"type"`
	Tool string `json:"tool,omitempty"`
	Text string `json:"text,omitempty"`
}

// EventFunc receives events in order. Returning an error aborts the exchange —
// that is how the REST edge stops the model work when the browser has gone.
type EventFunc func(Event) error

// streamingLLM is the optional capability a model client has when it can deliver
// an answer in fragments. The production *llm.Client implements it; a test
// double need not, and a client that does not is answered in ONE piece by the
// fallback in ask — the UI still gets its text, just not gradually.
type streamingLLM interface {
	ChatWithToolsStream(ctx context.Context, turns []llm.ChatTurn, tools []llm.ToolDef, onDelta func(string) error) (llm.ChatReply, error)
}

// ask runs one model turn.
//
// WITH onDelta it prefers the streaming client. A client without the capability
// is still asked, and its whole answer is handed over as one fragment: a
// deployment whose model client cannot stream must not answer with an EMPTY
// transcript, and a caller that asked for progress has no way to tell the
// difference from the outside.
func (r *Runner) ask(ctx context.Context, turns []llm.ChatTurn, tools []llm.ToolDef, onDelta func(string) error) (llm.ChatReply, error) {
	if onDelta == nil {
		return r.llm.ChatWithTools(ctx, turns, tools)
	}
	if s, ok := r.llm.(streamingLLM); ok {
		return s.ChatWithToolsStream(ctx, turns, tools, onDelta)
	}
	reply, err := r.llm.ChatWithTools(ctx, turns, tools)
	if err != nil {
		return llm.ChatReply{}, err
	}
	if reply.Content != "" {
		if err := onDelta(reply.Content); err != nil {
			return llm.ChatReply{}, err
		}
	}
	return reply, nil
}
