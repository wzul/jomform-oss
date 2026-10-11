package dbstore

// The legacy auto-provisioning path must never mint a RESERVED workspace slug
// (card t_ada797d1).
//
// WHY A BEHAVIOURAL GUARD, when the rule and its caller are already pinned by
// source scans (workspace_slug_rule_guard_test.go): those guards assert that the
// derivation calls ValidateWorkspaceSlug. They cannot see the EFFECT — that no
// workspace row is ever inserted on a reserved host, and that the form-owning
// user the rule exists for still ends up with a workspace. The defect this file
// guards was measured, not read: SlugFromEmailLocalPart takes the FIRST FIVE
// characters of the email local part, so a short reserved label survives the
// window intact (`docs@` -> "docs"; `billing@` -> "billi", which is why only the
// short ones are listed below).
//
// The two assertions that bite:
//
//   - no workspace is inserted carrying a slug urlguard reserves, for this user
//     or for anyone (the inserted name is read back from the DB, not from the
//     returned struct); and
//   - the user is not left stranded: his form is moved into the workspace that
//     WAS provisioned. The refusal this card replaced did satisfy the first
//     assertion while failing this one — the login path only logs the error, and
//     the ask-first create path makes an account of its own (issue #158) rather
//     than adopting his, so his forms stayed invisible on every sign-in.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/urlguard"
)

// derivationReachableReserved are the labels on urlguard's reserved list that
// SlugFromEmailLocalPart reproduces EXACTLY: a reserved label of 3-5 characters
// is short enough to survive the five-character window.
//
// A label leaving the reserved list would make its row here vacuous, so each one
// is asserted to still BE reserved rather than assumed — the guard fails loudly
// and points at this table instead of quietly testing nothing.
var derivationReachableReserved = []string{
	"admin", "api", "app", "blog", "cdn", "docs", "help", "login", "mail", "mcp", "pay", "www",
}

// TestLegacySlugLadderDropsPolicyRefusedNames pins the ladder invariant the fix
// rests on: legacySlugCandidates never yields a name the slug rule refuses on
// its reserved leg. It is a unit test (no database) because the ladder is pure.
//
// Neutralise the filter in legacySlugCandidates and this goes red on the
// reserved candidate, before any row is written.
func TestLegacySlugLadderDropsPolicyRefusedNames(t *testing.T) {
	const userID = "11111111-2222-3333-4444-555555555555"

	for _, reserved := range derivationReachableReserved {
		if !urlguard.ReservedSubdomain(reserved) {
			t.Errorf("%q is no longer reserved — this table's row proves nothing. Either the "+
				"name was removed from urlguard's list (drop the row) or it was renamed there", reserved)
			continue
		}
		got := legacySlugCandidates(reserved, userID)
		if len(got) == 0 {
			t.Fatalf("legacySlugCandidates(%q) is empty: the user would get errWorkspaceSlugExhausted "+
				"and no workspace, which is the stranding this fix removes", reserved)
		}
		seen := map[string]bool{}
		for _, c := range got {
			if seen[c] {
				t.Errorf("candidate %q appears twice for %q — the ladder must stay distinct", c, reserved)
			}
			seen[c] = true
			if urlguard.ReservedSubdomain(c) {
				t.Errorf("legacySlugCandidates(%q) yields %q, a RESERVED host: minting it would put a "+
					"workspace on a name JomForm owns or must protect", reserved, c)
			}
			if _, err := ValidateWorkspaceSlug(c); err != nil {
				t.Errorf("candidate %q for %q is refused by the slug rule: %v", c, reserved, err)
			}
		}
		// The ladder still starts from the derived name: it steps OVER the
		// refused one rather than jumping elsewhere (the collision ladder's own
		// ordering, "-2" first).
		if want := reserved + "-2"; got[0] != want {
			t.Errorf("legacySlugCandidates(%q)[0] = %q, want the next candidate %q", reserved, got[0], want)
		}
		if !strings.HasPrefix(got[len(got)-1], reserved+"-") {
			t.Errorf("the last candidate for %q is %q, which no longer derives from the wanted name — "+
				"the user's other links and the collision ladder's last resort both key on that prefix",
				reserved, got[len(got)-1])
		}
	}

	// Control: a name that is NOT reserved keeps the ladder byte-identical, so
	// the filter cannot quietly renumber the ordinary path.
	normal := legacySlugCandidates("owner", userID)
	if len(normal) < 2 || normal[0] != "owner" {
		t.Fatalf("legacySlugCandidates(\"owner\") = %v, want the wanted slug first — the filter "+
			"must not touch a candidate the rule accepts", normal)
	}
	for i := 2; i <= 9; i++ {
		if want := fmt.Sprintf("owner-%d", i); !slices.Contains(normal, want) {
			t.Errorf("legacySlugCandidates(\"owner\") is missing %q — the collision ladder changed shape: %v",
				want, normal)
		}
	}
}

// countReservedWorkspaceSlugs reports how many workspace rows carry a slug the
// reserved list refuses. It is a DELTA baseline, not an absolute: the migrated
// database already holds JomForm's own workspace (jomform-templates, seeded by
// 000027), which is reserved on purpose.
func countReservedWorkspaceSlugs(t *testing.T, ctx context.Context) int {
	t.Helper()
	rows, err := testPool(t).Query(ctx, `SELECT slug FROM workspaces`)
	if err != nil {
		t.Fatalf("read workspace slugs: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			t.Fatalf("scan workspace slug: %v", err)
		}
		if urlguard.ReservedSubdomain(slug) {
			n++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate workspace slugs: %v", err)
	}
	return n
}

// TestProvisionLegacyWorkspaceNeverMintsAReservedDerivedSlug is the integration
// guard the card asks for, generalised from `docs@` to every reserved label the
// derivation can produce. It drives the real login-path entry point against real
// rows: a user with one form and no workspace, whose email local part derives a
// reserved name.
func TestProvisionLegacyWorkspaceNeverMintsAReservedDerivedSlug(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	for _, local := range derivationReachableReserved {
		email := local + "@probe-legacy-reserved.test"
		user, account := legacyFixture(t, ctx, email, 1)

		derived := SlugFromEmailLocalPart(email, user)
		// If the derivation stops reproducing the label, this row proves nothing
		// about the reserved window and must be re-cut rather than pass.
		if derived != local {
			t.Fatalf("SlugFromEmailLocalPart(%q) = %q, want %q — this row no longer exercises a "+
				"reserved derived name", email, derived, local)
		}
		if !urlguard.ReservedSubdomain(derived) {
			t.Fatalf("%q is not on the reserved list any more — the row for %q proves nothing",
				derived, email)
		}

		reservedBefore := countReservedWorkspaceSlugs(t, ctx)

		ws, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
		if err != nil {
			t.Fatalf("provisioning a form-owning user whose derived name is reserved (%q) failed: %v — "+
				"the login path only logs this, so his forms would stay invisible on every sign-in",
				email, err)
		}
		if ws == nil {
			t.Fatalf("nothing was provisioned for %q, a user with forms and no workspace — Wan's rule "+
				"is that he GETS one, and the ask-first create path does not adopt his account", email)
		}
		// Read the slug back out of the DATABASE rather than trusting the
		// returned struct: the guard is about what was inserted.
		var stored string
		if err := p.QueryRow(ctx, `SELECT slug FROM workspaces WHERE id=$1`, ws.ID).Scan(&stored); err != nil {
			t.Fatalf("read back the provisioned workspace: %v", err)
		}
		if stored != ws.Slug {
			t.Fatalf("workspace %s stores slug %q but reported %q", ws.ID, stored, ws.Slug)
		}
		if urlguard.ReservedSubdomain(stored) {
			t.Fatalf("provisioning %q minted the RESERVED host %q.jomform.com (inserted, read back from "+
				"the workspaces row) — that is the defect this card is about", email, stored)
		}
		if _, err := ValidateWorkspaceSlug(stored); err != nil {
			t.Fatalf("the provisioned slug %q is refused by the slug rule itself: %v", stored, err)
		}
		if !strings.HasPrefix(stored, derived+"-") {
			t.Fatalf("the provisioned slug %q does not derive from %q — the ladder must step over the "+
				"reserved name, not jump to an unrelated one", stored, derived)
		}
		// The reserved name is claimed by NOBODY, and no new reserved slug
		// appeared at all (JomForm's own reserved workspace aside).
		var claimed int
		if err := p.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE slug=$1`, derived).Scan(&claimed); err != nil {
			t.Fatalf("count workspaces on %q: %v", derived, err)
		}
		if claimed != 0 {
			t.Fatalf("%d workspace(s) answer on the reserved host %q", claimed, derived)
		}
		if after := countReservedWorkspaceSlugs(t, ctx); after != reservedBefore {
			t.Fatalf("reserved workspace slugs went from %d to %d while provisioning %q — a reserved "+
				"name was minted somewhere in this path", reservedBefore, after, email)
		}
		// And the user is NOT stranded: the form his account carries moved into
		// the workspace that was provisioned.
		var moved int
		if err := p.QueryRow(ctx,
			`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id=$2`, account, ws.ID).
			Scan(&moved); err != nil {
			t.Fatalf("count moved forms: %v", err)
		}
		if moved != 1 {
			t.Fatalf("%d of 1 form(s) point at the provisioned workspace %q for %q — a workspace was "+
				"created but his forms were left behind, so nothing appears in his dashboard", moved, ws.Slug, email)
		}
	}
}
