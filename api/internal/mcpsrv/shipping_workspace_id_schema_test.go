package mcpsrv

// The shipping write tools — set_shipping_rates, connect_shipping_courier,
// disconnect_shipping_courier — used to promise "Kosong = akaun anda sendiri" in
// their own input schemas and then refuse an empty `workspace_id` with
// "workspace tidak dijumpai", while the READ halves (get_shipping_rates,
// get_shipping_couriers) really do fall back to the caller's account.
//
// OBSERVED with one readwrite API key on one workspace, one variable per call:
//
//	tools/call get_shipping_rates {}                      -> {account_id, sets:[…]}   (ok)
//	tools/call set_shipping_rates {"sets":[…]}             -> isError "workspace tidak dijumpai"
//	tools/call set_shipping_rates {"workspace_id":"…","sets":[…]} -> written, read back OK
//
// So an agent that followed the write tool's own schema got a message that reads
// like a tenancy failure on its first write.
//
// WHY THE FIX IS THE SENTENCE AND NOT THE GATE. The write gate is
// requireWorkspaceWriteRole, which checks the caller's manage-or-admin ROLE in a
// NAMED workspace (issue #162) — a role is a role IN a workspace, so there is
// nothing to check when no workspace is named. The fallback the read uses
// (accountAndScope → CallScope.Accounts[0], issue #307) picks the FIRST of every
// account the user can reach, which for a multi-workspace OAuth caller is not
// "your own account" but an arbitrary one — the same ambiguity #747 resolved for
// chat by REFUSING a call whose workspace scope cannot be resolved instead of
// falling back to an account. Teaching the write half that fallback would widen a
// tenancy gate to fix a doc bug. The honest fix is therefore the cheap one: the
// promise stays on the READ that implements it and is gone from the WRITE that
// refuses.
//
// WHAT THIS FILE GUARDS, AND IN WHICH DIRECTION
//
//  1. TestNoWriteToolAdvertisesTheOwnAccountFallback — SCHEMA ↔ GATE pairing, no
//     database. For every tool registerTools serves, if its handler can reach
//     requireWorkspaceWriteRole then its `workspace_id` description must not
//     advertise the fallback. THIS IS THE HALF THAT BITES ON THE PRE-FIX TREE:
//     the three shipping write tools still said "Kosong = akaun anda sendiri"
//     while gating through requireWorkspaceWriteRole, so it goes RED before the
//     fix and GREEN after.
//  2. TestShippingWorkspaceIDWritesTheBehaviourTheSchemaStates — the behaviour
//     the schemas now claim, over the real handlers with a real database and the
//     API-KEY identity shape that was measured: an empty id on the WRITE is
//     refused with "workspace tidak dijumpai" and writes NOTHING, the same key
//     with an explicit workspace writes and reads back, and the empty id on the
//     READ still resolves the caller's own account (the promise the read keeps).
//     It passes on the pre-fix tree as well, so it is a CONTRACT NET, not the fix
//     proof: if a later change makes the write fall back, this test reddens and
//     forces the schema sentence back with it — which is the state to aim for
//     ("the sentence and the behaviour must agree").

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// ownAccountFallbackPromise is the exact sentence the write schemas used to carry.
// It is spelled out HERE, not read from the source, so deleting it from the schema
// cannot also delete the guard's ability to notice it coming back.
const ownAccountFallbackPromise = "Kosong = akaun anda sendiri"

// workspaceWriteGateFunc is the gate whose presence makes the promise a lie: it
// refuses an empty workspace id by construction.
const workspaceWriteGateFunc = "requireWorkspaceWriteRole"

// wireTools lists every tool registerTools actually serves, with its input schema
// as the client receives it — the same authority mcp_parity_test.go's
// registeredToolNames measures against, kept here because this guard needs the
// schema and not just the name.
func wireTools(t *testing.T) map[string]*mcp.Tool {
	t.Helper()
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "workspace-id-schema-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	got := map[string]*mcp.Tool{}
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			got[tl.Name] = tl
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(got) == 0 {
		t.Fatal("no tools registered — registerTools did not run")
	}
	return got
}

// workspaceIDDescription returns the `description` the schema advertises for the
// `workspace_id` property, or "" when the tool has no such property.
func workspaceIDDescription(t *testing.T, tool *mcp.Tool) string {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("tool %q: marshal input schema: %v", tool.Name, err)
	}
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("tool %q: parse input schema %s: %v", tool.Name, raw, err)
	}
	return schema.Properties["workspace_id"].Description
}

// TestNoWriteToolAdvertisesTheOwnAccountFallback is the card's guard: a tool whose
// handler gates through requireWorkspaceWriteRole can never honour "Kosong = akaun
// anda sendiri", because that gate refuses an empty workspace id before any
// account resolution happens. The sentence and the gate must not coexist.
func TestNoWriteToolAdvertisesTheOwnAccountFallback(t *testing.T) {
	pkg := parsePackage(t)
	handlerOf := map[string]string{}
	for _, tool := range pkg.tools {
		handlerOf[tool.Name] = tool.Handler
	}

	wire := wireTools(t)
	if len(wire) < 100 {
		t.Fatalf("the wire served only %d tool(s) — this guard would pass vacuously", len(wire))
	}

	gated, promising := 0, 0
	var offenders []string
	for name, tool := range wire {
		claims := strings.Contains(workspaceIDDescription(t, tool), ownAccountFallbackPromise)
		handler := handlerOf[name]
		if handler == "" {
			t.Errorf("tool %q is served but the source parser does not know its handler — this guard "+
				"cannot tell whether it refuses an empty workspace_id", name)
			continue
		}
		reachesGate := pkg.reachableFuncs(handler)[workspaceWriteGateFunc]
		if reachesGate {
			gated++
			if claims {
				offenders = append(offenders, name)
			}
		}
		if claims {
			promising++
		}
	}

	// Floors, so a rename or a call-graph parse failure cannot make the sweep
	// pass by finding nothing. Each number is a live count on this tree, not a
	// guess: the shipping/legal/currency/workspace write tools all gate here.
	if gated < 5 {
		t.Fatalf("only %d tool(s) were recognised as gating through %s — the call-graph sweep is "+
			"measuring nothing", gated, workspaceWriteGateFunc)
	}
	if promising < 1 {
		t.Fatalf("no served tool advertises %q any more. That is the read half's promise "+
			"(get_shipping_rates / get_shipping_couriers do implement it), so either the read was "+
			"changed too — in which case update this guard with the behaviour — or the sentence was "+
			"dropped from everywhere and this test no longer measures the pairing it is named for",
			ownAccountFallbackPromise)
	}

	if len(offenders) > 0 {
		t.Errorf("%d write tool(s) advertise %q but gate through %s, which refuses an empty "+
			"workspace_id with %q — an agent following the schema gets a tenancy error on its first "+
			"write: %v\nEither drop the sentence from the write schema (the read keeps it; the write "+
			"gate needs an EXPLICIT workspace because the role it checks is a role IN a workspace), "+
			"or implement the fallback in the gate and keep the sentence.",
			len(offenders), ownAccountFallbackPromise, workspaceWriteGateFunc,
			"workspace tidak dijumpai", offenders)
	}
	t.Logf("%d served tools reach %s; %d serve the own-account fallback sentence "+
		"(expected: the reads only — get_shipping_rates / get_shipping_couriers)",
		gated, workspaceWriteGateFunc, promising)
}

// shippingRatesFixture wires the real shipping/account/workspace stores over a
// real database with the API-KEY identity shape QA measured on prod: the
// principal is an ACCOUNT whose owner is resolved from the row (UserID empty),
// which is what resolveScope short-circuits to the key's own account.
func shippingRatesFixture(t *testing.T) (srv *Server, ctx context.Context, wsID, accountID string, store *dbstore.ShippingStore, pool *pgxpool.Pool) {
	t.Helper()
	requireTestDB(t)
	ctx = context.Background()
	pool = testdb.Open(t)

	userID := insertUser(t, pool, "ship-ws")
	sp := dbstore.NewWorkspaceStore(pool)
	ws, err := sp.Create(ctx, userID, "Ship WS", "ship-ws-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	// Workspaces.Create makes the workspace AND a fresh account linked to it; the
	// account id is what every shipping row is keyed on.
	acc, ok, err := sp.AccessByID(ctx, ws.ID, userID)
	if err != nil || !ok || len(acc.Accounts) != 1 {
		t.Fatalf("resolve workspace account: ok=%v accounts=%v err=%v", ok, acc.Accounts, err)
	}
	accountID = acc.Accounts[0]

	store = dbstore.NewShippingStore(pool)
	srv = &Server{deps: &Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: sp,
		Shipping:   store,
	}}
	ctx = WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	return srv, ctx, ws.ID, accountID, store, pool
}

// insertUser creates one user row (the owner the workspace resolver reads).
func insertUser(t *testing.T, pool *pgxpool.Pool, tag string) string {
	t.Helper()
	var userID string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash)
		 VALUES ('`+tag+`-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

// oneShippingSet is a minimal VALID configuration (unnamed zone/rate would be
// refused by ValidateShippingSets before the store is touched, which would hide a
// wiring bug behind a validation message).
func oneShippingSet() []shippingSetIn {
	return []shippingSetIn{{
		Name: "QA Zone Rates",
		Zones: []shippingZoneIn{{
			Name:   "West Malaysia",
			States: []string{"Selangor"},
			Rates:  []shippingRateIn{{Label: "Standard", PriceSen: 800}},
		}},
	}}
}

// TestShippingWorkspaceIDWritesTheBehaviourTheSchemaStates pins the behaviour the
// two schemas now claim, with the same credential for every call so the empty id
// is the only variable — QA's prod measurement, re-made locally.
func TestShippingWorkspaceIDWritesTheBehaviourTheSchemaStates(t *testing.T) {
	srv, ctx, wsID, accountID, store, pool := shippingRatesFixture(t)
	setsBefore, err := store.Sets(ctx, []string{accountID})
	if err != nil {
		t.Fatalf("read shipping sets: %v", err)
	}
	if len(setsBefore) != 0 {
		t.Fatalf("fixture is not clean: %d shipping set(s) already exist on account %s",
			len(setsBefore), accountID)
	}

	// ── The WRITE half: an empty workspace_id is REFUSED, and writes nothing.
	_, _, err = srv.setShippingRates(ctx, nil, setShippingRatesIn{Sets: oneShippingSet()})
	if err == nil {
		t.Fatalf("set_shipping_rates with an empty workspace_id was ACCEPTED. The write schemas no "+
			"longer promise %q, so either the refusal is back (keep this test as the net) or the "+
			"fallback was implemented (then the sentence belongs in the schema again — %s)",
			ownAccountFallbackPromise, "TestNoWriteToolAdvertisesTheOwnAccountFallback")
	}
	if !strings.Contains(err.Error(), "workspace tidak dijumpai") {
		t.Errorf("set_shipping_rates with an empty workspace_id refused with %q, want the tenancy "+
			"message %q — a different error means the call failed somewhere other than the workspace "+
			"gate and this test no longer measures the gate", err.Error(), "workspace tidak dijumpai")
	}
	setsAfterRefusal, err := store.Sets(ctx, []string{accountID})
	if err != nil {
		t.Fatalf("read shipping sets after refusal: %v", err)
	}
	if len(setsAfterRefusal) != 0 {
		t.Fatalf("the REFUSED call still wrote %d shipping set(s) — the gate ran after the write",
			len(setsAfterRefusal))
	}

	// ── The same key WITH an explicit workspace must still write, or the refusal
	// above is satisfied by "everything fails" and proves nothing.
	_, out, err := srv.setShippingRates(ctx, nil, setShippingRatesIn{WorkspaceID: wsID, Sets: oneShippingSet()})
	if err != nil {
		t.Fatalf("set_shipping_rates with workspace_id=%s was refused: %v", wsID, err)
	}
	if out == nil || out.AccountID != accountID {
		t.Fatalf("set_shipping_rates resolved account %v, want the key's own account %s", out, accountID)
	}
	if len(out.Sets) != 1 || out.Sets[0].Name != "QA Zone Rates" {
		t.Fatalf("set_shipping_rates returned %d set(s) (%v), want the one written set",
			len(out.Sets), out.Sets)
	}
	if len(out.Sets[0].Zones) != 1 || len(out.Sets[0].Zones[0].Rates) != 1 ||
		out.Sets[0].Zones[0].Rates[0].PriceSen != 800 {
		t.Fatalf("the written set came back with the wrong zones/rates: %+v — money is integer sen "+
			"(RM8.00 = 800)", out.Sets[0])
	}
	writtenID := out.Sets[0].ID

	// ── The READ half keeps the promise the write half dropped: with NO
	// workspace_id it resolves the caller's own account and returns the row.
	_, read, err := srv.getShippingRates(ctx, nil, getShippingRatesIn{})
	if err != nil {
		t.Fatalf("get_shipping_rates with an empty workspace_id was refused: %v — the read schema "+
			"still promises %q and must keep honouring it", err, ownAccountFallbackPromise)
	}
	if read == nil || read.AccountID != accountID {
		t.Fatalf("get_shipping_rates resolved account %v, want the key's own account %s", read, accountID)
	}
	if len(read.Sets) != 1 || read.Sets[0].ID != writtenID {
		t.Fatalf("get_shipping_rates read %v back, want the set %s the write just stored",
			read.Sets, writtenID)
	}

	// ── And the gate is still a TENANCY gate, not merely an empty-string check:
	// another user's workspace is refused and writes nothing.
	_, _, err = srv.setShippingRates(ctx, nil, setShippingRatesIn{WorkspaceID: foreignWorkspaceID(t, pool, srv), Sets: oneShippingSet()})
	if err == nil || !strings.Contains(err.Error(), "workspace tidak dijumpai") {
		t.Errorf("set_shipping_rates on ANOTHER owner's workspace returned %v, want %q — the write "+
			"gate must not become an empty-string check", err, "workspace tidak dijumpai")
	}
	stillOne, err := store.Sets(ctx, []string{accountID})
	if err != nil {
		t.Fatalf("read shipping sets after foreign-workspace attempt: %v", err)
	}
	if len(stillOne) != 1 {
		t.Fatalf("the refused foreign-workspace call changed this account's sets: %d set(s), want 1",
			len(stillOne))
	}
}

// foreignWorkspaceID creates a workspace owned by a DIFFERENT user and returns its
// id, for the cross-tenant assertion above.
func foreignWorkspaceID(t *testing.T, pool *pgxpool.Pool, srv *Server) string {
	t.Helper()
	other := insertUser(t, pool, "ship-other")
	ws, err := srv.deps.Workspaces.Create(context.Background(), other, "Other WS", "ship-other-"+other[:8])
	if err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("foreign workspace came back with no id")
	}
	return ws.ID
}
