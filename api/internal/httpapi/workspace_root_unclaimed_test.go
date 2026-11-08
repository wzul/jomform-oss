// Card t_204a48c4 — an unclaimed *.jomform.com label must answer 404, not a
// "Shop <label>" shell.
//
// MEASURED ON PROD v1.18.0 @ 15337da0, against the ORIGIN (curl --resolve to
// bypass Cloudflare, so this was routing and not DNS):
//
//	notaworkspace-zzz.jomform.com/  200  <h1>Shop notaworkspace-zzz</h1>
//	templates.jomform.com/          200  <h1>Shop templates</h1>
//	support.jomform.com/            200  <h1>Kedai support</h1>
//	abc.jomform.com/                200  <h1>Kedai abc</h1>   <- the real one
//
// The invented-name line is the control that makes the others a defect: the
// shell is IDENTICAL for a label no workspace owns and for a live tenant,
// because the auto-listing renders "Kedai <slug>" and, for zero resolved forms,
// "Belum ada borang" — which is byte-for-byte what a real workspace with nothing
// published renders. The wildcard is registered by Traefik
// (HostRegexp([a-z0-9-]+.jomform.com), the only matching rule), so EVERY label
// under *.jomform.com reaches handleWorkspaceRoot and the API is the only layer
// that can tell "claimed" from "unclaimed".
//
// Two consequences this guard is written against:
//
//   - a 200 on the root is NOT evidence that the host resolved to a workspace.
//     A deploy card's reading of `templates.jomform.com/ -> 200` was measuring
//     THIS shell, not gallery content (no workspace owns `templates`), so the
//     claimed leg below asserts the form LINK the storefront actually lists,
//     not the status code alone.
//   - urlguard.reservedSubdomains refuses a CLAIM of admin/support/status/...,
//     but a request to those hosts used to answer the storefront shell, so a
//     merchant or a scanner reading status codes could not tell a
//     reserved-and-unclaimed name from a live tenant. Those labels are driven
//     here too — same rule, no row, same answer.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// labelRootPath is the one path this card is about: the storefront ROOT of a
// wildcard host. Kept as a constant so the claimed control and the unclaimed
// legs cannot drift onto different routes.
const labelRootPath = "/"

// labelRootOnHandler drives the REGISTERED handler directly, with no mux and no
// middleware, so the verdict below is the handler's own decision about the Host
// rather than something another layer could add.
func labelRootOnHandler(t *testing.T, h *PublicHandlers, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, labelRootPath, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.handleWorkspaceRoot(rec, req)
	return rec
}

// workspaceSlugRows counts the workspaces that hold a label, so every leg states
// its own precondition ("no workspace owns this label") instead of assuming it.
func workspaceSlugRows(t *testing.T, slug string) int {
	t.Helper()
	var n int
	if err := bundleTestPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM workspaces WHERE slug = $1`, slug).Scan(&n); err != nil {
		t.Fatalf("count workspaces with slug %q: %v", slug, err)
	}
	return n
}

// TestUnclaimedWorkspaceLabelRootIs404 is the guard for this card.
//
//  1. a label no workspace owns 404s, and the "Kedai <label>" shell is gone;
//  2. reserved-but-unclaimed labels (the ones urlguard refuses a CLAIM of, and
//     the labels measured on prod) 404 by the same rule;
//  3. the CLAIMED workspace still serves 200 AND still lists its published
//     form — a 200 alone proves nothing about resolution, so the form link is
//     asserted;
//  4. the ROW is the claim: inserting exactly the unclaimed label's row flips
//     the SAME host+path to 200 with the shell for that label, so the 404 is
//     caused by the missing row and not by the label's spelling.
func TestUnclaimedWorkspaceLabelRootIs404(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unclaimed-label storefront guard")
	}
	h, ws, formSlug := publicFixture(t)
	ctx := context.Background()
	pool := bundleTestPool(t)

	// Per-pass label: step 4 claims it, so a second -count pass must not find
	// the row this pass wrote (workspaces.slug is UNIQUE).
	invented := "it-unclaimed-" + runKey()

	// ── 1 & 2: a label with no workspace row is a 404 ────────────────────────
	labels := []struct {
		label string
		why   string
	}{
		{invented, "an invented label that has never been claimed (the card's prod control)"},
		{"templates", "reserved for JomForm's own account; measured 200 on prod, and no workspace owns it"},
		{"support", "reserved impersonation name; measured 200 on prod"},
		{"admin", "reserved impersonation name; measured 200 on prod"},
	}
	for _, tc := range labels {
		if n := workspaceSlugRows(t, tc.label); n != 0 {
			t.Fatalf("fixture precondition broken: %d workspace row(s) hold the label %q, so it is "+
				"CLAIMED and this leg would not be measuring an unclaimed host", n, tc.label)
		}
		rec := publicServe(t, h, tc.label+".jomform.com", labelRootPath)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s on %s.jomform.com -> %d, want 404 — no workspace owns %q (%s), so the "+
				"storefront shell is being served for a label that was never claimed. body: %.200s",
				labelRootPath, tc.label, rec.Code, tc.label, tc.why, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Kedai "+tc.label) {
			t.Errorf("GET %s on %s.jomform.com answered with the unclaimed-label shell "+
				"(%q); body: %.200s", labelRootPath, tc.label, "Kedai "+tc.label, rec.Body.String())
		}
	}

	// ── 3: the claimed workspace still serves its storefront ─────────────────
	if n := workspaceSlugRows(t, ws); n != 1 {
		t.Fatalf("fixture precondition broken: %d workspace row(s) hold the fixture slug %q, want 1", n, ws)
	}
	claimed := publicServe(t, h, ws+".jomform.com", labelRootPath)
	if claimed.Code != http.StatusOK {
		t.Fatalf("GET %s on the CLAIMED host %s.jomform.com -> %d, want 200 (body %.300s) — the "+
			"existence check must not take a live workspace's storefront off the air",
			labelRootPath, ws, claimed.Code, claimed.Body.String())
	}
	// The status code is not the resolution: the root must still LIST the
	// workspace's published form, which is what makes this a storefront.
	if link := `href='/` + formSlug + `'`; !strings.Contains(claimed.Body.String(), link) {
		t.Errorf("GET %s on %s.jomform.com returned 200 but does not list its published form "+
			"(%s missing). A 200 on the root proves nothing about resolution — assert the form "+
			"link. body: %.300s", labelRootPath, ws, link, claimed.Body.String())
	}

	// ── 4: the ROW is the claim ───────────────────────────────────────────────
	// Same host, same path, same handler — only a workspaces row is added. An
	// empty storefront for a workspace that EXISTS stays legal (the pre-existing
	// behavior for a merchant with nothing published), so the assertion is the
	// shell for THAT label plus the empty-listing hint.
	var ownerUserID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&ownerUserID); err != nil {
		t.Fatalf("create user for the claim control: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1, $2, $3)`,
		ownerUserID, "it-claimed-"+invented, invented); err != nil {
		t.Fatalf("claim the label %q: %v", invented, err)
	}
	nowClaimed := publicServe(t, h, invented+".jomform.com", labelRootPath)
	if nowClaimed.Code != http.StatusOK {
		t.Fatalf("GET %s on %s.jomform.com -> %d after a workspace row for that exact label was "+
			"inserted, want 200 — the answer must turn on the workspaces row (body %.300s)",
			labelRootPath, invented, nowClaimed.Code, nowClaimed.Body.String())
	}
	body := nowClaimed.Body.String()
	if !strings.Contains(body, "Shop "+invented) {
		t.Errorf("the claimed label %s serves no storefront shell: %q missing from body %.300s",
			invented, "Shop "+invented, body)
	}
	if !strings.Contains(body, "No forms yet.") {
		t.Errorf("the claimed-but-empty workspace %s should render the empty listing hint; body: %.300s",
			invented, body)
	}
}

// TestWorkspaceLabelClaimIsTheLiveRowNotTheSlug pins the two halves of the
// decision at the handler itself (no mux, no middleware): an unclaimed label
// 404s, a claimed one serves its form listing, and a TRASHED workspace's label
// is unclaimed again — IDBySlug's `deleted_at IS NULL` is load-bearing, because a
// trashed and a live workspace can hold one slug at the same time, and every
// other public lookup (handlePublicLegalPage, requestingAccountID) already
// treats the binned row as absent.
func TestWorkspaceLabelClaimIsTheLiveRowNotTheSlug(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping unclaimed-label handler guard")
	}
	h, ws, formSlug := publicFixture(t)

	unclaimed := "it-unclaimed-h-" + runKey()
	if n := workspaceSlugRows(t, unclaimed); n != 0 {
		t.Fatalf("fixture precondition broken: %d workspace row(s) hold %q", n, unclaimed)
	}

	unclaimedRec := labelRootOnHandler(t, h, unclaimed+".jomform.com")
	if unclaimedRec.Code != http.StatusNotFound {
		t.Errorf("handleWorkspaceRoot on %s.jomform.com -> %d, want 404 (body %.200s): it must read "+
			"the workspace row before it renders a storefront for any wildcard label",
			unclaimed, unclaimedRec.Code, unclaimedRec.Body.String())
	}

	claimedRec := labelRootOnHandler(t, h, ws+".jomform.com")
	if claimedRec.Code != http.StatusOK {
		t.Fatalf("handleWorkspaceRoot on %s.jomform.com -> %d, want 200", ws, claimedRec.Code)
	}
	if link := `href='/` + formSlug + `'`; !strings.Contains(claimedRec.Body.String(), link) {
		t.Errorf("claimed storefront root does not list form %q (%s missing); body: %.300s",
			formSlug, link, claimedRec.Body.String())
	}

	if _, err := bundleTestPool(t).Exec(context.Background(),
		`UPDATE workspaces SET deleted_at = now() WHERE slug = $1`, ws); err != nil {
		t.Fatalf("trash the fixture workspace: %v", err)
	}
	trashed := labelRootOnHandler(t, h, ws+".jomform.com")
	if trashed.Code != http.StatusNotFound {
		t.Errorf("handleWorkspaceRoot on %s.jomform.com -> %d after the workspace was trashed, want "+
			"404 — a binned workspace's label is unclaimed. body: %.200s",
			ws, trashed.Code, trashed.Body.String())
	}
}
