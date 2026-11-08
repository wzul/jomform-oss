// Per-process TRUSTED-DEVICE key namespace for the httpapi test binary (card
// t_ca742515).
//
// The same suite that leaked `jf:session:*` keys (t_e4c8ea65) also left
// `jf:trust:*`, `jf:trustset:*` and `jf:trustmeta:*` behind, with the
// PRODUCTION 30-day TTL. Measured on a DB index that started empty, one
// `go test ./internal/httpapi -count=1` run added 11-16 of them and removed
// none: the fixtures mint a trust token (directly through TrustedDevices, or
// through a login with TOTPRemember), the process exits, and the keys live on
// for a month.
//
// The ownership rule is the one the session keyspace already uses: this process
// writes its trust keys under `jf:trust:<ns>:`, `jf:trustset:<ns>:` and
// `jf:trustmeta:<ns>:` — the namespace derived from testdb.Prefix() — and
// TestMain deletes exactly that slice. The store REFUSES to sweep without a
// namespace, which is the property that keeps this from ever becoming a
// whole-keyspace delete of a live API's trusted devices.
//
// Nothing in this file writes a production-layout key, not even to prove the
// sweep spares one (card t_004b1769): the reach of the sweep is asserted from
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

// countKeys counts the keys under pattern with SCAN (never KEYS: the shared test
// DB is being served while this runs).
func countKeys(t *testing.T, c *goredis.Client, pattern string) int {
	t.Helper()
	n := 0
	var cursor uint64
	for {
		keys, next, err := c.Scan(context.Background(), cursor, pattern, 500).Result()
		if err != nil {
			t.Fatalf("scan %q: %v", pattern, err)
		}
		n += len(keys)
		cursor = next
		if cursor == 0 {
			return n
		}
	}
}

// sweepTestTrustKeys deletes every trusted-device key this process minted and
// reports how many it removed. It is a no-op when the test Valkey is not
// configured (a unit-only run minted nothing) and it never touches a key outside
// the namespace — including the per-user SETs and metadata hashes, which are
// separate key families from the token keys.
func sweepTestTrustKeys(ctx context.Context) error {
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
	deleted, err := s.TrustedDevices().DeleteNamespaceKeys(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "httpapi: reclaimed %d trusted-device key(s) from namespace %q on %s\n",
		deleted, testSessionKeyNamespace, testdb.ValkeyEnv)
	return nil
}

// TestTestTrustKeysLiveInTheProcessNamespaceAndTheSweepIsScoped is the guard for
// the mechanism above: without it a fixture could go back to minting the
// production trust keys and the TestMain sweep would still "pass" by deleting
// nothing.
//
// Two halves:
//
//  1. a trust token minted through the package fixture (authTestValkey, the real
//     wiring the login and 2FA tests use) lands under THIS process's namespace,
//     not the bare production layout; and
//  2. TrustedDevices.DeleteNamespaceKeys — the call TestMain makes — removes
//     only the namespace it was given: a peer namespace's keys are still there
//     afterwards AND no pattern it issued can reach a production-layout key.
//
// The second arm used to plant a production-layout key (and its set/meta) with a
// one-hour TTL and assert the sweep spared it. That plant was the residue this
// file exists to stop writing: the only thing that removed it was t.Cleanup, so
// a run killed between the plant and the cleanup (gate timeout, SIGTERM,
// dispatcher reclaim) left three production-layout trust keys behind for an
// hour — measured on the pre-fix tree, SIGKILL left
// `jf:trust:cdcd…cd-<pid>` at TTL 3,592 s plus `jf:trustset:prod-set-probe-<pid>`
// and `jf:trustmeta:prod-meta-probe-<pid>` (card t_004b1769).
//
// So the reach of the sweep is asserted from the commands it actually issues —
// the SCAN patterns and the DEL arguments — instead of from a key written to
// watch it survive. The production layout is never written to find out, and a
// killed run can only leave keys under this process's namespace with a
// one-minute TTL.
func TestTestTrustKeysLiveInTheProcessNamespaceAndTheSweepIsScoped(t *testing.T) {
	ctx := context.Background()
	sess := authTestValkey(t)

	// ── 1. the fixture's trust entries are namespaced ────────────────────────
	userID := fmt.Sprintf("trust-namespace-probe-%d", os.Getpid())
	token, err := sess.TrustedDevices().IssueDevice(ctx, userID, "probe-ua", "10.0.0.1")
	if err != nil {
		t.Fatalf("issue trust token through the fixture: %v", err)
	}
	hash := sess.TrustedDevices().HashFor(token)

	nsPrefixes := []string{
		"jf:trust:" + testSessionKeyNamespace + ":" + hash,
		"jf:trustset:" + testSessionKeyNamespace + ":" + userID,
		"jf:trustmeta:" + testSessionKeyNamespace + ":" + userID,
	}
	barePrefixes := []string{"jf:trust:" + hash, "jf:trustset:" + userID, "jf:trustmeta:" + userID}
	for _, k := range nsPrefixes {
		if n, err := sess.Client().Exists(ctx, k).Result(); err != nil || n != 1 {
			t.Fatalf("the fixture's trust key %q is missing (exists=%d err=%v); it must live under the process "+
				"namespace %q — a key under the production layout is the leak (t_ca742515): it outlives this "+
				"process by %v and no run can tell it apart from another run's",
				k, n, err, testSessionKeyNamespace, auth.TrustTTL)
		}
	}
	for _, k := range barePrefixes {
		if n, err := sess.Client().Exists(ctx, k).Result(); err != nil || n != 0 {
			t.Errorf("the fixture wrote a PRODUCTION-layout trust key %q (exists=%d err=%v)", k, n, err)
		}
	}

	// ── 2. the sweep is scoped ───────────────────────────────────────────────
	probeNS := fmt.Sprintf("jftest:p%d:nsprobe-trust-httpapi", os.Getpid())
	probe, err := auth.NewSessions(os.Getenv(testdb.ValkeyEnv), auth.WithKeyNamespace(probeNS), auth.WithTrustTTL(time.Minute))
	if err != nil {
		t.Fatalf("probe store: %v", err)
	}
	// t.Cleanup, not defer: cleanups run LIFO AFTER the function returns, and the
	// keys asserted below are deleted from these clients. A defer would close the
	// connection first and leave the keys behind — the leak this card is about,
	// one layer down.
	t.Cleanup(func() { _ = probe.Close() })

	// A real PEER namespace: the same three key families, minted by the same
	// store code the probe runs, under a namespace the probe must not reach.
	peerNS := fmt.Sprintf("jftest:p%d:nsprobe-trust-httpapi-peer", os.Getpid())
	peer, err := auth.NewSessions(os.Getenv(testdb.ValkeyEnv), auth.WithKeyNamespace(peerNS), auth.WithTrustTTL(time.Minute))
	if err != nil {
		t.Fatalf("peer store: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	probeTD, peerTD := probe.TrustedDevices(), peer.TrustedDevices()

	// The failure-path belt. Cleanups run LIFO, so this runs BEFORE the two Close
	// calls registered above. Nothing depends on it — the normal path sweeps both
	// namespaces synchronously below — and both are namespaced, so even a killed
	// run leaves only `jf:trust*:jftest:p<pid>:…` keys with a one-minute TTL.
	t.Cleanup(func() {
		_, _ = probeTD.DeleteNamespaceKeys(context.Background())
		_, _ = peerTD.DeleteNamespaceKeys(context.Background())
	})

	// Every key below carries this process's pid inside its namespace.
	// JOMFORM_TEST_VALKEY_URL is ONE database for the whole host and
	// scripts/preflight.sh gates two lanes at a time, so a fixed name makes two
	// concurrent runs mint and then DELETE each other's probes (card t_4331ef33,
	// measured 11 of 16 concurrent runs red).
	pid := os.Getpid()
	ownUser := fmt.Sprintf("trust-sweep-probe-%d", pid)
	if _, err := probeTD.Issue(ctx, ownUser); err != nil {
		t.Fatalf("create probe trust entry: %v", err)
	}
	peerUser := fmt.Sprintf("trust-sweep-probe-peer-%d", pid)
	if _, err := peerTD.Issue(ctx, peerUser); err != nil {
		t.Fatalf("create peer trust entry: %v", err)
	}

	// The stores' keys, named by PATTERN rather than by rebuilding auth's key
	// layout: the point of the namespace is that auth owns the names.
	ownPatterns := []string{
		auth.TrustKeyPrefix + probeNS + ":*",
		auth.TrustSetKeyPrefix + probeNS + ":*",
		auth.TrustMetaKeyPrefix + probeNS + ":*",
	}
	peerPatterns := []string{
		auth.TrustKeyPrefix + peerNS + ":*",
		auth.TrustSetKeyPrefix + peerNS + ":*",
		auth.TrustMetaKeyPrefix + peerNS + ":*",
	}
	for _, pattern := range ownPatterns {
		if n := countKeys(t, probe.Client(), pattern); n != 1 {
			t.Fatalf("setup: %q holds %d key(s), want exactly the one the probe minted", pattern, n)
		}
	}
	for _, pattern := range peerPatterns {
		if n := countKeys(t, probe.Client(), pattern); n != 1 {
			t.Fatalf("setup: the PEER namespace %q holds %d key(s), want exactly the one it minted — "+
				"the survival assertion below would be vacuous", pattern, n)
		}
	}

	// Record what the sweep issues. Added AFTER the setup reads (and to the
	// probe's client only), so the recorded commands are the sweep's alone.
	rec := &trustSweepCommands{}
	probe.Client().AddHook(rec)

	deleted, err := probeTD.DeleteNamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("sweep probe namespace: %v", err)
	}
	if deleted != 3 {
		t.Errorf("the sweep deleted %d key(s) from its own namespace, want exactly the 3 (token + set + meta) it minted", deleted)
	}

	// The sweep may only SCAN its own namespace, one pattern per key family.
	// SCAN is cursor-iterated, so the same pattern can be issued more than once;
	// what must hold is that every issued pattern IS one of the three and that
	// all three were issued (otherwise the reach assertions below are vacuous).
	wantPatterns := map[string]bool{ownPatterns[0]: true, ownPatterns[1]: true, ownPatterns[2]: true}
	seen := map[string]bool{}
	for _, pattern := range rec.patterns {
		seen[pattern] = true
		if !wantPatterns[pattern] {
			t.Errorf("the sweep scanned %q, which is not one of its own namespaced patterns %v", pattern, ownPatterns)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("the sweep issued %d distinct SCAN pattern(s) %v, want one per key family %v", len(seen), seen, ownPatterns)
	}

	// ...and may only DEL keys inside it.
	if len(rec.deleted) == 0 {
		t.Fatal("the sweep DELeted nothing — the assertion below would be vacuous")
	}
	inNamespace := func(key string) bool {
		for _, root := range []string{auth.TrustKeyPrefix, auth.TrustSetKeyPrefix, auth.TrustMetaKeyPrefix} {
			if strings.HasPrefix(key, root+probeNS+":") {
				return true
			}
		}
		return false
	}
	for _, key := range rec.deleted {
		if !inNamespace(key) {
			t.Errorf("the sweep DELeted %q, which is outside its namespace %q — a test must never delete a key it did not write", key, probeNS)
		}
	}
	for _, name := range rec.other {
		switch name {
		case "flushdb", "flushall", "keys":
			t.Errorf("the sweep issued %q — it must only SCAN/DEL its own namespace", name)
		}
	}

	// The property the deleted arm used to demonstrate at the server: no pattern
	// the sweep issued can match a production-layout key name, so a live API's
	// trusted device is out of reach without ever writing one.
	productionKeys := []string{
		auth.TrustKeyPrefix + strings.Repeat("a", 64),
		auth.TrustSetKeyPrefix + "a-production-layout-user",
		auth.TrustMetaKeyPrefix + "a-production-layout-user",
	}
	for _, pk := range productionKeys {
		for pattern := range seen {
			if ok, err := path.Match(pattern, pk); err == nil && ok {
				t.Errorf("sweep pattern %q matches the production-layout key %q — the sweep could delete a live API's trusted device", pattern, pk)
			}
		}
	}

	for _, pattern := range ownPatterns {
		if n := countKeys(t, probe.Client(), pattern); n != 0 {
			t.Errorf("%q still holds %d key(s) after its namespace sweep — the SET and metadata hash are separate "+
				"families and a token-only sweep leaves them behind", pattern, n)
		}
	}

	// The peer arm, asserted at the server: a real peer namespace's three key
	// families are all still there after the probe's sweep.
	for _, pattern := range peerPatterns {
		if n := countKeys(t, probe.Client(), pattern); n != 1 {
			t.Errorf("a PEER namespace's key under %q was swept (%d left, want 1) — one process must never "+
				"delete a key it did not write", pattern, n)
		}
	}

	// The normal path cleans the peer up here rather than in the exit hook, so a
	// test that reaches this line leaves nothing even if the process is killed
	// between it and TestMain.
	if _, err := peerTD.DeleteNamespaceKeys(ctx); err != nil {
		t.Errorf("sweep the peer namespace: %v", err)
	}
	for _, pattern := range peerPatterns {
		if n := countKeys(t, probe.Client(), pattern); n != 0 {
			t.Errorf("peer keys survived their own sweep under %q (%d left)", pattern, n)
		}
	}
}

// trustSweepCommands records the keyspace commands a sweep issues, so a test can
// assert its REACH without writing a production-layout key to find out.
type trustSweepCommands struct {
	patterns []string
	deleted  []string
	other    []string
}

func (h *trustSweepCommands) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h *trustSweepCommands) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (h *trustSweepCommands) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
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
