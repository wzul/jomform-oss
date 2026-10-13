package chat

// THE POST-CLAIM ROLE REFUSAL MUST KEEP ITS CAUSE (card t_1900adef).
//
// THE DEFECT, measured on live prod (v1.19.1) by the QA lane. A read_only member
// of a workspace confirmed a prepared change. The confirm ran AFTER the token was
// claimed, the write-role gate (mcpsrv.requireWriteRoleIn) refused it, and what
// reached the user was:
//
//	confirm response : 400 {"code":"request.badRequest","error":"cadangan tidak dapat dilaksanakan"}
//	card badge       : "Did not go through"
//	two DOM nodes    : "The change could not be applied. Please try again."
//
// The refusal's own cause — "perlu peranan manage or admin dalam workspace ini" —
// existed on the server and reached neither the wire nor the page, and the one
// piece of advice on screen told the user to retry an action whose token was
// already spent.
//
// WHAT THIS FILE PINS, over the REAL MCP server and the REAL database:
//
//  1. A confined write refused by the workspace ROLE comes back as the TYPED
//     ErrProposalRoleRefused, not as the anonymous `server menolak: %s` wrap every
//     other post-claim refusal gets. That distinction is the whole mechanism: the
//     REST edge maps this sentinel to the wire code `role.manageRequired`, and it
//     can only do that if the lane tells it which refusal fired.
//  2. The gate's OWN sentence survives into the error (`mcpsrv.
//     RoleManageRequiredMessage`), rather than being replaced by a paraphrase —
//     the wire prose the page's cause comes from.
//  3. The token IS spent, which is what makes the page's "this card cannot be used
//     again" copy true and its removed retry advice correct.
//  4. A CONTROL: the OWNER confirming the same shape SUCCEEDS. Without it, a lane
//     that refused everything would satisfy (1)–(3) while proving nothing about
//     the role.
//
// Integration against a real Postgres (the standard test env), skipped without it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// memberIdentity is the context of a SECOND person — a read_only member of the
// workspace that owns the fixture's account — built the way the REST edge builds
// it: the identity plus the per-call workspace scope (issue #747). The account is
// the fixture's; the USER is the member, which is exactly the pair the role gate
// reads (mcpsrv.roleForAccount → Workspaces.RoleByAccount).
func (f *acceptanceFixture) memberIdentity(userID string, scopes []string) context.Context {
	ctx := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: userID, Scopes: scopes, Agent: "in-app-chat",
	})
	return mcpsrv.WithCallScope(ctx, &mcpsrv.CallScope{Accounts: []string{f.accountID}})
}

// addMemberWithRole adds a NEW person to the fixture's workspace with the given
// role and returns their user id.
func (f *acceptanceFixture) addMemberWithRole(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	var memberID, workspaceID string
	if err := f.pool.QueryRow(ctx,
		`SELECT workspace_id FROM accounts WHERE id = $1`, f.accountID).Scan(&workspaceID); err != nil {
		t.Fatalf("read the workspace owning the fixture account: %v", err)
	}
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('chat-member-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if err := dbstore.NewWorkspaceMemberStore(f.pool).AddMember(ctx, workspaceID, memberID, role); err != nil {
		t.Fatalf("add member with role %q: %v", role, err)
	}
	// The fixture is only meaningful if the role really took: a row that silently
	// did not land would make the refusal below come from somewhere else.
	got, err := dbstore.NewWorkspaceStore(f.pool).RoleByAccount(ctx, f.accountID, memberID)
	if err != nil || got != role {
		t.Fatalf("RoleByAccount = %q (err %v), want %q — the fixture is not in the state this test claims",
			got, err, role)
	}
	return memberID
}

// consumedAt reads the proposal row's consumed_at back out of Postgres, so the
// "the token is spent" half is measured rather than assumed.
func (f *acceptanceFixture) consumedAt(t *testing.T, userID, token string) bool {
	t.Helper()
	var spent bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT consumed_at IS NOT NULL FROM chat_write_proposals
		  WHERE user_id = $1 AND token_hash = $2`,
		userID, dbstore.HashProposalToken(token)).Scan(&spent); err != nil {
		t.Fatalf("read the proposal row's consumed_at: %v", err)
	}
	return spent
}

func TestConfirmRefusedByWorkspaceRoleKeepsTheCause(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	memberID := f.addMemberWithRole(t, dbstore.RoleReadOnly)
	r := f.proposalRunner(t)

	// The member's OWN proposal — the store scopes every read by user id, and the
	// card they are asked to confirm has to be theirs for the confirm to be about
	// their role at all. The propose lane is a READ lane: a member who may read the
	// workspace may be offered a change there.
	readCtx := f.memberIdentity(memberID, ReadOnlyScopes())
	card := f.proposeUpdateProduct(t, r, readCtx, "Should not land")

	// The confirmation runs under the HUMAN's identity with the WRITE scope, which
	// is what the REST edge builds (chatConfirmContextFor). Everything else — the
	// token, the hash, the workspace scope — is the production shape.
	writeCtx := f.memberIdentity(memberID, WriteScopes())
	_, err := r.Confirm(context.Background(), writeCtx, memberID, card.Token, card.ArgsHash)
	if err == nil {
		t.Fatal("a read_only member's confirmed write SUCCEEDED — the workspace role gate did not refuse it, " +
			"so the refusal this card is about cannot be reproduced at all")
	}
	// (1) THE CLASSIFICATION. This is what the REST edge maps to `role.manageRequired`;
	// the anonymous wrap every other post-claim refusal gets carries no code, and
	// the page is back to its generic sentence.
	if !errors.Is(err, ErrProposalRoleRefused) {
		t.Errorf("a write refused by the workspace role returned %v, want ErrProposalRoleRefused — the REST "+
			"edge cannot name the cause it cannot tell apart, so the refusal ships as `request.badRequest` "+
			"and the page prints the generic failure copy (the measured defect)", err)
	}
	// (2) THE CAUSE SURVIVES, in the GATE'S OWN words. A paraphrase here would be a
	// second copy of the refusal in a second package.
	if !strings.Contains(err.Error(), mcpsrv.RoleManageRequiredMessage) {
		t.Errorf("the refusal does not carry the gate's own sentence (%q): %v — the wire prose the user's "+
			"cause is derived from must be the reason the server actually gave",
			mcpsrv.RoleManageRequiredMessage, err)
	}
	// It must NOT also wear the generic wrap: an error that is both would let a
	// caller read either cause out of it.
	if strings.Contains(err.Error(), "server menolak:") {
		t.Errorf("the role refusal also carries the generic `server menolak:` wrap: %v", err)
	}
	// (3) THE TOKEN IS SPENT. This is why the page must not offer a retry on this
	// outcome and must say the card cannot be used again.
	if !f.consumedAt(t, memberID, card.Token) {
		t.Error("the refused proposal's token is UNSPENT — the refusal then happened before the claim, and " +
			"the page's \"this card cannot be used again\" copy would be false")
	}
	// And the write really did not land.
	if got := f.productName(t); got == "Should not land" {
		t.Errorf("the product was renamed to %q by a write the role gate refused", got)
	}

	// (4) THE CONTROL. The OWNER of the same workspace, same tool, same shape —
	// this is what proves the refusal above came from the ROLE and not from the
	// proposal lane, the schema, or the workspace scope.
	ownerCard := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Landed by the owner")
	if _, err := r.Confirm(context.Background(), f.confirmCtx(), f.userID, ownerCard.Token, ownerCard.ArgsHash); err != nil {
		t.Fatalf("the OWNER's confirmation of the same change was refused (%v) — the role refusal above "+
			"therefore says nothing about the role", err)
	}
	if got := f.productName(t); got != "Landed by the owner" {
		t.Errorf("the owner's confirmed rename did not land (product name %q) — the control did not run", got)
	}
}
