package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sale lifecycle values.
const (
	SaleTypeOneTime      = "one_time"
	SaleTypeSubscription = "subscription"

	SalePending   = "pending"
	SalePaid      = "paid"
	SaleConfirmed = "confirmed"
	SaleCancelled = "cancelled"
	SaleFailed    = "failed"
)

// SaleClient is the buyer info JSON blob.
type SaleClient struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Phone    string `json:"phone,omitempty"`
}

// SaleItem is one line item in the sale JSON.
type SaleItem struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	PriceSen  int64  `json:"price_sen"`
	Qty       int64  `json:"qty"`
}

// Sale models the sales table.
type Sale struct {
	ID              string
	AccountID       string
	FormID          string
	RefCode         string
	Client          SaleClient
	Items           []SaleItem
	SubtotalSen     int64
	ShippingSen     int64
	DiscountSen     int64
	TotalSen        int64
	SaleType        string
	PaymentMethod   string
	Status          string
	ChipPurchaseID  string
	ChipCheckoutURL string
	InvoiceNo       string

	// Delayed capture (skip_capture) flow.
	SkipCapture bool
	Authorized  bool
	CapturedAt  *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SaleStore wraps pgx for sales.
type SaleStore struct{ pool *pgxpool.Pool }

// NewSaleStore builds a SaleStore.
func NewSaleStore(pool *pgxpool.Pool) *SaleStore { return &SaleStore{pool: pool} }

const saleSelect = `SELECT id, account_id, COALESCE(form_id::text,''), ref_code, client, items,
	subtotal_sen, shipping_sen, discount_sen, total_sen, sale_type, payment_method, status,
	COALESCE(chip_purchase_id,''), COALESCE(chip_checkout_url,''), COALESCE(invoice_no,''),
	skip_capture, authorized, captured_at, created_at, updated_at
	FROM sales`

func scanSale(row pgx.Row) (*Sale, error) {
	s := &Sale{}
	err := row.Scan(&s.ID, &s.AccountID, &s.FormID, &s.RefCode, &s.Client, &s.Items,
		&s.SubtotalSen, &s.ShippingSen, &s.DiscountSen, &s.TotalSen, &s.SaleType, &s.PaymentMethod, &s.Status,
		&s.ChipPurchaseID, &s.ChipCheckoutURL, &s.InvoiceNo,
		&s.SkipCapture, &s.Authorized, &s.CapturedAt, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// Create inserts a sale and assigns a unique ref_code (given by caller).
func (s *SaleStore) Create(ctx context.Context, sale *Sale) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, shipping_sen,
		   discount_sen, total_sen, sale_type, payment_method, status, skip_capture)
		 VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id, created_at, updated_at`,
		sale.AccountID, sale.FormID, sale.RefCode, sale.Client, sale.Items,
		sale.SubtotalSen, sale.ShippingSen, sale.DiscountSen, sale.TotalSen,
		sale.SaleType, sale.PaymentMethod, sale.Status, sale.SkipCapture,
	).Scan(&sale.ID, &sale.CreatedAt, &sale.UpdatedAt)
}

// ByRefCode fetches a sale by its public reference code.
func (s *SaleStore) ByRefCode(ctx context.Context, refCode string) (*Sale, error) {
	return scanSale(s.pool.QueryRow(ctx, saleSelect+" WHERE ref_code = $1", refCode))
}

// ByChipPurchaseID fetches a sale by CHIP purchase id.
func (s *SaleStore) ByChipPurchaseID(ctx context.Context, purchaseID string) (*Sale, error) {
	return scanSale(s.pool.QueryRow(ctx, saleSelect+" WHERE chip_purchase_id = $1", purchaseID))
}

// ByID fetches by uuid string.
func (s *SaleStore) ByID(ctx context.Context, id string) (*Sale, error) {
	return scanSale(s.pool.QueryRow(ctx, saleSelect+" WHERE id = $1", id))
}

// AttachChipPurchase links a CHIP purchase to the sale.
func (s *SaleStore) AttachChipPurchase(ctx context.Context, saleID, purchaseID, checkoutURL string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET chip_purchase_id=$2, chip_checkout_url=$3, updated_at=now() WHERE id=$1`,
		saleID, purchaseID, checkoutURL)
	return err
}

// MarkAuthorized flips an authorized (skip_capture) purchase to paid state
// after the merchant captures; CapturedAt marks the moment.
func (s *SaleStore) MarkCaptured(ctx context.Context, saleID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET authorized=true, captured_at=now(), status=$2, updated_at=now() WHERE id=$1`,
		saleID, SalePaid)
	return err
}

// ListAwaitingCapture returns the account's authorized-but-uncaptured
// skip_capture sales (the "Capture all pending" queue), oldest first so the
// oldest CHIP authorizations — the ones closest to expiry — are surfaced first.
func (s *SaleStore) ListAwaitingCapture(ctx context.Context, accountID string) ([]*Sale, error) {
	rows, err := s.pool.Query(ctx,
		saleSelect+` WHERE account_id=$1 AND skip_capture=true
		  AND captured_at IS NULL AND COALESCE(chip_purchase_id,'')<>''
		  AND status IN ('pending','paid','authorized')
		ORDER BY created_at ASC LIMIT 200`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Sale
	for rows.Next() {
		sale, err := scanSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sale)
	}
	return out, rows.Err()
}

// SetStatus transitions the sale status.
func (s *SaleStore) SetStatus(ctx context.Context, saleID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET status=$2, updated_at=now() WHERE id=$1`, saleID, status)
	return err
}

// ListByAccount returns recent sales for a dashboard.
func (s *SaleStore) ListByAccount(ctx context.Context, accountID string, limit int) ([]*Sale, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, saleSelect+" WHERE account_id=$1 ORDER BY created_at DESC LIMIT $2", accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Sale
	for rows.Next() {
		var s *Sale
		s, err = scanSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListByAccounts returns recent sales across a set of accounts (dashboard
// summary rollups). limit follows the same 1..200 clamp as ListByAccount.
func (s *SaleStore) ListByAccounts(ctx context.Context, accountIDs []string, limit int) ([]*Sale, error) {
	if len(accountIDs) == 0 {
		return []*Sale{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id = ANY($1) ORDER BY created_at DESC LIMIT $2", accountIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Sale{}
	for rows.Next() {
		sale, err := scanSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sale)
	}
	return out, rows.Err()
}

// ListByAccountsPage returns one page of sales across a set of accounts,
// newest first, plus the total row count for the same filter (for paging).
func (s *SaleStore) ListByAccountsPage(ctx context.Context, accountIDs []string, page, limit int) ([]*Sale, int64, error) {
	if len(accountIDs) == 0 {
		return []*Sale{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id = ANY($1)`, accountIDs).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id = ANY($1) ORDER BY created_at DESC LIMIT $2 OFFSET $3",
		accountIDs, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Sale{}
	for rows.Next() {
		sale, err := scanSale(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sale)
	}
	return out, total, rows.Err()
}

// ListByAccountForm returns recent sales for a single form within an account,
// newest first. Same limit clamp as ListByAccount.
func (s *SaleStore) ListByAccountForm(ctx context.Context, accountID, formID string, limit int) ([]*Sale, error) {
	if formID == "" {
		return s.ListByAccount(ctx, accountID, limit)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id=$1 AND form_id::text=$2 ORDER BY created_at DESC LIMIT $3",
		accountID, formID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Sale
	for rows.Next() {
		var s *Sale
		s, err = scanSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListByAccountsFormPage returns one page of sales for a single form across a
// set of accounts, newest first, plus the total row count for the same filter
// (for paging). formID is matched as text against the nullable form_id column,
// so a non-uuid value simply yields an empty page instead of a SQL error.
func (s *SaleStore) ListByAccountsFormPage(ctx context.Context, accountIDs []string, formID string, page, limit int) ([]*Sale, int64, error) {
	if len(accountIDs) == 0 || formID == "" {
		return []*Sale{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id = ANY($1) AND form_id::text = $2`,
		accountIDs, formID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id = ANY($1) AND form_id::text = $2 ORDER BY created_at DESC LIMIT $3 OFFSET $4",
		accountIDs, formID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Sale{}
	for rows.Next() {
		sale, err := scanSale(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sale)
	}
	return out, total, rows.Err()
}

// CountByAccounts counts sales across a set of accounts.
func (s *SaleStore) CountByAccounts(ctx context.Context, accountIDs []string) (int64, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	var total int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id = ANY($1)`, accountIDs).Scan(&total)
	return total, err
}
