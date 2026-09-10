package chip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePurchaseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/purchases/" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_key" {
			t.Errorf("auth header = %q", got)
		}
		var req PurchaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.BrandID != "brand-1" || len(req.Purchase.Products) != 1 {
			t.Errorf("bad request: %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "pur_abc", "status": "created",
			"checkout_url": "https://gate.example/checkout/pur_abc",
			"total":        2000, "currency": "MYR", "reference": "JF-99",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	p, err := c.CreatePurchase(context.Background(), "sk_test_key", PurchaseRequest{
		BrandID: "brand-1",
		Client:  &ClientDetails{Email: "buyer@test.my", FullName: "Ali"},
		Purchase: PurchaseDetails{
			Products:  []Product{{Name: "Kuih", Price: 1000, Quantity: "2"}},
			Currency:  "MYR",
			Reference: "JF-99",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID != "pur_abc" || p.CheckoutURL == "" || p.Total != 2000 {
		t.Errorf("unexpected purchase: %+v", p)
	}
}

func TestApiErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid brand", "code": "brand_not_found"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreatePurchase(context.Background(), "sk", PurchaseRequest{BrandID: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	ews, ok := err.(*ErrorWithStatus)
	if !ok {
		t.Fatalf("want ErrorWithStatus, got %T", err)
	}
	if ews.Status != http.StatusBadRequest {
		t.Errorf("status = %d", ews.Status)
	}
	if got := ews.Error(); got != "chip: Invalid brand (brand_not_found)" {
		t.Errorf("message = %q", got)
	}
}

func TestChargeFlow(t *testing.T) {
	mux := http.NewServeMux()
	var createdID string
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		createdID = "pur_new_1"
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": createdID, "status": "created"})
	})
	mux.HandleFunc("POST /purchases/pur_new_1/charge/", func(w http.ResponseWriter, r *http.Request) {
		var body ChargeRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.RecurringToken != "pur_token_orig" {
			t.Errorf("recurring_token = %q", body.RecurringToken)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pur_charge_child", "status": "pending_charge"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	p, err := c.Charge(context.Background(), "sk", PurchaseRequest{
		BrandID: "b",
		Purchase: PurchaseDetails{
			Products: []Product{{Name: "Sub", Price: 2900, Quantity: "1"}},
			Currency: "MYR",
		},
	}, "pur_token_orig")
	if err != nil {
		t.Fatalf("charge: %v", err)
	}
	if p.Status != StatusPendingCharge {
		t.Errorf("status = %s, want pending_charge", p.Status)
	}
}

// TestRefundFlow verifies the refund method hits POST /purchases/{id}/refund/
// with the amount in sen for a partial refund, and no body for a full refund.
func TestRefundFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/pur_paid/refund/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]int64
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["amount"] != 500 {
			t.Errorf("partial refund amount = %v, want 500 (sen)", body["amount"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pur_paid", "status": StatusRefunded})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	amount := int64(500)
	p, err := c.Refund(context.Background(), "sk", "pur_paid", &amount)
	if err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	if p.Status != StatusRefunded {
		t.Errorf("status = %s, want refunded", p.Status)
	}
}

// TestRefundFullNoBody verifies a full refund sends no amount body.
func TestRefundFullNoBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/pur_paid/refund/", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength != 0 {
			t.Errorf("full refund should send no body, got content-length %d", r.ContentLength)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pur_paid", "status": StatusRefunded})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	if _, err := c.Refund(context.Background(), "sk", "pur_paid", nil); err != nil {
		t.Fatalf("full refund: %v", err)
	}
}
