package installurl

// Pins BOTH branches of the workspace address, and pins the branch to the FLAG.
//
// The flag is the whole point: on the hosted product APP_BASE_URL is the
// product's own apex, so a rule that inferred "self-hosted" from the configured
// origin (or from the request host) would take the self-host branch on
// jomform.com itself — the mistake that broke issue #77 (card t_4ddab217).
// `TestWorkspaceHostedIgnoresAJomformOrigin` is that trap, written down.
import "testing"

func TestWorkspaceHosted(t *testing.T) {
	got := Workspace("https://jomform.com", false, "acme")
	if want := "https://acme.jomform.com"; got != want {
		t.Fatalf("hosted workspace url = %q, want %q", got, want)
	}
}

func TestWorkspaceHostedIgnoresTheConfiguredOrigin(t *testing.T) {
	// Both rows are the same FLAG value with a different origin. If the branch
	// ever starts reading the origin, one of these two answers changes.
	cases := []struct{ origin, want string }{
		{"https://jomform.com", "https://acme.jomform.com"},      // the product apex
		{"http://localhost:18099", "https://acme.jomform.com"},   // an install origin, flag OFF
		{"https://acme.jomform.com", "https://acme.jomform.com"}, // host equality, flag OFF
	}
	for _, c := range cases {
		if got := Workspace(c.origin, false, "acme"); got != c.want {
			t.Errorf("Workspace(%q, false, acme) = %q, want %q — the HOSTED branch must not read the origin",
				c.origin, got, c.want)
		}
	}
}

func TestWorkspaceSelfHosted(t *testing.T) {
	cases := []struct{ origin, want string }{
		{"http://localhost:18099", "http://localhost:18099/"},
		{"http://localhost:18099/", "http://localhost:18099/"},
		{"  https://forms.example.com//  ", "https://forms.example.com/"},
	}
	for _, c := range cases {
		if got := Workspace(c.origin, true, "acme"); got != c.want {
			t.Errorf("Workspace(%q, true, acme) = %q, want %q", c.origin, got, c.want)
		}
	}
}

// The self-hosted branch is TERMINAL: an unusable APP_BASE_URL yields "" rather
// than falling back to the hosted shape the branch exists to stop printing.
func TestWorkspaceSelfHostedWithNoUsableOriginIsEmpty(t *testing.T) {
	for _, origin := range []string{"", "   ", "/", "localhost:18099", "http://", "//forms.example.com"} {
		if got := Workspace(origin, true, "acme"); got != "" {
			t.Errorf("Workspace(%q, true, acme) = %q, want \"\" — self-hosted must not fall back to the hosted shape",
				origin, got)
		}
	}
}

func TestWorkspaceEmptySlug(t *testing.T) {
	if got := Workspace("https://jomform.com", false, ""); got != "" {
		t.Errorf("hosted with no slug = %q, want \"\"", got)
	}
	// A self-hosted address does not depend on the slug at all — the install has
	// ONE origin, and that is the defect this card fixes.
	if got := Workspace("http://localhost:18099", true, ""); got != "http://localhost:18099/" {
		t.Errorf("self-hosted with no slug = %q, want the install origin", got)
	}
}

// CNAMEHost is the custom-domain half of the same question: the settings copy,
// the REST add-domain hint and the MCP add_domain hint all tell the merchant
// where to point their domain. On a self-hosted install that is THIS install's
// host — pointing a domain at custom.jomform.com sends it to a server the
// merchant does not run.
func TestCNAMEHost(t *testing.T) {
	cases := []struct {
		name       string
		origin     string
		selfHosted bool
		want       string
	}{
		{"hosted", "https://jomform.com", false, "custom.jomform.com"},
		// Flag OFF with an install-looking origin must stay on the hosted target
		// — the #77 trap, in the custom-domain copy.
		{"hosted behind an install origin", "http://localhost:18099", false, "custom.jomform.com"},
		{"self-hosted", "http://localhost:18099", true, "localhost:18099"},
		{"self-hosted with a port and path", "https://forms.example.com:8443/app/", true, "forms.example.com:8443"},
		{"self-hosted with no usable origin", "", true, ""},
		{"self-hosted, schemeless", "forms.example.com", true, ""},
	}
	for _, c := range cases {
		if got := CNAMEHost(c.origin, c.selfHosted); got != c.want {
			t.Errorf("%s: CNAMEHost(%q, %v) = %q, want %q", c.name, c.origin, c.selfHosted, got, c.want)
		}
	}
}
