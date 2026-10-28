package httpapi

// Card t_2717ba30 — the dashboard showed only "Failed" when the server refused a
// blocked theme.custom_js.
//
// THE DEFECT (measured on prod v1.21.296, /form-settings?form=<id>, General tab,
// "Custom JS (power users)" textarea):
//
//	PATCH /api/rest/forms/<id>?workspace=<ws> {"theme":{"custom_js":"eval('2+2');"}}
//	  -> 400 {"code":"request.badRequest","error":"custom_js ditolak: active code execution marker: eval("}
//
//	page: p[data-testid="action-msg"] data-tone="failure" role="alert" text "Failed"
//
// The REFUSAL was correct and nothing was written. What was missing was a name for
// it on the wire: the sentence is ASSEMBLED around the scan's reason
// ("custom_js ditolak: " + reason), so no exact key in errCodeByMessage could ever
// match one, and it fell through to the HTTP class code `request.badRequest` —
// which web/src/lib/apiErr.ts deliberately does not translate, because a class
// code says nothing a page cannot say better itself. The merchant therefore read
// the page's generic "Failed", which names neither the control nor the reason.
//
// WHAT THIS FILE PINS
//  1. The refusal carries its own code (`form.customJsRejected`) on EVERY REST
//     door that writes that sentence: PATCH /v1/rest/forms/{id}, POST
//     /v1/rest/forms and POST /v1/rest/templates. One code, so the dashboard's one
//     sentence cannot miss a route.
//  2. It does so for all three reason classes the scan can fire — active code
//     execution, obfuscation/beacon, external URL — because the code is keyed on
//     the SENTENCE's lead-in, not on any one reason.
//  3. The generic fallback still holds for every OTHER 400 the same PATCH can
//     answer (a malformed product_image_size, a non-https logo_url, bad JSON): the
//     new code must not swallow its neighbours, or the dashboard would say "Custom
//     JS was rejected" for a colour/layout mistake.
//  4. A benign custom_js still saves (no false positive), and a refused write
//     still stores nothing.
//  5. The same lead-in is the one the MCP form/template tools write, so the code
//     claim ("one code for every door") is not a REST-only fact. The MCP tools
//     answer plain text (MCP has no error envelope), so the shared SENTENCE is the
//     whole contract there; a source check keeps the two halves from drifting.
//
// Runs the real mux against real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
)

// customJSRefusalCode decodes the machine `code` out of an error body.
func customJSRefusalCode(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var out struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode error body %s: %v", raw, err)
	}
	return out.Code
}

// TestCustomJSRefusalCodeIsNotTheClassCode is the pure half: the code the wire
// carries for each reason class, computed from the scan's REAL output rather than
// from a hand-written reason, plus the control that other 400s from the same PATCH
// keep the class code.
func TestCustomJSRefusalCodeIsNotTheClassCode(t *testing.T) {
	for _, tc := range blockedCustomJSCases() {
		reason := customform.ScanCustomJS(tc.js)
		if reason == "" {
			t.Fatalf("customform.ScanCustomJS(%q) returned no reason — the fixture no longer "+
				"exercises the scan", tc.js)
		}
		msg := customform.RejectedMessage(reason)
		if got := ErrCodeFor(msg); got != CodeFormCustomJSRejected {
			t.Errorf("ErrCodeFor(%q) = %q, want %q (%s).\n"+
				"Without it the 400 ships `%s`, a class code the dashboard deliberately does "+
				"not translate, so /form-settings renders its generic \"Failed\" and names "+
				"neither the field nor the reason.",
				msg, got, CodeFormCustomJSRejected, tc.name, ClassBadRequest)
		}
		if got := codeFor(http.StatusBadRequest, msg); got != CodeFormCustomJSRejected {
			t.Errorf("codeFor(400, %q) = %q, want %q", msg, got, CodeFormCustomJSRejected)
		}
	}

	// THE CONTROL. Every one of these is a 400 the same PATCH route can answer,
	// and none of them may be reported to the merchant as a rejected Custom JS
	// value. The first four have no specific code of their own and must keep the
	// class code; the fifth (urlguard) legitimately has its own URL code — what
	// matters here is only that it is NOT the custom_js code.
	for _, msg := range []string{
		"invalid json",
		"title wajib",
		"product_image_size mesti small|medium|large|hidden",
		"product_image_layout mesti thumbnail-left|image-top",
	} {
		if got := codeFor(http.StatusBadRequest, msg); got != ClassBadRequest {
			t.Errorf("codeFor(400, %q) = %q, want the class code %q — a non-custom_js 400 "+
				"must NOT be reported to the merchant as a rejected Custom JS value",
				msg, got, ClassBadRequest)
		}
	}
	// A neighbour that already owns a specific code keeps it: the new prefix must
	// not claim messages it does not describe.
	if got := codeFor(http.StatusBadRequest, "theme.logo_url mesti bermula dengan https:// — bukan http atau skema lain"); got != CodeURLNotHTTPS {
		t.Errorf("the non-https theme refusal = %q, want its own %q", got, CodeURLNotHTTPS)
	}
}

// TestCustomJSRefusalCodeOnEveryRestDoor drives the real mux: the update door the
// defect was measured on, the create door, and the template create door that clones
// its theme onto a form later.
func TestCustomJSRefusalCodeOnEveryRestDoor(t *testing.T) {
	fx := newRenameFixture(t, "it-cjs-code-"+randHex(4))
	// The gallery-door arm below seeds template rows through the store; remove
	// them with the rest of this test's forms so the package's global gallery
	// reads (ListTemplates is not account-scoped) never see them.
	cleanupProbeForms(t, fx)

	type door struct {
		name string
		call func(t *testing.T, body string) (int, json.RawMessage)
	}
	doors := []door{
		{"PATCH /v1/rest/forms/{id}", func(t *testing.T, body string) (int, json.RawMessage) {
			return fx.patch(t, body)
		}},
		{"POST /v1/rest/forms", func(t *testing.T, body string) (int, json.RawMessage) {
			return tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms", body)
		}},
		{"POST /v1/rest/templates", func(t *testing.T, body string) (int, json.RawMessage) {
			return tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates", body)
		}},
	}

	// Card t_d9b179d9: the OLDER gallery clone door is a door too — POST
	// /v1/templates/use accepts {template_id} and copies the template's theme
	// verbatim onto a new form, exactly like /v1/rest/templates/{id}/use. It
	// refuses on the value carried by a resolved ROW, so each case seeds one and
	// this closure reads the id set just before the door runs.
	galleryTemplateID := ""
	doors = append(doors, door{
		"POST /v1/templates/use {template_id}",
		func(t *testing.T, _ string) (int, json.RawMessage) {
			return tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/templates/use",
				`{"template_id":`+jsonString(galleryTemplateID)+`}`)
		},
	})

	for _, tc := range blockedCustomJSCases() {
		galleryTemplateID = seedUseTemplateRow(t, fx, map[string]any{"custom_js": tc.js}, false).ID
		theme := `{"custom_js":` + jsonString(tc.js) + `}`
		for _, d := range doors {
			// The create doors need a title; the PATCH ignores it. Send the same
			// shape to all three so the door, not the body, is what is exercised.
			body := `{"title":"custom js code probe","theme":` + theme + `}`
			code, raw := d.call(t, body)
			if code != http.StatusBadRequest {
				t.Fatalf("%s with custom_js %q = %d (body %s), want 400", d.name, tc.js, code, raw)
			}
			if got := customJSRefusalCode(t, raw); got != CodeFormCustomJSRejected {
				t.Errorf("%s refusal for %s carries code %q, want %q (body %s).\n"+
					"The dashboard maps this code to a sentence naming the Custom JS field; "+
					"`%s` would render its generic \"Failed\" again.",
					d.name, tc.name, got, CodeFormCustomJSRejected, raw, ClassBadRequest)
			}
			// The prose is deliberately unchanged: API-key/curl/MCP callers read it
			// and cannot look up an i18n key.
			var out struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("decode refusal body %s: %v", raw, err)
			}
			if !strings.Contains(out.Error, "custom_js ditolak: ") || !strings.Contains(out.Error, tc.wantReason) {
				t.Errorf("%s refusal prose = %q, want it to keep `custom_js ditolak: %s` — the "+
					"code is an ADDITION, not a replacement", d.name, out.Error, tc.wantReason)
			}
		}
	}

	// CONTROL 1: another 400 from the SAME PATCH keeps the class code.
	code, raw := fx.patch(t, `{"title":"custom js code probe","theme":{"product_image_size":"huge"}}`)
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH with a bad product_image_size = %d, want 400 (body %s)", code, raw)
	}
	if got := customJSRefusalCode(t, raw); got != ClassBadRequest {
		t.Errorf("bad product_image_size refusal carries code %q, want %q — a layout mistake "+
			"must not be reported as a rejected Custom JS value", got, ClassBadRequest)
	}

	// CONTROL 2: a benign custom_js still saves, so the code is attached to the
	// REFUSAL and the guard is not refusing everything.
	const benign = "document.addEventListener('click', function () { return true; });"
	code, raw = fx.patch(t, `{"title":"custom js code probe","theme":{"custom_js":`+jsonString(benign)+`}}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH with a benign custom_js = %d, want 200 (body %s)", code, raw)
	}
	if js, ok := customJSStoredOnSlug(t, fx, fx.slug); !ok || js != benign {
		t.Fatalf("stored custom_js = %q (present=%v), want the benign script verbatim", js, ok)
	}
}

// TestCustomJSRefusalLeadInHasExactlyOneCopy keeps the claim on the code's
// comment honest: ONE sentence, written by every door, is what lets one machine
// code speak for all of them. The REST doors are driven above; the MCP
// form/template tools answer PLAIN TEXT (MCP has no error envelope), so there
// the shared sentence IS the whole contract and nothing else can hold it.
//
// Two ways it could rot, both silent before this check:
//   - a door re-words the sentence locally, detaching the refusal from the code
//     that the dashboard keys its sentence on (the merchant sees "Failed"
//     again), or
//   - the scan's lead-in is reworded while a door keeps the old literal, so
//     half the doors ship an uncoded 400.
//
// Neither is a compile error: the wire is a string. So the invariant is that the
// literal exists in EXACTLY ONE non-test file — customform.go, which declares
// customform.RejectedLeadIn — and every write site (REST form create/update,
// REST template create/update/use, the MCP form/template tools) builds its
// sentence from that constant.
func TestCustomJSRefusalLeadInHasExactlyOneCopy(t *testing.T) {
	const literal = `"custom_js ditolak: "`

	root := filepath.Join("..") // api/internal
	var (
		holders []string
		mcpUses int
	)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		if strings.Contains(src, literal) {
			holders = append(holders, filepath.ToSlash(path))
		}
		if strings.Contains(filepath.ToSlash(path), "/mcpsrv/") &&
			strings.Contains(src, "customform.RejectedMessage(") {
			mcpUses++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// The declaration is the one and only copy of the sentence's lead-in.
	want := filepath.Join("..", "customform", "customform.go")
	if len(holders) != 1 || filepath.Clean(holders[0]) != filepath.Clean(want) {
		t.Fatalf("the `custom_js ditolak: ` literal appears in %v, want exactly one copy in %s "+
			"(the customform.RejectedLeadIn declaration).\nA second copy is a door that will "+
			"silently stop matching %q — the merchant reads the dashboard's generic \"Failed\" "+
			"again. Build every refusal with customform.RejectedMessage and key the code on "+
			"customform.RejectedLeadIn.", holders, want, CodeFormCustomJSRejected)
	}

	// And the MCP half still shares it: the package-level check would pass even
	// if mcpsrv had quietly gone back to its own wording.
	if mcpUses == 0 {
		t.Fatalf("no non-test file in internal/mcpsrv builds its custom_js refusal with " +
			"customform.RejectedMessage — the MCP form/template doors no longer share the " +
			"sentence the REST code is derived from, so " + CodeFormCustomJSRejected +
			" would describe one half only. Update both halves together.")
	}
}
