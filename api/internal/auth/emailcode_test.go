// Email backup-code digest tests (card t_a3f3aca5).
//
// The emailed 2FA backup code is six digits, so the stored value must not be a
// bare hash: `sha256(code)` over a 1,000,000-value space is inverted by
// enumerating every candidate — offline, in well under a second — so anyone who
// could read the Valkey keyspace (a dump, a snapshot/AOF file, MONITOR) would
// hold the second factor for a password they already have. The store therefore
// keys the digest with a server-side secret (EmailCodeKey, derived from
// ENCRYPTION_KEY).
//
// The decisive test here is the ATTACK ITSELF, run against a real stored value:
// enumerate all 1,000,000 codes, hash each with SHA-256, and require no match —
// with a control leg proving the same enumeration DOES recover a code from a
// bare `sha256(code)`, so a green cannot be a loop that never searched.
//
// The Valkey-backed tests skip without JOMFORM_TEST_VALKEY_URL (the preflight
// `test` gate sets it), matching the convention in
// session_keyspace_test.go. Every key is named per test and deleted in cleanup:
// the test Valkey DB is shared by concurrent checkouts, so nothing here scans,
// flushes, or deletes a key it did not create.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// testMasterKey is a deterministic 32-byte stand-in for the ENCRYPTION_KEY bytes
// (the real one is 64 hex chars decoded to 32 bytes). Every test derives its
// code key from one of these through EmailCodeKey, so the tests exercise the
// shipped derivation rather than a hand-built key.
func testMasterKey(b byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b + byte(i)
	}
	return key
}

// email2FATestStore opens the shared test Valkey wired with a code key derived
// from master, or skips when the URL is not configured.
func email2FATestStore(t *testing.T, master []byte) (*Sessions, *Email2FA) {
	t.Helper()
	url := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if url == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping email-2fa store test")
	}
	key, err := EmailCodeKey(master)
	if err != nil {
		t.Fatalf("derive email code key: %v", err)
	}
	s, err := NewSessions(url, WithEmailCodeKey(key))
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, s.Email2FA()
}

// email2FAUserID returns a userID unique to this test run and registers the
// cleanup of any code key it leaves behind.
func email2FAUserID(t *testing.T, s *Sessions, label string) string {
	t.Helper()
	id := fmt.Sprintf("t-a3f3aca5-%s-%d", label, time.Now().UnixNano())
	t.Cleanup(func() {
		_ = s.rdb.Del(context.Background(), email2FaCodeKey(id)).Err()
	})
	return id
}

// sha256Hex is the pre-fix digest: the one an attacker enumerates.
func sha256Hex(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// invertSixDigitDigest runs the card's attack verbatim: try every 6-digit
// candidate in the code's real space and return the first whose SHA-256 hex is
// digest. "" means the digest was not in that set. It is used in BOTH directions
// — to prove a stored HMAC is not recoverable, and (the control) to prove the
// enumeration really does recover a bare `sha256(code)`.
func invertSixDigitDigest(digest string) string {
	for n := 0; n < 1_000_000; n++ {
		cand := fmt.Sprintf("%06d", n)
		if sha256Hex(cand) == digest {
			return cand
		}
	}
	return ""
}

// TestEmailCodeStoredValueIsNotTheSha256OfAnySixDigitCode is the card's
// acceptance test: the value under jf:2faemail:{userID} must NOT be invertible
// by enumerating the whole 6-digit space.
func TestEmailCodeStoredValueIsNotTheSha256OfAnySixDigitCode(t *testing.T) {
	ctx := context.Background()
	sess, store := email2FATestStore(t, testMasterKey(0x10))
	userID := email2FAUserID(t, sess, "invertible")

	code, err := Generate6Digit()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := store.Store(ctx, userID, code); err != nil {
		t.Fatalf("store: %v", err)
	}
	stored, err := sess.rdb.Get(ctx, email2FaCodeKey(userID)).Result()
	if err != nil {
		t.Fatalf("read stored value: %v", err)
	}

	// CONTROL: the enumeration recovers the code from a bare sha256. Without
	// this leg, a green below could equally be a search that never ran.
	if got := invertSixDigitDigest(sha256Hex(code)); got != code {
		t.Fatalf("control failed: the 6-digit enumeration recovered %q from sha256(%q), want %q "+
			"— the inversion harness itself is broken", got, code, code)
	}
	// THE ATTACK: the shipped stored value is not in the sha256 code space.
	if got := invertSixDigitDigest(stored); got != "" {
		t.Fatalf("the stored value %s inverts to the live code %q: a keyspace read hands out the "+
			"second factor (keyspace inversion, not a one-way function)", stored, got)
	}
	// And it is not the code in the clear either.
	if strings.Contains(stored, code) {
		t.Fatalf("stored value %q contains the plaintext code %q", stored, code)
	}
	// The window the exposure would have covered is bounded and unchanged.
	ttl, err := sess.rdb.PTTL(ctx, email2FaCodeKey(userID)).Result()
	if err != nil {
		t.Fatalf("pttl: %v", err)
	}
	if ttl <= 0 || ttl > Email2FACodeTTL {
		t.Fatalf("stored code TTL = %v, want (0, %v]", ttl, Email2FACodeTTL)
	}
}

// TestEmailCodeStoreAcceptsTheRightCodeAndRejectsOthers pins the verification
// behaviour the keyed digest must not disturb: the right code verifies once, a
// wrong code is refused AND does not burn the stored one, and a replay fails.
func TestEmailCodeStoreAcceptsTheRightCodeAndRejectsOthers(t *testing.T) {
	ctx := context.Background()
	sess, store := email2FATestStore(t, testMasterKey(0x20))
	userID := email2FAUserID(t, sess, "consume")

	code, err := Generate6Digit()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := store.Store(ctx, userID, code); err != nil {
		t.Fatalf("store: %v", err)
	}

	wrong := "000000"
	if wrong == code {
		wrong = "000001"
	}
	if err := store.Consume(ctx, userID, wrong); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("wrong code err = %v, want ErrEmail2FAInvalid", err)
	}
	// A typo must not consume: the valid code is still there and still matches.
	if err := store.Consume(ctx, userID, ""); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("empty code err = %v, want ErrEmail2FAInvalid", err)
	}
	if _, err := sess.rdb.Get(ctx, email2FaCodeKey(userID)).Result(); err != nil {
		t.Fatalf("wrong code consumed the stored value: %v", err)
	}
	if err := store.Consume(ctx, userID, code); err != nil {
		t.Fatalf("correct code rejected: %v", err)
	}
	// Single-use: the atomic GetDel left nothing behind.
	if _, err := sess.rdb.Get(ctx, email2FaCodeKey(userID)).Result(); !errors.Is(err, goredis.Nil) {
		t.Fatalf("consumed key still present (err=%v), want redis.Nil", err)
	}
	if err := store.Consume(ctx, userID, code); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("replayed code err = %v, want ErrEmail2FAInvalid (single-use violated)", err)
	}
}

// TestEmailCodeDigestIsKeyedByTheServerSecret: the same code, the same user, a
// DIFFERENT server key does not verify. This is what makes the stored value
// useless to a keyspace reader who does not also hold ENCRYPTION_KEY.
func TestEmailCodeDigestIsKeyedByTheServerSecret(t *testing.T) {
	ctx := context.Background()
	sessA, storeA := email2FATestStore(t, testMasterKey(0x30))
	_, storeB := email2FATestStore(t, testMasterKey(0x40))
	userID := email2FAUserID(t, sessA, "keyed")

	code, err := Generate6Digit()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := storeA.Store(ctx, userID, code); err != nil {
		t.Fatalf("store: %v", err)
	}

	keyA, err := EmailCodeKey(testMasterKey(0x30))
	if err != nil {
		t.Fatalf("key A: %v", err)
	}
	keyB, err := EmailCodeKey(testMasterKey(0x40))
	if err != nil {
		t.Fatalf("key B: %v", err)
	}
	if emailCodeDigest(keyA, userID, code) == emailCodeDigest(keyB, userID, code) {
		t.Fatal("two different server keys produced the same digest — the digest is not keyed")
	}
	if err := storeB.Consume(ctx, userID, code); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("a store with a DIFFERENT server key verified the code (err=%v), want ErrEmail2FAInvalid", err)
	}
	// The mismatch must not consume the real code for the store that owns it.
	if err := storeA.Consume(ctx, userID, code); err != nil {
		t.Fatalf("the owning store could not verify its own code afterwards: %v", err)
	}
}

// TestEmailCodeStoreFailsClosedWithoutAKey: a Sessions wired without
// WithEmailCodeKey refuses to ARM a code, and verifies nothing. The failure has
// to be loud on the write side, because the alternative — silently storing the
// bare hash again — is the defect this card exists to remove.
func TestEmailCodeStoreFailsClosedWithoutAKey(t *testing.T) {
	ctx := context.Background()
	sessA, keyed := email2FATestStore(t, testMasterKey(0x50))

	url := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	keyless, err := NewSessions(url) // the production wiring mistake
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() { _ = keyless.Close() })
	store := keyless.Email2FA()

	userID := email2FAUserID(t, sessA, "keyless")
	code, err := Generate6Digit()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := store.Store(ctx, userID, code); !errors.Is(err, ErrEmail2FAKeyMissing) {
		t.Fatalf("Store without a code key err = %v, want ErrEmail2FAKeyMissing", err)
	}
	if _, err := sessA.rdb.Get(ctx, email2FaCodeKey(userID)).Result(); !errors.Is(err, goredis.Nil) {
		t.Fatalf("Store without a code key still wrote %s (err=%v) — it must write nothing",
			email2FaCodeKey(userID), err)
	}
	if err := store.Consume(ctx, userID, code); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("Consume without a code key err = %v, want ErrEmail2FAInvalid", err)
	}

	// Even a value minted with NO key (a digest over an empty key) must not be
	// accepted by a keyless store. The rule is "no key ⇒ verify nothing", not
	// "no key ⇒ compare against a keyless digest", which is what a plain
	// comparison would degrade to.
	if err := sessA.rdb.Set(ctx, email2FaCodeKey(userID), emailCodeDigest(nil, userID, code), Email2FACodeTTL).Err(); err != nil {
		t.Fatalf("plant keyless digest: %v", err)
	}
	if err := store.Consume(ctx, userID, code); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("keyless store verified a keyless digest (err=%v), want ErrEmail2FAInvalid", err)
	}
	_ = sessA.rdb.Del(ctx, email2FaCodeKey(userID)).Err()

	// A keyless store must not accept a value armed by a keyed one either: it
	// has no key to verify against, so it fails closed rather than open.
	if err := keyed.Store(ctx, userID, code); err != nil {
		t.Fatalf("keyed store: %v", err)
	}
	if err := store.Consume(ctx, userID, code); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("keyless store verified a keyed value (err=%v), want ErrEmail2FAInvalid", err)
	}
	if err := keyed.Consume(ctx, userID, code); err != nil {
		t.Fatalf("keyed store lost its code to a keyless Consume: %v", err)
	}
}

// TestEmailCodeDigestIsBoundToTheAccount: a stored digest lifted onto ANOTHER
// user's key (a keyspace WRITE, the complement of a read) does not verify. The
// account id is part of the MAC input, not just the key name.
func TestEmailCodeDigestIsBoundToTheAccount(t *testing.T) {
	ctx := context.Background()
	sess, store := email2FATestStore(t, testMasterKey(0x60))
	victim := email2FAUserID(t, sess, "victim")
	attacker := email2FAUserID(t, sess, "attacker")

	code, err := Generate6Digit()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// The attacker arms a code on an account they control…
	if err := store.Store(ctx, attacker, code); err != nil {
		t.Fatalf("store: %v", err)
	}
	lifted, err := sess.rdb.Get(ctx, email2FaCodeKey(attacker)).Result()
	if err != nil {
		t.Fatalf("read attacker value: %v", err)
	}
	// …and copies the digest onto the victim's key with the remaining TTL.
	ttl, err := sess.rdb.PTTL(ctx, email2FaCodeKey(attacker)).Result()
	if err != nil {
		t.Fatalf("pttl: %v", err)
	}
	if err := sess.rdb.Set(ctx, email2FaCodeKey(victim), lifted, ttl).Err(); err != nil {
		t.Fatalf("plant on victim: %v", err)
	}
	if err := store.Consume(ctx, victim, code); !errors.Is(err, ErrEmail2FAInvalid) {
		t.Fatalf("a digest minted for another account verified (err=%v), want ErrEmail2FAInvalid", err)
	}

	key, err := EmailCodeKey(testMasterKey(0x60))
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	if emailCodeDigest(key, attacker, code) == emailCodeDigest(key, victim, code) {
		t.Fatal("the digest does not depend on the account it was issued to")
	}
}

// TestEmailCodeKeyDerivation pins the KDF: 32 bytes out, deterministic,
// domain-separated from the ENCRYPTION_KEY bytes it is derived from, distinct
// per master key, and a hard error — never a zero value — on a bad key.
func TestEmailCodeKeyDerivation(t *testing.T) {
	master := testMasterKey(0x70)
	key, err := EmailCodeKey(master)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("derived key is %d bytes, want 32", len(key))
	}
	again, err := EmailCodeKey(master)
	if err != nil {
		t.Fatalf("derive again: %v", err)
	}
	if string(again) != string(key) {
		t.Fatal("derivation is not deterministic")
	}
	if string(key) == string(master) {
		t.Fatal("the code key IS the master ENCRYPTION_KEY — no domain separation")
	}
	other, err := EmailCodeKey(testMasterKey(0x80))
	if err != nil {
		t.Fatalf("derive other: %v", err)
	}
	if string(other) == string(key) {
		t.Fatal("two different master keys derived the same code key")
	}
	for _, bad := range [][]byte{nil, master[:31], append(append([]byte{}, master...), 0x00)} {
		if got, err := EmailCodeKey(bad); err == nil {
			t.Fatalf("EmailCodeKey(%d bytes) returned a %d-byte key and no error; want a hard error",
				len(bad), len(got))
		}
	}
}
