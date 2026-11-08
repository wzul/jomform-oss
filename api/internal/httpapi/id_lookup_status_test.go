package httpapi

// Regression: every remaining id-taking REST route that funnels a
// caller-supplied id into a uuid-keyed accessor must answer 404 for an id it
// cannot resolve, never 500.
//
// The stores in dbstore have two ways of failing to name a row the caller may
// see, and both are a NOT-FOUND rather than a server fault:
//
//   - the row exists nowhere, or the query's ownership filter excluded it
//     (another account's row) — the driver reports pgx.ErrNoRows / no rows
//     affected;
//   - the id cannot be a UUID at all, for which Postgres raises SQLSTATE 22P02
//     before any row is considered.
//
// respondErr only maps the store's not-found sentinels, so an accessor that
// returns the raw driver error turns the second shape into HTTP 500
// "internal error" with an error_class=internal counter — a client's bad id
// counted as a server fault. This covers the accessors the template fix did
// not reach: APIKeyStore.Revoke, ProductStore update/archive, and the webhook
// delete/update/deliveries/retry accessors.
//
// Driven through the real REST mux against a real Postgres; skipped without
// JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// idLookupBody decodes a JSON object body, tolerating an empty one.
func idLookupBody(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var body map[string]any
	if len(raw) == 0 {
		return body
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return body
}

// assertIDMissMsg is assertIDMiss for a route that names its own not-found
// message (more specific than the store's generic one).
func assertIDMissMsg(t *testing.T, h *RESTHandlers, key, method, path, body, wantMsg, what string) {
	t.Helper()
	code, raw := tplReq(t, h, key, method, path, body)
	if code != http.StatusNotFound {
		t.Errorf("%s %s (%s) = %d, want 404 (body=%s)", method, path, what, code, raw)
		return
	}
	if got := idLookupBody(t, raw)["error"]; got != wantMsg {
		t.Errorf("%s %s (%s) error = %v, want %q (body=%s)", method, path, what, got, wantMsg, raw)
	}
}

// assertIDMiss asserts a 404 carrying the store's generic not-found message.
// The message is asserted (not just the status) so a 404 produced elsewhere on
// the route cannot satisfy the test.
func assertIDMiss(t *testing.T, h *RESTHandlers, key, method, path, body, what string) {
	t.Helper()
	code, raw := tplReq(t, h, key, method, path, body)
	if code != http.StatusNotFound {
		t.Errorf("%s %s (%s) = %d, want 404 (body=%s)", method, path, what, code, raw)
		return
	}
	if got := idLookupBody(t, raw)["error"]; got != "not found" {
		t.Errorf("%s %s (%s) error = %v, want \"tidak dijumpai\" (body=%s)", method, path, what, got, raw)
	}
}

// assertIDOK asserts a 2xx control response: the caller's own row must keep
// working, so a fix that makes every id a 404 fails here.
func assertIDOK(t *testing.T, h *RESTHandlers, key, method, path, body, what string) json.RawMessage {
	t.Helper()
	code, raw := tplReq(t, h, key, method, path, body)
	if code < 200 || code > 299 {
		t.Errorf("%s %s (%s) = %d, want 2xx (body=%s)", method, path, what, code, raw)
	}
	return raw
}

// seedIdLookupSubscription creates an active subscription owned by accountID
// and returns its id, so the per-id subscription routes have a control row.
func seedIdLookupSubscription(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	prod := &dbstore.Product{
		AccountID: accountID, Name: "ID Sub Product", PriceSen: 1500,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create subscription product: %v", err)
	}
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "id-lookup-" + accountID[:8] + "@example.com",
		ProductID: prod.ID, ChipTokenPurchaseID: "tok-" + uuid.NewString(),
		Status: dbstore.SubActive, Period: "month", AmountSen: 1500,
		NextChargeAt: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := dbstore.NewSubscriptionStore(pool).Create(ctx, sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return sub.ID
}

func TestIDTakingRoutesAnswer404ForUnresolvableIDs(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping id lookup status test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	keyB := bundleAPIKey(t, ctx, pool, accB)
	h := newBundleHandlers(pool)

	keys := dbstore.NewAPIKeyStore(pool)
	products := dbstore.NewProductStore(pool)
	hooks := dbstore.NewWebhookStore(pool)

	// ── fixtures: one row of each kind for the caller, one for another account.
	mineKey, err := keys.Create(ctx, accA, "hash-"+accA[:8], "Mine", []string{"admin"})
	if err != nil {
		t.Fatalf("create own api key: %v", err)
	}
	theirsKey, err := keys.Create(ctx, accB, "hash-"+accB[:8], "Theirs", []string{"admin"})
	if err != nil {
		t.Fatalf("create foreign api key: %v", err)
	}
	// A second key for account B, used only for the "a foreign caller can still
	// revoke its OWN key" control: the failed revokes above must not be a
	// blanket refusal for account B.
	theirOwnKey, err := keys.Create(ctx, accB, "hash2-"+accB[:8], "Their Own", []string{"admin"})
	if err != nil {
		t.Fatalf("create second foreign api key: %v", err)
	}
	mineProduct := &dbstore.Product{AccountID: accA, Name: "ID Control", PriceSen: 500, Type: "one_time"}
	if err := products.Create(ctx, mineProduct); err != nil {
		t.Fatalf("create own product: %v", err)
	}
	theirsProduct := &dbstore.Product{AccountID: accB, Name: "Foreign", PriceSen: 500, Type: "one_time"}
	if err := products.Create(ctx, theirsProduct); err != nil {
		t.Fatalf("create foreign product: %v", err)
	}
	mineHook, err := hooks.Create(ctx, accA, "", "https://example.test/mine", "secret1234", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create own webhook: %v", err)
	}
	theirsHook, err := hooks.Create(ctx, accB, "", "https://example.test/theirs", "secret1234", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create foreign webhook: %v", err)
	}

	// idShapes is one representative input per failure shape: a well-formed id
	// naming no row, another account's row, and an id that cannot be a UUID.
	// Each reaches the accessor through a different branch (ErrNoRows / no rows
	// affected vs SQLSTATE 22P02), so each is a separate case.
	idShapes := func(missing, foreign string) []struct{ name, id string } {
		return []struct{ name, id string }{
			{"nonexistent uuid", missing},
			{"foreign uuid", foreign},
			{"non-uuid id", "not-a-uuid"},
		}
	}

	// ── products: PUT / GET / DELETE (GET was fixed with the form lookup; the
	// writing routes were not).
	assertIDOK(t, h, keyA, http.MethodPut, "/v1/rest/products/"+mineProduct.ID, `{"name":"ID Control Renamed"}`, "own product PUT")
	assertIDOK(t, h, keyA, http.MethodGet, "/v1/rest/products/"+mineProduct.ID, "", "own product GET")
	for _, tc := range idShapes(uuid.NewString(), theirsProduct.ID) {
		assertIDMiss(t, h, keyA, http.MethodPut, "/v1/rest/products/"+tc.id, `{"name":"hijacked"}`, tc.name)
		assertIDMiss(t, h, keyA, http.MethodGet, "/v1/rest/products/"+tc.id, "", tc.name)
		assertIDMiss(t, h, keyA, http.MethodDelete, "/v1/rest/products/"+tc.id, "", tc.name)
	}
	// The DELETE control runs last: it archives the row the other product
	// assertions read, and a repeat archive stays a no-op success.
	assertIDOK(t, h, keyA, http.MethodDelete, "/v1/rest/products/"+mineProduct.ID, "", "own product DELETE")

	// ── api keys: DELETE /v1/rest/keys/{id}. A revoke that named no key is a
	// not-found, not a silent 200: the caller asked to revoke a key and got
	// nothing revoked.
	for _, tc := range idShapes(uuid.NewString(), theirsKey.ID) {
		assertIDMiss(t, h, keyA, http.MethodDelete, "/v1/rest/keys/"+tc.id, "", tc.name)
	}
	// Control: the caller's own key still revokes and reports it.
	assertIDOK(t, h, keyA, http.MethodDelete, "/v1/rest/keys/"+mineKey.ID, "", "own key DELETE")
	// Revoking the caller's own key is idempotent: a repeat (or a client
	// retry) still answers 200 because the key IS revoked, rather than a
	// confusing 404 for a state the caller asked for and got.
	assertIDOK(t, h, keyA, http.MethodDelete, "/v1/rest/keys/"+mineKey.ID, "", "own key DELETE repeat")
	if err := keys.Revoke(ctx, accA, mineKey.ID); err != nil {
		t.Errorf("Keys.Revoke(own key, repeat) err = %v, want nil (idempotent)", err)
	}
	// Control: the refusal is scoped to ids the caller cannot see, not to
	// account B — its own key still revokes through its own credentials.
	assertIDOK(t, h, keyB, http.MethodDelete, "/v1/rest/keys/"+theirOwnKey.ID, "", "foreign caller's own key DELETE")
	// The foreign key must be untouched by the failed revokes above.
	var foreignRevoked *string
	if err := pool.QueryRow(ctx, `SELECT revoked_at::text FROM api_keys WHERE id=$1`, theirsKey.ID).Scan(&foreignRevoked); err != nil {
		t.Fatalf("re-read foreign key: %v", err)
	}
	if foreignRevoked != nil {
		t.Errorf("foreign api key %s was revoked by a caller who does not own it", theirsKey.ID)
	}

	// ── forms: POST /v1/rest/forms/{id}/restore (card t_94707517).
	//
	// The one FORM-trash route left out of this sweep. `FormStore.Restore`'s miss
	// guard was `RowsAffected()==0`, so a non-uuid never reached it: Postgres
	// refuses the CAST and raises SQLSTATE 22P02 out of Exec, the raw PgError
	// came back, and respondErr has no case for it — measured on main:
	//   POST /v1/rest/forms/not-a-uuid/restore -> 500 {"code":"internal"}
	// while every sibling read answers 404 for the same shape. `GetTrashed` (the
	// other half of the purge path) and the workspace trash reads via PR 859 were
	// already fixed; this was the copy left behind.
	//
	// The control matters as much as the misses: restoring a REAL trashed form the
	// caller owns must still answer 200, or "always 404" would satisfy the rest.
	// Constructed here rather than reusing the `forms` store built further down:
	// this block runs before it exists.
	formStore := dbstore.NewFormStore(pool)
	trashForm := &dbstore.Form{AccountID: accA, Slug: "id-control-trashed", Title: "ID Control", Status: dbstore.FormStatusDraft}
	if err := formStore.Create(ctx, trashForm); err != nil {
		t.Fatalf("create own form: %v", err)
	}
	if err := formStore.SoftDelete(ctx, accA, trashForm.ID); err != nil {
		t.Fatalf("trash own form: %v", err)
	}
	for _, tc := range idShapes(uuid.NewString(), "") {
		if tc.id == "" {
			continue // no foreign-form leg: a form id is not addressable across accounts here
		}
		assertIDMiss(t, h, keyA, http.MethodPost, "/v1/rest/forms/"+tc.id+"/restore", "", tc.name)
	}
	assertIDOK(t, h, keyA, http.MethodPost, "/v1/rest/forms/"+trashForm.ID+"/restore", "", "own trashed form RESTORE")

	// ── webhooks: DELETE, PUT, and the delivery log.
	assertIDOK(t, h, keyA, http.MethodGet, "/v1/rest/webhooks/"+mineHook.ID+"/deliveries", "", "own webhook deliveries")
	assertIDOK(t, h, keyA, http.MethodPut, "/v1/rest/webhooks/"+mineHook.ID,
		`{"url":"https://example.com/renamed","events":["sale.paid"]}`, "own webhook PUT")
	for _, tc := range idShapes(uuid.NewString(), theirsHook.ID) {
		assertIDMiss(t, h, keyA, http.MethodDelete, "/v1/rest/webhooks/"+tc.id, "", tc.name)
		assertIDMiss(t, h, keyA, http.MethodPut, "/v1/rest/webhooks/"+tc.id,
			`{"url":"https://example.com/hijacked","events":["sale.paid"]}`, tc.name)
		assertIDMiss(t, h, keyA, http.MethodGet, "/v1/rest/webhooks/"+tc.id+"/deliveries", "", tc.name)
	}
	// A webhook that exists and is owned, with no deliveries, is an empty list
	// rather than a not-found: the route must not turn "nothing happened yet"
	// into a 404.
	code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/webhooks/"+mineHook.ID+"/deliveries", "")
	if code != http.StatusOK {
		t.Fatalf("own webhook deliveries (no rows) = %d, want 200 (body=%s)", code, raw)
	}
	var deliveries []map[string]any
	if err := json.Unmarshal(raw, &deliveries); err != nil {
		t.Fatalf("decode deliveries: %v (%s)", err, raw)
	}
	if len(deliveries) != 0 {
		t.Errorf("own webhook with no deliveries returned %d rows, want 0", len(deliveries))
	}
	// The same shape one level down: the manual-retry routes take a delivery id
	// and a retry id out of the path. These two name their own message on the
	// path (they are more specific than the generic "tidak dijumpai"), so assert
	// the status plus that the answer is a not-found rather than the masked
	// internal error a 22P02 leak produced.
	for _, c := range []struct{ name, path, wantMsg string }{
		{"non-uuid delivery id", "/v1/rest/webhooks/" + mineHook.ID + "/deliveries/not-a-uuid/retry", "delivery not found"},
		{"non-uuid retry id", "/v1/rest/webhooks/" + mineHook.ID + "/retries/not-a-uuid/retry", "retry not found"},
		{"foreign delivery id", "/v1/rest/webhooks/" + mineHook.ID + "/deliveries/" + theirsHook.ID + "/retry", "delivery not found"},
	} {
		code, raw := tplReq(t, h, keyA, http.MethodPost, c.path, "")
		if code != http.StatusNotFound {
			t.Errorf("POST %s (%s) = %d, want 404 (body=%s)", c.path, c.name, code, raw)
			continue
		}
		if got := idLookupBody(t, raw)["error"]; got != c.wantMsg {
			t.Errorf("POST %s (%s) error = %v, want %q (body=%s)", c.path, c.name, got, c.wantMsg, raw)
		}
	}
	// Control: the DELETE runs last because it disables the webhook.
	assertIDOK(t, h, keyA, http.MethodDelete, "/v1/rest/webhooks/"+mineHook.ID, "", "own webhook DELETE")

	// The foreign webhook must still be active and unmodified by the misses.
	var foreignActive bool
	var foreignURL string
	if err := pool.QueryRow(ctx,
		`SELECT active, url FROM outbound_webhooks WHERE id=$1`, theirsHook.ID).Scan(&foreignActive, &foreignURL); err != nil {
		t.Fatalf("re-read foreign webhook: %v", err)
	}
	if !foreignActive || foreignURL != "https://example.test/theirs" {
		t.Errorf("foreign webhook mutated by an unauthorised id: active=%v url=%q", foreignActive, foreignURL)
	}

	// ── the store contract behind those statuses: every miss is a sentinel
	// whose error class is not_found (that is the error_class log field and the
	// metric bucket respondErr would otherwise count as internal).
	for _, tc := range idShapes(uuid.NewString(), theirsProduct.ID) {
		if err := products.Update(ctx, accA, tc.id, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil); !errors.Is(err, dbstore.ErrProductNotFound) {
			t.Errorf("Products.Update(%s) err = %v, want dbstore.ErrProductNotFound", tc.name, err)
		} else if class := errorClass(err); class != "not_found" {
			t.Errorf("errorClass(Products.Update(%s)) = %q, want not_found", tc.name, class)
		}
		if err := products.Archive(ctx, accA, tc.id); !errors.Is(err, dbstore.ErrProductNotFound) {
			t.Errorf("Products.Archive(%s) err = %v, want dbstore.ErrProductNotFound", tc.name, err)
		}
		if _, err := products.Get(ctx, accA, tc.id); !errors.Is(err, dbstore.ErrProductNotFound) {
			t.Errorf("Products.Get(%s) err = %v, want dbstore.ErrProductNotFound", tc.name, err)
		}
		// SetActive backs the MCP archive_product tool (no REST route of its
		// own), so it is asserted here rather than through the mux.
		if err := products.SetActive(ctx, accA, tc.id, false); !errors.Is(err, dbstore.ErrProductNotFound) {
			t.Errorf("Products.SetActive(%s) err = %v, want dbstore.ErrProductNotFound", tc.name, err)
		}
	}
	for _, tc := range idShapes(uuid.NewString(), theirsKey.ID) {
		if err := keys.Revoke(ctx, accA, tc.id); !errors.Is(err, dbstore.ErrAPIKeyInvalid) {
			t.Errorf("Keys.Revoke(%s) err = %v, want dbstore.ErrAPIKeyInvalid", tc.name, err)
		} else if class := errorClass(err); class != "not_found" {
			t.Errorf("errorClass(Keys.Revoke(%s)) = %q, want not_found", tc.name, class)
		}
	}
	for _, tc := range idShapes(uuid.NewString(), theirsHook.ID) {
		if err := hooks.Delete(ctx, accA, tc.id); !errors.Is(err, dbstore.ErrWebhookNotFound) {
			t.Errorf("Webhooks.Delete(%s) err = %v, want dbstore.ErrWebhookNotFound", tc.name, err)
		} else if class := errorClass(err); class != "not_found" {
			t.Errorf("errorClass(Webhooks.Delete(%s)) = %q, want not_found", tc.name, class)
		}
		if _, err := hooks.ListDeliveries(ctx, accA, tc.id, 10); !errors.Is(err, dbstore.ErrWebhookNotFound) {
			t.Errorf("Webhooks.ListDeliveries(%s) err = %v, want dbstore.ErrWebhookNotFound", tc.name, err)
		}
		if err := hooks.Update(ctx, accA, tc.id, "https://example.test/x", "", []string{"sale.paid"}); !errors.Is(err, dbstore.ErrWebhookNotFound) {
			t.Errorf("Webhooks.Update(%s) err = %v, want dbstore.ErrWebhookNotFound", tc.name, err)
		}
		// The retry subsystem keeps the driver's own not-found signal, which
		// its handlers already map to 404 by hand; a non-uuid id must join that
		// same answer instead of raising 22P02 into a 500.
		if _, err := hooks.GetDelivery(ctx, accA, tc.id); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("Webhooks.GetDelivery(%s) err = %v, want pgx.ErrNoRows", tc.name, err)
		}
		if err := hooks.RetryNow(ctx, accA, tc.id); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("Webhooks.RetryNow(%s) err = %v, want pgx.ErrNoRows", tc.name, err)
		}
	}

	// ── forms: the writing accessors reached from PATCH/DELETE and the
	// per-form sub-resource routes. Every one of them funnels a path id into a
	// uuid-keyed UPDATE, so the same two shapes apply.
	forms := dbstore.NewFormStore(pool)
	mineForm := &dbstore.Form{
		AccountID: accA, Slug: "ids-own-" + accA[:8], Title: "ID Form",
		Config: map[string]any{"mode": "response"}, Theme: map[string]any{},
		Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, mineForm); err != nil {
		t.Fatalf("create own form: %v", err)
	}
	theirsForm := &dbstore.Form{
		AccountID: accB, Slug: "ids-foreign-" + accB[:8], Title: "Foreign Form",
		Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, theirsForm); err != nil {
		t.Fatalf("create foreign form: %v", err)
	}

	// Controls: the caller's own form still accepts each write. Bodies are
	// valid so the route reaches the store rather than failing validation.
	assertIDOK(t, h, keyA, http.MethodPatch, "/v1/rest/forms/"+mineForm.ID,
		`{"title":"ID Form Renamed"}`, "own form PATCH")
	assertIDOK(t, h, keyA, http.MethodPut, "/v1/rest/forms/"+mineForm.ID+"/tracking",
		`{"head":"<meta name=\"x\" content=\"1\">"}`, "own form tracking")
	assertIDOK(t, h, keyA, http.MethodPut, "/v1/rest/forms/"+mineForm.ID+"/settings",
		`{"redirect_success":"https://example.com/ok","allow_embed":true}`, "own form settings")
	assertIDOK(t, h, keyA, http.MethodPut, "/v1/rest/forms/"+mineForm.ID+"/restrictions",
		`{"entry_limit":5}`, "own form restrictions")

	for _, tc := range idShapes(uuid.NewString(), theirsForm.ID) {
		assertIDMiss(t, h, keyA, http.MethodPatch, "/v1/rest/forms/"+tc.id,
			`{"title":"hijacked"}`, tc.name)
		assertIDMiss(t, h, keyA, http.MethodDelete, "/v1/rest/forms/"+tc.id, "", tc.name)
		assertIDMiss(t, h, keyA, http.MethodPost, "/v1/rest/forms/"+tc.id+"/publish", "", tc.name)
		assertIDMiss(t, h, keyA, http.MethodPut, "/v1/rest/forms/"+tc.id+"/tracking",
			`{"head":"<meta name=\"x\">"}`, tc.name)
		assertIDMiss(t, h, keyA, http.MethodPut, "/v1/rest/forms/"+tc.id+"/settings",
			`{"redirect_success":"https://example.com/ok"}`, tc.name)
		assertIDMiss(t, h, keyA, http.MethodPut, "/v1/rest/forms/"+tc.id+"/restrictions",
			`{"entry_limit":5}`, tc.name)
		assertIDMiss(t, h, keyA, http.MethodDelete, "/v1/rest/forms/"+tc.id+"/chip-override", "", tc.name)
	}

	// The store contract, including the per-form writing accessors.
	for _, tc := range idShapes(uuid.NewString(), theirsForm.ID) {
		for name, err := range map[string]error{
			"FormStore.Update":             forms.Update(ctx, accA, tc.id, "x", nil, nil),
			"FormStore.SetTracking":        forms.SetTracking(ctx, accA, tc.id, ""),
			"FormStore.UpdateSettings":     forms.UpdateSettings(ctx, accA, tc.id, "", "", true, "", ""),
			"FormStore.UpdateRestrictions": forms.UpdateRestrictions(ctx, accA, tc.id, nil, nil, nil, "", "", ""),
			"FormStore.SetFailureMessage":  forms.SetFailureMessage(ctx, accA, tc.id, ""),
			"FormStore.SetChipOverride":    forms.SetChipOverride(ctx, accA, tc.id, "b", "s", "", nil),
			"FormStore.ClearChipOverride":  forms.ClearChipOverride(ctx, accA, tc.id),
			"FormStore.SetAccessPassword":  forms.SetAccessPassword(ctx, accA, tc.id, ""),
			"FormStore.SoftDelete":         forms.SoftDelete(ctx, accA, tc.id),
		} {
			if !errors.Is(err, dbstore.ErrFormNotFound) {
				t.Errorf("%s(%s) err = %v, want dbstore.ErrFormNotFound", name, tc.name, err)
			} else if class := errorClass(err); class != "not_found" {
				t.Errorf("errorClass(%s(%s)) = %q, want not_found", name, tc.name, class)
			}
		}
	}

	// ── subscriptions: the three per-id mutation routes compared the store
	// error with ==, so a malformed id bypassed their 404 branch.
	subs := dbstore.NewSubscriptionStore(pool)
	mineSub := seedIdLookupSubscription(t, ctx, pool, accA)
	for _, tc := range idShapes(uuid.NewString(), "") {
		if tc.id == "" {
			continue
		}
		assertIDMissMsg(t, h, keyA, http.MethodPost, "/v1/rest/subscriptions/"+tc.id+"/cancel", "", "subscription not found", tc.name)
		if _, err := subs.ByIDForAccount(ctx, accA, tc.id); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("Subs.ByIDForAccount(%s) err = %v, want pgx.ErrNoRows", tc.name, err)
		}
	}
	// Control: the caller's own subscription still cancels.
	assertIDOK(t, h, keyA, http.MethodPost, "/v1/rest/subscriptions/"+mineSub+"/cancel", "", "own subscription cancel")
	// A foreign subscription id is a miss, not a cancel.
	foreignSub := seedIdLookupSubscription(t, ctx, pool, accB)
	assertIDMissMsg(t, h, keyA, http.MethodPost, "/v1/rest/subscriptions/"+foreignSub+"/cancel", "", "subscription not found", "foreign subscription")
	var foreignCancelled *string
	if err := pool.QueryRow(ctx, `SELECT cancelled_at::text FROM subscriptions WHERE id=$1`, foreignSub).Scan(&foreignCancelled); err != nil {
		t.Fatalf("re-read foreign subscription: %v", err)
	}
	if foreignCancelled != nil {
		t.Errorf("foreign subscription %s was cancelled by a caller who does not own it", foreignSub)
	}

	// ── media: SoftDelete returned the raw driver error for a non-uuid id, so
	// DELETE /v1/rest/media/{id} answered 500 instead of its own 404.
	media := dbstore.NewMediaStore(pool)
	h.Media = media
	var mineMedia string
	if err := pool.QueryRow(ctx,
		`INSERT INTO media (account_id, key, url, mime, size)
		 VALUES ($1, $2, 'https://cdn.example/x.png', 'image/png', 1234) RETURNING id`,
		accA, "k-"+uuid.NewString()).Scan(&mineMedia); err != nil {
		t.Fatalf("seed media row: %v", err)
	}
	assertIDMissMsg(t, h, keyA, http.MethodDelete, "/v1/rest/media/not-a-uuid", "", "media not found", "non-uuid media id")
	assertIDMissMsg(t, h, keyA, http.MethodDelete, "/v1/rest/media/"+uuid.NewString(), "", "media not found", "nonexistent media id")
	// A foreign media row is invisible rather than deleted.
	var foreignMedia string
	if err := pool.QueryRow(ctx,
		`INSERT INTO media (account_id, key, url, mime, size)
		 VALUES ($1, $2, 'https://cdn.example/y.png', 'image/png', 1234) RETURNING id`,
		accB, "k-"+uuid.NewString()).Scan(&foreignMedia); err != nil {
		t.Fatalf("seed foreign media row: %v", err)
	}
	assertIDMissMsg(t, h, keyA, http.MethodDelete, "/v1/rest/media/"+foreignMedia, "", "media not found", "foreign media id")
	var foreignMediaDeleted *string
	if err := pool.QueryRow(ctx, `SELECT deleted_at::text FROM media WHERE id=$1`, foreignMedia).Scan(&foreignMediaDeleted); err != nil {
		t.Fatalf("re-read foreign media row: %v", err)
	}
	if foreignMediaDeleted != nil {
		t.Errorf("foreign media row %s was soft-deleted by a caller who does not own it", foreignMedia)
	}
	if _, _, ok, err := media.SoftDelete(ctx, accA, "not-a-uuid"); err != nil || ok {
		t.Errorf("Media.SoftDelete(non-uuid) = ok=%v err=%v, want ok=false err=nil", ok, err)
	}
	// Control: the caller's own media row still deletes.
	assertIDOK(t, h, keyA, http.MethodDelete, "/v1/rest/media/"+mineMedia, "", "own media DELETE")

	// ── response-form submissions: the two read routes take a form id straight
	// into a submissions COUNT/SELECT, so a non-uuid id raised 22P02 into a 500.
	// Their existing semantics for a well-formed id naming no form is an empty
	// page / zero count, so the malformed shape must answer the same rather
	// than a server fault. Submissions is wired here for the same reason as
	// Media: the bare test bundle leaves it nil.
	h.Submissions = dbstore.NewSubmissionStore(pool)
	subsStore := h.Submissions
	for _, tc := range idShapes(uuid.NewString(), theirsForm.ID) {
		for _, suffix := range []string{"/submissions", "/submissions/count"} {
			code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/forms/"+tc.id+suffix, "")
			if code != http.StatusOK {
				t.Errorf("GET %s (%s) = %d, want 200 — a bad form id is an empty page, not a fault (body=%s)",
					suffix, tc.name, code, raw)
				continue
			}
			if got := idLookupBody(t, raw)["total"]; suffix == "/submissions" && got != float64(0) {
				t.Errorf("GET %s (%s) total = %v, want 0", suffix, tc.name, got)
			}
			if got := idLookupBody(t, raw)["count"]; suffix == "/submissions/count" && got != float64(0) {
				t.Errorf("GET %s (%s) count = %v, want 0", suffix, tc.name, got)
			}
		}
	}
	// The store contract: a non-uuid form id is zero rows, not an error.
	if _, total, err := subsStore.ListByAccountsFormPage(ctx, []string{accA}, "not-a-uuid", 1, 20); err != nil || total != 0 {
		t.Errorf("ListByAccountsFormPage(non-uuid) = total=%d err=%v, want 0/nil", total, err)
	}
	if n, err := subsStore.CountByAccountsForm(ctx, []string{accA}, "not-a-uuid"); err != nil || n != 0 {
		t.Errorf("CountByAccountsForm(non-uuid) = %d/%v, want 0/nil", n, err)
	}
	// Control: a real submission on the caller's form is still counted.
	var subID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO submissions (form_id, account_id, data) VALUES ($1,$2,'{"a":1}'::jsonb) RETURNING id`,
		mineForm.ID, accA).Scan(&subID); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
	if n, err := subsStore.CountByAccountsForm(ctx, []string{accA}, mineForm.ID); err != nil || n != 1 {
		t.Errorf("CountByAccountsForm(own form) = %d/%v, want 1/nil", n, err)
	}
}
