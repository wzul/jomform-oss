// Package db provides Postgres connectivity for jomform-api.
package db

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wzul/jomform/api/internal/metrics"
)

// QueryTracer implements pgx.QueryTracer to record every query's duration as a
// Prometheus histogram and to log queries that exceed a slow-query threshold
// (issue #211). It is nil-safe: a nil *Metrics or nil *slog.Logger is a no-op,
// so bare-mode tests and health-only servers keep working without wiring.
type QueryTracer struct {
	metrics   *metrics.Metrics
	log       *slog.Logger
	slowAfter time.Duration
}

// NewQueryTracer builds a QueryTracer. slowAfter is the threshold above which a
// query is logged as slow (and counted in jomform_db_slow_queries_total).
func NewQueryTracer(m *metrics.Metrics, log *slog.Logger, slowAfter time.Duration) *QueryTracer {
	return &QueryTracer{metrics: m, log: log, slowAfter: slowAfter}
}

// TraceQueryStart implements pgx.QueryTracer. It records the start time and the
// SQL text in the returned context so TraceQueryEnd can measure the full
// duration and classify the query.
func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = context.WithValue(ctx, queryStartKey{}, time.Now())
	return context.WithValue(ctx, querySQLKey{}, data.SQL)
}

// TraceQueryEnd implements pgx.QueryTracer. It records the query duration in
// the histogram and logs + counts the query when it exceeds the slow threshold.
func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if t == nil {
		return
	}
	start, ok := ctx.Value(queryStartKey{}).(time.Time)
	if !ok {
		return
	}
	sql, _ := ctx.Value(querySQLKey{}).(string)
	dur := time.Since(start)
	class := QueryClass(sql)

	if t.metrics != nil {
		t.metrics.ObserveDBQuery(class, dur.Seconds())
	}
	if t.slowAfter > 0 && dur >= t.slowAfter {
		if t.metrics != nil {
			t.metrics.IncDBSlowQuery(class)
		}
		if t.log != nil {
			t.log.Warn("slow_db_query",
				"query_class", class,
				"duration_ms", dur.Milliseconds(),
				"threshold_ms", t.slowAfter.Milliseconds(),
				"sql", sql,
			)
		}
	}
}

// queryStartKey is the context key used to carry the query start time.
type queryStartKey struct{}

// querySQLKey is the context key used to carry the query SQL text.
type querySQLKey struct{}

// QueryClass normalises a SQL statement into a bounded label so Prometheus
// cardinality stays low. It returns "<VERB> <table>" for the common
// SELECT/INSERT/UPDATE/DELETE statements (e.g. "SELECT forms"), falling back
// to a generic class for anything else.
func QueryClass(sql string) string {
	s := strings.TrimSpace(sql)
	if s == "" {
		return "unknown"
	}
	upper := strings.ToUpper(s)
	var verb string
	switch {
	case strings.HasPrefix(upper, "SELECT"):
		verb = "SELECT"
	case strings.HasPrefix(upper, "INSERT"):
		verb = "INSERT"
	case strings.HasPrefix(upper, "UPDATE"):
		verb = "UPDATE"
	case strings.HasPrefix(upper, "DELETE"):
		verb = "DELETE"
	case strings.HasPrefix(upper, "WITH"):
		verb = "WITH"
	default:
		return "other"
	}

	// For SELECT, the table follows the FROM keyword (skip the column list).
	// For INSERT it follows INTO. For UPDATE/DELETE it is the first identifier.
	var table string
	switch verb {
	case "SELECT":
		table = tokenAfterKeyword(s, "FROM")
	case "INSERT":
		table = tokenAfterKeyword(s, "INTO")
	case "UPDATE", "DELETE":
		table = firstIdentifier(s[len(verb):])
	case "WITH":
		table = firstIdentifier(s[len(verb):])
	}
	if table == "" {
		return verb
	}
	return verb + " " + table
}

// tokenAfterKeyword returns the first identifier token following the given
// keyword, or "" if the keyword is absent.
func tokenAfterKeyword(s, keyword string) string {
	upper := strings.ToUpper(s)
	idx := strings.Index(upper, keyword)
	if idx < 0 {
		return ""
	}
	return firstIdentifier(s[idx+len(keyword):])
}

// firstIdentifier returns the first non-keyword identifier token in s.
func firstIdentifier(s string) string {
	for _, f := range strings.Fields(s) {
		clean := strings.Trim(f, `"()`)
		if clean == "" {
			continue
		}
		switch strings.ToUpper(clean) {
		case "DISTINCT", "INTO", "FROM", "ALL", "ONLY", "*":
			continue
		}
		return clean
	}
	return ""
}
