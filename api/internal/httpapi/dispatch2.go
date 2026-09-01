// Outbound webhook dispatcher v2: delivery log, retries w/ backoff,
// provider-generic HMAC, event: sale.paid | sale.failed | subscription.charged.
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
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// DispatchWebhooks fires all matching active webhooks for an account.
// Sync for simplicity under caller goroutine; per-webhook 10s timeout.
func DispatchWebhooks(ctx context.Context, h *RESTHandlers, store *dbstore.WebhookStore, accountID, event string, payload map[string]any) {
	hooks, err := store.ListActiveByAccount(ctx, accountID)
	if err != nil {
		slog.Error("wh_list", "err", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	signPub, signPriv := ensureSigningKey(ctx, h, accountID)
	for _, wh := range hooks {
		if !matchesEvent(wh.Events, event) {
			continue
		}
		deliverWebhook(ctx, h, store, signPub, signPriv, accountID, wh, event, body, 1)
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

// deliverWebhook posts + records delivery; schedules retry on failure.
func deliverWebhook(ctx context.Context, h *RESTHandlers, store *dbstore.WebhookStore,
	signPub ed25519.PublicKey, signPriv ed25519.PrivateKey, accountID string, wh *dbstore.OutboundWebhook,
	event string, body []byte, attempt int) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		return
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)

	success := false
	var code int
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	} else {
		code = resp.StatusCode
		success = resp.StatusCode >= 200 && resp.StatusCode < 300
		resp.Body.Close()
	}

	_ = store.RecordDelivery(ctx, wh.ID, event, string(body), code, success, attempt, errMsg)

	if !success && attempt < 3 {
		// backoff: 1m, 5m
		delay := time.Duration(attempt) * 1 * time.Minute
		if attempt == 2 {
			delay = 5 * time.Minute
		}
		go func(at int) {
			time.Sleep(delay)
			dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			deliverWebhook(dctx, h, store, signPub, signPriv, accountID, wh, event, body, at)
		}(attempt + 1)
	}
	if success {
		slog.Info("wh_delivered", "event", event, "webhook", wh.ID, "attempt", attempt)
	} else {
		slog.Warn("wh_failed", "event", event, "webhook", wh.ID, "attempt", attempt, "err", errMsg)
	}
}
