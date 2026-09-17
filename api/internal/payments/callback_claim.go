package payments

// Issue #517: money-critical claim/complete processing for inbound CHIP
// callbacks.
//
// The dedup row used to be inserted BEFORE the business logic ran and stamped
// with processed_at only on success. A failure therefore left a row holding the
// dedup key with processed_at NULL: every redelivery was answered 200
// "duplicate" and never re-processed, so a customer could be charged with the
// sale never marked paid — permanently, with no path back.
//
// ProcessCallbackClaimed makes the dedup key mean "successfully processed":
// for a direct sale the claim row and the sale transition commit in the SAME
// transaction, so a failed attempt consumes nothing. Concurrent identical
// deliveries are serialized per sale by the existing advisory lock (#202) AND
// gated by the claim, so exactly one attempt settles the sale.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// ProcessCallbackClaimed processes one inbound callback exactly once, with the
// event claim committed atomically with the state change (issue #517).
//
// It returns claimed=true when this delivery ran the business logic (and
// committed it, together with the completed event row). claimed=false means
// another attempt owns the event — either it already succeeded (permanent
// duplicate: never re-fire a webhook, receipt, or subscription activation) or a
// live claim is in flight (a genuine concurrent duplicate, answered 2xx so CHIP
// stops retrying).
//
// A non-nil error means this attempt failed and left nothing behind: the claim
// transaction rolled back, so a redelivery or the reclaim sweep retries it.
//
// raw is the verified request body (stored verbatim for replay); when nil the
// decoded purchase is re-marshalled. A nil events store (bare-mode tests) runs
// the plain ProcessCallback path without dedup.
func (s *Service) ProcessCallbackClaimed(ctx context.Context, p *chip.Purchase, dedupKey string, raw []byte, events *dbstore.WebhookEventStore, claimTimeout time.Duration) (bool, error) {
	if p.ID == "" {
		return false, errors.New("callback missing purchase id")
	}
	if events == nil || dedupKey == "" {
		return true, s.ProcessCallback(ctx, p)
	}
	if claimTimeout <= 0 {
		claimTimeout = dbstore.DefaultClaimTimeout
	}
	payload := raw
	if len(payload) == 0 {
		payload = payloadOf(p)
	}

	// Direct sale? Look it up first (outside the claim tx) because the claim
	// must be taken under the same per-sale lock that serializes the
	// transition. A miss means a subscription charge / card-update purchase.
	sale, err := s.Sales.ByChipPurchaseID(ctx, p.ID)
	if err != nil {
		// Not a direct sale: claim the event on its own, then run the
		// subscription/card-update path, completing (or releasing) the claim
		// around it.
		claimed, cerr := events.ClaimStandalone(ctx, &dbstore.WebhookEvent{
			Source: "chip", EventType: p.EventType, Payload: payload, DedupKey: dedupKey,
		}, claimTimeout)
		if cerr != nil || !claimed {
			return claimed, cerr
		}
		if perr := s.processSubscriptionEvent(ctx, p); perr != nil {
			return true, perr // handler releases the claim (MarkFailed)
		}
		if merr := events.MarkProcessed(ctx, dedupKey); merr != nil {
			return true, merr
		}
		return true, nil
	}

	var claimed bool
	err = s.Sales.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		var cerr error
		claimed, cerr = events.ClaimTx(ctx, tx, &dbstore.WebhookEvent{
			Source:    "chip",
			EventType: p.EventType,
			Payload:   payload,
			DedupKey:  dedupKey,
		}, claimTimeout)
		if cerr != nil || !claimed {
			return cerr
		}
		// Business logic + the completed event row commit together. Any error
		// rolls both back, leaving the event re-claimable.
		if perr := s.ProcessCallbackTx(ctx, tx, p); perr != nil {
			return perr
		}
		if merr := completeEventTx(ctx, tx, dedupKey); merr != nil {
			return merr
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
}

// ReplayWebhookEvent re-processes an inbound event that was claimed but never
// successfully processed (issue #517) — the retry path for rows a crashed
// attempt left behind. It is the normal processing path, so an event that
// already succeeded is never re-fired. It is safe to call concurrently with a
// redelivery: the per-sale lock plus the claim gate makes the second attempt a
// no-op.
func (s *Service) ReplayWebhookEvent(ctx context.Context, events *dbstore.WebhookEventStore, e *dbstore.WebhookEvent) error {
	if len(e.Payload) == 0 {
		return errors.New("replay: empty payload")
	}
	var p chip.Purchase
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("replay: decode payload: %w", err)
	}
	_, err := s.ProcessCallbackClaimed(ctx, &p, e.DedupKey, e.Payload, events, 0)
	return err
}

// payloadOf marshals the decoded purchase back to the stored payload shape.
// Used when the caller did not pass the verified raw body.
func payloadOf(p *chip.Purchase) []byte {
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return b
}

// completeEventTx marks the event processed inside the caller's transaction, so
// the dedup key and the state change commit atomically.
func completeEventTx(ctx context.Context, tx pgx.Tx, dedupKey string) error {
	_, err := tx.Exec(ctx,
		`UPDATE webhook_events SET processed_at=now(), status=$2 WHERE dedup_key=$1`,
		dedupKey, dbstore.WebhookEventProcessed)
	return err
}

// RunInboundCallbackSweep re-processes inbound callback events that were
// claimed but never successfully processed (issue #517): a crash, deploy
// restart, or DB blip can leave such rows behind, and without this sweep they
// would only be retried if CHIP redelivered. It releases dead claims first
// (ReclaimStale), then replays pending/failed events.
//
// Runs until ctx is cancelled. interval defaults to 5 minutes.
func RunInboundCallbackSweep(ctx context.Context, s *Service, events *dbstore.WebhookEventStore, interval time.Duration) {
	if events == nil || s == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Run once immediately so events stranded by a previous process are
	// retried promptly after a restart.
	sweepInboundCallbacks(ctx, s, events)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepInboundCallbacks(ctx, s, events)
		}
	}
}

// sweepInboundCallbacks performs one reclaim + replay pass.
func sweepInboundCallbacks(ctx context.Context, s *Service, events *dbstore.WebhookEventStore) {
	// Issue #569: park events that have exhausted their attempts BEFORE
	// reclaiming, so a permanently unprocessable event stops being replayed
	// forever. Terminal — it leaves the backlog instead of pinning it non-zero.
	if n, err := events.BuryExhausted(ctx, "max replay attempts exceeded"); err != nil {
		slog.Warn("chip_callback_bury_exhausted", "err", err)
	} else if n > 0 {
		slog.Warn("chip_callback_buried", "count", n,
			"note", "events that failed too many times — parked as dead, no longer replayed")
	}
	if n, err := events.ReclaimStale(ctx, dbstore.DefaultClaimTimeout); err != nil {
		slog.Warn("chip_callback_reclaim_stale", "err", err)
	} else if n > 0 {
		slog.Warn("chip_callback_reclaim_stale", "released", n,
			"note", "events claimed by an attempt that never completed — replaying")
	}
	rows, err := events.Reclaimable(ctx, 100)
	if err != nil {
		slog.Warn("chip_callback_reclaim_list", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	// Surface the backlog: unprocessed inbound events are the #517 failure
	// mode, so they must be visible rather than silently dropped.
	if n, cerr := events.UnprocessedCount(ctx); cerr == nil {
		slog.Warn("chip_callback_replay_backlog", "replayable", len(rows), "unprocessed_total", n)
	}
	for _, e := range rows {
		if err := s.ReplayWebhookEvent(ctx, events, e); err != nil {
			slog.Warn("chip_callback_replay", "err", err, "dedup_key", e.DedupKey, "state", e.Status)
		} else {
			slog.Info("chip_callback_replayed", "dedup_key", e.DedupKey)
		}
	}
}
