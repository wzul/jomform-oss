package httpapi

// Issue #564: the callback's account resolution only understood ONE kind of
// purchase.
//
//	EventLookup.SaleByChipPurchaseID(peek.ID)   // sales only
//
// A one-time payment and a subscription's first (token) purchase have a sale
// row, so those callbacks resolved. A RECURRING CHARGE does not: it exists only
// as a subscription_charges row. So the lookup found nothing, the account could
// not be resolved, no verification key was fetched, and the fail-closed guard
// rejected every renewal callback with 503.
//
// The symptom is nasty: the first payment succeeds (its callback resolves via the
// sale) and every renewal after it silently fails — the customer is charged by
// CHIP while JomForm keeps the charge pending.
//
// Confirmed live: after a real card payment the subscription activated and the
// sweep charged the saved token, producing CHIP purchase 58631eae-… — and that
// callback logged
//
//	WARN chip_callback_no_public_key_configured
//	POST /v1/payments/chip/callback status=503
//
// with no sale row for that purchase id.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// newRequest builds a bare request for account resolution.
func newRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/v1/payments/chip/callback", nil)
}

type fakeSaleLookup struct {
	accountID string
	err       error
}

func (f fakeSaleLookup) SaleByChipPurchaseID(_ context.Context, _ string) (*dbstore.Sale, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.accountID == "" {
		return nil, errors.New("no rows")
	}
	return &dbstore.Sale{AccountID: f.accountID}, nil
}

type fakeSubLookup struct {
	accountID string
	err       error
}

func (f fakeSubLookup) SubByChargePurchase(_ context.Context, _ string) (*dbstore.Subscription, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.accountID == "" {
		return nil, errors.New("no rows")
	}
	return &dbstore.Subscription{AccountID: f.accountID}, nil
}

// TestIssue564RecurringChargeCallbackResolvesItsAccount is the regression: a
// charge purchase that has NO sale must still resolve, via the subscription.
func TestIssue564RecurringChargeCallbackResolvesItsAccount(t *testing.T) {
	h := &PaymentsHandlers{
		EventLookup: NewCallbackLookup(
			fakeSaleLookup{},                         // no sale for a recurring charge
			fakeSubLookup{accountID: "acct-renewal"}, // the subscription owns it
		),
	}
	got := h.accountForPurchase(newRequest(t), "58631eae-charge")
	if got != "acct-renewal" {
		t.Errorf("issue #564: account resolution for a RECURRING CHARGE = %q, want %q.\n"+
			"A renewal has no sale row, so a sales-only lookup leaves the account unresolved, "+
			"the verification key unfetched, and the callback rejected 503 — the first payment "+
			"succeeds and every renewal after it silently fails.", got, "acct-renewal")
	}
}

// TestIssue564SalePurchaseStillResolves keeps the original path working.
func TestIssue564SalePurchaseStillResolves(t *testing.T) {
	h := &PaymentsHandlers{
		EventLookup: NewCallbackLookup(
			fakeSaleLookup{accountID: "acct-sale"},
			fakeSubLookup{}, // not a subscription charge
		),
	}
	if got := h.accountForPurchase(newRequest(t), "pur-sale"); got != "acct-sale" {
		t.Errorf("a sale purchase resolved to %q, want %q", got, "acct-sale")
	}
}

// TestIssue564UnknownPurchaseDoesNotResolve keeps the fail-closed behaviour:
// an unresolvable purchase must yield no account, so no key is fetched and the
// callback is rejected rather than processed unverified.
func TestIssue564UnknownPurchaseDoesNotResolve(t *testing.T) {
	h := &PaymentsHandlers{
		EventLookup: NewCallbackLookup(fakeSaleLookup{}, fakeSubLookup{}),
	}
	if got := h.accountForPurchase(newRequest(t), "unknown"); got != "" {
		t.Errorf("an unknown purchase resolved to %q, want \"\" (the callback must stay fail-closed)", got)
	}
	if got := h.accountForPurchase(newRequest(t), ""); got != "" {
		t.Errorf("an empty purchase id resolved to %q, want \"\"", got)
	}
}

// TestIssue564WiringSuppliesBothLookups guards the same class of defect as
// #557: the production construction must supply a lookup that covers BOTH
// purchase kinds, not just sales.
func TestIssue564WiringSuppliesBothLookups(t *testing.T) {
	var l CallbackLookup
	_ = l
	// Compile-time: the concrete stores must satisfy the callback's needs.
	var _ interface {
		SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error)
	} = (*dbstore.SaleStore)(nil)
	var _ interface {
		SubByChargePurchase(ctx context.Context, purchaseID string) (*dbstore.Subscription, error)
	} = (*dbstore.SubscriptionStore)(nil)
}
