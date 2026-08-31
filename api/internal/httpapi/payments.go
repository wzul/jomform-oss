// CHIP callback endpoint: verifies signature, dedups, processes events.
package httpapi

import (
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
	PublicKey *rsa.PublicKey // CHIP company-wide callback verification key
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

	// 1. Verify RSA signature BEFORE trusting the payload.
	if h.PublicKey != nil {
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
	} else {
		slog.Warn("chip_callback_no_public_key_configured")
	}

	// 2. Parse the Purchase payload.
	var p chip.Purchase
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}

	// 3. Dedup on (purchase id, event_type, status) — CHIP may redeliver.
	dedupKey := p.ID + ":" + p.EventType + ":" + p.Status
	inserted, err := h.Events.TryInsert(r.Context(), &dbstore.WebhookEvent{
		Source:    "chip",
		EventType: p.EventType,
		Payload:   raw,
		DedupKey:  dedupKey,
	})
	if err != nil {
		slog.Error("chip_callback_store_event", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !inserted {
		// Already processed (or in-flight) — 2xx stops CHIP retrying.
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}

	// 4. Process business logic (sale/subscription transitions).
	if err := h.Service.ProcessCallback(r.Context(), &p); err != nil {
		slog.Error("chip_callback_process", "err", err, "purchase", p.ID, "event", p.EventType)
		writeErr(w, http.StatusInternalServerError, "process gagal")
		return
	}

	// Mark event processed.
	_ = h.Events.MarkProcessed(r.Context(), dedupKey)
	slog.Info("chip_callback_processed", "purchase", p.ID, "event", p.EventType, "status", p.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "at": time.Now().UTC().Format(time.RFC3339)})
}
