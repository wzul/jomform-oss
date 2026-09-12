package httpapi

// Issue #511 regression: the redirect_uri acceptance rule must be the SAME at
// token exchange as at authorize. Before this fix the authorize step used the
// loopback-aware rule from #505 while the token step compared exact strings, so
// a native client that authorized with one ephemeral loopback port and
// exchanged the code with another was rejected with `invalid_grant` at the very
// last step — after the user had already consented.
//
// The fix extracts ONE helper (redirectURIMatches) that both call sites use, so
// the two steps cannot drift again. These tests pin:
//
//   - loopback, same scheme+host+path, DIFFERENT port -> accepted
//   - different host / path / scheme -> rejected (no code-injection weakening)
//   - non-loopback keeps exact semantics
//   - an end-to-end authorize -> token run where the token step presents a
//     DIFFERENT loopback port than authorize did (the actual bug)
//   - the redirect_uri == "" omission behaviour that was deliberately kept

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestRedirectURIMatchesRule is the unit-level pin of the shared helper. The
// stored value plays the part of what authorize recorded on the code; the
// presented value is what the token request sends.
func TestRedirectURIMatchesRule(t *testing.T) {
	cases := []struct {
		name      string
		stored    string
		presented string
		want      bool
	}{
		// ── loopback: only the port may differ (RFC 8252 §7.3, #505/#511) ──
		{"loopback ipv4 same port", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/callback", true},
		{"loopback ipv4 different port", "http://127.0.0.1:61234/callback", "http://127.0.0.1:5555/callback", true},
		{"loopback ipv4 no port vs port", "http://127.0.0.1/callback", "http://127.0.0.1:61234/callback", true},
		{"loopback localhost different port", "http://localhost:5555/callback", "http://localhost:61234/callback", true},
		{"loopback localhost case-insensitive host", "http://LOCALHOST:5555/callback", "http://localhost:61234/callback", true},
		{"loopback any 127/8 address", "http://127.9.9.9:5555/callback", "http://127.9.9.9:61234/callback", true},
		{"loopback ipv6 different port", "http://[::1]:5555/callback", "http://[::1]:61234/callback", true},
		{"loopback ipv6 no port vs port", "http://[::1]/callback", "http://[::1]:61234/callback", true},

		// ── loopback: host/path must still be byte-identical ──
		{"loopback different path", "http://127.0.0.1:61234/callback", "http://127.0.0.1:5555/steal", false},
		{"loopback path prefix is not enough", "http://127.0.0.1:61234/callback", "http://127.0.0.1:5555/callback/extra", false},
		{"loopback empty path vs callback path", "http://127.0.0.1:61234/callback", "http://127.0.0.1:5555", false},
		{"loopback different host literal", "http://127.0.0.1:61234/callback", "http://127.0.0.2:5555/callback", false},
		{"loopback name vs literal are different hosts", "http://127.0.0.1:61234/callback", "http://localhost:5555/callback", false},
		{"loopback lookalike host", "http://127.0.0.1:61234/callback", "http://127.0.0.1.evil.com:5555/callback", false},
		{"loopback different scheme", "https://127.0.0.1:61234/callback", "http://127.0.0.1:5555/callback", false},
		{"loopback presented userinfo", "http://127.0.0.1:61234/callback", "http://user:pass@127.0.0.1:5555/callback", false},
		{"loopback stored userinfo", "http://user:pass@127.0.0.1:61234/callback", "http://127.0.0.1:5555/callback", false},
		{"loopback userinfo host trick", "http://127.0.0.1:61234/callback", "http://127.0.0.1:5555@evil.com/callback", false},

		// ── non-loopback: exact only, even for a port change ──
		{"non-loopback exact", "https://client.example.com:5555/cb", "https://client.example.com:5555/cb", true},
		{"non-loopback different port rejected", "https://client.example.com:5555/cb", "https://client.example.com:61234/cb", false},
		{"non-loopback different path rejected", "https://client.example.com:5555/cb", "https://client.example.com:5555/other", false},
		{"non-loopback different host rejected", "https://client.example.com/cb", "https://evil.example.com/cb", false},
		{"non-loopback different scheme rejected", "https://client.example.com/cb", "http://client.example.com/cb", false},
		{"non-loopback path prefix is not enough", "https://client.example.com/cb", "https://client.example.com/cb/extra", false},
		{"non-loopback suffix host rejected", "https://client.example.com/cb", "https://client.example.com.evil.com/cb", false},
		{"http non-loopback never relaxes", "http://client.example.com:5555/cb", "http://client.example.com:61234/cb", false},

		// ── malformed / empty ──
		{"both empty", "", "", true},
		{"stored empty presented non-empty", "", "https://client.example.com/cb", false},
		{"stored non-empty presented empty", "https://client.example.com/cb", "", false},
		{"stored loopback presented empty", "http://127.0.0.1:5555/callback", "", false},
		{"relative stored", "callback", "callback", true},
		{"relative stored vs other relative", "callback", "steal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redirectURIMatches(tc.stored, tc.presented); got != tc.want {
				t.Errorf("redirectURIMatches(%q, %q) = %v, want %v", tc.stored, tc.presented, got, tc.want)
			}
		})
	}
}

// TestAuthorizeAndTokenShareOneRule asserts the two call sites really are the
// same predicate: for every URI pair, the answer authorize gives (via
// redirectURIAllowed over a client that registered `stored`) must equal the
// answer token exchange gives (via redirectURIMatches). This is the drift guard
// the issue asks for — a future change to one site alone fails here.
func TestAuthorizeAndTokenShareOneRule(t *testing.T) {
	pairs := []struct {
		name       string
		registered string
		presented  string
	}{
		{"loopback different port", "http://127.0.0.1:5555/callback", "http://127.0.0.1:61234/callback"},
		{"loopback same port", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/callback"},
		{"loopback different path", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/steal"},
		{"loopback different host", "http://127.0.0.1:5555/callback", "http://localhost:5555/callback"},
		{"loopback different scheme", "http://127.0.0.1:5555/callback", "https://127.0.0.1:5555/callback"},
		{"non-loopback exact", "https://client.example.com/cb", "https://client.example.com/cb"},
		{"non-loopback different port", "https://client.example.com:5555/cb", "https://client.example.com:61234/cb"},
		{"non-loopback different host", "https://client.example.com/cb", "https://evil.example.com/cb"},
	}
	for _, tc := range pairs {
		t.Run(tc.name, func(t *testing.T) {
			client := &dbstore.OAuthClient{ClientID: "jfc_shared_rule_probe", RedirectURIs: []string{tc.registered}}
			atAuthorize := redirectURIAllowed(client, tc.presented)
			atToken := redirectURIMatches(tc.registered, tc.presented)
			if atAuthorize != atToken {
				t.Errorf("authorize(%q -> %q) = %v but token(%q -> %q) = %v — the two steps must share ONE rule",
					tc.registered, tc.presented, atAuthorize, tc.registered, tc.presented, atToken)
			}
		})
	}
}

// TestTokenAcceptsChangedLoopbackPort is the direct regression for the token
// step: the code was issued for one loopback URI and the exchange presents a
// different port (same scheme+host+path) — the token must be issued.
func TestTokenAcceptsChangedLoopbackPort(t *testing.T) {
	setProdMode(t) // loopback relaxation lives in the same rule regardless of mode

	cases := []struct {
		name          string
		authorizedURI string
		presentedURI  string
	}{
		{"ipv4 port 5555 -> 61234", "http://127.0.0.1:5555/callback", "http://127.0.0.1:61234/callback"},
		{"ipv4 no port -> port", "http://127.0.0.1/callback", "http://127.0.0.1:61234/callback"},
		{"localhost changed port", "http://localhost:5555/callback", "http://localhost:61234/callback"},
		{"ipv6 changed port", "http://[::1]:5555/callback", "http://[::1]:61234/callback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, tc.authorizedURI)
			verifier := "verifier-511"
			rec := performAuthorize(t, o, clientID, tc.authorizedURI, verifier, "state-511")
			if rec.Code != http.StatusFound {
				t.Fatalf("authorize(%q) = %d, want 302 (body %s)", tc.authorizedURI, rec.Code, rec.Body.String())
			}
			code := mustQuery(t, rec.Header().Get("Location")).Get("code")
			if code == "" {
				t.Fatalf("no code in redirect: %q", rec.Header().Get("Location"))
			}

			trec := postTokenForm(t, o, url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {tc.presentedURI},
				"client_id":     {clientID},
				"code_verifier": {verifier},
			})
			if trec.Code != http.StatusOK {
				t.Fatalf("token(redirect_uri=%q) = %d, want 200 (body %s)", tc.presentedURI, trec.Code, trec.Body.String())
			}
			var tok struct {
				AccessToken string `json:"access_token"`
			}
			decodeJSONBody(t, trec, &tok)
			if tok.AccessToken == "" {
				t.Fatal("no access_token in a 200 response")
			}
		})
	}
}

// TestTokenRejectsMismatchedRedirectURI is the scoping guard: a different host,
// path or scheme at token exchange is still rejected with invalid_grant (the
// check must not become a loose match).
func TestTokenRejectsMismatchedRedirectURI(t *testing.T) {
	setProdMode(t)

	cases := []struct {
		name          string
		authorizedURI string
		presentedURI  string
	}{
		{"different path", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/steal"},
		{"different path prefix not enough", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/callback/extra"},
		{"different host literal", "http://127.0.0.1:5555/callback", "http://127.0.0.2:5555/callback"},
		{"name vs literal are different hosts", "http://127.0.0.1:5555/callback", "http://localhost:5555/callback"},
		{"different scheme", "http://127.0.0.1:5555/callback", "https://127.0.0.1:5555/callback"},
		{"non-loopback host", "http://127.0.0.1:5555/callback", "http://evil.com:5555/callback"},
		{"lookalike host", "http://127.0.0.1:5555/callback", "http://127.0.0.1.evil.com:5555/callback"},
		{"userinfo trick", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555@evil.com/callback"},
		{"non-loopback different port", "https://client.example.com:5555/cb", "https://client.example.com:61234/cb"},
		{"non-loopback different path", "https://client.example.com:5555/cb", "https://client.example.com:5555/other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, tc.authorizedURI)
			verifier := "verifier-511-reject"
			rec := performAuthorize(t, o, clientID, tc.authorizedURI, verifier, "state-511")
			if rec.Code != http.StatusFound {
				t.Fatalf("authorize(%q) = %d, want 302 (body %s)", tc.authorizedURI, rec.Code, rec.Body.String())
			}
			code := mustQuery(t, rec.Header().Get("Location")).Get("code")

			trec := postTokenForm(t, o, url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {tc.presentedURI},
				"client_id":     {clientID},
				"code_verifier": {verifier},
			})
			if trec.Code != http.StatusBadRequest {
				t.Fatalf("token(redirect_uri=%q) = %d, want 400 (body %s)", tc.presentedURI, trec.Code, trec.Body.String())
			}
			var out map[string]string
			decodeJSONBody(t, trec, &out)
			if out["error"] != "invalid_grant" {
				t.Errorf("error = %q, want invalid_grant", out["error"])
			}
		})
	}
}

// TestTokenNonLoopbackKeepsExactMatch is the non-loopback regression at the
// token step: the exact URI is accepted and the exact URI still works.
func TestTokenNonLoopbackKeepsExactMatch(t *testing.T) {
	setProdMode(t)

	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	uri := "https://client.example.com:5555/cb"
	clientID := registerTestClient(t, o, uri)

	rec := performAuthorize(t, o, clientID, uri, "verifier-exact", "state-exact")
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")

	trec := postTokenForm(t, o, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {uri},
		"client_id":     {clientID},
		"code_verifier": {"verifier-exact"},
	})
	if trec.Code != http.StatusOK {
		t.Fatalf("token(exact) = %d, want 200 (body %s)", trec.Code, trec.Body.String())
	}
}

// TestTokenOmittedRedirectURIStaysTolerated pins the `redirect_uri == ""`
// decision reported in the PR: the omission is deliberately KEPT lenient (the
// parameter is not required here), while a present-but-different value is still
// rejected.
func TestTokenOmittedRedirectURIStaysTolerated(t *testing.T) {
	setProdMode(t)

	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	uri := "http://127.0.0.1:5555/callback"
	clientID := registerTestClient(t, o, uri)

	rec := performAuthorize(t, o, clientID, uri, "verifier-omit", "state-omit")
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")

	// Omitted redirect_uri → accepted (documented leniency).
	trec := postTokenForm(t, o, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"code_verifier": {"verifier-omit"},
	})
	if trec.Code != http.StatusOK {
		t.Fatalf("token(omitted redirect_uri) = %d, want 200 — the omission is deliberately tolerated (body %s)",
			trec.Code, trec.Body.String())
	}

	// A DIFFERENT present value on a fresh code is still rejected.
	rec2 := performAuthorize(t, o, clientID, uri, "verifier-omit2", "state-omit2")
	if rec2.Code != http.StatusFound {
		t.Fatalf("authorize 2 = %d, want 302", rec2.Code)
	}
	code2 := mustQuery(t, rec2.Header().Get("Location")).Get("code")
	trec2 := postTokenForm(t, o, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code2},
		"redirect_uri":  {"http://127.0.0.1:5555/steal"},
		"client_id":     {clientID},
		"code_verifier": {"verifier-omit2"},
	})
	if trec2.Code != http.StatusBadRequest {
		t.Fatalf("token(different redirect_uri) = %d, want 400 (body %s)", trec2.Code, trec2.Body.String())
	}
}

// TestOAuth511LoopbackEndToEndDifferentTokenPort is the end-to-end regression
// for the actual bug, over a real httptest server routed through
// RegisterRoutes (prod mode): register → authorize with ONE loopback port →
// token with a DIFFERENT loopback port. The user has already consented by the
// time the token step runs, so this is the step that must accept it.
func TestOAuth511LoopbackEndToEndDifferentTokenPort(t *testing.T) {
	setProdMode(t)

	o, store, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	mux := http.NewServeMux()
	o.RegisterRoutes(mux, 1000, 1000)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Register a native-client-style loopback redirect.
	registered := "http://127.0.0.1:5555/callback"
	regBody, _ := json.Marshal(map[string]any{"client_name": "Claude Code", "redirect_uris": []string{registered}})
	resp, err := http.Post(srv.URL+"/oauth/register", "application/json", strings.NewReader(string(regBody)))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regRaw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", resp.StatusCode, regRaw)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(regRaw, &reg); err != nil || reg.ClientID == "" {
		t.Fatalf("register response: %v %s", err, regRaw)
	}

	// 2. Authorize with a DIFFERENT ephemeral port than registered (#505) and a
	//    logged-in session cookie.
	verifier := "e2e-511-code-verifier-0123456789"
	authorizedPort := "http://127.0.0.1:61234/callback"
	authURL := srv.URL + "/oauth/authorize?client_id=" + url.QueryEscape(reg.ClientID) +
		"&redirect_uri=" + url.QueryEscape(authorizedPort) +
		"&response_type=code&code_challenge=" + url.QueryEscape(pkcePair(verifier)) +
		"&code_challenge_method=S256&state=" + url.QueryEscape("e2e-511-state")
	req, _ := http.NewRequest("GET", authURL, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	aresp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	aresp.Body.Close()
	if aresp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", aresp.StatusCode)
	}
	loc := aresp.Header.Get("Location")
	if !strings.HasPrefix(loc, authorizedPort+"?") {
		t.Fatalf("authorize redirect = %q, want prefix %q", loc, authorizedPort)
	}
	code := mustQuery(t, loc).Get("code")
	if code == "" {
		t.Fatalf("no code in authorize redirect: %q", loc)
	}

	// 3. Exchange the code presenting YET ANOTHER loopback port — the client
	//    picked a fresh listener. Before #511 this was a 400 invalid_grant
	//    after the user had already consented.
	tokenPort := "http://127.0.0.1:62777/callback"
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", tokenPort)
	form.Set("client_id", reg.ClientID)
	form.Set("code_verifier", verifier)
	tresp, err := http.Post(srv.URL+"/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	tokRaw, _ := io.ReadAll(tresp.Body)
	tresp.Body.Close()
	if tresp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d body=%s (want 200 — a different loopback port must be accepted, issue #511)", tresp.StatusCode, tokRaw)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(tokRaw, &tok); err != nil {
		t.Fatalf("decode token response: %v body=%s", err, tokRaw)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" {
		t.Fatalf("incomplete token response: %+v", tok)
	}
	// The issued token must resolve in the store (only the hash is stored).
	if _, err := store.AccessTokenByHash(req.Context(), hashToken(tok.AccessToken)); err != nil {
		t.Fatalf("issued access token does not resolve in the store: %v", err)
	}
	t.Logf("registered=%s authorized=%s token_step=%s -> 200 (issue #511 fixed)", registered, authorizedPort, tokenPort)
}

// postTokenForm drives POST /oauth/token through the real handler with a form
// body and returns the recorder.
func postTokenForm(t *testing.T, o *OAuthHandlers, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(rec, req)
	return rec
}
