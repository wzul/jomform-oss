package dbstore

// Rename a workspace (name and/or public slug), and the alias that keeps the
// addresses already in the wild alive.
//
// WHAT THIS PINS
//
//  1. A rename moves BOTH halves in ONE transaction: the workspace's slug and
//     the alias row recording the slug it left. A rename that moved the tenant
//     but wrote no alias would turn every printed QR stand, iframe snippet and
//     affiliate link naming the old host into a 404 the merchant never sees.
//  2. The alias RESOLVES (CurrentSlugForHost): an old slug answers with the
//     workspace's CURRENT slug, and the live workspace always wins the lookup —
//     a live slug must never be shadowed by a stale alias row.
//  3. A slug is unavailable when it is LIVE on another workspace OR RESERVED as
//     a former slug of one. Both are refusals that leave BOTH sides untouched,
//     and the reservation half is what stops a third workspace hijacking the
//     printed links of a workspace that renamed.
//  4. Renaming back onto a slug this workspace used before is ALLOWED and drops
//     the alias again, because a live slug must not also be an alias.
//  5. Only the OWNER may move the slug; a manager may rename the name. The rule
//     lives in the store, so a transport cannot forget it.
//  6. Deleting a workspace takes its aliases with it (ON DELETE CASCADE) — the
//     join to the delete-workspace card: a freed address must not keep
//     redirecting to a host that no longer exists.
//  7. The slug rule is ONE rule: ValidateWorkspaceSlug is what create, rename and
//     the MCP create path all call.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
// The store is where a transport cannot bypass these, so this is the file that
// makes the safety real rather than a per-handler promise.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// wsRenameFixture creates a user plus ONE workspace, returning the user id and
// the workspace.
func wsRenameFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool) (string, *Workspace) {
	t.Helper()
	sp := NewWorkspaceStore(p)
	user := testOwnerUser(t, ctx, p)
	w, err := sp.Create(ctx, user, "Rename Fixture", "it-wsren-"+user[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return user, w
}

// aliasRows returns the alias slugs recorded for a workspace, oldest first.
func aliasRows(t *testing.T, ctx context.Context, p *pgxpool.Pool, workspaceID string) []string {
	t.Helper()
	got, err := NewWorkspaceStore(p).AliasesForWorkspace(ctx, workspaceID)
	if err != nil {
		t.Fatalf("AliasesForWorkspace: %v", err)
	}
	return got
}

// TestWorkspaceRenameNameOnlyLeavesSlugAndAliasesAlone is the low-risk half: the
// name is a label, so renaming it must not touch the public identity or write an
// alias (an alias for a live slug would make the redirect path shadow the
// workspace itself).
func TestWorkspaceRenameNameOnlyLeavesSlugAndAliasesAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)

	res, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Name: "Kedai Bunga", CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("Rename(name): %v", err)
	}
	if !res.NameChanged || res.SlugChanged {
		t.Fatalf("flags = name:%v slug:%v, want name changed only", res.NameChanged, res.SlugChanged)
	}
	if res.Workspace.Slug != w.Slug || res.OldSlug != w.Slug {
		t.Errorf("name rename moved the slug: %q → %q", w.Slug, res.Workspace.Slug)
	}
	got, err := sp.ByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Name != "Kedai Bunga" || got.Slug != w.Slug {
		t.Fatalf("row = name:%q slug:%q, want the new name and slug %q", got.Name, got.Slug, w.Slug)
	}
	if al := aliasRows(t, ctx, p, w.ID); len(al) != 0 {
		t.Errorf("a name-only rename wrote %d alias row(s) %v — nothing moved, so nothing should be reserved",
			len(al), al)
	}
}

// TestWorkspaceRenameSlugWritesAliasAndResolves is the core of the feature: the
// alias lands in the same transaction, and the OLD slug still resolves to the
// workspace — via its CURRENT slug, because the caller's job is to move the
// request on, not to serve one tenant on two hosts.
//
// MUTATION EVIDENCE: drop the INSERT INTO workspace_slug_aliases in
// WorkspaceStore.Rename and the CurrentSlugForHost assertion below fails with an
// empty answer — the old address resolves to nothing, which is the 404 the alias
// exists to prevent.
func TestWorkspaceRenameSlugWritesAliasAndResolves(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)
	oldSlug := w.Slug
	newSlug := "it-wsren-new-" + w.ID[:8]

	res, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: newSlug, CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("Rename(slug): %v", err)
	}
	if !res.SlugChanged || res.Workspace.Slug != newSlug || res.OldSlug != oldSlug {
		t.Fatalf("result = slug:%q old:%q changed:%v, want %q / %q / true",
			res.Workspace.Slug, res.OldSlug, res.SlugChanged, newSlug, oldSlug)
	}
	// The name was NOT in the request and must have been kept.
	if res.Workspace.Name != w.Name {
		t.Errorf("an omitted name was overwritten: %q → %q", w.Name, res.Workspace.Name)
	}

	// The alias row exists and is the OLD slug.
	if al := aliasRows(t, ctx, p, w.ID); len(al) != 1 || al[0] != oldSlug {
		t.Fatalf("aliases for the workspace = %v, want exactly [%q]", al, oldSlug)
	}

	// THE RESOLUTION: the old host answers with the NEW slug.
	got, err := sp.CurrentSlugForHost(ctx, oldSlug)
	if err != nil {
		t.Fatalf("CurrentSlugForHost(old): %v", err)
	}
	if got != newSlug {
		t.Fatalf("CurrentSlugForHost(%q) = %q, want the CURRENT slug %q — the printed QR stand, "+
			"iframe and affiliate links name the old host and must move to the new one", oldSlug, got, newSlug)
	}
	// The new slug resolves to itself, and an unknown one to "".
	if got, err := sp.CurrentSlugForHost(ctx, newSlug); err != nil || got != newSlug {
		t.Errorf("CurrentSlugForHost(current) = %q (%v), want %q", got, err, newSlug)
	}
	if got, err := sp.CurrentSlugForHost(ctx, "it-never-existed-"+w.ID[:8]); err != nil || got != "" {
		t.Errorf("CurrentSlugForHost(unknown) = %q (%v), want \"\"", got, err)
	}
}

// TestWorkspaceRenameSlugIsRefusedWhenLiveOrReserved pins both collision halves,
// and that a refusal is total: neither workspace moves, not even the name that
// rode in the same call.
func TestWorkspaceRenameSlugIsRefusedWhenLiveOrReserved(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	user, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)

	other, err := sp.Create(ctx, user, "Other", "it-wsren-other-"+user[:8])
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	// LIVE collision.
	_, err = sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Name: "Should Not Apply", Slug: other.Slug, CanChangeSlug: true,
	})
	if !errors.Is(err, ErrWorkspaceSlugTaken) {
		t.Fatalf("renaming onto a LIVE slug = %v, want ErrWorkspaceSlugTaken", err)
	}
	mine, _ := sp.ByID(ctx, w.ID)
	if mine.Slug != w.Slug || mine.Name != w.Name {
		t.Errorf("the refused rename half-applied: slug:%q name:%q (want %q / %q)",
			mine.Slug, mine.Name, w.Slug, w.Name)
	}
	if al := aliasRows(t, ctx, p, w.ID); len(al) != 0 {
		t.Errorf("the refused rename wrote alias rows %v", al)
	}

	// RESERVED collision: the other workspace vacates a slug, and a THIRD
	// workspace cannot claim it — that address still resolves to the other one.
	vacated, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: other.ID, Slug: "it-wsren-moved-" + user[:8], CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("vacate other slug: %v", err)
	}
	_, err = sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: vacated.OldSlug, CanChangeSlug: true,
	})
	if !errors.Is(err, ErrWorkspaceSlugTaken) {
		t.Fatalf("claiming a RESERVED (former) slug = %v, want ErrWorkspaceSlugTaken — taking it would "+
			"hijack every printed link the other workspace still answers on", err)
	}
	// And the other workspace still owns that address.
	if got, _ := sp.CurrentSlugForHost(ctx, vacated.OldSlug); got != vacated.Workspace.Slug {
		t.Errorf("the former slug %q now resolves to %q, want the workspace that vacated it (%q)",
			vacated.OldSlug, got, vacated.Workspace.Slug)
	}

	// The reservation also blocks CREATE, not just rename: otherwise a new
	// workspace could mint the address a rename left behind.
	if _, err := sp.Create(ctx, user, "Hijack", vacated.OldSlug); !errors.Is(err, ErrWorkspaceSlugTaken) {
		t.Fatalf("creating a workspace on a RESERVED slug = %v, want ErrWorkspaceSlugTaken", err)
	}
}

// TestWorkspaceRenameBackOntoOldSlugDropsTheAlias pins requirement 4: a slug that
// comes back into service must stop being an alias, or the redirect path would
// treat the workspace's own live address as a former one.
func TestWorkspaceRenameBackOntoOldSlugDropsTheAlias(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)
	first := w.Slug
	second := "it-wsren-second-" + w.ID[:8]

	if _, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: second, CanChangeSlug: true,
	}); err != nil {
		t.Fatalf("rename away: %v", err)
	}
	// Renaming BACK is allowed — it is not a collision with ourselves.
	res, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: first, CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("renaming back onto our own former slug: %v", err)
	}
	if res.Workspace.Slug != first {
		t.Fatalf("slug after renaming back = %q, want %q", res.Workspace.Slug, first)
	}
	// The alias for the now-LIVE slug must be gone...
	al := aliasRows(t, ctx, p, w.ID)
	for _, a := range al {
		if a == first {
			t.Errorf("the LIVE slug %q is still an alias row (%v) — a live slug must never also be an "+
				"alias, or the redirect path shadows the workspace itself", first, al)
		}
	}
	// ...and the address we left on the way out is now the alias.
	var found bool
	for _, a := range al {
		if a == second {
			found = true
		}
	}
	if !found {
		t.Errorf("after renaming back, the alias set %v does not contain the slug just vacated (%q)", al, second)
	}
	// The live slug resolves to itself, not through the alias table.
	if got, _ := sp.CurrentSlugForHost(ctx, first); got != first {
		t.Errorf("CurrentSlugForHost(live %q) = %q, want %q", first, got, first)
	}
}

// TestWorkspaceRenameSlugNeedsOwnership pins the authority split IN THE STORE, so
// no transport can forget it: a caller without CanChangeSlug cannot move the
// public address, but can still rename the label.
func TestWorkspaceRenameSlugNeedsOwnership(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)

	_, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: "it-wsren-notmine-" + w.ID[:8],
	})
	if !errors.Is(err, ErrWorkspaceSlugNotOwned) {
		t.Fatalf("a slug change without authority = %v, want ErrWorkspaceSlugNotOwned", err)
	}
	got, _ := sp.ByID(ctx, w.ID)
	if got.Slug != w.Slug {
		t.Errorf("an unauthorised slug rename moved the slug: %q → %q", w.Slug, got.Slug)
	}
	// The name is still theirs to change.
	if _, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Name: "Renamed By Manager",
	}); err != nil {
		t.Fatalf("a manager could not rename the name: %v", err)
	}
}

// TestWorkspaceRenameValidationIsTheOneSharedRule pins that the store's rename
// uses the SAME rule the create paths use, by calling the rule and the rename
// with the same inputs and requiring the SAME error value back.
//
// Validation for CREATE lives in the transports (REST createWorkspace and the MCP
// create_workspace tool both call this same function), so the create-vs-rename
// agreement is asserted where both paths are reachable — through the real route
// table, in httpapi's TestRESTRenameInvalidSlugsAreRefusedByTheSharedRule. What
// this test adds is the other half: the STORE's rename cannot bypass the rule
// even if a future transport forgets to call it.
func TestWorkspaceRenameValidationIsTheOneSharedRule(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)

	for _, raw := range []string{"ab", "xy_z", "ab cd", "xyjomform", "fooJomform", "xy-jomform", "café",
		strings.Repeat("a", 41)} {
		_, ruleErr := ValidateWorkspaceSlug(raw)
		if ruleErr == nil {
			t.Errorf("ValidateWorkspaceSlug(%q) accepted an invalid slug", raw)
			continue
		}
		_, err := sp.Rename(ctx, WorkspaceRenameInput{
			WorkspaceID: w.ID, Slug: raw, CanChangeSlug: true,
		})
		if !errors.Is(err, ruleErr) {
			t.Errorf("Rename(%q) = %v, want the shared rule's own error %v — the store's rename must not "+
				"carry its own copy of what a slug is", raw, err, ruleErr)
		}
		// The refusal did not move the slug.
		if got, _ := sp.ByID(ctx, w.ID); got.Slug != w.Slug {
			t.Fatalf("a refused rename moved the slug to %q (want %q)", got.Slug, w.Slug)
		}
	}
	// The rule normalises, and rename stores the normalised value.
	if norm, err := ValidateWorkspaceSlug("  A-B  "); err != nil || norm != "a-b" {
		t.Errorf("ValidateWorkspaceSlug(%q) = %q, %v; want the normalised %q", "  A-B  ", norm, err, "a-b")
	}
	res, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: "  A-B  ", CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("rename with a normalisable slug: %v", err)
	}
	if res.Workspace.Slug != "a-b" {
		t.Errorf("rename stored %q for input %q, want the normalised %q", res.Workspace.Slug, "  A-B  ", "a-b")
	}
}

// TestWorkspaceDeleteCascadesItsAliases is requirement 6 and the join to the
// delete-workspace card: destroying a workspace must take its aliases with it, so
// a freed address does not keep redirecting to a host that no longer exists.
//
// INTEGRATION NOTE (PR 804 merging main 15337da0). This test was written on main
// against WorkspaceStore.Delete, which on main DESTROYS the workspace. On this
// branch the trash landed first and Delete now BINS instead (a reversible mark),
// while the row — and therefore the FK that cascades these aliases — is only
// removed by Purge. Asserting the cascade on Delete here would be asserting the
// hard delete the trash replaced. The REQUIREMENT is unchanged and still
// asserted; only the verb that reaches it is, because the verb that destroys a
// workspace is now Trash → Purge.
//
// The bin is checked on the way through on purpose: without it, a Purge that did
// nothing at all would leave this test green for the wrong reason, and the
// difference between "the trash released the name" and "the destroy removed the
// aliases" would be invisible.
func TestWorkspaceDeleteCascadesItsAliases(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	sp := NewWorkspaceStore(p)

	// Give the workspace-under-test a former slug, and confirm it resolves.
	vacated, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: ids[0], Slug: "it-wsdel-renamed-" + user[:8], CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got, _ := sp.CurrentSlugForHost(ctx, vacated.OldSlug); got != vacated.Workspace.Slug {
		t.Fatalf("precondition: the former slug does not resolve (%q → %q)", vacated.OldSlug, got)
	}

	// Bin it. The bin is REVERSIBLE, so the alias stays: the row is still there,
	// nothing has been destroyed, and a restore must bring the old address back
	// with it.
	if _, err := sp.Trash(ctx, ids[0], user); err != nil {
		t.Fatalf("trash workspace: %v", err)
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspace_slug_aliases WHERE workspace_id=$1`, ids[0]); n == 0 {
		t.Fatal("the bin dropped the workspace's aliases — a restore would come back without its " +
			"former addresses, and the destroy below would then prove nothing")
	}

	// Now destroy it for good: that is the delete of the ROW, and the FK cascades.
	if _, err := sp.Purge(ctx, ids[0], user); err != nil {
		t.Fatalf("purge workspace: %v", err)
	}

	var n int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM workspace_slug_aliases WHERE workspace_id=$1`, ids[0]).Scan(&n); err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if n != 0 {
		t.Errorf("%d alias row(s) survived the workspace purge — ON DELETE CASCADE is not doing its job, "+
			"so a freed address would keep redirecting to a host that no longer exists", n)
	}
	// And the old host stops resolving entirely.
	if got, _ := sp.CurrentSlugForHost(ctx, vacated.OldSlug); got != "" {
		t.Errorf("the purged workspace's former slug still resolves to %q", got)
	}
	// The other workspace is untouched (the control).
	if other, _ := sp.ByID(ctx, ids[1]); other == nil {
		t.Error("destroying one workspace removed another")
	}
}
