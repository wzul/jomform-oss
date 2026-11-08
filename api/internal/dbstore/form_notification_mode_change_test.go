package dbstore

// Issue #597's defect, surviving on the MODE-CHANGE path (card `t_a58848bd`,
// found by QA on `t_f37d710d` while driving the live response-submit route).
//
// THE BUG. `EnsureDefault` picks the default rule's event from the form's mode
// AT INSERT TIME ONLY, and the mode-aware call exists only on the CREATE paths.
// A form created as a payment form and later switched to response mode — the
// builder's "Form mode" control, i.e. a plain config write through
// `FormStore.Update` — therefore keeps the `sale.paid` default it was born with.
// `FireFormNotifications` asks `ListByFormEvent(formID, "response.submitted")`,
// matches ZERO rows, and the merchant is never told anyone responded. #597's
// repair migration (000062) re-pointed the rows that existed When it ran; it
// cannot reach a row a mode change creates afterwards. Prod measured three such
// rows on `test-wan` (cubaan, test, new-form-030b — all mode=response, all
// carrying an is_default `sale.paid` rule, all created AFTER the migration).
//
// THE GUARD below is the card's acceptance criterion, written as an assertion
// rather than a description: a RESPONSE-mode form must have a rule that fires
// `response.submitted`, and when it does not, the failure names the dead shape
// it found (`sale.paid` on a form that can never record a sale). It is the
// assertion that goes RED the moment the reconcile step is removed — which
// `api/scripts/biteproof_form_notification_mode_change.py` proves by reverting
// it on disk, rather than asserting it here in prose.

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// requireLiveNotificationForMode is THE GUARD. It reads the form's mode through
// the one definition (FormMode), asks the store for exactly the rules that event
// can fire, and fails when there are none — naming the enabled events it did
// find, so a `sale.paid` rule on a response form is named in the failure rather
// than left to be inferred.
//
// It deliberately reads through `ListByFormEvent`, the SAME call the send path
// makes (`FireFormNotifications`), so it can never pass while the real engine
// finds nothing: a guard that read `ListByForm` and matched the event itself
// could agree with the store while disagreeing with the sender.
func requireLiveNotificationForMode(t *testing.T, ctx context.Context, store *FormNotificationStore, f *Form) []*FormNotification {
	t.Helper()
	mode := FormMode(f)
	_, event, _, _ := defaultNotificationFor(mode)
	live, err := store.ListByFormEvent(ctx, f.ID, event)
	if err != nil {
		t.Fatalf("list rules for event %q: %v", event, err)
	}
	if len(live) == 0 {
		all, lerr := store.ListByForm(ctx, f.ID)
		if lerr != nil {
			t.Fatalf("list all rules: %v", lerr)
		}
		var enabled []string
		for _, r := range all {
			if r.Enabled {
				enabled = append(enabled, r.Event)
			}
		}
		t.Errorf("form %s is mode=%q but has NO enabled rule that fires %q — the merchant is never "+
			"told anyone responded. Enabled rule events: %v (a `sale.paid` rule on a response form can "+
			"never fire: a response records a submission, never a sale)",
			f.Slug, mode, event, enabled)
	}
	return live
}

// seedDefaultNotification creates a form through the store and installs the
// default rule the way the production create path does, then returns the form
// read back from the database (so Update's own post-write read is exercised on
// a row rather than on a struct the test guessed at).
func seedDefaultNotification(t *testing.T, ctx context.Context, forms *FormStore, notif *FormNotificationStore, acc, slug string, cfg map[string]any) *Form {
	t.Helper()
	f := &Form{
		AccountID: acc, Slug: slug, Title: slug,
		Config: cfg, Theme: map[string]any{}, Status: FormStatusDraft,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form %s: %v", slug, err)
	}
	// Production calls EnsureDefault with the freshly created struct; it reads
	// the mode from that config, exactly as this does.
	if err := notif.EnsureDefault(ctx, f); err != nil {
		t.Fatalf("EnsureDefault %s: %v", slug, err)
	}
	stored, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("read back %s: %v", slug, err)
	}
	return stored
}

// TestModeChangeToResponseRePointsTheUneditedDefault is the defect pinned end
// to end at the store seam: created as a payment form, switched to response
// through the real write path, and the merchant must end up with a rule that
// actually fires.
func TestModeChangeToResponseRePointsTheUneditedDefault(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-pay-resp-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})

	// The pre-condition the defect is built on: a payment form's default IS
	// sale.paid. If this ever stopped being true the rest of the test would be
	// measuring a different bug.
	before, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(before) != 1 || before[0].Event != NotifyEventSalePaid || !before[0].IsDefault {
		t.Fatalf("pre-condition: want exactly one is_default sale.paid rule, got %+v", before)
	}

	// The merchant switches the form to response mode — the real write path.
	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to response mode: %v", err)
	}
	stored, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if FormMode(stored) != FormModeResponse {
		t.Fatalf("stored mode = %q, want response (the write did not land, so nothing else here is measurable)",
			FormMode(stored))
	}

	live := requireLiveNotificationForMode(t, ctx, notif, stored)
	if len(live) != 1 {
		t.Fatalf("want exactly 1 live rule, got %d (%+v)", len(live), live)
	}
	if live[0].Event != NotifyEventResponseSubmitted {
		t.Errorf("live rule event = %q, want %q", live[0].Event, NotifyEventResponseSubmitted)
	}
	// The row must be the SYSTEM's response default, not the sale one renamed:
	// a sale-flavoured message on a response has no ref_code, no amount and no
	// items, so those placeholders would be mailed as literal text.
	_, _, wantSubject, wantBody := DefaultNotification(FormModeResponse)
	if live[0].Subject != wantSubject || live[0].Body != wantBody {
		t.Errorf("re-pointed rule does not carry the response default template.\nsubject=%q\nwant:   %q\nbody=%q\nwant:\n%s",
			live[0].Subject, wantSubject, live[0].Body, wantBody)
	}
	for _, bad := range []string{"{{ref_code}}", "{{amount}}", "{{items}}", "{{buyer_name}}"} {
		if strings.Contains(live[0].Subject, bad) || strings.Contains(live[0].Body, bad) {
			t.Errorf("the re-pointed rule still uses the sale placeholder %s, which has no value on a response",
				bad)
		}
	}
	// Exactly one rule: the reconcile must MOVE the default, never add a second
	// rule beside it (a duplicate would send the merchant two emails).
	all, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list all rules: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("response form has %d notification rules, want exactly 1 (%+v)", len(all), all)
	}
}

// TestModeChangeBackToPaymentRePointsTheDefault is the mirror direction, and it
// is the regression control for the naive fix: a reconcile that only knows
// "response forms need response.submitted" would leave a payment form silent on
// its own sale — the worse failure, because a payment form is the default and
// the common case.
func TestModeChangeBackToPaymentRePointsTheDefault(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-resp-pay-"+acc[:8],
		map[string]any{"mode": "response", "blocks": []any{}})
	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "payment", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to payment mode: %v", err)
	}
	stored, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	live := requireLiveNotificationForMode(t, ctx, notif, stored)
	if len(live) != 1 || live[0].Event != NotifyEventSalePaid {
		t.Fatalf("payment form's live rules = %+v, want exactly one sale.paid — a sales form "+
			"that stopped notifying on a sale is the worse regression", live)
	}
	// The re-pointed row stores NO template (card t_1768cb11): the event decides
	// which dictionary kind renders at send time, and that kind is the one that
	// carries the sale placeholders. Assert the resolution rather than the stored
	// bytes, because an unedited default is now exactly the empty pair.
	if live[0].Subject != "" || live[0].Body != "" {
		t.Errorf("re-pointed payment rule stores text: subject=%q body=%q",
			live[0].Subject, live[0].Body)
	}
	sale := emailtext.Text(emailtext.En, emailtext.NotificationSale, nil)
	if !strings.Contains(sale.Subject, "{{ref_code}}") || !strings.Contains(sale.Body, "RM {{amount}}") {
		t.Errorf("the sale template the re-pointed rule now renders lost its placeholders:\n"+
			"subject=%q\nbody=%q", sale.Subject, sale.Body)
	}
}

// TestModeChangeLeavesAMerchantEditedDefaultAlone is the contract 000062 also
// used, and the reason the reconcile may compare bytes at all: only a rule the
// SYSTEM wrote, byte for byte, is re-pointed. A merchant who reworded his
// default made a decision, and silently rewriting it would be worse than the
// bug — his words would go out under a heading he never chose.
func TestModeChangeLeavesAMerchantEditedDefaultAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-edited-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	rules, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	const merchantSubject = "Someone paid — go and post it"
	if err := notif.Update(ctx, f.ID, rules[0].ID, &FormNotification{
		Name: rules[0].Name, Event: rules[0].Event, ToEmail: rules[0].ToEmail,
		Subject: merchantSubject, Body: rules[0].Body, Enabled: true,
	}); err != nil {
		t.Fatalf("merchant edits the rule: %v", err)
	}

	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to response mode: %v", err)
	}
	stored, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	after, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("want exactly 1 rule, got %d (%+v)", len(after), after)
	}
	if after[0].Subject != merchantSubject {
		t.Errorf("the merchant's own subject was rewritten by the mode change:\n got: %q\nwant: %q",
			after[0].Subject, merchantSubject)
	}
	if after[0].Event != NotifyEventSalePaid {
		t.Errorf("the merchant's rule was re-pointed: event = %q, want it left on %q — his edit is his "+
			"decision, and the reconcile's scope is only a rule the system still owns",
			after[0].Event, NotifyEventSalePaid)
	}
	// Stated so the residual is visible rather than implied: this form now has a
	// sale.paid rule on a response form. That is NOT a defect of the reconcile —
	// it is the merchant's own rule, and the dashboard shows him its event. The
	// guard above therefore applies to the UNEDITED case only.
	_ = stored
}

// TestModeChangeLeavesReAddressedDefaultAlone covers the second half of "still
// the system's rule": the merchant may change only the RECIPIENT (send it to his
// ops inbox). That is a decision too, so the recipient is part of the byte
// comparison — a reconcile that ignored to_email would take the rule away from
// the inbox he chose.
func TestModeChangeLeavesReAddressedDefaultAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-readdr-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	rules, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if err := notif.Update(ctx, f.ID, rules[0].ID, &FormNotification{
		Name: rules[0].Name, Event: rules[0].Event, ToEmail: "ops@example.test",
		Subject: rules[0].Subject, Body: rules[0].Body, Enabled: true,
	}); err != nil {
		t.Fatalf("merchant re-addresses the rule: %v", err)
	}
	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to response mode: %v", err)
	}
	after, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after: %v", err)
	}
	if len(after) != 1 || after[0].ToEmail != "ops@example.test" || after[0].Event != NotifyEventSalePaid {
		t.Fatalf("a re-addressed default was seized by the mode change: %+v", after)
	}
}

// TestModeChangeLeavesAMerchantAuthoredRuleAlone: the `is_default` flag is the
// ONLY thing in the database that says "the system wrote this" for a rule whose
// bytes are otherwise identical, so this case is built to differ in that flag
// and nothing else: the merchant deleted the default and re-added a rule with
// the system's exact event, recipient, subject and body through the editor
// (which never sets is_default). A reconcile keyed on the bytes alone would take
// his rule and rewrite it — and it would then be re-pointed again on the next
// save, because nothing records that he owns it.
func TestModeChangeLeavesAMerchantAuthoredRuleAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-authored-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	rules, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	// Delete the system default (his opt-out) and re-add the same text as HIS
	// OWN rule. FormNotificationStore.Create does not set is_default, which is
	// exactly what the notification editor does.
	if err := notif.Delete(ctx, f.ID, rules[0].ID); err != nil {
		t.Fatalf("delete default: %v", err)
	}
	mine := &FormNotification{
		FormID: f.ID, Name: rules[0].Name, Event: rules[0].Event,
		ToEmail: rules[0].ToEmail, Subject: rules[0].Subject, Body: rules[0].Body,
		Enabled: true,
	}
	if err := notif.Create(ctx, mine); err != nil {
		t.Fatalf("create own rule: %v", err)
	}
	if mine.IsDefault {
		t.Fatalf("test setup: the merchant's own rule is flagged is_default (%+v)", mine)
	}

	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to response mode: %v", err)
	}
	after, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("want exactly 1 rule, got %d (%+v)", len(after), after)
	}
	if after[0].ID != mine.ID || after[0].Event != NotifyEventSalePaid || after[0].IsDefault {
		t.Fatalf("the merchant's own rule was taken by the mode change: %+v", after)
	}
	if after[0].Subject != rules[0].Subject || after[0].Body != rules[0].Body {
		t.Errorf("the merchant's own rule was rewritten:\nsubject=%q\nbody=%q",
			after[0].Subject, after[0].Body)
	}
}

// TestModeChangeKeepsAnOptedOutFormSilent: a form whose merchant deleted every
// rule keeps zero. EnsureDefault's opt-out contract is not reversed by the
// reconcile — nothing is INSERTed, only an existing system-owned default is
// re-pointed — so this pins that the fix cannot "helpfully" re-add a rule a
// merchant removed.
func TestModeChangeKeepsAnOptedOutFormSilent(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-optout-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	rules, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if err := notif.Delete(ctx, f.ID, rules[0].ID); err != nil {
		t.Fatalf("delete default: %v", err)
	}
	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to response mode: %v", err)
	}
	after, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("the reconcile re-created a rule the merchant deleted (opt-out reversed): %+v", after)
	}
}

// TestModeChangeIsIdempotentAndQuietWhenAlreadyCorrect: a form already in the
// right mode with the right default must not be touched, and a repeat save must
// not churn the row. The second half is what keeps a page save from writing a
// new updated_at on every keystroke-triggered save.
func TestModeChangeIsIdempotentAndQuietWhenAlreadyCorrect(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-quiet-"+acc[:8],
		map[string]any{"mode": "response", "blocks": []any{}})
	before, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	changed, err := notif.ReconcileDefault(ctx, f)
	if err != nil {
		t.Fatalf("ReconcileDefault: %v", err)
	}
	if changed {
		t.Errorf("ReconcileDefault reported a change on a form that was already correct")
	}
	after, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after: %v", err)
	}
	if len(after) != 1 || after[0].Event != NotifyEventResponseSubmitted {
		t.Fatalf("rules = %+v, want the untouched response default", after)
	}
	if !after[0].UpdatedAt.Equal(before[0].UpdatedAt) {
		t.Errorf("an already-correct rule was rewritten (updated_at moved %v -> %v)",
			before[0].UpdatedAt, after[0].UpdatedAt)
	}
	// The same answer through the public seam: a config save that does not move
	// the mode leaves the rule alone.
	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{"x"}}, nil); err != nil {
		t.Fatalf("unrelated config save: %v", err)
	}
	idem, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after save: %v", err)
	}
	if len(idem) != 1 || !idem[0].UpdatedAt.Equal(before[0].UpdatedAt) {
		t.Errorf("an unrelated config save rewrote the notification rule: %+v", idem)
	}
}

// TestReconcileDefaultReportsWhetherItChangedTheRow pins the boolean the
// store-level caller uses, in both directions — a function that always answered
// "false" would make every caller believe the rule was already correct.
func TestReconcileDefaultReportsWhetherItChangedTheRow(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-bool-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	// The struct must carry the NEW mode: ReconcileDefault reads the mode from
	// the form it is given, which is how the caller's own object decides.
	f.Config["mode"] = "response"
	changed, err := notif.ReconcileDefault(ctx, f)
	if err != nil {
		t.Fatalf("ReconcileDefault: %v", err)
	}
	if !changed {
		t.Errorf("ReconcileDefault reported no change on a stale sale.paid default (it did re-point it, or it did not)")
	}
	changedAgain, err := notif.ReconcileDefault(ctx, f)
	if err != nil {
		t.Fatalf("second ReconcileDefault: %v", err)
	}
	if changedAgain {
		t.Errorf("a second ReconcileDefault on the same form reported a change — the rule was moved twice")
	}
}

// TestModeChangeRePointsARelabelledDefault pins the direction the follow-up card
// `t_4f439f55` settled: the rule's NAME is a dashboard label, not a decision
// about the message, so a merchant who renamed his default and THEN switched the
// form's mode must still end up with a rule that fires.
//
// THE GAP THIS CLOSES. The reconcile used to require `AND name=$7` in its WHERE
// while migration 000062 — the contract it claims to match — has no `name`
// predicate at all (it SETS the name). So the reconcile was STRICTER than the
// migration: a re-labelled default was repaired by 000062 but left dead by the
// reconcile, and the merchant who renamed the rule and then switched payment →
// response kept an enabled `is_default` `sale.paid` rule on a form that records
// submissions. `ListByFormEvent(formID, "response.submitted")` matched zero rows
// and he was never told anyone responded — the identical symptom this file
// exists to close, reachable by one extra sequence.
//
// The write path is the real one: the Name field in form settings posts through
// `PUT .../notifications/{id}` → `FormNotificationStore.Update`, which — like
// this test — never sets `is_default`, so the row stays the system's default and
// only its label moved.
func TestModeChangeRePointsARelabelledDefault(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	forms := NewFormStore(p)
	notif := NewFormNotificationStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := seedDefaultNotification(t, ctx, forms, notif, acc, "mc-relabel-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	rules, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("pre-condition: want 1 default rule, got %d (%+v)", len(rules), rules)
	}
	// The merchant renames the label and touches nothing else. Every other byte
	// stays the system's, which is exactly what makes the row still his default.
	const merchantLabel = "Pesanan masuk"
	if err := notif.Update(ctx, f.ID, rules[0].ID, &FormNotification{
		Name: merchantLabel, Event: rules[0].Event, ToEmail: rules[0].ToEmail,
		Subject: rules[0].Subject, Body: rules[0].Body, Enabled: true,
	}); err != nil {
		t.Fatalf("merchant renames the default's label: %v", err)
	}
	relabelled, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list rules after rename: %v", err)
	}
	if relabelled[0].Name != merchantLabel || !relabelled[0].IsDefault {
		t.Fatalf("pre-condition: the rename did not land on an is_default row: %+v", relabelled[0])
	}

	// Now the mode moves. The unedited-bytes default must be re-pointed even
	// though its label is no longer the system's.
	if err := forms.Update(ctx, acc, f.ID, f.Title,
		map[string]any{"mode": "response", "blocks": []any{}}, nil); err != nil {
		t.Fatalf("switch to response mode: %v", err)
	}
	stored, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if FormMode(stored) != FormModeResponse {
		t.Fatalf("stored mode = %q, want response", FormMode(stored))
	}

	live := requireLiveNotificationForMode(t, ctx, notif, stored)
	if len(live) != 1 {
		t.Fatalf("want exactly 1 live rule, got %d (%+v) — a re-labelled default was left dead "+
			"by the mode change (card t_4f439f55)", len(live), live)
	}
	if live[0].Event != NotifyEventResponseSubmitted {
		t.Fatalf("live rule event = %q, want %q", live[0].Event, NotifyEventResponseSubmitted)
	}
	// The label the merchant typed must not be left on a rule that now says
	// something else: the SET re-points the name with the event, so the label and
	// the event cannot disagree in the dashboard.
	wantName, _, _, _ := defaultNotificationFor(FormModeResponse)
	if live[0].Name != wantName {
		t.Errorf("the re-pointed rule kept the merchant's stale label %q; want the system's %q for "+
			"the event it now fires", live[0].Name, wantName)
	}
	all, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list all rules: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("the reconcile must MOVE the default, not add a second rule; got %d (%+v)", len(all), all)
	}
}
