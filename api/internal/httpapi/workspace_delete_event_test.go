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
// counts, attributed to an account that still exists.
//
// It fails on the pre-fix code by construction: the write is attributed to the
// deleted account and Postgres refuses it with 23503, so the count is 0 while
// the DELETE itself still answers 200.
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
	// The delete must have happened — otherwise the assertion below could pass
	// on a no-op.
	var exists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, doomed.ID).Scan(&exists); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if exists != 0 {
		t.Fatalf("the workspace was not deleted, so this test proves nothing")
	}
	// The doomed account is really gone — that is the whole reason the pre-fix
	// event write failed.
	if acc, err := dbstore.NewAccountStore(pool).ByID(ctx, doomedAccID); err == nil && acc != nil {
		t.Errorf("the doomed workspace's account %s still exists; the fixture did not "+
			"reproduce the defect shape", doomedAccID)
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
	// The attributed account must be LIVE — that is the FK the pre-fix code violated.
	var live bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM accounts WHERE id=$1)`, survivorAccID).Scan(&live); err != nil {
		t.Fatalf("check surviving account: %v", err)
	}
	if !live {
		t.Fatalf("the account the event was attributed to (%s) does not exist", survivorAccID)
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
		// The JSON the UI reads must be unchanged by the reorder.
		var out struct {
			Status  string           `json:"status"`
			ID      string           `json:"id"`
			Slug    string           `json:"slug"`
			Deleted map[string]int64 `json:"deleted"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: decode response %s: %v", tag, rec.Body.String(), err)
		}
		if out.Status != "deleted" || out.ID != doomed.ID || out.Slug != doomed.Slug {
			t.Errorf("%s: response = %+v, want status=deleted id=%s slug=%s", tag, out, doomed.ID, doomed.Slug)
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

// TestRESTWorkspaceDeleteEventWithNoSurvivingAccountSkipsInsteadOfFailing is the
// case a live run against the real API surfaced: a caller whose ONLY account
// belongs to the workspace being deleted, with a second, ACCOUNT-LESS workspace
// beside it. The "one workspace must remain" guard is satisfied by that empty
// workspace, so the delete legitimately proceeds — but there is then no account
// left to file an event under.
//
// That case must not be handled by writing anyway: the insert would be refused
// with events_account_id_fkey (SQLSTATE 23503) and the refusal swallowed, which
// is the very shape of the original defect (a delete that looks audited and is
// not). It must SKIP, and say so.
//
// A zero-row count CANNOT tell a deliberate skip from a swallowed FK failure —
// both write nothing. So the skip is asserted POSITIVELY, three ways:
//
//  1. auditAccountAfterDelete itself returns "" (the resolution step names no
//     account, rather than the write step failing on one),
//  2. the deliberate-skip log line is present,
//  3. NO events-FK refusal appears in the audit path's log.
//
// Plus a positive control proving (3) is not vacuous: the same fixture's dead
// account really does refuse an event insert with events_account_id_fkey, so a
// write-anyway implementation would necessarily leave that marker in the log.
func TestRESTWorkspaceDeleteEventWithNoSurvivingAccountSkipsInsteadOfFailing(t *testing.T) {
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
	// The doomed workspace owns the user's ONLY account (created via Create).
	doomed, err := sp.Create(ctx, userID, "Only account", "ws-noacc-one-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace 1: %v", err)
	}
	// A second workspace with NO account: it satisfies the last-workspace guard
	// without giving the caller an account to attribute anything to.
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

	// Precondition: after this delete the caller really has no account left.
	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+doomed.ID,
		`{"confirm_slug":"`+doomed.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id} = %d (%s) — an empty second workspace "+
			"should let this through the one-workspace guard", rec.Code, rec.Body.String())
	}
	var accountsLeft int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE owner_user_id=$1`, userID).Scan(&accountsLeft); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if accountsLeft != 0 {
		t.Fatalf("accounts left = %d, want 0 — the fixture did not reach the no-survivor case", accountsLeft)
	}
	// (1) The resolution step must name NO account. This is the skip asserted at
	// its source: a fallback that had picked something would return a non-empty
	// id here and the write would be attempted.
	auditReq := httptest.NewRequest(http.MethodDelete, "/v1/workspaces/"+doomed.ID, nil)
	auditReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	if got := h.auditAccountAfterDelete(auditReq, userID, doomedAccID); got != "" {
		t.Errorf("auditAccountAfterDelete returned %q, want \"\" — the caller has no surviving "+
			"account of their own, so there is nothing to attribute the event to", got)
	}
	// Zero rows, as before.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE event_name='workspace.deleted' AND properties->>'workspace_id'=$1`,
		doomed.ID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 0 {
		t.Errorf("workspace.deleted rows = %d for a delete that left no account — a row "+
			"attributed to no surviving account cannot have landed", n)
	}
	// (2) The skip must be a stated decision.
	if !strings.Contains(logs.String(), "workspace_deleted_event_skipped") {
		t.Errorf("the deliberate skip was not logged, so a swallowed failure would look the "+
			"same; audit log was:\n%s", logs.String())
	}
	// (3) And nothing in the audit path was refused by the FK.
	assertNoFKEventRefusal(t, logs)

	// Positive control: the dead account really does refuse an event insert, so
	// (3) above is a real assertion rather than a vacuous one. This is exactly
	// the SQLSTATE a write-anyway implementation would log.
	ctlErr := h.Events.Record(ctx, doomedAccID, userID, "workspace.deleted", nil, "", "")
	if ctlErr == nil {
		t.Fatal("control failed: an event insert against the deleted account SUCCEEDED, so this " +
			"fixture is not the FK shape and the no-FK-error assertion above proves nothing")
	}
	if !strings.Contains(ctlErr.Error(), "events_account_id_fkey") {
		t.Errorf("control: the refused insert failed with %v, want events_account_id_fkey — the "+
			"FK premise of this test is not what it claims", ctlErr)
	}
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
	var accountsLeft int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE owner_user_id=$1`, userID).Scan(&accountsLeft); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if accountsLeft != 0 {
		t.Fatalf("accounts left = %d, want 0 — the fixture did not reach the no-own-account case", accountsLeft)
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

// TestRESTWorkspaceDeleteEventUsesTheResolvedAccountWhenItSurvives pins the
// other half of the attribution rule, which the live run also exercised: on a
// SCOPED request the resolved account belongs to a DIFFERENT (surviving)
// workspace, so it must be used as-is rather than being swapped for "any other"
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
