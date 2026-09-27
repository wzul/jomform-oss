package dbstore

// Product trash: soft delete / list / restore / purge.
//
// The product bin mirrors the form bin (form_trash_test.go), with the two
// differences the products table forces:
//
//	restore — CANNOT refuse. Products have no slug and no unique index, so
//	          trashing one releases no name and clearing deleted_at cannot
//	          violate anything. Pinned below so a future "products get a slug"
//	          change does not quietly inherit the form's refusal path.
//	purge   — refuses on TWO obstacles, not one. A sale detaches (the FK is ON
//	          DELETE SET NULL, so the delete would SUCCEED and silently orphan
//	          history), and a subscription BLOCKS (the FK is ON DELETE RESTRICT,
//	          so without the named refusal it would surface as a raw 23503/500).
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"testing"
)

// trashProduct creates a product on an account and trashes it.
func trashProduct(t *testing.T, ctx context.Context, acc, name string) *Product {
	t.Helper()
	ps := NewProductStore(testPool(t))
	p := &Product{AccountID: acc, Name: name, PriceSen: 1200, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := ps.SoftDelete(ctx, acc, p.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	return p
}

// TestProductTrashIsASeparateStateFromActive is the card's core distinction: a
// product in the bin and a product taken off sale are DIFFERENT states, and
// conflating them is why a trashed product used to keep counting as archived.
//
// The live catalogue reads must lose a trashed product entirely (both the active
// list and the admin list that shows archived rows), while `active` itself keeps
// its own meaning on the row.
func TestProductTrashIsASeparateStateFromActive(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)

	live := &Product{AccountID: acc, Name: "IT live", PriceSen: 100, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, live); err != nil {
		t.Fatalf("create live product: %v", err)
	}
	// An archived-but-live product: off sale, still in the catalogue.
	archived := &Product{AccountID: acc, Name: "IT archived", PriceSen: 200, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, archived); err != nil {
		t.Fatalf("create archived product: %v", err)
	}
	if err := ps.SetActive(ctx, acc, archived.ID, false); err != nil {
		t.Fatalf("archive: %v", err)
	}

	trashed := &Product{AccountID: acc, Name: "IT trashed", PriceSen: 300, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, trashed); err != nil {
		t.Fatalf("create trashed product: %v", err)
	}
	if err := ps.SoftDelete(ctx, acc, trashed.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// The admin list (active + archived) still shows the archived row and must
	// NOT show the trashed one: the catalogue is not the bin.
	all, err := ps.ListAllByAccounts(ctx, []string{acc})
	if err != nil {
		t.Fatalf("ListAllByAccounts: %v", err)
	}
	seen := map[string]bool{}
	for _, x := range all {
		seen[x.ID] = true
	}
	if !seen[archived.ID] {
		t.Error("an ARCHIVED (active=false) product is missing from the admin list — off sale is not the bin")
	}
	if seen[trashed.ID] {
		t.Error("a TRASHED product still appears in the admin catalogue list — the bin must be invisible there")
	}
	if seen[archived.ID] && seen[trashed.ID] {
		t.Error("archived and trashed are indistinguishable")
	}

	// The active list shows only the live one.
	active, err := ps.ListActiveByAccount(ctx, acc)
	if err != nil {
		t.Fatalf("ListActiveByAccount: %v", err)
	}
	for _, x := range active {
		if x.ID == trashed.ID {
			t.Error("a trashed product appears in the active list")
		}
	}

	// Get must not reach a trashed row.
	if _, err := ps.Get(ctx, acc, trashed.ID); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("Get(trashed) err = %v, want ErrProductNotFound", err)
	}

	// And it IS in the bin.
	bin, err := ps.ListTrashed(ctx, []string{acc})
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	var inBin bool
	for _, x := range bin {
		if x.ID == trashed.ID {
			inBin = true
			if x.DeletedAt == nil {
				t.Error("a trashed product has a nil DeletedAt — the bin cannot say when it was trashed")
			}
		}
	}
	if !inBin {
		t.Error("the trashed product is not in the bin")
	}
}

// TestProductRestoreBringsItBackToTheCatalogue pins the restore half, including
// that the SAME row comes back (no copy, no rename) and that nothing is left in
// the bin.
func TestProductRestoreBringsItBackToTheCatalogue(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)
	prod := trashProduct(t, ctx, acc, "IT restore")

	if err := ps.Restore(ctx, acc, prod.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := ps.Get(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("Get after restore: %v", err)
	}
	if got.Name != prod.Name {
		t.Errorf("name after restore = %q, want %q (a restore is not a rename)", got.Name, prod.Name)
	}
	if got.DeletedAt != nil {
		t.Error("restored product still carries a deleted_at")
	}
	bin, err := ps.ListTrashed(ctx, []string{acc})
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	for _, x := range bin {
		if x.ID == prod.ID {
			t.Error("restored product is still in the bin")
		}
	}
}

// TestProductPurgeRefusesWhenTheProductWasSold is the guard copied from the form
// bin, for a stronger reason: `sales.subscription_product_id` is ON DELETE SET
// NULL, so the delete would SUCCEED and silently detach every sale from the
// product it records. A refusal by name is the only honest answer.
func TestProductPurgeRefusesWhenTheProductWasSold(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)
	prod := trashProduct(t, ctx, acc, "IT sold")

	// A sale whose LINE ITEM names the product. `items` is JSONB, so the guard
	// has to look inside it — a product id hidden in the items array is exactly
	// the reference a column-only check would miss.
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-TR-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1)), 1200)`,
		acc, prod.ID); err != nil {
		t.Fatalf("seed sale: %v", err)
	}

	n, err := ps.CountSalesByProduct(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("CountSalesByProduct: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountSalesByProduct = %d, want 1", n)
	}

	err = ps.Purge(ctx, acc, prod.ID)
	if !errors.Is(err, ErrProductHasSales) {
		t.Fatalf("Purge(sold) err = %v, want ErrProductHasSales", err)
	}
	// The refusal must leave the row ALONE — still in the bin, not half-deleted.
	got, err := ps.GetTrashed(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("GetTrashed after refused purge: %v", err)
	}
	if got == nil {
		t.Fatal("a refused purge still removed the product")
	}
	// And the sale is untouched.
	var sales int64
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM sales WHERE account_id=$1`, acc).Scan(&sales); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if sales != 1 {
		t.Errorf("sales after refused purge = %d, want 1 (history is not destroyed)", sales)
	}
}

// TestProductPurgeRefusesWhileASubscriptionNeedsIt covers the second obstacle.
// subscriptions.product_id is ON DELETE RESTRICT, so without this named refusal
// the purge would surface as a raw SQLSTATE 23503 and a 500 — the caller would
// not be told which commitment to clear.
func TestProductPurgeRefusesWhileASubscriptionNeedsIt(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)

	sub := &Product{
		AccountID: acc, Name: "IT subbed", PriceSen: 2500, Currency: "MYR",
		Type: "subscription", BillingPeriod: "month",
	}
	if err := ps.Create(ctx, sub); err != nil {
		t.Fatalf("create subscription product: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, chip_token_purchase_id,
		                            period, amount_sen, next_charge_at)
		 VALUES ($1, 'it-sub@example.test', $2, 'tok-' || md5(random()::text), 'month', 2500, now())`,
		acc, sub.ID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	if err := ps.SoftDelete(ctx, acc, sub.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	n, err := ps.CountSubscriptionsByProduct(ctx, acc, sub.ID)
	if err != nil {
		t.Fatalf("CountSubscriptionsByProduct: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountSubscriptionsByProduct = %d, want 1", n)
	}

	err = ps.Purge(ctx, acc, sub.ID)
	if !errors.Is(err, ErrProductHasSubscriptions) {
		t.Fatalf("Purge(subbed) err = %v, want ErrProductHasSubscriptions "+
			"(the FK is ON DELETE RESTRICT; without this the caller gets a raw 23503)", err)
	}
	if got, err := ps.GetTrashed(ctx, acc, sub.ID); err != nil || got == nil {
		t.Fatalf("refused purge removed the row: got=%v err=%v", got, err)
	}
}

// TestProductPurgeRemovesAnUnusedProductAndReleasesTheBinRow is the success path,
// and the control for the two refusals above: a product nobody bought or
// subscribed to really does disappear, and disappears from the bin.
func TestProductPurgeRemovesAnUnusedProductAndReleasesTheBinRow(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)
	prod := trashProduct(t, ctx, acc, "IT purgeable")

	if err := ps.Purge(ctx, acc, prod.ID); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if got, err := ps.GetTrashed(ctx, acc, prod.ID); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("GetTrashed(purged) = (%v, %v), want ErrProductNotFound — the row must be gone for good", got, err)
	}
	bin, err := ps.ListTrashed(ctx, []string{acc})
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	for _, x := range bin {
		if x.ID == prod.ID {
			t.Error("purged product is still listed in the bin")
		}
	}
}

// TestProductPurgeRefusesALiveProduct is the form bin's other guard: the ONLY way
// into a permanent delete is through the bin, so a single mistyped call cannot
// destroy a product that is still being sold.
func TestProductPurgeRefusesALiveProduct(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)

	live := &Product{AccountID: acc, Name: "IT live", PriceSen: 100, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, live); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ps.Purge(ctx, acc, live.ID); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("Purge(live) err = %v, want ErrProductNotFound (the bin is the only way in)", err)
	}
	if got, err := ps.Get(ctx, acc, live.ID); err != nil || got == nil {
		t.Fatalf("a refused purge of a LIVE product removed it: got=%v err=%v", got, err)
	}
}

// TestProductTrashIsAccountScoped: the bin is a read like any other, so another
// account's trashed product is neither listed nor restorable nor purgeable.
func TestProductTrashIsAccountScoped(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	mine := uniqueAccount(t, ctx, p)
	theirs := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)

	foreign := trashProduct(t, ctx, theirs, "IT foreign")

	bin, err := ps.ListTrashed(ctx, []string{mine})
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	for _, x := range bin {
		if x.ID == foreign.ID {
			t.Error("another account's trashed product is listed in my bin")
		}
	}
	if err := ps.Restore(ctx, mine, foreign.ID); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("Restore(foreign) err = %v, want ErrProductNotFound", err)
	}
	if err := ps.Purge(ctx, mine, foreign.ID); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("Purge(foreign) err = %v, want ErrProductNotFound", err)
	}
	// It is still there, still trashed, for its owner.
	if got, err := ps.GetTrashed(ctx, theirs, foreign.ID); err != nil || got == nil {
		t.Errorf("foreign product was affected by another account's calls: got=%v err=%v", got, err)
	}
}

// TestProductSoftDeleteIsIdempotentOnTheBin: a second delete of an already
// trashed product is a 404, not a silent success — the same rule the form bin
// uses, so the API can answer truthfully about what it did.
func TestProductSoftDeleteIsIdempotentOnTheBin(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)
	prod := trashProduct(t, ctx, acc, "IT twice")

	if err := ps.SoftDelete(ctx, acc, prod.ID); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("second SoftDelete err = %v, want ErrProductNotFound", err)
	}
	// A non-uuid id must be the same answer, never a raw driver error (22P02).
	if err := ps.SoftDelete(ctx, acc, "not-a-uuid"); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("SoftDelete(non-uuid) err = %v, want ErrProductNotFound", err)
	}
	if err := ps.Restore(ctx, acc, "not-a-uuid"); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("Restore(non-uuid) err = %v, want ErrProductNotFound", err)
	}
}

// TestProductSetActiveDoesNotReachIntoTheBin pins a subtler rule: archiving is a
// catalogue operation, so it must not edit a row the merchant believes is gone.
// Without the `deleted_at IS NULL` filter, a bulk archive would quietly rewrite
// trashed rows and a restore would bring back a product with a different active
// flag than the one that went in.
func TestProductSetActiveDoesNotReachIntoTheBin(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)
	prod := trashProduct(t, ctx, acc, "IT bin-edit")

	if err := ps.SetActive(ctx, acc, prod.ID, false); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("SetActive(trashed) err = %v, want ErrProductNotFound", err)
	}
	if err := ps.Archive(ctx, acc, prod.ID); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("Archive(trashed) err = %v, want ErrProductNotFound", err)
	}
	n, err := ps.ArchiveMany(ctx, acc, []string{prod.ID})
	if err != nil {
		t.Fatalf("ArchiveMany: %v", err)
	}
	if n != 0 {
		t.Errorf("ArchiveMany touched %d trashed product(s), want 0", n)
	}
	// The trashed row keeps its own active flag.
	got, err := ps.GetTrashed(ctx, acc, prod.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTrashed: got=%v err=%v", got, err)
	}
	if !got.Active {
		t.Error("a catalogue operation changed the active flag of a product in the bin")
	}
}

// TestProductBinCountsAreOneQueryAndMatchThePurgeGuards proves the bin listing's
// pre-computed can_purge answer is derived from the SAME references the purge
// checks. A bin row that says can_purge=true while the purge refuses would make
// the button lie — and, because the refusal states its own NUMBER, a bin count
// that is merely non-zero while the refusal is exact is still a lie: the
// merchant reads `sales: 2` and then the 409 says the product has 1 sale.
//
// So every row is compared to the purge's OWN per-product answer
// (CountSalesByProduct / CountSubscriptionsByProduct), not just to zero, and
// the fixture carries the shape that broke that equality: a sale naming one
// product on BOTH subscription_product_id and as a line item, plus a second
// line item, so the bin's LATERAL emits TWO rows for ONE sale.
func TestProductBinCountsAreOneQueryAndMatchThePurgeGuards(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)

	sold := trashProduct(t, ctx, acc, "IT bin-sold")
	subbed := &Product{
		AccountID: acc, Name: "IT bin-subbed", PriceSen: 900, Currency: "MYR",
		Type: "subscription", BillingPeriod: "month",
	}
	if err := ps.Create(ctx, subbed); err != nil {
		t.Fatalf("create subbed product: %v", err)
	}
	if err := ps.SoftDelete(ctx, acc, subbed.ID); err != nil {
		t.Fatalf("soft delete subbed: %v", err)
	}
	// The issue-560 mixed cart: a subscription product that is ALSO a line item,
	// plus a second (one-time) line item.
	mixed := &Product{
		AccountID: acc, Name: "IT bin-mixed", PriceSen: 10000, Currency: "MYR",
		Type: "subscription", BillingPeriod: "month",
	}
	if err := ps.Create(ctx, mixed); err != nil {
		t.Fatalf("create mixed product: %v", err)
	}
	if err := ps.SoftDelete(ctx, acc, mixed.ID); err != nil {
		t.Fatalf("soft delete mixed: %v", err)
	}
	book := &Product{AccountID: acc, Name: "IT bin-book", PriceSen: 2500, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, book); err != nil {
		t.Fatalf("create book product: %v", err)
	}
	clean := trashProduct(t, ctx, acc, "IT bin-clean")

	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-TR-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1)), 1200)`,
		acc, sold.ID); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	// TWO sales name `mixed`, each exactly once: one mixed cart (mixed as a line
	// item AND on the subscription column, plus a second line item) and one
	// line-item-only sale. The bin must report 2 for it, not 3.
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen, subscription_product_id)
		 VALUES ($1, 'JF-TR-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1),
		                           jsonb_build_object('product_id', $3::text, 'qty', 1)),
		         12500, $4::uuid)`,
		acc, mixed.ID, book.ID, mixed.ID); err != nil {
		t.Fatalf("seed mixed-cart sale: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-TR-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1)), 10000)`,
		acc, mixed.ID); err != nil {
		t.Fatalf("seed second sale on the mixed product: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, chip_token_purchase_id,
		                            period, amount_sen, next_charge_at)
		 VALUES ($1, 'it-sub@example.test', $2, 'tok-' || md5(random()::text), 'month', 900, now())`,
		acc, subbed.ID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	sales, err := ps.SalesCountsByProduct(ctx, []string{acc})
	if err != nil {
		t.Fatalf("SalesCountsByProduct: %v", err)
	}
	subs, err := ps.SubscriptionCountsByProduct(ctx, []string{acc})
	if err != nil {
		t.Fatalf("SubscriptionCountsByProduct: %v", err)
	}

	// Every bin row is checked against the number the purge's own refusal would
	// print for that same product — the equality is the contract, so a count
	// that is merely non-zero does not pass.
	for _, tc := range []struct {
		name   string
		prod   *Product
		want   int64
		reason string
	}{
		{"one line-item sale", sold, 1, "can_purge would be true and the purge would refuse"},
		{"mixed cart + plain sale", mixed, 2, "one sale names this product on the subscription column AND as a line item; a per-ROW fold counts it twice and prints a number the 409 contradicts"},
		{"no sale", clean, 0, "a product with no sale must count 0, or can_purge is never true"},
	} {
		refusal, err := ps.CountSalesByProduct(ctx, acc, tc.prod.ID)
		if err != nil {
			t.Fatalf("CountSalesByProduct(%s): %v", tc.name, err)
		}
		if sales[tc.prod.ID] != refusal {
			t.Errorf("%s: bin sales_count=%d but the purge refusal says %d — the two numbers for one product disagree (%s)",
				tc.name, sales[tc.prod.ID], refusal, tc.reason)
		}
		if sales[tc.prod.ID] != tc.want {
			t.Errorf("%s: bin sales_count=%d, want %d (%s)", tc.name, sales[tc.prod.ID], tc.want, tc.reason)
		}
	}

	refusalSubs, err := ps.CountSubscriptionsByProduct(ctx, acc, subbed.ID)
	if err != nil {
		t.Fatalf("CountSubscriptionsByProduct: %v", err)
	}
	if subs[subbed.ID] != refusalSubs {
		t.Errorf("bin subscriptions_count=%d but the purge refusal says %d — the two numbers for one product disagree",
			subs[subbed.ID], refusalSubs)
	}
	if subs[subbed.ID] == 0 {
		t.Error("the subscribed product's subscription count is 0 — can_purge would be true and the purge would refuse")
	}
	if subs[clean.ID] != 0 {
		t.Errorf("a product with no subscription must count 0, got %d, or can_purge is never true", subs[clean.ID])
	}
}
