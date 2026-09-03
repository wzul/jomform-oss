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
type ProductVariation struct {
	ID        string
	ProductID string
	Name      string
	PriceSen  int64
	Stock     *int
	Images    []string
	SortOrder int
	CreatedAt time.Time
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
		`SELECT id, product_id, name, price_sen, stock, images, sort_order, created_at
		 FROM product_variations WHERE product_id=$1 ORDER BY sort_order, created_at`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProductVariation
	for rows.Next() {
		v := &ProductVariation{}
		if err := rows.Scan(&v.ID, &v.ProductID, &v.Name, &v.PriceSen, &v.Stock,
			&v.Images, &v.SortOrder, &v.CreatedAt); err != nil {
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
			`INSERT INTO product_variations (product_id, name, price_sen, stock, images, sort_order)
			 VALUES ($1,$2,$3,$4,COALESCE($5::jsonb,'[]'::jsonb),$6)`,
			productID, v.Name, v.PriceSen, v.Stock, v.Images, order); err != nil {
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
	v := &ProductVariation{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, product_id, name, price_sen, stock, images, sort_order, created_at
		 FROM product_variations WHERE id=$1 AND product_id=$2`, variationID, productID).
		Scan(&v.ID, &v.ProductID, &v.Name, &v.PriceSen, &v.Stock,
			&v.Images, &v.SortOrder, &v.CreatedAt)
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
