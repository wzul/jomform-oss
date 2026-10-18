package httpapi

// Card t_0da81d94 — a response form's DEFAULT confirmation body is server Malay,
// so an English-locale page thanks the buyer in BM under an English heading.
//
// Measured on prod while verifying t_41b9c427:
//
//	POST /qa-t41b9c427-legacy-en/order -> 201
//	{"confirmation":"message","message":"Terima kasih! Maklum balas anda telah diterima."}
//
// The heading came from the five-locale dictionary (Ls.ThankYou = "Thank you!"),
// the body from dbstore.DefaultConfirmationMessage — a BM constant written into
// every form's persisted default confirmation (dbstore/form_confirmation.go), so
// the renderer's own localized fallback (`d.message || '{{$.Ls.ResponseReceived}}'`)
// was never reached.
//
// The rule this pins (precedent t_30ffa28a / t_4a542321): a sentence the SERVER
// authors must not reach a buyer-facing page whose language is a dictionary
// lookup. The built-in default is a dictionary lookup, not prose — it is stored
// as "not merchant-authored" (empty) and resolved to Ls.ResponseReceived in the
// page's own language.
//
// These tests drive the REAL handler against a real Postgres: the submit that the
// buyer's page calls (POST /{slug}/order), the page it was served (GET /{slug}),
// and the payment thank-you page (GET /t/{ref}). The negative arm is the point —
// the BM prose must be unreachable, and a merchant-authored message must still be
// shown verbatim (a blanket "always use the dictionary" would break that).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// legacyDefaultConfirmationMessage is the exact BM sentence the shipped server
// wrote into every form's default confirmation. It is spelled out HERE, not read
// from the code under test, so this file fails if that prose is reached again —
// including if it comes back as the value of a constant, a fallback or a literal.
const legacyDefaultConfirmationMessage = "Terima kasih! Maklum balas anda telah diterima."

// legacyDefaultFailureMessage is the same ship for a failed payment.
const legacyDefaultFailureMessage = "Pembayaran gagal. Sila cuba lagi."

// confLangFixture is a workspace + account + form (response or payment mode) over
// the package's scratch schema, wired into a PublicHandlers the way the server
// wires it.
type confLangFixture struct {
	pool          *pgxpool.Pool
	h             *PublicHandlers
	workspaceID   string
	workspaceSlug string
	accountID     string
	formID        string
	formSlug      string
}

func newConfLangFixture(t *testing.T, mode string) *confLangFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('conf-lang-' || md5(random()::text) || '@example.test', 'x')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceID, workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-conf-lang-' || md5(random()::text), 'it-cl-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-conf-lang-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "IT form", Status: "published",
		Config: map[string]any{
			"mode": mode,
			"blocks": []any{
				map[string]any{"id": "b1", "type": "text", "value": "Nama penuh", "required": true},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	return &confLangFixture{
		pool: pool,
		h: &PublicHandlers{
			Forms:       fs,
			Accounts:    dbstore.NewAccountStore(pool),
			Workspaces:  dbstore.NewWorkspaceStore(pool),
			Submissions: dbstore.NewSubmissionStore(pool),
			Sales:       dbstore.NewSaleStore(pool),
			BaseURL:     "http://localhost:8080",
		},
		workspaceID: workspaceID, workspaceSlug: workspaceSlug,
		accountID: accountID, formID: f.ID, formSlug: formSlug,
	}
}

// setWorkspaceLang saves the workspace's default language the way /settings does.
func (fx *confLangFixture) setWorkspaceLang(t *testing.T, lang string) {
	t.Helper()
	sp := dbstore.NewWorkspaceStore(fx.pool)
	st, err := sp.Settings(context.Background(), fx.workspaceID)
	if err != nil {
		t.Fatalf("read workspace settings: %v", err)
	}
	st.DefaultLang = lang
	if err := sp.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("save default_lang=%s: %v", lang, err)
	}
}

func (fx *confLangFixture) serve(t *testing.T, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	fx.h.RegisterRoutes(mux)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Host = fx.workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// submit drives the response-form submit the buyer's page makes and returns the
// decoded JSON the page renders its confirmation from.
func (fx *confLangFixture) submit(t *testing.T) map[string]string {
	t.Helper()
	rec := fx.serve(t, http.MethodPost, "/"+fx.formSlug+"/order",
		`{"client":{"full_name":"Aisyah","email":"aisyah@example.test"},"fields":{"b1":"Aisyah"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode submit response %q: %v", rec.Body.String(), err)
	}
	return out
}

// storedDefaultMessage reads the form's persisted default confirmation message.
func (fx *confLangFixture) storedDefaultMessage(t *testing.T) (string, bool) {
	t.Helper()
	var msg string
	err := fx.pool.QueryRow(context.Background(),
		`SELECT message FROM form_confirmations WHERE form_id=$1 AND is_default`, fx.formID).Scan(&msg)
	if err != nil {
		return "", false
	}
	return msg, true
}

// seedSale inserts a sale with the given status so the thank-you page can be
// driven without a CHIP round-trip.
func (fx *confLangFixture) seedSale(t *testing.T, status string) string {
	t.Helper()
	ref := "JF-CONFLANG-" + strings.ToUpper(fx.accountID[:8])
	if _, err := fx.pool.Exec(context.Background(),
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, total_sen,
		                    sale_type, payment_method, status)
		 VALUES ($1, $2, $3, '{"full_name":"Buyer","email":"buyer@example.test"}'::jsonb,
		         '[]'::jsonb, 1000, 'one_time', 'chip', $4)`,
		fx.accountID, fx.formID, ref, status); err != nil {
		t.Fatalf("seed %s sale: %v", status, err)
	}
	return ref
}

// TestResponseFormDefaultConfirmationFollowsThePageLanguage is the card's
// acceptance, per locale: with NO merchant-authored confirmation, the body the
// submit returns — and the sentence the served page falls back to — is the
// dictionary's ResponseReceived, never the server's BM prose.
func TestResponseFormDefaultConfirmationFollowsThePageLanguage(t *testing.T) {
	for _, lang := range []string{"en", "ms", "zh", "ta", "ar"} {
		t.Run("workspace default "+lang, func(t *testing.T) {
			fx := newConfLangFixture(t, dbstore.FormModeResponse)
			fx.setWorkspaceLang(t, lang)

			want := formLangs[lang].ResponseReceived
			if want == "" {
				t.Fatalf("the %s dictionary carries no ResponseReceived — this test cannot assert anything", lang)
			}

			out := fx.submit(t)
			if out["confirmation"] != "message" {
				t.Fatalf("confirmation = %q, want message (body %v)", out["confirmation"], out)
			}
			if out["message"] != want {
				t.Errorf("submit message = %q, want %q — the body must follow the page's locale",
					out["message"], want)
			}
			if out["message"] == legacyDefaultConfirmationMessage {
				t.Errorf("submit message is the server's BM default %q — that sentence must not be "+
					"reachable on a %s page (its heading is already %q)",
					legacyDefaultConfirmationMessage, lang, formLangs[lang].ThankYou)
			}

			// The persisted default row must not carry server prose either: that
			// is what made the BM sentence reach EVERY response form, and what a
			// later render (or a REST/MCP read) would keep serving.
			stored, ok := fx.storedDefaultMessage(t)
			if !ok {
				t.Fatalf("no default confirmation row was persisted for form %s — the fixture is not "+
					"exercising the default path", fx.formID)
			}
			if stored != "" {
				t.Errorf("the default confirmation persists %q; a built-in default must be stored as "+
					"\"not merchant-authored\" (empty) and resolved per locale, not as one language's prose",
					stored)
			}

			// And the SERVED page: its own fallback must be the localized sentence.
			rec := fx.serve(t, http.MethodGet, "/"+fx.formSlug, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /%s = %d, want 200", fx.formSlug, rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, want) {
				t.Errorf("the served page does not carry the %s fallback %q", lang, want)
			}
			if strings.Contains(body, legacyDefaultConfirmationMessage) {
				t.Errorf("the served page carries the server's BM default %q", legacyDefaultConfirmationMessage)
			}
		})
	}
}

// TestMerchantAuthoredConfirmationIsShownVerbatim is the other direction: a
// message the MERCHANT typed is their words, in their language, and must be
// served byte-for-byte — a fix that always returned the dictionary would break
// every merchant who set one.
func TestMerchantAuthoredConfirmationIsShownVerbatim(t *testing.T) {
	ctx := context.Background()
	fx := newConfLangFixture(t, dbstore.FormModeResponse)
	fx.setWorkspaceLang(t, "en")

	const typed = "Sila ambil nota: resit anda akan dihantar dalam 3 hari bekerja."
	if err := fx.h.Forms.SaveConfirmations(ctx, fx.formID, []*dbstore.Confirmation{{
		ID:        "default",
		Name:      dbstore.DefaultConfirmationName,
		Type:      dbstore.ConfirmationTypeMessage,
		Message:   typed,
		IsDefault: true,
	}}); err != nil {
		t.Fatalf("save merchant confirmation: %v", err)
	}

	out := fx.submit(t)
	if out["message"] != typed {
		t.Errorf("submit message = %q, want the merchant's own words %q verbatim",
			out["message"], typed)
	}
	if out["message"] == formLangs["en"].ResponseReceived {
		t.Error("a merchant-authored message was replaced by the dictionary default")
	}
}

// TestPaymentThankYouBodyFollowsThePageLanguage covers the SAME server default on
// the payment thank-you page (GET /t/{ref}) — the other render site of
// dbstore.DefaultConfirmationMessage / DefaultFailureMessage.
func TestPaymentThankYouBodyFollowsThePageLanguage(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   string
		banned string
	}{
		{"paid", dbstore.SalePaid, "Your response has been received.", legacyDefaultConfirmationMessage},
		{"failed", dbstore.SaleFailed, "We could not complete your payment. Please try again.", legacyDefaultFailureMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newConfLangFixture(t, dbstore.FormModePayment)
			fx.setWorkspaceLang(t, "en")
			ref := fx.seedSale(t, tc.status)

			rec := fx.serve(t, http.MethodGet, "/t/"+ref, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /t/%s = %d, want 200 (body %s)", ref, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("thank-you page for a %s sale does not carry %q (body %s)",
					tc.status, tc.want, body)
			}
			if strings.Contains(body, tc.banned) {
				t.Errorf("thank-you page for a %s sale carries the server's BM default %q",
					tc.status, tc.banned)
			}
		})
	}
}
