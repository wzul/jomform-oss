package mcpsrv

// The Cloudflare MCP path renders a bounded copy of the upstream body into
// "cloudflare http %d: %s". The bound must cut on a RUNE boundary: a byte slice
// lands mid-rune on a multi-byte body (a localised Cloudflare error, a non-ASCII
// domain), so the operator reads a broken character where the provider's own
// text should be.
//
// This drives the REAL path (cfAPICallMCP → stored credential → unseal → HTTP →
// error render) against a real Postgres, with the HTTP client stubbed, and
// asserts on the RENDERED error string: valid UTF-8, still bounded, and still
// carrying the rune that straddles the cap.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset, like the other mcpsrv integration tests.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// cfStraddleRune is 3 bytes wide, so a cap offset one byte into it produces
// invalid UTF-8 under a byte slice.
const cfStraddleRune = "中"

// cfRoundTripFunc answers any request with a canned response, so the tests never
// dial the Cloudflare API.
type cfRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cfRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCloudflareHTTPErrorBodyIsValidUTF8(t *testing.T) {
	const capBytes = 512
	body := strings.Repeat("x", capBytes-1) + cfStraddleRune + strings.Repeat("y", 40)

	ctx := context.Background()
	pool := testdb.Open(t)

	// An account carrying an unsealable manual Cloudflare credential is the
	// precondition cfAPICallMCP checks before it does anything else.
	var userID, workspaceID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('cf-trunc-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'cf-trunc-ws-' || md5(random()::text), 'cf-trunc-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'cf-trunc-acc-' || md5(random()::text), 'cf-trunc-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	key := bytes.Repeat([]byte{7}, 32)
	blob, err := json.Marshal(cfCredMCP{Kind: cfKindAPITokenMCP, Access: "cf-token-for-test"})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	sealed, err := cryptoutil.Encrypt(key, blob)
	if err != nil {
		t.Fatalf("seal credential: %v", err)
	}
	accounts := dbstore.NewAccountStore(pool)
	if err := accounts.SaveCFToken(ctx, accountID, sealed); err != nil {
		t.Fatalf("store cf token: %v", err)
	}

	srv := New(&Deps{Accounts: accounts}, nil, key, nil)

	// cfAPICallMCP uses http.DefaultClient; stub it for the duration of the
	// call. No request leaves the process — the transport answers directly.
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: cfRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	defer func() { http.DefaultClient = old }()

	_, callErr := srv.cfAPICallMCP(ctx, accountID, http.MethodGet, "/zones?per_page=50", nil)
	if callErr == nil {
		t.Fatal("expected an error from the 502 response")
	}
	msg := callErr.Error()
	if !utf8.ValidString(msg) {
		t.Fatalf("rendered error is not valid UTF-8 — the operator reads a broken rune: %q", msg)
	}
	const prefix = "cloudflare http 502: "
	if !strings.HasPrefix(msg, prefix) {
		t.Fatalf("rendered error = %q, want it to start with %q", msg, prefix)
	}
	got := strings.TrimPrefix(msg, prefix)
	if n := utf8.RuneCountInString(got); n > capBytes {
		t.Errorf("body part is %d characters, want it capped at %d", n, capBytes)
	}
	if !strings.Contains(got, cfStraddleRune) {
		t.Errorf("body part %q lost the rune that straddles the cap", got)
	}
}
