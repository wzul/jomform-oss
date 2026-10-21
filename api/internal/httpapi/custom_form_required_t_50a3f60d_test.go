package httpapi

// Card t_50a3f60d: a custom-HTML response form's registered field contract —
// config.custom_html.fields, each with `required` and a `type` — is enforced
// server-side at POST /<slug>/order, not only by the buyer's browser.
//
// The gap: handleResponseSubmit validated against formfields.Contract, which
// reads config.registered_fields and then the form's builder blocks. A
// custom-HTML form has neither, so the derived contract was empty and
// ValidateFields returned nil unconditionally: a direct POST that omitted a
// `required:true` registered field, sent a non-email into a `type:"email"`
// field, or sent `fields:{}` was answered 201 and STORED. The browser refused
// the same three (form.checkValidity() via requestSubmit()), so the contract
// was real but only client-side — exactly the shape issue #254 exists to close.
//
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// customFormSubmissions counts the submission rows recorded for a form, so the
// assertion is about what the SERVER WROTE, not only about the status code —
// the defect stored a row while answering 201, and the fix must store nothing
// when it answers 400.
func customFormSubmissions(t *testing.T, formSlug string) int {
	t.Helper()
	pool := bundleTestPool(t)
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM submissions s JOIN forms f ON f.id = s.form_id
		 WHERE f.slug = $1`, formSlug).Scan(&n); err != nil {
		t.Fatalf("count submissions for %s: %v", formSlug, err)
	}
	return n
}

func TestCustomFormRequiredContractEnforcedOnDirectPost(t *testing.T) {
	h, workspaceSlug, formSlug := customFormFixture(t)
	host := workspaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// The fixture registers name (required, text) and email (required, email)
	// and feedback (optional). client is sent empty so this test exercises the
	// field contract, not the anti-spam email/MX layer.
	before := customFormSubmissions(t, formSlug)

	rejected := []struct {
		name      string
		body      string
		wantLabel string
	}{
		{
			// The exact bypass measured on prod: the required field is simply
			// absent from the payload.
			name:      "omits a required registered field",
			body:      `{"client":{},"fields":{"email":"ali@example.test"}}`,
			wantLabel: "Name",
		},
		{
			// The empty submission measured on prod.
			name:      "empty fields map",
			body:      `{"client":{},"fields":{}}`,
			wantLabel: "Name",
		},
		{
			name:      "required field submitted blank",
			body:      `{"client":{},"fields":{"name":"   ","email":"ali@example.test"}}`,
			wantLabel: "Name",
		},
		{
			// Present but not an email: the browser's type=email check, which a
			// direct POST also skipped.
			name:      "non-email into a type=email field",
			body:      `{"client":{},"fields":{"name":"Ali","email":"not-an-email"}}`,
			wantLabel: "Email",
		},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.wantLabel) {
				t.Errorf("400 body does not name the field %q: %s", tc.wantLabel, body)
			}
		})
	}

	// Every rejection above answered 400 — and none of them may have stored a
	// row. This is the assertion that fails when the contract is not enforced.
	if got := customFormSubmissions(t, formSlug); got != before {
		t.Fatalf("rejected submits stored rows: %d before, %d after", before, got)
	}

	// The same payload with the required fields present still succeeds, so the
	// guard rejects the bypass and not the form (feedback is optional and
	// omitted here on purpose).
	rec := post(`{"client":{},"fields":{"name":"Ali","email":"ali@example.test"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := customFormSubmissions(t, formSlug); got != before+1 {
		t.Fatalf("valid submit stored %d rows, want 1 new (%d -> %d)", got-before, before, got)
	}
}
