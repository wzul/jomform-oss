// Card t_806c3ea2: a footer legal URL whose host is a tenant storefront is not
// automatically dead just because its path is not one of the three legal pages.
//
// The v1.22.66 rule (unservableStorefrontLegalLink) dropped any storefront-host
// URL whose path was not exactly privacy|terms|refund, on the strength of
// "RegisterRoutes mounts only the exact GET /privacy|/terms|/refund patterns, so
// these paths 404 on EVERY storefront host we serve". That is true for /privacy/,
// /Privacy and /privacy/1 — and false as a general rule: the same host also
// serves GET /{$} (workspace landing) and GET /{slug} (published form / menu).
// Measured on prod at v1.22.66 (32ddee8f), read-only:
//
//	https://wan.jomform.com/test  -> 200 text/html   (published form)
//	https://wan.jomform.com/      -> 200 text/html   (workspace root)
//	https://wan.jomform.com/menu  -> 404 text/plain  (that slug does not exist)
//
// …and the footer dropped the first two. The rule now drops only a LEGAL-SHAPED
// near-miss (hostedLegalPathNearMiss): a first segment that is privacy/terms/
// refund in a non-canonical shape. Everything else stays the merchant's link,
// because we cannot tell a live form slug from a dead one without asking that
// tenant's data — and dropping a live link is the defect this card fixes.
package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// The predicate table, both directions. Enumerated in full because narrowing
// this rule is exactly the change that can silently drop an alternative it still
// needs: every legal near-miss must stay true while every live shape stays false.
func TestHostedLegalPathNearMiss(t *testing.T) {
	cases := []struct {
		path string
		want bool
		why  string
	}{
		// Canonical: the route serves these, so they are not a near-miss.
		{"/privacy", false, "canonical, served"},
		{"/terms", false, "canonical, served"},
		{"/refund", false, "canonical, served"},

		// Legal-shaped near-misses: exact-pattern routes do not match them.
		{"/privacy/", true, "trailing slash"},
		{"/Privacy", true, "capitalised segment"},
		{"/privacy/1", true, "extra segment"},
		{"/terms/2", true, "extra segment"},
		{"/REFUND/", true, "capitalised + trailing slash"},

		// Live storefront shapes (GET /{$} and GET /{slug}): must NOT be dropped.
		{"/", false, "workspace landing"},
		{"/test", false, "form slug"},
		{"/refund-request", false, "merchant's own refund form"},
		{"/privacy-policy", false, "form slug that merely starts like one"},
		{"", false, "empty path"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.why+": "+c.path, func(t *testing.T) {
			if got := hostedLegalPathNearMiss(c.path); got != c.want {
				t.Errorf("hostedLegalPathNearMiss(%q) = %v, want %v (%s)",
					c.path, got, c.want, c.why)
			}
		})
	}
}

// The footer link rule itself, without a database: which storefront URLs are
// ours to drop. `pages` has content for all three types, so a drop here can only
// be the path's doing.
func TestLiveStorefrontPathSurvivesFooterLegalLinkRule(t *testing.T) {
	pages := map[string]*dbstore.LegalPage{
		dbstore.LegalPagePrivacy: {WorkspaceID: "w", PageType: dbstore.LegalPagePrivacy, Content: "Saved."},
		dbstore.LegalPageTerms:   {WorkspaceID: "w", PageType: dbstore.LegalPageTerms, Content: "Saved."},
		dbstore.LegalPageRefund:  {WorkspaceID: "w", PageType: dbstore.LegalPageRefund, Content: "Saved."},
	}

	// LIVE paths the storefront really serves — on our own host, on another
	// tenant's host, and on a stale slug. The card's reachability example: a
	// merchant points "Refund policy" at their own refund-request form.
	for _, raw := range []string{
		"https://wan.jomform.com/test",
		"https://wan.jomform.com/",
		"https://wan.jomform.com/refund-request",
		"https://wan.jomform.com/privacy-policy",
		"https://wan.jomform.com/test?ref=mail",
		"https://other.jomform.com/test",
		"https://other.jomform.com/",
		"https://other.jomform.com/refund-request",
		"https://wanzulhosting.jomform.com/test",
	} {
		raw := raw
		t.Run("kept: "+raw, func(t *testing.T) {
			if got := servableHostedLegalLink(raw, "wan", pages); got != raw {
				t.Errorf("servableHostedLegalLink(%q) = %q, want it kept — the storefront serves "+
					"GET /{$} and GET /{slug}, so this link can be live", raw, got)
			}
		})
	}

	// The genuinely dead shapes stay dropped: legal near-misses, on our own host,
	// on another tenant's host, and on a stale slug.
	for _, raw := range []string{
		"https://wan.jomform.com/privacy/",
		"https://wan.jomform.com/Privacy",
		"https://wan.jomform.com/privacy/1",
		"https://wan.jomform.com/terms/2",
		"https://other.jomform.com/privacy/",
		"https://other.jomform.com/Privacy",
		"https://other.jomform.com/privacy/1",
		"https://wanzulhosting.jomform.com/privacy/",
	} {
		raw := raw
		t.Run("dropped: "+raw, func(t *testing.T) {
			if got := servableHostedLegalLink(raw, "wan", pages); got != "" {
				t.Errorf("servableHostedLegalLink(%q) = %q, want \"\" — a legal-shaped path the exact "+
					"GET /privacy|/terms|/refund patterns do not serve 404s on every host we serve",
					raw, got)
			}
		})
	}

	// The content-lookup gate follows the rule: a live path on another tenant's
	// host needs no lookup (nothing behind the gate may drop it), while a
	// near-miss still must be counted or the drop never runs.
	live := footerInfo{PrivacyURL: "https://other.jomform.com/test", TermsURL: "https://other.jomform.com/"}
	if n := judgedLegalLinkCount(live, "wan"); n != 0 {
		t.Errorf("judgedLegalLinkCount(live storefront paths) = %d, want 0 — no lookup can change them", n)
	}
	nearMiss := footerInfo{PrivacyURL: "https://other.jomform.com/privacy/"}
	if n := judgedLegalLinkCount(nearMiss, "wan"); n != 1 {
		t.Errorf("judgedLegalLinkCount(storefront near-miss) = %d, want 1 — the lookup that drops it "+
			"would be skipped", n)
	}
}

// End to end through the real public mux and a real database, in both
// directions, on our own host AND on another tenant's:
//
//	live paths (a published form slug, the workspace root)  -> 200 and LINKED
//	legal near-misses (/privacy/)                           -> 404 and DROPPED
//
// The premises are asserted first, so the "dropped" assertions cannot pass for
// lack of content and the "kept" ones cannot pass for a fixture that serves
// nothing.
func TestPublicLegalFooterKeepsLiveStorefrontPath(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := dbstore.NewWorkspaceStore(pool)
	fs := dbstore.NewFormStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('legal-live-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Workspace A: the footer under test.
	slug := "lv-" + userID[:8]
	ws, err := sp.Create(ctx, userID, "Live path footer", slug)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accountID); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	// Workspace B: the OTHER tenant. Its live form path is what the footer links
	// to (the v1.22.66 regression), and its /privacy/ is the dead shape that must
	// stay dropped.
	otherSlug := "ot-" + userID[:8]
	otherWS, err := sp.Create(ctx, userID, "Other tenant", otherSlug)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	var otherAccountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, otherWS.ID).Scan(&otherAccountID); err != nil {
		t.Fatalf("read other workspace account: %v", err)
	}

	// Content exists on both workspaces, so the only reason a link may be
	// dropped is its PATH.
	for _, pair := range []struct{ wsID, body string }{
		{ws.ID, "We keep your data safe."},
		{otherWS.ID, "Other tenant's privacy page."},
	} {
		if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
			WorkspaceID: pair.wsID, PageType: dbstore.LegalPagePrivacy,
			Mode: dbstore.LegalModeManual, Content: pair.body,
		}); err != nil {
			t.Fatalf("save privacy page: %v", err)
		}
	}

	blockForm := func(acct, formSlug, title string) *dbstore.Form {
		f := &dbstore.Form{
			AccountID: acct, Slug: formSlug, Title: title,
			Status: dbstore.FormStatusPublished,
			// A buyable block keeps the fixture on the BLOCK page, which is the
			// page carrying the footer this guard is about (card t_bfdcafbc).
			Config: map[string]any{"blocks": []any{
				map[string]any{"type": "text", "value": "Hello"},
				map[string]any{"id": "it_prod", "type": "product", "product_id": "it_prod_marker", "value": "Marker item", "price_sen": 1000},
			}},
			Theme: map[string]any{"primary": "#5e6ad2"},
		}
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", formSlug, err)
		}
		return f
	}
	form := blockForm(accountID, "live-"+userID[:8], "Live path form")
	otherForm := blockForm(otherAccountID, "test", "Other tenant form")

	h := &PublicHandlers{
		Forms:      fs,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: sp,
		BaseURL:    "http://localhost:8080",
	}
	host := slug + ".jomform.com"
	otherHost := otherSlug + ".jomform.com"

	liveOwnForm := "https://" + host + "/" + form.Slug
	liveOwnRoot := "https://" + host + "/"
	liveOtherForm := "https://" + otherHost + "/" + otherForm.Slug
	deadOwnPath := "https://" + host + "/privacy/"
	deadOtherPath := "https://" + otherHost + "/privacy/"

	// ── Premise: the paths are what the routes say they are ────────────────
	for _, premise := range []struct {
		host, path string
		want       int
		why        string
	}{
		{host, "/" + form.Slug, http.StatusOK, "a published form slug is served"},
		{host, "/", http.StatusOK, "the workspace root is served"},
		{otherHost, "/" + otherForm.Slug, http.StatusOK, "another tenant's published form slug is served"},
		{otherHost, "/privacy", http.StatusOK, "its privacy page has content"},
		{host, "/privacy/", http.StatusNotFound, "a legal near-miss is not served"},
		{otherHost, "/privacy/", http.StatusNotFound, "a legal near-miss is not served on any host"},
	} {
		rec := publicServe(t, h, premise.host, premise.path)
		if rec.Code != premise.want {
			t.Fatalf("fixture premise broken: GET %s%s = %d, want %d (%s; body %q)",
				premise.host, premise.path, rec.Code, premise.want, premise.why, rec.Body.String())
		}
	}

	st, err := sp.Settings(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.BusinessName = "Live Path Sdn Bhd"
	st.PrivacyURL = liveOwnForm
	st.TermsURL = liveOwnRoot
	st.RefundURL = liveOtherForm
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// ── Direction 1: live storefront paths survive, on both hosts ──────────
	body := publicServe(t, h, host, "/"+form.Slug).Body.String()
	if !strings.Contains(body, "Live Path Sdn Bhd") {
		t.Fatalf("the footer itself disappeared (no business name) — body %s", body)
	}
	for _, live := range []string{liveOwnForm, liveOwnRoot, liveOtherForm} {
		if !strings.Contains(body, `href="`+live+`"`) {
			t.Errorf("footer dropped the live storefront link %s — the storefront serves /{$} and "+
				"/{slug}, so this link answers 200 (body %s)", live, body)
		}
	}

	// ── Direction 2: the legal near-misses stay dropped ───────────────────
	st.PrivacyURL = deadOwnPath
	st.TermsURL = deadOtherPath
	st.RefundURL = liveOtherForm
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	body = publicServe(t, h, host, "/"+form.Slug).Body.String()
	if !strings.Contains(body, "Live Path Sdn Bhd") {
		t.Fatalf("the footer itself disappeared after storing dead links — the fix must drop the "+
			"DEAD LINK, not the merchant's footer (body %s)", body)
	}
	for _, dead := range []string{deadOwnPath, deadOtherPath} {
		if strings.Contains(body, `href="`+dead+`"`) {
			t.Errorf("footer still links %s, which answers 404 on the host it names", dead)
		}
	}
	// Control: the drop is narrow — the live link beside them survives.
	if !strings.Contains(body, `href="`+liveOtherForm+`"`) {
		t.Errorf("footer dropped the live link %s while dropping the dead ones", liveOtherForm)
	}
}
