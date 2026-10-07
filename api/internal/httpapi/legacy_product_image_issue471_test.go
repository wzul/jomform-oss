package httpapi

// Card t_4861f7d8 (follow-up found on t_3718ca22 / PR #1001): the legacy
// LAST-RESORT layout used to drop every theme key but `primary`.
//
// `renderLegacy` -> `formTmpl` is the renderer a published payment form reaches
// when it has no builder blocks, is not response-mode and has no enabled menu
// (public.go, the `h.renderLegacy` tail). It read the merchant theme through
// `themePrimaryValues(f)` only, so the product photo settings #471 introduced —
// `product_image_size` and `product_image_layout`, both saved by
// /form-settings and both applied by the block renderer (render_blocks.go) —
// reached the row and died at the renderer. formTmpl rendered no <img> at all,
// so a merchant who saved "large / image-top" on a block-less payment form got
// the setting applied to nothing.
//
// The first test drives the REAL call site end to end (public mux, workspace
// host, live product row) rather than the template by hand, so a value that
// stops reaching the markup anywhere on the path — the theme read, the variant
// resolution, the view field, the template — is a red. The second is the
// DB-free template-level guard for the three shapes the first cannot express
// without a fixture each: hidden, an unknown value, and an image-less product.
//
// Card t_5d97a38e closed a gap in that split: `hidden` was only guarded by the
// DB-free helper (`productImageForSize(hidden) == ""`), one layer away from the
// renderer. Dropping the `!imgHidden` short-circuit in renderLegacy left the
// whole suite green while a merchant-chosen `hidden` shipped a photo. The
// `hidden` row below now drives the real mux, so the render path itself is the
// thing that fails when the short-circuit goes.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// legacyImageFixture seeds one workspace + account + product whose LIVE image
// URL points at a canonical JomForm media object (so the #382 variant rewrite
// applies) and a published form with NO blocks and a stale products snapshot,
// which is the shape that falls through to renderLegacy.
func legacyImageFixture(t *testing.T, theme map[string]any) (h *PublicHandlers, host, formSlug, image string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-legacy-img-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-legacy-img-ws-' || md5(random()::text), 'itli-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-legacy-img-acc-' || md5(random()::text), 'itli-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// The live image: a canonical media key, so the size resolves to a real
	// media variant (-thumbnail/-medium/-full) rather than the URL verbatim.
	image = "https://jomform.com/media/u/" + accountID + "/11111111-2222-3333-4444-555555555555.jpg"
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak", PriceSen: 2100,
		Type: dbstore.SaleTypeOneTime, Active: true,
		Images: []string{image},
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// NO `blocks` key: this is the shape serveForm falls through on.
	fs := dbstore.NewFormStore(pool)
	formSlug = "it-legacy-img-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Legacy image form", Status: "published",
		Config: map[string]any{
			"description": "Order something",
			"products": []any{
				map[string]any{"id": prod.ID, "name": "Stale Name", "price_sen": 1800, "active": true},
			},
		},
		Theme: theme,
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	return &PublicHandlers{
		Forms:      fs,
		Sales:      dbstore.NewSaleStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   dbstore.NewProductStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}, workspaceSlug + ".jomform.com", formSlug, image
}

// legacyImageBody renders the form through the public mux and asserts it really
// took the LEGACY path (renderBlocks must decline it, or the case would be
// measuring the block renderer and calling it legacy).
func legacyImageBody(t *testing.T, h *PublicHandlers, host, formSlug string) string {
	t.Helper()
	rec := issue461Serve(t, h, host, http.MethodGet, "/"+formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// formTmpl is the only renderer that emits the legacy chrome label class
	// plus this layout's product markup; blockTmpl puts its CTA in a separate
	// <button form="of">. Asserted, not assumed.
	if !strings.Contains(body, `data-slug="`+formSlug+`"`) || !strings.Contains(body, `class="pname"`) {
		t.Fatalf("the served page is not the legacy product layout — this case is no longer measuring renderLegacy\nbody:\n%s", body)
	}
	return body
}

// TestLegacyFormAppliesProductImageSettings is the guard this card asks for: a
// zero-block themed form rendered through renderLegacy must carry the merchant's
// saved product_image_size / product_image_layout in the markup.
//
// A green suite on the broken shape proves nothing, so each case asserts on the
// emitted attribute values (the resolved variant URL, the data-size the CSS
// keys on, and the menu-card class) rather than on "the page rendered".
//
// wantSize == ProductImageSizeHidden is the render-path case for a merchant who
// hid the photo: no image row at all. Its assertions differ from the visible
// cases on purpose (there is no src, class or data-size attribute to assert), so
// it carries the dedicated `hidden` branch in the body below.
func TestLegacyFormAppliesProductImageSettings(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size        string
		layout      string
		wantVariant string // media variant suffix in the src
		wantSize    string // data-size value
		wantMenu    bool   // .prod.menu-card layout class
	}{
		{"default (no theme keys) is the pre-#471 thumbnail, left of the name", "", "", "-thumbnail.jpg", "small", false},
		{"large / image-top — the card's own reported case", "large", "image-top", "-full.jpg", "large", true},
		{"small / thumbnail-left — membe/new-form's saved setting", "small", "thumbnail-left", "-thumbnail.jpg", "small", false},
		{"medium / thumbnail-left", "medium", "thumbnail-left", "-medium.jpg", "medium", false},
		{"hidden — the merchant hid the photo, so the render path paints none", ProductImageSizeHidden, "image-top", "", ProductImageSizeHidden, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme := map[string]any{"primary": "#5e6ad2"}
			if tc.size != "" {
				theme["product_image_size"] = tc.size
			}
			if tc.layout != "" {
				theme["product_image_layout"] = tc.layout
			}
			h, host, formSlug, image := legacyImageFixture(t, theme)
			body := legacyImageBody(t, h, host, formSlug)

			// The hidden row is the card t_5d97a38e case: the `!imgHidden`
			// short-circuit in renderLegacy must keep the image out of the
			// render path, not just out of the DB-free helper.
			if tc.wantSize == ProductImageSizeHidden {
				if n := strings.Count(body, `class="pimg"`); n != 0 {
					t.Fatalf("hidden: rendered %d product images, want 0 — the merchant hid the product photo and the renderer painted %d (card t_5d97a38e)\nbody:\n%s", n, n, body)
				}
				if strings.Contains(body, `data-size="`) {
					t.Error("hidden: a data-size attribute reached the markup — no product image should have been rendered")
				}
				if strings.Contains(body, `class="prod menu-card"`) {
					t.Error("hidden: the image-top menu-card layout was applied even though the photo is hidden")
				}
				if !strings.Contains(body, "Nasi Lemak") {
					t.Error("hidden: the product name went missing — hiding the photo must not hide the row")
				}
				return
			}

			// 1. The product photo reaches the page at all.
			if n := strings.Count(body, `class="pimg"`); n != 1 {
				t.Fatalf("the legacy layout rendered %d product images, want exactly 1 — the merchant's product_image_size/layout died at the renderer (card t_4861f7d8)\nbody:\n%s", n, body)
			}
			// 2. The chosen size reached both the resolved variant and the
			//    data-size the stylesheet keys on.
			if !strings.Contains(body, `class="pimg" data-size="`+tc.wantSize+`"`) {
				t.Errorf("product image data-size is not %q — the merchant's product_image_size did not reach the markup\nbody:\n%s", tc.wantSize, body)
			}
			if !strings.Contains(body, tc.wantVariant) {
				t.Errorf("product image src does not resolve to the %s media variant — the saved size was not applied to the URL", tc.wantVariant)
			}
			// The full-size fallback a pre-#382 image needs.
			if !strings.Contains(body, `data-base="`+image+`"`) {
				t.Errorf("product image carries no data-base fallback to the full-size image %q", image)
			}
			// 3. The layout: image-top adds the menu-card class, thumbnail-left
			//    does not.
			if got := strings.Contains(body, `class="prod menu-card"`); got != tc.wantMenu {
				t.Errorf("menu-card layout class present = %v, want %v — the merchant's product_image_layout did not reach the markup", got, tc.wantMenu)
			}
			// 4. The image sits BEFORE the name under image-top (the menu-card
			//    contract), and after the checkbox otherwise.
			imgIdx := strings.Index(body, `class="pimg"`)
			nameIdx := strings.Index(body, `class="pname"`)
			if imgIdx < 0 || nameIdx < 0 || (tc.wantMenu && imgIdx > nameIdx) {
				t.Errorf("image-top must place the photo above the name (img=%d name=%d)", imgIdx, nameIdx)
			}
			// 5. The live product name still wins over the snapshot (issue
			//    #461 must not regress while the image is threaded through).
			if !strings.Contains(body, "Nasi Lemak") || strings.Contains(body, "Stale Name") {
				t.Error("the live product name no longer wins over the stale config snapshot")
			}
			// 6. The size CSS the attribute keys on is present.
			for _, rule := range []string{".pimg {", ".pimg[data-size=medium]", ".pimg[data-size=large]"} {
				if !strings.Contains(body, rule) {
					t.Errorf("the emitted stylesheet is missing %q — a data-size with no rule renders the default size", rule)
				}
			}
		})
	}
}

// TestLegacyFormProductImageHiddenAndFallbacks is the DB-free half: the shapes a
// fixture per case would only obscure — the merchant chose hidden, the stored
// value is one the allowlist does not know, and the product simply has no image.
// All three must render a plain checkbox row, never a broken <img>.
func TestLegacyFormProductImageHiddenAndFallbacks(t *testing.T) {
	base := func(p tmplProduct) legacyFormView {
		return legacyFormView{
			Slug: "demo", Title: "Demo", Description: "desc",
			Products: []tmplProduct{p},
			Lang:     "en", Ls: langStringsForCode("en"),
		}
	}
	render := func(t *testing.T, p tmplProduct) string {
		t.Helper()
		var buf strings.Builder
		if err := formTmpl.Execute(&buf, base(p)); err != nil {
			t.Fatalf("formTmpl.Execute: %v", err)
		}
		return buf.String()
	}

	t.Run("no image field renders no img", func(t *testing.T) {
		body := render(t, tmplProduct{ID: "p1", Name: "Item", PriceSen: 2500})
		if strings.Contains(body, `class="pimg"`) {
			t.Errorf("an image-less product rendered a product photo\nbody:\n%s", body)
		}
		if !strings.Contains(body, `class="pname"`) {
			t.Error("the product name went missing when the image is absent")
		}
		if strings.Contains(body, `class="prod menu-card"`) {
			t.Error("an image-less product must not take the image-top menu-card layout")
		}
	})

	t.Run("hidden size is expressed as an empty image, matching the block renderer", func(t *testing.T) {
		// The handler clears Image/BaseImage/ImgSize when the size resolves to
		// hidden (productImageForSize returns ""), exactly as render_blocks.go
		// does, so the template's `{{if .Image}}` guard is the mechanism.
		if got := productImageForSize("https://jomform.com/media/u/a/p1.jpg", ProductImageSizeHidden); got != "" {
			t.Errorf("productImageForSize(hidden) = %q, want \"\" — the hidden size would render an image", got)
		}
		body := render(t, tmplProduct{ID: "p1", Name: "Item", PriceSen: 2500})
		if strings.Contains(body, `class="pimg"`) {
			t.Error("hidden size rendered a product image")
		}
	})

	t.Run("an unknown stored value degrades to the pre-#471 look", func(t *testing.T) {
		// The renderer resolves through the allowlist helpers, so a hand-written
		// theme cannot reach the markup with a size the CSS has no rule for.
		for _, bad := range []string{"gigantic", "MEDIUM", "#fff", ""} {
			if got := productImageSizeOrDefault(bad); got != ProductImageSizeSmall {
				t.Errorf("productImageSizeOrDefault(%q) = %q, want %q — an unknown size must not reach the stylesheet", bad, got, ProductImageSizeSmall)
			}
		}
		for _, bad := range []string{"left", "IMAGE-TOP", ""} {
			if got := productImageLayoutOrDefault(bad); got != ProductImageLayoutThumbnailLeft {
				t.Errorf("productImageLayoutOrDefault(%q) = %q, want %q", bad, got, ProductImageLayoutThumbnailLeft)
			}
		}
	})
}
