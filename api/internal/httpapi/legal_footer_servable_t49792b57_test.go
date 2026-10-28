// Card t_49792b57: a public form footer must not link to a hosted legal page the
// api answers 404 for.
//
// Measured on prod at the v1.21.219 verify (t_467a57be): `wan.jomform.com`'s
// public forms render `Privacy Policy -> https://wan.jomform.com/privacy` and
// `Terms of Service -> https://wan.jomform.com/terms`, and BOTH answer 404
// (19 bytes, text/plain). The settings carry the OLD slug
// (`wanzulhosting.jomform.com/privacy`), issue #231's rewrite puts them on the
// current slug, and `workspace_legal_pages` has no row for either page — so
// `handlePublicLegalPage` 404s by design while the footer advertises the page.
//
// The fix (option 1 from the card) is at the source, in workspaceFooter: a link
// that addresses THIS workspace's own hosted page is published only when that
// page has stored content. Every renderer (block, response, legacy payment) reads
// the footer from there, so all three lose the dead link with one change.
//
// The test drives the REAL public handler over the real host shapes for both a
// block form and a legacy payment form, and keeps a control in each direction:
//   - the same link is proven 404ing at the moment the footer drops it (so the
//     assertion is about a link that really was dead), and
//   - after the page content is saved the link comes back and answers 200, so
//     the fix cannot be "drop every legal link".
package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestPublicLegalFooterDropsHostedPageThatWould404(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := dbstore.NewWorkspaceStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('legal-footer-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	slug := "lf-" + userID[:8]
	ws, err := sp.Create(ctx, userID, "Legal footer", slug)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accountID); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}

	// Prod shape: the stored URLs name the workspace's PREVIOUS subdomain.
	st, err := sp.Settings(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.BusinessName = "Acme Sdn Bhd"
	st.PrivacyURL = "https://wanzulhosting.jomform.com/privacy"
	st.TermsURL = "https://wanzulhosting.jomform.com/terms"
	st.RefundURL = "https://acme.example/refund"
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	blockForm := &dbstore.Form{
		AccountID: accountID, Slug: "block-" + userID[:8], Title: "Block form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, blockForm); err != nil {
		t.Fatalf("create block form: %v", err)
	}
	// A legacy PAYMENT form (no blocks) — the shape prod's /new-form serves
	// through formTmpl, the second template carrying the same footer.
	//
	// Card t_7d4c20c8: a payment form with nothing to sell is no longer served
	// this layout at all — serveForm answers it with the "not accepting orders"
	// page, which is a standalone message with no footer. One sellable product
	// keeps this fixture on the real legacy checkout (formTmpl), which is the
	// page whose footer this guard is about.
	paymentForm := &dbstore.Form{
		AccountID: accountID, Slug: "pay-" + userID[:8], Title: "Payment form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "payment", "products": []any{
			map[string]any{"id": "p1", "name": "Item", "price_sen": 2500, "active": true},
		}},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, paymentForm); err != nil {
		t.Fatalf("create payment form: %v", err)
	}

	h := &PublicHandlers{
		Forms:      fs,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: sp,
		BaseURL:    "http://localhost:8080",
	}

	host := slug + ".jomform.com"
	privacyURL := "https://" + host + "/privacy"
	termsURL := "https://" + host + "/terms"

	// ── Premise: the links the footer used to emit are dead ────────────────
	// Asserted BEFORE the fix's assertion (below) so a page that started
	// serving 200 could never make the "dropped" assertion pass for free.
	for _, p := range []string{"/privacy", "/terms"} {
		rec := publicServe(t, h, host, p)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("fixture premise broken: GET %s%s = %d, want 404 — this test only means "+
				"something if the advertised page really was missing (body %q)",
				host, p, rec.Code, rec.Body.String())
		}
	}

	// ── The fix: no footer link to a hosted page that 404s ──────────────────
	for _, formSlug := range []string{blockForm.Slug, paymentForm.Slug} {
		rec := publicServe(t, h, host, "/"+formSlug)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s/%s = %d, want 200 (body %s)", host, formSlug, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Acme Sdn Bhd") {
			t.Fatalf("form %s: the footer itself disappeared (no business name) — the fix must "+
				"drop the DEAD LINK, not the merchant's footer", formSlug)
		}
		for _, dead := range []string{privacyURL, termsURL} {
			if strings.Contains(body, `href="`+dead+`"`) {
				t.Errorf("form %s still links %s, which GET %s answers 404 for", formSlug, dead, dead)
			}
		}
		// Control: a link to the merchant's OWN site is not ours to judge and
		// must survive.
		if !strings.Contains(body, `href="https://acme.example/refund"`) {
			t.Errorf("form %s dropped the external refund link — only links to our own "+
				"hosted pages may be removed", formSlug)
		}
	}

	// ── Control the other way: save the page and the link returns, live ─────
	if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
		WorkspaceID: ws.ID, PageType: dbstore.LegalPagePrivacy,
		Mode: dbstore.LegalModeManual, Content: "We keep your data safe.",
	}); err != nil {
		t.Fatalf("save privacy page: %v", err)
	}
	if rec := publicServe(t, h, host, "/privacy"); rec.Code != http.StatusOK {
		t.Fatalf("GET %s/privacy = %d after saving content, want 200", host, rec.Code)
	}
	for _, formSlug := range []string{blockForm.Slug, paymentForm.Slug} {
		rec := publicServe(t, h, host, "/"+formSlug)
		body := rec.Body.String()
		if !strings.Contains(body, `href="`+privacyURL+`"`) {
			t.Errorf("form %s does not link %s although the page now has content and serves 200 — "+
				"the fix must not suppress a servable hosted page", formSlug, privacyURL)
		}
		if strings.Contains(body, `href="`+termsURL+`"`) {
			t.Errorf("form %s links %s, which still has no content and 404s", formSlug, termsURL)
		}
	}
}

// The link rule itself, without a database: which URLs are ours to judge, and
// what happens to one when its page has no content.
func TestServableHostedLegalLink(t *testing.T) {
	hosted := "https://wan.jomform.com/privacy"
	pages := map[string]*dbstore.LegalPage{
		dbstore.LegalPagePrivacy: {WorkspaceID: "w", PageType: dbstore.LegalPagePrivacy, Content: "Saved."},
		dbstore.LegalPageTerms:   {WorkspaceID: "w", PageType: dbstore.LegalPageTerms, Content: ""},
		dbstore.LegalPageRefund:  {WorkspaceID: "w", PageType: dbstore.LegalPageRefund, Content: "  \n "},
	}
	empty := map[string]*dbstore.LegalPage{
		dbstore.LegalPagePrivacy: {WorkspaceID: "w", PageType: dbstore.LegalPagePrivacy},
	}

	t.Run("hosted with content kept", func(t *testing.T) {
		if got := servableHostedLegalLink(hosted, "wan", pages); got != hosted {
			t.Errorf("servableHostedLegalLink = %q, want %q", got, hosted)
		}
	})
	t.Run("hosted with no row dropped", func(t *testing.T) {
		if got := servableHostedLegalLink(hosted, "wan", empty); got != "" {
			t.Errorf("servableHostedLegalLink = %q, want \"\" (the page 404s)", got)
		}
	})
	t.Run("hosted with whitespace-only content dropped", func(t *testing.T) {
		if got := servableHostedLegalLink("https://wan.jomform.com/refund", "wan", pages); got != "" {
			t.Errorf("servableHostedLegalLink = %q, want \"\" (blank content is not a page)", got)
		}
	})
	t.Run("terms with no row dropped", func(t *testing.T) {
		if got := servableHostedLegalLink("https://wan.jomform.com/terms", "wan", pages); got != "" {
			t.Errorf("servableHostedLegalLink = %q, want \"\"", got)
		}
	})
	t.Run("external URL is never judged", func(t *testing.T) {
		raw := "https://acme.example/privacy"
		if got := servableHostedLegalLink(raw, "wan", empty); got != raw {
			t.Errorf("servableHostedLegalLink = %q, want %q (another host is the merchant's site)", got, raw)
		}
	})
	t.Run("another workspace's host is not ours", func(t *testing.T) {
		raw := "https://other.jomform.com/privacy"
		if got := servableHostedLegalLink(raw, "wan", empty); got != raw {
			t.Errorf("servableHostedLegalLink = %q, want %q (not this workspace's host)", got, raw)
		}
	})
	t.Run("empty stays empty", func(t *testing.T) {
		if got := servableHostedLegalLink("", "wan", empty); got != "" {
			t.Errorf("servableHostedLegalLink = %q, want \"\"", got)
		}
	})
	t.Run("no workspace slug disables the judgement", func(t *testing.T) {
		raw := "https://wan.jomform.com/privacy"
		if got := servableHostedLegalLink(raw, "", empty); got != raw {
			t.Errorf("servableHostedLegalLink = %q, want %q (unresolvable workspace keeps the merchant's link)", got, raw)
		}
	})
}

func TestHostedLegalPageType(t *testing.T) {
	cases := []struct{ name, raw, slug, want string }{
		{"privacy on our host", "https://wan.jomform.com/privacy", "wan", dbstore.LegalPagePrivacy},
		{"terms on our host", "https://wan.jomform.com/terms", "wan", dbstore.LegalPageTerms},
		{"refund on our host", "https://wan.jomform.com/refund", "wan", dbstore.LegalPageRefund},
		{"host case-insensitive", "https://WAN.jomform.com/privacy", "wan", dbstore.LegalPagePrivacy},
		{"other workspace's host", "https://other.jomform.com/privacy", "wan", ""},
		{"external host", "https://acme.example/privacy", "wan", ""},
		{"non-legal path", "https://wan.jomform.com/contact", "wan", ""},
		{"lookalike host suffix", "https://wan.jomform.com.evil.test/privacy", "wan", ""},
		{"empty URL", "", "wan", ""},
		{"no slug", "https://wan.jomform.com/privacy", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hostedLegalPageType(c.raw, c.slug); got != c.want {
				t.Errorf("hostedLegalPageType(%q, %q) = %q, want %q", c.raw, c.slug, got, c.want)
			}
		})
	}
}
