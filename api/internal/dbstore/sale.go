package dbstore

import (
	"context"
	"errors"
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
	// SaleRefunded marks a sale whose payment was refunded (initiated from the
	// Sales page or received via the purchase.refunded webhook).
	SaleRefunded = "refunded"
)

// Refund states derived from Sale.RefundedSen (issue #518). A partial refund
// records the amount and leaves the sale reading as PARTIAL — the sale keeps
// its paid/confirmed status until the full total has been returned, and only
// then becomes the terminal SaleRefunded. These values are what the read
// surfaces (list/summary/CSV/MCP) expose as `refund_status`; nothing is
// persisted, so there is no new status column to keep in sync.
const (
	RefundNone    = "none"
	RefundPartial = "partial"
	RefundFull    = "full"
)

// RefundStatus derives the partial-vs-full refund state from the running
// refunded total (issue #518). A sale is only "full" once the whole total has
// been returned; anything in between is "partial".
func (s *Sale) RefundStatus() string {
	switch {
	case s == nil || s.RefundedSen <= 0:
		return RefundNone
	case s.RefundedSen >= s.TotalSen:
		return RefundFull
	default:
		return RefundPartial
	}
}

// OutstandingSen is the balance still refundable on the sale (issue #518):
// total_sen - refunded_sen, never negative.
func (s *Sale) OutstandingSen() int64 {
	if s == nil {
		return 0
	}
	if s.RefundedSen >= s.TotalSen {
		return 0
	}
	return s.TotalSen - s.RefundedSen
}

// Refund rejections (issue #518). The over-refund guard is the important one:
// a refund may never push refunded_sen past total_sen. These are exported so
// the REST and MCP callers can render the same message and so tests can assert
// the rejection reason rather than a bare error.
var (
	// ErrRefundAmountInvalid: amount_sen must be a positive integer.
	ErrRefundAmountInvalid = errors.New("amount_sen mesti positif")
	// ErrRefundExceedsBalance: refunded_sen + amount_sen > total_sen.
	ErrRefundExceedsBalance = errors.New("jumlah refund melebihi baki yang boleh dipulangkan")
	// ErrRefundNoOutstanding: nothing is left to refund on this sale.
	ErrRefundNoOutstanding = errors.New("jualan ini tiada baki untuk dipulangkan")
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
	ID          string
	AccountID   string
	FormID      string
	RefCode     string
	Client      SaleClient
	Items       []SaleItem
	SubtotalSen int64
	ShippingSen int64
	DiscountSen int64
	TotalSen    int64
	// RefundedSen is the running total already refunded against this sale, in
	// integer sen, never greater than TotalSen (issue #518). 0 = nothing
	// refunded; 0 < RefundedSen < TotalSen = a partial refund (the sale keeps
	// its paid/confirmed status); RefundedSen == TotalSen = fully refunded
	// (status moves to SaleRefunded). RefundStatus()/OutstandingSen() derive
	// the partial state and the remaining balance from it.
	RefundedSen int64
	// Issue #560: the subscription terms chosen at checkout, when one of the
	// purchased products is type=subscription. The recurring amount is the
	// SUBSCRIPTION product's own price x quantity (NOT total_sen, which may
	// include one-time items in the same cart), and the period comes from that
	// product rather than a hardcoded default. Empty/zero for every one-time
	// sale.
	SubscriptionProductID string
	SubscriptionAmountSen int64
	SubscriptionPeriod    string
	// Currency is the currency the sale was transacted in (issue #185).
	// Recorded at transaction time from the workspace's currency; historical sales
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

	// Issue #245: AI spam detection. IsSpam + SpamReason record the background
	// AI spam-judge result. A sale is accepted immediately (is_spam=false),
	// then judged in the background; a spam verdict flips IsSpam=true with a
	// reason. A spam-flagged sale with a successful payment (paid/confirmed)
	// is automatically un-spammed. Spam entries are auto-deleted after 30 days
	// by a retention sweep; un-spammed entries are never auto-deleted.
	IsSpam     bool
	SpamReason *string

	// Issue #438: QR ordering flow — the table number the QR stand encoded
	// (e.g. "8"). Empty = a regular online order with no table. Shown to the
	// merchant in the sale/notification so they know where to serve.
	TableNumber string

	// Issue #444: affiliate marketing — the affiliate code (?aff=CODE) the
	// buyer landed with. Empty = no affiliate attribution. Used to compute
	// commission on paid sales.
	AffiliateCode string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// SaleStore wraps pgx for sales.
type SaleStore struct{ pool *pgxpool.Pool }

// NewSaleStore builds a SaleStore.
func NewSaleStore(pool *pgxpool.Pool) *SaleStore { return &SaleStore{pool: pool} }

const saleSelect = `SELECT id, account_id, COALESCE(form_id::text,''), ref_code, client, items,
	subtotal_sen, shipping_sen, discount_sen, total_sen, refunded_sen, currency, sale_type, payment_method, status,
	COALESCE(chip_purchase_id,''), COALESCE(chip_checkout_url,''), COALESCE(invoice_no,''),
	skip_capture, authorized, captured_at, COALESCE(receipt_message_id,''),
	is_spam, spam_reason, COALESCE(table_number,''), COALESCE(affiliate_code,''), created_at, updated_at,
	COALESCE(subscription_product_id::text,''), COALESCE(subscription_amount_sen,0), COALESCE(subscription_period,'')
	FROM sales`

func scanSale(row pgx.Row) (*Sale, error) {
	s := &Sale{}
	err := row.Scan(&s.ID, &s.AccountID, &s.FormID, &s.RefCode, &s.Client, &s.Items,
		&s.SubtotalSen, &s.ShippingSen, &s.DiscountSen, &s.TotalSen, &s.RefundedSen, &s.Currency, &s.SaleType, &s.PaymentMethod, &s.Status,
		&s.ChipPurchaseID, &s.ChipCheckoutURL, &s.InvoiceNo,
		&s.SkipCapture, &s.Authorized, &s.CapturedAt, &s.ReceiptMessageID,
		&s.IsSpam, &s.SpamReason, &s.TableNumber, &s.AffiliateCode, &s.CreatedAt, &s.UpdatedAt,
		&s.SubscriptionProductID, &s.SubscriptionAmountSen, &s.SubscriptionPeriod)
	return s, err
}

// Create inserts a sale and assigns a unique ref_code (given by caller).
func (s *SaleStore) Create(ctx context.Context, sale *Sale) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, shipping_sen,
		   discount_sen, total_sen, currency, sale_type, payment_method, status, skip_capture, table_number, affiliate_code,
		   subscription_product_id, subscription_amount_sen, subscription_period)
		 VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NULLIF($15,''), NULLIF($16,''),
		    NULLIF($17,'')::uuid, NULLIF($18,0), NULLIF($19,''))
		 RETURNING id, created_at, updated_at`,
		sale.AccountID, sale.FormID, sale.RefCode, sale.Client, sale.Items,
		sale.SubtotalSen, sale.ShippingSen, sale.DiscountSen, sale.TotalSen,
		orDefault(sale.Currency, "MYR"),
		sale.SaleType, sale.PaymentMethod, sale.Status, sale.SkipCapture, sale.TableNumber, sale.AffiliateCode,
		sale.SubscriptionProductID, sale.SubscriptionAmountSen, sale.SubscriptionPeriod,
	).Scan(&sale.ID, &sale.CreatedAt, &sale.UpdatedAt)
}

// ByRefCode fetches a sale by ref code.
func (s *SaleStore) ByRefCode(ctx context.Context, refCode string) (*Sale, error) {
	return scanSale(s.pool.QueryRow(ctx, saleSelect+" WHERE ref_code = $1", refCode))
}

// ByAffiliateCode lists paid sales attributed to an affiliate code within a
// workspace (issue #444). Used by the affiliate dashboard to show sales +
// commission.
func (s *SaleStore) ByAffiliateCode(ctx context.Context, workspaceID, code string) ([]*Sale, error) {
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE affiliate_code=$2 AND account_id IN (SELECT id FROM accounts WHERE workspace_id=$1) ORDER BY created_at DESC",
		workspaceID, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Sale
	for rows.Next() {
		sl, err := scanSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
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

// SaleByChipPurchaseID reads a sale by CHIP purchase id outside a transaction.
//
// Issue #557: the CHIP callback resolves the owning account from the payload's
// purchase id before it can fetch that account's verification key, and it runs
// before any transaction is opened — so it needs the non-transactional form.
// The interface it satisfies (httpapi.PaymentsHandlers.EventLookup) had no
// implementation anywhere, which is why the dynamic-key path never ran and
// every callback was rejected 503.
func (s *SaleStore) SaleByChipPurchaseID(ctx context.Context, purchaseID string) (*Sale, error) {
	return scanSale(s.pool.QueryRow(ctx, saleSelect+" WHERE chip_purchase_id = $1", purchaseID))
}

// SaleByRefCodeTx reads a sale by ref code within an open transaction (used
// inside WithSaleLock so a refund sees the locked, current refunded total
// rather than a stale snapshot — issue #518).
func (s *SaleStore) SaleByRefCodeTx(ctx context.Context, tx pgx.Tx, refCode string) (*Sale, error) {
	return scanSale(tx.QueryRow(ctx, saleSelect+" WHERE ref_code = $1", refCode))
}

// SaleByIDTx reads a sale by id within an open transaction (issue #518).
func (s *SaleStore) SaleByIDTx(ctx context.Context, tx pgx.Tx, saleID string) (*Sale, error) {
	return scanSale(tx.QueryRow(ctx, saleSelect+" WHERE id = $1", saleID))
}

// ApplyRefundTx records a refund against a sale inside an open transaction
// (issue #518). It performs the whole refund bookkeeping atomically under the
// caller's per-sale lock (WithSaleLock):
//
//   - the balance guard refunded_sen + amountSen <= total_sen is enforced in
//     the UPDATE's WHERE clause, so two concurrent partial refunds cannot both
//     pass a stale read and over-refund — the second blocks on the advisory
//     lock, then re-reads the committed total and is rejected;
//   - amountSen is added to the running refunded total;
//   - a full refund sets refunded_sen = total_sen and moves the status to the
//     terminal SaleRefunded; a partial refund leaves the status as it was
//     (paid/confirmed) so the sale keeps reading as PARTIAL.
//
// Returns the updated sale (with the new refunded total). Returns
// ErrRefundNotAllowed when a partial total would exceed total_sen on the
// current row, and ErrRefundNoOutstanding when nothing is left to refund.
func (s *SaleStore) ApplyRefundTx(ctx context.Context, tx pgx.Tx, sale *Sale, amountSen int64) (*Sale, error) {
	if amountSen <= 0 {
		return nil, ErrRefundAmountInvalid
	}
	outstanding := sale.TotalSen - sale.RefundedSen
	if outstanding <= 0 {
		return nil, ErrRefundNoOutstanding
	}
	if amountSen > outstanding {
		return nil, ErrRefundExceedsBalance
	}
	status := sale.Status
	if sale.RefundedSen+amountSen >= sale.TotalSen {
		status = SaleRefunded
	}
	// The WHERE clause re-asserts the guard against the live row: even if a
	// caller reached here with a stale read, the UPDATE cannot push
	// refunded_sen past total_sen.
	tag, err := tx.Exec(ctx,
		`UPDATE sales SET refunded_sen = refunded_sen + $2, status = $3, updated_at = now()
		  WHERE id = $1 AND refunded_sen + $2 <= total_sen`,
		sale.ID, amountSen, status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrRefundExceedsBalance
	}
	return s.SaleByIDTx(ctx, tx, sale.ID)
}

// SetStatusTx updates the sale status within an open transaction (used inside
// WithSaleLock so the status write is atomic with the advisory lock).
func (s *SaleStore) SetStatusTx(ctx context.Context, tx pgx.Tx, saleID, status string) error {
	_, err := tx.Exec(ctx,
		`UPDATE sales SET status=$2, updated_at=now() WHERE id=$1`, saleID, status)
	return err
}

// SetRefundedSenTx records the running refunded total within an open
// transaction (issue #518). Used by the purchase.refunded webhook path, where
// the terminal `refunded` status means the full amount was returned but CHIP
// carries no amount on the event. The write is clamped to total_sen so the
// refunded_sen <= total_sen money invariant can never be violated.
func (s *SaleStore) SetRefundedSenTx(ctx context.Context, tx pgx.Tx, saleID string, sen int64) error {
	_, err := tx.Exec(ctx,
		`UPDATE sales SET refunded_sen = LEAST($2, total_sen), updated_at = now() WHERE id = $1`,
		saleID, sen)
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

// saleWhere builds the shared WHERE clause + args for a sales read. $start is
// the position the caller's account-id array occupies, so the same builder
// serves the per-store queries and the merged record read (records.go), with
// one definition of each filter's semantics.
func saleWhere(accountIDs []string, f SaleFilter, start int) (string, []any) {
	where := []string{"account_id = ANY($" + strconv.Itoa(start) + ")"}
	args := []any{accountIDs}
	next := func() int { return start + len(args) }

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
	return strings.Join(where, " AND "), args
}

// ListFilteredPage returns one page of sales across a set of accounts, newest
// first, matching the optional filters, plus the total row count for the same
// filter. Column-aligned with saleSelect/scanSale. Every clause targets sales
// rows only (no joins), so the scan stays the single-column form.
//
// Ordering carries an id tiebreaker: Postgres `now()` is the transaction
// timestamp, so rows inserted in one transaction share created_at exactly and
// an unordered LIMIT/OFFSET can repeat or skip rows across pages.
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

	clause, args := saleWhere(accountIDs, f, 1)
	offset := (page - 1) * limit

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	fullArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE "+clause+" ORDER BY created_at DESC, id DESC LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2),
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

// ByIDs hydrates a set of sales by id, scoped to an account set (ownership is
// enforced inside the query). It is how the merged record read gets the rows it
// selected in a different connection order, without re-implementing saleSelect
// or its scan — the column alignment stays defined once, in this file.
func (s *SaleStore) ByIDs(ctx context.Context, accountIDs []string, ids []string) ([]*Sale, error) {
	if len(accountIDs) == 0 || len(ids) == 0 {
		return []*Sale{}, nil
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id = ANY($1) AND id = ANY($2)", accountIDs, ids)
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

// CountByAccountsForm counts sales for a single form across a set of accounts
// (account-scoped). Used by the entry-limit check (issue #207).
func (s *SaleStore) CountByAccountsForm(ctx context.Context, accountIDs []string, formID string) (int64, error) {
	if len(accountIDs) == 0 || formID == "" {
		return 0, nil
	}
	var total int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id = ANY($1) AND form_id::text = $2`,
		accountIDs, formID).Scan(&total)
	return total, err
}

// ─── Issue #245: AI spam detection ────────────────────────────────────────────

// ListSpamByAccountsPage returns one page of spam-flagged sales across a set
// of accounts, newest first, plus the total count (issue #245 spam review
// queue). Ownership is enforced inside the query.
func (s *SaleStore) ListSpamByAccountsPage(ctx context.Context, accountIDs []string, page, limit int) ([]*Sale, int64, error) {
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
		`SELECT COUNT(*) FROM sales WHERE account_id = ANY($1) AND is_spam = true`,
		accountIDs).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE account_id = ANY($1) AND is_spam = true ORDER BY created_at DESC LIMIT $2 OFFSET $3",
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

// MarkSpam flags a sale as spam with a reason (issue #245). The AI judge
// calls this on a spam verdict.
func (s *SaleStore) MarkSpam(ctx context.Context, id, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET is_spam = true, spam_reason = $2, updated_at = now() WHERE id = $1`,
		id, reason)
	return err
}

// UnSpam clears the spam flag + reason on a sale (issue #245). Called by the
// merchant review queue (false positive) or automatically when a spam-flagged
// sale receives a successful payment (paid/confirmed). Un-spammed entries are
// never auto-deleted.
func (s *SaleStore) UnSpam(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sales SET is_spam = false, spam_reason = NULL, updated_at = now() WHERE id = $1`,
		id)
	return err
}

// UnSpamTx clears the spam flag + reason on a sale within an open transaction
// (issue #245). Used inside WithSaleLock so the auto-un-spam on a successful
// payment is atomic with the status transition.
func (s *SaleStore) UnSpamTx(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx,
		`UPDATE sales SET is_spam = false, spam_reason = NULL, updated_at = now() WHERE id = $1`,
		id)
	return err
}

// DeleteSpamOlderThan permanently deletes spam-flagged sales older than
// cutoff (issue #245 30-day retention sweep). Un-spammed entries are never
// touched. Returns the number of rows deleted.
func (s *SaleStore) DeleteSpamOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM sales WHERE is_spam = true AND created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
