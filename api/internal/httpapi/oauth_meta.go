// OAuth 2.1 authorization server metadata for MCP clients (RFC 8414).
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
)

// randTokenHex generates n random bytes, hex-encoded.
func randTokenHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// oauthBase returns the public issuer, e.g. https://mcp.jomform.com
func oauthBase() string {
	if v := os.Getenv("JF_OAUTH_BASE"); v != "" {
		return v
	}
	return "https://mcp.jomform.com"
}

// wellKnownOAuth serves RFC 8414 metadata (auto-discovery for MCP clients).
// The advertised endpoints — /oauth/authorize, /oauth/token, /oauth/revoke,
// /oauth/register — are all implemented in oauth.go (issue #121); discovery no
// longer advertises a broken contract.
func wellKnownOAuth(w http.ResponseWriter, r *http.Request) {
	base := oauthBase()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"},
		"service_documentation":                 "https://jomform.com/docs",
		"mcp_endpoint":                          "https://" + r.Host + "/mcp",
	})
}
