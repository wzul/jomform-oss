package httpapi

// The public form page refuses to serve a DRAFT form, and shows a password gate
// for a PRIVATE one (serveForm, public.go); an UNLISTED form is live at its
// direct URL. The two public QR routes did not apply that rule: handleQRStand and
// handleQRImage both resolved the form through findPublicForm — which is
// deliberately status-agnostic (issue #176) because the gate belongs to the
// CALLER — and served whatever came back.
//
// Measured 2026-10-26 on the built binary, real middleware chain, a form with
// status draft and the title "Western menu":
//
//	/{slug}             -> 404
//	/qr/{slug}/stand    -> 200, 3379 bytes, body contained "Western menu"
//	/qr/{slug}.png      -> 200,   911 bytes (the PNG encodes the form's URL only)
//
// So an unpublished form's IDENTITY was public on a printable artefact whose page
// answered 404, and a password-protected form's title was served without ever
// presenting the gate.
//
// The rule these tests pin is the one the page and every other public serving path
// already applies (serveForm, submitResolvedForm, embedForm, and the og:image
// fixed in t_a5dc2299):
//
//   - draft     -> 404, with the SAME body an unknown slug gets from the same
//     route, so neither route is an existence oracle for a slug
//   - private   -> served only to a caller holding THAT form's unlock cookie
//   - unlisted  -> served: an unlisted form IS live at its direct URL
//   - published -> served
//
// The PNG's fate is stated explicitly (TestQRCodePNGIs404ForADraftForm): it
// discloses the form's EXISTENCE, so it follows the same gate. Nothing in the
// dashboard fetches it — the settings page draws its QR client-side with the qrcode
// npm package — and the MCP generate_qr tool refuses a form that is not published
// before it hands back a /qr/ URL, so no merchant flow depends on the draft arm.
//
// The merchant is not locked out of their own print artefact: the dashboard's
// "Print stand" action opens /v1/rest/forms/{id}/qr-stand, an AUTHED route that
// serves the same document for a form the caller owns at any status
// (TestQRStandAuthedRouteServesWhatThePublicRouteRefuses). Without that test the
// public gate would be indistinguishable from "printing is broken for drafts".
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL): the status has to reach
// the handler through the real form store, because a stub would let the guard pass
// while the served stand followed a status the store never wrote.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// qrFixtureTitle is the title newRenameFixture gives its form. The tests assert
// the served stand actually carries it, so a fixture whose title changed fails
// loudly instead of passing on an empty match.
const qrFixtureTitle = "Western menu"

// qrStandPath is the public printable stand for slug, with the optional table
// folded into the encoded URL exactly as the dashboard passes it.
func qrStandPath(slug, table string) string {
	p := "/qr/" + slug + "/stand"
	if table != "" {
		p += "?table=" + table
	}
	return p
}

// restGetRaw drives one GET through the real REST mux, with an API key when one
// is given ("" = anonymous), and hands back the whole response — these tests need
// the body AND the headers, which the JSON-shaped helpers drop.
func restGetRaw(t *testing.T, h *RESTHandlers, rawKey, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if rawKey != "" {
		req.Header.Set("Authorization", "Bearer "+rawKey)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestQRStandIs404ForADraftForm is the card's repro: the page 404s a draft, so the
// printable stand that carries its title must 404 too — and must answer exactly as
// an unknown slug does, or the 404 itself tells a prober that a form exists here.
func TestQRStandIs404ForADraftForm(t *testing.T) {
	fx := newRenameFixture(t, "it-qrdraft-"+randHex(4))

	// Precondition: while the form is published BOTH the page and the stand are
	// served. Without this the 404s below could be a broken fixture (wrong host,
	// wrong slug) and the test would pass for the wrong reason.
	if rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug); rec.Code != http.StatusOK {
		t.Fatalf("precondition: published page /%s = %d, want 200 (body %s)", fx.slug, rec.Code, rec.Body.String())
	}
	published := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, ""))
	if published.Code != http.StatusOK {
		t.Fatalf("precondition: published GET /qr/%s/stand = %d, want 200 (body %s)",
			fx.slug, published.Code, published.Body.String())
	}
	if !strings.Contains(published.Body.String(), qrFixtureTitle) {
		t.Fatalf("precondition: the published stand does not carry the form's title %q — the fixture form is not the one being served",
			qrFixtureTitle)
	}

	fixtureSetStatus(t, fx, dbstore.FormStatusDraft)

	// The page rule this route has to follow.
	if rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug); rec.Code != http.StatusNotFound {
		t.Fatalf("draft page /%s = %d, want 404 — the page rule changed, re-derive this test", fx.slug, rec.Code)
	}

	rec := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /qr/%s/stand for a DRAFT form = %d (%d bytes, Content-Type %q), want 404.\n"+
			"The page for this form 404s, yet this URL served a printable page carrying the form's "+
			"title — an unpublished form's identity, published. Apply the same status gate the page "+
			"applies (draft -> 404, private -> requires the form's unlock cookie).",
			fx.slug, rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
	if strings.Contains(rec.Body.String(), qrFixtureTitle) {
		t.Errorf("the draft's stand body still contains the form's title %q (status %d, %d bytes)",
			qrFixtureTitle, rec.Code, rec.Body.Len())
	}

	// A draft must not be distinguishable from a slug that does not exist: the
	// 404 body is the only thing a prober sees.
	unknown := publicServeWith(t, fx.pub, fx.host, qrStandPath("it-nope-"+randHex(6), ""))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown slug /qr/.../stand = %d, want 404", unknown.Code)
	}
	if rec.Body.String() != unknown.Body.String() {
		t.Errorf("a draft's stand 404 (%d bytes, starting %q) differs from an unknown slug's "+
			"(%d bytes, starting %q), so the response still tells a caller that a form exists at that slug",
			rec.Body.Len(), shortBody(rec.Body.String()), unknown.Body.Len(), shortBody(unknown.Body.String()))
	}
}

// TestQRCodePNGIs404ForADraftForm states the PNG's fate EXPLICITLY, so a later
// change to the gate cannot leave it behind by accident. The PNG carries no
// merchant text — it encodes the form's public URL — but that is still the
// disclosure of "a form exists at this slug", which is the thing the gate exists
// to stop, and the 404 is identical to an unknown slug's.
func TestQRCodePNGIs404ForADraftForm(t *testing.T) {
	fx := newRenameFixture(t, "it-qrpngdraft-"+randHex(4))

	// Precondition: a published form's code is served.
	if rec := publicServeWith(t, fx.pub, fx.host, "/qr/"+fx.slug+".png"); rec.Code != http.StatusOK {
		t.Fatalf("precondition: published GET /qr/%s.png = %d, want 200", fx.slug, rec.Code)
	}

	fixtureSetStatus(t, fx, dbstore.FormStatusDraft)

	rec := publicServeWith(t, fx.pub, fx.host, "/qr/"+fx.slug+".png")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /qr/%s.png for a DRAFT form = %d (%d bytes), want 404: this route still "+
			"confirms that a form exists at a slug whose page answers 404", fx.slug, rec.Code, rec.Body.Len())
	}
	unknown := publicServeWith(t, fx.pub, fx.host, "/qr/it-nope-"+randHex(6)+".png")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown slug /qr/...png = %d, want 404", unknown.Code)
	}
	if rec.Body.String() != unknown.Body.String() {
		t.Errorf("a draft's PNG 404 (%d bytes, starting %q) differs from an unknown slug's "+
			"(%d bytes, starting %q) — the pair is an existence oracle",
			rec.Body.Len(), shortBody(rec.Body.String()), unknown.Body.Len(), shortBody(unknown.Body.String()))
	}
}

// TestQRStandIs404ForAPrivateFormUntilUnlocked: the page presents the password
// gate instead of the form, so the stand must not hand out the form's title to a
// caller that has not unlocked it — while an unlocked caller (the buyer's own
// browser, which sends the cookie) still gets it.
func TestQRStandIs404ForAPrivateFormUntilUnlocked(t *testing.T) {
	fx := newRenameFixture(t, "it-qrpriv-"+randHex(4))
	fx.pub.AccessKey = []byte("0123456789abcdef0123456789abcdef")
	fixtureSetStatus(t, fx, dbstore.FormStatusPrivate)

	// The page is NOT a 404 for a private form: it shows the gate, and it does not
	// render the form's content.
	page := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if page.Code != http.StatusOK {
		t.Fatalf("private page /%s = %d, want 200 (the password gate)", fx.slug, page.Code)
	}
	if !strings.Contains(page.Body.String(), `type="password"`) {
		t.Fatalf("private page /%s does not render the password gate — the page rule changed, re-derive this test", fx.slug)
	}

	rec := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /qr/%s/stand for a PRIVATE form without the unlock cookie = %d (%d bytes), want 404.\n"+
			"The page shows a password gate, but this URL printed the form's title to anyone who asked.",
			fx.slug, rec.Code, rec.Body.Len())
	}

	// The cookie is scoped to ONE form (privateUnlocked compares the token's form
	// id), so a token minted for another form must not open this one.
	otherToken, err := signFormAccessToken(fx.pub.AccessKey, "it-qrpriv-someone-else")
	if err != nil {
		t.Fatalf("sign a token for another form: %v", err)
	}
	wrong := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, ""),
		&http.Cookie{Name: formAccessCookie, Value: otherToken})
	if wrong.Code != http.StatusNotFound {
		t.Errorf("GET /qr/%s/stand with an unlock cookie minted for a DIFFERENT form = %d, want 404: "+
			"the gate must be that form's own unlock cookie, not merely some valid cookie", fx.slug, wrong.Code)
	}

	token, err := signFormAccessToken(fx.pub.AccessKey, fx.formID)
	if err != nil {
		t.Fatalf("sign the unlock token: %v", err)
	}
	unlocked := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, ""),
		&http.Cookie{Name: formAccessCookie, Value: token})
	if unlocked.Code != http.StatusOK {
		t.Fatalf("GET /qr/%s/stand with this form's unlock cookie = %d, want 200 (body %s)",
			fx.slug, unlocked.Code, shortBody(unlocked.Body.String()))
	}
	if !strings.Contains(unlocked.Body.String(), qrFixtureTitle) {
		t.Errorf("the unlocked stand does not carry the form's title %q", qrFixtureTitle)
	}
	// A stand that was gated behind a password must never be handed to a shared
	// cache: with a public TTL an edge would serve those bytes to the next caller,
	// who never unlocked the form.
	if cc := unlocked.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("the unlocked stand of a PRIVATE form is marked publicly cacheable (Cache-Control %q): "+
			"a CDN would then serve it to callers who never entered the password", cc)
	}

	// The PNG follows the same rule, for the same reason (existence) plus the
	// no-store obligation for the arm that is served.
	png := publicServeWith(t, fx.pub, fx.host, "/qr/"+fx.slug+".png")
	if png.Code != http.StatusNotFound {
		t.Errorf("GET /qr/%s.png for a PRIVATE form without the unlock cookie = %d, want 404", fx.slug, png.Code)
	}
	pngUnlocked := publicServeWith(t, fx.pub, fx.host, "/qr/"+fx.slug+".png",
		&http.Cookie{Name: formAccessCookie, Value: token})
	if pngUnlocked.Code != http.StatusOK {
		t.Fatalf("GET /qr/%s.png with this form's unlock cookie = %d, want 200", fx.slug, pngUnlocked.Code)
	}
	if cc := pngUnlocked.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("the unlocked PNG of a PRIVATE form is marked publicly cacheable (Cache-Control %q)", cc)
	}
}

// TestQRStandServesAnUnlistedForm states WHICH side of the gate an unlisted form is
// on: it is live at its direct URL (only the workspace listing omits it), so its
// page and its stand are both served. Pinned so a future blanket "non-published ->
// 404" cannot quietly break every unlisted form's printed stand.
func TestQRStandServesAnUnlistedForm(t *testing.T) {
	fx := newRenameFixture(t, "it-qrunl-"+randHex(4))
	fixtureSetStatus(t, fx, dbstore.FormStatusUnlisted)

	if rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug); rec.Code != http.StatusOK {
		t.Fatalf("precondition: unlisted page /%s = %d, want 200 (an unlisted form is live)", fx.slug, rec.Code)
	}
	rec := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, "8"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /qr/%s/stand for an UNLISTED form = %d, want 200: unlisted is live at its "+
			"direct URL, so its printed stand must be served", fx.slug, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), qrFixtureTitle) {
		t.Errorf("the unlisted stand does not carry the form's title %q", qrFixtureTitle)
	}
	if png := publicServeWith(t, fx.pub, fx.host, "/qr/"+fx.slug+".png"); png.Code != http.StatusOK {
		t.Errorf("GET /qr/%s.png for an UNLISTED form = %d, want 200", fx.slug, png.Code)
	}
}

// TestQRStandAuthedRouteServesWhatThePublicRouteRefuses is the other half of the
// fix: gating the public route must not take the merchant's own print artefact
// away. The dashboard's "Print stand" action opens the AUTHED route, so a DRAFT
// form's stand is still printable by the account that owns it — and only by it.
func TestQRStandAuthedRouteServesWhatThePublicRouteRefuses(t *testing.T) {
	fx := newRenameFixture(t, "it-qrmerchant-"+randHex(4))
	fixtureSetStatus(t, fx, dbstore.FormStatusDraft)

	// The public route refuses it (the card's defect, seen from the merchant's side).
	if rec := publicServeWith(t, fx.pub, fx.host, qrStandPath(fx.slug, "")); rec.Code != http.StatusNotFound {
		t.Fatalf("public GET /qr/%s/stand for a DRAFT = %d, want 404 — the gate under test is not applied",
			fx.slug, rec.Code)
	}

	// Unauthenticated: the REST layer refuses before the handler runs.
	if anon := restGetRaw(t, fx.h, "", "/v1/rest/forms/"+fx.formID+"/qr-stand?table=8"); anon.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/rest/forms/{id}/qr-stand with no credential = %d, want 401", anon.Code)
	}

	// Another account's key must not produce a stand for this form.
	other := newRenameFixture(t, "it-qrother-"+randHex(4))
	if foreign := restGetRaw(t, other.h, other.key, "/v1/rest/forms/"+fx.formID+"/qr-stand"); foreign.Code != http.StatusNotFound {
		t.Errorf("GET /v1/rest/forms/{id}/qr-stand for a form in ANOTHER account with a valid key = %d, want 404",
			foreign.Code)
	}

	// The owner: the stand is served, for a DRAFT, with the form's title on it.
	rec := restGetRaw(t, fx.h, fx.key, "/v1/rest/forms/"+fx.formID+"/qr-stand?table=8")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms/{id}/qr-stand as the owning account for a DRAFT = %d, want 200 (body %s)",
			rec.Code, shortBody(rec.Body.String()))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("the merchant's own stand Content-Type = %q, want text/html (it is opened in a browser tab)", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, qrFixtureTitle) {
		t.Errorf("the merchant's own stand for a draft does not carry the form's title %q", qrFixtureTitle)
	}
	if !strings.Contains(body, `<div class="table">Table 8</div>`) {
		t.Errorf("the merchant's own stand does not carry the TABLE 8 badge the dashboard asked for")
	}

	// The printed QR must encode the SAME public URL the dashboard shows for the
	// form — read out of the REST form payload, not reconstructed here, so the two
	// surfaces cannot drift.
	formRec := restGetRaw(t, fx.h, fx.key, "/v1/rest/forms/"+fx.formID)
	if formRec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms/{id} = %d, want 200", formRec.Code)
	}
	var form map[string]any
	if err := json.Unmarshal(formRec.Body.Bytes(), &form); err != nil {
		t.Fatalf("decode the form payload: %v", err)
	}
	formURL, _ := form["url"].(string)
	if formURL == "" {
		t.Fatalf("the form payload carries no url, so the printed stand cannot be checked against it: %s",
			shortBody(formRec.Body.String()))
	}
	assertStandQREncodes(t, body, formURL+"?table=8")

	// An authed document names an unpublished form: never handed to a shared cache.
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("the authed stand is marked publicly cacheable (Cache-Control %q)", cc)
	}
}
