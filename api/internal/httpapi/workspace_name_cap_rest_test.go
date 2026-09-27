// The workspace NAME cap over the REAL REST route table: the unit, the status
// class, and the create/rename symmetry.
//
// The store behaviour is pinned in dbstore's workspace_name_cap_test.go. THIS file
// covers the two things only the wire can show, and both were broken:
//
//  1. THE STATUS CLASS. The cap's refusal was a fresh `errors.New`, so it matched
//     no arm of renameWorkspace's switch, respondErr took its default, and a name
//     that is simply too long reached the merchant as
//     `500 {"error":"internal error"}` — indistinguishable from a real server
//     fault, and with no way for the UI to explain it. The assertions below are
//     on the status CLASS (must be 4xx, must be 400, must carry a stated reason),
//     so an unclassified error can never pass as a 500 again.
//
//  2. THE CREATE/RENAME SYMMETRY. createWorkspace checked only `name == ""` and
//     WorkspaceStore.Create checked nothing, so the cap was enforced on ONE of
//     two write paths — the same asymmetry the shared slug rule was written to
//     remove. The SAME inputs go through create and rename here and the two
//     verdicts are asserted equal, the shape of
//     TestRESTRenameInvalidSlugsAreRefusedByTheSharedRule.
//
// Also here: the mis-timing regression. A slug-only PATCH on a workspace whose
// STORED name is over the old byte cap must reach the alias logic and succeed —
// that is the case the defect made impossible, and it is destructive (the
// merchant cannot move the address at all).
//
// Every request carries a real session cookie issued by the real session store,
// and the legacy over-long name is seeded with SQL because no write path can
// create one any more.
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

	"github.com/wzul/jomform/api/internal/dbstore"
)

// nameCapProbe is the body a PATCH/create returns, for the assertions that care
// about the STORED value rather than the status.
type nameCapProbe struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	URL   string `json:"url"`
	Error string `json:"error"`
	// The machine code the dashboard decides its sentence from. Asserted here
	// because this probe is a REAL request through the real handler: it is the
	// one place that can prove the code reaches the WIRE, rather than only that
	// errcode.go's table would resolve it.
	Code string `json:"code"`
}

// TestRESTRenameWorkspaceNameAtTheCharacterCap is requirement 1/2 on the wire: a
// name at the character cap is accepted and the VALUE is asserted; one character
// over is a 400 with a stated reason — never a 500.
func TestRESTRenameWorkspaceNameAtTheCharacterCap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	h, ws, token := renameRESTFixture(t, "namecap")

	for _, tc := range []struct{ why, name string }{
		{"45 CJK characters = 135 BYTES: the byte check refused this, the character rule accepts it",
			strings.Repeat("店", 45)},
		{"120 accented characters = 240 bytes: `maxLength={120}` in the field counted 120, so the server must too",
			strings.Repeat("é", 120)},
	} {
		rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
			`{"name":`+jsonString(tc.name)+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s\nPATCH (name only) = %d (%s), want 200", tc.why, rec.Code, rec.Body.String())
		}
		var out nameCapProbe
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		if out.Name != tc.name {
			t.Errorf("%s\nresponse name = %q, want the submitted name verbatim", tc.why, out.Name)
		}
		// The ROW, not just the response — a 200 that wrote nothing must fail.
		got, err := h.Workspaces.ByID(ctx, ws.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Name != tc.name {
			t.Errorf("%s\nrow name = %q, want the submitted name — a 200 that stored something else "+
				"(truncated?) is not a pass", tc.why, got.Name)
		}
		if out.Slug != ws.Slug {
			t.Errorf("%s\na name-only rename moved the slug: %q → %q", tc.why, ws.Slug, out.Slug)
		}
	}

	// One character OVER the cap. The status class is the point: this used to be
	// a 500 because nothing classified the error.
	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"name":`+jsonString(strings.Repeat("a", dbstore.WorkspaceNameMaxChars+1))+`}`)
	if rec.Code == http.StatusInternalServerError {
		t.Errorf("a name of %d characters returned 500 — the refusal is not classified, so the merchant "+
			"sees \"internal error\" for a name that is merely too long (%s)",
			dbstore.WorkspaceNameMaxChars+1, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a name of %d characters = %d, want 400 (%s)",
			dbstore.WorkspaceNameMaxChars+1, rec.Code, rec.Body.String())
	}
	var refusal nameCapProbe
	_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
	if !strings.Contains(refusal.Error, "120") {
		t.Errorf("the 400 does not state the limit: %q", refusal.Error)
	}
	// …and it names the refusal with the NAME's own code. The dashboard renders
	// the sentence for the code it receives, so a slug code here would send a
	// merchant with an over-long name to go and fix their ADDRESS — the defect
	// this path's fix exists for, reachable again through paste or assistive
	// technology even though both name fields are capped at 120.
	if refusal.Code != CodeWorkspaceNameTooLong {
		t.Errorf("the over-cap name refusal carries code %q, want %q — the dashboard decides its "+
			"sentence from this code, so anything else reports a NAME problem as an ADDRESS one",
			refusal.Code, CodeWorkspaceNameTooLong)
	}
	// And the too-long name did not land.
	if got, err := h.Workspaces.ByID(ctx, ws.ID); err != nil || got.Name == strings.Repeat("a", dbstore.WorkspaceNameMaxChars+1) {
		t.Errorf("the refused over-long name was written anyway (%q, %v)", got.Name, err)
	}
}

// TestRESTRenameCreateAndRenameAgreeOnTheNameCap is requirement 4: the create path
// must give the SAME verdict as the rename path for the same name. Before the fix
// create accepted anything and rename refused over 120 bytes, so the cap was
// enforced on one of two write paths — invisible to a test that exercised one.
func TestRESTRenameCreateAndRenameAgreeOnTheNameCap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "nameparity")
	sp := dbstore.NewWorkspaceStore(pool)

	for _, tc := range []struct {
		label string
		name  string
		body  string
		ok    bool
	}{
		{"at the cap", "at the cap", strings.Repeat("a", dbstore.WorkspaceNameMaxChars), true},
		{"45 CJK characters (135 bytes, 45 characters)", "cjk45", strings.Repeat("店", 45), true},
		{"over the cap", "over the cap", strings.Repeat("a", dbstore.WorkspaceNameMaxChars+1), false},
		{"41 CJK characters (123 bytes, 41 characters)", "cjk41", strings.Repeat("店", 41), true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			// CREATE, through the real route. The caller needs an ACCOUNT, not
			// just a user: resolve() resolves the session's account and refuses
			// without one, so a bare user would 401 and every "expected to fail"
			// case would pass for the wrong reason.
			cToken := accountHoldingUser(t, h, pool, "nameparity-create-"+tc.name)
			probeSlug := "namecap-" + strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(tc.name, " ", "-"), ",", ""))
			if len(probeSlug) > 40 {
				probeSlug = probeSlug[:40]
			}
			recC := sessionReq(t, h, cToken, http.MethodPost, "/v1/workspaces",
				`{"name":`+jsonString(tc.body)+`,"slug":"`+probeSlug+`"}`)
			createOK := recC.Code == http.StatusCreated
			if createOK {
				// Free the workspace (and its slug) before the rename leg: the
				// point is that both paths are given the SAME input, and a slug
				// is unique per database, so leaving it behind would make the
				// rename fail as "taken" — a 409 about collision, not the rule.
				var created struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(recC.Body.Bytes(), &created)
				if _, err := pool.Exec(ctx, `DELETE FROM accounts WHERE workspace_id=$1`, created.ID); err != nil {
					t.Fatalf("free the created workspace's account: %v", err)
				}
				if _, err := pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, created.ID); err != nil {
					t.Fatalf("free the created workspace: %v", err)
				}
			}

			// RENAME, on a workspace of its own (unique slug per subtest).
			probe, err := sp.Create(ctx, ws.Owner, "name parity probe",
				"namecap-ren-"+strings.ToLower(strings.ReplaceAll(tc.label, " ", "")))
			if err != nil {
				t.Fatalf("create probe workspace: %v", err)
			}
			recR := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+probe.ID,
				`{"name":`+jsonString(tc.body)+`}`)
			renameOK := recR.Code == http.StatusOK

			if createOK != tc.ok {
				t.Errorf("create with a %d-character name = %d (ok=%v), want ok=%v — %s",
					len([]rune(tc.body)), recC.Code, createOK, tc.ok, recC.Body.String())
			}
			if renameOK != tc.ok {
				t.Errorf("rename with a %d-character name = %d (ok=%v), want ok=%v — %s",
					len([]rune(tc.body)), recR.Code, renameOK, tc.ok, recR.Body.String())
			}
			// THE SHARED-RULE ASSERTION. The REASON is asserted too, not just the
			// verdict: a create path that refuses an over-long name as "name dan
			// slug wajib" (because the rule returns "" with its error, and the
			// emptiness check ran first) has the same status as rename while
			// disagreeing about WHY — which is how this test passed while the
			// create path reported the wrong reason.
			if createOK != renameOK {
				t.Errorf("create and rename DISAGREE about a %d-character name: create ok=%v (HTTP %d) "+
					"rename ok=%v (HTTP %d) — the name rule is not one rule",
					len([]rune(tc.body)), createOK, recC.Code, renameOK, recR.Code)
			}
			if tc.ok {
				var cOut, rOut nameCapProbe
				_ = json.Unmarshal(recC.Body.Bytes(), &cOut)
				_ = json.Unmarshal(recR.Body.Bytes(), &rOut)
				if cOut.Name != tc.body {
					t.Errorf("create stored name %q, want the submitted name verbatim (%d chars, %d bytes)",
						cOut.Name, len([]rune(tc.body)), len(tc.body))
				}
				if rOut.Name != tc.body {
					t.Errorf("rename stored name %q, want the submitted name verbatim", rOut.Name)
				}
			} else {
				var cOut, rOut nameCapProbe
				_ = json.Unmarshal(recC.Body.Bytes(), &cOut)
				_ = json.Unmarshal(recR.Body.Bytes(), &rOut)
				want := dbstore.ErrWorkspaceNameLength.Error()
				if cOut.Error != want {
					t.Errorf("create refused a %d-character name with %q, want the name rule's own reason %q "+
						"— reporting it as a missing name is the wrong reason, and no status-class check "+
						"can see the difference", len([]rune(tc.body)), cOut.Error, want)
				}
				if rOut.Error != want {
					t.Errorf("rename refused a %d-character name with %q, want %q",
						len([]rune(tc.body)), rOut.Error, want)
				}
				// An over-long name is a 400 (the value is wrong) and never a
				// 500 (nothing is broken) and never a 201/200 (it was written).
				if recR.Code == http.StatusInternalServerError {
					t.Errorf("rename = 500 for an over-long name — an unclassified error reached the "+
						"default arm (%s)", recR.Body.String())
				}
				if recR.Code != http.StatusBadRequest {
					t.Errorf("rename = %d, want 400", recR.Code)
				}
				if after, err := sp.ByID(ctx, probe.ID); err != nil || after.Name != probe.Name {
					t.Errorf("a refused over-long rename wrote the name anyway: %q (%v)", after.Name, err)
				}
			}
		})
	}
}

// TestRESTRenameSlugOnlySurvivesALegacyOverLongStoredName is requirement 3, the
// destructive half: a workspace whose STORED name is over the old byte cap must
// still be able to move its slug. The omitted name fell back to the stored one,
// which was then length-checked, so the whole request was refused as too long —
// the merchant could not change the address AT ALL, and the slug-only PATCH is
// the primary use of this route.
func TestRESTRenameSlugOnlySurvivesALegacyOverLongStoredName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST rename test in -short mode")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, ws, token := renameRESTFixture(t, "legacy-longname")
	oldSlug := ws.Slug
	newSlug := "rest-legacylong-" + ws.ID[:6]

	// Seeded with SQL: no write path can produce this row any more (that is the
	// create-side cap this change adds), but rows created by the build that had
	// no create-side check DO exist, and the rename route must not be poisoned
	// by one.
	stored := strings.Repeat("店", 200)
	if _, err := pool.Exec(ctx, `UPDATE workspaces SET name=$2 WHERE id=$1`, ws.ID, stored); err != nil {
		t.Fatalf("seed the legacy over-long name: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodPatch, "/v1/workspaces/"+ws.ID,
		`{"slug":"`+newSlug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a slug-only PATCH on a workspace whose stored name is over the cap = %d (%s), want 200 — "+
			"the cap is for a name the caller SUPPLIES, and this request supplies none",
			rec.Code, rec.Body.String())
	}
	var out nameCapProbe
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if out.Slug != newSlug {
		t.Fatalf("slug = %q, want %q", out.Slug, newSlug)
	}
	if out.Name != stored {
		t.Errorf("the stored name was rewritten by a slug-only rename: %q", out.Name)
	}

	// The row moved AND the alias landed.
	got, err := h.Workspaces.ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Slug != newSlug || got.Name != stored {
		t.Fatalf("row = slug:%q name:%q, want %q / the stored name", got.Slug, got.Name, newSlug)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_slug_aliases WHERE slug=$1 AND workspace_id=$2`,
		oldSlug, ws.ID).Scan(&n); err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if n != 1 {
		t.Errorf("no alias row for the former slug %q — every URL already in the wild would 404", oldSlug)
	}

	// THE PROOF a merchant would notice: the former host still answers, and it
	// moves the caller to the new one with path and query intact.
	srv := newTestServer()
	srv.deps.Public = &PublicHandlers{Workspaces: dbstore.NewWorkspaceStore(pool)}
	wrapped := srv.Handler()
	req := sessionlessReq(http.MethodGet, "/f/harga?aff=ALI", oldSlug+".jomform.com")
	rr := newRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("the FORMER host of a long-named workspace = %d, want 301", rr.Code)
	}
	if want := "http://" + newSlug + ".jomform.com/f/harga?aff=ALI"; rr.Header().Get("Location") != want {
		t.Errorf("redirect Location = %q, want %q", rr.Header().Get("Location"), want)
	}
}

// ── helpers local to this file ───────────────────────────────────────────────
// (jsonString already exists in this package, in csv_injection_issue526_test.go,
// and is used above rather than redeclared.)

// sessionlessReq and newRecorder keep the alias-redirect leg below readable: the
// request carries a HOST and no session, because the redirect must work for an
// anonymous visitor following a printed QR code.
func sessionlessReq(method, path, host string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	return req
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
