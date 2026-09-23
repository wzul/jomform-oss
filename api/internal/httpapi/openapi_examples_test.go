// The /docs/api examples must actually work. This is the test that says so.
//
// WHY THIS EXISTS
//   The card's rule is blunt: an example that has not been run is an example that
//   is wrong. Every `curl` line on /docs/api is rendered from a restAPIExample
//   (openapi_examples.go), and this test EXECUTES each one against the real mux
//   with a real Postgres row and a real API key — not a mock, not the handler
//   function directly, but RegisterRoutes through ServeHTTP, which is the path a
//   reader's curl takes.
//
// WHAT EACH EXAMPLE PROVES
//   * The status is the documented one. A docs page that promises 201 and gets
//     400 is worse than no docs page.
//   * A documented SUBSTRING of the body is present. A status-only assertion
//     would pass on an empty body or an error with the same code, so each
//     example names something it must contain (`"slug"`, `"items"`, ...).
//
// THE VACUOUS-PASS GUARD
//   Every assertion is on a response this test received, and the test first
//   proves it got a response at all. The anonymous example (no key) is included
//   on purpose: it is the one case where the API must NOT answer with data, so an
//   API that accepted an unauthenticated read fails here.
//
// LIMITS (stated, not implied)
//   The test runs the REQUEST the example describes, built from the same
//   structured fields the page renders into a curl command. It does not invoke a
//   shell, so a quoting bug in the rendered string would not be caught by
//   execution — that is covered instead by asserting the rendered string still
//   names the example's method and path (TestRenderedCurlMatchesTheRequest).
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// exampleServer wires the real route table over a real database, with an API key
// whose scopes permit writes (the examples include create/publish).
func exampleServer(t *testing.T) (*RESTHandlers, *pgxpool.Pool, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	// The examples create and publish forms, so the key needs readwrite.
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, hash,
		"docs-examples", []string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("store api key: %v", err)
	}
	return h, pool, raw, accountID
}

// TestDocsAPIExamplesActuallyWork runs every published example.
func TestDocsAPIExamplesActuallyWork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed docs-examples test in -short mode")
	}
	h, pool, raw, accountID := exampleServer(t)
	ctx := context.Background()

	// The fixture the examples that act on a form need: a real draft form and a
	// real product, so "publish" and "list sales" are not run against nothing.
	formID := exampleFixtureForm(t, ctx, pool, accountID)

	ran, skipped := 0, 0
	for _, e := range restAPIExamples {
		if e.NeedsFixture && formID == "" {
			skipped++
			t.Errorf("example %q needs a fixture and none was created — the fixture helper broke", e.ID)
			continue
		}
		path := strings.ReplaceAll(e.Path, phFormID, formID)
		body := strings.ReplaceAll(e.Body, phFormID, formID)

		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(e.Method, path, reader)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if e.NeedsKey {
			req.Header.Set("Authorization", "Bearer "+raw)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != e.ExpectStatus {
			t.Errorf("example %q (%s %s): status = %d, want %d.\nThe docs page promises %d. Body: %s",
				e.ID, e.Method, path, rec.Code, e.ExpectStatus, e.ExpectStatus,
				truncate(rec.Body.String(), 400))
			continue
		}
		if !strings.Contains(rec.Body.String(), e.ExpectContains) {
			t.Errorf("example %q (%s %s): body does not contain %q, which the page says it will.\nBody: %s",
				e.ID, e.Method, path, e.ExpectContains, truncate(rec.Body.String(), 400))
			continue
		}
		ran++
	}
	if ran < len(restAPIExamples) {
		t.Errorf("only %d of %d examples actually ran and asserted — the rest were skipped, so the "+
			"page's examples are not all proven", ran, len(restAPIExamples))
	}
	_ = skipped
}

// TestDocsAPIExampleSetIsNotThin keeps the guard honest: if the example list is
// gutted, the assertions above would still pass on almost nothing.
func TestDocsAPIExampleSetIsNotThin(t *testing.T) {
	if len(restAPIExamples) < 6 {
		t.Errorf("only %d examples — the page promises a start-to-finish walk-through", len(restAPIExamples))
	}
	var hasAuth, hasRead, hasWrite, hasAnon bool
	for _, e := range restAPIExamples {
		switch {
		case !e.NeedsKey:
			hasAnon = true
		case e.Method == "GET":
			hasRead = true
		case e.Method == "POST":
			hasWrite = true
		}
		if e.ID == "auth" {
			hasAuth = true
		}
	}
	if !hasAuth {
		t.Error("the authentication example (id \"auth\") is gone — the card requires the auth shape to be proven")
	}
	if !hasRead {
		t.Error("no authenticated READ example remains — the card requires at least the auth shape and one read")
	}
	if !hasWrite {
		t.Error("no WRITE example remains — the end-to-end flow needs a create/publish step")
	}
	if !hasAnon {
		t.Error("no unauthenticated example remains — nothing proves the API refuses an anonymous call")
	}
}

// TestRenderedCurlMatchesTheRequest asserts the copy-pasteable string a reader
// gets still names the request the test proved. Execution cannot catch a
// rendering bug (the test never shells out), so this is the check that the page
// is not showing a curl for a different endpoint or method.
func TestRenderedCurlMatchesTheRequest(t *testing.T) {
	for _, e := range restAPIExamples {
		curl := exampleCurl(e)
		if !strings.Contains(curl, "https://api.jomform.com"+e.Path) {
			t.Errorf("example %q: the rendered curl does not contain the example's path %s:\n%s",
				e.ID, e.Path, curl)
		}
		if e.Method != "GET" && !strings.Contains(curl, "-X "+e.Method) {
			t.Errorf("example %q: the rendered curl does not name the method %s:\n%s",
				e.ID, e.Method, curl)
		}
		if e.Method == "GET" && strings.Contains(curl, "-X ") {
			t.Errorf("example %q: a GET rendered with an explicit method, which would look "+
				"like a different request than the one tested:\n%s", e.ID, curl)
		}
		if e.NeedsKey && !strings.Contains(curl, "Authorization: Bearer") {
			t.Errorf("example %q: needs a key but the rendered curl sends no Authorization header:\n%s",
				e.ID, curl)
		}
		if !e.NeedsKey && strings.Contains(curl, "Authorization: Bearer") {
			t.Errorf("example %q: does NOT need a key (it demonstrates the refusal) but the rendered "+
				"curl sends one, which would make the example show a 200 instead of the 401 it "+
				"documents:\n%s", e.ID, curl)
		}
		// The key must never be a literal in the docs text.
		if strings.Contains(curl, "jfk_") {
			t.Errorf("example %q: the rendered curl contains what looks like a real API key — the "+
				"page must show a placeholder", e.ID)
		}
	}
}

// TestEveryExampleIsInThePublishedArtifact asserts the page's data source
// actually carries every example, so the rendered reference cannot silently drop
// one.
func TestEveryExampleIsInThePublishedArtifact(t *testing.T) {
	b, err := restAPIExampleJSON()
	if err != nil {
		t.Fatalf("marshal examples: %v", err)
	}
	var doc struct {
		Examples []struct {
			ID   string `json:"id"`
			Curl string `json:"curl"`
		} `json:"examples"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the generated examples artifact does not parse: %v", err)
	}
	if len(doc.Examples) != len(restAPIExamples) {
		t.Fatalf("the artifact carries %d examples, the registry has %d",
			len(doc.Examples), len(restAPIExamples))
	}
	for i, e := range doc.Examples {
		if e.ID != restAPIExamples[i].ID {
			t.Errorf("artifact example %d has id %q, want %q (order and content must match)",
				i, e.ID, restAPIExamples[i].ID)
		}
		if e.Curl == "" {
			t.Errorf("artifact example %q carries no rendered curl — the page would render an empty block", e.ID)
		}
	}
}

// truncate is declared in qr_stand_issue499_test.go (same package).

// exampleFixtureForm creates the form the publish/list examples act on.
func exampleFixtureForm(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	f := &dbstore.Form{
		AccountID: accountID,
		Slug:      "docs-example-" + accountID[:8],
		Title:     "Docs Example Form",
		Config:    map[string]any{"mode": "response", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusDraft,
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, f); err != nil {
		t.Fatalf("create example form: %v", err)
	}
	return f.ID
}
