package mcpsrv

// MCP parity for the bulk-publish gallery gate (card t_80ea6b64).
//
// THE DEFECT (measured on prod, v1.22.32, card t_87893ac3): a whitespace-only
// template description is refused by the store's gallery predicate, so a bulk
// publish leaves the row at draft — but bulk_forms answered
// {"status":"ok","updated":1}, because it counted "SetStatusMany returned no
// error" as a write and threw away the rows-affected count. The caller's agent
// could not know the template never went live, while /docs/customize promises
// the opposite ("the skipped ids are reported back, so the skip is never
// silent"). The REST door (POST /v1/rest/forms/bulk) already reported it.
//
// These tests drive the real handler over a real Postgres and read the map an
// agent receives, then re-read the row: the reply must name the skipped id and
// must not count it, and the stored status must still be draft. Skipped without
// JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPBulkFormsDoesNotReportAGallerySkippedTemplateAsUpdated(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP bulk-forms gallery-skip test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-bulk-skip-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-bulk-skip-' || md5(random()::text), 'mcp-bulk-skip-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)

	// The control: an ordinary response form, which must still publish.
	good := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-bulk-skip-good-" + accountID[:8], Title: "RSVP",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Name"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	// The prod shape from t_87893ac3: a category, and a Section block whose value
	// is whitespace only — the card trims it away, so the gallery predicate
	// refuses the publish even though the block exists.
	blankIntro := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-bulk-skip-blank-" + accountID[:8], Title: "Blank Intro",
		IsTemplate: true,
		Config: map[string]any{"mode": "response", "category": "Contact", "blocks": []any{
			map[string]any{"id": "b1", "type": "section", "value": "   \n\t  "},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	// A payment template with NO description block and nothing to sell: it is
	// refused by the gallery gate AND would trip the needs-products guard. The
	// gallery gate is the one that cannot be acknowledged, so the id must be
	// reported as skipped — never as needs_products, whose advice ("send
	// acknowledge=true") would not publish it either.
	noIntro := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-bulk-skip-none-" + accountID[:8], Title: "No Intro",
		IsTemplate: true,
		Config: map[string]any{"mode": "payment", "category": "Contact", "blocks": []any{
			map[string]any{"id": "b1", "type": "email", "value": "Email"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	for _, f := range []*dbstore.Form{good, blankIntro, noIntro} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	ids := []string{good.ID, blankIntro.ID, noIntro.ID}

	_, out, err := srv.bulkFormsMCP(callCtx, nil, bulkFormsIn{Action: "publish", IDs: ids})
	if err != nil {
		t.Fatalf("bulkFormsMCP publish: %v", err)
	}

	// The whole point: the two templates were NOT written, so they are not
	// counted. Pre-fix this was int64(3) (every nil error counted as a write)
	// and the reply carried no skip list at all.
	if got := out["updated"]; got != int64(1) {
		t.Errorf("updated = %v, want 1 (only the plain form was written)", got)
	}
	skipped, _ := out["skipped"].([]string)
	sorted := append([]string(nil), skipped...)
	sort.Strings(sorted)
	want := []string{blankIntro.ID, noIntro.ID}
	sort.Strings(want)
	if len(sorted) != len(want) || sorted[0] != want[0] || sorted[1] != want[1] {
		t.Errorf("skipped = %v, want exactly the two refused templates %v", skipped, want)
	}
	if out["skipped_reason"] != BulkGallerySkippedReason {
		t.Errorf("skipped_reason = %q, want %q (the sentence the REST bulk door sends)",
			out["skipped_reason"], BulkGallerySkippedReason)
	}
	if _, present := out["needs_products"]; present {
		t.Errorf("needs_products = %v; a gallery-refused template cannot be published by acknowledging, "+
			"so it must not be reported under that advice", out["needs_products"])
	}

	// The reply must match the row: the plain form is live, both templates are
	// still draft.
	if got, err := forms.GetByID(ctx, accountID, good.ID); err != nil {
		t.Fatalf("re-read plain form: %v", err)
	} else if got.Status != dbstore.FormStatusPublished {
		t.Errorf("plain form status = %q, want published", got.Status)
	}
	for _, f := range []*dbstore.Form{blankIntro, noIntro} {
		got, err := forms.GetByID(ctx, accountID, f.ID)
		if err != nil {
			t.Fatalf("re-read template %s: %v", f.Slug, err)
		}
		if got.Status != dbstore.FormStatusDraft {
			t.Errorf("template %s status = %q after a skipped bulk publish, want draft", f.Slug, got.Status)
		}
	}

	// acknowledge=true is the caller saying "publish even without anything to
	// sell" — it cannot override the gallery gate, so a batch of just the two
	// templates must claim no write at all.
	_, out, err = srv.bulkFormsMCP(callCtx, nil, bulkFormsIn{
		Action: "publish", IDs: []string{blankIntro.ID, noIntro.ID}, Acknowledge: true,
	})
	if err != nil {
		t.Fatalf("acknowledged bulkFormsMCP publish: %v", err)
	}
	if got := out["updated"]; got != int64(0) {
		t.Errorf("updated after acknowledge = %v, want 0 (neither template can be published, acknowledged or not)", got)
	}
	for _, f := range []*dbstore.Form{blankIntro, noIntro} {
		got, err := forms.GetByID(ctx, accountID, f.ID)
		if err != nil {
			t.Fatalf("re-read template %s: %v", f.Slug, err)
		}
		if got.Status != dbstore.FormStatusDraft {
			t.Errorf("template %s status = %q after an acknowledged bulk publish, want draft", f.Slug, got.Status)
		}
	}
}

// TestMCPBulkFormsCountsOnlyRowsItWrote covers the other half of the defect:
// `updated` must be the number of rows the statement changed, not the number of
// ids it did not error on. An id the caller cannot write, and an id already in
// the bin, are not writes.
func TestMCPBulkFormsCountsOnlyRowsItWrote(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP bulk-forms count test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	newAccount := func(tag string) string {
		var userID, accountID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (email, password_hash) VALUES ($1 || md5(random()::text) || '@example.test', 'it') RETURNING id`,
			tag,
		).Scan(&userID); err != nil {
			t.Fatalf("create user: %v", err)
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO accounts (owner_user_id, name, slug)
			 SELECT $1, $2 || md5(random()::text), $2 || md5(random()::text) RETURNING id`,
			userID, tag,
		).Scan(&accountID); err != nil {
			t.Fatalf("create account: %v", err)
		}
		return accountID
	}
	accountID := newAccount("mcp-bulk-count-")
	foreignAccountID := newAccount("mcp-bulk-count-foreign-")

	forms := dbstore.NewFormStore(pool)
	mkForm := func(acc, slug string) *dbstore.Form {
		f := &dbstore.Form{
			AccountID: acc, Slug: slug, Title: "Form " + slug,
			Config: map[string]any{"mode": "response"}, Theme: map[string]any{},
			Status: dbstore.FormStatusDraft,
		}
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", slug, err)
		}
		return f
	}

	live := mkForm(accountID, "mcp-count-live-"+accountID[:8])
	binned := mkForm(accountID, "mcp-count-binned-"+accountID[:8])
	if err := forms.SoftDelete(ctx, accountID, binned.ID); err != nil {
		t.Fatalf("bin the second form: %v", err)
	}
	foreign := mkForm(foreignAccountID, "mcp-count-foreign-"+foreignAccountID[:8])

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	_, out, err := srv.bulkFormsMCP(callCtx, nil,
		bulkFormsIn{Action: "delete", IDs: []string{live.ID, binned.ID, foreign.ID}})
	if err != nil {
		t.Fatalf("bulkFormsMCP delete: %v", err)
	}
	if got := out["updated"]; got != int64(1) {
		t.Errorf("updated = %v, want 1 (one live row; the binned id and the other workspace's id are not writes)", got)
	}

	// The one row that WAS written really is in the bin, and the other
	// workspace's form is untouched.
	var liveTrashed, foreignTrashed bool
	if err := pool.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, live.ID).Scan(&liveTrashed); err != nil {
		t.Fatalf("re-read live form: %v", err)
	}
	if !liveTrashed {
		t.Error("the live form is still live after a bulk delete")
	}
	if err := pool.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, foreign.ID).Scan(&foreignTrashed); err != nil {
		t.Fatalf("re-read foreign form: %v", err)
	}
	if foreignTrashed {
		t.Error("a bulk delete crossed into another workspace's form")
	}

	// ── A no-op re-send is not a write either ─────────────────────────────
	//
	// The arms above never reach the rows-affected branch with a 0: the binned
	// id answers ErrFormNotFound before the write, and the foreign id is dropped
	// by resolveFormAccount. So every id that DOES reach SetStatusMany yields
	// n == 1, and replacing `updated += n` with a per-id counter
	// (`updated += n*0 + 1`) still passes this test — it did, measured at
	// v1.22.53. The arms below are live, owned, non-template ids that reach the
	// write and really come back with 0 rows, because they are already in the
	// requested status; one of them is in a mixed batch so the count is shown to
	// be per-row-changed rather than all-or-nothing.
	//
	// What a client is promised, verbatim from the bulk_forms description
	// (tools.go) and /docs/customize: "updated hanya mengira baris yang
	// benar-benar berubah" / "count only the forms that really changed".
	alreadyDraft := mkForm(accountID, "mcp-count-noop-draft-"+accountID[:8])
	alreadyPublished := mkForm(accountID, "mcp-count-noop-pub-"+accountID[:8])
	if n, err := forms.SetStatusMany(ctx, accountID, []string{alreadyPublished.ID}, dbstore.FormStatusPublished); err != nil || n != 1 {
		t.Fatalf("publish the no-op fixture: n=%d err=%v", n, err)
	}
	changing := mkForm(accountID, "mcp-count-changing-"+accountID[:8])

	updatedAt := func(id string) time.Time {
		t.Helper()
		var ts time.Time
		if err := pool.QueryRow(ctx, `SELECT updated_at FROM forms WHERE id=$1`, id).Scan(&ts); err != nil {
			t.Fatalf("read updated_at %s: %v", id, err)
		}
		return ts
	}
	draftStamp, pubStamp := updatedAt(alreadyDraft.ID), updatedAt(alreadyPublished.ID)

	for _, tc := range []struct {
		name   string
		in     bulkFormsIn
		rows   int64
		stamps map[string]time.Time
	}{
		{
			name: "draft over a form already draft",
			in:   bulkFormsIn{Action: "draft", IDs: []string{alreadyDraft.ID}},
			rows: 0, stamps: map[string]time.Time{alreadyDraft.ID: draftStamp},
		},
		{
			name: "publish over a form already published",
			in:   bulkFormsIn{Action: "publish", IDs: []string{alreadyPublished.ID}},
			rows: 0, stamps: map[string]time.Time{alreadyPublished.ID: pubStamp},
		},
		{
			name: "publish over a mix of already-published and draft",
			in:   bulkFormsIn{Action: "publish", IDs: []string{alreadyPublished.ID, changing.ID}},
			rows: 1, stamps: map[string]time.Time{alreadyPublished.ID: pubStamp},
		},
	} {
		_, out, err := srv.bulkFormsMCP(callCtx, nil, tc.in)
		if err != nil {
			t.Fatalf("%s: bulkFormsMCP: %v", tc.name, err)
		}
		if got := out["updated"]; got != tc.rows {
			t.Errorf("%s: updated = %v, want %d (rows the call actually changed, not ids it re-sent)",
				tc.name, got, tc.rows)
		}
		// A row that did not change must not even be re-stamped: the complaint
		// on prod was that a no-op re-send "only rewrote updated_at".
		for id, want := range tc.stamps {
			if got := updatedAt(id); !got.Equal(want) {
				t.Errorf("%s: %s updated_at moved from %s to %s — a no-op must not write the row at all",
					tc.name, id, want, got)
			}
		}
	}
	if got, err := forms.GetByID(ctx, accountID, changing.ID); err != nil {
		t.Fatalf("re-read the changing form: %v", err)
	} else if got.Status != dbstore.FormStatusPublished {
		t.Errorf("the draft form in the mixed batch is %q, want published (the no-op id must not swallow it)",
			got.Status)
	}
}
