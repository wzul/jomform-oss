package httpapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// restAPIExamples are the requests the /docs/api page shows, in one place.
//
// WHY THEY ARE DATA AND NOT PROSE
//
//	A curl example in a docs page is a claim, and nothing in this repo would
//	notice when it stopped being true — the page is static and no test ran it.
//	So each example is a structured request that api_examples_test.go EXECUTES
//	against a live server with a real database and a real API key, and asserts
//	the documented status and a documented substring of the body. The rendered
//	`curl` string is generated from those same fields, and the test asserts the
//	rendering still names the same method and path — so the string a reader
//	copy-pastes cannot drift from the request that was actually proven.
//
//	The page imports the generated copy (web/src/lib/apiExamples.gen.ts), and
//	TestGeneratedArtifactsAreInSync fails if that copy is stale.
type restAPIExample struct {
	// ID is a stable key, used for anchors on the docs page.
	ID string
	// Title is the reader-facing name ("First call: is this key alive?").
	Title string
	// Why says what the example demonstrates, in one sentence.
	Why string
	// Method and Path are the request. Path uses {} placeholders; the test
	// substitutes the fixture's real ids.
	Method string
	Path   string
	// Body is the JSON request body, "" when the request has none. Values may
	// use the {{...}} placeholders below, substituted at run time.
	Body string
	// ExpectStatus is the status the API returns, asserted by the test.
	ExpectStatus int
	// ExpectContains is a substring the response body must contain. It is what
	// makes the example a demonstration rather than a status-code check.
	ExpectContains string
	// NeedsKey is false for the anonymous example that demonstrates the 401.
	NeedsKey bool
	// NeedsFixture marks examples that need a form/product created by the test
	// first; the test skips them rather than running them against nothing.
	NeedsFixture bool
	// Note is the one line the page prints under the example.
	Note string
}

// Placeholders the test substitutes into Path/Body. They are deliberately
// explicit so an example cannot silently reference a fixture it never gets.
const (
	phFormID = "{{form_id}}"
)

// restAPIExampleList is the ordered set the page renders and the test runs.
//
// The first two examples are the ones the card requires to be proven: the
// authentication shape (what a bad credential gets) and a real read.
var restAPIExamples = []restAPIExample{
	{
		ID:    "auth",
		Title: "1. Authenticate",
		Why: "Every /v1/rest route takes the key in an Authorization header. There is no " +
			"query-string key and no cookie path for a script.",
		Method:         "GET",
		Path:           "/v1/rest/keys",
		ExpectStatus:   200,
		ExpectContains: "\"scopes\"",
		NeedsKey:       true,
		Note: "This lists your own keys — it never returns a secret. The raw key is shown " +
			"once, when you create it.",
	},
	{
		ID:    "no-key",
		Title: "2. What a missing credential gets",
		Why: "An unauthenticated call is refused with 401 before any account is resolved, so " +
			"a misconfigured client fails loudly instead of reading someone else's data.",
		Method:         "GET",
		Path:           "/v1/rest/keys",
		ExpectStatus:   401,
		ExpectContains: "\"error\"",
		NeedsKey:       false,
		Note:           "The body is {\"error\":\"authentication required\"} — the API's own wording, not a fixed error catalogue.",
	},
	{
		ID:             "create-form",
		Title:          "3. Create a form",
		Why:            "A new form is created as a draft. Nothing is live until you publish it.",
		Method:         "POST",
		Path:           "/v1/rest/forms",
		Body:           `{"title":"Docs example form"}`,
		ExpectStatus:   201,
		ExpectContains: "\"slug\"",
		NeedsKey:       true,
		Note: "Leave slug out and it is generated from the title and made unique in the " +
			"workspace. The response carries mode (payment by default) and the draft's url.",
	},
	{
		ID:             "publish-form",
		Title:          "4. Publish it",
		Why:            "Publishing is what makes the form live at its URL.",
		Method:         "POST",
		Path:           "/v1/rest/forms/" + phFormID + "/publish",
		ExpectStatus:   200,
		ExpectContains: "\"status\"",
		NeedsKey:       true,
		NeedsFixture:   true,
		Note: "A payment form with no products is refused here with 409 and " +
			"requires_ack: true, because it could never take money; send " +
			"{\"acknowledge\":true} to publish it anyway.",
	},
	{
		ID:    "read-forms",
		Title: "5. Read your forms back",
		Why: "The list carries everything needed to pick a form id: slug, title, " +
			"status, mode and public URL.",
		Method:         "GET",
		Path:           "/v1/rest/forms",
		ExpectStatus:   200,
		ExpectContains: "\"mode\"",
		NeedsKey:       true,
		NeedsFixture:   true,
		Note:           "mode is payment or response, so a response form is never mistaken for an order form.",
	},
	{
		ID:             "list-sales",
		Title:          "6. Read the sales this form produced",
		Why:            "The last step of the end-to-end flow: what the form actually recorded.",
		Method:         "GET",
		Path:           "/v1/rest/sales?page=1&limit=15",
		ExpectStatus:   200,
		ExpectContains: "\"items\"",
		NeedsKey:       true,
		NeedsFixture:   true,
		Note: "Without page this route answers with a plain array instead — the same rows, " +
			"the older shape the dashboard reads.",
	},
}

// exampleCurl renders the copy-pasteable command for one example, derived from
// its structured fields so the displayed text cannot name a different request.
//
// The key is shown as $JOMFORM_API_KEY rather than a literal: a docs page that
// prints a real-shaped key trains people to paste secrets into shells and issue
// threads, and the test substitutes the real key when it runs this same request.
func exampleCurl(e restAPIExample) string {
	var b strings.Builder
	b.WriteString("curl -sS ")
	if e.Method != "GET" {
		b.WriteString("-X " + e.Method + " ")
	}
	// Quote the URL: a query string contains & which a shell would background.
	b.WriteString("'https://api.jomform.com" + e.Path + "'")
	if e.NeedsKey {
		b.WriteString(" \\\n  -H \"Authorization: Bearer $JOMFORM_API_KEY\"")
	}
	if e.Body != "" {
		b.WriteString(" \\\n  -H \"Content-Type: application/json\" \\\n  -d '" + e.Body + "'")
	}
	return b.String()
}

// restAPIExampleDocs returns the examples in page order, each with its rendered
// curl attached. Deterministic by construction (the list is ordered).
func restAPIExampleDocs() []map[string]any {
	out := make([]map[string]any, 0, len(restAPIExamples))
	for _, e := range restAPIExamples {
		out = append(out, map[string]any{
			"id": e.ID, "title": e.Title, "why": e.Why, "note": e.Note,
			"method": e.Method, "path": e.Path, "curl": exampleCurl(e),
			"expectStatus": e.ExpectStatus, "expectContains": e.ExpectContains,
			"needsKey": e.NeedsKey, "needsFixture": e.NeedsFixture,
		})
	}
	return out
}

// restAPIExampleJSON is the artifact form: the same data, so a consumer that is
// not the docs page can read the examples without importing TypeScript.
func restAPIExampleJSON() ([]byte, error) {
	b, err := json.MarshalIndent(map[string]any{
		"examples": restAPIExampleDocs(),
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// exampleByID finds one example, for the test's error messages.
func exampleByID(id string) (restAPIExample, bool) {
	for _, e := range restAPIExamples {
		if e.ID == id {
			return e, true
		}
	}
	return restAPIExample{}, false
}

// exampleIDs returns the example ids, sorted, for the test's coverage assertion.
func exampleIDs() []string {
	out := make([]string, 0, len(restAPIExamples))
	for _, e := range restAPIExamples {
		out = append(out, e.ID)
	}
	sort.Strings(out)
	return out
}

func init() {
	// Cheap self-checks so a malformed example fails at start-up of the binary
	// build rather than only inside the test run: a duplicate id would make a
	// docs anchor ambiguous, and an empty expectation would make an example
	// assert nothing.
	seen := map[string]bool{}
	for _, e := range restAPIExamples {
		if e.ID == "" || e.Title == "" || e.Path == "" {
			panic(fmt.Sprintf("openapi: example %+v is missing an id, title or path", e))
		}
		if seen[e.ID] {
			panic("openapi: duplicate example id " + e.ID)
		}
		seen[e.ID] = true
		if e.ExpectStatus == 0 || e.ExpectContains == "" {
			panic("openapi: example " + e.ID + " asserts nothing (it would pass vacuously)")
		}
		if !strings.HasPrefix(e.Path, "/v1/rest/") {
			panic("openapi: example " + e.ID + " is outside /v1/rest")
		}
	}
}
