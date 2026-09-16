package payments

// Issue #562: a failed recurring charge recorded no reason.
//
//	_ = s.Subs.AddCharge(ctx, sub.ID, "", sub.AmountSen, dbstore.ChargeFailed, int(attempt))
//
// The gateway error was discarded, so the charge row said only "failed". A
// merchant could not tell an expired card from an invalid recurring token from a
// gateway outage, and neither could support — found while running a real
// subscription end-to-end, where CHIP returned
//
//	{"__all__":[{"message":"Invalid or inactive recurring token!","code":"invalid_recurring_token"}]}
//
// and JomForm recorded a bare "failed" with the reason nowhere.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// reasonChip always fails the charge with a specific gateway message.
type reasonChip struct{ msg string }

func newReasonChip(t *testing.T, msg string) *chip.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"pur-562","status":"pending_charge"}`))
	})
	mux.HandleFunc("POST /purchases/pur-562/charge/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"__all__":[{"message":%q,"code":"invalid_recurring_token"}]}`, msg)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL)
}

// TestIssue562FailedChargeRecordsTheGatewayReason is the regression.
func TestIssue562FailedChargeRecordsTheGatewayReason(t *testing.T) {
	const msg = "Invalid or inactive recurring token!"
	ctx := context.Background()
	pool := testdb.Open(t)

	// account + creds + subscription product + a due subscription
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email,password_hash) VALUES ('r562-'||md5(random()::text)||'@example.test','it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id,name,slug) SELECT $1,'r562-'||md5(random()::text),'r562-'||md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_562"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_562", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("creds: %v", err)
	}
	prod := &dbstore.Product{
		AccountID: accountID, Name: "R562", PriceSen: 10000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("product: %v", err)
	}
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "r562@example.test", ProductID: prod.ID,
		ChipTokenPurchaseID: fmt.Sprintf("tok-562-%d", time.Now().UnixNano()),
		Status:              dbstore.SubActive, Period: "month",
		AmountSen: 10000, NextChargeAt: time.Now().Add(-10 * time.Minute),
	}
	if err := dbstore.NewSubscriptionStore(pool).Create(ctx, sub); err != nil {
		t.Fatalf("sub: %v", err)
	}

	svc := &Service{
		Chip: newReasonChip(t, msg), Accounts: dbstore.NewAccountStore(pool),
		Sales: dbstore.NewSaleStore(pool), Subs: dbstore.NewSubscriptionStore(pool),
		Products: dbstore.NewProductStore(pool), DecryptKey: key, BaseURL: "https://app.example",
	}

	if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
		t.Fatalf("sweep: %v", err)
	}

	var status, reason string
	if err := pool.QueryRow(ctx,
		`SELECT status, COALESCE(failure_reason,'') FROM subscription_charges
		  WHERE subscription_id=$1 ORDER BY charged_at DESC LIMIT 1`, sub.ID).
		Scan(&status, &reason); err != nil {
		t.Fatalf("read charge: %v", err)
	}

	if status != dbstore.ChargeFailed {
		t.Fatalf("charge status = %q, want %q", status, dbstore.ChargeFailed)
	}
	if reason == "" {
		t.Errorf("issue #562: the failed charge recorded NO failure_reason.\n"+
			"The gateway said %q and JomForm kept only %q — a merchant cannot tell an "+
			"expired card from an invalid token from a gateway outage, and neither can support.",
			msg, status)
	}
	if reason != "" && !strings.Contains(reason, msg) {
		t.Errorf("failure_reason = %q, want it to contain the gateway message %q", reason, msg)
	}
}
