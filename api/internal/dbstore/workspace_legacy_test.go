package dbstore

// Legacy onboarding: an EXISTING user who already owns forms but no workspace.
//
// Wan's rule (2026-09-20, final revision):
//
//	has forms, no workspace   -> CREATE the workspace for him and attach the
//	                             account + its forms. Name/slug from the first 5
//	                             characters of the email. Nothing is deleted.
//	no forms, no workspace    -> ASK him (the ask-first flow in
//	                             web/src/app/workspace-picker, not this file).
//
// These tests are the guard for the half that has no UI: the provisioning and the
// gallery exclusion. They are integration tests (skipped without
// JOMFORM_TEST_DATABASE_URL) because the whole subject is what happens to real
// rows: a mock would assert the SQL was built, not that the forms moved.
//
// The failure the guard exists to catch is silent by construction: after a naive
// relink, accounts.workspace_id is set and everything LOOKS migrated while every
// form still carries workspace_id IS NULL, because migration 000045's
// trg_forms_sync_workspace_id fires only on INSERT / UPDATE OF account_id on
// forms — never on a change to the account. Nothing errors; the user just still
// cannot see his forms.

import (
	"context"
	"strings"
	"testing"
)

// legacyFixture creates the prod shape for one user and returns the ids.
func legacyFixture(t *testing.T, ctx context.Context, email string, forms int) (user, account string) {
	t.Helper()
	p := testPool(t)
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1,'x') RETURNING id`, email).
		Scan(&user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1,'legacy','legacy-' || md5(random()::text)) RETURNING id`, user).
		Scan(&account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	for i := 0; i < forms; i++ {
		if _, err := p.Exec(ctx,
			`INSERT INTO forms (account_id, slug, title, config, theme, status)
			 VALUES ($1, 'legacy-' || md5(random()::text), 'legacy form', '{}'::jsonb, '{}'::jsonb, 'published')`,
			account); err != nil {
			t.Fatalf("seed form %d: %v", i, err)
		}
	}
	return user, account
}

// TestProvisionLegacyWorkspaceMovesTheForms is the guard that BITES. It asserts
// the two things a naive implementation gets wrong, and one it gets wrong
// silently:
//
//   - forms.workspace_id is synced EXPLICITLY (the 000045 trigger does not fire
//     on an account update), so the count of unscoped forms goes to 0; and
//   - the user ends with exactly ONE workspace and ONE account (Workspaces.Create
//     inserts an account of its own — issue #158 — so a naive call leaves the user
//     owning two, one of them unreachable).
//
// Neutralise the sync (drop the `UPDATE forms ...` from ProvisionLegacyWorkspace)
// and the first assertion goes red while everything else still passes — which is
// the point: the account link alone looks like a complete migration.
func TestProvisionLegacyWorkspaceMovesTheForms(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	email := "legacy-" + randSuffix() + "@example.test"
	user, account := legacyFixture(t, ctx, email, 2)

	if got, err := sp.LegacyUnscopedAccount(ctx, user); err != nil {
		t.Fatalf("LegacyUnscopedAccount: %v", err)
	} else if got != account {
		t.Fatalf("LegacyUnscopedAccount = %q, want %q", got, account)
	}

	ws, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil {
		t.Fatalf("ProvisionLegacyWorkspaceForOwner: %v", err)
	}
	if ws == nil {
		t.Fatal("nothing was provisioned for a user with forms and no workspace")
	}

	// The slug follows Wan's rule: first 5 characters of the email local part.
	if want := SlugFromEmailLocalPart(email, user); ws.Slug != want {
		t.Fatalf("slug = %q, want %q (first 5 chars of the email local part)", ws.Slug, want)
	}
	if ws.Name == "" {
		t.Fatal("the provisioned workspace has an empty name")
	}

	// THE ASSERTION THAT BITES: no form may keep a NULL workspace_id.
	var unscoped int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id IS NULL`, account).
		Scan(&unscoped); err != nil {
		t.Fatalf("count unscoped forms: %v", err)
	}
	if unscoped != 0 {
		t.Fatalf("%d form(s) kept workspace_id IS NULL after their account was linked to %q — "+
			"the explicit sync is missing, and trg_forms_sync_workspace_id does not fire on an "+
			"account UPDATE (only on INSERT / UPDATE OF account_id on forms)", unscoped, ws.Slug)
	}
	// And they point at the NEW workspace, not merely at "not null".
	var moved int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id=$2`, account, ws.ID).
		Scan(&moved); err != nil {
		t.Fatalf("count moved forms: %v", err)
	}
	if moved != 2 {
		t.Fatalf("%d of 2 forms point at the provisioned workspace %q", moved, ws.Slug)
	}

	// Exactly one workspace and one account (issue #158 trap).
	var workspaces, accounts int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if err := p.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&accounts); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if workspaces != 1 {
		t.Fatalf("user owns %d workspaces after provisioning, want exactly 1", workspaces)
	}
	if accounts != 1 {
		t.Fatalf("user owns %d accounts after provisioning, want exactly 1 — the provision minted "+
			"a second account instead of reusing the legacy one", accounts)
	}

	// The user can now reach the workspace (that is the whole point) and is its
	// admin member (the 000025 convention).
	list, err := sp.ByMember(ctx, user)
	if err != nil {
		t.Fatalf("ByMember: %v", err)
	}
	if len(list) != 1 || list[0].WorkspaceID != ws.ID {
		t.Fatalf("ByMember = %+v, want the provisioned workspace %q", list, ws.ID)
	}
	if list[0].Role != RoleAdmin {
		t.Fatalf("role = %q, want %q", list[0].Role, RoleAdmin)
	}
}

// TestProvisionLegacyWorkspaceIsIdempotent pins that re-running changes nothing:
// the second call finds no applicable account (the owner now HAS a workspace) and
// returns (nil, nil) rather than provisioning a second workspace or stealing the
// forms into one.
func TestProvisionLegacyWorkspaceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	email := "idem-" + randSuffix() + "@example.test"
	user, account := legacyFixture(t, ctx, email, 1)

	first, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil || first == nil {
		t.Fatalf("first provision: ws=%v err=%v", first, err)
	}
	second, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if second != nil {
		t.Fatalf("re-running provisioned a SECOND workspace (%q) — it must be a no-op", second.Slug)
	}

	var workspaces, accounts int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if err := p.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&accounts); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if workspaces != 1 || accounts != 1 {
		t.Fatalf("after two provisions: %d workspaces / %d accounts, want 1 / 1", workspaces, accounts)
	}
	var moved int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id=$2`, account, first.ID).
		Scan(&moved); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	if moved != 1 {
		t.Fatalf("%d of 1 form still points at the first workspace", moved)
	}
}

// TestProvisionLegacyWorkspaceNeverTouchesAUserWhoAlreadyHasOne is the second
// half of the idempotence guard, and the one the plain re-run test cannot see.
//
// Re-running the provision is a no-op for a trivial reason: the first run linked
// the account, so `accounts.workspace_id IS NULL` already excludes it. That makes
// the re-run pass even with the `NOT EXISTS (... workspaces ...)` clause deleted —
// verified: the clause was removed and the re-run test stayed green.
//
// The shape that actually needs the clause is a user who already owns a workspace
// AND has a stray unscoped form-owning account (his own workspace, plus an account
// left over from before). Without the clause the rule fires for him: a SECOND
// workspace is created and the stray account's forms are stolen into it. Wan's rule
// is scoped to users who have no workspace at all, so the answer here must be "not
// applicable" and the stray rows must be left exactly as they were.
func TestProvisionLegacyWorkspaceNeverTouchesAUserWhoAlreadyHasOne(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	email := "hasws-" + randSuffix() + "@example.test"
	user, _ := legacyFixture(t, ctx, email, 1)

	// He creates/gets a workspace by some other route (the picker, the MCP tool).
	existing, err := sp.Create(ctx, user, "existing", "hasws-"+randSuffix())
	if err != nil {
		t.Fatalf("create the pre-existing workspace: %v", err)
	}

	// And he still has a stray unscoped form-owning account.
	var stray string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1,'stray','stray-' || md5(random()::text)) RETURNING id`, user).Scan(&stray); err != nil {
		t.Fatalf("create the stray account: %v", err)
	}
	var strayForm string
	if err := p.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, theme, status)
		 VALUES ($1, 'stray-' || md5(random()::text), 'stray form', '{}'::jsonb, '{}'::jsonb, 'published')
		 RETURNING id`, stray).Scan(&strayForm); err != nil {
		t.Fatalf("create the stray form: %v", err)
	}

	// The rule must not apply: he HAS a workspace.
	if got, err := sp.LegacyUnscopedAccount(ctx, user); err != nil {
		t.Fatalf("LegacyUnscopedAccount: %v", err)
	} else if got != "" {
		t.Fatalf("reported account %q as provisionable for a user who ALREADY owns a workspace — "+
			"Wan's rule is for users with no workspace; acting here would create a second one", got)
	}
	got, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got != nil {
		t.Fatalf("provisioned a SECOND workspace (%q) for a user who already had one", got.Slug)
	}

	// Nothing was stolen and nothing moved.
	var workspaces int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).
		Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaces != 1 {
		t.Fatalf("%d workspaces for this user, want 1 (the one he already had)", workspaces)
	}
	var strayWorkspace *string
	if err := p.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, stray).
		Scan(&strayWorkspace); err != nil {
		t.Fatalf("read the stray account: %v", err)
	}
	if strayWorkspace != nil && *strayWorkspace != existing.ID {
		t.Fatalf("the stray account was relinked to %q, want it untouched", *strayWorkspace)
	}
	var formWorkspace *string
	if err := p.QueryRow(ctx, `SELECT workspace_id FROM forms WHERE id=$1`, strayForm).
		Scan(&formWorkspace); err != nil {
		t.Fatalf("read the stray form: %v", err)
	}
	if formWorkspace != nil {
		t.Fatalf("the stray form was moved into workspace %q — the rule must leave it alone", *formWorkspace)
	}
}

// TestProvisionLegacyWorkspaceLeavesAFreshUserAlone is the boundary against the
// ask-first half: a user with NO forms must be ASKED, never provisioned. If this
// breaks, every new signup silently gets a workspace and the first-run prompt
// becomes dead code.
func TestProvisionLegacyWorkspaceLeavesAFreshUserAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	email := "fresh-" + randSuffix() + "@example.test"
	user, account := legacyFixture(t, ctx, email, 0)

	if got, err := sp.LegacyUnscopedAccount(ctx, user); err != nil {
		t.Fatalf("LegacyUnscopedAccount: %v", err)
	} else if got != "" {
		t.Fatalf("a user with NO forms reported as provisionable (%q) — he must be ASKED to "+
			"create a workspace, not given one", got)
	}
	ws, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil {
		t.Fatalf("ProvisionLegacyWorkspaceForOwner: %v", err)
	}
	if ws != nil {
		t.Fatalf("a user with no forms was provisioned workspace %q — that removes the ask-first prompt", ws.Slug)
	}
	var workspaces int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaces != 0 {
		t.Fatalf("a fresh user ended with %d workspaces, want 0", workspaces)
	}
	var wsID *string
	if err := p.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, account).Scan(&wsID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if wsID != nil {
		t.Fatalf("the fresh user's account was linked to %q", *wsID)
	}
}

// TestProvisionLegacyWorkspaceExcludesTheTemplateGallery is TRAP 1 as an
// assertion rather than prose.
//
// templates@system.jomform owns jomform-templates, which owns the 14 published
// is_template rows and no workspace ON PURPOSE: migration 000059 moved those rows
// there specifically so they STOP matching the workspace-scoped published queries.
// They back FormStore.ListTemplates (no account filter) → the template picker, the
// landing gallery and GET /api/templates. Auto-provisioning that account would
// re-expose all 14 as a public storefront and empty the gallery — the exact
// opposite of 000059.
//
// Neutralise the exclusion (drop `NOT (u.email = ANY(...))` from
// LegacyProvisionedEmails' predicate) and this test goes red.
func TestProvisionLegacyWorkspaceExcludesTheTemplateGallery(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	// Use the REAL system identity, which migration 000027 already seeds (its
	// slug is reserved and the account is never exposed to login or the
	// dashboard), rather than invented rows: the exclusion is keyed on the
	// identity that exists in every migrated database, so a test fixture of its
	// own would prove a different thing from the one that runs on prod.
	var user, account string
	if err := p.QueryRow(ctx,
		`SELECT u.id, a.id FROM users u JOIN accounts a ON a.owner_user_id = u.id
		  WHERE u.email = 'templates@system.jomform'`).Scan(&user, &account); err != nil {
		t.Fatalf("resolve the seeded system templates account: %v", err)
	}

	// Seed gallery-shaped rows on it. They are inserted with their OWN
	// workspace_id explicitly NULL so the assertion below can tell "the provision
	// left them alone" from "the INSERT trigger filled the column in".
	beforeTotal, beforeTpl := countGalleryRows(t, ctx, account)
	const seeded = 3
	for i := 0; i < seeded; i++ {
		if _, err := p.Exec(ctx,
			`INSERT INTO forms (account_id, workspace_id, slug, title, config, theme, status, is_template)
			 VALUES ($1, NULL, 'legacy-tpl-' || md5(random()::text), 'legacy template', '{}'::jsonb, '{}'::jsonb, 'published', true)`,
			account); err != nil {
			t.Fatalf("seed gallery row %d: %v", i, err)
		}
	}

	if got, err := sp.LegacyUnscopedAccount(ctx, user); err != nil {
		t.Fatalf("LegacyUnscopedAccount: %v", err)
	} else if got != "" {
		t.Fatalf("the system templates account was reported as provisionable (%q) — giving it a "+
			"workspace re-exposes the gallery as a public storefront (see migration 000059)", got)
	}
	ws, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil {
		t.Fatalf("ProvisionLegacyWorkspaceForOwner: %v", err)
	}
	if ws != nil {
		t.Fatalf("the template gallery account was provisioned workspace %q — the published "+
			"templates would become a public storefront and the picker's gallery would empty", ws.Slug)
	}

	// The rows are untouched: same count, still published templates, still unscoped.
	afterTotal, afterTpl := countGalleryRows(t, ctx, account)
	if afterTotal != beforeTotal+seeded || afterTpl != beforeTpl+seeded {
		t.Fatalf("gallery rows changed: total %d→%d, published templates %d→%d (seeded %d)",
			beforeTotal, afterTotal, beforeTpl, afterTpl, seeded)
	}
	var moved int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id IS NOT NULL`, account).
		Scan(&moved); err != nil {
		t.Fatalf("count scoped gallery rows: %v", err)
	}
	if moved != 0 {
		t.Fatalf("%d gallery row(s) gained a workspace_id — the gallery must stay unscoped so it "+
			"does not start matching the workspace-scoped published queries (migration 000059)", moved)
	}
}

func countGalleryRows(t *testing.T, ctx context.Context, account string) (total, templates int) {
	t.Helper()
	p := testPool(t)
	if err := p.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE is_template AND status='published')
		 FROM forms WHERE account_id=$1`, account).Scan(&total, &templates); err != nil {
		t.Fatalf("count gallery rows: %v", err)
	}
	return total, templates
}

// TestLegacySlugCandidatesAreDeterministicAndTerminate pins the collision ladder.
// Prod has the real case: owner00@example.com and owner00+test@
// gmail.com both reduce to "owner", and a user who never chose a slug must not be
// shown "slug already in use". The sequence must be identical on every call
// (deterministic) and finite (terminating).
func TestLegacySlugCandidatesAreDeterministicAndTerminate(t *testing.T) {
	const userID = "11111111-2222-3333-4444-555555555555"

	a := legacySlugCandidates("owner", userID)
	b := legacySlugCandidates("owner", userID)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("the ladder is not deterministic:\n %v\n %v", a, b)
	}
	if len(a) < 2 || len(a) > 20 {
		t.Fatalf("ladder length %d — it must be finite and bounded", len(a))
	}
	if a[0] != "owner" {
		t.Fatalf("first candidate = %q, want the wanted slug", a[0])
	}
	// Every candidate must be a slug the API itself would accept, otherwise the
	// retry could hand the user a workspace whose own host 400s.
	seen := map[string]bool{}
	for _, c := range a {
		if _, err := ValidateWorkspaceSlug(c); err != nil {
			t.Fatalf("candidate %q is not a valid workspace slug: %v", c, err)
		}
		if seen[c] {
			t.Fatalf("candidate %q appears twice", c)
		}
		seen[c] = true
	}
	if !strings.HasSuffix(a[len(a)-1], userID[:8]) {
		t.Fatalf("last candidate %q does not carry the user-id suffix, so a full ladder is not "+
			"guaranteed distinct from another user's", a[len(a)-1])
	}
}

// TestProvisionLegacyWorkspaceResolvesASlugCollision proves the ladder is actually
// USED, not merely defined: when the wanted slug is taken, provisioning succeeds
// on the next candidate instead of surfacing a unique-violation.
func TestProvisionLegacyWorkspaceResolvesASlugCollision(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	email := "clash-" + randSuffix() + "@example.test"
	user, _ := legacyFixture(t, ctx, email, 1)

	want := SlugFromEmailLocalPart(email, user)
	// Squat the wanted slug with somebody else's workspace.
	var other string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('squat-'||md5(random()::text)||'@example.test','x') RETURNING id`).
		Scan(&other); err != nil {
		t.Fatalf("create squatting user: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1,'squat',$2)`, other, want); err != nil {
		t.Fatalf("squat the slug: %v", err)
	}

	ws, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user)
	if err != nil {
		t.Fatalf("provisioning onto a taken slug failed instead of retrying: %v", err)
	}
	if ws == nil {
		t.Fatal("nothing was provisioned")
	}
	if ws.Slug == want {
		t.Fatalf("slug = %q, which was already taken", ws.Slug)
	}
	if _, err := ValidateWorkspaceSlug(ws.Slug); err != nil {
		t.Fatalf("retried slug %q is invalid: %v", ws.Slug, err)
	}
	if !strings.HasPrefix(ws.Slug, want) {
		t.Fatalf("retried slug %q does not derive from the wanted slug %q", ws.Slug, want)
	}
}

// TestLegacyProvisionReportsRealErrorsInsteadOfSwallowingThem pins the
// distinction between "the rule does not apply" and "the query failed".
//
// Both used to collapse into the same empty answer, which is a silent failure of
// exactly the kind this card removes: a broken query makes the provision report
// "nothing to do", every caller stays healthy, and the user is never migrated.
// The bite proof found it — with the SQL broken, the store-level tests still
// passed because they only asserted "no workspace was created".
//
// This drives a genuinely broken statement through both entry points and requires
// an error rather than an empty success. It needs no database.
func TestLegacyProvisionReportsRealErrorsInsteadOfSwallowingThem(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	// A cancelled context is a real failure, not a missing row.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := sp.LegacyUnscopedAccount(cancelled, "11111111-2222-3333-4444-555555555555"); err == nil {
		t.Fatal("LegacyUnscopedAccount returned no error for a cancelled context — a real failure is " +
			"being reported as \"the rule does not apply\", so a broken query would silently stop " +
			"provisioning while every caller looked healthy")
	}
	if _, err := sp.ProvisionLegacyWorkspaceForOwner(cancelled, "11111111-2222-3333-4444-555555555555"); err == nil {
		t.Fatal("ProvisionLegacyWorkspaceForOwner returned no error for a cancelled context — same " +
			"silent-failure class: it must not report a failed query as \"nothing to do\"")
	}

	// And the control: a user the rule genuinely does not apply to is still a
	// clean no-op, not an error. Without this half, "always error" would pass.
	fresh := "silent-" + randSuffix() + "@example.test"
	user, _ := legacyFixture(t, ctx, fresh, 0)
	if _, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, user); err != nil {
		t.Fatalf("a user the rule does not apply to produced an error: %v — \"not applicable\" must "+
			"stay a no-op", err)
	}
}

// TestSlugFromEmailLocalPartRule pins the derivation itself, including the two
// cases the card called out as needing an explicit decision.
func TestSlugFromEmailLocalPartRule(t *testing.T) {
	const uid = "abcdef12-0000-0000-0000-000000000000"
	for _, tc := range []struct {
		email, want, why string
	}{
		{"member00@example.com", "membe", "first 5 chars of the local part"},
		{"alpha00@example.com", "alpha", "first 5 chars"},
		{"bravo00@example.com", "bravo", "first 5 chars"},
		// The collision pair on prod: both reduce to the same 5 characters, and
		// the ladder (not this function) is what separates them.
		{"owner00@example.com", "owner", "collision pair, half 1"},
		{"owner00+test@example.com", "owner", "collision pair, half 2"},
		// Shorter than 5: take what exists rather than emitting a 2-char slug.
		{"wan@jomform.com", "wan", "local part shorter than 5 keeps its own length"},
		{"ab@jomform.com", "ababcdef12", "shorter than the 3-char minimum is padded from the user id"},
		// Non-alphanumerics: 5 RAW characters first, then sanitise. A dot/plus
		// becomes a dash/strip, so the window is over the RAW local part.
		{"a.b.c.d.e@x.test", "a-b-c", "5 raw chars then sanitise (not sanitise-then-window)"},
		{"+test.thing@x.test", "test", "leading non-alphanumerics are stripped and trimmed"},
	} {
		got := SlugFromEmailLocalPart(tc.email, uid)
		if got != tc.want {
			t.Errorf("SlugFromEmailLocalPart(%q) = %q, want %q (%s)", tc.email, got, tc.want, tc.why)
		}
		if _, err := ValidateWorkspaceSlug(got); err != nil {
			t.Errorf("SlugFromEmailLocalPart(%q) = %q, which the slug contract rejects: %v", tc.email, got, err)
		}
	}
}
