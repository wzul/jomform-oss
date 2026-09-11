package httpapi

// Issue #471 integration test (real Postgres, skipped unless
// JOMFORM_TEST_DATABASE_URL is set): the product image size + layout option is
// applied on the public form and the image URL resolves to the media variant
// that matches the chosen size — while the image itself keeps coming from the
// LIVE product row (#461).
//
// Covers:
//   - default (no theme key) → 52px thumbnail, thumbnail variant, no menu card;
//   - size=medium → the medium variant URL + data-size=medium;
//   - size=large + layout=image-top → the full variant URL and the menu-card
//     layout (image above the name);
//   - size=hidden → no <img class="pimg"> at all;
//   - an unknown size/layout value falls back to the default (never breaks).

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// issue471Fixture is the seeded state for the issue #471 integration tests.
type issue471FixtureState struct {
	h        *PublicHandlers
	host     string
	account  string
	formID   string
	formSlug string
	prodID   string
}

// issue471Fixture seeds a product whose LIVE image URL points at a canonical
// JomForm media object, so the variant rewrite applies.
func issue471Fixture(t *testing.T) issue471FixtureState {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-471-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-471-ws-' || md5(random()::text), 'it471-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-471-acc-' || md5(random()::text), 'it471-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// A canonical media object key so the variant rewrite applies (#382/#471).
	image := "https://jomform.com/media/u/" + accountID + "/11111111-2222-3333-4444-555555555555.jpg"
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak", PriceSen: 2100,
		Type: dbstore.SaleTypeOneTime, Active: true,
		Images: []string{image},
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-471-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Issue 471 form", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"type": "product", "product_id": prod.ID, "value": "Nasi Lemak"},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	return issue471FixtureState{
		h: &PublicHandlers{
			Forms:      fs,
			Sales:      dbstore.NewSaleStore(pool),
			Accounts:   dbstore.NewAccountStore(pool),
			Products:   dbstore.NewProductStore(pool),
			Workspaces: dbstore.NewWorkspaceStore(pool),
			BaseURL:    "http://localhost:8080",
		},
		host:     workspaceSlug + ".jomform.com",
		account:  accountID,
		formID:   f.ID,
		formSlug: formSlug,
		prodID:   prod.ID,
	}
}

// setTheme stores a theme map on the form (what the dashboard PATCH or MCP
// set_form_theme would do).
func (s issue471FixtureState) setTheme(t *testing.T, theme map[string]any) {
	t.Helper()
	if err := s.h.Forms.Update(context.Background(), s.account, s.formID, "", nil, theme); err != nil {
		t.Fatalf("update theme: %v", err)
	}
}

func (s issue471FixtureState) render(t *testing.T) string {
	t.Helper()
	rec := issue461Serve(t, s.h, s.host, http.MethodGet, "/"+s.formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestIssue471DefaultSizeIsSmallThumbnailVariant(t *testing.T) {
	s := issue471Fixture(t)
	body := s.render(t)

	if !strings.Contains(body, `class="pimg" data-size="small"`) {
		t.Errorf("default size is not small\nbody:\n%s", body)
	}
	if !strings.Contains(body, "-thumbnail.jpg") {
		t.Error("default size did not resolve to the thumbnail media variant")
	}
	if strings.Contains(body, `<div class="prod menu-card">`) {
		t.Error("default layout must be thumbnail-left, not image-top")
	}
}

func TestIssue471MediumSizeUsesMediumVariant(t *testing.T) {
	s := issue471Fixture(t)
	s.setTheme(t, map[string]any{"primary": "#5e6ad2", "product_image_size": "medium"})
	body := s.render(t)

	if !strings.Contains(body, `class="pimg" data-size="medium"`) {
		t.Errorf("medium size not applied\nbody:\n%s", body)
	}
	if !strings.Contains(body, "-medium.jpg") {
		t.Error("medium size did not resolve to the medium media variant")
	}
	if strings.Contains(body, "-thumbnail.jpg") {
		t.Error("medium size still used the thumbnail variant")
	}
}

func TestIssue471LargeSizeImageTopMenuCard(t *testing.T) {
	s := issue471Fixture(t)
	s.setTheme(t, map[string]any{
		"primary": "#5e6ad2", "product_image_size": "large", "product_image_layout": "image-top",
	})
	body := s.render(t)

	if !strings.Contains(body, `<div class="prod menu-card">`) {
		t.Errorf("image-top layout not applied\nbody:\n%s", body)
	}
	if !strings.Contains(body, `class="pimg" data-size="large"`) {
		t.Error("large size not applied")
	}
	if !strings.Contains(body, "-full.jpg") {
		t.Error("large size did not resolve to the full media variant")
	}
	imgIdx := strings.Index(body, `class="pimg"`)
	nameIdx := strings.Index(body, `class="pname"`)
	if imgIdx < 0 || nameIdx < 0 || imgIdx > nameIdx {
		t.Errorf("menu-card image must precede the name (img=%d name=%d)", imgIdx, nameIdx)
	}
}

func TestIssue471HiddenSizeRendersNoImage(t *testing.T) {
	s := issue471Fixture(t)
	s.setTheme(t, map[string]any{"primary": "#5e6ad2", "product_image_size": "hidden"})
	body := s.render(t)

	if strings.Contains(body, `class="pimg"`) {
		t.Errorf("hidden size rendered a product image\nbody:\n%s", body)
	}
	if !strings.Contains(body, `class="pname"`) {
		t.Error("product name missing when the image is hidden")
	}
}

func TestIssue471UnknownValueFallsBackToDefault(t *testing.T) {
	s := issue471Fixture(t)
	// A hand-written theme with a bad size/layout must degrade to the default
	// small / thumbnail-left look, never break the render.
	s.setTheme(t, map[string]any{
		"primary": "#5e6ad2", "product_image_size": "gigantic", "product_image_layout": "left",
	})
	body := s.render(t)

	if !strings.Contains(body, `class="pimg" data-size="small"`) {
		t.Error("unknown size did not fall back to small")
	}
	if strings.Contains(body, `<div class="prod menu-card">`) {
		t.Error("unknown layout did not fall back to thumbnail-left")
	}
}
