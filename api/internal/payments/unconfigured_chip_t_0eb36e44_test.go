package payments

// Card t_0eb36e44: a capture / release / refund on an account whose CHIP keys
// are NOT configured must answer payments.ErrNotConfigured ("CHIP is not
// configured for this account"), not cryptoutil's raw "ciphertext too short".
//
// decryptSecret had no empty-value guard, unlike its siblings (accountSecrets,
// CreateChipPurchaseForSale), so it called Decrypt("") on a fresh signup's
// empty chip_secret_key_enc and returned the crypto error. Every sale-lifecycle
// operation goes through that one function, so all three leaked it — and the
// REST door writes err.Error() as the 400 body, so the merchant was told
// "ciphertext too short" with nothing they could act on.
//
// The second half pins the decision the card asks for on the OTHER case: a key
// that IS stored but will not decrypt (a rotated ENCRYPTION_KEY, a corrupted
// row). That is a server-side fault, so it answers ErrChipKeyUnreadable — the
// sentinel the REST door masks to a logged 500 — and never ErrNotConfigured,
// which would send the merchant to re-enter keys that are already there.
//
// The stub CHIP server's hit counter is part of the assertion: an unconfigured
// account must never reach the gateway at all.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// without it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// t0eb36Fixture is a Service over the scratch schema plus one account and a
// stub CHIP API that counts every lifecycle call it receives.
type t0eb36Fixture struct {
	svc       *Service
	accountID string
	chipHits  *int64
}

// seeding modes for the account's CHIP columns.
const (
	t0eb36NoKeys      = ""        // both columns empty: a fresh signup
	t0eb36CorruptSeal = "corrupt" // brand set, secret present but sealed under ANOTHER key
)

func t0eb36Seed(t *testing.T, mode string) t0eb36Fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-t0eb36-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-t0eb36-acc-' || md5(random()::text), 'it-t0eb36-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if mode == t0eb36CorruptSeal {
		// A real sealed value, but under a DIFFERENT key — what a rotated
		// ENCRYPTION_KEY leaves behind: the row is well-formed, it just will
		// not open. This is the case that must NOT be reported as
		// "not configured".
		other := make([]byte, 32)
		for i := range other {
			other[i] = byte(255 - i)
		}
		sealed, err := cryptoutil.Encrypt(other, []byte("sk_live_sealed_under_old_key"))
		if err != nil {
			t.Fatalf("seal under old key: %v", err)
		}
		if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_t0eb36", sealed, "test", "", nil, true); err != nil {
			t.Fatalf("set chip creds: %v", err)
		}
	}

	var hits int64
	mux := http.NewServeMux()
	record := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusRefunded})
	}
	mux.HandleFunc("POST /purchases/{id}/capture/", record)
	mux.HandleFunc("POST /purchases/{id}/release/", record)
	mux.HandleFunc("POST /purchases/{id}/refund/", record)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return t0eb36Fixture{
		svc: &Service{
			Chip:       chip.New(srv.URL),
			Accounts:   dbstore.NewAccountStore(pool),
			Sales:      dbstore.NewSaleStore(pool),
			DecryptKey: key,
			BaseURL:    "https://app.example",
		},
		accountID: accountID,
		chipHits:  &hits,
	}
}

// t0eb36Sale seeds a sale on the fixture's account. skipCapture marks it an
// authorization awaiting capture (what CaptureSale/ReleaseSale require);
// otherwise it is a plain PAID sale (what RefundSale requires).
func (f t0eb36Fixture) sale(t *testing.T, skipCapture bool) *dbstore.Sale {
	t.Helper()
	ctx := context.Background()
	status := dbstore.SalePaid
	sale := &dbstore.Sale{
		AccountID:   f.accountID,
		RefCode:     fmt.Sprintf("IT-T0EB36-%s-%d", f.accountID[:8], time.Now().UnixNano()),
		Client:      dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: status,
		SkipCapture: skipCapture,
	}
	if err := f.svc.Sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	purchaseID := "pur_t0eb36_" + f.accountID[:8]
	if err := f.svc.Sales.AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/co/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}
	sale.ChipPurchaseID = purchaseID
	return sale
}

// TestSaleLifecycleAnswersErrNotConfiguredWithoutChipKeys is the defect itself:
// capture, release and refund on an account with no CHIP keys.
func TestSaleLifecycleAnswersErrNotConfiguredWithoutChipKeys(t *testing.T) {
	ctx := context.Background()
	f := t0eb36Seed(t, t0eb36NoKeys)

	ops := []struct {
		name string
		run  func(*dbstore.Sale) error
	}{
		{"capture", func(s *dbstore.Sale) error { return f.svc.CaptureSale(ctx, s, nil) }},
		{"release", func(s *dbstore.Sale) error { return f.svc.ReleaseSale(ctx, s) }},
		{"refund", func(s *dbstore.Sale) error { _, err := f.svc.RefundSale(ctx, s, nil); return err }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			// capture/release need an authorization; refund needs a paid sale.
			sale := f.sale(t, op.name != "refund")
			err := op.run(sale)
			if err == nil {
				t.Fatalf("%s on an account with no CHIP keys returned nil; want ErrNotConfigured", op.name)
			}
			if !errors.Is(err, ErrNotConfigured) {
				t.Errorf("%s err = %v; want payments.ErrNotConfigured", op.name, err)
			}
			// The defect's wire symptom: a raw cryptoutil message reaching the
			// caller. It must not appear in the error text, whoever prints it.
			if strings.Contains(err.Error(), "ciphertext") {
				t.Errorf("%s err = %q leaks the crypto detail", op.name, err.Error())
			}
			if got := atomic.LoadInt64(f.chipHits); got != 0 {
				t.Errorf("%s called the CHIP API %d time(s); an unconfigured account must never reach the gateway", op.name, got)
			}
		})
	}
}

// TestSaleLifecycleMasksUnreadableChipKey pins the other half of the decision: a
// stored key that will not decrypt is NOT "not configured". It must answer
// ErrChipKeyUnreadable (which the REST door masks to a logged 500) and must not
// put the underlying crypto text on the wire.
func TestSaleLifecycleMasksUnreadableChipKey(t *testing.T) {
	ctx := context.Background()
	f := t0eb36Seed(t, t0eb36CorruptSeal)

	ops := []struct {
		name string
		run  func(*dbstore.Sale) error
	}{
		{"capture", func(s *dbstore.Sale) error { return f.svc.CaptureSale(ctx, s, nil) }},
		{"release", func(s *dbstore.Sale) error { return f.svc.ReleaseSale(ctx, s) }},
		{"refund", func(s *dbstore.Sale) error { _, err := f.svc.RefundSale(ctx, s, nil); return err }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			sale := f.sale(t, op.name != "refund")
			err := op.run(sale)
			if err == nil {
				t.Fatalf("%s with an undecryptable key returned nil; want ErrChipKeyUnreadable", op.name)
			}
			if !errors.Is(err, ErrChipKeyUnreadable) {
				t.Errorf("%s err = %v; want payments.ErrChipKeyUnreadable", op.name, err)
			}
			if errors.Is(err, ErrNotConfigured) {
				t.Errorf("%s reported an unreadable key as ErrNotConfigured; the merchant would be sent to re-enter keys that are already stored", op.name)
			}
			for _, leak := range []string{"ciphertext", "message authentication failed"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("%s err = %q leaks %q", op.name, err.Error(), leak)
				}
			}
			if got := atomic.LoadInt64(f.chipHits); got != 0 {
				t.Errorf("%s called the CHIP API %d time(s) without a usable key", op.name, got)
			}
		})
	}
}
