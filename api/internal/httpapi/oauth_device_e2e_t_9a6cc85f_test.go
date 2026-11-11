package httpapi

// RFC 8628 headless onboarding, END TO END (card t_9a6cc85f).
//
// oauth_device_test.go drives the device handlers over an in-memory fake, which
// proves the response shapes and the RFC's error codes but cannot prove the two
// things the card is actually about:
//
//	1. that the credential a device authorisation produces is one the MCP
//	   TRANSPORT accepts and a tool call runs on — the whole point of headless
//	   onboarding is that no browser is involved, so the far end of the flow has
//	   to work;
//	2. that the dashboard's "Connected apps" routes really rotate the pair, so
//	   the OLD credential dies when the user upgrades or downgrades.
//
// So both tests below boot the REAL server (NewServer: real OAuth store, real
// Valkey sessions, real MCP transport over Postgres) and speak HTTP to it:
//
//	device_authorization → email (captured) → dashboard consent card (the
//	email's link) → token poll → MCP tools/call with the bearer token
//
// and
//
//	seed a read pair → dashboard upgrade needs confirm → rotate → old refresh
//	dead → downgrade → old access dead → revoke → both dead
//
// Integration test: needs JOMFORM_TEST_DATABASE_URL and JOMFORM_TEST_VALKEY_URL;
// skipped when either is unset.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/oauthmeta"
	"github.com/wzul/jomform/api/internal/testdb"
)

// deviceMail is one captured consent email.
type deviceMail struct{ To, Subject, Body, Lang string }

// deviceMailSpy stands in for the SES mailer: the device handler sends OFF the
// request goroutine in production, so the mailbox is a channel and the test
// waits for the message rather than reading a field a goroutine may not have
// written yet.
type deviceMailSpy struct{ ch chan deviceMail }

func newDeviceMailSpy() *deviceMailSpy { return &deviceMailSpy{ch: make(chan deviceMail, 8)} }

func (m *deviceMailSpy) Send(to, subject, body, lang string) error {
	select {
	case m.ch <- deviceMail{To: to, Subject: subject, Body: body, Lang: lang}:
	default:
	}
	return nil
}

func (m *deviceMailSpy) wait(t *testing.T) deviceMail {
	t.Helper()
	select {
	case mail := <-m.ch:
		return mail
	case <-time.After(5 * time.Second):
		t.Fatal("no consent email was sent within 5s")
		return deviceMail{}
	}
}

func (m *deviceMailSpy) empty() bool {
	select {
	case <-m.ch:
		return false
	default:
		return true
	}
}

// deviceFlowEnv is the live server plus the identity it authenticates.
type deviceFlowEnv struct {
	http   *httptest.Server
	pool   *pgxpool.Pool
	store  *dbstore.OAuthStore
	userID string
	acctID string
	email  string
	sess   string
	mail   *deviceMailSpy
}

// newDeviceFlowEnv boots the production wiring against Postgres + Valkey: the
// OAuth handlers (with a capturing mailer), the dashboard auth routes and the
// MCP transport, all behind one httptest listener.
func newDeviceFlowEnv(t *testing.T) *deviceFlowEnv {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	valkeyURL := strings.TrimSpace(os.Getenv(testdb.ValkeyEnv))
	if valkeyURL == "" {
		t.Skip(testdb.ValkeyEnv + " not set; skipping end-to-end device-flow walk")
	}
	sessions, err := auth.NewSessions(valkeyURL, testSessionOptions()...)
	if err != nil {
		t.Fatalf("valkey: %v", err)
	}
	t.Cleanup(func() { _ = sessions.Close() })
	if err := sessions.Ping(ctx); err != nil {
		t.Fatalf("valkey ping: %v", err)
	}

	email := "device-e2e-" + strings.ReplaceAll(randSuffix(), "-", "") + "@example.test"
	var userID, workspaceID, acctID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, 'it') RETURNING id`,
		email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'device-e2e-ws-' || md5(random()::text), 'device-e2e-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'device-e2e-acc-' || md5(random()::text), 'device-e2e-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&acctID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	// A seeded product is the proof that an authenticated MCP call reached THIS
	// account's data rather than merely parsing a bearer token.
	if err := dbstore.NewProductStore(pool).Create(ctx, &dbstore.Product{
		AccountID: acctID, Name: "Device Flow Widget", PriceSen: 1500,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	mail := newDeviceMailSpy()
	cfg := &config.Config{
		Port: "0", DatabaseURL: os.Getenv(testdb.DSNEnv), AppBaseURL: "http://localhost:8080", Env: "dev",
	}
	oauthStore := dbstore.NewOAuthStore(pool)
	users := dbstore.NewUserStore(pool)
	mcpSrv := mcpsrv.New(&mcpsrv.Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   dbstore.NewProductStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		Users:      users,
		OAuth:      oauthStore,
	}, nil, nil, nil)

	rest := &RESTHandlers{
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Forms:             dbstore.NewFormStore(pool),
		Keys:              dbstore.NewAPIKeyStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Subs:              dbstore.NewSubscriptionStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Users:             users,
		Sessions:          sessions,
		BaseURL:           "http://localhost:8080",
		SecretsKey:        make([]byte, 32),
	}

	srv := NewServer(cfg, testLogger(), &Deps{
		MCP:          mcpSrv,
		Users:        users,
		Sessions:     sessions,
		Accounts:     dbstore.NewAccountStore(pool),
		OAuth:        oauthStore,
		REST:         rest,
		VerifyMailer: mail,
		Events:       dbstore.NewEventStore(pool),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	sess, err := sessions.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &deviceFlowEnv{http: ts, pool: pool, store: oauthStore, userID: userID, acctID: acctID, email: email, sess: sess, mail: mail}
}

// authed builds a request carrying the browser session cookie.
func (e *deviceFlowEnv) authed(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, e.http.URL+path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.sess})
	return req
}

func (e *deviceFlowEnv) do(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := e.http.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// registerDeviceClient performs DCR and returns the client_id.
func (e *deviceFlowEnv) registerDeviceClient(t *testing.T, name string) string {
	t.Helper()
	req, _ := http.NewRequest("POST", e.http.URL+"/oauth/register",
		strings.NewReader(`{"client_name":"`+name+`","redirect_uris":["`+e.http.URL+`/callback"]}`))
	// JSON body for a JSON endpoint — .do() sets the content type only for the
	// authed helper, so set it here.
	req.Header.Set("Content-Type", "application/json")
	resp, raw := e.do(t, req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d body=%s", resp.StatusCode, raw)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil || reg.ClientID == "" {
		t.Fatalf("register response %q: %v", raw, err)
	}
	return reg.ClientID
}

// startDevice runs POST /oauth/device_authorization and returns the response.
func (e *deviceFlowEnv) startDevice(t *testing.T, clientID, scope, hint string) deviceAuthResponse {
	t.Helper()
	form := url.Values{"client_id": {clientID}, "scope": {scope}, "login_hint": {hint}}
	req, err := http.NewRequest("POST", e.http.URL+"/oauth/device_authorization", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build device request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, raw := e.do(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device_authorization = %d body=%s", resp.StatusCode, raw)
	}
	var out deviceAuthResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode device response %q: %v", raw, err)
	}
	return out
}

// pollToken drives one POST /oauth/token for the device grant.
func (e *deviceFlowEnv) pollToken(t *testing.T, clientID, deviceCode string) (int, map[string]any) {
	t.Helper()
	form := url.Values{
		"grant_type":  {oauthmeta.DeviceGrantType},
		"device_code": {deviceCode},
		"client_id":   {clientID},
	}
	req, err := http.NewRequest("POST", e.http.URL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, raw := e.do(t, req)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// mcpText runs one tool call over the real MCP transport with the given bearer
// token and returns the concatenated text content plus the tool's error flag.
func mcpText(t *testing.T, endpoint, token, tool string, args map[string]any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "device-e2e", Version: "1.0"}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint: endpoint + "/mcp",
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{
			base: http.DefaultTransport, token: token,
		}},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("MCP connect with the device-issued token failed: %v", err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", tool, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

// TestDeviceFlowE2EHeadlessOnboarding is the card's happy path, with every hop
// performed over HTTP and no browser anywhere: a harness asks for a code, the
// user_code arrives by EMAIL, the person approves it on the dashboard (which is
// where the email links), the harness polls a token, and an MCP tool call then
// runs on that credential.
func TestDeviceFlowE2EHeadlessOnboarding(t *testing.T) {
	env := newDeviceFlowEnv(t)
	clientID := env.registerDeviceClient(t, "claude-desktop-e2e")

	start := env.startDevice(t, clientID, oauthmeta.ScopeRead, env.email)
	if start.ExpiresIn != 600 || start.Interval != 5 {
		t.Fatalf("expires_in/interval = %d/%d, want 600/5", start.ExpiresIn, start.Interval)
	}

	// The email IS the consent screen.
	mail := env.mail.wait(t)
	if mail.To != env.email {
		t.Fatalf("consent email went to %q, want %q", mail.To, env.email)
	}
	t.Logf("consent email:\n%s", mail.Body)
	for _, want := range []string{start.UserCode, "claude-desktop-e2e", "read-only", "do not share this code"} {
		if !strings.Contains(mail.Body, want) {
			t.Fatalf("consent email missing %q:\n%s", want, mail.Body)
		}
	}

	// Polling before approval is authorization_pending.
	if code, body := env.pollToken(t, clientID, start.DeviceCode); code != http.StatusBadRequest || body["error"] != "authorization_pending" {
		t.Fatalf("pending poll = %d %v, want 400 authorization_pending", code, body)
	}

	// The mail's link: the consent card resolves the code for the signed-in
	// person and shows what is being asked for.
	resp, raw := env.do(t, env.authed(t, "GET", "/v1/auth/connected-apps/device?user_code="+url.QueryEscape(start.UserCode), nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent card = %d body=%s", resp.StatusCode, raw)
	}
	var consent deviceConsentOut
	if err := json.Unmarshal(raw, &consent); err != nil {
		t.Fatalf("decode consent card %s: %v", raw, err)
	}
	if consent.State != "pending" || consent.Client != "claude-desktop-e2e" ||
		strings.Join(consent.Scopes, " ") != oauthmeta.ScopeRead || !strings.Contains(consent.Scope, "read-only") {
		t.Fatalf("consent card = %+v, want pending / claude-desktop-e2e / read / plain words", consent)
	}

	resp, raw = env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/device",
		strings.NewReader(`{"user_code":"`+start.UserCode+`","decision":"approve"}`)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d body=%s", resp.StatusCode, raw)
	}

	// Stand in for the five seconds a well-behaved client waits between polls.
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE oauth_device_codes SET last_polled_at = now() - interval '10 seconds' WHERE user_code_hash = $1`,
		hashToken(start.UserCode)); err != nil {
		t.Fatalf("backdate poll: %v", err)
	}

	code, tok := env.pollToken(t, clientID, start.DeviceCode)
	if code != http.StatusOK {
		t.Fatalf("token exchange = %d %v", code, tok)
	}
	access, _ := tok["access_token"].(string)
	refresh, _ := tok["refresh_token"].(string)
	if access == "" || refresh == "" || tok["token_type"] != "Bearer" {
		t.Fatalf("token response = %v", tok)
	}
	if tok["scope"] != oauthmeta.ScopeRead {
		t.Fatalf("granted scope = %v, want read (the device ladder's floor)", tok["scope"])
	}

	// THE POINT OF THE CARD: the harness's credential opens the real MCP
	// transport and a tool call returns this account's data.
	text, isErr := mcpText(t, env.http.URL, access, "list_products", map[string]any{})
	if isErr || !strings.Contains(text, "Device Flow Widget") {
		t.Fatalf("list_products on a device-issued token: error=%v text=%q", isErr, text)
	}
	// And the ladder is enforced at the MCP layer too: a read credential cannot
	// write, so "read by default" is a real limit rather than a label.
	wtext, werr := mcpText(t, env.http.URL, access, "update_profile_language", map[string]any{"lang": "ar"})
	if !werr {
		t.Fatalf("a read-only device credential was allowed to write: %q", wtext)
	}
	var lang string
	if err := env.pool.QueryRow(context.Background(), `SELECT COALESCE(lang,'') FROM users WHERE id = $1`, env.userID).Scan(&lang); err != nil {
		t.Fatalf("read users.lang: %v", err)
	}
	if lang != "" {
		t.Fatalf("users.lang = %q after a refused write — the gate ran after the write", lang)
	}
}

// TestConnectedAppsDashboardScopeLadderE2E drives the dashboard's Connected apps
// routes over HTTP against the real store: widening needs confirm, every change
// rotates the pair, and revoke kills both halves at once.
func TestConnectedAppsDashboardScopeLadderE2E(t *testing.T) {
	env := newDeviceFlowEnv(t)
	ctx := context.Background()
	clientID := env.registerDeviceClient(t, "cursor-e2e")

	// A live read pair, as a device authorisation would have left it.
	access, refresh := newOAuthToken(), newOAuthToken()
	now := time.Now().UTC()
	refreshExp := now.Add(24 * time.Hour)
	if err := env.store.CreateToken(ctx, &dbstore.OAuthToken{
		TokenHash: hashToken(access), RefreshHash: hashToken(refresh),
		ClientID: clientID, UserID: env.userID, AccountID: env.acctID,
		Scope:           []string{oauthmeta.ScopeRead},
		AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: &refreshExp,
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	resp, raw := env.do(t, env.authed(t, "GET", "/v1/auth/connected-apps", nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d body=%s", resp.StatusCode, raw)
	}
	var apps []connectedAppOut
	if err := json.Unmarshal(raw, &apps); err != nil {
		t.Fatalf("decode list %s: %v", raw, err)
	}
	if len(apps) != 1 || strings.Join(apps[0].Scopes, " ") != oauthmeta.ScopeRead || apps[0].Name != "cursor-e2e" {
		t.Fatalf("connected apps = %+v, want one read row named cursor-e2e", apps)
	}
	id := apps[0].ID

	// WIDENING WITHOUT CONFIRM MUST BE REFUSED, and must change nothing.
	resp, raw = env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/"+id+"/scope",
		strings.NewReader(`{"scope":"readwrite"}`)))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unconfirmed upgrade = %d body=%s, want 400", resp.StatusCode, raw)
	}
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(access)); err != nil {
		t.Fatalf("a refused upgrade killed the existing credential: %v", err)
	}

	// UPGRADE via the dashboard: one confirm is the consent, and the pair ROTATES.
	resp, raw = env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/"+id+"/scope",
		strings.NewReader(`{"scope":"readwrite","confirm":true}`)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upgrade = %d body=%s", resp.StatusCode, raw)
	}
	var up struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		Scope         string `json:"scope"`
		PreviousScope string `json:"previous_scope"`
		AccessToken   string `json:"access_token"`
		RefreshToken  string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &up); err != nil {
		t.Fatalf("decode upgrade %s: %v", raw, err)
	}
	if up.Status != "rotated" || up.Scope != oauthmeta.ScopeReadWrite || up.PreviousScope != oauthmeta.ScopeRead {
		t.Fatalf("upgrade response = %+v", up)
	}
	// The rotation replaced the pair, so the id the list showed is dead too: the
	// next change acts on the id the rotation returned.
	id = up.ID
	if up.AccessToken == "" || up.RefreshToken == "" {
		t.Fatalf("upgrade did not return the new credential: %+v", up)
	}
	// The OLD pair is dead — both halves, not just the access token.
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(access)); err != dbstore.ErrAccessTokenInvalid {
		t.Fatalf("old access after upgrade = %v, want ErrAccessTokenInvalid", err)
	}
	if _, err := env.store.RefreshGrant(ctx, hashToken(refresh), hashToken("a1"), hashToken("a2"),
		now.Add(time.Hour), now.Add(time.Hour)); err != dbstore.ErrRefreshInvalid {
		t.Fatalf("old refresh after upgrade = %v, want ErrRefreshInvalid (a leaked refresh would outlive the upgrade)", err)
	}
	got, err := env.store.AccessTokenByHash(ctx, hashToken(up.AccessToken))
	if err != nil || strings.Join(got.Scope, " ") != oauthmeta.ScopeReadWrite {
		t.Fatalf("new access = %v / %v, want readwrite", got, err)
	}

	// The audit event carries both scopes and the route.
	var props string
	if err := env.pool.QueryRow(ctx,
		`SELECT properties::text FROM events
		 WHERE account_id = $1 AND event_name = 'oauth.scope_changed'
		 ORDER BY id DESC LIMIT 1`, env.acctID).Scan(&props); err != nil {
		t.Fatalf("read oauth.scope_changed audit event: %v", err)
	}
	for _, want := range []string{`"from": "read"`, `"to": "readwrite"`, `"route": "dashboard"`} {
		if !strings.Contains(props, want) {
			t.Fatalf("audit event %s missing %s", props, want)
		}
	}

	// DOWNGRADE needs no confirm, takes effect immediately, and also rotates.
	resp, raw = env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/"+id+"/scope",
		strings.NewReader(`{"scope":"read"}`)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("downgrade = %d body=%s", resp.StatusCode, raw)
	}
	var down struct {
		ID           string `json:"id"`
		Scope        string `json:"scope"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &down); err != nil {
		t.Fatalf("decode downgrade %s: %v", raw, err)
	}
	if down.Scope != oauthmeta.ScopeRead || down.AccessToken == "" {
		t.Fatalf("downgrade response = %+v", down)
	}
	id = down.ID
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(up.AccessToken)); err != dbstore.ErrAccessTokenInvalid {
		t.Fatalf("readwrite access after downgrade = %v, want ErrAccessTokenInvalid", err)
	}

	// REVOKE from Connected apps: access AND refresh die in the same request.
	resp, raw = env.do(t, env.authed(t, "POST", "/v1/auth/connected-apps/"+id+"/revoke", nil))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke = %d body=%s", resp.StatusCode, raw)
	}
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(down.AccessToken)); err != dbstore.ErrAccessTokenInvalid {
		t.Fatalf("access after revoke = %v, want ErrAccessTokenInvalid", err)
	}
	if _, err := env.store.RefreshGrant(ctx, hashToken(down.RefreshToken), hashToken("r1"), hashToken("r2"),
		now.Add(time.Hour), now.Add(time.Hour)); err != dbstore.ErrRefreshInvalid {
		t.Fatalf("refresh after revoke = %v, want ErrRefreshInvalid", err)
	}
	resp, raw = env.do(t, env.authed(t, "GET", "/v1/auth/connected-apps", nil))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("list after revoke = %d %s, want []", resp.StatusCode, raw)
	}
	// A malformed id names no row: it must read as "not active" (404), not as a
	// Postgres uuid-cast failure surfacing as a 500.
	malformed := []struct{ method, path string }{
		{"POST", "/v1/auth/connected-apps/not-a-uuid/revoke"},
		{"POST", "/v1/auth/connected-apps/not-a-uuid/scope"},
	}
	for _, m := range malformed {
		body := io.Reader(nil)
		if m.path != "" && strings.HasSuffix(m.path, "/scope") {
			body = strings.NewReader(`{"scope":"read"}`)
		}
		resp, raw = env.do(t, env.authed(t, m.method, m.path, body))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s = %d body=%s, want 404", m.method, m.path, resp.StatusCode, raw)
		}
	}

	// No email was sent by any dashboard action: the ladder's route (a) does not
	// touch the mail path at all.
	if !env.mail.empty() {
		t.Fatal("a dashboard scope change sent a consent email; route (a) must not need one")
	}
}

// TestConnectedAppsMCPToolsE2E is the MCP half of Connected apps, driven over
// the real transport with a real OAuth credential (AGENTS.md: every dashboard
// feature needs a matching MCP tool). Three properties are what make the parity
// SAFE rather than merely present:
//
//  1. an agent can see the credentials it holds (list_connected_apps);
//  2. it can LOWER one, and the rotation kills the old pair;
//  3. it can never RAISE one — widening needs a human consent screen, and an
//     agent that could widen its own grant would make the ladder theatre.
func TestConnectedAppsMCPToolsE2E(t *testing.T) {
	env := newDeviceFlowEnv(t)
	ctx := context.Background()
	clientID := env.registerDeviceClient(t, "mcp-parity-e2e")
	now := time.Now().UTC()
	refreshExp := now.Add(24 * time.Hour)

	seed := func() (access, refresh, id string) {
		access, refresh = newOAuthToken(), newOAuthToken()
		if err := env.store.CreateToken(ctx, &dbstore.OAuthToken{
			TokenHash: hashToken(access), RefreshHash: hashToken(refresh),
			ClientID: clientID, UserID: env.userID, AccountID: env.acctID,
			Scope:           []string{oauthmeta.ScopeReadWrite},
			AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: &refreshExp,
		}); err != nil {
			t.Fatalf("seed token: %v", err)
		}
		if err := env.pool.QueryRow(ctx,
			`SELECT id::text FROM oauth_tokens WHERE token_hash = $1`, hashToken(access)).Scan(&id); err != nil {
			t.Fatalf("read seeded token id: %v", err)
		}
		return access, refresh, id
	}
	access, _, id := seed()
	spareAccess, spareRefresh, spareID := seed()

	// 1. The agent sees both of its live credentials, with their scopes.
	text, isErr := mcpText(t, env.http.URL, access, "list_connected_apps", map[string]any{})
	if isErr {
		t.Fatalf("list_connected_apps failed: %s", text)
	}
	var listed struct {
		Items []struct {
			ID     string   `json:"id"`
			Client string   `json:"client_id"`
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &listed); err != nil {
		t.Fatalf("decode list_connected_apps %q: %v", text, err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("list_connected_apps returned %d rows, want 2: %s", len(listed.Items), text)
	}
	seen := map[string]bool{}
	for _, row := range listed.Items {
		if row.Name != "mcp-parity-e2e" || strings.Join(row.Scopes, " ") != oauthmeta.ScopeReadWrite {
			t.Fatalf("row = %+v, want mcp-parity-e2e / readwrite", row)
		}
		seen[row.ID] = true
	}
	if !seen[id] || !seen[spareID] {
		t.Fatalf("list_connected_apps did not return the seeded ids: %s", text)
	}

	// 2. WIDENING THROUGH THE TOOL IS REFUSED, and changes nothing.
	if out, werr := mcpText(t, env.http.URL, access, "set_connected_app_scope",
		map[string]any{"id": id, "scope": "readwrite"}); !werr {
		t.Fatalf("an agent widened its own grant through MCP: %q", out)
	}
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(access)); err != nil {
		t.Fatalf("a refused widening killed the existing credential: %v", err)
	}

	// 3. REVOKE kills both halves of ONE pair and leaves the caller's own alone.
	if out, werr := mcpText(t, env.http.URL, access, "revoke_connected_app",
		map[string]any{"id": spareID}); werr {
		t.Fatalf("revoke_connected_app failed: %s", out)
	}
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(spareAccess)); err != dbstore.ErrAccessTokenInvalid {
		t.Fatalf("revoked access = %v, want ErrAccessTokenInvalid", err)
	}
	if _, err := env.store.RefreshGrant(ctx, hashToken(spareRefresh), hashToken("x1"), hashToken("x2"),
		now.Add(time.Hour), now.Add(time.Hour)); err != dbstore.ErrRefreshInvalid {
		t.Fatalf("revoked refresh = %v, want ErrRefreshInvalid (the pair must die together)", err)
	}
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(access)); err != nil {
		t.Fatalf("revoking one connection killed another: %v", err)
	}

	// 4. LOWERING works, and it ROTATES: the old pair is dead in the same call.
	text, isErr = mcpText(t, env.http.URL, access, "set_connected_app_scope",
		map[string]any{"id": id, "scope": "read"})
	if isErr {
		t.Fatalf("set_connected_app_scope lowered a grant and failed: %s", text)
	}
	var lowered struct {
		Status       string `json:"status"`
		Scope        string `json:"scope"`
		NewAccess    string `json:"access_token"`
		NewRefresh   string `json:"refresh_token"`
		PreviousScop string `json:"previous_scope"`
	}
	if err := json.Unmarshal([]byte(text), &lowered); err != nil {
		t.Fatalf("decode set_connected_app_scope %q: %v", text, err)
	}
	if lowered.Status != "rotated" || lowered.Scope != oauthmeta.ScopeRead ||
		lowered.PreviousScop != oauthmeta.ScopeReadWrite || lowered.NewAccess == "" || lowered.NewRefresh == "" {
		t.Fatalf("lowering response = %+v", lowered)
	}
	if _, err := env.store.AccessTokenByHash(ctx, hashToken(access)); err != dbstore.ErrAccessTokenInvalid {
		t.Fatalf("old access after an MCP downgrade = %v, want ErrAccessTokenInvalid", err)
	}

	// The rotated credential is read-only at the MCP layer too: a write tool is
	// refused and nothing is written.
	if out, werr := mcpText(t, env.http.URL, lowered.NewAccess, "update_profile_language",
		map[string]any{"lang": "ar"}); !werr {
		t.Fatalf("a read-only rotated credential was allowed to write: %q", out)
	}
	var lang string
	if err := env.pool.QueryRow(ctx, `SELECT COALESCE(lang,'') FROM users WHERE id = $1`, env.userID).Scan(&lang); err != nil {
		t.Fatalf("read users.lang: %v", err)
	}
	if lang != "" {
		t.Fatalf("users.lang = %q after a refused write", lang)
	}
}

// TestRefreshCannotWidenScopeE2E is the BITE-PROOF for the scope ladder, run
// against the REAL store and the REAL token endpoint.
//
// Its unit-test twin (TestRefreshCannotWidenScope) drives an in-memory fake
// oauthStore, which is the wrong side of the contract for this property: the
// rule "a refresh keeps the grant" lives in dbstore.RefreshGrant's SQL
// (`Scope: old.Scope`), and a fake cannot regress it. Verified by mutation on
// this tree: changing that line to mint a widened scope left the fake-backed
// test GREEN (it asserted on the fake's own return value); this test goes RED
// on the same mutation, which is the only reason it exists.
func TestRefreshCannotWidenScopeE2E(t *testing.T) {
	env := newDeviceFlowEnv(t)
	ctx := context.Background()
	clientID := env.registerDeviceClient(t, "refresh-ladder-e2e")

	access, refresh := newOAuthToken(), newOAuthToken()
	now := time.Now().UTC()
	refreshExp := now.Add(24 * time.Hour)
	if err := env.store.CreateToken(ctx, &dbstore.OAuthToken{
		TokenHash: hashToken(access), RefreshHash: hashToken(refresh),
		ClientID: clientID, UserID: env.userID, AccountID: env.acctID,
		Scope:           []string{oauthmeta.ScopeRead},
		AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: &refreshExp,
	}); err != nil {
		t.Fatalf("seed read pair: %v", err)
	}

	// A refresh that ASKS for the wider scope. RFC 6749 §6: the response must
	// not carry a scope the grant did not already hold — the parameter is not an
	// upgrade path, and honouring it would make the whole ladder theatre.
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
		"scope":         {oauthmeta.ScopeReadWrite},
	}
	req, err := http.NewRequest("POST", env.http.URL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build refresh: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, raw := env.do(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d body=%s", resp.StatusCode, raw)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode refresh %s: %v", raw, err)
	}
	if out.Scope != oauthmeta.ScopeRead {
		t.Fatalf("refresh granted scope %q, want read — widening through refresh must be impossible", out.Scope)
	}
	// The credential ON THE WIRE, not just the echo.
	got, err := env.store.AccessTokenByHash(ctx, hashToken(out.AccessToken))
	if err != nil {
		t.Fatalf("rotated access token: %v", err)
	}
	if dbstore.ScopeHasWrite(got.Scope) {
		t.Fatalf("rotated token holds write scope %v after asking for readwrite on refresh", got.Scope)
	}
	// And the refresh token rotates: the one that was presented stops working.
	if _, err := env.store.RefreshGrant(ctx, hashToken(refresh), hashToken("n1"), hashToken("n2"),
		now.Add(time.Hour), now.Add(time.Hour)); err != dbstore.ErrRefreshInvalid {
		t.Fatalf("presented refresh token after rotation = %v, want ErrRefreshInvalid", err)
	}
}
