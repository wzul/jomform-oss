package dbstore

// Issue #601: the suppression list's admin/read surface.
//
// Before this, SuppressionStore could only be WRITTEN to (Upsert from the SNS
// ingestion path) and probed one address at a time (IsSuppressed). List/Remove
// had zero callers in the whole repo. These tests cover the store API the
// super-admin screen, the REST endpoints and the MCP tools are built on, and
// they pin the two behaviours that migration 000061's CHECK now enforces:
// the reason is one of three canonical codes, and the free text it replaced
// survives in `note`.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/testdb"
)

// suppressionFixture returns a store over this package's scratch schema with
// the table emptied, so a test that counts rows sees only its own.
func suppressionFixture(t *testing.T) (*SuppressionStore, string) {
	t.Helper()
	p := testPool(t)
	testdb.Reset(t, p, "suppressed_emails")
	// A unique prefix keeps each test's addresses distinct even though they
	// share the schema.
	return NewSuppressionStore(p), fmt.Sprintf("it601-%d", time.Now().UnixNano())
}

// TestSuppressionNormalisationAndIdempotence pins the normalisation contract:
// an address is lowercase+trimmed before it reaches SQL, so mixed case and
// surrounding whitespace address the SAME row — a second Add for a differently-cased
// spelling of an address already on the list must report created=false and must
// not produce a second row.
func TestSuppressionNormalisationAndIdempotence(t *testing.T) {
	ctx := context.Background()
	store, base := suppressionFixture(t)
	addr := base + "@Example.COM"

	created, err := store.Add(ctx, "  "+addr+"  ", SuppressionReasonHardBounce, SuppressionSourceBounce, "Permanent/NoEmail", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !created {
		t.Fatal("the first Add reported created=false — a new suppression must be reported as created")
	}

	// The stored address is lower-cased, so the equality lookup (and the UI)
	// never see the caller's casing.
	got, err := store.ByEmail(ctx, strings.ToUpper(addr))
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if got == nil {
		t.Fatal("issue #601: an address added with mixed case is not found by its upper-case spelling — normalisation is not applied")
	}
	want := strings.ToLower(strings.TrimSpace(addr))
	if got.Email != want {
		t.Errorf("stored email = %q, want %q", got.Email, want)
	}
	if got.Note != "Permanent/NoEmail" {
		t.Errorf("note = %q, want the recorded detail", got.Note)
	}

	// Second Add, differing only in case/whitespace: idempotent.
	created2, err := store.Add(ctx, addr, SuppressionReasonHardBounce, SuppressionSourceBounce, "Permanent/NoEmail", "")
	if err != nil {
		t.Fatalf("Add (second): %v", err)
	}
	if created2 {
		t.Error("issue #601: a second Add for the same address reported created=true — duplicate emails can be inserted")
	}

	// And the duplicate cannot be inserted at the DB level either: the address
	// is the CITEXT primary key.
	var count int
	if err := testPool(t).QueryRow(ctx,
		`SELECT count(*) FROM suppressed_emails WHERE email = $1`, want).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("suppressed_emails holds %d rows for %s, want 1 — the CITEXT PK must make the address unique case-insensitively", count, want)
	}
}

// TestSuppressionReasonIsCanonicalised pins the migration-000061 CHECK
// contract on the write path: the ingestion path's free-text reason (written by
// sesnotif.go before this change, e.g. "permanent bounce (Permanent)") must
// still be storable, and it must land as a canonical code with the original
// text preserved in `note`. Without the mapping the CHECK rejects the write and
// bounce ingestion stops recording anything.
func TestSuppressionReasonIsCanonicalised(t *testing.T) {
	ctx := context.Background()
	store, base := suppressionFixture(t)

	for _, tc := range []struct {
		name       string
		inReason   string
		source     SuppressionSource
		wantReason string
		wantNote   string
	}{
		{
			// The exact string the pre-#601 ingestion path wrote.
			name: "ingestion free text bounce", inReason: "permanent bounce (Permanent)",
			source: SuppressionSourceBounce, wantReason: "hard_bounce", wantNote: "permanent bounce (Permanent)",
		},
		{
			name: "ingestion free text complaint", inReason: "complaint",
			source: SuppressionSourceComplaint, wantReason: "complaint", wantNote: "",
		},
		{
			name: "already canonical", inReason: "manual",
			source: SuppressionSourceManual, wantReason: "manual", wantNote: "",
		},
		{
			name: "prose bounce", inReason: "hard bounce",
			source: SuppressionSourceBounce, wantReason: "hard_bounce", wantNote: "hard bounce",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := fmt.Sprintf("%s-%s@example.test", base, strings.ReplaceAll(tc.name, " ", "-"))
			// Add is the single write path, so the mapping must hold even for a
			// caller handing it free text.
			if _, err := store.Add(ctx, addr, tc.inReason, tc.source, tc.wantNote, ""); err != nil {
				t.Fatalf("Add(%q): %v", tc.inReason, err)
			}
			got, err := store.ByEmail(ctx, addr)
			if err != nil || got == nil {
				t.Fatalf("ByEmail: %v (row %v)", err, got)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q (the reason CHECK accepts only the canonical codes)", got.Reason, tc.wantReason)
			}
			if got.Note != tc.wantNote {
				t.Errorf("note = %q, want %q", got.Note, tc.wantNote)
			}
		})
	}

	// Upsert is the kept-for-compatibility wrapper: a legacy call site must not
	// be able to violate the CHECK, and must not lose the detail either.
	legacy := base + "-legacy@example.test"
	if _, err := store.Upsert(ctx, legacy, "permanent bounce (OnAccountSuppressionList)", SuppressionSourceBounce); err != nil {
		t.Fatalf("Upsert(legacy free text): %v — the pre-#601 write path must keep working through the CHECK", err)
	}
	got, err := store.ByEmail(ctx, legacy)
	if err != nil || got == nil {
		t.Fatalf("ByEmail(legacy): %v", err)
	}
	if got.Reason != "hard_bounce" {
		t.Errorf("legacy reason = %q, want hard_bounce", got.Reason)
	}
	if got.Note != "permanent bounce (OnAccountSuppressionList)" {
		t.Errorf("legacy note = %q, want the original free text preserved", got.Note)
	}
}

// TestSuppressionSearchNormalisesCaseAndWhitespaceAndPaginates covers the admin
// list: a search term with mixed case and surrounding whitespace must behave
// like its normalised form, the term must match the note as well as the
// address, and paging must be stable and consistent with the reported total.
func TestSuppressionSearchNormalisesCaseAndWhitespaceAndPaginates(t *testing.T) {
	ctx := context.Background()
	store, base := suppressionFixture(t)

	// Five rows, added oldest-first so the newest-first ordering is decisive.
	for i := 0; i < 5; i++ {
		addr := fmt.Sprintf("%s-p%d@example.test", base, i)
		note := ""
		if i == 4 {
			// The only row whose NOTE mentions bounce, to prove note matching.
			note = "repeated soft bounce (3 in 7d)"
		}
		if _, err := store.Add(ctx, addr, SuppressionReasonHardBounce, SuppressionSourceBounce, note, ""); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	full, total, err := store.Search(ctx, "", 1, 20)
	if err != nil {
		t.Fatalf("Search(all): %v", err)
	}
	if total != 5 || len(full) != 5 {
		t.Fatalf("Search(all) total=%d len=%d, want 5/5", total, len(full))
	}
	// Newest first: the last row added comes first.
	if full[0].Email != fmt.Sprintf("%s-p4@example.test", base) {
		t.Errorf("first row = %q, want the newest (%s-p4@example.test) — the list is newest-first", full[0].Email, base)
	}

	// Case + whitespace normalisation: " IT601-…-P2@EXAMPLE.TEST " must find
	// the row stored as "it601-…-p2@example.test".
	term := "  " + strings.ToUpper(fmt.Sprintf("%s-P2@Example.TEST", base)) + " "
	hit, n, err := store.Search(ctx, term, 1, 20)
	if err != nil {
		t.Fatalf("Search(normalised): %v", err)
	}
	if n != 1 || len(hit) != 1 {
		t.Fatalf("issue #601: search term %q total=%d len=%d, want 1/1 — the term is not lower-cased/trimmed before the query", term, n, len(hit))
	}
	if hit[0].Email != fmt.Sprintf("%s-p2@example.test", base) {
		t.Errorf("matched %q, want %s-p2@example.test", hit[0].Email, base)
	}

	// Note matching.
	byNote, n, err := store.Search(ctx, "SOFT BOUNCE", 1, 20)
	if err != nil {
		t.Fatalf("Search(note): %v", err)
	}
	if n != 1 || len(byNote) != 1 {
		t.Fatalf("issue #601: searching the note found total=%d len=%d, want 1/1 — the search only covers the address", n, len(byNote))
	}
	if !strings.Contains(byNote[0].Note, "soft bounce") {
		t.Errorf("note match returned %q (note %q)", byNote[0].Email, byNote[0].Note)
	}

	// Pagination: page 1 and page 2 of 2, no overlap, with the same total both
	// times (the pager must not shrink as it pages).
	p1, total1, err := store.Search(ctx, "", 1, 2)
	if err != nil {
		t.Fatalf("Search(page 1): %v", err)
	}
	p2, total2, err := store.Search(ctx, "", 2, 2)
	if err != nil {
		t.Fatalf("Search(page 2): %v", err)
	}
	p3, total3, err := store.Search(ctx, "", 3, 2)
	if err != nil {
		t.Fatalf("Search(page 3): %v", err)
	}
	if total1 != 5 || total2 != 5 || total3 != 5 {
		t.Errorf("totals across pages = %d/%d/%d, want 5/5/5", total1, total2, total3)
	}
	if len(p1) != 2 || len(p2) != 2 || len(p3) != 1 {
		t.Errorf("page sizes = %d/%d/%d, want 2/2/1", len(p1), len(p2), len(p3))
	}
	seen := map[string]bool{}
	for _, row := range append(append(append([]SuppressedEmail{}, p1...), p2...), p3...) {
		if seen[row.Email] {
			t.Errorf("issue #601: %s appears on more than one page — the page window is not advancing", row.Email)
		}
		seen[row.Email] = true
	}
	if len(seen) != 5 {
		t.Errorf("paging surfaced %d distinct rows, want 5", len(seen))
	}

	// A LIKE metacharacter is a literal, not a wildcard: searching "%" must not
	// return the whole list.
	wild, wildTotal, err := store.Search(ctx, "%", 1, 20)
	if err != nil {
		t.Fatalf("Search(%%): %v", err)
	}
	if wildTotal != 0 || len(wild) != 0 {
		t.Errorf("issue #601: searching %q returned %d rows — a LIKE wildcard is being interpreted instead of matched literally", "%", wildTotal)
	}
}

// TestSuppressionRemove covers the remove path the admin screen's Remove action
// uses: removing an address that is on the list takes it off (mail to it
// resumes), and removing one that is not is a no-op rather than an error.
func TestSuppressionRemove(t *testing.T) {
	ctx := context.Background()
	store, base := suppressionFixture(t)
	addr := base + "@example.test"

	if _, err := store.Add(ctx, addr, SuppressionReasonManual, SuppressionSourceManual, "", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if ok, err := store.IsSuppressed(ctx, addr); err != nil || !ok {
		t.Fatalf("IsSuppressed before remove = %v (err %v), want true", ok, err)
	}

	// RemoveIfPresent is what the DELETE endpoint needs: it must report whether
	// it actually removed something so a never-present address can answer 404.
	removed, err := store.RemoveIfPresent(ctx, strings.ToUpper(addr))
	if err != nil {
		t.Fatalf("RemoveIfPresent: %v", err)
	}
	if !removed {
		t.Error("issue #601: removing a listed address reported removed=false")
	}
	if ok, err := store.IsSuppressed(ctx, addr); err != nil || ok {
		t.Fatalf("IsSuppressed after remove = %v (err %v), want false — removing an address must re-enable sending to it", ok, err)
	}

	// A second remove (the address is now absent) is a no-op, and Remove itself
	// must not error either.
	removed2, err := store.RemoveIfPresent(ctx, addr)
	if err != nil {
		t.Fatalf("RemoveIfPresent (absent): %v", err)
	}
	if removed2 {
		t.Error("issue #601: removing an address that is not on the list reported removed=true")
	}
	if err := store.Remove(ctx, addr); err != nil {
		t.Errorf("Remove of a missing address errored: %v — it must be a no-op", err)
	}
}

// TestSuppressionCreatedByIsRecordedAndResolved covers the actor columns the
// admin table displays: a manual add records the super-admin's users.id and the
// row reports their email; an ingestion-style add has neither.
func TestSuppressionCreatedByIsRecordedAndResolved(t *testing.T) {
	ctx := context.Background()
	store, base := suppressionFixture(t)
	p := testPool(t)
	actor := testOwnerUser(t, ctx, p)

	manual := base + "-manual@example.test"
	if _, err := store.Add(ctx, manual, SuppressionReasonManual, SuppressionSourceManual, "asked us to stop", actor); err != nil {
		t.Fatalf("Add(manual): %v", err)
	}
	got, err := store.ByEmail(ctx, manual)
	if err != nil || got == nil {
		t.Fatalf("ByEmail(manual): %v", err)
	}
	if got.CreatedBy != actor {
		t.Errorf("created_by = %q, want the adding admin's user id %q", got.CreatedBy, actor)
	}
	if got.CreatedByEmail == "" {
		t.Error("issue #601: created_by_email is empty — the admin table shows the resolved actor and must be able to")
	}

	// An ingestion add has no actor: both fields are empty, not a stale value.
	ingested := base + "-ingested@example.test"
	if _, err := store.Add(ctx, ingested, SuppressionReasonHardBounce, SuppressionSourceBounce, "Permanent", ""); err != nil {
		t.Fatalf("Add(ingested): %v", err)
	}
	got2, err := store.ByEmail(ctx, ingested)
	if err != nil || got2 == nil {
		t.Fatalf("ByEmail(ingested): %v", err)
	}
	if got2.CreatedBy != "" || got2.CreatedByEmail != "" {
		t.Errorf("ingestion row created_by=%q created_by_email=%q, want both empty", got2.CreatedBy, got2.CreatedByEmail)
	}

	// A later ingestion event for an address an admin added by hand must not
	// erase the record of who added it (the UPSERT would otherwise clear it).
	if _, err := store.Add(ctx, manual, SuppressionReasonHardBounce, SuppressionSourceBounce, "Permanent", ""); err != nil {
		t.Fatalf("Add(refresh): %v", err)
	}
	got3, err := store.ByEmail(ctx, manual)
	if err != nil || got3 == nil {
		t.Fatalf("ByEmail(refresh): %v", err)
	}
	if got3.CreatedBy != actor {
		t.Errorf("issue #601: a later ingestion event cleared created_by (%q) — the record of who added the address by hand is lost", got3.CreatedBy)
	}
}
