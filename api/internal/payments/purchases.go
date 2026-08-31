package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

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

	req := chip.PurchaseRequest{
		BrandID:        acc.ChipBrandID,
		Client:         &chip.ClientDetails{Email: sale.Client.Email, FullName: sale.Client.FullName, Phone: sale.Client.Phone},
		ForceRecurring: sale.SaleType == dbstore.SaleTypeSubscription, // card must be saved for future charges
		Purchase: chip.PurchaseDetails{
			Products:        toCHIPProducts(sale.Items),
			Currency:        acc.Currency,
			Reference:       sale.RefCode,
			SuccessRedirect: fmt.Sprintf("%s/t/%s", s.BaseURL, sale.RefCode),
			FailureRedirect: fmt.Sprintf("%s/t/%s?status=fail", s.BaseURL, sale.RefCode),
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
func (s *Service) ProcessCallback(ctx context.Context, p *chip.Purchase) error {
	if p.ID == "" {
		return errors.New("callback missing purchase id")
	}

	sale, err := s.Sales.ByChipPurchaseID(ctx, p.ID)
	if err != nil {
		// Not a direct sale — may be a subscription charge or token purchase.
		return s.processSubscriptionEvent(ctx, p)
	}

	switch {
	case p.Status == chip.StatusPaid:
		if err := s.Sales.SetStatus(ctx, sale.ID, dbstore.SalePaid); err != nil {
			return err
		}
		// Subscription first payment: token purchase completed → activate sub.
		if sale.SaleType == dbstore.SaleTypeSubscription {
			return s.activateSubscription(ctx, sale)
		}
	case p.Status == chip.StatusError:
		if err := s.Sales.SetStatus(ctx, sale.ID, dbstore.SaleFailed); err != nil {
			return err
		}
	case p.Status == chip.StatusPendingCharge:
		// transient; nothing to persist beyond event row (caller stores raw event)
	}
	return nil
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
		return s.Subs.MarkChargePaid(ctx, p.ID)
	case chip.StatusError:
		return s.Subs.MarkChargeFailed(ctx, p.ID)
	}
	return ErrNotFound
}
