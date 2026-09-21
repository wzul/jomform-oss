package chat

// Phase 2 acceptance: the proposal lane, over the REAL MCP server and the REAL
// database.
//
// THE CARD'S MOST IMPORTANT CRITERION, and how this file tries to earn it:
//
//	"the model must not be able to perform a write by itself. Evidence: a set of
//	 attempts where the model calls a write tool — INCLUDING WITH confirm=true
//	 INSIDE THE ARGUMENTS — all of which must be refused or become a proposal,
//	 never executed."
//
// So the central test does not inspect the allowlist or the prompt. It takes the
// runner's own production dispatch path and calls every write class from the card
// directly, with `confirm=true` and `confirm:"yes"` in the arguments, and requires
// each one to be refused AND the database to be read back unchanged. The second
// half matters as much as the first: a refusal that arrived after the write would
// look identical in the call result.
//
// The token tests are the other half of the card: binding, single use, expiry and
// cross-caller refusal, each read back against Postgres rather than against the
// store's own return value.
//
// THE BITE PROOF. TestChatConfirmBiteProofSkippingTheTokenLandsTheWrite is the
// card's demand that the refusals be proven to be what stops the write: it drops
// the token checks (the shape a regression takes) and REQUIRES the same proposal
// to execute and change the database. If that test ever passes without a change,
// the refusal tests above prove nothing about the token.
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the standard test env).

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// proposalRunner wires a Runner over the acceptance fixture's real MCP server and
// a real proposal store, with the production session constructor. The fixture's
// own helper returns a caller directly, which is not enough here: the confirm path
// opens its OWN session, so the runner must be built the way New() builds it.
func (f *acceptanceFixture) proposalRunner(t *testing.T) *Runner {
	t.Helper()
	r := &Runner{mcp: f.srv, proposals: dbstore.NewChatWriteProposalStore(f.pool)}
	r.newSession = r.mcpSession
	return r
}

// proposalCtx is the READONLY identity a chat message runs under — built the same
// way the REST edge builds it, so nothing here passes a stricter scope than
// production uses.
func (f *acceptanceFixture) proposalCtx() context.Context {
	return mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID,
		Scopes: ReadOnlyScopes(), Agent: "in-app-chat",
	})
}

// confirmCtx is the identity the CONFIRMED tool runs under: the human's, with the
// write scope. It mirrors chatConfirmContextFor in the REST layer.
func (f *acceptanceFixture) confirmCtx() context.Context {
	return mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID,
		Scopes: WriteScopes(), Agent: "in-app-chat-confirm",
	})
}

// proposeUpdateProduct drives the model's own path — the dispatch of the
// propose_write tool — and returns the card the user would be shown.
func (f *acceptanceFixture) proposeUpdateProduct(t *testing.T, r *Runner, ctx context.Context, newName string) *ProposalCard {
	t.Helper()
	args := `{"tool":"update_product","arguments":{"product_id":` + jsonString(f.productID) +
		`,"name":` + jsonString(newName) + `},"reason":"pengguna minta tukar nama"}`
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the propose session: %v", err)
	}
	defer caller.close()
	st := &runState{}
	out, refused, err := r.dispatchIn(ctx, caller, toolCall("p1", ToolProposeWrite, args), st)
	if err != nil {
		t.Fatalf("dispatch propose_write: %v", err)
	}
	if refused {
		t.Fatalf("propose_write was refused (%s) — the proposal lane must exist for the tests "+
			"below to mean anything", out)
	}
	// The TOKEN must not be in what the model saw. This is asserted here rather
	// than in a test of its own because every test below depends on it: a card
	// whose token leaked into the prompt would make "the model cannot confirm"
	// false, and the confirmation tests would be measuring nothing.
	if strings.Contains(out, "\"token\"") && !strings.Contains(out, `"token":""`) {
		t.Fatalf("the propose_write result fed to the MODEL carries the confirmation token — a "+
			"model holding the token could complete the user's confirmation itself: %s", out)
	}
	card := st.lastCard()
	if card == nil || card.Token == "" {
		t.Fatalf("propose_write produced no token-bearing card for the browser (state=%+v, model saw %s)",
			st, out)
	}
	return card
}

// productName reads the product's current name back out of the database.
func (f *acceptanceFixture) productName(t *testing.T) string {
	t.Helper()
	var name string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT name FROM products WHERE id=$1`, f.productID).Scan(&name); err != nil {
		t.Fatalf("read the product back: %v", err)
	}
	return name
}

// TestChatModelCannotExecuteAWriteEvenWithConfirmInTheArguments is the card's
// central criterion, driven through the production dispatch path.
//
// Every write class from the card is attempted DIRECTLY — i.e. the model calls the
// write tool rather than propose_write — and every one of them carries
// `confirm=true` in the arguments, which is the exact shape the card names as the
// thing that must not work. Each must be refused, and the database must be read
// back afterwards to prove the refusal happened before any write.
func TestChatModelCannotExecuteAWriteEvenWithConfirmInTheArguments(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	ctx := f.proposalCtx()
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	defer caller.close()

	attempts := []struct {
		tool string
		args string
		what string
	}{
		{
			"update_product",
			`{"product_id":` + jsonString(f.productID) + `,"name":"HIJACKED BY CHAT","confirm":true}`,
			"rename a product (the card's own reproduction)",
		},
		{
			"update_product",
			`{"product_id":` + jsonString(f.productID) + `,"name":"CONFIRMED BY MODEL","confirm":"yes"}`,
			"rename a product with confirm as a STRING",
		},
		{"delete_form", `{"form_id":` + jsonString(f.formID) + `,"confirm":true}`, "delete a form"},
		{"archive_product", `{"product_id":` + jsonString(f.productID) + `,"confirm":true}`, "archive a product"},
		{"refund_sale", `{"ref_code":` + jsonString(f.saleRef) + `,"confirm":true}`, "refund a sale"},
		{"cancel_subscription", `{"email":` + jsonString(f.subEmail) + `,"confirm":true}`, "cancel a subscription"},
		{"create_api_key", `{"name":"chat-escalation","scopes":["readwrite"],"confirm":true}`, "mint an API key"},
		{"set_form_theme", `{"form_id":` + jsonString(f.formID) + `,"primary":"#000000","confirm":true}`, "restyle a form"},
	}

	for _, a := range attempts {
		out, refused, derr := r.dispatchIn(ctx, caller, toolCall("w", a.tool, a.args), &runState{})
		if derr != nil {
			t.Fatalf("dispatch %s: %v", a.tool, derr)
		}
		if !refused {
			t.Errorf("PHASE 2 WRITE LEAK: the model performed %s via %s. Result: %s\n"+
				"A model that calls a write tool directly must be REFUSED — a `confirm` boolean it "+
				"wrote itself is not a human's confirmation.", a.what, a.tool, out)
			continue
		}
		// The refusal must say the useful thing, so the model offers the user the
		// supported route rather than reporting a dead end.
		if !strings.Contains(out, ToolProposeWrite) {
			t.Errorf("the refusal of %s (%q) does not name %s — the model would report a dead "+
				"end instead of proposing the change", a.tool, out, ToolProposeWrite)
		}
	}

	// ── The database must be untouched. ──
	if got := f.productName(t); got == "HIJACKED BY CHAT" || got == "CONFIRMED BY MODEL" {
		t.Errorf("the fixture product was renamed to %q by the chat — the refusal ran after the "+
			"write, not before it", got)
	}
	var deleted bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, f.formID).Scan(&deleted); err != nil {
		t.Fatalf("read the form back: %v", err)
	}
	if deleted {
		t.Error("the fixture form was soft-deleted by the chat")
	}
	var refunded int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT refunded_sen FROM sales WHERE ref_code=$1`, f.saleRef).Scan(&refunded); err != nil {
		t.Fatalf("read the sale back: %v", err)
	}
	if refunded != 0 {
		t.Errorf("the sale shows %d sen refunded — the chat moved money", refunded)
	}
	var subStatus string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM subscriptions WHERE client_email=$1`, f.subEmail).Scan(&subStatus); err != nil {
		t.Fatalf("read the subscription back: %v", err)
	}
	if subStatus == dbstore.SubCanceled {
		t.Error("the chat cancelled a live subscription")
	}
	var keyCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM api_keys WHERE account_id=$1`, f.accountID).Scan(&keyCount); err != nil {
		t.Fatalf("count api keys: %v", err)
	}
	if keyCount != 0 {
		t.Errorf("the chat minted %d API key(s) — the escalation path is open", keyCount)
	}
	// The theme must not have been applied either: set_form_theme is PROPOSABLE in
	// phase 2, so a direct call is still a direct call.
	var theme []byte
	if err := f.pool.QueryRow(context.Background(),
		`SELECT theme FROM forms WHERE id=$1`, f.formID).Scan(&theme); err != nil {
		t.Fatalf("read the form theme back: %v", err)
	}
	if strings.Contains(string(theme), "#000000") {
		t.Errorf("the form theme was changed directly by the chat: %s", theme)
	}
}

// TestChatProposalLaneEndToEndWithRealPostgres drives the whole round trip: the
// model PROPOSES, nothing happens, the user CONFIRMS with the card's token, and
// only then does the write land — verified in Postgres.
func TestChatProposalLaneEndToEndWithRealPostgres(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	before := f.productName(t)

	card := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Nama baharu dari chat")

	// ── After the PROPOSAL: nothing has happened. ──
	if got := f.productName(t); got != before {
		t.Fatalf("the product was renamed to %q by the PROPOSAL alone — propose_write executed "+
			"something", got)
	}
	// The card must say what will happen, in the user's terms, and carry the
	// binding hash. A card with no fields is a card the user cannot judge.
	if len(card.Fields) == 0 {
		t.Error("the proposal card lists no fields — the user is asked to confirm an effect they " +
			"were not shown")
	}
	if card.ArgsHash == "" || card.Token == "" {
		t.Fatalf("the card carries no binding (token=%q hash=%q)", card.Token, card.ArgsHash)
	}
	if !strings.Contains(card.Title, "produk") && !strings.Contains(card.Title, "Produk") {
		t.Errorf("the card title %q does not name the effect in the user's terms", card.Title)
	}
	// The stored row must be the model's proposal, owned by the human.
	var storedTool string
	var storedUser string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT tool, user_id FROM chat_write_proposals WHERE token_hash=$1`,
		dbstore.HashProposalToken(card.Token)).Scan(&storedTool, &storedUser); err != nil {
		t.Fatalf("the card's token has no stored proposal: %v", err)
	}
	if storedTool != "update_product" {
		t.Errorf("stored tool = %q, want update_product", storedTool)
	}
	if storedUser != f.userID {
		t.Errorf("the proposal is owned by %q, want the caller %q", storedUser, f.userID)
	}
	// The raw token must NEVER be persisted: only its hash.
	var rawCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_write_proposals WHERE token_hash=$1`, card.Token).Scan(&rawCount); err != nil {
		t.Fatalf("count raw-token rows: %v", err)
	}
	if rawCount != 0 {
		t.Error("the RAW token is stored in the database — a backup, replica or psql session could " +
			"replay the user's confirmation")
	}

	// ── The CONFIRM: the human's click. ──
	res, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash)
	if err != nil {
		t.Fatalf("confirm the proposal: %v", err)
	}
	if res.Tool != "update_product" {
		t.Fatalf("confirmed tool = %q, want update_product", res.Tool)
	}
	if got := f.productName(t); got != "Nama baharu dari chat" {
		t.Fatalf("after a confirmed proposal the product name is %q, want the proposed name — the "+
			"confirmation did not execute", got)
	}

	// ── And the token is SPENT. ──
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); !errors.Is(err, dbstore.ErrChatProposalConsumed) {
		t.Errorf("a second confirm returned %v, want ErrChatProposalConsumed — a spent token must "+
			"never be spent twice", err)
	}
	if got := f.productName(t); got != "Nama baharu dari chat" {
		t.Errorf("the second confirm changed the product again: %q", got)
	}
	var consumed *time.Time
	if err := f.pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM chat_write_proposals WHERE token_hash=$1`,
		dbstore.HashProposalToken(card.Token)).Scan(&consumed); err != nil {
		t.Fatalf("read the proposal back: %v", err)
	}
	if consumed == nil {
		t.Error("the proposal row has no consumed_at after a successful confirm — \"used once\" is " +
			"not a row-level fact, so a second confirm could win a race")
	}
}

// TestChatProposalTokenIsBoundToTheArguments is the card's (a): a token whose
// arguments were changed between propose and confirm must be refused.
//
// It also asserts the refusal does NOT burn the token — the user's card is still
// confirmable with the arguments they actually read, which is what makes the
// refusal a protection rather than a punishment.
func TestChatProposalTokenIsBoundToTheArguments(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	before := f.productName(t)

	card := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Nama yang dipaparkan")

	// The model (or a tampered client) submits a DIFFERENT argument set: the name
	// the user read is not the name that would run. The wire carries the hash, so
	// this is the shape of the attack — echo a hash that does not match.
	otherHash, err := dbstore.HashArgs(map[string]any{"product_id": f.productID, "name": "PADAM SEMUA"})
	if err != nil {
		t.Fatalf("hash the substituted arguments: %v", err)
	}
	if otherHash == card.ArgsHash {
		t.Fatal("fixture error: the substituted arguments hash the same as the proposed ones")
	}
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, otherHash); !errors.Is(err, ErrProposalArgsChanged) {
		t.Fatalf("confirming with a substituted args_hash returned %v, want ErrProposalArgsChanged — "+
			"the proposal is not bound to what the user was shown", err)
	}
	if got := f.productName(t); got != before {
		t.Fatalf("the product was renamed to %q despite the hash mismatch", got)
	}

	// An empty hash is not "no change requested": nothing binds the execution.
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, ""); !errors.Is(err, ErrProposalArgsChanged) {
		t.Fatalf("confirming with NO args_hash returned %v, want ErrProposalArgsChanged", err)
	}

	// ── The refusal must not have burned the token: the user can still confirm
	//    the proposal they actually read. ──
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); err != nil {
		t.Fatalf("the ORIGINAL card could no longer be confirmed (%v) — a mismatched request "+
			"spent the user's proposal, so a tampered retry is a denial of service on the real "+
			"confirmation", err)
	}
	if got := f.productName(t); got != "Nama yang dipaparkan" {
		t.Fatalf("the confirmed proposal did not execute: name is %q", got)
	}
}

// TestChatProposalTokenExpires is the card's (c). A proposal is decided while the
// user is reading the card, not later; the window is enforced against the
// DATABASE clock (the row is written with a past expiry here, which is the same
// thing a skewed replica would produce).
func TestChatProposalTokenExpires(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	before := f.productName(t)

	card := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Nama yang luput")

	// Move the expiry into the past, as a five-minute-old card would be.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE chat_write_proposals SET expires_at = now() - interval '1 second'
		  WHERE token_hash = $1`, dbstore.HashProposalToken(card.Token)); err != nil {
		t.Fatalf("age the proposal: %v", err)
	}
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); !errors.Is(err, dbstore.ErrChatProposalExpired) {
		t.Fatalf("confirming an expired proposal returned %v, want ErrChatProposalExpired", err)
	}
	if got := f.productName(t); got != before {
		t.Fatalf("an EXPIRED proposal executed and renamed the product to %q", got)
	}
	// And it is genuinely dead, not merely reported dead: the store's own claim
	// path must refuse it too.
	if _, err := r.proposals.Claim(context.Background(), f.userID,
		dbstore.HashProposalToken(card.Token)); !errors.Is(err, dbstore.ErrChatProposalExpired) {
		t.Errorf("Claim on an expired proposal returned %v, want ErrChatProposalExpired — the "+
			"atomic single-use step and the read path disagree about expiry", err)
	}
}

// TestChatProposalTokenBelongsToItsCaller is the card's (d), plus the no-oracle
// rule: another person's token must answer EXACTLY like a token that was never
// issued.
func TestChatProposalTokenBelongsToItsCaller(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	before := f.productName(t)

	card := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Nama milik A")

	// Person B: a different user on a different account.
	userB, accountB := f.newSecondPerson(t)
	if userB == f.userID {
		t.Fatal("fixture error: the second person is the first person")
	}

	// B tries to confirm A's token, with a write scope for THEIR account.
	ctxB := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: accountB, UserID: userB, Scopes: WriteScopes(), Agent: "in-app-chat-confirm",
	})
	_, err := r.Confirm(f.proposalCtx(), ctxB, userB, card.Token, card.ArgsHash)
	if !errors.Is(err, dbstore.ErrChatProposalNotFound) {
		t.Fatalf("person B confirming person A's token returned %v, want ErrChatProposalNotFound — "+
			"the read is not scoped to the caller", err)
	}
	if got := f.productName(t); got != before {
		t.Fatalf("another person's confirm renamed A's product to %q", got)
	}

	// No existence oracle: an id that was never issued answers identically.
	_, errMissing := r.Confirm(f.proposalCtx(), ctxB, userB, strings.Repeat("ab", 32), card.ArgsHash)
	if !errors.Is(errMissing, dbstore.ErrChatProposalNotFound) {
		t.Fatalf("a token that was never issued returned %v, want ErrChatProposalNotFound", errMissing)
	}
	if err.Error() != errMissing.Error() {
		t.Errorf("the refusal for another person's token (%q) differs from the refusal for a "+
			"nonexistent token (%q) — a difference lets the token be probed by observation",
			err.Error(), errMissing.Error())
	}

	// ── And A's token is still good: B's attempts must not have spent it. ──
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); err != nil {
		t.Fatalf("person A could no longer confirm their own proposal after B's attempts (%v) — "+
			"a stranger can burn somebody's card", err)
	}
	if got := f.productName(t); got != "Nama milik A" {
		t.Fatalf("A's confirmed proposal did not execute: name is %q", got)
	}
}

// TestChatProposeWriteRefusesTheDestructiveClasses drives the refusal through the
// model's own tool, so the exclusion is a property of the LANE and not only of the
// allowlist in mcpsrv.
func TestChatProposeWriteRefusesTheDestructiveClasses(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	ctx := f.proposalCtx()
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	defer caller.close()

	for _, tool := range []string{"delete_form", "refund_sale", "archive_product", "cancel_subscription", "create_api_key", "publish_form"} {
		args := `{"tool":` + jsonString(tool) + `,"arguments":{"form_id":` + jsonString(f.formID) + `}}`
		out, refused, derr := r.proposeWrite(ctx, caller, args, &runState{})
		if derr != nil {
			t.Fatalf("proposeWrite(%s): %v", tool, derr)
		}
		if !refused {
			t.Errorf("propose_write ACCEPTED %s (%s) — the destructive classes are not proposable in "+
				"phase 2 and must be refused, not carded", tool, out)
		}
	}
	// No proposal rows were written for the refused attempts.
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_write_proposals WHERE user_id=$1`, f.userID).Scan(&n); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if n != 0 {
		t.Errorf("%d proposal row(s) exist after only refused proposals — a refusal must leave no "+
			"token behind", n)
	}
}

// TestChatProposalBudgetIsPerMessage bounds how many decision cards one answer can
// put in front of the user. A wall of confirmations is the failure mode that
// produces a careless click.
func TestChatProposalBudgetIsPerMessage(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)
	ctx := f.proposalCtx()
	caller, err := r.newSession(ctx)
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	defer caller.close()

	st := &runState{}
	accepted := 0
	for i := 0; i <= MaxProposalsPerRun; i++ {
		args := `{"tool":"update_product","arguments":{"product_id":` + jsonString(f.productID) +
			`,"name":"Nama ` + string(rune('a'+i)) + `"}}`
		out, refused, derr := r.proposeWrite(ctx, caller, args, st)
		if derr != nil {
			t.Fatalf("proposeWrite: %v", derr)
		}
		if !refused {
			accepted++
			continue
		}
		if i < MaxProposalsPerRun {
			t.Fatalf("proposal %d was refused but the budget is %d: %s", i+1, MaxProposalsPerRun, out)
		}
	}
	if accepted != MaxProposalsPerRun {
		t.Fatalf("accepted %d proposals in one message, want exactly %d", accepted, MaxProposalsPerRun)
	}
	// A fresh message gets a fresh budget — the state is per-message, not global.
	if _, refused, err := r.proposeWrite(f.proposalCtx(), caller,
		`{"tool":"update_product","arguments":{"product_id":`+jsonString(f.productID)+`,"name":"Selepas itu"}}`,
		&runState{}); err != nil || refused {
		t.Fatalf("a NEW message's first proposal was refused (err=%v refused=%v) — the budget leaked "+
			"across messages", err, refused)
	}
}

// TestChatConfirmBiteProofSkippingTheTokenLandsTheWrite is the card's bite proof
// for the token checks.
//
// The confirm tests above are green when the token step refuses. This one performs
// the SHAPE OF A REGRESSION — execute the stored arguments under the human's write
// identity WITHOUT the token checks, i.e. what Confirm would do if steps 1–4 were
// removed — and REQUIRES the write to land. If it does not, the refusals above are
// being produced by something else entirely (a broken harness, a dead session) and
// prove nothing about the token.
//
// The loosening is deliberately NOT a copy of Confirm with a line commented out:
// it uses the store's own canonical arguments and the same production tool channel,
// so it fails for the same reason production would.
func TestChatConfirmBiteProofSkippingTheTokenLandsTheWrite(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)

	card := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Ditulis tanpa token")
	if got := f.productName(t); got == "Ditulis tanpa token" {
		t.Fatal("precondition failed: the proposal already executed, so this proof has nothing to show")
	}

	// The loosening: take the STORED proposal and run its arguments through the
	// human's write identity, skipping the token entirely. Every step here is
	// production code — only the token check is absent.
	store := dbstore.NewChatWriteProposalStore(f.pool)
	stored, err := store.ByToken(context.Background(), f.userID, dbstore.HashProposalToken(card.Token))
	if err != nil {
		t.Fatalf("read the stored proposal back: %v", err)
	}
	argsJSON, err := dbstore.CanonicalArgs(stored.Args)
	if err != nil {
		t.Fatalf("canonicalise the stored arguments: %v", err)
	}
	writeCtx := f.confirmCtx()
	caller, err := r.newSession(writeCtx)
	if err != nil {
		t.Fatalf("open the write session: %v", err)
	}
	defer caller.close()
	out, refused, err := caller.call(writeCtx, stored.Tool, string(argsJSON))
	if err != nil {
		t.Fatalf("dispatch the stored write: %v", err)
	}
	if refused {
		t.Fatalf("BITE PROOF DID NOT BITE: the stored write was refused even WITHOUT the token step "+
			"(%s).\nThe confirm tests above assert the token is what refuses a write; if the write "+
			"cannot land at all in this harness, those assertions prove nothing.", out)
	}
	if got := f.productName(t); got != "Ditulis tanpa token" {
		t.Fatalf("BITE PROOF DID NOT BITE: the write reported success (%s) but the name is %q — the "+
			"harness cannot observe a write, so neither can the refusal tests", out, got)
	}
	t.Logf("BITE CONFIRMED: without the token check the stored proposal executes (%s) — the "+
		"refusals in the confirm tests are the token doing the work", bound(out, 120))

	// The loosening really was a loosening: the proposed token is STILL UNSPENT
	// after the write landed. That is what makes this proof meaningful — the write
	// happened without the token being consumed, so the token step is the thing
	// standing between the stored arguments and the database.
	var consumed *time.Time
	if err := f.pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM chat_write_proposals WHERE token_hash=$1`,
		dbstore.HashProposalToken(card.Token)).Scan(&consumed); err != nil {
		t.Fatalf("read the proposal back: %v", err)
	}
	if consumed != nil {
		t.Fatal("the token was spent by the bypass, so the bypass did not actually skip the token " +
			"step — this proof did not isolate the check it claims to")
	}
	// And with the token step in place the same proposal is STILL confirmable (it
	// was never spent) and, once confirmed, refuses a second use — which is the
	// property the confirm tests assert.
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); err != nil {
		t.Fatalf("the token was unusable after the bypass (%v) — the bypass left state the real "+
			"path would not", err)
	}
	if _, err := r.Confirm(f.proposalCtx(), f.confirmCtx(), f.userID, card.Token, card.ArgsHash); !errors.Is(err, dbstore.ErrChatProposalConsumed) {
		t.Errorf("a second confirm returned %v, want ErrChatProposalConsumed", err)
	}
}

// TestChatProposalSweepKeepsUnspentProposals pins the retention rule: the sweep
// removes spent rows only. A pending card the user has not answered is a decision
// in flight, not litter.
func TestChatProposalSweepKeepsUnspentProposals(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	r := f.proposalRunner(t)

	pending := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Masih menunggu")
	spent := f.proposeUpdateProduct(t, r, f.proposalCtx(), "Sudah digunakan")
	if err := r.Cancel(f.proposalCtx(), f.userID, spent.Token); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	store := dbstore.NewChatWriteProposalStore(f.pool)
	n, err := store.DeleteSpentBefore(context.Background(), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n < 1 {
		t.Fatalf("the sweep removed %d row(s) but one proposal was cancelled — it is not collecting", n)
	}
	if _, err := store.ByToken(context.Background(), f.userID,
		dbstore.HashProposalToken(pending.Token)); err != nil {
		t.Fatalf("the sweep removed the PENDING proposal (%v) — a card the user has not answered is "+
			"not litter", err)
	}
	if _, err := store.ByToken(context.Background(), f.userID,
		dbstore.HashProposalToken(spent.Token)); !errors.Is(err, dbstore.ErrChatProposalNotFound) {
		t.Errorf("the cancelled proposal survived the sweep (%v)", err)
	}
}

// TestChatProposalStoreNeedsNoDatabaseForItsRefusals is the cheap guard that the
// store refuses rather than panics on the degenerate inputs, so a misconfigured
// deployment answers an error instead of a 500.
func TestChatProposalStoreNeedsNoDatabaseForItsRefusals(t *testing.T) {
	_ = os.Getenv("JOMFORM_TEST_DATABASE_URL")
	var store *dbstore.ChatWriteProposalStore
	// A nil pool: every call must still return the typed not-found rather than
	// panicking, because that is what a deployment with no database would do.
	if _, err := store.ByToken(context.Background(), "", ""); !errors.Is(err, dbstore.ErrChatProposalNotFound) {
		t.Errorf("ByToken with empty inputs returned %v, want ErrChatProposalNotFound", err)
	}
	if _, err := store.ByToken(context.Background(), "u", ""); !errors.Is(err, dbstore.ErrChatProposalNotFound) {
		t.Errorf("ByToken with no token returned %v, want ErrChatProposalNotFound", err)
	}

	// The hash helpers are pure, and the binding rule is a pure function of them.
	h1, err := dbstore.HashArgs(map[string]any{"a": 1, "b": "x"})
	if err != nil {
		t.Fatalf("HashArgs: %v", err)
	}
	h2, err := dbstore.HashArgs(map[string]any{"b": "x", "a": 1})
	if err != nil {
		t.Fatalf("HashArgs: %v", err)
	}
	if h1 != h2 {
		t.Errorf("key order changed the argument hash (%s vs %s) — a client that reorders keys would "+
			"be refused for a change that did not happen", h1, h2)
	}
	if dbstore.HashProposalToken("abc") == dbstore.HashProposalToken("abd") {
		t.Error("two different tokens hash alike")
	}
	hA, err := dbstore.HashArgs(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("HashArgs: %v", err)
	}
	p := &dbstore.ChatWriteProposal{Args: map[string]any{"a": 1}, ArgsHash: hA}
	if !p.ArgsHashMatches(hA) {
		t.Error("ArgsHashMatches rejected the correct hash")
	}
	if p.ArgsHashMatches("") || p.ArgsHashMatches("deadbeef") {
		t.Error("ArgsHashMatches accepted a wrong or empty hash — the binding is not enforced")
	}
	if p.ArgsHashMatches(h1) {
		t.Error("ArgsHashMatches accepted the hash of DIFFERENT arguments")
	}
	// A row whose stored hash disagrees with its own arguments must not match:
	// that inconsistency is exactly what the confirm path would otherwise trust.
	inconsistent := &dbstore.ChatWriteProposal{Args: map[string]any{"a": 1}, ArgsHash: h1}
	if inconsistent.ArgsHashMatches(hA) {
		t.Error("ArgsHashMatches accepted a row whose args_hash disagrees with its own arguments — " +
			"the stored hash is no longer an anchor for the stored arguments")
	}
	var nilP *dbstore.ChatWriteProposal
	if nilP.ArgsHashMatches(hA) {
		t.Error("a nil proposal matched a hash")
	}
}

// poolForTest is a tiny helper kept for symmetry with the fixture helpers above;
// it returns the fixture's pool so a future test can build its own store without
// re-reading the fixture fields.
func (f *acceptanceFixture) poolForTest() *pgxpool.Pool { return f.pool }
