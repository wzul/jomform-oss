package chat

// Card t_bf4feea2 — the confirm that answered a bare 400 after spending the token.
//
// WHAT WAS MEASURED ON STAGING (7 Nov). A merchant asked the assistant to create a
// form, the card appeared, and Confirm answered
//
//	INFO http_request method=POST path=/v1/rest/chat/confirm status=400 duration_ms=113
//	UI: "The change could not be applied. Please try again."
//
// while chat_write_proposals showed the row with consumed_at set — so the failure
// happened AFTER the atomic claim, and "please try again" was advice that could
// never work. The server logged nothing that named the cause.
//
// THE CAUSE, read out of the staging database and reproduced here with the args the
// model actually stored (verbatim in stagingUpsertFormArgs): the test account was
// created by a magic link and never got a workspace, so `accounts.workspace_id` was
// NULL. upsert_form's CREATE half runs mcpsrv.requireLiveWorkspace, which refuses
// with mcpsrv.WorkspaceRequiredMessage — after the claim. The lane could only wrap
// that as `server refused: %s`, so the REST edge mapped it to its `default:` arm: a
// 400 carrying the untranslatable class code `request.badRequest`, which the page
// turns into its generic "try again" copy.
//
// The answer to "was it a role refusal, invalid arguments, or a dbstore error" is
// therefore NONE of the three, and the two tests below are the proof rather than
// the assertion:
//
//   - on the staging account's state (no workspace) the confirm refuses with the
//     gate's own sentence, classified as ErrProposalWorkspaceRequired, and no form
//     is created — the refusal is BEFORE the write, not after it;
//   - with the SAME arguments and a workspace present, the confirm SUCCEEDS and the
//     form exists (slug customer-feedback, the long-text block intact). That is the
//     control which rules the arguments out: if the block shape or the title were
//     the problem, this half would fail too.
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the standard test env).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

// stagingUpsertFormArgs is the args column of the proposal row the card measured on
// staging (chat_write_proposals id 7ad22268-…), copied verbatim: the model proposed
// a "field" block wrapping a textarea. Nothing here is reconstructed from memory,
// which is what makes the tests below a reproduction rather than an approximation.
const stagingUpsertFormArgs = `{"title":"Customer Feedback","config":{"blocks":[{"type":"field","field":{"name":"message","rows":5,"type":"textarea","label":"Your message","required":true}}]}}`

// createFixture is a user + account with (or without) a workspace, over the real
// MCP server and the real database. The workspace is the ONE variable the two tests
// below change, so the difference in outcome cannot come from anything else.
type createFixture struct {
	accountID string
	userID    string
	srv       *mcpsrv.Server
}

// newCreateFixture builds an account the way staging's test account was: a user,
// and — when withWorkspace is false — an account with NO workspace (the signup
// shape, #718), which is exactly `accounts.workspace_id IS NULL`.
func newCreateFixture(t *testing.T, withWorkspace bool) *createFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('chat-create-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if withWorkspace {
		var workspaceID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO workspaces (owner_user_id, name, slug)
			 VALUES ($1, 'chat-create-ws-' || md5(random()::text), 'chat-create-ws-' || md5(random()::text))
			 RETURNING id`, userID).Scan(&workspaceID); err != nil {
			t.Fatalf("create workspace: %v", err)
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
			 VALUES ($1, 'chat-create-' || md5(random()::text), 'chat-create-' || md5(random()::text), $2)
			 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
			t.Fatalf("create account: %v", err)
		}
	} else {
		if err := pool.QueryRow(ctx,
			`INSERT INTO accounts (owner_user_id, name, slug)
			 VALUES ($1, 'chat-create-' || md5(random()::text), 'chat-create-' || md5(random()::text))
			 RETURNING id`, userID).Scan(&accountID); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}

	// The real server over the real stores. chip/payments are wired with a stub
	// endpoint for the same reason the phase-1 fixture does it: an unwired
	// dependency would refuse for a reason that is not the one under test.
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(encKey, []byte("sk_tes...cret"))
	if err != nil {
		t.Fatalf("encrypt chip secret: %v", err)
	}
	accounts := dbstore.NewAccountStore(pool)
	if err := accounts.SetChipCredentials(ctx, accountID, "br_chat_create", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip credentials: %v", err)
	}
	chipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(chipSrv.Close)
	sales := dbstore.NewSaleStore(pool)
	subs := dbstore.NewSubscriptionStore(pool)
	srv := mcpsrv.New(&mcpsrv.Deps{
		Forms:             dbstore.NewFormStore(pool),
		Accounts:          accounts,
		Keys:              dbstore.NewAPIKeyStore(pool),
		Products:          dbstore.NewProductStore(pool),
		Sales:             sales,
		Subs:              subs,
		Webhooks:          dbstore.NewWebhookStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers:  dbstore.NewWorkspaceMemberStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Users:             dbstore.NewUserStore(pool),
		Feedback:          dbstore.NewFeedbackThreadStore(pool),
	}, chip.New(chipSrv.URL), encKey,
		payments.New(chip.New(chipSrv.URL), accounts, sales, subs, encKey, "http://localhost:8080"))

	return &createFixture{accountID: accountID, userID: userID, srv: srv}
}

// runner wires the production session constructor and a real proposal store.
func (f *createFixture) runner(t *testing.T) *Runner {
	t.Helper()
	r := &Runner{mcp: f.srv, proposals: dbstore.NewChatWriteProposalStore(testdb.Open(t))}
	r.newSession = r.mcpSession
	return r
}

// ctxs are the two identities the REST edge builds: the message's readonly one and
// the confirmation's write one, both scoped to the account this request resolved.
func (f *createFixture) ctxs() (read, write context.Context) {
	read = mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID, Scopes: ReadOnlyScopes(), Agent: "in-app-chat",
	})
	read = mcpsrv.WithCallScope(read, &mcpsrv.CallScope{Accounts: []string{f.accountID}})
	write = mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID, Scopes: WriteScopes(), Agent: "in-app-chat-confirm",
	})
	write = mcpsrv.WithCallScope(write, &mcpsrv.CallScope{Accounts: []string{f.accountID}})
	return read, write
}

// proposeStagingCreate drives the model's own path (the propose_write dispatch) with
// the arguments the model stored on staging, and returns the card the user saw.
func (f *createFixture) proposeStagingCreate(t *testing.T, r *Runner) (read, write context.Context, card *ProposalCard) {
	t.Helper()
	read, write = f.ctxs()
	caller, err := r.newSession(read)
	if err != nil {
		t.Fatalf("open the propose session: %v", err)
	}
	defer caller.close()
	st := &runState{}
	body := `{"tool":"upsert_form","arguments":` + stagingUpsertFormArgs + `,"reason":"user asked for a customer feedback form"}`
	out, ref, err := r.dispatchIn(read, caller, toolCall("p1", ToolProposeWrite, body), st)
	if err != nil || ref != nil {
		t.Fatalf("propose_write(upsert_form) was not accepted: err=%v refused=%v out=%s", err, ref != nil, out)
	}
	card = st.lastCard()
	if card == nil || card.Token == "" {
		t.Fatalf("propose_write produced no token-bearing card (state=%+v)", st)
	}
	return read, write, card
}

// TestConfirmFormCreationOnAnAccountWithNoWorkspaceNamesItsCause is the card's
// first criterion: the failure must be REPORTED with its real cause, not guessed at
// by whoever reads the user's screenshot.
func TestConfirmFormCreationOnAnAccountWithNoWorkspaceNamesItsCause(t *testing.T) {
	requireAcceptanceDB(t)
	f := newCreateFixture(t, false)
	r := f.runner(t)

	read, write, card := f.proposeStagingCreate(t, r)

	// The card is offered — that is the pre-fix behaviour the report describes
	// ("kad cadangan muncul … butang Confirm / Cancel — betul"). The defect is what
	// happens next, so the proposal must really reach the browser.
	if card.Tool != "upsert_form" {
		t.Fatalf("card tool = %q, want upsert_form", card.Tool)
	}

	_, err := r.Confirm(read, write, f.userID, card.Token, card.ArgsHash)
	if err == nil {
		// If this ever succeeds the rest of the test is moot: say so loudly rather
		// than letting a green suite imply the no-workspace gate was exercised.
		t.Fatalf("the confirm SUCCEEDED for an account with no workspace — the workspace gate no longer " +
			"refuses a form creation, so this test cannot observe the staging defect at all")
	}
	// THE CAUSE. Before the fix this was `server refused: …` (unclassified), which
	// the REST edge turned into a generic 400 and the page into "please try again".
	if !errors.Is(err, ErrProposalWorkspaceRequired) {
		t.Fatalf("confirm error = %v, want ErrProposalWorkspaceRequired — the no-workspace refusal is not "+
			"classified, so the confirm route can only answer its generic 400 and the user is told to "+
			"retry a card whose token was just spent", err)
	}
	// And it stays classifiable the way the audit row and the log line read it.
	if got := ProposeRefusalKind(err); got != "workspace_required" {
		t.Errorf("ProposeRefusalKind(err) = %q, want workspace_required — the log line and the audit row "+
			"would name no cause", got)
	}
	// The error carries the GATE's own sentence, so the operator log quotes the
	// refusal the tool actually made rather than a paraphrase written here.
	if !strings.Contains(err.Error(), mcpsrv.WorkspaceRequiredMessage) {
		t.Errorf("the error does not quote the gate's refusal %q: %v", mcpsrv.WorkspaceRequiredMessage, err)
	}
	// THE LOG LINE'S TWO OTHER FIELDS (card requirement: request_id, tool, err).
	// request_id is added by the REST edge; the tool and the stage have to come
	// from here, or every failed confirm logs an empty tool.
	var cf *ConfirmFailure
	if !errors.As(err, &cf) {
		t.Fatalf("the confirm error is not a *ConfirmFailure (%T) — the edge cannot log which tool failed", err)
	}
	if cf.Tool != "upsert_form" {
		t.Errorf("ConfirmFailure.Tool = %q, want upsert_form — the WARN line would not name the tool", cf.Tool)
	}
	if cf.Stage != "refused" {
		t.Errorf("ConfirmFailure.Stage = %q, want refused — the log line could not say where the lane stopped", cf.Stage)
	}

	// Nothing was written: the refusal arrives before the insert (proven by the
	// count, not by reading the error).
	if n := f.countForms(t); n != 0 {
		t.Errorf("%d form(s) exist after a refused creation — the refusal arrived AFTER the write", n)
	}
	// The token IS spent (that is the fact that makes "try again" false advice), so
	// a second confirm has to answer the consumed refusal rather than anything else.
	_, err2 := r.Confirm(read, write, f.userID, card.Token, card.ArgsHash)
	if !errors.Is(err2, dbstore.ErrChatProposalConsumed) {
		t.Errorf("a second confirm returned %v, want ErrChatProposalConsumed — a refused execution must "+
			"still leave the token spent (the file header's rule), and the UI copy depends on it", err2)
	}
}

// TestTheSameCreateSucceedsOnceTheAccountHasAWorkspace is the control that answers
// "was it the arguments?" — Wan's question, and the one a reader of the failure
// alone cannot settle.
//
// The arguments are byte-identical to the staging row; the ONLY difference is the
// workspace. If the block shape, the title or a dbstore constraint were at fault,
// this half would fail the same way.
func TestTheSameCreateSucceedsOnceTheAccountHasAWorkspace(t *testing.T) {
	requireAcceptanceDB(t)
	f := newCreateFixture(t, true)
	r := f.runner(t)

	read, write, card := f.proposeStagingCreate(t, r)

	res, err := r.Confirm(read, write, f.userID, card.Token, card.ArgsHash)
	if err != nil {
		t.Fatalf("confirm the form creation with a workspace present: %v — the staging arguments are "+
			"valid (the same set refused with the no-workspace cause above), so this failure is a "+
			"different defect", err)
	}
	if res.Tool != "upsert_form" {
		t.Fatalf("confirmed tool = %q, want upsert_form", res.Tool)
	}
	slug := f.formSlug(t)
	if slug != "customer-feedback" {
		t.Errorf("created form slug = %q, want customer-feedback — the model sent a title, not a slug, "+
			"and the tool derives the address from it", slug)
	}
	// The block the model proposed survives verbatim: a creation that "succeeded"
	// with an empty config would hand the merchant a form that collects nothing.
	if n := f.blockCount(t); n != 1 {
		t.Errorf("the created form has %d block(s), want 1 — the confirmed config was not stored", n)
	}
}

// countForms is the account's live form count, read from Postgres.
func (f *createFixture) countForms(t *testing.T) int {
	t.Helper()
	var n int
	if err := testdb.Open(t).QueryRow(context.Background(),
		`SELECT count(*) FROM forms WHERE account_id=$1 AND deleted_at IS NULL`, f.accountID).Scan(&n); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	return n
}

// formSlug reads the account's single form's slug.
func (f *createFixture) formSlug(t *testing.T) string {
	t.Helper()
	var slug string
	if err := testdb.Open(t).QueryRow(context.Background(),
		`SELECT slug FROM forms WHERE account_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		f.accountID).Scan(&slug); err != nil {
		t.Fatalf("read the created form's slug: %v", err)
	}
	return slug
}

// blockCount reads the stored config's block count through jsonb, so the assertion
// is about what the DATABASE holds rather than about the tool's own result string.
func (f *createFixture) blockCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := testdb.Open(t).QueryRow(context.Background(),
		`SELECT COALESCE(jsonb_array_length(config->'blocks'), 0) FROM forms
		  WHERE account_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		f.accountID).Scan(&n); err != nil {
		t.Fatalf("read the created form's block count: %v", err)
	}
	return n
}
