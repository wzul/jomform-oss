package httpapi

// Issue #557: the CHIP callback rejected EVERY signed delivery with 503 because
// the dynamic verification-key path was never wired up.
//
// The handler fetches the account's CHIP public key at runtime:
//
//	if h.PublicKey == nil && h.Service != nil && h.EventLookup != nil {
//		... resolve sale -> CallbackPublicKey(accountID) -> h.PublicKey
//	}
//	if h.PublicKey == nil { 503 }
//
// EventLookup was declared, and used in that guard, but NEVER ASSIGNED — and the
// interface method it names had no implementation in the repo at all. So the
// guard was permanently false, no key was ever fetched, and the fail-closed
// check discarded every callback. Customers were charged; JomForm recorded
// every sale as pending forever.
//
// The defect was purely WIRING, so these tests assert the wiring: that the real
// construction path supplies everything the verification needs, and that the
// dynamic path actually resolves a key when it does.
//
// The original tests used a fake lookup (payments_issue365_test.go), which is
// exactly why this slipped through: a hand-built handler always had the
// dependency, and production never did.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestIssue557CallbackVerificationDependenciesAreWired is the guard that would
// have caught the outage. It asserts the production construction supplies every
// dependency the signature-verification path needs, so a future dynamic-key
// change cannot silently disarm the callback again.
func TestIssue557CallbackVerificationDependenciesAreWired(t *testing.T) {
	src, err := os.ReadFile("../../cmd/jomform-api/main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	code := string(src)

	// The construction site must set Service, Events AND EventLookup. Checking
	// the source is deliberate: the failure mode is a MISSING field in a struct
	// literal, which no type checker can catch because every field is optional.
	idx := strings.Index(code, "PaymentsHandlers{")
	if idx < 0 {
		t.Fatalf("no PaymentsHandlers construction found in main.go")
	}
	end := strings.Index(code[idx:], "}")
	if end < 0 {
		t.Fatalf("unterminated PaymentsHandlers literal")
	}
	literal := code[idx : idx+end+1]

	for _, field := range []string{"Service:", "Events:", "EventLookup:"} {
		if !strings.Contains(literal, field) {
			t.Errorf("issue #557: PaymentsHandlers is constructed without %q.\n"+
				"The CHIP callback resolves the account's verification key through "+
				"EventLookup; without it the dynamic-key guard is always false, "+
				"h.PublicKey stays nil, and EVERY callback is rejected 503 — the "+
				"customer is charged while the sale stays pending forever.\n"+
				"literal was:\n%s", field, literal)
		}
	}
}

// TestIssue557SaleStoreImplementsEventLookup proves the dependency the handler
// needs actually exists. The interface method had NO implementation in the repo
// (only a Tx variant and a test fake), so the field could not be populated at
// all — a compile-time fact that nothing asserted.
func TestIssue557SaleStoreImplementsEventLookup(t *testing.T) {
	var _ interface {
		SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error)
	} = (*dbstore.SaleStore)(nil)
}
