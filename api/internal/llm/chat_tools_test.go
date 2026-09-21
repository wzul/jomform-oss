// The tool-calling half of the LLM client (in-app chat, phase 1).
//
// WHY THIS NEEDS ITS OWN TEST. ChatWithTools is the boundary where a model's
// output becomes a function call the chat will act on. Two properties matter and
// neither is visible from the chat package:
//
//  1. THE WIRE SHAPE. The request must use the OpenAI-compatible "tools" array
//     and the response's tool_calls must be carried through with their call id,
//     name and RAW arguments string. A dropped id makes the follow-up "tool"
//     turn unattributable — every provider rejects it — and a re-marshalled
//     arguments object would reinterpret the model's JSON before the executor
//     validates it.
//  2. THE UNTRUSTED BOUNDARY. Arguments are passed through as the raw string the
//     model emitted. This client must not "helpfully" parse or repair them; the
//     EXECUTOR validates, because that is where the schema and the allowlist
//     live.
//
// These run against an httptest server, so no network and no API key.
package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureServer answers every request with body and records the request the
// client sent, so both directions of the wire contract are assertable.
func captureServer(t *testing.T, status int, contentType, body string) (*Client, *[]byte) {
	t.Helper()
	sent := new([]byte)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		*sent = b
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	// The client's baseURL + "/chat/completions" is the target.
	return &Client{baseURL: srv.URL, model: "test-model", http: srv.Client()}, sent
}

// TestChatWithToolsSendsTheOpenAIToolsShape pins the request.
//
// The shape is what makes the model able to call anything at all: a tool whose
// parameters are omitted, or a tool_calls entry missing its function name, would
// be silently dropped by the provider and the chat would answer "I have no tool
// for that" with every test green.
func TestChatWithToolsSendsTheOpenAIToolsShape(t *testing.T) {
	client, sent := captureServer(t, 200, "application/json",
		`{"choices":[{"message":{"content":"ok"}}]}`)

	tools := []ToolDef{
		{Name: "list_forms", Description: "Senarai borang", Parameters: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`)},
		{Name: "submit_feedback", Description: "Failkan maklum balas", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	turns := []ChatTurn{
		{Role: "system", Content: "You are the assistant."},
		{Role: "user", Content: "borang saya?"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "c1", Name: "list_forms", Arguments: `{"limit":5}`}}},
		{Role: "tool", ToolCallID: "c1", Name: "list_forms", Content: `{"items":[]}`},
	}
	if _, err := client.ChatWithTools(context.Background(), turns, tools); err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}

	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
			Name       string `json:"name"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(*sent, &req); err != nil {
		t.Fatalf("the request the client sent is not valid JSON: %v (%s)", err, *sent)
	}
	if req.Model != "test-model" {
		t.Errorf("model = %q, want test-model", req.Model)
	}
	if req.Stream {
		t.Error("stream = true — the tool-calling path reads a single response body")
	}
	if len(req.Tools) != 2 {
		t.Fatalf("sent %d tools, want 2", len(req.Tools))
	}
	for _, tool := range req.Tools {
		if tool.Type != "function" {
			t.Errorf("tool type = %q, want function", tool.Type)
		}
		if tool.Function.Name == "" || tool.Function.Description == "" {
			t.Errorf("a tool was sent without a name/description: %+v", tool.Function)
		}
		if len(tool.Function.Parameters) == 0 {
			t.Errorf("tool %q was sent with no parameters — the model cannot call it correctly",
				tool.Function.Name)
		}
	}
	// The arguments must survive as the RAW string, not a re-marshalled object.
	if len(req.Messages) != 4 {
		t.Fatalf("sent %d messages, want 4", len(req.Messages))
	}
	asst := req.Messages[2]
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "c1" ||
		asst.ToolCalls[0].Function.Name != "list_forms" ||
		asst.ToolCalls[0].Function.Arguments != `{"limit":5}` {
		t.Errorf("the assistant tool_calls turn was not carried through verbatim: %+v", asst.ToolCalls)
	}
	res := req.Messages[3]
	if res.Role != "tool" || res.ToolCallID != "c1" || res.Name != "list_forms" {
		t.Errorf("the tool result is not attributable to its call: %+v", res)
	}
}

// TestChatWithToolsCarriesArgumentsThroughUnparsed is property 2: the client
// must not touch the model's arguments string. The executor validates; a client
// that parsed-and-repaired here would hide a malformed call from the allowlist
// and the schema check.
func TestChatWithToolsCarriesArgumentsThroughUnparsed(t *testing.T) {
	// Deliberately malformed (trailing comma) and hostile-looking: both must
	// arrive untouched.
	raw := `{"name":"'; DROP TABLE forms; --",,}`
	client, _ := captureServer(t, 200, "application/json",
		`{"choices":[{"message":{"tool_calls":[{"id":"c9","type":"function","function":{"name":"list_forms","arguments":`+
			jsonString(raw)+`}}]}}]}`)

	reply, err := client.ChatWithTools(context.Background(), []ChatTurn{{Role: "user", Content: "hai"}}, nil)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(reply.ToolCalls))
	}
	if reply.ToolCalls[0].Arguments != raw {
		t.Fatalf("arguments = %q, want the raw model output %q — the client must not parse or "+
			"repair them; the executor validates the schema and the allowlist",
			reply.ToolCalls[0].Arguments, raw)
	}
	if reply.ToolCalls[0].ID != "c9" || reply.ToolCalls[0].Name != "list_forms" {
		t.Fatalf("tool call identity = %+v, want id c9 / name list_forms", reply.ToolCalls[0])
	}
}

// TestChatWithToolsReturnsProseWhenNoToolIsCalled: the common case, and the one
// the runner's loop terminates on.
func TestChatWithToolsReturnsProseWhenNoToolIsCalled(t *testing.T) {
	client, _ := captureServer(t, 200, "application/json",
		`{"choices":[{"message":{"content":"  Anda ada 3 borang.  "}}]}`)

	reply, err := client.ChatWithTools(context.Background(), []ChatTurn{{Role: "user", Content: "hai"}}, nil)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if reply.Content != "Anda ada 3 borang." {
		t.Fatalf("content = %q, want it trimmed", reply.Content)
	}
	if len(reply.ToolCalls) != 0 {
		t.Fatalf("tool calls = %+v, want none", reply.ToolCalls)
	}
}

// TestChatWithToolsErrorPaths: an API error, a non-JSON body and an empty answer
// must all be ERRORS. A silent empty success is the failure this avoids: the
// runner would treat it as the model having nothing to say and show the user a
// blank reply.
func TestChatWithToolsErrorPaths(t *testing.T) {
	t.Run("api error object", func(t *testing.T) {
		client, _ := captureServer(t, 200, "application/json",
			`{"error":{"message":"model overloaded"}}`)
		_, err := client.ChatWithTools(context.Background(), nil, nil)
		if err == nil || !strings.Contains(err.Error(), "model overloaded") {
			t.Fatalf("err = %v, want the API's message", err)
		}
	})
	t.Run("non-200 status", func(t *testing.T) {
		client, _ := captureServer(t, 500, "application/json", `{"error":{"message":"boom"}}`)
		if _, err := client.ChatWithTools(context.Background(), nil, nil); err == nil {
			t.Fatal("a 500 was treated as success")
		}
	})
	t.Run("html body", func(t *testing.T) {
		client, _ := captureServer(t, 200, "text/html", "<html>landing page</html>")
		_, err := client.ChatWithTools(context.Background(), nil, nil)
		if err == nil || !strings.Contains(err.Error(), "Content-Type") {
			t.Fatalf("err = %v, want a Content-Type complaint (a misconfigured endpoint served "+
				"HTML)", err)
		}
	})
	t.Run("empty choices", func(t *testing.T) {
		client, _ := captureServer(t, 200, "application/json", `{"choices":[]}`)
		if _, err := client.ChatWithTools(context.Background(), nil, nil); err == nil {
			t.Fatal("an empty choices array was treated as success")
		}
	})
	t.Run("blank content and no calls", func(t *testing.T) {
		client, _ := captureServer(t, 200, "application/json",
			`{"choices":[{"message":{"content":"   "}}]}`)
		if _, err := client.ChatWithTools(context.Background(), nil, nil); err == nil {
			t.Fatal("a whitespace-only answer was treated as success — the user would see a blank " +
				"reply with no error")
		}
	})
	t.Run("tool call with no function name", func(t *testing.T) {
		// A provider that emits a nameless function must not produce a call the
		// dispatcher would then try to route (an empty name is not in any
		// allowlist, but dropping it here keeps the runner's steps honest).
		client, _ := captureServer(t, 200, "application/json",
			`{"choices":[{"message":{"content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"","arguments":"{}"}}]}}]}`)
		reply, err := client.ChatWithTools(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("ChatWithTools: %v", err)
		}
		if len(reply.ToolCalls) != 0 {
			t.Fatalf("a nameless function call was kept: %+v", reply.ToolCalls)
		}
		if reply.Content != "ok" {
			t.Fatalf("content = %q, want the prose alongside the dropped call", reply.Content)
		}
	})
}

// TestPostSurfacesANonJSONContentTypeForPlainChatToo pins the shared helper: the
// legal-page generator and the form builder go through the same post(), so a
// misconfigured endpoint must fail the same way there.
func TestPostSurfacesANonJSONContentTypeForPlainChatToo(t *testing.T) {
	client, _ := captureServer(t, 200, "text/html", "<html>landing page</html>")
	if _, err := client.GenerateFormConfig(context.Background(), "borang untuk kedai kopi"); err == nil {
		t.Fatal("GenerateFormConfig accepted an HTML response")
	}
}

// jsonString renders s as a JSON string literal for embedding in a canned body.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
