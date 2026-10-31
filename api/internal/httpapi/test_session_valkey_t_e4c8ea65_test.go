// Per-process session key namespace for the httpapi test binary (card
// t_e4c8ea65).
//
// The shared test Valkey (JOMFORM_TEST_VALKEY_URL, db 9) had ~98k `jf:session:*`
// keys — 95% of that database — and nothing removed them. Every fixture in this
// package that needs a login mints a session through auth.Sessions, which wrote
// the PRODUCTION key name with the production TTL (14 days); the test process
// exits in seconds and the key lives on. The only reason the DB was not larger
// is expiry, and the only reason a lane could not clean up after itself is that
// the keys were indistinguishable from every other run's: finding your own key
// meant scanning the whole keyspace.
//
// The fix is the same ownership rule testdb already applies to queue keys
// (`jftest:p<pid>:`), applied to sessions: this process writes under its own
// namespace, and at exit it deletes exactly that slice.
//
//   - `testSessionKeyNamespace` is derived from the pid through testdb.Prefix(),
//     so two concurrent checkouts — and two packages of one `go test ./...` —
//     never share a namespace.
//   - `testSessionTTL` (minutes, not the production 14 days) is belt to the sweep's
//     braces: a run KILLED before TestMain (gate timeout, SIGKILL) leaves keys
//     that expire on their own instead of week-long ones.
//   - TestMain sweeps the namespace after m.Run(). It deletes nothing else: the
//     auth store's DeleteNamespaceKeys refuses an unscoped store, so a bug here
//     cannot degrade into the whole-keyspace sweep / FlushDB this suite removed
//     several cards ago.
package httpapi

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

// testSessionKeyNamespace is the slice of the shared test Valkey's session
// keyspace THIS process owns (e.g. "jftest:p48213"). It is built from
// testdb.Prefix() rather than re-deriving the format, so the test suite has one
// expression of "how a test process names its Valkey keys".
var testSessionKeyNamespace = strings.TrimSuffix(testdb.Prefix(), ":")

// testSessionTTL is what a fixture-minted session lives for. Minutes, because
// nothing in this package needs a session to outlive its own test, and because a
// killed run must not be able to leave a 14-day key behind.
const testSessionTTL = 15 * time.Minute

// testSessionOptions returns opts plus the per-process session namespace and the
// short test TTL. Every auth.NewSessions call in this package's tests that can
// MINT a session goes through it — a store built without the namespace writes the
// production `jf:session:<hash>` key onto the shared test Valkey, where the exit
// sweep cannot see it. The exceptions are named rather than implied, because the
// first version of this comment claimed there were none and auth_totp_test.go's
// `auth.NewSessions("valkey://localhost:6379/0")` sat behind it unswept (db 0,
// the production default index, +11 keys per `-run Test2FA`):
//
//   - this file's own sweep (sweepTestSessionKeys) and the peer-namespace probe in
//     TestTestSessionsLiveInTheProcessNamespaceAndTheSweepIsScoped build their
//     store with auth.WithKeyNamespace explicitly: the first IS the sweep, the
//     second has to contrast against a namespace that is not this process's;
//   - auth_hardening_isolation_test.go's TestReclaimLeavesPeersLoginCountersAlone
//     opens an unscoped store only to reach .Client() — a raw connection for
//     seeding and deleting its own peer sentinel. It mints no session.
//
// TestEverySessionStoreOwnsItsKeyspace (session_store_guard_t_05dd8cbd_test.go)
// holds the invariant at source level: a new unscoped call fails the package
// instead of becoming the next fixture nobody swept.
func testSessionOptions(opts ...auth.SessionOption) []auth.SessionOption {
	return append(opts,
		auth.WithKeyNamespace(testSessionKeyNamespace),
		auth.WithDefaultTTL(testSessionTTL),
	)
}

// TestMain sweeps this process's session keys after the tests have run.
//
// A failure to sweep is a test failure, not a warning: silently leaving the keys
// behind is precisely the defect (t_e4c8ea65), and a sweep that does not run is
// indistinguishable from one that deletes nothing.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := sweepTestSessionKeys(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "httpapi: FAIL session-key sweep for namespace %q: %v\n", testSessionKeyNamespace, err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// sweepTestSessionKeys deletes every session key this process minted and reports
// how many it removed. It is a no-op when the test Valkey is not configured (a
// unit-only run minted nothing) and it never touches a key outside the
// namespace.
func sweepTestSessionKeys(ctx context.Context) error {
	url := strings.TrimSpace(os.Getenv(testdb.ValkeyEnv))
	if url == "" {
		return nil
	}
	s, err := auth.NewSessions(url, auth.WithKeyNamespace(testSessionKeyNamespace))
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	deleted, err := s.DeleteNamespaceKeys(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "httpapi: reclaimed %d session key(s) from namespace %q on %s\n",
		deleted, testSessionKeyNamespace, testdb.ValkeyEnv)
	return nil
}

// sessionsInNamespace counts the keys under pattern whose VALUE carries
// valuePrefix as its user id (a session value is `userID:issued:remember:magic`).
// A guard that has to prove WHERE a session landed finds it by value, because
// this package deliberately does not rebuild auth's key layout — the point of the
// namespace is that auth owns the names, not that every test knows them.
func sessionsInNamespace(ctx context.Context, t *testing.T, s *auth.Sessions, pattern, valuePrefix string) int {
	t.Helper()
	found := 0
	var cursor uint64
	for {
		keys, next, err := s.Client().Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			t.Fatalf("scan %q: %v", pattern, err)
		}
		for _, k := range keys {
			v, err := s.Client().Get(ctx, k).Result()
			if err != nil {
				continue
			}
			if strings.HasPrefix(v, valuePrefix+":") {
				found++
			}
		}
		cursor = next
		if cursor == 0 {
			return found
		}
	}
}

// TestTestSessionsLiveInTheProcessNamespaceAndTheSweepIsScoped is the guard for
// the mechanism above; without it the fixtures could quietly go back to the
// production layout and the sweep would still "pass" by deleting nothing.
//
// Two halves:
//
//  1. a session minted through the package fixture (authTestValkey, the real
//     wiring every login test uses) is under THIS process's namespace; and
//  2. DeleteNamespaceKeys — the same call TestMain makes — removes only keys in
//     the namespace it was given: a peer namespace's key and a production-layout
//     key both survive.
func TestTestSessionsLiveInTheProcessNamespaceAndTheSweepIsScoped(t *testing.T) {
	ctx := context.Background()
	sess := authTestValkey(t)

	// ── 1. the fixture's sessions are namespaced ─────────────────────────────
	userID := fmt.Sprintf("namespace-probe-%d", os.Getpid())
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Destroy(context.Background(), token) })

	pattern := "jf:session:" + testSessionKeyNamespace + ":*"
	if found := sessionsInNamespace(ctx, t, sess, pattern, userID); found != 1 {
		t.Fatalf("the fixture's session sits under %q %d time(s), want exactly 1: a session minted under the "+
			"production `jf:session:<hash>` layout is the leak (t_e4c8ea65) — it outlives this process by %v and "+
			"no run can tell it apart from another run's", pattern, found, auth.SessionTTL)
	}

	// ── 2. the sweep is scoped ───────────────────────────────────────────────
	probeNS := fmt.Sprintf("jftest:p%d:nsprobe-httpapi", os.Getpid())
	probe, err := auth.NewSessions(os.Getenv(testdb.ValkeyEnv), auth.WithKeyNamespace(probeNS))
	if err != nil {
		t.Fatalf("probe store: %v", err)
	}
	// t.Cleanup, not defer: cleanups run LIFO AFTER the function returns, and the
	// keys planted/asserted below are deleted from this client. A defer would
	// close the connection first and leave the planted keys behind — the leak
	// this card is about, one layer down.
	t.Cleanup(func() { _ = probe.Close() })

	peerKey := "jf:session:jftest:p1:peer-session-probe"
	prodKey := "jf:session:" + strings.Repeat("ab", 32) // the production layout: bare 64-hex hash
	for _, k := range []string{peerKey, prodKey} {
		if err := probe.Client().Set(ctx, k, "probe", time.Hour).Err(); err != nil {
			t.Fatalf("plant %s: %v", k, err)
		}
		t.Cleanup(func() { _ = probe.Client().Del(context.Background(), k) })
	}

	if _, err := probe.Create(ctx, "namespace-probe-scope"); err != nil {
		t.Fatalf("create probe session: %v", err)
	}
	nsPattern := "jf:session:" + probeNS + ":*"
	if n := sessionsInNamespace(ctx, t, probe, nsPattern, "namespace-probe-scope"); n != 1 {
		t.Fatalf("probe session missing before the sweep: %d key(s) under %q carry its value, want 1", n, nsPattern)
	}

	deleted, err := probe.DeleteNamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("sweep probe namespace: %v", err)
	}
	if deleted < 1 {
		t.Errorf("the sweep deleted %d key(s) from its own namespace, want at least the session it minted", deleted)
	}
	if n := sessionsInNamespace(ctx, t, probe, nsPattern, "namespace-probe-scope"); n != 0 {
		t.Errorf("the probe's own session key survived its namespace sweep (%d key(s) still under %q)", n, nsPattern)
	}
	if n, _ := probe.Client().Exists(ctx, peerKey).Result(); n != 1 {
		t.Errorf("a PEER namespace's session key was swept (exists=%d) — one process must never delete another's key", n)
	}
	if n, _ := probe.Client().Exists(ctx, prodKey).Result(); n != 1 {
		t.Errorf("a production-layout session key was swept (exists=%d) — that is a live API's login, the FlushDB this suite removed", n)
	}
}
