// The reserved-LIST refusal over the REAL REST route: PATCH /v1/workspaces/{id}
// renaming a workspace ONTO a label JomForm owns.
//
// WHY THIS FILE EXISTS (card t_b7f81f5e, the rework of PR 691). The reserved
// list is proven in internal/urlguard, the shared rule in dbstore's sentinel
// guard, and the CLAIM (POST) path in workspace_reserved_slug_586ff0c3_test.go.
// None of those can see the RENAME path, and that is where the defect lived:
//
//	PATCH {"slug":"billing"} -> 500 {"code":"internal"}   <- measured by t_758bb125
//	POST  {"slug":"billing"} -> 400 + reserved_subdomain  <- correct, same tree
//
// The cause was not the rule's verdict — the rename REFUSED the move and wrote
// nothing. It was that the refusal was an anonymous error satisfying no sentinel,
// so renameWorkspace's error switch had no arm for it and fell through to
// respondErr. A test that asserted only "the slug did not move" would have passed
// over the whole defect, so the assertions here are on the STATUS and the CODE.
//
// Skipped without the test database; the preflight gate requires it and fails
// loudly when it is unset.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/urlguard"
)

// renameReservedOut is the subset of the rename answer these tests read.
type renameReservedOut struct {
	Error string `json:"error"`
	Code  string `json:"code"`
	Slug  string `json:"slug"`
}

// TestRESTRenameOntoReservedSlugIsA400NotA500 is the card's acceptance item 1.
//
// The status is the whole point: a user-input refusal answered 500 is both the
// wrong contract (500 says "we broke", so the dashboard renders a generic
// failure and the operator sees an ERROR log with a stack trace) and the reason
// the qa card failed the PR.
func TestRESTRenameOntoReservedSlugIsA400NotA500(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "reserved")
	sp := h.Workspaces

	// One label per category, plus the two the qa card drove by hand.
	for _, slug := range []string{
		"www", "api", "app", "mail", "s3", // infrastructure
		"docs", "mcp", "templates", "custom", // routed / JomForm-owned
		"support", "help", "status", "billing", // sensitive / impersonation
		"admin", "account", "login", "payments", "checkout", "cdn", "assets",
	} {
		if !urlguard.ReservedSubdomain(slug) {
			t.Fatalf("fixture is wrong: %q is not reserved, so the assertions below would prove nothing", slug)
		}
		rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
			`{"slug":"`+slug+`"}`)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH /v1/workspaces/{id} {\"slug\":%q} = %d (%s), want 400. A reserved label is "+
				"input the caller can change: answering 500 {\"code\":\"internal\"} reports a user-input "+
				"refusal as an internal failure, logs it at ERROR with a stack trace, and leaves the "+
				"dashboard rendering a generic sentence", slug, rec.Code, rec.Body.String())
			continue
		}
		var out renameReservedOut
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Errorf("slug=%q: response %s is not the error envelope: %v", slug, rec.Body.String(), err)
			continue
		}
		// The machine token (#694): the dashboard renders ITS sentence from this,
		// and the reserved sentence is the only one that tells the merchant the
		// name is JomForm's rather than that their typing was bad.
		if out.Code != urlguard.ReservedSubdomainCode {
			t.Errorf("slug=%q body code = %q, want %q (body %s)", slug, out.Code, urlguard.ReservedSubdomainCode, rec.Body.String())
		}
		// The WHY, for curl/API-key/MCP callers who cannot look up an i18n key.
		if !strings.Contains(strings.ToLower(out.Error), "reserved") {
			t.Errorf("slug=%q refusal message %q does not say the name is reserved — the merchant reads "+
				"it as their own input being malformed and retries the same sensitive name", slug, out.Error)
		}

		// And it is a REFUSAL, not a message over a moved workspace. Read the ROW
		// (not the answer) so a write that happened but was not reported fails.
		after, err := sp.ByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if after.Slug != ws.Slug {
			t.Fatalf("slug=%q was refused with %d but the workspace moved: %q -> %q",
				slug, rec.Code, ws.Slug, after.Slug)
		}
		// The ALIAS half, which is the side effect a rename writes: a refused
		// rename must not reserve the reserved label as a former address of this
		// workspace (that would hand the caller's own claim to the label they were
		// just refused, and shadow the redirect path).
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM workspace_slug_aliases WHERE slug=$1 AND workspace_id=$2`,
			slug, ws.ID).Scan(&n); err != nil {
			t.Fatalf("count aliases for %q: %v", slug, err)
		}
		if n != 0 {
			t.Errorf("slug=%q was refused but %d alias row(s) were written for this workspace", slug, n)
		}
	}
}

// TestRESTRenameAndCreateAgreeOnAReservedSlug is the property the defect broke:
// the SAME input must get the SAME answer from both write paths. Create was
// right and rename was wrong on the same bytes; asserting the two verdicts equal
// is what makes a future divergence fail here rather than in production.
//
// It also keeps the create path honest: create answered the reserved code from a
// `default:` arm, which is an accident of that switch. If someone reorders it,
// this compares the two answers rather than restating create's.
func TestRESTRenameAndCreateAgreeOnAReservedSlug(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "reservedparity")

	for _, slug := range []string{"billing", "s3", "docs", "mcp"} {
		// CREATE, through the real route with a session-authed caller that has an
		// ACCOUNT (resolve() 401s a bare user, which would make every case pass
		// for the wrong reason).
		cToken := accountHoldingUser(t, h, pool, "resv-create-"+slug)
		createRec := sessionReq(t, h, cToken, http.MethodPost, "/v1/workspaces",
			`{"name":"reserved parity","slug":"`+slug+`"}`)

		renameRec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
			`{"slug":"`+slug+`"}`)

		var createOut, renameRes renameReservedOut
		_ = json.Unmarshal(createRec.Body.Bytes(), &createOut)
		_ = json.Unmarshal(renameRec.Body.Bytes(), &renameRes)

		if createRec.Code != http.StatusBadRequest {
			t.Errorf("create %q = %d (%s), want 400 — the control this comparison rests on is gone",
				slug, createRec.Code, createRec.Body.String())
			continue
		}
		if createRec.Code != renameRec.Code {
			t.Errorf("create and rename DISAGREE about the reserved label %q: create %d, rename %d "+
				"(create %s / rename %s) — the reserved rule is enforced on one write path and "+
				"reportable on another, which is the defect this card fixes",
				slug, createRec.Code, renameRec.Code, createRec.Body.String(), renameRec.Body.String())
		}
		if createOut.Code != renameRes.Code {
			t.Errorf("create and rename answer DIFFERENT codes for %q: create %q, rename %q — the "+
				"dashboard would render the reserved sentence on one surface and a generic one on the other",
				slug, createOut.Code, renameRes.Code)
		}
		if renameRes.Code != urlguard.ReservedSubdomainCode {
			t.Errorf("rename onto %q carries code %q, want %q", slug, renameRes.Code, urlguard.ReservedSubdomainCode)
		}
		// Both must say the same thing, since both forward the ONE rule's wording.
		if createOut.Error != renameRes.Error {
			t.Errorf("create and rename answer different MESSAGES for %q:\n  create: %q\n  rename: %q",
				slug, createOut.Error, renameRes.Error)
		}

		// Nothing was created by the refused create, so the next iteration's
		// assertion cannot be confused by a leftover row.
		if workspaceRowExists(t, slug) {
			t.Errorf("the refused create for %q left a workspace row behind", slug)
		}
	}
}

// TestRESTRenameOntoAReservedSlugIsStillRefusedAtTheStore pins the security half
// separately from the reporting half: the refusal HOLDS. The qa card measured
// that it held even while it was mis-reported, and this keeps that property from
// being traded away while fixing the status — a rename that started succeeding
// onto a reserved label would be an impersonation hole, not a status bug.
func TestRESTRenameOntoAReservedSlugIsStillRefusedAtTheStore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "reservedholds")

	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"billing","name":"Should Not Apply"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("renaming ONTO %q SUCCEEDED: %s — a merchant now answers on billing.jomform.com", "billing", rec.Body.String())
	}

	after, err := h.Workspaces.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	// Neither half moved — including the NAME that rode in the same request, so a
	// refusal cannot be a partial write.
	if after.Slug != ws.Slug {
		t.Errorf("the refused rename moved the slug: %q -> %q", ws.Slug, after.Slug)
	}
	if after.Name != ws.Name {
		t.Errorf("the refused rename applied the NAME that rode in the same request: %q -> %q", ws.Name, after.Name)
	}
	// No row anywhere holds the label, and no alias reserves it.
	var live, aliases int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE slug=$1`, "billing").Scan(&live); err != nil {
		t.Fatalf("count live workspaces on the reserved label: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspace_slug_aliases WHERE slug=$1`, "billing").Scan(&aliases); err != nil {
		t.Fatalf("count aliases on the reserved label: %v", err)
	}
	if live != 0 || aliases != 0 {
		t.Errorf("the reserved label %q is held by %d workspace row(s) and %d alias row(s), want 0/0 — "+
			"nothing may hold a name JomForm owns", "billing", live, aliases)
	}
}
