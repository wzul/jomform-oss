// Package auth — session management backed by Valkey.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const SessionTTL = 14 * 24 * time.Hour

// Sessions stores browser sessions in Valkey.
type Sessions struct {
	rdb *goredis.Client

	// emailCodeKey is the HMAC key the emailed 2FA backup code is authenticated
	// with (EmailCodeKey). It is optional at construction because most stores
	// sharing this client never touch it; the Email2FA store fails closed when
	// it is absent rather than storing an invertible digest.
	emailCodeKey []byte

	// namespace scopes this store's SESSION keys under `jf:session:<ns>:`. It is
	// empty in production, where the layout is the bare `jf:session:<hash>`, and
	// is set by test harnesses so a test process writes (and can therefore
	// delete) only its own keys on a Valkey DB it shares with every other
	// checkout. See WithKeyNamespace.
	namespace string

	// defaultTTL is what Create (the no-argument-TTL constructor) uses. Zero
	// means SessionTTL — production never sets it; a test harness sets minutes so
	// that the keys it mints through Create expire, not week-long ones. It does
	// NOT reach a session a login handler minted: those pass an explicit TTL to
	// CreateSession (24h for an unticked remember box, 14d for a ticked one), so a
	// killed test run can still leave those behind to drain by expiry — the
	// namespace, not the TTL, is what keeps them from colliding with anyone
	// (card t_39e2b840). See WithDefaultTTL.
	defaultTTL time.Duration

	// trustTTL is what a NEW trusted-device entry lives for. Zero means
	// TrustTTL (30 days) — production never sets it; a test harness sets
	// minutes so a run KILLED before its namespace sweep leaves trust keys that
	// expire in minutes instead of a month. See WithTrustTTL.
	trustTTL time.Duration
}

// SessionOption configures a store that rides on the session Valkey client.
type SessionOption func(*Sessions)

// WithEmailCodeKey wires the server-side key (from EmailCodeKey) that the
// emailed 2FA backup code is authenticated with. Production passes it; without
// it the Email2FA store refuses to arm a code, because a six-digit secret has
// no business being stored under a bare hash.
func WithEmailCodeKey(key []byte) SessionOption {
	return func(s *Sessions) { s.emailCodeKey = key }
}

// WithKeyNamespace scopes the SESSION keys this store mints to
// `jf:session:<ns>:<hash>` instead of `jf:session:<hash>`, so a process can name,
// find and delete exactly the keys it wrote. Production leaves it empty: the
// layout stays the bare hashed key, which is what a running binary's sessions
// have always been.
//
// It exists for the shared TEST Valkey (card t_e4c8ea65). Every test binary that
// mints a session used to write a PRODUCTION key name on a DB shared by every
// checkout, and nothing removed it: the key carried SessionTTL (14d) and the
// process that minted it exited long before then, so the `jf:session:` namespace
// grew without bound (measured at ~98k keys, 95% of that DB) and no run could
// even enumerate its own leftovers to clean them. A namespace turns that into
// "write under your own name, delete under your own name": DeleteNamespaceKeys,
// called from the package's TestMain, removes every key the run left, and no key
// another process wrote is in reach of it.
//
// ns is validated (letters, digits, ':', '-', '_' and '.'; no glob characters, no
// whitespace) because DeleteNamespaceKeys builds a SCAN pattern from it: a `*` in
// the namespace would widen the sweep beyond the keys the caller owns.
func WithKeyNamespace(ns string) SessionOption {
	return func(s *Sessions) { s.namespace = ns }
}

// WithDefaultTTL sets the TTL Create applies to a new session. It is NOT a cap:
// a session minted with an explicit TTL (CreateWithTTL, CreateRememberedWithTTL,
// CreateMagicWithTTL, CreateSession) keeps that TTL — and those are the login
// handlers' paths, which is where production's 24h/14d come from. Zero — the
// production setting — keeps SessionTTL.
func WithDefaultTTL(ttl time.Duration) SessionOption {
	return func(s *Sessions) { s.defaultTTL = ttl }
}

// WithTrustTTL overrides TrustTTL for the trusted-device store this Sessions
// hands out (TrustedDevices). Zero — the production setting — keeps TrustTTL
// (30 days). It exists for the same reason WithDefaultTTL does: the shared test
// Valkey must not accumulate month-long keys a killed run cannot sweep.
func WithTrustTTL(ttl time.Duration) SessionOption {
	return func(s *Sessions) { s.trustTTL = ttl }
}

// validKeyNamespace reports whether ns can be used as one component of a session
// key: a non-empty run of [A-Za-z0-9:._-]. It is deliberately narrower than what
// Valkey allows, because DeleteNamespaceKeys appends `*` to it and SCANs; a glob
// character or a space in the namespace would escape the caller's own keyspace.
func validKeyNamespace(ns string) bool {
	if ns == "" {
		return false
	}
	for _, r := range ns {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ':', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// NewSessions wires a Valkey client. go-redis only accepts redis:// /
// rediss:// URL schemes, so a valkey:// prefix (RESP-compatible, same wire
// protocol) is transparently rewritten before parsing.
func NewSessions(valkeyURL string, opts ...SessionOption) (*Sessions, error) {
	normalized := strings.TrimPrefix(valkeyURL, "valkey://")
	normalized = "redis://" + normalized
	o, err := goredis.ParseURL(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse valkey url: %w", err)
	}
	s := &Sessions{rdb: goredis.NewClient(o)}
	for _, opt := range opts {
		opt(s)
	}
	if s.namespace != "" && !validKeyNamespace(s.namespace) {
		_ = s.rdb.Close()
		return nil, fmt.Errorf("session key namespace %q is not a plain key component (letters, digits, ':._-' only)", s.namespace)
	}
	return s, nil
}

// ─── session key layout ─────────────────────────────────────────────────────
//
// The session cookie carries the raw 256-bit token; the keyspace only ever
// holds its SHA-256. Keying a session by the raw token made the key itself a
// full login — anything that could read the keyspace (a dump, a snapshot/AOF
// file, MONITOR) could present the token as the cookie for up to SessionTTL.
// Every other credential in this file (password reset, trust cookie, step-up
// grant) was already stored hashed; the session was the one that was not. The
// token is 256 bits of `crypto/rand`, so a plain SHA-256 is enough: there is
// nothing to brute-force and no secret to rotate (which is why this needs no
// SESSION_SECRET-style key).
//
// sessionKeyPrefix is the root of the session keyspace in EVERY deployment. A
// test store may insert a namespace after it (WithKeyNamespace) to own its keys
// on a shared DB; production's layout is the bare `jf:session:<hash>`.
const sessionKeyPrefix = "jf:session:"

// namespacePrefix inserts a store's namespace after a key family's ROOT:
// `jf:magic:<ns>:` for a namespaced store, the bare root in production
// (ns == ""). Every store that hangs off Sessions builds its keys through it,
// which is what keeps the production layout byte-identical while a namespaced
// test store owns every family it writes (card t_6004c826).
func namespacePrefix(root, ns string) string {
	if ns == "" {
		return root
	}
	return root + ns + ":"
}

// keyPrefix is this store's session key root, e.g. "jf:session:" in production
// or "jf:session:jftest:p48213:" for a namespaced test store.
func (s *Sessions) keyPrefix() string { return namespacePrefix(sessionKeyPrefix, s.namespace) }

// KeyNamespace reports the namespace this store writes every key family under,
// or "" for the production layout. A test harness uses it to say which slice of
// a shared DB it owns (and to quote it in a failure message) without repeating
// the key layout.
func (s *Sessions) KeyNamespace() string { return s.namespace }

// DeleteNamespaceKeys deletes every key this store minted under its namespace —
// sessions, pending-2FA, step-up grants, password resets, magic links and the
// emailed 2FA code/pending flags — and returns how many it removed. It is the
// exit sweep a test harness calls from TestMain, and it is only safe because
// the namespace is this process's own: every SCAN pattern is built from it, so
// the keys of a concurrent checkout (a different namespace) and every
// production-layout key are out of reach.
//
// It REFUSES to run on an unscoped store — namespace "" would make the patterns
// `jf:session:*`, `jf:magic:*`, `jf:totp_pending:*`, … i.e. a sweep of live
// credentials of anything else sharing the DB. That refusal is the property
// that keeps this from becoming the FlushDB the test suite spent several cards
// removing.
//
// The derived stores (PendingTOTP, StepUp, PasswordReset, MagicLink, Email2FA)
// inherit the namespace, so this ONE call covers every family a process can
// write; before t_6004c826 they ignored it and minted production-layout keys
// the sweep could not see. TrustedDevices keeps its own sweep because its three
// families are a separate store with a separate TTL.
//
// SCAN, not KEYS: the shared test DB is being served while this runs.
func (s *Sessions) DeleteNamespaceKeys(ctx context.Context) (int, error) {
	if s.namespace == "" {
		return 0, errors.New("auth: DeleteNamespaceKeys needs a key namespace; an unscoped store would sweep the whole session keyspace")
	}
	families := []struct{ family, root string }{
		{"session", sessionKeyPrefix},
		{"totp_pending", PendingTOTPKeyPrefix},
		{"stepup", StepUpKeyPrefix},
		{"pwreset", PasswordResetKeyPrefix},
		{"magic", MagicLinkKeyPrefix},
		{"2faemail", Email2FACodeKeyPrefix},
		{"2fa_pending", Email2FAPendingPrefix},
	}
	deleted := 0
	for _, fam := range families {
		pattern := namespacePrefix(fam.root, s.namespace) + "*"
		// Defensive: the pattern must still be scoped to this namespace after
		// the string building above, whatever the family.
		if !strings.HasPrefix(pattern, fam.root+s.namespace+":") {
			return deleted, fmt.Errorf("auth: refusing to sweep %q: not scoped to namespace %q", pattern, s.namespace)
		}
		var cursor uint64
		for {
			keys, next, err := s.rdb.Scan(ctx, cursor, pattern, 500).Result()
			if err != nil {
				return deleted, fmt.Errorf("scan %s keys %q: %w", fam.family, pattern, err)
			}
			if len(keys) > 0 {
				n, err := s.rdb.Del(ctx, keys...).Result()
				deleted += int(n)
				if err != nil {
					return deleted, fmt.Errorf("delete %d %s key(s) under %q: %w", len(keys), fam.family, pattern, err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	return deleted, nil
}

// sessionKey namespaces a session by the SHA-256 of its token.
func (s *Sessions) sessionKey(token string) string { return s.keyPrefix() + hashOf(token) }

// legacySessionKey is the pre-hash layout: the raw token AS the key name. It is
// only ever READ, to migrate a session minted before the re-keying (see
// getSessionValue) so a deploy does not log everyone out. Every legacy key
// carries at most SessionTTL, so this path drains by itself one TTL after the
// deploy — at that point it can be deleted outright.
func (s *Sessions) legacySessionKey(token string) string { return s.keyPrefix() + token }

// getSessionValue returns the stored session value for a RAW token (the value
// the cookie carries).
//
// It reads the hashed key and, on a miss only, falls back to the legacy
// raw-token key so pre-deploy sessions keep working. A legacy hit is migrated
// in the same call: re-SET under the hashed key with the REMAINING TTL (so a
// remember-me session keeps its deadline instead of being extended to a fresh
// SessionTTL) and DEL the raw key. The migration runs under WATCH, so it can
// never resurrect a session that was logged out between the read and the write:
// if the legacy key changes underneath it, EXEC aborts and the answer is
// re-read from the hashed key, which is authoritative.
func (s *Sessions) getSessionValue(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", goredis.Nil
	}
	v, err := s.rdb.Get(ctx, s.sessionKey(token)).Result()
	if err == nil || !errors.Is(err, goredis.Nil) {
		return v, err
	}
	return s.migrateLegacySession(ctx, token)
}

// migrateLegacySession reads a pre-deploy (raw-token-keyed) session, re-keys it
// by the token's hash with its remaining TTL, deletes the raw key and returns
// the value. redis.Nil means there is no session under either layout.
func (s *Sessions) migrateLegacySession(ctx context.Context, token string) (string, error) {
	legacy := s.legacySessionKey(token)
	var value string
	err := s.rdb.Watch(ctx, func(tx *goredis.Tx) error {
		v, gerr := tx.Get(ctx, legacy).Result()
		if gerr != nil {
			return gerr
		}
		ttl, terr := tx.PTTL(ctx, legacy).Result()
		if terr != nil {
			return terr
		}
		if ttl <= 0 {
			// The key expired between GET and PTTL (or carries no expiry to
			// carry over). Serve the value we read — the session was live a
			// moment ago — but do not mint a durable copy of it.
			value = v
			return nil
		}
		value = v
		_, perr := tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
			pipe.Set(ctx, s.sessionKey(token), v, ttl)
			pipe.Del(ctx, legacy)
			return nil
		})
		return perr
	}, legacy)
	if errors.Is(err, goredis.TxFailedErr) {
		// The legacy key moved under us: another request migrated it (the
		// hashed key now holds the session) or a logout deleted it. Only the
		// hashed key may answer.
		return s.rdb.Get(ctx, s.sessionKey(token)).Result()
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// value encodes the stored userID together with the session's issued-at
// timestamp (unix ns), the remember flag (issue #69) and the magic flag
// (issue #458). Formats over time:
//   - legacy v1: bare userID (no separator)
//   - v2:        userID:issuedNs
//   - v3:        userID:issuedNs:remember  (remember "1"/"0")
//   - v4 (now):  userID:issuedNs:remember:magic  (magic "1"/"0")
//
// The remember flag is persisted so a session-rotating action (2FA enable per
// issue #69) can inherit the original session's persistence choice instead of
// silently downgrading a "remember me" login to a 24h browser-session cookie.
//
// The magic flag (issue #458) records that the session was started by a
// passwordless magic-link click rather than a password/social/passkey login.
// It is a pure marker: it changes no UX on its own, but it makes the login
// method observable in the audit log and lets sensitive actions require a
// fresh second factor (TOTP step-up) when a magic session belongs to a
// 2FA-enabled user.
const valueSep = ":"

func encodeValue(userID string, issued time.Time, remember, magic bool) string {
	flag := "0"
	if remember {
		flag = "1"
	}
	mflag := "0"
	if magic {
		mflag = "1"
	}
	return userID + valueSep + strconv.FormatInt(issued.UnixNano(), 10) + valueSep + flag + valueSep + mflag
}

// parseValue returns the userID, issuance time, remember flag and magic flag.
// It tolerates every historical value format: a v1/v2/v3 value carries no
// magic component and is therefore reported as magic=false (the safe default —
// an unknown login method never gets the step-up exemption). A value carrying
// a zero issued time (bare or malformed) means "issued unknown" and defaults
// remember to false (the safe, short-session default).
func parseValue(v string) (userID string, issued time.Time, remember, magic bool) {
	parts := strings.Split(v, valueSep)
	if len(parts) > 0 {
		userID = parts[0]
	}
	if len(parts) >= 2 {
		if ns, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			issued = time.Unix(0, ns).UTC()
		}
	}
	if len(parts) >= 3 && parts[2] == "1" {
		remember = true
	}
	if len(parts) >= 4 && parts[3] == "1" {
		magic = true
	}
	return userID, issued, remember, magic
}

// createTTL is the TTL a created session gets: the store's WithDefaultTTL when
// it was set, SessionTTL otherwise.
func (s *Sessions) createTTL() time.Duration {
	if s.defaultTTL > 0 {
		return s.defaultTTL
	}
	return SessionTTL
}

// Create stores userID under a fresh opaque token with the store's default TTL —
// SessionTTL (14d) unless WithDefaultTTL set a shorter one — and returns it.
func (s *Sessions) Create(ctx context.Context, userID string) (string, error) {
	return s.CreateWithTTL(ctx, userID, s.createTTL())
}

// CreateWithTTL stores userID under a fresh opaque token with the given TTL
// and returns the token. TTL drives both the Valkey expiry and (caller-side)
// the cookie MaxAge, so remember-me works end to end (issue #62). Remember is
// stored into the session value so rotation can inherit it (issue #69).
func (s *Sessions) CreateWithTTL(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	return s.CreateRememberedWithTTL(ctx, userID, ttl, false)
}

// CreateRememberedWithTTL stores userID under a fresh opaque token with the
// given TTL and remember flag. Same contract as CreateWithTTL plus persisting
// remember so a later session rotation can preserve the persistence choice.
// The session is not a magic session (magic=false).
func (s *Sessions) CreateRememberedWithTTL(ctx context.Context, userID string, ttl time.Duration, remember bool) (string, error) {
	return s.CreateSession(ctx, userID, ttl, remember, false)
}

// CreateMagicWithTTL is CreateRememberedWithTTL for a session started by a
// passwordless magic-link click (issue #458): the persisted value carries
// magic=true so the login method is observable and the step-up gate can fire.
func (s *Sessions) CreateMagicWithTTL(ctx context.Context, userID string, ttl time.Duration, remember bool) (string, error) {
	return s.CreateSession(ctx, userID, ttl, remember, true)
}

// CreateSession stores userID under a fresh opaque token with the given TTL,
// remember flag and magic flag.
func (s *Sessions) CreateSession(ctx context.Context, userID string, ttl time.Duration, remember, magic bool) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	if err := s.rdb.Set(ctx, s.sessionKey(token), encodeValue(userID, time.Now().UTC(), remember, magic), ttl).Err(); err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}
	return token, nil
}

// Get resolves a session token to its userID.
func (s *Sessions) Get(ctx context.Context, token string) (string, error) {
	v, err := s.getSessionValue(ctx, token)
	if err != nil {
		return "", err
	}
	uid, _, _, _ := parseValue(v)
	return uid, nil
}

// GetCreated resolves a session token to its userID and issuance time. It is
// used by session resolution to reject sessions older than a password reset.
func (s *Sessions) GetCreated(ctx context.Context, token string) (string, time.Time, error) {
	v, err := s.getSessionValue(ctx, token)
	if err != nil {
		return "", time.Time{}, err
	}
	uid, issued, _, _ := parseValue(v)
	return uid, issued, nil
}

// Remembered resolves a session token to its persisted remember flag. A
// missing/legacy session is treated as remember=false (24h/browser-session) —
// the safe default. Used by session-rotating handlers (2FA enable, issue #69)
// to inherit the original login's persistence choice.
func (s *Sessions) Remembered(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	v, err := s.getSessionValue(ctx, token)
	if err != nil {
		return false
	}
	_, _, remember, _ := parseValue(v)
	return remember
}

// Magic reports whether a session token was started by a passwordless
// magic-link click (issue #458). A missing session, an expired session, or any
// legacy v1/v2/v3 value reports false — the safe default, since an unknown
// login method must never be exempted from the sensitive-action step-up gate.
func (s *Sessions) Magic(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	v, err := s.getSessionValue(ctx, token)
	if err != nil {
		return false
	}
	_, _, _, magic := parseValue(v)
	return magic
}

// ValidAfter reports whether the session's issuance time is not before cut —
// used to reject sessions created before a pw_reset_at timestamp. A session
// with an unknown (zero) issuance time is treated as valid only when cut is
// zero (the user has never reset), so existing sessions are never locked out.
func ValidAfter(issued time.Time, cut time.Time) bool {
	if cut.IsZero() {
		return true
	}
	if issued.IsZero() {
		// Legacy (pre-upgrade) session value carries no timestamp. Only reach
		// here when the user has reset (cut != 0); such a session predates the
		// reset by construction, so reject it conservatively.
		return false
	}
	return issued.After(cut) || issued.Equal(cut)
}

// Destroy removes a session (logout). Both layouts are deleted: the hashed key
// and — during the one-TTL migration window — the legacy raw-token key, so a
// logout also ends a session that was minted before the re-keying and has not
// been migrated yet.
func (s *Sessions) Destroy(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.rdb.Del(ctx, s.sessionKey(token), s.legacySessionKey(token)).Err()
}

// TTL reports the remaining lifetime of a session token, following the redis
// PTTL convention: a negative duration means the key carries no expiry (-1) or
// does not exist (-2). It exists so callers that need the server-side deadline
// (the remember-me cookie tests) do not have to know — or reproduce — the key
// layout.
func (s *Sessions) TTL(ctx context.Context, token string) (time.Duration, error) {
	if token == "" {
		return -2, nil
	}
	return s.rdb.PTTL(ctx, s.sessionKey(token)).Result()
}

// Ping verifies the Valkey connection for readiness probes.
func (s *Sessions) Ping(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}

// Client exposes the underlying Valkey client so the login-failure limiter can
// reuse the same connection pool (issue #42).
func (s *Sessions) Client() *goredis.Client { return s.rdb }

// Close releases the connection pool.
func (s *Sessions) Close() error { return s.rdb.Close() }

// PendingTOTP stores a generated-but-not-yet-enabled 2FA secret in Valkey.
// The secret only becomes active (DB) after the user confirms a valid code,
// so an abandoned setup never arms the login challenge.
const (
	PendingTOTPKeyPrefix = "jf:totp_pending:"
	PendingTOTPTTL       = 10 * time.Minute
)

func (s *PendingTOTP) key(userID string) string {
	return namespacePrefix(PendingTOTPKeyPrefix, s.namespace) + userID
}

// PendingTOTP exposes a pending-2FA store sharing the session client. It
// inherits the store's key namespace, so a namespaced test store writes
// `jf:totp_pending:<ns>:<userID>` and the exit sweep can reclaim it.
func (s *Sessions) PendingTOTP() *PendingTOTP {
	return &PendingTOTP{rdb: s.rdb, namespace: s.namespace}
}

type PendingTOTP struct {
	rdb *goredis.Client
	// namespace scopes this store's keys under `jf:totp_pending:<ns>:`. Empty
	// in production. See Sessions.WithKeyNamespace.
	namespace string
}

// Set stores the pending secret with a short TTL.
func (p *PendingTOTP) Set(ctx context.Context, userID, secret string) error {
	return p.rdb.Set(ctx, p.key(userID), secret, PendingTOTPTTL).Err()
}

// Get returns the pending secret (redis.Nil when none or expired).
func (p *PendingTOTP) Get(ctx context.Context, userID string) (string, error) {
	return p.rdb.Get(ctx, p.key(userID)).Result()
}

// Delete removes the pending secret (used after successful enable).
func (p *PendingTOTP) Delete(ctx context.Context, userID string) error {
	return p.rdb.Del(ctx, p.key(userID)).Err()
}

// ─── step-up grants (issue #458) ────────────────────────────────────────────
//
// A step-up grant records that the holder of a given session has just proved a
// second factor (a fresh TOTP code) for a sensitive action. It is keyed by the
// session token's hash and lives for a few minutes, so a multi-step flow (e.g.
// WebAuthn passkey registration: /start then /finish) only asks for the code
// once. Grants are per-session and short-lived; they are never a session and
// never survive a logout (the session key is destroyed, but the grant expires
// on its own TTL anyway).

const (
	StepUpKeyPrefix = "jf:stepup:"
	StepUpTTL       = 5 * time.Minute
)

func (s *StepUp) key(sessionToken string) string {
	return namespacePrefix(StepUpKeyPrefix, s.namespace) + hashOf(sessionToken)
}

// StepUp exposes the short-lived step-up grant store sharing the session
// client. It inherits the store's key namespace, so a namespaced test store's
// grants are swept with its sessions.
func (s *Sessions) StepUp() *StepUp { return &StepUp{rdb: s.rdb, namespace: s.namespace} }

type StepUp struct {
	rdb *goredis.Client
	// namespace scopes this store's keys under `jf:stepup:<ns>:`. Empty in
	// production. See Sessions.WithKeyNamespace.
	namespace string
}

// Grant records a fresh step-up for the session token (TTL StepUpTTL).
func (s *StepUp) Grant(ctx context.Context, sessionToken, userID string) error {
	if sessionToken == "" {
		return errors.New("stepup: empty session token")
	}
	return s.rdb.Set(ctx, s.key(sessionToken), userID, StepUpTTL).Err()
}

// Granted reports whether the session token carries a live step-up grant for
// userID.
func (s *StepUp) Granted(ctx context.Context, sessionToken, userID string) bool {
	if sessionToken == "" || userID == "" {
		return false
	}
	v, err := s.rdb.Get(ctx, s.key(sessionToken)).Result()
	if err != nil {
		return false
	}
	return v == userID
}

// Consume removes a step-up grant (single-use, after the sensitive action
// completes).
func (s *StepUp) Consume(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	return s.rdb.Del(ctx, s.key(sessionToken)).Err()
}

// ─── shared helpers ──────────────────────────────────────────────────────────

// randomToken returns a hex-encoded 32-byte (256-bit) random token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashOf returns the hex SHA-256 of a token. Every credential this file stores
// is keyed by it — sessions, password resets, trust cookies, step-up grants — so
// a Valkey leak (dump, snapshot/AOF file, MONITOR) never exposes a usable
// token: the raw value exists only in the cookie or the emailed link.
func hashOf(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ─── password-reset tokens (issue #62) ──────────────────────────────────────
//
// The reset link ships the raw 256-bit token to the user's inbox; only its
// SHA-256 is stored in Valkey, TTL 30 minutes, single-use (DEL on consume).

const (
	PasswordResetKeyPrefix = "jf:pwreset:"
	PasswordResetTTL       = 30 * time.Minute
)

func (p *PasswordReset) key(hash string) string {
	return namespacePrefix(PasswordResetKeyPrefix, p.namespace) + hash
}

// PasswordReset exposes a single-use, hashed, 30-min password-reset token
// store backed by the shared Valkey client. It inherits the store's key
// namespace, so a namespaced test store's reset tokens are swept with it.
func (s *Sessions) PasswordReset() *PasswordReset {
	return &PasswordReset{rdb: s.rdb, namespace: s.namespace}
}

type PasswordReset struct {
	rdb *goredis.Client
	// namespace scopes this store's keys under `jf:pwreset:<ns>:`. Empty in
	// production. See Sessions.WithKeyNamespace.
	namespace string
}

// Generate mints a fresh raw token, stores its hash→userID mapping and returns
// the raw token (the only copy of the usable secret).
func (p *PasswordReset) Generate(ctx context.Context, userID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := p.rdb.Set(ctx, p.key(hashOf(token)), userID, PasswordResetTTL).Err(); err != nil {
		return "", fmt.Errorf("pwreset set: %w", err)
	}
	return token, nil
}

// ErrResetTokenInvalid is returned when the token is absent, expired, or
// already consumed.
var ErrResetTokenInvalid = errors.New("password reset token invalid")

// Consume atomically invalidates the token and returns the userID it was
// issued to. A single-use token can never be replayed.
func (p *PasswordReset) Consume(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrResetTokenInvalid
	}
	key := p.key(hashOf(token))
	// GET before DEL so we only delete when the mapping actually existed
	// (redis handles this atomically under a script, but the token hash is a
	// unique key so GET→DEL is race-safe here: a second consumer would GET
	// redis.Nil after the first DEL).
	userID, err := p.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrResetTokenInvalid
		}
		return "", err
	}
	if err := p.rdb.Del(ctx, key).Err(); err != nil {
		return "", err
	}
	return userID, nil
}

// ─── trusted devices (issue #63 + #454) ─────────────────────────────────────
//
// jf_trust is a single-purpose device cookie that waives TOTP on subsequent
// logins (password still required every time). Raw token lives in the cookie;
// only its SHA-256 is stored. A per-user set key makes invalidation trivial.
//
// Issue #454 adds device metadata so Profile → Security can LIST each trusted
// device (label, IP, first trusted, expiry) and revoke one by id, instead of
// only being able to forget the current device. Layout in Valkey:
//
//	jf:trust:{hash}            → userID                  (TTL 30d, unchanged)
//	jf:trustset:{userID}       → SET of hashes           (unchanged)
//	jf:trustmeta:{userID}      → HASH hash → metaJSON    (new, TTL 30d)
//
// The metadata hash is best-effort: a trust entry minted before #454 (or one
// whose metadata expired) still authorizes logins — it just shows up in the
// list with an unknown label/ip and is revocable by its hash id. The list is
// always reconciled against the authoritative trust set, so a revoked/expired
// device never lingers in the UI.

const (
	TrustKeyPrefix     = "jf:trust:"
	TrustSetKeyPrefix  = "jf:trustset:"
	TrustMetaKeyPrefix = "jf:trustmeta:"
	TrustTTL           = 30 * 24 * time.Hour
)

// ─── trusted-device key layout ──────────────────────────────────────────────
//
// A trust entry is THREE keys, not one: the token key (hash → userID), the
// user's SET of live hashes, and the user's metadata HASH. Their prefixes are
// deliberately distinct from each other (note `jf:trust:` does not prefix
// `jf:trustset:` — the character after "trust" is `s`, not `:`), which is what
// lets a namespace sweep name each family separately and still cannot reach a
// key it did not write.
//
// The namespace sits immediately after the family prefix, exactly as it does
// for sessions: `jf:trust:jftest:p42:<hash>` in a test process, the bare
// `jf:trust:<hash>` in production (namespace empty). See WithKeyNamespace and
// TrustedDevices.DeleteNamespaceKeys.
func (t *TrustedDevices) trustKeyPrefix() string {
	if t.namespace == "" {
		return TrustKeyPrefix
	}
	return TrustKeyPrefix + t.namespace + ":"
}

func (t *TrustedDevices) trustSetPrefix() string {
	if t.namespace == "" {
		return TrustSetKeyPrefix
	}
	return TrustSetKeyPrefix + t.namespace + ":"
}

func (t *TrustedDevices) trustMetaPrefix() string {
	if t.namespace == "" {
		return TrustMetaKeyPrefix
	}
	return TrustMetaKeyPrefix + t.namespace + ":"
}

// trustKey namespaces one entry by the SHA-256 of its raw token.
func (t *TrustedDevices) trustKey(hash string) string { return t.trustKeyPrefix() + hash }

// trustSetKey is the SET holding the user's live device hashes. It carries the
// same TTL as the entries so it cannot outlive them as an empty set.
func (t *TrustedDevices) trustSetKey(userID string) string { return t.trustSetPrefix() + userID }

// trustMetaKey is the HASH of device metadata, one field per device hash.
func (t *TrustedDevices) trustMetaKey(userID string) string { return t.trustMetaPrefix() + userID }

// trustTTL is the lifetime a NEW trust entry gets: the store's WithTrustTTL
// when a test harness set one, TrustTTL (30 days) in production.
func (t *TrustedDevices) trustTTL() time.Duration {
	if t.ttl <= 0 {
		return TrustTTL
	}
	return t.ttl
}

// DeleteNamespaceKeys deletes every trust key this store minted — the token
// keys, the per-user sets AND the per-user metadata hashes — and returns how
// many it removed. It is the exit sweep a test harness calls from TestMain, and
// it is only safe because the namespace is the process's own: each SCAN pattern
// is built from it, so a concurrent checkout's keys (a different namespace) and
// every production-layout key are out of reach.
//
// It refuses an unscoped store for the same reason Sessions.DeleteNamespaceKeys
// does: namespace "" would make the patterns `jf:trust:*`, `jf:trustset:*` and
// `jf:trustmeta:*` — a sweep of every trusted device a live API is serving,
// i.e. the FlushDB this suite spent several cards removing.
//
// The SET is deleted as a whole (not just its members), so it cannot linger as
// an empty set after its member token keys are gone.
//
// SCAN, not KEYS: the shared test DB is being served while this runs.
func (t *TrustedDevices) DeleteNamespaceKeys(ctx context.Context) (int, error) {
	if t.namespace == "" {
		return 0, errors.New("auth: TrustedDevices.DeleteNamespaceKeys needs a key namespace; an unscoped store would sweep every live trusted device")
	}
	roots := []struct{ family, prefix, root string }{
		{"token", t.trustKeyPrefix(), TrustKeyPrefix},
		{"set", t.trustSetPrefix(), TrustSetKeyPrefix},
		{"meta", t.trustMetaPrefix(), TrustMetaKeyPrefix},
	}
	deleted := 0
	for _, fam := range roots {
		pattern := fam.prefix + "*"
		// Defensive: the pattern must still be scoped to this namespace after
		// the string building above, whatever the family.
		if !strings.HasPrefix(pattern, fam.root+t.namespace+":") {
			return deleted, fmt.Errorf("auth: refusing to sweep %q: not scoped to namespace %q", pattern, t.namespace)
		}
		var cursor uint64
		for {
			keys, next, err := t.rdb.Scan(ctx, cursor, pattern, 500).Result()
			if err != nil {
				return deleted, fmt.Errorf("scan %s keys %q: %w", fam.family, pattern, err)
			}
			if len(keys) > 0 {
				n, err := t.rdb.Del(ctx, keys...).Result()
				deleted += int(n)
				if err != nil {
					return deleted, fmt.Errorf("delete %d %s key(s) under %q: %w", len(keys), fam.family, pattern, err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	return deleted, nil
}

// TrustDevice is one trusted device as shown in Profile → Security.
type TrustDevice struct {
	// ID is the public, opaque identifier for the device (the SHA-256 of the
	// raw trust token). It is what DELETE /v1/auth/trust/{id} accepts.
	ID string `json:"id"`
	// Label is a short human label derived from the device's user-agent
	// (e.g. "Chrome · macOS"). Empty for legacy entries with no metadata.
	Label string `json:"label,omitempty"`
	// UserAgent is the raw UA recorded when the device was trusted.
	UserAgent string `json:"user_agent,omitempty"`
	// IP is the client IP recorded when the device was trusted.
	IP string `json:"ip,omitempty"`
	// FirstSeen is when the device was first trusted (RFC3339).
	FirstSeen string `json:"first_seen,omitempty"`
	// ExpiresAt is when the trust entry expires (RFC3339).
	ExpiresAt string `json:"expires_at,omitempty"`
	// Current marks the device making the request, so the UI can label it.
	Current bool `json:"current,omitempty"`
}

// trustMeta is the JSON blob stored per device hash.
type trustMeta struct {
	Label     string `json:"label,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	IP        string `json:"ip,omitempty"`
	FirstSeen string `json:"first_seen,omitempty"`
}

// DeviceLabel derives a short, human-readable label from a user-agent string
// (e.g. "Chrome · macOS", "Safari · iPhone"). Unrecognised UAs fall back to a
// trimmed prefix of the raw UA. An empty UA yields "" (the UI then shows a
// generic "unknown device" label).
func DeviceLabel(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return ""
	}
	browser := ""
	switch {
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "EdgA/") || strings.Contains(ua, "EdgiOS/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	case strings.Contains(ua, "curl/"):
		browser = "curl"
	case strings.Contains(ua, "python") || strings.Contains(ua, "Go-http-client"):
		browser = "API client"
	}
	osName := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		osName = "iPhone"
	case strings.Contains(ua, "iPad"):
		osName = "iPad"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		osName = "macOS"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "CrOS"):
		osName = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	switch {
	case browser != "" && osName != "":
		return browser + " · " + osName
	case browser != "":
		return browser
	case osName != "":
		return osName
	}
	if len(ua) > 48 {
		return ua[:48] + "…"
	}
	return ua
}

// TrustedDevices exposes the trust-cookie store sharing the session client.
//
// It inherits the store's key namespace and trusted-device TTL, so a test store
// built WithKeyNamespace writes `jf:trust:<ns>:<hash>` (and the matching
// trustset/trustmeta keys) and can delete exactly that slice on exit — the
// production layout is unchanged because the namespace is empty there.
func (s *Sessions) TrustedDevices() *TrustedDevices {
	return &TrustedDevices{rdb: s.rdb, namespace: s.namespace, ttl: s.trustTTL}
}

type TrustedDevices struct {
	rdb *goredis.Client

	// namespace scopes this store's THREE trust key families under
	// `jf:trust:<ns>:`, `jf:trustset:<ns>:` and `jf:trustmeta:<ns>:`. Empty in
	// production. See Sessions.WithKeyNamespace.
	namespace string

	// ttl overrides TrustTTL for entries this store mints. Zero means TrustTTL.
	ttl time.Duration
}

// Issue mints a fresh trust token, stores hash→userID (TTL 30d), adds it to
// the user's trust-set, records the device metadata and returns the raw token
// for the cookie. ua/ip are best-effort metadata for the #454 device list.
func (t *TrustedDevices) Issue(ctx context.Context, userID string) (string, error) {
	return t.IssueDevice(ctx, userID, "", "")
}

// IssueDevice is Issue with device metadata (issue #454). Legacy callers keep
// using Issue; the device list then shows the entry with an unknown label.
func (t *TrustedDevices) IssueDevice(ctx context.Context, userID, ua, ip string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	h := hashOf(token)
	meta, merr := json.Marshal(trustMeta{
		Label: DeviceLabel(ua), UserAgent: ua, IP: ip,
		FirstSeen: time.Now().UTC().Format(time.RFC3339),
	})
	pipe := t.rdb.TxPipeline()
	ttl := t.trustTTL()
	pipe.Set(ctx, t.trustKey(h), userID, ttl)
	pipe.SAdd(ctx, t.trustSetKey(userID), h)
	pipe.Expire(ctx, t.trustSetKey(userID), ttl)
	if merr == nil {
		pipe.HSet(ctx, t.trustMetaKey(userID), h, string(meta))
		pipe.Expire(ctx, t.trustMetaKey(userID), ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("trust issue: %w", err)
	}
	return token, nil
}

// UserIDFor returns the userID a raw trust token maps to, or "" if invalid.
func (t *TrustedDevices) UserIDFor(ctx context.Context, token string) string {
	if token == "" {
		return ""
	}
	userID, err := t.rdb.Get(ctx, t.trustKey(hashOf(token))).Result()
	if err != nil {
		return ""
	}
	return userID
}

// HashFor returns the public device id (hash) for a raw trust token, or "".
func (t *TrustedDevices) HashFor(token string) string {
	if token == "" {
		return ""
	}
	return hashOf(token)
}

// Expiry returns the absolute expiry time of a trust token, or zero time if
// the token is unknown/expired. Used by the Profile → Security line to show an
// exact "trusted until {date}".
func (t *TrustedDevices) Expiry(ctx context.Context, token string) time.Time {
	if token == "" {
		return time.Time{}
	}
	ttl, err := t.rdb.TTL(ctx, t.trustKey(hashOf(token))).Result()
	if err != nil || ttl <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(ttl)
}

// List returns every live trusted device for a user, newest-expiry first. It
// reconciles the set against the trust keys (dropping stale/expired members)
// so the UI never shows a device that can no longer skip TOTP. currentHash (if
// non-empty) marks the requesting device.
func (t *TrustedDevices) List(ctx context.Context, userID, currentHash string) ([]TrustDevice, error) {
	members, err := t.rdb.SMembers(ctx, t.trustSetKey(userID)).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	// Best-effort metadata for all members in one round-trip.
	metas, _ := t.rdb.HGetAll(ctx, t.trustMetaKey(userID)).Result()
	pipe := t.rdb.Pipeline()
	ttlCmds := make(map[string]*goredis.DurationCmd, len(members))
	for _, h := range members {
		ttlCmds[h] = pipe.TTL(ctx, t.trustKey(h))
	}
	_, _ = pipe.Exec(ctx)

	out := make([]TrustDevice, 0, len(members))
	stale := make([]string, 0)
	for _, h := range members {
		ttl, terr := ttlCmds[h].Result()
		if terr != nil || ttl <= 0 {
			// Key gone or expired → drop the stale set member.
			stale = append(stale, h)
			continue
		}
		d := TrustDevice{
			ID:        h,
			ExpiresAt: time.Now().UTC().Add(ttl).Format(time.RFC3339),
			Current:   currentHash != "" && h == currentHash,
		}
		if raw, ok := metas[h]; ok {
			var m trustMeta
			if json.Unmarshal([]byte(raw), &m) == nil {
				d.Label, d.UserAgent, d.IP, d.FirstSeen = m.Label, m.UserAgent, m.IP, m.FirstSeen
			}
		}
		out = append(out, d)
	}
	if len(stale) > 0 {
		// Keep the set + metadata hash tidy (revoked/expired devices).
		args := make([]any, 0, len(stale))
		for _, h := range stale {
			args = append(args, h)
		}
		pipe2 := t.rdb.TxPipeline()
		pipe2.SRem(ctx, t.trustSetKey(userID), args...)
		pipe2.HDel(ctx, t.trustMetaKey(userID), stale...)
		_, _ = pipe2.Exec(ctx)
	}
	// Sort by expiry ascending (soonest to expire first) for a stable UI.
	sort.SliceStable(out, func(i, j int) bool { return out[i].ExpiresAt < out[j].ExpiresAt })
	return out, nil
}

// RevokeHash deletes one trust entry by its public device id (hash) and removes
// it from the user's set + metadata. Immediate — the very next login on that
// device is challenged for TOTP again (issue #454).
func (t *TrustedDevices) RevokeHash(ctx context.Context, userID, hash string) error {
	if hash == "" {
		return nil
	}
	// Only revoke a device that actually belongs to this user's set.
	ok, err := t.rdb.SIsMember(ctx, t.trustSetKey(userID), hash).Result()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	pipe := t.rdb.TxPipeline()
	pipe.Del(ctx, t.trustKey(hash))
	pipe.SRem(ctx, t.trustSetKey(userID), hash)
	pipe.HDel(ctx, t.trustMetaKey(userID), hash)
	_, err = pipe.Exec(ctx)
	return err
}

// Revoke deletes the trust entry (by raw token) and removes it from the
// user's set — used by "forget this device".
func (t *TrustedDevices) Revoke(ctx context.Context, userID, token string) error {
	h := hashOf(token)
	pipe := t.rdb.TxPipeline()
	pipe.Del(ctx, t.trustKey(h))
	pipe.SRem(ctx, t.trustSetKey(userID), h)
	pipe.HDel(ctx, t.trustMetaKey(userID), h)
	_, err := pipe.Exec(ctx)
	return err
}

// RevokeAll deletes every trust entry for a user — called on password reset
// and 2FA disable.
func (t *TrustedDevices) RevokeAll(ctx context.Context, userID string) error {
	members, err := t.rdb.SMembers(ctx, t.trustSetKey(userID)).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		pipe := t.rdb.TxPipeline()
		pipe.Del(ctx, t.trustSetKey(userID))
		pipe.Del(ctx, t.trustMetaKey(userID))
		_, err := pipe.Exec(ctx)
		return err
	}
	keys := make([]string, 0, len(members)+1)
	for _, h := range members {
		keys = append(keys, t.trustKey(h))
	}
	keys = append(keys, t.trustSetKey(userID))
	pipe := t.rdb.TxPipeline()
	pipe.Del(ctx, keys...)
	pipe.Del(ctx, t.trustMetaKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}
