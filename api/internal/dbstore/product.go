package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Product models the products table.
type Product struct {
	ID          string
	AccountID   string
	Name        string
	Description string
	PriceSen    int64
	// Currency is the currency the product's price is denominated in (issue
	// #185). Set from the workspace's currency at creation; a later workspace currency
	// change applies to new products going forward (existing prices keep their
	// original currency).
	Currency            string
	Images              []string
	Stock               *int
	Type                string // one_time | subscription
	BillingPeriod       string // month | year when subscription
	TrialDays           int
	SubscriptionCharges *int // NULL = infinite; N = stop after Nth charge
	// ScheduleMode selects the next-charge date policy after a SUCCESSFUL charge
	// (round 13c). "from_payment" (default) = from the moment it succeeded;
	// "catch_up" = planned + 1 period (a missed period is still collected);
	// "skip_missed" = the next planned date still in the future.
	ScheduleMode string
	// Active is "on sale / not on sale" — a DIFFERENT axis from the trash.
	// A product can be archived (active=false) and live, or active and in the
	// bin, and the two are deliberately kept apart: conflating them is why a
	// product the merchant meant to remove used to keep counting as merely
	// archived.
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt is non-nil only for a product in the TRASH. The live reads all
	// filter `deleted_at IS NULL`, so a nil value is how a caller tells a live
	// product from a trashed one without a second query.
	DeletedAt *time.Time
}

var ErrProductNotFound = errors.New("product not found")

// ErrProductHasSales refuses a permanent purge of a product that was ever sold.
// `sales.subscription_product_id` is ON DELETE SET NULL and a sale's line items
// live in `sales.items` JSONB, so the delete itself would succeed and silently
// detach every sale from the product it records. Sales are history: history is
// never destroyed by deleting the thing that produced it.
var ErrProductHasSales = errors.New("produk ini mempunyai jualan")

// ErrProductHasSubscriptions refuses a permanent purge of a product a customer
// is still subscribed to. `subscriptions.product_id` is ON DELETE RESTRICT, so
// without this named refusal the delete would surface as a raw SQLSTATE 23503
// and a 500 — a billing commitment is not something a purge may quietly take
// with it, and the caller is told which obstacle to clear.
var ErrProductHasSubscriptions = errors.New("produk ini masih ada langganan aktif")

// ProductStore wraps pgx for products.
type ProductStore struct{ pool *pgxpool.Pool }

// NewProductStore builds a ProductStore.
func NewProductStore(pool *pgxpool.Pool) *ProductStore { return &ProductStore{pool: pool} }

// productSelect builds every non-trash product read. This mirrors formSelect:
// deleted_at is part of the column list (every product read scans into a
// Product, and the migration/scan-drift guard pins a select's column count to
// that struct's field count), and the WHERE filters to LIVE rows so a trashed
// product is invisible everywhere except the dedicated trash query.
const productSelect = `SELECT id, account_id, name, COALESCE(description,''), price_sen, currency,
	images, stock, type, COALESCE(billing_period,''), trial_days, subscription_charges, active, created_at, updated_at,
	COALESCE(schedule_mode, 'from_payment'), deleted_at
	FROM products WHERE deleted_at IS NULL`

// scanProductFull scans a productSelect row (the shared destination list, so a
// column added to the select cannot be missing from one of the call sites).
//
// It is a method on ProductStore rather than a package-level func for the same
// reason FormStore.scanOne is: the migration/scan-drift guard
// (migration_scan_drift_test.go) registers the SCAN FUNCTIONS of each *Select
// constant and requires the `Scan(` to be inside the registered function
// itself, so it can count the destinations. A helper that the registered
// functions merely CALL would leave the guard with nothing to count and it
// would fail the build on clean code.
func (s *ProductStore) scanProductFull(row rowScanner) (*Product, error) {
	q := &Product{}
	err := row.Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen, &q.Currency,
		&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.SubscriptionCharges,
		&q.Active, &q.CreatedAt, &q.UpdatedAt, &q.ScheduleMode, &q.DeletedAt)
	return q, err
}

// Create inserts a product.
func (s *ProductStore) Create(ctx context.Context, p *Product) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO products (account_id, name, description, price_sen, currency, type, billing_period, trial_days, subscription_charges, stock, images, schedule_mode)
		 VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,COALESCE($11::jsonb,'[]'::jsonb),COALESCE(NULLIF($12,''),'from_payment'))
		 RETURNING id, created_at, updated_at`,
		p.AccountID, p.Name, p.Description, p.PriceSen, orDefault(p.Currency, "MYR"), p.Type, p.BillingPeriod,
		p.TrialDays, p.SubscriptionCharges, p.Stock, p.Images, p.ScheduleMode,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

// ListActiveByAccount lists active products.
func (s *ProductStore) ListActiveByAccount(ctx context.Context, accountID string) ([]*Product, error) {
	rows, err := s.pool.Query(ctx, productSelect+" AND account_id=$1 AND active ORDER BY created_at DESC", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Product
	for rows.Next() {
		q, serr := s.scanProductFull(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// ListActiveByAccounts lists active products across a set of accounts
// (e.g. every account in the workspace the dashboard user selected).
func (s *ProductStore) ListActiveByAccounts(ctx context.Context, accountIDs []string) ([]*Product, error) {
	if len(accountIDs) == 0 {
		return []*Product{}, nil
	}
	rows, err := s.pool.Query(ctx,
		productSelect+" AND account_id = ANY($1) AND active ORDER BY created_at DESC", accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Product{}
	for rows.Next() {
		q, serr := s.scanProductFull(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// ListAllByAccounts lists ALL live products (active + archived) across a set of
// accounts — for the WooCommerce-style admin list that shows a status column
// and lets the owner toggle products back to active. Sorted newest first.
//
// A TRASHED product is deliberately excluded: "archived" (active=false, still
// in the catalogue) and "in the bin" (deleted_at set) are different states, and
// the admin list is the catalogue, not the bin. The bin has its own read.
func (s *ProductStore) ListAllByAccounts(ctx context.Context, accountIDs []string) ([]*Product, error) {
	if len(accountIDs) == 0 {
		return []*Product{}, nil
	}
	rows, err := s.pool.Query(ctx,
		productSelect+" AND account_id = ANY($1) ORDER BY created_at DESC", accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Product{}
	for rows.Next() {
		q, serr := s.scanProductFull(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// SetActive archives/restores a product. A miss — no such product, another
// account's, or an id that cannot be a UUID at all (SQLSTATE 22P02) — is
// ErrProductNotFound rather than a raw driver error (see isNoSuchRow).
//
// It only reaches LIVING products: a product in the bin is not "archived", it
// is trashed, and flipping its active flag from the catalogue view would be a
// silent edit of a row the merchant believes is gone. Restore it first.
func (s *ProductStore) SetActive(ctx context.Context, accountID, productID string, active bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET active=$3, updated_at=now() WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, productID, active)
	if isNoSuchRow(err) {
		return ErrProductNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// Get fetches one product scoped to the account. A miss — trashed, another
// account's product, or an id that cannot name a row — is ErrProductNotFound,
// never a raw driver error (see isNoSuchRow).
func (s *ProductStore) Get(ctx context.Context, accountID, productID string) (*Product, error) {
	q, err := s.scanProductFull(s.pool.QueryRow(ctx, productSelect+" AND id=$2 AND account_id=$1", accountID, productID))
	if isNoSuchRow(err) {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, err
	}
	return q, nil
}

var _ = pgxpool.Pool{} // ensure pgxpool import remains for future stores in this package

// Archive sets active=false on a product owned by the account. An id that
// names no row the account owns — never existed, another account's, trashed, or
// not a UUID at all (SQLSTATE 22P02) — is ErrProductNotFound, so DELETE
// /v1/rest/products/{id} answers 404 rather than 200 ("archived") for a row
// nothing archived, or 500 for a malformed id (see isNoSuchRow).
//
// NOTE for callers: archiving is NOT the bin. It takes the product off sale and
// leaves it in the catalogue; the bin is SoftDelete.
func (s *ProductStore) Archive(ctx context.Context, accountID, productID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET active=false, updated_at=now() WHERE id=$1 AND account_id=$2 AND deleted_at IS NULL`,
		productID, accountID)
	if isNoSuchRow(err) {
		return ErrProductNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// ArchiveMany archives many products at once; returns how many ids actually
// updated (account-scoped). Mirrors FormStore.SoftDeleteMany for the forms bulk
// pattern (#10). Trashed rows are not eligible.
func (s *ProductStore) ArchiveMany(ctx context.Context, accountID string, productIDs []string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET active=false, updated_at=now()
		 WHERE account_id=$1 AND active AND deleted_at IS NULL AND id = ANY($2)`,
		accountID, productIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── Tong sampah (trash): soft delete / list / restore / purge ────────────────
//
// The SAME shared shape forms already use (api/internal/dbstore/form.go):
//
//	SoftDelete — marks deleted_at. Destroys nothing, so it is reversible.
//	ListTrashed — the dedicated trash read. NEVER a relaxed filter on the live
//	              read: appending `deleted_at IS NOT NULL` to productSelect would
//	              make that query self-contradictory and the bin would always be
//	              empty (the issue #98 regression class).
//	GetTrashed — one trashed product, account-scoped, so it cannot be used as a
//	              side door to a live row.
//	Restore    — clears deleted_at.
//	Purge      — destroys the row for good, with the guards below.

// SoftDelete moves a product to the bin (sets deleted_at). Returns
// ErrProductNotFound when the row is unknown, another account's, or already
// trashed — so a second delete is a 404 rather than a silent success.
func (s *ProductStore) SoftDelete(ctx context.Context, accountID, productID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET deleted_at=now(), updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NULL`,
		accountID, productID)
	if isNoSuchRow(err) {
		return ErrProductNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// trashedProductSelect builds the two trash reads (ListTrashed / GetTrashed).
// It is a SEPARATE constant from productSelect, and its filter is the OPPOSITE
// one, on purpose — see the block comment above.
const trashedProductSelect = `SELECT id, account_id, name, COALESCE(description,''), price_sen, currency,
	images, stock, type, COALESCE(billing_period,''), trial_days, subscription_charges, active, created_at, updated_at,
	COALESCE(schedule_mode, 'from_payment'), deleted_at
	FROM products WHERE deleted_at IS NOT NULL`

// ListTrashed lists a set of accounts' trashed products, newest trash first.
//
// It takes a SLICE of account ids, matching every other product read here
// (ListActiveByAccounts / ListAllByAccounts): a dashboard user sees the products
// of the workspaces they can reach, so the bin must be read the same way or it
// would silently list only one workspace.
func (s *ProductStore) ListTrashed(ctx context.Context, accountIDs []string) ([]*Product, error) {
	if len(accountIDs) == 0 {
		return []*Product{}, nil
	}
	rows, err := s.pool.Query(ctx,
		trashedProductSelect+" AND account_id = ANY($1) ORDER BY deleted_at DESC", accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Product{}
	for rows.Next() {
		q, serr := s.scanProductFull(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// GetTrashed reads ONE trashed product, account-scoped. A live product, another
// account's, or an unknown id is ErrProductNotFound — so this cannot be used as
// a side door to read or act on a live product, and it is the read the purge
// confirmation is checked against.
func (s *ProductStore) GetTrashed(ctx context.Context, accountID, productID string) (*Product, error) {
	q, err := s.scanProductFull(s.pool.QueryRow(ctx,
		trashedProductSelect+" AND id=$2 AND account_id=$1", accountID, productID))
	if isNoSuchRow(err) {
		return nil, ErrProductNotFound
	}
	if err != nil {
		return nil, err
	}
	return q, nil
}

// Restore clears deleted_at on a trashed product.
//
// Unlike FormStore.Restore there is NO collision case to refuse: products have
// no slug and no unique index, so trashing a product releases no name and
// restoring one cannot violate anything. The signature still exists so every
// object in the bin restores the same way.
func (s *ProductStore) Restore(ctx context.Context, accountID, productID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET deleted_at=NULL, updated_at=now()
		 WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL`,
		accountID, productID)
	if isNoSuchRow(err) {
		return ErrProductNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// saleItemsArray renders `sales.items` (the column named by `col`) as a JSON
// array, whatever the column actually holds.
//
// Why this exists: `sales.items` is `JSONB NOT NULL` with NO CHECK constraint
// (000001_init.up.sql), and `jsonb_array_elements` raises SQLSTATE 22023
// ("cannot extract elements from an object"/"…from a scalar") on anything that
// is not an array. Every application write path serialises a Go slice, so a
// non-array value can only come from a direct/external write — but ONE such row
// is enough to matter, because the product bin calls SalesCountsByProduct ONCE
// for the WHOLE account and fails the request on its error: the merchant then
// cannot read the bin for any product they own. ProductAnalytics expands the
// same column and has the same exposure.
//
// Treating a non-array as empty is the tolerant reading: the row keeps its OTHER
// reference (subscription_product_id, a real column) and every other product in
// the account stays readable.
//
// The test is `jsonb_typeof(col) = 'array'`, NOT `COALESCE(col, '[]'::jsonb)`:
// COALESCE only replaces NULL, and it is precisely a NON-NULL non-array value
// (an object, a string, a number) that makes the extraction raise. The guard has
// to ask what the value IS, not whether it is present. (The column is also
// NOT NULL, so COALESCE could never fire here anyway.)
//
// Keep EVERY expansion of `sales.items` going through this helper — a bare
// `jsonb_array_elements(<col>)` on that column reintroduces the account-wide
// failure it exists to prevent.
func saleItemsArray(col string) string {
	return "CASE WHEN jsonb_typeof(" + col + ") = 'array' THEN " + col + " ELSE '[]'::jsonb END"
}

// CountSalesByProduct reports how many sales reference a product (account-scoped).
// Used by the purge guard so the refusal can state the number.
//
// Two references exist and BOTH are counted: the JSONB line items every sale
// carries (`items[].product_id`) and the subscription-terms column added by
// 000056. Counting only one would let a purge slip past the other.
func (s *ProductStore) CountSalesByProduct(ctx context.Context, accountID, productID string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales
		  WHERE account_id=$1
		    AND (subscription_product_id=$2
		         OR EXISTS (SELECT 1 FROM jsonb_array_elements(` + saleItemsArray("items") + `) e
		                     WHERE e->>'product_id' = $2::text))`,
		accountID, productID).Scan(&n)
	return n, err
}

// CountSubscriptionsByProduct reports how many subscriptions reference a
// product (account-scoped, every status). Used by the purge guard: the FK is
// ON DELETE RESTRICT, so a surviving subscription would turn the purge into a
// raw SQLSTATE 23503.
func (s *ProductStore) CountSubscriptionsByProduct(ctx context.Context, accountID, productID string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM subscriptions WHERE account_id=$1 AND product_id=$2`,
		accountID, productID).Scan(&n)
	return n, err
}

// Purge permanently deletes ONE trashed product (and, through the FK already on
// the table, its variations — product_variations.product_id is ON DELETE
// CASCADE).
//
// Three guards, because a purge cannot be undone:
//
//  1. Only a TRASHED product may be purged (`deleted_at IS NOT NULL`), so the
//     only way in is bin → purge and a live product cannot be destroyed by a
//     single mistyped call. A live product is ErrProductNotFound here, exactly
//     as an unknown id is.
//  2. A product with any sale is refused with ErrProductHasSales. Sales are
//     history, and `sales.subscription_product_id` is ON DELETE SET NULL, so
//     the delete would succeed and silently detach the sale from the product it
//     records.
//  3. A product any subscription still points at is refused with
//     ErrProductHasSubscriptions. `subscriptions.product_id` is ON DELETE
//     RESTRICT, so without this named refusal the caller would get a raw 23503
//     and a 500 instead of being told what to clear.
func (s *ProductStore) Purge(ctx context.Context, accountID, productID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row so a concurrent restore cannot flip it live between the
	// trashed check and the delete.
	var name string
	if err := tx.QueryRow(ctx,
		`SELECT name FROM products
		  WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL
		  FOR UPDATE`,
		accountID, productID).Scan(&name); isNoSuchRow(err) {
		return ErrProductNotFound
	} else if err != nil {
		return err
	}

	var subs int64
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM subscriptions WHERE account_id=$1 AND product_id=$2`,
		accountID, productID).Scan(&subs); err != nil {
		return err
	}
	if subs > 0 {
		return ErrProductHasSubscriptions
	}

	var sales int64
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales
		  WHERE account_id=$1
		    AND (subscription_product_id=$2
		         OR EXISTS (SELECT 1 FROM jsonb_array_elements(` + saleItemsArray("items") + `) e
		                     WHERE e->>'product_id' = $2::text))`,
		accountID, productID).Scan(&sales); err != nil {
		return err
	}
	if sales > 0 {
		return ErrProductHasSales
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM products WHERE id=$2 AND account_id=$1 AND deleted_at IS NOT NULL`,
		accountID, productID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SalesCountsByProduct returns, for every product of the given accounts that has
// at least one sale, how many sales reference it — in ONE query, so the bin can
// show the count and decide `can_purge` per row instead of N+1 queries.
//
// The map is keyed by product id and omits products with zero sales (a missing
// key IS zero).
//
// A sale that references the SAME product twice (e.g. two lines of one product)
// counts ONCE, matching CountSalesByProduct — which counts sale rows, not
// references. Those two answers are compared against each other per row by the
// bin's tests, so a reference-count here would make `can_purge` disagree with
// the refusal the purge actually gives.
//
// NOTE: the referencing columns are read as ROWS of `sales` and folded in Go,
// deliberately NOT as a set-union SQL statement. The record-scope guard
// (sales_read_decision_guard_test.go) forbids that construct outside
// dbstore/records.go, because it is how a second definition of the merged
// record set gets hand-rolled — and this query does not need one: it is a plain
// read of a single table.
//
// The fold is keyed on the SALE ID, not on the row: the LATERAL emits one row
// per distinct line-item pid, so a sale that names a product on BOTH the
// subscription column and as a line item (plus any second line item) arrives as
// two rows carrying the same subscription_product_id. A per-row set would count
// that one sale twice for that product and print a number the purge's own 409
// contradicts; the set is therefore reset when the sale id changes, and
// `ORDER BY s.id` is what makes each sale's rows contiguous for it.
func (s *ProductStore) SalesCountsByProduct(ctx context.Context, accountIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT s.id::text, s.subscription_product_id::text, e.pid
		   FROM sales s
		   LEFT JOIN LATERAL (
		     SELECT DISTINCT e->>'product_id' AS pid
		       FROM jsonb_array_elements(` + saleItemsArray("s.items") + `) e
		      WHERE e->>'product_id' IS NOT NULL
		   ) e ON true
		  WHERE s.account_id = ANY($1)
		  ORDER BY s.id`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var curSale string
	seen := map[string]bool{}
	for rows.Next() {
		var saleID string
		var subPID, itemPID *string
		if err := rows.Scan(&saleID, &subPID, &itemPID); err != nil {
			return nil, err
		}
		// A set per SALE (not per row): a product named on both the
		// subscription column and a line item still counts once for that sale.
		if saleID != curSale {
			curSale, seen = saleID, map[string]bool{}
		}
		for _, pid := range []*string{subPID, itemPID} {
			if pid == nil || *pid == "" || seen[*pid] {
				continue
			}
			seen[*pid] = true
			out[*pid]++
		}
	}
	return out, rows.Err()
}

// SubscriptionCountsByProduct is SalesCountsByProduct's counterpart for the
// subscriptions FK, so the bin can show which rows a purge would have to refuse
// before the caller clicks.
func (s *ProductStore) SubscriptionCountsByProduct(ctx context.Context, accountIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT product_id, COUNT(*) FROM subscriptions
		  WHERE account_id = ANY($1) GROUP BY product_id`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		var n int64
		if err := rows.Scan(&pid, &n); err != nil {
			return nil, err
		}
		out[pid] = n
	}
	return out, rows.Err()
}

// DecrementStock atomically reduces stock for a limited product.
// Returns ErrOutOfStock when the product is sold out. Unlimited (NULL) always ok.
func (s *ProductStore) DecrementStock(ctx context.Context, productID string, qty int) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET stock = stock - $2
		 WHERE id=$1 AND stock IS NOT NULL AND stock >= $2`, productID, qty)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// either unlimited product (fine) or insufficient stock
		var stock *int
		if err := s.pool.QueryRow(ctx,
			`SELECT stock FROM products WHERE id=$1`, productID).Scan(&stock); err != nil {
			return err
		}
		if stock != nil && *stock < qty {
			return ErrOutOfStock
		}
	}
	return nil
}

// ErrOutOfStock rejects submissions past available inventory.
var ErrOutOfStock = errors.New("produk telah habis (out of stock)")

// Update modifies a product's editable fields (nil = unchanged).
// Fields covered: name, description, price_sen, images[], stock, active, type,
// billing_period, trial_days, subscription_charges. Money stays integer sen.
// When type is set to one_time, billing_period is forced NULL (a one-time
// product can't carry a period); when type is subscription a caller-supplied
// billing_period is applied, otherwise the stored one is kept. images == nil
// leaves the stored images untouched; a non-nil (possibly empty) slice
// replaces them wholesale. subscriptionCharges is a pointer-to-pointer: nil =
// keep unchanged, non-nil pointing to nil = set NULL (infinite), non-nil
// pointing to a value = set that fixed charge count.
func (s *ProductStore) Update(ctx context.Context, accountID, productID string,
	name, description *string, priceSen *int64, images *[]string, stock *int,
	active *bool, typ, billingPeriod *string, trialDays *int, subscriptionCharges **int,
	scheduleMode *string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET
		   name = COALESCE($3, name),
		   description = COALESCE($4, description),
		   price_sen = COALESCE($5, price_sen),
		   images = CASE WHEN $6::boolean THEN $7::jsonb ELSE images END,
		   stock = CASE WHEN $8::boolean THEN $9 ELSE stock END,
		   type = COALESCE($10, type),
		   billing_period = CASE WHEN COALESCE($10, type) = 'one_time' THEN NULL ELSE COALESCE($11, billing_period) END,
		   trial_days = COALESCE($12, trial_days),
		   active = COALESCE($13, active),
		   subscription_charges = CASE WHEN $14::boolean THEN $15 ELSE subscription_charges END,
		   schedule_mode = COALESCE(NULLIF($16,''), schedule_mode),
		   updated_at = now()
		 WHERE id=$2 AND account_id=$1`,
		accountID, productID, name, description, priceSen, images != nil, images,
		stock != nil, stock, typ, billingPeriod, trialDays, active,
		subscriptionCharges != nil, derefIntPtr(subscriptionCharges), derefStr(scheduleMode))
	if isNoSuchRow(err) {
		return ErrProductNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// derefIntPtr unwraps a **int one level: nil → nil, non-nil → *p (which may
// itself be nil, mapping to SQL NULL). Used to pass an optional nullable int
// (e.g. subscription_charges) through a CASE WHEN guard.
func derefIntPtr(p **int) *int {
	if p == nil {
		return nil
	}
	return *p
}

// derefStr returns the pointed-to string, or "" when nil (which the product
// Update treats as "keep existing" via NULLIF + COALESCE).
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
