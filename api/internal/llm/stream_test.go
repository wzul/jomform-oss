// The streaming transport (card t_148d7100).
//
// WHAT THIS FILE OWNS, and why each arm exists:
//
//  1. THE FRAGMENTS ACTUALLY ARRIVE SEPARATELY. A "streaming" client that
//     buffers the whole body and then hands over one string is indistinguishable
//     from the non-streaming client at every level except the user's screen — and
//     the user's screen is what the card is about. The fake provider writes each
//     frame and FLUSHES, so the assertions are on what arrived per callback.
//  2. A TOOL CALL SURVIVES BEING STREAMED. The provider sends the function name
//     once and the arguments in pieces; a reader that parses each frame on its
//     own assembles `{"limit":` and drops the rest, and the assistant then calls
//     its tool with broken JSON. This is invisible in the prose tests.
//  3. THE FALLBACK IS REAL. A provider (or proxy) that ignores `stream: true`
//     answers one JSON body; the answer must still reach the caller, in one
//     piece, rather than becoming an empty reply.
//  4. THE TYPED DEPLOYMENT ERRORS SURVIVE. A 401 on the streaming call must still
//     be llm.ErrCredential, or the AI surface reports a broken deployment as a
//     generic 500 instead of "the assistant is unavailable".
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer answers one request with the given frames, each written and flushed
// on its own so the client sees them as separate reads.
func sseServer(t *testing.T, frames []string) (*Client, *string) {
	t.Helper()
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, f := range frames {
			_, _ = io.WriteString(w, f)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{baseURL: srv.URL, model: "test-model", http: srv.Client()}, &sent
}

func TestChatWithToolsStreamDeliversEachFragmentAsItArrives(t *testing.T) {
	client, sent := sseServer(t, []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"You have \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"3 live \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"forms.\"}}]}\n\n",
		"data: [DONE]\n\n",
	})

	var deltas []string
	reply, err := client.ChatWithToolsStream(
		context.Background(),
		[]ChatTurn{{Role: "user", Content: "which forms are live?"}},
		nil,
		func(s string) error { deltas = append(deltas, s); return nil },
	)
	if err != nil {
		t.Fatalf("ChatWithToolsStream: %v", err)
	}
	want := []string{"You have ", "3 live ", "forms."}
	if fmt.Sprint(deltas) != fmt.Sprint(want) {
		t.Errorf("fragments = %q, want %q (one callback per frame — a buffered answer would be ONE, and the\n"+
			"page could not grow the bubble while the model writes)", deltas, want)
	}
	if reply.Content != "You have 3 live forms." {
		t.Errorf("assembled reply = %q", reply.Content)
	}
	if !strings.Contains(*sent, `"stream":true`) {
		t.Errorf("the request did not ask the provider to stream: %s", *sent)
	}
}

func TestChatWithToolsStreamAssemblesAToolCallSplitAcrossFrames(t *testing.T) {
	client, _ := sseServer(t, []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"list_forms","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"limit\":"}}]}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"5}"}}]}}]}` + "\n\n",
		"data: [DONE]\n\n",
	})

	reply, err := client.ChatWithToolsStream(context.Background(), []ChatTurn{{Role: "user", Content: "x"}}, nil, nil)
	if err != nil {
		t.Fatalf("ChatWithToolsStream: %v", err)
	}
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1 (a per-frame parse would lose the name or the arguments)", len(reply.ToolCalls))
	}
	tc := reply.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "list_forms" || tc.Arguments != `{"limit":5}` {
		t.Errorf("assembled tool call = %+v", tc)
	}
}

func TestChatWithToolsStreamFallsBackToABufferedBody(t *testing.T) {
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"  Three live forms.  "}}]}`)
	}))
	t.Cleanup(srv.Close)
	client := &Client{baseURL: srv.URL, model: "test-model", http: srv.Client()}

	var deltas []string
	reply, err := client.ChatWithToolsStream(
		context.Background(),
		[]ChatTurn{{Role: "user", Content: "x"}},
		nil,
		func(s string) error { deltas = append(deltas, s); return nil },
	)
	if err != nil {
		t.Fatalf("a provider that ignored stream must still answer: %v", err)
	}
	if !strings.Contains(sent, `"stream":true`) {
		t.Errorf("the request must still ask to stream (the fallback is the provider's answer, not our guess): %s", sent)
	}
	if reply.Content != "Three live forms." {
		t.Errorf("reply = %q", reply.Content)
	}
	if len(deltas) != 1 || deltas[0] != "Three live forms." {
		t.Errorf("deltas = %q, want exactly one carrying the whole answer", deltas)
	}
}

func TestChatWithToolsStreamKeepsTheTypedCredentialError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	t.Cleanup(srv.Close)
	client := &Client{baseURL: srv.URL, model: "test-model", http: srv.Client()}

	_, err := client.ChatWithToolsStream(context.Background(), []ChatTurn{{Role: "user", Content: "x"}}, nil, nil)
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("err = %v, want ErrCredential (the surfaces answer 503 'unavailable' for it)", err)
	}
}

func TestChatWithToolsStreamStopsWhenTheReceiverGoesAway(t *testing.T) {
	client, _ := sseServer(t, []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"second\"}}]}\n\n",
		"data: [DONE]\n\n",
	})
	gone := errors.New("client went away")
	var deltas []string
	_, err := client.ChatWithToolsStream(
		context.Background(),
		[]ChatTurn{{Role: "user", Content: "x"}},
		nil,
		func(s string) error { deltas = append(deltas, s); return gone },
	)
	if !errors.Is(err, gone) {
		t.Fatalf("err = %v, want the receiver's own error (the runner returns it so the model work stops)", err)
	}
	if len(deltas) != 1 {
		t.Errorf("deltas = %q, want the receiver to stop the exchange after its first error", deltas)
	}
}

// TestToolRequestBodyStreamFlagAlone proves the two transports share ONE request
// shape: the streamed body differs from the buffered one only in `stream`, so a
// field added to one cannot silently go missing from the other.
func TestToolRequestBodyStreamFlagAlone(t *testing.T) {
	turns := []ChatTurn{
		{Role: "system", Content: "s"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "c1", Name: "list_forms", Arguments: "{}"}}},
	}
	tools := []ToolDef{{Name: "list_forms", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}
	buffered, err := toolRequestBody("m", turns, tools, false)
	if err != nil {
		t.Fatal(err)
	}
	streamed, err := toolRequestBody("m", turns, tools, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffered) == string(streamed) {
		t.Fatal("the streamed body is identical to the buffered one — `stream` is not set")
	}
	if strings.Replace(string(streamed), `"stream":true`, `"stream":false`, 1) != string(buffered) {
		t.Errorf("the two bodies differ by more than the stream flag:\n buffered=%s\n streamed=%s", buffered, streamed)
	}
}
