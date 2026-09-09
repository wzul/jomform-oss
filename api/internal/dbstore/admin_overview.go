// Platform-wide usage overview queries for the super-admin dashboard (issue
// #387). These are read-only and intentionally NOT account-scoped: they are
// only reachable through the super-admin gate in httpapi (requireSuperAdmin),
// never through normal user endpoints. Each query uses its own explicit
// SELECT (not the *Select constants) so the migration/scan-drift guard is
// unaffected.
package dbstore

import (
	"context"
	"time"
)

// PlatformTotals is the platform-wide headline counts (issue #387).
type PlatformTotals struct {
	Spaces   int64 `json:"spaces"`
	Forms    int64 `json:"forms"`
	Products int64 `json:"products"`
	Sales    int64 `json:"sales"`
	Users    int64 `json:"users"`
	// MediaBytes is the total non-deleted media storage across all spaces.
	MediaBytes int64 `json:"media_bytes"`
}

// PlatformTotals returns the platform-wide headline counts. Super-admin only
// (issue #387).
func (s *SpaceStore) PlatformTotals(ctx context.Context) (PlatformTotals, error) {
	var t PlatformTotals
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM spaces),
		  (SELECT COUNT(*) FROM forms WHERE deleted_at IS NULL),
		  (SELECT COUNT(*) FROM products),
		  (SELECT COUNT(*) FROM sales),
		  (SELECT COUNT(*) FROM users),
		  (SELECT COALESCE(SUM(size),0) FROM media WHERE deleted_at IS NULL)`).
		Scan(&t.Spaces, &t.Forms, &t.Products, &t.Sales, &t.Users, &t.MediaBytes)
	return t, err
}

// GrowthCounts is the number of rows created within a window (issue #387).
type GrowthCounts struct {
	Spaces   int64 `json:"spaces"`
	Forms    int64 `json:"forms"`
	Products int64 `json:"products"`
	Sales    int64 `json:"sales"`
	Users    int64 `json:"users"`
}

// GrowthSince returns how many spaces/forms/products/sales/users were created
// since `since`. Super-admin only (issue #387).
func (s *SpaceStore) GrowthSince(ctx context.Context, since time.Time) (GrowthCounts, error) {
	var g GrowthCounts
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM spaces WHERE created_at >= $1),
		  (SELECT COUNT(*) FROM forms WHERE deleted_at IS NULL AND created_at >= $1),
		  (SELECT COUNT(*) FROM products WHERE created_at >= $1),
		  (SELECT COUNT(*) FROM sales WHERE created_at >= $1),
		  (SELECT COUNT(*) FROM users WHERE created_at >= $1)`, since).
		Scan(&g.Spaces, &g.Forms, &g.Products, &g.Sales, &g.Users)
	return g, err
}

// EmailDeliverySummary is the email delivery volume broken down by event type
// (issue #387). Failures = bounce + complaint.
type EmailDeliverySummary struct {
	Delivered int64 `json:"delivered"`
	Bounce    int64 `json:"bounce"`
	Complaint int64 `json:"complaint"`
	Open      int64 `json:"open"`
	Click     int64 `json:"click"`
}

// EmailDeliverySummary returns the platform-wide email delivery event counts
// by type. Super-admin only (issue #387).
func (s *EmailDeliveryEventStore) EmailDeliverySummary(ctx context.Context) (EmailDeliverySummary, error) {
	var sum EmailDeliverySummary
	rows, err := s.pool.Query(ctx, `
		SELECT event_type, COUNT(*) FROM email_delivery_events GROUP BY event_type`)
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int64
		if err := rows.Scan(&typ, &n); err != nil {
			return sum, err
		}
		switch typ {
		case "delivered":
			sum.Delivered = n
		case "bounce":
			sum.Bounce = n
		case "complaint":
			sum.Complaint = n
		case "open":
			sum.Open = n
		case "click":
			sum.Click = n
		}
	}
	return sum, rows.Err()
}

// SpamQueueSize is the number of spam-flagged submissions and sales across
// all spaces (issue #387).
type SpamQueueSize struct {
	Submissions int64 `json:"submissions"`
	Sales       int64 `json:"sales"`
}

// SpamQueueSize returns the platform-wide spam queue size. Super-admin only
// (issue #387).
func (s *SubmissionStore) SpamQueueSize(ctx context.Context) (SpamQueueSize, error) {
	var q SpamQueueSize
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM submissions WHERE is_spam),
		  (SELECT COUNT(*) FROM sales WHERE is_spam)`).
		Scan(&q.Submissions, &q.Sales)
	return q, err
}

// RecentSignup is one recently-created user (issue #387).
type RecentSignup struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RecentSignups returns the most recent `limit` user signups. Super-admin only
// (issue #387).
func (s *UserStore) RecentSignups(ctx context.Context, limit int) ([]RecentSignup, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, COALESCE(name,''), created_at
		FROM users ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecentSignup{}
	for rows.Next() {
		var x RecentSignup
		if err := rows.Scan(&x.ID, &x.Email, &x.Name, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ActiveSpace is one space with recent activity (issue #387). A space is
// "active" if it has any sale or submission within the window.
type ActiveSpace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	OwnerEmail string   `json:"owner_email"`
	LastActivity time.Time `json:"last_activity"`
}

// ActiveSpaces returns spaces with any sale/submission activity within the
// last `days`, newest activity first. Super-admin only (issue #387).
func (s *SpaceStore) ActiveSpaces(ctx context.Context, days int) ([]ActiveSpace, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, COALESCE(u.email,''),
		       GREATEST(COALESCE(MAX(sl.created_at), 'epoch'::timestamptz),
		                COALESCE(MAX(sb.created_at), 'epoch'::timestamptz)) AS last_activity
		FROM spaces sp
		LEFT JOIN users u ON u.id = sp.owner_user_id
		LEFT JOIN sales sl ON sl.space_id = sp.id AND sl.created_at >= $1
		LEFT JOIN submissions sb ON sb.form_id IN (SELECT id FROM forms WHERE space_id = sp.id) AND sb.created_at >= $1
		GROUP BY sp.id, sp.name, sp.slug, u.email
		HAVING GREATEST(COALESCE(MAX(sl.created_at), 'epoch'::timestamptz),
		                COALESCE(MAX(sb.created_at), 'epoch'::timestamptz)) > 'epoch'::timestamptz
		ORDER BY last_activity DESC
		LIMIT 20`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActiveSpace{}
	for rows.Next() {
		var x ActiveSpace
		if err := rows.Scan(&x.ID, &x.Name, &x.Slug, &x.OwnerEmail, &x.LastActivity); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
