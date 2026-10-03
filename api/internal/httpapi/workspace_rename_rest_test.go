// Rename a workspace's name and/or its public slug, over the REAL REST route
// table.
//
// The store behaviour (transaction, alias, reservation, refusals) is pinned in
// dbstore's workspace_rename_test.go, and the MCP surface in mcpsrv's
// workspace_update_mcp_test.go. THIS file covers the third transport, where the
// risks are the ones only the route table can show:
//
//   - the route itself: PATCH /v1/workspaces/{id} is a method+pattern pair the
//     mux must actually match (a wrong verb or a shadowed literal is invisible
//     to a handler unit test);
//   - TWO DIFFERENT AUTHORITIES on one endpoint: a manage/admin member may
//     rename the NAME but must NOT be able to move the public SLUG;
//   - the OMITTED-FIELD contract: "slug only" must not blank the name, and vice
//     versa;
//   - the redirect a rename promises: the OLD host must still answer, and it
//     must answer with a redirect to the NEW one — proven by driving a real
//     request with the old host on it, not by reading the alias table (the test
//     the card explicitly asks for).
//
// Both workspace routes resolve the caller with sessionOwnerID, so every request
// here carries a real session cookie issued by the real session store.
//
// Skipped without the test database.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// renameRESTFixture seeds a user with one workspace and a session cookie, and
// returns the handlers, the workspace and the cookie value.
func renameRESTFixture(t *testing.T, tag string) (*RESTHandlers, *dbstore.Workspace, string) {
	t.Helper()
	pool := bundleTestPool(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)

	ctx := context.Background()
	userID := mkWsUser(t, pool, "rest-rename-"+tag)
	sp := dbstore.NewWorkspaceStore(pool)
	ws, err := sp.Create(ctx, userID, "Rename "+tag, "rest-rename-"+tag+"-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return h, ws, token
}

type renameOut struct {
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	URL              string `json:"url"`
	NameChanged      bool   `json:"name_changed"`
	SlugChanged      bool   `json:"slug_changed"`
	PreviousSlug     string `json:"previous_slug"`
	PreviousURL      string `json:"previous_url"`
	PreviousRedirect bool   `json:"previous_url_redirects"`
	Error            string `json:"error"`
}

// accountHoldingUser creates a user WITH an account (and therefore a workspace
// account context for resolve()) and returns a session cookie for them.
//
// WHY THIS EXISTS. A bare user row cannot drive the authed route table: resolve()
// resolves the session's account and returns 401 "auth diperlukan" when there is
// none. A test that used a bare user would see 401 for EVERY input and would
// therefore pass every "this invalid slug must be refused" case for entirely the
// wrong reason.
func accountHoldingUser(t *testing.T, h *RESTHandlers, pool *pgxpool.Pool, tag string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1,'it') RETURNING id`,
		"acct-"+tag+"-"+t.Name()[:6]+"-"+runKey()+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug) VALUES ($1,'probe','probe-'||md5(random()::text))`,
		userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	tok, err := h.Sessions.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return tok
}

// TestRESTRenameWorkspaceNameOnly proves requirement 1: renaming the NAME alone
// returns 200, changes the name, and leaves the slug — and therefore every
// public URL derived from it — byte-for-byte identical.
func TestRESTRenameWorkspaceNameOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	h, ws, token := renameRESTFixture(t, "name")

	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"name":"Kedai Bunga Baru"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /v1/workspaces/{id} (name only) = %d (%s)", rec.Code, rec.Body.String())
	}
	var out renameOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %s: %v", rec.Body.String(), err)
	}
	if out.Name != "Kedai Bunga Baru" {
		t.Errorf("response name = %q, want the new name", out.Name)
	}
	if out.Slug != ws.Slug {
		t.Errorf("a NAME rename moved the slug: %q → %q (the public URL must not move)", ws.Slug, out.Slug)
	}
	if !out.NameChanged || out.SlugChanged {
		t.Errorf("flags = name_changed:%v slug_changed:%v, want true/false", out.NameChanged, out.SlugChanged)
	}
	if !strings.HasSuffix(out.URL, ws.Slug+".jomform.com") {
		t.Errorf("url = %q, want it built from the UNCHANGED slug %q", out.URL, ws.Slug)
	}
	// A name-only rename must not advertise a URL move: no address changed.
	if out.PreviousURL != "" || out.PreviousRedirect || out.PreviousSlug != ws.Slug {
		t.Errorf("a name-only rename advertised a URL move: previous_slug=%q previous_url=%q redirects=%v",
			out.PreviousSlug, out.PreviousURL, out.PreviousRedirect)
	}

	// The ROW, not just the response: a 200 that wrote nothing must fail.
	got, err := h.Workspaces.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Name != "Kedai Bunga Baru" || got.Slug != ws.Slug {
		t.Fatalf("row after name rename = name:%q slug:%q, want name changed and slug %q",
			got.Name, got.Slug, ws.Slug)
	}
}

// TestRESTRenameSlugAndOldHostStillAnswers is the test the card asks for by
// name: after a slug rename, a request on the OLD host must be answered, and the
// answer must move the caller to the NEW host.
//
// It deliberately drives a real HTTP request with the old host on it. Asserting
// "the alias row exists" would pass even if nothing ever resolved that host —
// which is exactly the failure the merchant would experience (a printed QR stand
// 404s and nobody can see why).
func TestRESTRenameSlugAndOldHostStillAnswers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "slug")
	oldSlug := ws.Slug
	newSlug := "rest-renamed-" + ws.ID[:6]

	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"`+newSlug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /v1/workspaces/{id} (slug only) = %d (%s)", rec.Code, rec.Body.String())
	}
	var out renameOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !out.SlugChanged || out.Slug != newSlug || out.PreviousSlug != oldSlug {
		t.Fatalf("slug rename response = %s, want slug %q with previous_slug %q",
			rec.Body.String(), newSlug, oldSlug)
	}
	// OMITTED FIELD CONTRACT: a slug-only request must not blank the name.
	if out.Name != ws.Name {
		t.Errorf("a slug-only rename changed the name: %q → %q (empty means KEEP)", ws.Name, out.Name)
	}
	// The promise the merchant must be told: the old address still works.
	if out.PreviousURL != "https://"+oldSlug+".jomform.com" || !out.PreviousRedirect {
		t.Errorf("response did not promise the old URL keeps working: previous_url=%q redirects=%v",
			out.PreviousURL, out.PreviousRedirect)
	}

	// The row moved.
	got, err := h.Workspaces.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Slug != newSlug || got.Name != ws.Name {
		t.Fatalf("row = slug:%q name:%q, want %q / %q", got.Slug, got.Name, newSlug, ws.Name)
	}

	// THE PROOF: a real request on the OLD host is answered with a permanent
	// redirect to the NEW one, path and query preserved (so a specific form, a
	// table number or an ?aff= code is not lost).
	//
	// Driven through the WHOLE server Handler(), not a hand-built mux, because
	// the half of this feature that is easiest to get wrong is the WIRING: a
	// correct wrapper that nothing wraps is invisible to a unit test of the
	// wrapper. Handler() is what production serves.
	//
	// The server is built with Env="dev" because the redirect's SCHEME follows
	// the same dev/prod switch the cookie flag does (http on a dev host, https in
	// production); asserting https here would test the config, not the move.
	srv := newTestServer()
	srv.deps.Public = &PublicHandlers{Workspaces: dbstore.NewWorkspaceStore(pool)}
	wrapped := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/f/harga?aff=ALI", nil)
	req.Host = oldSlug + ".jomform.com"
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("a request on the FORMER host %s.jomform.com = %d, want 301 — the printed "+
			"QR/iframe/affiliate links name that host and must not 404", oldSlug, rr.Code)
	}
	want := "http://" + newSlug + ".jomform.com/f/harga?aff=ALI"
	if loc := rr.Header().Get("Location"); loc != want {
		t.Fatalf("redirect Location = %q, want %q (path and query must survive the move)", loc, want)
	}

	// The cases that must NOT be redirected. Each one is a host that belongs to
	// somebody else, and redirecting it would move a stranger's traffic.
	for _, tc := range []struct {
		why  string
		host string
	}{
		{"the CURRENT host would redirect to itself — an infinite loop", newSlug + ".jomform.com"},
		{"an unknown slug must stay a 404, not become a redirect to nowhere", "it-never-existed.jomform.com"},
		{"the apex is the product, not a tenant", "jomform.com"},
		{"www is not a tenant", "www.jomform.com"},
		{"the MCP host is the product (issue #140)", "mcp.jomform.com"},
		{"a custom domain is not a workspace subdomain (issue #323)", "forms.kedaibunga.com"},
		{"a localhost dev host has no subdomain at all", "localhost:3399"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/f/harga", nil)
		req.Host = tc.host
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		if rr.Code == http.StatusMovedPermanently {
			t.Errorf("%s was redirected to %q — %s", tc.host, rr.Header().Get("Location"), tc.why)
		}
	}
}

// TestRESTRenameSlugTakenLeavesBothSidesUntouched proves requirement 3: a slug
// another workspace holds is a 409, and NEITHER workspace changes — a refusal
// must not be a partial write.
func TestRESTRenameSlugTakenLeavesBothSidesUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "taken")
	sp := dbstore.NewWorkspaceStore(pool)

	other, err := sp.Create(ctx, ws.Owner, "Other", "rest-other-"+ws.ID[:6])
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"`+other.Slug+`","name":"Should Not Apply"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("renaming onto a slug another workspace holds = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// Neither side moved — including the NAME that rode in the same request.
	mine, err := sp.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID mine: %v", err)
	}
	if mine.Slug != ws.Slug || mine.Name != ws.Name {
		t.Errorf("the refused rename changed the refused workspace: slug:%q name:%q (want %q / %q)",
			mine.Slug, mine.Name, ws.Slug, ws.Name)
	}
	theirs, err := sp.ByID(ctx, other.ID)
	if err != nil {
		t.Fatalf("ByID other: %v", err)
	}
	if theirs.Slug != other.Slug {
		t.Errorf("the refused rename moved the OTHER workspace: %q → %q", other.Slug, theirs.Slug)
	}

	// And the RESERVATION half: a slug the other workspace has RENAMED AWAY FROM
	// is refused too, so a third workspace cannot hijack the printed links that
	// still point at it.
	vacated, err := sp.Rename(ctx, dbstore.WorkspaceRenameInput{
		WorkspaceID: other.ID, Slug: "rest-vacated-" + ws.ID[:6], CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("rename other to vacate a slug: %v", err)
	}
	rec = sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"`+vacated.OldSlug+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("claiming another workspace's FORMER slug = %d, want 409 — that address still resolves "+
			"to them and taking it would hijack their printed links (%s)", rec.Code, rec.Body.String())
	}
	mine, err = sp.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID mine: %v", err)
	}
	if mine.Slug != ws.Slug {
		t.Errorf("the refused reservation claim moved the slug anyway: %q → %q", ws.Slug, mine.Slug)
	}
}

// TestRESTRenameInvalidSlugsAreRefusedByTheSharedRule proves requirement 4 on the
// wire, and pins the boundary create and rename must agree on.
//
// The SAME inputs go through create and rename in the same test, and the two
// verdicts are asserted equal. A rule that was merely duplicated (or later
// drifts) fails here naming the exact input; a test that exercised only one path
// would not notice.
func TestRESTRenameInvalidSlugsAreRefusedByTheSharedRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "invalid")
	sp := dbstore.NewWorkspaceStore(pool)

	for _, tc := range []struct {
		name string
		raw  string
		ok   bool
	}{
		{"too short", "ab", false},
		{"uppercase A-B is normalised, so it is fine", "A-B", true},
		{"underscore", "xy_z", false},
		{"space", "ab cd", false},
		{"reserved suffix jomform", "xyjomform", false},
		{"reserved suffix camel case", "fooJomform", false},
		{"reserved suffix after dash", "xy-jomform", false},
		{"unicode", "café", false},
		{"too long", strings.Repeat("a", 41), false},
		{"valid", "kedai-bunga", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// CREATE, through the real route. The caller needs an ACCOUNT, not
			// just a user: resolve() resolves the session's account and refuses
			// the request without one, so a bare user would 401 and every
			// "expected to fail" case would pass for the wrong reason.
			cToken := accountHoldingUser(t, h, pool, "rule-create-"+strings.ReplaceAll(tc.name, " ", "-"))
			recC := sessionReq(t, h, cToken, http.MethodPost, "/v1/workspaces",
				`{"name":"create probe","slug":"`+tc.raw+`"}`)
			createOK := recC.Code == http.StatusCreated
			var storedSlug, createdID string
			if createOK {
				var co struct {
					ID   string `json:"id"`
					Slug string `json:"slug"`
				}
				_ = json.Unmarshal(recC.Body.Bytes(), &co)
				storedSlug, createdID = co.Slug, co.ID
				// FREE THE SLUG AGAIN before the rename leg, because the point of
				// this test is that BOTH paths are given the SAME input: a slug is
				// unique per DATABASE, so leaving the create's workspace in place
				// would make the rename fail as "taken" — a 409 about collision,
				// not the rule, and the comparison below would be meaningless.
				// (Renaming it away would not help: that reserves the slug as an
				// alias, which is itself a refusal.)
				if _, err := pool.Exec(ctx, `DELETE FROM accounts WHERE workspace_id=$1`, createdID); err != nil {
					t.Fatalf("free the created workspace's account: %v", err)
				}
				// The rename leg RESERVES the create's slug as an alias, so pass 2
				// finds it taken unless the alias goes too (card t_5299cae1).
				if _, err := pool.Exec(ctx,
					`DELETE FROM workspace_slug_aliases WHERE workspace_id=$1 OR slug=$2`,
					createdID, tc.raw); err != nil {
					t.Fatalf("free the created workspace's slug alias: %v", err)
				}
				if _, err := pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, createdID); err != nil {
					t.Fatalf("free the created workspace: %v", err)
				}
			}

			// RENAME, on a workspace of its own. The slug must be unique per
			// SUBTEST, not per parent: subtests run sequentially here, but a
			// repeated probe slug would collide with the previous one and make
			// this fail for the wrong reason.
			// The slug varies per RUN as well as per subtest: `t.Name()` and the
			// case name are identical on every `-test.count` pass, so a
			// name-derived slug collides on pass 2 (card t_5299cae1).
			probeSlug := "rule-probe-" + strings.ReplaceAll(strings.ToLower(tc.name), " ", "-") + "-" + runKey()
			if len(probeSlug) > 40 {
				probeSlug = "probe-" + runKey()
			}
			probe, err := sp.Create(ctx, ws.Owner, "rule probe", probeSlug)
			if err != nil {
				t.Fatalf("create probe workspace: %v", err)
			}
			recR := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+probe.ID,
				`{"slug":"`+tc.raw+`"}`)
			renameOK := recR.Code == http.StatusOK

			if createOK != tc.ok {
				t.Errorf("create %q = %d (ok=%v), want ok=%v — %s", tc.raw, recC.Code, createOK, tc.ok, recC.Body.String())
			}
			if renameOK != tc.ok {
				t.Errorf("rename %q = %d (ok=%v), want ok=%v — %s", tc.raw, recR.Code, renameOK, tc.ok, recR.Body.String())
			}
			// THE SHARED-RULE ASSERTION.
			if createOK != renameOK {
				t.Errorf("create and rename DISAGREE about %q: create ok=%v (HTTP %d) rename ok=%v (HTTP %d) — "+
					"the slug rule is not one rule", tc.raw, createOK, recC.Code, renameOK, recR.Code)
			}
			if !tc.ok {
				// An INVALID input is a 400 (the value is wrong), never a 409
				// (the value is fine but taken) and never a 500.
				if recR.Code != http.StatusBadRequest {
					t.Errorf("rename %q = %d, want 400", tc.raw, recR.Code)
				}
				after, err := sp.ByID(ctx, probe.ID)
				if err != nil {
					t.Fatalf("ByID probe: %v", err)
				}
				if after.Slug != probe.Slug {
					t.Errorf("a refused rename moved the slug anyway: %q → %q", probe.Slug, after.Slug)
				}
			}
			// The STORED value is the normalised one, on the create path (the
			// one place this test can read what was written without a second
			// lookup).
			if tc.ok && tc.raw == "A-B" && storedSlug != "a-b" {
				t.Errorf("create stored %q for input %q, want the normalised %q", storedSlug, tc.raw, "a-b")
			}
			// FREE THE PROBE before the next subtest (card t_5299cae1). The
			// create/rename legs reserve the slug they took as an ALIAS, and
			// subtests share a database: leaving the probe behind makes a later
			// subtest — or pass 2 of this one — see "slugTaken" for a value that
			// is not the thing under test.
			if _, err := pool.Exec(ctx,
				`DELETE FROM workspace_slug_aliases WHERE workspace_id=$1`, probe.ID); err != nil {
				t.Fatalf("free probe slug alias: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM accounts WHERE workspace_id=$1`, probe.ID); err != nil {
				t.Fatalf("free probe account: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, probe.ID); err != nil {
				t.Fatalf("free probe workspace: %v", err)
			}
		})
	}
}

// TestRESTRenameSlugRequiresOwnership proves requirement 5 on the wire: a
// manage/admin MEMBER may rename the workspace (a label their colleagues see)
// but may NOT move the public slug. Both halves are asserted in one test, so the
// permission cannot be "fixed" by denying the member everything.
func TestRESTRenameSlugRequiresOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, ownerToken := renameRESTFixture(t, "ownership")

	// The member: a manage-role member of the target, with a workspace of their
	// own so they are an ordinary user, and a session of their own.
	memberID := mkWsUser(t, pool, "rest-rename-member")
	sp := dbstore.NewWorkspaceStore(pool)
	if _, err := sp.Create(ctx, memberID, "Member Own", "rest-mem-own-"+memberID[:6]); err != nil {
		t.Fatalf("create member workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'manage')`,
		ws.ID, memberID); err != nil {
		t.Fatalf("add manage member: %v", err)
	}
	memberToken, err := h.Sessions.Create(ctx, memberID)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	// SLUG as a member: 403, and nothing changed.
	rec := sessionReq(t, h, memberToken, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"member-moved-this"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a manage member moved the public slug: %d (%s), want 403", rec.Code, rec.Body.String())
	}
	after, err := sp.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if after.Slug != ws.Slug {
		t.Fatalf("the refused slug rename moved the slug anyway: %q → %q", ws.Slug, after.Slug)
	}

	// NAME as the same member: allowed, on the same endpoint.
	rec = sessionReq(t, h, memberToken, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"name":"Renamed By Member"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a manage member could not rename the NAME: %d (%s) — the label is not the public identity",
			rec.Code, rec.Body.String())
	}
	after, err = sp.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if after.Name != "Renamed By Member" || after.Slug != ws.Slug {
		t.Fatalf("row = name:%q slug:%q, want the member's name and the UNCHANGED slug %q",
			after.Name, after.Slug, ws.Slug)
	}

	// The OWNER moves the slug on the same workspace — the control that shows the
	// 403 above is about ownership and not about a broken endpoint.
	// The target slug varies per RUN (card t_5299cae1): a fixed literal is stored
	// by pass 1 and pass 2's workspace then cannot take it, which reads as a 409
	// about collision rather than the ownership control this leg is testing.
	ownerTargetSlug := "owner-moved-" + runKey()
	rec = sessionReq(t, h, ownerToken, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"`+ownerTargetSlug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("the OWNER could not move the slug: %d (%s)", rec.Code, rec.Body.String())
	}
	after, err = sp.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if after.Slug != ownerTargetSlug {
		t.Fatalf("owner slug rename stored %q, want %q", after.Slug, ownerTargetSlug)
	}
}

// TestRESTRenameRefusedForReadonlyKey pins the scope axis (issue #645) on the
// new route. The route is behind the ordinary authed() wrapper, so a Bearer key
// DOES reach it — and must be stopped by the write-scope gate, with a readonly
// key refused before the handler touches anything.
//
// This is asserted from the readonly side because that is the side with a real
// risk: a readonly key that could rename a workspace would be moving a tenant's
// public address with a credential that is not allowed to change a form title.
func TestRESTRenameRefusedForReadonlyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, ws, _ := renameRESTFixture(t, "scope-ro")
	_, accountID, _ := trashRESTWorkspaceAccount(t, pool, "rename-ro")

	rh, key := scopeRESTFixture(t, pool, accountID, "rename-ro", []string{dbstore.ScopeReadonly})
	// Read control: this key authenticates and may READ, so the refusal below is
	// about the scope and not about the key being rejected outright.
	if rec := bearerReq(t, rh, key, http.MethodGet, "/v1/rest/forms", ""); rec.Code == http.StatusUnauthorized {
		t.Fatalf("the readonly key does not authenticate at all (%d) — the assertion below would be vacuous",
			rec.Code)
	}
	for _, body := range []string{`{"name":"readonly tried"}`, `{"slug":"readonly-tried"}`, `{"name":"x","slug":"readonly-tried"}`} {
		rec := bearerReq(t, rh, key, http.MethodPatch, "/v1/workspaces/"+ws.ID, body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("a READONLY key got %d on PATCH /v1/workspaces/{id} with %s — want 403 (%s)",
				rec.Code, body, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "scope") {
			t.Errorf("refused, but not for the scope reason: %s", rec.Body.String())
		}
	}
	// Nothing moved, on either half.
	sp := dbstore.NewWorkspaceStore(pool)
	after, err := sp.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if after.Name != ws.Name || after.Slug != ws.Slug {
		t.Errorf("a refused readonly rename changed the row: name:%q slug:%q", after.Name, after.Slug)
	}
	// And no alias was left behind by a refused call.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_slug_aliases WHERE slug=$1`, ws.Slug).Scan(&n); err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if n != 0 {
		t.Errorf("a REFUSED rename reserved the live slug %q as an alias", ws.Slug)
	}
}

// TestRESTRenameNoChangeIsRefused pins that a request asking for the values the
// workspace already has is a 400 rather than a silent 200 that wrote nothing —
// an agent or UI repeating a rename must be told nothing happened.
func TestRESTRenameNoChangeIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	h, ws, token := renameRESTFixture(t, "nochange")

	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"name":"`+ws.Name+`","slug":"`+ws.Slug+`"}`)
	if rec.Code == http.StatusOK {
		t.Errorf("renaming to the CURRENT values returned 200 — an unchanged write must not report success (%s)",
			rec.Body.String())
	}
	// And no empty body is accepted at all.
	rec = sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID, `{}`)
	if rec.Code == http.StatusOK {
		t.Error("PATCH with neither name nor slug returned 200")
	}

	// The ALIAS side-effect must not have happened: an unchanged request must not
	// reserve the workspace's own live slug as a former address.
	pool := bundleTestPool(t)
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM workspace_slug_aliases WHERE slug=$1`, ws.Slug).Scan(&n); err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if n != 0 {
		t.Errorf("an unchanged rename reserved the LIVE slug %q as an alias — a live slug must never also be "+
			"an alias, or the redirect path would shadow the workspace itself", ws.Slug)
	}
}

// TestWorkspaceSlugWireMessagesAreTheStores pins the three literals this package
// hands to writeErr for the workspace-slug rule against the store's own
// sentinels.
//
// WHY. errcode.go's message→code table keys on the EXACT string a handler writes,
// and its guard (TestIssue6177ErrorCodeTableHasNoDeadKeys) requires each key to
// appear as a Go string literal in a non-errcode.go file of this package. An
// `.Error()` call would not satisfy it — so forwarding the store's sentinel text
// verbatim would leave workspace.slugInvalid unreachable and the dashboard would
// render its generic 400 sentence instead of the slug rule ("Slug must be 3-40
// chars, a-z, 0-9, dash.") — issue #6177's failure mode, re-created on this path.
//
// The literals are therefore load-bearing, and the only thing a hand-written
// literal can drift from is the store. This is the same pin the form-slug path
// carries (TestFormSlugInvalidWireMessageMatchesTheStore).
func TestWorkspaceSlugWireMessagesAreTheStores(t *testing.T) {
	for _, c := range []struct {
		wire   string
		stored error
		want   string
	}{
		{workspaceSlugLengthMessage, dbstore.ErrWorkspaceSlugLength, CodeWorkspaceSlugBad},
		{workspaceSlugCharsMessage, dbstore.ErrWorkspaceSlugChars, CodeWorkspaceSlugBad},
		{workspaceSlugTailMessage, dbstore.ErrWorkspaceSlugReserved, CodeWorkspaceSlugBad},
		// The NAME cap travels the same route (both create paths and rename
		// forward the store's sentinel text), so it needs the same pin — but it
		// answers a DIFFERENT code on purpose. The dashboard renders the
		// sentence for the code it receives, so answering the slug code here
		// would tell a merchant with an over-long NAME to go and fix their
		// ADDRESS. Reachable by paste and by assistive technology even though
		// both name fields are capped at 120.
		{dbstore.ErrWorkspaceNameLength.Error(), dbstore.ErrWorkspaceNameLength, CodeWorkspaceNameTooLong},
	} {
		if c.wire != c.stored.Error() {
			t.Errorf("the workspace wire message drifted from the store's sentinel:\n"+
				"  handler writes: %q\n  dbstore says:   %q\n"+
				"The wire message is the key for its entry in errcode.go's table, so a drift "+
				"silently downgrades the 400 to the generic request.badRequest code and the "+
				"console stops rendering the rule.", c.wire, c.stored.Error())
			continue
		}
		// And each message still resolves to the code the console maps.
		if got := ErrCodeFor(c.wire); got != c.want {
			t.Errorf("ErrCodeFor(%q) = %q, want %q", c.wire, got, c.want)
		}
	}
}
