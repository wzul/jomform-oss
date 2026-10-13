package dbstore

// THE ACCOUNT RULE ON PURGE (card t_9fe4c36a).
//
// WorkspaceStore.Purge used to delete every account whose workspace_id pointed at
// the trashed workspace. That conflates two rows: the account the workspace
// CREATED (Workspaces.Create inserts one per workspace — it must die with the
// workspace or it is a CHIP credential nobody owns) and the account that EXISTED
// BEFORE it (the ask-first create LINKS the signup account the user already owns
// — SoleEmptyUnscopedAccount + CreateForAccount — and ProvisionLegacyWorkspace
// links a legacy content-carrying one the same way).
//
// Found on prod by t_eff6e23a: a pre-existing unlinked account (created
// 2026-09-01, workspace_id NULL) was adopted by a first-workspace create; the
// purge that followed answered {"purged":{"accounts":1,...}} and the row was
// gone for good. It had to be restored by hand.
//
// WHAT THIS PINS — both directions of the boundary, because a rule that only
// asserted the preservation half would pass just as well if the purge stopped
// deleting ANY account, which would strand a born-with-the-workspace account and
// its CHIP credentials:
//
//  1. The ADOPTED account is NOT destroyed. It returns to workspace_id IS NULL —
//     the state it was in before the adoption — keeping its slug and its CHIP
//     material, and its api_key (the workspace's credential) goes with the
//     workspace, as it did before this change.
//  2. The account the workspace CREATED is still destroyed, and the purge refuses
//     nothing: the workspace row goes in the same call.
//  3. The two outcomes are REPORTED separately (`accounts` vs `accounts_unlinked`)
//     rather than folded into one total, so a caller is told which happened.
//  4. The unlinked account is not a stranded row: the exact call the next
//     first-workspace create makes (CreateForAccount) re-links it, and
//     AccountIsLive — the predicate audit attribution uses — counts it live.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestWorkspacePurgeDoesNotDestroyAnAdoptedLegacyAccount is the fix's test: a
// purge of a workspace that ADOPTED the caller's pre-existing account destroys
// the workspace and everything it held, but not that account.
//
// MUTATION EVIDENCE: put the pre-fix statement back
// (`DELETE FROM accounts WHERE workspace_id=$1` in WorkspaceStore.Purge, step 2)
// and this test fails at the first block — the adopted row is gone and
// purged[accounts] reads 1 instead of accounts_unlinked.
func TestWorkspacePurgeDoesNotDestroyAnAdoptedLegacyAccount(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	user := testOwnerUser(t, ctx, p)

	// ── 1. The pre-workspace account, in the shape the adoption links ─────────
	//
	// Signup creates it (auth.go) with no workspace and NO content; the CHIP
	// material is what the merchant has entered by the time the workspace is
	// purged, and it is the load-bearing part: a credential, not content, so
	// nothing else in the purge is allowed to carry it away.
	var adopted, adoptedKey string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, chip_brand_id, chip_secret_key_enc, chip_mode)
		 VALUES ($1, 'signup', 'adopted-' || md5(random()::text), 'brand-qa-1', 'deadbeefcafe', 'live')
		 RETURNING id`, user).Scan(&adopted); err != nil {
		t.Fatalf("create the signup account: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO api_keys (account_id, name, key_hash)
		 VALUES ($1, 'agent-key', 'h-' || md5(random()::text))
		 RETURNING id`, adopted).Scan(&adoptedKey); err != nil {
		t.Fatalf("create the signup account's key: %v", err)
	}

	// The ask-first path, exactly: this account is the one it reuses...
	found, err := sp.SoleEmptyUnscopedAccount(ctx, user)
	if err != nil {
		t.Fatalf("SoleEmptyUnscopedAccount: %v", err)
	}
	if found != adopted {
		t.Fatalf("SoleEmptyUnscopedAccount = %q, want the signup account %q — the fixture "+
			"is not in the adopted shape this test is about", found, adopted)
	}
	// ...and the create LINKS it rather than inserting a second account.
	adopting, err := sp.CreateForAccount(ctx, user, "Adopted", "adopted-ws-"+randSuffix(), found)
	if err != nil {
		t.Fatalf("CreateForAccount: %v", err)
	}

	// ── 2. A control workspace whose account the workspace itself created ─────
	control, err := sp.Create(ctx, user, "Control", "control-ws-"+randSuffix())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var controlAcc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, control.ID).
		Scan(&controlAcc); err != nil {
		t.Fatalf("read the control workspace's account: %v", err)
	}
	// A third workspace, only so that both targets can be binned: the delete
	// refuses to take the owner's LAST LIVE workspace.
	if _, err := sp.Create(ctx, user, "Keeper", "keeper-ws-"+randSuffix()); err != nil {
		t.Fatalf("create the keeper workspace: %v", err)
	}

	// ── 3. The discriminator the rule turns on, asserted rather than assumed ──
	//
	// If a future write path ever inserted an account FOR a workspace with an
	// older created_at, this is the assertion that says so instead of leaving the
	// purge to decide it silently.
	accountTimes := func(accID, wsID string) (time.Time, time.Time) {
		t.Helper()
		var accCreated, wsCreated time.Time
		if err := p.QueryRow(ctx, `SELECT created_at FROM accounts WHERE id=$1`, accID).
			Scan(&accCreated); err != nil {
			t.Fatalf("read account created_at: %v", err)
		}
		if err := p.QueryRow(ctx, `SELECT created_at FROM workspaces WHERE id=$1`, wsID).
			Scan(&wsCreated); err != nil {
			t.Fatalf("read workspace created_at: %v", err)
		}
		return accCreated, wsCreated
	}
	if accCreated, wsCreated := accountTimes(adopted, adopting.ID); !accCreated.Before(wsCreated) {
		t.Fatalf("the adopted account (created_at %s) is NOT older than the workspace it was "+
			"linked into (%s) — the purge's rule keys on exactly this ordering, so the fixture "+
			"no longer exercises the adopted shape", accCreated, wsCreated)
	}
	if accCreated, wsCreated := accountTimes(controlAcc, control.ID); accCreated.Before(wsCreated) {
		t.Fatalf("the control account (created_at %s) PREDATES the workspace that created it "+
			"(%s) — an account created FOR a workspace is never older than it (Create inserts "+
			"the workspace first), so this fixture would silently test the adopted branch twice",
			accCreated, wsCreated)
	}

	// ── 4. Bin both, then purge the ADOPTED one first ────────────────────────
	for _, id := range []string{adopting.ID, control.ID} {
		if _, err := sp.Delete(ctx, id, user, ""); err != nil {
			t.Fatalf("bin %s: %v", id, err)
		}
	}
	purged, err := sp.Purge(ctx, adopting.ID, user)
	if err != nil {
		t.Fatalf("Purge (adopted): %v", err)
	}
	if purged["accounts_unlinked"] != 1 {
		t.Errorf("purged[accounts_unlinked] = %d, want 1 — the purge did not report that it "+
			"UNLINKED the adopted account, so a caller cannot tell the two outcomes apart",
			purged["accounts_unlinked"])
	}
	if purged["accounts"] != 0 {
		t.Errorf("purged[accounts] = %d, want 0 — the adopted account must not be deleted",
			purged["accounts"])
	}
	if purged["workspaces"] != 1 {
		t.Errorf("purged[workspaces] = %d, want 1 (the workspace itself must still go)", purged["workspaces"])
	}

	// THE ASSERTION THIS CARD EXISTS FOR: the account row is still there, back in
	// the state the adoption found it in, with its identity and CHIP material.
	var wsID *string
	var slug, brand, secret, mode string
	if err := p.QueryRow(ctx,
		`SELECT workspace_id, slug, coalesce(chip_brand_id,''), coalesce(chip_secret_key_enc,''), chip_mode
		   FROM accounts WHERE id=$1`, adopted).
		Scan(&wsID, &slug, &brand, &secret, &mode); err != nil {
		if err == pgx.ErrNoRows {
			t.Fatalf("the purge DESTROYED the adopted account %s — it existed before the "+
				"workspace (created at signup, linked by the ask-first create), and the purge "+
				"is the hard delete, so nothing in the product can bring it back", adopted)
		}
		t.Fatalf("read the adopted account back: %v", err)
	}
	if wsID != nil {
		t.Errorf("the adopted account's workspace_id = %q after the purge, want NULL — it must "+
			"return to the unlinked state it had before the adoption", *wsID)
	}
	if brand != "brand-qa-1" || secret != "deadbeefcafe" || mode != "live" {
		t.Errorf("the purged account's CHIP material = (%q, %q, %q), want (brand-qa-1, "+
			"deadbeefcafe, live): the purge names the WORKSPACE, so it must not carry away the "+
			"merchant's own credentials", brand, secret, mode)
	}
	if slug == "" {
		t.Error("the adopted account lost its slug")
	}
	if live, err := sp.AccountIsLive(ctx, adopted); err != nil {
		t.Fatalf("AccountIsLive: %v", err)
	} else if !live {
		t.Error("AccountIsLive(adopted) = false after the purge — an account with no workspace " +
			"is live by definition, so audit attribution would skip the surviving account")
	}

	// The workspace's own credential is NOT part of that preservation: api_keys
	// are deleted by account_id in step 1, which is the boundary stated beside
	// the rule in workspace.go.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM api_keys WHERE id=$1`, adoptedKey); n != 0 {
		t.Error("the adopted account's api_key survived the purge — keys are the workspace's " +
			"credentials and step 1 deletes them by account_id; only the ACCOUNT is preserved")
	}
	// And purging the adopted workspace must not have reached the other one.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM accounts WHERE id=$1`, controlAcc); n != 1 {
		t.Error("the purge of the adopted workspace destroyed ANOTHER workspace's account")
	}

	// ── 5. The other direction: an account the workspace created still dies ──
	purged, err = sp.Purge(ctx, control.ID, user)
	if err != nil {
		t.Fatalf("Purge (control): %v", err)
	}
	if purged["accounts"] != 1 || purged["accounts_unlinked"] != 0 {
		t.Errorf("purged = %v, want accounts=1 and accounts_unlinked=0 — an account created FOR "+
			"its workspace must still die with it, or the purge would strand a CHIP credential "+
			"on a row nothing can reach", purged)
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM accounts WHERE id=$1`, controlAcc); n != 0 {
		t.Error("the account the workspace CREATED survived its workspace's purge")
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM accounts WHERE id=$1`, adopted); n != 1 {
		t.Error("the second purge destroyed the adopted account")
	}

	// ── 6. The preserved account is reusable, not stranded ────────────────────
	//
	// This is the stated consequence of the rule: the unlinked account is exactly
	// what SoleEmptyUnscopedAccount looks for, so the next first-workspace create
	// links it through the same CreateForAccount call the ask-first path uses.
	again, err := sp.CreateForAccount(ctx, user, "Again", "again-ws-"+randSuffix(), adopted)
	if err != nil {
		t.Fatalf("re-link the preserved account (the next first-workspace create): %v", err)
	}
	var relinked *string
	if err := p.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, adopted).
		Scan(&relinked); err != nil {
		t.Fatalf("read the re-linked account: %v", err)
	}
	if relinked == nil || *relinked != again.ID {
		t.Errorf("the preserved account's workspace_id = %v after being reused, want %q — a row "+
			"the product cannot reuse would be stranded rather than preserved", relinked, again.ID)
	}
}
