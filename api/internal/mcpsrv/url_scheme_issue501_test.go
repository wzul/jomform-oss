package mcpsrv

// Issue #501: the MCP write paths must apply the SAME https-only scheme rule as
// the REST paths, for every caller-supplied image/URL field:
//
//   - set_form_theme        logo_url            (was NOT validated at all)
//   - upsert_form           theme logo_url, config blocks[].src,
//                           config products[].images
//   - create_product / update_product  images[], variations[].images
//   - set_form_og_image     image_url (stored as the form's logo_url)
//   - create_template / update_template  theme/config (cloned onto real forms)
//   - set_workspace_settings / set_workspace_landing_page  privacy/terms/refund
//     urls and landing image_url/logo_url
//
// A non-https value is REJECTED with the shared urlguard message; an omitted
// (nil) field keeps the stored value and an empty string means "no image" —
// neither is rejected. The rule itself lives in internal/urlguard, so these
// tests are the per-field write-path half of the coverage; the rendered-output
// half (never #ZgotmplZ) lives in httpapi/url_scheme_issue501_test.go, which
// owns the templates.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) for the
// per-form RBAC resolution; skipped when unset.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue501MCPState is one seeded user/workspace/account plus the MCP server.
type issue501MCPState struct {
	srv       *Server
	ctx       context.Context
	accountID string
	pool      *pgxpool.Pool
}

// issue501MCPFixture seeds a user + workspace + account and returns an MCP
// server whose caller is that account (API-key-shaped identity: account bound,
// readwrite scope).
func issue501MCPFixture(t *testing.T) issue501MCPState {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i501-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i501-ws-' || md5(random()::text), 'i501-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'i501-acc-' || md5(random()::text), 'i501-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	srv := &Server{deps: &Deps{
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Forms:             dbstore.NewFormStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
	}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	return issue501MCPState{srv: srv, ctx: callCtx, accountID: accountID, pool: pool}
}

// seedForm inserts a draft form and returns its id.
func (s issue501MCPState) seedForm(t *testing.T, slug string) string {
	t.Helper()
	f := &dbstore.Form{
		AccountID: s.accountID, Slug: slug, Title: "Issue 501 " + slug,
		Config: map[string]any{}, Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := s.srv.deps.Forms.Create(context.Background(), f); err != nil {
		t.Fatalf("seed form: %v", err)
	}
	return f.ID
}

// seedProduct inserts an active product and returns its id.
func (s issue501MCPState) seedProduct(t *testing.T, name string) string {
	t.Helper()
	p := &dbstore.Product{AccountID: s.accountID, Name: name, PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true}
	if err := s.srv.deps.Products.Create(context.Background(), p); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	return p.ID
}

func mustReject(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: non-https value was ACCEPTED — the #ZgotmplZ class is back", field)
	}
	if !strings.Contains(err.Error(), "https://") {
		t.Errorf("%s: rejection message does not explain the https:// rule: %v", field, err)
	}
	if !strings.Contains(err.Error(), field) {
		t.Errorf("%s: rejection message does not name the field: %v", field, err)
	}
}

// ── set_form_theme: logo_url ────────────────────────────────────────────────

// TestIssue501MCPSetFormThemeLogoRejectsNonHTTPS is the field the survey
// flagged as having NO validation at all on the MCP path.
func TestIssue501MCPSetFormThemeLogoRejectsNonHTTPS(t *testing.T) {
	st := issue501MCPFixture(t)
	formID := st.seedForm(t, "i501-theme")

	for _, bad := range []string{
		"data:image/png;base64,AAAA",
		"javascript:alert(1)",
		"http://cdn.example.com/logo.png",
	} {
		_, _, err := st.srv.setFormTheme(st.ctx, nil, setFormThemeIn{FormID: formID, LogoURL: bad})
		mustReject(t, err, "logo_url")
	}

	// A valid https logo is stored…
	mediaURL := "https://jomform.com/media/u/" + st.accountID + "/11111111-2222-3333-4444-555555555555.png"
	if _, _, err := st.srv.setFormTheme(st.ctx, nil, setFormThemeIn{FormID: formID, LogoURL: mediaURL}); err != nil {
		t.Fatalf("valid https logo rejected: %v", err)
	}
	f, err := st.srv.deps.Forms.GetByID(context.Background(), st.accountID, formID)
	if err != nil {
		t.Fatalf("reload form: %v", err)
	}
	if got, _ := f.Theme["logo_url"].(string); got != mediaURL {
		t.Errorf("stored logo_url = %q, want %q", got, mediaURL)
	}
	// …and an empty logo clears the key (the pre-#501 behaviour).
	if _, _, err := st.srv.setFormTheme(st.ctx, nil, setFormThemeIn{FormID: formID, LogoURL: ""}); err != nil {
		t.Fatalf("empty logo rejected: %v", err)
	}
	f, _ = st.srv.deps.Forms.GetByID(context.Background(), st.accountID, formID)
	if got, ok := f.Theme["logo_url"]; ok {
		t.Errorf("empty logo did not clear the theme key (got %v)", got)
	}
}

// ── upsert_form: config src/images + theme logo ─────────────────────────────

func TestIssue501MCPUpsertFormConfigURLsRejectNonHTTPS(t *testing.T) {
	st := issue501MCPFixture(t)

	_, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title:  "bad src",
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "image", "src": "data:image/png;base64,AA"}}},
	})
	mustReject(t, err, "blocks[].src")

	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title:  "bad snapshot",
		Config: map[string]any{"products": []any{map[string]any{"id": "p1", "images": []any{"javascript:alert(1)"}}}},
	})
	mustReject(t, err, "products[].images")

	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title: "bad theme logo",
		Theme: map[string]any{"logo_url": "http://cdn.example.com/logo.png"},
	})
	mustReject(t, err, "logo_url")

	// The update path is validated too.
	formID := st.seedForm(t, "i501-upsert")
	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "patch",
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "image", "src": "ftp://x/y.png"}}},
	})
	mustReject(t, err, "blocks[].src")

	// A legitimate upsert (https src, https logo, no URL at all) still works.
	if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title:  "good",
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "image", "src": "https://jomform.com/media/u/a/x.png"}}},
		Theme:  map[string]any{"logo_url": "https://jomform.com/media/u/a/logo.png"},
	}); err != nil {
		t.Fatalf("legit upsert_form rejected: %v", err)
	}
	if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{FormID: formID, Title: "bare patch"}); err != nil {
		t.Fatalf("upsert_form update with no config/theme rejected: %v", err)
	}
}

// ── products: images + variation images ─────────────────────────────────────

func TestIssue501MCPProductImagesRejectNonHTTPS(t *testing.T) {
	st := issue501MCPFixture(t)

	_, _, err := st.srv.createProduct(st.ctx, nil, createProductIn{
		Name: "Tee", PriceSen: 1000, Images: []string{"data:image/png;base64,AA"},
	})
	mustReject(t, err, "images")

	_, _, err = st.srv.createProduct(st.ctx, nil, createProductIn{
		Name: "Tee", PriceSen: 1000,
		Variations: []variationIn{{Name: "M", PriceSen: 1000, Images: []string{"javascript:alert(1)"}}},
	})
	mustReject(t, err, "variations[].images")

	// A valid https image works, and an image-less product works.
	if _, _, err := st.srv.createProduct(st.ctx, nil, createProductIn{
		Name: "Tee", PriceSen: 1000, Images: []string{"https://jomform.com/media/u/a/x.jpg"},
	}); err != nil {
		t.Fatalf("create_product with an https image rejected: %v", err)
	}
	prodID := st.seedProduct(t, "Update me")

	// update_product: an explicit non-https list is rejected…
	_, _, err = st.srv.updateProduct(st.ctx, nil, updateProductIn{
		ProductID: prodID, Images: []string{"http://cdn.example.com/x.jpg"},
	})
	mustReject(t, err, "images")
	// …while omitting images (nil = keep) is accepted.
	if _, _, err := st.srv.updateProduct(st.ctx, nil, updateProductIn{ProductID: prodID, Name: "Renamed"}); err != nil {
		t.Fatalf("update_product omitting images rejected: %v", err)
	}
	// A valid https replacement is accepted.
	if _, _, err := st.srv.updateProduct(st.ctx, nil, updateProductIn{
		ProductID: prodID, Images: []string{"https://jomform.com/media/u/a/y.jpg"},
	}); err != nil {
		t.Fatalf("update_product with an https image rejected: %v", err)
	}
}

// ── set_form_og_image (stored as the form logo_url) ─────────────────────────

func TestIssue501MCPSetFormOGImageRejectsNonHTTPS(t *testing.T) {
	st := issue501MCPFixture(t)
	formID := st.seedForm(t, "i501-og")

	_, _, err := st.srv.setFormOGImage(st.ctx, nil, setFormOGImageIn{FormID: formID, ImageURL: "data:image/png;base64,AA"})
	mustReject(t, err, "image_url")

	if _, _, err := st.srv.setFormOGImage(st.ctx, nil, setFormOGImageIn{
		FormID: formID, ImageURL: "https://jomform.com/media/u/a/og.png",
	}); err != nil {
		t.Fatalf("set_form_og_image with an https image rejected: %v", err)
	}
	// Empty clears the custom image and stays allowed.
	if _, _, err := st.srv.setFormOGImage(st.ctx, nil, setFormOGImageIn{FormID: formID, ImageURL: ""}); err != nil {
		t.Fatalf("set_form_og_image with an empty image rejected: %v", err)
	}
}

// ── templates (cloned onto real forms) ──────────────────────────────────────

func TestIssue501MCPTemplateConfigURLsRejectNonHTTPS(t *testing.T) {
	st := issue501MCPFixture(t)

	_, _, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{
		Title:  "bad tpl",
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "image", "src": "data:image/png;base64,AA"}}},
	})
	mustReject(t, err, "blocks[].src")

	_, _, err = st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{
		Title: "bad tpl theme", Theme: map[string]any{"logo_url": "javascript:alert(1)"},
	})
	mustReject(t, err, "logo_url")

	// A legitimate template saves, then accepts a legitimate update.
	_, out, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{Title: "good tpl"})
	if err != nil {
		t.Fatalf("create_template rejected: %v", err)
	}
	tplID, _ := out["template_id"].(string)
	if tplID == "" {
		t.Fatalf("create_template returned no template_id: %v", out)
	}
	_, _, err = st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{
		TemplateID: tplID,
		Config:     map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "image", "src": "http://x/y.png"}}},
	})
	mustReject(t, err, "blocks[].src")
	if _, _, err := st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{
		TemplateID: tplID, Title: "renamed",
	}); err != nil {
		t.Fatalf("update_template with no config/theme rejected: %v", err)
	}
}

// ── workspace settings + landing page ───────────────────────────────────────

func TestIssue501MCPWorkspaceLandingURLsRejectNonHTTPS(t *testing.T) {
	st := issue501MCPFixture(t)
	var wsID string
	if err := st.pool.QueryRow(context.Background(),
		`SELECT workspace_id::text FROM accounts WHERE id=$1`, st.accountID).Scan(&wsID); err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}

	_, _, err := st.srv.setWorkspaceLandingPage(st.ctx, nil, setWorkspaceLandingPageIn{
		WorkspaceID: wsID, Enabled: true, ImageURL: "data:image/png;base64,AA",
	})
	mustReject(t, err, "image_url")

	_, _, err = st.srv.setWorkspaceLandingPage(st.ctx, nil, setWorkspaceLandingPageIn{
		WorkspaceID: wsID, Enabled: true, LogoURL: "http://cdn.example.com/logo.png",
	})
	mustReject(t, err, "logo_url")

	_, _, err = st.srv.setWorkspaceSettings(st.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: wsID, PrivacyURL: ptrString("javascript:alert(1)"),
	})
	mustReject(t, err, "privacy_url")

	// A valid landing page saves, and an omitted field keeps its value.
	if _, _, err := st.srv.setWorkspaceLandingPage(st.ctx, nil, setWorkspaceLandingPageIn{
		WorkspaceID: wsID, Enabled: true, Hero: "Hi",
		ImageURL: "https://jomform.com/media/u/a/banner.jpg",
		LogoURL:  "https://jomform.com/media/u/a/logo.png",
	}); err != nil {
		t.Fatalf("set_workspace_landing_page with https URLs rejected: %v", err)
	}
	if _, _, err := st.srv.setWorkspaceSettings(st.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: wsID, FooterText: ptrString("hi"),
	}); err != nil {
		t.Fatalf("set_workspace_settings omitting URL fields rejected: %v", err)
	}
}

func ptrString(s string) *string { return &s }
