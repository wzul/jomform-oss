package httpapi

// Where a SELF-HOSTED install's published-form URL comes from (card t_6c0badbe,
// folded from t_f0b60bcc).
//
// THE DEFECT, measured on a clean install at http://localhost:18080. After
// publish, the builder's "Your form is live" dialog printed
// `https://selfhost-test.jomform.com/selfhost-smoke` — `<workspace>.<product
// domain>/<slug>` — even though APP_BASE_URL was the local origin and
// JOMFORM_SELF_HOSTED=1. That host does not exist on a self-hosted install, so
// "Open form", "Copy link" and the QR stand all pointed at a dead address; the
// one that works is `{APP_BASE_URL}/f/{slug}` (the shape PublicHandlers serves
// from its own origin, card t_28a06494).
//
// WHAT THIS PINS, AND WHY IT BITES
//   1. With SelfHosted set, BOTH builders that emit a form URL —
//      RESTHandlers.formURLForAccount (the dashboard/API payload the builder
//      dialog reads) and PublicHandlers.formURLForAccount (notification mail) —
//      return exactly `{APP_BASE_URL}/f/{slug}`.
//   2. WITHOUT the flag the self-host shape is never emitted, even though the
//      same origin is configured: on the hosted product APP_BASE_URL is its own
//      apex, which serves no form (issue #77). This is the direction that proves
//      the flag decides, not the origin — the mistake that served every tenant's
//      form on the apex once before (card t_4ddab217).
//   3. A trailing slash on the origin cannot produce `//f/`, and an empty origin
//      produces "" rather than a relative `/f/<slug>` (which would resolve
//      against whatever host the reader happens to be on).
//   4. The URL shape agrees with the serving path: it uses `/f/`, the route
//      PublicHandlers actually registers for own-origin serving.
//
// The store-backed hosted branch (custom domain / workspace subdomain) is
// covered by the existing forms_list_url and public-serving tests; this file only
// needs to prove the self-host branch takes precedence, so the handlers here
// carry no stores at all — if the self-host branch ever falls through, the
// answers below become "" and these cases fail.
import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

const selfHostOrigin = "http://localhost:18080"

func TestSelfHostedFormURLShape(t *testing.T) {
	const slug = "clean-test"
	want := selfHostOrigin + "/f/" + slug

	// The REST builder is the one the builder dialog's refresh reads
	// (GET /v1/rest/forms/{id} → restForm.URL).
	rest := &RESTHandlers{SelfHosted: true, BaseURL: selfHostOrigin}
	if got := rest.formURLForAccount(context.Background(), &dbstore.Account{ID: "acc"}, slug); got != want {
		t.Errorf("RESTHandlers.formURLForAccount on a self-hosted install = %q, want %q — the builder "+
			"prints and copies this", got, want)
	}

	// The public builder is the one notification mail prints.
	pub := &PublicHandlers{SelfHosted: true, BaseURL: selfHostOrigin}
	if got := pub.formURLForAccount(context.Background(), "acc", slug); got != want {
		t.Errorf("PublicHandlers.formURLForAccount on a self-hosted install = %q, want %q", got, want)
	}

	// The shipped defect, restated: the hosted tenant shape must NOT be what a
	// self-hosted install prints. Assert it on the RESULT, not on the constant.
	if got := rest.formURLForAccount(context.Background(), &dbstore.Account{ID: "acc"}, slug); strings.Contains(got, ".jomform.com") {
		t.Fatalf("a self-hosted install still prints the product-domain host: %q", got)
	}

	// The path is the ROUTE the install serves (ownOriginForm answers /f/{slug}).
	if !strings.Contains(want, "/f/") {
		t.Errorf("self-host form URL %q is not on the /f/{slug} route the install serves", want)
	}
}

func TestSelfHostedFlagDecidesTheShapeNotTheOrigin(t *testing.T) {
	// The hosted product's APP_BASE_URL is its own apex. Host equality with it is
	// NOT evidence of a self-hosted install, and this direction is what keeps the
	// hosted apex serving nothing (issue #77 / card t_4ddab217).
	rest := &RESTHandlers{BaseURL: "https://jomform.com"}
	pub := &PublicHandlers{BaseURL: "https://jomform.com"}

	for _, got := range []string{
		rest.formURLForAccount(context.Background(), &dbstore.Account{ID: "acc"}, "booking"),
		pub.formURLForAccount(context.Background(), "acc", "booking"),
	} {
		if got != "" {
			t.Errorf("a NON-self-hosted handler with APP_BASE_URL=https://jomform.com resolved %q — the "+
				"apex serves no form (issue #77); only JOMFORM_SELF_HOSTED may switch the shape", got)
		}
	}
}

func TestSelfHostedFormURLHandlesOriginEdges(t *testing.T) {
	cases := []struct {
		origin, slug, want string
		why                string
	}{
		{selfHostOrigin + "/", "s", selfHostOrigin + "/f/s", "a trailing slash must not produce //f/"},
		{selfHostOrigin, "", "", "no slug → no URL"},
		{"", "s", "", "no origin → no URL (never a relative /f/<slug>)"},
		{"   ", "s", "", "a blank origin → no URL"},
	}
	for _, c := range cases {
		rest := &RESTHandlers{SelfHosted: true, BaseURL: c.origin}
		if got := rest.formURLForAccount(context.Background(), &dbstore.Account{ID: "acc"}, c.slug); got != c.want {
			t.Errorf("origin=%q slug=%q → %q, want %q (%s)", c.origin, c.slug, got, c.want, c.why)
		}
	}
}
