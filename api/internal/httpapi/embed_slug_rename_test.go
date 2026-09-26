package httpapi

// Guard for the carded defect: /embed/f/{workspace}/{slug} was the ONE public
// slug lookup left off the rename's live-first resolver, so an iframe a merchant
// had already pasted on a form's OLD slug stopped rendering after a rename.
//
// THE BUG, in one line (embed.go, pre-fix):
//
//	f, err := h.Forms.ByWorkspaceSlug(r.Context(), accSlug, slug)
//	if err != nil { writeErr(w, http.StatusNotFound, "borang tidak dijumpai") }
//
// Every other public slug lookup resolves the live form first and a rename's
// temporary 301 second (render, submit, unlock, QR image, QR stand, OG image,
// menu order, the menu's attached order form). This route kept a single lookup,
// so it 404'd on exactly the class of URL the rename's 301 exists to keep alive:
// a merchant's embedded form, already published on his own site.
//
// Measured on a running API before the fix (published form, allow_embed=true,
// renamed emb-old -> emb-new, workspace subdomain host):
//
//	GET /embed/f/{ws}/emb-old-c1  ->  404   (the old slug: unhandled)
//	GET /embed/f/{ws}/emb-new-c1  ->  200 ALLOWALL
//
// WHAT THIS FILE PINS (the card's own acceptance criteria):
//
//  1. A renamed form's OLD embed URL serves the form (200), not 404.
//  2. It is served IN PLACE — no redirect — because this route's whole purpose
//     is to keep `frame-ancestors *` / `X-Frame-Options: ALLOWALL` on the
//     response. The plain /{slug} route answers SAMEORIGIN (measured), so a 301
//     out of the embed route would break the iframe it exists to serve. Serving
//     in place also keeps the request's query on the address the rendered page
//     reads, and the renderer echoes ?table=N / ?aff=CODE from location.search.
//  3. The route's OWN rules still apply to a resolved old slug: the 403 when
//     allow_embed is off, and the draft 404. The rename fallback is not a way
//     around either.
//  4. Wan's supersede rule holds HERE TOO: a form that later claims the freed
//     slug wins, so rename history can never shadow a live form's embed URL.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
// The fixture is the rename guard's (form_slug_rename_test.go) — real DB and the
// real mux, so this cannot pass while the resolver is absent.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// embedPath builds the route's path for a workspace (or account) slug + a form
// slug. The route carries its scope in the PATH, which is the one structural
// difference from findPublicForm (that one reads the scope off the request Host).
func embedPath(scopeSlug, formSlug string) string {
	return "/embed/f/" + scopeSlug + "/" + formSlug
}

// TestEmbedRouteResolvesARenamedFormsOldSlug is the core guard: an iframe on the
// OLD slug keeps rendering after the form is renamed.
func TestEmbedRouteResolvesARenamedFormsOldSlug(t *testing.T) {
	fx := newRenameFixture(t, "it-emb-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-emb-new-" + randHex(4)

	// The route has its own gate and the fixture's form defaults to
	// allow_embed=false, so turn framing on for this form.
	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET allow_embed=true WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("enable embedding on the fixture form: %v", err)
	}

	// CONTROL, before the rename: the LIVE slug answers on this route. Read it
	// first so a failure below cannot be "the route never worked at all".
	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug)); rec.Code != http.StatusOK {
		t.Fatalf("control: GET %s before the rename = %d, want 200 (body %s).\n"+
			"The control failing means the embed route (or its allow_embed gate) is broken independently, "+
			"so the assertion below would prove nothing.",
			embedPath(fx.workspaceSlug, oldSlug), rec.Code, rec.Body.String())
	}

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	// 1. The defect. A merchant's pasted <iframe> still points at the old slug.
	//    Carry a query too, because the embed route must not lose it (see 2).
	oldPath := embedPath(fx.workspaceSlug, oldSlug)
	rec := publicServe(t, fx.pub, fx.host, oldPath+"?table=7&aff=PROBE")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s (a renamed form's OLD embed URL) = %d, want 200 served from the renamed form (body %s).\n"+
			"A merchant pasted this URL into his own site before the rename; a 404 breaks a live embed, "+
			"which is the exact \"orphaned URL already handed out\" class the rename's 301 exists to prevent. "+
			"The plain /{slug} route, the QR image, the QR stand and the OG image all resolve the old slug — "+
			"this route must not be the one that does not.",
			oldPath, rec.Code, rec.Body.String())
	}

	// 2. Served IN PLACE, not a redirect out of the framing path. The plain
	//    route answers X-Frame-Options: SAMEORIGIN, so a 301 here would render
	//    the form unframable inside the merchant's iframe.
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("the old embed URL answered with Location=%q — it must be SERVED, not redirected: "+
			"the destination (the plain /{slug} route) sends X-Frame-Options: SAMEORIGIN, so a redirect "+
			"out of this route breaks the merchant's iframe, and it also drops ?table=N / ?aff=CODE, "+
			"which the renderer echoes from location.search.", loc)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors *" {
		t.Errorf("Content-Security-Policy on the old embed URL = %q, want %q — the resolved old slug must "+
			"keep this route's permissive framing.", got, "frame-ancestors *")
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "ALLOWALL" {
		t.Errorf("X-Frame-Options on the old embed URL = %q, want %q", got, "ALLOWALL")
	}
	if body := rec.Body.String(); !strings.Contains(body, "Hello") {
		t.Errorf("the old embed URL served a page without the form's own content — it resolved to the "+
			"wrong form (body %s)", body)
	}

	// The form's live embed URL is, of course, still the canonical one.
	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, newSlug)); rec.Code != http.StatusOK {
		t.Errorf("GET %s (the form's live embed slug) = %d, want 200", embedPath(fx.workspaceSlug, newSlug), rec.Code)
	}
}

// TestEmbedRouteOldSlugKeepsItsGate pins (3): the rename fallback must not become
// a way around this route's own rules. A form that forbids embedding is refused
// on its OLD embed URL too — 403, not a 200 the resolver accidentally unlocked.
func TestEmbedRouteOldSlugKeepsItsGate(t *testing.T) {
	fx := newRenameFixture(t, "it-embgate-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-embgate-new-" + randHex(4)

	// allow_embed is FALSE for this form (the fixture default).
	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET allow_embed=false WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("disable embedding on the fixture form: %v", err)
	}
	// Control: the LIVE slug is already refused with 403.
	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug)); rec.Code != http.StatusForbidden {
		t.Fatalf("control: GET %s with allow_embed=false = %d, want 403", embedPath(fx.workspaceSlug, oldSlug), rec.Code)
	}

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug))
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET %s (the OLD embed URL of a form with allow_embed=false) = %d, want 403.\n"+
			"The rename fallback resolves the form, and the route's gate must still be applied to it — "+
			"otherwise an old URL becomes a way to embed a form its owner refused to embed.",
			embedPath(fx.workspaceSlug, oldSlug), rec.Code)
	}
}

// TestEmbedRouteOldSlugDraftIs404 pins the other half of (3): a draft is never
// served, including through the rename fallback.
func TestEmbedRouteOldSlugDraftIs404(t *testing.T) {
	fx := newRenameFixture(t, "it-embdraft-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-embdraft-new-" + randHex(4)

	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET allow_embed=true WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("enable embedding: %v", err)
	}
	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}
	// The form is pulled back to draft AFTER the rename, so a redirect row for
	// the old slug exists while the target is no longer servable.
	if _, err := fx.pool.Exec(fx.ctx,
		`UPDATE forms SET status='draft' WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("move the form to draft: %v", err)
	}

	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug)); rec.Code != http.StatusNotFound {
		t.Errorf("GET %s (the OLD embed URL of a form since moved to draft) = %d, want 404.\n"+
			"A rename's redirect must not resurrect a form that was pulled back to draft.",
			embedPath(fx.workspaceSlug, oldSlug), rec.Code)
	}
}

// TestEmbedRouteOldSlugIsSupersededByALaterClaimant pins (4) on this route: the
// live form is asked FIRST, so a later claimant wins and rename history can never
// shadow a real, live form's embed URL.
func TestEmbedRouteOldSlugIsSupersededByALaterClaimant(t *testing.T) {
	fx := newRenameFixture(t, "it-embclaim-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-embclaim-new-" + randHex(4)

	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET allow_embed=true WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("enable embedding: %v", err)
	}
	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}
	// Precondition: the old embed URL is served by the renamed form right now.
	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug)); rec.Code != http.StatusOK {
		t.Fatalf("precondition: GET %s after the rename = %d, want 200", embedPath(fx.workspaceSlug, oldSlug), rec.Code)
	}

	// A different, published, embeddable form in the same workspace claims the
	// freed slug.
	claimant := &dbstore.Form{
		AccountID: fx.accountID, Slug: oldSlug, Title: "Claimant menu",
		Status: dbstore.FormStatusPublished, AllowEmbed: true,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Claimant body"}}},
		Theme:  map[string]any{"primary": "#000000"},
	}
	if err := dbstore.NewFormStore(fx.pool).Create(fx.ctx, claimant); err != nil {
		t.Fatalf("the freed old slug must be claimable by a later form, got: %v", err)
	}

	rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s after a later form claimed it = %d, want 200 served from the CLAIMANT (body %s).\n"+
			"rename history must never shadow a live form — the claimant wins.",
			embedPath(fx.workspaceSlug, oldSlug), rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Claimant body") {
		t.Errorf("the old embed URL did not serve the claimant's content — the rename redirect shadowed the "+
			"live form (body %s)", body)
	}
	if strings.Contains(body, "Hello") {
		t.Errorf("the old embed URL served the ORIGINAL form's content; the redirect must be superseded by "+
			"the claimant (body %s)", body)
	}
}

// TestEmbedRouteOldSlugResolvesThroughTheAccountSlugArm pins the OTHER half of
// the fallback: a rename's redirect row is keyed on the ACCOUNT namespace
// (form_slug_redirects.account_id -> accounts.slug), so the account arm of the
// rename fallback is not dead code.
//
// RE-CUT when the workspace-only serving rule landed (this guard was written
// before it): the route's path segment is a WORKSPACE slug and nothing else, so
// a LIVE account-slug lookup on this route now answers 404 — the control below
// asserts exactly that, instead of the 200 this guard used to require. The
// account arm is reached through the rename's redirect table only, which is
// what the assertion after the rename pins.
//
// WHY THIS GUARD EXISTS. The workspace-slug arm is reached by every other case
// in this file (the fixture's host and path both use fx.workspaceSlug), so the
// account arm had no case of its own. QA measured the consequence on the
// pre-fix commit (card t_385179b3): deleting ONLY RedirectedFormByAccountSlug
// left the file 4/4 green. That bite is preserved here: the post-rename
// assertion below still fails if the account arm is dropped.
func TestEmbedRouteOldSlugResolvesThroughTheAccountSlugArm(t *testing.T) {
	fx := newRenameFixture(t, "it-embacct-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-embacct-new-" + randHex(4)

	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET allow_embed=true WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("enable embedding: %v", err)
	}

	// The route is scoped by the PATH, and the account slug is a genuinely
	// different string from the workspace slug in this fixture — so resolving
	// this path cannot go through the workspace arm by accident.
	if fx.accountSlug == fx.workspaceSlug {
		t.Fatalf("fixture premise broken: account slug == workspace slug (%q), so this guard would "+
			"not distinguish the two fallback arms", fx.accountSlug)
	}
	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.workspaceSlug, oldSlug)); rec.Code != http.StatusOK {
		t.Fatalf("control: GET %s before the rename = %d, want 200 (this is the route's own scope, "+
			"so it proves the route and its allow_embed gate are live independently of the "+
			"rename fallback asserted below)",
			embedPath(fx.workspaceSlug, oldSlug), rec.Code)
	}
	// The account slug is NOT a serving scope for this route: a workspace is
	// served on its workspace slug and nothing else, and the route's segment is
	// a workspace slug. Asserted so a future refactor cannot quietly re-widen
	// the live lookup and make the account arm below meaningless.
	if rec := publicServe(t, fx.pub, fx.host, embedPath(fx.accountSlug, oldSlug)); rec.Code != http.StatusNotFound {
		t.Fatalf("control: GET %s (the account slug as this route's scope, on a form whose LIVE slug "+
			"is %s) = %d, want 404 — the route's segment names a workspace, and the account slug is "+
			"the CUSTOM-DOMAIN namespace, not a second serving scope. If this answers 200 the live "+
			"lookup has re-acquired the account fallback and the rename arm asserted below is no "+
			"longer the arm under test.",
			embedPath(fx.accountSlug, oldSlug), oldSlug, rec.Code)
	}

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	oldPath := embedPath(fx.accountSlug, oldSlug)
	rec := publicServe(t, fx.pub, fx.host, oldPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s (the account-slug form of a renamed embedded form's OLD URL) = %d, want 200. "+
			"Every other public lookup resolves a rename through the account arm; if this 404s, the "+
			"account-slug arm of embedFormBySlug has been dropped and only the workspace arm survives.",
			oldPath, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("the old embed URL answered with Location=%q — it must be SERVED, not redirected", loc)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors *" {
		t.Errorf("Content-Security-Policy on the account-slug old embed URL = %q, want %q", got, "frame-ancestors *")
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "ALLOWALL" {
		t.Errorf("X-Frame-Options on the account-slug old embed URL = %q, want %q", got, "ALLOWALL")
	}
	if body := rec.Body.String(); !strings.Contains(body, "Hello") {
		t.Errorf("the account-slug old embed URL did not serve the renamed form's own content (body %s)", body)
	}
}
