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
	Service   *payments.Service
	Events    *dbstore.WebhookEventStore
	PublicKey *rsa.PublicKey
	// ClaimTimeout (issue #517) bounds how long one callback attempt owns the
	// event claim before another attempt may retake it. Zero uses
	// dbstore.DefaultClaimTimeout.
	ClaimTimeout time.Duration
	EventLookup  interface {
		SaleByChipPurchaseID(ctx context.Context, id string) (*dbstore.Sale, error)
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

	// 1. Verify RSA signature BEFORE trusting the payload (fetch+cache per
	// account secret when no static key configured).
	if h.PublicKey == nil && h.Service != nil && h.EventLookup != nil {
		// account_id lives in the payload; parse first for key resolution
		var peek struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &peek) == nil {
			if sale, err := h.EventLookup.SaleByChipPurchaseID(r.Context(), peek.ID); err == nil {
				if k, kerr := h.Service.CallbackPublicKey(r.Context(), sale.AccountID); kerr == nil {
					h.PublicKey = k
				}
			}
		}
	}
	// FAIL-CLOSED (issue #365): CHIP signature verification is MANDATORY and
	// unconditional. Without a resolvable public key there is no way to verify
	// the signature, so the callback MUST be rejected (503) rather than
	// processed. Never fall through to ProcessCallback with an unverified
	// signature.
	if h.PublicKey == nil {
		slog.Warn("chip_callback_no_public_key_configured")
		writeErr(w, http.StatusServiceUnavailable, "verification key unavailable")
		return
	}
	sig := r.Header.Get("X-Signature")
	if sig == "" {
		writeErr(w, http.StatusUnauthorized, "signature hilang")
		return
	}
	if err := chip.VerifySignature(h.PublicKey, raw, sig); err != nil {
		slog.Warn("chip_callback_bad_signature", "err", err)
		writeErr(w, http.StatusUnauthorized, "signature tidak sah")
		return
	}

	// 2. Parse the Purchase payload.
	var p chip.Purchase
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
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
		writeErr(w, http.StatusInternalServerError, "process gagal")
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
