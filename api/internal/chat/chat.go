// Package chat is the in-app assistant: PHASE 1 read-only over the caller's own
// data plus one write (file feedback), and PHASE 2 proposal-only writes — the
// model may PROPOSE a write, the USER confirms it, and only then does the server
// execute. It can never execute a write on its own.
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
//  1. THE ALLOWLIST (this file + mcpsrv.ChatToolIsAllowed). The model is offered
//     a small, explicit set: the read tools a merchant needs about their own
//     account, plus submit_feedback (in-process) and propose_write (in-process,
//     executes nothing). A tool name outside the set — delete_form, refund_sale,
//     any write at all — is refused HERE and never dispatched, so a
//     prompt-injected instruction to "call delete_form" has nothing to call.
//     Phase 2 does NOT widen this: a proposal is not a dispatch.
//
//  2. THE IDENTITY (this file + mcpsrv). Every tool call made during a message
//     runs under an identity whose scopes are READONLY, whatever the caller's own
//     credential can do. So even if the allowlist were widened by mistake, every
//     write tool the server registers answers "scope tidak mencukupi" — the issue
//     #645 gate. The ONLY code that dispatches a tool with a write scope is
//     Runner.Confirm, which requires an opaque, single-use, server-issued token
//     bound to (tool, arguments, caller, expiry). See proposal.go.
//
//  3. THE SERVER'S OWN GATES (mcpsrv). Workspace role, ownership and scope are
//     re-checked per call, exactly as for an external agent — including the
//     confirmed call, which runs under the human's identity.
//
// Layer 2 is what makes "the model cannot write" a property of the SERVER rather
// than of a prompt, and layer 1 is what keeps the model from wandering: offering
// 161 tools and trusting the gate would work, but it also invites the model to
// try. Both are asserted by tests, and the refusal tests are proven to BITE (see
// refusal_i18n_test.go and the phase-2 acceptance file).
//
// A MODEL-SET `confirm=true` IS NOT A CONFIRMATION. The MCP tools that carry a
// Confirm argument (api_key_mcp.go) are confirmed by the human typing into their
// own client; in the chat the caller is the model, so a boolean it writes proves
// nothing. Therefore no `confirm` argument is consulted anywhere on the model's
// path: a write tool called directly is REFUSED, `confirm=true` included. The
// confirmation that counts is a token only the human's browser holds.
//
// UNTRUSTED CONTENT. Tool results are the caller's own data — product names,
// form answers, a customer's typed message. That data is wrapped in an explicit
// fence and the model is told, before and after it, that it is DATA and not
// instructions. Phase 1 had no write to reach; phase 2 has a PROPOSAL, which is
// why the fence matters more rather than less: injected text that talks the model
// into proposing a destructive edit still has to survive a human reading a card
// that the SERVER rendered from the arguments, and the destructive classes are not
// proposable at all.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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

// ToolSubmitFeedback is the chat's in-process filing tool: it is not dispatched
// over MCP but calls the shared feedback service directly, so the same
// validation, per-user daily limit and notification apply as on the /feedback
// page. It needs no confirmation — filing a report changes nothing about the
// caller's account.
const ToolSubmitFeedback = mcpsrv.ChatSubmitFeedbackTool

// ToolProposeWrite is the chat's in-process PROPOSAL tool (phase 2). It records
// what the model WANTS to do and returns a card plus an opaque token; it executes
// nothing. It is in-process for the same reason as submit_feedback: the tool it
// names is not being dispatched, so there is nothing to route through MCP.
const ToolProposeWrite = mcpsrv.ChatProposeWriteTool

// Step statuses. "proposed" is phase 2's addition: the model asked for a change
// and the server turned it into a card for the user instead of doing it. It is
// reported to the UI (and NOT as "called", which is what a read is) so the
// transcript can say "I have prepared this — confirm it?" rather than implying the
// change already happened.
const (
	StepCalled   = "called"
	StepRefused  = "refused"
	StepProposed = "proposed"
)

// allowedTools is the complete DISPATCH set: the read allowlist plus the two
// in-process tools (filing, proposing). Nothing that reaches a store write is in
// it — see mcpsrv.ChatToolIsAllowed, which is the single predicate this map and
// the dispatch path both consult.
//
// The allowlist itself lives in mcpsrv (ChatReadTools / ChatToolIsAllowed) so the
// call-graph guard that polices every registered tool's authorization also proves
// this list contains no writer. Keeping a second copy here is how the offered set
// and the dispatched set would drift apart.
func allowedTools() map[string]bool {
	out := make(map[string]bool, len(mcpsrv.ChatReadTools())+2)
	for _, n := range mcpsrv.ChatReadTools() {
		out[n] = true
	}
	out[ToolSubmitFeedback] = true
	out[ToolProposeWrite] = true
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
// Status is "called", "refused" or "proposed" — a refusal and a proposal are both
// shown, not hidden, because a silent refusal reads to the user as the assistant
// being broken, and a silent proposal would be a change they were never asked
// about.
type Step struct {
	Tool   string `json:"tool"`
	Status string `json:"status"`
	// DetailKey names what this step's detail IS, as a dictionary key: for a
	// PROPOSED step, the card that was prepared (chat.proposal.title.<tool>); for
	// a REFUSED step, WHY it was refused (chat.refused.*). It is resolved by the
	// dashboard exactly like the card's own wording (t_30ffa28a): the API does not
	// receive the caller's language on a JSON call (issue #6177), so a sentence
	// built here would be one language on every dashboard.
	//
	// THERE IS NO PROSE SIBLING, DELIBERATELY. This used to carry a `detail`
	// sentence the runner wrote in Malay — the allowlist refusal, "RALAT: " plus an
	// internal error, the proposal lane's message — and the page printed it
	// verbatim in every locale: an English transcript read
	//
	//	assistant tried — update_product — alat "update_product" tidak dibenarkan dalam chat ini
	//
	// (card t_3839995c). Deleting the field is what makes that class of defect
	// unrepresentable rather than merely fixed: there is nothing left on the wire
	// for a call site to print by accident, which is the same rule the card's own
	// wording follows (proposal.go). The MODEL's half is not lost — the refusal
	// text still travels as the tool result, which is fenced into the prompt and
	// never reaches the browser.
	DetailKey string `json:"detail_key,omitempty"`
	// DetailParams are the values interpolated into the DetailKey sentence — e.g.
	// {"max": "3"} for the per-answer proposal budget. Empty when the sentence has
	// nothing to substitute. The tool name is NOT one of them: the page prints
	// `tool` beside the sentence, so it stays the identifier it is.
	DetailParams map[string]string `json:"detail_params,omitempty"`
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
	// ReplyKey names a DICTIONARY SENTENCE for the answer when the model wrote
	// none of its own — the page resolves it in the caller's language, which the
	// API does not know (issue #6177). It is empty whenever Reply carries prose.
	//
	// WHY THIS EXISTS (card t_7f1694cb). A model whose proposal was refused is
	// free to end the turn having written nothing, and on prod it did: the
	// merchant's whole answer was an amber refusal block and `reply: ""`. Silently
	// passing that through is the one shape the page cannot repair — it has no
	// sentence to print — so the runner names one instead. A key, not a sentence,
	// for the same reason every refusal carries one (t_3839995c).
	ReplyKey string `json:"reply_key,omitempty"`
	Steps    []Step `json:"steps"`
	// Filed is non-nil when the model filed feedback during this exchange.
	Filed *Filed `json:"filed,omitempty"`
	// Proposals are the write proposals this exchange produced, in the order the
	// model made them. Each carries the opaque confirmation token, which goes to
	// the BROWSER: the token is what makes the user's click the only way the
	// proposed write can happen. Empty for a read-only exchange.
	Proposals []ProposalCard `json:"proposals,omitempty"`
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
	// proposals stores the write proposals the model makes (phase 2). Nil means
	// the proposal lane is not configured: propose_write refuses rather than
	// pretending, and Confirm refuses too. The runner still answers questions.
	proposals proposalStore

	// newSession builds the tool caller for one exchange. Overridden in tests.
	newSession func(ctx context.Context) (toolCaller, error)
	// toolDefsFn overrides the model-facing tool list for a NON-MCP caller (a
	// test fake). Production never sets it: the list comes from the registered
	// tools over the real session, which is the only source that cannot drift
	// from the tool registry.
	toolDefsFn func() []llm.ToolDef
}

// New builds a Runner over the real MCP server and the shared feedback service.
// proposals may be nil (no proposal lane); see SetProposalStore.
func New(client LLMClient, mcpSrv *mcpsrv.Server, fb *feedbacksvc.Service) *Runner {
	r := &Runner{llm: client, mcp: mcpSrv, feedback: fb}
	r.newSession = r.mcpSession
	return r
}

// SetProposalStore wires the phase-2 proposal lane. It is a setter rather than a
// New argument so the production wiring stays readable and a deployment that has
// no proposal store simply does not get the lane (propose_write refuses with a
// clear reason instead of failing at the first model call).
func (r *Runner) SetProposalStore(s proposalStore) { r.proposals = s }

// mcpSession opens a real MCP session against the in-process server, with the
// CALLER's ctx — which is what binds the identity (mcpsrv.WithAuthIdentity) to
// every tool call made through it.
func (r *Runner) mcpSession(ctx context.Context) (toolCaller, error) {
	if r.mcp == nil {
		return nil, errors.New("chat: MCP server not configured")
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
func (c *mcpCaller) call(ctx context.Context, name, argsJSON string) (string, bool, error) {
	var args map[string]any
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", true, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	res, err := c.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", true, err
	}
	return flattenToolResult(res), res.IsError, nil
}

// toolInputSchema returns one tool's DECLARED input schema from the live
// registry over tools/list, and whether the registry has that tool at all.
//
// It is the chat's only source for a tool's real contract, and it is deliberately
// the LIVE registry rather than a copy: the proposal lane's whole job is to
// predict what the tool's own gate will accept, and a second declaration of the
// schema (however carefully written) would be a second thing to keep in step —
// the drift `chat.go:toolDefs` already avoids by reading tools/list for the
// model-facing tool list.
//
// The client-side Tools iterator walks every page and is the same call the SDK
// uses for ListTools, so a registry larger than one page is still covered.
func (c *mcpCaller) toolInputSchema(ctx context.Context, name string) (any, bool, error) {
	for t, err := range c.cs.Tools(ctx, nil) {
		if err != nil {
			return nil, false, fmt.Errorf("tools/list: %w", err)
		}
		if t.Name == name {
			return t.InputSchema, true, nil
		}
	}
	return nil, false, nil
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

// answerOrKey stores the model's own words as the answer, or — when it wrote none
// — the key of the sentence the dashboard renders instead.
//
// WHY AN EMPTY ANSWER MUST NOT BE PASSED THROUGH (card t_7f1694cb). The proposal
// lane gives its objection to the MODEL, which is right (the model is the only one
// that can correct the call), and a model is free to answer that with nothing at
// all. On prod it did: the merchant's entire answer to "Create a form called My New
// Form with one text field" was a refusal block and `reply: ""`, and the page has
// no sentence of its own to fall back on. So the runner names one — a KEY, because
// the API does not know the caller's language (issue #6177) and a sentence written
// here would reach all five locales in one language (t_3839995c).
//
// WHAT COUNTS AS SILENCE. Whitespace is silence: a model that emits "\n\n" has said
// nothing, and the page would render an empty bubble. The check trims for the
// DECISION only — the prose itself is stored exactly as it arrived, because
// leading blank lines in a Markdown answer are harmless and rewriting a model's
// answer is not this function's job.
func (res *Result) answerOrKey(content string) {
	res.Reply = bound(content, MaxReplyChars)
	res.ReplyKey = ""
	if strings.TrimSpace(res.Reply) == "" {
		res.ReplyKey = keyReplyNoAnswer
	}
}

// Run answers one user message and returns the reply plus what happened.
//
// ctx MUST carry the caller's identity (mcpsrv.WithAuthIdentity) — see the REST
// handler, which builds it with READONLY scopes and the caller's user id.
func (r *Runner) Run(ctx context.Context, history []Turn, message string) (*Result, error) {
	return r.run(ctx, history, message, nil)
}

// RunStream answers one user message exactly as Run does, and additionally
// reports progress WHILE it works — the tool steps as they start, and the
// answer's prose as the model writes it (card t_148d7100).
//
// WHY THIS IS A SECOND ENTRY POINT AND NOT A FLAG ON Run. `Run` is what every
// non-streaming caller uses (the JSON POST, and the tests that pin the loop's
// behaviour). Keeping it a wrapper over the same loop means the two paths cannot
// disagree about tool dispatch, the step budget, proposals or refusals — the only
// difference is that this one calls emit as it goes.
//
// THE RESULT IS STILL AUTHORITATIVE. What emit carried is progress the UI may
// render immediately; `res` is the same answer Run would have returned, and a
// caller must render THAT when the exchange ends (a streamed fragment can be
// bounded or trimmed differently, and a round that turns out to be a tool round
// emits EventRetract to withdraw the prose the browser was shown).
//
// An error from emit aborts the exchange and is returned unchanged: it is how a
// disconnected client stops the model work behind it.
func (r *Runner) RunStream(ctx context.Context, history []Turn, message string, emit EventFunc) (*Result, error) {
	return r.run(ctx, history, message, emit)
}

func (r *Runner) run(ctx context.Context, history []Turn, message string, emit EventFunc) (*Result, error) {
	if r.llm == nil {
		return nil, errors.New("chat: LLM unavailable")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, errors.New("chat: empty message")
	}
	if len([]rune(message)) > MaxMessageChars {
		return nil, fmt.Errorf("chat: message too long (maximum %d characters)", MaxMessageChars)
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

	turns := r.buildTurns(ctx, history, message)
	res := &Result{Steps: []Step{}}
	// Per-message state: the proposal budget is this message's, not the Runner's.
	rs := &runState{}
	for step := 0; step < MaxSteps; step++ {
		// Prose this round has already streamed to the caller. A round that ends
		// with tool calls was NOT an answer — the model was thinking out loud
		// ("let me check your forms") before calling one — so the caller is told
		// to withdraw it rather than leaving a fragment of a discarded turn in the
		// transcript.
		streamed := false
		var onDelta func(string) error
		if emit != nil {
			onDelta = func(s string) error {
				streamed = true
				return emit(Event{Type: EventDelta, Text: s})
			}
		}
		reply, err := r.ask(ctx, turns, tools, onDelta)
		if err != nil {
			return nil, err
		}
		if len(reply.ToolCalls) == 0 {
			res.answerOrKey(reply.Content)
			return res, nil
		}
		if streamed {
			if err := emit(Event{Type: EventRetract}); err != nil {
				return nil, err
			}
		}
		// Record the assistant's own tool-request turn so the conversation the
		// model sees next is well-formed.
		turns = append(turns, llm.ChatTurn{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls})
		for _, tc := range reply.ToolCalls {
			// BEFORE the dispatch: this is the event that lets the UI say what is
			// happening while it happens ("Reading your forms…") instead of leaving
			// a static "thinking" line for the whole tool round-trip — the shape a
			// user read as the assistant being stuck (card t_148d7100).
			if emit != nil {
				if err := emit(Event{Type: EventStep, Tool: tc.Name}); err != nil {
					return nil, err
				}
			}
			out, ref, ferr := r.dispatchIn(ctx, caller, tc, rs)
			if ferr != nil {
				// A failure BEFORE the tool: the error text is an internal detail
				// and is one language, so the PROMPT keeps it and the step carries
				// a code — the page has its own sentence for it.
				out = "ERROR: " + ferr.Error()
				ref = &refusal{key: keyRefusedError}
			}
			st := Step{Tool: tc.Name, Status: StepCalled}
			if ref != nil {
				st.Status = StepRefused
				st.DetailKey = ref.key
				st.DetailParams = ref.params
			}
			if tc.Name == ToolSubmitFeedback && ref == nil {
				if f := filedFrom(out); f != nil {
					res.Filed = f
				}
			}
			// A proposal is a step in its own right: the model asked for a change
			// and the server prepared a card instead of doing it. Reporting it as
			// "called" would tell the user their thing had happened.
			//
			// The card comes from the per-message state (the TOKEN-BEARING one),
			// not from the tool result: the result the model saw was stripped of
			// the token, so the browser must get it from here.
			//
			// ONLY WHEN A CARD WAS ACTUALLY PREPARED (ref == nil). A REFUSED
			// propose_write used to be relabelled "proposed" here, over the
			// refusal the branch above had just written — and the page renders
			// refused steps only, so the merchant was never told the attempt
			// happened at all. That is the one shape "a refusal is shown, not
			// swallowed" (the page's own rule) cannot survive, and it would also
			// make the proposal lane's refusal key unreachable on the wire
			// (t_3839995c).
			if tc.Name == ToolProposeWrite && ref == nil {
				st.Status = StepProposed
				if n := len(rs.cards); n > 0 {
					card := rs.cards[n-1]
					res.Proposals = append(res.Proposals, card)
					// The step says WHICH card was prepared, by key: the server does
					// not know the caller's language, and this used to be the card's
					// Malay title (t_30ffa28a).
					st.DetailKey = card.TitleKey
				}
			}
			res.Steps = append(res.Steps, st)
			turns = append(turns, llm.ChatTurn{
				Role: "tool", ToolCallID: tc.ID, Name: tc.Name,
				Content: fenceToolResult(tc.Name, out),
			})
		}
		// The turn budget is spent: ask once more WITHOUT tools so the user gets
		// an answer built from what was gathered, rather than an error.
		if step == MaxSteps-1 {
			final, ferr := r.ask(ctx, turns, nil, onDelta)
			if ferr != nil {
				return nil, ferr
			}
			res.answerOrKey(final.Content)
			return res, nil
		}
	}
	// No step left and no answer of the model's own: answer with the DICTIONARY key
	// instead of writing prose here. A sentence at this line reaches all five
	// dashboard languages in one language — that is exactly the Malay bubble a
	// merchant on an English dashboard read (t_541a31d5, t_7f1694cb / issue #6177) —
	// so return the key the dashboard resolves in the visitor's language rather than
	// picking a language the API does not know.
	//
	// It is the zero-budget guard: MaxSteps > 0 means the loop above already
	// answers on its last step, so normal traffic never lands here. It is kept
	// rather than deleted because deleting it would make a future MaxSteps of 0
	// return an empty reply with no key, which is the shape the page cannot
	// repair.
	res.answerOrKey("")
	return res, nil
}

// One refusal, two audiences, and only one of them is language-dependent.
//
// The wording the MODEL reads is a different thing and stays exactly where it was:
// the runner returns it as the tool result, which is fenced into the prompt
// (prompt.go) and never reaches the browser. The wording the PERSON reads is the
// key below, resolved by the dashboard (web/src/lib/chatRefusal.ts).
//
// The keys are pinned in both directions cross-language by
// web/src/lib/chatRefusal.test.ts, which reads this file: a key here with no
// dictionary entry renders the raw key to the user, and a reworded key here that
// the test does not know about fails the web suite.
const (
	// keyRefusedWriteTool — the allowlist refused a WRITE tool name. It is the
	// refusal the chat's whole design produces: the model tried to change
	// something directly, and the only route to a write is a card the human
	// confirms.
	keyRefusedWriteTool = "chat.refused.writeTool"
	// keyRefusedNotAllowed — any other name this chat may not dispatch: an
	// invented tool, a typo of a real one, or a read it deliberately excludes
	// (list_api_keys).
	keyRefusedNotAllowed = "chat.refused.notAllowed"
	// keyRefusedProposal — the proposal lane could not prepare the change the
	// model asked for, so there is no card. WHICH argument the lane objected to
	// (and why) stays in the text the MODEL receives: that is the half that can
	// correct the call, and it is not a sentence for the merchant.
	keyRefusedProposal = "chat.refused.proposal"
	// keyRefusedProposalLimit — this answer already produced the maximum number
	// of cards. The sentence interpolates {max}.
	keyRefusedProposalLimit = "chat.refused.proposalLimit"
	// keyRefusedTool — the tool itself refused: it ran and declined (a read it
	// will not serve, a filing the feedback service rejected).
	keyRefusedTool = "chat.refused.tool"
	// keyRefusedError — the call failed before it reached the tool (a transport
	// failure, arguments that could not be parsed).
	keyRefusedError = "chat.refused.error"
	// keyReplyNoAnswer — the model wrote no prose, so the answer is a sentence the
	// dashboard renders. NOT a refusal: a turn that ends with nothing is the same
	// silence whether or not something was refused, and this covers both
	// (t_7f1694cb). It is deliberately outside the refusal family — it is the
	// assistant's own bubble, not a step's detail, and the page's two guards
	// extract different prefixes ("chat.refused." from the steps, "chat.reply."
	// from this file).
	keyReplyNoAnswer = "chat.reply.noAnswer"
)

// refusal is the UI's half of a refused tool call: the machine key the dashboard
// resolves into the visitor's language, plus the values its sentence interpolates.
// It carries NO sentence — see Step.DetailKey for why that field is gone from the
// wire.
//
// It is nil when a call was NOT refused, so `*refusal` doubles as the boolean
// dispatch used to return.
type refusal struct {
	key    string
	params map[string]string
}

// dispatch routes one model-requested call. It is the ONLY place a model-requested
// call becomes real, and it is where the allowlist is enforced.
//
// PHASE 2: a model that calls a write tool DIRECTLY is refused here, whatever it
// put in the arguments — `confirm=true` included. That is the whole point of the
// proposal design: a boolean the model typed is not a human's confirmation, so the
// write tools are not in the allowlist at all and the only path to one is
// Runner.Confirm with a token only the user's browser holds.
func (r *Runner) dispatch(ctx context.Context, caller toolCaller, tc llm.ToolCall) (string, bool, error) {
	out, ref, err := r.dispatchIn(ctx, caller, tc, &runState{})
	return out, ref != nil, err
}

// runState carries the per-MESSAGE state a dispatch decision needs.
//
// It lives here, not on the Runner: a Runner is shared (its doc says the identity
// comes from each Run's ctx), so per-message state on the struct would be a data
// race between two people talking to the same assistant at once.
type runState struct {
	// proposals is how many proposals this message has already produced. It
	// bounds how many confirmation cards one answer can put in front of the user.
	proposals int
	// cards are the cards this message produced, in order. They are collected
	// HERE rather than read back out of the tool result, because the tool result
	// is what the MODEL sees and it must not contain the confirmation token: the
	// token is the user's authorisation, and a model holding it could complete
	// the confirmation itself. See proposeWrite.
	cards []ProposalCard
}

// dispatchIn is dispatch with the per-message state threaded through. The second
// return value is the refusal AS THE UI SEES IT (nil when the call was not
// refused); the first is the text the MODEL reads back, which is what makes a
// refusal teachable without putting a sentence in front of a person.
func (r *Runner) dispatchIn(ctx context.Context, caller toolCaller, tc llm.ToolCall, st *runState) (string, *refusal, error) {
	if st == nil {
		st = &runState{}
	}
	if !mcpsrv.ChatToolIsAllowed(tc.Name) {
		// An invented, injected or WRITE tool name. Refused here, never
		// dispatched. The message names the proposal lane so the model can offer
		// the user the supported route instead of a dead end — and, for a write
		// tool, says plainly that `confirm` proves nothing.
		if isWriteToolName(tc.Name) {
			return fmt.Sprintf("tool %q cannot be executed by the chat — a write must be "+
				"PROPOSED to the user and confirmed by them (propose_write). "+
				"Setting confirm=true in the arguments authorises nothing: a model that "+
				"writes it is not a human. Propose the change instead; the user will confirm "+
				"it in the UI.", tc.Name), &refusal{key: keyRefusedWriteTool}, nil
		}
		return fmt.Sprintf("tool %q is not allowed in this chat", tc.Name),
			&refusal{key: keyRefusedNotAllowed}, nil
	}
	if tc.Name == ToolSubmitFeedback {
		out, refused, err := r.submitFeedback(ctx, tc.Arguments)
		if err != nil || !refused {
			return out, nil, err
		}
		return out, &refusal{key: keyRefusedTool}, nil
	}
	if tc.Name == ToolProposeWrite {
		return r.proposeWrite(ctx, caller, tc.Arguments, st)
	}
	out, refused, err := caller.call(ctx, tc.Name, tc.Arguments)
	if err != nil || !refused {
		return out, nil, err
	}
	// The tool ran and declined. Its own message is the tool's (api/internal/mcpsrv),
	// and it is one language — so the prompt keeps it and the person gets the key.
	return out, &refusal{key: keyRefusedTool}, nil
}

// isWriteToolName reports whether a refused name looks like a WRITE tool, so the
// refusal can say the useful thing ("propose it instead") rather than only "not
// allowed". It is a hint for the message ONLY — the allowlist decides, and the
// name is never trusted for anything else.
func isWriteToolName(name string) bool {
	for _, seg := range []string{"delete", "archive", "revoke", "update", "create",
		"set_", "cancel", "refund", "publish", "unpublish", "remove", "invite",
		"retry", "capture", "release", "confirm", "purge", "bulk", "reply", "use_"} {
		if strings.Contains(name, seg) {
			return true
		}
	}
	return false
}

// proposeWrite handles the in-process proposal tool: it records what the model
// wants to do and returns a description of the card. NOTHING IS EXECUTED — that is
// why this tool is dispatchable while every real writer is not.
//
// THE TOKEN IS NOT IN THE RESULT. What the model gets back is the card WITHOUT
// its token: which tool, the dictionary keys of its wording, and the field values,
// so it can tell the user what it prepared and what it will change. The token goes
// to the browser (res.Proposals) and never into the prompt. That is not cosmetic —
// a model that held the token could "confirm" the proposal by posting it, and the
// whole point of this design is that the confirmation is a HUMAN's click. Keeping
// the token out of the only channel the model reads is what makes that structural
// rather than advisory.
//
// THE KEYS ARE NOT FOR THE USER (t_30ffa28a). Since the card's wording became
// dictionary keys, this result carries `title_key` / `label_key` rather than the
// Malay sentences it used to. That is the right direction — nothing
// language-dependent belongs in the prompt — but the model must not read a key out
// to the merchant, so the system prompt says so (prompt.go, "HOW TO ANSWER").
//
// The proposal budget (MaxProposalsPerRun) is enforced by the runner, not by the
// model: a message can put at most that many decisions in front of the user, so a
// single answer cannot become a wall of confirmations to click through.
func (r *Runner) proposeWrite(ctx context.Context, caller toolCaller, argsJSON string, st *runState) (string, *refusal, error) {
	if st.proposals >= MaxProposalsPerRun {
		return fmt.Sprintf("proposal limit for one message reached (%d). Answer with the proposals "+
				"already prepared; the user can ask for more after this.", MaxProposalsPerRun),
			&refusal{key: keyRefusedProposalLimit,
				params: map[string]string{"max": strconv.Itoa(MaxProposalsPerRun)}}, nil
	}
	card, err := r.proposeFromArgs(ctx, caller, argsJSON)
	if err != nil {
		// The lane's own objection — which argument, and why — goes back to the
		// MODEL, which is the one that can correct the call; the person gets the
		// key (a sentence naming an argument key would be neither localized nor
		// useful to them).
		return err.Error(), &refusal{key: keyRefusedProposal}, nil
	}
	st.proposals++
	st.cards = append(st.cards, *card)
	// modelCard is the card as the MODEL may see it: no token, no args_hash.
	out, _ := json.Marshal(card.withoutSecrets())
	return string(out), nil, nil
}

// lastCard returns the most recent card a message produced, which is the
// TOKEN-BEARING one (the tool result the model saw has its token stripped). It is
// used by tests to read the card the way the UI receives it.
func (st *runState) lastCard() *ProposalCard {
	if len(st.cards) == 0 {
		return nil
	}
	return &st.cards[len(st.cards)-1]
}

// submitFeedback files a feedback thread from the conversation, through the
// shared feedback service (same store, same per-user daily limit, same
// notification as the /feedback page — not a second feedback path).
func (r *Runner) submitFeedback(ctx context.Context, argsJSON string) (string, bool, error) {
	if r.feedback == nil {
		return "feedback system is not configured", true, nil
	}
	var in struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
		return "invalid arguments: subject and body are required", true, nil
	}
	uid := userIDFrom(ctx)
	if uid == "" {
		return "no user identity", true, nil
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
	// A local copy of the two in-process tools' schemas: they are the chat's own
	// (submit_feedback runs through feedbacksvc, propose_write records intent),
	// so neither comes from tools/list.
	out = append(out, submitFeedbackDef(), proposeWriteDef())
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
	return []llm.ToolDef{submitFeedbackDef(), proposeWriteDef()}
}

// submitFeedbackDef describes the one write to the model.
//
// THE PROSE IS ENGLISH (card t_ec87d6c7). This def and proposeWriteDef below were
// the last model-facing text in the lane written in Malay, while the system
// prompt, the MCP tool descriptions and every refusal are English. A model reads
// its tool definitions as part of its instructions, and a Malay instruction block
// next to an English one invites a Malay answer — which is exactly the defect this
// card fixes (a Malay sentence on an English confirmation card). The model's
// ANSWER language is decided by the rule in the system prompt, not by the language
// of these descriptions.
func submitFeedbackDef() llm.ToolDef {
	return llm.ToolDef{
		Name: ToolSubmitFeedback,
		Description: "File feedback or a feature request from this conversation so the JomForm " +
			"team can read it and reply in a thread. Use it when the merchant reports a problem " +
			"with the product, asks for a new feature, or needs help you cannot give. It is NOT " +
			"how you answer a question about account data — answer those with the read tools.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "subject": {"type": "string", "description": "A short title (maximum 200 characters)"},
    "body": {"type": "string", "description": "The feedback body, at least 10 characters. Write what the merchant reported, in the language they wrote it in."}
  },
  "required": ["body"],
  "additionalProperties": false
}`),
	}
}

// proposeWriteDef describes propose_write to the model.
//
// The `arguments` object is deliberately an OPEN object (`additionalProperties`
// true) with the proposable names listed in the description rather than as a
// oneOf: the per-tool schemas belong to the registered tools, and copying them
// here would be a second contract that can drift from the one the tool actually
// validates. The model is told to build `arguments` from the tool's own
// definition, which it also has in this same tool list.
//
// The `reason` description names the DASHBOARD language (card t_ec87d6c7): the
// reason is printed on the confirmation card, which is dashboard UI, so it must
// match the card's own language and not the conversation's. It used to say "in
// the merchant's language", which produced a Malay sentence on an English card.
func proposeWriteDef() llm.ToolDef {
	proposable := mcpsrv.ChatProposableTools()
	sort.Strings(proposable)
	return llm.ToolDef{
		Name: ToolProposeWrite,
		Description: "PROPOSE one change so the MERCHANT can confirm it. This tool performs " +
			"NOTHING: it prepares a confirmation card in the chat and waits for the merchant to " +
			"click Confirm. After calling it, tell the merchant the proposal is ready and ask them " +
			"to confirm — do NOT say the change has happened. Use the same arguments as the real " +
			"tool (see that tool's definition in this list) inside the `arguments` field. " +
			"Proposable: " + strings.Join(proposable, ", ") + ". Other tools (delete, archive, " +
			"money, credentials, email, publish) CANNOT be proposed — tell the merchant to do " +
			"those themselves on the dashboard.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "tool": {"type": "string", "description": "The name of the tool to run, e.g. update_product."},
    "arguments": {"type": "object", "description": "The arguments for that tool, exactly as you would pass them when calling the tool itself.", "additionalProperties": true},
    "reason": {"type": "string", "description": "One short sentence in the DASHBOARD language named under CARD LANGUAGE — shown on the card as your words, not as a fact. It follows the card, not the conversation."}
  },
  "required": ["tool", "arguments"],
  "additionalProperties": false
}`),
	}
}

// buildTurns assembles the model-facing conversation: the fixed system prompt,
// the bounded client history, and the new message. A client-supplied "system" or
// "tool" turn is DROPPED — those roles are server-owned, and accepting one would
// let the caller overwrite the operating instructions.
//
// The system prompt carries the caller's DASHBOARD language (see
// WithDashboardLang): the card's one model-authored string has to be written in
// the language the card is displayed in, and the model is the only thing that
// writes it.
func (r *Runner) buildTurns(ctx context.Context, history []Turn, message string) []llm.ChatTurn {
	turns := []llm.ChatTurn{{Role: "system", Content: systemPromptText(DashboardLangFrom(ctx))}}
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
