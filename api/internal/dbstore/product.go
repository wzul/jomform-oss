package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Product models the products table.
type Product struct {
	ID            string
	AccountID     string
	Name          string
	Description   string
	PriceSen      int64
	Images        []string
	Stock         *int
	Type          string // one_time | subscription
	BillingPeriod string // month | year when subscription
	TrialDays     int
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

var ErrProductNotFound = errors.New("product not found")

// ProductStore wraps pgx for products.
type ProductStore struct{ pool *pgxpool.Pool }

// NewProductStore builds a ProductStore.
func NewProductStore(pool *pgxpool.Pool) *ProductStore { return &ProductStore{pool: pool} }

const productSelect = `SELECT id, account_id, name, COALESCE(description,''), price_sen,
	images, stock, type, COALESCE(billing_period,''), trial_days, active, created_at, updated_at
	FROM products`

// Create inserts a product.
func (s *ProductStore) Create(ctx context.Context, p *Product) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO products (account_id, name, description, price_sen, type, billing_period, trial_days)
		 VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7)
		 RETURNING id, created_at, updated_at`,
		p.AccountID, p.Name, p.Description, p.PriceSen, p.Type, p.BillingPeriod, p.TrialDays,
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
		if err := rows.Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen,
			&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.Active, &q.CreatedAt, &q.UpdatedAt); err != nil {
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
		Scan(&q.ID, &q.AccountID, &q.Name, &q.Description, &q.PriceSen,
			&q.Images, &q.Stock, &q.Type, &q.BillingPeriod, &q.TrialDays, &q.Active, &q.CreatedAt, &q.UpdatedAt)
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
