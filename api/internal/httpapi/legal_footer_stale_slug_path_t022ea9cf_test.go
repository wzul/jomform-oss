// Card t_022ea9cf: a stored legal URL on one of the platform's subdomains but
// with a STALE SLUG and a NON-CANONICAL PATH must not survive in the public
// form footer.
//
// The two rules that had to agree, and neither did:
//   - rewriteHostedLegalURL (issue #231) corrects the host of a stale-slug URL
//     only when its path is exactly privacy|terms|refund;
//   - classifyHostedLegalURL (card t_8366310f) judges a link only when its host
//     equals the CURRENT slug.
//
// So `https://wanzulhosting.jomform.com/privacy/` was neither rewritten onto
// `wan.jomform.com` nor dropped, and the footer kept a link that 404s (measured
// on the built binary and on prod: the public mux serves only the exact
// GET /privacy|/terms|/refund patterns, so /privacy/ and /privacy/1 fall to
// "404 page not found" and /Privacy to GET /{slug} "Borang tidak dijumpai" — on
// ANY host).
//
// The fix (option a of the card) teaches the footer that the PATH rule is not
// workspace-specific: a link on any platform subdomain at a path the route
// serves nowhere is ours to drop. Another tenant's CANONICAL page link, and any
// external host, stay untouched exactly as before.
package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// The defect itself, without a database: the rewrite leaves the URL alone, and
// the servable rule must then drop it instead of handing it back to the footer.
func TestStaleSlugUnservableLegalLinkDropped(t *testing.T) {
	pages := map[string]*dbstore.LegalPage{
		dbstore.LegalPagePrivacy: {WorkspaceID: "w", PageType: dbstore.LegalPagePrivacy, Content: "Saved."},
	}
	empty := map[string]*dbstore.LegalPage{}

	// The card's shapes: stale slug + a path the public route does not serve.
	// `pages` HAS privacy content on purpose — these can only pass for the path.
	for _, raw := range []string{
		"https://wanzulhosting.jomform.com/privacy/",
		"https://wanzulhosting.jomform.com/Privacy",
		"https://wanzulhosting.jomform.com/privacy/1",
		"https://wanzulhosting.jomform.com/privacy/?ref=mail",
		"https://WANZULHOSTING.jomform.com/privacy/",
	} {
		raw := raw
		t.Run("dropped: "+raw, func(t *testing.T) {
			rewritten := rewriteHostedLegalURL(raw, "wan")
			if rewritten != raw {
				t.Fatalf("rewriteHostedLegalURL(%q) = %q — this arm pins the shape the rewrite "+
					"leaves alone; if the host is now corrected the fixture no longer reproduces "+
					"the defect", raw, rewritten)
			}
			if got := servableHostedLegalLink(rewritten, "wan", pages); got != "" {
				t.Errorf("servableHostedLegalLink(%q) = %q, want \"\" — the public route serves "+
					"only /privacy|/terms|/refund on any host, so this footer link 404s", raw, got)
			}
		})
	}

	// Control, the other direction: the same stale slug with a canonical path is
	// rewritten onto the current slug and KEPT while its page has content — the
	// fix must not become "drop anything with a stale slug".
	t.Run("stale slug canonical path rewritten and kept", func(t *testing.T) {
		raw := "https://wanzulhosting.jomform.com/privacy"
		rewritten := rewriteHostedLegalURL(raw, "wan")
		if want := "https://wan.jomform.com/privacy"; rewritten != want {
			t.Fatalf("rewriteHostedLegalURL(%q) = %q, want %q", raw, rewritten, want)
		}
		if got := servableHostedLegalLink(rewritten, "wan", pages); got != rewritten {
			t.Errorf("servableHostedLegalLink(%q) = %q, want it kept (the page has content)",
				rewritten, got)
		}
		if got := servableHostedLegalLink(rewritten, "wan", empty); got != "" {
			t.Errorf("servableHostedLegalLink(%q, no content) = %q, want dropped", rewritten, got)
		}
	})

	// Control: another tenant's canonical page link is not ours to judge.
	t.Run("another tenant canonical page kept", func(t *testing.T) {
		raw := "https://other.jomform.com/privacy"
		if got := servableHostedLegalLink(raw, "wan", empty); got != raw {
			t.Errorf("servableHostedLegalLink(%q) = %q, want it kept", raw, got)
		}
	})

	// Controls: the widening stops at hosts the tenant mux serves as a
	// storefront. A truly external host, the bare apex, a serving-reserved label
	// (www/app — JomForm's own deployment, routed elsewhere), and a lookalike
	// suffix are all left alone, canonical path or not.
	for _, raw := range []string{
		"https://acme.example/privacy/",
		"https://jomform.com/privacy/",
		"https://www.jomform.com/privacy/",
		"https://app.jomform.com/privacy/",
		"https://wanzulhosting.jomform.com.evil.test/privacy/",
	} {
		raw := raw
		t.Run("untouched: "+raw, func(t *testing.T) {
			if got := servableHostedLegalLink(raw, "wan", empty); got != raw {
				t.Errorf("servableHostedLegalLink(%q) = %q, want it kept — not a host we serve",
					raw, got)
			}
		})
	}

	// Control: an unresolvable workspace disables the judgement, as it already
	// does for the own-host rule (a lookup outage is not evidence about the link).
	t.Run("no workspace slug keeps the link", func(t *testing.T) {
		raw := "https://wanzulhosting.jomform.com/privacy/"
		if got := servableHostedLegalLink(raw, "", empty); got != raw {
			t.Errorf("servableHostedLegalLink(%q, no slug) = %q, want it kept", raw, got)
		}
	})

	// The gate that skips the content lookup must COUNT this shape: it is the
	// rule behind the gate, so a gate that only counted the current slug would
	// skip the lookup and keep the dead link (the t_8366310f half of the defect).
	fi := footerInfo{PrivacyURL: "https://wanzulhosting.jomform.com/privacy/"}
	if n := judgedLegalLinkCount(fi, "wan"); n != 1 {
		t.Errorf("judgedLegalLinkCount(stale slug, unservable path) = %d, want 1 — the lookup "+
			"that drops it would be skipped", n)
	}
	// Control: a footer of external links still skips the lookup entirely.
	external := footerInfo{
		PrivacyURL: "https://acme.example/privacy",
		TermsURL:   "https://acme.example/terms",
		RefundURL:  "https://acme.example/refund",
	}
	if n := judgedLegalLinkCount(external, "wan"); n != 0 {
		t.Errorf("judgedLegalLinkCount(all external) = %d, want 0", n)
	}
}

// End to end through the real public handler and a real database: the stored
// footer of a renamed workspace carries the OLD slug with a trailing slash. The
// footer must drop that link while keeping the merchant's other links, and the
// path it named must really 404.
func TestPublicLegalFooterDropsStaleSlugUnservablePath(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := dbstore.NewWorkspaceStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('legal-stale-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	slug := "ss-" + userID[:8]
	ws, err := sp.Create(ctx, userID, "Stale slug footer", slug)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accountID); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}

	// Both pages have content, so a dropped link can only be the path's doing.
	for _, page := range []struct{ typ, body string }{
		{dbstore.LegalPagePrivacy, "We keep your data safe."},
		{dbstore.LegalPageRefund, "Refunds within 30 days."},
	} {
		if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
			WorkspaceID: ws.ID, PageType: page.typ,
			Mode: dbstore.LegalModeManual, Content: page.body,
		}); err != nil {
			t.Fatalf("save %s page: %v", page.typ, err)
		}
	}

	host := slug + ".jomform.com"
	staleHost := "old-" + host
	stalePrivacy := "https://" + staleHost + "/privacy/"
	termsExternal := "https://acme.example/terms"
	refundHosted := "https://" + host + "/refund"

	st, err := sp.Settings(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.BusinessName = "Stale Slug Sdn Bhd"
	st.PrivacyURL = stalePrivacy
	st.TermsURL = termsExternal
	st.RefundURL = refundHosted
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	blockForm := &dbstore.Form{
		AccountID: accountID, Slug: "blk-" + userID[:8], Title: "Block form",
		Status: dbstore.FormStatusPublished,
		// A buyable block keeps this fixture on the BLOCK page, whose footer
		// this guard is about (card t_bfdcafbc).
		Config: map[string]any{"blocks": []any{
			map[string]any{"type": "text", "value": "Hello"},
			map[string]any{"id": "it_prod", "type": "product", "product_id": "it_prod_marker", "value": "Marker item", "price_sen": 1000},
		}},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, blockForm); err != nil {
		t.Fatalf("create block form: %v", err)
	}
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

	// ── Premise: the PATH the dropped URL names is dead on a host we serve ──
	// Same host, same saved content: /privacy answers 200, /privacy/ answers
	// 404 — so the URL above is dead because of its path, not missing content.
	if rec := publicServe(t, h, host, "/privacy"); rec.Code != http.StatusOK {
		t.Fatalf("fixture premise broken: GET %s/privacy = %d, want 200 with the saved content "+
			"(body %q)", host, rec.Code, rec.Body.String())
	}
	if rec := publicServe(t, h, host, "/privacy/"); rec.Code != http.StatusNotFound {
		t.Fatalf("fixture premise broken: GET %s/privacy/ = %d, want 404 — this test only means "+
			"something if the trailing-slash path really is unservable (body %q)",
			host, rec.Code, rec.Body.String())
	}

	// ── The fix: the stale-slug trailing-slash link is gone from the footer ──
	for _, formSlug := range []string{blockForm.Slug, paymentForm.Slug} {
		rec := publicServe(t, h, host, "/"+formSlug)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s/%s = %d, want 200 (body %s)", host, formSlug, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Stale Slug Sdn Bhd") {
			t.Fatalf("form %s: the footer itself disappeared (no business name) — the fix must "+
				"drop the DEAD LINK, not the merchant's footer", formSlug)
		}
		if strings.Contains(body, staleHost) {
			t.Errorf("form %s still emits the stale-slug host %s (href %q), which 404s",
				formSlug, staleHost, stalePrivacy)
		}
		// Controls, both directions: the merchant's external link survives, and
		// the canonical hosted link with content survives — so the drop is
		// narrow (dead path), not "no legal links at all".
		if !strings.Contains(body, `href="`+termsExternal+`"`) {
			t.Errorf("form %s dropped the external terms link %s", formSlug, termsExternal)
		}
		if !strings.Contains(body, `href="`+refundHosted+`"`) {
			t.Errorf("form %s dropped the canonical hosted refund link %s, which has content "+
				"and answers 200", formSlug, refundHosted)
		}
	}

	// ── Control the other way: store the canonical URL and the privacy link
	// returns, live, on the current slug ──────────────────────────────────────
	canonical := "https://" + host + "/privacy"
	st.PrivacyURL = canonical
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if rec := publicServe(t, h, host, "/privacy"); rec.Code != http.StatusOK {
		t.Fatalf("control: GET %s/privacy = %d, want 200", host, rec.Code)
	}
	for _, formSlug := range []string{blockForm.Slug, paymentForm.Slug} {
		body := publicServe(t, h, host, "/"+formSlug).Body.String()
		if !strings.Contains(body, `href="`+canonical+`"`) {
			t.Errorf("form %s does not link %s although the page has content and serves 200 — "+
				"the fix must not suppress a servable hosted page", formSlug, canonical)
		}
	}

	// ── The GATE alone: a footer whose only platform link is the stale-slug,
	// trailing-slash one. No own-host canonical link is left to make the
	// content lookup run for another reason, so a gate that counted only the
	// current slug would skip the lookup and keep the dead link (the
	// t_8366310f half of this defect, one layer out).
	st.PrivacyURL = stalePrivacy
	st.RefundURL = "https://acme.example/refund"
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	for _, formSlug := range []string{blockForm.Slug, paymentForm.Slug} {
		body := publicServe(t, h, host, "/"+formSlug).Body.String()
		if strings.Contains(body, staleHost) {
			t.Errorf("form %s still emits %s when it is the footer's only platform link — the "+
				"content-lookup gate skipped the rule that drops it", formSlug, staleHost)
		}
	}
}
