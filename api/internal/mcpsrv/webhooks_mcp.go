// MCP parity tools: e-invoice settings + webhook CRUD + deliveries.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/ssrfguard"
)

// validWebhookEvents is the dispatcher's known event set (see
// api/internal/httpapi/dispatch2.go and purchases.go). "*" subscribes to all.
var validWebhookEvents = map[string]bool{
	"sale.paid":            true,
	"sale.failed":          true,
	"subscription.charged": true,
	"*":                    true,
}

// validateWebhookURL is the SSRF guard for create_webhook/update_webhook. It
// requires an http(s) URL and rejects hosts that resolve to private, loopback,
// link-local or unspecified addresses (RFC1918, ::1, 169.254.*, fe80::, CGNAT
// 100.64/10), mirroring and exceeding the REST handler's convention of
// https-only webhook endpoints. The shared ssrfguard is also enforced at
// delivery time in the dispatcher (issue #366).
func validateWebhookURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return errors.New("url webhook mesti https")
	}
	if _, err := ssrfguard.ValidateURL(ctx, raw); err != nil {
		return errors.New("url webhook tidak sah (bukan host peribadi/loopback)")
	}
	return nil
}

// validateWebhookEvents rejects event names outside the dispatcher's known set.
func validateWebhookEvents(events []string) error {
	for _, e := range events {
		if !validWebhookEvents[e] {
			return fmt.Errorf("event tidak sah: %q (sale.paid|sale.failed|subscription.charged|*)", e)
		}
	}
	return nil
}

// ── e-Invoice settings ──────────────────────────────────────────────────────

type setEinvoiceIn struct {
	TIN  string `json:"tin" jsonschema:"Nombor TIN LHDN (kosong = clear)"`
	MSIC string `json:"msic" jsonschema:"Kod MSIC 5 digit"`
}

func (s *Server) setEinvoice(
	ctx context.Context, _ *mcp.CallToolRequest, in setEinvoiceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.SetEinvoice(ctx, acc, in.TIN, in.MSIC); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "tin": in.TIN, "msic": in.MSIC}, nil
}

func (s *Server) getEinvoiceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: e-invoice settings are scoped to the call's target account
	// (explicit workspace_id/account_id or the user's first accessible account).
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	tin, msic, err := s.deps.Accounts.Einvoice(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"tin": tin, "msic": msic}, nil
}

// ── Webhooks CRUD ────────────────────────────────────────────────────────────

type webhookIn struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events,omitempty" jsonschema:"sale.paid|sale.failed|subscription.charged|*"`
	// Issue #196: optional form_id scopes the webhook to one form (fires only
	// for that form's events). Empty = workspace/account-wide.
	FormID string `json:"form_id,omitempty" jsonschema:"ID borang (pilihan). Set = webhook hanya untuk borang ini; kosong = seluruh workspace/akaun."`
}

func (s *Server) createWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: the target account is the scoped one (workspace_id/account_id or
	// the caller's first accessible account); RBAC-gate it.
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(in.URL) < 8 || len(in.Secret) < 8 {
		return nil, nil, errors.New("url dan secret (min 8 char) wajib")
	}
	events := in.Events
	if len(events) == 0 {
		events = []string{"sale.paid"}
	}
	if err := validateWebhookURL(ctx, in.URL); err != nil {
		return nil, nil, err
	}
	if err := validateWebhookEvents(events); err != nil {
		return nil, nil, err
	}
	// Issue #196: a form-scoped webhook must reference a form the account owns.
	if in.FormID != "" {
		if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
			return nil, nil, errors.New("borang tidak dijumpai")
		}
	}
	wh, err := s.deps.Webhooks.Create(ctx, acc, in.FormID, in.URL, in.Secret, events)
	if err != nil {
		return nil, nil, err
	}
	// The signing secret is only surfaced once (at creation), matching the
	// dashboard — it is never returned by list_webhooks.
	return nil, map[string]string{"id": wh.ID, "status": "created", "secret": wh.Secret}, nil
}

type webhookListOut struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
	// Issue #196: form_id is non-empty for form-scoped webhooks.
	FormID string `json:"form_id,omitempty"`
}

type webhookListIn struct {
	// Issue #196: optional form_id narrows to that form's webhooks; empty =
	// workspace/account-wide webhooks.
	FormID string `json:"form_id,omitempty" jsonschema:"ID borang (pilihan). Set = senarai webhook borang ini sahaja; kosong = webhook seluruh workspace/akaun."`
}

func (s *Server) listWebhooksMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookListIn,
) (*mcp.CallToolResult, *listOutput[webhookListOut], error) {
	accs, err := s.accountScopes(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	// Issue #307: list webhooks across all of the user's workspaces.
	out := []webhookListOut{}
	for _, acc := range accs {
		hooks, err := s.deps.Webhooks.ListByAccount(ctx, acc, in.FormID)
		if err != nil {
			return nil, nil, err
		}
		for _, w := range hooks {
			out = append(out, webhookListOut{ID: w.ID, URL: w.URL, Events: w.Events, Active: w.Active, FormID: w.FormID})
		}
	}
	return nil, newListOut(out), nil
}

type webhookRef struct {
	ID string `json:"id"`
}

func (s *Server) deleteWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookRef,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #307: the webhook may live in any of the user's workspaces; resolve the
	// owning account and RBAC-gate it.
	acc, err := s.resolveWebhookAccount(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Webhooks.Delete(ctx, acc, in.ID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "deleted"}, nil
}

type webhookUpdateIn struct {
	ID     string   `json:"id" jsonschema:"ID webhook (dari list_webhooks)"`
	URL    string   `json:"url" jsonschema:"URL endpoint baharu (https)"`
	Secret string   `json:"secret,omitempty" jsonschema:"Rahsia baharu; kosong = kekal sedia ada (min 8 char jika diisi)"`
	Events []string `json:"events,omitempty" jsonschema:"sale.paid|sale.failed|subscription.charged|*"`
}

func (s *Server) updateWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookUpdateIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.resolveWebhookAccount(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return nil, nil, err
	}
	if len(in.URL) < 8 {
		return nil, nil, errors.New("url wajib (min 8 char)")
	}
	if in.Secret != "" && len(in.Secret) < 8 {
		return nil, nil, errors.New("secret (min 8 char) wajib bila diisi; kosong = kekal sedia ada")
	}
	if err := validateWebhookURL(ctx, in.URL); err != nil {
		return nil, nil, err
	}
	events := in.Events
	if len(events) == 0 {
		events = []string{"sale.paid"}
	}
	if err := validateWebhookEvents(events); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Webhooks.Update(ctx, acc, in.ID, in.URL, in.Secret, events); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"id": in.ID, "status": "updated"}, nil
}

type webhookDeliveriesIn struct {
	WebhookID string `json:"webhook_id"`
	Limit     int    `json:"limit,omitempty"`
}

func (s *Server) webhookDeliveriesMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookDeliveriesIn,
) (*mcp.CallToolResult, *listOutput[map[string]any], error) {
	acc, err := s.resolveWebhookAccount(ctx, in.WebhookID)
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	deliveries, err := s.deps.Webhooks.ListDeliveries(ctx, acc, in.WebhookID, limit)
	if err != nil {
		return nil, nil, err
	}
	// Issue #197: surface dead-lettered retries alongside the delivery log so
	// merchants can see (and manually retry) dropped deliveries.
	dead, err := s.deps.Webhooks.ListDeadLettered(ctx, acc, in.WebhookID, limit)
	if err == nil && len(dead) > 0 {
		deliveries = append(deliveries, dead...)
	}
	return nil, newListOut(deliveries), nil
}

// retryWebhookDeliveryIn is the input for retry_webhook_delivery.
type retryWebhookDeliveryIn struct {
	WebhookID  string `json:"webhook_id" jsonschema:"ID webhook (dari list_webhooks)"`
	DeliveryID string `json:"delivery_id" jsonschema:"ID penghantaran yang gagal (dari webhook_deliveries)"`
}

// retryWebhookDeliveryMCP re-enqueues a failed webhook delivery immediately
// (issue #197, MCP parity with the dashboard Retry action). Both ids come from
// the caller: the delivery is looked up account-scoped, and — issue #522 — the
// delivery's owning webhook must be exactly the requested webhook_id before
// anything is enqueued. Without that check a caller could name another
// workspace's webhook and have its own payload delivered to that endpoint,
// signed with that webhook's secret. EnqueueRetry is itself account-scoped.
func (s *Server) retryWebhookDeliveryMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in retryWebhookDeliveryIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.resolveWebhookAccount(ctx, in.WebhookID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return nil, nil, err
	}
	delivery, err := s.deps.Webhooks.GetDelivery(ctx, acc, in.DeliveryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, errors.New("penghantaran tidak dijumpai")
		}
		return nil, nil, err
	}
	if owned, _ := delivery["webhook_id"].(string); owned != in.WebhookID {
		return nil, nil, errors.New("penghantaran tidak dijumpai")
	}
	event, _ := delivery["event"].(string)
	payload, _ := delivery["payload"].(string)
	if err := s.deps.Webhooks.EnqueueRetry(ctx, acc, in.WebhookID, event, payload, 1, time.Now()); err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"status": "queued", "webhook_id": in.WebhookID, "delivery_id": in.DeliveryID}, nil
}
