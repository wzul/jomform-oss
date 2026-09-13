// Super-admin MCP tools — platform-wide usage monitoring (issue #386).
//
// These tools let the platform owner's AI agent monitor usage across ALL
// workspaces once JomForm is public. They are gated on the super-admin role
// (#385): every handler calls requireSuperAdmin first and auditSuperAdminAction
// afterwards. They are read-only by default — destructive admin writes are
// deliberately separate and explicit (none exist yet). The account-scoped
// user tools (#307) remain the only path for normal merchants.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// adminGate runs the super-admin gate + audit for a read-only admin tool.
// It returns an error when the caller is not a super-admin. Every admin tool
// handler MUST call this before doing any work.
func (s *Server) adminGate(ctx context.Context, action string) error {
	if err := s.requireSuperAdmin(ctx); err != nil {
		return err
	}
	s.auditSuperAdminAction(ctx, action, nil)
	return nil
}

// ── admin_list_workspaces ───────────────────────────────────────────────────────

type adminWorkspaceOut struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	OwnerEmail   string `json:"owner_email"`
	CreatedAt    string `json:"created_at"`
	FormCount    int64  `json:"form_count"`
	ProductCount int64  `json:"product_count"`
}

// adminListWorkspaces lists every workspace with its owner email, creation time and
// form/product counts. Super-admin only, read-only, audited.
func (s *Server) adminListWorkspaces(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, []adminWorkspaceOut, error) {
	if err := s.adminGate(ctx, "admin.list_workspaces"); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	rows, err := sp.ListAllWithCounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]adminWorkspaceOut, 0, len(rows))
	for _, x := range rows {
		out = append(out, adminWorkspaceOut{
			ID: x.ID, Name: x.Name, Slug: x.Slug, OwnerEmail: x.OwnerEmail,
			CreatedAt: x.CreatedAt.UTC().Format(time.RFC3339),
			FormCount: x.FormCount, ProductCount: x.ProductCount,
		})
	}
	return nil, out, nil
}

// ── admin_media_usage ───────────────────────────────────────────────────────

type adminMediaUsageOut struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	Slug      string `json:"slug"`
	UsedBytes int64  `json:"used_bytes"`
	QuotaBytes int64 `json:"quota_bytes"`
}

// adminMediaUsage reports storage usage per workspace (bytes) against the 1GB
// per-workspace quota (#384). Super-admin only, read-only, audited.
func (s *Server) adminMediaUsage(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, []adminMediaUsageOut, error) {
	if err := s.adminGate(ctx, "admin.media_usage"); err != nil {
		return nil, nil, err
	}
	if s.deps.Media == nil {
		return nil, nil, errors.New("media tidak tersedia")
	}
	rows, err := s.deps.Media.UsageByWorkspace(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]adminMediaUsageOut, 0, len(rows))
	for _, x := range rows {
		out = append(out, adminMediaUsageOut{
			WorkspaceID: x.WorkspaceID, WorkspaceName: x.WorkspaceName, Slug: x.Slug,
			UsedBytes: x.UsedBytes, QuotaBytes: objstore.MaxWorkspaceBytes,
		})
	}
	return nil, out, nil
}

// ── admin_list_sales ────────────────────────────────────────────────────────

type adminListSalesIn struct {
	DateFrom string `json:"date_from,omitempty" jsonschema:"Optional — tarikh mula (YYYY-MM-DD, inklusif)"`
	DateTo   string `json:"date_to,omitempty" jsonschema:"Optional — tarikh tamat (YYYY-MM-DD, inklusif)"`
	WorkspaceID  string `json:"workspace_id,omitempty" jsonschema:"Optional — hadkan kepada satu workspace (id dari admin_list_workspaces)"`
	Status   string `json:"status,omitempty" jsonschema:"Optional — pending|paid|confirmed|failed|cancelled"`
	Page     int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

type adminSaleOut struct {
	RefCode   string `json:"ref_code"`
	Status    string `json:"status"`
	TotalSen  int64  `json:"total_sen"`
	SaleType  string `json:"sale_type"`
	Client    string `json:"client"`
	FormID    string `json:"form_id,omitempty"`
	CreatedAt string `json:"created_at"`
	// Issue #518: partial-refund accounting for the platform-wide sales list.
	RefundedSen    int64  `json:"refunded_sen"`
	OutstandingSen int64  `json:"outstanding_sen"`
	RefundStatus   string `json:"refund_status"`
}

// adminListSales lists sales across all workspaces, filterable by date, workspace and
// status. Super-admin only, read-only, audited.
func (s *Server) adminListSales(ctx context.Context, _ *mcp.CallToolRequest, in adminListSalesIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.adminGate(ctx, "admin.list_sales"); err != nil {
		return nil, nil, err
	}
	if s.deps.Sales == nil {
		return nil, nil, errors.New("sales tidak tersedia")
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	f := dbstore.SaleFilter{
		Status:   in.Status,
		DateFrom: in.DateFrom,
		DateTo:   in.DateTo,
	}
	// Workspace filter: resolve the workspace's account and scope the query to it.
	// Otherwise query across all accounts.
	var rows []*dbstore.Sale
	var total int64
	var err error
	if in.WorkspaceID != "" {
		if s.deps.Accounts == nil {
			return nil, nil, errors.New("accounts tidak tersedia")
		}
		a, aerr := s.deps.Accounts.ByWorkspaceID(ctx, in.WorkspaceID)
		if aerr != nil || a == nil {
			return nil, nil, errors.New("workspace tidak dijumpai")
		}
		rows, total, err = s.deps.Sales.ListFilteredPage(ctx, []string{a.ID}, f, page, limit)
	} else {
		rows, total, err = s.deps.Sales.ListAllFilteredPage(ctx, f, page, limit)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]adminSaleOut, 0, len(rows))
	for _, sl := range rows {
		out = append(out, adminSaleOut{
			RefCode: sl.RefCode, Status: sl.Status, TotalSen: sl.TotalSen,
			SaleType: sl.SaleType, Client: sl.Client.FullName, FormID: sl.FormID,
			CreatedAt: sl.CreatedAt.UTC().Format(time.RFC3339),
			RefundedSen: sl.RefundedSen, OutstandingSen: sl.OutstandingSen(),
			RefundStatus: sl.RefundStatus(),
		})
	}
	return nil, map[string]any{"items": out, "page": page, "limit": limit, "total": total}, nil
}

// ── admin_spam_queue ────────────────────────────────────────────────────────

type adminSpamQueueIn struct {
	Page  int `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit int `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

type adminSpamItem struct {
	Kind       string         `json:"kind"` // submission | sale
	ID         string         `json:"id,omitempty"`
	RefCode    string         `json:"ref_code,omitempty"`
	FormID     string         `json:"form_id,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Status     string         `json:"status,omitempty"`
	TotalSen   int64          `json:"total_sen,omitempty"`
	Buyer      string         `json:"buyer,omitempty"`
	SpamReason string         `json:"spam_reason,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

// adminSpamQueue lists all spam-flagged submissions and sales across all
// workspaces. Super-admin only, read-only, audited.
func (s *Server) adminSpamQueue(ctx context.Context, _ *mcp.CallToolRequest, in adminSpamQueueIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.adminGate(ctx, "admin.spam_queue"); err != nil {
		return nil, nil, err
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	out := []adminSpamItem{}
	var total int64
	// The account-scoped spam listers require an account set. For the
	// platform-wide view we enumerate all accounts and aggregate.
	accs := []string{}
	if s.deps.Accounts != nil {
		all, err := s.deps.Accounts.AllIDs(ctx)
		if err == nil {
			accs = all
		}
	}
	if len(accs) == 0 {
		return nil, map[string]any{"items": out, "page": page, "limit": limit, "total": 0}, nil
	}
	if s.deps.Submissions != nil {
		subs, n, err := s.deps.Submissions.ListSpamByAccountsPage(ctx, accs, page, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		total += n
		for _, sub := range subs {
			reason := ""
			if sub.SpamReason != nil {
				reason = *sub.SpamReason
			}
			out = append(out, adminSpamItem{
				Kind: "submission", ID: sub.ID, FormID: sub.FormID, Data: sub.Data,
				SpamReason: reason, CreatedAt: sub.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	if s.deps.Sales != nil {
		sales, n, err := s.deps.Sales.ListSpamByAccountsPage(ctx, accs, page, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		total += n
		for _, sl := range sales {
			reason := ""
			if sl.SpamReason != nil {
				reason = *sl.SpamReason
			}
			out = append(out, adminSpamItem{
				Kind: "sale", RefCode: sl.RefCode, FormID: sl.FormID, Status: sl.Status,
				TotalSen: sl.TotalSen, Buyer: sl.Client.FullName,
				SpamReason: reason, CreatedAt: sl.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	return nil, map[string]any{"items": out, "page": page, "limit": limit, "total": total}, nil
}

// ── admin_email_deliveries ──────────────────────────────────────────────────

type adminEmailDeliveriesIn struct {
	SaleRef   string `json:"sale_ref,omitempty" jsonschema:"Optional — hadkan kepada satu jualan (ref_code)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Optional — delivered | bounce | complaint | open | click"`
	FormID    string `json:"form_id,omitempty" jsonschema:"Optional — hadkan kepada jualan satu borang"`
	Page      int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

// adminEmailDeliveries lists email delivery status across all workspaces.
// Super-admin only, read-only, audited.
func (s *Server) adminEmailDeliveries(ctx context.Context, _ *mcp.CallToolRequest, in adminEmailDeliveriesIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.adminGate(ctx, "admin.email_deliveries"); err != nil {
		return nil, nil, err
	}
	if s.deps.EmailDeliveryEvents == nil {
		return nil, nil, errors.New("store penghantaran emel tidak dikonfigurasi")
	}
	f := dbstore.DeliveryFilter{
		SaleRef:   in.SaleRef,
		EventType: in.EventType,
		FormID:    in.FormID,
		Page:      in.Page,
		Limit:     in.Limit,
	}
	events, total, err := s.deps.EmailDeliveryEvents.ListAllByFilter(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	out := make([]emailDeliveryOut, 0, len(events))
	for _, e := range events {
		out = append(out, emailDeliveryOut{
			ID: e.ID, Email: e.Email, EventType: e.EventType,
			BounceType: e.BounceType, SaleRef: e.SaleRef, FormID: e.FormID, Detail: e.Detail,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{"items": out, "page": f.Page, "limit": f.Limit, "total": total}, nil
}

// ── admin_system_health ─────────────────────────────────────────────────────

type adminSystemHealthOut struct {
	UptimeSeconds int64  `json:"uptime_seconds"`
	RecentErrors  int64  `json:"recent_errors_24h"`
	QueueDepth    int64  `json:"queue_depth"`
	QueueNote     string `json:"queue_note,omitempty"`
}

// adminSystemHealth reports uptime, recent MCP tool-call errors and the email
// queue depth. Super-admin only, read-only, audited.
func (s *Server) adminSystemHealth(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, adminSystemHealthOut, error) {
	if err := s.adminGate(ctx, "admin.system_health"); err != nil {
		return nil, adminSystemHealthOut{}, err
	}
	out := adminSystemHealthOut{UptimeSeconds: int64(time.Since(s.startedAt).Seconds())}
	if s.deps.Events != nil {
		since := time.Now().Add(-24 * time.Hour)
		if n, err := s.deps.Events.CountRecentErrors(ctx, since); err == nil {
			out.RecentErrors = n
		}
	}
	if s.deps.AssetQueue != nil {
		if n, err := s.deps.AssetQueue.Len(ctx); err == nil {
			out.QueueDepth = n
		}
	} else {
		out.QueueNote = "queue tidak dikonfigurasi (dev)"
	}
	return nil, out, nil
}
