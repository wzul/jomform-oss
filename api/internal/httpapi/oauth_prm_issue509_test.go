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
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/config"
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
			if len(doc.ScopesSupported) != 1 || doc.ScopesSupported[0] != oauthmeta.ScopeReadWrite {
				t.Errorf("scopes_supported = %v, want [%s]", doc.ScopesSupported, oauthmeta.ScopeReadWrite)
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
// token endpoint returns is the scopes_supported both documents advertise.
func TestTokenResponseScopeMatchesDiscovery(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", "v", "s")
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")

	form := "grant_type=authorization_code&code=" + code +
		"&redirect_uri=" + "https%3A%2F%2Fclient.example.com%2Fcb" +
		"&client_id=" + clientID + "&code_verifier=v"
	trec := httptest.NewRecorder()
	treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec, treq)
	if trec.Code != http.StatusOK {
		t.Fatalf("token = %d body=%s", trec.Code, trec.Body.String())
	}
	var tok struct {
		Scope string `json:"scope"`
	}
	decodeJSONBody(t, trec, &tok)
	if tok.Scope != oauthmeta.ScopeReadWrite {
		t.Errorf("token scope = %q, want %q (must equal scopes_supported)", tok.Scope, oauthmeta.ScopeReadWrite)
	}
	asScopes := authorizationServerDocument("mcp.jomform.com")["scopes_supported"].([]string)
	if strings.Join(asScopes, " ") != tok.Scope {
		t.Errorf("AS scopes_supported %v does not contain the issued scope %q", asScopes, tok.Scope)
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
