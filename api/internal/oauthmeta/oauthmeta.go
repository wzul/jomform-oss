// Package oauthmeta is the single source of truth for the OAuth discovery
// documents and the authentication challenge JomForm serves to MCP clients.
//
// The MCP specification (2025-06-18, Authorization) makes OAuth 2.0 Protected
// Resource Metadata (RFC 9728) mandatory for servers and the mandatory
// authorization-server discovery mechanism for clients. That means one host
// now publishes TWO documents that describe the same deployment:
//
//	/.well-known/oauth-protected-resource      (RFC 9728) — what resource is
//	   protected, and which authorization server issues tokens for it;
//	/.well-known/oauth-authorization-server    (RFC 8414) — the authorization
//	   server's endpoints.
//
// They overlap by design: the PRM `authorization_servers` entry is the AS
// metadata `issuer`, and the PRM `resource` is the AS metadata `mcp_endpoint`.
// Both packages therefore derive those values from THIS package instead of
// repeating literals, so the two documents cannot drift apart (issue #509).
package oauthmeta

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// BaseEnv overrides the public OAuth issuer base.
const BaseEnv = "JF_OAUTH_BASE"

// DefaultBase is the public MCP host, which is also the OAuth issuer: the
// OAuth endpoints (/oauth/authorize, /oauth/token, …) are served on the same
// listener as the /mcp endpoint.
const DefaultBase = "https://mcp.jomform.com"

// MCPPath is the path of the protected MCP endpoint (the RFC 9728 `resource`).
const MCPPath = "/mcp"

// ProtectedResourcePath is the RFC 9728 default well-known location for the
// protected resource metadata document. Because the resource lives at a path
// (/mcp), the RFC also allows the path-inserted form
// `/.well-known/oauth-protected-resource/mcp`; JomForm serves both so a client
// that tries either discovery shape is answered.
const ProtectedResourcePath = "/.well-known/oauth-protected-resource"

// ProtectedResourcePathSuffixed is the path-inserted form for the /mcp resource.
const ProtectedResourcePathSuffixed = ProtectedResourcePath + MCPPath

// AuthorizationServerPath is the RFC 8414 well-known location for the
// authorization server metadata (plus its own path-suffixed form).
const AuthorizationServerPath = "/.well-known/oauth-authorization-server"

// ServiceDocumentation is the AS metadata `service_documentation` value and
// the PRM `resource_documentation` value (one constant, one home).
const ServiceDocumentation = "https://jomform.com/docs"

// MCPDocsPage documents the MCP connector and its OAuth discovery.
const MCPDocsPage = "https://jomform.com/docs/mcp"

// TokenType is the only bearer token type issued (RFC 6750).
const TokenType = "Bearer"

// OAuth 2.1 scopes. Two are advertised and issued: read and readwrite.
//
// Wan (2026-09-21): "kena ada read sahaja dan readwrite la untuk mcp" — a
// merchant who only wants their agent to LOOK at the store must be able to
// grant a credential that cannot change anything.
//
// These are the OAuth scope NAMES and they are deliberately distinct from the
// API-key names (dbstore.ScopeReadonly / ScopeReadWrite): the API-key scope
// vocabulary predates this and is not renamed here, because renaming a live
// credential axis is a migration, not a scope addition.
//
// ONE SOURCE OF TRUTH: the AS metadata `scopes_supported`, the RFC 9728 PRM
// `scopes_supported` and the set a grant can draw from are all derived from
// ScopesSupported() below. A scope the flow can issue but discovery does not
// advertise (or the reverse) is the divergence issue #509 exists to prevent.
const (
	// ScopeRead permits read-only MCP calls. A token holding it is REFUSED on
	// every write tool (mcpsrv.requireWriteScope / the accountScopes write
	// boolean both test for ScopeReadWrite, and read does not satisfy it).
	ScopeRead = "read"
	// ScopeReadWrite permits read + write.
	ScopeReadWrite = "readwrite"
)

// ScopesSupported returns the OAuth scopes this deployment advertises AND can
// issue. Both discovery documents and the grant resolver below read this one
// function, so they cannot list different sets.
func ScopesSupported() []string { return []string{ScopeRead, ScopeReadWrite} }

// DefaultScopes is the grant for an authorize request that carries no `scope`
// parameter (RFC 6749 §3.3 permits a server-chosen default).
//
// It is readwrite ON PURPOSE. Every MCP client connecting today — the Hermes
// gateway included — omits `scope`, and defaulting to read would silently strip
// write capability from all of them. A client that wants read asks for read.
func DefaultScopes() []string { return []string{ScopeReadWrite} }

// DeviceGrantType is the RFC 8628 §3.4 grant type for the device
// authorization exchange. It is a full URN (the RFC's spelling), which is what
// the AS metadata must advertise and what a client sends as `grant_type`.
const DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// DeviceDefaultScopes is the grant for a DEVICE authorisation that omits
// `scope`. It is read, deliberately, and different from DefaultScopes() above.
//
// WHY THEY DIFFER. A browser authorize request arrives with a human sitting in
// front of the consent screen; the historical default there is readwrite
// because every client connecting at the time omitted `scope` and read would
// have silently stripped write capability from them. A device request arrives
// from a HARNESS with nobody watching, and the consent screen is an email that
// a person reads — possibly on a phone, possibly at speed. The scope ladder for
// that path starts at read: a write-capable agent credential has to be asked
// for explicitly and has to be named in the email in plain words.
func DeviceDefaultScopes() []string { return []string{ScopeRead} }

// DeviceScopes resolves a device authorisation's requested `scope`.
//
// Same membership rule as GrantedScopes (exact, case-sensitive, no prefix
// matching, unadvertised value → ok=false), but the absent case resolves to
// read rather than readwrite. One resolver, two defaults — the difference is
// the whole scope ladder.
func DeviceScopes(requested string) ([]string, bool) {
	return grantedScopesFrom(requested, DeviceDefaultScopes())
}

// GrantedScopes resolves a requested `scope` parameter (RFC 6749 §3.3: a
// space-delimited, case-sensitive list) into the exact set to grant.
//
//   - absent or empty/whitespace-only -> DefaultScopes() (readwrite), ok=true.
//   - every value advertised          -> exactly those, deduped and in the
//     canonical ScopesSupported() order, ok=true. Asking for "readreadwrite"
//     style junk, or a value that merely LOOKS like a scope, is not accepted
//     by prefix matching — membership is exact.
//   - any value not advertised        -> nil, ok=false. The caller must answer
//     invalid_scope (RFC 6749 §4.1.2.1): never silently widen the grant to
//     readwrite and never silently narrow it to read, because either would
//     hand the client a credential it did not ask for.
func GrantedScopes(requested string) ([]string, bool) {
	return grantedScopesFrom(requested, DefaultScopes())
}

// grantedScopesFrom is the ONE resolver both entry points share.
func grantedScopesFrom(requested string, def []string) ([]string, bool) {
	want := strings.Fields(requested)
	if len(want) == 0 {
		return def, true
	}
	requestedSet := make(map[string]bool, len(want))
	for _, w := range want {
		requestedSet[w] = true
	}
	advertised := ScopesSupported()
	if len(requestedSet) > len(advertised) {
		return nil, false // more distinct values than exist: at least one is not advertised
	}
	granted := make([]string, 0, len(requestedSet))
	for _, s := range advertised {
		if requestedSet[s] {
			granted = append(granted, s)
			delete(requestedSet, s)
		}
	}
	if len(requestedSet) != 0 {
		return nil, false // unadvertised value(s) remain
	}
	return granted, true
}

// BearerMethodsSupported returns how a bearer token is presented (RFC 9728
// §2). JomForm only accepts the value in the Authorization header — never a
// query parameter or a form body.
func BearerMethodsSupported() []string { return []string{"header"} }

// Base returns the public OAuth issuer / MCP origin, e.g.
// https://mcp.jomform.com, with any trailing slash removed so path joining is
// unambiguous.
func Base() string {
	v := strings.TrimRight(strings.TrimSpace(os.Getenv(BaseEnv)), "/")
	if v != "" {
		return v
	}
	return DefaultBase
}

// HostOrDefault returns the request's Host header, falling back to the host of
// Base() when a request carries none (hand-built requests, tests). Deriving
// the resource URL from the request host is what keeps `resource` equal to the
// URL the client actually called.
func HostOrDefault(r *http.Request) string {
	if r != nil {
		if h := strings.TrimSpace(r.Host); h != "" {
			return h
		}
	}
	return hostOf(Base())
}

func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Host
}

// Resource returns the canonical protected resource identifier for a request
// host: https://<host>/mcp.
func Resource(host string) string {
	if host == "" {
		return ""
	}
	return "https://" + host + MCPPath
}

// CanonicalResource is the resource identifier derived from the configured
// issuer base (https://<issuer-host>/mcp). It is the value a client is expected
// to send as the RFC 8707 `resource` parameter.
func CanonicalResource() string { return Resource(hostOf(Base())) }

// MetadataURL returns the RFC 9728 protected-resource metadata URL a client
// should fetch for this host. This is exactly what the /mcp 401 challenge
// advertises, and it is the path the PRM handler serves.
func MetadataURL(host string) string {
	if host == "" {
		return ""
	}
	return "https://" + host + ProtectedResourcePath
}

// Challenge builds the RFC 6750 / RFC 9728 §5.1 WWW-Authenticate value for the
// /mcp 401: a Bearer challenge carrying the `resource_metadata` parameter, so
// a client can discover the protected resource metadata without guessing the
// URL.
//
// Per RFC 6750 §3.1 the `error` code is only included when the request
// actually carried credentials that failed: a request with no Authorization
// header gets the bare challenge, a request with a rejected token gets
// error="invalid_token".
func Challenge(host string, invalidToken bool) string {
	c := TokenType + ` resource_metadata="` + MetadataURL(host) + `"`
	if invalidToken {
		c += `, error="invalid_token"`
	}
	return c
}

// ResourceMatches reports whether a client-supplied RFC 8707 `resource`
// parameter designates the MCP endpoint this deployment protects.
//
// A single-resource authorization server has exactly one target, so the check
// is an equality test against the resource derived from the request host and
// the resource derived from the configured issuer base (both produced by
// Resource() above, so they cannot disagree with what /mcp serves). It is
// tolerant only of the harmless differences a client may introduce —
// scheme/host case and a trailing slash — and rejects anything else, including
// a different host or path. This is what prevents an access token minted for
// this MCP endpoint from being used to satisfy a request for some other
// resource.
func ResourceMatches(candidate, requestHost string) bool {
	c := normalizeResource(candidate)
	if c == "" {
		return false
	}
	for _, want := range []string{CanonicalResource(), Resource(HostOrDefault(nil)), Resource(requestHost)} {
		if w := normalizeResource(want); w != "" && w == c {
			return true
		}
	}
	return false
}

// normalizeResource lower-cases the scheme/host and drops a trailing slash so
// equivalent spellings compare equal. It returns "" for input that is not an
// absolute URL (that is never a valid `resource` value).
func normalizeResource(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.EscapedPath(), "/")
}
