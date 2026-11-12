package dbstore

// Card t_983425e5 — a PAID form whose product block is unpointed published
// silently while the buyer page said "This form is not accepting orders right
// now".
//
// THE DEFECT, measured on prod (form e1fcc6cc-218d-4075-90b0-b2b8e03944c6,
// workspace `test-wan`): status=published, mode=payment, and the product block
// was `{"id":"q5uhvoy","type":"product","value":"","quantity":true}` — no
// `product_id` at all. The form's `config.products` snapshot did carry the
// account's product, and that is exactly what hid it: the write-path rule counted
// a snapshot id as "something to sell", while the BLOCK renderer paints price
// controls from `blocks` alone and served the submit-less "not accepting orders"
// page. Two answers to one question.
//
// What this file pins:
//
//   - the block layout's sellability is the renderer's answer, not the snapshot's
//     (the prod shape above is unready, and it says WHICH code + block to fix);
//   - a product block naming a PURGED product is reported too — the config still
//     carries the id, so only the account's product-row set can see it;
//   - an ARCHIVED/TRASHED product is NOT reported: its row still exists and the
//     renderer/order path fall back to the form's snapshot, so the page still
//     sells at the snapshotted price. Over-reporting here would nag a merchant
//     whose product is merely off sale.
//
// Pure unit tests — no database; the rule reads the form struct plus the id set.

import "testing"

// prodEmptyProductBlockForm is the measured prod shape: a payment form with the
// merchant's fields and a product block that was never pointed at a product,
// while the workspace catalogue sits in `products`.
func prodEmptyProductBlockForm() *Form {
	return formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
		"blocks": []any{
			map[string]any{"id": "nm1", "type": "text", "value": "Nama Penuh"},
			map[string]any{"id": "em1", "type": "email", "value": "Emel"},
			map[string]any{"id": "q5uhvoy", "type": "product", "value": "", "quantity": true},
		},
		"products": []any{map[string]any{
			"id": "fdf8af0c-abc6-430a-885c-775342628bc2", "name": "derma", "price_sen": float64(100),
		}},
	})
}

func TestProductBlockWithoutAProductIsUnreadyDespiteTheSnapshot(t *testing.T) {
	f := prodEmptyProductBlockForm()
	// The config-only question: the snapshot must NOT rescue a block layout.
	if !PaymentFormWithoutProducts(f) {
		t.Fatal("a block layout with an unpointed product block was reported as sellable because of its " +
			"products snapshot — the buyer page paints controls from `blocks` only, so this form " +
			"publishes and serves \"This form is not accepting orders right now\"")
	}
	code, blockID := PublishWarning(f, FormStatusPublished, nil)
	if code != PublishWarningNoProductSelected {
		t.Fatalf("PublishWarning code = %q, want %q (the merchant picked a block, not a product; "+
			"\"no products\" would send them looking for a product they already created)",
			code, PublishWarningNoProductSelected)
	}
	if blockID != "q5uhvoy" {
		t.Errorf("block id = %q, want the product block %q — the builder links straight to it", blockID, "q5uhvoy")
	}
	// Acknowledging it is still allowed: the form may be published while the
	// catalogue is being set up (the 409 is a warning, not a wall).
	if !publishNeedsAck(f, FormStatusPublished) {
		t.Error("the guard did not fire on the measured prod shape")
	}
}

func TestLegacySnapshotLayoutStaysSellable(t *testing.T) {
	// The other side of the same rule: a form with NO blocks is the legacy
	// layout, whose page paints one checkbox per `products` snapshot entry. Its
	// snapshot IS something to sell and must keep publishing bare.
	legacy := formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
		"products": []any{map[string]any{"id": "p1", "name": "Tee", "price_sen": float64(1000)}},
	})
	if PaymentFormWithoutProducts(legacy) {
		t.Error("a legacy (block-less) form with a product snapshot was reported unready")
	}
	if code, _ := PublishWarning(legacy, FormStatusPublished, map[string]bool{}); code != PublishReady {
		t.Errorf("legacy snapshot form publish code = %q, want ready", code)
	}
}

func TestPurgedProductBlockIsTheSameSignalAsAnEmptyOne(t *testing.T) {
	// The sibling case: the merchant DID point the block at a product, and the
	// product row was later purged. The config still carries the id, so the
	// config-only rule cannot see it; the account's row set can.
	f := prodEmptyProductBlockForm()
	blocks := f.Config["blocks"].([]any)
	blocks[2].(map[string]any)["product_id"] = "purged-0000-0000-0000-000000000000"
	f.Config["blocks"] = blocks

	// No row for that id → the block points at nothing the account can resolve.
	if code, blockID := PublishWarning(f, FormStatusPublished, map[string]bool{}); code != PublishWarningNoProductSelected {
		t.Fatalf("purged product block: PublishWarning code = %q, want %q", code, PublishWarningNoProductSelected)
	} else if blockID != "q5uhvoy" {
		t.Errorf("purged product block: block id = %q, want q5uhvoy", blockID)
	}
	if _, dead := NotAcceptingOrders(f, map[string]bool{}); !dead {
		t.Error("a product block naming a PURGED product was not reported to the dashboard")
	}

	// An ARCHIVED or TRASHED product still HAS a row, and the renderer/order path
	// fall back to the form's snapshot for it — the page sells, so the guard must
	// stay silent (the prior decision: do not nag about an archived product).
	live := map[string]bool{"purged-0000-0000-0000-000000000000": true}
	if code, _ := PublishWarning(f, FormStatusPublished, live); code != PublishReady {
		t.Errorf("archived/trashed product block: PublishWarning code = %q, want ready", code)
	}
	if _, dead := NotAcceptingOrders(f, live); dead {
		t.Error("a product block naming an existing (archived/trashed) row was reported dead")
	}
}

func TestNotAcceptingOrdersExemptsResponseAndMenuForms(t *testing.T) {
	// Same exemptions as the publish rule, and for the same reason: these forms
	// do not take orders at all, so "not accepting orders" is not their defect.
	resp := formWithConfig(FormStatusPublished, FormModeResponse, map[string]any{
		"blocks": []any{map[string]any{"id": "b1", "type": "email", "value": "Email"}},
	})
	if _, dead := NotAcceptingOrders(resp, nil); dead {
		t.Error("a response form was reported as not accepting orders")
	}
	menu := formWithConfig(FormStatusPublished, FormModePayment, map[string]any{
		"menu": map[string]any{"enabled": true},
	})
	if _, dead := NotAcceptingOrders(menu, nil); dead {
		t.Error("a menu-enabled payment form was reported as not accepting orders")
	}
}

func TestNoProductBlockKeepsTheNoProductsCode(t *testing.T) {
	// No product block at all → `no_products`, the existing code the dashboard
	// already localizes. Only a real product block earns the new code, so an
	// ordinary field-only payment form keeps the sentence it always had.
	f := formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
		"blocks": nameEmailOnlyBlocks(),
	})
	code, blockID := PublishWarning(f, FormStatusPublished, nil)
	if code != PublishWarningNoProducts {
		t.Fatalf("field-only payment form: code = %q, want %q", code, PublishWarningNoProducts)
	}
	if blockID != "" {
		t.Errorf("field-only payment form: block id = %q, want empty (there is no product block to link to)", blockID)
	}
}

func nameEmailOnlyBlocks() []any {
	return []any{
		map[string]any{"id": "b1", "type": "email", "value": "Email"},
		map[string]any{"id": "b2", "type": "name", "value": "Name"},
	}
}
