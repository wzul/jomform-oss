// Streaming chat-completion support for the in-app assistant (card t_148d7100).
//
// THE DEFECT THIS CLOSES. A chat message is one POST that answers only once the
// model has finished: measured on the staging build, the transcript jumped from
// 45 to 361 characters in a single frame after 2–8 seconds of a static
// "Looking that up…". Nothing was wrong with the model — the transport never
// asked the provider to stream, so there was nothing to show in between.
//
// WHY THIS FILE IS THE SAME CLIENT. AGENTS.md is explicit that there is no
// second LLM client: this is a second METHOD on the existing one, sharing its
// base URL, key, status mapping and (via toolRequestBody) its request shape. A
// separate client would drift on the typed 401/403 and 404/410 answers that the
// AI surfaces report as "unavailable".
//
// THE WIRE SHAPE is the provider's OpenAI-compatible SSE stream:
//
//	data: {"choices":[{"delta":{"content":"Hel"}}]}
//	data: {"choices":[{"delta":{"content":"lo"}}]}
//	data: [DONE]
//
// Tool calls stream too, as fragments keyed by `index` — the name usually arrives
// once and the arguments arrive in pieces, so they are accumulated per index
// rather than parsed per frame.
//
// A PROVIDER (OR PROXY) THAT IGNORES `stream: true` answers one ordinary JSON
// body with an `application/json` content type. That is decoded through the same
// path ChatWithTools uses and the whole answer is handed to onDelta in ONE call,
// so the caller's answer is never lost to a missing stream — it is only less
// gradual. This is the fallback the card asks for, and it is chosen from the
// response's own Content-Type rather than from a retry.
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// streamChunk is one `data:` frame of the provider's stream. Only the fields
// this client reads are declared; the provider sends more.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatWithToolsStream is ChatWithTools with `stream: true`.
//
// onDelta is called for every fragment of PROSE the model writes, in order. It
// may return an error to abandon the exchange (the REST edge returns the write
// failure of a client that has gone away); that error is returned unchanged so
// the caller can test it with errors.Is.
//
// The returned ChatReply is the SAME shape ChatWithTools returns: the
// concatenated prose (trimmed, exactly as ChatWithTools trims) plus any tool
// calls the model asked for. A caller that streams and then ignores the reply
// would lose the tool calls, which is why the two travel together.
func (c *Client) ChatWithToolsStream(
	ctx context.Context,
	turns []ChatTurn,
	tools []ToolDef,
	onDelta func(string) error,
) (ChatReply, error) {
	body, err := toolRequestBody(c.model, turns, tools, true)
	if err != nil {
		return ChatReply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return ChatReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Ask for a stream AND say we can take one: a proxy that negotiates on
	// Accept would otherwise answer JSON even though the body asked to stream.
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatReply{}, fmt.Errorf("llm: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return ChatReply{}, providerStatusError(resp.StatusCode, raw)
	}

	// NOT A STREAM: the provider ignored `stream: true` (or a proxy buffered the
	// whole answer and relabelled it). Decode it exactly as ChatWithTools would.
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return decodeWholeReply(resp.Body, onDelta)
	}
	return readToolStream(resp.Body, onDelta)
}

// decodeWholeReply is the non-streaming fallback: one JSON body, delivered to
// onDelta in one piece so a streaming caller still ends up with the answer.
func decodeWholeReply(body io.Reader, onDelta func(string) error) (ChatReply, error) {
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return ChatReply{}, err
	}
	var out toolResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return ChatReply{}, fmt.Errorf("llm: response is not valid JSON: %w", err)
	}
	reply, err := replyFromResponse(out)
	if err != nil {
		return ChatReply{}, err
	}
	if reply.Content != "" && onDelta != nil {
		if err := onDelta(reply.Content); err != nil {
			return ChatReply{}, err
		}
	}
	return reply, nil
}

// readToolStream consumes the SSE frames until `[DONE]` (or EOF) and assembles
// one ChatReply out of them.
func readToolStream(body io.Reader, onDelta func(string) error) (ChatReply, error) {
	sc := bufio.NewScanner(body)
	// A model answer is long and one frame carries a chunk of it; the default
	// 64 KiB token limit is a real truncation risk for a long Markdown answer.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var content strings.Builder
	// fragments by index, in the order the provider first mentioned them.
	type frag struct {
		id, name string
		args     strings.Builder
	}
	order := []int{}
	byIndex := map[int]*frag{}

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			// Blank separators and comments (`: ping`) carry no payload.
			continue
		}
		// Only `data:` frames matter here; `event:`/`id:`/`retry:` lines are
		// part of SSE but the OpenAI-compatible shape puts the type inside the
		// JSON payload.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return ChatReply{}, fmt.Errorf("llm: stream frame is not valid JSON: %w", err)
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return ChatReply{}, fmt.Errorf("llm: %s", chunk.Error.Message)
		}
		for _, ch := range chunk.Choices {
			if txt := ch.Delta.Content; txt != "" {
				content.WriteString(txt)
				if onDelta != nil {
					if err := onDelta(txt); err != nil {
						return ChatReply{}, err
					}
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				f := byIndex[tc.Index]
				if f == nil {
					f = &frag{}
					byIndex[tc.Index] = f
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					f.id = tc.ID
				}
				// The name arrives once; later frames repeat it empty. Assigning
				// only when non-empty keeps a re-sent name from concatenating
				// into "list_formslist_forms".
				if tc.Function.Name != "" {
					f.name = tc.Function.Name
				}
				f.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return ChatReply{}, fmt.Errorf("llm: reading the stream: %w", err)
	}

	reply := ChatReply{Content: strings.TrimSpace(content.String())}
	for _, i := range order {
		f := byIndex[i]
		if f.name == "" {
			continue
		}
		reply.ToolCalls = append(reply.ToolCalls, ToolCall{
			ID: f.id, Name: f.name, Arguments: f.args.String(),
		})
	}
	if reply.Content == "" && len(reply.ToolCalls) == 0 {
		return ChatReply{}, fmt.Errorf("llm: empty response")
	}
	return reply, nil
}
