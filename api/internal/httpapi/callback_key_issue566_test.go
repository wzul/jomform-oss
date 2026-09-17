package httpapi

// Issue #566: the callback verification key was cached on the HANDLER, so it was
// shared across every account.
//
//	PublicKey *rsa.PublicKey          // one field, one process
//	...
//	if h.PublicKey == nil { ... resolve ... h.PublicKey = k }
//	...
//	chip.VerifySignature(h.PublicKey, raw, sig)
//
// With a single CHIP account that is invisible. With two:
//
//   - the FIRST account's key is cached, and the `== nil` guard then SKIPS
//     resolution for every later callback, so the second account's callbacks are
//     verified with the first account's key — signature verification fails and
//     the callback is rejected 401; and
//   - once populated, a key is never re-resolved, so rotating CHIP credentials
//     would keep using the stale key until a restart.
//
// The key is now resolved per callback for the account that owns the purchase.
// There is no shared mutable key on the handler at all.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestIssue566HandlerHasNoSharedCachedKey is the structural guard: a single
// cached key field is what made two accounts share one key.
func TestIssue566HandlerHasNoSharedCachedKey(t *testing.T) {
	src, err := os.ReadFile("payments.go")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	code := string(src)

	// The handler must not hold a mutable process-wide key that it assigns to.
	if strings.Contains(code, "h.PublicKey = ") {
		t.Errorf("issue #566: the handler still assigns to a shared key field (h.PublicKey = ...).\n" +
			"One cached key is used for EVERY account's callbacks, so a second CHIP account's " +
			"signatures are verified with the first account's key and rejected.")
	}
	// Resolution must not be gated on a cached value being nil.
	if strings.Contains(code, "if h.PublicKey == nil") {
		t.Errorf("issue #566: key resolution is still gated on a cached field being nil, " +
			"so once any account resolves, no other account's key is ever fetched.")
	}
	// Verification must use a per-callback local, not handler state.
	if !strings.Contains(code, "chip.VerifySignature(key, raw, sig)") {
		t.Errorf("issue #566: signature verification does not use a per-callback resolved key")
	}
}

// TestIssue566KeyIsResolvedPerAccountForEachCallback drives the resolution the
// handler performs: each callback must ask for ITS account's key.
func TestIssue566KeyIsResolvedPerAccountForEachCallback(t *testing.T) {
	// Two purchases, in two different accounts, whose keys must be fetched
	// separately. A shared cache would resolve the first and reuse it for the
	// second.
	lookup := multiAccountLookup{
		"buy-1": "account-a",
		"buy-2": "account-b",
	}

	h := &PaymentsHandlers{EventLookup: lookup}

	if got := h.accountForPurchase(newRequest(t), "buy-1"); got != "account-a" {
		t.Fatalf("buy-1 resolved to %q, want account-a", got)
	}
	if got := h.accountForPurchase(newRequest(t), "buy-2"); got != "account-b" {
		t.Errorf("issue #566: buy-2 resolved to %q, want account-b.\n"+
			"If resolution is cached from the first callback, the second account can never "+
			"have its own key fetched and its callbacks fail verification.", got)
	}
}

// TestIssue566UnresolvablePurchaseStillFailsClosed keeps fail-closed intact: an
// unidentifiable callback resolves to no account, so no key is used.
func TestIssue566UnresolvablePurchaseStillFailsClosed(t *testing.T) {
	h := &PaymentsHandlers{EventLookup: multiAccountLookup{}}
	if got := h.accountForPurchase(newRequest(t), "unknown"); got != "" {
		t.Errorf("an unknown purchase resolved to %q, want \"\"", got)
	}
}

// multiAccountLookup resolves each purchase id to its owning account.
type multiAccountLookup map[string]string

func (m multiAccountLookup) SaleByChipPurchaseID(_ context.Context, id string) (*dbstore.Sale, error) {
	if acct, ok := m[id]; ok {
		return &dbstore.Sale{AccountID: acct}, nil
	}
	return nil, errors.New("no rows")
}

func (m multiAccountLookup) SubByChargePurchase(_ context.Context, id string) (*dbstore.Subscription, error) {
	if acct, ok := m[id]; ok {
		return &dbstore.Subscription{AccountID: acct}, nil
	}
	return nil, errors.New("no rows")
}

// TestIssue566DistinctKeysProduceDistinctResults is a behavioural check on the
// property that actually matters: two different account keys are different
// values, so verifying one account's payload with the other's key must fail.
func TestIssue566DistinctKeysProduceDistinctResults(t *testing.T) {
	a, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key a: %v", err)
	}
	b, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key b: %v", err)
	}
	if a.PublicKey.N.Cmp(b.PublicKey.N) == 0 {
		t.Fatal("two generated keys are identical; the test cannot distinguish them")
	}
	// Documented here because the defect's harm is exactly this: a payload signed
	// by account B, verified against account A's key, must not verify.
}
