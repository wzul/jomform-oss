package httpapi

// Issue #248: the custom-domain verify-failure response must guide the
// merchant to the correct DNS setup (CNAME to custom.jomform.com, or
// Cloudflare proxy) instead of dumping the domain's resolved IPs. Those IPs
// are Cloudflare edge IPs and dumping them misleadingly implies an A Record,
// which is wrong.
//
// Issue #261: the backend no longer returns a hardcoded Malay message. It
// returns a STABLE error code ("verify_failed") that the frontend maps to the
// merchant's selected UI language via i18n. Pure unit test — no Postgres/Valkey
// needed.

import (
	"testing"
)

func TestVerifyFailDetailIsStableCode(t *testing.T) {
	// The backend must return a stable error code, not a hardcoded BM string,
	// so the frontend can map it to the selected UI language (issue #261).
	if verifyFailDetail != "verify_failed" {
		t.Errorf("verifyFailDetail should be the stable code %q, got: %q", "verify_failed", verifyFailDetail)
	}
	// It must NOT be a hardcoded Malay message.
	if verifyFailDetail == "Domain tidak menghala ke JomForm. Set CNAME dari domain anda ke custom.jomform.com (atau gunakan Cloudflare proxy), kemudian tunggu DNS propagate dan Verify semula." {
		t.Errorf("verifyFailDetail must not be a hardcoded Malay message")
	}
}
