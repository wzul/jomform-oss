// Trusted-device key NAMESPACE tests (card t_ca742515).
//
// The shared test Valkey accumulated `jf:trust:*`, `jf:trustset:*` and
// `jf:trustmeta:*` keys with the PRODUCTION 30-day TTL: every test fixture that
// minted a trust token wrote the production key name and the process that wrote
// it exited seconds later, so nothing deleted it. Measured on a DB index that
// started empty, one `go test ./internal/httpapi -count=1` run added 11-16 of
// them; db 9 held 151 / 114 / 114 at filing time.
//
// `TrustedDevices` rides on the same client (and therefore the same
// WithKeyNamespace) as sessions, but it is a different store with three key
// families, so the scoping rule has to be pinned for all three:
//
//  1. the namespace is part of every family's PREFIX, so a sweep built from it
//     cannot reach a peer's keys or production-layout keys (a whole-prefix
//     sweep deletes a live API's trusted devices);
//  2. an unscoped store REFUSES to sweep at all; and
//  3. the SET (`jf:trustset:`) is deleted as a whole, so it cannot linger as an
//     empty set after its member token keys are gone.
//
// The Valkey-backed half skips without JOMFORM_TEST_VALKEY_URL, matching the
// convention in this package. Every key it writes is under a per-test namespace
// with a short TTL and is deleted in the test; NOTHING here mints a
// production-layout key, not even to prove the sweep spares one — that proof is
// read off the sweep's own SCAN/DEL commands (card t_15694421), because a
// production-layout key written by a test is residue the moment the run is
// killed before its cleanup.
package auth

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// scanPattern returns every key under pattern. SCAN, not KEYS: the shared test
// DB is being served while this runs.
func scanPattern(t *testing.T, c *goredis.Client, pattern string) []string {
	t.Helper()
	var out []string
	var cursor uint64
	for {
		keys, next, err := c.Scan(context.Background(), cursor, pattern, 500).Result()
		if err != nil {
			t.Fatalf("scan %q: %v", pattern, err)
		}
		out = append(out, keys...)
		cursor = next
		if cursor == 0 {
			return out
		}
	}
}

// TestTrustKeyPrefixesAreUnchangedInProduction pins that the namespacing change
// is invisible to a running API: with no namespace the three key builders emit
// exactly the production names they always did.
func TestTrustKeyPrefixesAreUnchangedInProduction(t *testing.T) {
	td := &TrustedDevices{}
	if got, want := td.trustKeyPrefix(), TrustKeyPrefix; got != want {
		t.Errorf("production trust key prefix = %q, want %q", got, want)
	}
	if got, want := td.trustSetPrefix(), TrustSetKeyPrefix; got != want {
		t.Errorf("production trust set prefix = %q, want %q", got, want)
	}
	if got, want := td.trustMetaPrefix(), TrustMetaKeyPrefix; got != want {
		t.Errorf("production trust meta prefix = %q, want %q", got, want)
	}
	if got, want := td.trustTTL(), TrustTTL; got != want {
		t.Errorf("production trust TTL = %v, want %v", got, want)
	}
	// A zero/negative override must never mint an immortal or already-expired
	// entry.
	if got := (&TrustedDevices{ttl: -time.Minute}).trustTTL(); got != TrustTTL {
		t.Errorf("negative trust TTL override = %v, want the TrustTTL fallback", got)
	}

	// And a namespaced store puts the namespace after EACH family prefix, so the
	// three families never collide with one another.
	ns := "jftest:p1"
	scoped := &TrustedDevices{namespace: ns}
	for fam, got := range map[string]string{
		"token": scoped.trustKeyPrefix(),
		"set":   scoped.trustSetPrefix(),
		"meta":  scoped.trustMetaPrefix(),
	} {
		if !strings.HasSuffix(got, ns+":") {
			t.Errorf("%s prefix %q does not carry the namespace %q", fam, got, ns)
		}
	}
	if scoped.trustKeyPrefix() == scoped.trustSetPrefix() {
		t.Errorf("the token and set prefixes are identical (%q); a sweep of one would eat the other", scoped.trustKeyPrefix())
	}
}

// TestTrustedDevicesSweepRefusesTheUnscopedStore is the safety property: a store
// with no namespace would sweep `jf:trust*`, which on a live DB is every
// trusted device the API is serving. It must refuse, and it must refuse BEFORE
// touching the client (the client here is nil on purpose).
func TestTrustedDevicesSweepRefusesTheUnscopedStore(t *testing.T) {
	var td TrustedDevices
	n, err := td.DeleteNamespaceKeys(context.Background())
	if err == nil {
		t.Fatalf("TrustedDevices.DeleteNamespaceKeys on an unscoped store deleted %d key(s) with no error; it must refuse", n)
	}
	if n != 0 {
		t.Errorf("refused sweep reported %d deleted key(s), want 0", n)
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Errorf("refusal %q does not say a namespace is required", err)
	}
}

// TestTrustedDeviceKeysAreNamespacedAndTheSweepIsScoped is the decisive test:
// a namespaced store mints all THREE key families under its namespace, the
// sweep removes exactly those, and NEITHER a peer namespace's keys NOR
// production-layout keys are in its reach.
//
// The third arm used to be a PRODUCTION-layout store (a session store built
// with no namespace) that minted `jf:trust:<hash>`,
// `jf:trustset:probe-trust-prod-<pid>` and
// `jf:trustmeta:probe-trust-prod-<pid>` with the production TrustTTL (30 days)
// and asserted the sweep spared them. That mint is the residue this file
// exists to stop writing: the ONLY thing that removed it was t.Cleanup, so a
// run killed between the mint and the cleanup (gate timeout, SIGTERM, dispatcher
// reclaim — all happen on this board) left a month-long key that no namespace
// sweep can see and that nothing can tell from a live trusted device's record.
// Measured on the pre-fix tree: SIGKILL right after the IssueDevice loop left
// `jf:trust:2c560a46…` with TTL 2,591,991 s (~30 days) on the shared test DB
// (card t_15694421).
//
// So the reach of the sweep is asserted from the commands it actually issues —
// the SCAN patterns and the DEL arguments — instead of from a key written to
// watch it survive. The production layout is never written to find out, and a
// killed run can only leave keys under this process's namespace with a
// two-minute TTL.
func TestTrustedDeviceKeysAreNamespacedAndTheSweepIsScoped(t *testing.T) {
	ctx := context.Background()
	mine := probeStore(t, "trust-mine", WithTrustTTL(2*time.Minute))
	peer := probeStore(t, "trust-peer", WithTrustTTL(2*time.Minute))
	// The probeStore cleanup sweeps SESSION keys only; these two own trust keys
	// too, so clean them up here rather than leave the very residue the card is
	// about. This is the FAILURE path's belt: the normal path sweeps `peer`
	// synchronously below, so nothing depends on the exit hook.
	t.Cleanup(func() {
		_, _ = peer.TrustedDevices().DeleteNamespaceKeys(context.Background())
	})

	mineTD, peerTD := mine.TrustedDevices(), peer.TrustedDevices()
	mineNS, peerNS := mine.KeyNamespace(), peer.KeyNamespace()

	users := map[string]string{
		"mine": fmt.Sprintf("probe-trust-mine-%d", os.Getpid()),
		"peer": fmt.Sprintf("probe-trust-peer-%d", os.Getpid()),
	}
	stores := map[string]*TrustedDevices{"mine": mineTD, "peer": peerTD}
	tokens := map[string]string{}
	for label, td := range stores {
		tok, err := td.IssueDevice(ctx, users[label], "probe-ua", "10.0.0.1")
		if err != nil {
			t.Fatalf("issue %s trust token: %v", label, err)
		}
		tokens[label] = tok
	}

	// Every store wrote all three families where its key builder says it did.
	for label, td := range stores {
		h := td.HashFor(tokens[label])
		if n, err := mine.Client().Exists(ctx, td.trustKey(h)).Result(); err != nil || n != 1 {
			t.Fatalf("setup: %s token key %q missing (exists=%d err=%v)", label, td.trustKey(h), n, err)
		}
		if n, err := mine.Client().Exists(ctx, td.trustSetKey(users[label])).Result(); err != nil || n != 1 {
			t.Fatalf("setup: %s set key missing (exists=%d err=%v)", label, n, err)
		}
		if n, err := mine.Client().Exists(ctx, td.trustMetaKey(users[label])).Result(); err != nil || n != 1 {
			t.Fatalf("setup: %s meta key missing (exists=%d err=%v)", label, n, err)
		}
	}

	// Every family of the test stores is under their namespace...
	for _, label := range []string{"mine", "peer"} {
		td := stores[label]
		h := td.HashFor(tokens[label])
		got := map[string]string{
			"token": td.trustKey(h),
			"set":   td.trustSetKey(users[label]),
			"meta":  td.trustMetaKey(users[label]),
		}
		want := map[string]string{
			"token": TrustKeyPrefix + td.namespace + ":",
			"set":   TrustSetKeyPrefix + td.namespace + ":",
			"meta":  TrustMetaKeyPrefix + td.namespace + ":",
		}
		for fam := range want {
			if !strings.HasPrefix(got[fam], want[fam]) {
				t.Errorf("%s %s key %q is not under the namespace %q", label, fam, got[fam], td.namespace)
			}
		}
	}

	// Record what the sweep issues. Added AFTER the setup reads, so the recorded
	// commands are the sweep's alone.
	rec := &sweepCommands{}
	mine.rdb.AddHook(rec)

	deleted, err := mineTD.DeleteNamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 3 {
		t.Errorf("sweep deleted %d key(s), want exactly the 3 (token + set + meta) this namespace owned", deleted)
	}

	// The sweep may only SCAN its own namespace, and must reach all three
	// families. The comparison is a SET of DISTINCT patterns in both
	// directions, not a count of SCAN commands: SCAN is cursor-iterated over
	// the WHOLE keyspace, so one pattern costs as many commands as it takes to
	// walk the hash table (`COUNT 500` over the shared test DB — ~1.1k keys and
	// growing — is 2-3 rounds per family). Counting commands made this
	// assertion a function of the shared DB's size rather than of the sweep's
	// reach, so it went red on keyspace growth with the code untouched.
	//   - a pattern outside this set means the sweep left its namespace; and
	//   - a missing one means a family this store writes is no longer
	//     reclaimed, i.e. it leaks onto the shared test Valkey again.
	wantPatterns := []string{
		mineTD.trustKeyPrefix() + "*",
		mineTD.trustSetPrefix() + "*",
		mineTD.trustMetaPrefix() + "*",
	}
	want := make(map[string]bool, len(wantPatterns))
	for _, p := range wantPatterns {
		want[p] = true
	}
	seen := map[string]bool{}
	for _, pattern := range rec.patterns {
		seen[pattern] = true
		if !want[pattern] {
			t.Errorf("the sweep scanned %q, which is not one of its own namespaced patterns %v", pattern, wantPatterns)
		}
	}
	if len(seen) == 0 {
		t.Fatal("the sweep issued no SCAN — the reach assertions below would be vacuous")
	}
	for _, p := range wantPatterns {
		if !seen[p] {
			t.Errorf("the sweep never scanned %q — one of the three key families this store writes is no longer reclaimed (want all of %v, saw %v)", p, wantPatterns, seen)
		}
	}
	// ...and may only DEL keys inside it.
	if len(rec.deleted) == 0 {
		t.Fatal("the sweep DELeted nothing — the assertion below would be vacuous")
	}
	inNamespace := func(key string) bool {
		for _, root := range []string{TrustKeyPrefix, TrustSetKeyPrefix, TrustMetaKeyPrefix} {
			if strings.HasPrefix(key, root+mineNS+":") {
				return true
			}
		}
		return false
	}
	for _, key := range rec.deleted {
		if !inNamespace(key) {
			t.Errorf("the sweep DELeted %q, which is outside its namespace %q — a test must never delete a key it did not write", key, mineNS)
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
		TrustKeyPrefix + hashOf("a-production-layout-trust-device"),
		TrustSetKeyPrefix + "a-production-layout-user",
		TrustMetaKeyPrefix + "a-production-layout-user",
	}
	for _, pk := range productionKeys {
		for _, pattern := range rec.patterns {
			if ok, err := path.Match(pattern, pk); err == nil && ok {
				t.Errorf("sweep pattern %q matches the production-layout key %q — the sweep could delete a live API's trusted device", pattern, pk)
			}
		}
	}

	if left := scanPattern(t, mine.Client(), "jf:trust:"+mineNS+":*"); len(left) != 0 {
		t.Errorf("token keys survived their own namespace sweep: %v", left)
	}
	if left := scanPattern(t, mine.Client(), "jf:trustset:"+mineNS+":*"); len(left) != 0 {
		t.Errorf("the SET survived its own namespace sweep (an empty set is still a leaked key): %v", left)
	}
	if left := scanPattern(t, mine.Client(), "jf:trustmeta:"+mineNS+":*"); len(left) != 0 {
		t.Errorf("metadata hashes survived their own namespace sweep: %v", left)
	}
	for _, pattern := range []string{
		"jf:trust:" + peerNS + ":*",
		"jf:trustset:" + peerNS + ":*",
		"jf:trustmeta:" + peerNS + ":*",
	} {
		if got := scanPattern(t, mine.Client(), pattern); len(got) != 1 {
			t.Errorf("a PEER namespace lost keys under %q (%d left) — a test must never delete a key it did not write", pattern, len(got))
		}
	}

	// The normal path cleans `peer` up here rather than in the exit hook, so a
	// test that reaches this line leaves nothing even if the process is killed
	// between it and TestMain.
	if _, err := peerTD.DeleteNamespaceKeys(ctx); err != nil {
		t.Errorf("sweep the peer namespace: %v", err)
	}
	for _, pattern := range []string{
		"jf:trust:" + peerNS + ":*",
		"jf:trustset:" + peerNS + ":*",
		"jf:trustmeta:" + peerNS + ":*",
	} {
		if left := scanPattern(t, mine.Client(), pattern); len(left) != 0 {
			t.Errorf("peer keys survived their own sweep under %q: %v", pattern, left)
		}
	}
}

// TestTrustedDeviceKeysHonourTheShortTestTTL is the belt to the sweep's braces:
// a run KILLED before TestMain (gate timeout, SIGKILL) must leave keys that
// expire in minutes, not a month.
func TestTrustedDeviceKeysHonourTheShortTestTTL(t *testing.T) {
	ctx := context.Background()
	s := probeStore(t, "trust-ttl", WithTrustTTL(time.Minute))
	td := s.TrustedDevices()
	t.Cleanup(func() { _, _ = td.DeleteNamespaceKeys(context.Background()) })

	userID := fmt.Sprintf("probe-trust-ttl-%d", os.Getpid())
	tok, err := td.Issue(ctx, userID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	for _, key := range []string{td.trustKey(td.HashFor(tok)), td.trustSetKey(userID), td.trustMetaKey(userID)} {
		ttl, err := s.Client().TTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("read TTL of %q: %v", key, err)
		}
		if ttl <= 0 || ttl > 61*time.Second {
			t.Errorf("%q carries TTL %v, want ~1m (the test override), not the production %v", key, ttl, TrustTTL)
		}
	}
}
