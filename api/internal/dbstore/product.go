package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Product models the products table.
type Product struct {
	ID                 string
	AccountID          string
	Name               string
	Description        string
	PriceSen           int64
	// Currency is the currency the product's price is denominated in (issue
	// #185). Set from the space's currency at creation; a later space currency
	// change applies to new products going forward (existing prices keep their
	// original currency).
	Currency           string
	Images             []string
	Stock              *int
	Type               string // one_time | subscription
	BillingPeriod      string // month | year when subscription
	TrialDays          int
	SubscriptionCharges *int // NULL = infinite; N = stop after Nth charge
	Active             bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

var ErrProductNotFound = errors.New("product not found")

// ProductStore wraps pgx for products.
type ProductStore struct{ pool *pgxpool.Pool }

// NewProductStore builds a ProductStore.
func NewProductStore(pool *pgxpool.Pool) *ProductStore { return &ProductStore{pool: pool} }

const productSelect = `SELECT id, account_id, name, COALESCE(description,''), price_sen, currency,
	images, stock, type, COALESCE(billing_period,''), trial_days, subscription_charges, active, created_at, updated_at
	FROM products`

// Create inserts a product.
func (s *ProductStore) Create(ctx context.Context, p *Product) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO products (account_id, name, description, price_sen, currency, type, billing_period, trial_days, subscription_charges, stock, images)
		 VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,COALESCE($11::jsonb,'[]'::jsonb))
		 RETURNING id, created_at, updated_at`,
		p.AccountID, p.Name, p.Description, p.PriceSen, orDefault(p.Currency, "MYR"), p.Type, p.BillingPeriod,
		p.TrialDays, p.SubscriptionCharges, p.Stock, p.Images,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

// ListActiveByAccount lists active products.
func (s *ProductStore) ListActiveByAccount(ctx context.Context, accountID string) ([]*Product, error) {
	rows, err := s.pool.Query(ctx, productSelect+" WHERE account_id=$1 AND active ORDER BY created_at DESC", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Product
	for rows.Next() {
		q := &Product{}
		if err := rows.Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen, &q.Currency,
			&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.SubscriptionCharges, &q.Active, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// ListActiveByAccounts lists active products across a set of accounts
// (e.g. every account in the space the dashboard user selected).
func (s *ProductStore) ListActiveByAccounts(ctx context.Context, accountIDs []string) ([]*Product, error) {
	if len(accountIDs) == 0 {
		return []*Product{}, nil
	}
	rows, err := s.pool.Query(ctx,
		productSelect+" WHERE account_id = ANY($1) AND active ORDER BY created_at DESC", accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Product{}
	for rows.Next() {
		q := &Product{}
		if err := rows.Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen, &q.Currency,
			&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.SubscriptionCharges, &q.Active, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// ListAllByAccounts lists ALL products (active + archived) across a set of
// accounts — for the WooCommerce-style admin list that shows a status column
// and lets the owner toggle products back to active. Sorted newest first.
func (s *ProductStore) ListAllByAccounts(ctx context.Context, accountIDs []string) ([]*Product, error) {
	if len(accountIDs) == 0 {
		return []*Product{}, nil
	}
	rows, err := s.pool.Query(ctx,
		productSelect+" WHERE account_id = ANY($1) ORDER BY created_at DESC", accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Product{}
	for rows.Next() {
		q := &Product{}
		if err := rows.Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen, &q.Currency,
			&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.SubscriptionCharges, &q.Active, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// SetActive archives/restores a product.
func (s *ProductStore) SetActive(ctx context.Context, accountID, productID string, active bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET active=$3, updated_at=now() WHERE id=$2 AND account_id=$1`,
		accountID, productID, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}

// Get fetches one product scoped to the account.
func (s *ProductStore) Get(ctx context.Context, accountID, productID string) (*Product, error) {
	q := &Product{}
	err := s.pool.QueryRow(ctx, productSelect+" WHERE id=$2 AND account_id=$1", accountID, productID).
		Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen, &q.Currency,
			&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.SubscriptionCharges, &q.Active, &q.CreatedAt, &q.UpdatedAt)
	return q, err
}

var _ = pgxpool.Pool{} // ensure pgxpool import remains for future stores in this package

// Archive sets active=false on a product owned by the account.
func (s *ProductStore) Archive(ctx context.Context, accountID, productID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE products SET active=false, updated_at=now() WHERE id=$1 AND account_id=$2`,
		productID, accountID)
	return err
}

// ArchiveMany soft-deletes (archives) many products at once; returns how many
// ids actually updated (account-scoped). Mirrors FormStore.SoftDeleteMany for
// the forms bulk pattern (#10).
func (s *ProductStore) ArchiveMany(ctx context.Context, accountID string, productIDs []string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET active=false, updated_at=now()
		 WHERE account_id=$1 AND active AND id = ANY($2)`,
		accountID, productIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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
	active *bool, typ, billingPeriod *string, trialDays *int, subscriptionCharges **int) error {
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
		   updated_at = now()
		 WHERE id=$2 AND account_id=$1`,
		accountID, productID, name, description, priceSen, images != nil, images,
		stock != nil, stock, typ, billingPeriod, trialDays, active,
		subscriptionCharges != nil, derefIntPtr(subscriptionCharges))
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
