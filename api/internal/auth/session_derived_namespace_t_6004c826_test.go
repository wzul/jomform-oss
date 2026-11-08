// Derived-store key namespace tests (card t_6004c826).
//
// WithKeyNamespace scoped SESSIONS, but the stores Sessions hands out —
// PendingTOTP, StepUp, PasswordReset, MagicLink and Email2FA — built their keys
// from the raw client and ignored it. A namespaced test store therefore still
// wrote PRODUCTION-layout keys (`jf:totp_pending:<uid>`, `jf:2fa_pending:<ip>:<email>`,
// `jf:magic:<hash>`, …) onto the shared test Valkey, and DeleteNamespaceKeys
// swept only `jf:session:<ns>:*`, so nothing could reclaim them. Measured on
// main @ 148a3a2d: one `go test ./internal/httpapi` run left 12 jf:magic:, 3
// jf:2fa_pending:, 1 jf:totp_pending: and 2 jf:stepup: keys behind.
//
// Two properties are pinned here, in the same shape as the session and trust
// namespaces before them:
//
//  1. the PRODUCTION layout is byte-identical — an unscoped store (the only
//     thing production builds) still emits exactly today's key names, so this
//     change cannot rename a live credential; and
//  2. a namespaced store writes every family under its namespace, and ONE
//     DeleteNamespaceKeys call reclaims all of them while a peer namespace and
//     a production-layout store keep their keys.
//
// The Valkey-backed half skips without JOMFORM_TEST_VALKEY_URL and runs under a
// per-process, per-call namespace, so it can only ever touch keys it wrote.
package auth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDerivedStoreKeyLayoutIsUnchangedInProduction pins the names production
// actually uses. The prefixes are written out as literals on purpose: comparing
// against the package constants would move with the code and pin nothing.
//
// No client is needed — every key builder is pure.
func TestDerivedStoreKeyLayoutIsUnchangedInProduction(t *testing.T) {
	s := &Sessions{} // no namespace, no client: the production store
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	hash := hashOf(token)

	cases := []struct {
		name   string
		got    string
		wanted string
	}{
		{"session", s.sessionKey(token), "jf:session:" + hash},
		{"totp_pending", s.PendingTOTP().key("user-1"), "jf:totp_pending:user-1"},
		{"stepup", s.StepUp().key(token), "jf:stepup:" + hash},
		{"pwreset", s.PasswordReset().key(hash), "jf:pwreset:" + hash},
		{"magic", s.MagicLink().key(hash), "jf:magic:" + hash},
		{"2faemail", s.Email2FA().codeKeyName("user-1"), "jf:2faemail:user-1"},
		{"2fa_pending", s.Email2FA().pendingKeyName("203.0.113.7", "A@Example.test"), "jf:2fa_pending:203.0.113.7:a@example.test"},
	}
	for _, c := range cases {
		if c.got != c.wanted {
			t.Errorf("%s key with no namespace = %q, want the production layout %q — an unscoped store is the only one production builds", c.name, c.got, c.wanted)
		}
	}
}

// derivedFixture is one store plus the namespace it owns, and the keys each
// family landed on in both layouts.
type derivedFixture struct {
	store *Sessions
	ns    string
	// nsKeys are the keys this store minted, under its namespace.
	nsKeys []string
	// prodKeys are the same values expressed in the production layout; they
	// must NOT exist for a namespaced store.
	prodKeys []string
}

// mintDerived mints one key in every family Sessions hands out and records
// where each landed. label keeps the user ids and values unique per fixture so
// a production-layout key from one fixture can never be mistaken for another's.
func mintDerived(t *testing.T, s *Sessions, label string) *derivedFixture {
	t.Helper()
	ctx := context.Background()
	uid := "derived-" + label + "-user"

	sessToken, err := s.Create(ctx, uid)
	if err != nil {
		t.Fatalf("%s: create session: %v", label, err)
	}
	if err := s.PendingTOTP().Set(ctx, uid, "seed"); err != nil {
		t.Fatalf("%s: pending totp: %v", label, err)
	}
	stepToken := "stepup-token-" + label
	if err := s.StepUp().Grant(ctx, stepToken, uid); err != nil {
		t.Fatalf("%s: step-up grant: %v", label, err)
	}
	resetToken, err := s.PasswordReset().Generate(ctx, uid)
	if err != nil {
		t.Fatalf("%s: password reset: %v", label, err)
	}
	magicToken, err := s.MagicLink().Generate(ctx, uid)
	if err != nil {
		t.Fatalf("%s: magic link: %v", label, err)
	}
	if err := s.Email2FA().Store(ctx, uid, "123456"); err != nil {
		t.Fatalf("%s: email code: %v", label, err)
	}
	// The pending flag is keyed by ip+email, not by user id, so both parts carry
	// the label: two fixtures sharing them would collide on one key and the
	// "production twin is absent" assertion would read the OTHER fixture's key.
	ip := "203.0.113.9"
	email := "derived-" + label + "@example.test"
	if err := s.Email2FA().ArmPending(ctx, ip, email); err != nil {
		t.Fatalf("%s: email pending: %v", label, err)
	}

	nsPrefix := func(root string) string {
		if s.namespace == "" {
			return root
		}
		return root + s.namespace + ":"
	}
	// Both layouts for the same value, so "the namespaced one exists and the
	// production one does not" is a real contrast rather than two guesses.
	pairs := [][2]string{
		{s.sessionKey(sessToken), "jf:session:" + hashOf(sessToken)},
		{nsPrefix("jf:totp_pending:") + uid, "jf:totp_pending:" + uid},
		{nsPrefix("jf:stepup:") + hashOf(stepToken), "jf:stepup:" + hashOf(stepToken)},
		{nsPrefix("jf:pwreset:") + hashOf(resetToken), "jf:pwreset:" + hashOf(resetToken)},
		{nsPrefix("jf:magic:") + hashOf(magicToken), "jf:magic:" + hashOf(magicToken)},
		{nsPrefix("jf:2faemail:") + uid, "jf:2faemail:" + uid},
		{nsPrefix("jf:2fa_pending:") + ip + ":" + email, "jf:2fa_pending:" + ip + ":" + email},
	}
	f := &derivedFixture{store: s, ns: s.namespace}
	for _, p := range pairs {
		f.nsKeys = append(f.nsKeys, p[0])
		if p[1] != p[0] {
			f.prodKeys = append(f.prodKeys, p[1])
		}
	}
	// Register the reclaim BEFORE the caller asserts anything: a Fatal in the
	// test body must not leave a key behind. Both layouts are named exactly, so
	// this deletes only what this fixture wrote — the peer and production
	// fixtures included, which is why the test's own sweep assertions still run
	// before this (t.Cleanup is LIFO at the end of the test, not on registration).
	t.Cleanup(func() {
		for _, k := range append(append([]string{}, f.nsKeys...), f.prodKeys...) {
			_ = s.Client().Del(context.Background(), k).Err()
		}
	})
	return f
}

// TestDerivedStoresInheritTheNamespaceAndOneSweepReclaimsThem is the decisive
// test: every family a namespaced store writes lives under its namespace, and
// the single DeleteNamespaceKeys call a TestMain makes removes all of them —
// while a peer namespace and a production-layout store keep every key.
func TestDerivedStoresInheritTheNamespaceAndOneSweepReclaimsThem(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_VALKEY_URL") == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping derived-store namespace test")
	}
	codeKey, err := EmailCodeKey(testMasterKey(0x90))
	if err != nil {
		t.Fatalf("derive email code key: %v", err)
	}
	mine := probeStore(t, "derived-mine", WithEmailCodeKey(codeKey))
	peer := probeStore(t, "derived-peer", WithEmailCodeKey(codeKey))

	// The production-layout leg is the only fixture in this file that writes
	// production key NAMES, and production `Create` TTL is 14 days. A two-minute
	// default bounds the session that leg mints, so even a SIGKILL between the
	// write and the cleanup drains in minutes; every other family's TTL is the
	// production one (5–30 min) and mintDerived's cleanup names those keys
	// exactly. It is a real contrast, not a guess: an unscoped store is the only
	// thing production builds, and it must still emit today's names.
	prod, err := NewSessions(os.Getenv("JOMFORM_TEST_VALKEY_URL"), WithEmailCodeKey(codeKey), WithDefaultTTL(2*time.Minute))
	if err != nil {
		t.Fatalf("production-layout store: %v", err)
	}
	t.Cleanup(func() { _ = prod.Close() })

	ctx := context.Background()
	mineFx := mintDerived(t, mine, "mine")
	peerFx := mintDerived(t, peer, "peer")
	prodFx := mintDerived(t, prod, "prod")

	// Everything a namespaced store minted is under its namespace, and the
	// production-layout twin is absent. A store that ignored the namespace
	// fails here, at the exact key that is the leak.
	for _, k := range mineFx.nsKeys {
		if !strings.Contains(k, ":"+mine.namespace+":") {
			t.Fatalf("minted key %q is not under the process namespace %q", k, mine.namespace)
		}
		if n, err := mine.Client().Exists(ctx, k).Result(); err != nil || n != 1 {
			t.Fatalf("namespaced key %q missing (exists=%d err=%v)", k, n, err)
		}
	}
	for _, k := range mineFx.prodKeys {
		if n, err := mine.Client().Exists(ctx, k).Result(); err != nil || n != 0 {
			t.Errorf("namespaced store wrote the PRODUCTION-layout key %q (exists=%d err=%v) — the exit sweep cannot see it", k, n, err)
		}
	}
	// The production-layout fixture is where those keys DO belong; without this
	// leg the absence above could mean the store never wrote anything.
	for _, k := range prodFx.nsKeys {
		if n, err := prod.Client().Exists(ctx, k).Result(); err != nil || n != 1 {
			t.Fatalf("production-layout store did not write %q (exists=%d err=%v)", k, n, err)
		}
	}

	// Every key these three fixtures wrote is reclaimed by mintDerived's own
	// t.Cleanup (registered before the assertions below), so nothing here needs
	// a second delete — and that reclaim is what keeps the production-layout
	// leg from outliving a failing run.

	start := time.Now()
	deleted, err := mine.DeleteNamespaceKeys(ctx)
	t.Logf("swept %d key(s) from namespace %q in %v", deleted, mine.namespace, time.Since(start))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if want := len(mineFx.nsKeys); deleted != want {
		t.Errorf("sweep deleted %d key(s), want the %d this namespace owned (one per family) — a family it cannot see is the leak", deleted, want)
	}
	for _, k := range mineFx.nsKeys {
		if n, err := mine.Client().Exists(ctx, k).Result(); err != nil || n != 0 {
			t.Errorf("key %q survived its own namespace sweep (exists=%d err=%v)", k, n, err)
		}
	}
	for _, k := range peerFx.nsKeys {
		if n, err := peer.Client().Exists(ctx, k).Result(); err != nil || n != 1 {
			t.Errorf("a PEER namespace's key %q was swept (exists=%d err=%v) — one process must never delete another's", k, n, err)
		}
	}
	for _, k := range prodFx.nsKeys {
		if n, err := prod.Client().Exists(ctx, k).Result(); err != nil || n != 1 {
			t.Errorf("a PRODUCTION-layout key %q was swept (exists=%d err=%v) — that is a live credential", k, n, err)
		}
	}
}
