package dbstore

// Per-workspace processing-fee rates (card t_135c3c72).
//
// The merchant decides, PER CHIP PAYMENT METHOD, whether the acquirer's charge
// is passed on to the buyer and at what rate — their own MDR, never a hardcoded
// CHIP rate. Two things gate the feature beyond the rate itself:
//
//   - the merchant must have ACKNOWLEDGED the disclosure sentence before any fee
//     may reach a buyer (Consumer Protection Act 1999: the price shown must be
//     the price paid, so the merchant has to have seen the sentence the settings
//     page displays next to the switch);
//   - a FORM may switch the fee off for itself even when the workspace has it on
//     (`forms.fee_override = 'off'`), following the per-form CHIP credential
//     override pattern.
//
// The resolution below is the ONE place that combines those three facts, so the
// checkout page, the order route and the settings page cannot disagree about
// whether a fee is active.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FeeOverrideOff is the forms.fee_override value that switches the processing
// fee off for one form. Empty means "inherit the workspace".
const FeeOverrideOff = "off"

// PaymentFeeRate is one workspace's rate for one CHIP payment method.
type PaymentFeeRate struct {
	Method    string
	Enabled   bool
	FixedSen  int64
	PercentBP int64
	UpdatedBy string
	UpdatedAt time.Time
}

// FeeAcknowledgement records the merchant's acknowledgement of the
// pass-on disclosure, and who made it (with when).
type FeeAcknowledgement struct {
	AcknowledgedBy string
	AcknowledgedAt time.Time
}

// PaymentFeeStore wraps pgx for payment_fee_rates + workspace_fee_acknowledgements.
type PaymentFeeStore struct{ pool *pgxpool.Pool }

// NewPaymentFeeStore builds a PaymentFeeStore.
func NewPaymentFeeStore(pool *pgxpool.Pool) *PaymentFeeStore {
	return &PaymentFeeStore{pool: pool}
}

// ErrFeeRateInvalid is the refusal for a rate the schema would also reject.
// Exported so the REST/MCP callers render one message for it.
var ErrFeeRateInvalid = errors.New("invalid fee rate: the percentage must be between 0% and 99.99%, the fixed fee must be 0 or more")

// ErrFeeUnknownMethod is the refusal for a rate stored against a method CHIP
// does not offer JomForm a per-method choice for.
var ErrFeeUnknownMethod = errors.New("invalid payment method")

// validFeeMethods mirrors the payment_fee_rates_method_chk constraint. Keeping
// the list here (and the CHECK in the migration) means a bad method is refused
// with a sentence at the API edge instead of a 500 from the schema.
var validFeeMethods = map[string]bool{
	"fpx":        true,
	"card":       true,
	"ewallet":    true,
	"duitnow_qr": true,
}

// List returns every stored rate for the workspace, ordered by method so the
// settings page and the checkout chooser render the same order on every read.
// A workspace that never configured fees returns an empty slice (not an error).
func (s *PaymentFeeStore) List(ctx context.Context, workspaceID string) ([]PaymentFeeRate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT method, fee_enabled, fee_fixed_sen, fee_percent_bp, updated_by, updated_at
		 FROM payment_fee_rates WHERE workspace_id=$1
		 ORDER BY array_position(ARRAY['fpx','card','ewallet','duitnow_qr'], method)`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaymentFeeRate
	for rows.Next() {
		var r PaymentFeeRate
		if err := rows.Scan(&r.Method, &r.Enabled, &r.FixedSen, &r.PercentBP, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ValidateFeeRate rejects a rate the schema would reject, before it reaches the
// database — one sentence at the API edge instead of a constraint violation.
// The percentage bound is strictly below 100%: at 100% the fee grows as fast as
// the amount and the gross-up has no solution, so it must never be storable.
func ValidateFeeRate(method string, fixedSen, percentBP int64) error {
	if !validFeeMethods[method] {
		return ErrFeeUnknownMethod
	}
	if fixedSen < 0 || percentBP < 0 || percentBP >= 10000 {
		return ErrFeeRateInvalid
	}
	return nil
}

// Replace stores the workspace's full rate set in one transaction: the rows in
// rates replace what was there, and a method absent from rates is REMOVED (so
// un-setting a method cannot leave a stale rate behind). Callers pass only the
// methods they want to keep.
func (s *PaymentFeeStore) Replace(ctx context.Context, workspaceID, updatedBy string, rates []PaymentFeeRate) error {
	for _, r := range rates {
		if err := ValidateFeeRate(r.Method, r.FixedSen, r.PercentBP); err != nil {
			return err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM payment_fee_rates WHERE workspace_id=$1`, workspaceID); err != nil {
		return err
	}
	for _, r := range rates {
		if _, err := tx.Exec(ctx,
			`INSERT INTO payment_fee_rates (workspace_id, method, fee_enabled, fee_fixed_sen, fee_percent_bp, updated_by, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,now())`,
			workspaceID, r.Method, r.Enabled, r.FixedSen, r.PercentBP, updatedBy); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Acknowledgement returns the workspace's acknowledgement, or nil when the
// merchant has never accepted the disclosure sentence.
func (s *PaymentFeeStore) Acknowledgement(ctx context.Context, workspaceID string) (*FeeAcknowledgement, error) {
	var a FeeAcknowledgement
	err := s.pool.QueryRow(ctx,
		`SELECT acknowledged_by, acknowledged_at FROM workspace_fee_acknowledgements WHERE workspace_id=$1`,
		workspaceID).Scan(&a.AcknowledgedBy, &a.AcknowledgedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Acknowledge records (or re-records) the merchant's acknowledgement. Re-recording
// is deliberate: whoever switched the fee on last is the person who accepted the
// disclosure, and that is who a later dispute has to point at.
func (s *PaymentFeeStore) Acknowledge(ctx context.Context, workspaceID, by string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO workspace_fee_acknowledgements (workspace_id, acknowledged_by, acknowledged_at)
		 VALUES ($1,$2,now())
		 ON CONFLICT (workspace_id) DO UPDATE
		   SET acknowledged_by=EXCLUDED.acknowledged_by, acknowledged_at=EXCLUDED.acknowledged_at`,
		workspaceID, by)
	return err
}

// EnabledRate is one method the buyer can choose, with the rate that applies to
// it. It is the shape the checkout page renders and the order route prices.
type EnabledRate struct {
	Method    string
	FixedSen  int64
	PercentBP int64
}

// ForForm resolves the fee policy for one form: the workspace's enabled rates,
// minus everything when the merchant never acknowledged the disclosure, and
// minus everything when THIS form switched the fee off. An empty result means
// the fee is not active for this checkout — the caller must then send the
// unmodified price and must not restrict the buyer's payment methods.
//
// formID may be empty (a formless/menu order): the form override then cannot
// apply and the workspace's settings decide.
func (s *PaymentFeeStore) ForForm(ctx context.Context, accountID, formID string) ([]EnabledRate, error) {
	wsID, err := s.WorkspaceIDForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if wsID == "" {
		return nil, nil
	}
	if formID != "" {
		var override string
		err := s.pool.QueryRow(ctx, `SELECT fee_override FROM forms WHERE id=$1 AND account_id=$2`,
			formID, accountID).Scan(&override)
		if errors.Is(err, pgx.ErrNoRows) {
			// A form that is not this account's: no override to honour, and the
			// sale itself will be refused elsewhere. Treat as inherit.
			override = ""
		} else if err != nil {
			return nil, err
		}
		if override == FeeOverrideOff {
			return nil, nil
		}
	}
	ack, err := s.Acknowledgement(ctx, wsID)
	if err != nil {
		return nil, err
	}
	if ack == nil {
		// The merchant never accepted the pass-on sentence. A fee must never
		// reach a buyer on an unacknowledged workspace, so the feature is
		// inactive here regardless of what the rate rows say.
		return nil, nil
	}
	rows, err := s.List(ctx, wsID)
	if err != nil {
		return nil, err
	}
	var out []EnabledRate
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		if r.FixedSen == 0 && r.PercentBP == 0 {
			// Enabled with no rate charges nothing: treat it as off rather than
			// offering the buyer a method whose "fee" is zero.
			continue
		}
		out = append(out, EnabledRate{Method: r.Method, FixedSen: r.FixedSen, PercentBP: r.PercentBP})
	}
	return out, nil
}

// FeeOverrideFor returns a form's fee override ("" or "off"). It is a dedicated
// accessor, which is why forms.fee_override stays out of formSelect (see
// intentionallyUnselected in migration_scan_drift_test.go).
func (s *FormStore) FeeOverrideFor(ctx context.Context, accountID, formID string) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT fee_override FROM forms WHERE id=$1 AND account_id=$2`,
		formID, accountID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrFormNotFound
	}
	return v, err
}

// SetFeeOverride stores a form's processing-fee override. Empty inherits the
// workspace; FeeOverrideOff switches the fee off for this form. Anything else is
// refused rather than silently stored (the CHECK constraint enforces the same).
func (s *FormStore) SetFeeOverride(ctx context.Context, accountID, formID, override string) error {
	if override != "" && override != FeeOverrideOff {
		return errors.New("invalid fee override")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE forms SET fee_override=$3 WHERE id=$2 AND account_id=$1`,
		accountID, formID, override)
	if isNoSuchRow(err) {
		return ErrFormNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFormNotFound
	}
	return nil
}

// WorkspaceIDForAccount resolves the workspace that owns an account. It mirrors
// WorkspaceStore.WorkspaceIDByAccount but keeps the fee store self-contained, so
// a handler holding only the fee store can still resolve the policy.
func (s *PaymentFeeStore) WorkspaceIDForAccount(ctx context.Context, accountID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT a.workspace_id FROM accounts a WHERE a.id=$1`, accountID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}
