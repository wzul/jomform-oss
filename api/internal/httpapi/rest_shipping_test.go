package httpapi

// REST surface for the managed shipping zones + rates (the dashboard's Shipping
// tab). Skipped without JOMFORM_TEST_DATABASE_URL.
//
// WHAT THESE PROVE:
//
//   - a save round-trips through the wire shape the dashboard sends (ids, zone
//     rules, rates) and the set id survives, so a form's block keeps resolving;
//   - an unlinked zone set id is a 404, an unresolvable configuration a 400, and
//     deleting a set a form still uses a 409 NAMING the form — the three
//     refusals the dashboard renders as messages;
//   - a readonly API key cannot write (issue #645's scope axis), so the Shipping
//     tab cannot be used to reprice a merchant's deliveries with a read key;
//   - the read is account-scoped: another account's configuration is not visible.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestShippingRESTSaveAndRead(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping shipping REST test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	keyB := bundleAPIKey(t, ctx, pool, accB)
	h := newBundleHandlers(pool)

	// Empty to start: the read answers a shaped object, not null.
	code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/shipping", "")
	if code != http.StatusOK {
		t.Fatalf("initial GET = %d, want 200 (body %s)", code, raw)
	}
	var empty struct {
		AccountID string            `json:"account_id"`
		Sets      []json.RawMessage `json:"sets"`
	}
	if err := json.Unmarshal(raw, &empty); err != nil {
		t.Fatalf("decode initial GET: %v (%s)", err, raw)
	}
	if empty.AccountID != accA || len(empty.Sets) != 0 {
		t.Fatalf("initial GET = %s, want account %s with no sets", raw, accA)
	}

	// Save a configuration (integer sen, a fallback zone, a free threshold).
	save := `{"sets":[{"name":"Malaysia","is_default":true,"zones":[` +
		`{"name":"West Malaysia","states":["peninsular"],"rates":[{"label":"Standard","price_sen":800,"free_over_sen":15000}]},` +
		`{"name":"East Malaysia","states":["east_malaysia"],"priority":1,"rates":[{"label":"Standard","price_sen":1500}]},` +
		`{"name":"Elsewhere","is_fallback":true,"priority":99,"rates":[{"label":"Standard","price_sen":5000}]}]}]}`
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping", save)
	if code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200 (body %s)", code, raw)
	}
	var saved struct {
		Sets []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsDefault bool   `json:"is_default"`
			Zones     []struct {
				Name       string `json:"name"`
				States     []string
				IsFallback bool `json:"is_fallback"`
				Rates      []struct {
					Label       string `json:"label"`
					PriceSen    int64  `json:"price_sen"`
					FreeOverSen int64  `json:"free_over_sen"`
				} `json:"rates"`
			} `json:"zones"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("decode PUT: %v (%s)", err, raw)
	}
	if len(saved.Sets) != 1 || saved.Sets[0].ID == "" || !saved.Sets[0].IsDefault {
		t.Fatalf("PUT response = %s, want one set with an id", raw)
	}
	setID := saved.Sets[0].ID
	if len(saved.Sets[0].Zones) != 3 {
		t.Fatalf("zones = %d, want 3", len(saved.Sets[0].Zones))
	}
	west := saved.Sets[0].Zones[0]
	if west.Name != "West Malaysia" || west.Rates[0].PriceSen != 800 || west.Rates[0].FreeOverSen != 15000 {
		t.Errorf("west zone = %+v, want Standard/800/free over 15000", west)
	}
	if !saved.Sets[0].Zones[2].IsFallback {
		t.Errorf("the fallback flag was lost on the wire: %+v", saved.Sets[0].Zones[2])
	}

	// A second save with the SAME id edits in place — the id a form references
	// must survive.
	save2 := strings.Replace(save, `{"name":"Malaysia"`, `{"id":"`+setID+`","name":"Malaysia"`, 1)
	save2 = strings.Replace(save2, `"price_sen":800`, `"price_sen":900`, 1)
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping", save2)
	if code != http.StatusOK {
		t.Fatalf("second PUT = %d, want 200 (body %s)", code, raw)
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("decode second PUT: %v", err)
	}
	if saved.Sets[0].ID != setID {
		t.Fatalf("set id changed across a save (%s -> %s)", setID, saved.Sets[0].ID)
	}
	if saved.Sets[0].Zones[0].Rates[0].PriceSen != 900 {
		t.Errorf("edited price = %d, want 900", saved.Sets[0].Zones[0].Rates[0].PriceSen)
	}

	// A foreign account still sees nothing.
	code, raw = tplReq(t, h, keyB, http.MethodGet, "/v1/rest/shipping", "")
	if code != http.StatusOK {
		t.Fatalf("second account GET = %d, want 200", code)
	}
	if err := json.Unmarshal(raw, &empty); err != nil {
		t.Fatalf("decode foreign GET: %v", err)
	}
	if empty.AccountID != accB || len(empty.Sets) != 0 {
		t.Fatalf("a foreign account sees another account's shipping config: %s", raw)
	}
}

// The three refusals the dashboard renders as messages.
func TestShippingRESTRefusals(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping shipping REST refusal test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	h := newBundleHandlers(pool)

	// An unknown set id: 404, never a silent new set.
	code, raw := tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"id":"00000000-0000-0000-0000-000000000000","name":"Malaysia","zones":[
			{"name":"West","states":["peninsular"],"rates":[{"price_sen":800}]}]}]}`)
	if code != http.StatusNotFound {
		t.Fatalf("unknown set id = %d, want 404 (body %s)", code, raw)
	}

	// A zone with no rate cannot resolve, so it is refused at save time with a
	// message naming the zone.
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"name":"Malaysia","zones":[{"name":"West","states":["peninsular"],"rates":[]}]}]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("zone with no rate = %d, want 400 (body %s)", code, raw)
	}
	if !strings.Contains(string(raw), "West") || !strings.Contains(string(raw), "kadar") {
		t.Errorf("refusal should name the zone and the missing rate, got %s", raw)
	}

	// A negative price is refused rather than stored.
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"name":"Malaysia","zones":[{"name":"West","states":["peninsular"],"rates":[{"price_sen":-5}]}]}]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("negative price = %d, want 400 (body %s)", code, raw)
	}

	// Store a valid configuration, point a form at it, then try to drop the set:
	// 409, naming the form.
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"name":"Malaysia","zones":[{"name":"West","states":["peninsular"],"rates":[{"price_sen":800}]}]}]}`)
	if code != http.StatusOK {
		t.Fatalf("seed PUT = %d, want 200 (body %s)", code, raw)
	}
	var listed struct {
		Sets []struct {
			ID string `json:"id"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed.Sets) != 1 {
		t.Fatalf("decode seed PUT: %v (%s)", err, raw)
	}
	setID := listed.Sets[0].ID

	forms := dbstore.NewFormStore(pool)
	if err := forms.Create(ctx, &dbstore.Form{
		AccountID: accA, Slug: "rest-ship-form-" + accA[:8], Title: "Delivery form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{
			map[string]any{"id": "ship1", "type": "shipping", "value": "Postage",
				"product_id": "ship1", "source": "zones", "zone_set_id": setID},
		}},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// The read reports the feature as in use.
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/shipping", "")
	if code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", code)
	}
	var withUse struct {
		FormsUsingZones int `json:"forms_using_zones"`
	}
	if err := json.Unmarshal(raw, &withUse); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if withUse.FormsUsingZones != 1 {
		t.Errorf("forms_using_zones = %d, want 1", withUse.FormsUsingZones)
	}

	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"name":"Replacement","zones":[{"name":"Anywhere","states":["peninsular"],"rates":[{"price_sen":100}]}]}]}`)
	if code != http.StatusConflict {
		t.Fatalf("dropping an in-use set = %d, want 409 (body %s)", code, raw)
	}
	if !strings.Contains(string(raw), "Delivery form") {
		t.Errorf("the 409 should name the form that uses the set, got %s", raw)
	}
	// The refusal must ALSO carry the machine code the dashboard translates
	// (issue #6177): the prose names the set and the form, but a page cannot
	// render Malay prose, and without this code the merchant only ever saw the
	// generic "Failed to save shipping rates" for a refusal whose remedy is
	// "re-point that form first". The code is keyed on the dbstore lead-in, so
	// this pins the pairing, not a copy of the sentence.
	var conflict struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &conflict); err != nil {
		t.Fatalf("decode 409 body: %v (%s)", err, raw)
	}
	if conflict.Code != CodeShippingSetInUse {
		t.Errorf("409 code = %q, want %q — the dashboard cannot translate the "+
			"refusal without it", conflict.Code, CodeShippingSetInUse)
	}
}

// A readonly API key cannot write the shipping configuration (issue #645's scope
// axis, applied to the new surface).
func TestShippingRESTReadonlyKeyCannotSave(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping readonly-key test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)

	// A readonly key: the same write the dashboard's Shipping tab performs.
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accA, hash, "it-readonly-ship", []string{"read"}); err != nil {
		t.Fatalf("store readonly key: %v", err)
	}
	h := newBundleHandlers(pool)

	code, body := tplReq(t, h, raw, http.MethodPut, "/v1/rest/shipping",
		`{"sets":[{"name":"Malaysia","zones":[{"name":"West","states":["peninsular"],"rates":[{"price_sen":800}]}]}]}`)
	if code != http.StatusForbidden {
		t.Fatalf("readonly key PUT = %d, want 403 (body %s)", code, body)
	}
	// ...and the configuration really is untouched.
	sets, err := dbstore.NewShippingStore(pool).Sets(ctx, []string{accA})
	if err != nil || len(sets) != 0 {
		t.Fatalf("a refused readonly write created %d set(s) (err %v)", len(sets), err)
	}
}
