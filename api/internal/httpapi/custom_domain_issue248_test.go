package httpapi

// Issue #248: the custom-domain verify-failure message must guide the
// merchant to the correct DNS setup (CNAME to custom.jomform.com, or
// Cloudflare proxy) instead of dumping the domain's resolved IPs. Those IPs
// are Cloudflare edge IPs and dumping them misleadingly implies an A Record,
// which is wrong. Pure unit test — no Postgres/Valkey needed.

import (
	"strings"
	"testing"
)

func TestVerifyFailDetailGuidesToCNAME(t *testing.T) {
	// The message must tell the merchant to set a CNAME to custom.jomform.com
	// (or use Cloudflare proxy) — the correct setup.
	for _, want := range []string{"CNAME", "custom.jomform.com", "Cloudflare"} {
		if !strings.Contains(verifyFailDetail, want) {
			t.Errorf("verifyFailDetail should mention %q, got: %q", want, verifyFailDetail)
		}
	}
}

func TestVerifyFailDetailDoesNotDumpRawIPs(t *testing.T) {
	// The message must NOT dump raw IPs (Cloudflare edge IPs like 104.21.x,
	// 172.67.x, 2606:4700:...) — that misleadingly implies an A Record.
	for _, bad := range []string{"104.21", "172.67", "2606:4700"} {
		if strings.Contains(verifyFailDetail, bad) {
			t.Errorf("verifyFailDetail should NOT contain %q (raw Cloudflare edge IP), got: %q", bad, verifyFailDetail)
		}
	}
}
