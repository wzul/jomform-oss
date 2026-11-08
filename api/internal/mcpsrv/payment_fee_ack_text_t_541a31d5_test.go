package mcpsrv

// THE MCP SURFACE SERVES THE SAME ENGLISH ACKNOWLEDGEMENT (card t_541a31d5).
//
// THE DEFECT. get_payment_fees returned acknowledgement_text in MALAY
// ("Saya faham caj pemprosesan ini dipindahkan kepada pembayar …"), because the
// constant lived in api/internal/payments with the dashboard's sentence baked in.
// An agent reading the disclosure was told, in Malay, what the merchant was
// agreeing to — on a tool whose every other field is a machine value. The
// dashboard printed the same string verbatim, which is how an English Settings
// page showed Malay.
//
// WHAT THIS TEST PINS, and what the payments package's own test cannot:
//
//	1. THE TOOL REALLY READS THE SHARED CONSTANT. `payments.FeeAcknowledgementText`
//	   being English is proven in api/internal/payments; this asserts the MCP
//	   answer carries THAT value and not a second copy — the REST edge and the MCP
//	   edge must not be able to drift apart (they are the same disclosure, and
//	   before this card the drift was inside payments itself).
//	2. THE ANSWER IS THE FULL READ SHAPE, not a stub: every CHIP method is listed
//	   with a rate_label, so the sentence is asserted on the payload an agent
//	   actually receives.
//
// It EXECUTES paymentFeesOut against a real Postgres (testdb.Open gives this test
// process its own scratch schema) instead of scanning the source: a source scan
// cannot show that the field survives JSON marshalling or that the workspace
// resolution still works.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestGetPaymentFeesServesTheEnglishAcknowledgement(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, wsID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('feeackmcp-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).
		Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'feeackmcp-' || md5(random()::text), 'feeackmcp-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'feeackmcp-' || md5(random()::text), 'feeackmcp-' || md5(random()::text), $2)
		 RETURNING id`, userID, wsID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	s := &Server{deps: &Deps{PaymentFees: dbstore.NewPaymentFeeStore(pool)}}
	out, err := s.paymentFeesOut(ctx, accountID)
	if err != nil {
		t.Fatalf("paymentFeesOut: %v", err)
	}

	if out.AcknowledgementText != payments.FeeAcknowledgementText {
		t.Fatalf("get_payment_fees acknowledgement_text = %q\nwant %q — the MCP tool must serve the "+
			"same sentence as the REST edge, or an agent and its merchant read two different "+
			"disclosures", out.AcknowledgementText, payments.FeeAcknowledgementText)
	}
	// The payload is what a client caches; assert the field survives the wire.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire struct {
		AcknowledgementText string `json:"acknowledgement_text"`
		Rates               []struct {
			Method    string `json:"method"`
			RateLabel string `json:"rate_label"`
		} `json:"rates"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire.AcknowledgementText == "" {
		t.Fatalf("the marshalled answer drops acknowledgement_text: %s", raw)
	}
	if len(wire.Rates) != len(payments.FeeMethods) {
		t.Fatalf("the answer lists %d methods, want all %d — the sentence would arrive next to an "+
			"incomplete rate table", len(wire.Rates), len(payments.FeeMethods))
	}
	for _, r := range wire.Rates {
		if r.RateLabel == "" {
			t.Fatalf("method %q carries no rate_label, so the merchant/agent cannot tell what the "+
				"disclosure is about", r.Method)
		}
	}
}
