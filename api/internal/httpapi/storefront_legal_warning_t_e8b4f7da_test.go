// The storefront's legal URLs, told to the MERCHANT (card t_e8b4f7da).
//
// THE DEFECT THIS PINS
//   Saving a legal URL that the public footer will silently drop was confirmed
//   as success: the settings PUT answered 200, the page showed "Footer saved ✓",
//   the DB row held the URL verbatim — and the public form rendered no footer
//   link at all, because the footer drops a link the public route would 404.
//   Measured on prod (v1.22.43): `https://abc.jomform.com/privacy/` saved fine
//   while GET https://abc.jomform.com/privacy/ answered 404 and the form page
//   stayed byte-identical to the pre-save baseline. Nothing on /settings said
//   why, in either the Storefront or the Legal tab.
//
//   The fix does not change the footer (the drop is correct — a dead link is
//   worse). It makes the save ANSWER: `legal_link_status` carries the footer's
//   own verdict per field, so the dashboard can say "not shown, because …" next
//   to the field. The GET carries the same key, so a merchant who saved such a
//   URL before this existed still learns it on load.
//
// WHY THE PARITY TEST EXISTS
//   The card forbids a second copy of the host/path rule in the dashboard. The
//   server therefore reuses rewriteHostedLegalURL + servableHostedLegalLink, and
//   TestLegalLinkStateAgreesWithTheFooterRule asserts the two can never disagree
//   across the whole matrix of hosts, paths and stored-content states.

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// legalWarningFixture creates an isolated user + workspace (+ settings row) and
// returns the workspace slug, which is what the footer's link rule keys on.
func legalWarningFixture(t *testing.T, ctx context.Context, prefix string) (userID, workspaceID, slug string) {
	t.Helper()
	p := bundleTestPool(t)

	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ($1 || '-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`, prefix).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, $2 || '-' || md5(random()::text), $2 || '-' || md5(random()::text))
		 RETURNING id, slug`, userID, prefix).Scan(&workspaceID, &slug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, $2 || '-acc-' || md5(random()::text), $2 || '-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $3
		 RETURNING id`, userID, prefix, slug).Scan(new(string)); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := dbstore.NewWorkspaceStore(p).Settings(ctx, workspaceID); err != nil {
		t.Fatalf("materialise settings row: %v", err)
	}
	return userID, workspaceID, slug
}

// legalWarningEndpoints drives the REAL REST routes with a real session cookie.
type legalWarningEndpoints struct {
	put func(body string) *httptest.ResponseRecorder
	get func() *httptest.ResponseRecorder
}

func newLegalWarningEndpoints(t *testing.T, pool *pgxpool.Pool, userID, workspaceID string) legalWarningEndpoints {
	t.Helper()
	h := newBundleHandlers(pool)
	sess := authTestValkey(t)
	h.Sessions = sess
	token, err := sess.Create(context.Background(), userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	do := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/workspaces/"+workspaceID+"/settings", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	return legalWarningEndpoints{
		put: func(body string) *httptest.ResponseRecorder { return do(http.MethodPut, body) },
		get: func() *httptest.ResponseRecorder { return do(http.MethodGet, "") },
	}
}

// legalWarningBody is the save/load response as the dashboard reads it.
type legalWarningBody struct {
	Status          string                    `json:"status"`
	LegalLinkStatus map[string]legalLinkState `json:"legal_link_status"`
}

func decodeLegalWarningBody(t *testing.T, rec *httptest.ResponseRecorder) legalWarningBody {
	t.Helper()
	var body legalWarningBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, rec.Body.String())
	}
	if body.LegalLinkStatus == nil {
		t.Fatalf("response has no `legal_link_status` — the dashboard then has nothing to "+
			"warn the merchant with (body %s)", rec.Body.String())
	}
	return body
}

func putLegalURL(t *testing.T, ep legalWarningEndpoints, field, url string) legalWarningBody {
	t.Helper()
	raw, err := json.Marshal(map[string]string{field: url})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rec := ep.put(string(raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s=%q status = %d, want 200 (body %s)", field, url, rec.Code, rec.Body.String())
	}
	return decodeLegalWarningBody(t, rec)
}

// TestStorefrontSaveWarnsThatAHostedSlashURLWillNotBeShown is the card's own
// reproduction: the exact URL QA typed on prod, saved through the real handler.
// The save still SUCCEEDS and stores the value verbatim (that is the merchant's
// data); what changes is that the answer now says the footer will not publish it.
func TestStorefrontSaveWarnsThatAHostedSlashURLWillNotBeShown(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	userID, workspaceID, slug := legalWarningFixture(t, ctx, "legal-warn-slash")
	ep := newLegalWarningEndpoints(t, pool, userID, workspaceID)

	typed := "https://" + slug + ".jomform.com/privacy/"
	body := putLegalURL(t, ep, "privacy_url", typed)

	st, err := dbstore.NewWorkspaceStore(pool).Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if st.PrivacyURL != typed {
		t.Fatalf("stored privacy_url = %q, want %q verbatim — the warning must not rewrite the "+
			"merchant's value", st.PrivacyURL, typed)
	}

	got := body.LegalLinkStatus["privacy_url"]
	if got.Shown {
		t.Fatalf("privacy_url %q reported shown=true, but the public route 404s that path — "+
			"the merchant would be confirmed into a footer link that never appears", typed)
	}
	if got.Reason != legalLinkPathNotServed {
		t.Fatalf("privacy_url reason = %q, want %q", got.Reason, legalLinkPathNotServed)
	}
	if want := "https://" + slug + ".jomform.com/privacy"; got.ServedURL != want {
		t.Errorf("served_url = %q, want %q — the warning tells the merchant which URL the route "+
			"actually serves", got.ServedURL, want)
	}
	if got.PageType != dbstore.LegalPagePrivacy {
		t.Errorf("page_type = %q, want %q", got.PageType, dbstore.LegalPagePrivacy)
	}
	if got.HasContent {
		t.Errorf("has_content = true for a workspace with no legal pages — the dashboard would then " +
			"advise changing the URL only, and the canonical URL still would not render")
	}

	// The warning must agree with what the footer really does with this value.
	pages, err := dbstore.NewWorkspaceStore(pool).LegalPages(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read legal pages: %v", err)
	}
	if served := servableHostedLegalLink(rewriteHostedLegalURL(typed, slug), slug, pages); served != "" {
		t.Fatalf("the footer WOULD publish %q (served=%q) while the save warned it would not — "+
			"the warning and the footer disagree", typed, served)
	}
}

// TestStorefrontSaveWarnsThatTheCanonicalHostedLinkHasNoPageYet covers the second
// arm of the same rule: the path is right, the page content is missing (0 rows in
// workspace_legal_pages is the state of every workspace on prod).
func TestStorefrontSaveWarnsThatTheCanonicalHostedLinkHasNoPageYet(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	userID, workspaceID, slug := legalWarningFixture(t, ctx, "legal-warn-empty")
	ep := newLegalWarningEndpoints(t, pool, userID, workspaceID)
	sp := dbstore.NewWorkspaceStore(pool)

	canonical := "https://" + slug + ".jomform.com/privacy"
	body := putLegalURL(t, ep, "privacy_url", canonical)
	got := body.LegalLinkStatus["privacy_url"]
	if got.Shown {
		t.Fatalf("privacy_url %q reported shown=true with no stored page content — the hosted "+
			"route answers 404 for it", canonical)
	}
	if got.Reason != legalLinkNoContent {
		t.Fatalf("privacy_url reason = %q, want %q", got.Reason, legalLinkNoContent)
	}
	if got.HasContent {
		t.Errorf("has_content = true with zero workspace_legal_pages rows")
	}

	// With content saved the same URL becomes shippable, and the warning clears —
	// otherwise the merchant has no way to make the warning go away.
	if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
		WorkspaceID: workspaceID,
		PageType:    dbstore.LegalPagePrivacy,
		Mode:        "manual",
		Content:     "Kami menghormati privasi anda.",
	}); err != nil {
		t.Fatalf("save legal page: %v", err)
	}
	body = putLegalURL(t, ep, "privacy_url", canonical)
	got = body.LegalLinkStatus["privacy_url"]
	if !got.Shown || got.Reason != "" {
		t.Fatalf("after saving the page: shown=%v reason=%q, want shown=true and no reason — "+
			"the warning must clear when the merchant does what it asks", got.Shown, got.Reason)
	}
}

// TestStorefrontSaveIsSilentAboutLinksItWillShow: an external URL (any other
// host) is the merchant's own site and is published unchanged, and an empty field
// is an absent link, not a warning. A false warning here would train the merchant
// to ignore the real one.
func TestStorefrontSaveIsSilentAboutLinksItWillShow(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	userID, workspaceID, _ := legalWarningFixture(t, ctx, "legal-warn-quiet")
	ep := newLegalWarningEndpoints(t, pool, userID, workspaceID)

	body := putLegalURL(t, ep, "terms_url", "https://acme.example/privacy-policy")
	got := body.LegalLinkStatus["terms_url"]
	if !got.Shown || got.Reason != "" {
		t.Fatalf("external terms_url: shown=%v reason=%q, want shown=true, no reason", got.Shown, got.Reason)
	}
	if got.ServedURL != "" || got.PageType != "" {
		t.Errorf("external terms_url carried hosted fields (page_type=%q served_url=%q) — "+
			"nothing about our host applies to it", got.PageType, got.ServedURL)
	}

	body = putLegalURL(t, ep, "refund_url", "")
	got = body.LegalLinkStatus["refund_url"]
	if got.Shown || got.Reason != "" {
		t.Fatalf("empty refund_url: shown=%v reason=%q, want shown=false and NO reason (an "+
			"unset field is not a warning)", got.Shown, got.Reason)
	}
}

// TestStorefrontWarningDoesNotClaimNoContentWithoutLookingItUp pins the
// lookup-skip gate (card t_16d159f5). The dashboard skips the legal-page lookup
// for the common all-external footer; that skip must never become
// "has_content: false" for a link the footer DOES judge.
//
// A stale slug at an unservable path is such a link after #1511: the footer drops
// it, yet it is not on the current slug's host, so rewriteHostedLegalURL does not
// rewrite it and an own-host-only gate counted zero — handed nil pages to the
// verdict, and told the merchant to write a page that already has text.
//
// The URL here is deliberately on ANOTHER host, so this is the ONE non-own-host
// link in the settings row: the old gate skips the lookup, the fixed gate does not.
func TestStorefrontWarningDoesNotClaimNoContentWithoutLookingItUp(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	userID, workspaceID, slug := legalWarningFixture(t, ctx, "legal-warn-stale-slug")
	ep := newLegalWarningEndpoints(t, pool, userID, workspaceID)
	sp := dbstore.NewWorkspaceStore(pool)

	// The privacy page HAS content …
	if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
		WorkspaceID: workspaceID,
		PageType:    dbstore.LegalPagePrivacy,
		Mode:        "manual",
		Content:     "Kami menghormati privasi anda.",
	}); err != nil {
		t.Fatalf("save legal page: %v", err)
	}

	// … and the ONLY legal link is a stale-slug host at a path the route 404s.
	staleSlug := "https://old-slug.jomform.com/privacy/"
	st, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.PrivacyURL = staleSlug
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// The gate itself: this link must be counted as one whose verdict needs the
	// lookup. An own-host-only count is 0 here, which is the defect.
	if n := legalLinkJudgedCount(st, slug); n != 1 {
		t.Fatalf("legalLinkJudgedCount(%q) = %d, want 1 — the footer drops this link, so the "+
			"lookup must not be skipped", staleSlug, n)
	}

	rec := ep.get()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	got := decodeLegalWarningBody(t, rec).LegalLinkStatus["privacy_url"]
	if got.Shown || got.Reason != legalLinkPathNotServed {
		t.Fatalf("privacy_url %q: shown=%v reason=%q, want shown=false reason=%q",
			staleSlug, got.Shown, got.Reason, legalLinkPathNotServed)
	}
	if want := "https://" + slug + ".jomform.com/privacy"; got.ServedURL != want {
		t.Errorf("served_url = %q, want %q", got.ServedURL, want)
	}
	if !got.HasContent {
		t.Fatalf("has_content=false while the privacy page HAS content — the lookup was skipped, " +
			"and the dashboard would tell the merchant to write a page that already has text")
	}
}

// TestStorefrontSettingsLoadCarriesTheFooterVerdict: the warning must not depend
// on pressing Save again. A merchant who saved a dropped URL BEFORE this shipped
// sees it when the tab loads. The response keeps every existing top-level key.
func TestStorefrontSettingsLoadCarriesTheFooterVerdict(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	userID, workspaceID, slug := legalWarningFixture(t, ctx, "legal-warn-load")
	ep := newLegalWarningEndpoints(t, pool, userID, workspaceID)
	sp := dbstore.NewWorkspaceStore(pool)

	// Store the dropped URL directly, as a pre-existing row would be.
	st, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	st.PrivacyURL = "https://" + slug + ".jomform.com/privacy/"
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	rec := ep.get()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeLegalWarningBody(t, rec)
	got := body.LegalLinkStatus["privacy_url"]
	if got.Shown || got.Reason != legalLinkPathNotServed {
		t.Fatalf("GET legal_link_status.privacy_url = %+v, want shown=false reason=%q", got, legalLinkPathNotServed)
	}

	// Shape guard: the dashboard reads the settings fields FLAT, so the new key
	// must be added beside them, not wrapped around them.
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &flat); err != nil {
		t.Fatalf("decode flat body: %v", err)
	}
	if _, ok := flat["privacy_url"]; !ok {
		t.Fatalf("GET settings no longer carries a top-level `privacy_url` — every existing "+
			"reader of this response breaks (body %s)", rec.Body.String())
	}
	if _, ok := flat["legal_link_status"]; !ok {
		t.Fatalf("GET settings has no `legal_link_status`")
	}
}

// TestLegalLinkStateAgreesWithTheFooterRule is the anti-drift guard the card
// demands: the dashboard's verdict is the FOOTER's verdict, over the whole matrix
// of host/path/content states. A hand-rolled second copy of the path rule fails
// here the moment it disagrees — including the arms a naive copy gets wrong (a
// stale slug, a capitalised segment, an extra path segment, a trailing slash).
func TestLegalLinkStateAgreesWithTheFooterRule(t *testing.T) {
	const slug = "wan"
	pages := map[string]*dbstore.LegalPage{
		dbstore.LegalPagePrivacy: {PageType: dbstore.LegalPagePrivacy, Content: "privasi"},
		dbstore.LegalPageTerms:   {PageType: dbstore.LegalPageTerms, Content: "   "}, // blank is not content
	}
	empty := map[string]*dbstore.LegalPage{}

	urls := []string{
		"",
		"https://wan.jomform.com/privacy",       // canonical + content → shown
		"https://wan.jomform.com/privacy/",      // trailing slash → dropped
		"https://wan.jomform.com/Privacy",       // capitalised → dropped
		"https://wan.jomform.com/privacy/1",     // extra segment → dropped
		"https://wan.jomform.com/privacy?x=1",   // query is not part of the path → shown
		"https://wan.jomform.com/terms",         // canonical, blank content → dropped
		"https://wan.jomform.com/refund",        // canonical, no row → dropped
		"https://wan.jomform.com/",              // bare host → dropped
		"https://wan.jomform.com",               // no path → dropped
		"https://old-slug.jomform.com/privacy/", // stale slug + bad path → dropped
		"https://old-slug.jomform.com/privacy",  // stale slug rewritten → shown
		"https://acme.example/privacy-policy",   // another host → shown, untouched
		"https://other.jomform.com/privacy",     // rewritten onto ours → judged as ours
		"https://other.jomform.com/privacy/",    // storefront host + bad path → dropped (t_16d159f5 arms below)
		// Card t_806c3ea2: a storefront host also serves / and /{slug}, so a
		// non-legal path there can be live and must stay published — only the
		// legal-shaped near-miss is dead on every host.
		"https://wan.jomform.com/test",   // own host, live path → shown
		"https://other.jomform.com/test", // another storefront host, live path → shown
	}
	fields := []string{dbstore.LegalPagePrivacy, dbstore.LegalPageTerms, dbstore.LegalPageRefund}
	pageSets := []map[string]*dbstore.LegalPage{pages, empty, nil}

	for _, pt := range fields {
		for _, pg := range pageSets {
			for _, raw := range urls {
				state := legalLinkStateFor(pt, raw, slug, pg)
				want := servableHostedLegalLink(rewriteHostedLegalURL(raw, slug), slug, pg) != ""
				if state.Shown != want {
					t.Errorf("legalLinkStateFor(%q, %q).Shown = %v, footer publishes it: %v — the "+
						"dashboard's warning and the footer's rule have diverged", pt, raw, state.Shown, want)
					continue
				}
				// A warning must always be actionable: it names the hosted page and
				// the exact URL to type, and says whether content is missing.
				if !state.Shown && state.Reason != "" {
					if state.PageType == "" {
						t.Errorf("legalLinkStateFor(%q, %q) warned (%q) without naming the hosted page",
							pt, raw, state.Reason)
					}
					if want := "https://" + slug + ".jomform.com/"; len(state.ServedURL) <= len(want) ||
						state.ServedURL[:len(want)] != want {
						t.Errorf("legalLinkStateFor(%q, %q) warned without a served_url on our host (got %q)",
							pt, raw, state.ServedURL)
					}
				}
				if state.Shown && state.Reason != "" {
					t.Errorf("legalLinkStateFor(%q, %q) is shown but carries reason %q", pt, raw, state.Reason)
				}
			}
		}
	}

	// ── The storefront arms #1511 made reachable (card t_16d159f5) ────────────
	// A legal URL on ANOTHER storefront host at a path the public route serves
	// nowhere (a stale slug, or another tenant's host, with /privacy/) is dropped
	// by the footer even though it is not on the current slug's host. The
	// dashboard must return the same actionable verdict as for our own bad path:
	// the path warning, this field's page, our own served URL, and the truth
	// about that page's content.
	for _, raw := range []string{
		"https://old-slug.jomform.com/privacy/", // the merchant's own stale slug, bad path
		"https://other.jomform.com/privacy/",    // another tenant's host, same shape
	} {
		for _, pt := range fields {
			for _, pg := range pageSets {
				st := legalLinkStateFor(pt, raw, slug, pg)
				if st.Shown {
					t.Errorf("legalLinkStateFor(%q, %q).Shown = true, but the footer drops that "+
						"link — the dashboard would confirm the merchant into a link the footer "+
						"withholds", pt, raw)
					continue
				}
				if st.Reason != legalLinkPathNotServed {
					t.Errorf("legalLinkStateFor(%q, %q).Reason = %q, want %q — the link is dropped "+
						"for its PATH, not for missing content", pt, raw, st.Reason, legalLinkPathNotServed)
				}
				if st.PageType != pt {
					t.Errorf("legalLinkStateFor(%q, %q).PageType = %q, want the field's own page %q",
						pt, raw, st.PageType, pt)
				}
				if want := "https://" + slug + ".jomform.com/" + pt; st.ServedURL != want {
					t.Errorf("legalLinkStateFor(%q, %q).ServedURL = %q, want %q", pt, raw, st.ServedURL, want)
				}
				if want := hostedLegalPageHasContent(pg, pt); st.HasContent != want {
					t.Errorf("legalLinkStateFor(%q, %q).HasContent = %v, want %v (the stored page's "+
						"own state)", pt, raw, st.HasContent, want)
				}
			}
		}
	}

	// A foreign host we cannot judge stays published and unaccused.
	if st := legalLinkStateFor(dbstore.LegalPagePrivacy, "https://acme.example/privacy-policy", slug, pages); !st.Shown || st.Reason != "" {
		t.Errorf("the merchant's own site (acme.example) = %+v, want shown with no warning", st)
	}

	// ── The dashboard's lookup gate must be the footer's gate ────────────────
	// legalLinkStatuses skips the legal-page lookup when the dashboard's count is
	// 0. A narrower count (e.g. own-host-only, which is what it was before card
	// t_16d159f5) hands legalLinkStateFor nil pages for a link the footer judges,
	// and has_content then reads false without ever having been looked up.
	for _, raw := range urls {
		if raw == "" {
			continue
		}
		st := &dbstore.WorkspaceSettings{PrivacyURL: raw}
		got := legalLinkJudgedCount(st, slug)
		want := judgedLegalLinkCount(footerInfo{PrivacyURL: rewriteHostedLegalURL(raw, slug)}, slug)
		if got != want {
			t.Errorf("lookup gate for %q: dashboard counts %d, footer counts %d — the dashboard "+
				"would skip (or demand) the content lookup the footer does not", raw, got, want)
		}
	}
}
