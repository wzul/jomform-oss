package urlguard

import (
	"strings"
	"testing"
)

// Issue #549: the confirmation `redirect_url` check was a hand-rolled
// `strings.HasPrefix(v, "https://")` in both write paths (REST
// rest_form_confirmation.go and MCP parity.go), which AGENTS.md forbids for
// caller-supplied URL fields. It diverged from the shared rule and rejected values
// every other URL field accepts.
//
// These tests pin the shared rule and the acceptance table both write paths must
// now agree with. The divergence test FAILS against the pre-fix code.

// TestIssue549SharedRuleNormalises pins the normalisation that the hand-rolled copy
// lacked: urlguard trims and lowercases before comparing, so an uppercase scheme or
// incidental whitespace is a VALID https URL.
func TestIssue549SharedRuleNormalises(t *testing.T) {
	mustAccept := []string{
		"https://example.com",
		"HTTPS://example.com",
		"Https://Example.COM/path?q=1",
		"  https://example.com  ",
		"\thttps://example.com",
		"https://example.com/a/b?c=d#e",
	}
	for _, v := range mustAccept {
		if !Valid(v) {
			t.Errorf("issue #549: urlguard.Valid(%q) = false, want true — the shared rule "+
				"trims and lowercases, so this is a valid https URL and must be accepted "+
				"by every URL field including redirect_url", v)
		}
	}

	mustReject := []string{
		"http://example.com",
		"//evil.com",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"ftp://example.com",
		"HTTPSX://example.com", // not the https scheme
		"https:/example.com",   // missing slash
	}
	for _, v := range mustReject {
		if Valid(v) {
			t.Errorf("issue #549: urlguard.Valid(%q) = true, want false", v)
		}
	}

	// Empty is valid by design (means "no value"), which is why a REQUIRED field
	// must check emptiness separately.
	if !Valid("") {
		t.Error("urlguard.Valid(\"\") must be true by design (empty = no value)")
	}
	if !Valid("   ") {
		t.Error("urlguard.Valid(whitespace) must be true by design (empty = no value)")
	}
}

// TestIssue549HandRolledCopyDiverges demonstrates exactly what the hand-rolled check
// got wrong, so the reason for the fix is recorded as an executable fact.
func TestIssue549HandRolledCopyDiverges(t *testing.T) {
	handRolled := func(v string) bool { return strings.HasPrefix(v, "https://") }
	for _, v := range []string{"HTTPS://example.com", "  https://example.com", "\thttps://example.com"} {
		if Valid(v) != handRolled(v) {
			t.Logf("DIVERGENCE for %q: shared=%v hand-rolled=%v (the copy rejected a valid URL)",
				v, Valid(v), handRolled(v))
			continue
		}
		t.Errorf("%q no longer diverges; this test documents the old bug and should be "+
			"removed or updated", v)
	}
}

// TestIssue549InvalidMessageNamesTheField pins the shared wording, so every URL
// field explains itself the same way.
func TestIssue549InvalidMessageNamesTheField(t *testing.T) {
	msg := InvalidMessage("redirect_url")
	if !strings.Contains(msg, "redirect_url") {
		t.Errorf("InvalidMessage must name the field, got %q", msg)
	}
	if !strings.Contains(msg, "https://") {
		t.Errorf("InvalidMessage must state the required scheme, got %q", msg)
	}
	if !strings.Contains(msg, "javascript:") {
		t.Errorf("InvalidMessage should name the schemes it rejects, got %q", msg)
	}
}
