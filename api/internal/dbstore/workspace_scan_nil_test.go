// WorkspaceStore.BySlug/ByID must report a MISSING row as (nil, pgx.ErrNoRows),
// never as (allocated struct, pgx.ErrNoRows).
//
// WHY THIS IS A DEFECT AND NOT A STYLE CHOICE
//
// scanWorkspace used to allocate the result struct BEFORE scanning and return it
// alongside the scan error, so a miss came back as a non-nil pointer carrying
// pgx.ErrNoRows. Callers split into two shapes, and ONE of them is a live bug:
//
//	mcpsrv/workspaces.go:187          if existing, _ := sp.BySlug(...); existing != nil
//	mcpsrv/workspace_members_mcp.go   if err != nil || workspace == nil
//	httpapi/rest_workspace_members.go if err != nil || sp == nil
//	httpapi/profile.go                if err != nil || sp == nil || sp.Owner != uid
//
// create_workspace's uniqueness pre-check is the odd one out: it DISCARDS the
// error and tests only the pointer, so it read EVERY slug — taken or not — as
// taken and refused every create_workspace call, while the same create worked
// over REST (which has no pre-check) and so the break went unnoticed. The other
// three read a miss correctly by accident (they test the error too); they are
// listed because the fix must not break them, not because they were wrong.
//
// The fix reports a miss as (nil, pgx.ErrNoRows), which is the convention every
// sibling store in this package already follows (see notfound.go, and
// subscription.go's note about callers comparing errors.Is(err, pgx.ErrNoRows)).
// nil keeps the pointer test honest; the sentinel keeps the error test working.
//
// This test drives the real store against a real Postgres, because the shape it
// pins only exists once a query has actually missed.

package dbstore

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/testdb"
)

// TestWorkspaceBySlugMissingRowIsNilAndNoError is the regression guard for the
// create_workspace refusal above.
func TestWorkspaceBySlugMissingRowIsNilAndNoError(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := NewWorkspaceStore(pool)

	// Seed one workspace so the present-row half is asserted against a real row,
	// not just an absence.
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('nils-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	created, err := sp.Create(ctx, userID, "Nil Shape", "nil-shape-1")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// The present case must still work (the fix must not have made BySlug always
	// return nil — that would break every caller the other way).
	got, err := sp.BySlug(ctx, created.Slug)
	if err != nil {
		t.Fatalf("BySlug(present) err = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("BySlug(present) = nil, want the stored workspace")
	}
	if got.ID != created.ID {
		t.Errorf("BySlug(present).ID = %q, want %q", got.ID, created.ID)
	}

	// The missing case is the defect: a non-nil struct here is what made
	// create_workspace's `existing != nil` pre-check refuse every slug.
	missing, err := sp.BySlug(ctx, "definitely-absent-slug")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("BySlug(absent) err = %v, want pgx.ErrNoRows — the not-found "+
			"sentinel every sibling store returns (see notfound.go)", err)
	}
	if missing != nil {
		t.Errorf("BySlug(absent) = %+v, want nil — a non-nil struct on a miss makes "+
			"`existing != nil` read every slug as taken (create_workspace then "+
			"refuses every create), and this caller discards the error so nil is "+
			"the only signal it reads", missing)
	}

	// ByID shares scanWorkspace, so it carried the same shape.
	byID, err := sp.ByID(ctx, created.ID)
	if err != nil || byID == nil {
		t.Fatalf("ByID(present) = (%v, %v), want the stored workspace", byID, err)
	}
	absentID, err := sp.ByID(ctx, "00000000-0000-4000-8000-000000000000")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("ByID(absent) err = %v, want pgx.ErrNoRows", err)
	}
	if absentID != nil {
		t.Errorf("ByID(absent) = %+v, want nil", absentID)
	}
}
