package httpapi

// The public form page refuses to serve a DRAFT form, and shows a password gate
// for a PRIVATE one (serveForm, public.go). The preview route /og/{slug}.png did
// neither: handleOGImage resolved the form through findPublicForm — which is
// deliberately status-agnostic (issue #176) because the gate belongs to the
// CALLER — and served whatever came back. So an unpublished form's TITLE, its
// workspace name and its host were rendered into a public 1200x630 PNG at a URL
// that the page itself answers with 404:
//
//	/og/tuition-class-registration.png -> 200, image/png, 59022 bytes
//	/tuition-class-registration        -> 404          (status = draft, prod)
//
// The same shape applied to a private form: its title was served to anyone who
// asked, without ever showing the password gate the page presents.
//
// The rule these tests pin is the one every other public serving path in this
// package already applies (serveForm, submitResolvedForm, embedForm):
//
//   - draft   -> 404 (never public)
//   - private -> served only to a caller holding that form's unlock cookie
//   - unlisted -> served: an unlisted form IS live at its direct URL, it is only
//     left out of the workspace listing, so its preview follows its page
//   - published -> served
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL): the status has to
// reach the handler through the real form store, because a stub would let the
// guard pass while the served card followed a status the store never wrote.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ogStatusServe drives a GET through the real public mux on host, carrying the
// cookies the caller supplies (the private-form unlock cookie, in practice).
func ogStatusServe(t *testing.T, h *PublicHandlers, host, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// shortBody clips a response body for an error message: the og:image arms of
// this test can return a 60KB PNG, and dumping that into a failure line hides
// the diagnosis instead of showing it.
func shortBody(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ogSetStatus writes the status through the store and reads it back, so a test
// never asserts against a status the database did not accept.
func ogSetStatus(t *testing.T, fx renameFixture, status string) *dbstore.Form {
	t.Helper()
	fs := dbstore.NewFormStore(fx.pool)
	if err := fs.SetStatus(fx.ctx, fx.accountID, fx.formID, status); err != nil {
		t.Fatalf("SetStatus(%s): %v", status, err)
	}
	f, err := fs.GetByID(fx.ctx, fx.accountID, fx.formID)
	if err != nil {
		t.Fatalf("read the form back after SetStatus(%s): %v", status, err)
	}
	if f.Status != status {
		t.Fatalf("stored status = %q after SetStatus(%q), want %q", f.Status, status, status)
	}
	return f
}

// TestOGImageIs404ForADraftForm is the card's repro: the page 404s a draft, so
// its preview must too. Pre-fix this route answered 200 with the form's title
// on the card.
func TestOGImageIs404ForADraftForm(t *testing.T) {
	fx := newRenameFixture(t, "it-ogdraft-"+randHex(4))

	// Precondition: while the form is published BOTH the page and its preview
	// are served. Without this the 404s below could just be a broken fixture
	// (wrong host, wrong slug) and the test would pass for the wrong reason.
	if rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug); rec.Code != http.StatusOK {
		t.Fatalf("precondition: published page /%s = %d, want 200 (body %s)", fx.slug, rec.Code, rec.Body.String())
	}
	if body := ogServed(t, fx.pub, fx.host, fx.slug); len(body) == 0 {
		t.Fatalf("precondition: published /og/%s.png served no bytes", fx.slug)
	}

	ogSetStatus(t, fx, dbstore.FormStatusDraft)

	// The page rule this route has to follow.
	if rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug); rec.Code != http.StatusNotFound {
		t.Fatalf("draft page /%s = %d, want 404 — the page rule changed, re-derive this test", fx.slug, rec.Code)
	}

	rec := ogStatusServe(t, fx.pub, fx.host, "/og/"+fx.slug+".png")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /og/%s.png for a DRAFT form = %d (%d bytes, Content-Type %q), want 404.\n"+
			"The page for this form 404s, so this URL published the form's title, workspace name "+
			"and host on a card nobody can reach the form from — an unpublished form's "+
			"identity, served publicly. Apply the same status gate the page applies "+
			"(draft -> 404, private -> requires the form's unlock cookie).",
			fx.slug, rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}

	// A draft must not be distinguishable from a slug that does not exist: the
	// 404 body is the only thing a prober sees, and "something is here" is the
	// disclosure this gate exists to stop.
	unknown := ogStatusServe(t, fx.pub, fx.host, "/og/it-nope-"+randHex(6)+".png")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown slug /og/... = %d, want 404", unknown.Code)
	}
	if rec.Body.String() != unknown.Body.String() {
		t.Errorf("a draft's /og/ response (status %d, %d bytes, starting %q) differs from an unknown "+
			"slug's (status %d, %d bytes, starting %q), so the response still tells a caller that a "+
			"form exists at that slug",
			rec.Code, rec.Body.Len(), shortBody(rec.Body.String()),
			unknown.Code, unknown.Body.Len(), shortBody(unknown.Body.String()))
	}
}

// TestOGImageIs404ForAPrivateFormUntilUnlocked: the page presents the password
// gate instead of the form, so the preview must not hand out the form's identity
// to a caller that has not unlocked it — while an unlocked caller (the buyer's
// own browser, which sends the cookie) still gets the card.
func TestOGImageIs404ForAPrivateFormUntilUnlocked(t *testing.T) {
	fx := newRenameFixture(t, "it-ogpriv-"+randHex(4))
	fx.pub.AccessKey = []byte("0123456789abcdef0123456789abcdef")
	ogSetStatus(t, fx, dbstore.FormStatusPrivate)

	// The page is NOT a 404 for a private form: it shows the gate, and it does
	// not render the form's content.
	page := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if page.Code != http.StatusOK {
		t.Fatalf("private page /%s = %d, want 200 (the password gate)", fx.slug, page.Code)
	}
	if !strings.Contains(page.Body.String(), `type="password"`) {
		t.Fatalf("private page /%s does not render the password gate — the page rule changed, re-derive this test", fx.slug)
	}
	if strings.Contains(page.Body.String(), "Hello") {
		t.Fatalf("private page /%s rendered the form's own block content before unlock", fx.slug)
	}

	rec := ogStatusServe(t, fx.pub, fx.host, "/og/"+fx.slug+".png")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /og/%s.png for a PRIVATE form without the unlock cookie = %d (%d bytes), "+
			"want 404.\nThe page shows a password gate, but this URL served the form's title "+
			"and workspace to anyone who asked — the gate is bypassed by fetching the preview.",
			fx.slug, rec.Code, rec.Body.Len())
	}

	// The cookie is scoped to ONE form (privateUnlocked compares the token's
	// form id), so a token minted for another form must not open this one.
	otherToken, err := signFormAccessToken(fx.pub.AccessKey, "it-ogpriv-someone-else")
	if err != nil {
		t.Fatalf("sign a token for another form: %v", err)
	}
	wrong := ogStatusServe(t, fx.pub, fx.host, "/og/"+fx.slug+".png",
		&http.Cookie{Name: formAccessCookie, Value: otherToken})
	if wrong.Code != http.StatusNotFound {
		t.Errorf("GET /og/%s.png with an unlock cookie minted for a DIFFERENT form = %d, want 404: "+
			"the gate must be that form's own unlock cookie, not merely some valid cookie",
			fx.slug, wrong.Code)
	}

	token, err := signFormAccessToken(fx.pub.AccessKey, fx.formID)
	if err != nil {
		t.Fatalf("sign the unlock token: %v", err)
	}
	unlocked := ogStatusServe(t, fx.pub, fx.host, "/og/"+fx.slug+".png",
		&http.Cookie{Name: formAccessCookie, Value: token})
	if unlocked.Code != http.StatusOK {
		t.Fatalf("GET /og/%s.png with this form's unlock cookie = %d, want 200 (body %s)",
			fx.slug, unlocked.Code, unlocked.Body.String())
	}
	if ct := unlocked.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("unlocked preview Content-Type = %q, want image/png", ct)
	}
	f, err := dbstore.NewFormStore(fx.pool).GetByID(fx.ctx, fx.accountID, fx.formID)
	if err != nil {
		t.Fatalf("read the form back: %v", err)
	}
	if want := ogStoredCard(t, fx.pub, fx.ctx, f, fx.host); !bytes.Equal(unlocked.Body.Bytes(), want) {
		t.Errorf("the unlocked preview is not a render of this form's stored state")
	}
	// A card that was gated behind a password must never be handed to a shared
	// cache: with "public, max-age=86400" an edge would serve those bytes to the
	// next caller, who never unlocked the form.
	if cc := unlocked.Header().Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("the unlocked preview of a PRIVATE form is marked publicly cacheable (Cache-Control %q): "+
			"a CDN would then serve the card to callers who never entered the password", cc)
	}
}

// TestOGImageServesAnUnlistedForm states WHICH side of the gate an unlisted form
// is on: it is live at its direct URL (only the workspace listing omits it), so
// its page and its preview are both served. Pin it so a future blanket
// "non-published -> 404" cannot quietly break every unlisted form's preview.
func TestOGImageServesAnUnlistedForm(t *testing.T) {
	fx := newRenameFixture(t, "it-ogunl-"+randHex(4))
	ogSetStatus(t, fx, dbstore.FormStatusUnlisted)

	if rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug); rec.Code != http.StatusOK {
		t.Fatalf("precondition: unlisted page /%s = %d, want 200 (an unlisted form is live)",
			fx.slug, rec.Code)
	}
	rec := ogStatusServe(t, fx.pub, fx.host, "/og/"+fx.slug+".png")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /og/%s.png for an UNLISTED form = %d, want 200: unlisted is live at its "+
			"direct URL, so its preview must be served", fx.slug, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("unlisted preview Content-Type = %q, want image/png", ct)
	}
}
