// Streamed progress from the chat runner (card t_148d7100).
//
// WHAT THIS FILE OWNS, and why each arm is needed:
//
//  1. THE STEP EVENT ARRIVES BEFORE THE TOOL RUNS. The card's report is that a
//     tool step was visible only when it was REFUSED, so the ordinary case showed
//     the user nothing while the assistant was reading. An event emitted after
//     the dispatch would render the same static wait with a label on it, so the
//     assertion here is on ORDER (the runner's event log and the tool caller's
//     own log share one sequence) rather than on presence.
//  2. A TOOL ROUND'S PROSE IS RETRACTED. A model that writes "let me check" and
//     then calls a tool has not answered; leaving that fragment in a transcript
//     under a bubble that then fills with the real answer is a lie about what the
//     assistant said.
//  3. STREAMING DOES NOT CHANGE THE ANSWER. The same script through Run and
//     RunStream must produce the same Result — the streaming path is a reporting
//     channel, not a second implementation of the loop.
//  4. A MODEL CLIENT THAT CANNOT STREAM STILL ANSWERS. The capability is an
//     optional interface (a test double need not have it, and neither does any
//     future client); the fallback hands over the whole answer as one fragment.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/llm"
)

// streamLLM is scriptedLLM plus the streaming half: fragments[round] is what the
// model "writes" on that round-trip.
type streamLLM struct {
	scriptedLLM
	round     int
	fragments [][]string
}

func (s *streamLLM) ChatWithToolsStream(ctx context.Context, turns []llm.ChatTurn, tools []llm.ToolDef, onDelta func(string) error) (llm.ChatReply, error) {
	reply, err := s.scriptedLLM.ChatWithTools(ctx, turns, tools)
	if err != nil {
		return llm.ChatReply{}, err
	}
	round := s.round
	s.round++
	if round < len(s.fragments) {
		for _, f := range s.fragments[round] {
			if err := onDelta(f); err != nil {
				return llm.ChatReply{}, err
			}
		}
	}
	return reply, nil
}

var errClientGone = errors.New("client went away")

// loggingCaller writes into the SAME sequence as the runner's events, so the
// test can assert that the step event preceded the call rather than merely that
// both happened.
type loggingCaller struct {
	log  *[]string
	text string
}

func (c *loggingCaller) call(_ context.Context, name, _ string) (string, bool, error) {
	*c.log = append(*c.log, "tool:"+name)
	return c.text, false, nil
}

func (c *loggingCaller) close() error { return nil }

func newStreamingRunner(t *testing.T, model *streamLLM, caller *loggingCaller, log *[]string) *Runner {
	t.Helper()
	r := &Runner{llm: model}
	r.newSession = func(context.Context) (toolCaller, error) { return caller, nil }
	r.toolDefsFn = func() []llm.ToolDef {
		return []llm.ToolDef{{Name: "list_forms", Parameters: []byte(`{"type":"object"}`)}}
	}
	return r
}

func collector(log *[]string) EventFunc {
	return func(ev Event) error {
		switch ev.Type {
		case EventStep:
			*log = append(*log, "ev:step:"+ev.Tool)
		case EventDelta:
			*log = append(*log, "ev:delta:"+ev.Text)
		case EventRetract:
			*log = append(*log, "ev:retract")
		default:
			*log = append(*log, "ev:?"+ev.Type)
		}
		return nil
	}
}

func TestRunStreamReportsTheToolStepBeforeTheToolRuns(t *testing.T) {
	var log []string
	model := &streamLLM{
		scriptedLLM: scriptedLLM{replies: []llm.ChatReply{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "list_forms", Arguments: `{}`}}},
			{Content: "Three live forms."},
		}},
		fragments: [][]string{nil, {"Three ", "live ", "forms."}},
	}
	caller := &loggingCaller{log: &log, text: `{"items":["a","b","c"]}`}
	r := newStreamingRunner(t, model, caller, &log)

	res, err := r.RunStream(chatCtx(), nil, "which of my forms are live?", collector(&log))
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	want := []string{
		"ev:step:list_forms", // the label exists BEFORE the work
		"tool:list_forms",    // ...and the work really ran after it
		"ev:delta:Three ",
		"ev:delta:live ",
		"ev:delta:forms.",
	}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("progress sequence = %q\nwant %q\n(a step emitted after the dispatch would put tool:list_forms first, and the\n"+
			"user would watch a static \"thinking\" line through the whole round-trip)", log, want)
	}
	if res.Reply != "Three live forms." {
		t.Errorf("reply = %q", res.Reply)
	}
	if len(res.Steps) != 1 || res.Steps[0].Tool != "list_forms" || res.Steps[0].Status != StepCalled {
		t.Errorf("steps = %+v, want the finished step in the Result as well", res.Steps)
	}
}

func TestRunStreamRetractsProseFromAToolRound(t *testing.T) {
	var log []string
	model := &streamLLM{
		scriptedLLM: scriptedLLM{replies: []llm.ChatReply{
			// A round that thinks out loud AND calls a tool.
			{Content: "Let me check that.", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "list_forms", Arguments: `{}`}}},
			{Content: "You have three."},
		}},
		fragments: [][]string{{"Let me ", "check that."}, {"You have three."}},
	}
	caller := &loggingCaller{log: &log, text: `{"items":3}`}
	r := newStreamingRunner(t, model, caller, &log)

	res, err := r.RunStream(chatCtx(), nil, "how many forms?", collector(&log))
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	want := []string{
		"ev:delta:Let me ",
		"ev:delta:check that.",
		"ev:retract", // that was not the answer
		"ev:step:list_forms",
		"tool:list_forms",
		"ev:delta:You have three.",
	}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("progress sequence = %q\nwant %q", log, want)
	}
	if res.Reply != "You have three." {
		t.Errorf("reply = %q, want ONLY the answer — the preamble belongs to a discarded round", res.Reply)
	}
}

func TestRunStreamReturnsTheSameAnswerAsRun(t *testing.T) {
	script := func() []llm.ChatReply {
		return []llm.ChatReply{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "list_forms", Arguments: `{}`}}},
			{Content: "Three live forms."},
		}
	}

	var plainLog []string
	plainCaller := &loggingCaller{log: &plainLog, text: `{"items":3}`}
	rPlain := newStreamingRunner(t, &streamLLM{scriptedLLM: scriptedLLM{replies: script()}}, plainCaller, &plainLog)
	gotPlain, err := rPlain.Run(chatCtx(), nil, "which forms?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var streamLog []string
	streamed := &streamLLM{
		scriptedLLM: scriptedLLM{replies: script()},
		fragments:   [][]string{nil, {"Three live ", "forms."}},
	}
	streamCaller := &loggingCaller{log: &streamLog, text: `{"items":3}`}
	rStream := newStreamingRunner(t, streamed, streamCaller, &streamLog)
	gotStream, err := rStream.RunStream(chatCtx(), nil, "which forms?", collector(&streamLog))
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}

	if gotPlain.Reply != gotStream.Reply {
		t.Errorf("reply differs: Run=%q RunStream=%q", gotPlain.Reply, gotStream.Reply)
	}
	if len(gotPlain.Steps) != len(gotStream.Steps) || gotPlain.Steps[0].Tool != gotStream.Steps[0].Tool {
		t.Errorf("steps differ: Run=%+v RunStream=%+v", gotPlain.Steps, gotStream.Steps)
	}
}

func TestRunStreamFallsBackWhenTheClientCannotStream(t *testing.T) {
	// scriptedLLM does NOT implement ChatWithToolsStream — the capability is
	// optional, and this is the client that does not have it.
	var log []string
	model := &scriptedLLM{replies: []llm.ChatReply{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "list_forms", Arguments: `{}`}}},
		{Content: "Three live forms."},
	}}
	caller := &loggingCaller{log: &log, text: `{"items":3}`}
	r := &Runner{llm: model}
	r.newSession = func(context.Context) (toolCaller, error) { return caller, nil }
	r.toolDefsFn = func() []llm.ToolDef {
		return []llm.ToolDef{{Name: "list_forms", Parameters: []byte(`{"type":"object"}`)}}
	}

	res, err := r.RunStream(chatCtx(), nil, "which forms?", collector(&log))
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	if res.Reply != "Three live forms." {
		t.Errorf("reply = %q", res.Reply)
	}
	joined := strings.Join(log, "|")
	if !strings.Contains(joined, "ev:step:list_forms") {
		t.Errorf("no step event when the model client cannot stream: %q", log)
	}
	if !strings.Contains(joined, "ev:delta:Three live forms.") {
		t.Errorf("the answer must still reach the caller as one fragment: %q", log)
	}
}

func TestRunStreamStopsWhenTheReceiverFails(t *testing.T) {
	var log []string
	model := &streamLLM{
		scriptedLLM: scriptedLLM{replies: []llm.ChatReply{{Content: "Three live forms."}}},
		fragments:   [][]string{{"Three live forms."}},
	}
	caller := &loggingCaller{log: &log}
	r := newStreamingRunner(t, model, caller, &log)

	_, err := r.RunStream(chatCtx(), nil, "which forms?", func(Event) error { return errClientGone })
	if !errors.Is(err, errClientGone) {
		t.Fatalf("err = %v, want the receiver's error (it is how a gone browser stops the model work)", err)
	}
}
