package httpapi

// Issue #507 regression at the HTTP level. The pre-existing OAuth handler tests
// inject an in-memory fake store, which is exactly why the suite stayed green
// while production returned 500 on POST /oauth/authorize: the real INSERT was
// never executed. These tests wire the REAL dbstore.OAuthStore over the real
// Postgres (JOMFORM_TEST_DATABASE_URL; skipped when unset) and drive
// GET /oauth/authorize → POST /oauth/token → refresh rotation → authenticated
// access-token resolution, for both the account-set and the empty-account
// (#307) shape.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// oauth506TestPool returns this package's scratch-schema pool.
func oauth506TestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.Open(t)
}

// newRealOAuthHandlers wires OAuthHandlers over the real store, faking only the
// session/user/account resolution (the identity layer, not the persistence the
// bug lived in).
func newRealOAuthHandlers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, grantAccount bool) (*OAuthHandlers, string, string) {
	t.Helper()
	userID := oauthTestUser(t, ctx, pool)
	accountID := ""
	if grantAccount {
		accountID = oauthTestAccount(t, ctx, pool, userID)
	}
	users := &fakeOAuthUsers{byID: map[string]*dbstore.User{userID: {ID: userID, Email: "it-507@example.test", Verified: true}}}
	accounts := &fakeOAuthAccounts{}
	if grantAccount {
		accounts.ownerAccounts = []*dbstore.Account{{ID: accountID}}
	}
	o := &OAuthHandlers{
		Store:    dbstore.NewOAuthStore(pool),
		Sessions: &fakeOAuthSessions{userID: userID},
		Users:    users,
		Accounts: accounts,
		BaseURL:  "http://localhost:8080",
		CodeTTL:  10 * time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	return o, userID, accountID
}

func oauthTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-507-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func oauthTestAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-507-acc-' || md5(random()::text), 'it-507-' || md5(random()::text)
		 RETURNING id`, ownerID).Scan(&id); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

// TestOAuth507AuthorizeAndTokenRealStore is the end-to-end regression: authorize
// must 302 with a code (no 500), the code must exchange for a usable access
// token, and refresh rotation must produce a token that resolves.
func TestOAuth507AuthorizeAndTokenRealStore(t *testing.T) {
	ctx := context.Background()
	pool := oauth506TestPool(t)

	for _, tc := range []struct {
		name         string
		grantAccount bool
	}{
		{"account set", true},
		{"empty account (#307)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, userID, accountID := newRealOAuthHandlers(t, ctx, pool, tc.grantAccount)
			clientID := registerTestClient(t, o, "https://client.example.com/cb")
			verifier := "verifier-507-" + tc.name

			rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", verifier, "state-507")
			if rec.Code != http.StatusFound {
				t.Fatalf("authorize = %d body=%s (want 302; a 500 here is the #507 regression)",
					rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			code := mustQuery(t, loc).Get("code")
			if code == "" {
				t.Fatalf("no code in redirect: %q", loc)
			}

			// The code row must exist and carry the expected account.
			var stored *string
			if err := pool.QueryRow(ctx,
				`SELECT account_id::text FROM oauth_auth_codes WHERE code_hash=$1`, hashToken(code)).Scan(&stored); err != nil {
				t.Fatalf("read auth code: %v", err)
			}
			if tc.grantAccount {
				if stored == nil || *stored != accountID {
					t.Fatalf("stored account_id = %v, want %q", stored, accountID)
				}
			} else if stored != nil {
				t.Fatalf("stored account_id = %q, want NULL", *stored)
			}

			// Exchange the code (PKCE) → a usable Bearer access token.
			form := url.Values{}
			form.Set("grant_type", "authorization_code")
			form.Set("code", code)
			form.Set("redirect_uri", "https://client.example.com/cb")
			form.Set("client_id", clientID)
			form.Set("code_verifier", verifier)
			trec := httptest.NewRecorder()
			treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
			treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			o.token(trec, treq)
			if trec.Code != http.StatusOK {
				t.Fatalf("token = %d body=%s (want 200; a 500 here is the token half of #507)",
					trec.Code, trec.Body.String())
			}
			var tok struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				TokenType    string `json:"token_type"`
			}
			decodeJSONBody(t, trec, &tok)
			if tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" {
				t.Fatalf("incomplete token response: %+v", tok)
			}

			// The access token is usable: it resolves to the right user/account.
			at, err := dbstore.NewOAuthStore(pool).AccessTokenByHash(ctx, hashToken(tok.AccessToken))
			if err != nil {
				t.Fatalf("issued access token does not resolve: %v", err)
			}
			if at.UserID != userID {
				t.Fatalf("token user_id = %q, want %q", at.UserID, userID)
			}
			if tc.grantAccount {
				if at.AccountID != accountID {
					t.Fatalf("token account_id = %q, want %q", at.AccountID, accountID)
				}
			} else if at.AccountID != "" {
				t.Fatalf("token account_id = %q, want empty/NULL", at.AccountID)
			}

			// Refresh rotation (the third #507 insert site, inside the tx).
			rform := url.Values{}
			rform.Set("grant_type", "refresh_token")
			rform.Set("refresh_token", tok.RefreshToken)
			rform.Set("client_id", clientID)
			rrec := httptest.NewRecorder()
			rreq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(rform.Encode()))
			rreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			o.token(rrec, rreq)
			if rrec.Code != http.StatusOK {
				t.Fatalf("refresh = %d body=%s", rrec.Code, rrec.Body.String())
			}
			var rotated struct {
				AccessToken string `json:"access_token"`
			}
			decodeJSONBody(t, rrec, &rotated)
			if _, err := dbstore.NewOAuthStore(pool).AccessTokenByHash(ctx, hashToken(rotated.AccessToken)); err != nil {
				t.Fatalf("rotated access token does not resolve: %v", err)
			}
		})
	}
}

// failingOAuthStore wraps the real store and forces the write paths to fail, so
// the 5xx log path (#507 item 3) can be asserted without breaking Postgres.
type failingOAuthStore struct {
	*dbstore.OAuthStore
	err error
}

func (f *failingOAuthStore) StoreAuthCode(_ context.Context, _ string, _ *dbstore.OAuthAuthCode) error {
	return f.err
}
func (f *failingOAuthStore) CreateToken(_ context.Context, _ *dbstore.OAuthToken) error {
	return f.err
}

// TestOAuth507ServerErrorLogsUnderlyingError asserts a 5xx OAuth failure logs
// the underlying error server-side while the client still receives only the
// generic message (no internal detail leaks).
func TestOAuth507ServerErrorLogsUnderlyingError(t *testing.T) {
	ctx := context.Background()
	pool := oauth506TestPool(t)

	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// 1. authorize / StoreAuthCode failure.
	_, userID, accountID := newRealOAuthHandlers(t, ctx, pool, true)
	users := &fakeOAuthUsers{byID: map[string]*dbstore.User{userID: {ID: userID, Verified: true}}}
	o := &OAuthHandlers{
		Store:    &failingOAuthStore{OAuthStore: dbstore.NewOAuthStore(pool), err: errOAuth507Sentinel},
		Sessions: &fakeOAuthSessions{userID: userID}, Users: users,
		Accounts: &fakeOAuthAccounts{ownerAccounts: []*dbstore.Account{{ID: accountID}}},
		BaseURL:  "http://localhost:8080",
		CodeTTL:  10 * time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", "v", "s")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("authorize = %d, want 500", rec.Code)
	}
	// Client must keep the generic message — the sentinel must NOT leak.
	if strings.Contains(rec.Body.String(), errOAuth507Sentinel.Error()) {
		t.Errorf("underlying error leaked to client: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to store the authorization code") {
		t.Errorf("generic message missing: %s", rec.Body.String())
	}
	// Server log must carry the sentinel + the handler tag.
	logged := buf.String()
	if !strings.Contains(logged, errOAuth507Sentinel.Error()) {
		t.Errorf("underlying error not logged server-side; log=%s", logged)
	}
	if !strings.Contains(logged, "oauth_error") {
		t.Errorf("no oauth_error log record; log=%s", logged)
	}

	// 2. token / CreateToken failure.
	buf.Reset()
	o2 := &OAuthHandlers{
		Store:    &failingOAuthStore{OAuthStore: dbstore.NewOAuthStore(pool), err: errOAuth507Sentinel},
		Sessions: &fakeOAuthSessions{userID: userID}, Users: users,
		Accounts: &fakeOAuthAccounts{}, BaseURL: "http://localhost:8080",
		CodeTTL: 10 * time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	o2.issueTokenPair(httptest.NewRecorder(), httptest.NewRequest("POST", "/oauth/token", nil),
		"jfc_test", userID, "", []string{"readwrite"})
	if !strings.Contains(buf.String(), errOAuth507Sentinel.Error()) {
		t.Errorf("token 5xx underlying error not logged; log=%s", buf.String())
	}
}

// errOAuth507Sentinel stands in for a DB failure in the swallowed-error test.
var errOAuth507Sentinel = errOAuth507("simulated db write failure for issue 507")

// TestOAuth507ServerErrorAlwaysLogs pins the invariant directly: a 5xx is
// logged even when the caller passes no props (the exact shape that swallowed
// the prod failure), while a 4xx with no props stays as quiet as before and the
// written client body is identical in both cases.
func TestOAuth507ServerErrorAlwaysLogs(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// 5xx, no props → must log.
	rec := httptest.NewRecorder()
	oauthErr(rec, http.StatusInternalServerError, "server_error", "failed to store the authorization code")
	if !strings.Contains(buf.String(), "server_error") {
		t.Errorf("5xx with no props was not logged: %q", buf.String())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "failed to store the authorization code") {
		t.Errorf("client body = %s", rec.Body.String())
	}

	// 4xx, no props → unchanged (no log record).
	buf.Reset()
	rec4 := httptest.NewRecorder()
	oauthErr(rec4, http.StatusBadRequest, "invalid_request", "state wajib")
	if strings.Contains(buf.String(), "oauth_error") {
		t.Errorf("4xx with no props should stay quiet; log=%q", buf.String())
	}
	if rec4.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec4.Code)
	}
}

type errOAuth507 string

func (e errOAuth507) Error() string { return string(e) }
