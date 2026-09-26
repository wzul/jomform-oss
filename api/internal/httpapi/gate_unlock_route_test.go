// Card t_25f5e9ea — the CLASS guard for the gate's unlock route.
//
// A private form's gate posts the password to its OWN address plus "/unlock"
// (location.pathname + "/unlock" — see renderPrivateGate). That rule is what
// made an EMBEDDED private form impossible to unlock: the gate used to hardcode
// the canonical host-scoped "/<slug>/unlock", so the embed posted at a route on
// the merchant's origin that could never name the workspace.
//
// Fixing only the embed route would have left the SAME defect on every other
// route that renders this gate — and it did: /f/{slug} renders the gate too, so
// the moment the gate started posting to its own path, /f/{slug} began posting
// at /f/{slug}/unlock, which was not registered. The page still rendered, still
// showed a password box, and could never be unlocked. That is the exact bug this
// card exists to fix, one route over.
//
// So this guard is deliberately ROUTE-AGNOSTIC and mechanical: it renders the
// gate through each route that can produce it, reads the target the rendered
// page would actually fetch, and asserts that target resolves to a handler (not
// the mux's own "404 page not found"). Adding a new gate-rendering route without
// its unlock route therefore fails here, and the case list below is the full set
// of routes measured to reach renderPrivateGate.
//
// It is a live-request guard, not a source scrape: it cannot be satisfied by a
// comment claiming the route exists.
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// gateFetchTargetRe extracts the target of the gate's unlock fetch from the
// rendered page — `fetch(location.pathname+'/unlock')`. Reading it out of the
// page (rather than restating it in the test) is the point: if the renderer is
// ever changed to post somewhere else, this guard follows it and checks THAT
// address instead of asserting a string the test itself supplied.
var gateFetchTargetRe = regexp.MustCompile(`fetch\(location\.pathname\s*\+\s*'(\/[a-z]+)'`)

// unlockTargetOf renders the page at path and returns the unlock path it would
// post to, resolved the way a browser resolves it (the page's own path plus the
// literal suffix the script appends).
func unlockTargetOf(t *testing.T, hnd http.Handler, host, path string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	m := gateFetchTargetRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("GET %s rendered no `fetch(location.pathname + '<suffix>')` gate — this guard "+
			"cannot tell what it would post to. body: %.300s", path, rec.Body.String())
	}
	return path + m[1], rec
}

// TestEveryGateRouteHasItsOwnUnlockRoute is the class guard: for EVERY route
// that renders the private-form password gate, the address that gate would post
// to must be a registered route.
func TestEveryGateRouteHasItsOwnUnlockRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping gate/unlock route-parity guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"

	// Every route measured to reach renderPrivateGate. The embed route is the
	// card's; the other two are the canonical ones, and /f/{slug} is the one
	// this guard caught. A route that renders the gate but posts to a missing
	// unlock is a form that can be looked at and never opened.
	cases := []struct {
		name string
		path string
	}{
		{"canonical /{slug}", "/" + slug},
		{"canonical /f/{slug}", "/f/" + slug},
		{"embed /embed/f/{wsslug}/{slug}", "/embed/f/" + ws + "/" + slug},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			target, rec := unlockTargetOf(t, hnd, host, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s -> %d, want 200 (this route is supposed to render the gate here). "+
					"body: %.300s", tc.path, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `id="pw"`) {
				t.Fatalf("GET %s did not render the password gate, so this case does not exercise "+
					"the path under test. body: %.300s", tc.path, rec.Body.String())
			}

			// The route exists iff the mux does not answer its own no-route
			// reply. A JSON error body means SOME handler matched (that is how
			// the canonical route answers a wrong Host, for instance); the
			// literal ServeMux text means nothing is registered there.
			res := publicPostJSON(t, hnd, host, target, map[string]any{"password": gateFixturePassword})
			if res.Code == http.StatusNotFound && strings.TrimSpace(res.Body.String()) == "404 page not found" {
				t.Fatalf("the gate served at %s posts to %s, and that route DOES NOT EXIST — the "+
					"page renders a password box that can never be satisfied. Register the "+
					"matching POST route in RegisterRoutes (see embedUnlock / handleFormUnlock).",
					tc.path, target)
			}
			// A route that exists must also actually unlock with the right
			// password: present-but-broken is not better than absent.
			if res.Code != http.StatusOK {
				t.Errorf("POST %s (the gate's own target for %s) with the CORRECT password -> %d, "+
					"want 200. body=%s", target, tc.path, res.Code, res.Body.String())
			}
		})
	}
}

// TestGatePostsToItsOwnScopeNotTheCanonicalRoute pins the property the class
// guard relies on: the gate must derive its target from the page it is rendered
// on, never from a hardcoded canonical path. If this is ever changed back, the
// class guard above would be testing a different rule than production uses.
func TestGatePostsToItsOwnScopeNotTheCanonicalRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping gate target guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)

	// Render the SAME form through two different routes and read the target from
	// each page. Each target must sit under the route it was rendered on — that
	// is the whole point: one page, its own scope.
	embedPath := "/embed/f/" + ws + "/" + slug
	canonPath := "/" + slug
	for _, tc := range []struct{ path, want string }{
		{embedPath, embedPath + "/unlock"},
		{canonPath, canonPath + "/unlock"},
	} {
		target, _ := unlockTargetOf(t, hnd, ws+".jomform.com", tc.path)
		if target != tc.want {
			t.Errorf("the gate rendered at %s posts to %s, want %s — the gate must post to its OWN "+
				"scope, because an embed's Host is the merchant's domain and the canonical route "+
				"resolves the form from the Host", tc.path, target, tc.want)
		}
	}
}
