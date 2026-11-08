package mcpsrv

// Card t_48a93084: the MCP workspace tools must print the SAME address the REST
// payloads print, and the branch must be the deployment flag — an agent that
// hands a merchant `https://<slug>.jomform.com` on a self-hosted install hands
// them a host that does not exist there.
//
// This is the MCP half of the rule; internal/installurl holds the rule itself
// and httpapi/workspace_url_t_48a93084_test.go holds the REST half.
import "testing"

func TestMCPWorkspaceURLFollowsTheFlag(t *testing.T) {
	cases := []struct {
		name       string
		selfHosted bool
		baseURL    string
		want       string
	}{
		{"hosted", false, "https://jomform.com", "https://acme.jomform.com"},
		// flag OFF with a non-jomform origin: the HOSTED shape still wins. This is
		// the issue #77 trap — the hosted product's own APP_BASE_URL is its apex,
		// so "the origin is not a jomform.com host" is not evidence of a
		// self-hosted install.
		{"hosted behind an install-looking origin", false, "http://localhost:18099", "https://acme.jomform.com"},
		{"self-hosted", true, "http://localhost:18099", "http://localhost:18099/"},
		{"self-hosted with a trailing slash", true, "http://localhost:18099/", "http://localhost:18099/"},
		{"self-hosted with no usable origin", true, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{deps: &Deps{SelfHosted: c.selfHosted, AppBaseURL: c.baseURL}}
			if got := s.workspaceURL("acme"); got != c.want {
				t.Fatalf("MCP workspace url = %q, want %q", got, c.want)
			}
		})
	}
}

// A Server built without deps (bare unit tests) must not panic on the address
// path; it answers the hosted shape with no origin.
func TestMCPWorkspaceURLWithoutDeps(t *testing.T) {
	s := &Server{}
	if got := s.workspaceURL("acme"); got != "https://acme.jomform.com" {
		t.Fatalf("MCP workspace url without deps = %q, want the hosted shape", got)
	}
}

// The custom-domain hint is the same question asked about a DIFFERENT address:
// where the merchant points their domain. The measured defect (card t_48a93084)
// was the add_domain response telling a self-hosted merchant to CNAME to
// custom.jomform.com — a host that routes to the hosted product, not to their
// install — while the dashboard copy beside it named the install itself.
func TestMCPCNAMEHostFollowsTheFlag(t *testing.T) {
	cases := []struct {
		name       string
		selfHosted bool
		baseURL    string
		want       string
	}{
		{"hosted", false, "https://jomform.com", "custom.jomform.com"},
		{"hosted behind an install origin", false, "http://localhost:18099", "custom.jomform.com"},
		{"self-hosted", true, "http://localhost:18099", "localhost:18099"},
		{"self-hosted with a subpath install", true, "https://forms.example.com:8443/app/", "forms.example.com:8443"},
		// An install with no usable origin must not print an empty arrow.
		{"self-hosted with no usable origin", true, "", "your installation's origin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{deps: &Deps{SelfHosted: c.selfHosted, AppBaseURL: c.baseURL}}
			if got := s.cnameHost(); got != c.want {
				t.Fatalf("MCP cname host = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMCPCNAMEHostWithoutDeps(t *testing.T) {
	s := &Server{}
	if got := s.cnameHost(); got != "custom.jomform.com" {
		t.Fatalf("MCP cname host without deps = %q, want the hosted target", got)
	}
}
