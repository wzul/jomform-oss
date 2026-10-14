package chat

// THE ARGUMENT-SHAPE DEFECT (card t_623676d1).
//
// WHAT WAS WRONG. The card's object row was rendered from ONE hardcoded key —
// `args["product_id"]` — with a plain `if id != ""`, and NOTHING checked the
// arguments against the target tool. So a proposal that keyed the object as `id`
// (which is exactly what the chat's own READ tools return: mcpsrv.handlers.go's
// productOut is `json:"id"`, while update_product's updateProductIn requires
// `product_id`) rendered a card whose summary asserts "the product fields listed
// below will be changed" with NO product row on it — and whose Confirm then
// answered a bare 400 and SET consumed_at, spending a token on a decision that
// could never be carried out. The user was shown a retry hint for a card that can
// never succeed.
//
// WHAT THESE TESTS PIN, and the two mechanisms that hold it:
//
//	1. the ARGUMENTS are checked against the TARGET TOOL'S OWN REGISTERED SCHEMA
//	   at propose time (so the refusal reaches the MODEL, which is the only party
//	   that can correct a mis-keyed object), and
//	2. the CARD refuses to render for a tool whose declared object key is absent
//	   or blank (so "a card is never wrong about WHICH object" is enforced, not
//	   asserted), and
//	3. CONFIRM re-checks the STORED arguments before the claim, so a row that
//	   entered under an older contract is refused with the token UNSPENT.
//
// THE BITE. `TestProposalArgsGuardBitesOnTheRequiredKey` mutates ONE value — it
// drops `required` from a copy of the real registry schema — and the SAME
// arguments flip from refused to accepted. The predicate is the production
// `validateAgainstToolSchema`, driven with the schema the live registry serves, so
// a mutation that weakened the check (e.g. comparing against a hand-written alias
// list) would leave the first half green while the second half stopped flipping.
//
// Integration against a real Postgres, skipped without the standard test env.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func requireArgShapeDB(t *testing.T) {
	t.Helper()
	requireAcceptanceDB(t)
}

// TestChatProposeWriteRefusesAnObjectKeyTheToolDoesNotAccept is the end-to-end
// half of requirement (a): the MODEL's own path, over the REAL server, with the
// mis-keyed shape the shipped data model invites.
//
// The refusal must be a REFUSAL, not a card: the proposal lane is the one place
// that can still tell the model "the tool wants product_id", and a card here is a
// decision the user cannot act on.
func TestChatProposeWriteRefusesAnObjectKeyTheToolDoesNotAccept(t *testing.T) {
	requireArgShapeDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	ctx := f.proposalCtx()
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the propose session: %v", err)
	}
	defer caller.close()

	// The reproduction, verbatim: the object keyed `id`, exactly what list_products
	// returned one turn earlier.
	args := `{"tool":"update_product","arguments":{"id":` + jsonString(f.productID) +
		`,"name":"QA ArgShape Renamed"},"reason":"menukar nama produk"}`

	// POSITIVE CONTROL FIRST. Without it, a lane that refused EVERYTHING would
	// satisfy the assertion below while proving nothing about the key.
	good := `{"tool":"update_product","arguments":{"product_id":` + jsonString(f.productID) +
		`,"name":"Good Shape"},"reason":"menukar nama produk"}`
	if out, ref, derr := r.proposeWrite(ctx, caller, good, &runState{}); derr != nil || ref != nil {
		t.Fatalf("the CORRECTLY keyed proposal was refused (err=%v refused=%v out=%s) — the refusal "+
			"below would then say nothing about the key", derr, ref != nil, out)
	}

	out, ref, derr := r.proposeWrite(ctx, caller, args, &runState{})
	if derr != nil {
		t.Fatalf("proposeWrite(mis-keyed): %v", derr)
	}
	if ref == nil {
		t.Fatalf("propose_write ACCEPTED an argument set the target tool would never accept (%s). "+
			"The card it produced names NO product, so the user is asked to confirm a change that "+
			"cannot be carried out — and Confirm then spends the token refusing it.", out)
	}
	// THE UI'S HALF: the step's key, not a sentence. The page resolves it in the
	// visitor's language (card t_3839995c) — the server cannot.
	if ref.key != keyRefusedProposal {
		t.Errorf("the refused proposal carries %q, want %q", ref.key, keyRefusedProposal)
	}
	// The refusal must be usable BY THE MODEL: it is the only party that can fix a
	// mis-keyed object, so the message has to say which tool and what the schema
	// objected to. A bare "tidak dibenarkan" would leave the model guessing.
	if !strings.Contains(out, "update_product") {
		t.Errorf("the refusal does not name the tool (%q) — the model cannot tell which call to fix", out)
	}
	if !strings.Contains(out, "required") && !strings.Contains(out, "product_id") {
		t.Errorf("the refusal does not say what the schema requires (%q) — the schema's own message is "+
			"the model's correction cue, and without it the model can only retry the same call", out)
	}

	// ── And NOTHING was recorded: no token was minted for a dead card. ──
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_write_proposals WHERE user_id=$1`, f.userID).Scan(&n); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if n != 1 {
		t.Errorf("%d proposal row(s) exist, want exactly 1 — the ONE from the positive control. A "+
			"refused proposal must leave no token behind: a row here is an authorisation the user "+
			"was never shown and that nothing can carry out.", n)
	}
}

// TestChatCardNeverClaimsAnObjectItCannotName is requirement (a)'s rendering half,
// driven through Describe — the ONE renderer both propose and confirm consult.
//
// The defect was a card whose SUMMARY asserted a listed object while its FIELDS
// listed none. Asserting "the fields are empty" would not be enough: a card can
// have fields and still not name the object (that is precisely the `id` shape, and
// it is what the QA run saw on the live page). So the assertion is about the
// OBJECT ROW the card claims.
func TestChatCardNeverClaimsAnObjectItCannotName(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	noLabel := func(string, string) string { return "" }

	objectKinds := []struct {
		tool  string
		label string
	}{
		{"update_product", keyLabelProduct},
		{"set_form_theme", keyLabelForm},
	}

	for _, tc := range objectKinds {
		// A BLANK object key is as unusable as a missing one, and it is the shape a
		// model produces when it half-fills an optional field.
		for _, args := range []map[string]any{
			{"id": id, "name": "renamed"},           // the live defect: keyed `id`
			{"name": "renamed"},                     // no object key at all
			{mapKeyFor(tc.tool): "  ", "name": "x"}, // present but blank
		} {
			if _, _, _, ok := Describe(tc.tool, args, noLabel); ok {
				t.Errorf("Describe(%s, %v) rendered a card, but its arguments do not name the object "+
					"the card's summary claims is listed. The user would be asked to confirm a change "+
					"to something the card never identifies (t_623676d1).", tc.tool, args)
			}
		}
		// The positive control, so the loop above cannot pass by refusing everything.
		good := map[string]any{mapKeyFor(tc.tool): id, "name": "renamed"}
		titleKey, summaryKey, fields, ok := Describe(tc.tool, good, noLabel)
		if !ok {
			t.Fatalf("Describe(%s, %v) refused the CORRECTLY keyed arguments — the check is refusing on "+
				"the wrong condition", tc.tool, good)
		}
		if titleKey == "" || summaryKey == "" {
			t.Errorf("Describe(%s) produced an empty title/summary KEY — the card's wording is resolved "+
				"from these by the dashboard, so an empty one is a card with no heading", tc.tool)
		}
		found := false
		for _, fld := range fields {
			if fld.LabelKey == tc.label {
				found = true
			}
		}
		if !found {
			t.Errorf("Describe(%s, %v) rendered a card with NO %q row: %+v. A card that says an object "+
				"is listed must list it.", tc.tool, good, tc.label, fields)
		}
	}
}

// mapKeyFor is the tool's declared object key, spelled once so the test cannot
// drift from the tools it exercises.
func mapKeyFor(tool string) string {
	switch tool {
	case "set_form_theme":
		return "form_id"
	default:
		return "product_id"
	}
}

// TestChatConfirmRefusesAnUnacceptableStoredProposalWithoutSpendingTheToken is
// requirement (b), and it is the half the propose-time check cannot cover: a row
// written BEFORE the check existed (or by any future path that bypasses it) must
// still not be able to burn the user's token.
//
// The row is inserted DIRECTLY through the store, because the proposal lane now
// correctly refuses to create it — that is the point: this test is about the
// already-stored row, and it would be vacuous if it could only reach rows the new
// check had already rejected.
func TestChatConfirmRefusesAnUnacceptableStoredProposalWithoutSpendingTheToken(t *testing.T) {
	requireArgShapeDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	ctx := f.proposalCtx()

	store := dbstore.NewChatWriteProposalStore(f.pool)

	// ── POSITIVE CONTROL: a stored row the tool DOES accept confirms and spends. ──
	// Without it, "consumed_at is still NULL" below would also hold if Confirm were
	// broken for every input.
	goodCard := f.storeProposal(t, store, map[string]any{
		"product_id": f.productID, "name": "Confirmed Control",
	})
	if _, err := r.Confirm(ctx, f.confirmCtx(), f.userID, goodCard.Token, goodCard.ArgsHash); err != nil {
		t.Fatalf("the CORRECTLY keyed stored proposal was refused (%v) — the refusal below would say "+
			"nothing about the arguments", err)
	}
	if got := f.productName(t); got != "Confirmed Control" {
		t.Fatalf("the control confirm did not execute: name is %q", got)
	}

	// ── THE CASE: the live defect's exact argument shape, stored. ──
	badCard := f.storeProposal(t, store, map[string]any{
		"id": f.productID, "name": "Must Not Land",
	})
	_, err := r.Confirm(ctx, f.confirmCtx(), f.userID, badCard.Token, badCard.ArgsHash)
	if !errors.Is(err, ErrProposalArgsInvalid) {
		t.Fatalf("Confirm of a stored proposal the tool would never accept returned %v, want "+
			"ErrProposalArgsInvalid — the user is told to retry a card that cannot work", err)
	}

	// ── THE TOKEN IS UNSPENT. ──
	var consumed *time.Time
	if err := f.pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM chat_write_proposals WHERE token_hash=$1`,
		dbstore.HashProposalToken(badCard.Token)).Scan(&consumed); err != nil {
		t.Fatalf("read the proposal back: %v", err)
	}
	if consumed != nil {
		t.Error("the unacceptable proposal was CONSUMED — the token is spent on a refusal the user " +
			"cannot act on, and the copy that says 'ask for a fresh card' is a lie about a token that " +
			"no longer exists (t_623676d1)")
	}

	// ── And nothing was written: the refusal ran before the dispatch. ──
	if got := f.productName(t); got == "Must Not Land" {
		t.Fatalf("the product was renamed to %q — the refusal arrived AFTER the write", got)
	}

	// A SECOND attempt is refused identically (still unspent, still the same
	// verdict), which is what distinguishes this from "consumed on first failure".
	if _, err2 := r.Confirm(ctx, f.confirmCtx(), f.userID, badCard.Token, badCard.ArgsHash); !errors.Is(err2, ErrProposalArgsInvalid) {
		t.Errorf("a second Confirm returned %v, want ErrProposalArgsInvalid — the verdict must be "+
			"stable while the token stays unspent", err2)
	}

	// ── CANCEL still settles it, so the user is not stuck with a card that does
	// nothing at all: cancel is a real decision and spends the token. ──
	if err := r.Cancel(ctx, f.userID, badCard.Token); err != nil {
		t.Errorf("Cancel of an unexecutable card failed (%v) — the user must still be able to dismiss "+
			"a card that will never work", err)
	}
}

// storeProposal writes one proposal row DIRECTLY, bypassing the propose lane, and
// returns the token-bearing card the user would have been shown.
//
// The card's text is written BY HAND rather than through Describe, deliberately:
// the row under test is one the OLD lane produced, and Describe now (correctly)
// refuses the mis-keyed shape — reaching Describe would make this test unable to
// build the very row it exists to defend against.
func (f *acceptanceFixture) storeProposal(t *testing.T, store *dbstore.ChatWriteProposalStore, args map[string]any) *ProposalCard {
	t.Helper()
	raw, err := newProposalToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	titleKey, summaryKey, _ := ProposalKeys("update_product")
	p := &dbstore.ChatWriteProposal{
		TokenHash:  dbstore.HashProposalToken(raw),
		UserID:     f.userID,
		AccountID:  f.accountID,
		Tool:       "update_product",
		Args:       args,
		TitleKey:   titleKey,
		SummaryKey: summaryKey,
		ExpiresAt:  time.Now().UTC().Add(ProposalTTL),
	}
	if err := store.Create(context.Background(), p); err != nil {
		t.Fatalf("store the proposal: %v", err)
	}
	return &ProposalCard{
		Token: raw, ArgsHash: p.ArgsHash, Tool: p.Tool,
		TitleKey: titleKey, SummaryKey: summaryKey,
		ExpiresAt: p.ExpiresAt.Format(time.RFC3339), Reversible: true,
	}
}
