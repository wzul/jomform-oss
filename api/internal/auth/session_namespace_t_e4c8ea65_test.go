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
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
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
	s, err := NewSessions(url, append([]SessionOption{
		WithKeyNamespace(ns),
		WithDefaultTTL(valkeyTestSessionTTL),
		WithTrustTTL(valkeyTestTrustTTL),
	}, opts...)...)
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

// TestCreateHonoursTheShortTestTTL is the Valkey-backed half of the above: a key
// minted through Create really does carry minutes. That is the whole of the
// guarantee — a login handler passes its own TTL and ignores this default, so a
// killed run's handler-minted keys drain by expiry (24h, or 14d for a remember
// login) rather than in minutes (card t_39e2b840).
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
// sweep removes this store's keys and NONE of a peer namespace's.
//
// The third arm used to be a PRODUCTION-layout store that minted a
// `jf:session:<hash>` key and asserted the sweep spared it. That mint is exactly
// the residue this package must never write: a run killed between the mint and
// the cleanup leaves a production-layout key behind, and nothing can tell it from
// a live login (card t_d08b36c2). The reach of the sweep is asserted from the
// commands it actually issues instead — the SCAN patterns and the DEL arguments —
// so the production layout is never written to find out.
func TestDeleteNamespaceKeysSweepsOnlyItsOwnNamespace(t *testing.T) {
	mine := probeStore(t, "mine")
	peer := probeStore(t, "peer")
	ctx := context.Background()

	type minted struct {
		store *Sessions
		token string
	}
	sessions := map[string]minted{}
	for label, s := range map[string]*Sessions{"mine": mine, "peer": peer} {
		tok, err := s.Create(ctx, "probe-"+label+"-user")
		if err != nil {
			t.Fatalf("create %s session: %v", label, err)
		}
		sessions[label] = minted{store: s, token: tok}
		t.Cleanup(func() {
			// peer is not swept below; mine already is.
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

	rec := &sweepCommands{}
	mine.rdb.AddHook(rec)

	deleted, err := mine.DeleteNamespaceKeys(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Errorf("sweep deleted %d key(s), want exactly the 1 this namespace owned", deleted)
	}

	// The sweep may only SCAN its own namespace and may only DEL keys inside it.
	if len(rec.patterns) == 0 {
		t.Fatal("the sweep issued no SCAN — the reach assertions below would be vacuous")
	}
	// Sessions hands out SEVEN key families and DeleteNamespaceKeys sweeps all
	// seven (card t_6004c826): the session keys plus every derived store —
	// pending TOTP, step-up, password reset, magic link, the emailed 2FA code and
	// its pending flag. The roots are named HERE rather than read off the sweep,
	// and the comparison is a SET in both directions:
	//   - a pattern outside these seven means the sweep left its namespace; and
	//   - a missing one means a family this store still writes is no longer
	//     reclaimed, i.e. it leaks onto the shared test Valkey again — the exact
	//     residue t_6004c826 exists to stop, so it must red, not narrow.
	// SCAN is cursor-iterated, so the same pattern may be issued more than once;
	// what must hold is the set of DISTINCT patterns.
	ns := mine.KeyNamespace()
	wantPatterns := []string{
		namespacePrefix(sessionKeyPrefix, ns) + "*",
		namespacePrefix(PendingTOTPKeyPrefix, ns) + "*",
		namespacePrefix(StepUpKeyPrefix, ns) + "*",
		namespacePrefix(PasswordResetKeyPrefix, ns) + "*",
		namespacePrefix(MagicLinkKeyPrefix, ns) + "*",
		namespacePrefix(Email2FACodeKeyPrefix, ns) + "*",
		namespacePrefix(Email2FAPendingPrefix, ns) + "*",
	}
	want := make(map[string]bool, len(wantPatterns))
	for _, p := range wantPatterns {
		want[p] = true
	}
	seen := map[string]bool{}
	for _, pattern := range rec.patterns {
		seen[pattern] = true
		if !want[pattern] {
			t.Errorf("sweep scanned %q, which is not one of this namespace's seven family patterns %v — the sweep must never leave its namespace", pattern, wantPatterns)
		}
	}
	for _, p := range wantPatterns {
		if !seen[p] {
			t.Errorf("the sweep never scanned %q — one of the seven key families this store writes is no longer reclaimed (want all of %v, saw %v)", p, wantPatterns, seen)
		}
	}
	if want := mine.sessionKey(sessions["mine"].token); len(rec.deleted) != 1 || rec.deleted[0] != want {
		t.Errorf("sweep DELeted %v, want exactly its own key %q", rec.deleted, want)
	}
	for _, name := range rec.other {
		switch name {
		case "flushdb", "flushall", "keys":
			t.Errorf("the sweep issued %q — it must only SCAN/DEL its own namespace", name)
		}
	}
	// The property the deleted arm used to demonstrate at the server: a
	// production-layout key name is not inside this store's sweep root, so no
	// SCAN pattern above can match it.
	productionKey := sessionKeyPrefix + hashOf("a-production-layout-session")
	if strings.HasPrefix(productionKey, mine.keyPrefix()) {
		t.Errorf("production key %q starts with the sweep root %q — the sweep could reach a live login", productionKey, mine.keyPrefix())
	}

	if n, _ := mine.Client().Exists(ctx, mine.sessionKey(sessions["mine"].token)).Result(); n != 0 {
		t.Errorf("this namespace's session key survived its own sweep (exists=%d)", n)
	}
	if n, _ := peer.Client().Exists(ctx, peer.sessionKey(sessions["peer"].token)).Result(); n != 1 {
		t.Errorf("a PEER namespace's session key was swept (exists=%d) — a test must never delete a key it did not write", n)
	}
}

// sweepCommands records the keyspace commands a sweep issues, so a test can
// assert its REACH without writing a production-layout key to find out.
type sweepCommands struct {
	patterns []string
	deleted  []string
	other    []string
}

func (h *sweepCommands) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h *sweepCommands) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (h *sweepCommands) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
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
