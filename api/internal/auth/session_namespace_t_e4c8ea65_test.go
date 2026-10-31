// Session key NAMESPACE tests (card t_e4c8ea65).
//
// The shared test Valkey accumulated `jf:session:*` keys forever: every test
// binary minted sessions under the PRODUCTION key name with the production TTL
// (14 days) and nothing removed them, because the process that wrote a key exits
// long before the key expires. A probe could not even clean up after itself: the
// keyspace held ~98k keys, so "find the key I just wrote" was a full-DB scan.
//
// The fix is ownership by name: a store built WithKeyNamespace writes
// `jf:session:<ns>:<hash>`, and DeleteNamespaceKeys deletes exactly that slice.
// These tests pin the two properties that make it safe —
//
//  1. the namespace is part of the key PREFIX, not a decoration, so a sweep
//     built from it cannot reach a peer's key or a production-layout one; and
//  2. an unscoped store REFUSES to sweep at all (namespace "" would mean
//     `jf:session:*` — the whole keyspace, i.e. the FlushDB this suite spent
//     several cards removing).
//
// The Valkey-backed half skips without JOMFORM_TEST_VALKEY_URL, matching the
// convention in this package. Every key it writes is under a randomsed
// per-test namespace and is deleted in the test, so it neither reads nor
// touches anything a concurrent checkout owns.
package auth

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// probeStore opens a store on the shared test Valkey under a namespace unique to
// this test process AND this call, so the sweep it performs can only ever touch
// keys this test wrote.
func probeStore(t *testing.T, suffix string, opts ...SessionOption) *Sessions {
	t.Helper()
	url := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if url == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping namespace test")
	}
	ns := "jftest:p" + strconv.Itoa(os.Getpid()) + ":nsprobe-" + suffix
	s, err := NewSessions(url, append([]SessionOption{WithKeyNamespace(ns)}, opts...)...)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() {
		// Best effort: whatever the test left under its own namespace goes.
		_, _ = s.DeleteNamespaceKeys(context.Background())
		_ = s.Close()
	})
	return s
}

// TestKeyNamespaceIsValidated pins the charset rule. DeleteNamespaceKeys builds a
// SCAN pattern by appending `*` to the namespace, so a glob character inside it
// would widen the sweep past the caller's own keys — that is a correctness
// property of the option, not a style preference.
func TestKeyNamespaceIsValidated(t *testing.T) {
	for _, ns := range []string{"jftest:p48213", "run.2026-10-30_x", "A:B-C_D.E"} {
		if !validKeyNamespace(ns) {
			t.Errorf("validKeyNamespace(%q) = false, want true", ns)
		}
		s, err := NewSessions("valkey://127.0.0.1:6379/9", WithKeyNamespace(ns))
		if err != nil {
			t.Errorf("NewSessions(WithKeyNamespace(%q)) = %v, want nil", ns, err)
			continue
		}
		_ = s.Close()
		if got := s.KeyNamespace(); got != ns {
			t.Errorf("KeyNamespace() = %q, want %q", got, ns)
		}
	}
	for _, ns := range []string{"jftest:*", "has space", "semi;colon", "quote'", "br[ack]et", "new\nline"} {
		s, err := NewSessions("valkey://127.0.0.1:6379/9", WithKeyNamespace(ns))
		if err == nil {
			_ = s.Close()
			t.Errorf("NewSessions(WithKeyNamespace(%q)) = nil error; a glob or space in the namespace would widen the sweep", ns)
		}
	}
	// The production store carries no namespace, so the layout is unchanged.
	plain, err := NewSessions("valkey://127.0.0.1:6379/9")
	if err != nil {
		t.Fatalf("NewSessions: %v", err)
	}
	defer func() { _ = plain.Close() }()
	if got := plain.KeyNamespace(); got != "" {
		t.Errorf("a store with no WithKeyNamespace reports namespace %q, want \"\"", got)
	}
	if got, want := plain.keyPrefix(), sessionKeyPrefix; got != want {
		t.Errorf("production keyPrefix = %q, want the bare %q", got, want)
	}
}

// TestDeleteNamespaceKeysRefusesTheUnscopedStore is the safety property: a store
// with no namespace would sweep `jf:session:*`, which on the shared test DB is
// every live session anything else is using. It must refuse, and it must refuse
// BEFORE touching the client.
func TestDeleteNamespaceKeysRefusesTheUnscopedStore(t *testing.T) {
	var s Sessions // nil client on purpose: the refusal must not reach it
	n, err := s.DeleteNamespaceKeys(context.Background())
	if err == nil {
		t.Fatalf("DeleteNamespaceKeys on an unscoped store deleted %d key(s) with no error; it must refuse", n)
	}
	if n != 0 {
		t.Errorf("refused sweep reported %d deleted key(s), want 0", n)
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Errorf("refusal %q does not say a namespace is required", err)
	}
}

// TestDefaultTTLIsOnlyAShortcutForTestStores pins that WithDefaultTTL changes
// what Create writes and nothing else — production (which never sets it) keeps
// SessionTTL.
func TestDefaultTTLIsOnlyAShortcutForTestStores(t *testing.T) {
	if got := (&Sessions{}).createTTL(); got != SessionTTL {
		t.Errorf("default createTTL = %v, want SessionTTL %v — production must not be shortened", got, SessionTTL)
	}
	if got := (&Sessions{defaultTTL: 15 * time.Minute}).createTTL(); got != 15*time.Minute {
		t.Errorf("createTTL with WithDefaultTTL(15m) = %v, want 15m", got)
	}
	// A zero/negative override must not mint an immortal or already-expired key.
	if got := (&Sessions{defaultTTL: -time.Minute}).createTTL(); got != SessionTTL {
		t.Errorf("createTTL with a negative override = %v, want the SessionTTL fallback", got)
	}
}

// TestCreateHonoursTheShortTestTTL is the Valkey-backed half of the above: the
// key really does carry minutes, so a run KILLED before its namespace sweep
// leaves keys that expire instead of week-long ones.
func TestCreateHonoursTheShortTestTTL(t *testing.T) {
	s := probeStore(t, "ttl", WithDefaultTTL(time.Minute))
	ctx := context.Background()
	token, err := s.Create(ctx, "probe-ttl-user")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ttl, err := s.TTL(ctx, token)
	if err != nil {
		t.Fatalf("read TTL: %v", err)
	}
	if ttl <= 0 || ttl > 61*time.Second {
		t.Errorf("created session TTL = %v, want ~1m (the test default), not the production %v", ttl, SessionTTL)
	}
}

// TestDeleteNamespaceKeysSweepsOnlyItsOwnNamespace is the decisive test: the
// sweep removes this store's keys and NEITHER a peer namespace's key NOR a
// production-layout key. Deleting the latter is what a whole-prefix sweep would
// do, and it is how a test fixture signs a live API's users out.
func TestDeleteNamespaceKeysSweepsOnlyItsOwnNamespace(t *testing.T) {
	mine := probeStore(t, "mine")
	peer := probeStore(t, "peer")
	ctx := context.Background()

	// A production-layout store, i.e. exactly what a running API holds: no
	// namespace, keys named `jf:session:<hash>`.
	url := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	prod, err := NewSessions(url)
	if err != nil {
		t.Fatalf("production-layout store: %v", err)
	}
	t.Cleanup(func() { _ = prod.Close() })

	type minted struct {
		store *Sessions
		token string
	}
	sessions := map[string]minted{}
	for label, s := range map[string]*Sessions{"mine": mine, "peer": peer, "prod": prod} {
		tok, err := s.Create(ctx, "probe-"+label+"-user")
		if err != nil {
			t.Fatalf("create %s session: %v", label, err)
		}
		sessions[label] = minted{store: s, token: tok}
		t.Cleanup(func() {
			// peer/prod are not swept by the sweep under test; mine already is.
			_ = s.Destroy(ctx, tok)
		})
	}

	// Sanity: each key is where its store says it is, so a "gone" below means
	// the sweep removed it rather than that it was never written.
	for label, m := range sessions {
		if n, err := m.store.Client().Exists(ctx, m.store.sessionKey(m.token)).Result(); err != nil || n != 1 {
			t.Fatalf("setup: %s session key missing (exists=%d err=%v)", label, n, err)
		}
	}

	deleted, err := mine.DeleteNamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Errorf("sweep deleted %d key(s), want exactly the 1 this namespace owned", deleted)
	}
	if n, _ := mine.Client().Exists(ctx, mine.sessionKey(sessions["mine"].token)).Result(); n != 0 {
		t.Errorf("this namespace's session key survived its own sweep (exists=%d)", n)
	}
	if n, _ := peer.Client().Exists(ctx, peer.sessionKey(sessions["peer"].token)).Result(); n != 1 {
		t.Errorf("a PEER namespace's session key was swept (exists=%d) — a test must never delete a key it did not write", n)
	}
	if n, _ := prod.Client().Exists(ctx, prod.sessionKey(sessions["prod"].token)).Result(); n != 1 {
		t.Errorf("a PRODUCTION-layout session key was swept (exists=%d) — that is a live API's login", n)
	}
}
