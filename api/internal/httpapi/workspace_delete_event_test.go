// The `workspace.deleted` audit event must LAND, on the request shape the
// dashboard actually makes.
//
// The bug this pins: recordEvent attributes an event to the caller's RESOLVED
// account (rest_events.go), and events.account_id is
// `REFERENCES accounts(id) ON DELETE CASCADE`. deleteWorkspace deleted the
// workspace (and therefore its account) and only THEN recorded the event — so
// whenever the resolved account belonged to the workspace being destroyed, the
// INSERT hit `events_account_id_fkey` (SQLSTATE 23503), recordEvent swallowed it
// as best-effort, and the DELETE still returned 200. Zero rows, no error.
//
// The dashboard always hits it: WorkspaceTab.tsx issues
// `DELETE /api/workspaces/{id}` with NO `?workspace=` scope, and an unscoped
// session request resolves to `Accounts.ByOwner(userID)[0]` — the FIRST-created
// account. So for a two-workspace owner the Danger zone on their first
// workspace writes the event against the row it just deleted.
//
// The test therefore drives the UNSCALED shape with a two-workspace owner whose
// accs[0] account belongs to the doomed workspace, and asserts on the events
// ROW — a 200 that wrote nothing fails here. The scoped case is kept as a
// CONTROL on the same fixture: it passed before the fix, so it proves the
// fixture can produce the event at all, and that the assertion is not vacuous.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// wsEventLogs routes the default slog logger into a buffer for ONE test, so the
// audit path's log lines can be asserted on. Restored by t.Cleanup. No test in
// this package calls t.Parallel, so the global logger swap cannot race.
func wsEventLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// wsDeleteEventFixture wires the delete route plus the event store, and creates
// a two-workspace owner. It returns the handlers, a session token, and both
// workspaces with the account each one owns — the caller needs the account ids
// to know which account an unscoped request will resolve to.
func wsDeleteEventFixture(t *testing.T, tag string) (
	h *RESTHandlers, token string, doomed, survivor *dbstore.Workspace, doomedAccID, survivorAccID string,
) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)

	h = trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)

	userID := mkWsUser(t, pool, tag)
	sp := dbstore.NewWorkspaceStore(pool)
	// Created first → Accounts.ByOwner orders by created_at, so this one's
	// account is accs[0] and is what an unscoped request resolves to.
	var err error
	if doomed, err = sp.Create(ctx, userID, "Event WS One", tag+"-one-"+userID[:6]); err != nil {
		t.Fatalf("create workspace 1: %v", err)
	}
	if survivor, err = sp.Create(ctx, userID, "Event WS Two", tag+"-two-"+userID[:6]); err != nil {
		t.Fatalf("create workspace 2: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE workspace_id=$1`, doomed.ID).Scan(&doomedAccID); err != nil {
		t.Fatalf("read first workspace account: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE workspace_id=$1`, survivor.ID).Scan(&survivorAccID); err != nil {
		t.Fatalf("read second workspace account: %v", err)
	}
	if token, err = sess.Create(ctx, userID); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Precondition, asserted rather than assumed: an unscoped request on this
	// fixture resolves to the DOOMED workspace's account. If that ever stops
	// being true the test has stopped testing the defect, so fail loudly here
	// instead of silently passing.
	accs, err := dbstore.NewAccountStore(pool).ByOwner(ctx, userID)
	if err != nil {
		t.Fatalf("ByOwner: %v", err)
	}
	if len(accs) < 2 || accs[0].ID != doomedAccID {
		t.Fatalf("fixture is not the defect shape: ByOwner()[0]=%v want the doomed "+
			"workspace's account %s", accs, doomedAccID)
	}
	return h, token, doomed, survivor, doomedAccID, survivorAccID
}

// wsDeleteEvents counts workspace.deleted rows attributed to accountID and
// returns the properties of the newest one (nil when there is none).
func wsDeleteEvents(t *testing.T, accountID string) (int, map[string]any) {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := testdb.Open(t).QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='workspace.deleted' AND account_id=$1`,
		accountID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n == 0 {
		return 0, nil
	}
	var props map[string]any
	if err := testdb.Open(t).QueryRow(ctx,
		`SELECT properties FROM events WHERE event_name='workspace.deleted' AND account_id=$1
		 ORDER BY id DESC LIMIT 1`, accountID).Scan(&props); err != nil {
		t.Fatalf("read event properties: %v", err)
	}
	return n, props
}

// TestRESTWorkspaceDeleteRecordsEventUnscoped is the regression test for the
// defect: the UI's own request shape (no ?workspace= scope) must leave exactly
// one workspace.deleted row, carrying the workspace identity and the per-table
// counts, attributed to an account the caller can still read the event through.
//
// It fails on the pre-fix code by construction: the write was attributed to the
// account the DELETE removed, and Postgres refused it with 23503 while the
// DELETE itself still answered 200. Under the trash the FK does not fire (the
// row survives) but the account is parked in the bin, so the same test still
// bites — attribution must reach a LIVE account, which is what makes the event
// visible in the merchant's audit view.
func TestRESTWorkspaceDeleteRecordsEventUnscoped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	h, token, doomed, _, doomedAccID, survivorAccID := wsDeleteEventFixture(t, "ws-event-unscoped")

	// Exactly the shape WorkspaceTab.tsx sends: no query string at all.
	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	// The workspace must be BINNED, and every row kept. The old version of this
	// assertion checked the row was GONE; under the trash the equivalent
	// pre-condition is `deleted_at IS NOT NULL`, and the account surviving is
	// exactly why the audit write can no longer fail the way it used to.
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, doomed.ID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 1 {
		t.Fatalf("the workspace was not binned, so this test proves nothing")
	}
	// The workspace's account SURVIVES the move to the bin — that is the card's
	// promise (the CHIP credentials are never orphaned) and it is also why this
	// event can be filed against a row that still exists.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err != nil || acc == nil {
		t.Errorf("the binned workspace's account %s is gone (err=%v); the trash must keep every "+
			"row — its CHIP credentials are not destroyed by a move to the bin", doomedAccID, err)
	}

	n, props := wsDeleteEvents(t, survivorAccID)
	if n != 1 {
		t.Fatalf("workspace.deleted rows attributed to the SURVIVING account %s = %d, "+
			"want 1 — an unscoped DELETE /v1/workspaces/{id} left no audit event (the "+
			"write is attributed to the account the delete just removed, so "+
			"events_account_id_fkey rejects it with SQLSTATE 23503 and recordEvent's "+
			"best-effort path swallows it)", survivorAccID, n)
	}
	if props["workspace_id"] != doomed.ID {
		t.Errorf("event properties workspace_id = %v, want %s", props["workspace_id"], doomed.ID)
	}
	if props["slug"] != doomed.Slug {
		t.Errorf("event properties slug = %v, want %s", props["slug"], doomed.Slug)
	}
	if props["name"] != "Event WS One" {
		t.Errorf("event properties name = %v, want %q", props["name"], "Event WS One")
	}
	del, ok := props["deleted"].(map[string]any)
	if !ok {
		t.Fatalf("event properties deleted = %v (%T), want the per-table count map",
			props["deleted"], props["deleted"])
	}
	for k, want := range map[string]float64{
		"accounts": 1, "forms": 0, "products": 0, "sales": 0, "workspaces": 1,
	} {
		if got, _ := del[k].(float64); got != want {
			t.Errorf("event properties deleted[%s] = %v, want %v (full props: %v)",
				k, del[k], want, props)
		}
	}
	// The attributed account must be LIVE, not merely present. Under the trash
	// every account row survives, so an existence check would pass on exactly the
	// mis-filing this test exists to catch: the event parked on the workspace that
	// was just put away.
	var live bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM accounts a JOIN workspaces sp ON sp.id = a.workspace_id
		                 WHERE a.id=$1 AND sp.deleted_at IS NULL)`, survivorAccID).Scan(&live); err != nil {
		t.Fatalf("check surviving account: %v", err)
	}
	if !live {
		t.Fatalf("the account the event was attributed to (%s) is not on a live workspace", survivorAccID)
	}
}

// TestRESTWorkspaceDeleteEventControlIsScopedOnTheSurvivor is the CONTROL that
// makes the test above non-vacuous: with `?workspace=<surviving ws>` the
// resolved account is NOT the one being deleted, so the event is written even by
// the buggy code. This pair — same fixture, same API, only the scope differs —
// is what isolates "the resolved account is the deleted one" as the cause.
func TestRESTWorkspaceDeleteEventControlIsScopedOnTheSurvivor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	h, token, doomed, survivor, _, survivorAccID := wsDeleteEventFixture(t, "ws-event-scoped")

	rec := sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/"+doomed.ID+"?workspace="+survivor.Slug,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	if n, _ := wsDeleteEvents(t, survivorAccID); n != 1 {
		t.Fatalf("workspace.deleted rows on %s = %d, want 1 — the CONTROL failed, so the "+
			"unscoped test above is not isolating the scope as the cause", survivorAccID, n)
	}
}

// TestRESTWorkspaceDeleteEventRefusedDeletesWriteNothing pins the other side: a
// refused delete (wrong confirm slug) must not leave an audit event claiming a
// workspace was deleted. Without this, "record it before the delete" could be
// implemented in a way that logs a delete that never happened.
func TestRESTWorkspaceDeleteEventRefusedDeletesWriteNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	h, token, doomed, _, _, _ := wsDeleteEventFixture(t, "ws-event-refused")

	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
		`{"confirm_slug":"definitely-not-the-slug"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("an unconfirmed delete returned 200 (%s)", rec.Body.String())
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='workspace.deleted' AND properties->>'workspace_id'=$1`,
		doomed.ID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 0 {
		t.Errorf("a REFUSED delete wrote %d workspace.deleted row(s) — an audit event must "+
			"not claim a workspace was destroyed when it was not", n)
	}
	// Sanity: the workspace is still there, so the refusal was real.
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, doomed.ID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 1 {
		t.Error("the workspace is gone after a refused delete")
	}
}

// TestRESTWorkspaceDeleteEventIsRecordedOncePerDelete drives the unscoped shape
// on two separate owners and asserts exactly one row per delete. A fix that
// wrote the event twice (e.g. before AND after the cascade) would pass a
// single-delete assertion and fail here.
func TestRESTWorkspaceDeleteEventIsRecordedOncePerDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	for _, tag := range []string{"ws-event-count-a", "ws-event-count-b"} {
		h, token, doomed, _, _, survivorAccID := wsDeleteEventFixture(t, tag)
		rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
			`{"confirm_slug":"`+doomed.Slug+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: DELETE = %d (%s)", tag, rec.Code, rec.Body.String())
		}
		// The JSON is unchanged in shape but names the bin: the action is still
		// "delete" from the merchant's side, and the payload now also says the
		// workspace can be brought back.
		var out struct {
			Status     string           `json:"status"`
			ID         string           `json:"id"`
			Slug       string           `json:"slug"`
			Restorable bool             `json:"restorable"`
			Deleted    map[string]int64 `json:"deleted"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: decode response %s: %v", tag, rec.Body.String(), err)
		}
		if out.Status != "trashed" || out.ID != doomed.ID || out.Slug != doomed.Slug {
			t.Errorf("%s: response = %+v, want status=trashed id=%s slug=%s", tag, out, doomed.ID, doomed.Slug)
		}
		if !out.Restorable {
			t.Errorf("%s: response restorable = false, want true — the row went to the bin, not away", tag)
		}
		if out.Deleted["accounts"] != 1 || out.Deleted["workspaces"] != 1 {
			t.Errorf("%s: response deleted = %v, want accounts:1 workspaces:1", tag, out.Deleted)
		}
		if n, _ := wsDeleteEvents(t, survivorAccID); n != 1 {
			t.Errorf("%s (owner %s): workspace.deleted rows = %d, want exactly 1",
				tag, survivorAccID, n)
		}
	}
}

// TestRESTWorkspaceDeleteEventIsReadableThroughTheAuditLog is the end-to-end
// read: after the delete, GET /v1/rest/events — the endpoint the dashboard
// actually uses — surfaces the event. This is what "the event lands" means to a
// user, rather than a row count in the test.
func TestRESTWorkspaceDeleteEventIsReadableThroughTheAuditLog(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	h, token, doomed, survivor, _, _ := wsDeleteEventFixture(t, "ws-event-read")

	if rec := sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/"+doomed.ID+"?workspace="+survivor.Slug,
		`{"confirm_slug":"`+doomed.Slug+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d (%s)", rec.Code, rec.Body.String())
	}
	rec := sessionReq(t, h, token, http.MethodGet,
		"/v1/rest/events?workspace="+survivor.Slug+"&event_name=workspace.deleted", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/events = %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			EventName  string         `json:"event_name"`
			Properties map[string]any `json:"properties"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode events listing %s: %v", rec.Body.String(), err)
	}
	if out.Total != 1 || len(out.Items) != 1 {
		t.Fatalf("audit log total=%d items=%d, want 1 workspace.deleted row (%s)",
			out.Total, len(out.Items), rec.Body.String())
	}
	if out.Items[0].Properties["workspace_id"] != doomed.ID {
		t.Errorf("logged event workspace_id = %v, want %s",
			out.Items[0].Properties["workspace_id"], doomed.ID)
	}
}

// TestRESTWorkspaceDeleteEventSkipsWhenNoLiveAccountRemains pins the one case
// where the delete's audit event is deliberately NOT written: the caller has no
// LIVE account of their own left.
//
// The fixture is the old defect shape — the binned workspace owns the user's
// ONLY account, beside a second workspace with no account at all. The
// last-workspace guard is satisfied by that empty workspace, so the bin
// proceeds; but every account the caller has is now parked in the trash, and
// events are read through the account scope. Filing the row would store the
// audit trail inside the thing that was just put away, where the merchant's
// live audit view cannot reach it.
//
// So it must SKIP, and say so. A zero-row count CANNOT tell a deliberate skip
// from a swallowed failure — both write nothing — so the skip is asserted
// POSITIVELY: the resolution step names no account, and the deliberate-skip log
// line is present.
func TestRESTWorkspaceDeleteEventSkipsWhenNoLiveAccountRemains(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	logs := wsEventLogs(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)

	userID := mkWsUser(t, pool, "ws-event-noacc")
	sp := dbstore.NewWorkspaceStore(pool)
	// The binned workspace owns the user's ONLY account (created via Create).
	doomed, err := sp.Create(ctx, userID, "Only account", "ws-noacc-one-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace 1: %v", err)
	}
	// A second workspace with NO account: it satisfies the last-workspace guard
	// without giving the caller a live account to attribute anything to.
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1,'Accountless','ws-noacc-two-'||substr(md5(random()::text),1,8))`,
		userID); err != nil {
		t.Fatalf("create account-less workspace: %v", err)
	}
	var doomedAccID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE workspace_id=$1`, doomed.ID).Scan(&doomedAccID); err != nil {
		t.Fatalf("read doomed account: %v", err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id} = %d (%s) — an empty second workspace "+
			"should let this through the one-workspace guard", rec.Code, rec.Body.String())
	}
	// The account row is still there (the bin destroys nothing) — the point is
	// that it is not LIVE, which is a different question.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err != nil || acc == nil {
		t.Fatalf("the binned workspace's account %s is gone (err=%v); the trash must keep every row",
			doomedAccID, err)
	}
	// (1) The resolution step must name NO account, because the caller has none
	// left that is live. A predicate that accepted the binned row would return a
	// non-empty id here and the mis-filing would happen.
	auditReq := httptest.NewRequest(http.MethodDelete, "/v1/workspaces/"+doomed.ID, nil)
	auditReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	if got := h.auditAccountAfterDelete(auditReq, userID, doomedAccID); got != "" {
		t.Errorf("auditAccountAfterDelete returned %q, want \"\" — the caller's only account is now "+
			"in the bin, so there is no LIVE account to file this event under", got)
	}
	// (2) Zero rows anywhere: the event belongs to no live account of the
	// caller's, and must never be borrowed onto another tenancy.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='workspace.deleted' AND properties->>'workspace_id'=$1`,
		doomed.ID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 0 {
		t.Errorf("workspace.deleted rows = %d for a delete that left no LIVE account — the event "+
			"must be skipped, not parked in the bin or filed against another tenant", n)
	}
	// (3) And the skip must be a stated decision, not a swallowed failure.
	if !strings.Contains(logs.String(), "workspace_deleted_event_skipped") {
		t.Errorf("the deliberate skip was not logged, so a swallowed failure would look the "+
			"same; audit log was:\n%s", logs.String())
	}
	assertNoFKEventRefusal(t, logs)
}

// assertNoFKEventRefusal fails when the captured log carries an events-account FK
// refusal or a generic event-record failure. A swallowed FK failure and a
// deliberate skip both leave zero rows, so the log is the only place the two can
// be told apart.
func assertNoFKEventRefusal(t *testing.T, logs *bytes.Buffer) {
	t.Helper()
	got := logs.String()
	for _, marker := range []string{"events_account_id_fkey", "23503", "event_record"} {
		if strings.Contains(got, marker) {
			t.Errorf("the audit path logged %q — an event insert was attempted and refused "+
				"(then swallowed) instead of the event being deliberately skipped; log was:\n%s",
				marker, got)
		}
	}
}

// wsStrangerWorkspace creates ANOTHER user's workspace, with its own account, and
// returns it. It exists to put an account the caller does NOT own into reach via
// workspace MEMBERSHIP — the candidate class the fallback must never use.
//
// The stranger's workspace is created BEFORE the caller's own, so it heads the
// list Workspaces.ByMember returns (ORDER BY sp.created_at), which is where the
// pre-rework fallback looked first.
func wsStrangerWorkspace(t *testing.T, pool *pgxpool.Pool, tag string) (ws *dbstore.Workspace, accID string) {
	t.Helper()
	ctx := context.Background()
	strangerID := mkWsUser(t, pool, "stranger-"+tag)
	ws, err := dbstore.NewWorkspaceStore(pool).Create(ctx, strangerID, "Stranger "+tag,
		"stranger-"+tag+"-"+strangerID[:6])
	if err != nil {
		t.Fatalf("create stranger workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE workspace_id=$1`, ws.ID).Scan(&accID); err != nil {
		t.Fatalf("read stranger account: %v", err)
	}
	return ws, accID
}

// wsJoinAsMember makes userID a plain member of workspaceID — the relationship
// through which a stranger's account becomes reachable without ever being owned.
func wsJoinAsMember(t *testing.T, pool *pgxpool.Pool, workspaceID, userID string) {
	t.Helper()
	if err := dbstore.NewWorkspaceMemberStore(pool).
		AddMember(context.Background(), workspaceID, userID, dbstore.RoleReadOnly); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

// wsRequireFirstByMemberCandidate asserts the fixture's core premise: the
// stranger's account is the FIRST candidate Workspaces.ByMember would offer the
// caller. If membership ordering ever stops exposing it, the test below stops
// reproducing the defect, so this fails loudly rather than passing quietly.
func wsRequireFirstByMemberCandidate(t *testing.T, pool *pgxpool.Pool, userID, wantAcc string) {
	t.Helper()
	mine, err := dbstore.NewWorkspaceStore(pool).ByMember(context.Background(), userID)
	if err != nil {
		t.Fatalf("ByMember: %v", err)
	}
	if len(mine) == 0 || len(mine[0].Accounts) == 0 || mine[0].Accounts[0] != wantAcc {
		t.Fatalf("fixture is not the defect shape: ByMember()[0].Accounts = %v, want the "+
			"stranger's account %s first", mine, wantAcc)
	}
}

// TestRESTWorkspaceDeleteEventNeverLandsOnAStrangerAccount is the regression test
// for the cross-tenant mis-attribution this rework fixes.
//
// Shape (the store's own documented one): the caller OWNS exactly one workspace —
// so the Danger zone lets them delete it, because Guard 1 counts owned-or-member
// and "a member of someone else's workspace may still delete their own last one"
// — and is a MEMBER of a stranger's older workspace. Deleting their own workspace
// removes their only account, so the post-delete event has no account of the
// caller's to land on.
//
// Pre-rework the fallback walked Workspaces.ByMember — owned OR joined — so the
// stranger's account was a candidate and the caller's own workspace.deleted row
// was written into the STRANGER's account-scoped audit log with zero FK errors
// and a 200: the stranger read the caller's delete out of their own audit log
// while the caller could not see it. Attribution must stay inside the caller's
// own tenancy, and skip when there is nowhere of their own to write.
func TestRESTWorkspaceDeleteEventNeverLandsOnAStrangerAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	logs := wsEventLogs(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)

	strangerWS, strangerAcc := wsStrangerWorkspace(t, pool, "xtenant")
	userID := mkWsUser(t, pool, "ws-event-xtenant")
	wsJoinAsMember(t, pool, strangerWS.ID, userID)
	sp := dbstore.NewWorkspaceStore(pool)
	doomed, err := sp.Create(ctx, userID, "Caller only WS", "xtenant-own-"+userID[:6])
	if err != nil {
		t.Fatalf("create caller workspace: %v", err)
	}
	wsRequireFirstByMemberCandidate(t, pool, userID, strangerAcc)
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	// Precondition, in the trash's terms: the caller has no LIVE account left.
	// The old form of this check counted rows and expected zero, because the hard
	// delete removed them. The bin keeps the row, so counting rows would now be
	// satisfied by the very account we are proving must not be used — the
	// question is whether any of the caller's accounts is still live.
	var liveLeft int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE owner_user_id=$1`,
		userID).Scan(&liveLeft); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if liveLeft == 0 {
		t.Fatalf("the fixture has NO account at all, so it does not exercise the " +
			"no-live-account case this test is about")
	}
	var liveOnLiveWS int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts a JOIN workspaces sp ON sp.id = a.workspace_id
		  WHERE a.owner_user_id=$1 AND sp.deleted_at IS NULL`, userID).Scan(&liveOnLiveWS); err != nil {
		t.Fatalf("count live accounts: %v", err)
	}
	if liveOnLiveWS != 0 {
		t.Fatalf("live accounts left = %d, want 0 — the fixture did not reach the no-live-account case",
			liveOnLiveWS)
	}
	// The stranger's audit log must be untouched.
	if n, props := wsDeleteEvents(t, strangerAcc); n != 0 {
		t.Errorf("the caller's workspace.deleted event was written into the STRANGER's account %s "+
			"(n=%d, properties=%v) — attribution leaked across tenants", strangerAcc, n, props)
	}
	// Nor may it have landed anywhere else.
	var anywhere int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='workspace.deleted' AND properties->>'workspace_id'=$1`,
		doomed.ID).Scan(&anywhere); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if anywhere != 0 {
		t.Errorf("workspace.deleted rows for the deleted workspace = %d, want 0 — the caller had no "+
			"account of their own left, so the event must be skipped, not filed against another tenant", anywhere)
	}
	// The skip must be deliberate, not a swallowed FK failure.
	if !strings.Contains(logs.String(), "workspace_deleted_event_skipped") {
		t.Errorf("the deliberate skip was not logged; audit log was:\n%s", logs.String())
	}
	assertNoFKEventRefusal(t, logs)
}

// TestRESTWorkspaceDeleteEventUsesTheCallersOwnSurvivingAccount is case E of the
// defect: the caller keeps a surviving account of their own AND is a member of a
// stranger's OLDER workspace.
//
// Pre-rework the fallback took the stranger's account off ByMember (ordered by
// workspace created_at, so the stranger's older workspace came first), and the
// measured result was the stranger's audit log reading the caller's delete while
// the caller's own log stayed empty. The faithful attribution is the caller's OWN
// surviving account.
func TestRESTWorkspaceDeleteEventUsesTheCallersOwnSurvivingAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)

	strangerWS, strangerAcc := wsStrangerWorkspace(t, pool, "surv")
	userID := mkWsUser(t, pool, "ws-event-surv")
	wsJoinAsMember(t, pool, strangerWS.ID, userID)
	sp := dbstore.NewWorkspaceStore(pool)
	acctOf := func(wsID string) string {
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsID).Scan(&id); err != nil {
			t.Fatalf("read account for %s: %v", wsID, err)
		}
		return id
	}
	// Created first among the caller's own workspaces, so the unscoped request
	// resolves to ITS account — that is the account the delete destroys.
	doomed, err := sp.Create(ctx, userID, "Doomed own", "surv-doomed-"+userID[:6])
	if err != nil {
		t.Fatalf("create doomed workspace: %v", err)
	}
	survivor, err := sp.Create(ctx, userID, "Surviving own", "surv-keep-"+userID[:6])
	if err != nil {
		t.Fatalf("create surviving workspace: %v", err)
	}
	doomedAcc, survivorAcc := acctOf(doomed.ID), acctOf(survivor.ID)
	wsRequireFirstByMemberCandidate(t, pool, userID, strangerAcc)
	// Precondition: the unscoped request resolves to the DOOMED account, so the
	// fallback is what the event's attribution actually exercises.
	accs, err := dbstore.NewAccountStore(pool).ByOwner(ctx, userID)
	if err != nil {
		t.Fatalf("ByOwner: %v", err)
	}
	if len(accs) < 2 || accs[0].ID != doomedAcc {
		t.Fatalf("fixture is not the defect shape: ByOwner()[0]=%v want the doomed account %s",
			accs, doomedAcc)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	if n, props := wsDeleteEvents(t, survivorAcc); n != 1 {
		t.Fatalf("workspace.deleted rows on the caller's OWN surviving account %s = %d, want 1 — "+
			"the caller still owns a live account, which is the faithful place to file this event "+
			"(props=%v)", survivorAcc, n, props)
	} else if props["workspace_id"] != doomed.ID {
		t.Errorf("event workspace_id = %v, want %s", props["workspace_id"], doomed.ID)
	}
	// And it must NOT have gone to the stranger the caller only belongs to.
	if n, props := wsDeleteEvents(t, strangerAcc); n != 0 {
		t.Errorf("the caller's workspace.deleted event was filed under the STRANGER's account %s "+
			"(n=%d, properties=%v) instead of the caller's own surviving account %s",
			strangerAcc, n, props, survivorAcc)
	}
}

// TestRESTWorkspaceDeleteEventScopedToAStrangersWorkspaceStaysOwned is the
// regression test for the RESOLVED-account half of the cross-tenant
// mis-attribution.
//
// Shape: the caller OWNS the workspace it deletes, is a MEMBER of a stranger's
// OLDER workspace, and scopes the request to the STRANGER's slug. Only the
// SCOPE differs from the tests above — the request itself is the UI's own
// (`DELETE /v1/workspaces/{id}` with `?workspace=<slug>` added).
//
// resolveSessionAccount takes the ?workspace=<slug> path →
// Workspaces.AccessBySlug → WorkspaceStore.access, which admits owner OR member
// and returns that workspace's account. So the request resolves to the
// STRANGER's account, which SURVIVES the call — and an existence-only test
// accepts it as the attribution target. The caller's own workspace.deleted row
// then lands in the stranger's account-scoped audit log, the stranger reads the
// caller's delete out of their own audit log, and the caller cannot see it.
//
// Tenancy, not existence, is what has to decide: the resolved account may be
// used only when the caller OWNS it. Otherwise the fallback (the caller's own
// surviving account) applies, and the stranger's log stays empty.
func TestRESTWorkspaceDeleteEventScopedToAStrangersWorkspaceStaysOwned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	logs := wsEventLogs(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)

	acctOf := func(wsID string) string {
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsID).Scan(&id); err != nil {
			t.Fatalf("read account for %s: %v", wsID, err)
		}
		return id
	}
	// The stranger's workspace is created BEFORE the caller's own, so it heads
	// the list Workspaces.ByMember returns (ORDER BY sp.created_at).
	strangerWS, strangerUser := wsStrangerWorkspaceOwned(t, pool, "rest-scoped-xtenant")
	strangerAcc := acctOf(strangerWS.ID)
	userID := mkWsUser(t, pool, "ws-event-scoped-x")
	wsJoinAsMember(t, pool, strangerWS.ID, userID)
	sp := dbstore.NewWorkspaceStore(pool)
	// Created first among the caller's own workspaces, so an UNSCOPED request
	// would resolve to ITS account — which is the account the delete destroys,
	// and therefore not what this fixture exercises.
	doomed, err := sp.Create(ctx, userID, "Doomed own", "scopedx-doomed-"+userID[:6])
	if err != nil {
		t.Fatalf("create doomed workspace: %v", err)
	}
	survivor, err := sp.Create(ctx, userID, "Surviving own", "scopedx-keep-"+userID[:6])
	if err != nil {
		t.Fatalf("create surviving workspace: %v", err)
	}
	survivorAcc := acctOf(survivor.ID)

	// Precondition, asserted rather than assumed: the scoped request really does
	// resolve to the STRANGER's account, and that account is NOT the caller's.
	// If either stops being true the test no longer reproduces the defect, so it
	// fails loudly here instead of passing quietly.
	acc, ok, err := sp.AccessBySlug(ctx, strangerWS.Slug, userID)
	if err != nil || !ok {
		t.Fatalf("AccessBySlug(%s, caller) = ok=%v err=%v — the caller is not a member of the "+
			"stranger's workspace, so the fixture is not the defect shape", strangerWS.Slug, ok, err)
	}
	if len(acc.Accounts) != 1 || acc.Accounts[0] != strangerAcc {
		t.Fatalf("AccessBySlug resolved %v, want exactly the stranger's account %s", acc.Accounts, strangerAcc)
	}
	strangerAccRow, err := dbstore.NewAccountStore(pool).ByID(ctx, strangerAcc)
	if err != nil || strangerAccRow == nil {
		t.Fatalf("read stranger account %s: %v", strangerAcc, err)
	}
	if strangerAccRow.OwnerUserID == userID {
		t.Fatalf("the caller OWNS the stranger's account %s; the fixture is not cross-tenant", strangerAcc)
	}
	// And the caller genuinely has an account of its own to fall back to.
	if accs, err := dbstore.NewAccountStore(pool).ByOwner(ctx, userID); err != nil || len(accs) != 2 {
		t.Fatalf("ByOwner(caller) = %v (err=%v), want the caller's own two accounts", accs, err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// The caller deletes its OWN workspace, scoped to the STRANGER's workspace.
	rec := sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/"+doomed.ID+"?workspace="+strangerWS.Slug,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	// The delete must have happened — otherwise this proves nothing. Under the
	// trash the equivalent pre-condition is `deleted_at IS NOT NULL`, not
	// "the row is gone": every row survives a move to the bin, so a COUNT of
	// rows would read 1 either way and a hard-delete premise here would fail on
	// the bin rather than on the leak this test is about.
	var exists int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`,
		doomed.ID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 1 {
		t.Fatal("the workspace was not binned, so this test proves nothing")
	}
	// The stranger's audit log must be untouched.
	if n, props := wsDeleteEvents(t, strangerAcc); n != 0 {
		t.Errorf("the caller's workspace.deleted event was filed under the STRANGER's account %s "+
			"(n=%d, properties=%v) — the caller only MEMBERS that workspace, so the request's "+
			"resolved account is the stranger's and attribution leaked across tenants",
			strangerAcc, n, props)
	}
	// The faithful home is the caller's own surviving account.
	if n, props := wsDeleteEvents(t, survivorAcc); n != 1 {
		t.Errorf("workspace.deleted rows on the caller's OWN surviving account %s = %d, want 1 — "+
			"the caller owns a live account, so that is where its delete belongs (props=%v)",
			survivorAcc, n, props)
	}
	// The stranger must not be able to read the caller's delete out of their own
	// audit log — the user-visible consequence, driven through the real endpoint.
	victimToken, err := sess.Create(ctx, strangerUser)
	if err != nil {
		t.Fatalf("create stranger session: %v", err)
	}
	read := sessionReq(t, h, victimToken, http.MethodGet,
		"/v1/rest/events?workspace="+strangerWS.Slug+"&event_name=workspace.deleted", "")
	if read.Code != http.StatusOK {
		t.Fatalf("the stranger's GET /v1/rest/events = %d (%s)", read.Code, read.Body.String())
	}
	var out struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode the stranger's events listing %s: %v", read.Body.String(), err)
	}
	if out.Total != 0 {
		t.Errorf("the STRANGER's own audit read reports total=%d workspace.deleted row(s), want 0 — "+
			"the stranger reads the caller's delete out of their own audit log (%s)",
			out.Total, read.Body.String())
	}
	// Whatever else happens, the write must not have been attempted-and-refused.
	assertNoFKEventRefusal(t, logs)
}

// wsStrangerWorkspaceOwned is wsStrangerWorkspace plus the stranger's user id, for
// the tests that need to read the audit log AS the stranger.
func wsStrangerWorkspaceOwned(t *testing.T, pool *pgxpool.Pool, tag string) (ws *dbstore.Workspace, strangerUserID string) {
	t.Helper()
	ctx := context.Background()
	strangerUserID = mkWsUser(t, pool, "stranger-"+tag)
	ws, err := dbstore.NewWorkspaceStore(pool).Create(ctx, strangerUserID, "Stranger "+tag,
		"stranger-"+tag+"-"+strangerUserID[:6])
	if err != nil {
		t.Fatalf("create stranger workspace: %v", err)
	}
	return ws, strangerUserID
}

// TestRESTWorkspaceDeleteEventUsesTheResolvedAccountWhenItSurvives pins the
// other half of the attribution rule, which the live run also exercised: on a
// SCOPED request the resolved account belongs to an owned workspace of the
// caller, so it must be used as-is rather than being swapped for "any other"
// account. That keeps the events row faithful to the account the request
// actually acted under.
//
// The fixture is built so the two choices are DISTINGUISHABLE: the caller also
// owns a workspace created BEFORE both others, whose account the "any account
// that is not the resolved one" fallback would pick. Without that workspace the
// fallback happens to land on the resolved account anyway and this test could
// not tell the implementations apart.
func TestRESTWorkspaceDeleteEventUsesTheResolvedAccountWhenItSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)
	h.Events = dbstore.NewEventStore(pool)

	userID := mkWsUser(t, pool, "ws-event-resolved")
	sp := dbstore.NewWorkspaceStore(pool)
	acctOf := func(wsID string) string {
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsID).Scan(&id); err != nil {
			t.Fatalf("read account for %s: %v", wsID, err)
		}
		return id
	}
	// Created FIRST, so a "prefer any other account" implementation would reach
	// for this one and the assertion below would fail.
	early, err := sp.Create(ctx, userID, "Early", "ws-res-early-"+userID[:6])
	if err != nil {
		t.Fatalf("create early workspace: %v", err)
	}
	earlyAcc := acctOf(early.ID)
	doomed, err := sp.Create(ctx, userID, "Doomed", "ws-res-doomed-"+userID[:6])
	if err != nil {
		t.Fatalf("create doomed workspace: %v", err)
	}
	survivor, err := sp.Create(ctx, userID, "Survivor", "ws-res-scope-"+userID[:6])
	if err != nil {
		t.Fatalf("create survivor workspace: %v", err)
	}
	survivorAcc := acctOf(survivor.ID)
	if earlyAcc == survivorAcc {
		t.Fatal("fixture is degenerate: the early and scope accounts are the same")
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/"+doomed.ID+"?workspace="+survivor.Slug,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped DELETE = %d (%s)", rec.Code, rec.Body.String())
	}
	n, props := wsDeleteEvents(t, survivorAcc)
	if n != 1 {
		t.Fatalf("workspace.deleted rows on the RESOLVED surviving account %s = %d, want 1 "+
			"— the scoped request resolved to that account, so that is who the event belongs to",
			survivorAcc, n)
	}
	if props["workspace_id"] != doomed.ID {
		t.Errorf("event workspace_id = %v, want %s", props["workspace_id"], doomed.ID)
	}
	// And it must NOT have been filed under the unrelated early account.
	if other, _ := wsDeleteEvents(t, earlyAcc); other != 0 {
		t.Errorf("the event was filed under the unrelated account %s instead of the resolved "+
			"one %s — attribution must follow the account the request acted under",
			earlyAcc, survivorAcc)
	}
}
