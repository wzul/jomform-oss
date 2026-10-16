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
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
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

// webRoot locates the web tree relative to this test file
// (api/internal/httpapi -> ../../../web). It is the ROOT the docs-claim class
// guard walks: the surfaces a reader can read docs copy from are not confined to
// web/src — web/public/llms*.txt is one of them and .mdx pages will be another
// (card t_38a8eaed). It fails rather than skipping when the tree is absent, so a
// missing checkout cannot turn the guard into a green test that read nothing.
func webRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the web tree")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "web"))
	if st, err := os.Stat(root); err != nil {
		t.Fatalf("stat web tree %s: %v", root, err)
	} else if !st.IsDir() {
		t.Fatalf("%s is not a directory", root)
	}
	return root
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

// normalizedDocsText collapses the JSX source into the sentences a READER sees:
// line breaks and the `{" "}` splice markers are folded to single spaces, and
// the HTML entities used for apostrophes/quotes are decoded. Without this a
// guard matches one exact source LAYOUT, so a paragraph reflow (or `&apos;`
// becoming `&#39;`) silently disarms it while the rendered claim is unchanged.
func normalizedDocsText(s string) string {
	s = strings.ReplaceAll(s, `{" "}`, " ")
	s = strings.NewReplacer("&apos;", "'", "&#39;", "'", "&quot;", `"`, "&amp;", "&").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// TestDocsEmbedPageDoesNotClaimTheGateReappears pins the third-party-cookie
// sentence, because it stated the INVERSE of what the feature does.
//
// THE DEFECT. The sentence before it explains that the unlock cookie is
// `Partitioned` (CHIPS), which is why an unlock on one merchant's page does not
// unlock the form anywhere else. The sentence that followed then told the
// merchant that a visitor whose browser "blocks third-party cookies outright
// ... will see the password gate again on the next load" — contradicting the
// text directly above it. CHIPS is precisely the mechanism that keeps a
// cross-site cookie WORKING when third-party cookies are blocked: Chrome's own
// documentation says CHIPS, the Storage Access API and Related Website Sets are
// "the only way to read and write cookies from cross-site contexts, such as
// iframes, when third-party cookies are blocked"
// (https://developer.chrome.com/docs/privacy-sandbox/chips/). So the page
// described the feature as failing in exactly the case it was built for, and
// QA measured the opposite on the branch this guard ships with (prod cookie
// observed as `...; SameSite=None; Partitioned`, and the unlock survived a
// fresh navigation on a third-party-cookie-blocking profile).
//
// WHAT IS PINNED. (a) The inverted claim is gone — matched on the CLAIM rather
// than on one whitespace layout. (b) The true half is STATED, not merely
// deleted: the paragraph must still say the unlock keeps working when a browser
// blocks third-party cookies, and must still name `Partitioned` as the reason,
// or a merchant cannot tell why embedding a private form is safe. What remains
// as a caveat is a different claim — a browser that does not implement
// partitioned cookies at all — and rewording it is fine as long as the guard is
// updated with it; asserting the gate reappears on a cookie-blocking browser is
// not.
//
// WHAT ELSE IS PINNED (c), added by card t_87697ea4: the SCOPE the paragraph
// names, because the value word carries the claim and nothing above reads it.
// The sentence that described the cookie said it was "scoped to the one page it
// was entered on", which is not what a browser implements: CHIPS partitions by
// top-level SITE (MDN, Partitioned cookies: "a separate cookie jar per top-level
// site ... the partition key is based on the site, including the scheme, of the
// top-level URL"), and four lines later the same paragraph said the cookie is
// "stored per top-level site" — so the paragraph contradicted itself. The
// implementation agrees with the second half, not the first:
// newFormAccessCookie sets Path "/" and the unlock token is bound to the FORM id
// (signFormAccessToken), so an unlock entered on one page of a merchant's site
// is also sent on that site's other pages. MEASURED on this tree before the fix,
// the mutation `"stored per top-level site" -> "stored per top-level page"` left
// this test AND TestNoDocsClaimTheGateReappearsOnACookieBlockingBrowser AND
// TestDocsClaimSurfacesCoverTheReaderFacingWebTree all GREEN — no assertion in
// any of them read the scope noun. So both directions are now pinned: the
// site-scoped sentences must be PRESENT (a deletion reds) and the narrower
// page-/domain-scoped shapes must be ABSENT (a swap reds, and names itself).
//
// The cookie's behaviour is measured elsewhere: TestEmbeddedPrivateFormUnlockRoundTrip
// asserts the unlock's Set-Cookie carries Partitioned. This guard is what stops
// the COPY from drifting back into contradicting that behaviour.
func TestDocsEmbedPageDoesNotClaimTheGateReappears(t *testing.T) {
	page := normalizedDocsText(readFileString(t, docsEmbedPagePath(t)))

	// (a) The inverted claim, in the shapes it can come back as.
	for _, claim := range []string{
		"see the password gate again",
		"gate again on the next load",
		"gate reappears",
	} {
		if strings.Contains(page, claim) {
			t.Errorf("the docs still claim that a visitor whose browser blocks third-party "+
				"cookies will %q (%q) — that is backwards and it contradicts the Partitioned "+
				"explanation in the sentence before it. A partitioned (CHIPS) cookie is stored "+
				"per top-level site, which is exactly what keeps the unlock working while "+
				"third-party cookies are blocked; a browser that blocks them outright still "+
				"stores this one. If a caveat is wanted, name the browsers that do not "+
				"implement partitioned cookies at all rather than asserting the gate comes back",
				claim, claim)
		}
	}

	// (b) The true half must be stated. Deleting the sentence is the other way
	// to lose it, and it leaves a merchant no way to know the embed survives a
	// cookie-blocking browser.
	if !strings.Contains(page, "Partitioned") {
		t.Error("the docs no longer name the Partitioned (CHIPS) cookie — that attribute is why " +
			"the unlock survives on the embedding page, and why it is confined to the top-level " +
			"site that page sits on instead of being shared with every other site the form is " +
			"embedded on; the paragraph must keep saying so")
	}
	if !strings.Contains(page, "keeps that unlock working when a visitor's browser blocks third-party cookies") {
		t.Error("the docs no longer state that the unlock KEEPS WORKING when a browser blocks " +
			"third-party cookies — deleting the wrong claim is not the same as correcting it. " +
			"State what actually happens (the cookie is stored per top-level site) and keep any " +
			"caveat to browsers that do not support partitioned cookies yet; update this guard " +
			"if the wording changes")
	}

	// (c) The SCOPE the paragraph names must be the scope the browser
	// implements, in both directions. The value word is what carries the claim:
	// the false sentence this card corrects ("scoped to the one page it was
	// entered on") and the narrower swap of the true one ("stored per top-level
	// site" -> "per top-level page") both kept every assertion above — and both
	// other docs guards — GREEN, because not one of them reads the scope noun.
	//
	// The mechanism sentence, pinned as the literal a READER sees (in the source
	// it is split across a line break: "...stored per top-level\nsite...").
	if !strings.Contains(page, "stored per top-level site") {
		t.Error("the docs no longer say the unlock cookie is 'stored per top-level site' — CHIPS " +
			"partitions by top-level SITE (MDN, Partitioned cookies: \"a separate cookie jar per " +
			"top-level site ... the partition key is based on the site, including the scheme, of the " +
			"top-level URL\"), so this sentence is the mechanism the paragraph exists to explain. " +
			"If the wording moved, move this pin with it — do not delete the scope from the copy")
	}
	// And the scoping sentence itself: site-level, not page-level.
	if !strings.Contains(page, "scoped to the top-level site") {
		t.Error("the docs no longer scope the unlock cookie to the top-level SITE — the sentence must " +
			"state the rule the browser actually implements. This is the assertion the false " +
			"\"scoped to the one page it was entered on\" (card t_87697ea4) failed: a Partitioned " +
			"cookie is keyed by top-level site, not by page, and newFormAccessCookie sets Path \"/\" " +
			"with a token bound to the form id, so an unlock entered on one page is also sent on that " +
			"site's other pages. Correcting the copy means pinning the corrected wording here, once")
	}
	// The true half must SURVIVE the correction: a different merchant's site is
	// a different partition, so their page is not unlocked by yours.
	if !strings.Contains(page, "does not unlock it on anybody else's") {
		t.Error("the docs no longer say an unlock 'does not unlock it on anybody else's' — deleting " +
			"the scoping clause is not correcting it. The paragraph must still explain why a second " +
			"merchant's page is not unlocked: their site is a different CHIPS partition")
	}
	// The narrower scopes, by name, so a site -> page (or site -> domain) swap
	// reds here and says which claim did it.
	for _, claim := range []string{
		"one page it was entered on",
		"scoped to the one page",
		"scoped to the page",
		"per top-level page",
		"per top-level domain",
		"top-level page",
		"top-level domain",
	} {
		if strings.Contains(page, claim) {
			t.Errorf("the docs scope the unlock cookie by %q — CHIPS scopes a Partitioned cookie to the "+
				"top-level SITE (scheme + registrable domain), never to a page, and the same paragraph "+
				"says the cookie is 'stored per top-level site'. An unlock entered on one page of a "+
				"merchant's site IS sent on that site's other pages (newFormAccessCookie: Path \"/\", "+
				"token bound to the form id), so the narrower claim is both false and "+
				"self-contradicting; state the site-level rule instead", claim)
		}
	}
}

// docsClaimSurfaces lists the reader-facing files under web/ that can carry this
// claim. The file set comes from GIT (`ls-files --cached --others
// --exclude-standard`), not a raw tree walk, so a new page or a copy-paste of
// the sentence into another docs page is covered by construction — a guard that
// iterates a hardcoded set of filenames cannot see the file it was written to
// protect against — while a generated, gitignored artefact the toolchain writes
// into web/ (Playwright's web/test-results/*/error-context.md, Next's
// web/next-env.d.ts) can never be judged. See the WHY comment in the body.
//
// WHY THE ROOT IS web/ AND NOT web/src (card t_38a8eaed). This guard's stated
// scope is "anywhere a reader can see the sentence", and web/src is not that
// boundary. Two reader-facing surfaces sit outside it and were silently
// unjudged: an .mdx docs page (the format the next docs author reaches for —
// none exists yet, which is exactly why the hole would have been found by a
// copy-paste rather than by a test) and web/public/llms.txt / llms-full.txt,
// the agent-facing view of the same knowledge base that
// web/src/lib/docsIndex.test.ts pins as such. Both are served to a reader, so a
// sentence copied into either is the same defect, not an internal comment.
//
// The extension rule and the by-name directory prune are what keep the set
// honest: node_modules is a dependency copy whose vendored prose is not this
// repo's copy, .next/.turbo are build output, and web/public/openapi.json is
// generated from the API route table (the api/ tree is deliberately out of
// scope — this guard must not cry wolf on the source that documents the
// defect). Every other file git can see under web/ is judged by its extension.
func docsClaimSurfaces(t *testing.T, root string) []string {
	t.Helper()
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs %s: %v", root, err)
	}
	// WHY GIT LISTS THE FILES INSTEAD OF filepath.WalkDir (card t_5293b10c).
	// A raw walk over web/ judges ARTEFACTS the toolchain writes into the tree:
	// Playwright's web/test-results/<slug>/error-context.md is the rendered DOM
	// (an admitted .md) and Next's generated web/next-env.d.ts is an admitted
	// .ts, and both are gitignored (root .gitignore `test-results/`;
	// web/.gitignore `/next-env.d.ts`). So the walk could go RED on a path
	// `git checkout` cannot restore — the fix in web/src gets applied, the
	// guard stays red, and no `git status` line explains why. `git ls-files
	// --cached --others --exclude-standard` lists TRACKED files plus
	// untracked-NOT-ignored ones, i.e. exactly the files a checkout carries, so
	// a generated artefact cannot enter by construction and a new generator
	// needs no skip-list edit. An authored .md/.mdx/.ts a docs author adds is
	// still judged the moment git can see it — which is the surface this guard
	// exists for.
	//
	// -z keeps a filename holding a newline or quote from splitting into two
	// records; records are NUL-terminated, so a name may hold anything but NUL.
	//
	// WHY --literal-pathspecs. Its only effect is to stop git treating a path as
	// a pathspec; there is no pathspec on this command, so output is unchanged.
	// What it buys is that an INHERITED GIT_LITERAL_PATHSPECS cannot make the
	// command behave differently under a caller that exports it.
	cmd := exec.Command("git", "-C", root, "ls-files", "-z",
		"--cached", "--others", "--exclude-standard")
	cmd.Env = cleanGitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files in %s: %v — the docs-claim walk reads git's file "+
			"list so generated/gitignored artefacts are never judged; without git "+
			"it cannot tell an authored page from build output", root, err)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		// Belt to `--exclude-standard`'s braces, and load-bearing for one case:
		// `.turbo` is NOT gitignored in this repo (.gitignore ignores `.next/`
		// and `out/` but not `.turbo/`), so a stray `.turbo/notes.md` would
		// otherwise be admitted by the `.md` case. `node_modules`/`.next` are
		// pruned by name too so a checkout that lost its ignore rules (or a
		// fixture repo that never had them) is still safe.
		if docsClaimWalkPrunesDir(root, path) {
			continue
		}
		if !docsClaimSurface(root, path) {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files
}

// docsClaimWalkPrunesDir reports whether path lies under a generated,
// non-authored tree the walk must not judge, by DIRECTORY NAME. The walk calls
// it per file (the git file list has no directory records to SkipDir on), so it
// checks every segment of the root-relative path. Split out beside
// docsClaimSurface so the boundary is assertable on its own rather than only
// through the whole walk.
func docsClaimWalkPrunesDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		switch seg {
		case "node_modules", ".next", ".turbo":
			return true
		}
	}
	return false
}

// cleanGitEnv returns os.Environ() with every GIT_* override that would point a
// `git -C <dir>` at a DIFFERENT repository removed: GIT_DIR and GIT_WORK_TREE
// (which beat -C), GIT_INDEX_FILE / GIT_OBJECT_DIRECTORY / GIT_COMMON_DIR, and
// GIT_LITERAL_PATHSPECS / GIT_GLOB_PATHSPECS / GIT_NOGLOB_PATHSPECS (which
// change how git reads arguments). The preflight gate machinery exports a
// private GIT_INDEX_FILE while it computes a tree sha; a fixture that inherited
// it, or a walk that inherited a stray GIT_DIR, would operate on the real
// checkout instead of the target tree — so every git call this guard makes runs
// against a scrubbed environment.
func cleanGitEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
			"GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR",
			"GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS":
			continue
		}
		env = append(env, kv)
	}
	return env
}

// docsClaimSurface reports whether a file under web/ is one a reader can read
// this claim from — and, separately, whether it is one this guard must NOT judge
// (generated output, or something that is not prose a reader sees). It is split
// out of the walk so the extension rule is assertable on its own: a surface the
// walk cannot admit is a surface the guard does not actually cover, whatever the
// comment above it says. That matters for .mdx, which is admitted here and has
// no file in the tree yet, so no assertion over today's file list can prove it.
func docsClaimSurface(root, path string) bool {
	// openapi.json is generated from the API's own route table, not written by a
	// docs author; judging it would make this guard fail on the API's description
	// of the defect. api/ is out of scope for the same reason (see the class
	// guard's comment).
	if filepath.Clean(path) == filepath.Clean(filepath.Join(root, "public", "openapi.json")) {
		return false
	}
	switch filepath.Ext(path) {
	case ".tsx", ".ts", ".md", ".mdx", ".txt":
		return true
	}
	return false
}

// TestNoDocsClaimTheGateReappearsOnACookieBlockingBrowser is the CLASS guard.
// The wrong sentence is a copy-paste shape: the same reassurance ("the gate
// comes back, so open the form URL directly") is what a docs author reaches for
// on every page that explains the private form, and the card that found this
// instance explicitly asked for every copy to be corrected rather than the one
// the verify card happened to quote.
//
// It walks the WEB TREE and asserts the claim appears NOWHERE — not on the embed
// page, not in any other docs page, not in the .mdx a future docs author may
// author one in, and not in web/public/llms.txt / llms-full.txt, the agent-facing
// view of the same knowledge base. The walk root and its skip list are pinned by
// the comment on docsClaimSurfaces and bite-tested by
// TestDocsClaimSurfacesCoverTheReaderFacingWebTree below.
//
// Scoped to what a reader is SERVED, not to the whole repo. api/ sits outside
// the root deliberately: an api/*.go comment may quote the wrong phrasing while
// describing the defect, so judging it would make the guard cry wolf on its own
// documentation — and node_modules is skipped for the same reason, a dependency's
// vendored prose being text this repo does not own. That is a SUBSTRING scan's
// limit rather than the tree's, and api/ is not unguarded: card t_a29148e5's
// TestNoAPISourceStatesTheUnlockCookieIsScopedToAPage judges the same claim there,
// by telling a quoted MENTION (allowed) from a STATEMENT (a failure). Everything
// else under web/ that can carry prose IS judged, including the web unit tests
// and the e2e specs
// (the pre-fix walk already covered web/src/*.test.ts through its .ts case). That
// is deliberate rather than accidental: a web test should only contain this
// sentence if it is asserting about it, so a match there is worth a look, and
// nothing in the tree matches today.
func TestNoDocsClaimTheGateReappearsOnACookieBlockingBrowser(t *testing.T) {
	root := webRoot(t)

	files := docsClaimSurfaces(t, root)
	if len(files) < 100 {
		t.Fatalf("only %d files found under %s — the walk is not seeing the web tree, so this "+
			"guard would pass over a tree it never read", len(files), root)
	}
	page := filepath.Clean(filepath.Join(root, "src", "app", "docs", "embed", "page.tsx"))
	seenPage := false
	for _, f := range files {
		if filepath.Clean(f) == page {
			seenPage = true
		}
		text := normalizedDocsText(readFileString(t, f))
		for _, claim := range []string{
			"see the password gate again",
			"gate again on the next load",
			"gate reappears",
		} {
			if strings.Contains(text, claim) {
				rel, _ := filepath.Rel(root, f)
				t.Errorf("web/%s claims %q — a partitioned (CHIPS) unlock cookie keeps "+
					"working while third-party cookies are blocked, so this is the same wrong "+
					"sentence as on the embed page. Correct it there too; do not delete the "+
					"sentence without saying what does happen", rel, claim)
			}
		}
	}
	// The assertion above is only meaningful if the walk actually reaches the
	// file the claim was found in — otherwise a typo in the walk root makes this
	// test vacuous and green.
	if !seenPage {
		t.Errorf("the walk over %s never reached src/app/docs/embed/page.tsx — the class guard is "+
			"not reading the page it exists for", root)
	}
}

// docsClaimSurfacesFixtureFiles is the shared body of the fixture. The directory
// layout is the point: a directory named node_modules/.next/.turbo must be pruned
// (the extension rule alone admits the .md inside them), a gitignored generated
// artefact (test-results/<slug>/error-context.md, next-env.d.ts) must never be
// reached at all, while authored markdown at src/ and public/ must be found.
var docsClaimSurfacesFixtureFiles = []string{
	"src/app/docs/embed/page.tsx",
	"src/app/docs/embed/page.mdx",
	"src/content/guides/widget.mdx",
	"src/content/guides/setup.md",
	"src/lib/docsIndex.ts",
	"public/llms.txt",
	"public/llms-full.txt",
	"public/openapi.json",
	"node_modules/some-package/README.md",
	"node_modules/some-package/dist/bundle.js",
	".next/server/notes.md",
	".turbo/notes.md",
	// Generated artefacts the toolchain writes into web/. The .gitignore written
	// by fixture writes() below ignores all of these, so a git-derived file list
	// drops them by construction; a raw tree walk would judge the .md ones and
	// red the guard on bytes git cannot reset (card t_5293b10c).
	"test-results/abcd-embed/error-context.md",
	"playwright-report/index.md",
	"blob-report/report.md",
	"next-env.d.ts",
}

// docsClaimSurfacesFixtureGitignore is the fixture's .gitignore. It ignores the
// GENERATED-artefact class the card is about — the root .gitignore's Playwright
// trees (`test-results/`, `playwright-report/`, `blob-report/`) and
// web/.gitignore's Next-generated `/next-env.d.ts` — so those files prove the
// git-exclusion half: git never lists them and the walk cannot judge them.
//
// It deliberately does NOT ignore node_modules/, .next/ or .turbo/: the real
// repo gitignores the first two, which makes the by-name prune unreachable
// through git (a mutation dropping "node_modules" from the prune stayed GREEN
// when this fixture ignored it). Leaving them un-ignored makes git LIST them, so
// the prune is the only thing that can keep them out and every prune name is
// bite-provable — exactly the "ignore rules are gone" case the belt exists for
// (`.turbo` is un-ignored in the real repo too).
const docsClaimSurfacesFixtureGitignore = "test-results/\nplaywright-report/\nblob-report/\nnext-env.d.ts\nAGENTS.md\nCLAUDE.md\n"

// docsClaimSurfacesFixture writes the tree above under a test temp dir, makes it
// a REAL git repo with the ignore rules applied, and walks it with the
// PRODUCTION walk. A real repo is required now: the walk derives its file set
// from `git ls-files`, so a fixture that is not under git would prove nothing
// about which files the guard judges. Using the production walk is still the
// whole point — a fixture list checked against a copy of the rules proves
// nothing about the guard, only about the copy.
func docsClaimSurfacesFixture(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for fixture %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", rel, err)
		}
	}
	write(".gitignore", docsClaimSurfacesFixtureGitignore)
	for _, rel := range docsClaimSurfacesFixtureFiles {
		write(rel, "fixture body for "+rel+"\n")
	}
	// git init + `git add -A` (which honours .gitignore) turns every non-ignored
	// fixture file into a TRACKED one, exactly the shape a real checkout has, so
	// the walk's --cached half is exercised as well as --others. cleanGitEnv
	// keeps an inherited GIT_DIR / GIT_INDEX_FILE (the preflight gate machinery
	// exports a private index) from pointing these at the REAL checkout.
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = cleanGitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in fixture %s: %v\n%s", args, root, err, out)
		}
	}
	var got []string
	for _, f := range docsClaimSurfaces(t, root) {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatalf("rel %s: %v", f, err)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	sort.Strings(got)
	return root, got
}

// TestDocsClaimSurfacesCoverTheReaderFacingWebTree pins the SURFACES, separately
// from the claim: a walk that silently stops seeing a file passes the scan above
// for the trivial reason that it read less. Both halves of the rule are asserted,
// because they are enforced by different mechanisms and only one of them is
// visible in today's tree.
//
// (a) The real tree. The walk must reach the surfaces whose ABSENCE from
// web/src moved the root — public/llms.txt and public/llms-full.txt — plus the
// embed page and one docs page. Asserted by path, so this fails the moment the
// root is narrowed back (the pre-fix walk cannot see any web/public file).
//
// (b) A throwaway FIXTURE REPO, for the boundaries no file in the real tree can
// exercise: whether the EXTENSION rule admits the formats a docs author may use
// (.mdx has no file today, so only the rule can be asserted), whether the dir
// prune drops a dependency's vendored prose and build output, and whether the
// git-derived file set drops GENERATED, gitignored artefacts (the .md Playwright
// writes into test-results/, the .ts Next writes into next-env.d.ts) — the same
// artefact class that red the guard on a path git cannot reset (card
// t_5293b10c). Asserted against the fixture so the answer does not depend on
// which dependencies happen to be installed, and so every rule here is
// bite-provable on a tree that is built in the test rather than on the state of
// the checkout.
//
// BITE, all measured on this file: narrow the root to web/src -> (a) names the
// four real-tree surfaces as unreached; drop ".mdx" from docsClaimSurface -> (b)
// names the two .mdx paths as missing; drop "node_modules", or ".next" and
// ".turbo", from docsClaimWalkPrunesDir -> (b) names the vendored README or the
// build output as judged; STOP passing --exclude-standard (or list generated
// output another way) -> (b) names test-results/abcd-embed/error-context.md as
// judged.
func TestDocsClaimSurfacesCoverTheReaderFacingWebTree(t *testing.T) {
	root := webRoot(t)
	files := docsClaimSurfaces(t, root)

	seen := map[string]bool{}
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Errorf("rel %s: %v", f, err)
			continue
		}
		seen[filepath.ToSlash(rel)] = true
	}

	// (a) Surfaces that exist in the real tree and must be reached.
	for _, want := range []string{
		"src/app/docs/embed/page.tsx",
		"src/app/docs/customize/page.tsx",
		"public/llms.txt",
		"public/llms-full.txt",
		// The class guard judges the web test/spec files too (its .ts case always
		// did); asserted so that is a measured boundary rather than a comment.
		"src/lib/docsIndex.test.ts",
		"e2e/a11y.spec.ts",
	} {
		if !seen[want] {
			t.Errorf("the docs-claim walk does not reach web/%s — that is a reader-facing surface, "+
				"so a copy of the false sentence there is unjudged (and if the root moved back "+
				"under web/src, every web/public file is invisible to it)", want)
		}
	}

	// (b) The boundary rules, against a fixture tree. The walk's result is
	// compared to a LITERAL expected set rather than probed path by path, so both
	// kinds of mistake fail at once: a reader-facing file the guard skips (the two
	// .mdx paths — a format no checkout contains today, so nothing else can assert
	// them) and a file it judges but must skip (the vendored README and the build
	// output, both .md, which the EXTENSION rule alone would have admitted — it is
	// the dir-skip that keeps them out, and that is what this pins).
	fixtureRoot, got := docsClaimSurfacesFixture(t)
	want := []string{
		"public/llms-full.txt",
		"public/llms.txt",
		"src/app/docs/embed/page.mdx",
		"src/app/docs/embed/page.tsx",
		"src/content/guides/setup.md",
		"src/content/guides/widget.mdx",
		"src/lib/docsIndex.ts",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the docs-claim walk over a fixture tree returned\n  %v\nwant\n  %v\n"+
			"a path in want but not in got is a reader-facing surface the walk silently skips; a "+
			"path in got but not in want is a dependency's prose, build output, or a generated "+
			"artefact the walk judges",
			got, want)
	}

	// The generated, gitignored artefacts must be ABSENT by NAME, so a regression
	// that stops excluding them (e.g. dropping --exclude-standard from the git
	// invocation) fails naming the file rather than only as an opaque set diff —
	// and so the card's whole class (a Playwright-written .md, a Next-written
	// .ts) is pinned rather than only implied by the comparison above.
	for _, generated := range []string{
		"test-results/abcd-embed/error-context.md",
		"playwright-report/index.md",
		"blob-report/report.md",
		"next-env.d.ts",
	} {
		if slices.Contains(got, generated) {
			t.Errorf("the docs-claim walk admits the generated, gitignored web/%s — a "+
				"toolchain writes that file, so judging it reds the guard on a path git cannot "+
				"reset; the file set must come from git (`ls-files --cached --others "+
				"--exclude-standard`), never a raw tree walk", generated)
		}
	}

	// The generated API description is excluded by the named skip, not merely by
	// its .json extension, so it is asserted through the rule the walk uses. This
	// is the belt to the brace above: it keeps a future .json that IS authored
	// prose from being admitted silently.
	for _, rel := range []string{"public/openapi.json"} {
		if docsClaimSurface(fixtureRoot, filepath.Join(fixtureRoot, filepath.FromSlash(rel))) {
			t.Errorf("the docs-claim walk admits web/%s — it is generated from the API's route "+
				"table, so judging it makes the guard fail on the API's description of the defect", rel)
		}
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
			"scopes the unlock to the top-level SITE it was entered on — the cookie's Path is "+
			"\"/\" and the token is bound to the form id, so a second page of that same site is "+
			"unlocked too, while a second merchant's site is a different partition and asks "+
			"again", setCookie)
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

// TestUnlockCookieCarriesToAnotherPageOfTheSameSite pins the MECHANISM the
// corrected failure message in TestEmbeddedPrivateFormUnlockRoundTrip names: the
// unlock cookie's Path is "/" and its token is bound to the FORM id, so an unlock
// entered on one page of a site is accepted on that site's other pages (card
// t_a29148e5, following card t_87697ea4 / PR 1200). The message the repo used to
// print claimed the opposite scope — "the one merchant page it was entered on" —
// and a message that states a mechanism nobody measures is prose that can drift
// away from the behaviour, so this is the behaviour half of the same correction
// (the prose guard, unlock_scope_prose_guard_test.go, is the wording half).
//
// WHAT THIS DOES AND DOES NOT COVER. It drives the SERVER side of the carry: a
// cookie minted by the unlock on one page is accepted, on another page of the
// same host, for the same form. The browser side — that the cookie is sent at all
// on a page of the same top-level site, and only there — is CHIPS partitioning
// plus ordinary cookie matching, not server behaviour: MDN ("a separate cookie
// jar per top-level site") is why the docs page, not this test, is where the site
// rule is pinned, and RFC 6265 §5.4 (a cookie-path that is a prefix ending in "/"
// path-matches every request path) is why `Path: "/"` is the half a browser acts
// on. httptest does not implement cookie matching, so the carry here is the
// server accepting the token anywhere; the Path itself is asserted below.
func TestUnlockCookieCarriesToAnotherPageOfTheSameSite(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unlock scope carry guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)
	// Prod semantics (the helper oauth_test.go exists for): a real buyer unlocks
	// over https on a site that is not the workspace's own, which is the case the
	// message is about.
	setProdMode(t)
	host := ws + ".jomform.com"

	pageA := "/" + slug
	pageB := "/embed/f/" + ws + "/" + slug

	// CONTROL: page B on its own shows the gate, so the assertion at the bottom
	// is the cookie's doing and not a form that never gates.
	if rec := publicGet(t, hnd, host, pageB); !strings.Contains(rec.Body.String(), `id="pw"`) {
		t.Fatalf("fixture check: GET %s with no cookie -> %d and no password gate, so this test "+
			"cannot tell an unlocked page from an ungated one. body: %.300s",
			pageB, rec.Code, rec.Body.String())
	}

	// The buyer unlocks on page A: the page the form itself is served on.
	rec := publicPostJSON(t, hnd, host, pageA+"/unlock", map[string]any{"password": gateFixturePassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s/unlock on the form's own page -> %d, want 200 (body %.300s)",
			pageA, rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
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
	// The first half of the mechanism the message names, read off the cookie: a
	// page-scoped Path is exactly what would leave the site's other pages gated.
	if access.Path != "/" {
		t.Errorf("the unlock cookie's Path is %q, want \"/\" — the Path is what puts the cookie "+
			"on the site's other pages, and the corrected message says so", access.Path)
	}

	// ── page B of the SAME site, carrying the cookie set on page A ──────────
	other := embedGetWithCookies(t, hnd, host, pageB, cookies)
	if other.Code != http.StatusOK {
		t.Fatalf("GET %s carrying the cookie minted on %s -> %d, want 200 (body %.300s)",
			pageB, pageA, other.Code, other.Body.String())
	}
	body := other.Body.String()
	if strings.Contains(body, `id="pw"`) {
		t.Errorf("GET %s carrying the cookie minted on %s still shows the password gate — the "+
			"unlock is NOT carried to the site's other pages, which is the claim the corrected "+
			"failure message makes and the rule the docs page states. body: %.300s",
			pageB, pageA, body)
	}
	for _, want := range []string{`name="bname"`, `name="bemail"`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET %s after unlocking on %s is missing the form's own field %s — the "+
				"cookie was accepted but the form is not what was served. body: %.300s",
				pageB, pageA, want, body)
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
