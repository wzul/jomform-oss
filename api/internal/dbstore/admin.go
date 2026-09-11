// Admin store queries — platform-wide monitoring surfaces for the super-admin
// MCP tools (issue #386). These are read-only and intentionally NOT
// account-scoped: they are only reachable through the super-admin gate in
// mcpsrv (requireSuperAdmin), never through normal user tools. Each query
// uses its own explicit SELECT (not the *Select constants) so the
// migration/scan-drift guard is unaffected.
package dbstore

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// WorkspaceWithCounts is one workspace row plus its owner email, creation time and
// form/product counts — the admin_list_workspaces surface (issue #386).
type WorkspaceWithCounts struct {
	ID           string
	Name         string
	Slug         string
	OwnerUserID  string
	OwnerEmail   string
	CreatedAt    time.Time
	FormCount    int64
	ProductCount int64
}

// ListAllWithCounts returns every workspace with owner email + form/product
// counts, newest first. Super-admin only (issue #386).
func (s *WorkspaceStore) ListAllWithCounts(ctx context.Context) ([]WorkspaceWithCounts, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, COALESCE(u.email,''), sp.created_at,
		       (SELECT COUNT(*) FROM forms f WHERE f.workspace_id = sp.id),
		       (SELECT COUNT(*) FROM products p WHERE p.workspace_id = sp.id)
		FROM workspaces sp
		LEFT JOIN users u ON u.id = sp.owner_user_id
		ORDER BY sp.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceWithCounts{}
	for rows.Next() {
		var x WorkspaceWithCounts
		if err := rows.Scan(&x.ID, &x.Name, &x.Slug, &x.OwnerUserID, &x.OwnerEmail,
			&x.CreatedAt, &x.FormCount, &x.ProductCount); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// MediaUsageByWorkspace is one workspace's media storage usage (issue #386).
type MediaUsageByWorkspace struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	Slug          string `json:"slug"`
	UsedBytes     int64  `json:"used_bytes"`
}

// UsageByWorkspace returns media storage usage per workspace (non-deleted bytes),
// joined through the workspace's account. Super-admin only (issue #386).
func (s *MediaStore) UsageByWorkspace(ctx context.Context) ([]MediaUsageByWorkspace, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, COALESCE(SUM(m.size),0)
		FROM workspaces sp
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		LEFT JOIN media m ON m.account_id = a.id AND m.deleted_at IS NULL
		GROUP BY sp.id, sp.name, sp.slug
		ORDER BY sp.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaUsageByWorkspace{}
	for rows.Next() {
		var x MediaUsageByWorkspace
		if err := rows.Scan(&x.WorkspaceID, &x.WorkspaceName, &x.Slug, &x.UsedBytes); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// AllIDs returns every account id. Super-admin only (issue #386).
func (s *AccountStore) AllIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListAllFilteredPage returns one page of sales across ALL accounts, newest
// first, filterable by form/status/date/query. Super-admin only (issue #386);
// the account-scoped ListFilteredPage remains the user-facing path.
func (s *SaleStore) ListAllFilteredPage(ctx context.Context, f SaleFilter, page, limit int) ([]*Sale, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	where := []string{"1=1"}
	args := []any{}
	next := func() int { return len(args) + 1 }

	if f.FormID != "" {
		args = append(args, f.FormID)
		where = append(where, "form_id::text = $"+strconv.Itoa(next()-1))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, "status = $"+strconv.Itoa(next()-1))
	}
	if f.DateFrom != "" {
		args = append(args, f.DateFrom+" 00:00:00")
		where = append(where, "created_at >= $"+strconv.Itoa(next()-1))
	}
	if f.DateTo != "" {
		args = append(args, f.DateTo+" 23:59:59")
		where = append(where, "created_at <= $"+strconv.Itoa(next()-1))
	}
	if f.Q != "" {
		args = append(args, "%"+f.Q+"%")
		where = append(where, "(ref_code ILIKE $"+strconv.Itoa(next()-1)+" OR client->>'full_name' ILIKE $"+strconv.Itoa(next()-1)+")")
	}

	clause := strings.Join(where, " AND ")
	offset := (page - 1) * limit

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	fullArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.pool.Query(ctx,
		saleSelect+" WHERE "+clause+" ORDER BY created_at DESC LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2),
		fullArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Sale{}
	for rows.Next() {
		sale, err := scanSale(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sale)
	}
	return out, total, rows.Err()
}

// ListAllByFilter returns one page of email delivery events across ALL
// accounts, newest first, filterable by sale/event/form. Super-admin only
// (issue #386); the account-scoped ListByFilter remains the user-facing path.
func (s *EmailDeliveryEventStore) ListAllByFilter(ctx context.Context, f DeliveryFilter) ([]EmailDeliveryEvent, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	offset := (f.Page - 1) * f.Limit

	args := []any{}
	next := func() int { return len(args) + 1 }
	clause := " WHERE 1=1"
	if f.SaleRef != "" {
		args = append(args, f.SaleRef)
		clause += " AND e.sale_ref = $" + strconv.Itoa(next()-1)
	}
	if f.EventType != "" {
		args = append(args, f.EventType)
		clause += " AND e.event_type = $" + strconv.Itoa(next()-1)
	}
	if f.FormID != "" {
		args = append(args, f.FormID)
		clause += " AND s.form_id::text = $" + strconv.Itoa(next()-1)
	}

	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM email_delivery_events e
		LEFT JOIN sales s ON s.ref_code = e.sale_ref`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(e.account_id::text,''), e.email, e.event_type,
		       COALESCE(e.bounce_type,''), COALESCE(e.sale_ref,''), e.detail, e.created_at,
		       COALESCE(s.form_id::text,'')
		FROM email_delivery_events e
		LEFT JOIN sales s ON s.ref_code = e.sale_ref`+clause+`
		ORDER BY e.created_at DESC, e.id DESC LIMIT $`+strconv.Itoa(len(args)+1)+` OFFSET $`+strconv.Itoa(len(args)+2),
		append(args, f.Limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []EmailDeliveryEvent{}
	for rows.Next() {
		var e EmailDeliveryEvent
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Email, &e.EventType,
			&e.BounceType, &e.SaleRef, &e.Detail, &e.CreatedAt, &e.FormID); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// CountRecentErrors counts mcp.tool_call events that failed since `since`
// (issue #386 admin_system_health). Super-admin only.
func (s *EventStore) CountRecentErrors(ctx context.Context, since time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM events
		WHERE event_name = 'mcp.tool_call' AND properties->>'success' = 'false' AND created_at >= $1`,
		since).Scan(&n)
	return n, err
}
