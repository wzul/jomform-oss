package lifecycle

// The forms table's own rules (card t_3d5a8d32).
//
// The bulk of this file is GENERATED over the table, not written out by hand:
// for every declared state and every declared event it drives Apply and pins the
// answer (a declared edge lands in the state the table names; an undeclared one
// is an *IllegalTransitionError). A hand-written case list can only cover the
// pairs someone remembered, which is how a state gets added without its rules.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestFormsMachineIsWellFormed(t *testing.T) {
	if err := FormsMachine.Validate(); err != nil {
		t.Fatalf("forms machine does not validate: %v", err)
	}
	if FormsMachine.Name != "forms" {
		t.Errorf("machine name = %q, want forms", FormsMachine.Name)
	}
	if got := MachineByName("forms"); got != FormsMachine {
		t.Errorf("MachineByName(forms) = %p, want the package's own machine %p", got, FormsMachine)
	}
}

// TestFormsMachineStatesAreTheDatabaseVocabulary pins the declared states to
// dbstore's own list, so a state cannot be added to one and silently missed in
// the other (the same guard sales_test.go puts on the sales table).
func TestFormsMachineStatesAreTheDatabaseVocabulary(t *testing.T) {
	want := map[string]bool{
		dbstore.FormStatusDraft:            true,
		dbstore.FormStatusPrivate:          true,
		dbstore.FormStatusPublished:        true,
		dbstore.FormStatusUnlisted:         true,
		dbstore.FormStateTemplateDraft:     true,
		dbstore.FormStateTemplatePublished: true,
		dbstore.FormStateTrashed:           true,
		dbstore.FormStatePurged:            true,
	}
	for state := range want {
		if _, ok := FormsMachine.States[state]; !ok {
			t.Errorf("dbstore names the state %q, the forms table does not declare it", state)
		}
	}
	for state := range FormsMachine.States {
		if !want[state] {
			t.Errorf("the forms table invents the state %q", state)
		}
	}
}

// formForState builds a row that LifecycleState() reads as the named state.
func formForState(t *testing.T, state string) *dbstore.Form {
	t.Helper()
	now := time.Now()
	switch state {
	case dbstore.FormStatusDraft:
		return &dbstore.Form{Status: dbstore.FormStatusDraft}
	case dbstore.FormStatusPrivate:
		return &dbstore.Form{Status: dbstore.FormStatusPrivate}
	case dbstore.FormStatusPublished:
		return &dbstore.Form{Status: dbstore.FormStatusPublished}
	case dbstore.FormStatusUnlisted:
		return &dbstore.Form{Status: dbstore.FormStatusUnlisted}
	case dbstore.FormStateTemplateDraft:
		return &dbstore.Form{Status: dbstore.FormStatusDraft, IsTemplate: true}
	case dbstore.FormStateTemplatePublished:
		return &dbstore.Form{Status: dbstore.FormStatusPublished, IsTemplate: true}
	case dbstore.FormStateTrashed:
		return &dbstore.Form{Status: dbstore.FormStatusPublished, DeletedAt: &now}
	case dbstore.FormStatePurged:
		// A purged row does not exist, so no read produces this state — and no
		// *dbstore.Form can ever report it. The matrix covers it with a stub
		// entity (see fixedState) instead of a row.
		return nil
	}
	t.Fatalf("no fixture for state %q", state)
	return nil
}

// fixedState is an Entity for a state no row can be in (purged). Apply only ever
// reads LifecycleState for an unguarded edge, and the purged state declares none.
type fixedState struct{ state string }

func (s fixedState) LifecycleState() string { return s.state }

// TestFormsTableCoversEveryStateEventPair is the generated matrix: every state in
// the table x every event the machine declares. It asserts four things per cell:
//
//	declared   <=> Apply succeeds
//	declared    => the result lands where the table says (and Changed is exact)
//	undeclared  => a typed *IllegalTransitionError with the stable code
//	no cell     => a panic or an untyped error
func TestFormsTableCoversEveryStateEventPair(t *testing.T) {
	events := FormsMachine.Events()
	if len(events) == 0 {
		t.Fatal("the forms machine declares no events")
	}
	for _, state := range FormsMachine.StateNames() {
		for _, event := range events {
			declared := FormsMachine.Allows(state, event)
			tr := FormsMachine.States[state].Events[event]
			f := formForState(t, state)
			if f == nil {
				// purged: no row can be in it. Apply must still refuse.
			} else if tr.Guard != nil {
				// A template publish is refused when the fixture carries no
				// gallery metadata, which would make a DECLARED edge look
				// unapplied. The guard is exercised on its own below; here the
				// fixture is made publishable so the matrix is about edges, not
				// guards.
				f = publishableForm(state)
			}
			var entity Entity = fixedState{state: state}
			if f != nil {
				entity = f
			}
			res, err := FormsMachine.Apply(entity, event)
			cell := state + "." + event

			if !declared {
				if err == nil {
					t.Errorf("%s: table declares no edge, Apply allowed it (→%q)", cell, res.To)
					continue
				}
				if !errors.Is(err, ErrIllegalTransition) {
					t.Errorf("%s: undeclared edge returned %v, want ErrIllegalTransition", cell, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: table declares the edge, Apply refused it: %v", cell, err)
				continue
			}
			want := tr.To
			if tr.Target != nil {
				want = tr.Target(f)
			}
			if res.To != want {
				t.Errorf("%s: landed in %q, table says %q", cell, res.To, want)
			}
			if res.Changed != (want != state) {
				t.Errorf("%s: Changed=%v, want %v", cell, res.Changed, want != state)
			}
			if _, ok := FormsMachine.States[res.To]; !ok {
				t.Errorf("%s: landed in undeclared state %q", cell, res.To)
			}
		}
	}
}

// publishableForm is a fixture that passes every guard, for the edge matrix.
func publishableForm(state string) *dbstore.Form {
	f := &dbstore.Form{
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"category": "Contact",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "section", "value": "Say hello"},
			},
		},
	}
	if state == dbstore.FormStateTemplateDraft || state == dbstore.FormStateTemplatePublished {
		f.IsTemplate = true
		if state == dbstore.FormStateTemplateDraft {
			f.Status = dbstore.FormStatusDraft
		}
	}
	if state == dbstore.FormStateTrashed {
		now := time.Now()
		f.Status = dbstore.FormStatusPublished
		f.DeletedAt = &now
	}
	return f
}

// TestFormsRestoreReturnsTheRowToTheStateItWasTrashedFrom is the dynamic target:
// SoftDelete writes deleted_at and NOTHING else, so a trashed published form
// comes back published and a trashed draft comes back draft.
func TestFormsRestoreReturnsTheRowToTheStateItWasTrashedFrom(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		row  *dbstore.Form
		want string
	}{
		{"published", &dbstore.Form{Status: dbstore.FormStatusPublished, DeletedAt: &now}, dbstore.FormStatusPublished},
		{"draft", &dbstore.Form{Status: dbstore.FormStatusDraft, DeletedAt: &now}, dbstore.FormStatusDraft},
		{"private", &dbstore.Form{Status: dbstore.FormStatusPrivate, DeletedAt: &now}, dbstore.FormStatusPrivate},
		{"unlisted", &dbstore.Form{Status: dbstore.FormStatusUnlisted, DeletedAt: &now}, dbstore.FormStatusUnlisted},
		{"gallery template", &dbstore.Form{Status: dbstore.FormStatusPublished, IsTemplate: true, DeletedAt: &now}, dbstore.FormStateTemplatePublished},
		{"own template", &dbstore.Form{Status: dbstore.FormStatusDraft, IsTemplate: true, DeletedAt: &now}, dbstore.FormStateTemplateDraft},
	}
	for _, c := range cases {
		res, err := FormsMachine.Apply(c.row, FormEventRestore)
		if err != nil {
			t.Fatalf("%s: restore: %v", c.name, err)
		}
		if res.To != c.want {
			t.Errorf("%s: restored into %q, want %q", c.name, res.To, c.want)
		}
	}
}

// TestFormsPurgeIsReachableOnlyFromTheTrash pins the irreversible move: no live
// state declares purge, so a live row cannot be destroyed by a mistyped call.
func TestFormsPurgeIsReachableOnlyFromTheTrash(t *testing.T) {
	for _, state := range FormsMachine.StateNames() {
		if state == dbstore.FormStateTrashed {
			continue
		}
		if FormsMachine.Allows(state, FormEventPurge) {
			t.Errorf("%q declares purge — the store only purges a trashed row, so the table must not offer it", state)
		}
	}
	if !FormsMachine.Allows(dbstore.FormStateTrashed, FormEventPurge) {
		t.Error("the trashed state does not declare purge")
	}
}

// soldTrashedForm is a trashed row that knows it has sales. The count is not a
// column of a forms row (it lives in `sales` and is carried only by the trash
// reads, through dbstore.Form.FormSalesCount), so the guard's refusing arm is
// exercised through the same interface the store's row implements.
type soldTrashedForm struct {
	*dbstore.Form
	sales int64
}

func (s soldTrashedForm) FormSalesCount() (int64, bool) { return s.sales, true }

// TestFormsPurgeGuardRefusesASoldForm pins the table's ONE irreversible guard:
// the store refuses to destroy a form that recorded a sale (409 form.hasSales),
// so the table must refuse it too — otherwise an agent reading
// available_events is told it may purge and then hits a refusal it could not
// have known about.
func TestFormsPurgeGuardRefusesASoldForm(t *testing.T) {
	now := time.Now()
	sold := soldTrashedForm{
		Form:  &dbstore.Form{Status: dbstore.FormStatusPublished, DeletedAt: &now},
		sales: 1,
	}
	_, err := FormsMachine.Apply(sold, FormEventPurge)
	if err == nil {
		t.Fatal("a form with sales was purged by the table")
	}
	if !errors.Is(err, ErrTransitionRefused) {
		t.Errorf("refusal = %v, want ErrTransitionRefused (a stated rule, not a missing edge)", err)
	}
	if !strings.Contains(err.Error(), "sales") {
		t.Errorf("refusal %q does not state the rule", err.Error())
	}
	for _, ev := range FormsMachine.Available(sold) {
		if ev.Event != FormEventPurge {
			continue
		}
		if ev.Enabled {
			t.Error("a sold form offers an ENABLED purge")
		}
		if !strings.Contains(ev.Reason, "sales") {
			t.Errorf("disabled purge reason = %q, want the sales rule", ev.Reason)
		}
	}

	// An unsold trashed form is purged exactly as before: a row that carries no
	// sales is the shape the door destroys.
	clean := &dbstore.Form{Status: dbstore.FormStatusPublished, DeletedAt: &now}
	res, err := FormsMachine.Apply(clean, FormEventPurge)
	if err != nil {
		t.Fatalf("purging an unsold trashed form: %v", err)
	}
	if res.To != dbstore.FormStatePurged {
		t.Errorf("purge landed in %q, want %q", res.To, dbstore.FormStatePurged)
	}
}

// TestFormsPublishIsNotGuardedByTheProductRule pins a DELIBERATE non-guard.
// Publishing a payment form with nothing to sell is NOT a refusal: the door
// answers 409 with the product code and accepts the same call again with
// `acknowledge` (dbstore.PublishWarning). A guard here would hide a button the
// API accepts, so the table must keep the edge available — and the buyer-side
// truth is carried by FormOrders instead.
func TestFormsPublishIsNotGuardedByTheProductRule(t *testing.T) {
	draft := &dbstore.Form{
		Status: dbstore.FormStatusDraft,
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "name"}}},
	}
	if code, _ := dbstore.PublishWarning(draft, dbstore.FormStatusPublished, nil); code == dbstore.PublishReady {
		t.Skip("the publish door no longer warns about this shape — the non-guard's premise changed")
	}
	if _, err := FormsMachine.Apply(draft, FormEventPublish); err != nil {
		t.Fatalf("the table refused a publish the door accepts with acknowledgement: %v", err)
	}
	for _, ev := range FormsMachine.Available(draft) {
		if ev.Event == FormEventPublish && !ev.Enabled {
			t.Errorf("publish is disabled for an empty payment form (%q) — the dashboard would hide a button the API accepts", ev.Reason)
		}
	}
	// The consequence lives in the ONE derived predicate, with the same code the
	// publish door uses — so "the button exists" and "the page is dead" are both
	// stated, by the surfaces that own each question.
	live := &dbstore.Form{Status: dbstore.FormStatusPublished, Config: draft.Config}
	got := FormOrders(live, FormOrderEnv{Entries: -1})
	if got.Accepting || got.Code != FormOrderNoProducts {
		t.Errorf("a sold-nothing payment form reads %+v, want code %q", got, FormOrderNoProducts)
	}
}

// TestFormsNoArchiveAsTemplateEdge pins a DELIBERATE omission. Saving a form as
// a template clones it (FormStore.CreateTemplate); the source row never moves.
// An `archive_as_template` edge would therefore offer an action no door
// implements — the lying table this package exists to prevent. If the product
// ever grows an in-place conversion, this test is the place that says the table
// must learn it.
func TestFormsNoArchiveAsTemplateEdge(t *testing.T) {
	for _, event := range FormsMachine.Events() {
		if strings.Contains(event, "template") && event != "publish" && event != "unpublish" {
			t.Errorf("the forms table declares %q, but no write path converts a form into a template in place", event)
		}
	}
}

// TestFormsPublishGuardRefusesATemplateWithoutGalleryMetadata exercises the one
// row-local guard: the same rule setStatusSQL enforces (issue #594).
func TestFormsPublishGuardRefusesATemplateWithoutGalleryMetadata(t *testing.T) {
	cases := []struct {
		name    string
		config  map[string]any
		refused bool
	}{
		{"no category", map[string]any{"blocks": []any{
			map[string]any{"type": "section", "value": "Hello"},
		}}, true},
		{"blank category", map[string]any{"category": "   ", "blocks": []any{
			map[string]any{"type": "section", "value": "Hello"},
		}}, true},
		{"non-string category", map[string]any{"category": 7.0, "blocks": []any{
			map[string]any{"type": "section", "value": "Hello"},
		}}, true},
		{"no description block", map[string]any{"category": "Contact", "blocks": []any{
			map[string]any{"type": "name", "value": "Your name"},
		}}, true},
		{"whitespace description", map[string]any{"category": "Contact", "blocks": []any{
			map[string]any{"type": "section", "value": "   "},
		}}, true},
		{"complete", map[string]any{"category": "Contact", "blocks": []any{
			map[string]any{"type": "section", "value": "Hello"},
		}}, false},
		{"legacy text block", map[string]any{"category": "Contact", "blocks": []any{
			map[string]any{"type": "text", "value": "Hello"},
		}}, false},
	}
	for _, c := range cases {
		f := &dbstore.Form{Status: dbstore.FormStatusDraft, IsTemplate: true, Config: c.config}
		_, err := FormsMachine.Apply(f, FormEventPublish)
		if c.refused {
			if err == nil {
				t.Errorf("%s: template published with no gallery card", c.name)
				continue
			}
			if !errors.Is(err, ErrTransitionRefused) {
				t.Errorf("%s: refusal = %v, want ErrTransitionRefused", c.name, err)
			}
			if !strings.Contains(err.Error(), "category and one description block") {
				t.Errorf("%s: refusal %q does not state the rule", c.name, err.Error())
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: complete template refused: %v", c.name, err)
		}
	}
}

// TestFormsAvailableEventsAreWhatTheDashboardRenders is the UX contract: the
// list is what the server says a row accepts, so a published-and-unsellable form
// offers Unpublish/Delete and NOT a Publish button that would be a no-op.
func TestFormsAvailableEventsAreWhatTheDashboardRenders(t *testing.T) {
	published := &dbstore.Form{Status: dbstore.FormStatusPublished}
	got := FormsMachine.Available(published)
	byName := map[string]AvailableEvent{}
	for _, ev := range got {
		byName[ev.Event] = ev
	}
	if _, ok := byName[FormEventPublish]; ok {
		t.Error("a published form offers publish — it is a no-op, not an action")
	}
	for _, want := range []string{FormEventUnpublish, FormEventDelete} {
		ev, ok := byName[want]
		if !ok {
			t.Errorf("a published form does not offer %s", want)
			continue
		}
		if !ev.Enabled {
			t.Errorf("%s is disabled on a published form: %s", want, ev.Reason)
		}
		if ev.LabelKey == "" {
			t.Errorf("%s has no label key, so the dashboard cannot render a button", want)
		}
	}

	// A draft offers Publish and Delete, and NOT Unpublish (also a no-op).
	draft := FormsMachine.Available(&dbstore.Form{Status: dbstore.FormStatusDraft})
	names := map[string]bool{}
	for _, ev := range draft {
		names[ev.Event] = true
	}
	if !names[FormEventPublish] || !names[FormEventDelete] {
		t.Errorf("a draft offers %v, want publish and delete", names)
	}
	if names[FormEventUnpublish] {
		t.Error("a draft offers unpublish — it is already a draft")
	}

	// A trashed row offers restore and purge, and nothing else.
	now := time.Now()
	trashed := FormsMachine.Available(&dbstore.Form{Status: dbstore.FormStatusPublished, DeletedAt: &now})
	names = map[string]bool{}
	for _, ev := range trashed {
		names[ev.Event] = true
	}
	if len(names) != 2 || !names[FormEventRestore] || !names[FormEventPurge] {
		t.Errorf("a trashed form offers %v, want restore and purge only", names)
	}

	// Every label key has the i18n KEY SHAPE the dashboard resolves.
	for _, state := range FormsMachine.StateNames() {
		for _, ev := range FormsMachine.Available(formForState(t, state)) {
			if ev.LabelKey != "" && !strings.Contains(ev.LabelKey, ".") {
				t.Errorf("%s.%s label key %q is not a dotted i18n key", state, ev.Event, ev.LabelKey)
			}
		}
	}
}

// TestFormsDisabledActionCarriesAReason: a guard refusal is reported as
// enabled=false WITH its sentence, not hidden — the caller asked what it can do.
func TestFormsDisabledActionCarriesAReason(t *testing.T) {
	f := &dbstore.Form{Status: dbstore.FormStatusDraft, IsTemplate: true, Config: map[string]any{}}
	for _, ev := range FormsMachine.Available(f) {
		if ev.Event != FormEventPublish {
			continue
		}
		if ev.Enabled {
			t.Fatal("publishing a template with no gallery metadata is offered as enabled")
		}
		if !strings.Contains(ev.Reason, "category and one description block") {
			t.Errorf("disabled publish reason = %q, want the gallery rule", ev.Reason)
		}
		return
	}
	t.Fatal("the draft template does not offer publish at all, so the guard is unreachable")
}

// TestFormOrdersMatrix is the generated (state x condition) matrix for the ONE
// derived buyer predicate. Every row is a shape the product has, and the code is
// pinned so two surfaces cannot answer differently.
func TestFormOrdersMatrix(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	limit := 5
	products := []any{map[string]any{"id": "p-1"}}
	blocksWithProduct := []any{
		map[string]any{"id": "b1", "type": "name"},
		map[string]any{"id": "b2", "type": "product", "product_id": "p-1"},
	}
	blocksNoProduct := []any{
		map[string]any{"id": "b1", "type": "name"},
		map[string]any{"id": "b2", "type": "product", "product_id": ""},
	}

	cases := []struct {
		name string
		form *dbstore.Form
		env  FormOrderEnv
		code string
		// blockID is the product block the answer names, when it names one.
		blockID string
	}{
		{
			name: "nil row",
			form: nil,
			code: FormOrderUnknown,
		},
		{
			name: "trashed",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, DeletedAt: &past},
			code: FormOrderTrashed,
		},
		{
			name: "draft",
			form: &dbstore.Form{Status: dbstore.FormStatusDraft},
			code: FormOrderNotPublished,
		},
		{
			name: "draft template",
			form: &dbstore.Form{Status: dbstore.FormStatusDraft, IsTemplate: true},
			code: FormOrderNotPublished,
		},
		{
			name: "scheduled",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, StartAt: &future,
				Config: map[string]any{"blocks": blocksWithProduct}},
			code: FormOrderScheduled,
		},
		{
			name: "closed",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, EndAt: &past,
				Config: map[string]any{"blocks": blocksWithProduct}},
			code: FormOrderClosed,
		},
		{
			name: "entry limit reached",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, EntryLimit: &limit,
				Config: map[string]any{"blocks": blocksWithProduct}},
			env:  FormOrderEnv{Entries: 5},
			code: FormOrderEntryLimitReached,
		},
		{
			name: "entry limit not reached",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, EntryLimit: &limit,
				Config: map[string]any{"blocks": blocksWithProduct}},
			env:  FormOrderEnv{Entries: 4},
			code: FormOrderAccepting,
		},
		{
			name: "entry limit unknown skips the arm",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, EntryLimit: &limit,
				Config: map[string]any{"blocks": blocksWithProduct}},
			env:  FormOrderEnv{Entries: -1},
			code: FormOrderAccepting,
		},
		{
			name: "payment form with nothing to sell",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "name"}}}},
			code: FormOrderNoProducts,
		},
		{
			name: "payment form with an unpointed product block",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"blocks": blocksNoProduct}},
			code:    FormOrderNoProductSelected,
			blockID: "b2",
		},
		{
			name: "payment form whose product row was purged",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"blocks": blocksWithProduct}},
			env:     FormOrderEnv{LiveProductIDs: map[string]bool{"p-2": true}},
			code:    FormOrderNoProductSelected,
			blockID: "b2",
		},
		{
			name: "payment form with a sellable block",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"blocks": blocksWithProduct}},
			code: FormOrderAccepting,
		},
		{
			name: "payment form with a legacy snapshot",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"products": products}},
			code: FormOrderAccepting,
		},
		{
			name: "unlisted is live",
			form: &dbstore.Form{Status: dbstore.FormStatusUnlisted,
				Config: map[string]any{"blocks": blocksWithProduct}},
			code: FormOrderAccepting,
		},
		{
			name: "private is live (behind its gate)",
			form: &dbstore.Form{Status: dbstore.FormStatusPrivate,
				Config: map[string]any{"blocks": blocksWithProduct}},
			code: FormOrderAccepting,
		},
		{
			name: "response form needs no product",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"mode": "response", "blocks": []any{map[string]any{"id": "b1", "type": "name"}}}},
			code: FormOrderAccepting,
		},
		{
			name: "enabled menu form sells through its menu",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished,
				Config: map[string]any{"mode": "response", "menu": map[string]any{"enabled": true}}},
			code: FormOrderAccepting,
		},
		{
			name: "published gallery template with a sellable block",
			form: &dbstore.Form{Status: dbstore.FormStatusPublished, IsTemplate: true,
				Config: map[string]any{"blocks": blocksWithProduct}},
			code: FormOrderAccepting,
		},
	}
	for _, c := range cases {
		c.env.Now = now
		got := FormOrders(c.form, c.env)
		if got.Code != c.code {
			t.Errorf("%s: code = %q (%s), want %q", c.name, got.Code, got.Reason, c.code)
			continue
		}
		if got.Accepting != (c.code == FormOrderAccepting) {
			t.Errorf("%s: accepting = %v with code %q", c.name, got.Accepting, got.Code)
		}
		if got.Accepting && got.Reason != "" {
			t.Errorf("%s: an accepting form carries a reason (%q)", c.name, got.Reason)
		}
		if !got.Accepting && got.Reason == "" {
			t.Errorf("%s: a refused form has no reason", c.name)
		}
		if got.BlockID != c.blockID {
			t.Errorf("%s: block id = %q, want %q", c.name, got.BlockID, c.blockID)
		}
	}
}

// TestFormOrdersProductCodesAreThePublishCodes pins the two product codes to
// dbstore's publish-warning vocabulary: the read surface and the publish refusal
// must name the same defect with the same token, or the dashboard's "fix this
// block" link detaches from the API's error code.
func TestFormOrdersProductCodesAreThePublishCodes(t *testing.T) {
	if FormOrderNoProducts != dbstore.PublishWarningNoProducts {
		t.Errorf("FormOrderNoProducts = %q, dbstore says %q", FormOrderNoProducts, dbstore.PublishWarningNoProducts)
	}
	if FormOrderNoProductSelected != dbstore.PublishWarningNoProductSelected {
		t.Errorf("FormOrderNoProductSelected = %q, dbstore says %q",
			FormOrderNoProductSelected, dbstore.PublishWarningNoProductSelected)
	}
	if !FormOrderIsProductCode(FormOrderNoProducts) || !FormOrderIsProductCode(FormOrderNoProductSelected) {
		t.Error("the product codes are not reported as product codes")
	}
	for _, code := range []string{FormOrderAccepting, FormOrderTrashed, FormOrderScheduled, FormOrderClosed, FormOrderEntryLimitReached} {
		if FormOrderIsProductCode(code) {
			t.Errorf("%q is not a product code but is reported as one", code)
		}
	}
}

// TestFormOrdersAgreesWithThePublishWarning walks the SHAPES the publish guard
// and the derived read both judge, and pins them to the same verdict: a form the
// publish route would refuse silently (or warn about) is a form whose buyer page
// is dead, which is the whole point of one predicate.
func TestFormOrdersAgreesWithThePublishWarning(t *testing.T) {
	shapes := []map[string]any{
		nil,
		{"blocks": []any{map[string]any{"id": "b1", "type": "name"}}},
		{"blocks": []any{map[string]any{"id": "b2", "type": "product", "product_id": ""}}},
		{"blocks": []any{map[string]any{"id": "b2", "type": "product", "product_id": "p-1"}}},
		{"products": []any{map[string]any{"id": "p-1"}}},
		{"blocks": []any{map[string]any{"id": "o1", "type": "option",
			"choices": []any{map[string]any{"value": "a"}}}}},
		{"mode": "response"},
	}
	for i, cfg := range shapes {
		draft := &dbstore.Form{Status: dbstore.FormStatusDraft, Config: cfg}
		warn, _ := dbstore.PublishWarning(draft, dbstore.FormStatusPublished, nil)

		// The same row once live: the read surface's verdict.
		live := &dbstore.Form{Status: dbstore.FormStatusPublished, Config: cfg}
		got := FormOrders(live, FormOrderEnv{Entries: -1, Now: time.Now()})

		switch warn {
		case dbstore.PublishWarningNoProducts, dbstore.PublishWarningNoProductSelected:
			if got.Accepting {
				t.Errorf("shape %d: publish would warn (%s) but the read says accepting", i, warn)
			}
			if got.Code != warn {
				t.Errorf("shape %d: read code = %q, publish warning = %q", i, got.Code, warn)
			}
		default:
			if !got.Accepting {
				t.Errorf("shape %d: publish needs no warning but the read refuses: %s", i, got.Code)
			}
		}
	}
}
