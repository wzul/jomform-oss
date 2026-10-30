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

	// Card t_8366310f: a link on OUR host whose path the public route does not
	// serve is still OURS to drop. Measured on prod (2026-10-28): all three
	// shapes below answer 404 — /privacy/ and /privacy/1 fall through to the mux
	// default ("404 page not found"), /Privacy to GET /{slug} ("Borang tidak
	// dijumpai"). They were returned unchanged because the classifier collapsed
	// "on our host, unservable path" into "not ours". `pages` HAS privacy
	// content here on purpose: these cases cannot pass for the content reason.
	for _, raw := range []string{
		"https://wan.jomform.com/privacy/",
		"https://wan.jomform.com/Privacy",
		"https://wan.jomform.com/privacy/1",
	} {
		raw := raw
		t.Run("our host, unservable path dropped: "+raw, func(t *testing.T) {
			if got := servableHostedLegalLink(raw, "wan", pages); got != "" {
				t.Errorf("servableHostedLegalLink(%q) = %q, want \"\" — the public route 404s that "+
					"path, so the footer must not link it", raw, got)
			}
		})
	}
	// Control the other way: the same non-canonical paths on a host that is NOT
	// ours are the merchant's own site and stay untouched — we do not serve
	// them, so we cannot know what they answer.
	for _, raw := range []string{
		"https://acme.example/privacy/",
		"https://other.jomform.com/privacy/",
	} {
		raw := raw
		t.Run("not our host, untouched: "+raw, func(t *testing.T) {
			if got := servableHostedLegalLink(raw, "wan", empty); got != raw {
				t.Errorf("servableHostedLegalLink(%q) = %q, want it kept (another host is the "+
					"merchant's own site)", raw, got)
			}
		})
	}
}

func TestHostedLegalPageType(t *testing.T) {
	cases := []struct{ name, raw, slug, want string }{
		{"privacy on our host", "https://wan.jomform.com/privacy", "wan", dbstore.LegalPagePrivacy},
		{"terms on our host", "https://wan.jomform.com/terms", "wan", dbstore.LegalPageTerms},
		{"refund on our host", "https://wan.jomform.com/refund", "wan", dbstore.LegalPageRefund},
		{"host case-insensitive", "https://WAN.jomform.com/privacy", "wan", dbstore.LegalPagePrivacy},
		{"canonical path with query", "https://wan.jomform.com/privacy?ref=mail", "wan", dbstore.LegalPagePrivacy},
		{"other workspace's host", "https://other.jomform.com/privacy", "wan", ""},
		{"external host", "https://acme.example/privacy", "wan", ""},
		{"non-legal path", "https://wan.jomform.com/contact", "wan", ""},
		{"lookalike host suffix", "https://wan.jomform.com.evil.test/privacy", "wan", ""},
		// Card t_8366310f — the three prod-measured non-canonical shapes. The
		// public route serves only the exact segments, so these are "", which
		// is why the footer must drop them instead of treating them as another
		// host's URL.
		{"trailing slash", "https://wan.jomform.com/privacy/", "wan", ""},
		{"capitalised segment", "https://wan.jomform.com/Privacy", "wan", ""},
		{"extra path segment", "https://wan.jomform.com/privacy/1", "wan", ""},
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

// Card t_8366310f: the footer's "is this link servable here?" judgement and the
// public route's "do I serve this path?" answer must be ONE rule. This test
// drives the REAL mux (RegisterRoutes, so the actual GET /privacy|/terms|/refund
// patterns) for the prod-measured path shapes and pins the two answers
// together in both directions:
//
//	route answers 200  <=>  classifier names a page  <=>  footer keeps the link
//
// A future private copy of the path rule that disagrees with the router — in
// either direction: keeping a dead link, or dropping a live one — reds here.
func TestOwnHostLegalPathParityWithPublicRoute(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := dbstore.NewWorkspaceStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('legal-parity-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	slug := "lp-" + userID[:8]
	ws, err := sp.Create(ctx, userID, "Legal parity", slug)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accountID); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}

	// Content IS saved: the only reason a shape may be dropped is the path.
	if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
		WorkspaceID: ws.ID, PageType: dbstore.LegalPagePrivacy,
		Mode: dbstore.LegalModeManual, Content: "We keep your data safe.",
	}); err != nil {
		t.Fatalf("save privacy page: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	form := &dbstore.Form{
		AccountID: accountID, Slug: "lp-" + userID[:8], Title: "Parity form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:      fs,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: sp,
		BaseURL:    "http://localhost:8080",
	}
	host := slug + ".jomform.com"

	st, err := sp.Settings(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.BusinessName = "Parity Sdn Bhd"

	keptAny := false
	for _, path := range []string{"/privacy", "/privacy/", "/Privacy", "/privacy/1"} {
		path := path
		t.Run(path, func(t *testing.T) {
			rec := publicServe(t, h, host, path)
			served := rec.Code == http.StatusOK

			raw := "https://" + host + path
			pageType, ours := classifyHostedLegalURL(raw, slug)
			if !ours {
				t.Fatalf("classifyHostedLegalURL(%q) says the host is not ours — the fixture's own "+
					"subdomain must always be ours to judge", raw)
			}
			if gotServed := pageType != ""; gotServed != served {
				t.Errorf("path rule drifted from the route: %q => pageType %q (servable=%v) but the "+
					"real mux answered %d %s", path, pageType, gotServed, rec.Code, rec.Body.String())
			}

			// The footer must agree with the route: linked iff it answers 200.
			st.PrivacyURL = raw
			if err := sp.SaveSettings(ctx, st); err != nil {
				t.Fatalf("save settings: %v", err)
			}
			body := publicServe(t, h, host, "/"+form.Slug).Body.String()
			linked := strings.Contains(body, `href="`+raw+`"`)
			if linked != served {
				t.Errorf("footer link for %q: linked=%v but the route answered %d — the footer must "+
					"advertise exactly the paths that serve (body %s)", path, linked, rec.Code, body)
			}
			if served {
				keptAny = true
			}
		})
	}
	// Control: at least one shape really was served, so "dropped" assertions
	// above cannot pass because the fixture serves nothing at all.
	if !keptAny {
		t.Fatalf("no path shape answered 200 — the fixture proves nothing about keeping a live link")
	}
	if rec := publicServe(t, h, host, "/privacy"); rec.Code != http.StatusOK {
		t.Fatalf("control: GET %s/privacy = %d, want 200 with the saved content", host, rec.Code)
	}
}
