package dbstore

// Real-Postgres tests for the auth state machines (card t_df243a6e): the token
// pair, the authorization code and the API key each derive their state ONCE, in
// api/internal/statemachine, and these are the tests that prove every READER
// now asks that one derivation rather than its own SQL predicate.
//
// WHY AGAINST THE REAL STORE. The predicates these readers used to carry were
// SQL (`revoked_at IS NULL`, `refresh_expires_at > now()`, `consumed_at IS NULL
// AND expires_at > now()`), and two of them had drifted: ConnectedAppsByUser
// read a refresh credential with NO deadline as dead while RefreshGrant accepted
// it. Only executing the statements can show the readers now agree; a fake would
// agree with whatever the test believed.
//
// Skipped when JOMFORM_TEST_DATABASE_URL is unset, like every other integration
// test in this package.

import (
	"context"
	"errors"
	"testing"
	"time"
)

// seedTokenPair inserts one oauth_tokens row with explicit deadlines so a test
// can place it on either side of them without sleeping.
func seedTokenPair(t *testing.T, ctx context.Context, store *OAuthStore, clientID, userID, accountID string,
	accessExpires time.Time, refreshExpires *time.Time) (accessHash, refreshHash string) {
	t.Helper()
	suffix := randSuffix()
	accessHash, refreshHash = hashForTest("it-access-"+suffix), hashForTest("it-refresh-"+suffix)
	if err := store.CreateToken(ctx, &OAuthToken{
		TokenHash: accessHash, RefreshHash: refreshHash, ClientID: clientID,
		UserID: userID, AccountID: accountID, Scope: []string{"read"},
		AccessExpiresAt: accessExpires, RefreshExpiresAt: refreshExpires,
	}); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return accessHash, refreshHash
}

// TestTokenReadersAgreeAcrossTheLifecycle walks one pair through live →
// refreshed (old row revoked) and asserts that every reader answers the same way
// at every step: the /mcp authenticator's lookup, the connected-apps list, the
// connected-agents list and the refresh path.
func TestTokenReadersAgreeAcrossTheLifecycle(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)
	now := time.Now().UTC()

	accessHash, refreshHash := seedTokenPair(t, ctx, store, clientID, userID, accountID,
		now.Add(time.Hour), ptrTime(now.Add(24*time.Hour)))

	// LIVE: every reader agrees the pair works.
	if _, err := store.AccessTokenByHash(ctx, accessHash); err != nil {
		t.Fatalf("live access token refused: %v", err)
	}
	apps, err := store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("connected apps = %d (%v), want 1", len(apps), err)
	}
	agents, err := store.ConnectedAgentsByUser(ctx, userID)
	if err != nil || len(agents) != 1 || agents[0].ClientID != clientID {
		t.Fatalf("connected agents = %+v (%v), want the seeded client", agents, err)
	}

	// REFRESH: the rotation revokes the row it replaces.
	if _, err := store.RefreshGrant(ctx, refreshHash, hashForTest("it-new-access-"+randSuffix()),
		hashForTest("it-new-refresh-"+randSuffix()), now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatalf("live refresh token refused: %v", err)
	}
	// The old access token is now revoked — and this is the arm a reader that
	// only looked at the clock would get wrong: it is still unexpired.
	if _, err := store.AccessTokenByHash(ctx, accessHash); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("revoked-but-unexpired access token = %v, want ErrAccessTokenInvalid", err)
	}
	// Replaying the spent refresh token is refused, once and again.
	if _, err := store.RefreshGrant(ctx, refreshHash, hashForTest("it-replay-a-"+randSuffix()),
		hashForTest("it-replay-b-"+randSuffix()), now.Add(time.Hour), now.Add(24*time.Hour)); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("spent refresh token = %v, want ErrRefreshInvalid", err)
	}
	// The rotated pair carries the client, so the agents list still shows it
	// (the OLD row is gone from both lists).
	agents, err = store.ConnectedAgentsByUser(ctx, userID)
	if err != nil || len(agents) != 1 || agents[0].ClientID != clientID {
		t.Fatalf("connected agents after rotation = %+v (%v), want the client once", agents, err)
	}
	// Both the old pair and the rotated one belong to the user, but the old one
	// is revoked: exactly one row is listed.
	apps, err = store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("connected apps after rotation = %d (%v), want 1", len(apps), err)
	}
}

// TestTokenReadersAgreeWhenOnlyTheRefreshTokenSurvives is the forgotten arm:
// the access token has lapsed but the pair can still be refreshed. The list must
// keep it (the connection works), the authenticator must refuse it, and the
// agents list — which is about "who can call right now" — must omit it.
func TestTokenReadersAgreeWhenOnlyTheRefreshTokenSurvives(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)
	now := time.Now().UTC()

	accessHash, refreshHash := seedTokenPair(t, ctx, store, clientID, userID, accountID,
		now.Add(-time.Minute), ptrTime(now.Add(24*time.Hour)))

	if _, err := store.AccessTokenByHash(ctx, accessHash); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("lapsed access token = %v, want ErrAccessTokenInvalid", err)
	}
	apps, err := store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("connected apps = %d (%v), want 1 (a refreshable pair is a live connection)", len(apps), err)
	}
	agents, err := store.ConnectedAgentsByUser(ctx, userID)
	if err != nil || len(agents) != 0 {
		t.Fatalf("connected agents = %+v (%v), want none (the access token cannot authenticate)", agents, err)
	}
	if _, err := store.RefreshGrant(ctx, refreshHash, hashForTest("it-rot-a-"+randSuffix()),
		hashForTest("it-rot-b-"+randSuffix()), now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatalf("refreshable pair refused a refresh: %v", err)
	}
}

// TestTokenReadersAgreeOnALegacyRefreshCredentialWithNoDeadline is the
// disagreement this card removes, and the test that BITES on the old code: a row
// whose refresh_hash is set and whose refresh_expires_at is NULL (a credential
// written before the column existed) has no deadline, so RefreshGrant accepts it
// — while ConnectedAppsByUser used to require `refresh_expires_at IS NOT NULL`
// and hid the connection. The machine reads it one way; both readers now agree.
func TestTokenReadersAgreeOnALegacyRefreshCredentialWithNoDeadline(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)
	now := time.Now().UTC()

	accessHash, refreshHash := seedTokenPair(t, ctx, store, clientID, userID, accountID,
		now.Add(-time.Hour), nil)

	if _, err := store.AccessTokenByHash(ctx, accessHash); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("lapsed access token = %v, want ErrAccessTokenInvalid", err)
	}
	apps, err := store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("connected apps = %d (%v), want 1: a refresh credential with no deadline is unbounded, "+
			"which is exactly what RefreshGrant has always accepted", len(apps), err)
	}
	if _, err := store.RefreshGrant(ctx, refreshHash, hashForTest("it-legacy-a-"+randSuffix()),
		hashForTest("it-legacy-b-"+randSuffix()), now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatalf("a refresh credential with no deadline was refused: %v", err)
	}
}

// TestTokenBothDeadlinesPassedIsDeadEverywhere pins the other end: nothing is
// left, so no reader may claim the pair is alive.
func TestTokenBothDeadlinesPassedIsDeadEverywhere(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)
	now := time.Now().UTC()

	accessHash, refreshHash := seedTokenPair(t, ctx, store, clientID, userID, accountID,
		now.Add(-time.Hour), ptrTime(now.Add(-time.Minute)))

	if _, err := store.AccessTokenByHash(ctx, accessHash); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("dead access token = %v, want ErrAccessTokenInvalid", err)
	}
	apps, err := store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 0 {
		t.Fatalf("connected apps = %d (%v), want none", len(apps), err)
	}
	agents, err := store.ConnectedAgentsByUser(ctx, userID)
	if err != nil || len(agents) != 0 {
		t.Fatalf("connected agents = %+v (%v), want none", agents, err)
	}
	if _, err := store.RefreshGrant(ctx, refreshHash, hashForTest("it-dead-a-"+randSuffix()),
		hashForTest("it-dead-b-"+randSuffix()), now.Add(time.Hour), now.Add(24*time.Hour)); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("refresh past its deadline = %v, want ErrRefreshInvalid", err)
	}
}

// TestAuthCodeRedemptionIsSingleUseAndExpiryBounded pins the code machine at the
// store: redeem once, refuse the replay, refuse an expired code, and leave the
// row consumed (not expired) afterwards.
func TestAuthCodeRedemptionIsSingleUseAndExpiryBounded(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	now := time.Now().UTC()

	newCode := func(expiresAt time.Time) string {
		hash := hashForTest("it-code-" + randSuffix())
		if err := store.StoreAuthCode(ctx, hash, &OAuthAuthCode{
			ClientID: "jfc_it_t_df243a6e", UserID: userID, AccountID: accountID,
			RedirectURI: "https://client.example.test/cb", CodeChallenge: "chal",
			Scope: []string{"read"}, ExpiresAt: expiresAt,
		}); err != nil {
			t.Fatalf("StoreAuthCode: %v", err)
		}
		return hash
	}

	live := newCode(now.Add(10 * time.Minute))
	ac, err := store.ConsumeAuthCode(ctx, live)
	if err != nil {
		t.Fatalf("live code refused: %v", err)
	}
	if ac.ConsumedAt == nil {
		t.Fatal("the redeemed code must come back carrying consumed_at")
	}
	if _, err := store.ConsumeAuthCode(ctx, live); !errors.Is(err, ErrAuthCodeInvalid) {
		t.Fatalf("replayed code = %v, want ErrAuthCodeInvalid", err)
	}
	// The state is CONSUMED, not expired: single use is absolute even once the
	// window has closed.
	var consumedAt *time.Time
	var expiresAt time.Time
	if err := p.QueryRow(ctx, `SELECT consumed_at, expires_at FROM oauth_auth_codes WHERE code_hash = $1`, live).
		Scan(&consumedAt, &expiresAt); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if consumedAt == nil {
		t.Fatal("the row must stay consumed")
	}

	expired := newCode(now.Add(-time.Minute))
	if _, err := store.ConsumeAuthCode(ctx, expired); !errors.Is(err, ErrAuthCodeInvalid) {
		t.Fatalf("expired code = %v, want ErrAuthCodeInvalid", err)
	}
	if _, err := store.ConsumeAuthCode(ctx, hashForTest("it-never-existed-"+randSuffix())); !errors.Is(err, ErrAuthCodeInvalid) {
		t.Fatalf("unknown code = %v, want ErrAuthCodeInvalid", err)
	}
}

// TestRevokedAPIKeyIsNeitherAuthenticatedNorListed pins the key machine at the
// store: the authenticator and the inventory are the same answer, and a repeat
// revoke stays a success (RFC-style idempotency the API relies on).
func TestRevokedAPIKeyIsNeitherAuthenticatedNorListed(t *testing.T) {
	ctx := context.Background()
	keys := NewAPIKeyStore(testPool(t))
	accountID := uniqueAccount(t, ctx, keys.pool)

	key, err := keys.Create(ctx, accountID, hashForTest("it-key-"+randSuffix()), "it key", []string{ScopeReadonly})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := keys.LookupByKeyHash(ctx, key.KeyHash); err != nil {
		t.Fatalf("active key refused: %v", err)
	}
	if listed, err := keys.ListByAccount(ctx, accountID); err != nil || len(listed) != 1 {
		t.Fatalf("keys = %d (%v), want 1", len(listed), err)
	}

	if err := keys.Revoke(ctx, accountID, key.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := keys.LookupByKeyHash(ctx, key.KeyHash); !errors.Is(err, ErrAPIKeyInvalid) {
		t.Fatalf("revoked key = %v, want ErrAPIKeyInvalid", err)
	}
	if listed, err := keys.ListByAccount(ctx, accountID); err != nil || len(listed) != 0 {
		t.Fatalf("keys after revoke = %d (%v), want none", len(listed), err)
	}
	if err := keys.Revoke(ctx, accountID, key.ID); err != nil {
		t.Fatalf("repeat Revoke must stay a success (idempotent), got %v", err)
	}
}
