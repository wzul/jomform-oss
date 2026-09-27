package dbstore

// The workspace NAME cap: one rule, counted in characters, applied only to a
// name the caller actually supplied.
//
// WHY THIS FILE EXISTS. The name check used to be
//
//	newName := strings.TrimSpace(in.Name)
//	if newName == "" { newName = cur.Name }
//	if len(newName) > 120 { return nil, errors.New("nama workspace terlalu panjang") }
//
// which is three faults in one line, and all three were live:
//
//  1. BYTES, not characters. 45 CJK characters is 135 UTF-8 bytes, so a name the
//     dashboard's own field accepted (maxLength counts UTF-16 code units, one per
//     character for everything outside a surrogate pair) was refused by the
//     server. The UI could not save the user from it.
//  2. MIS-TIMED. It ran on the FALLBACK value, so a workspace whose STORED name
//     is over the cap could not be renamed at all — including a slug-only PATCH,
//     which is the primary use of the route and the one case where the name has
//     nothing to do with the request.
//  3. UNMAPPED. errors.New produced a fresh value that matched no arm of the
//     transports' error switches, so a refusal meant "too long" arrived as a 500
//     to the merchant (see the httpapi and mcpsrv tests for that half).
//
// WHAT THIS PINS
//   - TestValidateWorkspaceNameCountsCharacters: the unit, and the boundary on
//     both sides of it.
//   - TestWorkspaceRenameNameAtTheCapIsAccepted: a 120-character name (which is
//     240 bytes when accented) renames and the VALUE is asserted, because a rule
//     that silently truncated would pass a status-only check.
//   - TestWorkspaceRenameSlugOnlyIgnoresAnOverLongStoredName: the destructive
//     regression — a workspace holding a name over the old byte cap must still be
//     able to move its slug, and the alias must land.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestValidateWorkspaceNameCountsCharacters pins the UNIT and both sides of the
// boundary, so a future change back to len() fails here with the byte count in
// the message rather than somewhere downstream.
func TestValidateWorkspaceNameCountsCharacters(t *testing.T) {
	jomform45 := strings.Repeat("店", 45) // 45 characters, 135 UTF-8 bytes
	if utf8.RuneCountInString(jomform45) != 45 || len(jomform45) != 135 {
		t.Fatalf("fixture is wrong: %d chars / %d bytes, want 45 / 135",
			utf8.RuneCountInString(jomform45), len(jomform45))
	}
	for _, tc := range []struct {
		why  string
		name string
		ok   bool
	}{
		{"45 CJK characters is 135 BYTES — the byte check refused it, the character rule must not",
			jomform45, true},
		{"120 accented characters is 240 bytes; 120 code units in the field, so the server must agree",
			strings.Repeat("é", 120), true},
		{"exactly at the cap", strings.Repeat("a", WorkspaceNameMaxChars), true},
		{"one over the cap", strings.Repeat("a", WorkspaceNameMaxChars+1), false},
		{"one CJK character over the cap", strings.Repeat("店", WorkspaceNameMaxChars+1), false},
		{"whitespace does not count — the rule trims first",
			"  " + strings.Repeat("a", WorkspaceNameMaxChars) + "  ", true},
		{"an empty name is not a length failure (rename reads it as \"keep\", create states its own requirement)",
			"", true},
	} {
		got, err := ValidateWorkspaceName(tc.name)
		if tc.ok && err != nil {
			t.Errorf("%s: ValidateWorkspaceName(%d chars / %d bytes) = %v, want accepted",
				tc.why, utf8.RuneCountInString(tc.name), len(tc.name), err)
			continue
		}
		if !tc.ok {
			if !errors.Is(err, ErrWorkspaceNameLength) {
				t.Errorf("%s: err = %v, want ErrWorkspaceNameLength — an unclassified error is what "+
					"reached the transports' default arm and 500'd", tc.why, err)
			}
			continue
		}
		if trimmed := strings.TrimSpace(tc.name); got != trimmed {
			t.Errorf("%s: returned %q, want the trimmed %q", tc.why, got, trimmed)
		}
	}
}

// TestWorkspaceRenameNameAtTheCapIsAccepted is requirement 1/2 at the store: a
// name at the character cap is written, and the VALUE is asserted — a rule that
// quietly truncated would still return a changed flag.
func TestWorkspaceRenameNameAtTheCapIsAccepted(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)

	for _, tc := range []struct{ why, name string }{
		{"45 CJK characters (135 bytes — over the OLD byte cap, well under the character one)",
			strings.Repeat("店", 45)},
		{"120 accented characters (240 bytes — the field allows it, so the server must)",
			strings.Repeat("é", 120)},
	} {
		res, err := sp.Rename(ctx, WorkspaceRenameInput{
			WorkspaceID: w.ID, Name: tc.name, CanChangeSlug: true,
		})
		if err != nil {
			t.Fatalf("%s: Rename(name) = %v, want accepted", tc.why, err)
		}
		if res.Workspace.Name != tc.name {
			t.Errorf("%s: stored %q, want the submitted name verbatim (%d chars, %d bytes)",
				tc.why, res.Workspace.Name, utf8.RuneCountInString(tc.name), len(tc.name))
		}
		if got, err := sp.ByID(ctx, w.ID); err != nil || got.Name != tc.name {
			t.Errorf("%s: row name = %q (%v), want the submitted name", tc.why, got.Name, err)
		}
	}

	// And one character over is refused with the SENTINEL, not a fresh error.
	if _, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Name: strings.Repeat("a", WorkspaceNameMaxChars+1), CanChangeSlug: true,
	}); !errors.Is(err, ErrWorkspaceNameLength) {
		t.Errorf("renaming to %d characters = %v, want ErrWorkspaceNameLength",
			WorkspaceNameMaxChars+1, err)
	}
}

// TestWorkspaceRenameSlugOnlyIgnoresAnOverLongStoredName is the destructive
// regression the card names: a workspace whose STORED name is over the old byte
// cap must still be able to move its slug. Before the fix the omitted name fell
// back to cur.Name and was then length-checked, so the request was refused and
// the merchant could not change the address at all.
//
// The stored name is written directly because no write path can create it any
// more (that is the create/rename symmetry the fix adds) — but such rows EXIST,
// created by the very build that had no create-side check, and the point is that
// the rename route must not be poisoned by one.
func TestWorkspaceRenameSlugOnlyIgnoresAnOverLongStoredName(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, w := wsRenameFixture(t, ctx, p)
	sp := NewWorkspaceStore(p)

	stored := strings.Repeat("店", 200) // 200 chars, 600 bytes: over BOTH caps, so a check on the fallback must fail too
	if _, err := p.Exec(ctx, `UPDATE workspaces SET name=$2 WHERE id=$1`, w.ID, stored); err != nil {
		t.Fatalf("seed the legacy over-long name: %v", err)
	}
	newSlug := "it-wsname-slugonly-" + w.ID[:8]

	res, err := sp.Rename(ctx, WorkspaceRenameInput{
		WorkspaceID: w.ID, Slug: newSlug, CanChangeSlug: true,
	})
	if err != nil {
		t.Fatalf("a slug-only rename of a workspace whose STORED name is over the cap = %v, want "+
			"success — the cap is for a name the caller supplies, and this request supplied none", err)
	}
	if !res.SlugChanged || res.Workspace.Slug != newSlug {
		t.Fatalf("slug = %q (changed=%v), want %q", res.Workspace.Slug, res.SlugChanged, newSlug)
	}
	// The stored name is untouched — the cap was not applied, and was not used
	// as an excuse to rewrite it.
	if res.Workspace.Name != stored {
		t.Errorf("the stored name was changed by a slug-only rename: %q → %q", stored, res.Workspace.Name)
	}
	// The alias landed, so the address that was already in the wild still works.
	if al := aliasRows(t, ctx, p, w.ID); len(al) != 1 || al[0] != w.Slug {
		t.Fatalf("aliases = %v, want exactly [%q] — a slug move must leave the old host alive", al, w.Slug)
	}
	if got, err := sp.CurrentSlugForHost(ctx, w.Slug); err != nil || got != newSlug {
		t.Fatalf("the former host %q resolves to %q (%v), want the new slug %q", w.Slug, got, err, newSlug)
	}
}
