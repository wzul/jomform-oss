package httpapi

// Issue #597 END-TO-END PROOF — sends ONE REAL email, from inside the real
// public submit request.
//
// Skipped unless JOMFORM_E2E_SEND_TO is set, so `go test ./...` never mails
// anyone. This is the strongest available statement about the fix, because
// NOTHING in the path is stubbed:
//
//	POST /{slug}/order (the real public submit route, real Postgres)
//	  -> handleResponseSubmit -> fireResponseNotifications
//	  -> payments.FireFormNotifications over the rule EnsureDefault created
//	  -> renderNotification (the real template engine)
//	  -> email.Sender.SendReceipt (the real SESv2 client) -> SES
//
// The recipient is the merchant, resolved from the form's account the same way
// production resolves `{admin_email}`.
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/payments"
)

func TestE2EResponseRegistrationSendsRealEmail(t *testing.T) {
	to := os.Getenv("JOMFORM_E2E_SEND_TO")
	if to == "" {
		t.Skip("JOMFORM_E2E_SEND_TO not set; skipping the real-send end-to-end proof")
	}
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, ownerUserID := bundleFixture(t, ctx, pool)

	// The merchant's account email IS the recipient: make it the address under
	// test so `{admin_email}` resolving correctly is what puts the mail there.
	if _, err := pool.Exec(ctx, `UPDATE users SET email=$2 WHERE id=$1`, ownerUserID, to); err != nil {
		t.Fatalf("point the owner at the test recipient: %v", err)
	}

	var workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-e2e-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, ownerUserID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET workspace_id = (SELECT id FROM workspaces WHERE slug=$2) WHERE id=$1`,
		acc, workspaceSlug); err != nil {
		t.Fatalf("link account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	store := dbstore.NewFormNotificationStore(pool)
	formSlug := "it-e2e-" + acc[:8]
	f := &dbstore.Form{
		AccountID: acc, Slug: formSlug, Title: "Event Registration",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
			},
		},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	// The production writer: the same call the REST create route makes.
	if err := store.EnsureDefault(ctx, f); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	rules, _ := store.ListByForm(ctx, f.ID)
	if len(rules) != 1 || rules[0].Event != dbstore.NotifyEventResponseSubmitted {
		blob, _ := json.Marshal(rules)
		t.Fatalf("the response form's default rule is not a response rule: %s", blob)
	}
	t.Logf("default rule created for %s: event=%q subject=%q", formSlug, rules[0].Event, rules[0].Subject)

	// A REAL SES sender — no queue, so the send happens inside the request.
	sender := email.New(email.SMTPConfig{
		Host:     os.Getenv("JOMFORM_E2E_SES_HOST"),
		Port:     os.Getenv("JOMFORM_E2E_SES_PORT"),
		Username: os.Getenv("JOMFORM_E2E_SES_USER"),
		Password: os.Getenv("JOMFORM_E2E_SES_PASS"),
		From:     os.Getenv("JOMFORM_E2E_SES_FROM"),
		Region:   "ap-southeast-1",
	})
	pay := &payments.Service{
		Accounts:      dbstore.NewAccountStore(pool),
		Users:         dbstore.NewUserStore(pool),
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Notifications: store,
		Receipts:      &payments.Receipts{Sender: sender},
	}
	h := &PublicHandlers{
		Forms:       forms,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		Pay:         pay,
		BaseURL:     "http://localhost:8080",
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := fmt.Sprintf(`{"client":{"full_name":"Siti Aminah","email":"%s"},
	                        "fields":{"b1":"Siti Aminah","b2":"%s"}}`, to, to)
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", strings.NewReader(body))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Capture the notification engine's own log for THIS request, so "the email
	// went out" is an observation rather than an inference from a 201. The
	// engine logs `notify_no_recipient` when the recipient resolves to "" (the
	// silent-drop shape this card is about) and `notify_send_failed` when SES
	// refuses the send — and it logs no failure line at all when the rule never
	// matched, which is the pre-fix behaviour on a response form.
	var logBuf strings.Builder
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prevLogger)

	mux.ServeHTTP(rec, req)
	logs := logBuf.String()

	if rec.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	t.Logf("registration accepted: %s", rec.Body.String())
	t.Logf("notification engine log for this request:\n%s", logs)

	if strings.Contains(logs, "notify_no_recipient") {
		t.Errorf("the notification was dropped with no recipient — {admin_email} did not resolve:\n%s", logs)
	}
	if strings.Contains(logs, "notify_send_failed") {
		t.Errorf("the notification was handed to SES and REJECTED:\n%s", logs)
	}
	if strings.Contains(logs, "notify_list_failed") {
		t.Errorf("the rule lookup failed, so no rule could fire:\n%s", logs)
	}
	t.Logf("SES ACCEPTED the notification for the registration of Siti Aminah (no engine failure line)")

	// ── The control that makes the silence above decisive ────────────────────
	// An empty failure log is ALSO what the pre-fix code produces (no rule ever
	// matches, so nothing is logged), so the log alone does not discriminate.
	// Re-run the SAME request with the real sender pointed at a region that
	// cannot resolve, so the send MUST fail: if the rule matched and reached the
	// send step, that is now visible as notify_send_failed. Silence here would
	// mean the rule never fired.
	pay.Receipts = &payments.Receipts{Sender: email.New(email.SMTPConfig{
		Username: os.Getenv("JOMFORM_E2E_SES_USER"),
		Password: os.Getenv("JOMFORM_E2E_SES_PASS"),
		From:     os.Getenv("JOMFORM_E2E_SES_FROM"),
		Region:   "not-a-real-region-jf597",
	})}
	var ctlBuf strings.Builder
	slog.SetDefault(slog.New(slog.NewTextHandler(&ctlBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	ctlBody := fmt.Sprintf(`{"client":{"full_name":"Control Responder","email":"%s"},
	                          "fields":{"b1":"Control Responder","b2":"%s"}}`, to, to)
	ctlReq := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", strings.NewReader(ctlBody))
	ctlReq.Host = workspaceSlug + ".jomform.com"
	ctlReq.Header.Set("Content-Type", "application/json")
	ctlRec := httptest.NewRecorder()
	mux.ServeHTTP(ctlRec, ctlReq)
	ctlLogs := ctlBuf.String()
	slog.SetDefault(prevLogger)
	t.Logf("control (send must fail) engine log:\n%s", ctlLogs)

	if ctlRec.Code != http.StatusCreated {
		t.Errorf("control registration status = %d, want 201", ctlRec.Code)
	}
	if !strings.Contains(ctlLogs, "notify_send_failed") {
		t.Errorf("with a sender that cannot send, the engine logged NO send failure: the response "+
			"rule did not match, so the silent-drop defect is still present in this path.\nlogs:\n%s", ctlLogs)
	}

	// The rule fired, so no sale may exist for a response form.
	var saleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sales WHERE form_id=$1`, f.ID).Scan(&saleCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if saleCount != 0 {
		t.Errorf("response registration produced %d sales, want 0", saleCount)
	}
}
