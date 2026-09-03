package dbstore

import (
	"context"
	"strconv"
	"strings"
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

// SaleClient is the buyer info JSON blob. It mirrors the CHIP client fields
// (issue #186) so a merchant-mapped form can send full_name/email/phone plus
// address/legal fields to CHIP, not just the hardcoded three.
type SaleClient struct {
	FullName        string `json:"full_name"`
	Email           string `json:"email"`
	Phone           string `json:"phone,omitempty"`
	StreetAddress   string `json:"street_address,omitempty"`
	City            string `json:"city,omitempty"`
	State           string `json:"state,omitempty"`
	ZipCode         string `json:"zip_code,omitempty"`
	Country         string `json:"country,omitempty"`
	LegalName       string `json:"legal_name,omitempty"`
	RegistrationNum string `json:"registration_number,omitempty"`
	TaxNumber       string `json:"tax_number,omitempty"`
}

// SaleItem is one line item in the sale JSON.
type SaleItem struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	PriceSen  int64  `json:"price_sen"`
	Qty       int64  `json:"qty"`
	// VariationID and VariationName record which variation was bought (issue
	// #153). Empty for simple products. Persisted on the sale so the receipt
	// and merchant sales view show which variation was purchased.
	VariationID   string `json:"variation_id,omitempty"`
	VariationName string `json:"variation_name,omitempty"`
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
	// Currency is the currency the sale was transacted in (issue #185).
	// Recorded at transaction time from the space's currency; historical sales
	// keep their original currency (never auto-converted on a later change).
	Currency        string
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

	// ReceiptMessageID is the SES MessageID returned when the receipt email
	// was accepted for sending (issue #106). It is the correlation key the SNS
	// endpoint uses to tie an Open/Click/Delivered event back to this sale.
	ReceiptMessageID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SaleStore wraps pgx for sales.
type SaleStore struct{ pool *pgxpool.Pool }

// NewSaleStore builds a SaleStore.
func NewSaleStore(pool *pgxpool.Pool) *SaleStore { return &SaleStore{pool: pool} }

const saleSelect = `SELECT id, account_id, COALESCE(form_id::text,''), ref_code, client, items,
	subtotal_sen, shipping_sen, discount_sen, total_sen, currency, sale_type, payment_method, status,
	COALESCE(chip_purchase_id,''), COALESCE(chip_checkout_url,''), COALESCE(invoice_no,''),
	skip_capture, authorized, captured_at, COALESCE(receipt_message_id,''), created_at, updated_at
	FROM sales`

func scanSale(row pgx.Row) (*Sale, error) {
	s := &Sale{}
	err := row.Scan(&s.ID, &s.AccountID, &s.FormID, &s.RefCode, &s.Client, &s.Items,
		&s.SubtotalSen, &s.ShippingSen, &s.DiscountSen, &s.TotalSen, &s.Currency, &s.SaleType, &s.PaymentMethod, &s.Status,
		&s.ChipPurchaseID, &s.ChipCheckoutURL, &s.InvoiceNo,
		&s.SkipCapture, &s.Authorized, &s.CapturedAt, &s.ReceiptMessageID, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// Create inserts a sale and assigns a unique ref_code (given by caller).
func (s *SaleStore) Create(ctx context.Context, sale *Sale) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, shipping_sen,
		   discount_sen, total_sen, currency, sale_type, payment_method, status, skip_capture)
		 VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id, created_at, updated_at`,
		sale.AccountID, sale.FormID, sale.RefCode, sale.Client, sale.Items,
		sale.SubtotalSen, sale.ShippingSen, sale.DiscountSen, sale.TotalSen,
		orDefault(sale.Currency, "MYR"),
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

// ByReceiptMessageID fetches a sale by its receipt's SES MessageID (issue
// #106). The SNS endpoint uses this to correlate an Open/Click/Delivered
// event back to the sale that sent the receipt. Returns pgx.ErrNoRows when no
// sale has that message id.
func (s *SaleStore) ByReceiptMessageID(ctx context.Context, messageID string) (*Sale, error) {
	return scanSale(s.pool.QueryRow(ctx, saleSelect+" WHERE receipt_message_id = $1", messageID))
}

// SetReceiptMessageID records the SES MessageID returned when the receipt
// email was accepted for sending (issue #106). Best-effort: a failure is
// logged by the caller and never fails the payment flow.
func (s *SaleStore) SetReceiptMessageID(ctx context.Context, saleID, messageID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET receipt_message_id=$2, updated_at=now() WHERE id=$1`,
		saleID, messageID)
	return err
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

// WithSaleLock runs fn inside a transaction that holds a per-sale advisory
// lock (pg_advisory_xact_lock keyed on a hash of the sale id). This serializes
// concurrent ProcessCallback calls for the same sale: only one callback
// processes a given sale at a time; the rest block until the first commits,
// then re-read the sale's current status and apply the transition guard. fn
// receives the tx-bound pool so reads/writes see the locked, current state.
func (s *SaleStore) WithSaleLock(ctx context.Context, saleID string, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var key int64
	if err := tx.QueryRow(ctx, `SELECT hashtext($1)::bigint`, saleID).Scan(&key); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaleByChipPurchaseIDTx reads a sale by CHIP purchase id within an open
// transaction (used inside WithSaleLock so the read sees the locked, current
// status rather than a stale snapshot).
func (s *SaleStore) SaleByChipPurchaseIDTx(ctx context.Context, tx pgx.Tx, purchaseID string) (*Sale, error) {
	return scanSale(tx.QueryRow(ctx, saleSelect+" WHERE chip_purchase_id = $1", purchaseID))
}

// SetStatusTx updates the sale status within an open transaction (used inside
// WithSaleLock so the status write is atomic with the advisory lock).
func (s *SaleStore) SetStatusTx(ctx context.Context, tx pgx.Tx, saleID, status string) error {
	_, err := tx.Exec(ctx,
		`UPDATE sales SET status=$2, updated_at=now() WHERE id=$1`, saleID, status)
	return err
}

// ListByAccountStatus returns recent sales for one account filtered by status
// (issue #126 MCP parity with the /sales page status dropdown), newest first.
func (s *SaleStore) ListByAccountStatus(ctx context.Context, accountID, status string, limit int) ([]*Sale, error) {
	if status == "" {
		return s.ListByAccount(ctx, accountID, limit)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id=$1 AND status=$2 ORDER BY created_at DESC LIMIT $3",
		accountID, status, limit)
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

// SaleFilter is the optional filter set for the dedicated /sales page (issue
// #126): per-form, status, created-at date range and a free-text search over
// the reference code and buyer name. Date fields are YYYY-MM-DD (timezone-agnostic
// day bounds) — the from day starts at 00:00 and the to day runs through its
// end so an inclusive day range is intuitive in the UI.
type SaleFilter struct {
	FormID   string
	Status   string
	DateFrom string // YYYY-MM-DD, inclusive
	DateTo   string // YYYY-MM-DD, inclusive
	Q        string // matches ref_code or buyer full name (case-insensitive)
}

// ListFilteredPage returns one page of sales across a set of accounts, newest
// first, matching the optional filters, plus the total row count for the same
// filter. Column-aligned with saleSelect/scanSale. Every clause targets sales
// rows only (no joins), so the scan stays the single-column form.
func (s *SaleStore) ListFilteredPage(ctx context.Context, accountIDs []string, f SaleFilter, page, limit int) ([]*Sale, int64, error) {
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

	where := []string{"account_id = ANY($1)"}
	args := []any{accountIDs}
	next := func() int { return len(args) + 1 }

	if f.FormID != "" {
		args = append(args, f.FormID)
		where = append(where, "form_id::text = $"+strconv.Itoa(next()-1))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, "status = $"+strconv.Itoa(next()-1))
	}
	if f.DateFrom != "" {
		args = append(args, f.DateFrom+" 00:00:00")
		where = append(where, "created_at >= $"+strconv.Itoa(next()-1))
	}
	if f.DateTo != "" {
		args = append(args, f.DateTo+" 23:59:59")
		where = append(where, "created_at <= $"+strconv.Itoa(next()-1))
	}
	if f.Q != "" {
		args = append(args, "%"+f.Q+"%")
		where = append(where, "(ref_code ILIKE $"+strconv.Itoa(next()-1)+" OR client->>'full_name' ILIKE $"+strconv.Itoa(next()-1)+")")
	}

	clause := strings.Join(where, " AND ")
	offset := (page - 1) * limit

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	fullArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE "+clause+" ORDER BY created_at DESC LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2),
		fullArgs...)
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
