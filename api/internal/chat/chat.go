// Package chat is the PHASE 1 in-app chat: the model may READ the caller's own
// account data and FILE FEEDBACK. It cannot do anything else.
//
// WHY A RUNNER AND NOT A SECOND MCP SERVER. The tools the model may call are the
// real registered MCP tools, dispatched over a real MCP session against the real
// server with the CALLER's identity attached. That is the whole design: the chat
// has no privileged path, no second implementation of a tool, and — most
// importantly — no enforcement of its own to get wrong. If a tool's gate refuses
// the caller, the refusal comes from the same code that refuses a badly scoped
// API key, and the chat just reports it.
//
// THREE INDEPENDENT LAYERS STAND BETWEEN THE MODEL AND A WRITE:
//
//  1. THE ALLOWLIST (this file). The model is offered a small, explicit set of
//     tools — the read tools a merchant needs about their own account, plus one
//     write (submit_feedback). A tool name outside the set is refused here and
//     never dispatched, so a prompt-injected instruction to "call delete_form"
//     has nothing to call.
//
//  2. THE IDENTITY (this file + mcpsrv). Every tool call runs under an identity
//     whose scopes are READONLY, whatever the caller's own credential can do. So
//     even if the allowlist were widened by mistake, every write tool the server
//     registers answers "scope tidak mencukupi" — the issue #645 gate.
//
//  3. THE SERVER'S OWN GATES (mcpsrv). Workspace role, ownership and scope are
//     re-checked per call, exactly as for an external agent.
//
// Layer 2 is what makes "the model cannot write" a property of the SERVER rather
// than of a prompt, and layer 1 is what keeps the model from wandering: offering
// 161 tools and trusting the gate would work, but it also invites the model to
// try. Both are asserted by tests, and the refusal tests are proven to BITE
// (see chat_refusal_test.go).
//
// UNTRUSTED CONTENT. Tool results are the caller's own data — product names,
// form answers, a customer's typed message. That data is wrapped in an explicit
// fence and the model is told, before and after it, that it is DATA and not
// instructions. Phase 1 has no write to reach, so a successful injection could
// at worst produce a wrong ANSWER; the fence exists because phase 2 (writes with
// confirmation) will depend on exactly this framing.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/feedbacksvc"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

const (
	// MaxTurns is how many conversation turns are kept when calling the model.
	// A transcript is client-supplied, so it is the client's word for how much
	// text this request costs; keeping the last MaxTurns bounds that. The chat
	// is a conversation about a recent question, not a document store.
	MaxTurns = 16
	// MaxTurnChars bounds ONE turn's text, for the same reason: a client must
	// not be able to make a single request arbitrarily expensive.
	MaxTurnChars = 4000
	// MaxMessageChars bounds one new user message, checked at the REST edge too
	// so the error is a 400 rather than a truncation.
	MaxMessageChars = 2000
	// MaxSteps caps tool round-trips per user message. A read question needs one
	// or two; a loop that keeps calling tools is a cost problem and usually a
	// sign the model is guessing. Hitting the cap still produces an answer from
	// what was already gathered.
	MaxSteps = 4
	// MaxToolResultChars bounds what one tool's output contributes to the
	// prompt. A 50-row sales page is far more than an answer needs, and the
	// uncapped version makes a single question cost as much as a dozen.
	MaxToolResultChars = 6000
	// MaxReplyChars bounds the model's own answer before it is stored/echoed.
	MaxReplyChars = 4000
)

// ToolSubmitFeedback is the ONE write capability the chat has, and it is not
// dispatched over MCP: it calls the shared feedback service directly, so the
// same validation, per-user daily limit and notification apply as on the
// /feedback page. It is the only name in the allowlist that is allowed to
// change anything.
const ToolSubmitFeedback = mcpsrv.ChatSubmitFeedbackTool

// allowedTools is the complete exposure set: the read allowlist plus the one
// write. Nothing else is ever offered to the model or dispatched.
//
// The allowlist itself lives in mcpsrv (ChatReadTools / ChatToolIsAllowed) so
// the call-graph guard that polices every registered tool's authorization also
// proves this list contains no writer. Keeping a second copy here is how the
// offered set and the dispatched set would drift apart.
func allowedTools() map[string]bool {
	out := make(map[string]bool, len(mcpsrv.ChatReadTools())+1)
	for _, n := range mcpsrv.ChatReadTools() {
		out[n] = true
	}
	out[ToolSubmitFeedback] = true
	return out
}

// LLMClient is the model. It is an interface only so the loop can be driven
// deterministically in tests; the production implementation is the existing
// api/internal/llm client (there is no second LLM client — AGENTS.md).
type LLMClient interface {
	ChatWithTools(ctx context.Context, turns []llm.ChatTurn, tools []llm.ToolDef) (llm.ChatReply, error)
}

// credentialedClient is the optional capability a model client has when it can
// say whether a credential is CONFIGURED. The production *llm.Client implements
// it; a test double need not, because a test wants the loop, not the deployment
// question.
type credentialedClient interface{ HasCredential() bool }

// Ready reports whether this runner can answer at all — a model is wired AND it
// has a credential. The REST status endpoint asks before offering the chat box:
// a deployment with no key must hide the assistant rather than show a box whose
// every message dies.
//
// It answers the CONFIGURATION half only. Whether the key still works is the
// provider's answer, which arrives on the first real message as
// llm.ErrCredential (the edge maps that to 503, the same "unavailable" answer).
func (r *Runner) Ready() bool {
	if r == nil || r.llm == nil {
		return false
	}
	if c, ok := r.llm.(credentialedClient); ok {
		return c.HasCredential()
	}
	// A client that cannot report its credential is taken at its word. Assuming
	// "no credential" here would silently hide the whole feature.
	return true
}

// Turn is one turn of the conversation as the client supplies it. Role is
// "user" or "assistant" (a client-supplied "system" or "tool" turn is dropped:
// the system prompt and tool results are constructed server-side, and letting a
// client forge one is exactly the injection this design closes).
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Step records one tool the model attempted, for the UI to show what happened.
// Status is "called" or "refused" — a refusal is shown, not hidden, because a
// silent refusal reads to the user as the assistant being broken.
type Step struct {
	Tool   string `json:"tool"`
	Status string `json:"status"`
	// Detail is a short human-readable note for a refusal (the server's own
	// message). Empty for a successful read.
	Detail string `json:"detail,omitempty"`
}

// Filed describes a feedback thread the chat filed during this exchange. It is
// returned so the UI can say so explicitly: the card requires that the user SEES
// the filing happen rather than discovering it later in another page.
type Filed struct {
	ThreadID string `json:"thread_id"`
	Subject  string `json:"subject"`
	Excerpt  string `json:"excerpt"`
}

// Result is one answered message.
type Result struct {
	Reply string `json:"reply"`
	Steps []Step `json:"steps"`
	// Filed is non-nil when the model filed feedback during this exchange.
	Filed *Filed `json:"filed,omitempty"`
}

// toolCaller dispatches one tool by name. It exists so the Runner can be given
// a fake in tests without an MCP session; production always uses the real
// session.
type toolCaller interface {
	call(ctx context.Context, name string, args string) (string, bool, error)
	close() error
}

// Runner answers one chat message. It holds no per-user state: the identity is
// taken from the ctx of each Run, so a Runner can be shared.
type Runner struct {
	llm      LLMClient
	mcp      *mcpsrv.Server
	feedback *feedbacksvc.Service

	// newSession builds the tool caller for one exchange. Overridden in tests.
	newSession func(ctx context.Context) (toolCaller, error)
	// toolDefsFn overrides the model-facing tool list for a NON-MCP caller (a
	// test fake). Production never sets it: the list comes from the registered
	// tools over the real session, which is the only source that cannot drift
	// from the tool registry.
	toolDefsFn func() []llm.ToolDef
}

// New builds a Runner over the real MCP server and the shared feedback service.
func New(client LLMClient, mcpSrv *mcpsrv.Server, fb *feedbacksvc.Service) *Runner {
	r := &Runner{llm: client, mcp: mcpSrv, feedback: fb}
	r.newSession = r.mcpSession
	return r
}

// mcpSession opens a real MCP session against the in-process server, with the
// CALLER's ctx — which is what binds the identity (mcpsrv.WithAuthIdentity) to
// every tool call made through it.
func (r *Runner) mcpSession(ctx context.Context) (toolCaller, error) {
	if r.mcp == nil {
		return nil, errors.New("chat: MCP server tiada")
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := r.mcp.Raw().Connect(ctx, st, nil)
	if err != nil {
		return nil, fmt.Errorf("chat: connect server: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "jomform-chat", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		ss.Close()
		return nil, fmt.Errorf("chat: connect client: %w", err)
	}
	return &mcpCaller{ss: ss, cs: cs}, nil
}

// mcpCaller is the real tool channel: a live MCP session to the in-process
// server.
type mcpCaller struct {
	ss *mcp.ServerSession
	cs *mcp.ClientSession
}

func (c *mcpCaller) close() error {
	err := c.cs.Close()
	if e := c.ss.Close(); e != nil && err == nil {
		err = e
	}
	return err
}

// call dispatches one tool. The second return value is the server's "the tool
// refused" flag (a tool-level error result), distinct from a transport failure.
func (c *mcpCaller) call(ctx context.Context, name string, argsJSON string) (string, bool, error) {
	var args map[string]any
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", true, fmt.Errorf("argumen tidak sah: %w", err)
		}
	}
	res, err := c.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", true, err
	}
	return flattenToolResult(res), res.IsError, nil
}

// flattenToolResult renders a tool result for the prompt: the structured content
// when the server produced it, otherwise the text blocks. Both are bounded.
func flattenToolResult(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			return bound(string(b), MaxToolResultChars)
		}
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return bound(sb.String(), MaxToolResultChars)
}

// bound truncates s to max characters (never bytes: a multi-byte character split
// in half is how a Tamil product name becomes mojibake in the prompt).
func bound(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…[dipotong]"
}

// Run answers one user message and returns the reply plus what happened.
//
// ctx MUST carry the caller's identity (mcpsrv.WithAuthIdentity) — see the REST
// handler, which builds it with READONLY scopes and the caller's user id.
func (r *Runner) Run(ctx context.Context, history []Turn, message string) (*Result, error) {
	if r.llm == nil {
		return nil, errors.New("chat: LLM tidak tersedia")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, errors.New("chat: mesej kosong")
	}
	if len([]rune(message)) > MaxMessageChars {
		return nil, fmt.Errorf("chat: mesej terlalu panjang (maksimum %d aksara)", MaxMessageChars)
	}

	caller, err := r.newSession(ctx)
	if err != nil {
		return nil, err
	}
	defer caller.close()

	tools, err := r.toolDefs(ctx, caller)
	if err != nil {
		return nil, err
	}

	turns := r.buildTurns(history, message)
	res := &Result{Steps: []Step{}}
	for step := 0; step < MaxSteps; step++ {
		reply, err := r.llm.ChatWithTools(ctx, turns, tools)
		if err != nil {
			return nil, err
		}
		if len(reply.ToolCalls) == 0 {
			res.Reply = bound(reply.Content, MaxReplyChars)
			return res, nil
		}
		// Record the assistant's own tool-request turn so the conversation the
		// model sees next is well-formed.
		turns = append(turns, llm.ChatTurn{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls})
		for _, tc := range reply.ToolCalls {
			out, refused, ferr := r.dispatch(ctx, caller, tc)
			switch {
			case ferr != nil:
				out = "RALAT: " + ferr.Error()
				refused = true
			}
			st := Step{Tool: tc.Name, Status: "called"}
			if refused {
				st.Status = "refused"
				st.Detail = bound(out, 300)
			}
			res.Steps = append(res.Steps, st)
			if tc.Name == ToolSubmitFeedback && !refused {
				if f := filedFrom(out); f != nil {
					res.Filed = f
				}
			}
			turns = append(turns, llm.ChatTurn{
				Role: "tool", ToolCallID: tc.ID, Name: tc.Name,
				Content: fenceToolResult(tc.Name, out),
			})
		}
		// The turn budget is spent: ask once more WITHOUT tools so the user gets
		// an answer built from what was gathered, rather than an error.
		if step == MaxSteps-1 {
			final, ferr := r.llm.ChatWithTools(ctx, turns, nil)
			if ferr != nil {
				return nil, ferr
			}
			res.Reply = bound(final.Content, MaxReplyChars)
			return res, nil
		}
	}
	res.Reply = "Maaf, saya tidak dapat menyelesaikan permintaan itu. Boleh cuba soalan yang lebih khusus?"
	return res, nil
}

// dispatch routes one model-requested call. It is the ONLY place a call becomes
// real, and it is where the allowlist is enforced.
func (r *Runner) dispatch(ctx context.Context, caller toolCaller, tc llm.ToolCall) (string, bool, error) {
	if !mcpsrv.ChatToolIsAllowed(tc.Name) {
		// An invented or injected tool name. Refused here, never dispatched.
		return fmt.Sprintf("alat %q tidak dibenarkan dalam chat ini", tc.Name), true, nil
	}
	if tc.Name == ToolSubmitFeedback {
		return r.submitFeedback(ctx, tc.Arguments)
	}
	return caller.call(ctx, tc.Name, tc.Arguments)
}

// submitFeedback files a feedback thread from the conversation, through the
// shared feedback service (same store, same per-user daily limit, same
// notification as the /feedback page — not a second feedback path).
func (r *Runner) submitFeedback(ctx context.Context, argsJSON string) (string, bool, error) {
	if r.feedback == nil {
		return "sistem maklum balas tidak dikonfigurasi", true, nil
	}
	var in struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
		return "argumen tidak sah: subject dan body diperlukan", true, nil
	}
	uid := userIDFrom(ctx)
	if uid == "" {
		return "pengguna tidak dikenali", true, nil
	}
	th, err := r.feedback.CreateThread(ctx, uid, in.Subject, in.Body)
	if err != nil {
		// The typed refusals are surfaced verbatim so the model can tell the
		// user what to do (write more, or come back tomorrow) instead of
		// inventing a success.
		return err.Error(), true, nil
	}
	out, _ := json.Marshal(map[string]any{
		"status": "created", "thread_id": th.ID,
		"subject": th.Subject, "body": strings.TrimSpace(in.Body),
	})
	return string(out), false, nil
}

// filedFrom reads the filing result back out of the tool result text so the
// caller can show the user that it happened.
func filedFrom(toolResult string) *Filed {
	var m struct {
		ThreadID string `json:"thread_id"`
		Subject  string `json:"subject"`
		Body     string `json:"body"`
	}
	if err := json.Unmarshal([]byte(toolResult), &m); err != nil || m.ThreadID == "" {
		return nil
	}
	subject := m.Subject
	if strings.TrimSpace(subject) == "" {
		subject = bound(strings.TrimSpace(m.Body), 60)
	}
	return &Filed{ThreadID: m.ThreadID, Subject: subject, Excerpt: bound(strings.TrimSpace(m.Body), 200)}
}

// toolDefs builds the model-facing tool list from the REAL registered tools:
// their names, their descriptions (the docs contract — not a copy that can
// drift) and their input schemas.
func (r *Runner) toolDefs(ctx context.Context, caller toolCaller) ([]llm.ToolDef, error) {
	cs, ok := caller.(*mcpCaller)
	if !ok {
		// Test caller: it supplies its own defs (see r.testToolDefs).
		return r.testToolDefs(), nil
	}
	res, err := cs.cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("chat: tools/list: %w", err)
	}
	allowed := allowedTools()
	out := make([]llm.ToolDef, 0, len(allowed))
	for _, t := range res.Tools {
		// The same predicate the dispatch path consults (mcpsrv.ChatToolIsAllowed
		// via allowedTools), so the offered set cannot be wider than the
		// reachable set.
		//
		// submit_feedback is registered as an MCP tool (parity) but is executed
		// IN-PROCESS by the chat, so its model-facing definition comes from
		// submitFeedbackDef() below — never twice.
		if !allowed[t.Name] || t.Name == ToolSubmitFeedback {
			continue
		}
		params, _ := json.Marshal(t.InputSchema)
		out = append(out, llm.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	// A local copy of the one write's schema: it is registered as an MCP tool
	// too (parity), but the chat executes it in-process.
	out = append(out, submitFeedbackDef())
	return out, nil
}

// testToolDefs is the fallback tool list for a NON-MCP caller (a test fake): the
// real list comes from the registered tools over the session, which a fake does
// not have. Set on the Runner (not a package global) so two tests cannot
// interfere, and so a Runner built in one test cannot leak its list into the next.
func (r *Runner) testToolDefs() []llm.ToolDef {
	if r.toolDefsFn != nil {
		return r.toolDefsFn()
	}
	return []llm.ToolDef{submitFeedbackDef()}
}

// submitFeedbackDef describes the one write to the model.
func submitFeedbackDef() llm.ToolDef {
	return llm.ToolDef{
		Name: ToolSubmitFeedback,
		Description: "Failkan maklum balas atau permintaan ciri daripada perbualan ini supaya pasukan " +
			"JomForm dapat membacanya dan membalas dalam thread. Guna apabila pengguna melaporkan masalah " +
			"produk, meminta ciri baharu, atau meminta sesuatu yang tidak dapat anda bantu. Ia BUKAN cara " +
			"menjawab soalan tentang data akaun — jawab soalan itu dengan alat bacaan.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "subject": {"type": "string", "description": "Tajuk ringkas (maksimum 200 aksara)"},
    "body": {"type": "string", "description": "Isi maklum balas, minimum 10 aksara. Tulis apa yang pengguna laporkan, dalam bahasa pengguna."}
  },
  "required": ["body"],
  "additionalProperties": false
}`),
	}
}

// buildTurns assembles the model-facing conversation: the fixed system prompt,
// the bounded client history, and the new message. A client-supplied "system" or
// "tool" turn is DROPPED — those roles are server-owned, and accepting one would
// let the caller overwrite the operating instructions.
func (r *Runner) buildTurns(history []Turn, message string) []llm.ChatTurn {
	turns := []llm.ChatTurn{{Role: "system", Content: systemPrompt}}
	if len(history) > MaxTurns {
		history = history[len(history)-MaxTurns:]
	}
	for _, h := range history {
		role := strings.ToLower(strings.TrimSpace(h.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		text := strings.TrimSpace(h.Content)
		if text == "" {
			continue
		}
		turns = append(turns, llm.ChatTurn{Role: role, Content: bound(text, MaxTurnChars)})
	}
	turns = append(turns, llm.ChatTurn{Role: "user", Content: bound(message, MaxMessageChars)})
	return turns
}

// userIDFrom reads the authenticated user id off the identity mcpsrv carries.
// The chat refuses to file feedback for a caller with no user identity (an API
// key), because a feedback thread belongs to a PERSON.
func userIDFrom(ctx context.Context) string {
	id := mcpsrv.IdentityFromContext(ctx)
	if id == nil {
		return ""
	}
	return id.UserID
}

// ReadOnlyScopes is the scope set the chat's tool calls run under, whatever the
// caller's own credential holds. It is exported so the REST handler sets it in
// one named place: the chat's "cannot write" claim is then a property of the
// identity, not of a prompt.
func ReadOnlyScopes() []string { return []string{dbstore.ScopeReadonly} }
