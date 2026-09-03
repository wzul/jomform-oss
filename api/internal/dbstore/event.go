// EventStore — generic user-action audit/behaviour log (issue #92).
//
// The `events` table is append-only and immutable: rows are written once by
// Record and never updated or deleted except by the retention sweep. This
// matches ClickHouse's model, so a future export is a straight COPY/stream
// into a MergeTree partitioned by toYYYYMM(created_at). No heavy per-row
// analytics indexes are added now — the two time indexes + JSONB cover current
// needs and ClickHouse would replace them at scale.
package dbstore

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event mirrors one events row.
type Event struct {
	ID         int64
	AccountID  string
	UserID     string // "" when null (system/API-key action)
	EventName  string
	Properties map[string]any
	IP         string // "" when null
	UserAgent  string
	CreatedAt  time.Time
}

// EventFilter narrows an events query. All fields are optional; empty values
// are ignored. AccountIDs is required (account-scoped reads).
type EventFilter struct {
	AccountIDs []string
	EventName  string
	From       *time.Time
	To         *time.Time
	Page       int
	Limit      int
}

// EventStore wraps pgx for the events table.
type EventStore struct{ pool *pgxpool.Pool }

// NewEventStore builds an EventStore over the given pool.
func NewEventStore(pool *pgxpool.Pool) *EventStore { return &EventStore{pool: pool} }

// Record appends one event row. accountID is required; userID, ip and ua may
// be empty (stored as NULL). props is stored as JSONB (nil → {}). The write is
// best-effort for the caller's convenience: it returns the error so callers
// can log it, but event recording must never break the primary action.
func (s *EventStore) Record(ctx context.Context, accountID, userID, eventName string, props map[string]any, ip, ua string) error {
	if props == nil {
		props = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (account_id, user_id, event_name, properties, ip, user_agent)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, NULLIF($5,'')::inet, NULLIF($6,''))`,
		accountID, userID, eventName, props, ip, ua)
	return err
}

// eventWhere builds the WHERE clause + args for an account-scoped filter.
// Returns the clause (with leading " WHERE ") and the bound args.
//
// EventName supports a trailing "*" wildcard (e.g. "mcp.*" matches every
// mcp.tool_call row) so the dashboard and list_events can filter MCP request
// history (issue #195). A plain name matches exactly.
func eventWhere(f EventFilter) (string, []any) {
	args := []any{f.AccountIDs}
	clause := " WHERE account_id = ANY($1)"
	if f.EventName != "" {
		if strings.HasSuffix(f.EventName, "*") {
			args = append(args, strings.TrimSuffix(f.EventName, "*")+"%")
			clause += " AND event_name LIKE $" + itoa(len(args))
		} else {
			args = append(args, f.EventName)
			clause += " AND event_name = $" + itoa(len(args))
		}
	}
	if f.From != nil {
		args = append(args, *f.From)
		clause += " AND created_at >= $" + itoa(len(args))
	}
	if f.To != nil {
		args = append(args, *f.To)
		clause += " AND created_at <= $" + itoa(len(args))
	}
	return clause, args
}

// List returns one page of events, newest first, plus the total row count for
// the same filter (for paging). Account-scoped.
func (s *EventStore) List(ctx context.Context, f EventFilter) ([]Event, int64, error) {
	if len(f.AccountIDs) == 0 {
		return []Event{}, 0, nil
	}
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

	where, args := eventWhere(f)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id::text, COALESCE(user_id::text,''), event_name, properties,
		       COALESCE(host(ip),''), COALESCE(user_agent,''), created_at
		FROM events`+where+` ORDER BY created_at DESC, id DESC LIMIT $`+itoa(len(args)+1)+` OFFSET $`+itoa(len(args)+2),
		append(args, f.Limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.AccountID, &e.UserID, &e.EventName, &e.Properties,
			&e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Count returns the number of events matching the filter (behaviour analytics).
func (s *EventStore) Count(ctx context.Context, f EventFilter) (int64, error) {
	if len(f.AccountIDs) == 0 {
		return 0, nil
	}
	where, args := eventWhere(f)
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM events`+where, args...).Scan(&total)
	return total, err
}

// EventBreakdown is one event_name bucket in the aggregate.
type EventBreakdown struct {
	EventName string `json:"event_name"`
	Count     int64  `json:"count"`
}

// Breakdown aggregates event counts by event_name for behaviour analytics.
// Rows are ordered by count descending.
func (s *EventStore) Breakdown(ctx context.Context, f EventFilter) ([]EventBreakdown, error) {
	if len(f.AccountIDs) == 0 {
		return []EventBreakdown{}, nil
	}
	where, args := eventWhere(f)
	rows, err := s.pool.Query(ctx, `
		SELECT event_name, COUNT(*) FROM events`+where+`
		GROUP BY event_name ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventBreakdown{}
	for rows.Next() {
		var b EventBreakdown
		if err := rows.Scan(&b.EventName, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Sweep deletes events older than their per-class TTL. ttlByClass maps an
// event_name prefix (e.g. "auth.") to a retention duration; the default TTL
// applies to any event whose name does not match a class. Returns the number
// of rows deleted. This is the ONLY DELETE allowed on the events table.
func (s *EventStore) Sweep(ctx context.Context, ttlByClass map[string]time.Duration, defaultTTL time.Duration) (int64, error) {
	// Delete in two passes: first the default-TTL rows (older than default),
	// then any class rows whose own TTL is shorter than default. To keep the
	// query simple and correct, we delete per class with its own cutoff and
	// finally the default bucket for names not covered by a class.
	var deleted int64

	// Class buckets: delete rows older than the class TTL.
	for prefix, ttl := range ttlByClass {
		if ttl <= 0 {
			continue
		}
		cutoff := time.Now().UTC().Add(-ttl)
		tag, err := s.pool.Exec(ctx,
			`DELETE FROM events WHERE event_name LIKE $1 AND created_at < $2`,
			prefix+"%", cutoff)
		if err != nil {
			return deleted, err
		}
		deleted += tag.RowsAffected()
	}

	// Default bucket: rows whose name matches no class, older than default TTL.
	if defaultTTL > 0 {
		cutoff := time.Now().UTC().Add(-defaultTTL)
		clause := "created_at < $1"
		args := []any{cutoff}
		for prefix := range ttlByClass {
			args = append(args, prefix+"%")
			clause += " AND event_name NOT LIKE $" + itoa(len(args))
		}
		tag, err := s.pool.Exec(ctx, `DELETE FROM events WHERE `+clause, args...)
		if err != nil {
			return deleted, err
		}
		deleted += tag.RowsAffected()
	}
	return deleted, nil
}

// itoa is a tiny int→string helper (avoids importing strconv in hot paths).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
