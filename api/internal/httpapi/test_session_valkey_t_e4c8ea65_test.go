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
//
//   - `testSessionTTL` bounds the TTL of sessions minted through `Create` — the
//     only constructor that consults the store's default — and that is ALL it
//     bounds. The sessions these fixtures actually drive do not go through
//     `Create`: every login handler passes an explicit TTL. startSession mints
//     `rememberShortTTL` (24h) for an unticked remember box and
//     `rememberLongTTL` (14d) for a ticked one; the passkey path
//     (RESTHandlers.startSessionFor) always mints 14d. So a run KILLED before
//     TestMain — gate timeout, SIGKILL — leaves keys in its own namespace that
//     live up to 24h (up to 14d for a remember/passkey login) and then expire.
//     No later sweep reclaims them: the namespace carries the pid, and the next
//     process has a different one. They drain by expiry, not by the sweep, and
//     they cannot collide with anyone because they are namespaced.
//
//     Measured, not asserted in prose: TestAuthRememberMeUncheckedShortSession
//     (auth_ux_test.go) pins the 24h bound and
//     TestAuthRememberMeCheckedLongSession the 14d one, both against a store
//     built by testSessionOptions. The earlier version of this bullet called the
//     fixture TTL "minutes, not the production 14 days" — true of `Create`,
//     never true of the login-minted sessions the fixtures check (card
//     t_39e2b840).
//
//   - TestMain sweeps the namespace after m.Run(). It deletes nothing else: the
//     auth store's DeleteNamespaceKeys refuses an unscoped store, so a bug here
//     cannot degrade into the whole-keyspace sweep / FlushDB this suite removed
//     several cards ago.
//
// Nothing in this file writes a production-layout key, not even to prove the
// sweep spares one (card t_5588aaaa): the reach of the sweep is asserted from
// the SCAN patterns and DEL arguments it issues, because a production-layout key
// written by a test is residue the moment a run is killed before its cleanup —
// and a killed run is a normal event on this board (gate timeout, SIGTERM,
// dispatcher reclaim).
package httpapi

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/testdb"
)

// testSessionKeyNamespace is the slice of the shared test Valkey's session
// keyspace THIS process owns (e.g. "jftest:p48213"). It is built from
// testdb.Prefix() rather than re-deriving the format, so the test suite has one
// expression of "how a test process names its Valkey keys".
var testSessionKeyNamespace = strings.TrimSuffix(testdb.Prefix(), ":")

// testSessionTTL is the TTL a session minted through Create gets from a fixture
// store. It is NOT a cap on what the fixtures leave behind: login handlers pass
// an explicit TTL and never consult this default (see the package comment
// above), so a killed run's handler-minted keys drain by expiry — up to 24h
// later, or up to 14d for a remember/passkey login — rather than minutes.
const testSessionTTL = 15 * time.Minute

// testTrustTTL is the same rule for the trusted-device store (card t_ca742515):
// the production TTL is 30 days, which is longer still than a session's, and the
// trust fixtures mint 11-16 such keys per run. Minutes mean a killed run leaves
// nothing that outlives the next gate.
const testTrustTTL = 15 * time.Minute

// testSessionOptions returns opts plus the per-process session namespace and the
// short TTL Create applies. Every auth.NewSessions call in this package's tests
// that can MINT a session goes through it — a store built without the namespace
// writes the production `jf:session:<hash>` key onto the shared test Valkey,
// where the exit sweep cannot see it. The trust TTL rides along because
// TrustedDevices inherits both settings from the session store, so the same
// fixtures mint trusted-device keys under the same namespace (t_ca742515); it is
// a real bound there, unlike the session default — IssueDevice takes no TTL of
// its own and always mints with trustTTL(). The exceptions are named rather
// than implied, because the first version of this comment claimed there were
// none and auth_totp_test.go's `auth.NewSessions("valkey://localhost:6379/0")`
// sat behind it unswept (db 0, the production default index, +11 keys per
// `-run Test2FA`):
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
		auth.WithTrustTTL(testTrustTTL),
	)
}

// TestMain sweeps this process's namespaced auth keys after the tests have run:
// sessions, pending-2FA, step-up grants, password resets, magic links and the
// emailed-2FA code/pending flags (one DeleteNamespaceKeys call), plus the
// trusted-device families (their own sweep).
//
// A failure to sweep is a test failure, not a warning: silently leaving the keys
// behind is precisely the defect (t_e4c8ea65 for sessions, t_ca742515 for
// trusted devices, t_6004c826 for the derived stores), and a sweep that does not
// run is indistinguishable from one that deletes nothing.
func TestMain(m *testing.M) {
	// runShardedTests re-executes this binary as a few concurrent children (card
	// t_0d0af849): the suite is serial and single-core, so processes are the only
	// lever that scales it. It reports false when it did not shard, and then the
	// run is exactly the one this package has always had.
	code, sharded := runShardedTests()
	if !sharded {
		code = m.Run()
	}
	for _, sweep := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"session", sweepTestSessionKeys},
		{"trust", sweepTestTrustKeys},
	} {
		if err := sweep.fn(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: FAIL %s-key sweep for namespace %q: %v\n", sweep.name, testSessionKeyNamespace, err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// sweepTestSessionKeys deletes every key this process minted under its namespace
// — every family DeleteNamespaceKeys covers, not just `jf:session:` — and
// reports how many it removed. It is a no-op when the test Valkey is not
// configured (a unit-only run minted nothing) and it never touches a key outside
// the namespace.
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
	fmt.Fprintf(os.Stderr, "httpapi: reclaimed %d namespaced key(s) from namespace %q on %s\n",
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
//     the namespace it was given: a peer namespace's key survives, and no
//     pattern the sweep issues can reach a production-layout key.
//
// The second arm used to plant a production-layout key
// (`jf:session:<64ab>-<pid>`) with a one-hour TTL and assert the sweep spared
// it. That plant was the residue this file exists to stop writing: the only
// thing that removed it was t.Cleanup, so a run killed between the plant and
// the cleanup (gate timeout, SIGTERM, dispatcher reclaim) left a
// production-layout session key behind for an hour — measured on the pre-fix
// tree, SIGKILL left `jf:session:abab…ab-<pid>` at TTL 3,597 s (card
// t_5588aaaa). The reach is now asserted from the commands the sweep actually
// issues — its SCAN pattern and its DEL arguments — instead of from a key
// written to watch it survive, so the production layout is never written to
// find out.
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
	probe, err := auth.NewSessions(os.Getenv(testdb.ValkeyEnv),
		auth.WithKeyNamespace(probeNS), auth.WithDefaultTTL(time.Minute))
	if err != nil {
		t.Fatalf("probe store: %v", err)
	}
	// t.Cleanup, not defer: cleanups run LIFO AFTER the function returns, and the
	// keys planted/asserted below are deleted from this client. A defer would
	// close the connection first and leave the planted keys behind — the leak
	// this card is about, one layer down.
	t.Cleanup(func() { _ = probe.Close() })

	// A real PEER namespace: a session minted by the same store code under a
	// namespace the probe must never reach. Every name below carries this
	// process's pid: JOMFORM_TEST_VALKEY_URL is ONE database for the whole host
	// and the gate runs two lanes at a time, so a fixed name lets one run's
	// t.Cleanup delete the other's keys and red the gate on an innocent ref
	// (measured 9 of 16 concurrent runs; card t_4331ef33).
	peerNS := fmt.Sprintf("jftest:p%d:nsprobe-httpapi-peer", os.Getpid())
	peer, err := auth.NewSessions(os.Getenv(testdb.ValkeyEnv),
		auth.WithKeyNamespace(peerNS), auth.WithDefaultTTL(time.Minute))
	if err != nil {
		t.Fatalf("peer store: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	// The failure-path belt. Cleanups run LIFO, so this runs BEFORE the two Close
	// calls registered above. Nothing depends on it — the normal path sweeps both
	// namespaces synchronously below — and both namespaces are pid-unique, so
	// even a killed run leaves only `jf:session:jftest:p<pid>:…` keys with a
	// one-minute TTL, which the next run cannot confuse with its own and which
	// expire on their own.
	t.Cleanup(func() {
		_, _ = probe.DeleteNamespaceKeys(context.Background())
		_, _ = peer.DeleteNamespaceKeys(context.Background())
	})

	peerUser := fmt.Sprintf("namespace-probe-scope-peer-%d", os.Getpid())
	if _, err := peer.Create(ctx, peerUser); err != nil {
		t.Fatalf("create peer session: %v", err)
	}
	if _, err := probe.Create(ctx, "namespace-probe-scope"); err != nil {
		t.Fatalf("create probe session: %v", err)
	}
	nsPrefix := "jf:session:" + probeNS + ":"
	nsPattern := nsPrefix + "*"
	peerPattern := "jf:session:" + peerNS + ":*"
	if n := sessionsInNamespace(ctx, t, probe, nsPattern, "namespace-probe-scope"); n != 1 {
		t.Fatalf("probe session missing before the sweep: %d key(s) under %q carry its value, want 1", n, nsPattern)
	}
	if n := sessionsInNamespace(ctx, t, probe, peerPattern, peerUser); n != 1 {
		t.Fatalf("setup: the PEER namespace %q holds %d key(s), want exactly the one it minted — "+
			"the survival assertion below would be vacuous", peerPattern, n)
	}

	// Record what the sweep issues. Added AFTER the setup reads above (and to the
	// probe's client only), so the recorded commands are the sweep's alone.
	rec := &sessionSweepCommands{}
	probe.Client().AddHook(rec)

	deleted, err := probe.DeleteNamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("sweep probe namespace: %v", err)
	}
	if deleted != 1 {
		t.Errorf("the sweep deleted %d key(s) from its own namespace, want exactly the one session it minted", deleted)
	}

	// (a) The sweep may only SCAN its own namespace, across every key family.
	// Sessions hands out SEVEN families and DeleteNamespaceKeys sweeps all seven
	// (card t_6004c826); the roots are named here rather than read off the sweep,
	// and the comparison is a SET in both directions — a pattern outside them
	// means the sweep left its namespace, and a missing one means a family these
	// fixtures still write is no longer reclaimed (the residue this file exists to
	// stop), so it must red rather than narrow. SCAN is cursor-iterated, so the
	// same pattern can be issued more than once; what must hold is the set of
	// DISTINCT patterns. "jf:session:" is spelled out because it is the one root
	// auth keeps unexported (it is the production layout, not a switch).
	wantPatterns := []string{
		"jf:session:" + probeNS + ":*",
		auth.PendingTOTPKeyPrefix + probeNS + ":*",
		auth.StepUpKeyPrefix + probeNS + ":*",
		auth.PasswordResetKeyPrefix + probeNS + ":*",
		auth.MagicLinkKeyPrefix + probeNS + ":*",
		auth.Email2FACodeKeyPrefix + probeNS + ":*",
		auth.Email2FAPendingPrefix + probeNS + ":*",
	}
	want := make(map[string]bool, len(wantPatterns))
	for _, p := range wantPatterns {
		want[p] = true
	}
	seen := map[string]bool{}
	for _, pattern := range rec.patterns {
		seen[pattern] = true
		if !want[pattern] {
			t.Errorf("the sweep scanned %q, which is outside this process's namespace %q — the seven family patterns are %v", pattern, probeNS, wantPatterns)
		}
	}
	for _, p := range wantPatterns {
		if !seen[p] {
			t.Errorf("the sweep never scanned %q — one of the seven key families these fixtures write is no longer reclaimed (want all of %v, saw %v)", p, wantPatterns, seen)
		}
	}

	// (b) ...and may only DEL keys inside it.
	if len(rec.deleted) == 0 {
		t.Fatal("the sweep DELeted nothing — the assertion below would be vacuous")
	}
	for _, key := range rec.deleted {
		if !strings.HasPrefix(key, nsPrefix) {
			t.Errorf("the sweep DELeted %q, which is outside its namespace %q — a test must never delete a key it did not write", key, probeNS)
		}
	}

	// (c) no FLUSHDB/KEYS: the whole-keyspace commands this suite removed.
	for _, name := range rec.other {
		switch name {
		case "flushdb", "flushall", "keys":
			t.Errorf("the sweep issued %q — it must only SCAN/DEL its own namespace", name)
		}
	}

	// (d) The property the deleted arm used to demonstrate at the server: no
	// pattern the sweep issued can match a production-layout session name, so a
	// live API's login is out of reach without ever writing one. Both layouts are
	// covered: the hashed key (`jf:session:<sha256>`) and the pre-hash legacy key,
	// which is the raw 64-hex token under the same root.
	productionKeys := []string{
		"jf:session:" + strings.Repeat("a", 64),
		"jf:session:" + strings.Repeat("0123456789abcdef", 4),
	}
	for _, pk := range productionKeys {
		for pattern := range seen {
			if ok, err := path.Match(pattern, pk); err == nil && ok {
				t.Errorf("sweep pattern %q matches the production-layout key %q — the sweep could delete a live API's login", pattern, pk)
			}
		}
	}

	if n := sessionsInNamespace(ctx, t, probe, nsPattern, "namespace-probe-scope"); n != 0 {
		t.Errorf("the probe's own session key survived its namespace sweep (%d key(s) still under %q)", n, nsPattern)
	}

	// The peer arm, asserted at the server: a real peer namespace's session is
	// still there after the probe's sweep.
	if n := sessionsInNamespace(ctx, t, probe, peerPattern, peerUser); n != 1 {
		t.Errorf("a PEER namespace's session key was swept (%d left, want 1) — one process must never "+
			"delete a key it did not write", n)
	}

	// The normal path cleans the peer up here rather than in the exit hook, so a
	// test that reaches this line leaves nothing even if the process is killed
	// between it and TestMain.
	if _, err := peer.DeleteNamespaceKeys(ctx); err != nil {
		t.Errorf("sweep the peer namespace: %v", err)
	}
	if n := sessionsInNamespace(ctx, t, probe, peerPattern, peerUser); n != 0 {
		t.Errorf("the peer's session key survived its own namespace sweep (%d still under %q)", n, peerPattern)
	}
}

// sessionSweepCommands records the keyspace commands a sweep issues, so a test
// can assert its REACH without writing a production-layout key to find out.
type sessionSweepCommands struct {
	patterns []string
	deleted  []string
	other    []string
}

func (h *sessionSweepCommands) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h *sessionSweepCommands) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (h *sessionSweepCommands) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		switch strings.ToLower(cmd.Name()) {
		case "scan":
			for i := 1; i+1 < len(args); i++ {
				if strings.EqualFold(fmt.Sprint(args[i]), "match") {
					h.patterns = append(h.patterns, fmt.Sprint(args[i+1]))
				}
			}
		case "del":
			for _, a := range args[1:] {
				h.deleted = append(h.deleted, fmt.Sprint(a))
			}
		default:
			h.other = append(h.other, strings.ToLower(cmd.Name()))
		}
		return next(ctx, cmd)
	}
}
