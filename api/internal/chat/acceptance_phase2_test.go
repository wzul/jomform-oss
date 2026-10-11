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
// directly, in TWO argument shapes: with `confirm=true` / `confirm:"yes"` (the
// shape the card names) AND with a schema-valid body that carries no `confirm`
// key, and it requires each one to be refused BY A SECURITY LAYER — the allowlist
// ban or the scope gate — with the database read back unchanged. The second half
// matters as much as the first: a refusal that arrived after the write would look
// identical in the call result.
//
// WHY BOTH SHAPES, AND WHY THE REFUSAL'S IDENTITY IS ASSERTED. A tool call comes
// back `refused=true` for three unrelated reasons: the allowlist refused the
// NAME, the readonly identity refused the SCOPE, or the tool's input schema
// refused the ARGUMENTS. The write tools declare `additionalProperties: false`,
// so the `confirm=…` shape is refused by the SCHEMA, before any security layer
// is consulted — and a schema refusal reaches the caller as the same
// `refused=true` as a security refusal, so a test that only asserted "it was
// refused" would be satisfied by argument validation and could not say which
// layer did the work. The schema-valid shape removes that ambiguity: nothing but
// the two security layers can refuse it, and the assertion refuses any refusal
// that does not name a security reason.
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
// way the REST edge builds it (identity PLUS the per-call workspace scope, issue
// #747), so nothing here passes a stricter scope than production uses.
func (f *acceptanceFixture) proposalCtx() context.Context {
	ctx := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID,
		Scopes: ReadOnlyScopes(), Agent: "in-app-chat",
	})
	return mcpsrv.WithCallScope(ctx, &mcpsrv.CallScope{Accounts: []string{f.accountID}})
}

// confirmCtx is the identity the CONFIRMED tool runs under: the human's, with the
// write scope, for the workspace the request resolved. It mirrors
// chatConfirmContextFor in the REST layer.
func (f *acceptanceFixture) confirmCtx() context.Context {
	ctx := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID,
		Scopes: WriteScopes(), Agent: "in-app-chat-confirm",
	})
	return mcpsrv.WithCallScope(ctx, &mcpsrv.CallScope{Accounts: []string{f.accountID}})
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

// isSchemaValidationRefusal reports whether a refusal came from the MCP INPUT
// SCHEMA — i.e. the ARGUMENTS were rejected — rather than from a security gate.
//
// This is the trap this file exists to close. A chat tool call has three
// indistinguishable ways to come back refused:
//
//  1. the dispatch allowlist refused the NAME,
//  2. the readonly identity refused the SCOPE,
//  3. the tool's input schema refused the ARGUMENTS.
//
// All three arrive as the same `refused=true` with a message in the output, and
// every write tool's schema uses `additionalProperties: false` — so an attempt
// that carries a key the tool does not declare (`confirm`, the shape the card
// named) is refused by layer 3, the schema, before any security layer is
// consulted. A test that only asserts "it was refused" is therefore satisfied by
// argument validation: it cannot attribute the refusal, which is why this file
// asserts the refusal's identity — `isAllowlistBanRefusal` for the name and
// `isScopeRefusal` for the scope — instead of only its existence.
func isSchemaValidationRefusal(out string) bool {
	return strings.Contains(out, `validating "arguments"`)
}

// isAllowlistBanRefusal reports whether the DISPATCH ALLOWLIST refused the call
// (layer 1) — the message names the tool, says the chat may not execute it, and
// points at the proposal lane so the model can offer the user the supported
// route instead of a dead end.
//
// A caller asserting the security boundary must use this, not `refused`:
// `refused` cannot tell a security refusal from an argument-validation failure.
func isAllowlistBanRefusal(out string) bool {
	return !isSchemaValidationRefusal(out) &&
		strings.Contains(out, "tidak boleh dilaksanakan oleh chat") &&
		strings.Contains(out, ToolProposeWrite)
}

// isScopeRefusal reports whether the READONLY IDENTITY refused the call
// (layer 2) — the message is the issue #645 scope gate. It is asserted
// separately from the ban because it is the other load-bearing layer: a test
// that only ever sees the ban cannot notice the identity being widened.
func isScopeRefusal(out string) bool {
	return !isSchemaValidationRefusal(out) && strings.Contains(out, "scope tidak mencukupi")
}

// TestChatModelCannotExecuteAWriteEvenWithConfirmInTheArguments is the card's
// central criterion.
//
// Every write class from the card is attempted DIRECTLY — i.e. the model calls
// the write tool rather than propose_write — in TWO argument shapes, and both
// SAFETY LAYERS are attributed, so this test fails if EITHER one is removed:
//
//	Part 1, the dispatch path (layer 1): the model's own call, in
//	  confirm=…    the card's own reproduction: the model writes a confirmation
//	               into its own arguments. Kept because the card asks for it
//	               explicitly. On its own it proves NOTHING about the boundary —
//	               the write tools' schemas reject unknown properties, so this
//	               shape is refused by argument validation too.
//	  schema-valid the same call with no `confirm` key and a body the tool's own
//	               schema accepts, so argument validation cannot be the reason.
//	  Each must be refused BY THE ALLOWLIST BAN, naming the tool and the proposal
//	  lane. A schema-validation refusal is a FAILURE here, not a pass.
//
//	Part 2, the raw server channel (layer 2): the same write calls dispatched
//	  straight at the MCP server, bypassing the allowlist entirely, under the
//	  production ReadOnlyScopes(). The identity must refuse every one with the
//	  scope gate. Without this, widening ReadOnlyScopes() would leave this test
//	  green (the allowlist would still be refusing), and the card's "must fail
//	  when either layer is removed" would not hold.
//
// The database is read back at the end of both parts, so a refusal that arrived
// after the write is caught as well as one that never happened.
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

	// ── Positive control: a READ must still work. ──
	// Without this, the assertions below are satisfied by a session that refuses
	// everything — a dead session would look exactly like a working boundary.
	if out, refused := callRaw(t, caller, ctx, "list_products", `{}`); refused {
		t.Fatalf("list_products was REFUSED (%s) — the chat is meant to answer questions, so "+
			"the write refusals below would prove nothing about a boundary", out)
	} else if !strings.Contains(out, f.productID) {
		t.Fatalf("list_products did not return the fixture product — the read path is not reading "+
			"the account the identity names, so neither is the refusal path: %s", out)
	}

	before := f.productName(t)

	// The write classes, with the arguments each tool's own schema ACCEPTS.
	// `product_id`/`form_id`/`ref_code`/`email`/`name`/`scopes` are all declared
	// by the corresponding input struct — no key here is an extra property, so
	// argument validation cannot refuse any of them (verified by dispatch: every
	// one of these reaches its gate).
	valid := []struct {
		tool string
		args string
		what string
	}{
		{"update_product", `{"product_id":` + jsonString(f.productID) + `,"name":"HIJACKED BY CHAT"}`, "rename a product (the card's own reproduction)"},
		{"delete_form", `{"form_id":` + jsonString(f.formID) + `}`, "delete a form"},
		{"archive_product", `{"product_id":` + jsonString(f.productID) + `}`, "archive a product"},
		{"refund_sale", `{"ref_code":` + jsonString(f.saleRef) + `}`, "refund a sale"},
		{"cancel_subscription", `{"email":` + jsonString(f.subEmail) + `}`, "cancel a subscription"},
		{"create_api_key", `{"name":"chat-escalation","scopes":["readwrite"]}`, "mint an API key"},
		{"set_form_theme", `{"form_id":` + jsonString(f.formID) + `,"primary":"#000000"}`, "restyle a form"},
	}

	type attempt struct {
		tool  string
		args  string
		what  string
		shape string
	}
	attempts := make([]attempt, 0, len(valid)*2)
	// The card's shape: confirm in the arguments. Kept verbatim.
	for _, v := range valid {
		attempts = append(attempts, attempt{v.tool, strings.TrimSuffix(v.args, "}") + `,"confirm":true}`, v.what, "confirm=true"})
	}
	attempts = append(attempts, attempt{
		"update_product",
		`{"product_id":` + jsonString(f.productID) + `,"name":"CONFIRMED BY MODEL","confirm":"yes"}`,
		"rename a product with confirm as a STRING",
		`confirm="yes"`,
	})
	// The shape that pins the layers: the same calls, schema-valid.
	for _, v := range valid {
		attempts = append(attempts, attempt{v.tool, v.args, v.what, "schema-valid"})
	}

	// ── PART 1 — layer 1, the dispatch allowlist. ──
	for _, a := range attempts {
		out, refused, derr := r.dispatchIn(ctx, caller, toolCall("w", a.tool, a.args), &runState{})
		if derr != nil {
			t.Fatalf("dispatch %s (%s): %v", a.tool, a.shape, derr)
		}
		if !refused {
			t.Errorf("PHASE 2 WRITE LEAK [%s]: the model performed %s via %s. Result: %s\n"+
				"A model that calls a write tool directly must be REFUSED — a `confirm` boolean it "+
				"wrote itself is not a human's confirmation.", a.shape, a.what, a.tool, out)
			continue
		}
		// ── WHICH refusal fired. ──
		// A refusal by JSON-schema validation of the arguments is NOT the
		// security boundary: it is what a `confirm`-shaped attempt gets the
		// moment the allowlist is widened, and it is indistinguishable from a
		// security refusal at the call site unless it is named.
		if isSchemaValidationRefusal(out) {
			t.Errorf("the refusal of %s (%s, %s) is ARGUMENT VALIDATION, not the safety boundary: %q\n"+
				"The dispatch allowlist never ran: the tool's input schema refused the arguments "+
				"before any security layer was consulted, so this attempt says nothing about either "+
				"layer. A widened allowlist shows up HERE, as an argument-validation refusal, and "+
				"not as a silence.",
				a.tool, a.shape, a.what, out)
			continue
		}
		if !isAllowlistBanRefusal(out) {
			t.Errorf("the refusal of %s (%s, %s) did not come from the dispatch allowlist: %q\n"+
				"The allowlist is layer 1 and it is what must refuse this call — a refusal from "+
				"somewhere else means the allowlist is not doing the work this test claims it does.",
				a.tool, a.shape, a.what, out)
			continue
		}
		// It must name the tool it refused, so the refusal cannot be about some
		// other call, and the proposal lane so the model is not left at a dead end.
		if !strings.Contains(out, a.tool) {
			t.Errorf("the allowlist refusal of %s (%s) does not name the tool: %q", a.tool, a.shape, out)
		}
	}

	// ── PART 2 — layer 2, the readonly identity, over the raw server channel. ──
	// Same calls, dispatched straight at the MCP server so the allowlist is out
	// of the path; the only thing left to refuse them is the scope on the
	// identity. This is what turns "either layer removed" into a real failure
	// instead of a claim.
	//
	// REVERSED ORDER, deliberately. These calls EXECUTE when the identity is
	// widened (that is the failure being detected), so an earlier entry can
	// destroy the target a later one names — delete_form soft-deletes the
	// fixture form, and set_form_theme then refuses with "entiti tidak dijumpai",
	// a red for the wrong reason. Running the destructive entry last keeps every
	// entry's refusal attributable to the scope gate.
	rawCaller, rawCtx := f.toolCallerFor(t, ReadOnlyScopes())
	if out, refused := callRaw(t, rawCaller, rawCtx, "list_products", `{}`); refused {
		t.Fatalf("the raw-channel positive control was refused (%s) — a session that refuses "+
			"everything cannot attribute a refusal to the scope gate", out)
	}
	for i := len(valid) - 1; i >= 0; i-- {
		v := valid[i]
		out, refused := callRaw(t, rawCaller, rawCtx, v.tool, v.args)
		if !refused {
			t.Errorf("PHASE 2 WRITE LEAK [raw channel]: %s executed under the readonly identity. "+
				"Result: %s\nThe identity is layer 2 — the only thing standing between a widened "+
				"allowlist and a real write.", v.what, out)
			continue
		}
		if !isScopeRefusal(out) {
			t.Errorf("the raw-channel refusal of %s (%s) is not the scope gate: %q\n"+
				"Attributing this refusal to the wrong layer is how the readonly identity gets "+
				"widened without a test going red.", v.tool, v.what, out)
		}
	}

	// ── The database must be untouched. ──
	// Exact equality, not a name-match: any rename at all is a leak, including
	// one this test did not think of.
	if got := f.productName(t); got != before {
		t.Errorf("the fixture product was renamed to %q by the chat (want %q) — the refusal ran "+
			"after the write, not before it", got, before)
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
	// The card must say what will happen, in the KEYS the dashboard resolves to the
	// user's own language, and carry the binding hash. A card with no fields is a
	// card the user cannot judge, and a card with a SENTENCE here would be one
	// language on every dashboard (t_30ffa28a).
	if len(card.Fields) == 0 {
		t.Error("the proposal card lists no fields — the user is asked to confirm an effect they " +
			"were not shown")
	}
	if card.ArgsHash == "" || card.Token == "" {
		t.Fatalf("the card carries no binding (token=%q hash=%q)", card.Token, card.ArgsHash)
	}
	if card.TitleKey != keyTitleUpdateProduct || card.SummaryKey != keySummaryUpdateProduct {
		t.Errorf("the card carries title_key=%q summary_key=%q, want the update_product keys — the "+
			"dashboard renders these through its dictionary, so a missing or wrong key is a card with "+
			"no heading, and a sentence here would be one language for every locale",
			card.TitleKey, card.SummaryKey)
	}
	keys := map[string]bool{}
	for _, f := range card.Fields {
		keys[f.LabelKey] = true
	}
	if !keys[keyLabelProduct] || !keys[keyLabelName] {
		t.Errorf("the card's fields do not name the product and the new name by key (keys=%v) — a "+
			"card that says an object is listed must list it", keys)
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
