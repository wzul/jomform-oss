package chat

// The WORKSPACE-SCOPE acceptance run for the confirmation lane.
//
// THE DEFECT THIS FILE EXISTS FOR. A merchant with two workspaces selects the
// second one in the Shell. The card's claim is that confirming a proposal the
// assistant prepared changes the SELECTED workspace's product and leaves the
// other workspace's product byte-identical.
//
// It is not a hypothetical: the /chat page used to send no ?workspace= at all,
// so the server resolved `Accounts.ByOwner(...)[0]` — the user's FIRST account —
// and the proposal lane made that account WRITABLE through the confirm token. The
// card the user read named the first account's product while the user was looking
// at the second.
//
// This test drives the REAL seam rather than the page: the account a request
// resolves is what production passes into chatContextFor /
// chatConfirmContextFor, so it builds the identity for the workspace the caller
// SELECTED and requires the write to land there — and requires the OTHER
// workspace's product row to be byte-identical afterwards (name, price_sen and
// updated_at), read straight back out of Postgres. A refusal that arrived after
// the write, or a write that landed in the wrong workspace, both fail.
//
// WHAT IT DOES NOT COVER (stated honestly):
//
//   * The HTTP resolution itself — resolveSessionAccount /
//     requireResolvedWorkspaceScope — is pinned in internal/httpapi, where the
//     request lives; and the page's own ?workspace= is pinned by the
//     cross-language guard in web/src/lib/chatPage.test.ts.
//   * WHICH ACCOUNT THE CONFIRMATION RAN UNDER is not discriminated here, and a
//     mutation proved it: feeding this test the OTHER workspace's AccountID still
//     passes. That is not a hole in the lane, it is how the MCP layer works —
//     resolveEntityAccount walks ownedOrScopedAccounts (issue #307), i.e. every
//     account the USER can reach, so an entity-keyed write is routed by which
//     account OWNS the product, not by the identity's AccountID
//     (api/internal/mcpsrv/callscope.go). The identity's account is what the
//     HTTP edge must get right, and that assertion lives in
//     chat_workspace_scope_test.go, which sets it directly.
//
// So this file owns the DATA boundary the other two point at: the confirmed
// rename lands on the named product's row and the other workspace's row is
// byte-identical.
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the standard test env).

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/testdb"
)

// twoWorkspaceFixture is one user owning TWO workspaces, each with its own
// account and its own product. Everything below reads and writes through the same
// stores production uses; the ids are the only fixture state.
type twoWorkspaceFixture struct {
	pool   *pgxpool.Pool
	userID string
	srv    *mcpsrv.Server
	// selected / other are the two workspaces, in the order the Shell would
	// list them: `first` is the one Accounts.ByOwner returns FIRST (hence the one
	// the unscoped fallback used to pick), `second` is the one the user actually
	// selected in the Shell.
	first  twoWorkspaceSide
	second twoWorkspaceSide
}

type twoWorkspaceSide struct {
	slug      string
	workspace string
	account   string
	product   string
	// name is the product's starting name, read back from the store.
	name string
}

func newTwoWorkspaceFixture(t *testing.T) *twoWorkspaceFixture {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping the workspace-scope acceptance run")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	// Created through the stores, not raw SQL, so this fixture cannot drift from
	// the schema (Workspaces.Create also links the new account to the workspace,
	// which is the ownership rule the resolution depends on).
	wsStore := dbstore.NewWorkspaceStore(pool)
	productStore := dbstore.NewProductStore(pool)

	// An explicit owner id: the workspace store takes the owner user id, and the
	// row must exist for the FK.
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('chat-scope-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	mk := func(label string, priceSen int64) twoWorkspaceSide {
		slug := "scope-" + label + "-" + userID[:8]
		ws, err := wsStore.Create(ctx, userID, "Scope "+label, slug)
		if err != nil {
			t.Fatalf("create workspace %s: %v", label, err)
		}
		// AccessBySlug is how the resolver finds the account for a ?workspace=
		// slug; taking the account from IT (rather than assuming Create's account)
		// is what keeps this fixture honest about which account a scoped request
		// resolves.
		acc, ok, err := wsStore.AccessBySlug(ctx, slug, userID)
		if err != nil || !ok {
			t.Fatalf("resolve workspace %s by slug: ok=%v err=%v", label, ok, err)
		}
		if len(acc.Accounts) != 1 {
			t.Fatalf("workspace %s owns %d accounts, want exactly 1 (each workspace owns its own account)",
				label, len(acc.Accounts))
		}
		accountID := acc.Accounts[0]
		p := &dbstore.Product{
			AccountID: accountID, Name: "product-" + label, PriceSen: priceSen, Type: "one_time",
		}
		if err := productStore.Create(ctx, p); err != nil {
			t.Fatalf("create product for %s: %v", label, err)
		}
		return twoWorkspaceSide{
			slug: slug, workspace: ws.ID, account: accountID, product: p.ID, name: p.Name,
		}
	}

	f := &twoWorkspaceFixture{pool: pool, userID: userID}
	f.first = mk("a", 1000)
	f.second = mk("b", 2000)

	// The premise, checked rather than assumed: the unscoped fallback picks
	// Accounts.ByOwner(...)[0], so `first` MUST be that row for the regression
	// this test guards to be the one production had. (accounts.created_at is the
	// ordering key; both rows are created milliseconds apart by the fixture.)
	accs, err := dbstore.NewAccountStore(pool).ByOwner(ctx, userID)
	if err != nil {
		t.Fatalf("list owned accounts: %v", err)
	}
	if len(accs) != 2 {
		t.Fatalf("fixture user owns %d accounts, want 2", len(accs))
	}
	if accs[0].ID != f.first.account {
		t.Fatalf("the unscoped fallback would resolve account %s, but the fixture's FIRST workspace is %s "+
			"— this test's premise (that the fallback picks the other workspace than the user selected) "+
			"does not hold, so it would not be measuring the defect",
			accs[0].ID, f.first.account)
	}

	f.srv = mcpsrv.New(&mcpsrv.Deps{
		Forms:      dbstore.NewFormStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Keys:       dbstore.NewAPIKeyStore(pool),
		Products:   productStore,
		Workspaces: wsStore,
		Users:      dbstore.NewUserStore(pool),
	}, nil, nil, nil)
	return f
}

// productRow reads the product straight out of Postgres — the fixture's own
// column-aligned read, so an assertion cannot be satisfied by a store's cached or
// derived value.
type productRow struct {
	Name      string
	PriceSen  int64
	UpdatedAt string
}

func (f *twoWorkspaceFixture) productRow(t *testing.T, id string) productRow {
	t.Helper()
	var r productRow
	if err := f.pool.QueryRow(context.Background(),
		`SELECT name, price_sen, updated_at::text FROM products WHERE id=$1`, id).
		Scan(&r.Name, &r.PriceSen, &r.UpdatedAt); err != nil {
		t.Fatalf("read product %s back: %v", id, err)
	}
	return r
}

// identityFor builds the READ-ONLY identity the chat's message path uses, for a
// given account — the same construction chatContextFor performs in the REST edge.
func identityFor(ctx context.Context, accountID, userID string) context.Context {
	return mcpsrv.WithAuthIdentity(ctx, &mcpsrv.AuthIdentity{
		AccountID: accountID, UserID: userID, Scopes: ReadOnlyScopes(), Agent: "in-app-chat",
	})
}

// TestConfirmWritesToTheSelectedWorkspaceAndLeavesTheOtherUntouched is the
// card's acceptance criterion, end to end through the real proposal lane.
//
// Steps, all through production code paths:
//  1. the assistant (a READ-ONLY identity, exactly as the edge builds it) prepares
//     a proposal to rename this workspace's product,
//  2. the human confirms it under the WRITE identity built for the SELECTED
//     workspace's account,
//  3. the selected workspace's product is renamed, and the OTHER workspace's
//     product row is byte-identical (name, price_sen, updated_at) — read back from
//     Postgres, not from the store's return value.
//
// The negative half is what makes it a scope test: a lane that wrote to the wrong
// account, or one that wrote to both, fails on the read-back.
func TestConfirmWritesToTheSelectedWorkspaceAndLeavesTheOtherUntouched(t *testing.T) {
	f := newTwoWorkspaceFixture(t)
	ctx := context.Background()

	proposals := dbstore.NewChatWriteProposalStore(f.pool)
	r := &Runner{mcp: f.srv, proposals: proposals}
	r.newSession = r.mcpSession

	selected := f.second
	other := f.first

	// A card naming the SELECTED workspace's product.
	card := proposeRename(t, r, identityFor(ctx, selected.account, f.userID), selected.product,
		selected.name+" (renamed in "+selected.slug+")")

	// What the other workspace's product looked like before the confirmation.
	before := f.productRow(t, other.product)

	if got := f.productRow(t, selected.product).Name; got == cardTargetName(card) {
		t.Fatalf("the product already carries the proposed name %q before any confirmation — "+
			"the fixture cannot show the write happening", got)
	}

	// The human's click: the write identity is built for the SELECTED workspace's
	// account, which is what the REST edge resolves from ?workspace=<slug>.
	writeCtx := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: selected.account, UserID: f.userID, Scopes: WriteScopes(),
		Agent: "in-app-chat-confirm",
	})
	confirmed, err := r.Confirm(ctx, writeCtx, f.userID, card.Token, card.ArgsHash)
	if err != nil {
		t.Fatalf("confirm the proposal: %v — the proposal prepared under a read-only identity must be "+
			"executable by the human's confirmation", err)
	}
	if confirmed.Tool != "update_product" {
		t.Fatalf("the confirmed tool was %q, want update_product", confirmed.Tool)
	}

	// 1. The SELECTED workspace changed.
	after := f.productRow(t, selected.product)
	if after.Name != cardTargetName(card) {
		t.Fatalf("the selected workspace's product name is %q after the confirmation, want %q — "+
			"the confirmed write did not land in the workspace the user selected",
			after.Name, cardTargetName(card))
	}
	// 2. The OTHER workspace did not — compared on the full row, and on
	//    updated_at as well as the values, so a no-op write that touched the row
	//    is caught. (updated_at is what a "write" that changed nothing would still
	//    move.)
	if got := f.productRow(t, other.product); got != before {
		t.Fatalf("the OTHER workspace's product row changed: %+v -> %+v — a confirmation is "+
			"authorised for the workspace the user selected, and nothing else", before, got)
	}
}

// cardTargetName recovers the name the card's fields carry, so the assertion does
// not restate the fixture's own literal (a typo in the test would otherwise agree
// with itself).
func cardTargetName(card *ProposalCard) string {
	for _, fld := range card.Fields {
		if fld.LabelKey == keyLabelName {
			return fld.Value
		}
	}
	return ""
}

// proposeRename prepares a rename proposal through the model's own path
// (propose_write), under the identity it is given.
func proposeRename(t *testing.T, r *Runner, ctx context.Context, productID, newName string) *ProposalCard {
	t.Helper()
	args := `{"tool":"update_product","arguments":{"product_id":` + jsonString(productID) +
		`,"name":` + jsonString(newName) + `},"reason":"tukar nama produk"}`
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the propose session: %v", err)
	}
	defer caller.close()
	st := &runState{}
	out, refused, err := r.dispatchIn(ctx, caller, toolCall("p1", ToolProposeWrite, args), st)
	if err != nil {
		t.Fatalf("dispatch propose_write: %v", err)
	}
	if refused {
		t.Fatalf("propose_write was refused (%s) — the lane must exist for this test to mean anything", out)
	}
	card := st.lastCard()
	if card == nil || card.Token == "" {
		t.Fatalf("propose_write produced no token-bearing card (state=%+v, model saw %s)", st, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("the propose_write result fed to the model is not JSON: %s", out)
	}
	return card
}
