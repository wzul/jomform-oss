// Processing-fee checkout surface (card t_135c3c72).
//
// The merchant sets their own MDR per CHIP payment method in the dashboard. When
// at least one method is active for a form, the buyer picks their method ON the
// JomForm page (a small step before paying), the server prices the processing fee
// with the gross-up in payments.GrossUp, and the CHIP purchase is created with
// that ONE method whitelisted and the grossed-up total as its amount — so the
// buyer cannot switch to a cheaper method at the CHIP page after seeing the fee.
//
// This file holds the ONE resolution both the page and the route use, plus the
// dashboard endpoints for the rates and the merchant's acknowledgement.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

var (
	// errFeeChooseMethod: more than one method is active and the client sent
	// none. The buyer must choose; guessing would price the wrong rate.
	errFeeChooseMethod = buyertext.New(buyertext.CodeOrderFeeMethodRequired, nil)
	// errFeeMethodInvalid: the client sent a method that is not active for this
	// checkout. Refused (400), never silently corrected — a corrected amount is
	// a charge the buyer did not agree to.
	errFeeMethodInvalid = buyertext.New(buyertext.CodeOrderFeeMethodInvalid, nil)
	// errFeeInactive: the client sent a fee method on a checkout where no fee is
	// active, so the amount it saw includes a charge that must not be applied.
	errFeeInactive = buyertext.New(buyertext.CodeOrderFeeInactive, nil)
	// errFeeTotalMismatch: the total the page displayed is not the total the
	// server computes now (a price or a rate changed under the buyer). Refused
	// rather than charging an amount that was never shown.
	errFeeTotalMismatch = buyertext.New(buyertext.CodeOrderTotalChanged, nil)
	// errTotalTooLarge: beyond payments.MaxAmountSen nothing can be priced
	// exactly, so the order is refused instead of priced with clamped numbers.
	errTotalTooLarge = buyertext.New(buyertext.CodeOrderTotalTooLarge, nil)
)

// feeChoiceView is one radio in the buyer's payment-method chooser on the
// public payment form (render_blocks.go). The rate rides on data attributes so
// the page's client-side gross-up uses the SAME numbers the server will.
type feeChoiceView struct {
	Method    string
	Label     string
	RateLabel string
	FixedSen  int64
	PercentBP int64
	Selected  bool
}

// feeChoiceViews is the template-facing chooser for the public renderers. The
// first active method is preselected so a workspace with one rate costs the
// buyer no extra click; with more than one the buyer must pick (and the order
// route refuses a submission that names none).
func (h *PublicHandlers) feeChoiceViews(ctx context.Context, f *dbstore.Form) []feeChoiceView {
	if f == nil {
		return nil
	}
	opts := h.feeOptions(ctx, f.AccountID, f.ID)
	out := make([]feeChoiceView, 0, len(opts))
	for i, o := range opts {
		out = append(out, feeChoiceView{
			Method: o.Method, Label: o.Label, RateLabel: o.RateLabel,
			FixedSen: o.FixedSen, PercentBP: o.PercentBP, Selected: i == 0,
		})
	}
	return out
}

// feeOption is one method the buyer may choose, with the rate that applies and
// the label shown both in the chooser and on the receipt.
type feeOption struct {
	Method    string
	Label     string // "Card"
	RateLabel string // "Card 2.00%" — the frozen wording
	FixedSen  int64
	PercentBP int64
}

// feeOptions resolves the active fee methods for a checkout. An empty result
// means the processing fee is not active here: the page must show the plain
// price and must not restrict the buyer's payment methods.
//
// Nil store (bare-mode tests) resolves to no fee, which is the pre-feature
// behaviour.
func (h *PublicHandlers) feeOptions(ctx context.Context, accountID, formID string) []feeOption {
	if h.PaymentFees == nil || accountID == "" {
		return nil
	}
	rates, err := h.PaymentFees.ForForm(ctx, accountID, formID)
	if err != nil {
		// A read failure must not silently charge nothing: but it also must not
		// break a checkout whose buyer is not paying a fee. Logging-free here is
		// deliberate — the caller's page/route both treat no rates as "fee off",
		// and the merchant's settings page surfaces the read error separately.
		return nil
	}
	out := make([]feeOption, 0, len(rates))
	for _, r := range rates {
		out = append(out, feeOption{
			Method:    r.Method,
			Label:     payments.FeeMethodLabel(r.Method),
			RateLabel: payments.FeeRateLabel(r.Method, r.FixedSen, r.PercentBP),
			FixedSen:  r.FixedSen,
			PercentBP: r.PercentBP,
		})
	}
	return out
}

// pickFeeOption applies the buyer's choice to the resolved options. When exactly
// one method is active it is the default, so the buyer is not asked to click for
// a choice that has only one answer (the card's requirement).
func pickFeeOption(opts []feeOption, chosen string) (feeOption, error) {
	if len(opts) == 0 {
		if chosen != "" {
			return feeOption{}, errFeeInactive
		}
		return feeOption{}, nil
	}
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		if len(opts) == 1 {
			return opts[0], nil
		}
		return feeOption{}, errFeeChooseMethod
	}
	for _, o := range opts {
		if o.Method == chosen {
			return o, nil
		}
	}
	return feeOption{}, errFeeMethodInvalid
}

// ─── Dashboard: rates + acknowledgement ─────────────────────────────────────

// restFeeRate is the JSON shape the settings page reads and writes. The money
// fields are integer SEN and the percentage is BASIS POINTS (100 = 1%).
type restFeeRate struct {
	Method    string `json:"method"`
	Enabled   bool   `json:"fee_enabled"`
	FixedSen  int64  `json:"fee_fixed_sen"`
	PercentBP int64  `json:"fee_percent_bp"`
	RateLabel string `json:"rate_label"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

type restFeeSettings struct {
	Rates []restFeeRate `json:"rates"`
	// Acknowledged is whether the merchant accepted the pass-on disclosure.
	// Without it the feature is inactive no matter what the rates say, which
	// is why the settings page shows the checkbox before the rates.
	Acknowledged        bool   `json:"acknowledged"`
	AcknowledgedBy      string `json:"acknowledged_by,omitempty"`
	AcknowledgedAt      string `json:"acknowledged_at,omitempty"`
	AcknowledgementText string `json:"acknowledgement_text"`
}

// feeAcknowledgementText is the sentence the settings page renders and the
// record agrees to. Defined in payments so the REST and MCP surfaces cannot
// drift from each other.
const feeAcknowledgementText = payments.FeeAcknowledgementText

// getFeeSettings returns every method's stored rate (including disabled ones, so
// the merchant sees what they saved) plus the acknowledgement state.
func (h *RESTHandlers) getFeeSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.PaymentFees == nil {
		writeErr(w, http.StatusServiceUnavailable, "fee settings are not available")
		return
	}
	wsID, err := h.PaymentFees.WorkspaceIDForAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	rows, err := h.PaymentFees.List(r.Context(), wsID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	byMethod := map[string]dbstore.PaymentFeeRate{}
	for _, row := range rows {
		byMethod[row.Method] = row
	}
	out := restFeeSettings{
		AcknowledgementText: feeAcknowledgementText,
		// Every storable method is listed, so the page cannot invent a method or
		// forget one the DB accepts.
		Rates: make([]restFeeRate, 0, len(payments.FeeMethods)),
	}
	for _, m := range payments.FeeMethods {
		row, ok := byMethod[m]
		rate := restFeeRate{Method: m}
		if ok {
			rate.Enabled = row.Enabled
			rate.FixedSen = row.FixedSen
			rate.PercentBP = row.PercentBP
			rate.UpdatedBy = row.UpdatedBy
		}
		rate.RateLabel = payments.FeeRateLabel(m, rate.FixedSen, rate.PercentBP)
		out.Rates = append(out.Rates, rate)
	}
	if ack, err := h.PaymentFees.Acknowledgement(r.Context(), wsID); err != nil {
		respondErr(w, r, err)
		return
	} else if ack != nil {
		out.Acknowledged = true
		out.AcknowledgedBy = ack.AcknowledgedBy
		out.AcknowledgedAt = ack.AcknowledgedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	writeJSON(w, http.StatusOK, out)
}

// putFeeSettings replaces the workspace's rate set. Enabling a method REQUIRES
// the acknowledgement in the same request: the merchant must have ticked the
// disclosure box, and whoever did is recorded with the time.
func (h *RESTHandlers) putFeeSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.PaymentFees == nil {
		writeErr(w, http.StatusServiceUnavailable, "fee settings are not available")
		return
	}
	var in struct {
		Rates []struct {
			Method    string `json:"method"`
			Enabled   bool   `json:"fee_enabled"`
			FixedSen  int64  `json:"fee_fixed_sen"`
			PercentBP int64  `json:"fee_percent_bp"`
		} `json:"rates"`
		Acknowledged bool `json:"acknowledged"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	wsID, err := h.PaymentFees.WorkspaceIDForAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Who switched it on: the session user's email when there is one, else the
	// form's account id. A blank "who" would make the acknowledgement
	// unattributable, which is the one thing it exists for.
	actor := ""
	if u, uerr := h.profileUser(r); uerr == nil && u != nil {
		actor = u.Email
	}
	if actor == "" && actx.Account != nil && actx.Account.OwnerUserID != "" {
		// An API-key call has no session user; record the workspace owner, who
		// is the legal person the acknowledgement belongs to.
		if owner, oerr := h.Users.ByID(r.Context(), actx.Account.OwnerUserID); oerr == nil && owner != nil {
			actor = owner.Email
		}
	}
	if actor == "" {
		actor = actx.Account.ID
	}
	enabling := false
	rates := make([]dbstore.PaymentFeeRate, 0, len(in.Rates))
	for _, row := range in.Rates {
		method := strings.TrimSpace(row.Method)
		// A method that is already disabled is dropped rather than stored: the
		// resolution ignores disabled rows anyway, and keeping a stale rate for
		// a method the merchant turned off is how a "0%" row outlives its
		// meaning.
		if !row.Enabled && row.FixedSen == 0 && row.PercentBP == 0 {
			continue
		}
		rates = append(rates, dbstore.PaymentFeeRate{
			Method: method, Enabled: row.Enabled, FixedSen: row.FixedSen, PercentBP: row.PercentBP,
		})
		if err := dbstore.ValidateFeeRate(method, row.FixedSen, row.PercentBP); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if row.Enabled && (row.FixedSen > 0 || row.PercentBP > 0) {
			enabling = true
		}
	}
	if enabling && !in.Acknowledged {
		// Consumer Protection Act 1999: the price shown must be the price paid.
		// Switching the fee on without the merchant accepting the disclosure
		// sentence would let a charge reach a buyer that nobody acknowledged
		// passing on.
		writeErr(w, http.StatusBadRequest, "tick the disclosure acknowledgement before enabling the processing fee")
		return
	}
	if enabling {
		if err := h.PaymentFees.Acknowledge(r.Context(), wsID, actor); err != nil {
			respondErr(w, r, err)
			return
		}
	}
	if err := h.PaymentFees.Replace(r.Context(), wsID, actor, rates); err != nil {
		if errors.Is(err, dbstore.ErrFeeRateInvalid) || errors.Is(err, dbstore.ErrFeeUnknownMethod) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// putFormFeeOverride sets a form's processing-fee override: "" inherits the
// workspace, "off" switches the fee off for this form.
func (h *RESTHandlers) putFormFeeOverride(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	formID := r.PathValue("id")
	var in struct {
		Override string `json:"fee_override"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if err := h.Forms.SetFeeOverride(r.Context(), actx.Account.ID, formID, strings.TrimSpace(in.Override)); err != nil {
		if errors.Is(err, dbstore.ErrFormNotFound) {
			writeErr(w, http.StatusNotFound, "form not found")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid fee override")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// getFormFeeOverride reads a form's processing-fee override.
func (h *RESTHandlers) getFormFeeOverride(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	formID := r.PathValue("id")
	ov, err := h.Forms.FeeOverrideFor(r.Context(), actx.Account.ID, formID)
	if err != nil {
		if errors.Is(err, dbstore.ErrFormNotFound) {
			writeErr(w, http.StatusNotFound, "form not found")
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"fee_override": ov})
}
