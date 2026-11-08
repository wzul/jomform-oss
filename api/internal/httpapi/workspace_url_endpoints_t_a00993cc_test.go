package httpapi

// Card t_a00993cc: the WORKSPACE address on the two WRITE routes.
//
// Card t_48a93084/#1536 put the deployment decision in internal/installurl and
// routed the payload builders through it. The defect it closed lived in four
// HANDLER arms, each with its own copy of `"https://" + slug + ".jomform.com"`,
// and the tests that landed with that fix call `workspaceCreatedBody` /
// `workspaceRenamedBody` DIRECTLY. A handler arm that re-inlines the literal —
// exactly the shape the defect had — leaves every one of those tests green,
// because none of them ever asks the ROUTE what it answers.
//
// So this test drives POST /v1/workspaces and PATCH /v1/workspaces/{id} through
// the real route table, once per flag value:
//
//   - JOMFORM_SELF_HOSTED on → `url` is the install's own origin and `url` /
//     `previous_url` never carry a `.jomform.com` host. A slug rename moves no
//     address there (one origin serves every workspace), so it must not claim
//     a former URL that still redirects.
//   - flag off → byte-identical to the hosted product:
//     `https://<slug>.jomform.com`, and a slug rename's `previous_url` is the
//     OLD `https://<old-slug>.jomform.com` with the redirect promise intact.
//
// BOTH create arms are exercised on purpose: the ask-first arm
// (`CreateForAccount`, the path every new signup takes) and the general arm
// (`Workspaces.Create`). They were two separate literals in the defect, and a
// test that covered only one would not have caught the other.
//
// Skipped without the test database (and Valkey, for the session).
import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// wsURLCase is one deployment shape. `wantURL` is the address the payload must
// carry for a given slug, spelled out per branch so neither can absorb the
// other's expectation.
type wsURLCase struct {
	name       string
	selfHosted bool
	baseURL    string
	wantURL    func(slug string) string
}

func wsURLCases() []wsURLCase {
	return []wsURLCase{
		{
			name:       "self-hosted",
			selfHosted: true,
			baseURL:    "http://localhost:18083",
			wantURL:    func(string) string { return "http://localhost:18083/" },
		},
		{
			name:       "hosted",
			selfHosted: false,
			baseURL:    "https://jomform.com",
			wantURL:    func(slug string) string { return "https://" + slug + ".jomform.com" },
		},
	}
}

type wsURLCreateOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

// TestRESTWorkspaceCreateURLFollowsTheFlag asserts the wire answer of
// POST /v1/workspaces on both create arms, on both deployment shapes.
func TestRESTWorkspaceCreateURLFollowsTheFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace-url test in -short mode")
	}
	pool := bundleTestPool(t)
	sess := authTestValkey(t)

	for _, tc := range wsURLCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := trashRESTFixture(t, pool)
			h.Sessions = sess
			h.Users = dbstore.NewUserStore(pool)
			h.SelfHosted = tc.selfHosted
			h.BaseURL = tc.baseURL

			// A user WITH an account and NO workspace: the ask-first state, so
			// the first POST below takes the CreateForAccount arm.
			token := accountHoldingUser(t, h, pool, "wsurl-"+tc.name)

			// ── create arm 1: ask-first (first workspace) ─────────────────────
			firstSlug := "wsurl-first-" + runKey()
			rec := sessionReq(t, h, token, http.MethodPost, "/v1/workspaces",
				`{"name":"URL Probe First","slug":"`+firstSlug+`"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST /v1/workspaces (first/%s) = %d, want 201 (body %s)",
					tc.name, rec.Code, rec.Body.String())
			}
			var first wsURLCreateOut
			if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
				t.Fatalf("decode create response %s: %v", rec.Body.String(), err)
			}
			if want := tc.wantURL(firstSlug); first.URL != want {
				t.Errorf("first-workspace create url = %q, want %q (body %s) — this is the arm every "+
					"new signup takes", first.URL, want, rec.Body.String())
			}
			if tc.selfHosted && strings.Contains(rec.Body.String(), "jomform.com") {
				t.Errorf("a self-hosted install answered the first-workspace create with a host it does "+
					"not serve: %s", rec.Body.String())
			}
			if first.ID == "" || first.Slug != firstSlug {
				t.Errorf("first-workspace create payload lost id/slug: %s", rec.Body.String())
			}

			// ── create arm 2: general (the caller already has a workspace) ────
			secondSlug := "wsurl-second-" + runKey()
			rec = sessionReq(t, h, token, http.MethodPost, "/v1/workspaces",
				`{"name":"URL Probe Second","slug":"`+secondSlug+`"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST /v1/workspaces (second/%s) = %d, want 201 (body %s)",
					tc.name, rec.Code, rec.Body.String())
			}
			var second wsURLCreateOut
			if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
				t.Fatalf("decode second create response %s: %v", rec.Body.String(), err)
			}
			if second.ID == first.ID {
				t.Fatalf("the second POST reused the ask-first workspace (%s): the general arm was "+
					"never exercised, so this test would not guard it", first.ID)
			}
			if want := tc.wantURL(secondSlug); second.URL != want {
				t.Errorf("second-workspace create url = %q, want %q (body %s)", second.URL, want, rec.Body.String())
			}
			if tc.selfHosted && strings.Contains(rec.Body.String(), "jomform.com") {
				t.Errorf("a self-hosted install answered the second-workspace create with a host it does "+
					"not serve: %s", rec.Body.String())
			}
		})
	}
}

// TestRESTWorkspaceRenameURLFollowsTheFlag asserts the wire answer of
// PATCH /v1/workspaces/{id} on a SLUG change — the arm that shipped
// `previous_url` from its own copy of the hosted literal.
func TestRESTWorkspaceRenameURLFollowsTheFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace-url test in -short mode")
	}
	pool := bundleTestPool(t)
	sess := authTestValkey(t)

	for _, tc := range wsURLCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := trashRESTFixture(t, pool)
			h.Sessions = sess
			h.Users = dbstore.NewUserStore(pool)
			h.SelfHosted = tc.selfHosted
			h.BaseURL = tc.baseURL

			token := accountHoldingUser(t, h, pool, "wsurl-ren-"+tc.name)
			oldSlug := "wsurl-old-" + runKey()
			rec := sessionReq(t, h, token, http.MethodPost, "/v1/workspaces",
				`{"name":"URL Probe Rename","slug":"`+oldSlug+`"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST /v1/workspaces (%s) = %d, want 201 (body %s)", tc.name, rec.Code, rec.Body.String())
			}
			var created wsURLCreateOut
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatalf("decode create response %s: %v", rec.Body.String(), err)
			}

			newSlug := "wsurl-new-" + runKey()
			rec = sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+created.ID,
				`{"slug":"`+newSlug+`"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("PATCH /v1/workspaces/{id} (%s) = %d, want 200 (body %s)",
					tc.name, rec.Code, rec.Body.String())
			}
			// Decoded into a MAP so an OMITTED key and an empty one are
			// distinguishable — "omit previous_url" and "previous_url: ''" are
			// different answers, and only one of them is what the self-hosted
			// branch promises.
			var out map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode rename response %s: %v", rec.Body.String(), err)
			}
			if got, _ := out["url"].(string); got != tc.wantURL(newSlug) {
				t.Errorf("rename url = %q, want %q (body %s)", got, tc.wantURL(newSlug), rec.Body.String())
			}
			if got, _ := out["slug"].(string); got != newSlug {
				t.Errorf("rename did not store the new slug: %q (body %s)", got, rec.Body.String())
			}

			if tc.selfHosted {
				if _, ok := out["previous_url"]; ok {
					t.Errorf("a self-hosted delete-free slug rename answered previous_url=%v — one origin "+
						"serves every workspace, so no address moved and no old address redirects",
						out["previous_url"])
				}
				if _, ok := out["previous_url_redirects"]; ok {
					t.Errorf("a self-hosted slug rename claimed previous_url_redirects although its address "+
						"cannot move (body %s)", rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), "jomform.com") {
					t.Errorf("a self-hosted install answered the rename with a host it does not serve: %s",
						rec.Body.String())
				}
				return
			}

			// HOSTED: the address really moved, and the old one keeps working.
			if got, _ := out["previous_url"].(string); got != tc.wantURL(oldSlug) {
				t.Errorf("hosted rename previous_url = %q, want %q (body %s)", got, tc.wantURL(oldSlug), rec.Body.String())
			}
			if redirects, _ := out["previous_url_redirects"].(bool); !redirects {
				t.Errorf("hosted rename lost previous_url_redirects (body %s)", rec.Body.String())
			}
		})
	}
}
