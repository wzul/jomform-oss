package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// PurchaseArgs is the platform-level purchase request (pre-CHIP mapping).
type PurchaseArgs struct {
	BrandID      string
	ClientEmail  string
	ClientName   string
	ClientPhone  string
	Reference    string
	Currency     string
	LineItems    []dbstore.SaleItem
	SuccessURL   string
	FailureURL   string
	CallbackURL  string
	SaveCard     bool
	CreatorAgent string
}

// toCHIPProducts maps JomForm items to CHIP products. Prices already in sen.
func toCHIPProducts(items []dbstore.SaleItem) []chip.Product {
	out := make([]chip.Product, 0, len(items))
	for _, it := range items {
		out = append(out, chip.Product{
			Name:     it.Name,
			Price:    it.PriceSen,
			Quantity: fmt.Sprintf("%d", it.Qty),
		})
	}
	return out
}

// saleClientDetails builds the CHIP client details from the sale's resolved
// buyer record (issue #186). clientEmail is the effective email after the
// account-level fallback is applied (CHIP requires a client email). All other
// fields come straight from the sale's client blob, which handleSubmit has
// already resolved from the merchant's chip_mapping.
func saleClientDetails(sale *dbstore.Sale, clientEmail string) *chip.ClientDetails {
	return &chip.ClientDetails{
		Email:           clientEmail,
		FullName:        sale.Client.FullName,
		Phone:           sale.Client.Phone,
		StreetAddress:   sale.Client.StreetAddress,
		City:            sale.Client.City,
		State:           sale.Client.State,
		ZipCode:         sale.Client.ZipCode,
		Country:         sale.Client.Country,
		LegalName:       sale.Client.LegalName,
		RegistrationNum: sale.Client.RegistrationNum,
		TaxNumber:       sale.Client.TaxNumber,
	}
}

// CreateChipPurchaseForSale creates the CHIP Purchase for an existing
// pending sale and attaches the checkout URL + purchase id.
func (s *Service) CreateChipPurchaseForSale(ctx context.Context, sale *dbstore.Sale) error {
	acc, err := s.Accounts.ByID(ctx, sale.AccountID)
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}
	if acc.ChipBrandID == "" || acc.ChipSecretKeyEnc == "" {
		return ErrNotConfigured
	}
	secret, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return fmt.Errorf("decrypt key: %w", err)
	}

	// Email fallback (CHIP requires a client email). Subscription sales MUST
	// carry the real buyer email for future charges — fallback applies to
	// one-time sales only, consistent with the CHIP WooCommerce plugin.
	clientEmail := sale.Client.Email
	if clientEmail == "" && sale.SaleType != dbstore.SaleTypeSubscription && acc.ChipEmailFallback != "" {
		clientEmail = acc.ChipEmailFallback
	}

	whitelist := s.ResolveWhitelist(ctx, string(secret), sale.Currency, sale.TotalSen, acc.ChipMethodWhitelist)

	sRedirect, fRedirect := s.formRedirects(ctx, acc.ID, sale.FormID)
	// Issue #83: thank-you redirects must land on the workspace subdomain (or
	// custom domain) the buyer came from, not the apex base. Fall back to the
	// apex base only when no workspace/custom domain is resolvable.
	tyBase := s.workspaceBaseURL(ctx, acc.ID)
	if tyBase == "" {
		tyBase = s.BaseURL
	}
	if sRedirect == "" {
		sRedirect = fmt.Sprintf("%s/t/%s", tyBase, sale.RefCode)
	}
	if fRedirect == "" {
		fRedirect = fmt.Sprintf("%s/t/%s?status=fail", tyBase, sale.RefCode)
	}
	req := chip.PurchaseRequest{
		SkipCapture:            sale.SkipCapture,
		CreatorAgent:           "JomForm",
		BrandID:                acc.ChipBrandID,
		PaymentMethodWhitelist: whitelist,
		Client:                 saleClientDetails(sale, clientEmail),
		ForceRecurring:         sale.SaleType == dbstore.SaleTypeSubscription, // card must be saved for future charges
		Purchase: chip.PurchaseDetails{
			Products:        toCHIPProducts(sale.Items),
			Currency:        sale.Currency,
			Reference:       sale.RefCode,
			SuccessRedirect: sRedirect,
			FailureRedirect: fRedirect,
			SuccessCallback: fmt.Sprintf("%s/v1/payments/chip/callback", s.BaseURL),
			CreatorAgent:    "JomForm",
		},
	}

	p, err := s.Chip.CreatePurchase(ctx, string(secret), req)
	if err != nil {
		return fmt.Errorf("chip create: %w", err)
	}
	if err := s.Sales.AttachChipPurchase(ctx, sale.ID, p.ID, p.CheckoutURL); err != nil {
		return fmt.Errorf("attach purchase: %w", err)
	}
	sale.ChipPurchaseID = p.ID
	sale.ChipCheckoutURL = p.CheckoutURL
	return nil
}

// ProcessCallback handles a verified purchase.paid / payment_failure /
// pending_charge event. idempotent on (chip_purchase_id, event_type).
//
// Issue #202: direct-sale events are serialized per sale via a Postgres
// advisory lock (WithSaleLock). Only one callback processes a given sale at a
// time; a second event for the same sale blocks until the first commits, then
// re-reads the sale's current status and applies the transition guard. This
// prevents a success_callback + success_redirect (or duplicate/different
// events) from double-processing or flipping the status.
func (s *Service) ProcessCallback(ctx context.Context, p *chip.Purchase) error {
	if p.ID == "" {
		return errors.New("callback missing purchase id")
	}

	sale, err := s.Sales.ByChipPurchaseID(ctx, p.ID)
	if err != nil {
		// Not a direct sale — may be a subscription charge or token purchase.
		return s.processSubscriptionEvent(ctx, p)
	}

	// Serialize per sale: acquire the advisory lock, re-read the sale inside
	// the transaction (so we see the locked, current status), apply the
	// transition guard, and only run side effects on a real transition.
	return s.Sales.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		return s.ProcessCallbackTx(ctx, tx, p)
	})
}

// ProcessCallbackTx runs the direct-sale callback transitions inside an
// already-open transaction (the per-sale advisory lock transaction opened by
// WithSaleLock, issue #202).
//
// Split out of ProcessCallback for issue #517: the HTTP callback handler claims
// the inbound event in the SAME transaction as the state change, so a failed
// attempt can never leave a consumed dedup key behind. Callers must hold the
// per-sale lock (or an equivalent serialization) for the sale that owns
// p.ID — the transition guard alone is not a concurrency guard.
func (s *Service) ProcessCallbackTx(ctx context.Context, tx pgx.Tx, p *chip.Purchase) error {
	cur, err := s.Sales.SaleByChipPurchaseIDTx(ctx, tx, p.ID)
	if err != nil {
		return err
	}

	switch p.Status {
	case chip.StatusPaid:
		// Transition guard: only pending → paid is a real transition.
		// A sale already paid (or confirmed/cancelled) must not re-fire
		// webhooks or re-send the receipt.
		if cur.Status == dbstore.SalePaid {
			slog.Info("callback_paid_idempotent", "purchase", p.ID, "sale", cur.ID)
			return nil
		}
		if cur.Status != dbstore.SalePending {
			slog.Warn("callback_paid_conflicting_transition",
				"purchase", p.ID, "sale", cur.ID, "from", cur.Status, "to", dbstore.SalePaid)
			return nil
		}
		if err := s.Sales.SetStatusTx(ctx, tx, cur.ID, dbstore.SalePaid); err != nil {
			return err
		}
		// Issue #245: a successful payment means the entry is legit — a
		// spam-flagged sale that gets paid is automatically un-spammed
		// (atomic with the status transition under the sale lock).
		if cur.IsSpam {
			if err := s.Sales.UnSpamTx(ctx, tx, cur.ID); err != nil {
				return err
			}
			slog.Info("sale_paid_auto_unspam", "sale", cur.ID, "ref", cur.RefCode)
		}
		if s.OnPaid != nil {
			event := "sale.paid"
			if cur.SaleType == dbstore.SaleTypeSubscription {
				event = "subscription.charged"
			}
			go s.OnPaid(ctx, cur.AccountID, event, map[string]any{
				"ref_code": cur.RefCode, "amount_sen": cur.TotalSen,
				"buyer_email": cur.Client.Email, "purchase_id": p.ID,
				// Issue #196: form-scoped webhooks key off the sale's form.
				"form_id": cur.FormID,
				// Issue #438: QR ordering flow — the table number the QR
				// stand encoded, so the merchant's webhook knows where to serve.
				"table_number": cur.TableNumber,
			})
		}
		// Issue #193: fire per-form notifications on the paid event.
		event := "sale.paid"
		if cur.SaleType == dbstore.SaleTypeSubscription {
			event = "subscription.charged"
		}
		s.fireSaleNotifications(ctx, cur, event)
		// Receipt (one-time sales only; subscription receipts follow their
		// own charge lifecycle). Never blocks the payment flow.
		if cur.SaleType != dbstore.SaleTypeSubscription {
			s.sendReceiptAsync(ctx, cur)
		}
		// Subscription first payment: token purchase completed → activate sub.
		if cur.SaleType == dbstore.SaleTypeSubscription {
			return s.activateSubscription(ctx, cur)
		}
	case chip.StatusError:
		// Transition guard: only pending → failed is a real transition.
		// A late payment_failure after the sale is already paid must not
		// flip it back to failed.
		if cur.Status == dbstore.SaleFailed {
			slog.Info("callback_failed_idempotent", "purchase", p.ID, "sale", cur.ID)
			return nil
		}
		if cur.Status != dbstore.SalePending {
			slog.Warn("callback_failed_conflicting_transition",
				"purchase", p.ID, "sale", cur.ID, "from", cur.Status, "to", dbstore.SaleFailed)
			return nil
		}
		if s.OnPaid != nil {
			go s.OnPaid(ctx, cur.AccountID, "sale.failed", map[string]any{
				"ref_code": cur.RefCode, "buyer_email": cur.Client.Email,
			})
		}
		// Issue #193: fire per-form notifications on the failed event.
		s.fireSaleNotifications(ctx, cur, "sale.failed")
		if err := s.Sales.SetStatusTx(ctx, tx, cur.ID, dbstore.SaleFailed); err != nil {
			return err
		}
	case chip.StatusPendingCharge:
		// transient; nothing to persist beyond event row (caller stores raw event)
	case chip.StatusRefunded:
		// Issue #407: a refunded purchase (initiated from the Sales page or
		// manually in the CHIP dashboard) flips the sale to refunded. Only a
		// real transition from a paid/confirmed state is applied; an already
		// refunded sale is idempotent.
		if cur.Status == dbstore.SaleRefunded {
			slog.Info("callback_refunded_idempotent", "purchase", p.ID, "sale", cur.ID)
			return nil
		}
		if cur.Status != dbstore.SalePaid && cur.Status != dbstore.SaleConfirmed {
			slog.Warn("callback_refunded_conflicting_transition",
				"purchase", p.ID, "sale", cur.ID, "from", cur.Status, "to", dbstore.SaleRefunded)
			return nil
		}
		if err := s.Sales.SetStatusTx(ctx, tx, cur.ID, dbstore.SaleRefunded); err != nil {
			return err
		}
		// Issue #518: the refunded purchase is terminal at CHIP, so record
		// the refunded amount as the FULL total. Without this an
		// out-of-band refund (CHIP dashboard / another integration) would
		// leave refunded_sen behind a `refunded` status, which would
		// misstate the balance and trip the refunded_sen <= total_sen money
		// invariant. The refunded event carries no amount, so the full
		// total is the only value consistent with the terminal status.
		if err := s.Sales.SetRefundedSenTx(ctx, tx, cur.ID, cur.TotalSen); err != nil {
			return err
		}
		if s.OnPaid != nil {
			go s.OnPaid(ctx, cur.AccountID, "sale.refunded", map[string]any{
				"ref_code": cur.RefCode, "buyer_email": cur.Client.Email,
				"purchase_id": p.ID, "form_id": cur.FormID,
			})
		}
		s.fireSaleNotifications(ctx, cur, "sale.refunded")
	}
	return nil
}

// sendReceiptAsync builds and fires the buyer receipt for a one-time sale in
// the background (issue #202: only called on a real pending → paid transition,
// so a sale already marked paid never re-sends the receipt).
func (s *Service) sendReceiptAsync(ctx context.Context, sale *dbstore.Sale) {
	summary := itemsSummary(sale.Items)
	tin, msic := "", ""
	if acc2, aerr := s.Accounts.ByID(ctx, sale.AccountID); aerr == nil {
		tin, msic = acc2.EinvoiceTIN, acc2.EinvoiceMSIC
	}
	// Issue #83: 'Borang dibeli' must link to the purchased form's
	// canonical workspace-scoped URL ({workspace}.jomform.com/{slug} or a
	// verified custom domain), not the apex thank-you page. Fall back
	// to the apex thank-you page only when no workspace/custom domain is
	// resolvable.
	formURL := s.formPublicURL(ctx, sale.AccountID, sale.FormID)
	if formURL == "" {
		formURL = s.BaseURL + "/t/" + sale.RefCode
	}
	// Issue #107: resolve the workspace id so the receipt renders from the
	// workspace's per-workspace email settings (defaults when none).
	workspaceID := ""
	if s.Workspaces != nil {
		if sid, serr := s.Workspaces.WorkspaceIDByAccount(ctx, sale.AccountID); serr == nil {
			workspaceID = sid
		}
	}
	// Language consistency: resolve the BUYER-facing language on the form/
	// workspace axis. The form is already in hand via formPublicURL's lookup, and
	// the workspace default comes from the same chain the public page uses —
	// never the merchant's personal users.lang.
	formLang := s.formLangForSale(ctx, sale)
	wsLang := ""
	if s.Workspaces != nil && workspaceID != "" {
		if st, werr := s.Workspaces.Settings(ctx, workspaceID); werr == nil && st != nil {
			wsLang = st.WorkspaceLang()
		}
	}
	go s.SendSaleReceipt(ctx, SaleRef{
		SaleID:        sale.ID,
		RefCode:       sale.RefCode,
		BuyerEmail:    sale.Client.Email,
		BuyerName:     sale.Client.FullName,
		AmountSen:     sale.TotalSen,
		ItemsSummary:  summary,
		FormURL:       formURL,
		WorkspaceID:   workspaceID,
		FormID:        sale.FormID,
		TIN:           tin,
		MSIC:          msic,
		FormLang:      formLang,
		WorkspaceLang: wsLang,
	})
}

// formLangForSale reads the language a sale's form declares in its own
// `config.lang` (issue #57). "" means the form carries no usable override, so
// the receipt falls through to the workspace default. Best-effort: an unreadable
// form contributes nothing rather than blocking the receipt.
func (s *Service) formLangForSale(ctx context.Context, sale *dbstore.Sale) string {
	if s.Forms == nil || sale == nil || sale.FormID == "" {
		return ""
	}
	lang, err := s.Forms.FormLang(ctx, sale.FormID)
	if err != nil {
		slog.Warn("receipt_form_lang_unresolved", "form", sale.FormID, "err", err)
		return ""
	}
	return lang
}

// activateSubscription records the token purchase and schedules the next charge.
func (s *Service) activateSubscription(ctx context.Context, sale *dbstore.Sale) error {
	sub, err := s.Subs.ByTokenPurchase(ctx, sale.ChipPurchaseID)
	if err == nil {
		// Already activated (idempotent redelivery).
		_ = sub
		return nil
	}

	// Issue #560: the terms come from the SALE, recorded when the checkout
	// resolved the subscription product.
	//
	// Two things used to be wrong here:
	//   - period was hardcoded "month" ("product lookup lands with products
	//     store wiring" — a TODO never finished), so a YEARLY product would have
	//     been billed monthly;
	//   - the amount was sale.TotalSen, so a cart mixing a subscription with
	//     one-time items would have re-billed those one-time items every period.
	//
	// Issue #623: the period recorded on the sale is already the EFFECTIVE cycle
	// (the chosen variation's when it carries one, otherwise the product's), and
	// the variation id rides along so every later charge resolves its terms
	// through the same rule.
	period := sale.SubscriptionPeriod
	if period != "month" && period != "year" {
		period = "month" // a sale with no recorded terms (pre-#560 rows)
	}
	amount := sale.SubscriptionAmountSen
	if amount <= 0 {
		amount = sale.TotalSen
	}
	productID := sale.SubscriptionProductID
	if productID == "" {
		productID = firstProductID(sale)
	}
	next := nextChargeFrom(time.Now(), period)

	newSub := &dbstore.Subscription{
		AccountID:           sale.AccountID,
		ClientEmail:         sale.Client.Email,
		ProductID:           productID,
		VariationID:         sale.SubscriptionVariationID,
		SaleID:              sale.ID,
		ChipTokenPurchaseID: sale.ChipPurchaseID,
		Status:              dbstore.SubActive,
		Period:              period,
		AmountSen:           amount,
		NextChargeAt:        next,
	}
	if err := s.Subs.Create(ctx, newSub); err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	// The initial token purchase is the customer's FIRST payment, and it is
	// already paid at this point — we are in the purchase.paid callback. Record
	// it as PAID, stamped with the token purchase id.
	//
	// It used to be recorded as PENDING with an empty chip_purchase_id. That was
	// a phantom outstanding charge: ClaimDue refuses to charge a subscription
	// that has a pending row, and MarkChargePaid/MarkChargeFailed can only match
	// a row BY chip_purchase_id — which this row never had. Nothing else ever
	// reconciled it, so no subscription was EVER charged a second time: the
	// recurring engine silently did nothing for every subscription (whether the
	// product had a fixed charge count or was infinite).
	saleType := dbstore.ChargePaid
	if err := s.Subs.AddCharge(ctx, newSub.ID, sale.ChipPurchaseID, amount, saleType, 0); err != nil {
		return err
	}
	// A fixed billing count (subscription_charges = N) counts PAID charges, so if
	// N == 1 the whole subscription is already complete: there is no month 2.
	_, err = s.maybeCompleteSubscription(ctx, newSub)
	return err
}

// itemsSummary renders "2× Tee RM45.00, 1× Cap RM25.00"-style text. When a
// variation was bought (issue #153) the variation name is appended so the
// receipt shows which variation was purchased, e.g. "1× Tee (Size M)".
func itemsSummary(items []dbstore.SaleItem) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		label := it.Name
		if it.VariationName != "" {
			label = it.Name + " (" + it.VariationName + ")"
		}
		parts = append(parts, fmt.Sprintf("%d× %s", it.Qty, label))
	}
	return strings.Join(parts, ", ")
}

func nextChargeFrom(now time.Time, period string) time.Time {
	if period == "year" {
		return now.AddDate(1, 0, 0)
	}
	return now.AddDate(0, 1, 0)
}

// Schedule-mode values for products.schedule_mode (round 13c). They decide the
// next-charge date after a SUCCESSFUL charge.
const (
	// ScheduleFromPayment computes the next charge from the moment the charge
	// succeeded. This is the historical behaviour: a late charge permanently
	// shifts every later charge.
	ScheduleFromPayment = "from_payment"
	// ScheduleCatchUp computes it from the PLANNED date plus one period, so a
	// period that was charged late is still collected in full — the schedule
	// returns to its original anchors. For a fixed-term class (RM100 x 3) the
	// three periods land on the original dates even after a failure.
	ScheduleCatchUp = "catch_up"
	// ScheduleSkipMissed jumps to the next planned date that is still in the
	// FUTURE, so a period missed during a failure is never collected. The
	// schedule also returns to its original anchors.
	ScheduleSkipMissed = "skip_missed"
)

// ValidScheduleMode reports whether a caller-supplied schedule mode is one of
// the three supported values.
func ValidScheduleMode(m string) bool {
	switch m {
	case ScheduleFromPayment, ScheduleCatchUp, ScheduleSkipMissed:
		return true
	}
	return false
}

// nextChargeForPolicy returns the next charge date after a successful charge.
//
//	now     = the moment the charge succeeded
//	planned = the date this charge was DUE (subscriptions.next_charge_at)
//
// from_payment is `now` — the historical behaviour, where a late charge
// permanently shifts every later charge.
//
// The two fixed-schedule modes keep the subscription on its ORIGINAL anchors
// (e.g. the 1st of the month) by walking forward from the planned date in whole
// periods. A charge is always created at or after the period it settles, so
// `planned` is the start of the period being paid and `planned + 1 period` is
// the next UNPAID period. That is why the two modes differ by exactly one step:
//
//	catch_up    -> planned + 1 period. Collects the period that came next, even
//	               if `now` has already passed it, so a missed month is still
//	               billed. Walking from the true planned date converges on its
//	               own, so the same period is never collected twice.
//	skip_missed -> the first anchor at or after `now`. The period missed during
//	               the failure is dropped.
//
// Both fall back to normal billing when nothing was missed (planned is in the
// future, or zero), so an on-time charge behaves identically in every mode.
func nextChargeForPolicy(mode string, now, planned time.Time, period string, paidLate bool) time.Time {
	if mode != ScheduleCatchUp && mode != ScheduleSkipMissed {
		return nextChargeFrom(now, period) // from_payment (default)
	}
	if planned.IsZero() || planned.After(now) {
		return nextChargeFrom(now, period)
	}
	if mode == ScheduleCatchUp {
		return nextChargeFrom(planned, period)
	}
	// skip_missed: first anchor at or after now.
	next := planned
	for i := 0; i < 600; i++ { // 600 periods ≈ 50 years: always terminates
		next = nextChargeFrom(next, period)
		if !next.Before(now) {
			return next
		}
	}
	return nextChargeFrom(now, period)
}

func firstProductID(sale *dbstore.Sale) string {
	if len(sale.Items) > 0 {
		return sale.Items[0].ProductID
	}
	return ""
}

// processSubscriptionEvent routes charge events (child purchases) by looking
// up which subscription charge row references the purchase id via its parent
// token subscription (charges table tracks by purchase id).
func (s *Service) processSubscriptionEvent(ctx context.Context, p *chip.Purchase) error {
	switch p.Status {
	case chip.StatusPaid:
		// Issue #154: a paid purchase may be a completed card-update
		// re-tokenize (the customer authorized a new card). If so, swap the
		// subscription's token. Otherwise treat it as a subscription charge.
		if err := s.CompleteCardUpdate(ctx, p); err == nil {
			return nil
		}
		if err := s.Subs.MarkChargePaid(ctx, p.ID); err != nil {
			return err
		}
		// round 13c (Wan): a payment settles the arrears, so a past_due
		// subscription returns to active immediately. Without this a
		// subscription that failed 3x stayed past_due until the customer
		// completed a card update — it could not recover on its own, and it
		// was one grace-window expiry away from being auto-cancelled even
		// though the customer had just paid. Marking it active also clears
		// card_update_pending (there is nothing left to update).*/
		if err := s.Subs.ReactivateOnPayment(ctx, p.ID); err != nil {
			return err
		}
		// Advance the next charge by the billing period and, for a fixed
		// billing count, mark the subscription completed once the Nth charge
		// has been reached (no further charges scheduled).
		sub, err := s.Subs.ByChargePurchase(ctx, p.ID)
		if err != nil {
			return err
		}
		// round 13c: the next-charge date follows the product's schedule mode.
		// A charge that succeeded late used to shift every later charge, because
		// this was always computed from time.Now().
		mode := s.scheduleModeFor(ctx, sub)
		next := nextChargeForPolicy(mode, time.Now(), sub.NextChargeAt, sub.Period, false)
		if err := s.Subs.ScheduleNextCharge(ctx, sub.ID, next); err != nil {
			return err
		}
		return s.completeIfLimitReached(ctx, sub)
	case chip.StatusError:
		return s.Subs.MarkChargeFailed(ctx, p.ID)
	}
	return ErrNotFound
}

// completeIfLimitReached marks a subscription completed once it has reached its
// fixed billing count (subscription_charges = N, NULL = infinite).
//
// ChargeCount counts PAID charge rows. The customer's first payment
// (activateSubscription) is itself recorded as a paid row stamped with the token
// purchase id, so `paid` already includes it — the total consumed is simply
// `paid`. It must NOT be `1 + paid`: that counted the first payment twice and
// left the subscription permanently short of its limit, so it never completed.
// Returns nil when the limit is not (yet) reached.
//
// Issue #623: the limit is resolved through the subscription's VARIATION when it
// has one (the same EffectiveCycle rule the checkout used), so a variation with
// its own fixed count is not billed on the product's.
func (s *Service) completeIfLimitReached(ctx context.Context, sub *dbstore.Subscription) error {
	cycle, ok, err := s.subscriptionCycleFor(ctx, sub)
	if err != nil {
		return err
	}
	if !ok || cycle.Charges == nil {
		return nil // no product resolved, or infinite
	}
	paid, err := s.Subs.ChargeCount(ctx, sub.ID)
	if err != nil {
		// Issue #535: a swallowed error here would read as "0 charges consumed",
		// so a fixed-billing subscription would keep charging past its Nth charge
		// instead of completing. Fail the cycle instead of guessing.
		return err
	}
	if paid >= int64(*cycle.Charges) {
		if err := s.Subs.SetStatus(ctx, sub.ID, dbstore.SubCompleted); err != nil {
			return err
		}
		slog.Info("sub_completed_charge_limit", "sub", sub.ID, "charges", paid)
	}
	return nil
}

// CaptureSale captures an authorized (skip_capture) purchase. amountSen nil
// means capture the full original amount.
func (s *Service) CaptureSale(ctx context.Context, sale *dbstore.Sale, amountSen *int64) error {
	if sale.ChipPurchaseID == "" || !sale.SkipCapture {
		return errors.New("bukan jualan authorize (skip_capture)")
	}
	secret, err := s.decryptSecret(ctx, sale.AccountID)
	if err != nil {
		return err
	}
	if _, err := s.Chip.CapturePayment(ctx, secret, sale.ChipPurchaseID, amountSen); err != nil {
		return err
	}
	return nil
}

// ReleaseSale voids an authorization so the card is never charged.
func (s *Service) ReleaseSale(ctx context.Context, sale *dbstore.Sale) error {
	if sale.ChipPurchaseID == "" || !sale.SkipCapture {
		return errors.New("bukan jualan authorize (skip_capture)")
	}
	secret, err := s.decryptSecret(ctx, sale.AccountID)
	if err != nil {
		return err
	}
	if _, err := s.Chip.ReleasePayment(ctx, secret, sale.ChipPurchaseID); err != nil {
		return err
	}
	return nil
}

// refundResult reports what a completed refund did (issue #518).
type RefundResult struct {
	// AmountSen is the amount actually refunded on this call.
	AmountSen int64
	// RefundedSen is the sale's running refunded total after this refund.
	RefundedSen int64
	// OutstandingSen is the balance still refundable (total - refunded).
	OutstandingSen int64
	// FullyRefunded is true once the whole total has been returned.
	FullyRefunded bool
	// Status is the sale's status after the refund: SaleRefunded once the full
	// total is returned, otherwise the sale keeps its paid/confirmed status.
	Status string
}

// ErrRefundNotRefundable is returned when a refund is attempted on a sale that
// is not in a refundable state (the pre-existing paid/confirmed guard).
var ErrRefundNotRefundable = errors.New("jualan bukan dalam status boleh-refund (paid/confirmed)")

// RefundSale issues a refund against a paid sale (issue #407) and records the
// amount on the sale (issue #518). amountSen nil = full refund of the
// OUTSTANDING balance; a non-nil amount (integer sen) issues a partial refund.
//
// The whole operation is atomic under the per-sale advisory lock
// (WithSaleLock): the balance check and the refunded-total increment happen in
// one serialized transaction, so two concurrent partial refunds cannot both
// pass a stale balance read and over-refund. Order of operations:
//
//  1. lock the sale and re-read its current status + refunded total;
//  2. enforce the refundable-state guard (paid/confirmed only — a partially
//     refunded sale keeps its status, so further partials are allowed but a
//     fully refunded one is rejected);
//  3. enforce the over-refund guard: amount > 0 and
//     refunded_sen + amount <= total_sen;
//  4. only then call CHIP (an over-refund never reaches the gateway);
//  5. record the amount, moving the status to SaleRefunded only when the full
//     total has been returned.
//
// The caller persists nothing — the refunded amount, the status transition and
// the audit are all handled here. Any error is returned unwrapped so the REST
// 400 / MCP error carries the user-facing reason, with
// dbstore.ErrRefundExceedsBalance / ErrRefundNoOutstanding /
// ErrRefundAmountInvalid identifying the guard that fired.
func (s *Service) RefundSale(ctx context.Context, sale *dbstore.Sale, amountSen *int64) (*RefundResult, error) {
	if sale.ChipPurchaseID == "" {
		return nil, errors.New("jualan tiada purchase CHIP")
	}
	if amountSen != nil && *amountSen <= 0 {
		return nil, dbstore.ErrRefundAmountInvalid
	}

	var result *RefundResult
	err := s.Sales.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		cur, err := s.Sales.SaleByIDTx(ctx, tx, sale.ID)
		if err != nil {
			return err
		}
		// The refundable-state guard. A partial refund deliberately leaves the
		// status paid/confirmed, so a partially refunded sale is still
		// refundable here — the balance guard below is what stops over-refunding.
		switch cur.Status {
		case dbstore.SalePaid, dbstore.SaleConfirmed:
			// refundable
		default:
			return ErrRefundNotRefundable
		}

		// Resolve the amount: nil = the whole outstanding balance. Guard it
		// BEFORE calling CHIP so an over-refund never reaches the gateway.
		amount := cur.OutstandingSen()
		if amountSen != nil {
			amount = *amountSen
		}
		if amount <= 0 {
			return dbstore.ErrRefundNoOutstanding
		}
		if amount > cur.OutstandingSen() {
			return dbstore.ErrRefundExceedsBalance
		}

		secret, err := s.decryptSecret(ctx, cur.AccountID)
		if err != nil {
			return err
		}
		refunded := amount
		if _, err := s.Chip.Refund(ctx, secret, cur.ChipPurchaseID, &refunded); err != nil {
			return err
		}

		updated, err := s.Sales.ApplyRefundTx(ctx, tx, cur, amount)
		if err != nil {
			// A failure here (after CHIP accepted the refund) rolls the record
			// back. That cannot be a concurrent-write race — every refund for
			// this sale is serialized by the advisory lock — so it is a real DB
			// error; the purchase.refunded webhook then flips the sale to
			// refunded and records the full amount, so the ledger self-heals.
			return err
		}
		result = &RefundResult{
			AmountSen:      amount,
			RefundedSen:    updated.RefundedSen,
			OutstandingSen: updated.OutstandingSen(),
			FullyRefunded:  updated.RefundStatus() == dbstore.RefundFull,
			Status:         updated.Status,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ChipOverride carries per-form credentials that, when set, replace the
// account-level ones for this purchase only.
type ChipOverride struct {
	BrandID         string
	SecretEnc       string
	EmailFallback   string
	MethodWhitelist []string
}

// CreateChipPurchaseForSaleWithOverride behaves like CreateChipPurchaseForSale
// but resolves credentials as: form override first, then account default.
func (s *Service) CreateChipPurchaseForSaleWithOverride(ctx context.Context, sale *dbstore.Sale, ov *ChipOverride) error {
	acc, err := s.Accounts.ByID(ctx, sale.AccountID)
	if err != nil {
		return fmt.Errorf("account: %w", err)
	}

	brand := acc.ChipBrandID
	secretEnc := acc.ChipSecretKeyEnc
	fallback := acc.ChipEmailFallback
	whitelist := acc.ChipMethodWhitelist
	fromForm := false
	if ov != nil && ov.BrandID != "" && ov.SecretEnc != "" {
		brand = ov.BrandID
		secretEnc = ov.SecretEnc
		fallback = ov.EmailFallback
		whitelist = ov.MethodWhitelist
		fromForm = true
	}
	if brand == "" || secretEnc == "" {
		return ErrNotConfigured
	}

	secret, err := cryptoutil.Decrypt(s.DecryptKey, secretEnc)
	if err != nil {
		return fmt.Errorf("decrypt key: %w", err)
	}

	clientEmail := sale.Client.Email
	if clientEmail == "" && sale.SaleType != dbstore.SaleTypeSubscription && fallback != "" {
		clientEmail = fallback
	}

	wl := whitelist
	if fromForm {
		// form whitelists resolve directly (no account-level merge)
		wl = s.ResolveWhitelist(ctx, string(secret), sale.Currency, sale.TotalSen, whitelist)
	} else {
		wl = s.ResolveWhitelist(ctx, string(secret), sale.Currency, sale.TotalSen, whitelist)
	}

	// Issue #83: thank-you redirects must land on the workspace subdomain (or
	// custom domain) the buyer came from, not the apex base. Fall back to the
	// apex base only when no workspace/custom domain is resolvable.
	tyBase := s.workspaceBaseURL(ctx, acc.ID)
	if tyBase == "" {
		tyBase = s.BaseURL
	}
	req := chip.PurchaseRequest{
		SkipCapture:            sale.SkipCapture,
		CreatorAgent:           "JomForm",
		BrandID:                brand,
		PaymentMethodWhitelist: wl,
		Client:                 saleClientDetails(sale, clientEmail),
		ForceRecurring:         sale.SaleType == dbstore.SaleTypeSubscription,
		// TOP-LEVEL — CHIP only honors these here (nested-in-purchase gets discarded):
		SuccessRedirect: fmt.Sprintf("%s/t/%s", tyBase, sale.RefCode),
		FailureRedirect: fmt.Sprintf("%s/t/%s?status=fail", tyBase, sale.RefCode),
		SuccessCallback: fmt.Sprintf("%s/v1/payments/chip/callback", s.BaseURL),
		Reference:       sale.RefCode,
		Purchase: chip.PurchaseDetails{
			Products: toCHIPProducts(sale.Items),
			Currency: sale.Currency,
		},
	}

	p, err := s.Chip.CreatePurchase(ctx, string(secret), req)
	if err != nil {
		return fmt.Errorf("chip create: %w", err)
	}
	if err := s.Sales.AttachChipPurchase(ctx, sale.ID, p.ID, p.CheckoutURL); err != nil {
		return fmt.Errorf("attach purchase: %w", err)
	}
	sale.ChipPurchaseID = p.ID
	sale.ChipCheckoutURL = p.CheckoutURL
	// keep the secret used on the sale for later capture/release of THIS purchase
	return nil
}

// decryptSecret fetches the account and decrypts its CHIP secret key.
// decryptSecret fetches the account and decrypts its CHIP secret key.
func (s *Service) decryptSecret(ctx context.Context, accountID string) (string, error) {
	acc, err := s.Accounts.ByID(ctx, accountID)
	if err != nil {
		return "", err
	}
	dec, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return "", err
	}
	return string(dec), nil
}

var (
	dnqrGroup   = []string{"duitnow_qr", "dnqr"}
	shopeeGroup = []string{"razer_shopeepay", "shopee_pay"}
	// cardGroup: the admin-facing "Card" aggregator expands to the network
	// identifiers CHIP's API expects; the 'card' key itself is never sent
	// (Woo parity: resolve_payment_method_groups strips it before the call).
	cardGroup = []string{"visa", "mastercard", "maestro"}
)

// ResolveWhitelist expands legacy alias pairs then intersects with the brand's
// truly-available methods (CHIP /payment_methods/). Mirrors the WooCommerce
// plugin resolver: dnqr preferred over duitnow_qr; shopee_pay over
// razer_shopeepay.
func (s *Service) ResolveWhitelist(ctx context.Context, secret string, currency string, amountSen int64, whitelist []string) []string {
	if len(whitelist) == 0 {
		return nil
	}
	// 'card' aggregator → network identifiers; never sent as 'card' itself.
	hasCard := false
	for _, m := range whitelist {
		if m == "card" {
			hasCard = true
		}
	}
	if hasCard {
		expanded := make([]string, 0, len(whitelist)+len(cardGroup))
		for _, m := range whitelist {
			if m != "card" {
				expanded = append(expanded, m)
			}
		}
		whitelist = append(expanded, cardGroup...)
	}
	groups := map[string][]string{}
	in := func(list, want []string) bool {
		for _, m := range list {
			for _, w := range want {
				if m == w {
					return true
				}
			}
		}
		return false
	}
	if in(whitelist, dnqrGroup) {
		groups["dnqr"] = dnqrGroup
	}
	if in(whitelist, shopeeGroup) {
		groups["shopee"] = shopeeGroup
	}
	if len(groups) == 0 {
		return whitelist
	}
	expanded := append([]string{}, whitelist...)
	for _, members := range groups {
		expanded = append(expanded, members...)
	}
	avail, err := s.Chip.PaymentMethods(ctx, secret, currency, "", amountSen)
	if err != nil || len(avail) == 0 {
		return expanded
	}
	resolved := append([]string{}, whitelist...)
	for key, members := range groups {
		var ok []string
		for _, m := range members {
			for _, a := range avail {
				if m == a {
					ok = append(ok, m)
				}
			}
		}
		preferred := "shopee_pay"
		dropped := "razer_shopeepay"
		if key == "dnqr" {
			preferred = "dnqr"
			dropped = "duitnow_qr"
		}
		final := []string{}
		hasPreferred := false
		for _, m := range ok {
			if m != dropped {
				final = append(final, m)
				if m == preferred {
					hasPreferred = true
				}
			}
		}
		_ = hasPreferred
		resolved = append(resolved, final...)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, m := range resolved {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// formRedirects returns per-form custom redirects when configured ("" = default).
func (s *Service) formRedirects(ctx context.Context, accountID, formID string) (successURL, failureURL string) {
	if s.Forms == nil || accountID == "" || formID == "" {
		return "", ""
	}
	f, err := s.Forms.GetByID(ctx, accountID, formID)
	if err != nil || f == nil {
		return "", ""
	}
	return f.RedirectSuccess, f.RedirectFailure
}
