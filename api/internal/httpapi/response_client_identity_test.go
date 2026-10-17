package httpapi

// Card t_98d67234 — a response form's derived submitter identity.
//
// THE DEFECT. responseTmpl mapped the client identity by POSITION and input TYPE:
//
//     var nm=form.querySelector('input[type=text]'); if(nm) body.client.full_name=nm.value;
//
// `input[type=text]` was only ever a proxy for "the name field", and it stopped
// being one as soon as a `text` block rendered a real text input (card
// t_99c3a08b). On a form whose blocks are ordered [text, …, name] — the shape
// measured on live prod — the FIRST input[type=text] is the text/label field, so
// client.full_name carried the LABEL field's answer into the stored
// data["full_name"] (prod: {"nm1":"QANAME-…","txt1":"QATXT-…","full_name":
// "QATXT-…"}), and from there into the submissions Buyer column
// (rest_handlers.go) and the CSV export (rest_export.go). The payment renderer
// never had this bug: its name arm emits name="full_name" (blockTmpl) and its
// script reads [name=full_name] — by identity, not by position.
//
// WHAT THIS FILE DRIVES. The page is rendered by the handler that serves it
// (renderResponseForm), its OWN inline submit script is executed in the shared
// goja + cascadia DOM harness (menu_dom_test.go) against that same served
// markup, an answer is typed into each block, the form is submitted, and the
// JSON body the PAGE built is posted through the real public submit route
// (mux → handleResponseSubmit). The stored submission is then read back with the
// submission store — so the assertion is about data["full_name"] for a responder
// who filled the form the way a person does, not about a hand-written body.
//
// THE BITE. Restoring the pre-fix lookup reds
// TestResponseFormClientFullNameComesFromTheNameBlock and
// TestResponseFormIdentityReachesTheStoredSubmission: full_name becomes the
// text block's answer instead of the name block's. See the mutation table in the
// PR body.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// identityBlockConfig is the ORDER the card measured on prod: the text block
// FIRST, then static content, then the name block, then email and phone. The
// text block's `value` is its LABEL (that is what a `text` block means), so the
// two answers a responder types are deliberately different strings.
func identityBlockConfig() map[string]any {
	return map[string]any{
		"mode": "response",
		"blocks": []any{
			map[string]any{"id": "txt1", "type": "text", "value": "Cuba. Nama"},
			map[string]any{"id": "h1", "type": "html", "value": "<b>Intro</b>"},
			map[string]any{"id": "nm1", "type": "name", "value": "Nama Penuh"},
			map[string]any{"id": "em1", "type": "email", "value": "Emel"},
			map[string]any{"id": "ph1", "type": "phone", "value": "Telefon"},
		},
		"products": []any{},
	}
}

// The answers QA's live repro used, so a failure here is directly comparable
// with the stored prod submission on the card.
const (
	identityTextAnswer  = "QATXT-6f21c8"
	identityNameAnswer  = "QANAME-6f21c8"
	identityEmailAnswer = "qaname@example.test"
	identityPhoneAnswer = "60123456789"
)

// responseSubmitScriptMarker identifies the response page's OWN submit script
// among the inline scripts on the served document: the conditional-logic script
// and the iframe-height script sit on the same page and query the DOM too.
const responseSubmitScriptMarker = "body.client.full_name"

// servedSubmitBody renders a response page through the handler that serves it,
// runs its own inline submit script in the shared goja + cascadia DOM harness,
// types one answer per block id, submits, and returns the JSON body the PAGE
// posted. An id with no control on the page is fatal — a test that typed into
// nothing would otherwise assert on an empty answer.
func servedSubmitBody(t *testing.T, slug string, cfg map[string]any, answers map[string]string) string {
	t.Helper()
	body := renderResponsePage(t, slug, cfg)
	d := newPageDOM(t, body)
	// The page's script replaces the form with the confirmation panel on
	// success; answer it with the success shape so a failure alert is never
	// mistaken for the page being broken.
	d.orderResponse = map[string]any{"status": "submitted", "message": "ok"}
	d.runPageScript(body, responseSubmitScriptMarker)

	ids := make([]string, 0, len(answers))
	for id := range answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		typeIntoBlock(t, d, id, answers[id])
	}

	d.dispatch("submit", d.byID["of"])
	posted := d.storage["__posted_body"]
	if strings.TrimSpace(posted) == "" {
		t.Fatalf("the served response page's own script posted no body (requests: %v)", d.fetches)
	}
	return posted
}

// typeIntoBlock types an answer into the control the block id names, the way a
// responder does. The harness models an element's `value` off its attribute.
func typeIntoBlock(t *testing.T, d *menuDOM, blockID, val string) {
	t.Helper()
	n := d.find(func(n *html.Node) bool {
		if n.Type != html.ElementNode || (n.Data != "input" && n.Data != "textarea") {
			return false
		}
		return menuAttr(n, "name") == blockID
	})
	if n == nil {
		t.Fatalf("the served page has no control named %q — the block did not render", blockID)
	}
	setMenuAttr(n, "value", val)
}

type servedSubmitBodyShape struct {
	Client struct {
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
	} `json:"client"`
	Fields map[string]string `json:"fields"`
}

func decodeServedSubmitBody(t *testing.T, posted string) servedSubmitBodyShape {
	t.Helper()
	var got servedSubmitBodyShape
	if err := json.Unmarshal([]byte(posted), &got); err != nil {
		t.Fatalf("the served page posted an unparsable body: %v (%s)", err, posted)
	}
	return got
}

// Acceptance #1: on a form whose FIRST input[type=text] is the text block, the
// derived client identity must still be the NAME block's answer.
func TestResponseFormClientFullNameComesFromTheNameBlock(t *testing.T) {
	posted := servedSubmitBody(t, "identity", identityBlockConfig(), map[string]string{
		"txt1": identityTextAnswer,
		"nm1":  identityNameAnswer,
		"em1":  identityEmailAnswer,
		"ph1":  identityPhoneAnswer,
	})
	got := decodeServedSubmitBody(t, posted)

	if got.Client.FullName != identityNameAnswer {
		t.Errorf("client.full_name = %q, want %q (the name block's answer). The first input[type=text] "+
			"on this form is the text block, whose answer is %q — a label field is not the responder's name "+
			"(body: %s)", got.Client.FullName, identityNameAnswer, identityTextAnswer, posted)
	}
	if got.Client.Email != identityEmailAnswer {
		t.Errorf("client.email = %q, want %q (the email block's answer)", got.Client.Email, identityEmailAnswer)
	}
	if got.Client.Phone != identityPhoneAnswer {
		t.Errorf("client.phone = %q, want %q (the phone block's answer)", got.Client.Phone, identityPhoneAnswer)
	}
	// The fix changes the DERIVED identity, never the collected answers.
	if got.Fields["txt1"] != identityTextAnswer {
		t.Errorf("fields.txt1 = %q, want %q — the text block's own answer must be untouched",
			got.Fields["txt1"], identityTextAnswer)
	}
	if got.Fields["nm1"] != identityNameAnswer {
		t.Errorf("fields.nm1 = %q, want %q — the name block's own answer must be untouched",
			got.Fields["nm1"], identityNameAnswer)
	}
}

// The identity must hang off the BLOCK, not off a position or an input type —
// which is what makes it immune to the next arm that renders a text-ish control.
func TestResponseFormIdentityFollowsTheBlockNotThePosition(t *testing.T) {
	body := renderResponsePage(t, "identity-markup", identityBlockConfig())

	for _, want := range []string{
		`<input type="text" name="nm1" data-jf-client="full_name">`,
		`<input type="email" name="em1" data-jf-client="email">`,
		`<input type="tel" name="ph1" data-jf-client="phone">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the served page is missing the identity marker %q", want)
		}
	}
	// The text block renders input[type=text] too: it must NOT claim an
	// identity, and exactly one control answers for each client field.
	in := firstInputNamed(body, "txt1")
	if in == "" {
		t.Fatalf("the text block rendered no input named txt1 (page: %s)", body)
	}
	if strings.Contains(in, "data-jf-client") {
		t.Errorf("the text block's input claims a client identity: %s", in)
	}
	for _, marker := range []string{
		`data-jf-client="full_name"`, `data-jf-client="email"`, `data-jf-client="phone"`,
	} {
		if n := strings.Count(body, marker); n != 1 {
			t.Errorf("%s appears %d times on the page, want exactly 1", marker, n)
		}
	}
	// And the served script maps through that marker, keeping the old lookup
	// only as the fallback for a form with no name/email/phone block at all.
	for _, want := range []string{
		"form.querySelector('[data-jf-client=full_name]')||form.querySelector('input[type=text]')",
		"form.querySelector('[data-jf-client=email]')||form.querySelector('input[type=email]')",
		"form.querySelector('[data-jf-client=phone]')||form.querySelector('input[type=tel]')",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the served script does not map by identity: missing %q", want)
		}
	}
}

// The card's item 2: a form with NO identity block keeps today's mapping — the
// type-based lookup, which is what full_name held before this fix. Deleting the
// fallback instead of demoting it would red this: the email block is the FIRST
// named control of the second case, so a "first [name]" mapping answers with the
// email address.
func TestResponseFormWithoutAnIdentityBlockKeepsTheOldMapping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blocks  []any
		answers map[string]string
	}{
		{"only a text block", []any{
			map[string]any{"id": "txt1", "type": "text", "value": "Cuba. Nama"},
		}, map[string]string{"txt1": identityTextAnswer}},
		{"email block BEFORE the text block", []any{
			map[string]any{"id": "em1", "type": "email", "value": "Emel"},
			map[string]any{"id": "txt1", "type": "text", "value": "Cuba. Nama"},
		}, map[string]string{"em1": identityEmailAnswer, "txt1": identityTextAnswer}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"mode": "response", "blocks": tc.blocks, "products": []any{}}
			posted := servedSubmitBody(t, "no-identity", cfg, tc.answers)
			got := decodeServedSubmitBody(t, posted)
			if got.Client.FullName != identityTextAnswer {
				t.Errorf("client.full_name = %q, want the text block's answer %q — a form with no name "+
					"block must map exactly as it did before this card (body: %s)",
					got.Client.FullName, identityTextAnswer, posted)
			}
			if _, hasEmail := tc.answers["em1"]; hasEmail && got.Client.Email != identityEmailAnswer {
				t.Errorf("client.email = %q, want %q — the email block still answers for email",
					got.Client.Email, identityEmailAnswer)
			}
		})
	}
}

// Acceptance #3: the identity the PAGE derived is what the public submit route
// stores — the Buyer column (rest_handlers.go) and the CSV export
// (rest_export.go) both read data["full_name"]. Real Postgres; skipped without
// JOMFORM_TEST_DATABASE_URL.
func TestResponseFormIdentityReachesTheStoredSubmission(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-identity-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Cuba. Nama",
		Status: dbstore.FormStatusPublished, Config: identityBlockConfig(),
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// The page the responder gets, and the body ITS OWN script builds from the
	// answers typed into it.
	posted := servedSubmitBody(t, formSlug, identityBlockConfig(), map[string]string{
		"txt1": identityTextAnswer,
		"nm1":  identityNameAnswer,
		"em1":  identityEmailAnswer,
		"ph1":  identityPhoneAnswer,
	})
	// The page also carries the anti-spam render timestamp it collected, and
	// the route refuses a submit faster than antiSpamMinRenderTime after that
	// stamp — so honor it rather than hand-editing the page's own body.
	waitForRenderStamp(t, posted)

	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", bytes.NewBufferString(posted))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s; posted %s)", rec.Code, rec.Body.String(), posted)
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("submit response = %+v, want status submitted + an id", resp)
	}

	sub, err := h.Submissions.ByID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if got := sub.Data["full_name"]; got != identityNameAnswer {
		t.Errorf("stored data[full_name] = %v, want %q (the name block's answer). This is the value the "+
			"submissions Buyer column and the CSV export show; the text block's answer is %q",
			got, identityNameAnswer, identityTextAnswer)
	}
	if got := sub.Data["txt1"]; got != identityTextAnswer {
		t.Errorf("stored data[txt1] = %v, want %q", got, identityTextAnswer)
	}
	if got := sub.Data["nm1"]; got != identityNameAnswer {
		t.Errorf("stored data[nm1] = %v, want %q", got, identityNameAnswer)
	}
}

// waitForRenderStamp honors the public route's time-based anti-spam check: the
// page's own jf_ts field is younger than antiSpamMinRenderTime immediately after
// render, and a submit inside that window is refused for a reason that has
// nothing to do with this card.
func waitForRenderStamp(t *testing.T, posted string) {
	t.Helper()
	raw := strings.TrimSpace(decodeServedSubmitBody(t, posted).Fields["jf_ts"])
	if raw == "" {
		return
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("the page's render stamp %q is not a unix time: %v", raw, err)
	}
	if wait := time.Until(time.Unix(ts, 0).Add(antiSpamMinRenderTime).Add(250 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
}
