// Issue #196: per-form webhook overrides.
// A webhook with form_id set fires only for that form's events; account-wide
// ones (form_id NULL) fire for all forms. Integration against a real Postgres;
// skipped without JOMFORM_TEST_DATABASE_URL.
package dbstore

import (
	"context"
	"testing"
)

func TestWebhookFormScoping(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	acc := uniqueAccount(t, ctx, p)
	fs := NewFormStore(p)
	ws := NewWebhookStore(p)

	// Two forms in the same account.
	f1 := &Form{AccountID: acc, Slug: "it-f1-" + randSuffix(), Title: "F1", Status: "published"}
	if err := fs.Create(ctx, f1); err != nil {
		t.Fatalf("create form1: %v", err)
	}
	f2 := &Form{AccountID: acc, Slug: "it-f2-" + randSuffix(), Title: "F2", Status: "published"}
	if err := fs.Create(ctx, f2); err != nil {
		t.Fatalf("create form2: %v", err)
	}

	// One account-wide webhook + one form-scoped webhook (f1).
	if _, err := ws.Create(ctx, acc, "", "https://example.test/acc", "secret1234", []string{"sale.paid"}); err != nil {
		t.Fatalf("create account-wide webhook: %v", err)
	}
	if _, err := ws.Create(ctx, acc, f1.ID, "https://example.test/f1", "secret1234", []string{"sale.paid"}); err != nil {
		t.Fatalf("create form webhook: %v", err)
	}

	// ListByAccount with empty formID returns only account-wide webhooks.
	accWide, err := ws.ListByAccount(ctx, acc, "")
	if err != nil {
		t.Fatalf("list account-wide: %v", err)
	}
	if len(accWide) != 1 {
		t.Fatalf("account-wide webhooks = %d, want 1", len(accWide))
	}
	if accWide[0].URL != "https://example.test/acc" {
		t.Fatalf("account-wide url = %q, want acc", accWide[0].URL)
	}

	// ListByAccount with formID returns only that form's webhooks.
	f1Hooks, err := ws.ListByAccount(ctx, acc, f1.ID)
	if err != nil {
		t.Fatalf("list form webhooks: %v", err)
	}
	if len(f1Hooks) != 1 || f1Hooks[0].URL != "https://example.test/f1" {
		t.Fatalf("form1 webhooks = %+v, want only the f1 hook", f1Hooks)
	}

	// f2 has no form-scoped webhooks.
	f2Hooks, err := ws.ListByAccount(ctx, acc, f2.ID)
	if err != nil {
		t.Fatalf("list form2 webhooks: %v", err)
	}
	if len(f2Hooks) != 0 {
		t.Fatalf("form2 webhooks = %d, want 0", len(f2Hooks))
	}

	// Dispatch for f1 fires the account-wide + f1-scoped webhooks.
	dispatch, err := ws.ListActiveForDispatch(ctx, acc, f1.ID)
	if err != nil {
		t.Fatalf("dispatch f1: %v", err)
	}
	if len(dispatch) != 2 {
		t.Fatalf("dispatch f1 webhooks = %d, want 2 (account-wide + f1)", len(dispatch))
	}

	// Dispatch for f2 fires only the account-wide webhook.
	dispatch2, err := ws.ListActiveForDispatch(ctx, acc, f2.ID)
	if err != nil {
		t.Fatalf("dispatch f2: %v", err)
	}
	if len(dispatch2) != 1 || dispatch2[0].URL != "https://example.test/acc" {
		t.Fatalf("dispatch f2 webhooks = %+v, want only account-wide", dispatch2)
	}

	// Account isolation: a different account sees none of these.
	other := uniqueAccount(t, ctx, p)
	otherHooks, err := ws.ListByAccount(ctx, other, "")
	if err != nil {
		t.Fatalf("list other account: %v", err)
	}
	if len(otherHooks) != 0 {
		t.Fatalf("other account webhooks = %d, want 0", len(otherHooks))
	}
}
