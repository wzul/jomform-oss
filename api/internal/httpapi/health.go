package httpapi

import (
	"context"
	"time"
)

// checkDatabase pings Postgres with a short timeout.
func (s *Server) checkDatabase(ctx context.Context) error {
	if s.deps == nil || s.deps.Users == nil {
		return errDepsUnavailable("database not wired")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.deps.Users.Ping(ctx)
}

// checkValkey pings Valkey via the session store client.
func (s *Server) checkValkey(ctx context.Context) error {
	if s.deps == nil || s.deps.Sessions == nil {
		return errDepsUnavailable("valkey not wired")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.deps.Sessions.Ping(ctx)
}

type stringErr string

func (e stringErr) Error() string { return string(e) }

func errDepsUnavailable(msg string) error { return stringErr(msg) }
