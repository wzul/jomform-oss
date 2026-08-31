package dbstore

import "context"

// Ping verifies database reachability for readiness probes.
func (s *UserStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
