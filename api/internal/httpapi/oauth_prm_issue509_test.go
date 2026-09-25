package httpapi

// Issue #509 — OAuth 2.0 Protected Resource Metadata (RFC 9728) drift guards.
//
// The MCP spec (2025-06-18) makes PRM mandatory, and MCP clients MUST use it
// for authorization-server discovery. JomForm now publishes two discovery
// documents for one deployment, so the acceptance criteria here are about
// AGREEMENT, not only shape:
//
//  1. both PRM paths return a valid RFC 9728 document whose `resource` is the
//     MCP endpoint and whose `authorization_servers` is the OAuth issuer;
//  2. the PRM document and the RFC 8414 document cannot drift — resource ==
//     the AS metadata's mcp_endpoint, authorization_servers == its issuer, and
//     the scopes agree with what the token response issues;
//  3. the unauthenticated /mcp 401 carries `resource_metadata`, and that URL
//     is a path this server actually serves (not a 404);
//  4. the RFC 8707 `resource` parameter is validated on authorize and token.
//
// These are unit-level (fake OAuth store, no Postgres); the full
// discovery → DCR → authorize → token → authenticated /mcp walk over a real
// server + real database lives in oauth_prm_flow_issue509_test.go.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// newMCPServer builds a server with the MCP endpoint wired (nil OAuth store:
// API-key auth only) so the /mcp guard clause and the well-known discovery
// routes — which are registered with it — are reachable. The nil OAuth store
// is safe here: mcpsrv.authenticate only consults it when it is non-nil.
func newMCPServer() *Server {
	return NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{MCP: mcpsrv.New(&mcpsrv.Deps{}, nil, nil, nil)})
}

// hostOfBase is the host of the configured issuer (mcp.jomform.com in prod).
func hostOfBase() string {
	u, err := url.Parse(oauthmeta.Base())
	if err != nil {
		return ""
	}
	return u.Host
}

// prmRequest performs a GET against a well-known path for a given Host.
func prmRequest(t *testing.T, path, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	wellKnownProtectedResource(rec, req)
	return rec
}

// TestProtectedResourceDocumentShape: RFC 9728 minimum useful shape, on the
// default well-known location and the /mcp path-inserted form, with `resource`
// equal to the MCP endpoint the client calls.
func TestProtectedResourceDocumentShape(t *testing.T) {
	const host = "mcp.jomform.com"
	for _, path := range []string{
		oauthmeta.ProtectedResourcePath,
		oauthmeta.ProtectedResourcePathSuffixed,
	} {
		t.Run(path, func(t *testing.T) {
			rec := prmRequest(t, path, host)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("content-type = %q, want application/json", ct)
			}
			var doc struct {
				Resource             string   `json:"resource"`
				AuthorizationServers []string `json:"authorization_servers"`
				ScopesSupported      []string `json:"scopes_supported"`
				BearerMethods        []string `json:"bearer_methods_supported"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
			}
			if doc.Resource != "https://"+host+"/mcp" {
				t.Errorf("resource = %q, want %q (must be the MCP endpoint that was called)", doc.Resource, "https://"+host+"/mcp")
			}
			if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != oauthmeta.Base() {
				t.Errorf("authorization_servers = %v, want [%s]", doc.AuthorizationServers, oauthmeta.Base())
			}
			if len(doc.ScopesSupported) != 2 || doc.ScopesSupported[0] != oauthmeta.ScopeRead || doc.ScopesSupported[1] != oauthmeta.ScopeReadWrite {
				t.Errorf("scopes_supported = %v, want [%s %s]", doc.ScopesSupported, oauthmeta.ScopeRead, oauthmeta.ScopeReadWrite)
			}
			if len(doc.BearerMethods) != 1 || doc.BearerMethods[0] != "header" {
				t.Errorf("bearer_methods_supported = %v, want [header]", doc.BearerMethods)
			}
		})
	}
}

// TestProtectedResourceAgreesWithAuthorizationServerMetadata is THE drift
// guard: the PRM document and the AS metadata are built from the same source,
// so resource/issuer/scopes must agree for any host. If someone re-introduces a
// literal into either document this test fails.
func TestProtectedResourceAgreesWithAuthorizationServerMetadata(t *testing.T) {
	for _, host := range []string{"mcp.jomform.com", "mcp.staging.example", "localhost:8080"} {
		t.Run(host, func(t *testing.T) {
			prm := protectedResourceDocument(host)
			as := authorizationServerDocument(host)

			// 1. resource (PRM) == mcp_endpoint (AS metadata).
			if prm["resource"] != as["mcp_endpoint"] {
				t.Errorf("PRM resource = %v != AS mcp_endpoint = %v", prm["resource"], as["mcp_endpoint"])
			}
			// 2. authorization_servers == [AS issuer].
			servers, ok := prm["authorization_servers"].([]string)
			if !ok || len(servers) != 1 {
				t.Fatalf("authorization_servers = %v", prm["authorization_servers"])
			}
			if servers[0] != as["issuer"] {
				t.Errorf("PRM authorization_servers[0] = %q != AS issuer = %v", servers[0], as["issuer"])
			}
			// 3. on the issuer host itself (production) the resource must be
			//    anchored on the issuer, and both documents must describe the
			//    same host. A request that arrives on some other host still
			//    reports ITS MCP endpoint as the resource (that is what the
			//    client called), while the authorization server stays the
			//    configured issuer — which is exactly what the AS metadata
			//    advertises as mcp_endpoint too.
			if host == hostOfBase() && !strings.HasPrefix(prm["resource"].(string), oauthmeta.Base()) {
				t.Errorf("resource %v is not on the issuer base %q", prm["resource"], oauthmeta.Base())
			}
			// 4. scopes agree.
			prmScopes, _ := prm["scopes_supported"].([]string)
			asScopes, _ := as["scopes_supported"].([]string)
			if strings.Join(prmScopes, " ") != strings.Join(asScopes, " ") {
				t.Errorf("PRM scopes %v != AS scopes %v", prmScopes, asScopes)
			}
		})
	}
}

// TestTokenResponseScopeMatchesDiscovery pins the third leg: the scope the
// token endpoint returns is a scope BOTH discovery documents advertise.
//
// The card's item 5 wants the divergence class guarded, so this test does not
// just compare strings — it asserts the three-way relationship that must hold
// for ANY grant:
//
//	token response scope  ⊆  PRM scopes_supported  ==  AS scopes_supported
//
// It walks the grant space (no scope, `read`, `readwrite`) so a response built
// from a literal (the old bug: echoing ScopesSupported() regardless of what was
// issued) fails on the `read` case: the response would then claim
// "read readwrite" for a token that cannot write.
func TestTokenResponseScopeMatchesDiscovery(t *testing.T) {
	prmScopes := protectedResourceDocument("mcp.jomform.com")["scopes_supported"].([]string)
	asScopes := authorizationServerDocument("mcp.jomform.com")["scopes_supported"].([]string)

	// The two documents are one source today; assert it here too so a future
	// split shows up as this test failing rather than as a silent divergence.
	if strings.Join(prmScopes, " ") != strings.Join(asScopes, " ") {
		t.Fatalf("PRM scopes_supported %v != AS scopes_supported %v — the two discovery "+
			"documents must be built from oauthmeta.ScopesSupported()", prmScopes, asScopes)
	}

	cases := []struct {
		name      string
		requested string
		want      string
	}{
		{"no scope parameter -> the readwrite default", "", oauthmeta.ScopeReadWrite},
		{"read is granted as read", oauthmeta.ScopeRead, oauthmeta.ScopeRead},
		{"readwrite is granted as readwrite", oauthmeta.ScopeReadWrite, oauthmeta.ScopeReadWrite},
		{"both are granted in canonical order", oauthmeta.ScopeRead + " " + oauthmeta.ScopeReadWrite,
			oauthmeta.ScopeRead + " " + oauthmeta.ScopeReadWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issued := tokenScopeForRequest(t, tc.requested)

			if issued != tc.want {
				t.Errorf("token response scope = %q, want %q — the response must echo the ISSUED "+
					"grant, not the advertised list (a `read` client told it holds readwrite is a lie "+
					"about what the credential can do)", issued, tc.want)
			}
			// Every issued scope must be advertised by BOTH documents: that is
			// the actual divergence guard, and it holds whatever the grant is.
			for _, s := range strings.Fields(issued) {
				if !slices.Contains(asScopes, s) {
					t.Errorf("issued scope %q is not in the AS scopes_supported %v", s, asScopes)
				}
				if !slices.Contains(prmScopes, s) {
					t.Errorf("issued scope %q is not in the PRM scopes_supported %v", s, prmScopes)
				}
			}
			// And when the client asked for EVERY advertised scope, the grant
			// must be exactly that — i.e. the response is not a subset by
			// accident of the code always emitting one fixed value.
			//
			// The absent-scope default is deliberately NOT asserted here: it is
			// readwrite alone (item 2 — never `read`, or every existing client
			// loses write), which is a proper subset of the advertised set, so
			// requiring equality would contradict the default.
			if tc.requested == oauthmeta.ScopeRead+" "+oauthmeta.ScopeReadWrite {
				if issued != strings.Join(asScopes, " ") {
					t.Errorf("a request for every advertised scope issued %q but discovery advertises %q",
						issued, strings.Join(asScopes, " "))
				}
			}
		})
	}
}

// TestTokenResponseScopeIsNeverTheAdvertisedList is the direct bite proof for
// item 3: a `read` grant's token response must NOT contain readwrite.
//
// This is separate from the table above so the failure message names the exact
// defect (writeTokenResponse echoing ScopesSupported() instead of its scope
// argument) rather than a generic mismatch.
func TestTokenResponseScopeIsNeverTheAdvertisedList(t *testing.T) {
	issued := tokenScopeForRequest(t, oauthmeta.ScopeRead)
	if strings.Contains(issued, oauthmeta.ScopeReadWrite) {
		t.Fatalf("token response for a `read`-scoped authorize said %q — writeTokenResponse is "+
			"echoing the advertised list instead of the grant it was handed, so the client "+
			"cannot tell what it actually got (and is told it can write)", issued)
	}
}

// tokenScopeForRequest drives authorize (?scope=<requested>) -> token exchange
// through the real handlers and returns the `scope` the token response carried.
func tokenScopeForRequest(t *testing.T, requested string) string {
	t.Helper()
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")

	challenge := pkcePair("v")
	u := "/oauth/authorize?client_id=" + url.QueryEscape(clientID) +
		"&redirect_uri=" + url.QueryEscape("https://client.example.com/cb") +
		"&response_type=code&code_challenge=" + url.QueryEscape(challenge) +
		"&code_challenge_method=S256&state=s"
	if requested != "" {
		u += "&scope=" + url.QueryEscape(requested)
	}
	req := httptest.NewRequest("GET", u, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize(scope=%q) = %d body=%s", requested, rec.Code, rec.Body.String())
	}
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")
	if code == "" {
		t.Fatalf("authorize(scope=%q) redirected with no code: %q", requested, rec.Header().Get("Location"))
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", clientID)
	form.Set("code_verifier", "v")
	trec := httptest.NewRecorder()
	treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec, treq)
	if trec.Code != http.StatusOK {
		t.Fatalf("token(scope=%q) = %d body=%s", requested, trec.Code, trec.Body.String())
	}
	var tok struct {
		Scope string `json:"scope"`
	}
	decodeJSONBody(t, trec, &tok)
	return tok.Scope
}

// TestTokenRowCarriesTheResolvedGrant closes the chain between the two layers
// that this feature lives across.
//
// The authorize tests assert the CODE records the resolved grant, and the mcpsrv
// tests assert that a token ROW holding `read` is refused on writes. Neither
// asserts the link: that exchanging a `read` code actually STORES a token row
// whose scope is exactly ["read"]. If that link broke, the code would record
// read, the row would somehow carry readwrite, and every enforcement test would
// still pass while a merchant's read-only connection could write. So assert the
// persisted row, not the response — the MCP gate reads the row, not the response.
func TestTokenRowCarriesTheResolvedGrant(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested string
		want      []string
	}{
		{"read stays read on the token row", oauthmeta.ScopeRead, []string{oauthmeta.ScopeRead}},
		{"readwrite stays readwrite", oauthmeta.ScopeReadWrite, []string{oauthmeta.ScopeReadWrite}},
		{"absent defaults to readwrite", "", []string{oauthmeta.ScopeReadWrite}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, store, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, "https://client.example.com/cb")

			u := "/oauth/authorize?client_id=" + url.QueryEscape(clientID) +
				"&redirect_uri=" + url.QueryEscape("https://client.example.com/cb") +
				"&response_type=code&code_challenge=" + url.QueryEscape(pkcePair("v")) +
				"&code_challenge_method=S256&state=s"
			if tc.requested != "" {
				u += "&scope=" + url.QueryEscape(tc.requested)
			}
			areq := httptest.NewRequest("GET", u, nil)
			areq.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
			arec := httptest.NewRecorder()
			o.authorize(arec, areq)
			if arec.Code != http.StatusFound {
				t.Fatalf("authorize = %d body=%s", arec.Code, arec.Body.String())
			}
			code := mustQuery(t, arec.Header().Get("Location")).Get("code")

			form := url.Values{}
			form.Set("grant_type", "authorization_code")
			form.Set("code", code)
			form.Set("redirect_uri", "https://client.example.com/cb")
			form.Set("client_id", clientID)
			form.Set("code_verifier", "v")
			trec := httptest.NewRecorder()
			treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
			treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			o.token(trec, treq)
			if trec.Code != http.StatusOK {
				t.Fatalf("token = %d body=%s", trec.Code, trec.Body.String())
			}

			store.mu.Lock()
			if len(store.tokens) != 1 {
				store.mu.Unlock()
				t.Fatalf("expected exactly 1 stored token row, got %d", len(store.tokens))
			}
			rowScope := append([]string(nil), store.tokens[0].Scope...)
			store.mu.Unlock()

			if strings.Join(rowScope, " ") != strings.Join(tc.want, " ") {
				t.Errorf("stored token row scope = %v, want %v — the row is what the MCP write gate "+
					"reads, so a row that does not carry the resolved grant means a read-only "+
					"connection can write", rowScope, tc.want)
			}
		})
	}
}

// TestRefreshPreservesTheGrantAndNeverWidens pins the refresh half of item 3:
// rotating a `read` token must return `read`, not the advertised list. A refresh
// that widened the grant would turn a read-only connection into a write-capable
// one without the merchant approving anything, which is the whole failure this
// scope exists to prevent.
func TestRefreshPreservesTheGrantAndNeverWidens(t *testing.T) {
	o, store, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")

	// Mint a read token straight into the store (the authorize path is covered
	// above); RefreshGrant copies scope from the row it revokes. The handler
	// looks the row up by hashToken(refresh_token), so the fixture must store
	// the SAME derivation — a plain string would never be found.
	rawRefresh := "refresh-read-fixture"
	store.mu.Lock()
	store.tokens = append(store.tokens, &dbstore.OAuthToken{
		TokenHash: "h-access", RefreshHash: hashToken(rawRefresh),
		ClientID: clientID, UserID: "user-1", AccountID: "acct-1",
		Scope:           []string{oauthmeta.ScopeRead},
		AccessExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	store.mu.Unlock()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rawRefresh)
	form.Set("client_id", clientID)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d body=%s", rec.Code, rec.Body.String())
	}
	var tok struct {
		Scope string `json:"scope"`
	}
	decodeJSONBody(t, rec, &tok)
	if tok.Scope != oauthmeta.ScopeRead {
		t.Errorf("refreshed token response scope = %q, want %q — a read grant must not widen on "+
			"refresh", tok.Scope, oauthmeta.ScopeRead)
	}
	store.mu.Lock()
	last := store.tokens[len(store.tokens)-1]
	store.mu.Unlock()
	if strings.Join(last.Scope, " ") != oauthmeta.ScopeRead {
		t.Errorf("refreshed token ROW scope = %v, want [%s]", last.Scope, oauthmeta.ScopeRead)
	}
}

// ─── the requested scope at authorize (RFC 6749 §3.3 / §4.1.2.1) ─────────────

// TestAuthorizeHonoursRequestedScope is the card's acceptance criterion at the
// handler level: the grant recorded on the authorization code is EXACTLY what
// the client asked for.
func TestAuthorizeHonoursRequestedScope(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		want      []string
	}{
		{"read", "read", []string{oauthmeta.ScopeRead}},
		{"readwrite", "readwrite", []string{oauthmeta.ScopeReadWrite}},
		{"both", "read readwrite", []string{oauthmeta.ScopeRead, oauthmeta.ScopeReadWrite}},
		{"absent -> readwrite default", "", []string{oauthmeta.ScopeReadWrite}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, store, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, "https://client.example.com/cb")

			u := "/oauth/authorize?client_id=" + url.QueryEscape(clientID) +
				"&redirect_uri=" + url.QueryEscape("https://client.example.com/cb") +
				"&response_type=code&code_challenge=" + url.QueryEscape(pkcePair("v")) +
				"&code_challenge_method=S256&state=s"
			if tc.requested != "" {
				u += "&scope=" + url.QueryEscape(tc.requested)
			}
			req := httptest.NewRequest("GET", u, nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
			rec := httptest.NewRecorder()
			o.authorize(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("authorize(scope=%q) = %d body=%s", tc.requested, rec.Code, rec.Body.String())
			}
			code := mustQuery(t, rec.Header().Get("Location")).Get("code")
			if code == "" {
				t.Fatalf("no code in redirect: %q", rec.Header().Get("Location"))
			}

			// The CODE is what carries the grant forward to the token; assert on
			// the stored row, not on the response, so this catches a mint that
			// recorded the wrong set even if a later step papered over it.
			store.mu.Lock()
			ac, ok := store.codes[hashToken(code)]
			store.mu.Unlock()
			if !ok {
				t.Fatal("authorization code not found in the store by hash")
			}
			if strings.Join(ac.Scope, " ") != strings.Join(tc.want, " ") {
				t.Errorf("code scope = %v, want %v — the authorisation code must record the "+
					"RESOLVED grant, or the token minted from it cannot carry it", ac.Scope, tc.want)
			}
		})
	}
}

// TestAuthorizeRefusesUnadvertisedScope pins RFC 6749 §4.1.2.1's invalid_scope:
// a scope this server does not advertise is refused, never widened to
// readwrite and never narrowed to read.
func TestAuthorizeRefusesUnadvertisedScope(t *testing.T) {
	for _, requested := range []string{"admin", "readonly", "openid", "readreadwrite", "READ"} {
		t.Run(requested, func(t *testing.T) {
			o, store, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, "https://client.example.com/cb")

			u := "/oauth/authorize?client_id=" + url.QueryEscape(clientID) +
				"&redirect_uri=" + url.QueryEscape("https://client.example.com/cb") +
				"&response_type=code&code_challenge=" + url.QueryEscape(pkcePair("v")) +
				"&code_challenge_method=S256&state=s" +
				"&scope=" + url.QueryEscape(requested)
			req := httptest.NewRequest("GET", u, nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
			rec := httptest.NewRecorder()
			o.authorize(rec, req)

			// The redirect_uri is valid here, so the error must come back as a
			// redirect carrying invalid_scope (RFC 6749 §4.1.2.1) — not a bare
			// 400, and above all not a code.
			if rec.Code != http.StatusFound {
				t.Fatalf("authorize(scope=%q) = %d body=%s, want a redirect with invalid_scope",
					requested, rec.Code, rec.Body.String())
			}
			loc := mustQuery(t, rec.Header().Get("Location"))
			if loc.Get("error") != "invalid_scope" {
				t.Errorf("authorize(scope=%q) error = %q, want invalid_scope", requested, loc.Get("error"))
			}
			if loc.Get("code") != "" {
				t.Errorf("authorize(scope=%q) returned a CODE (%q) for an unadvertised scope — "+
					"the request was silently granted something it did not ask for",
					requested, loc.Get("code"))
			}
			// And nothing may be left in the store: the refusal happens before
			// the mint, not after.
			store.mu.Lock()
			n := len(store.codes)
			store.mu.Unlock()
			if n != 0 {
				t.Errorf("an unadvertised scope left %d authorization code(s) in the store — "+
					"the refusal ran after the mint", n)
			}
		})
	}
}

// TestWellKnownMetadataRoutesAreWired pins that /mcp actually mounts both PRM
// paths (a document helper that no route serves would still be a 404 in prod —
// the exact production symptom in issue #509).
func TestWellKnownMetadataRoutesAreWired(t *testing.T) {
	// The well-known routes are registered alongside the MCP endpoint (they
	// describe it), so the server under test must have MCP wired — a bare
	// health-only server registers none of them.
	srv := newMCPServer()
	for _, path := range []string{
		oauthmeta.ProtectedResourcePath,
		oauthmeta.ProtectedResourcePathSuffixed,
		oauthmeta.AuthorizationServerPath,
		oauthmeta.AuthorizationServerPath + "/mcp",
	} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

// ─── RFC 8707 resource parameter (issue #509 item 4) ────────────────────────

func TestAuthorizeValidatesResourceParameter(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")

	// Accepted: omitted, or exactly the MCP endpoint — the canonical one
	// (issuer base) or the one for the host the request arrived on. A native
	// client sends the resource it read out of the PRM document, and the PRM
	// `resource` is derived from the request host.
	for _, resource := range []string{
		"",
		oauthmeta.CanonicalResource(),
		oauthmeta.CanonicalResource() + "/",
		// The host the authorize request arrives on (httptest.NewRequest sets
		// example.com for a relative target): the PRM document for that host
		// advertises exactly that resource, so it must be accepted.
		"https://example.com/mcp",
	} {
		rec := authorizeWithResource(t, o, clientID, resource)
		if rec.Code != http.StatusFound {
			t.Errorf("authorize(resource=%q) = %d body=%s, want 302", resource, rec.Code, rec.Body.String())
		}
	}

	// Rejected: a resource this server does not serve. Must NOT redirect (the
	// redirect_uri itself is valid, but the request is refused outright).
	for _, resource := range []string{
		"https://evil.example/mcp",
		"https://mcp.jomform.com/other",
		"https://mcp.jomform.com",
	} {
		rec := authorizeWithResource(t, o, clientID, resource)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("authorize(resource=%q) = %d, want 400", resource, rec.Code)
			continue
		}
		var out map[string]string
		decodeJSONBody(t, rec, &out)
		if out["error"] != "invalid_target" {
			t.Errorf("authorize(resource=%q) error = %q, want invalid_target", resource, out["error"])
		}
	}
}

// authorizeWithResource runs authorize with an optional RFC 8707 resource.
func authorizeWithResource(t *testing.T, o *OAuthHandlers, clientID, resource string) *httptest.ResponseRecorder {
	t.Helper()
	const redirect = "https://client.example.com/cb"
	u := "/oauth/authorize?client_id=" + clientID +
		"&redirect_uri=" + "https%3A%2F%2Fclient.example.com%2Fcb" +
		"&response_type=code&code_challenge=" + pkcePair("v") +
		"&code_challenge_method=S256&state=s"
	if resource != "" {
		u += "&resource=" + resource
	}
	req := httptest.NewRequest("GET", u, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	return rec
}

func TestTokenValidatesResourceParameter(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", "v", "s")
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")

	// A resource naming another host must be refused before any token is minted.
	form := "grant_type=authorization_code&code=" + code +
		"&redirect_uri=https%3A%2F%2Fclient.example.com%2Fcb" +
		"&client_id=" + clientID + "&code_verifier=v" +
		"&resource=https%3A%2F%2Fevil.example%2Fmcp"
	trec := httptest.NewRecorder()
	treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec, treq)
	if trec.Code != http.StatusBadRequest {
		t.Fatalf("token(resource=evil) = %d, want 400", trec.Code)
	}
	var out map[string]string
	decodeJSONBody(t, trec, &out)
	if out["error"] != "invalid_target" {
		t.Errorf("error = %q, want invalid_target", out["error"])
	}
}
