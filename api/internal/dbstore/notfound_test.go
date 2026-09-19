package dbstore

// Boundary test for isNoSuchRow: it must map exactly the two shapes that mean
// "that id names no row this query could return" — pgx.ErrNoRows and SQLSTATE
// 22P02 — and must NOT swallow a real failure. The second direction is the one
// no route-level test can see: a helper mutated to `return err != nil` would
// turn every database outage, permission error and constraint violation into a
// 404, and every not-found test would still pass.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsNoSuchRowMatchesOnlyTheTwoMissShapes(t *testing.T) {
	// The two shapes that mean "no such row to the caller".
	miss := []struct {
		name string
		err  error
	}{
		{"bare ErrNoRows", pgx.ErrNoRows},
		{"wrapped ErrNoRows", fmt.Errorf("lookup product: %w", pgx.ErrNoRows)},
		{"SQLSTATE 22P02", &pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type uuid: "not-a-uuid"`}},
		{"wrapped 22P02", fmt.Errorf("exec: %w", &pgconn.PgError{Code: "22P02"})},
	}
	for _, c := range miss {
		if !isNoSuchRow(c.err) {
			t.Errorf("isNoSuchRow(%s) = false, want true", c.name)
		}
	}

	// Real failures must stay failures: a helper that returned true for any
	// non-nil error would convert these into 404s and blind the error counter.
	failures := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"plain error", errors.New("connection refused")},
		{"undefined table 42P01", &pgconn.PgError{Code: "42P01", Message: `relation "forms" does not exist`}},
		{"not-null violation 23502", &pgconn.PgError{Code: "23502", Message: "null value in column violates not-null constraint"}},
		{"invalid password 28P01", &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}},
		{"connection failure 08006", &pgconn.PgError{Code: "08006", Message: "connection failure"}},
		{"query canceled 57014", &pgconn.PgError{Code: "57014", Message: "canceling statement due to user request"}},
		{"unique violation 23505", &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}},
		{"context canceled", context.Canceled},
		{"wrapped 42P01", fmt.Errorf("count: %w", &pgconn.PgError{Code: "42P01"})},
	}
	for _, c := range failures {
		if isNoSuchRow(c.err) {
			t.Errorf("isNoSuchRow(%s) = true, want false — a real failure must not become a 404", c.name)
		}
	}
}
