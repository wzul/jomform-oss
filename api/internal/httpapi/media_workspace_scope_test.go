package httpapi

// THE WORKSPACE SCOPE OF THE MEDIA LIBRARY AT THE HTTP EDGE (card t_e19368af).
//
// THE DEFECT. GET /v1/rest/media was the last dashboard list endpoint that read
// actx.Account.ID without asking scopeOr403, so ?workspace= had only two outcomes:
// the slug was the caller's and resolveSessionAccount had switched the account to
// that workspace, or the slug was unknown (a typo, a stale slug after a rename, or
// an ACCOUNT slug where a WORKSPACE slug belongs) and resolveSessionAccount
// SILENTLY fell back to the caller's first owned account while the handler answered
// 200 with THAT account's rows. Measured on prod: ?workspace=nope-not-a-slug and
// ?workspace=abc-abefb94b both listed the first account's library.
//
// It reads the caller's own other account, so it is not a cross-tenant leak — but a
// merchant who typed or kept a stale scope was silently shown a DIFFERENT workspace
// than the one on their screen, with no error and nothing looking wrong. Every other
// list endpoint answers that pair with 403 "workspace tidak dijumpai"
// (rest_scope.go's scopeUnresolved doc is explicit about it), and so must this one.
//
// WHAT IS PINNED HERE:
//   - a scoped list serves the SELECTED workspace's rows and only those (the fixture
//     gives each workspace its own account and its own media row, so "the first
//     account" and "the selected one" cannot be confused);
//   - an unknown slug is a 403 with the shared message, never a 200 fallback;
//   - an ACCOUNT id where a workspace slug belongs is the same 403 (the exact
//     mistyped-scope shape that was measured);
//   - the unscoped request keeps its documented legacy answer (the caller's first
//     owned account) — this card changes the SCOPED contract, not the API-key /
//     legacy unscoped one.
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// mediaWorkspaceEnv is the shared scope fixture with the media store wired.
func mediaWorkspaceEnv(t *testing.T) *scopeEnv {
	t.Helper()
	e := newScopeEnv(t)
	e.h.Media = dbstore.NewMediaStore(e.pool)
	return e
}

// seedMedia inserts one live media row (an image, so the URL needs no account
// host) for the account and returns the row id.
func seedMedia(t *testing.T, e *scopeEnv, accountID, name string) string {
	t.Helper()
	m := &dbstore.Media{
		AccountID: accountID,
		Key:       "u/" + accountID + "/" + uuid.NewString() + "-" + name + ".jpg",
		URL:       "https://media.example.test/" + name + ".jpg",
		Mime:      "image/jpeg",
		Size:      142,
	}
	if err := e.h.Media.Create(t.Context(), m); err != nil {
		t.Fatalf("seed media for account %s: %v", accountID, err)
	}
	return m.ID
}

// mediaListIDs decodes a list response into its row ids.
func mediaListIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode media list %s: %v", body, err)
	}
	ids := make([]string, 0, len(resp.Items))
	for _, it := range resp.Items {
		ids = append(ids, it.ID)
	}
	if resp.Total != len(ids) {
		t.Errorf("total=%d but %d item(s) returned — paging is inconsistent", resp.Total, len(ids))
	}
	return ids
}

func TestMediaListRefusesAnUnknownWorkspaceAndServesTheSelectedOne(t *testing.T) {
	e := mediaWorkspaceEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 2)
	first := e.accountForWorkspace(t, slugs[0])
	selected := e.accountForWorkspace(t, slugs[1])
	if first == selected {
		t.Fatal("the two workspaces resolved to the same account — the fixture cannot distinguish them")
	}

	firstRow := seedMedia(t, e, first, "first")
	selectedRow := seedMedia(t, e, selected, "selected")

	// 1. Scoped to the SECOND workspace: its own row, and NOT the first account's.
	rec := e.do(t, token, http.MethodGet, "/v1/rest/media?page=1&limit=24&workspace="+slugs[1], "")
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped media list = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	ids := mediaListIDs(t, rec.Body.Bytes())
	if len(ids) != 1 || ids[0] != selectedRow {
		t.Fatalf("scoped media list returned %v, want exactly the selected workspace's row %s "+
			"(the first workspace's row is %s) — a scoped list must be the workspace on screen, "+
			"not a guess", ids, selectedRow, firstRow)
	}

	// 2. An unknown slug: 403, never a fallback to the first account. This is the
	//    exact shape that answered 200 on prod.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/media?workspace=nope-not-a-slug", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("?workspace=<unknown> = %d, want 403 (body %s) — a slug that cannot be resolved "+
			"must be refused, not answered with the caller's first owned account", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "workspace tidak dijumpai") {
		t.Fatalf("refusal body = %s, want the shared \"workspace tidak dijumpai\" message every "+
			"dashboard page already handles", got)
	}

	// 3. An ACCOUNT id where a workspace slug belongs: the same 403. This is what a
	//    stale/confused client sends, and it used to serve the first account's rows.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/media?workspace="+selected, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("?workspace=<account id> = %d, want 403 (body %s) — an account id is not a "+
			"workspace slug, and the refusal is the point", rec.Code, rec.Body.String())
	}

	// 4. The unscoped legacy request keeps its documented answer: the caller's FIRST
	//    owned account. Pinned so a future "fix" cannot silently change the API-key /
	//    legacy contract while fixing the scoped one.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/media", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unscoped media list = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	ids = mediaListIDs(t, rec.Body.Bytes())
	if len(ids) != 1 || ids[0] != firstRow {
		t.Fatalf("unscoped media list returned %v, want the first owned account's row %s", ids, firstRow)
	}
}
