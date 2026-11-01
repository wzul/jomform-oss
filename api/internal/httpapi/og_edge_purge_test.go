package httpapi

// THE DEFECT THESE GUARD. A form's generated social card is served with a public
// freshness lifetime, so a crawler that fetched /og/{slug}.png while the form was
// live left a copy at the CDN edge. The status gate made the ORIGIN honest (draft
// -> 404, private -> 404 until unlocked) and did nothing about that copy.
//
// Measured on prod 2026-10-26, nine minutes after the release that shipped the
// gate: a DRAFT form's card answered from Cloudflare with
// `cf-cache-status: HIT`, `age: 1243`, `last-modified: 08:06:58Z` and bytes
// identical (cmp) to the deploy lane's PRE-release control, while the same URL
// with a cache-buster answered `404, 10 bytes "not found\n"`. The trigger a
// merchant actually hits: publish -> share (a crawler fills the edge with the 200
// card) -> set the form back to draft. The page 404s; the card keeps naming the
// title, the workspace and the host.
//
// What these tests measure, end to end over the real REST route and a real
// Postgres fixture. Since card t_b870d44b every transition below pins BOTH
// artefacts — the share card /og/{slug}.png and the QR PNG /qr/{slug}.png — on
// both public hosts, because the QR PNG discloses the form's existence at the
// slug and is cached for the same window:
//
//	TestDraftingAFormPurgesItsCachedSocialCard
//	    — POST /status {"status":"draft"} on a published form sends the card's and
//	      the QR PNG's PREFIXes to the CDN in the same request, names the outcome
//	      (`edge: purged`), and the origin then 404s both.
//	TestMakingAFormPrivatePurgesItsPublicCard
//	    — the same for a form going private: it is still LIVE (its page shows the
//	      password gate) but its card is no longer publicly served, so the public
//	      copy must go.
//	TestTrashingAPublishedFormPurgesItsCachedCard
//	    — a trashed form stops resolving publicly, so its card must go too.
//	TestDraftTransitionStillSucceedsWhenTheEdgePurgeIsNotConfigured
//	    — no CDN credentials (a supported deploy, and prod's state today): the
//	      status change still answers 200, the row really changed, and the reply
//	      says `edge: not_configured` plus the window that then bounds staleness —
//	      never a silent skip, never a failed request.
//	TestStatusChangeThatLeavesNoCachedCardAsksTheEdgeForNothing
//	    — published -> unlisted is public-to-public: nothing was invalidated, so
//	      no CDN call is made and the reply claims no edge action.
//	TestOGImagePublicCacheControlIsBoundedNotADay
//	    — the served public header is pinned to cdnpurge.OGImageMaxAgeSeconds and
//	      is no longer a day, because that lifetime is what let a pre-fix card
//	      outlive the fix.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// pointTheCardPurgeAt makes cdnpurge.FromEnv reach the fake CDN and accept the
// hosts these fixtures publish on: the workspace subdomains (a wildcard, because
// they are per-workspace) and the fixture's verified custom domain.
func pointTheCardPurgeAt(t *testing.T, srv *httptest.Server, extraHosts ...string) {
	t.Helper()
	t.Setenv(cdnpurge.EnvToken, "test-token")
	t.Setenv(cdnpurge.EnvZoneID, "zone-123")
	t.Setenv(cdnpurge.EnvAPIBase, srv.URL)
	hosts := "*.jomform.com"
	for _, h := range extraHosts {
		hosts += "," + h
	}
	t.Setenv(cdnpurge.EnvHosts, hosts)
}

// cardPurgeFixture is renameFixture plus the two public hosts of the form:
// <workspace>.jomform.com and the account's verified custom domain.
type cardPurgeFixture struct {
	renameFixture
	customDomain string
}

// newCardPurgeFixture builds a published form with a verified custom domain, so
// the two hosts a card can be cached on are both real and both purged.
func newCardPurgeFixture(t *testing.T) cardPurgeFixture {
	t.Helper()
	fx := newRenameFixture(t, "it-ogpurge-"+randHex(4))
	accounts := dbstore.NewAccountStore(fx.pool)
	if err := accounts.SetCustomDomain(fx.ctx, fx.accountID, fx.customDomain); err != nil {
		t.Fatalf("claim custom domain %s: %v", fx.customDomain, err)
	}
	if err := accounts.MarkCustomDomainVerified(fx.ctx, fx.accountID); err != nil {
		t.Fatalf("verify custom domain %s: %v", fx.customDomain, err)
	}
	// The priority rule is the one the public handlers use: a VERIFIED custom
	// domain outranks the workspace subdomain. Assert it here, so the prefixes
	// below are derived from the same decision the serving path makes.
	acc, err := accounts.ByID(fx.ctx, fx.accountID)
	if err != nil {
		t.Fatalf("read account: %v", err)
	}
	if acc.CustomDomain != fx.customDomain || !acc.CustomDomainVerified {
		t.Fatalf("account custom domain = %q verified=%v, want %q verified",
			acc.CustomDomain, acc.CustomDomainVerified, fx.customDomain)
	}
	return cardPurgeFixture{renameFixture: fx, customDomain: fx.customDomain}
}

// wantArtefactPrefixes is the set of edge prefixes a purge of this form must name:
// the share card AND the QR PNG, on BOTH public hosts. Two artefacts × two hosts,
// because the card is an entry per host and the QR encodes the request host too.
func (fx cardPurgeFixture) wantArtefactPrefixes() []string {
	var out []string
	for _, host := range []string{fx.customDomain, fx.host} {
		out = append(out,
			"https://"+host+"/og/"+fx.slug+".png",
			"https://"+host+"/qr/"+fx.slug+".png",
		)
	}
	return out
}

// setStatus drives the real REST status route.
func (fx cardPurgeFixture) setStatus(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms/"+fx.formID+"/status", body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return code, out
}

// storedStatus reads the status straight out of the database: a reply is not
// evidence that the row changed.
func (fx cardPurgeFixture) storedStatus(t *testing.T) string {
	t.Helper()
	var s string
	if err := fx.pool.QueryRow(fx.ctx, `SELECT status FROM forms WHERE id=$1`, fx.formID).Scan(&s); err != nil {
		t.Fatalf("read stored status: %v", err)
	}
	return s
}

// assertPrefixes compares the prefixes the CDN was asked about with the ones the
// form's artefacts can be cached under, as a SET (order is the handler's business).
func assertPrefixes(t *testing.T, got, want []string) {
	t.Helper()
	seen := make(map[string]int, len(got))
	for _, p := range got {
		seen[p]++
	}
	for _, w := range want {
		if seen[w] == 0 {
			t.Errorf("the purge never named %q — prefixes sent: %v", w, got)
		}
		delete(seen, w)
	}
	if len(seen) > 0 {
		t.Errorf("the purge named prefixes beyond the form's own artefact URLs: %v (wanted %v)", seen, want)
	}
}

func TestDraftingAFormPurgesItsCachedSocialCard(t *testing.T) {
	fx := newCardPurgeFixture(t)

	rec := &edgePurgeRecorder{}
	cdn := httptest.NewServer(rec)
	defer cdn.Close()
	pointTheCardPurgeAt(t, cdn, fx.customDomain)

	// Precondition: while published BOTH hosts serve the card — and the QR PNG —
	// which is what a crawler caches. Without this the 404 after the transition
	// could be a broken fixture passing for the wrong reason.
	for _, host := range []string{fx.host, fx.customDomain} {
		if body := ogServed(t, fx.pub, host, fx.slug); len(body) == 0 {
			t.Fatalf("precondition: published /og/%s.png served no bytes on %s", fx.slug, host)
		}
		if r := publicServeWith(t, fx.pub, host, "/qr/"+fx.slug+".png"); r.Code != http.StatusOK {
			t.Fatalf("precondition: published /qr/%s.png on %s = %d, want 200 — the QR PNG is the "+
				"second artefact this transition has to invalidate", fx.slug, host, r.Code)
		}
	}

	code, out := fx.setStatus(t, `{"status":"draft"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /status {draft} = %d (body %v), want 200", code, out)
	}
	if got := fx.storedStatus(t); got != dbstore.FormStatusDraft {
		t.Fatalf("stored status = %q, want draft", got)
	}
	if out["status"] != "draft" {
		t.Errorf("reply status = %v, want draft (body %v)", out["status"], out)
	}

	// 1. The CDN was asked, for the card's PREFIX on every public host.
	calls, method, path, auth, bodyErr, prefixes := rec.snapshot()
	if calls != 1 {
		t.Fatalf("the edge purge was requested %d times, want 1 (prefixes %v)", calls, prefixes)
	}
	if method != http.MethodPost || path != "/zones/zone-123/purge_cache" {
		t.Errorf("purge call = %s %s, want POST /zones/zone-123/purge_cache", method, path)
	}
	if auth != "Bearer test-token" {
		t.Errorf("purge Authorization = %q, want the configured token", auth)
	}
	if bodyErr != "" {
		t.Errorf("purge request body: %s", bodyErr)
	}
	assertPrefixes(t, prefixes, fx.wantArtefactPrefixes())

	// 2. The reply NAMES the outcome instead of swallowing it, and does not name a
	//    residual window that does not exist.
	if out["edge"] != cdnpurge.StatusPurged {
		t.Errorf("reply edge = %v, want %q (body %v)", out["edge"], cdnpurge.StatusPurged, out)
	}
	if _, ok := out["edge_max_age_s"]; ok {
		t.Errorf("a successful purge must not report a residual window (body %v)", out)
	}

	// 3. And the origin really stopped serving BOTH artefacts, on both hosts.
	for _, host := range []string{fx.host, fx.customDomain} {
		if r := publicServeWith(t, fx.pub, host, "/og/"+fx.slug+".png"); r.Code != http.StatusNotFound {
			t.Errorf("GET /og/%s.png on %s after the draft transition = %d, want 404",
				fx.slug, host, r.Code)
		}
		if r := publicServeWith(t, fx.pub, host, "/qr/"+fx.slug+".png"); r.Code != http.StatusNotFound {
			t.Errorf("GET /qr/%s.png on %s after the draft transition = %d, want 404 — the origin "+
				"must stop confirming the form exists at this slug", fx.slug, host, r.Code)
		}
	}
}

func TestMakingAFormPrivatePurgesItsPublicCard(t *testing.T) {
	fx := newCardPurgeFixture(t)

	rec := &edgePurgeRecorder{}
	cdn := httptest.NewServer(rec)
	defer cdn.Close()
	pointTheCardPurgeAt(t, cdn, fx.customDomain)

	code, out := fx.setStatus(t, `{"status":"private","password":"hunter2-pass"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /status {private} = %d (body %v), want 200", code, out)
	}
	if got := fx.storedStatus(t); got != dbstore.FormStatusPrivate {
		t.Fatalf("stored status = %q, want private", got)
	}
	calls, _, _, _, _, prefixes := rec.snapshot()
	if calls != 1 {
		t.Fatalf("a published->private transition did not ask the edge to drop the public card (%d calls)", calls)
	}
	assertPrefixes(t, prefixes, fx.wantArtefactPrefixes())
	if out["edge"] != cdnpurge.StatusPurged {
		t.Errorf("reply edge = %v, want %q (body %v)", out["edge"], cdnpurge.StatusPurged, out)
	}
	// The private arm is still gated: an anonymous caller gets the same 404 as an
	// unknown slug (t_8e3142f3's verified arms must not regress).
	if r := publicServeWith(t, fx.pub, fx.host, "/og/"+fx.slug+".png"); r.Code != http.StatusNotFound {
		t.Errorf("GET /og/%s.png without the unlock cookie = %d, want 404", fx.slug, r.Code)
	}
}

func TestTrashingAPublishedFormPurgesItsCachedCard(t *testing.T) {
	fx := newCardPurgeFixture(t)

	rec := &edgePurgeRecorder{}
	cdn := httptest.NewServer(rec)
	defer cdn.Close()
	pointTheCardPurgeAt(t, cdn, fx.customDomain)

	if body := ogServed(t, fx.pub, fx.host, fx.slug); len(body) == 0 {
		t.Fatalf("precondition: published /og/%s.png served no bytes", fx.slug)
	}

	code, raw := tplReq(t, fx.h, fx.key, http.MethodDelete, "/v1/rest/forms/"+fx.formID, "")
	if code != http.StatusOK {
		t.Fatalf("DELETE /v1/rest/forms/{id} = %d (body %s), want 200", code, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	calls, _, _, _, _, prefixes := rec.snapshot()
	if calls != 1 {
		t.Fatalf("trashing a PUBLISHED form did not ask the edge to drop its card (%d calls)", calls)
	}
	assertPrefixes(t, prefixes, fx.wantArtefactPrefixes())
	if out["edge"] != cdnpurge.StatusPurged {
		t.Errorf("reply edge = %v, want %q (body %s)", out["edge"], cdnpurge.StatusPurged, raw)
	}
}

func TestDraftTransitionStillSucceedsWhenTheEdgePurgeIsNotConfigured(t *testing.T) {
	fx := newCardPurgeFixture(t)
	// The supported "no CDN credential" deploy — and prod's actual state today.
	t.Setenv(cdnpurge.EnvToken, "")
	t.Setenv(cdnpurge.EnvZoneID, "")

	code, out := fx.setStatus(t, `{"status":"draft"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /status {draft} without CDN credentials = %d (body %v), want 200: "+
			"the status change has already happened and a purge cannot undo it", code, out)
	}
	if got := fx.storedStatus(t); got != dbstore.FormStatusDraft {
		t.Fatalf("stored status = %q, want draft — the request was answered but did not change the row", got)
	}
	if out["edge"] != cdnpurge.StatusNotConfigured {
		t.Errorf("reply edge = %v, want %q (body %v): an unconfigured purge must be NAMED, never implied",
			out["edge"], cdnpurge.StatusNotConfigured, out)
	}
	got, ok := out["edge_max_age_s"].(float64)
	if !ok || int(got) != cdnpurge.OGImageMaxAgeSeconds {
		t.Errorf("reply edge_max_age_s = %v, want %d — the window that now bounds how long the old card can be served (body %v)",
			out["edge_max_age_s"], cdnpurge.OGImageMaxAgeSeconds, out)
	}
}

func TestStatusChangeThatLeavesNoCachedCardAsksTheEdgeForNothing(t *testing.T) {
	fx := newCardPurgeFixture(t)

	rec := &edgePurgeRecorder{}
	cdn := httptest.NewServer(rec)
	defer cdn.Close()
	// Configured on purpose: a purge would be observable. published -> unlisted is
	// public-to-public, so nothing was invalidated and nothing may be claimed.
	pointTheCardPurgeAt(t, cdn, fx.customDomain)

	code, out := fx.setStatus(t, `{"status":"unlisted"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /status {unlisted} = %d (body %v), want 200", code, out)
	}
	if calls, _, _, _, _, prefixes := rec.snapshot(); calls != 0 {
		t.Errorf("published->unlisted asked the CDN to purge %v; the card is served in both "+
			"statuses, so nothing was invalidated", prefixes)
	}
	if _, ok := out["edge"]; ok {
		t.Errorf("reply claims an edge outcome (%v) for a transition that invalidated nothing", out)
	}
	if body := ogServed(t, fx.pub, fx.host, fx.slug); len(body) == 0 {
		t.Fatalf("precondition: an unlisted form must still serve its card")
	}
}

func TestOGImagePublicCacheControlIsBoundedNotADay(t *testing.T) {
	if got := cacheMaxAgeSeconds(ogImageCacheControl); got != cdnpurge.OGImageMaxAgeSeconds {
		t.Fatalf("ogImageCacheControl = %q (max-age %d) but cdnpurge.OGImageMaxAgeSeconds = %d — "+
			"the purge contract and the served header must be one number",
			ogImageCacheControl, got, cdnpurge.OGImageMaxAgeSeconds)
	}
	if ogImageCacheMaxAgeSeconds != cdnpurge.OGImageMaxAgeSeconds {
		t.Fatalf("ogImageCacheMaxAgeSeconds = %d, want %d",
			ogImageCacheMaxAgeSeconds, cdnpurge.OGImageMaxAgeSeconds)
	}
	if n := cacheMaxAgeSeconds(ogImageCacheControl); n >= 86400 {
		t.Fatalf("ogImageCacheControl = %q — a day-long edge lifetime for a card that names an "+
			"unpublished form is the defect this card fixes", ogImageCacheControl)
	}

	// Measured on the real route, so the constant cannot be right while the
	// handler sends something else.
	fx := newRenameFixture(t, "it-ogcc-"+randHex(4))
	rec := publicServeWith(t, fx.pub, fx.host, "/og/"+fx.slug+".png")
	if rec.Code != http.StatusOK {
		t.Fatalf("published /og/%s.png = %d, want 200", fx.slug, rec.Code)
	}
	cc := rec.Header().Get("Cache-Control")
	if got := cacheMaxAgeSeconds(cc); got != cdnpurge.OGImageMaxAgeSeconds {
		t.Fatalf("served Cache-Control = %q (max-age %d), want max-age %d",
			cc, got, cdnpurge.OGImageMaxAgeSeconds)
	}
}

// TestQRImagePublicCacheControlIsBoundedNotADay is the QR PNG's side of the same
// rule (card t_b870d44b). The PNG is the artefact that kept answering
// `cf-cache-status: HIT, age: 1566, cache-control: public, max-age=86400` for a
// DRAFT form on prod on 2026-10-31, 26 minutes after the origin started 404ing.
//
// Two things are pinned together here:
//
//  1. the served public arm's lifetime is cdnpurge.QRImageMaxAgeSeconds and is not
//     a day — the self-healing floor for a deployment that cannot purge (prod's
//     state today: JF_CF_PURGE_TOKEN / JF_CF_PURGE_ZONE_ID empty); and
//  2. that window is the SAME number the purge report calls `edge_max_age_s`
//     (cdnpurge.OGImageMaxAgeSeconds, one constant), because ONE purge covers both
//     artefacts and the reply names ONE residual window. A QR lifetime longer than
//     the reported one would make the report understate the exposure.
func TestQRImagePublicCacheControlIsBoundedNotADay(t *testing.T) {
	if cdnpurge.QRImageMaxAgeSeconds != cdnpurge.OGImageMaxAgeSeconds {
		t.Fatalf("cdnpurge.QRImageMaxAgeSeconds = %d, cdnpurge.OGImageMaxAgeSeconds = %d — one purge "+
			"invalidates both artefacts and the reply reports ONE edge_max_age_s, so the two windows "+
			"must be the same number", cdnpurge.QRImageMaxAgeSeconds, cdnpurge.OGImageMaxAgeSeconds)
	}
	if cdnpurge.QRImageMaxAgeSeconds >= 86400 {
		t.Fatalf("cdnpurge.QRImageMaxAgeSeconds = %d — a day-long edge lifetime for a PNG that "+
			"confirms an unpublished form exists is the defect this card fixes",
			cdnpurge.QRImageMaxAgeSeconds)
	}

	// Measured on the real route, so the constant cannot be right while the
	// handler sends something else.
	fx := newRenameFixture(t, "it-qrcc-"+randHex(4))
	rec := publicServeWith(t, fx.pub, fx.host, "/qr/"+fx.slug+".png")
	if rec.Code != http.StatusOK {
		t.Fatalf("published /qr/%s.png = %d, want 200 (body %s)", fx.slug, rec.Code, shortBody(rec.Body.String()))
	}
	cc := rec.Header().Get("Cache-Control")
	want := "public, max-age=" + strconv.Itoa(cdnpurge.QRImageMaxAgeSeconds)
	if cc != want {
		t.Fatalf("served /qr/%s.png Cache-Control = %q, want %q — the window the purge report names",
			fx.slug, cc, want)
	}
	if got := cacheMaxAgeSeconds(cc); got != cdnpurge.OGImageMaxAgeSeconds {
		t.Fatalf("served Cache-Control = %q (max-age %d), want the reported residual window %d",
			cc, got, cdnpurge.OGImageMaxAgeSeconds)
	}
}
