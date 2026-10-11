package httpapi

// THE CHAT'S TOOL LANE AT THE HTTP EDGE — what the assistant actually READS and
// WRITES under ?workspace= (issue #747).
//
// THE DEFECT. The chat's REST edge resolved the caller's account from
// ?workspace= correctly — a proposal under ?workspace=abc was stored with
// account_id = abc's account, and a card under ?workspace=wan with wan's. What
// was NOT scoped is everything the assistant then did with it. The runner opens
// the MCP session with the caller's ctx, mcpsrv.addTool re-resolves a per-call
// scope from the tool call's OWN arguments, and a chat tool call's arguments are
// the MODEL's: the reserved `workspace_id`/`account_id` helper fields are
// optional and no model writes them. So every tool resolved resolveScope's
// UNSCOPED branch — EVERY account the user can reach. Measured on prod
// (v1.16.29): under ?workspace=abc, "which account am I looking at?" answered
// "wan Store (`wan`)", "how many forms do I have?" answered 21 while abc owns 2,
// and a proposed AND CONFIRMED update_product renamed a product owned by the wan
// account.
//
// WHY THIS FILE EXISTS ALONGSIDE chat_workspace_scope_test.go. That file asserts
// WHICH ACCOUNT THE EDGE HANDED OVER — it reads the identity out of the ctx a fake
// runner received, and a mutation proves what that leaves open: hand the lane the
// other workspace's AccountID and it still passes, because the account handed over
// is not what decides where a tool reads. That is exactly why the class shipped
// green, so the assertion here is deliberately NOT about the identity: this file
// drives the REAL chat.Runner over the REAL in-process MCP server with a scripted
// model, and asserts on WHAT THE TOOL LANE RESOLVED — the account get_account
// answered with, and whether a confirmed write could reach an object owned by
// another workspace.
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// scriptedChatModel drives the REAL runner deterministically. What it answers
// depends on the conversation it is shown rather than on a call counter, so one
// env can serve several HTTP requests in one test without being reset.
type scriptedChatModel struct {
	// next builds the reply for one round-trip given the turns so far. Set after
	// the environment exists when the reply needs the fixture's ids.
	next func(turns []llm.ChatTurn) llm.ChatReply
	// seen records the turns of every round-trip, for a failure message.
	seen [][]llm.ChatTurn
}

func (m *scriptedChatModel) ChatWithTools(_ context.Context, turns []llm.ChatTurn, _ []llm.ToolDef) (llm.ChatReply, error) {
	m.seen = append(m.seen, turns)
	if m.next == nil {
		return llm.ChatReply{}, errors.New("scriptedChatModel: no next reply scripted")
	}
	return m.next(turns), nil
}

// lastToolResult returns the content of the most recent tool result the runner
// handed back to the model, and whether there was one. It is how a scripted reply
// ECHOES what the tool lane returned, so the HTTP body's `reply` — the thing the
// user reads — carries the tool's own answer rather than the test's expectation of
// it.
func lastToolResult(turns []llm.ChatTurn) (string, bool) {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "tool" {
			return turns[i].Content, true
		}
	}
	return "", false
}

// toolLaneSide is one of the two workspaces the fixture gives one user: a
// workspace, the account it owns, and a product in it.
type toolLaneSide struct {
	slug    string
	account string
	// accSlug is the ACCOUNT's own slug, which is what get_account reports — the
	// discriminating value, because both accounts take their name from their
	// workspace.
	accSlug string
	product string
	name    string
}

// toolLaneEnv owns a mux wired the way production wires it: the real chat runner
// over the real MCP server, plus the stores the resolution and the tools need.
type toolLaneEnv struct {
	mux    *http.ServeMux
	pool   *pgxpool.Pool
	sess   *auth.Sessions
	userID string
	token  string
	// sides[0] is the FIRST owned account (what the old unscoped fallback picked),
	// sides[1] the workspace the tests select.
	sides [2]toolLaneSide
}

func newToolLaneEnv(t *testing.T, model *scriptedChatModel) *toolLaneEnv {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	sess := authTestValkey(t)

	wsStore := dbstore.NewWorkspaceStore(pool)
	productStore := dbstore.NewProductStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('tool-lane-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var sides [2]toolLaneSide
	for i, label := range []string{"a", "b"} {
		slug := "tl-" + label + "-" + userID[:8]
		if _, err := wsStore.Create(ctx, userID, "Tool Lane "+label, slug); err != nil {
			t.Fatalf("create workspace %s: %v", label, err)
		}
		// AccessBySlug is how a ?workspace= request finds the account; taking the
		// account from IT keeps this fixture honest about what a scoped request
		// resolves to.
		acc, ok, err := wsStore.AccessBySlug(ctx, slug, userID)
		if err != nil || !ok {
			t.Fatalf("resolve workspace %s by slug: ok=%v err=%v", label, ok, err)
		}
		if len(acc.Accounts) != 1 {
			t.Fatalf("workspace %s owns %d accounts, want exactly 1", label, len(acc.Accounts))
		}
		accountID := acc.Accounts[0]
		var accSlug string
		if err := pool.QueryRow(ctx, `SELECT slug FROM accounts WHERE id=$1`, accountID).
			Scan(&accSlug); err != nil {
			t.Fatalf("read account slug for workspace %s: %v", label, err)
		}
		p := &dbstore.Product{
			AccountID: accountID, Name: "tl-product-" + label,
			PriceSen: int64(1000 * (i + 1)), Type: "one_time",
		}
		if err := productStore.Create(ctx, p); err != nil {
			t.Fatalf("create product for workspace %s: %v", label, err)
		}
		sides[i] = toolLaneSide{slug: slug, account: accountID, accSlug: accSlug, product: p.ID, name: p.Name}
	}

	// The premise, checked rather than assumed: the unscoped fallback resolves
	// Accounts.ByOwner(...)[0], so sides[0] must be that row for the regression
	// this file guards to be the one production had.
	accs, err := dbstore.NewAccountStore(pool).ByOwner(ctx, userID)
	if err != nil {
		t.Fatalf("list owned accounts: %v", err)
	}
	if len(accs) != 2 {
		t.Fatalf("fixture user owns %d accounts, want 2", len(accs))
	}
	if accs[0].ID != sides[0].account {
		t.Fatalf("the unscoped fallback resolves account %s, but the fixture's FIRST workspace is %s — "+
			"the premise (that the fallback picks the other workspace than the one selected) does not hold",
			accs[0].ID, sides[0].account)
	}

	mcpSrv := mcpsrv.New(&mcpsrv.Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   productStore,
		Forms:      dbstore.NewFormStore(pool),
		Workspaces: wsStore,
		Users:      dbstore.NewUserStore(pool),
	}, nil, nil, nil)
	runner := chat.New(model, mcpSrv, nil)
	runner.SetProposalStore(dbstore.NewChatWriteProposalStore(pool))

	h := &RESTHandlers{
		Accounts:           dbstore.NewAccountStore(pool),
		Users:              dbstore.NewUserStore(pool),
		Forms:              dbstore.NewFormStore(pool),
		Products:           productStore,
		Workspaces:         wsStore,
		Sessions:           sess,
		Chat:               runner,
		ChatLimiter:        NewChatLimiter(),
		ChatConfirmLimiter: NewChatConfirmLimiter(),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &toolLaneEnv{mux: mux, pool: pool, sess: sess, userID: userID, token: token, sides: sides}
}

// do drives one request through the same mux the routes are registered on, with
// the fixture's session cookie — the same shape newScopeEnv's helper uses.
func (e *toolLaneEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.token})
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, r)
	return rec
}

// laneProductRow is a product row read straight out of Postgres, so an assertion
// cannot be satisfied by a store's cached or derived value.
type laneProductRow struct {
	Name      string
	PriceSen  int64
	UpdatedAt string
}

func (e *toolLaneEnv) productRow(t *testing.T, id string) laneProductRow {
	t.Helper()
	var r laneProductRow
	if err := e.pool.QueryRow(context.Background(),
		`SELECT name, price_sen, updated_at::text FROM products WHERE id=$1`, id).
		Scan(&r.Name, &r.PriceSen, &r.UpdatedAt); err != nil {
		t.Fatalf("read product %s back: %v", id, err)
	}
	return r
}

// TestChatToolLaneReadsTheSelectedWorkspace is the READ half of the card's item 1,
// asserted where the defect actually was: the TOOL LANE, not the handed-over
// account.
//
// The scripted model asks for get_account and the reply echoes the tool result, so
// the HTTP body's `reply` is literally what the tool answered — the same shape the
// live bug produced ("wan Store (`wan`)") and the shape the fix has to change.
// Both directions are asserted: ?workspace=<b> must answer b's account and
// ?workspace=<a> must answer a's. That pair is what cannot pass by accident — an
// unscoped tool lane answers the same thing for both, so it fails one of them
// whichever account the union happens to list first.
func TestChatToolLaneReadsTheSelectedWorkspace(t *testing.T) {
	model := &scriptedChatModel{next: func(turns []llm.ChatTurn) llm.ChatReply {
		if out, ok := lastToolResult(turns); ok {
			return llm.ChatReply{Content: out}
		}
		return llm.ChatReply{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "get_account", Arguments: "{}"}}}
	}}
	e := newToolLaneEnv(t, model)

	for _, selected := range []int{1, 0} {
		other := 1 - selected
		rec := e.do(t, http.MethodPost, "/v1/rest/chat?workspace="+e.sides[selected].slug,
			`{"message":"akaun mana yang anda sedang lihat?","history":[]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("scoped chat POST for workspace %s = %d, want 200 (body %s)",
				e.sides[selected].slug, rec.Code, rec.Body.String())
		}
		var body struct {
			Reply string      `json:"reply"`
			Steps []chat.Step `json:"steps"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("chat response is not the documented envelope: %v (body %s)", err, rec.Body.String())
		}
		// The dispatch happened at all, so a reply that says nothing cannot pass
		// as "the lane is scoped".
		if len(body.Steps) != 1 || body.Steps[0].Tool != "get_account" || body.Steps[0].Status != "called" {
			t.Fatalf("steps = %+v, want one called get_account step — the lane under test did not run",
				body.Steps)
		}
		if !strings.Contains(body.Reply, e.sides[selected].accSlug) {
			t.Fatalf("under ?workspace=%s the get_account TOOL answered %q, which does not name that "+
				"workspace's account %s — the tool lane read a different workspace than the request "+
				"resolved (the first workspace's account is %s)",
				e.sides[selected].slug, body.Reply,
				e.sides[selected].accSlug, e.sides[other].accSlug)
		}
		if strings.Contains(body.Reply, e.sides[other].accSlug) {
			t.Fatalf("under ?workspace=%s the get_account TOOL answered with the OTHER workspace's "+
				"account %s: %q — every tool the assistant calls must read the workspace the user is "+
				"looking at (issue #747)", e.sides[selected].slug, e.sides[other].accSlug, body.Reply)
		}
	}
}

// TestChatConfirmCannotReachAnotherWorkspacesObject is the WRITE half (the card's
// item 3), and it is the half that matters: the READ defect leaks data, this one
// changes it.
//
// The scripted model proposes update_product on the OTHER workspace's product
// while the request is scoped to sides[1]. The confirmation is then sent on the
// same scoped route — no request in this test ever names the other workspace — and
// the expected answer is a REFUSAL: that product is not owned by any account in
// the selected workspace's scope, so the tool's own ownership gate finds nothing
// to write (mcpsrv.resolveProductAccount walks the call's scope).
//
// Why a 200 here is a bug and not a permission question: the confirm token is a
// human approving a card for the workspace they are looking at, and the other
// workspace is not in that scope at all. On the old code this exact request
// answered 200 {"status":"done"} and renamed the row.
func TestChatConfirmCannotReachAnotherWorkspacesObject(t *testing.T) {
	const proposedName = "tl-crossws-written"
	model := &scriptedChatModel{}
	e := newToolLaneEnv(t, model)
	selected, other := e.sides[1], e.sides[0]

	proposalArgs := fmt.Sprintf(
		`{"tool":"update_product","arguments":{"product_id":%q,"name":%q},"reason":"tukar nama produk"}`,
		other.product, proposedName)
	model.next = func(turns []llm.ChatTurn) llm.ChatReply {
		if out, ok := lastToolResult(turns); ok {
			return llm.ChatReply{Content: out}
		}
		return llm.ChatReply{ToolCalls: []llm.ToolCall{{
			ID: "p1", Name: "propose_write", Arguments: proposalArgs,
		}}}
	}

	before := e.productRow(t, other.product)

	rec := e.do(t, http.MethodPost, "/v1/rest/chat?workspace="+selected.slug,
		`{"message":"tukar nama produk itu","history":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped chat POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var answer struct {
		Proposals []chat.ProposalCard `json:"proposals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("chat response is not the documented envelope: %v (body %s)", err, rec.Body.String())
	}
	if len(answer.Proposals) != 1 || answer.Proposals[0].Token == "" {
		t.Fatalf("propose_write produced no token-bearing card: %+v", answer.Proposals)
	}
	card := answer.Proposals[0]

	// The REST edge resolved the SELECTED workspace — that half already worked,
	// and pinning it here keeps a future failure attributable to the tool lane.
	var storedAccount string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT account_id FROM chat_write_proposals WHERE user_id=$1`, e.userID).
		Scan(&storedAccount); err != nil {
		t.Fatalf("read the stored proposal: %v", err)
	}
	if storedAccount != selected.account {
		t.Fatalf("the proposal is stored under account %s, want the SELECTED workspace's %s — the "+
			"REST edge is not resolving ?workspace=", storedAccount, selected.account)
	}

	confirmBody := fmt.Sprintf(`{"token":%q,"args_hash":%q}`, card.Token, card.ArgsHash)
	rec = e.do(t, http.MethodPost, "/v1/rest/chat/confirm?workspace="+selected.slug, confirmBody)
	if rec.Code == http.StatusOK {
		t.Fatalf("confirming a card for a product owned by ANOTHER workspace answered %d (%s) — the "+
			"confirmed call targeted an object outside the workspace the request resolved, which is "+
			"the write half of issue #747", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("confirm = %d, want 400 (the server's own ownership gate refused) — body %s",
			rec.Code, rec.Body.String())
	}
	if got := e.productRow(t, other.product); got != before {
		t.Fatalf("the OTHER workspace's product changed: %+v -> %+v — a confirmation is authorised "+
			"for the workspace the user selected, and nothing else", before, got)
	}
	if got := e.productRow(t, selected.product).Name; got == proposedName {
		t.Fatalf("the selected workspace's product was renamed to %q — that product was not the one "+
			"the card named", got)
	}
}
