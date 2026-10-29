package httpapi

// Card t_f9e2f855 — a REQUIRED option/shipping block is enforced on the ORDER
// route, not only in the browser.
//
// THE DEFECT. Card t_addad3bd made both payment-renderer arms emit `required`
// on the `<select>` a buyer answers with, so the page's own constraint
// validation refuses an empty choice before the submit event. The ORDER route
// validated only against `formfields.Contract`, and that derivation's
// allowlist (formfields.collectableTypes) deliberately excludes the pricing
// blocks (`product`, `option`, `shipping`, `total`). So the requirement the
// page advertises had no server-side half: a direct POST created the sale with
// the required choice absent. Measured before this fix, with one product line
// and the answer left out of the same payload:
//
//	required option   block → 201 {"status":"bank_direct"}
//	required shipping block → 201 {"status":"bank_direct"}
//	required text     block → 400 "medan wajib tidak diisi: Nama"
//
// THE FIX SHAPE. `formfields.Contract` is untouched, because THREE surfaces
// derive from it and only one of them renders a pricing control:
//
//   - the form's own order route (order.go) — the buyer sees the select, so the
//     choice must be enforced;
//   - the menu checkout (menu.go menuExtraContract) — renders the attached
//     order form's INPUT fields and never a pricing block, so enforcing one
//     would refuse an order for a control the checkout never paints;
//   - the response surface (renderResponseForm) — drops the pricing blocks
//     behind an explicit case, same reasoning.
//
// So the pricing answer is its own derivation, `formfields.PricingContract`,
// applied on the ONE surface that renders the select: the order core's
// form-scoped path (`tgt.FieldsForm != nil`, which is exactly the form's own
// page and its embed). The menu checkout passes `FieldsForm: nil` and the
// response route never reaches the order core at all, so both keep their
// previous behaviour byte for byte — pinned below rather than assumed.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
)

// pricingReqPlan is the fixture in one place: the ids, labels and slugs a test
// needs, plus the handlers and host the requests go to.
type pricingReqPlan struct {
	h                            *PublicHandlers
	host, accountID, accountSlug string
	prodID                       string
	orderSlug, respSlug          string
	menuSlug, extraSlug          string
	blockID, blockLabel          string
	kind                         string
	orderFormID, respFormID      string
}

// pricingPricingBlock is the pricing block both forms carry: one priced choice
// of `kind` (option or shipping), required exactly as `required` says. The
// builder sets product_id = the block's own id (that is how the page's data-id,
// the submit key and the line item's product_id all agree), so the fixture uses
// the builder's shape.
func pricingPricingBlock(prodID, kind string, required bool) []any {
	id, label := "o1", "Saiz"
	if kind == "shipping" {
		id, label = "s1", "Penghantaran"
	}
	return []any{
		map[string]any{"id": "p1", "type": "product", "product_id": prodID, "value": "Tiket"},
		map[string]any{
			"id": id, "type": kind, "value": label, "required": required,
			"product_id": id,
			"choices":    []any{map[string]any{"label": "Besar", "value": "l", "price_sen": 700}},
		},
	}
}

// seedPricingReqTenant creates the workspace, account, product and handlers a
// pricing-requirement fixture needs and returns the plan with those fields
// filled. Split out of newPricingReqPlan so the conditional fixture below seeds
// the SAME tenant shape rather than restating sixty lines of it: a fixture
// copied from another fixture drifts from the production writer the moment
// either copy changes.
func seedPricingReqTenant(t *testing.T, kind string) *pricingReqPlan {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	fx := &pricingReqPlan{kind: kind}
	if kind == "shipping" {
		fx.blockID, fx.blockLabel = "s1", "Penghantaran"
	} else {
		fx.blockID, fx.blockLabel = "o1", "Saiz"
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id, slug`, userID, workspaceSlug).Scan(&fx.accountID, &fx.accountSlug); err != nil {
		t.Fatalf("create account: %v", err)
	}

	prod := &dbstore.Product{
		AccountID: fx.accountID, Name: "Tiket", PriceSen: 1500,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	fx.prodID = prod.ID

	fx.h = &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Submissions:       dbstore.NewSubmissionStore(pool),
		BaseURL:           "http://localhost:8080",
	}
	fx.host = workspaceSlug + ".jomform.com"
	return fx
}

// newPricingReqPlan seeds one account with:
//
//   - a published PAYMENT form carrying the product + the required pricing
//     block (the surface this card fixes);
//   - a published RESPONSE form whose only block is the same required pricing
//     block (the response renderer drops it — the pin);
//   - a published extra order form carrying the same required pricing block,
//     attached to a menu form, so the menu checkout has a required pricing
//     block in its attached config and must still accept an order (the pin).
func newPricingReqPlan(t *testing.T, kind string, required bool) *pricingReqPlan {
	t.Helper()
	ctx := context.Background()
	fx := seedPricingReqTenant(t, kind)

	create := func(slug, title, mode string, blocks []any, extra map[string]any) *dbstore.Form {
		cfg := map[string]any{"blocks": blocks}
		if mode != "" {
			cfg["mode"] = mode
		}
		for k, v := range extra {
			cfg[k] = v
		}
		f := &dbstore.Form{
			AccountID: fx.accountID, Slug: slug, Title: title,
			Status: dbstore.FormStatusPublished, Config: cfg,
		}
		if err := fx.h.Forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %q: %v", slug, err)
		}
		return f
	}

	pricing := pricingPricingBlock(fx.prodID, kind, required)
	fx.orderSlug = "it-preq-order-" + fx.accountID[:8]
	fx.orderFormID = create(fx.orderSlug, "Tiket Order", "", pricing, nil).ID

	// The response surface: its ONLY block is the required pricing block, so
	// any refusal could only come from it. The response renderer drops pricing
	// blocks; the route must ignore them exactly as it did before this card.
	fx.respSlug = "it-preq-resp-" + fx.accountID[:8]
	fx.respFormID = create(fx.respSlug, "Borang", dbstore.FormModeResponse,
		[]any{pricing[1]}, nil).ID

	// The menu checkout's attached order form: an INPUT field beside the
	// required pricing block, so the menu keeps a real (non-empty) contract.
	fx.extraSlug = "it-preq-extra-" + fx.accountID[:8]
	create(fx.extraSlug, "Maklumat Tambahan", "",
		[]any{map[string]any{"id": "note", "type": "textarea", "value": "Nota"}, pricing[1]}, nil)

	// The menu form itself: its own product block plus an enabled menu that
	// names the extra order form above.
	fx.menuSlug = "it-preq-menu-" + fx.accountID[:8]
	create(fx.menuSlug, "Warung", "",
		[]any{map[string]any{"id": "p1", "type": "product", "product_id": fx.prodID, "value": "Tiket"}},
		map[string]any{"menu": map[string]any{
			"enabled": true, "categories": []any{}, "order_form_slug": fx.extraSlug,
		}})

	return fx
}

// orderBody builds the payload a page posts: the product line, and (when
// fields is non-empty) the answer map. The pricing choice's own line item is
// added by the page only when the choice is answered, so `withChoiceItem`
// mirrors that.
func (fx *pricingReqPlan) orderBody(fields map[string]string, withChoiceItem bool) string {
	items := `{"product_id":"` + fx.prodID + `","quantity":1}`
	if withChoiceItem {
		items += `,{"product_id":"` + fx.blockID + `","quantity":1}`
	}
	body := `{"client":{"full_name":"Ali","email":"buyer@example.test"}`
	if len(fields) > 0 {
		raw, _ := json.Marshal(fields)
		body += `,"fields":` + string(raw)
	}
	return body + `,"items":[` + items + `],"payment_method":"bank_direct"}`
}

// TestOrderRefusesARequiredPricingChoiceLeftUnanswered is the card's measured
// case, on both arms, end to end through the real route.
func TestOrderRefusesARequiredPricingChoiceLeftUnanswered(t *testing.T) {
	for _, kind := range []string{"option", "shipping"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			fx := newPricingReqPlan(t, kind, true)

			// (a) the measured payload: the required choice is simply not there.
			rec := postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(nil, false))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("order without the required %s choice = %d, want 400 (body %s).\n"+
					"The page emits `required` on this select (t_addad3bd); if the route accepts it the "+
					"two surfaces disagree and a direct POST buys without the choice.",
					kind, rec.Code, rec.Body.String())
			}
			if want := "medan wajib tidak diisi: " + fx.blockLabel; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("refusal body = %s, want it to contain %q (the same shape a required text "+
					"block gets)", rec.Body.String(), want)
			}

			// (b) the page's own shape for an unanswered choice: the submit script
			// collects every [data-id] control, so an empty select is submitted as
			// "" rather than omitted. Same refusal.
			rec = postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(map[string]string{fx.blockID: ""}, false))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("order with the required %s choice submitted empty = %d, want 400 (body %s)",
					kind, rec.Code, rec.Body.String())
			}

			// A refused order stores nothing.
			sales, err := fx.h.Sales.ListByAccount(ctx, fx.accountID, 10)
			if err != nil {
				t.Fatalf("list sales: %v", err)
			}
			if len(sales) != 0 {
				t.Fatalf("a refused order recorded %d sales, want 0", len(sales))
			}

			// (c) the control: the same form, the choice answered exactly as the
			// page posts it (answer map + the add-on line item), is accepted and
			// charged the choice's price. Without this a fix that refuses every
			// order carrying a pricing block would pass the assertions above.
			rec = postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(map[string]string{fx.blockID: "l"}, true))
			if rec.Code != http.StatusCreated {
				t.Fatalf("order WITH the required %s choice = %d, want 201 (body %s)",
					kind, rec.Code, rec.Body.String())
			}
			sales, err = fx.h.Sales.ListByAccount(ctx, fx.accountID, 10)
			if err != nil {
				t.Fatalf("list sales: %v", err)
			}
			if len(sales) != 1 {
				t.Fatalf("sales recorded = %d, want 1", len(sales))
			}
			var charged bool
			for _, it := range sales[0].Items {
				if it.ProductID == fx.blockID {
					charged = true
					if it.PriceSen != 700 {
						t.Errorf("the answered %s line = %d sen, want the choice's 700", kind, it.PriceSen)
					}
				}
			}
			if !charged {
				t.Errorf("the answered %s choice is not among the sale's line items: %+v", kind, sales[0].Items)
			}
		})
	}
}

// TestOrderRequiredPricingChoiceNeutralised is the neutralising arm: the SAME
// form with the flag flipped false accepts the same payload. That is what makes
// the refusals above a test of the flag rather than of some unrelated rule.
func TestOrderRequiredPricingChoiceNeutralised(t *testing.T) {
	for _, kind := range []string{"option", "shipping"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			fx := newPricingReqPlan(t, kind, false)

			rec := postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(nil, false))
			if rec.Code != http.StatusCreated {
				t.Fatalf("the %s block is NOT required: order without the choice = %d, want 201 "+
					"(body %s). The neutralising arm must accept what the required arm refuses.",
					kind, rec.Code, rec.Body.String())
			}
			rec = postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(map[string]string{fx.blockID: ""}, false))
			if rec.Code != http.StatusCreated {
				t.Fatalf("the %s block is NOT required: order with an empty choice = %d, want 201 (body %s)",
					kind, rec.Code, rec.Body.String())
			}
			sales, err := fx.h.Sales.ListByAccount(ctx, fx.accountID, 10)
			if err != nil {
				t.Fatalf("list sales: %v", err)
			}
			if len(sales) != 2 {
				t.Fatalf("sales recorded = %d, want 2", len(sales))
			}
		})
	}
}

// TestMenuCheckoutStillAcceptsAnOrderWithoutTheExtraFormsPricingChoice pins the
// menu surface. Its checkout renders the attached order form's INPUT fields
// (menuExtraContract) and never a pricing block, so a required pricing block in
// that config must not make the checkout refuse an order — the drift this card
// was split out to avoid.
func TestMenuCheckoutStillAcceptsAnOrderWithoutTheExtraFormsPricingChoice(t *testing.T) {
	fx := newPricingReqPlan(t, "option", true)

	// The premise, checked rather than assumed: the menu's own contract for the
	// attached form carries no field for the pricing block, so nothing there
	// could enforce it.
	attached, err := fx.h.Forms.PublishedByAccountSlug(context.Background(), fx.accountSlug, fx.extraSlug)
	if err != nil || attached == nil {
		t.Fatalf("resolve the attached order form %q: %v", fx.extraSlug, err)
	}
	for _, f := range menuExtraContract(attached.Config) {
		if f.FieldID == fx.blockID {
			t.Fatalf("the menu checkout's contract carries the pricing block %q — that is the drift "+
				"this card exists to avoid (contract: %+v)", f.FieldID, menuExtraContract(attached.Config))
		}
	}

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"fields":{"note":"Ok"},"items":[{"product_id":"` + fx.prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec := postSlug(t, fx.h, fx.host, fx.menuSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("menu order without the attached form's required pricing choice = %d, want 201 "+
			"(body %s). The menu checkout renders no pricing control, so it must not enforce one — "+
			"that is why the order route's pricing rule is not part of formfields.Contract.",
			rec.Code, rec.Body.String())
	}
}

// TestResponseRouteStillIgnoresARequiredPricingChoice pins the response surface:
// renderResponseForm drops the pricing blocks, so its submit route must keep
// accepting a submission that answers none of them, exactly as before this card.
func TestResponseRouteStillIgnoresARequiredPricingChoice(t *testing.T) {
	fx := newPricingReqPlan(t, "shipping", true)

	for _, body := range []string{`{}`, `{"fields":{"s1":""}}`, `{"fields":{"s1":"l"}}`} {
		rec := postSlug(t, fx.h, fx.host, fx.respSlug, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("response submit %s = %d, want 201 (body %s). A required pricing block is never "+
				"rendered on the response surface, so it must not be enforced there.",
				body, rec.Code, rec.Body.String())
		}
	}
	if n := submissionCount(t, fx.h, fx.accountID, fx.respFormID); n != 3 {
		t.Fatalf("submissions stored = %d, want 3", n)
	}
}

// TestPricingContractKeysAreThePagesSubmitKeys is the unit half of the join: the
// key the server validates must be the key the page's select is tagged with
// (`data-id="{{.ProductID}}"` for the option/shipping arms), with the block's
// own id as the fallback for a config that never set product_id.
func TestPricingContractKeysAreThePagesSubmitKeys(t *testing.T) {
	cfg := map[string]any{"blocks": []any{
		map[string]any{"id": "p1", "type": "product", "product_id": "prod-1", "value": "Tiket"},
		map[string]any{
			"id": "o1", "type": "option", "value": "Saiz", "required": true,
			"product_id": "opt-key",
			"choices":    []any{map[string]any{"label": "Besar", "value": "l", "price_sen": 700}},
		},
		map[string]any{
			"id": "s1", "type": "shipping", "value": "Penghantaran", "required": false,
			"choices": []any{map[string]any{"label": "Pos", "value": "pos", "price_sen": 500}},
		},
		map[string]any{"id": "tx", "type": "text", "value": "Nama", "required": true},
	}}
	got := formfields.PricingContract(cfg)
	if len(got) != 2 {
		t.Fatalf("PricingContract = %+v, want exactly the option + shipping blocks", got)
	}
	if got[0].FieldID != "opt-key" || got[0].Type != "option" || !got[0].Required || got[0].Label != "Saiz" {
		t.Errorf("option entry = %+v, want the page's data-id (product_id) with the merchant's label and flag", got[0])
	}
	// No product_id → the block's own id, which is the builder's own shape and
	// what SubmitKeys derives for a block that has one.
	if got[1].FieldID != "s1" || got[1].Type != "shipping" || got[1].Required {
		t.Errorf("shipping entry = %+v, want id s1, not required", got[1])
	}
	// A required TEXT block stays the ordinary contract's business; this
	// derivation must not carry it too (it would double-report in the message).
	for _, f := range got {
		if f.Type == "text" {
			t.Errorf("PricingContract carried the text block %q", f.FieldID)
		}
	}

	// The flag is what it enforces: nothing required → nothing to refuse.
	relaxed := formfields.PricingContract(map[string]any{"blocks": []any{
		map[string]any{"id": "o1", "type": "option", "value": "Saiz", "product_id": "o1"},
	}})
	if err := formfields.ValidateFields(relaxed, nil); err != nil {
		t.Errorf("a non-required pricing block refused an empty answer: %v", err)
	}
	if err := formfields.ValidateFields(got, nil); err == nil ||
		!strings.Contains(err.Error(), "medan wajib tidak diisi: Saiz") {
		t.Errorf("a required pricing block with no answer = %v, want the required message naming the label", err)
	}
}

// newConditionalPricingReqPlan seeds ONE payment form: a text block `em`, and a
// required pricing block whose conditional rule shows it only while `em`
// answers "pergi".
//
// This config is what the REST/MCP authoring surface stores verbatim (the
// builder's panel offers a condition only on `fieldTypes`, which excludes
// option/shipping, but the write path stores the block JSON as given). What the
// page does with it is not assumed here: render_blocks.go sets CondJSON for
// EVERY block and the template guards the wrapper on it, and blockConditionJS
// hides AND disables the controls of a block whose rule is false, so the value
// is never posted and the `required` constraint never runs.
func newConditionalPricingReqPlan(t *testing.T, kind string) *pricingReqPlan {
	t.Helper()
	ctx := context.Background()
	fx := seedPricingReqTenant(t, kind)

	pricing := pricingPricingBlock(fx.prodID, kind, true)[1].(map[string]any)
	pricing["condition"] = map[string]any{"field_id": "em", "op": "equals", "value": "pergi"}
	blocks := []any{
		map[string]any{"id": "em", "type": "text", "value": "Destinasi"},
		pricing,
	}

	fx.orderSlug = "it-pcond-order-" + fx.accountID[:8]
	f := &dbstore.Form{
		AccountID: fx.accountID, Slug: fx.orderSlug, Title: "Tiket Bersyarat",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": blocks},
	}
	if err := fx.h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create conditional form %q: %v", fx.orderSlug, err)
	}
	fx.orderFormID = f.ID
	return fx
}

// TestPaymentPageWrapsAConditionalPricingBlock is the renderer half of the
// parity claim the route fix depends on: the payment page puts the pricing block
// inside the SAME `data-condition` wrapper it puts a contract field in, and the
// select inside it carries `required`. Without this, carrying the rule in
// PricingContract would make the server SKIP a block the page shows — the drift
// in the other direction. If a future change stops wrapping pricing blocks, the
// page never hides them and this must go red, because the semantics decision has
// to be revisited, not silently kept.
func TestPaymentPageWrapsAConditionalPricingBlock(t *testing.T) {
	body := renderPaymentPage(t, "pcond-payment", map[string]any{
		"blocks": []any{
			map[string]any{"id": "em", "type": "text", "value": "Destinasi"},
			map[string]any{
				"id": "o1", "type": "option", "value": "Saiz", "required": true, "product_id": "o1",
				"condition": map[string]any{"field_id": "em", "op": "equals", "value": "pergi"},
				"choices":   []any{map[string]any{"label": "Besar", "value": "l", "price_sen": 700}},
			},
		},
		"products": []any{},
	})

	if got, want := conditionAttr(t, body), `{"field_id":"em","op":"equals","value":"pergi"}`; got != want {
		t.Fatalf("the pricing block's data-condition = %q, want %q. The payment page must wrap an "+
			"option/shipping block in its own rule exactly as it wraps a contract field, or the page "+
			"and the order route disagree about whether the choice was shown.", got, want)
	}
	inner := wrappedConditionalBlock(body)
	if inner == "" {
		t.Fatalf("the conditional pricing block is not wrapped in a balanced <div class=\"jf-block\">")
	}
	for _, want := range []string{
		`<label class="lbl">Saiz (required)</label>`,
		`<select class="opt" data-id="o1" required>`,
	} {
		if !strings.Contains(inner, want) {
			t.Errorf("the wrapped pricing block is missing %q:\n%s", want, inner)
		}
	}
}

// TestOrderHonoursAPricingBlocksConditionalRule is the arm PR #1443 was missing,
// and the regression the verify lane measured: the pricing requirement must
// apply only while the block was VISIBLE.
//
//	fields.em="tidak", choice unanswered      -> 201   (the bug: 400)
//	fields.em="pergi", choice unanswered      -> 400
//	fields.em="pergi", choice="l" + its item  -> 201
//
// Row 1 is the bug: before the fix the route answered 400
// "medan wajib tidak diisi: Saiz" for a control the page had hidden and disabled.
// Row 2 keeps the fix from becoming "a conditional pricing block is never
// required" — the rule is what decides, not the presence of the rule. Row 3 is
// the control: a fix that simply stopped enforcing any pricing block would pass
// row 1 and fail row 2, and one that dropped charging would pass both and fail
// here.
func TestOrderHonoursAPricingBlocksConditionalRule(t *testing.T) {
	for _, kind := range []string{"option", "shipping"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			fx := newConditionalPricingReqPlan(t, kind)

			// (a) the rule is FALSE for this submission, so the page hides and
			// disables the select and the value is never posted. Accept.
			rec := postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(map[string]string{"em": "tidak"}, false))
			if rec.Code != http.StatusCreated {
				t.Fatalf("order whose required %s block is HIDDEN by its own rule (em=\"tidak\") = %d, "+
					"want 201 (body %s).\nThe page disables the select, so this choice can never be "+
					"answered; requiring it refuses an order the buyer had no way to complete.",
					kind, rec.Code, rec.Body.String())
			}
			sales, err := fx.h.Sales.ListByAccount(ctx, fx.accountID, 10)
			if err != nil {
				t.Fatalf("list sales: %v", err)
			}
			if len(sales) != 1 {
				t.Fatalf("sales recorded after the accepted order = %d, want 1", len(sales))
			}

			// (b) the same config with the rule TRUE: the page shows the select
			// with `required`, so the requirement is real again.
			rec = postSlug(t, fx.h, fx.host, fx.orderSlug, fx.orderBody(map[string]string{"em": "pergi"}, false))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("order whose required %s block is SHOWN by its rule (em=\"pergi\") and left "+
					"unanswered = %d, want 400 (body %s). A rule that happens to exist must not "+
					"neutralise the requirement while it holds.", kind, rec.Code, rec.Body.String())
			}
			if want := "medan wajib tidak diisi: " + fx.blockLabel; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("refusal body = %s, want it to contain %q", rec.Body.String(), want)
			}

			// (c) rule true and the choice answered exactly as the page posts it.
			rec = postSlug(t, fx.h, fx.host, fx.orderSlug,
				fx.orderBody(map[string]string{"em": "pergi", fx.blockID: "l"}, true))
			if rec.Code != http.StatusCreated {
				t.Fatalf("order with the shown %s choice answered = %d, want 201 (body %s)",
					kind, rec.Code, rec.Body.String())
			}
			sales, err = fx.h.Sales.ListByAccount(ctx, fx.accountID, 10)
			if err != nil {
				t.Fatalf("list sales: %v", err)
			}
			if len(sales) != 2 {
				t.Fatalf("sales recorded = %d, want 2 (the hidden-rule order and the answered one)", len(sales))
			}
			var charged bool
			for _, it := range sales[0].Items {
				if it.ProductID == fx.blockID {
					charged = true
					if it.PriceSen != 700 {
						t.Errorf("the answered %s line = %d sen, want the choice's 700", kind, it.PriceSen)
					}
				}
			}
			if !charged {
				t.Errorf("the answered %s choice is not among the sale's line items: %+v", kind, sales[0].Items)
			}
		})
	}
}
