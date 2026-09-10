package db

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wzul/jomform/api/internal/metrics"
)

func TestQueryClass(t *testing.T) {
	cases := []struct {
		sql  string
		want string
	}{
		{"SELECT * FROM forms WHERE id = $1", "SELECT forms"},
		{"select id, name from products", "SELECT products"},
		{"SELECT DISTINCT slug FROM workspaces", "SELECT workspaces"},
		{"INSERT INTO sales (id) VALUES ($1)", "INSERT sales"},
		{"UPDATE accounts SET name = $1", "UPDATE accounts"},
		{"DELETE FROM events WHERE id = $1", "DELETE events"},
		{"WITH x AS (SELECT 1) SELECT * FROM x", "WITH x"},
		{"  ", "unknown"},
		{"", "unknown"},
		{"BEGIN", "other"},
	}
	for _, c := range cases {
		if got := QueryClass(c.sql); got != c.want {
			t.Errorf("QueryClass(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
}

func TestQueryTracerRecordsHistogramAndSlowLog(t *testing.T) {
	m := metrics.New()
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	tr := NewQueryTracer(m, logger, 1*time.Millisecond)

	// Fast query: recorded in histogram, not slow.
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM forms"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	// Slow query: recorded in histogram + slow counter + logged.
	ctx = tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM sales"})
	time.Sleep(2 * time.Millisecond)
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	// Verify the metrics are exposed on /metrics.
	body := scrape(t, m)
	if !strings.Contains(body, "jomform_db_query_duration_seconds") {
		t.Errorf("histogram metric missing from /metrics output")
	}
	if !strings.Contains(body, "jomform_db_slow_queries_total") {
		t.Errorf("slow-query counter missing from /metrics output")
	}
	if !strings.Contains(body, `query_class="SELECT sales"`) {
		t.Errorf("slow-query counter label missing: %s", body)
	}
	if !strings.Contains(logBuf.String(), "slow_db_query") {
		t.Errorf("slow query not logged: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "SELECT * FROM sales") {
		t.Errorf("slow query log missing SQL text: %s", logBuf.String())
	}
}

func TestQueryTracerNilSafe(t *testing.T) {
	// A nil tracer must not panic.
	var tr *QueryTracer
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
}

func scrape(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}
