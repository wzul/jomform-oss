package dbstore

// Issue #98 regression integration tests for the shipped fixes that had no
// automated coverage:
//
//   - #16  form CRUD + soft-delete + publish (theme-null bug)
//   - #10  bulk form operations (SoftDeleteMany / SetStatusMany)
//   - #12  sales capture/release/skip_capture state machine
//   - #11  per-form sales scoping (ListByAccountForm / ListByAccountsFormPage)
//
// These run against a real Postgres (JOMFORM_TEST_DATABASE_URL) and are
// skipped when it is unset, mirroring the other dbstore integration tests.

import (
	"context"
	"testing"
)

// ─── #16: form CRUD + soft-delete + publish ────────────────────────────────

// TestFormCRUDSoftDeletePublish walks the full form lifecycle: create → read →
// publish → soft-delete (invisible to normal reads) → restore → unpublish.
func TestFormCRUDSoftDeletePublish(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := &Form{
		AccountID: acc, Slug: "it-crud-" + acc[:8], Title: "CRUD form",
		Status: "draft",
		Config: map[string]any{"blocks": []any{"a"}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := s.Create(ctx, f); err != nil {
		t.Fatalf("create: %v", err)
	}
	if f.ID == "" {
		t.Fatal("create did not assign an id")
	}

	// Read back.
	got, err := s.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "CRUD form" || got.Status != "draft" {
		t.Fatalf("read back = %+v", got)
	}

	// Publish.
	if err := s.SetStatus(ctx, acc, f.ID, "published"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	got, _ = s.GetByID(ctx, acc, f.ID)
	if got.Status != "published" {
		t.Fatalf("status after publish = %q, want published", got.Status)
	}

	// Soft-delete → invisible to normal reads.
	if err := s.SoftDelete(ctx, acc, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := s.GetByID(ctx, acc, f.ID); err == nil {
		t.Fatal("get after delete succeeded, want error (soft-deleted form invisible)")
	}
	// But visible in the trash.
	trashed, err := s.ListTrashed(ctx, acc)
	if err != nil {
		t.Fatalf("list trashed: %v", err)
	}
	found := false
	for _, tf := range trashed {
		if tf.ID == f.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("soft-deleted form not in trash list")
	}

	// Restore → visible again.
	if err := s.Restore(ctx, acc, f.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := s.GetByID(ctx, acc, f.ID); err != nil {
		t.Fatalf("get after restore: %v", err)
	}

	// Unpublish.
	if err := s.SetStatus(ctx, acc, f.ID, "draft"); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	got, _ = s.GetByID(ctx, acc, f.ID)
	if got.Status != "draft" {
		t.Fatalf("status after unpublish = %q, want draft", got.Status)
	}
}

// TestFormSoftDeleteScopedToAccount: soft-deleting a form under one account
// must not affect a same-id form under another account, and a foreign account
// cannot delete/restore a form it does not own.
func TestFormSoftDeleteScopedToAccount(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	accA := uniqueAccount(t, ctx, p)
	accB := uniqueAccount(t, ctx, p)

	fa := &Form{AccountID: accA, Slug: "it-scope-a-" + accA[:8], Title: "A", Status: "draft"}
	if err := s.Create(ctx, fa); err != nil {
		t.Fatalf("create A: %v", err)
	}
	// Foreign account cannot delete A.
	if err := s.SoftDelete(ctx, accB, fa.ID); err != ErrFormNotFound {
		t.Fatalf("foreign soft-delete err = %v, want ErrFormNotFound", err)
	}
	// Owner can.
	if err := s.SoftDelete(ctx, accA, fa.ID); err != nil {
		t.Fatalf("owner soft-delete: %v", err)
	}
	// Foreign account cannot restore A.
	if err := s.Restore(ctx, accB, fa.ID); err != ErrFormNotFound {
		t.Fatalf("foreign restore err = %v, want ErrFormNotFound", err)
	}
}

// ─── #10: bulk form operations ────────────────────────────────────────────

// TestFormBulkSoftDeleteAndPublish: SoftDeleteMany and SetStatusMany operate
// only on the account's live forms, return the affected count, and skip
// foreign/unknown ids.
func TestFormBulkSoftDeleteAndPublish(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)
	other := uniqueAccount(t, ctx, p)

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		f := &Form{AccountID: acc, Slug: "it-bulk-" + acc[:8] + "-" + string(rune('a'+i)), Title: "B", Status: "draft"}
		if err := s.Create(ctx, f); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids = append(ids, f.ID)
	}
	// A foreign form that must NOT be touched.
	fo := &Form{AccountID: other, Slug: "it-bulk-other-" + other[:8], Title: "O", Status: "draft"}
	if err := s.Create(ctx, fo); err != nil {
		t.Fatalf("create foreign: %v", err)
	}

	// Bulk publish 2 of the 3 + a foreign id (ignored).
	n, err := s.SetStatusMany(ctx, acc, []string{ids[0], ids[1], fo.ID}, "published")
	if err != nil {
		t.Fatalf("set status many: %v", err)
	}
	if n != 2 {
		t.Fatalf("SetStatusMany affected = %d, want 2 (foreign id ignored)", n)
	}
	for _, id := range ids[:2] {
		f, _ := s.GetByID(ctx, acc, id)
		if f.Status != "published" {
			t.Fatalf("form %s status = %q, want published", id, f.Status)
		}
	}
	// The third stays draft.
	f3, _ := s.GetByID(ctx, acc, ids[2])
	if f3.Status != "draft" {
		t.Fatalf("form %s status = %q, want draft", ids[2], f3.Status)
	}
	// Foreign form untouched.
	fo2, _ := s.GetByID(ctx, other, fo.ID)
	if fo2.Status != "draft" {
		t.Fatalf("foreign form status = %q, want draft", fo2.Status)
	}

	// Bulk soft-delete all 3.
	n, err = s.SoftDeleteMany(ctx, acc, ids)
	if err != nil {
		t.Fatalf("soft delete many: %v", err)
	}
	if n != 3 {
		t.Fatalf("SoftDeleteMany affected = %d, want 3", n)
	}
	// All gone from normal reads.
	all, err := s.ListByAccount(ctx, acc)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("list after bulk delete = %d, want 0", len(all))
	}
	// All in trash.
	trashed, _ := s.ListTrashed(ctx, acc)
	if len(trashed) != 3 {
		t.Fatalf("trash after bulk delete = %d, want 3", len(trashed))
	}
}

// ─── #12: sales capture/release/skip_capture state machine ─────────────────

// TestSaleCaptureStateMachine: a skip_capture sale starts pending+unauthorized,
// is listed as awaiting capture, then MarkCaptured flips it to paid+authorized
// with a captured_at timestamp and removes it from the awaiting queue.
func TestSaleCaptureStateMachine(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	sale := &Sale{
		AccountID: acc, RefCode: "IT-CAP-" + acc[:8],
		Client: SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:  []SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: SaleTypeOneTime, PaymentMethod: "chip",
		Status: SalePending, SkipCapture: true,
	}
	if err := s.Create(ctx, sale); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Attach a CHIP purchase id (required for the awaiting-capture queue).
	if err := s.AttachChipPurchase(ctx, sale.ID, "pur_cap_1", "https://gate.example/checkout/pur_cap_1"); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	// Initially pending, not authorized, no captured_at.
	got, err := s.ByRefCode(ctx, sale.RefCode)
	if err != nil {
		t.Fatalf("by ref: %v", err)
	}
	if got.Status != SalePending || got.Authorized || got.CapturedAt != nil {
		t.Fatalf("initial state = status=%q authorized=%v captured=%v", got.Status, got.Authorized, got.CapturedAt)
	}

	// Listed as awaiting capture.
	awaiting, err := s.ListAwaitingCapture(ctx, acc)
	if err != nil {
		t.Fatalf("list awaiting: %v", err)
	}
	if len(awaiting) != 1 || awaiting[0].ID != sale.ID {
		t.Fatalf("awaiting = %d rows, want 1 for this sale", len(awaiting))
	}

	// Capture → paid + authorized + captured_at set.
	if err := s.MarkCaptured(ctx, sale.ID); err != nil {
		t.Fatalf("mark captured: %v", err)
	}
	got, _ = s.ByRefCode(ctx, sale.RefCode)
	if got.Status != SalePaid || !got.Authorized || got.CapturedAt == nil {
		t.Fatalf("after capture = status=%q authorized=%v captured=%v", got.Status, got.Authorized, got.CapturedAt)
	}
	// No longer awaiting capture.
	awaiting, _ = s.ListAwaitingCapture(ctx, acc)
	if len(awaiting) != 0 {
		t.Fatalf("awaiting after capture = %d, want 0", len(awaiting))
	}
}

// TestSaleReleaseStateMachine: releasing a skip_capture sale (voiding the
// authorization) marks it cancelled and removes it from the awaiting queue.
func TestSaleReleaseStateMachine(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	sale := &Sale{
		AccountID: acc, RefCode: "IT-REL-" + acc[:8],
		Client: SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:  []SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: SaleTypeOneTime, PaymentMethod: "chip",
		Status: SalePending, SkipCapture: true,
	}
	if err := s.Create(ctx, sale); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AttachChipPurchase(ctx, sale.ID, "pur_rel_1", "https://gate.example/checkout/pur_rel_1"); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	// Release = void the authorization → cancelled.
	if err := s.SetStatus(ctx, sale.ID, SaleCancelled); err != nil {
		t.Fatalf("release (set cancelled): %v", err)
	}
	got, _ := s.ByRefCode(ctx, sale.RefCode)
	if got.Status != SaleCancelled {
		t.Fatalf("status after release = %q, want cancelled", got.Status)
	}
	// Cancelled sales are not awaiting capture.
	awaiting, _ := s.ListAwaitingCapture(ctx, acc)
	if len(awaiting) != 0 {
		t.Fatalf("awaiting after release = %d, want 0", len(awaiting))
	}
}

// TestSaleAwaitingCaptureExcludesNonSkipCapture: a normal (non-skip_capture)
// sale is never listed as awaiting capture even with a CHIP purchase attached.
func TestSaleAwaitingCaptureExcludesNonSkipCapture(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	sale := &Sale{
		AccountID: acc, RefCode: "IT-NOSKIP-" + acc[:8],
		Client: SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:  []SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: SaleTypeOneTime, PaymentMethod: "chip",
		Status: SalePaid, SkipCapture: false,
	}
	if err := s.Create(ctx, sale); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AttachChipPurchase(ctx, sale.ID, "pur_noskip_1", "https://gate.example/checkout/pur_noskip_1"); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}
	awaiting, err := s.ListAwaitingCapture(ctx, acc)
	if err != nil {
		t.Fatalf("list awaiting: %v", err)
	}
	if len(awaiting) != 0 {
		t.Fatalf("awaiting = %d, want 0 (non-skip_capture sale must not appear)", len(awaiting))
	}
}

// ─── #11: per-form sales scoping ───────────────────────────────────────────

// TestSalePerFormScoping: ListByAccountForm returns only the sales for the
// given form within an account, and ListByAccountsFormPage pages across a set
// of accounts for a single form.
func TestSalePerFormScoping(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	s := NewSaleStore(p)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	// Real form rows (the sales.form_id column has an FK to forms).
	mkForm := func(slug string) string {
		f := &Form{AccountID: acc, Slug: slug, Title: "F", Status: "published"}
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", slug, err)
		}
		return f.ID
	}
	formA := mkForm("it-scope-fa-" + acc[:8])
	formB := mkForm("it-scope-fb-" + acc[:8])

	mk := func(formID, ref string) *Sale {
		return &Sale{
			AccountID: acc, FormID: formID, RefCode: ref,
			Client: SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
			Items:  []SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
			TotalSen: 1000, SaleType: SaleTypeOneTime, PaymentMethod: "chip",
			Status: SaleConfirmed,
		}
	}
	for _, ref := range []string{"IT-FA-1", "IT-FA-2"} {
		if err := s.Create(ctx, mk(formA, ref+"-"+acc[:8])); err != nil {
			t.Fatalf("create %s: %v", ref, err)
		}
	}
	if err := s.Create(ctx, mk(formB, "IT-FB-1-"+acc[:8])); err != nil {
		t.Fatalf("create formB: %v", err)
	}

	// Per-form list: only form A's two sales.
	fa, err := s.ListByAccountForm(ctx, acc, formA, 50)
	if err != nil {
		t.Fatalf("list by form: %v", err)
	}
	if len(fa) != 2 {
		t.Fatalf("form A sales = %d, want 2", len(fa))
	}
	fb, _ := s.ListByAccountForm(ctx, acc, formB, 50)
	if len(fb) != 1 {
		t.Fatalf("form B sales = %d, want 1", len(fb))
	}

	// Paged across accounts for a single form.
	page, total, err := s.ListByAccountsFormPage(ctx, []string{acc}, formA, 1, 10)
	if err != nil {
		t.Fatalf("list by accounts form page: %v", err)
	}
	if total != 2 || len(page) != 2 {
		t.Fatalf("form A page total=%d len=%d, want 2/2", total, len(page))
	}
	// A form with no sales yields an empty page, not an error.
	empty, total, err := s.ListByAccountsFormPage(ctx, []string{acc}, "33333333-3333-3333-3333-333333333333", 1, 10)
	if err != nil {
		t.Fatalf("empty form page: %v", err)
	}
	if total != 0 || len(empty) != 0 {
		t.Fatalf("empty form total=%d len=%d, want 0/0", total, len(empty))
	}
}
