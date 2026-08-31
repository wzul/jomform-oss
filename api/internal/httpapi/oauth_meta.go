// OAuth 2.1 authorization server metadata + DCR for MCP clients.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
func wellKnownOAuth(w http.ResponseWriter, r *http.Request) {
	base := oauthBase()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"},
		"service_documentation":                 "https://jomform.com/docs",
		"mcp_endpoint":                          "https://" + r.Host + "/mcp",
	})
}

// oauthRegister implements RFC 7591 Dynamic Client Registration (open).
func oauthRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.RedirectURIs) == 0 {
		writeErr(w, http.StatusBadRequest, "redirect_uris wajib")
		return
	}
	// MVP: stateless dynamic client (redirect URIs echoed + signed id).
	// Production hardening: store in DB w/ rotation.
	clientID := "jfc_" + randTokenHex(16)
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_name":                in.ClientName,
		"redirect_uris":              in.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}
