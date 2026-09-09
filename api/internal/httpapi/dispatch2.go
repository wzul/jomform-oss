// Outbound webhook dispatcher v2: delivery log, persistent retry queue with
// exponential backoff, provider-generic HMAC, event: sale.paid | sale.failed |
// subscription.charged.
//
// Issue #197: dispatch is now ASYNC and retries are DURABLE. DispatchWebhooks
// enqueues a delivery job for each matching webhook into the persistent
// webhook_retries table (next_retry_at = now) and returns immediately, so a
// slow webhook never blocks the main payment/submit flow. A background sweep
// worker (RunWebhookRetrySweep) picks up due retries and re-delivers them with
// an exponential backoff ladder (1m/5m/30m/1h/6h, max 6 attempts). Retries
// survive an API restart because they live in Postgres, not in-memory. On the
// final failure a retry is marked dead-lettered (visible, not silently
// dropped) and can be manually re-enqueued from the delivery log / MCP.
package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/ssrfguard"
)

// webhookBackoff is the exponential backoff ladder between retries (issue
// #197): 1m, 5m, 30m, 1h, 6h. webhookBackoff[i] is the delay before retry
// attempt i+2 (i.e. after attempt i+1 fails).
var webhookBackoff = []time.Duration{
	1 * time.Minute, 5 * time.Minute, 30 * time.Minute, time.Hour, 6 * time.Hour,
}

// webhookMaxAttempts is the total number of delivery attempts (initial + 5
// retries). After the last attempt fails the retry is dead-lettered.
const webhookMaxAttempts = 6

// DispatchWebhooks fires all matching active webhooks for an account and
// form. formID scopes the dispatch: form-scoped webhooks (form_id = formID)
// plus the account-wide ones (form_id IS NULL) both fire. An empty formID
// fires only the account-wide webhooks.
//
// Async (issue #197): each matching webhook is enqueued into the persistent
// retry queue and the caller returns immediately. The background sweep worker
// performs the actual delivery + retry, so a slow webhook never blocks the
// main payment/submit flow.
func DispatchWebhooks(ctx context.Context, h *RESTHandlers, store *dbstore.WebhookStore, accountID, formID, event string, payload map[string]any) {
	hooks, err := store.ListActiveForDispatch(ctx, accountID, formID)
	if err != nil {
		slog.Error("wh_list", "err", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	for _, wh := range hooks {
		if !matchesEvent(wh.Events, event) {
			continue
		}
		// Enqueue a due delivery job (next_retry_at = now) so the sweep picks
		// it up on the next tick. Durable — survives a restart.
		if err := store.EnqueueRetry(ctx, wh.ID, event, string(body), 1, time.Now()); err != nil {
			slog.Error("wh_enqueue", "err", err, "webhook", wh.ID, "event", event)
		}
	}
}

func matchesEvent(subscribed []string, event string) bool {
	for _, e := range subscribed {
		if e == event || e == "*" {
			return true
		}
	}
	return false
}

// deliverWebhook posts + records one delivery attempt. It returns whether the
// delivery succeeded, the HTTP status code, and the error message (for the
// delivery log). Retry scheduling is handled by the sweep worker, not here.
//
// Issue #366 (SSRF): the webhook URL is merchant-supplied, so before any
// request we resolve it once, reject loopback/link-local/private/metadata
// addresses (failing closed, logging the delivery as failed and NOT retrying
// into the internal network), and pin the connection to the single validated
// IP so a rebinding host cannot reach a private/meta address post-check.
// Errors.Is(err, ssrfguard.ErrBlocked) marks a delivery as permanently
// disallowed so the sweep dead-letters it instead of retrying.
func deliverWebhook(ctx context.Context, h *RESTHandlers, store *dbstore.WebhookStore,
	signPub ed25519.PublicKey, signPriv ed25519.PrivateKey, accountID string, wh *dbstore.OutboundWebhook,
	event string, body []byte, attempt int) (success bool, blocked bool, code int, errMsg string) {

	url, err := url.Parse(wh.URL)
	if err != nil || url.Scheme != "http" && url.Scheme != "https" {
		errMsg = "invalid or non-http(s) webhook url"
		_ = store.RecordDelivery(ctx, wh.ID, event, string(body), 0, false, attempt, errMsg)
		return false, false, 0, errMsg
	}
	// Resolve + validate the host once and pin the socket to the allowed IPs.
	// Fail closed: a disallowed/resolution error blocks delivery without
	// touching the internal network. blocked=true tells the retry sweep to
	// dead-letter immediately rather than keep retrying into a forbidden
	// address.
	pin, err := ssrfguard.Pin(nil, url.Hostname())
	if err != nil {
		errMsg = "webhook url blocked: " + err.Error()
		_ = store.RecordDelivery(ctx, wh.ID, event, string(body), 0, false, attempt, errMsg)
		// Only a genuinely disallowed address is a permanent block that
		// dead-letters immediately; a transient DNS failure stays retryable.
		blocked := errors.Is(err, ssrfguard.ErrBlocked)
		if blocked {
			slog.Warn("wh_ssrf_blocked", "event", event, "webhook", wh.ID, "attempt", attempt, "err", errMsg)
		}
		return false, blocked, 0, errMsg
	}
	defer pin.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		return false, false, 0, err.Error()
	}
	mac := hmac.New(sha256.New, []byte(wh.Secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-JomForm-Event", event)
	req.Header.Set("X-JomForm-Signature", "sha256="+sig)
	if signPriv != nil {
		edSig := ed25519.Sign(signPriv, body)
		req.Header.Set("X-JomForm-Signature-Ed25519", base64.StdEncoding.EncodeToString(edSig))
		req.Header.Set("X-JomForm-Key-Id", accountID)
	}
	req.Header.Set("X-JomForm-Delivery", fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), attempt))
	req.Header.Set("User-Agent", "JomForm-Webhook/2.0")

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: pin.Transport(),
	}
	resp, err := client.Do(req)

	if err != nil {
		errMsg = err.Error()
	} else {
		code = resp.StatusCode
		success = resp.StatusCode >= 200 && resp.StatusCode < 300
		resp.Body.Close()
	}

	_ = store.RecordDelivery(ctx, wh.ID, event, string(body), code, success, attempt, errMsg)

	if success {
		slog.Info("wh_delivered", "event", event, "webhook", wh.ID, "attempt", attempt)
	} else {
		slog.Warn("wh_failed", "event", event, "webhook", wh.ID, "attempt", attempt, "err", errMsg)
	}
	return success, false, code, errMsg
}

// RunWebhookRetrySweep polls the persistent retry queue and re-delivers due
// retries with exponential backoff. It runs until ctx is cancelled. The sweep
// interval controls how promptly due retries (and manual retries) are picked
// up; a short interval (e.g. 30s) keeps manual retries snappy.
func RunWebhookRetrySweep(ctx context.Context, h *RESTHandlers, store *dbstore.WebhookStore, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Run once immediately so queued retries from a previous process are
	// delivered promptly after a restart.
	sweepDueRetries(ctx, h, store)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sweepDueRetries(ctx, h, store); err != nil {
				slog.Warn("wh_retry_sweep", "err", err)
			}
		}
	}
}

// sweepDueRetries re-delivers every due, non-dead-lettered retry. On success
// the retry is removed from the queue; on failure it is rescheduled with the
// next backoff step, or dead-lettered after the final attempt.
func sweepDueRetries(ctx context.Context, h *RESTHandlers, store *dbstore.WebhookStore) error {
	retries, err := store.DueRetries(ctx, 100)
	if err != nil {
		return err
	}
	for _, r := range retries {
		wh, err := store.GetByIDAny(ctx, r.WebhookID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Webhook deleted — drop the orphaned retry.
				_ = store.DeleteRetry(ctx, r.ID)
				continue
			}
			slog.Warn("wh_retry_lookup", "err", err, "retry", r.ID)
			continue
		}
		if !wh.Active {
			// Webhook disabled — drop the retry.
			_ = store.DeleteRetry(ctx, r.ID)
			continue
		}
		signPub, signPriv := ensureSigningKey(ctx, h, wh.AccountID)
		success, blocked, _, _ := deliverWebhook(ctx, h, store, signPub, signPriv, wh.AccountID, wh, r.Event, []byte(r.Payload), r.Attempt)
		if success {
			_ = store.DeleteRetry(ctx, r.ID)
			continue
		}
		// Issue #366: an SSRF-blocked delivery is treated as permanently
		// failed — dead-letter it immediately instead of re-dispatching into
		// a private/metadata address over and over with backoff.
		if blocked {
			if err := store.MarkDeadLetter(ctx, r.ID); err != nil {
				slog.Warn("wh_dead_letter", "err", err, "retry", r.ID)
			}
			continue
		}
		if r.Attempt >= webhookMaxAttempts {
			// Final failure — dead-letter so it stays visible and can be
			// manually retried from the delivery log / MCP.
			if err := store.MarkDeadLetter(ctx, r.ID); err != nil {
				slog.Warn("wh_dead_letter", "err", err, "retry", r.ID)
			} else {
				slog.Error("wh_dead_lettered", "event", r.Event, "webhook", r.WebhookID, "attempt", r.Attempt)
			}
			continue
		}
		delay := webhookBackoff[r.Attempt-1]
		next := time.Now().Add(delay)
		if err := store.ScheduleRetry(ctx, r.ID, r.Attempt+1, next); err != nil {
			slog.Warn("wh_retry_schedule", "err", err, "retry", r.ID)
			continue
		}
		slog.Warn("wh_retry_scheduled", "event", r.Event, "webhook", r.WebhookID, "attempt", r.Attempt+1, "delay", delay)
	}
	return nil
}
