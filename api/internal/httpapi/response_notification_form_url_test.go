package httpapi

// The response-submission notification's {{form_url}} may only ever name a host
// that SERVES the form — or it must be omitted.
//
// THE DEFECT (card t_55c7da68, found by QA on prod v1.18.0). fireResponseNotifications
// resolved the form's link through the workspace-only lookup and then fell back
// to `h.BaseURL + "/f/" + form.Slug`. That fallback could only ever 404: the
// apex /f/{slug} lookup was removed on purpose in issue #77 — measured on prod,
// `GET https://jomform.com/f/{slug}` answers 404 "Borang tidak dijumpai" — and
// the fallback fired exactly for the accounts the workspace lookup cannot answer
// for, which are the ones whose forms are served on a CUSTOM DOMAIN instead. A
// merchant who put a response notification on {{form_url}} therefore received a
// link that could never resolve.
//
// WHAT THESE TESTS DRIVE.
//
//  1. TestResponseNotificationFormURLNamesAServedHost goes through the real
//     public submit route on a real custom-domain host, the real notification
//     engine and the real template renderer, and reads the rendered email back
//     out of the queue: every URL in the delivered notification must resolve on
//     the public serving path, and none may be the removed apex /f/{slug} route.
//  2. TestResponseNotificationOmitsTheLinkWhenNoHostServes drives the same
//     notification entry point for the shape a REQUEST cannot produce (no
//     workspace AND no custom domain — nothing serves the form) and reads the
//     rendered email back out of the queue: the substitution must be OMITTED.
//     This is the leg that bites on a restored apex fallback even when a
//     resolvable host exists, since no fallback is reached in leg 1.
//  3. TestFormURLForAccountNeverFallsBackToTheApexRoute pins the resolver's own
//     answers: the two hosts it resolves (verified custom domain, then workspace
//     subdomain) and that it never answers the apex route.
//
// THE BITE. Restore `formURL = h.BaseURL + "/f/" + form.Slug` in
// fireResponseNotifications and legs 1 and 2 go RED: the delivered notification
// names http://localhost:8080/f/<slug> and no longer names the host that served
// the submission.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.
import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
)

// urlInText finds the links in a rendered notification body.
var urlInText = regexp.MustCompile(`https?://[^\s"'<>]+`)

// TestResponseNotificationFormURLNamesAServedHost is the end-to-end guard: the
// link a response notification actually mails must be a host that serves the
// form.
func TestResponseNotificationFormURLNamesAServedHost(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping response notification form-url test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	q, rdb := respNotifQueue(t) // skips when JOMFORM_TEST_VALKEY_URL is unset

	// The shape that reaches the defect: an account with NO workspace (a signup
	// creates an account and no workspace, #718), whose forms are served on a
	// verified custom domain instead. Asserted, not assumed — if the fixture
	// ever links a workspace, this leg would be measuring the wrong branch.
	var workspaceID *string
	if err := pool.QueryRow(ctx,
		`SELECT workspace_id::text FROM accounts WHERE id=$1`, acc).Scan(&workspaceID); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if workspaceID != nil {
		t.Fatalf("fixture precondition: account %s is linked to workspace %s — this leg needs the "+
			"workspace-less shape the apex fallback fired for", acc, *workspaceID)
	}
	domain := "it-notif-" + acc[:8] + ".example.test"
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=$2, custom_domain_verified=true WHERE id=$1`, acc, domain); err != nil {
		t.Fatalf("claim custom domain: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	store := dbstore.NewFormNotificationStore(pool)
	formSlug := "it-respurl-" + acc[:8]
	f := &dbstore.Form{
		AccountID: acc, Slug: formSlug, Title: "Contact",
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
		t.Fatalf("create response form: %v", err)
	}
	// The production writer of the default rule (issue #597).
	if err := store.EnsureDefault(ctx, f); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	rules, err := store.ListByForm(ctx, f.ID)
	if err != nil || len(rules) != 1 {
		t.Fatalf("list rules = %d/%v, want exactly 1 response rule", len(rules), err)
	}
	if rules[0].Event != dbstore.NotifyEventResponseSubmitted || !rules[0].Enabled {
		t.Fatalf("default rule = event %q enabled %v, want an enabled response.submitted rule",
			rules[0].Event, rules[0].Enabled)
	}

	pay := &payments.Service{
		Accounts:      dbstore.NewAccountStore(pool),
		Users:         dbstore.NewUserStore(pool),
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Notifications: store,
		EmailQueue:    q,
	}
	ph := &PublicHandlers{
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
	ph.RegisterRoutes(mux)

	submitBody := `{"client":{"full_name":"Siti Aminah","email":"siti@example.test"},
	                "fields":{"b1":"Siti Aminah","b2":"siti@example.test"}}`
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", strings.NewReader(submitBody))
	req.Host = domain
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit on the custom-domain host %s status = %d, want 201 (body %s)",
			domain, rec.Code, rec.Body.String())
	}

	jobs := queuedFormNotifications(t, q, rdb)
	var sent []emailqueue.Job
	for _, j := range jobs {
		if j.Type == "form_notification" {
			sent = append(sent, j)
		}
	}
	if len(sent) != 1 {
		t.Fatalf("a response submission enqueued %d form_notification jobs, want exactly 1 "+
			"(all jobs: %+v)", len(sent), jobs)
	}
	body := sent[0].Body
	t.Logf("notification body as delivered:\n%s", body)

	// THE ASSERTION THIS CARD EXISTS FOR: every link the merchant is handed
	// must resolve on the public serving path.
	links := urlInText.FindAllString(body, -1)
	if len(links) == 0 {
		t.Fatalf("the delivered notification carries no form link at all:\n%s", body)
	}
	wantURL := "https://" + domain + "/" + formSlug
	foundServed := false
	for _, raw := range links {
		u, perr := url.Parse(strings.TrimRight(raw, ".,)"))
		if perr != nil || u.Host == "" {
			t.Errorf("the delivered notification carries an unparseable link %q", raw)
			continue
		}
		slug := strings.TrimPrefix(u.Path, "/")
		probe := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
		probe.Host = u.Host
		served := ph.findPublicForm(ctx, probe, slug)
		if served == nil || served.ID != f.ID {
			t.Errorf("the delivered notification links %s, which does not resolve to this form on the "+
				"public serving path (host %q, slug %q) — a dead link in the merchant's inbox\nbody:\n%s",
				raw, u.Host, slug, body)
			continue
		}
		if raw == wantURL {
			foundServed = true
		}
	}
	if !foundServed {
		t.Errorf("the notification does not name the host that served the submission.\n"+
			"want a link %q\nbody:\n%s", wantURL, body)
	}
	// The removed apex route, named explicitly: it is the route #77 deleted, so
	// a link to it is dead by construction whatever the host looks like.
	if strings.Contains(body, "/f/"+formSlug) {
		t.Errorf("the delivered notification still carries the removed apex /f/{slug} route:\n%s", body)
	}
}

// TestResponseNotificationOmitsTheLinkWhenNoHostServes drives the notification
// entry point directly for the shape the SUBMIT route cannot produce: an account
// with no workspace and no verified custom domain, where no host serves the
// form at all. Nothing may be substituted for {{form_url}} there — the removed
// apex /f/{slug} route least of all.
//
// This is the leg that bites on a restored apex fallback even when a resolvable
// host exists (the reachable custom-domain case above would still be green under
// such a fallback, because no fallback is reached there). fireResponseNotifications
// is the same code the public submit route calls; only the routing is skipped.
func TestResponseNotificationOmitsTheLinkWhenNoHostServes(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping response notification omission test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	q, rdb := respNotifQueue(t)

	// Precondition, asserted: this account has NO serving host — no workspace
	// (the lookup is workspace-only) and no custom domain.
	var workspaceID *string
	if err := pool.QueryRow(ctx,
		`SELECT workspace_id::text FROM accounts WHERE id=$1`, acc).Scan(&workspaceID); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	var domain string
	var verified bool
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(custom_domain,''), custom_domain_verified FROM accounts WHERE id=$1`,
		acc).Scan(&domain, &verified); err != nil {
		t.Fatalf("read custom domain: %v", err)
	}
	if workspaceID != nil || (domain != "" && verified) {
		t.Fatalf("fixture precondition: account %s has a serving host (workspace %v, domain %q verified %v)",
			acc, workspaceID, domain, verified)
	}

	forms := dbstore.NewFormStore(pool)
	store := dbstore.NewFormNotificationStore(pool)
	formSlug := "it-unserved-" + acc[:8]
	f := &dbstore.Form{
		AccountID: acc, Slug: formSlug, Title: "Unserved",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
		}},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := store.EnsureDefault(ctx, f); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	ph := &PublicHandlers{
		Forms:       forms,
		Accounts:    dbstore.NewAccountStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		Pay: &payments.Service{
			Accounts:      dbstore.NewAccountStore(pool),
			Users:         dbstore.NewUserStore(pool),
			Workspaces:    dbstore.NewWorkspaceStore(pool),
			Notifications: store,
			EmailQueue:    q,
		},
		BaseURL: "http://localhost:8080",
	}
	ph.fireResponseNotifications(ctx, f, map[string]any{"b1": "Siti Aminah"})

	var sent []emailqueue.Job
	for _, j := range queuedFormNotifications(t, q, rdb) {
		if j.Type == "form_notification" {
			sent = append(sent, j)
		}
	}
	if len(sent) != 1 {
		t.Fatalf("the response notification enqueued %d jobs, want exactly 1 — the rule exists and "+
			"must fire whether or not a link can be built", len(sent))
	}
	body := sent[0].Body
	t.Logf("notification body as delivered:\n%s", body)

	// The substitution is OMITTED, not replaced by a URL that cannot serve:
	// the pre-fix code put h.BaseURL + "/f/" + slug here, and the public server
	// answers that route with 404 (issue #77).
	if links := urlInText.FindAllString(body, -1); len(links) != 0 {
		t.Errorf("no host serves this form, so {{form_url}} must render EMPTY; the notification "+
			"instead carries %v — a dead link in the merchant's inbox\nbody:\n%s", links, body)
	}
	if strings.Contains(body, "/f/") {
		t.Errorf("the notification carries the removed apex /f/{slug} route:\n%s", body)
	}
}

// TestFormURLForAccountNeverFallsBackToTheApexRoute pins the resolver itself,
// including the shape the submit route cannot produce: an account with no
// workspace AND no verified custom domain, where no host serves the form.
func TestFormURLForAccountNeverFallsBackToTheApexRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping form-url resolver test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, ownerUserID := bundleFixture(t, ctx, pool)
	ph := &PublicHandlers{
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	const slug = "it-unserved-form"

	// (a) NOTHING serves this account's forms: no workspace subdomain (the
	// lookup is workspace-only) and no verified custom domain. The only honest
	// answer is "" — an empty {{form_url}}. The pre-fix code answered
	// http://localhost:8080/f/it-unserved-form, which the public server 404s
	// (issue #77 removed the apex lookup).
	if got := ph.formURLForAccount(ctx, acc, slug); got != "" {
		t.Errorf("no host serves this account's forms, so the resolver must answer \"\" (an omitted "+
			"{{form_url}}); got %q — a link the public server cannot serve is exactly the dead link "+
			"this card is about", got)
	}
	// The apex shape of the old fallback, asserted directly so the two answers
	// cannot be confused when the code is read.
	if got := ph.formURLForAccount(ctx, acc, slug); strings.Contains(got, "/f/") {
		t.Errorf("resolver answered the removed apex /f/ route: %q", got)
	}

	// (b) an ask-first workspace (WorkspaceStore.CreateForAccount): the
	// workspace subdomain is the host the serving path resolves.
	wsSlug := "it-notifws-" + acc[:8]
	if _, err := dbstore.NewWorkspaceStore(pool).CreateForAccount(ctx, ownerUserID, "it-notifws", wsSlug, acc); err != nil {
		t.Fatalf("ask-first workspace create: %v", err)
	}
	if got, want := ph.formURLForAccount(ctx, acc, slug), "https://"+wsSlug+".jomform.com/"+slug; got != want {
		t.Errorf("resolver = %q, want %q (the workspace subdomain the public server serves)", got, want)
	}

	// (c) a VERIFIED custom domain outranks the workspace subdomain — the rule
	// every other buyer-facing URL builder applies (payments/formurl.go,
	// mcpsrv/forms.go, rest_handlers.go, issue #83).
	domain := "it-notifdom-" + acc[:8] + ".example.test"
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=$2, custom_domain_verified=true WHERE id=$1`, acc, domain); err != nil {
		t.Fatalf("claim custom domain: %v", err)
	}
	if got, want := ph.formURLForAccount(ctx, acc, slug), "https://"+domain+"/"+slug; got != want {
		t.Errorf("resolver = %q, want %q (the verified custom domain)", got, want)
	}

	// (d) an UNVERIFIED custom domain is not used: the claim is not ours to
	// point a merchant's customers at until it is verified, which is the rule
	// the other builders share.
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET custom_domain_verified=false WHERE id=$1`, acc); err != nil {
		t.Fatalf("unverify custom domain: %v", err)
	}
	if got, want := ph.formURLForAccount(ctx, acc, slug), "https://"+wsSlug+".jomform.com/"+slug; got != want {
		t.Errorf("resolver used an UNVERIFIED custom domain: got %q, want %q", got, want)
	}
}
