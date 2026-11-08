package httpapi

// Card t_8fcb546d — the SHAPE half of the `config.blocks` class, at the REST
// write surface.
//
// A template created with a non-array `config.blocks` used to be ACCEPTED
// (201): `urlguard.CheckConfig` walks blocks with `v, ok := raw.([]any)` and a
// JSON object simply does not match, so the scheme rule reported the config as
// clean. The row was then stored, and the gallery-metadata predicate's
// `jsonb_array_elements` raised SQLSTATE 22023 on every publish that touched it
// — including a bulk publish of OTHER, well-formed forms in the same account
// (measured: 500 {"code":"internal"}, and the clean form stayed draft).
//
// These tests pin the refusal at the route: 400, a message that names the field
// and the wanted type, and no change to what was already accepted (a real array,
// an omitted key, and a non-template form all still work).
//
// Integration (needs the test database); skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"net/http"
	"strings"
	"testing"
)

func TestRESTTemplateNonArrayBlocksRejected(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)

	for _, tc := range []struct {
		name string
		body string
	}{
		{"object", `{"title":"Bad obj","config":{"mode":"response","category":"qa","blocks":{"oops":true}}}`},
		{"string", `{"title":"Bad str","config":{"mode":"response","category":"qa","blocks":"oops"}}`},
		{"number", `{"title":"Bad num","config":{"mode":"response","category":"qa","blocks":42}}`},
	} {
		rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/templates", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST template with a non-array blocks (%s) = %d, want 400 — the shape must be "+
				"refused at write time, not stored and then raised on publish (body %s)",
				tc.name, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "array") {
			t.Errorf("%s: 400 body does not say what the value should have been: %s", tc.name, body)
		}
		// The rejection must NOT be the scheme wording: the caller's object is of
		// the wrong JSON type, and "must start with https://" would be false.
		if strings.Contains(body, "https://") {
			t.Errorf("%s: 400 body states the https:// scheme rule for a shape violation: %s", tc.name, body)
		}
	}

	// A real array still creates…
	rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/templates",
		`{"title":"Good","config":{"mode":"response","category":"qa","blocks":[{"id":"b1","type":"text","value":"Hi"}]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST template with an array blocks = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	path := "/v1/rest/templates/" + issue501JSON(t, rec)["id"].(string)

	// …and so does an OMITTED blocks key ("keep existing").
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/templates",
		`{"title":"No blocks","config":{"mode":"response"}}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST template without a blocks key = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// The UPDATE route refuses the same shape, and still accepts the array.
	if rec := issue501Do(t, h, raw, http.MethodPut, path,
		`{"config":{"blocks":{"oops":true}}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT template with a non-array blocks = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := issue501Do(t, h, raw, http.MethodPut, path,
		`{"config":{"mode":"response","category":"qa","blocks":[{"id":"b1","type":"text","value":"Hi"}]}}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT template with an array blocks = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRESTFormNonArrayBlocksRejected(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)

	// A plain form's config reaches the same predicate as a template's (a form
	// can be turned into a template), so the create route refuses it too.
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms",
		`{"title":"Bad form","config":{"blocks":{"oops":true}}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST form with a non-array blocks = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// An ordinary form with a real array is unaffected.
	rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms",
		`{"title":"Good form","config":{"blocks":[{"id":"b1","type":"text","value":"Hi"}]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST form with an array blocks = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	path := "/v1/rest/forms/" + issue501JSON(t, rec)["id"].(string)
	if rec := issue501Do(t, h, raw, http.MethodPatch, path,
		`{"config":{"blocks":"oops"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH form with a non-array blocks = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
