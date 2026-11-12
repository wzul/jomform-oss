// MCP parity tools for the buyer-borne processing fee (card t_135c3c72).
//
// The dashboard's Settings -> Payment fees tab reads and writes one workspace's
// per-method rates through /v1/rest/settings/payment-fees; these two tools are
// the same surface over MCP. Parity is a rule in AGENTS.md (every dashboard
// feature needs an MCP tool) and MCP-PARITY.md names both of them for
// mcp_parity_test.go.
//
// The write tool carries the same gate as the REST PUT: a method may only be
// ENABLED when the merchant acknowledges the pass-on disclosure, and the tool
// records who did it. A fee must never reach a buyer on an unacknowledged
// workspace, however it was switched on.
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// errPaymentFeesUnavailable is returned when the fee store is not wired (a
// bare-mode deployment). Refusing loudly is deliberate: a missing store must
// never read as "the fee is off", which would silently drop a charge the
// merchant configured.
var errPaymentFeesUnavailable = errors.New("processing fee settings not available")

// errPaymentFeeAckRequired mirrors the REST PUT's refusal: a method may not be
// enabled without the merchant's pass-on acknowledgement.
var errPaymentFeeAckRequired = errors.New("tick the disclosure acknowledgement before enabling a fee")

// feeRateIn is one method's stored charge. fpx (FPX B2C, personal) and
// fpx_b2b1 (FPX B2B1, corporate) are two separate methods (card t_1d0198e3), so
// a workspace can carry a different amount for each.
//
// Two models, one of them the buyer's path: PlatformFeeSen is a flat amount in
// SEN added to the buyer's price as its OWN purchase line; FixedSen +
// PercentBP are the LEGACY gross-up kept for one release (100 BP = 1%).
type feeRateIn struct {
	Method  string `json:"method" jsonschema:"CHIP method: fpx (FPX B2C), fpx_b2b1 (FPX B2B1), card, ewallet or duitnow_qr"`
	Enabled bool   `json:"fee_enabled" jsonschema:"Enable the charge for this method (payer pays it)"`
	// PlatformFeeSen is the model going forward: a flat amount added to the
	// buyer's price as its OWN line in the CHIP purchase. Ptr on purpose — an
	// absent field keeps the stored amount, so a client that predates the
	// platform fee cannot silently zero it.
	PlatformFeeSen *int64 `json:"platform_fee_sen,omitempty" jsonschema:"PLATFORM FEE in SEN (RM1.00 = 100): a flat amount added on top of the buyer's price as its own purchase line. Omit to keep the stored amount"`
	FixedSen       int64  `json:"fee_fixed_sen,omitempty" jsonschema:"LEGACY: fixed fee in SEN (RM1.00 = 100) for the old gross-up model. Ignored when platform_fee_sen is set"`
	PercentBP      int64  `json:"fee_percent_bp,omitempty" jsonschema:"LEGACY: percentage fee in BASIS POINTS (100 = 1%) for the old gross-up model. Must be less than 10000 (99.99%)"`
}

// feeRateOut is the read shape for one method.
type feeRateOut struct {
	Method         string `json:"method"`
	Enabled        bool   `json:"fee_enabled"`
	PlatformFeeSen int64  `json:"platform_fee_sen"`
	FixedSen       int64  `json:"fee_fixed_sen"`
	PercentBP      int64  `json:"fee_percent_bp"`
	RateLabel      string `json:"rate_label"`
	// LegacyGrossUp flags a row still priced by the old percentage gross-up
	// (migration 000090). A merchant reading this knows why the amount moves
	// with the price instead of being the flat platform amount.
	LegacyGrossUp bool   `json:"legacy_grossup"`
	UpdatedBy     string `json:"updated_by,omitempty"`
}

type paymentFeesOut struct {
	AccountID           string       `json:"account_id"`
	Rates               []feeRateOut `json:"rates"`
	Acknowledged        bool         `json:"acknowledged"`
	AcknowledgedBy      string       `json:"acknowledged_by,omitempty"`
	AcknowledgedAt      string       `json:"acknowledged_at,omitempty"`
	AcknowledgementText string       `json:"acknowledgement_text"`
}

type getPaymentFeesIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace ID (from list_workspaces). Empty = your own account"`
}

// getPaymentFees reads every method's stored rate plus the acknowledgement.
func (s *Server) getPaymentFees(
	ctx context.Context, _ *mcp.CallToolRequest, _ getPaymentFeesIn,
) (*mcp.CallToolResult, *paymentFeesOut, error) {
	if s.deps.PaymentFees == nil {
		return nil, nil, errPaymentFeesUnavailable
	}
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	out, outErr := s.paymentFeesOut(ctx, accountID)
	if outErr != nil {
		return nil, nil, outErr
	}
	return nil, out, nil
}

type setPaymentFeesIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace ID (from list_workspaces). REQUIRED: this write tool rejects an empty workspace_id — call list_workspaces first"`
	// Acknowledged is the merchant's acceptance of the pass-on disclosure. It is
	// REQUIRED when any method is enabled: without it no fee may reach a buyer.
	Acknowledged bool        `json:"acknowledged" jsonschema:"The account owner's acknowledgement that this fee is passed on to the payer and is subject to acquirer rules. REQUIRED when enabling a fee"`
	Rates        []feeRateIn `json:"rates" jsonschema:"ALL methods to keep. Methods absent here will be DELETED"`
}

// setPaymentFees replaces the workspace's rate set.
func (s *Server) setPaymentFees(
	ctx context.Context, _ *mcp.CallToolRequest, in setPaymentFeesIn,
) (*mcp.CallToolResult, *paymentFeesOut, error) {
	if s.deps.PaymentFees == nil {
		return nil, nil, errPaymentFeesUnavailable
	}
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	wsID, err := s.deps.PaymentFees.WorkspaceIDForAccount(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	// platform_fee_sen is optional in the payload. An absent field means "keep
	// the stored amount", so an agent that only knows the legacy rate fields can
	// still send a rates array without wiping a platform fee set elsewhere.
	stored, err := s.deps.PaymentFees.List(ctx, wsID)
	if err != nil {
		return nil, nil, err
	}
	storedPlatform := map[string]int64{}
	for _, row := range stored {
		storedPlatform[row.Method] = row.PlatformFeeSen
	}
	enabling := false
	rates := make([]dbstore.PaymentFeeRate, 0, len(in.Rates))
	for _, r := range in.Rates {
		platform := storedPlatform[r.Method]
		if r.PlatformFeeSen != nil {
			platform = *r.PlatformFeeSen
		}
		if !r.Enabled && r.FixedSen == 0 && r.PercentBP == 0 && platform == 0 {
			continue
		}
		if err := dbstore.ValidateFeeRate(r.Method, r.FixedSen, r.PercentBP, platform); err != nil {
			return nil, nil, err
		}
		rates = append(rates, dbstore.PaymentFeeRate{
			Method: r.Method, Enabled: r.Enabled, FixedSen: r.FixedSen,
			PercentBP: r.PercentBP, PlatformFeeSen: platform,
		})
		if r.Enabled && (r.FixedSen > 0 || r.PercentBP > 0 || platform > 0) {
			enabling = true
		}
	}
	if enabling && !in.Acknowledged {
		return nil, nil, errPaymentFeeAckRequired
	}
	actor := s.paymentFeeActor(ctx, accountID)
	if enabling {
		if err := s.deps.PaymentFees.Acknowledge(ctx, wsID, actor); err != nil {
			return nil, nil, err
		}
	}
	if err := s.deps.PaymentFees.Replace(ctx, wsID, actor, rates); err != nil {
		return nil, nil, err
	}
	out, outErr := s.paymentFeesOut(ctx, accountID)
	if outErr != nil {
		return nil, nil, outErr
	}
	return nil, out, nil
}

// paymentFeeActor is the "who" recorded on the acknowledgement: the workspace
// owner's email when it can be resolved, else the account id. It mirrors the
// REST PUT, which records the session user (or the owner for an API-key call).
func (s *Server) paymentFeeActor(ctx context.Context, accountID string) string {
	if s.deps.Accounts != nil && s.deps.Users != nil {
		if acc, err := s.deps.Accounts.ByID(ctx, accountID); err == nil && acc != nil && acc.OwnerUserID != "" {
			if u, uerr := s.deps.Users.ByID(ctx, acc.OwnerUserID); uerr == nil && u != nil {
				return u.Email
			}
		}
	}
	return accountID
}

// paymentFeesOut builds the read shape from the store — one row per storable
// method, so an agent sees the same complete list the dashboard renders.
func (s *Server) paymentFeesOut(ctx context.Context, accountID string) (*paymentFeesOut, error) {
	wsID, err := s.deps.PaymentFees.WorkspaceIDForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	rows, err := s.deps.PaymentFees.List(ctx, wsID)
	if err != nil {
		return nil, err
	}
	byMethod := map[string]dbstore.PaymentFeeRate{}
	for _, row := range rows {
		byMethod[row.Method] = row
	}
	out := &paymentFeesOut{
		AccountID:           accountID,
		Rates:               make([]feeRateOut, 0, len(payments.FeeMethods)),
		AcknowledgementText: payments.FeeAcknowledgementText,
	}
	for _, m := range payments.FeeMethods {
		row, ok := byMethod[m]
		rate := feeRateOut{Method: m}
		if ok {
			rate.Enabled = row.Enabled
			rate.PlatformFeeSen = row.PlatformFeeSen
			rate.FixedSen = row.FixedSen
			rate.PercentBP = row.PercentBP
			rate.LegacyGrossUp = row.PercentBP > 0
			rate.UpdatedBy = row.UpdatedBy
		}
		if rate.LegacyGrossUp {
			rate.RateLabel = payments.FeeRateLabel(m, rate.FixedSen, rate.PercentBP)
		} else {
			platform := rate.PlatformFeeSen
			if platform == 0 {
				platform = rate.FixedSen
			}
			rate.RateLabel = payments.FeeRateLabel(m, platform, 0)
		}
		out.Rates = append(out.Rates, rate)
	}
	ack, err := s.deps.PaymentFees.Acknowledgement(ctx, wsID)
	if err != nil {
		return nil, err
	}
	if ack != nil {
		out.Acknowledged = true
		out.AcknowledgedBy = ack.AcknowledgedBy
		out.AcknowledgedAt = ack.AcknowledgedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out, nil
}
