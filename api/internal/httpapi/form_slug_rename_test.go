package httpapi

// Regression guard for the carded defect: A FORM'S URL SLUG CAN NEVER BE
// RENAMED — the console's slug field and upsert_form silently dropped it.
//
// THE BUG, end to end. The form-settings page has an editable slug field and its
// save handler POSTs the value:
//
//	const payload = { slug: slug.toLowerCase()..., title, theme };
//	fetch(`/api/rest/forms/${id}`, { method: "PATCH", body: JSON.stringify(payload) })
//
// and the endpoint that received it declared only Title/Config/Theme — no Slug
// member — so the value was dropped. `grep -rn "SET slug"` over dbstore returned
// nothing: no statement anywhere updated the column, and FormStore.Update could
// not carry one. The save returned {status:"updated"} and the page showed
// "Saved ✓", so a merchant concluded the rename worked and that his new URL
// existed. It 404'd. A form created from a template was stuck on a template slug
// (every "Untitled template" form serving at /untitled-template, …) forever.
//
// WHAT THIS FILE PINS (each assertion is the card's own acceptance criterion):
//
//  1. A rename through PATCH /v1/rest/forms/{id} changes forms.slug AND the
//     form's public URL answers at the new slug. Asserted against the DATABASE
//     and against the real public serving path, never against the PATCH
//     response — the defect was a response that lied.
//  2. A collision is 409 and the FIRST form is untouched (two forms, one slug).
//  3. An OMITTED slug leaves the stored value alone (the COALESCE-style contract
//     config/theme already have, issue #16) — so the documented PATCH stays a
//     partial update and a title-only save cannot blank the URL.
//  4. The old URL is handled DELIBERATELY: a temporary 301 to the new slug, not
//     404-with-no-trace (Wan's rule: "301 sementara").
//  5. Wan's SUPERSEDE rule holds: when another form later claims the old slug,
//     the old URL resolves to THAT live form — the claimant wins, and rename
//     history can never shadow a real URL.
//  6. An expired redirect is a plain 404, so the 301 really is temporary.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
// Real DB + real mux on purpose: a fake store would let the guard pass while the
// column write was absent, which is precisely the failure being guarded.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// renameFixture seeds a user + workspace + account (linked, so the workspace
// slug is the public host) + one published form, and returns everything the
// assertions need.
type renameFixture struct {
	h             *RESTHandlers
	pub           *PublicHandlers
	pool          *pgxpool.Pool
	ctx           context.Context
	accountID     string
	accountSlug   string
	workspaceSlug string
	host          string
	// customDomain is a verified custom domain owned by the same account, so
	// the /f/{slug} custom-domain branch of handleRender is reachable too.
	customDomain string
	formID       string
	slug         string
	key          string
}

func newRenameFixture(t *testing.T, slug string) renameFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID, accountSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-slugrename-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-ren-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id, slug`, userID, workspaceSlug).Scan(&accountID, &accountSlug); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Western menu", Status: dbstore.FormStatusPublished,
		// Card t_bfdcafbc: a payment form whose blocks carry nothing a buyer can
		// add to the cart is served the submit-less "not accepting orders" page,
		// so a fixture that asserts the BLOCK page (its content, theme, footer or
		// chrome) must carry one buyable block.
		Config: map[string]any{"blocks": []any{
			map[string]any{"type": "text", "value": "Hello"},
			map[string]any{"id": "it_prod", "type": "product", "product_id": "it_prod_marker", "value": "Marker item", "price_sen": 1000},
		}},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// A verified custom domain for the same account, so the custom-domain
	// branch of handleRender (the /f/{slug} route) is reachable and its 301 is
	// asserted like the subdomain one.
	domains := dbstore.NewDomainStore(pool)
	customDomain := "it-" + accountSlug + ".example.test"
	if _, err := domains.AddDomain(ctx, accountID, customDomain, "cname"); err != nil {
		t.Fatalf("add custom domain: %v", err)
	}
	if err := domains.MarkVerified(ctx, accountID, customDomain); err != nil {
		t.Fatalf("verify custom domain: %v", err)
	}

	return renameFixture{
		h:             newBundleHandlers(pool),
		pub:           &PublicHandlers{Forms: fs, Accounts: dbstore.NewAccountStore(pool), Domains: domains, Workspaces: dbstore.NewWorkspaceStore(pool)},
		pool:          pool,
		ctx:           ctx,
		accountID:     accountID,
		accountSlug:   accountSlug,
		workspaceSlug: workspaceSlug,
		host:          workspaceSlug + ".jomform.com",
		customDomain:  customDomain,
		formID:        f.ID,
		slug:          f.Slug,
		key:           bundleAPIKey(t, ctx, pool, accountID),
	}
}

// storedSlug reads the slug straight out of the database. Every assertion about
// a rename goes through here (or through the public serving path) rather than
// through the PATCH response, because a lying response was the defect.
func (fx renameFixture) storedSlug(t *testing.T) string {
	t.Helper()
	var slug string
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT slug FROM forms WHERE id=$1 AND account_id=$2`, fx.formID, fx.accountID).Scan(&slug); err != nil {
		t.Fatalf("read stored slug: %v", err)
	}
	return slug
}

// patchSlug drives the real REST route with a JSON body.
func (fx renameFixture) patch(t *testing.T, body string) (int, json.RawMessage) {
	t.Helper()
	return tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+fx.formID, body)
}

// TestFormSlugRenameChangesTheColumnAndThePublicURL is the core guard: the
// rename must land in forms.slug and the form must ANSWER on its new URL.
func TestFormSlugRenameChangesTheColumnAndThePublicURL(t *testing.T) {
	fx := newRenameFixture(t, "it-old-menu-"+randHex(4))
	newSlug := "it-new-menu-" + randHex(4)

	code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH with a slug = %d, want 200 (body %s)", code, raw)
	}

	// 1. The column actually moved. A dropped write (the defect: no `SET slug`
	//    anywhere) leaves this at the old value and the guard goes red.
	if got := fx.storedSlug(t); got != newSlug {
		t.Fatalf("forms.slug = %q after a rename, want %q.\n"+
			"That is the carded defect: the slug field accepted the value and the write dropped it, "+
			"so the merchant's new URL never existed.", got, newSlug)
	}

	// 2. The public serving path answers on the new URL — read out of the real
	//    renderer, not inferred from the payload.
	rec := publicServe(t, fx.pub, fx.host, "/"+newSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s after the rename = %d, want 200 (body %s)", newSlug, rec.Code, rec.Body.String())
	}
	// ...and the served page is really this form, not some other resolution.
	if body := rec.Body.String(); !strings.Contains(body, "Hello") {
		t.Errorf("GET /%s served a page without the form's content — the URL resolved to the wrong form (body %s)", newSlug, body)
	}

	// 3. The response the console reads must report the slug it now owns, so a
	//    client can show the moved URL without a second round trip.
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode PATCH response: %v (%s)", err, raw)
	}
	if out["status"] != "updated" {
		t.Errorf("PATCH status = %v, want \"updated\"", out["status"])
	}
	if got, _ := out["slug"].(string); got != newSlug {
		t.Errorf("PATCH response slug = %q, want %q — the response must not describe a URL the form does not have", got, newSlug)
	}

	// 4. The API's own view of the form (the payload the console hydrates from)
	//    carries the new slug too, so a reload shows the URL it actually serves.
	_, detail := tplReq(t, fx.h, fx.key, http.MethodGet, "/v1/rest/forms/"+fx.formID, "")
	var detailBody struct {
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(detail, &detailBody); err != nil {
		t.Fatalf("decode form GET: %v (%s)", err, detail)
	}
	if detailBody.Slug != newSlug {
		t.Errorf("GET form slug = %q, want %q", detailBody.Slug, newSlug)
	}
	if !strings.HasSuffix(detailBody.URL, "/"+newSlug) {
		t.Errorf("GET form url = %q, want it to end in /%s — the merchant-facing URL must follow the slug", detailBody.URL, newSlug)
	}
}

// TestFormSlugRenameOmittedSlugKeepsTheStoredValue pins the partial-update
// contract: the console saves title/theme on every visit, and a save that omits
// the slug must not blank it (the same "leave unchanged" rule config/theme have
// had since issue #16).
func TestFormSlugRenameOmittedSlugKeepsTheStoredValue(t *testing.T) {
	fx := newRenameFixture(t, "it-keep-"+randHex(4))

	code, raw := fx.patch(t, `{"title":"Renamed title only"}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH without a slug = %d, want 200 (body %s)", code, raw)
	}
	if got := fx.storedSlug(t); got != fx.slug {
		t.Fatalf("forms.slug = %q after a PATCH that omitted the slug, want the stored %q.\n"+
			"An omitted field must mean \"leave unchanged\", or every unrelated save blanks the form's URL.", got, fx.slug)
	}
	// A PATCH that echoes the slug the form already has is a no-op, not a
	// self-redirect loop — and it must still resolve.
	if code, raw := fx.patch(t, `{"slug":"`+fx.slug+`","title":"same slug"}`); code != http.StatusOK {
		t.Fatalf("PATCH with the form's own slug = %d, want 200 (body %s)", code, raw)
	}
	rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s after a same-slug PATCH = %d, want 200", fx.slug, rec.Code)
	}
}

// TestFormSlugRenameCollisionIs409AndLeavesTheFirstFormAlone pins the uniqueness
// requirement: two forms in one workspace must not share a slug, and the refused
// rename must not touch either form.
func TestFormSlugRenameCollisionIs409AndLeavesTheFirstFormAlone(t *testing.T) {
	fx := newRenameFixture(t, "it-first-"+randHex(4))

	// A second form in the SAME account claiming the slug we will try to take.
	taken := "it-taken-" + randHex(4)
	other := &dbstore.Form{
		AccountID: fx.accountID, Slug: taken, Title: "Other form",
		Status: dbstore.FormStatusPublished, Config: map[string]any{"blocks": []any{}}, Theme: map[string]any{},
	}
	if err := dbstore.NewFormStore(fx.pool).Create(fx.ctx, other); err != nil {
		t.Fatalf("create colliding form: %v", err)
	}

	code, raw := fx.patch(t, `{"slug":"`+taken+`","title":"Western menu"}`)
	if code != http.StatusConflict {
		t.Fatalf("PATCH onto a slug another live form serves = %d, want 409 (body %s).\n"+
			"A collision must be refused loudly: the defect being fixed was a write that reported "+
			"success while changing nothing.", code, raw)
	}
	// The refusal carries the machine code the console translates. Without it the
	// merchant is shown the Malay prose, because the API never receives the
	// dashboard's language (issue #6177) — so this is part of the reason being
	// READABLE, which is the half of the card the human actually sees.
	//
	// The code comes from errcode.go's SHARED table via plain writeErr; this PR
	// carries no local error-code mechanism of its own.
	var conflictBody struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(raw, &conflictBody); err != nil {
		t.Fatalf("decode the collision envelope: %v (%s)", err, raw)
	}
	if conflictBody.Code != CodeFormSlugInUse {
		t.Errorf("collision envelope code = %q, want %q — the console keys its localized "+
			"message on this code, so without it the banner falls back to a generic \"Failed\" "+
			"(or, if it prefers the prose, to Malay on an English dashboard)",
			conflictBody.Code, CodeFormSlugInUse)
	}
	if conflictBody.Error == "" {
		t.Error("the collision envelope dropped its prose — API-key and MCP callers read it")
	}

	// The form being renamed keeps its own slug...
	if got := fx.storedSlug(t); got != fx.slug {
		t.Errorf("the renamed form's slug changed to %q on a refused rename, want %q untouched", got, fx.slug)
	}
	// ...and the form that owns the slug is untouched (a rename must never
	// steal a live URL).
	var otherSlug string
	if err := fx.pool.QueryRow(fx.ctx, `SELECT slug FROM forms WHERE id=$1`, other.ID).Scan(&otherSlug); err != nil {
		t.Fatalf("read the colliding form's slug: %v", err)
	}
	if otherSlug != taken {
		t.Errorf("the form owning slug %q now has %q — a refused rename must not touch it", taken, otherSlug)
	}
	// Both URLs still resolve to their own forms.
	for _, tc := range []struct{ slug, want string }{{fx.slug, "Hello"}, {taken, ""}} {
		rec := publicServe(t, fx.pub, fx.host, "/"+tc.slug)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /%s after the refused rename = %d, want 200", tc.slug, rec.Code)
		}
	}
}

// TestFormSlugRenameInvalidSlugIs400 pins the validation the create path's
// generator already implies: a rename cannot produce a slug the create path
// would not have made.
func TestFormSlugRenameInvalidSlugIs400(t *testing.T) {
	fx := newRenameFixture(t, "it-valid-"+randHex(4))

	for _, bad := range []string{
		"Bad Slug",              // space
		"UPPER",                 // uppercase (normalised to lower, so this one passes — see below)
		"has/slash",             // path separator
		"emoji-\U0001F600",      // non-ASCII
		"dot.dot",               // dot is not in the allowlist
		strings.Repeat("a", 41), // over the 40-char bound
		"../escape",             // traversal-ish
		"jomform.com",           // dot, same as above — kept explicit
		"---",                   //
	} {
		if bad == "UPPER" || bad == "---" {
			// "UPPER" is lowercased before validation (the console lowercases
			// too), and "---" is within the character allowlist; neither is a
			// rejection case. Asserted here so the list stays honest rather
			// than silently expecting a 400 that never comes.
			continue
		}
		body, _ := json.Marshal(map[string]string{"slug": bad, "title": "Western menu"})
		code, raw := fx.patch(t, string(body))
		if code != http.StatusBadRequest {
			t.Errorf("PATCH slug %q = %d, want 400 (body %s)", bad, code, raw)
			continue
		}
		// The 400 must carry the machine code too, from errcode.go's shared
		// table: the console renders the slug RULE from the code, and without it
		// this refusal falls back to the page's generic "Failed" — the reason
		// being invisible is the defect this half of the card fixes (issue #6177).
		var badBody struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal(raw, &badBody); err != nil {
			t.Fatalf("decode the 400 envelope for %q: %v (%s)", bad, err, raw)
		}
		if badBody.Code != CodeFormSlugInvalid {
			t.Errorf("PATCH slug %q refused with code %q, want %q — the console renders the slug "+
				"rule off this code (issue #6177)", bad, badBody.Code, CodeFormSlugInvalid)
		}
		if badBody.Error == "" {
			t.Errorf("PATCH slug %q dropped its wire prose — API-key and MCP callers read it", bad)
		}
	}
	if got := fx.storedSlug(t); got != fx.slug {
		t.Errorf("a rejected slug changed the stored value to %q, want %q", got, fx.slug)
	}
	// The upper-case case normalises rather than rejecting (the console already
	// lowercases on input), so it must succeed with the lowercased slug.
	lowered := "it-lowered-" + randHex(4)
	code, raw := fx.patch(t, `{"slug":"`+strings.ToUpper(lowered)+`","title":"Western menu"}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH with an uppercase slug = %d, want 200 (body %s)", code, raw)
	}
	if got := fx.storedSlug(t); got != lowered {
		t.Errorf("uppercase slug stored as %q, want the normalised %q", got, lowered)
	}
}

// TestFormSlugInvalidWireMessageMatchesTheStore pins the one thing a hand-written
// literal can drift from: the invalid-slug wire message must stay equal to
// dbstore.ErrInvalidSlug's own text.
//
// WHY BOTH EXIST. errcode.go's message→code table keys on the EXACT string the
// handler hands to writeErr, and its guard (TestIssue6177ErrorCodeTableHasNoDeadKeys)
// requires each key to appear as a Go string literal in a non-errcode.go file of
// this package — an `.Error()` call would not satisfy it, so the handler writes
// `invalidSlugMessage` as a literal. That makes the literal load-bearing for the
// machine code: reword the store's sentinel and, without this test, the 400 would
// silently fall to the generic 400 class code and the console would stop showing
// the slug RULE (issue #6177's failure mode, on this path).
func TestFormSlugInvalidWireMessageMatchesTheStore(t *testing.T) {
	if invalidSlugMessage != dbstore.ErrInvalidSlug.Error() {
		t.Fatalf("the invalid-slug wire message drifted from the store's sentinel:\n"+
			"  handler writes: %q\n  dbstore says:   %q\n"+
			"The wire message is the key for its entry in errcode.go's table, so a drift "+
			"silently downgrades the 400 to the generic request.badRequest code and the "+
			"console stops rendering the slug rule.",
			invalidSlugMessage, dbstore.ErrInvalidSlug.Error())
	}
	// And the code that message resolves to is the one the console maps.
	if got := ErrCodeFor(invalidSlugMessage); got != CodeFormSlugInvalid {
		t.Fatalf("ErrCodeFor(the invalid-slug wire message) = %q, want %q", got, CodeFormSlugInvalid)
	}
	// Both rename codes must be distinct from the trash-restore code: they are
	// different user actions with different remedies, and the dashboard renders
	// one sentence per code (see the comment on CodeFormSlugInUse).
	if CodeFormSlugInUse == CodeFormSlugTaken {
		t.Fatal("CodeFormSlugInUse and CodeFormSlugTaken are the same code, so a rename " +
			"collision would show the merchant the RESTORE instruction (change the other " +
			"form's slug) instead of its own (pick another slug for this form)")
	}
}

// TestFormSlugRenameOldURLRedirectsDeliberately pins the rename's old-URL
// behaviour: a temporary 301 to the new slug, so a printed QR stand, a shared
// link, or an open tab keeps working instead of 404-ing with no trace.
func TestFormSlugRenameOldURLRedirectsDeliberately(t *testing.T) {
	fx := newRenameFixture(t, "it-oldurl-"+randHex(4))
	newSlug := "it-moved-" + randHex(4)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /%s (the form's OLD url after a rename) = %d, want 301 (body %s).\n"+
			"A rename must not orphan the URL already handed out — a printed QR or a shared link "+
			"has to keep resolving, or the merchant's own campaign breaks silently.", fx.slug, rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/"+newSlug {
		t.Errorf("old-URL redirect Location = %q, want %q", loc, "/"+newSlug)
	}

	// The submission path resolves the old URL to the same form too, so a page
	// still open in a buyer's tab can still be submitted.
	form := fx.pub.Forms
	old, err := form.RedirectedFormByWorkspaceSlug(fx.ctx, fx.workspaceSlug, fx.slug)
	if err != nil || old == nil {
		t.Fatalf("RedirectedFormByWorkspaceSlug(%q, %q) = %v, %v — the old URL has no trace", fx.workspaceSlug, fx.slug, old, err)
	}
	if old.ID != fx.formID {
		t.Errorf("the old URL resolves to form %s, want %s", old.ID, fx.formID)
	}
}

// TestFormSlugRenameOldURLRedirectKeepsTheQuery is the half the query-less
// guard above could not see (the carded defect). The 301's Location used to be
// built from the path ALONE, and http.Redirect writes that Location verbatim —
// it never re-appends r.URL.RawQuery. So every parameter on the old URL was
// dropped on the way to the live slug, and the two the product depends on are
// exactly the two a rename is most likely to meet:
//
//   - ?table=N is what a printed QR stand encodes (qr.go). The stand's whole
//     purpose is to tell the kitchen which table ordered; the page rendered the
//     right menu with no table number, and sales.table_number was empty.
//   - ?aff=CODE is an affiliate campaign link (menu.go). Losing it records the
//     sale with no affiliate — the promoter is not paid.
//
// A Location assertion alone would not prove the fix: it is necessary and not
// sufficient. So this case follows the redirect to the destination the header
// names and drives the page's OWN checkout script, then reads the submitted
// order body for table_number / affiliate_code — i.e. what the sale records.
//
// Every route that answers a rename's old URL is checked, because they are
// separate call sites: /{slug} (handleWorkspaceRootForm) and the subdomain and
// custom-domain branches of /f/{slug} (handleRender).
func TestFormSlugRenameOldURLRedirectKeepsTheQuery(t *testing.T) {
	fx := newRenameFixture(t, "it-q-old-"+randHex(4))
	newSlug := "it-q-new-" + randHex(4)
	oldSlug := fx.slug

	// A menu form whose own product block references a live product, so the
	// destination really renders the menu page that consumes the query.
	prod := &dbstore.Product{
		AccountID: fx.accountID, Name: "Nasi Lemak Ayam", PriceSen: 780,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(fx.pool).Create(fx.ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := fx.pool.Exec(fx.ctx,
		`UPDATE forms SET config=$2::jsonb WHERE id=$1`,
		fx.formID, `{"blocks":[{"id":"p1","type":"product","product_id":"`+prod.ID+`","value":"Nasi Lemak"}],`+
			`"menu":{"enabled":true,"categories":[{"name":"Makanan","product_ids":["`+prod.ID+`"]}]}}`); err != nil {
		t.Fatalf("turn the form into a menu: %v", err)
	}
	fx.pub.Products = dbstore.NewProductStore(fx.pool)
	fx.pub.ProductVariations = dbstore.NewProductVariationStore(fx.pool)
	fx.pub.Workspaces = dbstore.NewWorkspaceStore(fx.pool)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	// The query a printed QR stand carries and the one an affiliate link
	// carries, percent-encoded the way a real client sends them.
	const query = "table=7&aff=PROBE%20AFF"

	routes := []struct {
		name string
		host string
		path string
		want string
	}{
		{"workspace subdomain /{slug}", fx.host, "/" + oldSlug, "/" + newSlug + "?" + query},
		{"subdomain /f/{slug}", fx.host, "/f/" + oldSlug, "/f/" + newSlug + "?" + query},
		// The same route on a CUSTOM DOMAIN. Its lookup goes through
		// findPublicForm, whose custom-domain branch used to discard the
		// store's error: ByAccountSlug hands back a non-nil struct with an
		// empty Slug on a miss, so this route answered 301 to the bare
		// workspace root for ANY unknown slug and the rename's redirect to
		// the live slug was never reached. Pinned here so the branch stays
		// honest and the query survives it too.
		{"custom domain /{slug}", fx.customDomain, "/" + oldSlug, "/" + newSlug + "?" + query},
		{"custom domain /f/{slug}", fx.customDomain, "/f/" + oldSlug, "/f/" + newSlug + "?" + query},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			rec := publicServe(t, fx.pub, rt.host, rt.path+"?"+query)
			if rec.Code != http.StatusMovedPermanently {
				t.Fatalf("GET %s on %s = %d, want 301 (body %s)", rt.path, rt.host, rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			if loc != rt.want {
				t.Fatalf("GET %s on %s -> 301 Location=%q, want %q.\n"+
					"The request query was NOT carried: the buyer lands on the right form with the table "+
					"number gone (the QR stand's whole purpose) and the affiliate unpaid. http.Redirect "+
					"writes the Location verbatim and never re-appends r.URL.RawQuery, so the query has "+
					"to be appended at the call site.", rt.path, rt.host, loc, rt.want)
			}
			// The path must stay SERVER-chosen: no caller-supplied authority
			// may ride into the Location.
			if strings.Contains(loc, "://") || strings.HasPrefix(loc, "//") {
				t.Errorf("Location %q is not a same-origin path — the redirect must never reflect a caller-supplied host or scheme", loc)
			}
			// Follow it: the destination must be the live menu page and must
			// actually consume ?table=N / ?aff=CODE.
			u, perr := url.Parse(loc)
			if perr != nil {
				t.Fatalf("parse Location %q: %v", loc, perr)
			}
			dest := publicServe(t, fx.pub, rt.host, u.RequestURI())
			if dest.Code != http.StatusOK {
				t.Fatalf("following %q = %d, want 200 (body %s)", loc, dest.Code, dest.Body.String())
			}
			body := dest.Body.String()
			for _, want := range []string{
				`data-table="7"`,
				`data-aff="PROBE AFF"`,
				"Table 7",
				`<input type="hidden" name="table" value="7">`,
				`<input type="hidden" name="aff" value="PROBE AFF">`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("the page reached through %q is missing %q — the 301 carried the query but the destination does not use it\nbody:\n%s", loc, want, body)
				}
			}
			// The recorded sale is the actual point. Drive the page's own
			// checkout script and read what it posts.
			d, _ := newMenuDOMPageFrom(t, body)
			d.dispatch("click", d.buttonFor(prod.ID))
			if got := len(d.cart()); got != 1 {
				t.Fatalf("adding the menu item put %d line(s) in the basket, want 1", got)
			}
			posted := d.postCheckout()
			var order struct {
				TableNumber   string `json:"table_number"`
				AffiliateCode string `json:"affiliate_code"`
			}
			if err := json.Unmarshal([]byte(posted), &order); err != nil {
				t.Fatalf("decode the posted order body: %v (%s)", err, posted)
			}
			if order.TableNumber != "7" {
				t.Errorf("the order posted through the renamed URL carries table_number=%q, want \"7\" — "+
					"the sale lands with no table, so the QR stand does not say where to serve", order.TableNumber)
			}
			if order.AffiliateCode != "PROBE AFF" {
				t.Errorf("the order posted through the renamed URL carries affiliate_code=%q, want \"PROBE AFF\" — "+
					"the promoter is not attributed, so the affiliate half is money lost", order.AffiliateCode)
			}
		})
	}

	// A request with NO query must still redirect to the bare path — the
	// preservation must not invent a "?".
	if rec := publicServe(t, fx.pub, fx.host, "/"+oldSlug); rec.Code != http.StatusMovedPermanently {
		t.Fatalf("query-less old URL = %d, want 301", rec.Code)
	} else if loc := rec.Header().Get("Location"); loc != "/"+newSlug {
		t.Errorf("query-less old URL Location = %q, want %q", loc, "/"+newSlug)
	}

	// An unknown slug is NOT a rename and must 404 on every host. This is the
	// negative half of the same rule: without it a miss in the custom-domain
	// branch reads as "a form whose slug is empty" and answers 301 to the bare
	// workspace root — a 200-at-/ soft 404 that no Location assertion above
	// would notice, because /{old-slug} still looks like a redirect.
	unknown := "it-unknown-" + randHex(8)
	for _, host := range []string{fx.host, fx.customDomain, "other-" + randHex(6) + ".jomform.com"} {
		rec := publicServe(t, fx.pub, host, "/"+unknown)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /%s on %s = %d (Location=%q), want 404 — an unknown slug is not a rename, "+
				"and a miss must never be read as a form with an empty slug",
				unknown, host, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// TestFormSlugRenameOldURLIsSupersededByALaterClaimant pins Wan's rule verbatim:
// "301 sementara. kalau ada form lain yg claim, maka url tu akan ke form yang
// baru". A later form's live slug must never be shadowed by an earlier form's
// rename history — which is why the live lookup runs FIRST and the redirect is
// only a fallback.
func TestFormSlugRenameOldURLIsSupersededByALaterClaimant(t *testing.T) {
	fx := newRenameFixture(t, "it-claim-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-claim-new-" + randHex(4)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}
	// Precondition: the old URL is a live redirect right now.
	if rec := publicServe(t, fx.pub, fx.host, "/"+oldSlug); rec.Code != http.StatusMovedPermanently {
		t.Fatalf("precondition failed: old URL = %d, want 301 before the claim", rec.Code)
	}

	// A DIFFERENT form in the same workspace now claims the freed slug.
	claimant := &dbstore.Form{
		AccountID: fx.accountID, Slug: oldSlug, Title: "Claimant menu",
		Status: dbstore.FormStatusPublished,
		// Card t_bfdcafbc: one buyable block keeps this fixture on the BLOCK
		// payment page (its "Claimant body" marker) instead of the
		// "not accepting orders" page a nothing-to-sell payment form now serves.
		Config: map[string]any{"blocks": []any{
			map[string]any{"type": "text", "value": "Claimant body"},
			map[string]any{"id": "it_prod", "type": "product", "product_id": "it_prod_marker", "value": "Marker item", "price_sen": 1000},
		}},
		Theme: map[string]any{"primary": "#000000"},
	}
	if err := dbstore.NewFormStore(fx.pool).Create(fx.ctx, claimant); err != nil {
		t.Fatalf("the freed old slug must be claimable by a later form, got: %v", err)
	}

	rec := publicServe(t, fx.pub, fx.host, "/"+oldSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s after a later form claimed it = %d, want 200 served from the CLAIMANT "+
			"(body %s).\nrename history must never shadow a live form — the claimant wins.", oldSlug, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Claimant body") {
		t.Errorf("GET /%s did not serve the claimant's content — the old form's redirect shadowed the live form (body %s)", oldSlug, body)
	}
	if strings.Contains(body, "Hello") {
		t.Errorf("GET /%s served the ORIGINAL form's content; the redirect must be superseded by the claimant", oldSlug)
	}
}

// TestFormSlugRenameRedirectExpires pins that the 301 really is temporary: an
// expired entry is not consulted, so the old URL 404s like any unknown slug.
func TestFormSlugRenameRedirectExpires(t *testing.T) {
	fx := newRenameFixture(t, "it-expiry-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-expiry-new-" + randHex(4)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	// Age the redirect past its window.
	if _, err := fx.pool.Exec(fx.ctx,
		`UPDATE form_slug_redirects SET expires_at = now() - interval '1 second'
		  WHERE account_id=$1 AND old_slug=$2`, fx.accountID, oldSlug); err != nil {
		t.Fatalf("expire the redirect: %v", err)
	}

	if rec := publicServe(t, fx.pub, fx.host, "/"+oldSlug); rec.Code != http.StatusNotFound {
		t.Errorf("GET /%s with an EXPIRED redirect = %d, want 404 — the 301 is temporary, not a permanent alias",
			oldSlug, rec.Code)
	}
	// The form's own live URL is unaffected.
	if rec := publicServe(t, fx.pub, fx.host, "/"+newSlug); rec.Code != http.StatusOK {
		t.Errorf("GET /%s (the form's live slug) = %d, want 200", newSlug, rec.Code)
	}
	// And the row really did exist before expiry (so the 404 above is the
	// expiry rule, not a redirect that was never written).
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT count(*) FROM form_slug_redirects WHERE account_id=$1 AND old_slug=$2`,
		fx.accountID, oldSlug).Scan(&n); err != nil {
		t.Fatalf("count redirect rows: %v", err)
	}
	if n != 1 {
		t.Errorf("redirect rows for the old slug = %d, want exactly 1", n)
	}
}

// TestFormSlugRenameLeavesItsTTLInTheRow pins the "temporary" claim to data,
// not prose: the row a rename writes must carry a finite future expiry.
func TestFormSlugRenameLeavesItsTTLInTheRow(t *testing.T) {
	fx := newRenameFixture(t, "it-ttl-"+randHex(4))
	newSlug := "it-ttl-new-" + randHex(4)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}
	var expires time.Time
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT expires_at FROM form_slug_redirects WHERE account_id=$1 AND old_slug=$2`,
		fx.accountID, fx.slug).Scan(&expires); err != nil {
		t.Fatalf("read the redirect's expiry: %v", err)
	}
	ttl := time.Until(expires)
	if ttl <= 0 {
		t.Fatalf("the redirect expires in %v — it must be a FUTURE window", ttl)
	}
	if ttl > 365*24*time.Hour {
		t.Errorf("the redirect lives %v — 'temporary' must be bounded well under a year", ttl)
	}
}
