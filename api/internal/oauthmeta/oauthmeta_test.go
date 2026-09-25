package oauthmeta

// Unit tests for the shared OAuth metadata source (issue #509): the derivations
// the RFC 9728 document, the RFC 8414 document, the /mcp 401 challenge and the
// RFC 8707 `resource` check all read from here.

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestBaseDefaultsAndEnvOverride(t *testing.T) {
	t.Setenv(BaseEnv, "")
	if got := Base(); got != DefaultBase {
		t.Fatalf("Base() = %q, want %q", got, DefaultBase)
	}
	// A trailing slash must not survive: path joining is string concatenation.
	t.Setenv(BaseEnv, "https://oauth.example.test/")
	if got := Base(); got != "https://oauth.example.test" {
		t.Fatalf("Base() with trailing slash = %q", got)
	}
}

func TestResourceAndMetadataURLDeriveFromHost(t *testing.T) {
	if got := Resource("mcp.jomform.com"); got != "https://mcp.jomform.com/mcp" {
		t.Errorf("Resource = %q", got)
	}
	if got := Resource(""); got != "" {
		t.Errorf("Resource(\"\") = %q, want empty", got)
	}
	if got := MetadataURL("mcp.jomform.com"); got != "https://mcp.jomform.com/.well-known/oauth-protected-resource" {
		t.Errorf("MetadataURL = %q", got)
	}
	if got := MetadataURL(""); got != "" {
		t.Errorf("MetadataURL(\"\") = %q, want empty", got)
	}
}

func TestHostOrDefaultFallsBackToBaseHost(t *testing.T) {
	t.Setenv(BaseEnv, "")
	// No request → the configured issuer host.
	if got := HostOrDefault(nil); got != "mcp.jomform.com" {
		t.Errorf("HostOrDefault(nil) = %q", got)
	}
	// Request without a Host (rare, but a hand-built request has one empty).
	if got := HostOrDefault(&http.Request{}); got != "mcp.jomform.com" {
		t.Errorf("HostOrDefault(empty) = %q", got)
	}
	// Request host wins, so `resource` is the URL the client actually called.
	r, _ := http.NewRequest("POST", "https://mcp.staging.example/mcp", nil)
	if got := HostOrDefault(r); got != "mcp.staging.example" {
		t.Errorf("HostOrDefault(req) = %q", got)
	}
}

// TestChallengeShape pins the RFC 9728 §5.1 / RFC 6750 §3.1 challenge: the
// resource_metadata URL is always present, and error="invalid_token" only when
// a credential was actually presented and rejected.
func TestChallengeShape(t *testing.T) {
	got := Challenge("mcp.jomform.com", false)
	want := `Bearer resource_metadata="https://mcp.jomform.com/.well-known/oauth-protected-resource"`
	if got != want {
		t.Errorf("Challenge(no credentials) = %q, want %q", got, want)
	}
	got = Challenge("mcp.jomform.com", true)
	want += `, error="invalid_token"`
	if got != want {
		t.Errorf("Challenge(bad credentials) = %q, want %q", got, want)
	}
}

// TestResourceMatches pins the RFC 8707 resource check: exactly the MCP
// endpoint is accepted (modulo case and a trailing slash), everything else —
// including a different host or path and a non-absolute value — is rejected.
func TestResourceMatches(t *testing.T) {
	t.Setenv(BaseEnv, "")

	accepted := []string{
		"https://mcp.jomform.com/mcp",
		"https://mcp.jomform.com/mcp/",
		"HTTPS://MCP.JOMFORM.COM/mcp",
		"  https://mcp.jomform.com/mcp  ",
	}
	for _, c := range accepted {
		if !ResourceMatches(c, "mcp.jomform.com") {
			t.Errorf("ResourceMatches(%q) = false, want true", c)
		}
	}

	rejected := []string{
		"",
		"https://mcp.jomform.com",               // the origin is not the resource
		"https://mcp.jomform.com/other",         // wrong path
		"https://evil.example/mcp",              // wrong host — token must not be minted
		"https://mcp.jomform.com.evil.test/mcp", // host suffix trick
		"http://mcp.jomform.com/mcp",            // wrong scheme
		"mcp.jomform.com/mcp",                   // not absolute
		"not a url",
	}
	for _, c := range rejected {
		if ResourceMatches(c, "mcp.jomform.com") {
			t.Errorf("ResourceMatches(%q) = true, want false", c)
		}
	}

	// The request host counts too: a deployment served on another host accepts
	// its own resource identifier.
	if !ResourceMatches("https://mcp.staging.example/mcp", "mcp.staging.example") {
		t.Error("request-host resource should match")
	}
}

// TestScopesAndBearerMethods pins the advertised values that the token
// endpoint's responses must agree with.
//
// Two scopes since the read-scope work: `read` and `readwrite`, in that order.
// The order is part of the contract — ScopesSupported() is the canonical order
// oauthmeta.GrantedScopes emits a grant in, and the token response is asserted
// against it — so a reshuffle here is a behaviour change, not cosmetics.
func TestScopesAndBearerMethods(t *testing.T) {
	got := ScopesSupported()
	want := []string{ScopeRead, ScopeReadWrite}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("ScopesSupported() = %v, want %v", got, want)
	}
	if got := BearerMethodsSupported(); len(got) != 1 || got[0] != "header" {
		t.Errorf("BearerMethodsSupported() = %v, want [header]", got)
	}
}

// TestGrantedScopes is the resolution rule the authorize endpoint relies on:
// absent -> readwrite (the default that keeps every existing client writing),
// an advertised subset -> exactly that, anything unadvertised -> refused.
func TestGrantedScopes(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		want      []string
		wantOK    bool
	}{
		{"absent", "", []string{ScopeReadWrite}, true},
		{"whitespace only", "   	 ", []string{ScopeReadWrite}, true},
		{"read", "read", []string{ScopeRead}, true},
		{"readwrite", "readwrite", []string{ScopeReadWrite}, true},
		{"both, canonical order", "readwrite read", []string{ScopeRead, ScopeReadWrite}, true},
		{"both, requested order is normalised", "read readwrite", []string{ScopeRead, ScopeReadWrite}, true},
		{"duplicates collapse", "read read read", []string{ScopeRead}, true},
		{"extra whitespace", "  read   readwrite  ", []string{ScopeRead, ScopeReadWrite}, true},
		// Unadvertised values. Note each of these is refused rather than
		// rounded down to something we do support.
		{"unadvertised admin", "admin", nil, false},
		{"unadvertised readonly (the API-key spelling)", "readonly", nil, false},
		{"case is significant", "READ", nil, false},
		{"prefix is not a match", "readreadwrite", nil, false},
		{"one good one bad", "read admin", nil, false},
		{"not a scope at all", "openid email", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := GrantedScopes(tc.requested)
			if ok != tc.wantOK {
				t.Fatalf("GrantedScopes(%q) ok = %v, want %v", tc.requested, ok, tc.wantOK)
			}
			if !tc.wantOK {
				if got != nil {
					t.Errorf("GrantedScopes(%q) = %v on refusal, want nil (never a narrowed grant)", tc.requested, got)
				}
				return
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("GrantedScopes(%q) = %v, want %v", tc.requested, got, tc.want)
			}
			// Whatever is granted must be advertised — the invariant the
			// divergence guard depends on.
			for _, s := range got {
				if !slices.Contains(ScopesSupported(), s) {
					t.Errorf("granted scope %q is not in ScopesSupported() %v", s, ScopesSupported())
				}
			}
		})
	}
}

// TestDefaultScopesIsReadWriteTheFullGrant guards the one decision that would
// silently break every existing client: the absent-scope default must stay
// readwrite. Defaulting to `read` would strip write access from every MCP
// client that connects without asking (the Hermes gateway included).
func TestDefaultScopesIsReadWriteTheFullGrant(t *testing.T) {
	if got := DefaultScopes(); len(got) != 1 || got[0] != ScopeReadWrite {
		t.Fatalf("DefaultScopes() = %v, want [%s] — defaulting to read would downgrade every "+
			"existing client that sends no scope parameter", got, ScopeReadWrite)
	}
	// And the default must be exactly what an empty request resolves to.
	got, ok := GrantedScopes("")
	if !ok || strings.Join(got, " ") != strings.Join(DefaultScopes(), " ") {
		t.Errorf("GrantedScopes(\"\") = %v ok=%v, want DefaultScopes() %v", got, ok, DefaultScopes())
	}
}
