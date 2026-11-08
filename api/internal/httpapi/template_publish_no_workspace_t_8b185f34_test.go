package httpapi

// Card t_8b185f34: the publish gate refused a class it was never about.
//
// THE DEFECT. Card t_fadfe9f7 closed "publishing a form with no workspace" —
// such a row is 404 at every address forever, so a 200 was a lie. The gate ran
// on the ACCOUNT (HasLiveWorkspace), after the form read, and never looked at
// the row. That also locked out `is_template` rows, whose public address is not
// a workspace at all: GET /v1/templates serves `is_template AND
// status='published'` with NO account or workspace filter. And the account that
// owns the gallery — templates@system.jomform — must NEVER have a workspace
// (dbstore.LegacyProvisionedEmails, migration 000059), so the refusal's own
// remedy ("buat workspace dahulu") is the one action that account may not take.
// Publishing a template through REST/MCP was therefore closed by accident on
// the one account whose rows are addressable without a workspace.
//
// WHAT THIS PINS
//  1. POST /v1/rest/forms/{id}/status {"status":"published"} on a
//     workspace-less `is_template` row answers 200 and the row is then served
//     by the public gallery.
//  2. POST /v1/rest/forms/{id}/publish does the same for a second template.
//  3. The exemption is the CLASS, not the account: an ORDINARY form on the SAME
//     workspace-less account is still refused 409 workspace.required by
//     /status, /publish and /forms/bulk, and its stored status stays draft.
//  4. A bulk publish is exempt only when every owned id in the batch is a
//     template: a mixed batch is refused whole and the template in it stays
//     draft (the bulk door must not become a way around the gate).
//  5. The boundary is `published`: private/unlisted on a template are in no
//     public list and are served only through a workspace, so those stay
//     refused — otherwise the exemption would mint exactly the unreachable-live
//     row t_fadfe9f7 is about.
//
// BITE: drop the hasGalleryAddress() terms from publishForm/setFormStatus (and
// the AllGalleryTemplates() exemption from bulkForms) and cases 1, 2 and 4
// answer 409 on a row the gallery would have served.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestGalleryTemplatesPublishWithoutAWorkspace(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping gallery-template publish test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	// bundleFixture is deliberately workspace-less: that is the gallery
	// account's required state, not an accident of the fixture.
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	fs := h.Forms
	flat := strings.ReplaceAll(acc, "-", "")

	var wsOfAccount *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc).Scan(&wsOfAccount); err != nil {
		t.Fatalf("read account workspace: %v", err)
	}
	if wsOfAccount != nil {
		t.Fatalf("fixture is wrong: the account already carries workspace_id %v", *wsOfAccount)
	}

	statusOf := func(id string) string {
		t.Helper()
		var st string
		if err := pool.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, id).Scan(&st); err != nil {
			t.Fatalf("read stored status: %v", err)
		}
		return st
	}
	// assertNoAddressRefusal checks the shape of a 409 the workspace gate sends.
	assertNoAddressRefusal := func(code int, raw json.RawMessage, what string) {
		t.Helper()
		if code != http.StatusConflict {
			t.Fatalf("%s = %d, want 409 (body=%s)", what, code, raw)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s: decode refusal: %v (%s)", what, err, raw)
		}
		if body["code"] != CodeWorkspaceRequired {
			t.Fatalf("%s: refusal code = %v, want %q (body=%s)", what, body["code"], CodeWorkspaceRequired, raw)
		}
		if body["error"] != workspaceRequiredMessage {
			t.Fatalf("%s: refusal = %v, want %q", what, body["error"], workspaceRequiredMessage)
		}
	}

	// A gallery-ready template (issue #594 requires a category and a
	// description block before a template may be published).
	seedTemplate := func(slug, title string) *dbstore.Form {
		t.Helper()
		f := &dbstore.Form{
			AccountID: acc, Slug: slug, Title: title,
			Config: galleryReadyConfig("a template's intro line"),
			Theme:  map[string]any{},
		}
		if err := fs.CreateTemplate(ctx, f); err != nil {
			t.Fatalf("create template %s: %v", slug, err)
		}
		return f
	}
	// An ordinary response form on the same account — a legacy row, since the
	// create door has refused to mint one without a workspace since t_6c0badbe.
	ordinary := &dbstore.Form{
		AccountID: acc, Slug: "rest-nows-" + flat[:10], Title: "Ordinary form",
		Config: map[string]any{"mode": "response", "blocks": []any{}},
		Theme:  map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := fs.Create(ctx, ordinary); err != nil {
		t.Fatalf("seed ordinary form: %v", err)
	}

	tplStatus := seedTemplate("tpl-nows-status-"+flat[:8], "Gallery template via status")
	tplPublish := seedTemplate("tpl-nows-publish-"+flat[:8], "Gallery template via publish")
	tplBulk := seedTemplate("tpl-nows-bulk-"+flat[:8], "Gallery template via bulk")
	tplMixed := seedTemplate("tpl-nows-mixed-"+flat[:8], "Gallery template in a mixed batch")

	// Fixture sanity: every row really is workspace-less, templates included.
	for _, f := range []*dbstore.Form{ordinary, tplStatus, tplPublish, tplBulk, tplMixed} {
		var ws *string
		if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM forms WHERE id=$1`, f.ID).Scan(&ws); err != nil {
			t.Fatalf("read workspace_id of %s: %v", f.ID, err)
		}
		if ws != nil {
			t.Fatalf("fixture is wrong: %s already carries workspace_id %v", f.Slug, *ws)
		}
	}
	// None of the templates is in the gallery before publishing.
	if got := galleryTemplateIDs(t, h); got[tplStatus.ID] || got[tplPublish.ID] || got[tplBulk.ID] {
		t.Fatalf("fixture is wrong: a seeded template is already published (gallery=%v)", got)
	}

	// ── 1. The status route publishes a gallery template ───────────────────
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+tplStatus.ID+"/status",
		`{"status":"published"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /forms/{template}/status published on a workspace-less account = %d, want 200 "+
			"(body=%s) — the gallery is this row's address, so the workspace gate must not fire", code, raw)
	}
	if got := statusOf(tplStatus.ID); got != dbstore.FormStatusPublished {
		t.Fatalf("stored status = %q, want published", got)
	}
	if !galleryTemplateIDs(t, h)[tplStatus.ID] {
		t.Fatalf("the published template is not in GET /v1/templates — the publish answered 200 for a " +
			"row the gallery does not serve, which is the lie the other direction")
	}

	// ── 2. The /publish route publishes the same class ─────────────────────
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+tplPublish.ID+"/publish", ""); code != http.StatusOK {
		t.Fatalf("POST /forms/{template}/publish on a workspace-less account = %d, want 200 (body=%s)", code, raw)
	}
	if got := statusOf(tplPublish.ID); got != dbstore.FormStatusPublished {
		t.Fatalf("stored status after /publish = %q, want published", got)
	}
	if !galleryTemplateIDs(t, h)[tplPublish.ID] {
		t.Fatalf("the /publish-published template is not in GET /v1/templates")
	}

	// ── 3. The exemption is the CLASS, not the account ─────────────────────
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+ordinary.ID+"/status",
		`{"status":"published"}`)
	assertNoAddressRefusal(code, raw, "POST /forms/{ordinary}/status published")
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+ordinary.ID+"/publish", "")
	assertNoAddressRefusal(code, raw, "POST /forms/{ordinary}/publish")
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/bulk",
		`{"action":"publish","ids":["`+ordinary.ID+`"]}`)
	assertNoAddressRefusal(code, raw, "POST /forms/bulk publish [ordinary]")
	if got := statusOf(ordinary.ID); got != dbstore.FormStatusDraft {
		t.Fatalf("ordinary form status = %q, want draft — the refusal ran after the write", got)
	}

	// ── 4. Bulk: all-template batches are exempt, mixed ones are not ───────
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/bulk",
		`{"action":"publish","ids":["`+tplBulk.ID+`"]}`)
	if code != http.StatusOK {
		t.Fatalf("POST /forms/bulk publish over ONE gallery template = %d, want 200 (body=%s) — "+
			"the single publish is exempt, so the bulk door must agree", code, raw)
	}
	if got := statusOf(tplBulk.ID); got != dbstore.FormStatusPublished {
		t.Fatalf("bulk-published template status = %q, want published", got)
	}
	if !galleryTemplateIDs(t, h)[tplBulk.ID] {
		t.Fatalf("the bulk-published template is not in GET /v1/templates")
	}
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/bulk",
		`{"action":"publish","ids":["`+tplMixed.ID+`","`+ordinary.ID+`"]}`)
	assertNoAddressRefusal(code, raw, "POST /forms/bulk publish [template + ordinary]")
	if got := statusOf(tplMixed.ID); got != dbstore.FormStatusDraft {
		t.Fatalf("template in the refused mixed batch = %q, want draft — the gate must run before the write", got)
	}

	// ── 5. The boundary is `published` ─────────────────────────────────────
	// unlisted/private templates are in no public list and are served only
	// through their workspace, so on a workspace-less account they still have
	// no address.
	for _, body := range []string{
		`{"status":"unlisted"}`,
		`{"status":"private","password":"rahasia123"}`,
	} {
		code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+tplMixed.ID+"/status", body)
		assertNoAddressRefusal(code, raw, "POST /forms/{template}/status "+body)
	}
	if got := statusOf(tplMixed.ID); got != dbstore.FormStatusDraft {
		t.Fatalf("template status after the unlisted/private refusals = %q, want draft", got)
	}
}
