package httpapi

// Card t_e2024698 — a buyer whose CHIP purchase creation fails must READ the
// failure, and the merchant must not be left with a phantom sale.
//
// Measured on prod (v1.21.260, 2026-10-20) while a published payment form was
// submitted with a stored CHIP key CHIP rejects:
//
//	ERROR chip_create_for_sale err="chip create: chip: http 401 … Incorrect secret_key" sale=JF-1ce524460b4f
//	INFO  http_request method=POST path=/new-form/order status=502 duration_ms=331
//
// The api answered 502 and the buyer never saw that body: Cloudflare substitutes
// its own error page for an origin 5xx (the browser's fetch resolved with
// `<title>jomform.com | 502: Bad gateway</title>`; a curl of the same route got
// `error code: 502`, content-length 16), while a 4xx from the SAME route arrived
// intact. On the block renderer, whose handler had no `.catch`, `r.json()`
// rejected on that HTML page and the buyer's click did NOTHING at all. The sale
// was also left `pending` with `chip_purchase_id IS NULL`, so every provider
// failure added a phantom row to the merchant's Sales list.
//
// Two halves are guarded here:
//  1. TestChipPurchaseFailureAnswersTheBuyer… drives the REAL /order route
//     against a CHIP stub that answers the prod 401 verbatim, and asserts what
//     the BUYER receives (status + body) and what the sale becomes — not the api
//     log.
//  2. TestEveryPublicOrderSurfaceRendersThePaymentFailureInApp drives the four
//     real page renderers and asserts each one paints the failure in-app, with
//     the copy in the page's own language, and never leaves an unreadable answer
//     silent.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// chipRejectingServer answers CHIP purchase creation with the exact envelope
// CHIP returned on prod for the account whose key is rejected. Every method is
// answered the same way: the buyer path only ever calls CreatePurchase, and a
// stub that 200s anything else would hide a wrong endpoint.
func chipRejectingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"__all__": []any{map[string]any{
				"message": "Incorrect secret_key",
				"code":    "authentication_failed",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// payableFormFixture seeds the account/product fixture and a PUBLISHED payment
// form (no menu), then wires h.Pay at the CHIP stub. withCreds=false leaves the
// account without CHIP credentials, which is the other way this handler fails
// (payments.ErrNotConfigured).
func payableFormFixture(t *testing.T, withCreds bool) (h *PublicHandlers, host, formSlug, prodID string) {
	t.Helper()
	h, workspaceSlug, accountID, prod, _ := menuOrderFixture(t)
	ctx := context.Background()

	formSlug = "it-t_e2024698-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Donation", Status: "published",
		Config: map[string]any{
			// The legacy `{description, products[]}` shape: the product row is
			// still the price authority (#461), the snapshot is the fallback.
			"description": "Support us",
			"products": []any{map[string]any{
				"id": prod, "name": "Donation", "price_sen": 300, "active": true,
			}},
		},
	}
	if err := h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if withCreds {
		sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_rejected_key"))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if err := h.Accounts.SetChipCredentials(ctx, accountID, "br_rejected", sealed, "test", "", nil, true); err != nil {
			t.Fatalf("set chip creds: %v", err)
		}
	}
	srv := chipRejectingServer(t)
	pay := payments.New(chip.New(srv.URL), h.Accounts, h.Sales,
		dbstore.NewSubscriptionStore(bundleTestPool(t)), key, "http://localhost:8080")
	pay.Workspaces = h.Workspaces
	h.Pay = pay

	return h, workspaceSlug + ".jomform.com", formSlug, prod
}

// TestChipPurchaseFailureAnswersTheBuyerAndFailsTheSale is the card's acceptance
// criterion: submit the public form for an account whose CHIP secret is
// rejected, and read what the BROWSER gets.
func TestChipPurchaseFailureAnswersTheBuyerAndFailsTheSale(t *testing.T) {
	cases := []struct {
		name      string
		withCreds bool
		// why describes the provider-side failure this case exercises.
		why string
	}{
		{"chip rejects the stored secret", true,
			"the prod case: the purchase call answers 401 Incorrect secret_key"},
		{"account has no CHIP credentials", false,
			"payments.ErrNotConfigured — a merchant who never configured CHIP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, host, formSlug, prodID := payableFormFixture(t, tc.withCreds)
			ctx := context.Background()

			body := `{"client":{"email":"buyer@example.test"},` +
				`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
				`"payment_method":"chip"}`
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", strings.NewReader(body))
			req.Host = host
			req.RemoteAddr = uniqueTestIP() + ":1234"
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			// THE DEFECT, half one: a 5xx never reaches the buyer through the
			// edge (Cloudflare substitutes its own body for an origin 5xx), so
			// the answer must be one the edge passes through — and it must be
			// JSON, or the page cannot read the state out of it.
			if rec.Code != http.StatusOK {
				t.Fatalf("buyer answer = %d, want 200 (%s). A 5xx body is replaced by the edge, "+
					"so the buyer would read the CDN's error page instead of this JSON; body %s",
					rec.Code, tc.why, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("content-type = %q, want JSON — the page calls r.json() on this answer", ct)
			}

			var resp struct {
				Status      string `json:"status"`
				RefCode     string `json:"ref_code"`
				Error       string `json:"error"`
				CheckoutURL string `json:"checkout_url"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode buyer answer: %v (body %s)", err, rec.Body.String())
			}
			// The state the page branches on. Without it the page cannot tell a
			// failed payment from a checkout, and the buyer is left on a dead
			// button.
			if resp.Status != "payment_error" {
				t.Errorf("status = %q, want %q (body %s)", resp.Status, "payment_error", rec.Body.String())
			}
			if resp.CheckoutURL != "" {
				t.Errorf("checkout_url = %q, want empty on a failed purchase", resp.CheckoutURL)
			}
			// The reference is what the buyer quotes to the merchant; the copy is
			// what tells them to do it.
			if resp.RefCode == "" {
				t.Errorf("ref_code is empty — the buyer has nothing to quote to the merchant (body %s)",
					rec.Body.String())
			}
			if strings.TrimSpace(resp.Error) == "" {
				t.Errorf("error prose is empty — a non-browser caller gets no instruction (body %s)",
					rec.Body.String())
			}

			// THE DEFECT, half two: the sale must not stay `pending` with no
			// chip_purchase_id, which is what accumulated phantoms in the
			// merchant's Sales list for money that can never be collected.
			sale, err := h.Sales.ByRefCode(ctx, resp.RefCode)
			if err != nil {
				t.Fatalf("fetch sale %q: %v", resp.RefCode, err)
			}
			if sale.Status != dbstore.SaleFailed {
				t.Errorf("sale.status = %q, want %q — a pending row with no purchase is a phantom "+
					"in the merchant's Sales list", sale.Status, dbstore.SaleFailed)
			}
			if sale.ChipPurchaseID != "" {
				t.Errorf("sale.chip_purchase_id = %q, want empty (CHIP never created a purchase)",
					sale.ChipPurchaseID)
			}
		})
	}
}

// TestEveryPublicOrderSurfaceRendersThePaymentFailureInApp drives the FOUR real
// renderers — the block payment form, the response-only form, the custom-HTML
// form and the last-resort product-checkbox form — because a renderer that drops
// the failure branch leaves a real buyer with a dead submit button. The count
// floor is deliberate: the shared renderer is DEFINED once and CALLED from both
// the payment_error branch and the unparseable-answer catch, so a surface that
// loses either call site is a red.
func TestEveryPublicOrderSurfaceRendersThePaymentFailureInApp(t *testing.T) {
	for _, c := range rendererCases() {
		t.Run(c.name, func(t *testing.T) {
			page := c.render(t, "#5e6ad2")
			if !strings.Contains(page, "function jfPayFail(d){") {
				t.Fatalf("%s: the page carries no payment-failure renderer at all", c.name)
			}
			if !strings.Contains(page, "'payment_error'") {
				t.Errorf("%s: the submit handler never branches on the payment_error state — "+
					"a failed purchase falls through to the checkout path and the buyer gets nothing", c.name)
			}
			if n := strings.Count(page, "jfPayFail("); n < 3 {
				t.Errorf("%s: jfPayFail( appears %d time(s), want >= 3 (definition + the "+
					"payment_error call + the unreadable-answer catch). Without the catch a "+
					"non-JSON body made r.json() reject and the click did NOTHING.", c.name, n)
			}
			// English is the default for a form with no language config.
			if !strings.Contains(page, copyEnContactMerchant) {
				t.Errorf("%s: the default (English) page does not carry the actionable copy "+
					"(\"contact the merchant\") — the buyer gets a failure with no instruction", c.name)
			}
		})
	}
}

// copyEnContactMerchant / copyMsContactMerchant are the halves
// of the dictionary copy that name the remedy, so the assertion does not depend
// on the surrounding wording.
const (
	copyEnContactMerchant = "contact the merchant"
	copyMsContactMerchant = "hubungi penjual"
)

// TestPaymentFailureCopyFollowsThePageLanguage pins the language of the failure
// copy on BOTH template paths: three surfaces receive langStrings through a
// data MAP, the last-resort layout through a STRUCT field. A fix that wired the
// map and forgot the struct (or passed a fixed English copy) shows here as a
// Malay form rendering English — the same shape as the earlier jf_lang false
// reading, where a page rendered in a language no new visitor gets.
func TestPaymentFailureCopyFollowsThePageLanguage(t *testing.T) {
	msBlockCfg := map[string]any{
		"lang":   "ms",
		"mode":   "payment",
		"blocks": []any{map[string]any{"type": "product", "value": "Item"}},
	}
	msLegacyCfg := map[string]any{
		"lang":        "ms",
		"description": "Derma",
		"products": []any{map[string]any{
			"id": "p1", "name": "Item", "price_sen": 300, "active": true,
		}},
	}

	render := map[string]func(t *testing.T) string{
		"block form (map data)": func(t *testing.T) string {
			h := &PublicHandlers{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://demo.jomform.com/ms-block", nil)
			if !h.renderBlocks(rec, req, themedForm("ms-block", "#5e6ad2", msBlockCfg)) {
				t.Fatal("renderBlocks declined a payment form with a product block")
			}
			return rec.Body.String()
		},
		"legacy product-checkbox form (struct data)": func(t *testing.T) string {
			h := &PublicHandlers{}
			f := themedForm("ms-legacy", "#5e6ad2", msLegacyCfg)
			req := httptest.NewRequest("GET", "http://demo.jomform.com/ms-legacy", nil)
			if h.renderBlocks(httptest.NewRecorder(), req, f) {
				t.Fatal("renderBlocks handled a form with no blocks — this is no longer the legacy path")
			}
			rec := httptest.NewRecorder()
			h.renderLegacy(rec, req, f)
			return rec.Body.String()
		},
	}

	for name, fn := range render {
		t.Run(name, func(t *testing.T) {
			page := fn(t)
			if !strings.Contains(page, copyMsContactMerchant) {
				t.Errorf("%s: a form configured lang=ms does not carry the Malay failure copy "+
					"(%q) — the page language is not reaching the failure renderer",
					name, copyMsContactMerchant)
			}
			if strings.Contains(page, copyEnContactMerchant) {
				t.Errorf("%s: a lang=ms form carries the ENGLISH failure copy — the buyer is "+
					"being told what to do in a language the merchant did not choose", name)
			}
		})
	}
}
