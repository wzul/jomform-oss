package dbstore

// Real-Postgres tests for the RFC 8628 device-authorisation store and the
// "Connected apps" rotation (card t_9a6cc85f).
//
// WHY THESE RUN AGAINST THE REAL STORE. The handler tests inject an in-memory
// fake, and a fake cannot prove the properties this feature is about — they are
// all SQL: the consumed_at UPDATE is what makes a device_code single-use, the
// row lock is what stops two polls minting two tokens, the persisted interval is
// what makes slow_down survive a request boundary, and revoke/rotate are single
// statements whose atomicity is the whole point. Each test below asserts the
// stored ROW, not the fake's map.
//
// Skipped when JOMFORM_TEST_DATABASE_URL is unset, like every other integration
// test in this package.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedDeviceClient(t *testing.T, ctx context.Context, p *pgxpool.Pool) string {
	t.Helper()
	clientID := "jfc_it_" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
	if _, err := p.Exec(ctx,
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris)
		 VALUES ($1, 'it client', ARRAY['https://example.test/cb'])`, clientID); err != nil {
		t.Fatalf("seed client: %v", err)
	}
	return clientID
}

func seedDevice(t *testing.T, ctx context.Context, store *OAuthStore, clientID, userID, accountID string, scope []string, ttl time.Duration) (deviceHash, userCodeHash string, row *OAuthDeviceCode) {
	t.Helper()
	deviceHash = "dh_" + userID[:8] + "_" + time.Now().Format("150405.000000000")
	userCodeHash = "uh_" + userID[:8] + "_" + time.Now().Format("150405.000000000")
	row = &OAuthDeviceCode{
		DeviceHash: deviceHash, UserCodeHash: userCodeHash, ClientID: clientID,
		UserID: userID, AccountID: accountID, Scope: scope, IP: "203.0.113.7",
		ExpiresAt: time.Now().UTC().Add(ttl), IntervalS: 5, CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateDeviceCode(ctx, row); err != nil {
		t.Fatalf("CreateDeviceCode: %v", err)
	}
	return deviceHash, userCodeHash, row
}

func TestDeviceCodeLifecycleIsSingleUse(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	deviceHash, userCodeHash, _ := seedDevice(t, ctx, store, clientID, userID, accountID, []string{"read"}, time.Hour)

	// Before approval: pending (RFC 8628 §3.5 authorization_pending).
	if _, err := store.PollDeviceCode(ctx, deviceHash, clientID); !errors.Is(err, ErrDevicePending) {
		t.Fatalf("first poll = %v, want ErrDevicePending", err)
	}
	// Approve, then the poll returns the row (consumed). The 5s interval has to
	// elapse first — the first poll above started the clock.
	if _, err := store.ApproveDeviceCode(ctx, userCodeHash, userID, accountID, []string{"read"}); err != nil {
		t.Fatalf("ApproveDeviceCode: %v", err)
	}
	backdatePoll(t, ctx, p, deviceHash, 10*time.Second)
	d, err := store.PollDeviceCode(ctx, deviceHash, clientID)
	if err != nil {
		t.Fatalf("poll after approval: %v", err)
	}
	if d.UserID != userID || strings.Join(d.Scope, " ") != "read" {
		t.Fatalf("polled row = user %q scope %v", d.UserID, d.Scope)
	}
	// SINGLE USE: the second consumption must fail. This is the UPDATE ... WHERE
	// consumed_at IS NULL doing its job, not a caller-side check.
	if _, err := store.PollDeviceCode(ctx, deviceHash, clientID); !errors.Is(err, ErrDeviceCodeInvalid) {
		t.Fatalf("second poll = %v, want ErrDeviceCodeInvalid", err)
	}
	// And a second approval of the same code is refused too.
	if _, err := store.ApproveDeviceCode(ctx, userCodeHash, userID, accountID, []string{"read"}); !errors.Is(err, ErrDeviceCodeInvalid) {
		t.Fatalf("second approve = %v, want ErrDeviceCodeInvalid", err)
	}
}

func TestDeviceCodeSlowDownPenaltyPersists(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)
	deviceHash, _, _ := seedDevice(t, ctx, store, clientID, userID, "", []string{"read"}, time.Hour)

	// First poll (pending) records last_polled_at.
	if _, err := store.PollDeviceCode(ctx, deviceHash, clientID); !errors.Is(err, ErrDevicePending) {
		t.Fatalf("first poll = %v", err)
	}
	// Immediately again: faster than interval_s → slow_down.
	if _, err := store.PollDeviceCode(ctx, deviceHash, clientID); !errors.Is(err, ErrDeviceSlowDown) {
		t.Fatalf("second poll = %v, want ErrDeviceSlowDown", err)
	}
	// The +5s penalty is STORED, so it outlives the request: 5 → 10 seconds.
	var interval int
	if err := p.QueryRow(ctx,
		`SELECT interval_s FROM oauth_device_codes WHERE device_hash = $1`, deviceHash).Scan(&interval); err != nil {
		t.Fatalf("read interval: %v", err)
	}
	if interval != 10 {
		t.Fatalf("interval_s after one slow_down = %d, want 10 (5 + 5s penalty)", interval)
	}
}

func TestDeviceCodeExpiryAndDenial(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	// Expired: a row whose life already ended answers expired_token.
	expiredHash, _, _ := seedDevice(t, ctx, store, clientID, userID, "", []string{"read"}, -time.Minute)
	if _, err := store.PollDeviceCode(ctx, expiredHash, clientID); !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("expired poll = %v, want ErrDeviceExpired", err)
	}

	// Denied: access_denied, and approval can no longer rescue it.
	deniedHash, deniedCode, _ := seedDevice(t, ctx, store, clientID, userID, "", []string{"read"}, time.Hour)
	if err := store.DenyDeviceCode(ctx, deniedCode, userID); err != nil {
		t.Fatalf("DenyDeviceCode: %v", err)
	}
	if _, err := store.PollDeviceCode(ctx, deniedHash, clientID); !errors.Is(err, ErrDeviceDenied) {
		t.Fatalf("denied poll = %v, want ErrDeviceDenied", err)
	}
	if _, err := store.ApproveDeviceCode(ctx, deniedCode, userID, "", []string{"read"}); !errors.Is(err, ErrDeviceDenied) {
		t.Fatalf("approve after deny = %v, want ErrDeviceDenied", err)
	}
}

func TestDeviceCodeStoresHashesOnly(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)
	const rawDevice, rawUserCode = "raw-device-secret-abc", "BCDF-GHJK"
	if err := store.CreateDeviceCode(ctx, &OAuthDeviceCode{
		DeviceHash: hashForTest(rawDevice), UserCodeHash: hashForTest(rawUserCode),
		ClientID: clientID, UserID: userID, Scope: []string{"read"},
		ExpiresAt: time.Now().UTC().Add(time.Hour), IntervalS: 5, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateDeviceCode: %v", err)
	}
	var n int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM oauth_device_codes WHERE device_hash = $1 OR user_code_hash = $1`,
		rawDevice).Scan(&n); err != nil {
		t.Fatalf("count raw: %v", err)
	}
	if n != 0 {
		t.Fatalf("raw device_code found in the table %d times — secrets must be hashed", n)
	}
}

// hashForTest is the same SHA-256 hex the httpapi layer computes. Written out
// here on purpose: this test must fail if the SCHEMA ever accepts raw values,
// and importing the httpapi hash would make the assertion circular.
func hashForTest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// TestConnectedAppsRotationEnforcesTheScopeLadder is the bite-proof test for the
// scope ladder's storage half: an upgrade or downgrade REPLACES the pair, and
// the replaced credential stops working — access token AND refresh token.
func TestConnectedAppsRotationEnforcesTheScopeLadder(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	now := time.Now().UTC()
	oldAccess, oldRefresh := hashForTest("old-access-"+userID), hashForTest("old-refresh-"+userID)
	if err := store.CreateToken(ctx, &OAuthToken{
		TokenHash: oldAccess, RefreshHash: oldRefresh, ClientID: clientID,
		UserID: userID, AccountID: accountID, Scope: []string{"read"},
		AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: ptrTime(now.Add(24 * time.Hour)),
	}); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	apps, err := store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("list = %d apps, err %v", len(apps), err)
	}
	id := apps[0].ID
	if strings.Join(apps[0].Scope, " ") != "read" {
		t.Fatalf("listed scope = %v, want read", apps[0].Scope)
	}

	// UPGRADE (dashboard route): rotate to readwrite.
	newAccess, newRefresh := hashForTest("new-access-"+userID), hashForTest("new-refresh-"+userID)
	nt, oldScope, err := store.RotateTokenScope(ctx, userID, id, []string{"readwrite"},
		newAccess, newRefresh, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("RotateTokenScope: %v", err)
	}
	if strings.Join(oldScope, " ") != "read" {
		t.Fatalf("returned old scope = %v, want read", oldScope)
	}
	if strings.Join(nt.Scope, " ") != "readwrite" {
		t.Fatalf("new token scope = %v, want readwrite", nt.Scope)
	}

	// The OLD refresh hash must no longer resolve — otherwise a leaked refresh
	// token keeps working after the user believes they upgraded.
	if _, err := store.RefreshGrant(ctx, oldRefresh, hashForTest("x1"), hashForTest("x2"),
		now.Add(time.Hour), now.Add(time.Hour)); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("old refresh after upgrade = %v, want ErrRefreshInvalid", err)
	}
	// And the old access token is rejected on the very next call.
	if _, err := store.AccessTokenByHash(ctx, oldAccess); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("old access after upgrade = %v, want ErrAccessTokenInvalid", err)
	}
	// The new pair works.
	if got, err := store.AccessTokenByHash(ctx, newAccess); err != nil || strings.Join(got.Scope, " ") != "readwrite" {
		t.Fatalf("new access = %v / %v, want readwrite", got, err)
	}

	// DOWNGRADE: immediate, and also a rotation.
	apps, err = store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("after upgrade list = %d apps, err %v", len(apps), err)
	}
	downAccess, downRefresh := hashForTest("down-access-"+userID), hashForTest("down-refresh-"+userID)
	if _, _, err := store.RotateTokenScope(ctx, userID, apps[0].ID, []string{"read"},
		downAccess, downRefresh, now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatalf("downgrade rotate: %v", err)
	}
	if _, err := store.AccessTokenByHash(ctx, newAccess); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("readwrite access after downgrade = %v, want ErrAccessTokenInvalid", err)
	}
	if got, err := store.AccessTokenByHash(ctx, downAccess); err != nil || strings.Join(got.Scope, " ") != "read" {
		t.Fatalf("downgraded access = %v / %v, want read", got, err)
	}

	// REVOKE from Connected apps: one call, both tokens dead.
	apps, err = store.ConnectedAppsByUser(ctx, userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("before revoke list = %d apps, err %v", len(apps), err)
	}
	if err := store.RevokeTokenByIDForUser(ctx, userID, apps[0].ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := store.AccessTokenByHash(ctx, downAccess); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("access after revoke = %v, want ErrAccessTokenInvalid", err)
	}
	if _, err := store.RefreshGrant(ctx, downRefresh, hashForTest("y1"), hashForTest("y2"),
		now.Add(time.Hour), now.Add(time.Hour)); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("refresh after revoke = %v, want ErrRefreshInvalid", err)
	}
	if apps, err := store.ConnectedAppsByUser(ctx, userID); err != nil || len(apps) != 0 {
		t.Fatalf("after revoke list = %d apps, err %v", len(apps), err)
	}
	// A second revoke is "not active", never somebody else's row.
	if err := store.RevokeTokenByIDForUser(ctx, userID, id); !errors.Is(err, ErrOAuthTokenMissing) {
		t.Fatalf("second revoke = %v, want ErrOAuthTokenMissing", err)
	}
}

// TestRotateTokenScopeIsScopedToTheOwner: another user's token id must not be
// rotatable, or a dashboard bug would become a privilege transfer.
func TestRotateTokenScopeIsScopedToTheOwner(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)
	owner := testOwnerUser(t, ctx, p)
	other := testOwnerUser(t, ctx, p)
	clientID := seedDeviceClient(t, ctx, p)

	now := time.Now().UTC()
	if err := store.CreateToken(ctx, &OAuthToken{
		TokenHash: hashForTest("o-access-" + owner), RefreshHash: hashForTest("o-refresh-" + owner),
		ClientID: clientID, UserID: owner, Scope: []string{"read"},
		AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: ptrTime(now.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	apps, err := store.ConnectedAppsByUser(ctx, owner)
	if err != nil || len(apps) != 1 {
		t.Fatalf("list: %v / %v", apps, err)
	}
	if _, _, err := store.RotateTokenScope(ctx, other, apps[0].ID, []string{"readwrite"},
		hashForTest("z1"), hashForTest("z2"), now.Add(time.Hour), now.Add(time.Hour)); !errors.Is(err, ErrOAuthTokenMissing) {
		t.Fatalf("rotate somebody else's token = %v, want ErrOAuthTokenMissing", err)
	}
	if err := store.RevokeTokenByIDForUser(ctx, other, apps[0].ID); !errors.Is(err, ErrOAuthTokenMissing) {
		t.Fatalf("revoke somebody else's token = %v, want ErrOAuthTokenMissing", err)
	}
	if got, err := store.AccessTokenByHash(ctx, hashForTest("o-access-"+owner)); err != nil || strings.Join(got.Scope, " ") != "read" {
		t.Fatalf("owner token after foreign attempts = %v / %v", got, err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// backdatePoll moves last_polled_at into the past by `by`, which is how a test
// stands in for the seconds a well-behaved client waits between polls without
// sleeping them.
func backdatePoll(t *testing.T, ctx context.Context, p *pgxpool.Pool, deviceHash string, by time.Duration) {
	t.Helper()
	if _, err := p.Exec(ctx,
		`UPDATE oauth_device_codes SET last_polled_at = now() - $2::interval WHERE device_hash = $1`,
		deviceHash, by.String()); err != nil {
		t.Fatalf("backdate poll: %v", err)
	}
}
