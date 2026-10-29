package mcpsrv

// set_shipping_rates could not remove an account's LAST zone set (card t_417fbacb).
//
// THE DEFECT, measured on prod v1.21.345. dbstore.ValidateShippingSets refused a
// zero-length `sets` list ("sekurang-kurangnya satu set zon diperlukan"), so BOTH
// write doors — PUT /v1/rest/shipping and this tool — answered 400 for exactly the
// request that removes the last set. The dashboard's Shipping tab offers a Delete
// on every set including the only one, and its empty view describes zero sets as a
// legitimate state ("No zone set yet. Add one, then add the zones and their
// rates."), so the merchant had a control that could never succeed, and a merchant
// who wanted managed rates gone had no route at all.
//
// WHY THIS FILE IS THE MCP ONE. The REST half is pinned in
// httpapi/rest_shipping_test.go and the store half in dbstore/shipping_test.go.
// The tool has its own door — API-key write scope, the manage-or-admin role in
// the named workspace, its own input type — so a fix that only satisfied the
// validator would leave it unproven that this door reaches the deletion. These
// calls go through the real MCP server over HTTP with a real API key, and the
// assertions are read back from Postgres.
//
// The safety half is asserted too: an EMPTY payload still cannot remove a set a
// form's shipping block points at. That refusal is what makes "no sets" a state
// nothing can break, and it is the reason the validator does not have to hold the
// list non-empty.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
	"github.com/wzul/jomform/api/internal/testdb"
)

// shipMCPServer seeds a user + workspace + account with a readwrite API key and
// returns the real MCP server over HTTP, the account it is scoped to and the raw
// key.
func shipMCPServer(t *testing.T) (url, rawKey, accountID, workspaceID string, ship *dbstore.ShippingStore, pool *pgxpool.Pool) {
	t.Helper()
	requireTestDB(t)
	ctx := context.Background()
	pool = testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('ship-lastset-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'ship-lastset-ws-' || md5(random()::text), 'ship-lastset-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'ship-lastset-acc-' || md5(random()::text), 'ship-lastset-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	rawKey = "jf_shiplastset_" + accountID[:8] + "0000000000"
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, auth.HashAPIKey(rawKey),
		"ship-lastset", []string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	ship = dbstore.NewShippingStore(pool)
	srv := New(&Deps{
		Accounts:         dbstore.NewAccountStore(pool),
		Keys:             dbstore.NewAPIKeyStore(pool),
		Users:            dbstore.NewUserStore(pool),
		Forms:            dbstore.NewFormStore(pool),
		Workspaces:       dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers: dbstore.NewWorkspaceMemberStore(pool),
		Shipping:         ship,
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL, rawKey, accountID, workspaceID, ship, pool
}

// seedOneShipSet saves one resolvable set (the state the dashboard creates by
// clicking through the Shipping tab).
func seedOneShipSet(t *testing.T, ship *dbstore.ShippingStore, accountID string) {
	t.Helper()
	if err := ship.Replace(context.Background(), accountID, []shipping.ZoneSet{{
		Name: "Malaysia", Default: true,
		Zones: []shipping.Zone{{
			Name: "West", States: []string{"peninsular"},
			Rates: []shipping.Rate{{Label: "Standard", PriceSen: 800}},
		}},
	}}); err != nil {
		t.Fatalf("seed zone set: %v", err)
	}
}

// TestMCPSetShippingRatesRemovesTheLastSet is the regression guard: an empty
// `sets` list must delete the account's only set instead of being refused.
func TestMCPSetShippingRatesRemovesTheLastSet(t *testing.T) {
	url, rawKey, accountID, workspaceID, ship, pool := shipMCPServer(t)
	ctx := context.Background()
	seedOneShipSet(t, ship, accountID)

	sets, err := ship.Sets(ctx, []string{accountID})
	if err != nil || len(sets) != 1 {
		t.Fatalf("seeded sets = %d err=%v, want 1", len(sets), err)
	}

	out := mcpResult(t, url, rawKey, "set_shipping_rates",
		map[string]any{"workspace_id": workspaceID, "sets": []any{}})

	// The tool's own answer: no sets left.
	if raw, ok := out["sets"].([]any); !ok || len(raw) != 0 {
		t.Fatalf("set_shipping_rates with an empty list answered sets=%v, want [] — "+
			"the merchant's only way to remove the last set is still a refusal", out["sets"])
	}

	// ...and it is true in Postgres, not just in the response.
	sets, err = ship.Sets(ctx, []string{accountID})
	if err != nil || len(sets) != 0 {
		t.Fatalf("after the empty save the account still has %d set(s) (err=%v) — "+
			"the deletion did not reach the store", len(sets), err)
	}
	var zones, rates int
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM shipping_zones WHERE account_id=$1),
		        (SELECT count(*) FROM shipping_rates WHERE account_id=$1)`,
		accountID).Scan(&zones, &rates); err != nil {
		t.Fatalf("count zones/rates: %v", err)
	}
	if zones != 0 || rates != 0 {
		t.Errorf("zones=%d rates=%d survived the set they belong to", zones, rates)
	}
}

// TestMCPSetShippingRatesRefusesAnEmptyListOverAnInUseSet is the safety half: the
// empty payload must NOT be able to break a live form's shipping block.
func TestMCPSetShippingRatesRefusesAnEmptyListOverAnInUseSet(t *testing.T) {
	url, rawKey, accountID, workspaceID, ship, pool := shipMCPServer(t)
	ctx := context.Background()
	seedOneShipSet(t, ship, accountID)
	sets, err := ship.Sets(ctx, []string{accountID})
	if err != nil || len(sets) != 1 {
		t.Fatalf("seeded sets = %d err=%v, want 1", len(sets), err)
	}
	setID := sets[0].ID

	if err := dbstore.NewFormStore(pool).Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: "ship-lastset-form-" + accountID[:8],
		Title: "Delivery form", Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1", "source": "zones", "zone_set_id": setID},
		}},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}

	res := mcpCall(t, url, rawKey, "set_shipping_rates",
		map[string]any{"workspace_id": workspaceID, "sets": []any{}})
	if !res.IsError {
		t.Fatalf("an empty save over a set a form still points at was ACCEPTED (%v) — "+
			"that would leave the form's shipping block resolving to nothing", res.StructuredContent)
	}
	msg := contentText(res)
	if !strings.Contains(msg, dbstore.ShippingSetInUseLeadIn) || !strings.Contains(msg, "Delivery form") {
		t.Errorf("refusal = %q, want it to name the form using the set", msg)
	}

	// The transaction rolled back: the set is still there and still the same row.
	sets, err = ship.Sets(ctx, []string{accountID})
	if err != nil || len(sets) != 1 || sets[0].ID != setID {
		t.Fatalf("a refused empty save changed the configuration: %+v (err=%v)", sets, err)
	}
}
