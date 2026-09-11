package httpapi

import (
	"fmt"
	"log/slog"
	"sync/atomic"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

var testIPCounter atomic.Uint32

// uniqueTestIP returns a fresh IP for one test. The auth handlers key their
// per-IP lockout and rate-limit counters in the shared test Valkey (15-minute
// TTL), so a fixed IP accumulates failures across -count runs: a test that
// deliberately drives one IP into lockout would find that IP already locked on
// the next run. Deriving the IP per test keeps the counters isolated by
// construction (issue #474).
//
// Generate one value at the top of the test and reuse it for every request in
// that test — tests that compare responses, or that drive a single IP to a
// limit, need a stable IP within the test.
func uniqueTestIP() string {
	n := testIPCounter.Add(1)
	return fmt.Sprintf("10.20.%d.%d", (n/250)%250, n%250+1)
}
