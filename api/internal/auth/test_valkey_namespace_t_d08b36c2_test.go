// Per-process Valkey key namespace for this package's own test binary
// (card t_d08b36c2).
//
// Every Valkey-backed test in `internal/auth` used to open an UNSCOPED store on
// the shared test Valkey: `NewSessions($JOMFORM_TEST_VALKEY_URL)`. That store
// writes the PRODUCTION key layout — `jf:session:<sha256>` — and the seeds in
// session_keyspace_test.go wrote it with the raw token, one of them with TTL 0.
// Nothing removed those keys except each test's own t.Cleanup, and this package
// had no TestMain, so a run KILLED before cleanup (gate timeout, SIGTERM,
// dispatcher reclaim — all happen on this board) left production-layout session
// keys on a DB shared with every other checkout. One of them was IMMORTAL
// (TTL -1), which no expiry would ever clear and which a residue census has to
// stop and inspect by hand. Measured on the pre-fix tree: SIGKILL while the
// TTL-0 seed was held left `jf:session:adbb…` with PTTL -1.
//
// The fix is the one httpapi and mcpsrv already use: the process writes under
// its own namespace (`jf:session:<ns>:<hash>`, ns = testdb.Prefix() without the
// trailing colon, e.g. `jftest:p48213`), TestMain deletes exactly that slice,
// and the default TTLs are minutes so a killed run's leftovers expire instead
// of living for weeks. The namespace, not the TTL, is what makes the keys
// attributable: DeleteNamespaceKeys sweeps `jf:session:<ns>:*` and can reach
// neither a peer process's keys nor a production-layout one.
//
// The matching class guard lives in internal/httpapi
// (session_store_guard_t_05dd8cbd_test.go) and scans EVERY package's `_test.go`
// files for an unscoped session store, so a new package cannot reintroduce this.
package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/testdb"
)

// valkeyTestNamespace is the slice of the shared test Valkey THIS process owns.
// It is built from testdb.Prefix() rather than re-deriving the format, so the
// suite has one expression of "how a test process names its Valkey keys".
var valkeyTestNamespace = strings.TrimSuffix(testdb.Prefix(), ":")

const (
	// valkeyTestSessionTTL / valkeyTestTrustTTL are what fixture-minted keys live
	// for. Minutes, because nothing here needs a credential to outlive its own
	// test, and because a killed run must not be able to leave a 14-day session
	// or a 30-day trusted device behind.
	valkeyTestSessionTTL = 15 * time.Minute
	valkeyTestTrustTTL   = 15 * time.Minute

	// valkeyTestLegacyTTL is the TTL a hand-written legacy (raw-token) seed gets.
	// Short for the same reason: a seed the process never got to delete is
	// residue only until it expires.
	valkeyTestLegacyTTL = 15 * time.Minute
)

// valkeyTestOptions is what every session store in this package's tests passes:
// the per-process namespace plus the short test TTLs. A store built without the
// namespace writes the production key layout, which the exit sweep cannot see.
func valkeyTestOptions(opts ...SessionOption) []SessionOption {
	return append(opts,
		WithKeyNamespace(valkeyTestNamespace),
		WithDefaultTTL(valkeyTestSessionTTL),
		WithTrustTTL(valkeyTestTrustTTL),
	)
}

// TestValkeyTestStoreOwnsTheProcessNamespace pins the fix at the store the
// package's Valkey tests actually build: its keys are namespaced, its default
// TTLs are minutes, and the production layout is not its prefix.
func TestValkeyTestStoreOwnsTheProcessNamespace(t *testing.T) {
	if !strings.HasPrefix(valkeyTestNamespace, "jftest:p") {
		t.Fatalf("test key namespace = %q, want the per-process `jftest:p<pid>` form", valkeyTestNamespace)
	}
	s := valkeySessions(t)
	if got := s.KeyNamespace(); got != valkeyTestNamespace {
		t.Errorf("the package's Valkey test store is unscoped (namespace %q), want %q — an unscoped store writes the PRODUCTION `jf:session:<hash>` layout", got, valkeyTestNamespace)
	}
	if got, want := s.keyPrefix(), sessionKeyPrefix+valkeyTestNamespace+":"; got != want {
		t.Errorf("test store keyPrefix = %q, want %q", got, want)
	}
	if s.keyPrefix() == sessionKeyPrefix {
		t.Errorf("test store writes the production prefix %q", s.keyPrefix())
	}
	if got := s.createTTL(); got != valkeyTestSessionTTL {
		t.Errorf("test store default session TTL = %v, want %v (a killed run's keys must drain in minutes)", got, valkeyTestSessionTTL)
	}
	if got := s.TrustedDevices().ttl; got != valkeyTestTrustTTL {
		t.Errorf("test store trusted-device TTL = %v, want %v", got, valkeyTestTrustTTL)
	}
}

// TestMain is the exit sweep this package did not have. A failure to sweep is a
// test failure, not a warning: leaving the keys behind is precisely the defect,
// and a sweep that does not run is indistinguishable from one that deletes
// nothing.
func TestMain(m *testing.M) {
	code := m.Run()
	url := strings.TrimSpace(os.Getenv(testdb.ValkeyEnv))
	if url == "" {
		os.Exit(code)
	}
	s, err := NewSessions(url, valkeyTestOptions()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth: FAIL opening the test Valkey for the exit sweep: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	for _, sweep := range []struct {
		name string
		fn   func(context.Context) (int, error)
	}{
		{"session", func(ctx context.Context) (int, error) { return s.DeleteNamespaceKeys(ctx) }},
		{"trust", func(ctx context.Context) (int, error) { return s.TrustedDevices().DeleteNamespaceKeys(ctx) }},
	} {
		if _, err := sweep.fn(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "auth: FAIL %s-key sweep for namespace %q: %v\n", sweep.name, valkeyTestNamespace, err)
			if code == 0 {
				code = 1
			}
		}
	}
	_ = s.Close()
	os.Exit(code)
}
