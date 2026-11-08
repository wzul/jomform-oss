package httpapi

// Onboarding-gap guard, at the HTTP surface the dashboard actually calls: a
// PAYMENT form with nothing to sell cannot be published silently, and a RESPONSE
// form with nothing to sell still publishes without a murmur.
//
// THE GAP (drop-off, not a crash). Three outside signups built a form, saved it
// as a draft and never published. Two of the three drafts were payment-mode
// forms with NO `products` key in config — had they published, the form could
// never record a sale and nothing at the moment that mattered would have said
// so. The publish handler answered 200 either way.
//
// This is the integration proof: it goes through the real mux, the real API-key
// auth and a real Postgres, so a unit-level test of the rule alone cannot hide a
// handler that stopped calling it. Integration; skipped without
// JOMFORM_TEST_DATABASE_URL (scripts/preflight.sh fails loudly if it is unset).

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// pprPaymentDraft is the exact shape the two production drafts had: payment
// mode (the default) and ordinary field blocks, with no `products` key at all.
func pprPaymentDraft() map[string]any {
	return map[string]any{
		"mode": "payment",
		"blocks": []any{
			map[string]any{"id": "b1", "type": "email", "value": "Email"},
			map[string]any{"id": "b2", "type": "name", "value": "Name"},
			map[string]any{"id": "b3", "type": "phone", "value": "Phone"},
		},
	}
}

func TestPublishRefusesAPaymentFormWithNoProductsUntilAcknowledged(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping publish-readiness integration test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	// The production shape for anything that publishes: ask-first onboarding
	// makes the workspace BEFORE the form (t_6c0badbe). Since t_fadfe9f7 a form
	// cannot enter a live status on an account with no live workspace, so the
	// ack rule this test is about must be measured on a linked account.
	bundleLinkWorkspace(t, ctx, pool, acc)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)

	// 1. The production shape: payment mode, no products.
	paid := &dbstore.Form{
		AccountID: acc, Slug: "ppr-paid-" + acc[:8], Title: "Contest Entry",
		Config: pprPaymentDraft(), Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, paid); err != nil {
		t.Fatalf("create payment form: %v", err)
	}

	// Publishing it bare is refused, and the refusal is the machine-readable one
	// the dashboard matches on — not a generic 400.
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+paid.ID+"/publish", "")
	if code != http.StatusConflict {
		t.Fatalf("publish payment form with no products = %d, want 409 (body=%s)", code, raw)
	}
	var refused map[string]any
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, raw)
	}
	if refused["code"] != dbstore.PublishWarningNoProducts {
		t.Errorf("refusal code = %v, want %q", refused["code"], dbstore.PublishWarningNoProducts)
	}
	if refused["requires_ack"] != true {
		t.Errorf("refusal requires_ack = %v, want true", refused["requires_ack"])
	}
	// And nothing changed: the form is still a draft.
	after, err := forms.GetByID(ctx, acc, paid.ID)
	if err != nil {
		t.Fatalf("re-read form: %v", err)
	}
	if after.Status != dbstore.FormStatusDraft {
		t.Errorf("status after refusal = %q, want draft (a refused publish must not change state)", after.Status)
	}

	// 2. The same call acknowledged succeeds.
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+paid.ID+"/publish",
		`{"acknowledge":true}`)
	if code != http.StatusOK {
		t.Fatalf("acknowledged publish = %d, want 200 (body=%s)", code, raw)
	}
	after, err = forms.GetByID(ctx, acc, paid.ID)
	if err != nil {
		t.Fatalf("re-read form: %v", err)
	}
	if after.Status != dbstore.FormStatusPublished {
		t.Errorf("status after acknowledged publish = %q, want published", after.Status)
	}

	// 3. A second publish of a form that is ALREADY live is never refused — a
	// guard that asked again would nag on every unrelated save.
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+paid.ID+"/publish", "")
	if code != http.StatusOK {
		t.Errorf("re-publishing an already-live form = %d, want 200 (body=%s)", code, raw)
	}

	// 4. A payment form that HAS something to sell publishes bare.
	selling := &dbstore.Form{
		AccountID: acc, Slug: "ppr-selling-" + acc[:8], Title: "Tee Order",
		Config: map[string]any{
			"mode": "payment",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "email", "value": "Email"},
				map[string]any{"id": "b2", "type": "product", "product_id": "3f1e5c58-1111-2222-3333-444455556666"},
			},
		},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, selling); err != nil {
		t.Fatalf("create selling form: %v", err)
	}
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+selling.ID+"/publish", ""); code != http.StatusOK {
		t.Errorf("publishing a payment form WITH a product = %d, want 200 (body=%s)", code, raw)
	}
}

func TestResponseFormWithNoProductsPublishesWithoutAWarning(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping publish-readiness integration test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	// A publish needs a live workspace (t_fadfe9f7); link one so this test
	// measures the ack rule, not that guard.
	bundleLinkWorkspace(t, ctx, pool, acc)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)

	// The over-blocking guard, at the HTTP surface: an RSVP-style response form
	// has no products BY DESIGN. Refusing it would break the one legitimate
	// reason to build a product-free form.
	resp := &dbstore.Form{
		AccountID: acc, Slug: "ppr-rsvp-" + acc[:8], Title: "RSVP",
		Config: map[string]any{
			"mode":   "response",
			"blocks": []any{map[string]any{"id": "b1", "type": "email", "value": "Email"}},
		},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, resp); err != nil {
		t.Fatalf("create response form: %v", err)
	}
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+resp.ID+"/publish", "")
	if code != http.StatusOK {
		t.Fatalf("publishing a response form with no products = %d, want 200 — a product-free "+
			"response form is legitimate and must not be blocked (body=%s)", code, raw)
	}
	after, err := forms.GetByID(ctx, acc, resp.ID)
	if err != nil {
		t.Fatalf("re-read form: %v", err)
	}
	if after.Status != dbstore.FormStatusPublished {
		t.Errorf("status = %q, want published", after.Status)
	}
}

func TestStatusRouteAppliesTheSameRuleAsThePublishRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping publish-readiness integration test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	// The status route is a publish door too: since t_fadfe9f7 it refuses
	// entering a live status on an account with no live workspace, so link one
	// before measuring the ack rule.
	bundleLinkWorkspace(t, ctx, pool, acc)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)

	// The dashboard's status control reaches THIS route, so a guard that only
	// lived on /publish would be trivially sidestepped.
	f := &dbstore.Form{
		AccountID: acc, Slug: "ppr-status-" + acc[:8], Title: "Donation",
		Config: map[string]any{
			"mode":   "payment",
			"blocks": []any{map[string]any{"id": "b1", "type": "text", "value": "Notes"}},
		},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/status",
		`{"status":"published"}`)
	if code != http.StatusConflict {
		t.Fatalf("status->published on a payment form with no products = %d, want 409 (body=%s)", code, raw)
	}
	var refused map[string]any
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if refused["code"] != dbstore.PublishWarningNoProducts {
		t.Errorf("code = %v, want %q", refused["code"], dbstore.PublishWarningNoProducts)
	}
	still, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if still.Status != dbstore.FormStatusDraft {
		t.Errorf("status after refusal = %q, want draft", still.Status)
	}

	// Acknowledged, the same call goes through.
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/status",
		`{"status":"published","acknowledge":true}`)
	if code != http.StatusOK {
		t.Fatalf("acknowledged status change = %d, want 200 (body=%s)", code, raw)
	}

	// Moving to DRAFT is never gated (draft is not live), and neither is
	// re-sending the status the form already has.
	draft := &dbstore.Form{
		AccountID: acc, Slug: "ppr-draft-" + acc[:8], Title: "Empty Payment",
		Config: pprPaymentDraft(), Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, draft); err != nil {
		t.Fatalf("create draft form: %v", err)
	}
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+draft.ID+"/status",
		`{"status":"draft"}`); code != http.StatusOK {
		t.Errorf("status->draft = %d, want 200 (body=%s)", code, raw)
	}
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/status",
		`{"status":"published"}`); code != http.StatusOK {
		t.Errorf("re-sending the status an already-live form has = %d, want 200 (body=%s)", code, raw)
	}
}

func TestBulkPublishWithholdsPaymentFormsWithNoProducts(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping publish-readiness integration test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	// A bulk publish needs a live workspace too (t_fadfe9f7); link one so this
	// test measures the ack withholding, not that guard.
	bundleLinkWorkspace(t, ctx, pool, acc)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)

	// A bulk publish must not be a way around the guard: one form with something
	// to sell, one without.
	selling := &dbstore.Form{
		AccountID: acc, Slug: "ppr-bulk-ok-" + acc[:8], Title: "Tee Order",
		Config: map[string]any{
			"mode":   "payment",
			"blocks": []any{map[string]any{"id": "b2", "type": "product", "product_id": "3f1e5c58-1111-2222-3333-444455556666"}},
		},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	empty := &dbstore.Form{
		AccountID: acc, Slug: "ppr-bulk-empty-" + acc[:8], Title: "No Products",
		Config: pprPaymentDraft(), Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	for _, f := range []*dbstore.Form{selling, empty} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create %s: %v", f.Slug, err)
		}
	}

	body := `{"action":"publish","ids":["` + selling.ID + `","` + empty.ID + `"]}`
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/bulk", body)
	if code != http.StatusConflict {
		t.Fatalf("bulk publish holding back a product-free payment form = %d, want 409 (body=%s)", code, raw)
	}
	var refused map[string]any
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	ids, _ := refused["needs_products"].([]any)
	if len(ids) != 1 || ids[0] != empty.ID {
		t.Errorf("needs_products = %v, want exactly [%s]", refused["needs_products"], empty.ID)
	}
	// Nothing was published by the refused batch — including the form that WAS
	// publishable. A partial write the caller never learned about would be worse
	// than an all-or-nothing refusal.
	for _, f := range []*dbstore.Form{selling, empty} {
		got, err := forms.GetByID(ctx, acc, f.ID)
		if err != nil {
			t.Fatalf("re-read %s: %v", f.Slug, err)
		}
		if got.Status != dbstore.FormStatusDraft {
			t.Errorf("%s status = %q, want draft (a refused bulk publish must change nothing)", f.Slug, got.Status)
		}
	}

	// Acknowledged, the whole batch publishes and the product-free id is still
	// reported so the merchant knows which live form cannot take money.
	body = `{"action":"publish","acknowledge":true,"ids":["` + selling.ID + `","` + empty.ID + `"]}`
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/bulk", body)
	if code != http.StatusOK {
		t.Fatalf("acknowledged bulk publish = %d, want 200 (body=%s)", code, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if out["updated"] != float64(2) {
		t.Errorf("updated = %v, want 2 (body=%s)", out["updated"], raw)
	}
	reported, _ := out["needs_products"].([]any)
	if len(reported) != 1 || reported[0] != empty.ID {
		t.Errorf("needs_products after ack = %v, want exactly [%s]", out["needs_products"], empty.ID)
	}
	for _, f := range []*dbstore.Form{selling, empty} {
		got, err := forms.GetByID(ctx, acc, f.ID)
		if err != nil {
			t.Fatalf("re-read %s: %v", f.Slug, err)
		}
		if got.Status != dbstore.FormStatusPublished {
			t.Errorf("%s status = %q, want published", f.Slug, got.Status)
		}
	}
}
