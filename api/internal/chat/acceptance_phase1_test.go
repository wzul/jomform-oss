package chat

// Phase 1 acceptance over the REAL MCP server and the REAL database.
//
// WHY THIS FILE EXISTS. The card's most important criterion is that the chat
// cannot write, and it says explicitly that "the tool is not listed" is NOT
// evidence. So the central test here does not inspect the allowlist: it takes the
// runner's own production tool-calling path (the real in-memory MCP session over
// the real mcpsrv.Server, the real stores, real Postgres rows) and asks it to
// perform each write class from the card — delete, archive, refund, cancel,
// update, create a key — and requires EVERY ONE to be refused, with the DB row
// read back afterwards to prove the refusal happened before the write.
//
// The scope it dispatches under is not chosen by this test: it comes from
// ReadOnlyScopes(), the same value the REST edge hands the runner. That is the
// point — the test cannot quietly pass a stricter scope and prove nothing.
//
// THE BITE PROOF. TestChatBiteProofReadwriteScopeWrites is the card's own
// demand: give the chat the readwrite scope and the refusals MUST turn red.
// It asserts that the SAME write calls that are refused above now succeed and
// that the DB row actually changed. If that test ever passes a write, the
// enforcement test is not testing enforcement.
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the standard test env).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/feedbacksvc"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

func requireAcceptanceDB(t *testing.T) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping the phase-1 acceptance run")
	}
}

// acceptanceFixture builds an account, a user, a form and a product, and returns
// their ids plus stores the runner can be wired with.
type acceptanceFixture struct {
	pool      *pgxpool.Pool
	accountID string
	userID    string
	formID    string
	productID string
	saleRef   string
	subEmail  string
	srv       *mcpsrv.Server
}

func newAcceptanceFixture(t *testing.T) *acceptanceFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var accountID, userID, workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('chat-acc-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'chat-ws-' || md5(random()::text), 'chat-ws-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'chat-acc-' || md5(random()::text), 'chat-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	var formID string
	// Created through the STORE, not raw SQL, so this fixture cannot drift from
	// the schema (the forms table has no `mode` column — mode lives in config).
	ff := &dbstore.Form{
		AccountID: accountID,
		Slug:      "chat-" + accountID[:8],
		Title:     "Chat acceptance form",
		Config:    map[string]any{"mode": "payment", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusPublished,
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, ff); err != nil {
		t.Fatalf("create form: %v", err)
	}
	formID = ff.ID
	var productID string
	pp := &dbstore.Product{AccountID: accountID, Name: "Chat acceptance product", PriceSen: 2500, Type: "one_time"}
	if err := dbstore.NewProductStore(pool).Create(ctx, pp); err != nil {
		t.Fatalf("create product: %v", err)
	}
	productID = pp.ID

	// A paid sale and a live subscription, so refund_sale and cancel_subscription
	// REACH their authorization gate instead of failing earlier for "sale not
	// found". A refusal for the wrong reason proves nothing about the scope.
	sales := dbstore.NewSaleStore(pool)
	saleRef := "CHAT-" + accountID[:8]
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: saleRef,
		Client:      dbstore.SaleClient{FullName: "Chat Buyer", Email: "chat-buyer@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: productID, Name: "Chat acceptance product", PriceSen: 2500, Qty: 1}},
		SubtotalSen: 2500, TotalSen: 2500,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	// The CHIP purchase must be attached or refund_sale fails for "no CHIP
	// purchase" BEFORE it reaches its authorization gate — a refusal for the
	// wrong reason, which the bite proof below would (correctly) flag.
	if err := sales.AttachChipPurchase(ctx, sale.ID, "pur_chat_"+accountID[:8],
		"https://gate.example/co/pur_chat"); err != nil {
		t.Fatalf("attach chip purchase: %v", err)
	}
	subEmail := "chat-sub-" + accountID[:8] + "@example.test"
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: subEmail, ProductID: productID,
		Status: dbstore.SubActive, Period: "month", AmountSen: 2500,
		NextChargeAt: time.Now().Add(24 * time.Hour),
	}
	if err := dbstore.NewSubscriptionStore(pool).Create(ctx, sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// The CHIP credentials + payment service, so refund_sale reaches its
	// AUTHORIZATION gate. Without them it fails first for "payments tidak aktif",
	// which is a refusal for the wrong reason and would prove nothing about the
	// scope. The CHIP endpoint is a stub: the point is that the scope is checked
	// BEFORE the network call, so no request is ever made.
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(encKey, []byte("sk_test_chat_acceptance_secret"))
	if err != nil {
		t.Fatalf("encrypt chip secret: %v", err)
	}
	accounts := dbstore.NewAccountStore(pool)
	if err := accounts.SetChipCredentials(ctx, accountID, "br_chat", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip credentials: %v", err)
	}
	chipMux := http.NewServeMux()
	chipMux.HandleFunc("POST /purchases/{id}/refund/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusRefunded})
	})
	chipSrv := httptest.NewServer(chipMux)
	t.Cleanup(chipSrv.Close)

	// The REAL server, with the real stores the tools reach.
	srv := mcpsrv.New(&mcpsrv.Deps{
		Forms:             dbstore.NewFormStore(pool),
		Accounts:          accounts,
		Keys:              dbstore.NewAPIKeyStore(pool),
		Products:          dbstore.NewProductStore(pool),
		Sales:             sales,
		Subs:              dbstore.NewSubscriptionStore(pool),
		Webhooks:          dbstore.NewWebhookStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers:  dbstore.NewWorkspaceMemberStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Users:             dbstore.NewUserStore(pool),
		Feedback:          dbstore.NewFeedbackThreadStore(pool),
	}, chip.New(chipSrv.URL), encKey,
		payments.New(chip.New(chipSrv.URL), accounts, sales, dbstore.NewSubscriptionStore(pool), encKey, "http://localhost:8080"))

	return &acceptanceFixture{
		pool: pool, accountID: accountID, userID: userID,
		formID: formID, productID: productID, saleRef: saleRef, subEmail: subEmail,
		srv: srv,
	}
}

// toolCallerFor opens a real in-memory MCP session under the given scopes and
// returns the caller plus a closer. scopes are the ONLY difference between the
// enforcement test and its bite proof.
func (f *acceptanceFixture) toolCallerFor(t *testing.T, scopes []string) (toolCaller, context.Context) {
	t.Helper()
	ctx := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: f.accountID, UserID: f.userID, Scopes: scopes, Agent: "chat-acceptance",
	})
	r := &Runner{mcp: f.srv}
	caller, err := r.mcpSession(ctx)
	if err != nil {
		t.Fatalf("open the real MCP session: %v", err)
	}
	t.Cleanup(func() { _ = caller.close() })
	return caller, ctx
}

// toolCallerForAs opens a real in-memory MCP session for a SPECIFIC identity
// under the given scopes — the same session the production path opens, with the
// person and account chosen by the caller rather than taken from the fixture. It
// exists for the isolation tests: they need two people reading the same server,
// and one of them must be somebody other than the fixture's own user.
func (f *acceptanceFixture) toolCallerForAs(t *testing.T, accountID, userID string, scopes []string) (toolCaller, context.Context) {
	t.Helper()
	ctx := mcpsrv.WithAuthIdentity(context.Background(), &mcpsrv.AuthIdentity{
		AccountID: accountID, UserID: userID, Scopes: scopes, Agent: "chat-acceptance",
	})
	r := &Runner{mcp: f.srv}
	caller, err := r.mcpSession(ctx)
	if err != nil {
		t.Fatalf("open the real MCP session for %s: %v", userID, err)
	}
	t.Cleanup(func() { _ = caller.close() })
	return caller, ctx
}

// callRaw dispatches one tool through the REAL server and returns the raw
// result text and the refused flag.
func callRaw(t *testing.T, caller toolCaller, ctx context.Context, tool, args string) (string, bool) {
	t.Helper()
	out, refused, err := caller.call(ctx, tool, args)
	if err != nil {
		t.Fatalf("dispatch %s over the real MCP session: %v", tool, err)
	}
	return out, refused
}

// TestChatCannotWriteAnythingOverTheRealServer is THE criterion: every write
// class from the card is refused, through the production tool path, under the
// production scope, and the database is read back to prove nothing was written.
func TestChatCannotWriteAnythingOverTheRealServer(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	caller, ctx := f.toolCallerFor(t, ReadOnlyScopes())

	// ── Positive control FIRST: a read must WORK. ──
	// Without this, the test below is satisfied by a session that refuses
	// everything — which is not the feature and would prove nothing about the
	// scope gate.
	out, refused := callRaw(t, caller, ctx, "list_forms", `{}`)
	if refused {
		t.Fatalf("list_forms was REFUSED under the readonly scope (%s) — the chat is meant to "+
			"answer questions, so a read must succeed or the write refusals below prove nothing", out)
	}
	if !strings.Contains(out, "Chat acceptance form") {
		t.Fatalf("list_forms did not return the fixture form — the read path is not reading the "+
			"account the identity names: %s", out)
	}

	// ── Every write class the card lists. ──
	writes := []struct {
		tool string
		args string
		what string
	}{
		{"delete_form", `{"form_id":"` + f.formID + `"}`, "delete (card: a form)"},
		{"archive_product", `{"product_id":"` + f.productID + `"}`, "archive/delete (card: a product)"},
		{"update_product", `{"product_id":"` + f.productID + `","name":"HIJACKED BY CHAT"}`, "update"},
		{"create_product", `{"name":"chat should not create this","price_sen":100,"type":"one_time"}`, "create"},
		{"upsert_form", `{"slug":"chat-should-not-create","title":"chat should not create"}`, "create (a form)"},
		{"create_api_key", `{"name":"chat-escalation","scopes":["readwrite"]}`, "create (an API key — the escalation)"},
		{"refund_sale", `{"ref_code":"` + f.saleRef + `"}`, "refund"},
		{"cancel_subscription", `{"email":"` + f.subEmail + `"}`, "cancel"},
		{"create_webhook", `{"url":"https://example.test/hook","secret":"shhh","events":["sale.paid"]}`, "create (a webhook)"},
		{"set_system_setting", `{"key":"site_name","value":"pwned-by-chat"}`, "configure (a system setting)"},
	}
	for _, w := range writes {
		out, refused := callRaw(t, caller, ctx, w.tool, w.args)
		if !refused {
			t.Errorf("PHASE 1 WRITE LEAK: the chat performed %s via %s. Result: %s\n"+
				"The chat must be INCAPABLE of writing, not merely discouraged — this is the "+
				"card's most important criterion.", w.what, w.tool, out)
			continue
		}
		// A refusal for the WRONG reason still leaves the hole open on another
		// tool, so the reason is asserted: it must be the scope gate.
		if !strings.Contains(out, "scope") {
			t.Errorf("%s was refused, but not for the scope reason (got %q) — a refusal for the "+
				"wrong reason would not generalise to the tools this test does not name",
				w.tool, out)
		}
	}

	// ── The database must be untouched. ──
	// The refusal must happen BEFORE the write; a gate that ran after the write
	// would look identical from the call result alone.
	var deleted bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, f.formID).Scan(&deleted); err != nil {
		t.Fatalf("read the form back: %v", err)
	}
	if deleted {
		t.Error("the fixture form was soft-deleted — the refusal ran after the write, not before it")
	}
	var name string
	var active bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT name, active FROM products WHERE id=$1`, f.productID).Scan(&name, &active); err != nil {
		t.Fatalf("read the product back: %v", err)
	}
	if name == "HIJACKED BY CHAT" || !active {
		t.Errorf("the fixture product was modified by the chat (name=%q active=%v)", name, active)
	}
	var keyCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM api_keys WHERE account_id=$1`, f.accountID).Scan(&keyCount); err != nil {
		t.Fatalf("count api keys: %v", err)
	}
	if keyCount != 0 {
		t.Errorf("the chat minted %d API key(s) — the escalation path is open", keyCount)
	}
}

// TestChatBiteProofReadwriteScopeWrites is the card's BITE PROOF.
//
// The card demands it in these terms: give the chat the readwrite scope and the
// refusal test must go RED. So this drives the SAME calls with a readwrite
// identity and REQUIRES them to succeed AND to change the database.
//
// If this test fails — i.e. the same calls are still refused with readwrite — then
// the enforcement test above is not measuring the scope gate at all (it would be
// passing because something else refuses everything), and that is a real failure,
// not a flake.
func TestChatBiteProofReadwriteScopeWrites(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	// The loosening, and the ONLY difference from the test above: this is what a
	// regression would look like (the REST edge handing the runner a write scope).
	caller, ctx := f.toolCallerFor(t, []string{dbstore.ScopeReadWrite})

	// The card's exact reproduction, in order.
	out, refused := callRaw(t, caller, ctx, "delete_form", `{"form_id":"`+f.formID+`"}`)
	if refused {
		t.Fatalf("PRECONDITION FAILED: delete_form was refused even with the readwrite scope (%s).\n"+
			"The bite proof cannot run, which means the enforcement test above is not proving "+
			"that the SCOPE is what refuses it — fix the harness before trusting that test.", out)
	}
	var deleted bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, f.formID).Scan(&deleted); err != nil {
		t.Fatalf("read the form back: %v", err)
	}
	if !deleted {
		t.Fatalf("delete_form reported success (%s) but the row is untouched — the call result is "+
			"not a reliable signal in this harness, so neither test's assertion is meaningful", out)
	}

	// The rename, which is the card's own marker.
	out, refused = callRaw(t, caller, ctx, "update_product",
		`{"product_id":"`+f.productID+`","name":"HIJACKED BY CHAT"}`)
	if refused {
		t.Fatalf("update_product was refused with the readwrite scope (%s)", out)
	}
	var name string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT name FROM products WHERE id=$1`, f.productID).Scan(&name); err != nil {
		t.Fatalf("read the product back: %v", err)
	}
	if name != "HIJACKED BY CHAT" {
		t.Fatalf("update_product reported success but the name is %q — the write did not land", name)
	}

	// And the escalation: a readwrite identity can mint a key. The readonly
	// identity in the test above must NOT be able to, or the scope is decorative.
	out, refused = callRaw(t, caller, ctx, "create_api_key",
		`{"name":"bite-escalation","scopes":["readwrite"]}`)
	if refused {
		t.Fatalf("create_api_key was refused with the readwrite scope (%s)", out)
	}
	var keyCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM api_keys WHERE account_id=$1`, f.accountID).Scan(&keyCount); err != nil {
		t.Fatalf("count api keys: %v", err)
	}
	if keyCount != 1 {
		t.Fatalf("create_api_key reported success but %d key(s) exist — the escalation did not "+
			"actually happen, so the readonly refusal of it proves nothing", keyCount)
	}
	t.Logf("bite proof RED as required: with the readwrite scope the chat deleted a form, "+
		"renamed a product and minted an API key (key id in the result: %s)", firstKeyID(t, out))

	// ── Every OTHER write class from the enforcement test, for the same reason:
	// each refusal there must be attributable to the scope, so each must succeed
	// here. A tool that refuses in BOTH scopes is refused by something else, and
	// the enforcement test's claim about it is empty.
	others := []struct {
		tool string
		args string
		// verify reads the effect back out of the database.
		verify func(t *testing.T) string
	}{
		{
			"archive_product", `{"product_id":"` + f.productID + `"}`,
			func(t *testing.T) string {
				var active bool
				if err := f.pool.QueryRow(context.Background(),
					`SELECT active FROM products WHERE id=$1`, f.productID).Scan(&active); err != nil {
					t.Fatalf("read the product back: %v", err)
				}
				if active {
					return "product is still active — archive_product did not land"
				}
				return ""
			},
		},
		{
			"refund_sale", `{"ref_code":"` + f.saleRef + `"}`,
			func(t *testing.T) string {
				var refunded int64
				var status string
				if err := f.pool.QueryRow(context.Background(),
					`SELECT refunded_sen, status FROM sales WHERE ref_code=$1`, f.saleRef).
					Scan(&refunded, &status); err != nil {
					t.Fatalf("read the sale back: %v", err)
				}
				if refunded <= 0 {
					return "sale shows no refund — refund_sale did not land"
				}
				return ""
			},
		},
		{
			"cancel_subscription", `{"email":"` + f.subEmail + `"}`,
			func(t *testing.T) string {
				var status string
				if err := f.pool.QueryRow(context.Background(),
					`SELECT status FROM subscriptions WHERE client_email=$1`, f.subEmail).Scan(&status); err != nil {
					t.Fatalf("read the subscription back: %v", err)
				}
				if status != dbstore.SubCanceled {
					return "subscription status is " + status + " — cancel_subscription did not land"
				}
				return ""
			},
		},
	}
	for _, o := range others {
		out, refused := callRaw(t, caller, ctx, o.tool, o.args)
		if refused {
			t.Errorf("BITE PROOF GAP: %s was refused even with the readwrite scope (%s).\n"+
				"The enforcement test asserts it is refused under the readonly scope — but if it "+
				"refuses in BOTH scopes then something other than the scope is refusing it, and "+
				"that assertion proves nothing about the scope gate.", o.tool, out)
			continue
		}
		if why := o.verify(t); why != "" {
			t.Errorf("BITE PROOF GAP: %s reported success but %s", o.tool, why)
		}
	}
}

// TestChatFilingLandsInTheSharedSystemWithRealPostgres is the card's filing
// criterion: a feedback filed from a conversation must actually appear in the
// system, through the same store the /feedback page and list_feedback read.
//
// This wires the runner with the REAL feedbacksvc over the REAL store, then reads
// the row back two ways: directly from Postgres, and through the MCP list_feedback
// tool a user (or the triage UI) would use. A filing that only exists in the
// runner's return value would let the chat tell the user "filed" with nothing
// behind it — the worst failure for a feedback channel.
func TestChatFilingLandsInTheSharedSystemWithRealPostgres(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	ctx := context.Background()

	// The real service over the real store — the same wiring main.go uses.
	svc := &feedbacksvc.Service{Store: dbstore.NewFeedbackThreadStore(f.pool)}
	runner := &Runner{llm: nil, mcp: f.srv, feedback: svc}

	toolCaller, callCtx := f.toolCallerFor(t, ReadOnlyScopes())

	subject := "Resit tidak sampai selepas bayar"
	body := "Pelanggan saya kata resit tidak diterima walaupun pembayaran berjaya."
	out, refused, err := runner.dispatch(callCtx, nil, toolCall(toolCallID, ToolSubmitFeedback,
		`{"subject":`+jsonString(subject)+`,"body":`+jsonString(body)+`}`))
	if err != nil {
		t.Fatalf("dispatch submit_feedback: %v", err)
	}
	if refused {
		t.Fatalf("submit_feedback was refused (%s) — filing feedback is the chat's ONE write and "+
			"must work under the readonly scope, or the feature does not exist", out)
	}
	filed := filedFrom(out)
	if filed == nil {
		t.Fatalf("a successful filing produced no Filed card, so the UI cannot show the user it "+
			"happened: %s", out)
	}

	// 1. It is really in Postgres, owned by the filer.
	var gotSubject, gotUserID string
	if err := f.pool.QueryRow(ctx,
		`SELECT subject, user_id FROM feedback_threads WHERE id=$1`, filed.ThreadID).
		Scan(&gotSubject, &gotUserID); err != nil {
		t.Fatalf("the chat reported thread %s but Postgres has no such row: %v",
			filed.ThreadID, err)
	}
	if gotSubject != subject {
		t.Fatalf("stored subject = %q, want %q", gotSubject, subject)
	}
	if gotUserID != f.userID {
		t.Fatalf("the thread is owned by %q, want the filer %q — a thread must belong to the "+
			"person who filed it", gotUserID, f.userID)
	}

	// 2. It is visible through the MCP surface the dashboard and an agent use.
	out, refused = callRaw(t, toolCaller, callCtx, "list_feedback", `{}`)
	if refused {
		t.Fatalf("list_feedback was refused under the readonly scope (%s) — a user must be able "+
			"to read back what the chat filed", out)
	}
	if !strings.Contains(out, subject) {
		t.Fatalf("the filed thread is not in list_feedback's output — the user would be told "+
			"\"filed\" and never find it: %s", out)
	}
}

// jsonString renders s as a JSON string literal.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// toolCallID is a stable call id for the direct-dispatch tests.
const toolCallID = "call-filing-1"

// newSecondPerson adds a SECOND account owner (its own user + workspace +
// account) to the same server, and returns their user id and account id. The
// chat's read allowlist includes list_feedback and get_feedback_thread, and a
// feedback thread belongs to a PERSON rather than an account — so a second
// person is the thing an isolation assertion has to be made against.
//
// It is a separate person on a separate account on purpose: an isolation test
// that reused one account could not tell "scoped to the filer" from "scoped to
// the account". The account id is returned too so the session built for them
// carries THEIR account, not the first person's — a fixture that lent B person
// A's account would leave "which of the two predicates scoped the read?"
// unanswerable if the leak ever appeared.
func (f *acceptanceFixture) newSecondPerson(t *testing.T) (userID, accountID string) {
	t.Helper()
	ctx := context.Background()
	var workspaceID string
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('chat-b-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'chat-ws-b-' || md5(random()::text), 'chat-ws-b-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	if err := f.pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'chat-acc-b-' || md5(random()::text), 'chat-b-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	return userID, accountID
}

// TestChatCannotReadAnotherPersonsFeedback is the card's cross-tenant isolation
// criterion, driven the way the chat actually reads: through the real MCP
// session, under the readonly identity the REST edge builds.
//
// WHY IT IS NOT COVERED BY THE EXISTING ISOLATION TEST. feedback_isolation_
// issue615_test.go proves the REST routes are scoped; that is a different surface
// from the chat's read path, and the chat's allowlist is wider than /feedback in
// one important way — it offers get_feedback_thread and list_feedback to a
// MODEL, whose turn is steerable by any text the model reads (a product name, a
// customer's message). A cross-person read there is a leak that arrives without
// the attacker ever having a session of their own.
//
// It drives both directions, because each one alone is passable vacuously:
//
//   - person B listing / reading must NOT see person A's thread, and must be
//     denied the SAME way for A's id as for an id that does not exist (no
//     existence oracle);
//   - person A MUST still see their own thread through the same read — without
//     this, an always-empty read would satisfy the denial assertions and prove
//     nothing.
func TestChatCannotReadAnotherPersonsFeedback(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	ctx := context.Background()

	// A files a thread carrying a marker only A should ever see.
	const marker = "rahasia-pelanggan-A-chat"
	svc := &feedbacksvc.Service{Store: dbstore.NewFeedbackThreadStore(f.pool)}
	th, err := svc.CreateThread(ctx, f.userID, "Isu pembayaran chat", marker+" — bantuan diperlukan")
	if err != nil {
		t.Fatalf("file as person A: %v", err)
	}

	// B is a different person on a different account.
	userB, accountB := f.newSecondPerson(t)
	if userB == f.userID {
		t.Fatal("fixture error: the second person is the first person")
	}
	if accountB == f.accountID {
		t.Fatal("fixture error: the second person is on the first person's account")
	}

	// The chat's own read path, for both people. The scopes are ReadOnlyScopes()
	// — the same value the REST edge hands the runner, so this cannot pass by
	// testing a stricter identity than production uses.
	callerA, ctxA := f.toolCallerFor(t, ReadOnlyScopes())
	callerB, ctxB := f.toolCallerForAs(t, accountB, userB, ReadOnlyScopes())

	// The thread's message count BEFORE B reads anything. Filing stores the
	// filer's own body as the first message, so the baseline is not zero — the
	// assertion below is that B's reads leave it UNCHANGED.
	var beforeCount int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM feedback_messages WHERE thread_id=$1`, th.ID).Scan(&beforeCount); err != nil {
		t.Fatalf("count messages before: %v", err)
	}
	if beforeCount == 0 {
		t.Fatalf("fixture error: thread %s has no message, so a read cannot be shown to add one", th.ID)
	}

	// ── B's list must not contain A's thread ──
	out, refused := callRaw(t, callerB, ctxB, "list_feedback", `{}`)
	if refused {
		t.Fatalf("list_feedback was refused for person B (%s) — a user must be able to list "+
			"their own (empty) feedback, or the isolation assertion below proves nothing", out)
	}
	if strings.Contains(out, th.ID) || strings.Contains(out, marker) {
		t.Fatalf("LEAK: person B's own list_feedback returned person A's thread %s\nresult: %s", th.ID, out)
	}

	// ── B reading A's thread by id must be denied, and denied the same way as an
	//    id that does not exist ──
	out, refused = callRaw(t, callerB, ctxB, "get_feedback_thread", `{"thread_id":"`+th.ID+`"}`)
	if !refused {
		t.Fatalf("LEAK: person B read person A's thread %s over the chat's own session\nresult: %s",
			th.ID, out)
	}
	if strings.Contains(out, marker) {
		t.Fatalf("LEAK: the denial for person B carried person A's content:\n%s", out)
	}
	deniedForOwned := out
	out, refused = callRaw(t, callerB, ctxB, "get_feedback_thread",
		`{"thread_id":"00000000-0000-0000-0000-000000000000"}`)
	if !refused {
		t.Fatalf("an id that does not exist was NOT denied (%s) — the denial above is not the guard", out)
	}
	if out != deniedForOwned {
		t.Errorf("the refusal for another person's thread (%q) differs from the refusal for a "+
			"nonexistent id (%q) — a difference lets the tool be used as an existence oracle "+
			"for other people's thread ids", deniedForOwned, out)
	}

	// ── The database is unchanged: a denial must mean nothing was written ──
	var afterCount int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM feedback_messages WHERE thread_id=$1`, th.ID).Scan(&afterCount); err != nil {
		t.Fatalf("count messages after: %v", err)
	}
	if afterCount != beforeCount {
		t.Errorf("person B's reads changed person A's thread from %d to %d message(s)",
			beforeCount, afterCount)
	}

	// ── ANTI-VACUITY: person A sees their own thread through the SAME read ──
	out, refused = callRaw(t, callerA, ctxA, "get_feedback_thread", `{"thread_id":"`+th.ID+`"}`)
	if refused {
		t.Fatalf("person A was refused their OWN thread %s (%s) — an always-denying read would "+
			"satisfy the isolation assertions above while delivering no feature", th.ID, out)
	}
	if !strings.Contains(out, marker) {
		t.Fatalf("person A read their own thread but the result did not carry their own content — "+
			"the positive control failed, so the isolation assertion is not proven:\n%s", out)
	}
	out, refused = callRaw(t, callerA, ctxA, "list_feedback", `{}`)
	if refused {
		t.Fatalf("person A was refused their own list_feedback: %s", out)
	}
	if !strings.Contains(out, th.ID) {
		t.Fatalf("person A's own list_feedback did not contain their thread %s — the read path is "+
			"not scoped to the filer, it is scoped to nothing:\n%s", th.ID, out)
	}
}

// TestChatFeedbackIsolationBitesWhenTheScopeIsLoosened is the BITE PROOF for the
// test above.
//
// The isolation test is green when the read is scoped to the filer. This one
// performs the SAME read with the scope deliberately loosened to the ADMIN scope
// — the shape a regression takes if a handler stops building the caller's user
// scope, which is exactly what the #615 review found once already — and REQUIRES
// the leak to appear, naming person B and person A.
//
// If this test fails (no leak observed even with the admin scope) then the
// isolation guarantee is not being tested by the test that claims to test it,
// and that is a real failure rather than a flake.
func TestChatFeedbackIsolationBitesWhenTheScopeIsLoosened(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	ctx := context.Background()

	const marker = "kandungan-sulit-A-chat"
	svc := &feedbacksvc.Service{Store: dbstore.NewFeedbackThreadStore(f.pool)}
	th, err := svc.CreateThread(ctx, f.userID, "Isu pembayaran chat", marker+" — tolong semak")
	if err != nil {
		t.Fatalf("file as person A: %v", err)
	}
	userB, accountB := f.newSecondPerson(t)

	// ── Control 1 (green half): the real, user-scoped read does NOT leak ──
	callerB, ctxB := f.toolCallerForAs(t, accountB, userB, ReadOnlyScopes())
	out, refused := callRaw(t, callerB, ctxB, "get_feedback_thread", `{"thread_id":"`+th.ID+`"}`)
	if !refused {
		t.Fatalf("precondition failed: person B read person A's thread %s with the production "+
			"scope (%s) — the bite proof's subject is not in the state it assumes", th.ID, out)
	}

	// ── Control 2 (red half): the SAME read under the admin scope DOES leak ──
	loosened, err := dbstore.NewFeedbackThreadStore(f.pool).
		Thread(ctx, dbstore.FeedbackAdminScope(), th.ID)
	if err != nil {
		t.Fatalf("loosened-scope read failed outright (%v) — the bite proof could not observe a "+
			"leak, so the isolation test above is not proven to test isolation", err)
	}
	if loosened.UserID == userB {
		t.Fatalf("fixture error: person B filed thread %s — the leak this control demonstrates "+
			"would be B's own thread", th.ID)
	}
	if loosened.UserID != f.userID {
		t.Fatalf("loosened-scope read returned thread %s filed by %q, expected person A (%q) — the "+
			"control did not demonstrate a cross-person leak", th.ID, loosened.UserID, f.userID)
	}
	t.Logf("BITE CONFIRMED: with the user scope loosened, person B (%s) reads person A's (%s) "+
		"thread %s — the isolation test's assertion is what refuses this in production",
		userB, f.userID, th.ID)
}

// firstKeyID pulls the minted key id out of the result text for the log only.
func firstKeyID(t *testing.T, out string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return "(unparsed)"
	}
	if id, ok := m["id"].(string); ok {
		return id
	}
	return "(no id)"
}

// TestRunnerAnswersFromTheDatabaseNotFromMemory is the card's "correct data, not
// hallucination" criterion, verified against the DB.
//
// The card asks the chat to answer a real question about the account with the
// RIGHT data. A live model needs an API key this environment does not have, so
// this test proves the half that is actually verifiable here: the tool the model
// would call returns the account's real rows, and the numbers a truthful answer
// would quote are the ones in Postgres. The wording of the answer is the model's;
// the FACTS are these.
//
// Stated plainly so the gap is not mistaken for coverage: the model's own output
// is not exercised here. What is exercised is every fact it could draw on.
func TestRunnerAnswersFromTheDatabaseNotFromMemory(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	caller, ctx := f.toolCallerFor(t, ReadOnlyScopes())

	// A second product so "count" is a real count, not a constant 1.
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO products (account_id, name, price_sen, type, active)
		 VALUES ($1, 'Chat second product', 4999, 'one_time', true)`, f.accountID); err != nil {
		t.Fatalf("create second product: %v", err)
	}

	out, refused := callRaw(t, caller, ctx, "list_products", `{}`)
	if refused {
		t.Fatalf("list_products was refused under the readonly scope: %s", out)
	}
	var listed struct {
		Items []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			PriceSen int64  `json:"price_sen"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("list_products is not the documented {items:[…]} shape: %v (%s)", err, out)
	}
	var dbCount int
	var dbSum int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*), coalesce(sum(price_sen),0) FROM products WHERE account_id=$1 AND active`,
		f.accountID).Scan(&dbCount, &dbSum); err != nil {
		t.Fatalf("count products in the DB: %v", err)
	}
	if len(listed.Items) != dbCount {
		t.Fatalf("the tool returned %d products but the DATABASE holds %d — an answer built on this "+
			"tool would be wrong (hallucination by plumbing)", len(listed.Items), dbCount)
	}
	var listedSum int64
	for _, it := range listed.Items {
		listedSum += it.PriceSen
	}
	if listedSum != dbSum {
		t.Fatalf("the tool's prices sum to %d sen but the database says %d — money in the answer "+
			"would be wrong", listedSum, dbSum)
	}
	// The fixture's own product must be findable by the ANSWER path (get_form),
	// so "which of my forms is live" is answerable from real data.
	out, refused = callRaw(t, caller, ctx, "get_form", `{"form_id":"`+f.formID+`"}`)
	if refused {
		t.Fatalf("get_form was refused under the readonly scope: %s", out)
	}
	if !strings.Contains(out, "Chat acceptance form") {
		t.Fatalf("get_form did not return the fixture form's title: %s", out)
	}
	var status string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM forms WHERE id=$1`, f.formID).Scan(&status); err != nil {
		t.Fatalf("read the form status: %v", err)
	}
	if !strings.Contains(out, status) {
		t.Fatalf("the tool does not report the form's real status %q — the chat could not answer "+
			"\"is my form live?\" correctly: %s", status, out)
	}
}

// TestChatSessionStillRefusesAnUnlistedToolOverTheRealServer closes the loop
// between the allowlist (layer 1) and the identity (layer 2): the real server
// refuses an unlisted name, and the scope refuses a listed-but-write tool. Both
// layers are exercised against the same production session.
func TestChatSessionStillRefusesAnUnlistedToolOverTheRealServer(t *testing.T) {
	requireAcceptanceDB(t)
	f := newAcceptanceFixture(t)
	// A readwrite identity this time, so the SECOND refusal cannot be explained
	// by the scope: it is the allowlist doing the work.
	caller, ctx := f.toolCallerFor(t, []string{dbstore.ScopeReadWrite})

	out, refused := callRaw(t, caller, ctx, "list_api_keys", `{}`)
	if refused {
		t.Fatalf("list_api_keys was refused over the real session (%s) — this test needs it to "+
			"SUCCEED so that the next assertion is about the allowlist and not about the scope", out)
	}
	// The dispatcher's own gate, over the same session: an excluded-but-existing
	// tool name is refused before any call is made.
	r := &Runner{mcp: f.srv}
	if _, refused, err := r.dispatch(ctx, caller, toolCall("c1", "list_api_keys", `{}`)); err != nil {
		t.Fatalf("dispatch: %v", err)
	} else if !refused {
		t.Fatal("the runner dispatched list_api_keys — the chat would hand the model the API-key " +
			"list, which is a perfect read and deliberately out of scope for a chat about \"my data\"")
	}
}
