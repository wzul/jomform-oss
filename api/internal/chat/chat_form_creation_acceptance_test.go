package chat

// Card t_804040be — acceptance criteria 1 and 3, and the round trip that is the
// entire justification for allowing a creation at all.
//
// Wan's decision, verbatim:
//
//	"Assistant boleh buat sampai masuk ke bin. tapi bukan permanently delete dlm bin"
//	"Manusia sahaja boleh purge kekal — assistant HANYA boleh hantar ke bin, tak pernah padam kekal"
//
// The old exclusion in chat_tools.go refused form creation because "the chat has no
// delete, so a mistaken confirm leaves litter the user must clean up by hand". The
// bin dissolves that: a mistaken confirm is recoverable by the SAME chat. That claim
// is what this file has to earn — it is the whole reason the exclusion was lifted, so
// an assertion that the form merely EXISTS would leave the justification unproven
// and admit the tool on a promise.

import (
	"context"
	"errors"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// formBySlug reads a form back out of Postgres by slug. The database is the
// authority; the proposal result and the card are the runner's own words.
func (f *acceptanceFixture) formBySlug(t *testing.T, slug string) (id string, deleted bool, found bool) {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM forms WHERE slug=$1`, slug).Scan(&n); err != nil {
		t.Fatalf("count forms by slug: %v", err)
	}
	if n == 0 {
		return "", false, false
	}
	var deletedAt *string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id, deleted_at::text FROM forms WHERE slug=$1 ORDER BY created_at DESC LIMIT 1`, slug).
		Scan(&id, &deletedAt); err != nil {
		t.Fatalf("read form %q back: %v", slug, err)
	}
	return id, deletedAt != nil && *deletedAt != "", true
}

// proposeFormCreation drives the MODEL's own path — the dispatch of propose_write —
// and returns the card the user would be shown.
func (f *acceptanceFixture) proposeFormCreation(t *testing.T, r *Runner, ctx context.Context, title, slug string) *ProposalCard {
	t.Helper()
	args := `{"tool":"upsert_form","arguments":{"title":` + jsonString(title) +
		`,"slug":` + jsonString(slug) + `},"reason":"pengguna minta borang baru"}`
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the propose session: %v", err)
	}
	defer caller.close()
	st := &runState{}
	out, ref, err := r.dispatchIn(ctx, caller, toolCall("pf", ToolProposeWrite, args), st)
	if err != nil {
		t.Fatalf("dispatch propose_write(upsert_form): %v", err)
	}
	if ref != nil {
		t.Fatalf("propose_write was REFUSED for upsert_form (%s). It must be proposable once the "+
			"bin exists (card t_804040be): a refusal here means the capability Wan decided on is "+
			"not reachable at all", out)
	}
	card := st.lastCard()
	if card == nil || card.Token == "" {
		t.Fatalf("propose_write produced no token-bearing card for the browser (state=%+v, saw %s)",
			st, out)
	}
	return card
}

// runToolAsTheHuman dispatches a registered tool through the HUMAN's identity — the
// write scope the confirm path builds — which is how the bin round trip is driven.
// delete_form/restore_form are deliberately NOT proposable, so there is no card lane
// for them; what is under test is that they exist as the creation's inverse.
func (f *acceptanceFixture) runToolAsTheHuman(t *testing.T, r *Runner, tool, args string) (out string, refused bool) {
	t.Helper()
	writeCtx := f.confirmCtx()
	caller, err := r.newSession(writeCtx)
	if err != nil {
		t.Fatalf("open the write session for %s: %v", tool, err)
	}
	defer caller.close()
	out, refused, err = caller.call(writeCtx, tool, args)
	if err != nil {
		t.Fatalf("dispatch %s: %v", tool, err)
	}
	return out, refused
}

// TestChatProposesFormCreationInsteadOfExecutingIt is acceptance criterion 1: a chat
// turn asking for a form produces a PROPOSAL, and nothing exists until a human
// confirms. Asserting only the second half would pass on a build that created the
// form eagerly AND carded it.
func TestChatProposesFormCreationInsteadOfExecutingIt(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	slug := "chat-new-" + f.accountID[:8]
	title := "Borang dari chat"

	if _, _, found := f.formBySlug(t, slug); found {
		t.Fatalf("a form with slug %q already exists before the test starts — this test would prove "+
			"nothing", slug)
	}

	card := f.proposeFormCreation(t, r, f.proposalCtx(), title, slug)

	// ── After the PROPOSAL: no form. ──
	if id, _, found := f.formBySlug(t, slug); found {
		t.Fatalf("the form (%s) exists after the PROPOSAL alone — propose_write EXECUTED the "+
			"creation instead of preparing it", id)
	}
	if len(card.Fields) == 0 {
		t.Error("the proposal card lists no fields — the user is asked to confirm a creation they " +
			"were not shown, so there is nothing on the card to judge")
	}
	var storedTool, storedUser string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT tool, user_id FROM chat_write_proposals WHERE token_hash=$1`,
		dbstore.HashProposalToken(card.Token)).Scan(&storedTool, &storedUser); err != nil {
		t.Fatalf("the card's token has no stored proposal: %v", err)
	}
	if storedTool != "upsert_form" {
		t.Errorf("stored tool = %q, want upsert_form", storedTool)
	}
	if storedUser != f.userID {
		t.Errorf("the proposal is owned by %q, want the caller %q", storedUser, f.userID)
	}

	// ── The HUMAN confirms: only now does it exist. ──
	res, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash)
	if err != nil {
		t.Fatalf("confirm the form creation: %v", err)
	}
	if res.Tool != "upsert_form" {
		t.Fatalf("confirmed tool = %q, want upsert_form", res.Tool)
	}
	id, deleted, found := f.formBySlug(t, slug)
	if !found {
		t.Fatalf("after a confirmed proposal there is NO form with slug %q — the confirmation did "+
			"not execute the creation", slug)
	}
	if deleted {
		t.Error("the freshly created form is already in the bin")
	}

	// ── The card is spent. ──
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); !errors.Is(err, dbstore.ErrChatProposalConsumed) {
		t.Errorf("a second confirm returned %v, want ErrChatProposalConsumed — a spent token must "+
			"never be spent twice", err)
	}
	_ = id
}

// TestAMistakenFormCreationGoesToTheBinAndComesBack is acceptance criterion 3 and
// the reason the exclusion was lifted. The failure mode the old comment named was
// "litter the user must clean up by hand"; if the chat can bin the form AND restore
// it, there is no litter.
func TestAMistakenFormCreationGoesToTheBinAndComesBack(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	slug := "chat-oops-" + f.accountID[:8]

	card := f.proposeFormCreation(t, r, f.proposalCtx(), "Borang tersilap", slug)
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); err != nil {
		t.Fatalf("confirm the form creation: %v", err)
	}
	id, _, found := f.formBySlug(t, slug)
	if !found {
		t.Fatalf("setup failed: the confirmed creation produced no form with slug %q", slug)
	}

	// ── The mistake: the user bins it. ──
	if out, refused := f.runToolAsTheHuman(t, r, "delete_form", `{"form_id":`+jsonString(id)+`}`); refused {
		t.Fatalf("delete_form refused a form the same user just created: %s — the bin is what makes "+
			"a mistaken confirm recoverable, so it must accept it", out)
	}
	_, deleted, found := f.formBySlug(t, slug)
	if !found {
		t.Fatal("delete_form REMOVED the row instead of binning it — the form is gone and no restore " +
			"can bring it back, which is exactly what the chat must never be able to cause")
	}
	if !deleted {
		t.Fatal("delete_form left deleted_at NULL — the form was not binned")
	}

	// ── The undo: the same chat restores it, with the original slug. ──
	if out, refused := f.runToolAsTheHuman(t, r, "restore_form", `{"form_id":`+jsonString(id)+`}`); refused {
		t.Fatalf("restore_form could not bring back the binned form: %s — the round trip is the "+
			"entire justification for letting the assistant create a form", out)
	}
	restoredID, deleted, found := f.formBySlug(t, slug)
	if !found {
		t.Fatalf("after restore_form there is no form with slug %q — the round trip did not complete",
			slug)
	}
	if deleted {
		t.Fatal("restore_form left the form in the bin")
	}
	if restoredID != id {
		t.Errorf("restore produced a different form id (%s, want %s)", restoredID, id)
	}
}

// TestChatCannotReachAPurgeEvenWhenTheModelNamesOne drives the card's 'no purge'
// rule through the RUNNER, not the list: the model asks for purge_form, and the
// proposal lane must refuse it — for a form that really exists and really is in the
// bin, so the refusal cannot be a missing row's.
func TestChatCannotReachAPurgeEvenWhenTheModelNamesOne(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	slug := "chat-purge-" + f.accountID[:8]
	ctx := f.proposalCtx()

	card := f.proposeFormCreation(t, r, ctx, "Borang untuk purge", slug)
	if _, err := r.Confirm(ctx, f.confirmCtx(), f.userID, card.Token, card.ArgsHash); err != nil {
		t.Fatalf("confirm the form creation: %v", err)
	}
	id, _, found := f.formBySlug(t, slug)
	if !found {
		t.Fatalf("setup failed: no form with slug %q", slug)
	}
	if out, refused := f.runToolAsTheHuman(t, r, "delete_form", `{"form_id":`+jsonString(id)+`}`); refused {
		t.Fatalf("bin the form for the purge attempt: %s", out)
	}

	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the propose session: %v", err)
	}
	defer caller.close()
	args := `{"tool":"purge_form","arguments":{"form_id":` + jsonString(id) +
		`,"confirm_slug":` + jsonString(slug) + `},"reason":"hapus kekal"}`
	_, ref, derr := r.dispatchIn(ctx, caller, toolCall("pp", ToolProposeWrite, args), &runState{})
	if derr != nil {
		t.Fatalf("dispatch propose_write(purge_form): %v", derr)
	}
	if ref == nil {
		t.Fatal("the proposal lane ACCEPTED purge_form — the assistant can reach a permanent delete. " +
			"Wan: \"Manusia sahaja boleh purge kekal\"")
	}
	// The form must be untouched in the bin: a refusal that arrived AFTER the write
	// would look identical in the call result.
	_, deleted, still := f.formBySlug(t, slug)
	if !still {
		t.Fatal("purge_form DESTROYED the form even though the proposal was refused — the refusal " +
			"arrived after the write")
	}
	if !deleted {
		t.Error("the form is no longer in the bin after a refused purge")
	}
}
