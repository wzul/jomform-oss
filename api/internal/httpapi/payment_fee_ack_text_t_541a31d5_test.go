package httpapi

// THE REST SURFACE SERVES THE ENGLISH ACKNOWLEDGEMENT (card t_541a31d5).
//
// THE DEFECT. GET /v1/rest/settings/payment-fees answered
// {"acknowledgement_text": "Saya faham caj pemprosesan ini dipindahkan …"}, and
// the Settings page printed that string verbatim
// (`{feeAckText || t("set.feesAck")}`), so a merchant on an ENGLISH dashboard
// (default `en`, per-profile `users.lang`) read Malay. The English sentence
// already existed in the dashboard's dictionary and is quoted by
// https://jomform.com/docs/payment-fees, which says the sentence is served by the
// API — so prod's docs and prod's API contradicted each other.
//
// WHY THIS TEST EXECUTES THE ROUTE rather than reading the constant: the constant
// being English is `api/internal/payments`' own test. What can go wrong HERE is
// the wiring — a handler that forgets the field, serves a different string, or
// stops resolving the workspace — and only a request through the registered mux
// shows that. The assertion is against `payments.FeeAcknowledgementText` on
// purpose: two copies of a sentence is how the dashboard and the API drifted
// apart in the first place.
//
// Integration against a real Postgres + Valkey (the session cookie is a Valkey
// key), skipped without the test env — like every other test in this package.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

func TestFeeSettingsRouteServesTheEnglishAcknowledgement(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	sess := authTestValkey(t) // skips without JOMFORM_TEST_VALKEY_URL

	var userID, wsSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('feeack-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ws := dbstore.NewWorkspaceStore(pool)
	if _, err := ws.Create(ctx, userID, "Fee ack ws", "feeack-"+userID[:8]); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	wsSlug = "feeack-" + userID[:8]
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
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/settings/payment-fees?workspace="+wsSlug, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/settings/payment-fees = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		AcknowledgementText string `json:"acknowledgement_text"`
		Rates               []struct {
			Method string `json:"method"`
		} `json:"rates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (%s)", err, rec.Body.String())
	}
	if len(body.Rates) == 0 {
		t.Fatal("the answer carries no rates — this route resolves a workspace the fixture did not own, " +
			"so the acknowledgement assertion below would be about an unrelated account")
	}
	if body.AcknowledgementText != payments.FeeAcknowledgementText {
		t.Fatalf("acknowledgement_text = %q\nwant %q\n"+
			"the dashboard renders its own dictionary arm and the MCP tool serves this constant; a "+
			"different value here means the two surfaces the merchant and their agent read have drifted",
			body.AcknowledgementText, payments.FeeAcknowledgementText)
	}
}
