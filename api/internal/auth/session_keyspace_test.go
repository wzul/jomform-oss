// Session keyspace tests (card t_074ae1df).
//
// The session cookie carries the raw 256-bit token; the keyspace must hold only
// its SHA-256, so a Valkey read (dump, snapshot/AOF file, MONITOR) cannot hand
// out a live login. That property is about the NAME OF A KEY, which no fake can
// prove, so the decisive tests here are Valkey-backed: they scan the real
// keyspace for the raw token. They skip without JOMFORM_TEST_VALKEY_URL (the
// preflight `test` gate sets it), matching the integration-test convention in
// internal/httpapi.
//
// Every key these tests write is named per test and deleted in cleanup — the
// test Valkey DB is shared by concurrent checkouts, so nothing here scans-and-
// deletes, or flushes, a key it did not create.
package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// valkeySessions opens the shared test Valkey, or skips the test when the URL is
// not configured.
func valkeySessions(t *testing.T) *Sessions {
	t.Helper()
	url := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if url == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping session-keyspace test")
	}
	s, err := NewSessions(url)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// scanKeysContaining returns every key in the DB whose NAME contains sub. It
// uses SCAN, not KEYS: the test DB is shared, and KEYS blocks the server for
// every other checkout's tests.
func scanKeysContaining(ctx context.Context, t *testing.T, s *Sessions, sub string) []string {
	t.Helper()
	var (
		cursor uint64
		found  []string
	)
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, "*"+sub+"*", 200).Result()
		if err != nil {
			t.Fatalf("scan keyspace for %q: %v", sub, err)
		}
		for _, k := range keys {
			if strings.Contains(k, sub) {
				found = append(found, k)
			}
		}
		cursor = next
		if cursor == 0 {
			return found
		}
	}
}

// cleanupToken deletes whatever either layout holds for token.
func cleanupToken(t *testing.T, s *Sessions, token string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		if err := s.rdb.Del(ctx, s.sessionKey(token), s.legacySessionKey(token)).Err(); err != nil {
			t.Errorf("cleanup session keys for %q: %v", token, err)
		}
	})
}

// TestSessionTokenIsNeverAKeyName is the requirement in one place: a token
// minted by Create must not be a key name, in either the exact legacy form or
// anywhere else in the keyspace, and Get must still resolve it.
func TestSessionTokenIsNeverAKeyName(t *testing.T) {
	s := valkeySessions(t)
	ctx := context.Background()

	token, err := s.CreateRememberedWithTTL(ctx, "user-hash", time.Hour, true)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cleanupToken(t, s, token)

	// The pre-change layout, named exactly.
	if n, err := s.rdb.Exists(ctx, sessionKeyPrefix+token).Result(); err != nil {
		t.Fatalf("exists raw key: %v", err)
	} else if n != 0 {
		t.Errorf("the raw session token %q is still a key name — a Valkey dump would hand out this login", sessionKeyPrefix+token)
	}

	// And nowhere else: a scan for the token text must come back empty.
	if keys := scanKeysContaining(ctx, t, s, token); len(keys) != 0 {
		t.Errorf("keyspace carries the raw session token in %d key name(s): %v", len(keys), keys)
	}

	// The hash IS the key, and it resolves, so the session is not merely gone.
	hashed := s.sessionKey(token)
	if hashed == sessionKeyPrefix+token {
		t.Fatalf("sessionKey(%q) is the raw token", token)
	}
	if n, err := s.rdb.Exists(ctx, hashed).Result(); err != nil || n != 1 {
		t.Fatalf("hashed session key %q missing (exists=%d err=%v)", hashed, n, err)
	}
	uid, err := s.Get(ctx, token)
	if err != nil || uid != "user-hash" {
		t.Errorf("Get(created token) = (%q, %v), want (user-hash, nil)", uid, err)
	}
	if !s.Remembered(ctx, token) {
		t.Error("Remembered(created token) = false, want true — the value must survive the re-keying")
	}
}

// TestSessionKeyIsHashNotToken is the pure unit half: the derivation itself.
// It pins that the key suffix is the hex SHA-256 (64 chars), not the token, so
// a token carrying characters that mean something to the keyspace (globs, a
// newline) can never alias another session's key. The namespace half of the
// layout (WithKeyNamespace) is pinned the same way: it is INSERTED after the
// root, so the hashed suffix is still the whole identity of the session.
func TestSessionKeyIsHashNotToken(t *testing.T) {
	for _, ns := range []string{"", "jftest:p48213", "run.2026-10-30_x"} {
		s := &Sessions{namespace: ns}
		prefix := sessionKeyPrefix
		if ns != "" {
			prefix += ns + ":"
		}
		if got := s.keyPrefix(); got != prefix {
			t.Errorf("keyPrefix(namespace %q) = %q, want %q", ns, got, prefix)
			continue
		}
		for _, token := range []string{
			"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"*",
			"a\r\nDEL jf:session:x",
			"jf:session:other",
		} {
			key := s.sessionKey(token)
			if key == prefix+token {
				t.Errorf("sessionKey(%q, namespace %q) returns the raw token", token, ns)
				continue
			}
			if want := prefix + hashOf(token); key != want {
				t.Errorf("sessionKey(%q, namespace %q) = %q, want the SHA-256 layout %q", token, ns, key, want)
				continue
			}
			suffix := strings.TrimPrefix(key, prefix)
			if len(suffix) != 64 {
				t.Errorf("sessionKey(%q) suffix %q is not a hex SHA-256 (len %d)", token, suffix, len(suffix))
			}
			if raw, err := hex.DecodeString(suffix); err != nil || len(raw) != 32 {
				t.Errorf("sessionKey(%q) suffix %q is not hex-encoded 32 bytes (err=%v)", token, suffix, err)
			}
		}
	}
}

// TestLegacyRawTokenSessionResolvesAndIsRekeyed covers the deploy window: a
// session minted before the re-keying sits under the raw-token key. It must keep
// working (no forced logout), be moved to the hashed key on first read, and keep
// its ORIGINAL deadline — a migration that reset the TTL would silently extend a
// short session to SessionTTL, and one that dropped it would log out a
// remember-me user early.
func TestLegacyRawTokenSessionResolvesAndIsRekeyed(t *testing.T) {
	s := valkeySessions(t)
	ctx := context.Background()

	token, err := randomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	cleanupToken(t, s, token)

	const remaining = 2 * time.Hour
	issued := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	value := encodeValue("user-legacy", issued, true, true)
	if err := s.rdb.Set(ctx, sessionKeyPrefix+token, value, remaining).Err(); err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}

	uid, err := s.Get(ctx, token)
	if err != nil || uid != "user-legacy" {
		t.Fatalf("Get(legacy token) = (%q, %v), want (user-legacy, nil) — a pre-deploy session must not be logged out", uid, err)
	}
	if n, err := s.rdb.Exists(ctx, sessionKeyPrefix+token).Result(); err != nil || n != 0 {
		t.Errorf("legacy raw-token key survived the migration (exists=%d err=%v) — it must be deleted once hashed", n, err)
	}
	ttl, err := s.rdb.PTTL(ctx, s.sessionKey(token)).Result()
	if err != nil {
		t.Fatalf("read migrated TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("migrated session key carries no expiry (ttl=%v)", ttl)
	}
	if ttl > remaining {
		t.Errorf("migrated TTL = %v, longer than the %v the legacy key had — the migration extended the session", ttl, remaining)
	}
	if ttl < remaining-2*time.Minute {
		t.Errorf("migrated TTL = %v, want ~%v (the legacy key's remaining lifetime)", ttl, remaining)
	}

	// The whole value moved, not just the user id: remember/magic/issued drive
	// session rotation and the step-up gate.
	if !s.Remembered(ctx, token) {
		t.Error("Remembered(legacy token) = false after migration, want true")
	}
	if !s.Magic(ctx, token) {
		t.Error("Magic(legacy token) = false after migration, want true")
	}
	if _, gotIssued, gerr := s.GetCreated(ctx, token); gerr != nil || !gotIssued.Equal(issued) {
		t.Errorf("GetCreated(legacy token) issued = %v (err=%v), want %v", gotIssued, gerr, issued)
	}
}

// TestLegacyKeyWithoutExpiryIsServedButNotRekeyed pins the ttl<=0 branch: there
// is no deadline to carry over, so a durable copy must not be minted from it —
// the session is still served (it was live a moment ago) and is left for
// Destroy/expiry to end.
func TestLegacyKeyWithoutExpiryIsServedButNotRekeyed(t *testing.T) {
	s := valkeySessions(t)
	ctx := context.Background()

	token, err := randomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	cleanupToken(t, s, token)

	if err := s.rdb.Set(ctx, sessionKeyPrefix+token, encodeValue("user-nottl", time.Now().UTC(), false, false), 0).Err(); err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}
	uid, err := s.Get(ctx, token)
	if err != nil || uid != "user-nottl" {
		t.Fatalf("Get(legacy token, no expiry) = (%q, %v), want (user-nottl, nil)", uid, err)
	}
	if n, err := s.rdb.Exists(ctx, s.sessionKey(token)).Result(); err != nil || n != 0 {
		t.Errorf("a legacy key with no expiry was re-keyed (exists=%d err=%v) — the copy would never expire either", n, err)
	}
}

// TestLogoutEndsALegacySession: during the migration window a session may still
// live under the raw key when the user logs out. Destroy must end it there too,
// otherwise logout on a pre-deploy session does nothing.
func TestLogoutEndsALegacySession(t *testing.T) {
	s := valkeySessions(t)
	ctx := context.Background()

	token, err := randomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	cleanupToken(t, s, token)

	if err := s.rdb.Set(ctx, sessionKeyPrefix+token, encodeValue("user-out", time.Now().UTC(), false, false), time.Hour).Err(); err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}
	if err := s.Destroy(ctx, token); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := s.Get(ctx, token); !isRedisNil(err) {
		t.Errorf("Get after logout = %v, want redis.Nil — the legacy key must be destroyed too", err)
	}
	for _, k := range []string{s.sessionKey(token), sessionKeyPrefix + token} {
		if n, err := s.rdb.Exists(ctx, k).Result(); err != nil || n != 0 {
			t.Errorf("key %q survived logout (exists=%d err=%v)", k, n, err)
		}
	}
}

// TestGetOnMissingAndEmptyTokenDoesNotTouchTheKeyspace: an empty cookie value
// must be a miss, not a lookup of the well-known digest of "" (which could only
// ever be a key by accident) — and no read may mint anything.
func TestGetOnMissingAndEmptyTokenDoesNotTouchTheKeyspace(t *testing.T) {
	s := valkeySessions(t)
	ctx := context.Background()

	if _, err := s.Get(ctx, ""); !isRedisNil(err) {
		t.Errorf(`Get("") = %v, want redis.Nil`, err)
	}
	if n, err := s.rdb.Exists(ctx, s.sessionKey("")).Result(); err != nil || n != 0 {
		t.Errorf("a key exists for the empty token (exists=%d err=%v)", n, err)
	}

	missing, err := randomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	if _, err := s.Get(ctx, missing); !isRedisNil(err) {
		t.Errorf("Get(unknown token) = %v, want redis.Nil", err)
	}
	if n, err := s.rdb.Exists(ctx, s.sessionKey(missing), sessionKeyPrefix+missing).Result(); err != nil || n != 0 {
		t.Errorf("reading an unknown token created a key (exists=%d err=%v)", n, err)
	}
}

// TestLogoutDuringMigrationDoesNotResurrect is the race the WATCH exists for.
//
// The migration reads a legacy session and then re-keys it. A logout that lands
// in between must not leave a hashed session behind: that read would resurrect
// a session the user just ended, and it would live for the remaining TTL. The
// interleaving is driven deterministically with a go-redis hook that plays the
// logout from a second connection right after the transaction has read the
// legacy key's TTL — the last instant before EXEC.
func TestLogoutDuringMigrationDoesNotResurrect(t *testing.T) {
	s := valkeySessions(t)
	ctx := context.Background()

	token, err := randomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	cleanupToken(t, s, token)
	if err := s.rdb.Set(ctx, sessionKeyPrefix+token, encodeValue("user-race", time.Now().UTC(), false, false), time.Hour).Err(); err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}

	loggedOut, err := NewSessions(os.Getenv("JOMFORM_TEST_VALKEY_URL"))
	if err != nil {
		t.Fatalf("second valkey client: %v", err)
	}
	defer func() { _ = loggedOut.Close() }()

	hook := &logoutOnLegacyTTL{logout: loggedOut, token: token, legacy: sessionKeyPrefix + token}
	s.rdb.AddHook(hook)

	if _, err := s.Get(ctx, token); !isRedisNil(err) {
		t.Errorf("Get during a concurrent logout = %v, want redis.Nil — the session was ended, not migrated", err)
	}
	if !hook.fired {
		t.Fatal("the logout never interleaved with the migration — the assertion above is vacuous")
	}
	if n, err := s.rdb.Exists(ctx, s.sessionKey(token)).Result(); err != nil || n != 0 {
		t.Errorf("the migration resurrected a logged-out session under the hashed key (exists=%d err=%v)", n, err)
	}
	if n, err := s.rdb.Exists(ctx, sessionKeyPrefix+token).Result(); err != nil || n != 0 {
		t.Errorf("the legacy key survived the logout (exists=%d err=%v)", n, err)
	}
}

// logoutOnLegacyTTL plays a concurrent logout from a second connection the
// first time this client reads the legacy key's TTL.
type logoutOnLegacyTTL struct {
	logout *Sessions
	token  string
	legacy string
	fired  bool
}

func (h *logoutOnLegacyTTL) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h *logoutOnLegacyTTL) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (h *logoutOnLegacyTTL) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if h.fired || err != nil || !strings.EqualFold(cmd.Name(), "pttl") {
			return err
		}
		args := cmd.Args()
		if len(args) < 2 || fmt.Sprint(args[1]) != h.legacy {
			return err
		}
		h.fired = true
		// Destroy() deletes both layouts — exactly what a logout does while the
		// session is still under the raw-token key.
		_ = h.logout.Destroy(context.Background(), h.token)
		return err
	}
}

func isRedisNil(err error) bool { return errors.Is(err, goredis.Nil) }
