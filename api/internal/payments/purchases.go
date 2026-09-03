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
	// Issue #83: thank-you redirects must land on the space subdomain (or
	// custom domain) the buyer came from, not the apex base. Fall back to the
	// apex base only when no space/custom domain is resolvable.
	tyBase := s.spaceBaseURL(ctx, acc.ID)
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
			if s.OnPaid != nil {
				event := "sale.paid"
				if cur.SaleType == dbstore.SaleTypeSubscription {
					event = "subscription.charged"
				}
				go s.OnPaid(ctx, cur.AccountID, event, map[string]any{
					"ref_code": cur.RefCode, "amount_sen": cur.TotalSen,
					"buyer_email": cur.Client.Email, "purchase_id": p.ID,
				})
			}
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
			if err := s.Sales.SetStatusTx(ctx, tx, cur.ID, dbstore.SaleFailed); err != nil {
				return err
			}
		case chip.StatusPendingCharge:
			// transient; nothing to persist beyond event row (caller stores raw event)
		}
		return nil
	})
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
	// canonical space-scoped URL ({space}.jomform.com/{slug} or a
	// verified custom domain), not the apex thank-you page. Fall back
	// to the apex thank-you page only when no space/custom domain is
	// resolvable.
	formURL := s.formPublicURL(ctx, sale.AccountID, sale.FormID)
	if formURL == "" {
		formURL = s.BaseURL + "/t/" + sale.RefCode
	}
	// Issue #107: resolve the space id so the receipt renders from the
	// space's per-space email settings (defaults when none).
	spaceID := ""
	if s.Spaces != nil {
		if sid, serr := s.Spaces.SpaceIDByAccount(ctx, sale.AccountID); serr == nil {
			spaceID = sid
		}
	}
	go s.SendSaleReceipt(ctx, SaleRef{
		SaleID:       sale.ID,
		RefCode:      sale.RefCode,
		BuyerEmail:   sale.Client.Email,
		BuyerName:    sale.Client.FullName,
		AmountSen:    sale.TotalSen,
		ItemsSummary: summary,
		FormURL:      formURL,
		SpaceID:      spaceID,
		TIN:          tin,
		MSIC:         msic,
	})
}

// activateSubscription records the token purchase and schedules the next charge.
func (s *Service) activateSubscription(ctx context.Context, sale *dbstore.Sale) error {
	sub, err := s.Subs.ByTokenPurchase(ctx, sale.ChipPurchaseID)
	if err == nil {
		// Already activated (idempotent redelivery).
		_ = sub
		return nil
	}

	// Product determines period & amount — fetch from items metadata stored in sale.
	period := "month" // default; product lookup lands with products store wiring
	next := nextChargeFrom(time.Now(), period)

	newSub := &dbstore.Subscription{
		AccountID:           sale.AccountID,
		ClientEmail:         sale.Client.Email,
		ProductID:           firstProductID(sale),
		SaleID:              sale.ID,
		ChipTokenPurchaseID: sale.ChipPurchaseID,
		Status:              dbstore.SubActive,
		Period:              period,
		AmountSen:           sale.TotalSen,
		NextChargeAt:        next,
	}
	if err := s.Subs.Create(ctx, newSub); err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return s.Subs.AddCharge(ctx, newSub.ID, "", sale.TotalSen, dbstore.ChargePending, 0)
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
		// Advance the next charge by the billing period and, for a fixed
		// billing count, mark the subscription completed once the Nth charge
		// has been reached (no further charges scheduled).
		sub, err := s.Subs.ByChargePurchase(ctx, p.ID)
		if err != nil {
			return err
		}
		if err := s.Subs.ScheduleNextCharge(ctx, sub.ID, nextChargeFrom(time.Now(), sub.Period)); err != nil {
			return err
		}
		return s.completeIfLimitReached(ctx, sub)
	case chip.StatusError:
		return s.Subs.MarkChargeFailed(ctx, p.ID)
	}
	return ErrNotFound
}

// completeIfLimitReached marks a subscription completed once it has reached its
// fixed billing count (subscription_charges = N, NULL = infinite). The initial
// token purchase is always charge #1, so the total consumed is 1 + paid
// recurring charges. Returns nil when the limit is not (yet) reached.
func (s *Service) completeIfLimitReached(ctx context.Context, sub *dbstore.Subscription) error {
	if s.Products == nil {
		return nil
	}
	prod, err := s.Products.Get(ctx, sub.AccountID, sub.ProductID)
	if err != nil {
		return err
	}
	if prod.SubscriptionCharges == nil {
		return nil // infinite
	}
	total := 1 + s.Subs.ChargeCount(ctx, sub.ID)
	if total >= int64(*prod.SubscriptionCharges) {
		if err := s.Subs.SetStatus(ctx, sub.ID, dbstore.SubCompleted); err != nil {
			return err
		}
		slog.Info("sub_completed_charge_limit", "sub", sub.ID, "charges", total)
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

	// Issue #83: thank-you redirects must land on the space subdomain (or
	// custom domain) the buyer came from, not the apex base. Fall back to the
	// apex base only when no space/custom domain is resolvable.
	tyBase := s.spaceBaseURL(ctx, acc.ID)
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
