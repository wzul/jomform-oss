package chip

import (
	"context"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// CreatePurchase issues POST /purchases/ and returns the created Purchase
// (including checkout_url).
func (c *Client) CreatePurchase(ctx context.Context, secretKey string, req PurchaseRequest) (*Purchase, error) {
	var out Purchase
	if err := c.do(ctx, secretKey, "POST", "/purchases/", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Charge creates a new Purchase for the subscription invoice, then charges the
// card tokenized by tokenPurchaseID. The new Purchase may come back as
// "pending_charge" — final result arrives via purchase.paid /
// purchase.payment_failure webhooks.
func (c *Client) Charge(ctx context.Context, secretKey string, newPurchase PurchaseRequest, tokenPurchaseID string) (*Purchase, error) {
	created, err := c.CreatePurchase(ctx, secretKey, newPurchase)
	if err != nil {
		return nil, err
	}
	body := ChargeRequest{RecurringToken: tokenPurchaseID}
	var out Purchase
	if err := c.do(ctx, secretKey, "POST", "/purchases/"+created.ID+"/charge/", body, &out); err != nil {
		return nil, fmt.Errorf("charge %s: %w", created.ID, err)
	}
	return &out, nil
}

// DeleteRecurringToken revokes a stored card token.
func (c *Client) DeleteRecurringToken(ctx context.Context, secretKey, purchaseID string) error {
	return c.do(ctx, secretKey, "POST", "/purchases/"+purchaseID+"/delete_recurring_token/", nil, nil)
}

// PublicKey fetches the company-wide callback verification key (PEM).
func (c *Client) PublicKey(ctx context.Context, secretKey string) (string, error) {
	var out struct {
		PublicKey string `json:"public_key"` // PEM-encoded RSA
	}
	if err := c.do(ctx, secretKey, "GET", "/public_key/", nil, &out); err != nil {
		return "", err
	}
	return out.PublicKey, nil
}

// ParsePublicKey parses a PEM-encoded RSA public key (e.g. from PublicKey or
// Webhook.public_key).
func ParsePublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("chip: no PEM block in public key")
	}
	return parsePKIX(block.Bytes)
}

// CapturePayment captures a previously authorized (skip_capture) purchase.
// Optional params may override the final amount ({"amount": sen}).
func (c *Client) CapturePayment(ctx context.Context, secretKey, purchaseID string, amountSen *int64) (*Purchase, error) {
	var body any
	if amountSen != nil {
		body = map[string]int64{"amount": *amountSen}
	}
	var p Purchase
	if err := c.do(ctx, secretKey, "POST", fmt.Sprintf("/purchases/%s/capture/", purchaseID), body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ReleasePayment voids an authorization so the card is never charged.
func (c *Client) ReleasePayment(ctx context.Context, secretKey, purchaseID string) (*Purchase, error) {
	var p Purchase
	if err := c.do(ctx, secretKey, "POST", fmt.Sprintf("/purchases/%s/release/", purchaseID), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Refund issues a refund against a paid purchase. amountSen nil = full refund;
// a non-nil amount (integer sen) issues a partial refund. Returns the updated
// Purchase (status "refunded" on success).
func (c *Client) Refund(ctx context.Context, secretKey, purchaseID string, amountSen *int64) (*Purchase, error) {
	var body any
	if amountSen != nil {
		body = map[string]int64{"amount": *amountSen}
	}
	var p Purchase
	if err := c.do(ctx, secretKey, "POST", fmt.Sprintf("/purchases/%s/refund/", purchaseID), body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// PaymentMethods lists available payment methods for a brand/currency/amount.
func (c *Client) PaymentMethods(ctx context.Context, secretKey, currency, language string, amount int64) ([]string, error) {
	req := map[string]string{"currency": currency, "amount": strconv.FormatInt(amount, 10)}
	if language != "" {
		req["language"] = language
	}
	q := make([]string, 0, 3)
	for k, v := range req {
		q = append(q, k+"="+url.QueryEscape(v))
	}
	sort.Strings(q)
	var out struct {
		Available []string `json:"available_payment_methods"`
	}
	if err := c.do(ctx, secretKey, "GET", "/payment_methods/?"+strings.Join(q, "&"), nil, &out); err != nil {
		return nil, err
	}
	return out.Available, nil
}

// ValidateCredentials performs a cheap authenticated call to prove the
// brand_id + secret_key pair works (Woo parity: validate upon save and
// show a ✓ status). Returns nil on success.
func (c *Client) ValidateCredentials(ctx context.Context, secret string) error {
	_, err := c.PublicKey(ctx, secret)
	return err
}
