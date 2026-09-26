package httpapi

// Guard for the /docs/embed snippet (card t_028218bf).
//
// THE DEFECT. web/src/app/docs/embed/page.tsx told merchants to iframe the
// PLAIN public form URL (https://yourworkspace.jomform.com/your-form-slug) and
// said "Framing is allowed for embedded forms". That URL answers
// X-Frame-Options: SAMEORIGIN, and a browser refuses to frame it from another
// origin — so the one src the page handed out was the one src that could not
// work. Measured, not inferred: on a page served from a third-party origin and
// read out of the CDP frame tree, the plain URL's frame resolved to
// chrome-error://chromewebdata/ while the /embed/f/ frame loaded.
//
// WHAT IS PINNED HERE, and why each half is needed:
//
//  1. The BEHAVIOUR the docs describe — /embed/f/{ws}/{slug} is framable
//     (frame-ancestors * + X-Frame-Options ALLOWALL) while the plain form route
//     is not (SAMEORIGIN). Driven through the FULL server chain, because the
//     SAMEORIGIN default comes from middleware.Secure and the embed route is
//     what overrides it; a guard on the bare mux reads neither.
//
//  2. The URL the docs actually TELL A MERCHANT TO PASTE must be the one from
//     (1). The snippet is read out of the docs page's source — the same
//     artefact a merchant copies — and every src it prints is probed for its
//     live framing. Asserting only the headers would leave the original bug
//     fully reproducible: correct behaviour, wrong instructions.
//
//  3. The docs must not claim framing is allowed for the plain URL. A future
//     edit that re-adds the old copy next to the old snippet is the exact
//     regression, and it fails here by name.
//
//  4. The embed page's SUBMIT path must resolve. The rendered embed posts to
//     location.pathname + "/order", i.e. /embed/f/{ws}/{slug}/order; with no
//     route registered there the form rendered but could never be submitted
//     (measured in a real browser: the fetch came back "404 page not found"
//     and the page sat unchanged). A snippet that renders but cannot submit is
//     not a working snippet, so it is asserted here rather than left implicit.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset,
// like the rest of the integration suite.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// readFileString reads a repo file this package's guards inspect, failing the
// test with the path when it cannot (never silently skipping).
func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// publicServer builds the FULL server chain around a PublicHandlers, exactly as
// production serves it. The bare mux is not enough for this guard: the
// X-Frame-Options headers on a form page come from middleware.Secure (the
// conservative SAMEORIGIN default) with the embed route overriding them, so a
// guard that drives only the mux cannot see the header this whole card is
// about — it would read an empty value and prove nothing either way.
func publicServer(h *PublicHandlers) http.Handler {
	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{Public: h})
	return srv.Handler()
}

// publicGet drives a GET through the full chain with the given Host.
func publicGet(t *testing.T, hnd http.Handler, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	return rec
}

// publicPostJSON drives a POST through the full chain with the given Host, the
// way a buyer's browser reaches the API for a form on that host.
func publicPostJSON(t *testing.T, hnd http.Handler, host, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Host = host
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	return rec
}

// docsEmbedPagePath locates web/src/app/docs/embed/page.tsx relative to this
// test file (api/internal/httpapi -> ../../../web/src/app/docs/embed/page.tsx).
func docsEmbedPagePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the docs page")
	}
	p := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..",
		"web", "src", "app", "docs", "embed", "page.tsx"))
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("stat docs embed page: %v", err)
	}
	return p
}

// docsSnippetSrcs returns every iframe src the docs page prints, in source
// order. The snippets are literal template strings in that file, so a merchant
// can copy them and this guard can read the same concrete address.
func docsSnippetSrcs(t *testing.T, page string) []string {
	t.Helper()
	re := regexp.MustCompile(`<iframe\s+src="([^"]+)"`)
	ms := re.FindAllStringSubmatch(page, -1)
	if len(ms) == 0 {
		t.Fatal("no <iframe src=...> found in the docs embed page — the guard cannot see " +
			"the snippet it exists to check (did the snippet move?)")
	}
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m[1])
	}
	return out
}

// embedFixture publishes a form with allow_embed=true in its own workspace and
// returns the public handlers plus the workspace and form slugs.
func embedFixture(t *testing.T) (*PublicHandlers, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-embed-' || md5(random()::text), 'it-emb-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-embed-acc-' || md5(random()::text), 'it-embacc-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	formSlug := "it-emb-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "IT embed form",
		Status: dbstore.FormStatusPublished, AllowEmbed: true,
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "bname", "type": "name", "value": "Name", "required": true},
			map[string]any{"id": "bemail", "type": "email", "value": "Email", "required": true},
		}},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:       forms,
		Accounts:    dbstore.NewAccountStore(pool),
		Domains:     dbstore.NewDomainStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug
}

// docsProbePath maps a documented URL to the public path it resolves to, with
// the docs' placeholder slugs swapped for the fixture's real ones so the probe
// reaches a form that exists (otherwise every probe would read a 404 and prove
// nothing about framing). Returns "" when the value is not a URL this guard can
// probe at all.
func docsProbePath(t *testing.T, raw, ws, slug string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if re := regexp.MustCompile(`^/embed/f/[^/]+/[^/]+`); re.MatchString(u.Path) {
		return re.ReplaceAllString(u.Path, "/embed/f/"+ws+"/"+slug)
	}
	return "/" + slug
}

// TestDocsEmbedSnippetIsTheFramableURL is the card's headline guard: the src the
// docs hand a merchant must be the route that actually permits framing, and the
// plain form route (which the docs used to print) must still refuse it.
func TestDocsEmbedSnippetIsTheFramableURL(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed docs framing guard")
	}
	h, ws, slug := embedFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"

	embedPath := "/embed/f/" + ws + "/" + slug
	plainPath := "/" + slug

	embedRec := publicGet(t, hnd, host, embedPath)
	plainRec := publicGet(t, hnd, host, plainPath)

	// The fixture must actually reproduce the conditions the card is about:
	// both routes serve the form, so the difference really is the framing.
	if embedRec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (fixture is not exercising the embed route)", embedPath, embedRec.Code)
	}
	if plainRec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (the plain-route control must serve too)", plainPath, plainRec.Code)
	}

	// 1. The behaviour the docs describe.
	embedCSP := embedRec.Header().Get("Content-Security-Policy")
	embedXFO := embedRec.Header().Get("X-Frame-Options")
	plainXFO := plainRec.Header().Get("X-Frame-Options")

	if !strings.Contains(embedCSP, "frame-ancestors *") {
		t.Errorf("embed route CSP = %q, want it to contain 'frame-ancestors *' — the docs promise this route is framable",
			embedCSP)
	}
	if embedXFO != "ALLOWALL" {
		t.Errorf("embed route X-Frame-Options = %q, want ALLOWALL", embedXFO)
	}
	if plainXFO != "SAMEORIGIN" {
		t.Errorf("plain form route X-Frame-Options = %q, want SAMEORIGIN — if this changed, the "+
			"docs' same-origin note is now wrong too (see the /docs/embed page)", plainXFO)
	}

	// 2. The instructions must match the behaviour: every src the docs print is
	// resolved to its public path and its REAL framing is read back.
	page := readFileString(t, docsEmbedPagePath(t))
	srcs := docsSnippetSrcs(t, page)
	if len(srcs) < 2 {
		t.Fatalf("only %d iframe src(s) found in the docs page; expected the embed snippet and "+
			"the plain-URL counter-example — this guard would be vacuous", len(srcs))
	}

	var framableSeen bool
	for _, src := range srcs {
		probePath := docsProbePath(t, src, ws, slug)
		if probePath == "" {
			t.Errorf("the docs print <iframe src=%q>, which is not a URL this guard can probe — "+
				"the snippet must be a concrete address a merchant can paste", src)
			continue
		}
		isEmbedURL := strings.Contains(src, "/embed/f/")
		if isEmbedURL {
			framableSeen = true
		}
		rec := publicGet(t, hnd, host, probePath)
		framable := rec.Header().Get("X-Frame-Options") == "ALLOWALL" &&
			strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors *")

		if isEmbedURL {
			if rec.Code != http.StatusOK {
				t.Errorf("the docs tell merchants to paste %q; probing %q -> %d (want 200) — the "+
					"documented embed URL does not serve a form", src, probePath, rec.Code)
				continue
			}
			if !framable {
				t.Errorf("the docs tell merchants to paste %q; %q answers X-Frame-Options %q / CSP %q, "+
					"which a browser refuses to frame cross-origin. This is exactly the bug the card "+
					"fixes: a snippet that renders nowhere", src, probePath,
					rec.Header().Get("X-Frame-Options"), rec.Header().Get("Content-Security-Policy"))
			}
		} else if framable {
			t.Errorf("the docs print %q as a NON-embed URL, but %q IS framable — the docs' "+
				"same-origin warning would now be wrong", src, probePath)
		}
	}
	if !framableSeen {
		t.Error("the docs page prints no /embed/f/ src at all — the snippet is back to the " +
			"plain public URL, which cannot be framed cross-origin")
	}
}

// TestDocsEmbedPageDoesNotClaimThePlainURLIsFramable pins the copy, not the
// code. The original bug was a sentence as much as a snippet: the page said
// "Framing is allowed for embedded forms" directly above an iframe of the
// plain URL. Re-adding that pairing is the regression, so it is named here.
func TestDocsEmbedPageDoesNotClaimThePlainURLIsFramable(t *testing.T) {
	page := readFileString(t, docsEmbedPagePath(t))

	// The blanket claim must be gone: framing is NOT allowed for the plain URL.
	if strings.Contains(page, "Framing is allowed for embedded forms") {
		t.Error("the docs still say 'Framing is allowed for embedded forms' with no " +
			"qualification — the plain public URL answers X-Frame-Options: SAMEORIGIN, " +
			"so that sentence is false for the URL it used to sit above")
	}
	// And the limitation must be stated, not merely deleted.
	if !strings.Contains(page, "X-Frame-Options") {
		t.Error("the docs never mention X-Frame-Options — the page must state plainly that " +
			"the plain form URL is same-origin only, or a merchant has no way to know " +
			"why their iframe is empty")
	}
	if !strings.Contains(page, "/embed/f/") {
		t.Error("the docs never name the /embed/f/ route — a merchant cannot construct the " +
			"framable URL without it")
	}
}

// TestEmbeddedFormCanBeSubmitted is the other half of the card's acceptance:
// the documented snippet must be one a merchant can paste, which means the page
// it renders must be able to SUBMIT. The embed page posts to its own path plus
// "/order"; if no route is registered there the form renders and then 404s on
// submit.
func TestEmbeddedFormCanBeSubmitted(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed submit guard")
	}
	h, ws, slug := embedFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"

	// The address the rendered page posts to, derived the way the page derives
	// it: location.pathname + "/order". Asserted by REQUEST rather than by
	// reading the mux, so a handler that is registered but broken still fails.
	orderPath := "/embed/f/" + ws + "/" + slug + "/order"
	rec := publicPostJSON(t, hnd, host, orderPath, map[string]any{
		"client": map[string]any{"full_name": "It Buyer", "email": "it-buyer@example.test"},
		"fields": map[string]any{"bname": "It Buyer", "bemail": "it-buyer@example.test"},
	})

	if rec.Code == http.StatusNotFound {
		t.Fatalf("POST %s -> 404. The embed page posts here (location.pathname + '/order'), "+
			"so the documented snippet renders a form whose submit button cannot work. "+
			"body=%s", orderPath, rec.Body.String())
	}
	// A response-mode form answers 201 Created (the same status the canonical
	// /{slug}/order route returns).
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s -> %d, want 201 (body %s)", orderPath, rec.Code, rec.Body.String())
	}

	// The submission must actually have LANDED, not merely answered 201.
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.ID == "" {
		t.Fatalf("embed submit answered 201 with no submission id (body %s)", rec.Body.String())
	}
	sub, err := h.Submissions.ByID(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("fetch the submission the embed route reported (%s): %v — a 201 that "+
			"stored nothing is not a working submit", resp.ID, err)
	}
	if sub.Data["bname"] != "It Buyer" {
		t.Errorf("stored submission data[bname] = %v, want 'It Buyer' — the route answered but "+
			"did not carry the buyer's values", sub.Data["bname"])
	}
}

// TestEmbedRouteRendersTheSameFormAsTheCanonicalRoute pins the fix for the
// render-dispatch drift. The embed route used to carry its OWN copy of the
// render dispatch (renderBlocks, then serveForm), which diverged from the
// canonical /{slug} path in three ways at once — and every one of them was
// invisible on a payment form with builder blocks, which is why they survived:
//
//	response-only form  -> rendered with the PAYMENT layout ("Proceed to
//	                       Payment", a total, and a "select at least one
//	                       product" dead end) instead of its collect-only layout
//	private form        -> rendered its FIELDS with no password gate
//	scheduled form      -> rendered as open, ignoring the start window
//
// One assertion covers the class: for each form, the embed body must be what the
// form's own URL returns. That is the property a merchant is promised (the
// snippet renders YOUR form), and it cannot be satisfied by patching one symptom.
func TestEmbedRouteRendersTheSameFormAsTheCanonicalRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed render-parity guard")
	}
	h, ws, slug := embedFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"
	ctx := context.Background()
	pool := bundleTestPool(t)

	// A response-only form is the mode the drift broke worst: its layout is
	// chosen by mode, and the old embed path never consulted it.
	bodyOf := func(path string) string {
		rec := publicGet(t, hnd, host, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s -> %d (want 200)", path, rec.Code)
		}
		return rec.Body.String()
	}

	// ── response mode ──────────────────────────────────────────────────────
	canonical := bodyOf("/" + slug)
	embedded := bodyOf("/embed/f/" + ws + "/" + slug)
	if !strings.Contains(embedded, "Submit") {
		t.Errorf("the embed of a RESPONSE form does not offer a Submit control — it is being " +
			"rendered as a payment form (the mode-specific layout was skipped)")
	}
	if strings.Contains(embedded, "Proceed to Payment") {
		t.Error("the embed of a RESPONSE form says 'Proceed to Payment' — a response form takes " +
			"no money, so that button is a dead end for the buyer")
	}
	// The form's own field must survive the embed path (the payment layout
	// renames it to full_name/email).
	for _, want := range []string{`name="bname"`, `name="bemail"`} {
		if !strings.Contains(embedded, want) {
			t.Errorf("the embed of a response form is missing its own field %s — the payment "+
				"layout replaced the form's fields", want)
		}
	}
	if strings.Contains(canonical, "Proceed to Payment") {
		t.Fatal("fixture check: the canonical route also renders a payment form, so this test " +
			"cannot tell the layouts apart")
	}

	// ── private mode: the gate must be shown, not the fields ───────────────
	if _, err := pool.Exec(ctx,
		`UPDATE forms SET status = 'private' WHERE slug = $1`, slug); err != nil {
		t.Fatalf("make the form private: %v", err)
	}
	privEmbed := bodyOf("/embed/f/" + ws + "/" + slug)
	if strings.Contains(privEmbed, `name="bname"`) {
		t.Error("the embed of a PRIVATE form renders its fields without the password gate — " +
			"the embed URL must not be a way around the gate")
	}
	if !strings.Contains(privEmbed, "password") && !strings.Contains(privEmbed, "kata laluan") {
		t.Error("the embed of a PRIVATE form does not show the password gate at all")
	}

	// ── scheduled: the pending state must be shown, not the open form ──────
	if _, err := pool.Exec(ctx,
		`UPDATE forms SET status = 'published', start_at = now() + interval '30 days' WHERE slug = $1`,
		slug); err != nil {
		t.Fatalf("schedule the form: %v", err)
	}
	schedEmbed := bodyOf("/embed/f/" + ws + "/" + slug)
	if strings.Contains(schedEmbed, `name="bname"`) {
		t.Error("the embed of a form that has NOT OPENED YET renders the live form — the " +
			"schedule restriction was skipped on the embed path")
	}
}

// TestEmbedFramingKeepsTheRestOfTheCSP pins the header mechanism, because the
// obvious implementation is silently wrong: setting frame-ancestors * before
// rendering loses to any renderer that sets a stricter policy of its own (the
// custom-HTML response form pins frame-ancestors 'self' + SAMEORIGIN), and the
// merchant's iframe is then refused — the exact failure this route exists to
// prevent. So the framing is forced at write time, and the renderer's OTHER
// directives must survive untouched.
func TestEmbedFramingKeepsTheRestOfTheCSP(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed framing guard")
	}
	h, ws, slug := embedFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"
	ctx := context.Background()

	// A custom-HTML response form: the renderer pins its own strict CSP.
	custom := map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"enabled": true,
			"html":    `<form><label>Custom field <input name="cf" required></label></form>`,
			"fields": []any{
				map[string]any{"field_id": "cf", "type": "text", "required": true},
			},
		},
	}
	raw, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if _, err := bundleTestPool(t).Exec(ctx,
		`UPDATE forms SET config = $2::jsonb WHERE slug = $1`, slug, string(raw)); err != nil {
		t.Fatalf("set custom-html config: %v", err)
	}

	rec := publicGet(t, hnd, host, "/embed/f/"+ws+"/"+slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET embed custom form -> %d, want 200", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	xfo := rec.Header().Get("X-Frame-Options")

	if xfo != "ALLOWALL" {
		t.Errorf("X-Frame-Options = %q, want ALLOWALL — the renderer's own SAMEORIGIN must not "+
			"win over this route's framing", xfo)
	}
	if !strings.Contains(csp, "frame-ancestors *") {
		t.Errorf("CSP = %q, want frame-ancestors * — the custom-HTML renderer sets "+
			"frame-ancestors 'self' and the embed must override exactly that directive", csp)
	}
	if strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("CSP = %q still carries frame-ancestors 'self' — the embed route must replace "+
			"that directive, not leave both", csp)
	}
	// The renderer's own protections are not this route's to drop.
	for _, want := range []string{"default-src 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP = %q dropped %q from the renderer's policy — forcing framing must "+
				"touch ONLY frame-ancestors", csp, want)
		}
	}
}

// TestEmbedSubmitRefusesADisallowedEmbed asserts the submit route honours the
// same allow_embed rule its GET does. Without this, switching embedding off
// would hide the page while leaving a live endpoint that creates sales.
func TestEmbedSubmitRefusesADisallowedEmbed(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed submit refusal guard")
	}
	h, ws, slug := embedFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"
	ctx := context.Background()

	if _, err := bundleTestPool(t).Exec(ctx,
		`UPDATE forms SET allow_embed = false WHERE slug = $1`, slug); err != nil {
		t.Fatalf("disable embedding: %v", err)
	}

	orderPath := "/embed/f/" + ws + "/" + slug + "/order"
	rec := publicPostJSON(t, hnd, host, orderPath, map[string]any{
		"client": map[string]any{"email": "it-buyer@example.test"},
		"fields": map[string]any{"bname": "X", "bemail": "it-buyer@example.test"},
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST %s with allow_embed=false -> %d, want 403: an embed that is switched "+
			"off must not accept submissions either", orderPath, rec.Code)
	}

	// And the GET must refuse the same form, so the two agree.
	if rec := publicGet(t, hnd, host, "/embed/f/"+ws+"/"+slug); rec.Code != http.StatusForbidden {
		t.Errorf("GET /embed/f/%s/%s with allow_embed=false -> %d, want 403 (the GET and the "+
			"POST must apply the same rule)", ws, slug, rec.Code)
	}
}

// ─── private-form EMBED: the unlock round-trip ───────────────────────────────
//
// Found by QA while verifying #722: a PRIVATE form's embed showed the password
// gate but the unlock could never succeed, so an embedded private form was
// unusable. Two independent defects on the path #722 opened, and neither was
// covered — the guard above stops at "the gate is shown"
// (embed URL must not be a way around the gate) and never round-trips the
// unlock, which is why the gap survived that card.
//
// WHAT THIS GUARD ADDS, and why each half is not optional:
//
//  1. THE ROUND-TRIP ITSELF. POST the password to the EMBED-scoped unlock and
//     then GET the embed carrying the cookie it returned, asserting the FIELDS
//     render (not the gate). Every defect in this class — a missing route, a
//     gate posting to the wrong scope, a cookie the frame cannot use — is
//     invisible to a guard that never completes the exchange.
//
//  2. THE ROUTE EXISTS. Before the fix there was no
//     POST /embed/f/{wsslug}/{slug}/unlock at all ("404 page not found"),
//     so the gate's post could not resolve. Asserted by REQUEST, not by reading
//     the mux, so a route registered but broken still fails.
//
//  3. THE COOKIE IS USABLE FROM A CROSS-SITE FRAME. The gate lives on the
//     MERCHANT's origin, so its request is cross-site and comes from a document
//     whose "site for cookies" is the empty string
//     (draft-ietf-httpbis-cookie-same-site §2.1.1). A SameSite=Lax cookie is
//     neither stored from such a response nor sent on such a request, and the
//     frame would stay locked forever even with the route in place — so the
//     cookie attributes are asserted directly, which is what makes this guard
//     bite on the cookie half too. Partitioned (CHIPS) is required because a
//     plain SameSite=None third-party cookie is what browsers are restricting;
//     Secure is required for both to be honoured at all.
//
//  4. THE FIELD LEAK IS STILL CLOSED. The gate must not be a way around the
//     password, so the pre-unlock body must carry no field and the post-unlock
//     body must. Kept from the existing guard: the fix must not trade one
//     defect for the other.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset,
// like the rest of the integration suite.

// gateFixturePassword is the access password the embed round-trip unlocks with.
const gateFixturePassword = "it-embed-secret-123"

// embedPrivateFixture builds the embedFixture, sets an argon2id access password
// on the form, flips it to private, and wires the AccessKey the unlock cookie is
// signed with. Returns the handlers plus the workspace and form slugs.
func embedPrivateFixture(t *testing.T) (*PublicHandlers, string, string) {
	t.Helper()
	h, ws, slug := embedFixture(t)
	ctx := context.Background()
	pool := bundleTestPool(t)

	var formID, accountID string
	if err := pool.QueryRow(ctx,
		`SELECT id, account_id FROM forms WHERE slug = $1 ORDER BY created_at DESC LIMIT 1`,
		slug).Scan(&formID, &accountID); err != nil {
		t.Fatalf("lookup form id for %s: %v", slug, err)
	}
	hash, err := auth.HashPassword(gateFixturePassword)
	if err != nil {
		t.Fatalf("hash the access password: %v", err)
	}
	if err := h.Forms.SetAccessPassword(ctx, accountID, formID, hash); err != nil {
		t.Fatalf("set access password: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE forms SET status = 'private' WHERE id = $1 AND account_id = $2`,
		formID, accountID); err != nil {
		t.Fatalf("make the form private: %v", err)
	}
	// The signed unlock cookie needs a 32-byte key; without it the unlock is
	// refused (403 "akses tidak tersedia") and this guard would prove nothing.
	h.AccessKey = []byte("0123456789abcdef0123456789abcdef")
	return h, ws, slug
}

// embedGetWithCookies drives a GET through the FULL server chain carrying the
// cookies a previous response set — the way the embedded frame re-requests its
// own document after unlocking.
func embedGetWithCookies(t *testing.T, hnd http.Handler, host, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	return rec
}

// TestEmbeddedPrivateFormUnlockRoundTrip is the card's headline guard: an
// embedded private form must be UNLOCKABLE — post the access password to the
// embed-scoped unlock, then re-request the embed with the cookie it returned and
// see the form's fields instead of the gate.
func TestEmbeddedPrivateFormUnlockRoundTrip(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping private-form embed unlock guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)
	// The cookie HALF of this guard is a PRODUCTION property: an embedded form
	// is served over https on the merchant's site, so `Secure` is set and the
	// cross-site attributes are emitted. Dev deliberately differs — a dev host
	// is same-site with itself, so the gate's own fetch needs no relaxation and
	// the cookie stays Lax. Pin prod semantics (the helper the package already
	// uses for exactly this, oauth_test.go) so the assertion is about the shape
	// a real embed sees rather than about whichever mode leaked in.
	setProdMode(t)
	host := ws + ".jomform.com"

	embedPath := "/embed/f/" + ws + "/" + slug
	unlockPath := embedPath + "/unlock"

	// ── the fixture really is the case the card is about ──────────────────
	// A private form's embed shows the gate and NOT the fields. If this fixture
	// served fields here the guard could pass without unlocking anything.
	locked := publicGet(t, hnd, host, embedPath)
	if locked.Code != http.StatusOK {
		t.Fatalf("GET %s -> %d, want 200 (fixture is not exercising the embed route)", embedPath, locked.Code)
	}
	lockedBody := locked.Body.String()
	if !strings.Contains(lockedBody, `id="pw"`) {
		t.Fatalf("the embed of a PRIVATE form does not show the password gate at all; body: %.400s", lockedBody)
	}
	if strings.Contains(lockedBody, `name="bname"`) {
		t.Fatalf("the embed of a PRIVATE form renders its fields before unlocking — the embed "+
			"URL is a way around the gate. body: %.400s", lockedBody)
	}

	// ── 1+2: the embed-scoped unlock route exists and accepts the password ──
	wrong := publicPostJSON(t, hnd, host, unlockPath, map[string]any{"password": "not-the-password"})
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("POST %s with a WRONG password -> %d, want 403 — a gate that accepts any "+
			"password is not a gate. body=%s", unlockPath, wrong.Code, wrong.Body.String())
	}

	ok := publicPostJSON(t, hnd, host, unlockPath, map[string]any{"password": gateFixturePassword})
	if ok.Code == http.StatusNotFound {
		t.Fatalf("POST %s -> 404: there is no embed-scoped unlock route, so the gate rendered "+
			"inside the embed can never be satisfied. This is the defect the card reports "+
			"(the gate hardcoded the canonical host-scoped /{slug}/unlock instead). body=%s",
			unlockPath, ok.Body.String())
	}
	if ok.Code != http.StatusOK {
		t.Fatalf("POST %s with the CORRECT password -> %d, want 200 (body %s) — the embed's own "+
			"unlock must resolve the form by its {wsslug} PATH segment, because an embed runs on "+
			"the merchant's origin and its Host never names the workspace",
			unlockPath, ok.Code, ok.Body.String())
	}
	cookies := ok.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("the embed unlock answered 200 with no cookie — the frame has nothing to "+
			"present on its next request, so the gate would re-render forever. body=%s", ok.Body.String())
	}

	// ── 3: the cookie is one a CROSS-SITE frame can actually use ───────────
	//
	// This is the half that makes the round-trip work for a real embed, and the
	// half a SameSite=Lax cookie silently fails: the gate's request comes from a
	// frame on the merchant's site, so the browser neither stores a Lax cookie
	// from that response nor sends it on the next request. Measured in this
	// browser with a real cross-site iframe: a Lax cookie set by the frame's
	// fetch was not stored at all, while a SameSite=None control was stored AND
	// sent.
	var access *http.Cookie
	for _, c := range cookies {
		if c.Name == formAccessCookie {
			access = c
		}
	}
	if access == nil {
		t.Fatalf("the unlock set no %s cookie (got %v) — that cookie is the whole unlock",
			formAccessCookie, cookies)
	}
	// SameSite is not serialised onto the parsed cookie, so assert on the header
	// the browser actually reads.
	setCookie := strings.Join(ok.Result().Header.Values("Set-Cookie"), "\n")
	if !strings.Contains(setCookie, "SameSite=None") {
		t.Errorf("the unlock cookie is not SameSite=None (Set-Cookie: %q). A private form can be "+
			"EMBEDDED on the merchant's site, where the gate's request is cross-site from a frame "+
			"whose \"site for cookies\" is empty (draft-ietf-httpbis-cookie-same-site §2.1.1) — a "+
			"Lax cookie is neither stored from that response nor sent on that request, so the "+
			"round-trip below only passes here because this test carries the cookie by hand",
			setCookie)
	}
	if !strings.Contains(setCookie, "Partitioned") {
		t.Errorf("the unlock cookie is not Partitioned (Set-Cookie: %q). A bare SameSite=None "+
			"cookie is exactly the third-party cookie browsers are restricting; CHIPS "+
			"(Partitioned) is what an embedded widget must use for its own state, and it also "+
			"scopes the unlock to the one merchant page it was entered on", setCookie)
	}
	if !strings.Contains(setCookie, "Secure") {
		t.Errorf("the unlock cookie is not Secure (Set-Cookie: %q). SameSite=None without Secure "+
			"is refused by the browsers that enforce the pairing, and a Partitioned cookie MUST "+
			"carry Secure, so the cookie would be silently discarded", setCookie)
	}
	if !access.HttpOnly {
		t.Errorf("the unlock cookie is not HttpOnly (Set-Cookie: %q) — the unlock token must not "+
			"be readable by script", setCookie)
	}

	// ── 1+4: with the cookie, the embed serves the FIELDS, not the gate ─────
	unlocked := embedGetWithCookies(t, hnd, host, embedPath, cookies)
	if unlocked.Code != http.StatusOK {
		t.Fatalf("GET %s after unlocking -> %d, want 200 (body %s)", embedPath, unlocked.Code, unlocked.Body.String())
	}
	body := unlocked.Body.String()
	if strings.Contains(body, `id="pw"`) {
		t.Fatalf("the embed STILL shows the password gate after a successful unlock — the "+
			"round-trip does not complete, so an embedded private form is unusable. body: %.400s", body)
	}
	for _, want := range []string{`name="bname"`, `name="bemail"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the embed after unlocking is missing the form's own field %s — the unlock "+
				"returned a cookie the form does not accept. body: %.400s", want, body)
		}
	}
}

// TestEmbedUnlockRefusesADisallowedEmbed asserts the embed unlock honours the
// same allow_embed rule its GET does, exactly as the submit route must. Without
// this, switching embedding off would hide the page while leaving a live
// password-checking endpoint behind the 403.
func TestEmbedUnlockRefusesADisallowedEmbed(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed unlock refusal guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"

	if _, err := bundleTestPool(t).Exec(context.Background(),
		`UPDATE forms SET allow_embed = false WHERE slug = $1`, slug); err != nil {
		t.Fatalf("disable embedding: %v", err)
	}

	unlockPath := "/embed/f/" + ws + "/" + slug + "/unlock"
	rec := publicPostJSON(t, hnd, host, unlockPath, map[string]any{"password": gateFixturePassword})
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST %s with allow_embed=false -> %d, want 403: an embed that is switched off "+
			"must not accept unlocks either", unlockPath, rec.Code)
	}
	if rec := publicGet(t, hnd, host, "/embed/f/"+ws+"/"+slug); rec.Code != http.StatusForbidden {
		t.Errorf("GET /embed/f/%s/%s with allow_embed=false -> %d, want 403 (the GET and the "+
			"unlock must apply the same rule)", ws, slug, rec.Code)
	}
}

// TestEmbedUnlockDoesNotUnlockTheCanonicalRoute asserts the two unlock scopes
// stay INDEPENDENT in the direction that matters for security: the embed route
// resolves the form by path, so it must not become a way to reach a form the
// request's own host does not serve, and the cookie it returns must not unlock a
// different form.
//
// The unlock cookie is bound to the form id (signFormAccessToken), so this also
// pins that an embed unlock cannot be replayed against another private form.
func TestEmbedUnlockDoesNotUnlockTheCanonicalRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping embed unlock scope guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)

	// The embed route resolves by PATH, so it must answer on a Host that names
	// no workspace at all — that is the whole reason it exists (an embed runs on
	// the merchant's domain).
	unlockPath := "/embed/f/" + ws + "/" + slug + "/unlock"
	rec := publicPostJSON(t, hnd, "merchant.example.com", unlockPath, map[string]any{"password": gateFixturePassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s on the merchant's own Host -> %d, want 200: the embed scope is the "+
			"{wsslug} PATH segment precisely because the Host never names the workspace. body=%s",
			unlockPath, rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()

	// The cookie is bound to THIS form: the canonical route on a host that
	// serves no such form must still 404 rather than accept it.
	if rec := publicGet(t, hnd, "merchant.example.com", "/"+slug); rec.Code == http.StatusOK {
		t.Errorf("GET /%s on a Host that names no workspace -> 200; a public payment link must "+
			"not be reachable on the bare/foreign Host", slug)
	}
	// And the unlock cookie does not unlock a form it was not minted for.
	if rec := embedGetWithCookies(t, hnd, "merchant.example.com", unlockPath, cookies); rec.Code != http.StatusMethodNotAllowed {
		// A GET on a POST-only route is 405; anything else means the mux matched
		// something this guard does not know about.
		t.Logf("note: GET %s -> %d (expected 405 from the POST-only route)", unlockPath, rec.Code)
	}
}

// TestCanonicalUnlockOnAnUnknownSlugIs404NotA500 pins the join between the two
// halves this branch had to reconcile: the canonical unlock resolves through
// findPublicForm (the rename's live-first resolver, which returns nil on a miss)
// and then hands the result to unlockResolvedForm (the shared impl, which reads
// f.Status on its first line and does NOT tolerate nil).
//
// The failure mode this guards is the shape a merge can produce and a unit test
// of either half alone cannot see: feed the nil straight in and the dereference
// panics, the recoverer turns it into a 500, and a bogus unlock — which must be
// a clean 404 — is reported as a server error. Measured directly: the assertion
// is on the STATUS and on the recoverer's own log line, so "the guard happens to
// sit somewhere else on the path" cannot make it pass for the wrong reason.
func TestCanonicalUnlockOnAnUnknownSlugIs404NotA500(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unknown-slug unlock guard")
	}
	h, ws, _ := embedFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"

	// The recoverer (server.go) logs `panic_recovered`, so an empty capture is a
	// direct observation that no panic fired on the request.
	logs := captureSlog(t)

	// A slug no form in this workspace serves, and no rename left a redirect for:
	// findPublicForm returns nil for it (both the live lookup and the fallback
	// miss), which is exactly the value the shared impl must not dereference.
	const missing = "it-no-such-slug-e6c7afd5"
	unlockPath := "/" + missing + "/unlock"
	rec := publicPostJSON(t, hnd, host, unlockPath, map[string]any{"password": "anything"})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST %s (unknown slug) -> %d, want 404. A bogus unlock is a bad "+
			"REQUEST, not a server fault; a 500 here means the nil findPublicForm returned "+
			"for a miss reached unlockResolvedForm, which dereferences f.Status. body=%s\n"+
			"recoverer log:\n%s", unlockPath, rec.Code, rec.Body.String(), logs())
	}
	if got := logs(); strings.Contains(got, "panic_recovered") {
		t.Fatalf("the unknown-slug unlock answered %d but the recoverer still fired — the "+
			"nil guard is missing (or sits after the dereference), so the 404 above is not "+
			"what this handler actually does on a miss:\n%s", rec.Code, got)
	}

	// Control, in the same run and on the same host: a slug that DOES resolve
	// but is not private must also be a 404 from the same shared impl, so the
	// guard above is pinned to the miss path and not to a route that 404s
	// unconditionally.
	if rec := publicPostJSON(t, hnd, host, "/"+missing+"-still-missing/unlock",
		map[string]any{"password": "anything"}); rec.Code != http.StatusNotFound {
		t.Fatalf("control POST on a second unknown slug -> %d, want 404", rec.Code)
	}
	if got := logs(); strings.Contains(got, "panic_recovered") {
		t.Fatalf("the recoverer fired on the control request too:\n%s", got)
	}
}
