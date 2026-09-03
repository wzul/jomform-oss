// Package db provides Postgres connectivity for jomform-api.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/metrics"
)

// Connect opens a lazy connection pool and verifies reachability.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return ConnectWithTracer(ctx, databaseURL, nil, nil, 0)
}

// ConnectWithTracer opens a lazy connection pool wired with a query tracer
// (issue #211). When m and log are non-nil, every query's duration is recorded
// in the jomform_db_query_duration_seconds histogram and queries exceeding
// slowAfter are logged + counted in jomform_db_slow_queries_total. Passing nil
// metrics/logger (or slowAfter <= 0) disables instrumentation.
func ConnectWithTracer(ctx context.Context, databaseURL string, m *metrics.Metrics, log *slog.Logger, slowAfter time.Duration) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if m != nil || log != nil {
		cfg.ConnConfig.Tracer = NewQueryTracer(m, log, slowAfter)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
