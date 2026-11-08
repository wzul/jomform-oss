package httpapi

// Card t_48a93084: on a SELF-HOSTED install the workspace url in the API payloads
// and the deployment facts the dashboard reads must name the install's own
// origin — and the HOSTED product must be unchanged.
//
// What was measured before this: `POST /v1/workspaces` on a real self-hosted
// binary (JOMFORM_SELF_HOSTED=1, APP_BASE_URL=http://localhost:18099) answered
// `"url":"https://probe-ws-1793605997.jomform.com"` — a host that does not
// resolve on that install. The dashboard printed it, the picker's hint promised
// it, and a merchant's first screen held a dead address.
//
// The branch is the FLAG, never the host. The hosted product's APP_BASE_URL is
// its own apex, so a rule that compared hosts would take the self-hosted branch
// on jomform.com and break issue #77 (card t_4ddab217). The second case below is
// that trap: flag OFF with an origin that is NOT a jomform.com host still
// answers the hosted shape.
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestWorkspaceCreatedPayloadFollowsTheFlag(t *testing.T) {
	cases := []struct {
		name       string
		selfHosted bool
		baseURL    string
		want       string
	}{
		{"hosted", false, "https://jomform.com", "https://acme.jomform.com"},
		{"hosted behind an install-looking origin (issue #77 trap)", false, "http://localhost:18099", "https://acme.jomform.com"},
		{"self-hosted", true, "http://localhost:18099", "http://localhost:18099/"},
		{"self-hosted with a trailing slash on APP_BASE_URL", true, "http://localhost:18099/", "http://localhost:18099/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &RESTHandlers{BaseURL: c.baseURL, SelfHosted: c.selfHosted}
			body := h.workspaceCreatedBody(&dbstore.Workspace{ID: "w1", Name: "Acme", Slug: "acme"})

			// The payload as the wire carries it — not just the helper's return.
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			var got map[string]string
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if got["url"] != c.want {
				t.Fatalf("POST /v1/workspaces url = %q, want %q (payload %s)", got["url"], c.want, raw)
			}
			if got["slug"] != "acme" || got["name"] != "Acme" || got["id"] != "w1" {
				t.Fatalf("payload lost id/name/slug: %s", raw)
			}
		})
	}
}

func TestWorkspaceRenamedPayloadFollowsTheFlag(t *testing.T) {
	res := &dbstore.WorkspaceRenameResult{
		Workspace:   &dbstore.Workspace{ID: "w1", Name: "Acme", Slug: "acme-new"},
		OldName:     "Acme",
		OldSlug:     "acme-old",
		SlugChanged: true,
		NameChanged: true,
	}

	// HOSTED: the address moved, and the old one keeps redirecting.
	hosted := (&RESTHandlers{BaseURL: "https://jomform.com"}).workspaceRenamedBody(res)
	if hosted["url"] != "https://acme-new.jomform.com" {
		t.Errorf("hosted rename url = %v, want https://acme-new.jomform.com", hosted["url"])
	}
	if hosted["previous_url"] != "https://acme-old.jomform.com" || hosted["previous_url_redirects"] != true {
		t.Errorf("hosted rename lost previous_url/redirects: %v", hosted)
	}

	// SELF-HOSTED: one origin serves every workspace, so the address did NOT
	// move. Printing a previous_url that equals url, plus a "redirects" flag,
	// would tell the merchant and their agent something false.
	self := (&RESTHandlers{BaseURL: "http://localhost:18099", SelfHosted: true}).workspaceRenamedBody(res)
	if self["url"] != "http://localhost:18099/" {
		t.Errorf("self-hosted rename url = %v, want the install origin", self["url"])
	}
	if _, ok := self["previous_url"]; ok {
		t.Errorf("self-hosted rename answered previous_url=%v although the address did not move", self["previous_url"])
	}
	if _, ok := self["previous_url_redirects"]; ok {
		t.Errorf("self-hosted rename claimed previous_url_redirects although the address did not move")
	}
}

// The dashboard reads this route to choose its copy; if it 404s the web falls
// back to the hosted wording, which is the defect this card closes.
func TestMetaRoutePublishesTheDeployment(t *testing.T) {
	cases := []struct {
		selfHosted bool
		baseURL    string
		wantOrigin string
	}{
		{true, "http://localhost:18099/", "http://localhost:18099"},
		{false, "https://jomform.com", "https://jomform.com"},
		{true, "", ""},
	}
	for _, c := range cases {
		h := &RESTHandlers{BaseURL: c.baseURL, SelfHosted: c.selfHosted}
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/meta", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /v1/meta = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var body struct {
			SelfHosted bool   `json:"self_hosted"`
			Origin     string `json:"origin"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET /v1/meta: bad json: %v (%s)", err, rec.Body.String())
		}
		if body.SelfHosted != c.selfHosted {
			t.Errorf("GET /v1/meta self_hosted = %v, want %v", body.SelfHosted, c.selfHosted)
		}
		if body.Origin != c.wantOrigin {
			t.Errorf("GET /v1/meta origin = %q, want %q", body.Origin, c.wantOrigin)
		}
	}
}

// The REST custom-domain hint and the settings copy it mirrors must not drift:
// the hosted CNAME target has exactly ONE home (internal/installurl) and every
// payload builds it through there. A literal `custom.jomform.com` in a handler
// is the shape that shipped the hosted target to self-hosted installs.
func TestHostedCNAMEStringHasOneHome(t *testing.T) {
	const literal = "custom.jomform.com"
	dirs := []string{".", "../mcpsrv", "../installurl"}
	checked := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// The package that OWNS the value is allowed to spell it.
			if dir == "../installurl" && name == "installurl.go" {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", filepath.Join(dir, name), err)
			}
			checked++
			if strings.Contains(stripLineComments(string(src)), literal) {
				t.Errorf("%s still hardcodes %q — build it with installurl.CNAMEHost(origin, selfHosted) so a "+
					"self-hosted install is told to point the domain at ITSELF", filepath.Join(dir, name), literal)
			}
		}
	}
	if checked == 0 {
		t.Fatal("scanned no files — the guard proved nothing")
	}
}

// stripLineComments drops `//` comments so the guard judges what a merchant is
// SHOWN, not what a comment explains. Block comments are untouched: a hosted
// target inside one has to be rewritten by hand, which is the point.
func stripLineComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "//") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
