package mcpsrv

// Per-process Valkey key namespace for the mcpsrv test binary (card t_ca742515).
//
// This package's 2FA tests are the only MCP-side fixtures that mint
// trusted-device records: `disable_2fa` must revoke an account's trusted
// devices, so the tests arm them with TrustedDevices().IssueDevice and count
// them back. They used to build the store as `auth.NewSessions(valkeyURL)` —
// the PRODUCTION key layout with the production 30-day TTL — on a Valkey DB
// shared by every checkout, and nothing deleted the keys: the process exits,
// the `jf:trust:*` / `jf:trustset:*` / `jf:trustmeta:*` keys live on for a
// month.
//
// The rule is the one the session keyspace already applies (t_e4c8ea65): the
// process writes under its own namespace (testdb.Prefix(), e.g.
// `jftest:p48213`) and TestMain deletes exactly that slice from both stores.
// The session store's TTL is shortened too, so a run killed before its sweep
// leaves minutes, not weeks.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/testdb"
)

// testValkeyNamespace is the slice of the shared test Valkey THIS process owns.
// It is built from testdb.Prefix() rather than re-deriving the format, so the
// suite has one expression of "how a test process names its Valkey keys".
var testValkeyNamespace = strings.TrimSuffix(testdb.Prefix(), ":")

const (
	// testValkeySessionTTL / testValkeyTrustTTL are what fixture-minted keys live
	// for. Minutes, because nothing here needs a credential to outlive its own
	// test, and because a killed run must not be able to leave a 14-day session
	// or a 30-day trusted device behind.
	testValkeySessionTTL = 15 * time.Minute
	testValkeyTrustTTL   = 15 * time.Minute
)

// testValkeyOptions is what every auth.NewSessions call in this package's tests
// passes: the per-process namespace plus the short test TTLs. A store built
// without the namespace mints keys the exit sweep cannot see.
func testValkeyOptions(opts ...auth.SessionOption) []auth.SessionOption {
	return append(opts,
		auth.WithKeyNamespace(testValkeyNamespace),
		auth.WithDefaultTTL(testValkeySessionTTL),
		auth.WithTrustTTL(testValkeyTrustTTL),
	)
}

// TestMain sweeps this process's session AND trusted-device keys after the tests
// have run.
//
// A failure to sweep is a test failure, not a warning: silently leaving the keys
// behind is precisely the defect (t_ca742515 for trusted devices, t_e4c8ea65 for
// sessions), and a sweep that does not run is indistinguishable from one that
// deletes nothing.
func TestMain(m *testing.M) {
	code := m.Run()
	url := strings.TrimSpace(os.Getenv(testdb.ValkeyEnv))
	if url != "" {
		for _, sweep := range []struct {
			name string
			fn   func(context.Context, *auth.Sessions) (int, error)
		}{
			{"session", func(ctx context.Context, s *auth.Sessions) (int, error) { return s.DeleteNamespaceKeys(ctx) }},
			{"trust", func(ctx context.Context, s *auth.Sessions) (int, error) {
				return s.TrustedDevices().DeleteNamespaceKeys(ctx)
			}},
		} {
			if err := runTestValkeySweep(url, sweep.name, sweep.fn); err != nil {
				fmt.Fprintf(os.Stderr, "mcpsrv: FAIL %s-key sweep for namespace %q: %v\n", sweep.name, testValkeyNamespace, err)
				if code == 0 {
					code = 1
				}
			}
		}
	}
	os.Exit(code)
}

// runTestValkeySweep opens a store scoped to this process's namespace and runs
// one sweep on it. The store REFUSES an unscoped sweep, which is the property
// that keeps this from ever becoming a whole-keyspace delete of a live API's
// keys.
func runTestValkeySweep(url, name string, sweep func(context.Context, *auth.Sessions) (int, error)) error {
	s, err := auth.NewSessions(url, auth.WithKeyNamespace(testValkeyNamespace))
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	deleted, err := sweep(ctx, s)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "mcpsrv: reclaimed %d %s key(s) from namespace %q on %s\n", deleted, name, testValkeyNamespace, testdb.ValkeyEnv)
	return nil
}

// TestMcpsrvValkeyFixtureWritesTrustKeysUnderTheProcessNamespace is the guard for
// the wiring above: the 2FA fixtures must not go back to minting production
// trusted-device keys, or TestMain's sweep would still "pass" by deleting
// nothing while the keys accumulate for 30 days.
func TestMcpsrvValkeyFixtureWritesTrustKeysUnderTheProcessNamespace(t *testing.T) {
	url := strings.TrimSpace(os.Getenv(testdb.ValkeyEnv))
	if url == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; the trust set lives in Valkey")
	}
	sess, err := auth.NewSessions(url, testValkeyOptions()...)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	td := sess.TrustedDevices()
	t.Cleanup(func() { _, _ = td.DeleteNamespaceKeys(context.Background()) })

	ctx := context.Background()
	userID := fmt.Sprintf("trust-namespace-probe-%d", os.Getpid())
	token, err := td.IssueDevice(ctx, userID, "probe-ua", "10.0.0.1")
	if err != nil {
		t.Fatalf("issue trust token: %v", err)
	}
	hash := td.HashFor(token)

	for _, k := range []string{
		"jf:trust:" + testValkeyNamespace + ":" + hash,
		"jf:trustset:" + testValkeyNamespace + ":" + userID,
		"jf:trustmeta:" + testValkeyNamespace + ":" + userID,
	} {
		if n, err := sess.Client().Exists(ctx, k).Result(); err != nil || n != 1 {
			t.Fatalf("the fixture's trust key %q is missing (exists=%d err=%v); it must live under the process "+
				"namespace %q — a key under the production layout is the leak (t_ca742515): it outlives this "+
				"process by %v", k, n, err, testValkeyNamespace, auth.TrustTTL)
		}
	}
	for _, k := range []string{"jf:trust:" + hash, "jf:trustset:" + userID, "jf:trustmeta:" + userID} {
		if n, err := sess.Client().Exists(ctx, k).Result(); err != nil || n != 0 {
			t.Errorf("the fixture wrote a PRODUCTION-layout trust key %q (exists=%d err=%v)", k, n, err)
		}
	}
}
