package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProductVariation models one row of the product_variations table. A product
// is "simple" when it has zero variations (uses product-level price/stock/
// images); "variable" when it has >=1 variation. Money stays integer sen.
//
// Issue #623: a variation may carry its own subscription cycle (BillingPeriod,
// TrialDays, SubscriptionCharges). Each is POINTER/optional on purpose, because
// "not set" and "set to the product's value" are different states:
//
//	nil  = the variation has no value of its own → it INHERITS the product's
//	set  = the variation's own value governs that field
//
// Every variation that exists today has nil in all three, which is exactly the
// state "no cycle of its own" — so existing products keep billing on the
// product's cycle, unchanged.
type ProductVariation struct {
	ID        string
	ProductID string
	Name      string
	PriceSen  int64
	Stock     *int
	Images    []string
	SortOrder int
	CreatedAt time.Time

	// Issue #623: the variation's own subscription cycle. nil = inherit the
	// product's for that field. Only meaningful on a subscription product.
	BillingPeriod string // "" = not set → inherit; month | year when set
	TrialDays     *int   // nil = not set → inherit
	Charges       *int   // nil = not set → inherit; N = a variation-fixed count
}

// variationSelect is the shared column list for every variation read, so a
// migration that adds a column surfaces here (and at each Scan below) instead
// of silently skewing the row.
const variationSelect = `SELECT id, product_id, name, price_sen, stock, images, sort_order, created_at,
	COALESCE(billing_period,''), trial_days, subscription_charges
	FROM product_variations`

// scanVariation scans one variationSelect row.
func scanVariation(row pgx.Row) (*ProductVariation, error) {
	v := &ProductVariation{}
	err := row.Scan(&v.ID, &v.ProductID, &v.Name, &v.PriceSen, &v.Stock,
		&v.Images, &v.SortOrder, &v.CreatedAt,
		&v.BillingPeriod, &v.TrialDays, &v.Charges)
	return v, err
}

// ProductVariationStore wraps pgx for product_variations.
type ProductVariationStore struct{ pool *pgxpool.Pool }

// NewProductVariationStore builds a ProductVariationStore.
func NewProductVariationStore(pool *pgxpool.Pool) *ProductVariationStore {
	return &ProductVariationStore{pool: pool}
}

// ListByProduct returns a product's variations ordered by sort_order then
// created_at (stable for the editor's reorder UI).
func (s *ProductVariationStore) ListByProduct(ctx context.Context, productID string) ([]*ProductVariation, error) {
	rows, err := s.pool.Query(ctx,
		variationSelect+` WHERE product_id=$1 ORDER BY sort_order, created_at`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProductVariation
	for rows.Next() {
		v, err := scanVariation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReplaceAll wholesale-replaces a product's variations with the given set
// (delete-then-insert in one transaction). Used on product create/update so
// the editor's add/remove/reorder always lands as the authoritative list.
// sort_order is taken from each variation's SortOrder field (or its index).
//
// Issue #623: the INSERT carries the variation's subscription cycle too. This
// does NOT change the replace-all contract (delete-then-insert, same sort_order
// rule) — it only widens the row. A caller that omits the cycle fields stores
// NULL, which is the "inherit the product" state.
func (s *ProductVariationStore) ReplaceAll(ctx context.Context, productID string, variations []*ProductVariation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM product_variations WHERE product_id=$1`, productID); err != nil {
		return err
	}
	for i, v := range variations {
		order := v.SortOrder
		if order == 0 && i > 0 {
			order = i
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO product_variations (product_id, name, price_sen, stock, images, sort_order,
			   billing_period, trial_days, subscription_charges)
			 VALUES ($1,$2,$3,$4,COALESCE($5::jsonb,'[]'::jsonb),$6,
			   NULLIF($7,''),$8,$9)`,
			productID, v.Name, v.PriceSen, v.Stock, v.Images, order,
			v.BillingPeriod, v.TrialDays, v.Charges); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteByProduct removes all variations for a product (used when a product
// is switched back to "simple").
func (s *ProductVariationStore) DeleteByProduct(ctx context.Context, productID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM product_variations WHERE product_id=$1`, productID)
	return err
}

// GetByProduct fetches a single variation scoped to its product. Returns
// ErrVariationNotFound when the variation does not exist or does not belong
// to the given product. Used by the buyer submit path to validate that a
// chosen variation_id really belongs to the product being bought.
func (s *ProductVariationStore) GetByProduct(ctx context.Context, productID, variationID string) (*ProductVariation, error) {
	v, err := scanVariation(s.pool.QueryRow(ctx,
		variationSelect+` WHERE id=$1 AND product_id=$2`, variationID, productID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVariationNotFound
		}
		return nil, err
	}
	return v, nil
}

// ErrVariationNotFound rejects a submit that names a variation which does not
// exist or does not belong to the product being bought.
var ErrVariationNotFound = errors.New("variation not found")

// Create inserts ONE variation (issue #623). Distinct from ReplaceAll: an MCP
// agent adds a single variation without having to re-send the whole set (and
// without racing the editor's wholesale save). sort_order is placed last when
// the caller leaves it at 0.
func (s *ProductVariationStore) Create(ctx context.Context, v *ProductVariation) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO product_variations (product_id, name, price_sen, stock, images, sort_order,
		   billing_period, trial_days, subscription_charges)
		 VALUES ($1,$2,$3,$4,COALESCE($5::jsonb,'[]'::jsonb),
		   COALESCE(NULLIF($6,0), (SELECT COALESCE(MAX(sort_order),-1)+1 FROM product_variations WHERE product_id=$1)),
		   NULLIF($7,''),$8,$9)
		 RETURNING id, sort_order, created_at`,
		v.ProductID, v.Name, v.PriceSen, v.Stock, v.Images, v.SortOrder,
		v.BillingPeriod, v.TrialDays, v.Charges,
	).Scan(&v.ID, &v.SortOrder, &v.CreatedAt)
}

// Update modifies ONE variation's editable fields scoped to its product
// (issue #623). A nil pointer leaves that field unchanged.
//
// The subscription cycle is nullable-by-design: clearBillingPeriod / clearTrial
// / clearCharges explicitly reset a field back to "not set", which is what
// restores INHERITANCE from the product. Without a way to clear them, the
// "not set" and "set to the product's value" states would be indistinguishable
// after the first edit.
func (s *ProductVariationStore) Update(ctx context.Context, productID, variationID string,
	name *string, priceSen *int64, stock **int, images *[]string, sortOrder *int,
	billingPeriod *string, clearBillingPeriod bool,
	trialDays *int, clearTrial bool,
	charges *int, clearCharges bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE product_variations SET
		   name = COALESCE($3, name),
		   price_sen = COALESCE($4, price_sen),
		   stock = CASE WHEN $5::boolean THEN $6 ELSE stock END,
		   images = CASE WHEN $7::boolean THEN $8::jsonb ELSE images END,
		   sort_order = COALESCE($9, sort_order),
		   billing_period = CASE WHEN $10::boolean THEN NULL
		                         ELSE COALESCE(NULLIF($11,''), billing_period) END,
		   trial_days = CASE WHEN $12::boolean THEN NULL
		                     ELSE COALESCE($13, trial_days) END,
		   subscription_charges = CASE WHEN $14::boolean THEN NULL
		                               ELSE COALESCE($15, subscription_charges) END
		 WHERE id=$2 AND product_id=$1`,
		productID, variationID, name, priceSen,
		stock != nil, derefIntPtr(stock), images != nil, images, sortOrder,
		clearBillingPeriod, billingPeriod,
		clearTrial, trialDays,
		clearCharges, charges)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVariationNotFound
	}
	return nil
}

// Delete removes ONE variation scoped to its product (issue #623). Returns
// ErrVariationNotFound when the id does not belong to that product, so an
// agent cannot delete a variation out of another product's set.
func (s *ProductVariationStore) Delete(ctx context.Context, productID, variationID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM product_variations WHERE id=$1 AND product_id=$2`, variationID, productID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVariationNotFound
	}
	return nil
}

// SubscriptionCycle is the subscription cycle a purchase actually bills on
// (issue #623).
type SubscriptionCycle struct {
	// Period is month | year. Empty means neither the variation nor the
	// product carries a cycle.
	Period string
	// TrialDays is the effective trial length in days.
	TrialDays int
	// Charges is the effective fixed billing count; nil = infinite.
	Charges *int
}

// EffectiveCycle resolves the ONE place a variation's subscription cycle is
// decided: a field the variation sets itself wins, otherwise the product's
// value applies. Called from the checkout (the sale's recorded cycle) and from
// the recurring engine (the fixed billing count), so a variation's cycle can
// never be honoured on one path and ignored on another.
//
// Per-field inheritance is deliberate rather than all-or-nothing: each column
// is nullable, so a variation can set only the period and keep inheriting the
// product's charge count. `nil` for both p and v is safe and yields no cycle.
func EffectiveCycle(p *Product, v *ProductVariation) SubscriptionCycle {
	var out SubscriptionCycle
	if p != nil {
		out.Period = p.BillingPeriod
		out.TrialDays = p.TrialDays
		out.Charges = p.SubscriptionCharges
	}
	if v == nil {
		return out
	}
	if v.BillingPeriod != "" {
		out.Period = v.BillingPeriod
	}
	if v.TrialDays != nil {
		out.TrialDays = *v.TrialDays
	}
	if v.Charges != nil {
		out.Charges = v.Charges
	}
	return out
}

// DecrementStock atomically reduces a variation's stock for a limited
// variation. Returns ErrOutOfStock when the variation is sold out. Unlimited
// (NULL) always ok. The WHERE guard (stock >= $3) makes the decrement
// race-safe: two concurrent buyers can never oversell a single unit.
func (s *ProductVariationStore) DecrementStock(ctx context.Context, variationID string, qty int) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE product_variations SET stock = stock - $2
		 WHERE id=$1 AND stock IS NOT NULL AND stock >= $2`, variationID, qty)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// either unlimited variation (fine) or insufficient stock
		var stock *int
		if err := s.pool.QueryRow(ctx,
			`SELECT stock FROM product_variations WHERE id=$1`, variationID).Scan(&stock); err != nil {
			return err
		}
		if stock != nil && *stock < qty {
			return ErrOutOfStock
		}
	}
	return nil
}
