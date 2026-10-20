package mcpsrv

// Card t_67db0213 — structural guard for the seam the fix introduced.
//
// get_cloudflare_status must publish `configured` from the REST rule
// (httpapi.cfOAuthConfigured, reached through Deps.CFOAuthConfigured). The
// tempting "fix" is to copy the env test into this package
// (`os.Getenv("JF_CF_OAUTH_CLIENT_ID") != ""`) — which answers today's question
// but recreates the original defect the moment the rule grows a second
// condition, because nothing then keeps the two copies in step. That is exactly
// how the two surfaces came to disagree in the first place.
//
// So: the key is read in exactly ONE place. This test fails if mcpsrv starts
// reading it. It scans the non-test sources of this package (the read would
// live in production code, not in a test) and looks for the READ — mentioning
// the key in a comment is fine, reading it is not.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPNeverReadsTheCloudflareClientIDEnvItself(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for _, needle := range []string{
			`Getenv("JF_CF_OAUTH_CLIENT_ID")`,
			`LookupEnv("JF_CF_OAUTH_CLIENT_ID")`,
		} {
			if strings.Contains(string(src), needle) {
				t.Errorf("%s reads the Cloudflare client id itself (%s). Wire Deps.CFOAuthConfigured from httpapi.CFOAuthConfigured instead — two copies of this rule is the defect t_67db0213 fixed.", name, needle)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no non-test sources — this guard is looking in the wrong place")
	}
}
