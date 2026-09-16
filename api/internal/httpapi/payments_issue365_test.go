package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// failingLookup never resolves a sale, so no account public key can be
// derived — simulating an unresolvable CHIP verification key.
type failingLookup struct{}

func (failingLookup) SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error) {
	return nil, errors.New("sale not found")
}

// SubByChargePurchase: issue #564 extended the callback's account resolution to
// cover recurring charges too. This failure-injection lookup fails BOTH, which is
// what the #365 test needs (no account resolvable by any route).
func (failingLookup) SubByChargePurchase(ctx context.Context, purchaseID string) (*dbstore.Subscription, error) {
	return nil, errors.New("subscription charge not found")
}

// Issue #365 regression: a forged CHIP callback (e.g. purchase.paid) where the
// public key cannot be resolved MUST be rejected (503) and MUST NOT proceed to
// ProcessCallback. Previously this fell through and marked the sale paid
// without any signature check.
func TestChipCallbackFailsClosedWhenKeyUnresolvable(t *testing.T) {
	h := &PaymentsHandlers{
		// No static PublicKey; EventLookup fails to resolve the sale → key nil.
		EventLookup: failingLookup{},
	}

	body := bytes.NewBufferString(`{"id":"purchase-1","event_type":"purchase.paid","status":"paid","total":100}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/chip/callback", body)
	rr := httptest.NewRecorder()

	// handler returns 503 before touching Events/Service (both nil here), so
	// reaching the end of this call without a panic proves we exit at the
	// signature-resolution guard, i.e. ProcessCallback is never invoked.
	h.handleChipCallback(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", rr.Code)
	}
	// A nil PublicKey is the "unresolvable" condition — assert the guard state.
	if h.PublicKey != nil {
		t.Fatal("expected PublicKey to remain unresolvable (nil)")
	}
}
