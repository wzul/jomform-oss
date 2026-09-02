// MCP parity tools: e-invoice settings + webhook CRUD + deliveries.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// validWebhookEvents is the dispatcher's known event set (see
// api/internal/httpapi/dispatch2.go and purchases.go). "*" subscribes to all.
var validWebhookEvents = map[string]bool{
	"sale.paid":            true,
	"sale.failed":          true,
	"subscription.charged": true,
	"*":                    true,
}

// validateWebhookURL is the SSRF guard for create_webhook. It requires an
// https URL and rejects hosts that resolve to private, loopback, link-local or
// unspecified addresses (RFC1918, ::1, 169.254.*, fe80::, CGNAT 100.64/10),
// mirroring and exceeding the REST handler's convention of https-only webhook
// endpoints.
func validateWebhookURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() {
		return errors.New("url webhook tidak sah")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return errors.New("url webhook mesti https")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("url webhook mesti ada host")
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return errors.New("url webhook tidak boleh resolve")
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return errors.New("url webhook tidak boleh menunjuk ke host peribadi/loopback")
		}
		if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			// CGNAT 100.64.0.0/10
			return errors.New("url webhook tidak boleh menunjuk ke host peribadi")
		}
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
	acc, err := s.accountAndScope(ctx, true)
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
}

func (s *Server) createWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
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
	wh, err := s.deps.Webhooks.Create(ctx, acc, in.URL, in.Secret, events)
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
}

func (s *Server) listWebhooksMCP(
	ctx context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, []webhookListOut, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	hooks, err := s.deps.Webhooks.ListByAccount(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	out := []webhookListOut{}
	for _, w := range hooks {
		out = append(out, webhookListOut{ID: w.ID, URL: w.URL, Events: w.Events, Active: w.Active})
	}
	return nil, out, nil
}

type webhookRef struct {
	ID string `json:"id"`
}

func (s *Server) deleteWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookRef,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
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
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
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
) (*mcp.CallToolResult, []map[string]any, error) {
	acc, err := s.accountAndScope(ctx, true)
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
	return nil, deliveries, nil
}
