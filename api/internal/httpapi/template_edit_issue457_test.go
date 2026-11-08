package httpapi

// Editing one of your OWN templates from the dashboard (Wan, 2026-09-19).
//
// The dashboard gained an Edit action on /templates that opens the template in
// the builder and saves it through PUT /v1/rest/templates/{id}. The trap that
// decision avoids is that the builder's ordinary save path writes a form's
// STATUS (POST /v1/rest/forms/{id}/status), and the two template queries are
// disjoint by status:
//
//	ListTemplatesByAccount  (the /templates page):
//	  account_id=$1 AND is_template AND NOT (status='published')
//	ListTemplates           (the public gallery every New-form picker reads):
//	  is_template AND status='published'        -- NO account filter
//
// so publishing a template would both hide it from the merchant's own list and
// expose it to every other merchant. These tests pin BOTH halves against a real
// Postgres: the template route keeps the row a draft and edits its config, and
// the form status route really does break it (which is why the UI must not call
// it). Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// galleryTemplateIDs fetches the public gallery (GET /v1/templates, no auth).
func galleryTemplateIDs(t *testing.T, h *RESTHandlers) map[string]bool {
	t.Helper()
	code, raw := tplReq(t, h, "", http.MethodGet, "/v1/templates", "")
	if code != http.StatusOK {
		t.Fatalf("gallery status = %d (body=%s)", code, raw)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode gallery: %v (%s)", err, raw)
	}
	ids := make(map[string]bool, len(rows))
	for _, r := range rows {
		ids[r.ID] = true
	}
	return ids
}

// ownTemplateIDs fetches the merchant's own templates (GET /v1/rest/templates).
func ownTemplateIDs(t *testing.T, h *RESTHandlers, key string) map[string]bool {
	t.Helper()
	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/templates", "")
	if code != http.StatusOK {
		t.Fatalf("own templates status = %d (body=%s)", code, raw)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode own templates: %v (%s)", err, raw)
	}
	ids := make(map[string]bool, len(rows))
	for _, r := range rows {
		ids[r.ID] = true
	}
	return ids
}

// galleryReadyConfig is a config the gallery accepts: publishing a template
// requires a category and a text block (issue #594), which is what makes the
// status route in the second test able to run at all.
func galleryReadyConfig(text string) map[string]any {
	return map[string]any{
		"mode":     "response",
		"category": "Test",
		"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": text},
		},
	}
}

// TestEditOwnTemplateSavesConfigAndStaysOutOfTheGallery is the flow the new
// Edit action performs, at the API level: save through the TEMPLATE route, then
// verify the new config is stored, the row is still in "my templates", and it
// has NOT appeared in the public gallery.
func TestEditOwnTemplateSavesConfigAndStaysOutOfTheGallery(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template edit test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	// No workspace is linked, deliberately: that is the state the gallery
	// account is REQUIRED to be in (templates@system.jomform must never have
	// one — dbstore.LegacyProvisionedEmails), and since t_8b185f34 a template
	// may be saved without one. The template route never publishes, so the
	// workspace gate (t_fadfe9f7) does not apply to this flow anyway.
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)

	// A template as the dashboard creates one: config + theme, draft status.
	cfg := galleryReadyConfig("before")
	tpl := &dbstore.Form{
		AccountID: acc, Slug: "tpl-edit-" + acc[:8], Title: "Editable",
		Config: cfg, Theme: map[string]any{"primary": "#123456"},
	}
	if err := h.Forms.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	// The Edit action's save: PUT the template route with the edited config.
	bodyBytes, err := json.Marshal(map[string]any{
		"title":  "Editable",
		"config": galleryReadyConfig("after"),
	})
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}
	code, raw := tplReq(t, h, key, http.MethodPut, "/v1/rest/templates/"+tpl.ID, string(bodyBytes))
	if code != http.StatusOK {
		t.Fatalf("update template status = %d, want 200 (body=%s)", code, raw)
	}

	var got struct {
		Config struct {
			Blocks []struct {
				Value string `json:"value"`
			} `json:"blocks"`
		} `json:"config"`
		Theme map[string]any `json:"theme"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode update response: %v (%s)", err, raw)
	}
	if len(got.Config.Blocks) != 1 || got.Config.Blocks[0].Value != "after" {
		t.Fatalf("update response config not applied: %s", raw)
	}
	// The theme the builder round-trips must survive: the store COALESCEs a nil
	// theme, so an edit that omits it can never wipe the design.
	if got.Theme["primary"] != "#123456" {
		t.Errorf("theme lost on edit: %v", got.Theme)
	}

	// Read it back the way the builder does.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/templates/"+tpl.ID, "")
	if code != http.StatusOK {
		t.Fatalf("get template status = %d (body=%s)", code, raw)
	}
	var readBack struct {
		Config struct {
			Blocks []struct {
				Value string `json:"value"`
			} `json:"blocks"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &readBack); err != nil {
		t.Fatalf("decode read-back: %v (%s)", err, raw)
	}
	if len(readBack.Config.Blocks) != 1 || readBack.Config.Blocks[0].Value != "after" {
		t.Fatalf("stored config is not the edited one: %s", raw)
	}

	// The row must still be listed as an own template...
	if !ownTemplateIDs(t, h, key)[tpl.ID] {
		t.Errorf("the edited template vanished from the merchant's own list")
	}
	// ...and must NOT have reached the public gallery. If it did, the save
	// published it, and every other merchant now sees it in their picker.
	if galleryTemplateIDs(t, h)[tpl.ID] {
		t.Errorf("the edited template appeared in the public gallery — it was published by accident")
	}
}

// TestPublishingATemplateBreaksBothLists documents the failure mode the Edit
// action avoids, so the guard is pinned to a real mechanism rather than a
// comment: the form status route, pointed at a template, both removes it from
// the merchant's list and inserts it into every merchant's gallery. If either
// query ever changes, the UI decision must be re-derived — this test fails
// instead of the reasoning going quietly stale.
func TestPublishingATemplateBreaksBothLists(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template publish-damage test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	// No workspace is linked, deliberately: a template's public address is the
	// GALLERY, not a workspace (t_8b185f34), and the account that owns the
	// gallery must never have one. This test therefore runs in the exact account
	// state the system templates account is in — publishing here is what puts a
	// template in GET /v1/templates, not the accident that guard was about.
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)

	tpl := &dbstore.Form{
		AccountID: acc, Slug: "tpl-pub-" + acc[:8], Title: "Publishable",
		Config: galleryReadyConfig("gallery ready"),
	}
	if err := h.Forms.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	if !ownTemplateIDs(t, h, key)[tpl.ID] {
		t.Fatalf("template missing from the own list before publishing")
	}
	if galleryTemplateIDs(t, h)[tpl.ID] {
		t.Fatalf("template already in the gallery before publishing")
	}

	// This is the route the builder's ordinary save path calls. On a template it
	// is the damaging one.
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+tpl.ID+"/status", `{"status":"published"}`)
	if code != http.StatusOK {
		t.Fatalf("status route = %d, want 200 (body=%s)", code, raw)
	}

	if ownTemplateIDs(t, h, key)[tpl.ID] {
		t.Errorf("a published template is still in the merchant's own list — " +
			"the own-list predicate changed; re-derive the builder's template contract")
	}
	if !galleryTemplateIDs(t, h)[tpl.ID] {
		t.Errorf("a published template did not reach the gallery — " +
			"the gallery predicate changed; re-derive the builder's template contract")
	}
}
