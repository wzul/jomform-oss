package dbstore

// Managed shipping zones + rates: the store half, against a real Postgres.
//
// The resolver's RULES are unit-tested in internal/shipping (no DB). These prove
// the store's own guarantees, which is where a silent break would hurt most:
//
//   - a save PRESERVES a zone set's id, so every form pointing at it keeps
//     resolving (a regenerated id would quietly disconnect live forms);
//   - a set the payload omits is DELETED, and that delete is REFUSED while a form
//     still references it (deleting it would make every checkout of that form a
//     400);
//   - the reading is scoped to the account set it is given (a foreign account's
//     sets and their zones/rates never leak into another account's view);
//   - ValidateShippingSets refuses the shapes that could not resolve.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/shipping"
	"github.com/wzul/jomform/api/internal/testdb"
)

// bundlePool opens the per-process scratch schema (testdb) for this package.
func bundlePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.Open(t)
}

// shippingFixture seeds a user + workspace + account and returns the account id.
func shippingFixture(t *testing.T) (accountID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundlePool(t)
	var userID, workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-shipstore-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-shipstore-' || md5(random()::text), 'it-shipstore-' || md5(random()::text)) RETURNING id`,
		userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-shipstore-acc-' || md5(random()::text), 'it-shipstore-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID
}

func sampleSets() []shipping.ZoneSet {
	return []shipping.ZoneSet{{
		Name: "Malaysia", Default: true,
		Zones: []shipping.Zone{
			{Name: "West Malaysia", States: []string{"peninsular"}, Priority: 0,
				Rates: []shipping.Rate{
					{Label: "Standard", PriceSen: 800, FreeOverSen: 15000, Sort: 0},
					{Label: "Express", PriceSen: 1600, Sort: 1},
				}},
			{Name: "East Malaysia", States: []string{"east_malaysia"}, Priority: 1,
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 1500}}},
			{Name: "Elsewhere", Fallback: true, Priority: 99,
				Rates: []shipping.Rate{{Label: "Standard", PriceSen: 5000}}},
		},
	}}
}

// A save round-trips exactly, and the ORDER of the rates is the store's
// documented (sort, price, label) — the resolver depends on it.
func TestShippingStoreReplaceRoundTrips(t *testing.T) {
	ctx := context.Background()
	pool := bundlePool(t)
	accountID := shippingFixture(t)
	store := NewShippingStore(pool)

	if err := store.Replace(ctx, accountID, sampleSets()); err != nil {
		t.Fatalf("replace: %v", err)
	}
	sets, err := store.Sets(ctx, []string{accountID})
	if err != nil || len(sets) != 1 {
		t.Fatalf("sets = %d err=%v, want 1", len(sets), err)
	}
	set := sets[0]
	if set.Name != "Malaysia" || !set.Default {
		t.Errorf("set = %+v, want Malaysia/default", set)
	}
	if len(set.Zones) != 3 {
		t.Fatalf("zones = %d, want 3", len(set.Zones))
	}
	// Priority order is the read order, and the fallback flag survives.
	if set.Zones[0].Name != "West Malaysia" || set.Zones[0].Fallback {
		t.Errorf("zone[0] = %+v, want West Malaysia (not fallback)", set.Zones[0])
	}
	if !set.Zones[2].Fallback || set.Zones[2].Name != "Elsewhere" {
		t.Errorf("zone[2] = %+v, want the fallback zone last", set.Zones[2])
	}
	west := set.Zones[0]
	if len(west.Rates) != 2 {
		t.Fatalf("west rates = %d, want 2", len(west.Rates))
	}
	if west.Rates[0].Label != "Standard" || west.Rates[0].PriceSen != 800 || west.Rates[0].FreeOverSen != 15000 {
		t.Errorf("rate[0] = %+v, want Standard/800/free over 15000", west.Rates[0])
	}
	if west.Rates[1].PriceSen != 1600 {
		t.Errorf("rate[1] = %+v, want Express/1600", west.Rates[1])
	}
	// The zone rules survive as given (the region token is data, not a state).
	if len(west.States) != 1 || west.States[0] != "peninsular" {
		t.Errorf("west states = %v, want [peninsular]", west.States)
	}
	if len(set.Zones[1].States) != 1 || set.Zones[1].States[0] != "east_malaysia" {
		t.Errorf("east states = %v, want [east_malaysia]", set.Zones[1].States)
	}
}

// The set ID is PRESERVED across a second save — that id is what a form's
// shipping block carries, so regenerating it would silently disconnect every
// live form from its rates.
func TestShippingStoreReplaceKeepsSetID(t *testing.T) {
	ctx := context.Background()
	pool := bundlePool(t)
	accountID := shippingFixture(t)
	store := NewShippingStore(pool)

	if err := store.Replace(ctx, accountID, sampleSets()); err != nil {
		t.Fatalf("first save: %v", err)
	}
	before, _ := store.Sets(ctx, []string{accountID})
	if len(before) != 1 {
		t.Fatalf("sets = %d, want 1", len(before))
	}
	edited := sampleSets()
	edited[0].ID = before[0].ID
	edited[0].Name = "Malaysia (edited)"
	edited[0].Zones = edited[0].Zones[:2] // drop the fallback zone
	edited[0].Zones[0].Rates[0].PriceSen = 900

	if err := store.Replace(ctx, accountID, edited); err != nil {
		t.Fatalf("second save: %v", err)
	}
	after, err := store.Sets(ctx, []string{accountID})
	if err != nil || len(after) != 1 {
		t.Fatalf("after = %d err=%v, want 1", len(after), err)
	}
	if after[0].ID != before[0].ID {
		t.Fatalf("set id changed across a save (%s -> %s) — every form reference would break",
			before[0].ID, after[0].ID)
	}
	if after[0].Name != "Malaysia (edited)" || len(after[0].Zones) != 2 {
		t.Errorf("second save did not apply: %+v", after[0])
	}
	if after[0].Zones[0].Rates[0].PriceSen != 900 {
		t.Errorf("rate price = %d, want 900", after[0].Zones[0].Rates[0].PriceSen)
	}
	// The zones/rates are REWRITTEN, not appended: a rate removed by the edit is
	// gone rather than duplicated.
	if len(after[0].Zones[0].Rates) != 2 {
		t.Errorf("west rates after the edit = %d, want 2 (rewritten, not appended)", len(after[0].Zones[0].Rates))
	}
}

// A set the payload drops is deleted — and deleting one a form still references
// is REFUSED, naming the form. This is what keeps a live form's block from
// resolving to nothing.
func TestShippingStoreDeleteIsRefusedWhileAFormUsesTheSet(t *testing.T) {
	ctx := context.Background()
	pool := bundlePool(t)
	accountID := shippingFixture(t)
	store := NewShippingStore(pool)
	forms := NewFormStore(pool)

	if err := store.Replace(ctx, accountID, sampleSets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	sets, _ := store.Sets(ctx, []string{accountID})
	setID := sets[0].ID

	// A published form whose shipping block points at the set.
	if err := forms.Create(ctx, &Form{
		AccountID: accountID, Slug: "it-shipstore-form-" + accountID[:8],
		Title: "Delivery form", Status: FormStatusPublished,
		Config: map[string]any{"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1", "source": "zones", "zone_set_id": setID},
		}},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}

	refs, err := store.FormsUsingSet(ctx, []string{accountID}, setID)
	if err != nil || len(refs) != 1 {
		t.Fatalf("FormsUsingSet = %d err=%v, want 1", len(refs), err)
	}
	if refs[0].Title != "Delivery form" {
		t.Errorf("ref = %+v, want the form that uses the set", refs[0])
	}
	if n, err := store.FormsUsingZones(ctx, []string{accountID}); err != nil || n != 1 {
		t.Errorf("FormsUsingZones = %d err=%v, want 1", n, err)
	}
	// Another account's view is empty: the reading is account-scoped.
	other := shippingFixture(t)
	if n, err := store.FormsUsingZones(ctx, []string{other}); err != nil || n != 0 {
		t.Errorf("a foreign account sees %d forms using zones, want 0", n)
	}

	// Dropping the set from the payload is a refusal that NAMES the form.
	err = store.Replace(ctx, accountID, []shipping.ZoneSet{{
		Name: "Replacement",
		Zones: []shipping.Zone{{Name: "Anywhere", States: []string{"peninsular"},
			Rates: []shipping.Rate{{PriceSen: 100}}}},
	}})
	inUse, ok := err.(*ShippingSetInUseError)
	if !ok {
		t.Fatalf("deleting an in-use set: err=%v, want *ShippingSetInUseError", err)
	}
	if len(inUse.Forms) != 1 || !strings.Contains(inUse.Error(), "Delivery form") {
		t.Errorf("refusal = %q, want it to name the form", inUse.Error())
	}
	// The transaction rolled back: the original set is still there.
	sets, _ = store.Sets(ctx, []string{accountID})
	if len(sets) != 1 || sets[0].ID != setID {
		t.Fatalf("a refused save changed the configuration: %+v", sets)
	}

	// With the form gone, the same save succeeds and really deletes the set.
	form, err := forms.PublishedByAccountSlug(ctx, "", "")
	if err != nil {
		_ = form
	}
	var formID string
	if err := pool.QueryRow(ctx, `SELECT id FROM forms WHERE account_id=$1`, accountID).Scan(&formID); err != nil {
		t.Fatalf("read form id: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM forms WHERE id=$1`, formID); err != nil {
		t.Fatalf("delete form: %v", err)
	}
	if err := store.Replace(ctx, accountID, []shipping.ZoneSet{{
		Name: "Replacement",
		Zones: []shipping.Zone{{Name: "Anywhere", States: []string{"peninsular"},
			Rates: []shipping.Rate{{PriceSen: 100}}}},
	}}); err != nil {
		t.Fatalf("save after the form is gone: %v", err)
	}
	sets, _ = store.Sets(ctx, []string{accountID})
	if len(sets) != 1 || sets[0].ID == setID {
		t.Fatalf("the dropped set was not deleted: %+v", sets)
	}
	// ...and its zones went with it (the FK cascade), so nothing is orphaned.
	var zones int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM shipping_zones WHERE set_id=$1`, setID).Scan(&zones); err != nil {
		t.Fatalf("count zones: %v", err)
	}
	if zones != 0 {
		t.Errorf("%d zone(s) survived the set they belong to", zones)
	}
}

// An EMPTY payload removes the account's last zone set (card t_417fbacb).
//
// THE DEFECT. ValidateShippingSets refused a zero-length list, so the one request
// that reaches "no managed rates" — a merchant deleting the only set in Form
// Settings → Shipping and pressing Save — answered 400, and the dashboard showed
// only its generic "Failed to save shipping rates" while the set came back on
// reload. The empty view the page renders for zero sets ("No zone set yet…") is
// the state this save has to be able to reach.
//
// The safety half is asserted in the same place: the empty save still cannot drop
// a set a form's shipping block points at, so "no sets" can only ever be a state
// nothing resolves against.
func TestShippingStoreReplaceRemovesTheLastSet(t *testing.T) {
	ctx := context.Background()
	pool := bundlePool(t)
	accountID := shippingFixture(t)
	store := NewShippingStore(pool)

	if err := store.Replace(ctx, accountID, sampleSets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if sets, err := store.Sets(ctx, []string{accountID}); err != nil || len(sets) != 1 {
		t.Fatalf("seeded %d set(s) err=%v, want 1", len(sets), err)
	}
	sets, _ := store.Sets(ctx, []string{accountID})
	setID := sets[0].ID

	// The dashboard's "Delete the only set, then Save" payload.
	if err := store.Replace(ctx, accountID, nil); err != nil {
		t.Fatalf("removing the LAST set was refused: %v — the reported defect: the "+
			"merchant has no route to a configuration with no managed rates", err)
	}
	if sets, err := store.Sets(ctx, []string{accountID}); err != nil || len(sets) != 0 {
		t.Fatalf("after the empty save the account has %d set(s) (err=%v), want 0", len(sets), err)
	}
	// Nothing is left behind: the set's zones and rates went with it.
	var setsLeft, zonesLeft, ratesLeft int
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM shipping_zone_sets WHERE account_id=$1),
		        (SELECT count(*) FROM shipping_zones WHERE account_id=$1),
		        (SELECT count(*) FROM shipping_rates WHERE account_id=$1)`,
		accountID).Scan(&setsLeft, &zonesLeft, &ratesLeft); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if setsLeft != 0 || zonesLeft != 0 || ratesLeft != 0 {
		t.Errorf("after the empty save: sets=%d zones=%d rates=%d, want 0/0/0",
			setsLeft, zonesLeft, ratesLeft)
	}

	// ...and the empty state is not a dead end: the merchant can configure rates
	// again (a NEW set id, because the old one is gone).
	if err := store.Replace(ctx, accountID, sampleSets()); err != nil {
		t.Fatalf("re-saving after an empty configuration: %v", err)
	}
	sets, _ = store.Sets(ctx, []string{accountID})
	if len(sets) != 1 || sets[0].ID == setID {
		t.Fatalf("re-save = %+v, want one new set", sets)
	}

	// Safety half: an empty save must NOT be able to break a live form's block.
	if err := NewFormStore(pool).Create(ctx, &Form{
		AccountID: accountID, Slug: "it-shipstore-last-" + accountID[:8],
		Title: "Delivery form", Status: FormStatusPublished,
		Config: map[string]any{"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1", "source": "zones", "zone_set_id": sets[0].ID},
		}},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}
	inUse, ok := store.Replace(ctx, accountID, nil).(*ShippingSetInUseError)
	if !ok {
		t.Fatalf("an empty save over a set a form still uses was not refused with " +
			"*ShippingSetInUseError — a form's shipping block would resolve to nothing")
	}
	if len(inUse.Forms) != 1 || !strings.Contains(inUse.Error(), "Delivery form") {
		t.Errorf("refusal = %q, want it to name the form", inUse.Error())
	}
	if sets, err := store.Sets(ctx, []string{accountID}); err != nil || len(sets) != 1 {
		t.Fatalf("a refused empty save changed the configuration: %+v (err=%v)", sets, err)
	}
}

// An unknown set id is a REFUSAL, never a silent new set: a stale dashboard must
// not mint a second configuration while live forms keep pointing at the old id.
func TestShippingStoreReplaceRefusesAnUnknownSetID(t *testing.T) {
	ctx := context.Background()
	pool := bundlePool(t)
	accountID := shippingFixture(t)
	store := NewShippingStore(pool)
	if err := store.Replace(ctx, accountID, sampleSets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	bad := sampleSets()
	bad[0].ID = "00000000-0000-0000-0000-000000000000"
	if err := store.Replace(ctx, accountID, bad); err != ErrShippingSetNotFound {
		t.Fatalf("unknown set id: err=%v, want ErrShippingSetNotFound", err)
	}
	sets, _ := store.Sets(ctx, []string{accountID})
	if len(sets) != 1 || sets[0].Name != "Malaysia" {
		t.Fatalf("a refused save changed the configuration: %+v", sets)
	}
}

// The read is scoped: a foreign account's set is not found by SetByID.
func TestShippingStoreSetByIDIsAccountScoped(t *testing.T) {
	ctx := context.Background()
	pool := bundlePool(t)
	mine := shippingFixture(t)
	theirs := shippingFixture(t)
	store := NewShippingStore(pool)
	if err := store.Replace(ctx, mine, sampleSets()); err != nil {
		t.Fatalf("save: %v", err)
	}
	sets, _ := store.Sets(ctx, []string{mine})
	setID := sets[0].ID

	if _, found, err := store.SetByID(ctx, []string{mine}, setID); err != nil || !found {
		t.Fatalf("own set not found: found=%v err=%v", found, err)
	}
	if foreign, found, err := store.SetByID(ctx, []string{theirs}, setID); err != nil || found {
		t.Fatalf("a foreign account resolved another account's set: %+v found=%v err=%v", foreign, found, err)
	}
	if all, err := store.Sets(ctx, []string{theirs}); err != nil || len(all) != 0 {
		t.Fatalf("a foreign account sees %d sets, want 0", len(all))
	}
}

// ValidateShippingSets refuses every shape that could not resolve at checkout.
func TestValidateShippingSets(t *testing.T) {
	ok := sampleSets()
	if err := ValidateShippingSets(ok); err != nil {
		t.Fatalf("a valid configuration was refused: %v", err)
	}
	rate := []shipping.Rate{{PriceSen: 800}}
	zone := func(name string) shipping.Zone {
		return shipping.Zone{Name: name, States: []string{"peninsular"}, Rates: rate}
	}
	cases := []struct {
		name string
		sets []shipping.ZoneSet
		want string
	}{
		{"unnamed set", []shipping.ZoneSet{{Zones: []shipping.Zone{zone("A")}}}, "a zone set needs a name"},
		{"duplicate set name", []shipping.ZoneSet{
			{Name: "Same", Zones: []shipping.Zone{zone("A")}},
			{Name: "same", Zones: []shipping.Zone{zone("B")}},
		}, "duplicate zone set name"},
		{"empty set", []shipping.ZoneSet{{Name: "Malaysia"}}, "has no zones"},
		{"unnamed zone", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{zone("")}}}, "unnamed zone"},
		{"zone with no rule", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{
			{Name: "A", Rates: rate},
		}}}, "at least one rule"},
		{"zone with no rate", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{
			{Name: "A", States: []string{"peninsular"}},
		}}}, "has no rates"},
		{"negative price", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{
			{Name: "A", States: []string{"peninsular"}, Rates: []shipping.Rate{{PriceSen: -1}}},
		}}}, "cannot be negative"},
		{"inverted band", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{
			{Name: "A", States: []string{"peninsular"}, Rates: []shipping.Rate{
				{PriceSen: 100, MinWeightG: 5000, MaxWeightG: 1000}}},
		}}}, "invalid weight range"},
		{"two fallbacks", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{
			{Name: "A", Fallback: true, Rates: rate},
			{Name: "B", Fallback: true, Rates: rate},
		}}}, "more than one default zone"},
		{"non-numeric postcode range", []shipping.ZoneSet{{Name: "Malaysia", Zones: []shipping.Zone{
			{Name: "A", PostcodeFrom: "abc", Rates: rate},
		}}}, "postcodes must be numbers"},
	}
	for _, c := range cases {
		err := ValidateShippingSets(c.sets)
		if err == nil {
			t.Errorf("%s: accepted, want a refusal naming %q", c.name, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: refusal = %q, want it to name %q", c.name, err.Error(), c.want)
		}
		// Every shape refusal carries the wire lead-in the REST edge keys the
		// `shipping.invalid` code on (errcode.go's errCodeByPrefix). Without it the
		// 400 ships with the class code and the dashboard can only say "Failed to
		// save shipping rates" (card t_417fbacb) — so the pairing is asserted here,
		// beside the messages it names, rather than trusted to a comment.
		if !strings.HasPrefix(err.Error(), ShippingInvalidLeadIn) {
			t.Errorf("%s: refusal = %q, want it to start with %q — the dashboard's "+
				"error code is keyed on that lead-in", c.name, err.Error(), ShippingInvalidLeadIn)
		}
	}

	// AN EMPTY LIST IS NOT A REFUSAL (card t_417fbacb). Zero sets is the state the
	// Shipping tab's own empty view describes, and the only way a merchant can
	// remove their LAST set. The per-set rules above still bite: an empty SET is
	// refused ("empty set" above), so this is not "accept anything".
	for _, empty := range [][]shipping.ZoneSet{nil, {}} {
		if err := ValidateShippingSets(empty); err != nil {
			t.Errorf("an empty set list was refused (%q) — a merchant cannot remove their "+
				"last zone set, which is the reported defect", err)
		}
	}
}
