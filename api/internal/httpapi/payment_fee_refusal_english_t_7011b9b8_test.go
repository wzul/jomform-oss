package httpapi

// THE FEE-CONSENT REFUSAL ON THE WIRE IS ENGLISH (card t_7011b9b8).
//
// THE DEFECT. PUT /v1/rest/settings/payment-fees refuses to switch a method on
// without the merchant's pass-on acknowledgement, and it answered
// {"error":"tick pengakuan pendedahan sebelum mengaktifkan caj pemprosesan"}.
// The API serves ONE language to every client whatever its locale, so an
// English-only refusal is the rule; a Malay one is prose a curl caller, an
// API-key integration and the dashboard's own apiErr fallback all receive
// verbatim.
//
// WHY IT SURVIVED: the Settings page pre-checks the checkbox itself
// (`showMsgFail(t("set.feesNeedAck"))` in web/src/app/settings/page.tsx), so the
// dashboard never renders this body — only a caller that is NOT the dashboard
// sees it, and nothing drove that path.
//
// WHY THIS TEST DRIVES THE ROUTE instead of reading the literal: the assertion
// that matters is the bytes a caller reads. A source-level check would still
// pass if the handler stopped using the message. The request goes through the
// registered mux with a real session, exactly as a client sends it, and the
// status is asserted too — the refusal must stay a 400, not become a 500 while
// the wording changes.
//
// Integration against a real Postgres + Valkey (the session cookie is a Valkey
// key), skipped without the test env — like every other test in this package.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// feeAckRefusalEnglish is the sentence the refusal must carry. It is written out
// here on purpose: this is the pin, so a revert to Malay (or a silent reword
// nobody meant) shows up as a failing assertion with both strings printed.
const feeAckRefusalEnglish = "tick the disclosure acknowledgement before enabling the processing fee"

func TestFeeAckRefusalBodyIsEnglish(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	sess := authTestValkey(t) // skips without JOMFORM_TEST_VALKEY_URL

	// Fixture: one user + workspace + account, the same shape the GET-side test
	// (payment_fee_ack_text_t_541a31d5_test.go) builds. Fresh rows per call so
	// this test never collides with a peer's.
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('feeack-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ws := dbstore.NewWorkspaceStore(pool)
	wsSlug := "feeack-" + userID[:8]
	if _, err := ws.Create(ctx, userID, "Fee ack ws", wsSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'feeack-' || md5(random()::text), 'feeack-' || md5(random()::text),
		         (SELECT id FROM workspaces WHERE slug = $2))
		 RETURNING id`, userID, wsSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	h := &RESTHandlers{
		Accounts:    dbstore.NewAccountStore(pool),
		Users:       dbstore.NewUserStore(pool),
		Workspaces:  ws,
		Sessions:    sess,
		PaymentFees: dbstore.NewPaymentFeeStore(pool),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Enabling fpx with a real fee and NO acknowledgement: the only branch that
	// refuses this request. Acknowledged:true is deliberately absent, not false,
	// so the body also covers a client that omits the field entirely.
	payload := `{"rates":[{"method":"fpx","fee_enabled":true,"fee_fixed_sen":100,"fee_percent_bp":0}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/v1/rest/settings/payment-fees?workspace="+wsSlug, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /v1/rest/settings/payment-fees without the acknowledgement = %d, want 400 "+
			"(body %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode refusal body %q: %v", rec.Body.String(), err)
	}
	if body.Error != feeAckRefusalEnglish {
		t.Fatalf("the fee-consent refusal body is %q\nwant %q\n"+
			"the API serves one language to every client, so this refusal must be English — "+
			"the dashboard hides it behind its own t(\"set.feesNeedAck\") pre-check, which is why "+
			"only a REST/MCP caller ever reads it",
			body.Error, feeAckRefusalEnglish)
	}
}
