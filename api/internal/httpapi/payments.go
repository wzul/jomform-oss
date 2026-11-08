// CHIP callback endpoint: verifies signature, dedups, processes events.
package httpapi

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// PaymentsHandlers exposes payment-related HTTP endpoints.
type PaymentsHandlers struct {
	Service *payments.Service
	Events  *dbstore.WebhookEventStore
	// StaticPublicKey is an OPTIONAL company-wide verification key.
	//
	// Issue #566: this used to be a single `PublicKey` field that the handler
	// CACHED after the first successful fetch. With more than one CHIP account
	// that is wrong: the first account's key was reused to verify every other
	// account's callbacks (and the nil-check then skipped resolving the right
	// one), so a second merchant's callbacks would fail signature verification.
	// The key is now resolved per callback, per account, and cached inside
	// payments.Service keyed by the account's secret.
	//
	// Leave this nil in production: each account has its own CHIP key.
	StaticPublicKey *rsa.PublicKey
	// ClaimTimeout (issue #517) bounds how long one callback attempt owns the
	// event claim before another attempt may retake it. Zero uses
	// dbstore.DefaultClaimTimeout.
	ClaimTimeout time.Duration
	// EventLookup resolves the ACCOUNT owning a purchase id, so the account's CHIP
	// secret can fetch the verification key.
	//
	// Issue #564: it must cover BOTH purchase kinds. A one-time/token purchase has
	// a sale row; a RECURRING CHARGE has only a subscription_charges row and never
	// a sale, so the sale lookup alone could never resolve it and every renewal
	// callback was rejected 503 — the first payment would work and every month
	// after it would silently fail.
	EventLookup interface {
		SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error)
		SubByChargePurchase(ctx context.Context, purchaseID string) (*dbstore.Subscription, error)
	} // CHIP company-wide callback verification key
}

// claimTimeout returns the configured event-claim timeout or the default.
func (h *PaymentsHandlers) claimTimeout() time.Duration {
	if h.ClaimTimeout > 0 {
		return h.ClaimTimeout
	}
	return dbstore.DefaultClaimTimeout
}

// RegisterRoutes wires payment endpoints.
func (h *PaymentsHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/payments/chip/callback", h.handleChipCallback)
}

// handleChipCallback processes CHIP's signed success_callback delivery.
func (h *PaymentsHandlers) handleChipCallback(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad body")
		return
	}

	// 1. Resolve the verification key for THIS callback's account, then verify the
	// RSA signature BEFORE trusting the payload.
	//
	// Issue #566: the key must be resolved per account on every callback. This
	// used to be a single cached field on the handler, which meant the first
	// account's key was reused for every other account's callbacks.
	key := h.StaticPublicKey
	if key == nil && h.Service != nil && h.EventLookup != nil {
		// account_id lives in the payload; parse first for key resolution
		var peek struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &peek) == nil {
			accountID := h.accountForPurchase(r, peek.ID)
			if accountID == "" {
				slog.Warn("chip_callback_account_unresolved", "purchase", peek.ID)
			} else if k, kerr := h.Service.CallbackPublicKey(r.Context(), accountID); kerr == nil {
				key = k
			} else {
				slog.Warn("chip_callback_key_unresolved", "account", accountID, "err", kerr)
			}
		}
	}
	// FAIL-CLOSED (issue #365): CHIP signature verification is MANDATORY and
	// unconditional. Without a resolvable public key there is no way to verify
	// the signature, so the callback MUST be rejected (503) rather than
	// processed. Never fall through to ProcessCallback with an unverified
	// signature.
	if key == nil {
		slog.Warn("chip_callback_no_public_key_configured")
		writeErr(w, http.StatusServiceUnavailable, "verification key unavailable")
		return
	}
	sig := r.Header.Get("X-Signature")
	if sig == "" {
		writeErr(w, http.StatusUnauthorized, "signature is missing")
		return
	}
	if err := chip.VerifySignature(key, raw, sig); err != nil {
		slog.Warn("chip_callback_bad_signature", "err", err)
		writeErr(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// 2. Parse the Purchase payload.
	var p chip.Purchase
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	// 3. Claim the event, then process it (issue #517).
	//
	// The claim is recorded in the SAME transaction as the sale transition and
	// commits only when processing succeeds, so the dedup key now means "this
	// event was successfully processed". A failed attempt leaves nothing
	// behind: the redelivery (or the reclaim sweep) retries it instead of
	// being swallowed as a duplicate. Concurrent identical deliveries are
	// serialized per sale (#202) and gated by the claim, so exactly one attempt
	// settles the sale; an already-processed event stays idempotent and never
	// re-fires a webhook, a receipt, or a subscription activation.
	dedupKey := p.ID + ":" + p.EventType + ":" + p.Status
	claimed, err := h.Service.ProcessCallbackClaimed(r.Context(), &p, dedupKey, raw, h.Events, h.claimTimeout())
	if err != nil {
		recordPaymentFailure("callback_process")
		// Release the claim immediately when this attempt failed, so the retry does
		// not wait out the claim timeout. Best effort: the event is re-claimable
		// either way (it is 'processing' and its heartbeat is now).
		_ = h.Events.MarkFailed(r.Context(), dedupKey)
		slog.Error("chip_callback_process", "err", err, "purchase", p.ID, "event", p.EventType,
			"dedup_key", dedupKey, "redelivery_will_retry", true)
		writeErr(w, http.StatusInternalServerError, "process failed")
		return
	}
	if !claimed {
		// Not processed by this delivery. Log WHICH case it is so the #517
		// failure mode is visible in production instead of silent: a settled
		// duplicate is fine, an unprocessed (stale/concurrent) row is the
		// signal that a delivery is being swallowed.
		if st, pat, serr := h.Events.EventState(r.Context(), dedupKey); serr == nil {
			if pat == nil || st != dbstore.WebhookEventProcessed {
				slog.Warn("chip_callback_duplicate_unprocessed",
					"purchase", p.ID, "event", p.EventType, "status", p.Status,
					"dedup_key", dedupKey, "event_state", st,
					"note", "delivery NOT processed; retried via redelivery or the reclaim sweep")
			} else {
				slog.Info("chip_callback_duplicate_processed",
					"purchase", p.ID, "event", p.EventType, "dedup_key", dedupKey)
			}
		} else {
			slog.Warn("chip_callback_duplicate_state_unknown",
				"purchase", p.ID, "event", p.EventType, "dedup_key", dedupKey, "err", serr)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}

	slog.Info("chip_callback_processed", "purchase", p.ID, "event", p.EventType, "status", p.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "at": time.Now().UTC().Format(time.RFC3339)})
}

// accountForPurchase returns the id of the account that owns a purchase id, for
// callback-key resolution (issue #564).
//
// Two kinds of purchases arrive here:
//
//   - a sale purchase (the initial one-time payment, or the subscription's first
//     token purchase) — it has a sale row;
//   - a RECURRING CHARGE — it exists only as a subscription_charges row, and
//     looking it up as a sale finds nothing. Missing this case made every renewal
//     callback unresolvable, so the key was never fetched and the callback was
//     rejected 503: the first payment succeeded and every renewal after it
//     silently failed.
//
// Returns "" when neither resolves, which leaves the key nil and the fail-closed
// guard to reject the callback.
func (h *PaymentsHandlers) accountForPurchase(r *http.Request, purchaseID string) string {
	if purchaseID == "" || h.EventLookup == nil {
		return ""
	}
	if sale, err := h.EventLookup.SaleByChipPurchaseID(r.Context(), purchaseID); err == nil && sale != nil {
		return sale.AccountID
	}
	if sub, err := h.EventLookup.SubByChargePurchase(r.Context(), purchaseID); err == nil && sub != nil {
		return sub.AccountID
	}
	return ""
}

// CallbackLookup resolves the account that owns a CHIP purchase id, so the
// callback can fetch that account's verification key (issue #564).
//
// It spans two stores because a purchase id can be either a sale (a one-time
// payment, or a subscription's first token purchase) or a recurring charge (a
// subscription_charges row, which has no sale).
type CallbackLookup struct {
	Sales interface {
		SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error)
	}
	Subscriptions interface {
		SubByChargePurchase(ctx context.Context, purchaseID string) (*dbstore.Subscription, error)
	}
}

// NewCallbackLookup builds the lookup used by the payment callback handler.
func NewCallbackLookup(sales interface {
	SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error)
}, subs interface {
	SubByChargePurchase(ctx context.Context, purchaseID string) (*dbstore.Subscription, error)
}) CallbackLookup {
	return CallbackLookup{Sales: sales, Subscriptions: subs}
}

// SaleByChipPurchaseID resolves a sale purchase.
func (c CallbackLookup) SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error) {
	return c.Sales.SaleByChipPurchaseID(ctx, id)
}

// SubByChargePurchase resolves a recurring charge purchase.
func (c CallbackLookup) SubByChargePurchase(ctx context.Context, purchaseID string) (*dbstore.Subscription, error) {
	return c.Subscriptions.SubByChargePurchase(ctx, purchaseID)
}
