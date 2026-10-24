package httpapi

// Card t_3d11f98d — the SAVE-side half of the option/shipping pricing fix.
//
// The charge fix lives in lookupProduct (a pricing block is charged the price
// of the CHOICE the buyer picked). This file guards the write paths: a config
// that cannot say what a pick costs must not reach storage, and the shape the
// builder really produces must still save.
//
// The rule itself is unit-tested in formfields (pricing_block_test.go); these
// are the call-site guards, because a correct rule that nobody calls protects
// nothing. Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// pricingFormBody builds a form create/update body carrying one option block.
// When priced is false the block carries no price anywhere — the SHAPE
// generate_form used to produce.
func pricingFormBody(t *testing.T, slug string, priced bool) string {
	t.Helper()
	choice := map[string]any{"label": "Kad", "value": "card"}
	if priced {
		choice["price_sen"] = 500
	}
	body := map[string]any{
		"title": "Pricing guard",
		"slug":  slug,
		"config": map[string]any{
			"blocks": []any{
				map[string]any{"id": "b-name", "type": "name", "value": "Nama"},
				map[string]any{"id": "b-opt", "type": "option", "value": "Tambahan",
					"product_id": "b-opt",
					"choices":    []any{choice},
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal form body: %v", err)
	}
	return string(raw)
}

// TestCreateFormRefusesAnUnpricedPricingBlock: POST /v1/rest/forms is the
// builder's own save route, so this is the path a merchant (and the AI
// generator) actually uses.
func TestCreateFormRefusesAnUnpricedPricingBlock(t *testing.T) {
	fx := newRenameFixture(t, "it-pricing-"+randHex(4))
	slug := "it-pricing-" + randHex(4)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		pricingFormBody(t, slug, false))
	if code != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/forms with an unpriced option block = %d, want 400 (body %s).\n"+
			"Storing it is the carded defect: the buyer is shown an add-on with no price and the sale records 0 sen.",
			code, raw)
	}
	body := string(raw)
	if !strings.Contains(body, "option") || !strings.Contains(body, "harga") {
		t.Errorf("400 body = %s, want it to name the option block and the missing price", body)
	}
	// A refused write leaves nothing behind.
	if n := liveFormCount(t, fx, slug); n != 0 {
		t.Fatalf("%d live forms on %q after a refused create, want 0", n, slug)
	}

	// The BUILDER's shape — price on the choice, none on the block — saves.
	okSlug := "it-pricing-ok-" + randHex(4)
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		pricingFormBody(t, okSlug, true))
	if code != http.StatusCreated {
		t.Fatalf("POST with a builder-shaped priced option block = %d, want 201 (body %s).\n"+
			"The guard must refuse an unpriced block, not the shape the builder writes.",
			code, raw)
	}
	if n := liveFormCount(t, fx, okSlug); n != 1 {
		t.Fatalf("%d live forms on %q after an accepted create, want 1", n, okSlug)
	}
}

// TestUpdateFormRefusesAnUnpricedPricingBlock is the second write path: a PATCH
// that rewrites the config must not be a way around the create-time refusal.
func TestUpdateFormRefusesAnUnpricedPricingBlock(t *testing.T) {
	fx := newRenameFixture(t, "it-pricing-upd-"+randHex(4))
	slug := "it-pricing-upd-" + randHex(4)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		pricingFormBody(t, slug, true))
	if code != http.StatusCreated {
		t.Fatalf("seed create = %d, want 201 (body %s)", code, raw)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	formID, _ := created["id"].(string)
	if formID == "" {
		t.Fatalf("create response carries no id: %s", raw)
	}

	// The PATCH body is the create body minus the slug (the update route keeps
	// the stored slug when it is omitted).
	var patch map[string]any
	if err := json.Unmarshal([]byte(pricingFormBody(t, slug, false)), &patch); err != nil {
		t.Fatalf("decode patch body: %v", err)
	}
	delete(patch, "slug")
	patchRaw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+formID, string(patchRaw))
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH introducing an unpriced option block = %d, want 400 (body %s)", code, raw)
	}
	// The stored config still carries the priced choice.
	cfg := formConfig(t, fx, slug)
	blocks, _ := cfg["blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("stored blocks = %v, want the seed's 2 blocks", cfg["blocks"])
	}
	opt, _ := blocks[1].(map[string]any)
	choices, _ := opt["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("stored option choices = %v, want 1", opt["choices"])
	}
	first, _ := choices[0].(map[string]any)
	if got, ok := first["price_sen"].(float64); !ok || got != 500 {
		t.Fatalf("stored choice price_sen = %v, want 500 — a refused PATCH must not have written", first["price_sen"])
	}
}
