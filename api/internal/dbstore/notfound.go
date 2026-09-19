package dbstore

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// isNoSuchRow reports whether err is the driver's way of saying "that id names
// no row this query could return", rather than a real failure.
//
// Two shapes mean that, and both must map to the store's not-found sentinel:
//
//   - pgx.ErrNoRows — the row exists nowhere, or the query's ownership filter
//     excluded it (another account's row).
//   - SQLSTATE 22P02, "invalid input syntax for type uuid" — a caller-supplied
//     id that cannot name a row in a uuid-keyed table at all. Postgres raises
//     this for a malformed id, so passing the raw error up would turn a 404
//     into a 500 and a client's bad id into a server-fault metric.
//
// A caller-supplied id is untrusted input, so "cannot be a row id" and "is not
// a row I may see" are the same answer to the client. Checked at the error
// level rather than by pre-parsing the id so the store's opinion of a valid
// uuid stays exactly Postgres's (braces, hyphens, prefixes and all).
func isNoSuchRow(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
