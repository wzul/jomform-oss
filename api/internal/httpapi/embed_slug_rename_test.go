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
//  5. A store read that FAILS ITS SCAN — not a miss — is never served: the
//     fallback's error check in each of its two arms must keep such a row from
//     being read as a hit. That shape is unreachable through the resolver's
//     normal miss path, so it has its own guard
//     (TestEmbedRouteOldSlugStoreScanFailureIs404) which induces a real scan
//     failure instead of asserting it.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
// The fixture is the rename guard's (form_slug_rename_test.go) — real DB and the
// real mux, so this cannot pass while the resolver is absent.

import (
	"context"
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

// TestEmbedRouteOldSlugStoreScanFailureIs404 pins the store-error check in BOTH
// arms of the rename fallback — the line the comment above the fallback calls
// load-bearing, and until now the one part of this file that no guard in it
// could make go red.
//
// WHY THIS GUARD HAS TO INDUCE THE FAILURE ITSELF. The other five guards drive a
// real Postgres in which a NORMAL miss never produces this shape:
// RedirectedFormByAccountSlug / ...WorkspaceSlug map `isNoSuchRow` to
// (nil, ErrFormNotFound) BEFORE returning, so for every row those guards can
// build, `err == nil && f != nil` and a bare `f != nil` behave identically. The
// shape the check exists for only appears when the SCAN genuinely fails, because
// scanOne (dbstore/form.go) returns its PRE-ALLOCATED struct NON-NIL together
// with the error: the discarded-error shape then hands embedFormBySlug a
// half-read row that still looks like a live, embeddable form, and the route
// serves it rather than 404.
//
// So this guard makes a column the arm's SELECT reads hold a value its Go
// destination cannot take, which fails the read the way a column-alignment slip
// fails: the SELECT succeeds, the row comes back, Scan errors. Two independent
// inducers, one per shape, so the guard is not value-specific:
//
//	timestamptz 'infinity' into *time.Time  -> "cannot scan Infinity into *time.Time"
//	jsonb '"…"' into map[string]any         -> json unmarshal error
//
// The first fails on a column that sits AFTER status and allow_embed in
// redirectFormColumns, so the half-read struct still carries published +
// allow_embed=true and the discarded-error shape SERVES it (200 with the form's
// own "Hello"); the second fails earlier, so that shape answers 403 — a refusal
// for the wrong reason, out of a row the store never returned as a hit. Either
// way the answer is not a 404 the resolver deliberately chose, which is the
// whole of the contract, and the assertion below is the same for both.
//
// The ALIGNMENT cause behind this class is guarded where it belongs, in dbstore:
// migration_scan_drift_test.go registers redirectFormColumns -> Form. This guard
// pins the resolver's own discipline for such a failure however it arrives.
func TestEmbedRouteOldSlugStoreScanFailureIs404(t *testing.T) {
	const (
		// A timestamptz value no time.Time can take. Fails on the 15th column
		// of redirectFormColumns, i.e. after status (7th) and allow_embed
		// (11th) have already been assigned into scanOne's struct.
		corruptStartAtToInfinity = `UPDATE forms SET start_at='infinity'::timestamptz WHERE id=$1`
		// A jsonb value no map[string]any can take. Fails on the 5th column,
		// i.e. before the two the route's own gate reads.
		corruptConfigToAJSONString = `UPDATE forms SET config=to_jsonb('not-an-object'::text) WHERE id=$1`
	)

	// One entry per (fallback arm × scan-failure shape). The scope slug is the
	// path segment each arm alone resolves: the workspace arm joins
	// workspaces.slug, the account arm joins accounts.slug, and the fixture
	// keeps those two strings different.
	cases := []struct {
		name    string
		arm     string
		scope   func(fx renameFixture) string
		lookup  func(*dbstore.FormStore, context.Context, string, string) (*dbstore.Form, error)
		corrupt string
		// gateIntact is whether the columns the route's gate reads (status,
		// allow_embed) are assigned before the failing one — i.e. whether the
		// half-read struct the discarded-error shape would serve is a live,
		// embeddable form. Asserted as a premise so a future reordering of
		// redirectFormColumns cannot quietly turn this guard into one that
		// pins a refusal instead of a served form.
		gateIntact bool
	}{
		{
			name: "workspace-arm/infinity-start-at", arm: "RedirectedFormByWorkspaceSlug",
			scope: func(fx renameFixture) string { return fx.workspaceSlug }, lookup: (*dbstore.FormStore).RedirectedFormByWorkspaceSlug,
			corrupt: corruptStartAtToInfinity, gateIntact: true,
		},
		{
			name: "workspace-arm/jsonb-type-mismatch", arm: "RedirectedFormByWorkspaceSlug",
			scope: func(fx renameFixture) string { return fx.workspaceSlug }, lookup: (*dbstore.FormStore).RedirectedFormByWorkspaceSlug,
			corrupt: corruptConfigToAJSONString, gateIntact: false,
		},
		{
			name: "account-arm/infinity-start-at", arm: "RedirectedFormByAccountSlug",
			scope: func(fx renameFixture) string { return fx.accountSlug }, lookup: (*dbstore.FormStore).RedirectedFormByAccountSlug,
			corrupt: corruptStartAtToInfinity, gateIntact: true,
		},
		{
			name: "account-arm/jsonb-type-mismatch", arm: "RedirectedFormByAccountSlug",
			scope: func(fx renameFixture) string { return fx.accountSlug }, lookup: (*dbstore.FormStore).RedirectedFormByAccountSlug,
			corrupt: corruptConfigToAJSONString, gateIntact: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newRenameFixture(t, "it-embscan-old-"+randHex(4))
			oldSlug := fx.slug
			newSlug := "it-embscan-new-" + randHex(4)
			scope := tc.scope(fx)
			if scope != fx.workspaceSlug && scope != fx.accountSlug {
				t.Fatalf("fixture premise broken: scope %q is neither the workspace slug %q nor the account slug %q, so "+
					"the request below would not name the arm under test", scope, fx.workspaceSlug, fx.accountSlug)
			}

			// The route's own gate, so a form resolved through the fallback is
			// one this route WOULD serve if the read had not failed.
			if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET allow_embed=true WHERE id=$1`, fx.formID); err != nil {
				t.Fatalf("enable embedding on the fixture form: %v", err)
			}
			if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
				t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
			}

			path := embedPath(scope, oldSlug)
			// CONTROL, before the row is made unscannable: this arm serves the
			// old slug. Without it a 404 below could simply mean the request
			// never reached this arm at all.
			if rec := publicServe(t, fx.pub, fx.host, path); rec.Code != http.StatusOK {
				t.Fatalf("control: GET %s (the OLD slug through the %s arm) before the row is corrupted = %d, "+
					"want 200 (body %s)", path, tc.arm, rec.Code, rec.Body.String())
			}

			if _, err := fx.pool.Exec(fx.ctx, tc.corrupt, fx.formID); err != nil {
				t.Fatalf("corrupt the row to fail the scan (%s): %v", tc.name, err)
			}

			// PREMISE: this is the shape the check exists for, measured on the
			// store itself rather than assumed. scanOne hands back a non-nil
			// struct WITH the error, and the struct is genuinely half-read
			// (slug already assigned), so `f != nil` alone would read it as a
			// hit.
			f, err := tc.lookup(dbstore.NewFormStore(fx.pool), fx.ctx, scope, oldSlug)
			if f == nil || err == nil {
				t.Fatalf("premise broken: %s(%q, %q) = (form %v, err %v), want a NON-NIL struct together with a NON-NIL error. "+
					"Nothing below can bite unless the corrupted row still fails the SCAN and still returns scanOne's pre-allocated "+
					"struct — check that %s still reads the column this case corrupts.", tc.arm, scope, oldSlug, f, err, tc.arm)
			}
			if f.Slug != newSlug {
				t.Fatalf("premise broken: the failed scan returned a form whose Slug = %q, want %q (the renamed form's live slug) — "+
					"the struct is supposed to be partially assigned up to the failing column, which is what makes it look like a hit",
					f.Slug, newSlug)
			}
			if tc.gateIntact {
				if f.Status != dbstore.FormStatusPublished || !f.AllowEmbed {
					t.Fatalf("premise broken: the half-read form carries status=%q allow_embed=%v, want %q/true — this case expects "+
						"the scan to fail AFTER the gate columns, so that discarding the error would SERVE it (200) rather than 404",
						f.Status, f.AllowEmbed, dbstore.FormStatusPublished)
				}
			} else if f.Status != "" || f.AllowEmbed {
				t.Fatalf("premise broken: the half-read form carries status=%q allow_embed=%v, want both zero — this case expects the "+
					"scan to fail BEFORE the gate columns", f.Status, f.AllowEmbed)
			}

			// THE ASSERTION: a row the store could not read is a 404, not a hit.
			// Delete the error check from either arm (`if f, _ := …; f != nil`)
			// or weaken it to `err == nil || f != nil` and this goes red: the
			// route answers 200 with the form's own body (infinity case) or 403
			// (jsonb case) out of a row no store call ever returned.
			rec := publicServe(t, fx.pub, fx.host, path)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s after %s(%q, %q) failed its SCAN = %d, want 404 (body %s).\n"+
					"The store returned its pre-allocated struct non-nil together with the error, and this route read it as a real "+
					"hit: a form whose row could not be read must be treated as not found, not served (or refused) as if it had been "+
					"read. The check in BOTH arms of the fallback is what makes the difference — a normal miss never takes this path, "+
					"so no other guard in this file can catch its removal.",
					path, tc.arm, scope, oldSlug, rec.Code, rec.Body.String())
			}
		})
	}
}
