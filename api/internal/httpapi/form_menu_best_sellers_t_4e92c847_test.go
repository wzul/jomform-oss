package httpapi

// Regression: the per-form menu's best sellers survive a REST PUT in the LIST
// shape (issue #485 `best_seller_ids`), and a Save that round-trips that list
// does not erase it.
//
// WHY. The Form Settings → "Restaurant menu" tab used to read and write ONLY
// the legacy single `best_seller` field (web/src/app/form-settings/page.tsx).
// A menu configured through MCP/REST with `best_seller_ids` therefore rendered
// as "No best-seller selected", and its Save sent `best_seller:"",
// best_seller_ids:null` — which `dbstore.WithMenu` stores verbatim, because it
// REPLACES the whole menu object (`out[MenuConfigKey] = menu`) rather than
// merging into the stored one. The tab now hydrates and saves the list
// (web/src/lib/formMenu.ts); this test pins the SERVER half of that round trip
// so the shape the browser sends is the shape that comes back.
//
// WHAT THIS PINS:
//  1. PUT {enabled, categories, best_seller_ids} stores the ids on
//     forms.config.menu, with no legacy `best_seller` key written.
//  2. GET /menu returns the same list, in the same order — the read the tab and
//     the MCP `get_form_menu` tool both depend on.
//  3. A SECOND PUT carrying the same list (one dashboard Save with an untouched
//     selection) still stores all three ids: the erase is what this guards.
//  4. The legacy single-id PUT still works (control: the fix does not drop the
//     documented fallback vocabulary).
//
// Integration against a real Postgres; skipped without
// JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestFormMenuBestSellerIDsSurviveSave(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping form menu best-seller test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)

	ids := []string{
		"76109c58-0000-4000-8000-000000000001",
		"ba4d4656-0000-4000-8000-000000000002",
		"71aa6068-0000-4000-8000-000000000003",
	}
	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: acc, Slug: "menu-bs-" + acc[:8], Title: "Menu Form",
		Config: map[string]any{"mode": "payment", "blocks": []any{
			map[string]any{"id": "b1", "type": "product", "product_id": ids[0]},
			map[string]any{"id": "b2", "type": "product", "product_id": ids[1]},
			map[string]any{"id": "b3", "type": "product", "product_id": ids[2]},
		}},
		Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	put := func(body string) {
		t.Helper()
		code, raw := tplReq(t, h, key, http.MethodPut, "/v1/rest/forms/"+f.ID+"/menu", body)
		if code != http.StatusOK {
			t.Fatalf("PUT menu = %d, want 200 (body=%s)", code, raw)
		}
	}
	get := func() dbstore.MenuPage {
		t.Helper()
		code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/forms/"+f.ID+"/menu", "")
		if code != http.StatusOK {
			t.Fatalf("GET menu = %d, want 200 (body=%s)", code, raw)
		}
		var m dbstore.MenuPage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode menu %s: %v", raw, err)
		}
		return m
	}
	// storedMenu reads the row itself, so the assertion is on what the DATABASE
	// holds rather than on the handler's own echo.
	storedMenu := func() map[string]any {
		t.Helper()
		var cfg map[string]any
		if err := pool.QueryRow(ctx, `SELECT config FROM forms WHERE id=$1`, f.ID).Scan(&cfg); err != nil {
			t.Fatalf("read stored config: %v", err)
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal stored config: %v", err)
		}
		var shape struct {
			Menu map[string]any `json:"menu"`
		}
		if err := json.Unmarshal(raw, &shape); err != nil {
			t.Fatalf("decode stored config: %v", err)
		}
		if shape.Menu == nil {
			t.Fatalf("stored config has no menu key: %s", raw)
		}
		return shape.Menu
	}
	assertIDs := func(where string, m dbstore.MenuPage, stored map[string]any) {
		t.Helper()
		if len(m.BestSellerIDs) != len(ids) {
			t.Fatalf("%s: GET best_seller_ids = %v, want %v", where, m.BestSellerIDs, ids)
		}
		for i, want := range ids {
			if m.BestSellerIDs[i] != want {
				t.Fatalf("%s: GET best_seller_ids = %v, want %v (order matters)", where, m.BestSellerIDs, ids)
			}
		}
		if m.BestSeller != "" {
			t.Errorf("%s: GET best_seller = %q, want empty (the list shape was written)", where, m.BestSeller)
		}
		if _, ok := stored["best_seller"]; ok {
			t.Errorf("%s: stored menu carries a legacy best_seller key: %v", where, stored)
		}
		storedIDs, ok := stored["best_seller_ids"].([]any)
		if !ok || len(storedIDs) != len(ids) {
			t.Fatalf("%s: stored menu best_seller_ids = %v, want %v", where, stored["best_seller_ids"], ids)
		}
		for i, want := range ids {
			if got, _ := storedIDs[i].(string); got != want {
				t.Fatalf("%s: stored menu best_seller_ids = %v, want %v", where, storedIDs, ids)
			}
		}
	}

	listBody := `{"enabled":true,"categories":[{"name":"Makanan","product_ids":["` + ids[0] + `"]}],` +
		`"best_seller_ids":["` + ids[0] + `","` + ids[1] + `","` + ids[2] + `"]}`

	// 1 + 2: the list shape is stored and read back.
	put(listBody)
	assertIDs("first save", get(), storedMenu())

	// 3: the dashboard's own round trip — load, then save the SAME list.
	m := get()
	again, err := json.Marshal(map[string]any{
		"enabled":         m.Enabled,
		"categories":      m.Categories,
		"best_seller_ids": m.BestSellerIDs,
		"order_form_slug": m.OrderFormSlug,
	})
	if err != nil {
		t.Fatalf("marshal round-trip body: %v", err)
	}
	put(string(again))
	assertIDs("round trip", get(), storedMenu())

	// 4: control — the legacy single id still works on its own.
	legacy := &dbstore.Form{
		AccountID: acc, Slug: "menu-bs-legacy-" + acc[:8], Title: "Legacy Menu Form",
		Config: map[string]any{"mode": "payment", "blocks": []any{
			map[string]any{"id": "b1", "type": "product", "product_id": ids[0]},
		}},
		Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, legacy); err != nil {
		t.Fatalf("create legacy form: %v", err)
	}
	code, raw := tplReq(t, h, key, http.MethodPut, "/v1/rest/forms/"+legacy.ID+"/menu",
		`{"enabled":true,"categories":[],"best_seller":"`+ids[0]+`"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT legacy menu = %d, want 200 (body=%s)", code, raw)
	}
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/forms/"+legacy.ID+"/menu", "")
	if code != http.StatusOK {
		t.Fatalf("GET legacy menu = %d, want 200 (body=%s)", code, raw)
	}
	var legacyMenu dbstore.MenuPage
	if err := json.Unmarshal(raw, &legacyMenu); err != nil {
		t.Fatalf("decode legacy menu %s: %v", raw, err)
	}
	if legacyMenu.BestSeller != ids[0] || len(legacyMenu.BestSellerIDs) != 0 {
		t.Errorf("legacy menu = best_seller %q / ids %v, want %q / none",
			legacyMenu.BestSeller, legacyMenu.BestSellerIDs, ids[0])
	}
}
