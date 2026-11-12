// OAuth discovery metadata for MCP clients:
//
//   - RFC 9728 Protected Resource Metadata (mandatory for MCP servers since
//     the 2025-06-18 revision of the MCP spec) — wellKnownProtectedResource.
//   - RFC 8414 Authorization Server Metadata — wellKnownOAuth.
//
// Both documents are built from internal/oauthmeta, and this file exposes the
// builder functions (rather than only handlers) so the drift-guard tests can
// compare the two documents directly instead of comparing hand-copied strings
// (issue #509).
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// randTokenHex generates n random bytes, hex-encoded.
func randTokenHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// oauthBase returns the public issuer, e.g. https://mcp.jomform.com
// (JF_OAUTH_BASE override). Delegates to internal/oauthmeta so the issuer
// cannot differ from the value the PRM document advertises.
func oauthBase() string { return oauthmeta.Base() }

// protectedResourceDocument builds the RFC 9728 document served at
// /.well-known/oauth-protected-resource (and its /mcp-suffixed form).
//
// Field notes:
//   - resource: the MCP endpoint itself, derived from the request host exactly
//     like the AS metadata's mcp_endpoint, so `resource` is the URL the client
//     called.
//   - authorization_servers: the OAuth issuer, i.e. literally the AS metadata
//     `issuer` value (same source), which is what RFC 9728 requires.
//   - scopes_supported: the scopes the flow actually issues.
//   - bearer_methods_supported: ["header"] — the only accepted presentation.
func protectedResourceDocument(host string) map[string]any {
	return map[string]any{
		"resource":                 oauthmeta.Resource(host),
		"authorization_servers":    []string{oauthmeta.Base()},
		"scopes_supported":         oauthmeta.ScopesSupported(),
		"bearer_methods_supported": oauthmeta.BearerMethodsSupported(),
		"resource_documentation":   oauthmeta.MCPDocsPage,
	}
}

// authorizationServerDocument builds the RFC 8414 metadata document.
//
// RFC 8628 §4 requires a device-grant server to advertise
// `device_authorization_endpoint` and to list the device grant type, so an MCP
// client (Claude Desktop, Cursor, any harness) can DISCOVER headless onboarding
// with no client-side change: it fetches this document, sees the endpoint, and
// falls back to the device flow when it has no browser.
//
// `grant_types_supported` is oauthmeta.GrantTypesSupported(), the same list the
// DCR response hands back, so a client cannot read a narrower grant from its own
// registration than this document advertises.
func authorizationServerDocument(host string) map[string]any {
	base := oauthmeta.Base()
	return map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"device_authorization_endpoint":         base + "/oauth/device_authorization",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 oauthmeta.GrantTypesSupported(),
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"},
		"scopes_supported":                      oauthmeta.ScopesSupported(),
		"service_documentation":                 oauthmeta.ServiceDocumentation,
		"mcp_endpoint":                          oauthmeta.Resource(host),
	}
}

// wellKnownProtectedResource serves OAuth 2.0 Protected Resource Metadata
// (RFC 9728) for the /mcp resource (issue #509). MCP clients are REQUIRED to
// discover the authorization server through this document, so a 404 here locks
// native clients out of the connector entirely (the reported production
// symptom).
func wellKnownProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, protectedResourceDocument(oauthmeta.HostOrDefault(r)))
}

// wellKnownOAuth serves RFC 8414 metadata (auto-discovery for MCP clients).
// The advertised endpoints — /oauth/authorize, /oauth/token, /oauth/revoke,
// /oauth/register — are all implemented in oauth.go (issue #121); discovery no
// longer advertises a broken contract.
func wellKnownOAuth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authorizationServerDocument(oauthmeta.HostOrDefault(r)))
}
