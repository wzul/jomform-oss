package httpapi

// Anti-drift guard for the form-slug RESERVED list (card t_f76f81eb).
//
// A form's slug is served at GET /{slug} on the SAME host and mux as JomForm's
// own public routes: a workspace subdomain and a verified custom domain both
// resolve to this API (deploy/vps/traefik-jomform.yml — jf-wildcard and
// jf-custom-root point at jf-api). Go's ServeMux prefers the more specific
// pattern, so every LITERAL single-segment GET route wins over /{slug}: a form
// minted with that label would be stored, reported live, and never reachable at
// the address the merchant was shown.
//
// That is why urlguard.reservedFormSlugPaths exists, and why this test exists:
// a route added later must not reopen the hole silently. It parses every mux
// registration in this package and requires each slug-shaped single-segment GET
// literal to be refused by the SAME predicate the write paths use.
//
// Routes that can never collide with a slug are skipped, deliberately and
// visibly: multi-segment patterns (/f/{slug}, /assets/{key...}), wildcards
// (/{slug}, /{$}), and literals a slug may not contain (/sitemap.xml,
// /favicon.ico carry a dot, which the slug rule refuses as a shape error).

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/urlguard"
)

// formSlugShaped matches a value the slug rule would accept on shape alone.
var formSlugShaped = regexp.MustCompile(`^[a-z0-9-]+$`)

// muxGetLiteral matches a route registration and captures the method and path.
var muxGetLiteral = regexp.MustCompile(`mux\.(?:HandleFunc|Handle)\("([A-Z]+) (/([^"]*))"`)

// singleSegmentGetRoutes returns the slug-shaped single-segment GET literals
// registered anywhere in this package's non-test sources.
func singleSegmentGetRoutes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var out []string
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		files++
		for _, m := range muxGetLiteral.FindAllStringSubmatch(string(b), -1) {
			if m[1] != "GET" {
				continue
			}
			path := m[2] // includes the leading slash
			label := strings.TrimPrefix(path, "/")
			// Multi-segment (or a wildcard segment) cannot be a slug, which has
			// no slash; a wildcard pattern is the catch-all itself.
			if strings.Contains(label, "/") || strings.Contains(label, "{") {
				continue
			}
			if !formSlugShaped.MatchString(label) {
				continue // a dot/bang in the literal is refused by the shape rule
			}
			out = append(out, label)
		}
	}
	if files < 5 {
		t.Fatalf("only %d non-test source files scanned — the walk is not covering the package", files)
	}
	sort.Strings(out)
	return out
}

func TestEverySingleSegmentGETRouteIsARejectedFormSlug(t *testing.T) {
	routes := singleSegmentGetRoutes(t)

	// Vacuity floor: the scan must see the routes that motivated the list.
	// Without this, a broken regex would make the whole guard pass silently.
	for _, must := range []string{"privacy", "terms", "refund", "healthz"} {
		found := false
		for _, r := range routes {
			if r == must {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("the scan did not find the GET /%s literal — the route parser is not reading "+
				"this package's registrations, so it proves nothing (found %v)", must, routes)
		}
	}

	var offenders []string
	for _, r := range routes {
		if !urlguard.ReservedFormSlug(r) {
			offenders = append(offenders, r)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("these single-segment GET routes are served by this package but are NOT refused as a "+
			"form slug: %v.\nA form minted with that label wins no request — the literal route answers "+
			"first — so the merchant is shown a public URL that can never serve their form. Add each to "+
			"urlguard.reservedFormSlugPaths with the reason it is served.", offenders)
	}

	// And the predicate must not be a blanket denial: these are merchant slugs
	// that must keep working (the same over-reach control the workspace list has).
	for _, ok := range []string{"order", "menu", "kedai-kopi", "hubungi-kami", "daftar"} {
		if urlguard.ReservedFormSlug(ok) {
			t.Errorf("urlguard.ReservedFormSlug(%q) = true — the reserved list is over-reaching and "+
				"refusing a legitimate merchant slug", ok)
		}
	}
}
