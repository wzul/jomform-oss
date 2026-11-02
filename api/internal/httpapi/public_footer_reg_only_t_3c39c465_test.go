package httpapi

// Card t_3c39c465: a workspace can store a company registration number and no
// business name at all — prod workspace `wan` is exactly that shape
// (business_name='', company_reg='SA6123456-K'). footerInfo.HasFooter counted
// CompanyReg, but all three footer renderers printed the number only INSIDE the
// business-name branch:
//
//	{{if .Footer.BusinessName}}<div class="jf-fbiz">{{.Footer.BusinessName}}{{if .Footer.CompanyReg}} · {{.Footer.CompanyReg}}{{end}}</div>{{end}}
//
// so the page shipped <footer class="jf-footer"> with no content in it (height 0
// in the browser) and the merchant's registration number appeared nowhere. The
// contract these tests pin:
//
//  1. a stored field is never silently dropped — a set CompanyReg appears in the
//     rendered body of every renderer (legacy form, block/payment form, and the
//     response-only form);
//  2. an emitted <footer> is never empty — the footer element always carries
//     content, so the aggregate that decides to emit it agrees with what the
//     templates paint;
//  3. the business-name case is unchanged: the name still owns the line and the
//     number is still its " · <reg>" suffix, printed exactly once.

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// footerRegOnly is the prod shape from the card: a registration number, no
// business name, no contact fields, no footer text, no legal links.
var footerRegOnly = footerInfo{HasFooter: true, CompanyReg: "SA6123456-K"}

// footerElement returns the inside of the <footer class="jf-footer"> element.
// Fails when the body has no footer or the element is unterminated, so a
// "no empty footer" assertion can never pass vacuously.
func footerElement(t *testing.T, body string) string {
	t.Helper()
	const open = `<footer class="jf-footer">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatalf("rendered body has no %s element", open)
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</footer>")
	if j < 0 {
		t.Fatalf("rendered body has an unclosed <footer class=\"jf-footer\">")
	}
	return rest[:j]
}

func renderLegacyFooter(t *testing.T, fi footerInfo) string {
	t.Helper()
	var buf bytes.Buffer
	err := formTmpl.Execute(&buf, legacyFormView{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
		OGMeta:   template.HTML(""),
		Footer:   fi,
		AntiSpam: template.HTML(antiSpamRenderFields()),
		NoIndex:  true,
		HeightJS: template.HTML(iframeHeightJS),
		Lang:     "en",
		Ls:       langStringsForCode("en"),
	})
	if err != nil {
		t.Fatalf("formTmpl.Execute: %v", err)
	}
	return buf.String()
}

func renderBlockFooter(t *testing.T, fi footerInfo) string {
	t.Helper()
	type block struct {
		ID, Type, Value, Src string
		ProductID            string
		PriceSen             int64
		Required             bool
		HasPrice             bool
		CondJSON             string
		Style                string
		Placeholder          string
		Description          string
		CSSClass             string
		Format               string
		Pattern              string
		RequiredMsg          string
		MaxLength            int
		DefaultValue         string
		Min, Max             string
		Step                 string
	}
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo",
		"Blocks":    []block{{Type: "text", Value: "Hello"}},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "ms", "Ls": formLangs["ms"],
		"Footer": fi,
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	return buf.String()
}

func renderResponseFooter(t *testing.T, fi footerInfo) string {
	t.Helper()
	var buf bytes.Buffer
	err := responseTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Fields": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS": template.HTML(iframeHeightJS),
		"AntiSpam": template.HTML(""),
		"Footer":   fi,
	})
	if err != nil {
		t.Fatalf("responseTmpl.Execute: %v", err)
	}
	return buf.String()
}

// TestFooterRegistrationNumberRendersWithoutBusinessName is the card's
// acceptance test: CompanyReg set + BusinessName empty, on all three renderers.
func TestFooterRegistrationNumberRendersWithoutBusinessName(t *testing.T) {
	renderers := []struct {
		name string
		body func(*testing.T, footerInfo) string
	}{
		{"formTmpl (legacy product form, public.go)", renderLegacyFooter},
		{"blockTmpl (payment/order form, render_blocks.go)", renderBlockFooter},
		{"responseTmpl (response-only form, render_blocks.go)", renderResponseFooter},
	}
	for _, r := range renderers {
		t.Run(r.name, func(t *testing.T) {
			body := r.body(t, footerRegOnly)

			// (1) the stored field reaches the page.
			if !strings.Contains(body, "SA6123456-K") {
				t.Fatalf("registration number is not rendered: a set CompanyReg must appear in the body")
			}
			if n := strings.Count(body, "SA6123456-K"); n != 1 {
				t.Errorf("registration number rendered %d times, want exactly 1", n)
			}

			// (2) the footer element is not empty.
			inner := footerElement(t, body)
			if strings.TrimSpace(inner) == "" {
				t.Fatalf("footer element is empty (%q): HasFooter counted CompanyReg but nothing rendered", inner)
			}
			if !strings.Contains(inner, "SA6123456-K") {
				t.Errorf("registration number is outside the footer element; inner=%q", inner)
			}

			// The chosen shape: the number owns its own footer row when there is
			// no business name to attach it to.
			if !strings.Contains(inner, `<div class="jf-frow">SA6123456-K</div>`) {
				t.Errorf("registration number is not rendered in its own .jf-frow: inner=%q", inner)
			}
			// No empty identity div: .jf-fbiz exists only with a business name.
			if strings.Contains(body, `<div class="jf-fbiz"></div>`) {
				t.Errorf("empty .jf-fbiz div rendered with no business name")
			}
		})
	}
}

// TestFooterRegistrationNumberStaysWithBusinessName guards the other direction:
// when the merchant has both, the number must keep its " · <reg>" suffix on the
// business-name line and must not be printed twice (once there, once in a row).
func TestFooterRegistrationNumberStaysWithBusinessName(t *testing.T) {
	fi := footerInfo{HasFooter: true, BusinessName: "Acme Sdn Bhd", CompanyReg: "SA6123456-K"}
	renderers := []struct {
		name string
		body func(*testing.T, footerInfo) string
	}{
		{"formTmpl", renderLegacyFooter},
		{"blockTmpl", renderBlockFooter},
		{"responseTmpl", renderResponseFooter},
	}
	for _, r := range renderers {
		t.Run(r.name, func(t *testing.T) {
			body := r.body(t, fi)
			if !strings.Contains(body, `<div class="jf-fbiz">Acme Sdn Bhd · SA6123456-K</div>`) {
				t.Errorf("business-name line lost its registration suffix; footer inner=%q", footerElement(t, body))
			}
			if n := strings.Count(body, "SA6123456-K"); n != 1 {
				t.Errorf("registration number rendered %d times, want exactly 1 (it belongs on the business-name line)", n)
			}
		})
	}
}

// TestFooterInfoAnySetCountsCompanyReg pins the aggregate that decides to emit a
// footer: dropping CompanyReg from it would hide a set field again, so the
// aggregate and the templates must agree about what counts as content.
func TestFooterInfoAnySetCountsCompanyReg(t *testing.T) {
	if !(footerInfo{CompanyReg: "SA6123456-K"}).anySet() {
		t.Error("footerInfo.anySet() is false for a footer whose only field is CompanyReg; a set field must emit the footer")
	}
	if (footerInfo{}).anySet() {
		t.Error("footerInfo.anySet() is true for a footer with no fields (issue #80: no footer by default)")
	}
	for name, fi := range map[string]footerInfo{
		"BusinessName":   {BusinessName: "Acme"},
		"ContactPhone":   {ContactPhone: "+60 12 345 6789"},
		"ContactAddress": {ContactAddress: "1 Jalan Test"},
		"FooterText":     {FooterText: "Terima kasih"},
		"PrivacyURL":     {PrivacyURL: "https://acme.example/privacy"},
		"TermsURL":       {TermsURL: "https://acme.example/terms"},
		"RefundURL":      {RefundURL: "https://acme.example/refund"},
	} {
		if !fi.anySet() {
			t.Errorf("footerInfo.anySet() is false for a footer whose only field is %s", name)
		}
	}
}

// TestFooterRegistrationNumberServedWithoutBusinessName drives the REAL public
// handlers over the exact prod shape (workspace `wan`: company_reg set,
// business_name empty) instead of hand-feeding footerInfo to the templates, so a
// call site that stops passing the footer, or a resolver that stops filling
// CompanyReg, reds here too.
func TestFooterRegistrationNumberServedWithoutBusinessName(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	wss := dbstore.NewWorkspaceStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('footer-reg-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	slug := "fr-" + userID[:8]
	ws, err := wss.Create(ctx, userID, "Footer reg", slug)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accountID); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}

	// Prod shape: ONLY the registration number is set.
	st, err := wss.Settings(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.BusinessName = ""
	st.CompanyReg = "SA6123456-K"
	st.ContactPhone, st.ContactAddress = "", ""
	st.PrivacyURL, st.TermsURL, st.RefundURL = "", "", ""
	st.FooterText = ""
	if err := wss.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	// One form per renderer: block/payment (blockTmpl), legacy checkout
	// (formTmpl), response-only (responseTmpl).
	forms := map[string]*dbstore.Form{
		"block (blockTmpl)": {
			AccountID: accountID, Slug: "reg-block-" + userID[:8], Title: "Block form",
			Status: dbstore.FormStatusPublished,
			Config: map[string]any{"blocks": []any{
				map[string]any{"type": "text", "value": "Hello"},
				map[string]any{"id": "it_prod", "type": "product", "product_id": "it_prod_marker", "value": "Marker item", "price_sen": 1000},
			}},
			Theme: map[string]any{"primary": "#5e6ad2"},
		},
		"legacy checkout (formTmpl)": {
			AccountID: accountID, Slug: "reg-pay-" + userID[:8], Title: "Payment form",
			Status: dbstore.FormStatusPublished,
			Config: map[string]any{"mode": "payment", "products": []any{
				map[string]any{"id": "p1", "name": "Item", "price_sen": 2500, "active": true},
			}},
			Theme: map[string]any{"primary": "#5e6ad2"},
		},
		"response-only (responseTmpl)": {
			AccountID: accountID, Slug: "reg-resp-" + userID[:8], Title: "Response form",
			Status: dbstore.FormStatusPublished,
			Config: map[string]any{"mode": "response", "blocks": []any{
				map[string]any{"type": "text", "value": "Hello"},
			}},
			Theme: map[string]any{"primary": "#5e6ad2"},
		},
	}
	for name, f := range forms {
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create %s form: %v", name, err)
		}
	}

	h := &PublicHandlers{
		Forms: fs, Accounts: dbstore.NewAccountStore(pool), Workspaces: wss,
		BaseURL: "http://localhost:8080",
	}
	host := slug + ".jomform.com"
	for name, f := range forms {
		rec := publicServe(t, h, host, "/"+f.Slug)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET %s/%s = %d, want 200 (body %s)", name, host, f.Slug, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		inner := footerElement(t, body)
		if strings.TrimSpace(inner) == "" {
			t.Errorf("%s: served footer element is empty (%q)", name, inner)
		}
		if !strings.Contains(inner, "SA6123456-K") {
			t.Errorf("%s: the stored registration number is not in the served footer; inner=%q", name, inner)
		}
	}

	// Control: adding a business name keeps the number as its suffix, once.
	st.BusinessName = "Acme Sdn Bhd"
	if err := wss.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings with business name: %v", err)
	}
	for name, f := range forms {
		body := publicServe(t, h, host, "/"+f.Slug).Body.String()
		inner := footerElement(t, body)
		if !strings.Contains(inner, `<div class="jf-fbiz">Acme Sdn Bhd · SA6123456-K</div>`) {
			t.Errorf("%s: business-name line lost its registration suffix; inner=%q", name, inner)
		}
		if n := strings.Count(body, "SA6123456-K"); n != 1 {
			t.Errorf("%s: registration number served %d times, want exactly 1", name, n)
		}
	}
}
