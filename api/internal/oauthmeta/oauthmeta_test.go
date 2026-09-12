package oauthmeta

// Unit tests for the shared OAuth metadata source (issue #509): the derivations
// the RFC 9728 document, the RFC 8414 document, the /mcp 401 challenge and the
// RFC 8707 `resource` check all read from here.

import (
	"net/http"
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
		"https://mcp.jomform.com",              // the origin is not the resource
		"https://mcp.jomform.com/other",        // wrong path
		"https://evil.example/mcp",             // wrong host — token must not be minted
		"https://mcp.jomform.com.evil.test/mcp", // host suffix trick
		"http://mcp.jomform.com/mcp",           // wrong scheme
		"mcp.jomform.com/mcp",                  // not absolute
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
func TestScopesAndBearerMethods(t *testing.T) {
	if got := ScopesSupported(); len(got) != 1 || got[0] != ScopeReadWrite {
		t.Errorf("ScopesSupported() = %v, want [%s]", got, ScopeReadWrite)
	}
	if got := BearerMethodsSupported(); len(got) != 1 || got[0] != "header" {
		t.Errorf("BearerMethodsSupported() = %v, want [header]", got)
	}
}
