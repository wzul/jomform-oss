// The forms machine, and the ONE derived answer to "can this form take an
// order right now".
//
// THE TWO HALVES OF THE SAME DEFECT.
//
// (1) The lifecycle. A forms row has a status CHECK (draft|private|published|
// unlisted) and THREE hidden state fields on the same row — `deleted_at`,
// `is_template`, and the schedule/limit restrictions — so which action a form
// accepts was decided by scattered `if` statements: the publish route, the
// status route, the trash routes and the MCP tools each knew their own subset.
// The dashboard then guessed from `status` alone, which is how a row could read
// "published" while the page behind it refused every buyer.
//
// (2) The derived buyer state. "This form is not accepting orders right now" is
// not a state and never was: it is a predicate over the LIVE status, the trash,
// the schedule window, the entry limit and whether the config sells anything.
// Three readers computed it three ways before this file:
//
//	the public page   its own schedule + entry limit check, then its own
//	                  renderer-level "no line item" question;
//	the dashboard     dbstore.NotAcceptingOrders (products only — a form at its
//	                  entry limit, or one whose window had not opened, read LIVE);
//	MCP               nothing at all (a 200 read and no signal).
//
// FormOrders is that predicate, in one place. The public page, the REST form
// reads and the MCP form reads all call it, so a row's answer cannot depend on
// which surface asked.
//
// WHAT THIS FILE DOES NOT DO: it does not add a transition. Every edge below is
// a move the product already performs, and the write paths are unchanged — the
// table DESCRIBES them (and is what the dashboard's buttons are rendered from),
// so an edge that is not in the table does not exist for any surface.
//
// TWO GUARDS ARE DELIBERATELY ABSENT, and both are measurable non-refusals:
//
//   - publishing a payment form with nothing to sell. That door does not
//     refuse: it answers 409 with dbstore.PublishWarning* and accepts the very
//     same call again with `acknowledge`, because an empty payment form is a
//     decision the merchant may take deliberately. A guard here would hide a
//     button the API accepts. The buyer-side consequence is stated instead, by
//     FormOrders: such a form reads accepting_orders=false with the same code,
//     which is what the dashboard pill and the agent read. The table promises
//     the ACTION exists, never that the page behind it works; that is the one
//     derived predicate's job.
//   - `unlist`/`make_private` on a template. A template's status IS its gallery
//     switch, and no template surface offers either event; the row would not
//     refuse the write, but the machine cannot represent the state it lands in
//     (see the template states below), so the edge is not declared at all.
//
// `archive-as-template` is deliberately NOT an edge. Saving a form as a template
// CLONES it (`FormStore.CreateTemplate` inserts a new row with is_template=true);
// the source form does not move. Declaring `archive_as_template` would give an
// agent an enabled action that no door implements — the exact lying table this
// package exists to prevent. A template's own states are still modelled
// (template_draft / template_published) because a template row IS a forms row
// whose publish means "into the gallery".
package lifecycle

import (
	"errors"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
)

// The form events. Named as the routes talk about them, so a call site reads as
// the thing that happened rather than as the column it wrote.
const (
	// FormEventPublish: the merchant puts the form live at its URL (POST
	// /forms/{id}/publish, the status route's published arm, MCP publish_form).
	FormEventPublish = "publish"
	// FormEventUnpublish: the merchant takes the form out of a live state and
	// back to draft (status route's draft arm, MCP publish_form status=draft,
	// the legacy unpublish_form).
	FormEventUnpublish = "unpublish"
	// FormEventUnlist: live at its URL, hidden from listings (status route's
	// unlisted arm). It is not a person's action in the dashboard, so it has no
	// label key.
	FormEventUnlist = "unlist"
	// FormEventMakePrivate: live behind an access password (status route's
	// private arm). The password itself is written by the same statement; the
	// row cannot refuse the move on its own, which is why the edge has no guard.
	FormEventMakePrivate = "make_private"
	// FormEventDelete: soft delete — the row leaves every normal read and its
	// slug is released; a restore brings it back.
	FormEventDelete = "delete"
	// FormEventRestore: clears deleted_at. The row returns to the status it was
	// trashed FROM, which is why this edge's target is dynamic.
	FormEventRestore = "restore"
	// FormEventPurge: permanent delete of a trashed row. Irreversible, and the
	// store refuses one that has ever had a sale.
	FormEventPurge = "purge"
)

// FormsMachine is the forms aggregate's table.
//
// A state-preserving edge with no LabelKey is an IDEMPOTENT NO-OP: publishing a
// published form, or unpublishing a draft, answers 200 today and changes
// nothing, so the table defines the edge (a caller asking for it is not doing
// something illegal) but Available does not offer it as a button.
//
// State names come from dbstore (FormStatus* for the live statuses, and
// FormState* for trashed/template/purged) so there is ONE spelling of each — a
// test pins the declared states to dbstore's list.
var FormsMachine = &Machine{
	Name: "forms",
	States: map[string]State{
		dbstore.FormStatusDraft: {Events: map[string]Transition{
			FormEventPublish:     {To: dbstore.FormStatusPublished, LabelKey: "builder.publish"},
			FormEventUnlist:      {To: dbstore.FormStatusUnlisted},
			FormEventMakePrivate: {To: dbstore.FormStatusPrivate},
			FormEventDelete:      {To: dbstore.FormStateTrashed, LabelKey: "forms.delete"},
			// Saving a draft (the status route posts the current status on
			// every save) is a no-op, not an action.
			FormEventUnpublish: {To: dbstore.FormStatusDraft},
		}},

		dbstore.FormStatusPrivate: {Events: map[string]Transition{
			// Publishing a private form clears its access password in the same
			// statement (SetStatusWithPassword with "") — measured behaviour,
			// not a new rule.
			FormEventPublish:   {To: dbstore.FormStatusPublished, LabelKey: "builder.publish"},
			FormEventUnlist:    {To: dbstore.FormStatusUnlisted},
			FormEventUnpublish: {To: dbstore.FormStatusDraft, LabelKey: "forms.unpublish"},
			FormEventDelete:    {To: dbstore.FormStateTrashed, LabelKey: "forms.delete"},
			// Re-saving private with a new password is a state-preserving write
			// (set_form_password is its own tool).
			FormEventMakePrivate: {To: dbstore.FormStatusPrivate},
		}},

		dbstore.FormStatusPublished: {Events: map[string]Transition{
			// A duplicate publish is a no-op (the routes allow it).
			FormEventPublish:     {To: dbstore.FormStatusPublished},
			FormEventUnpublish:   {To: dbstore.FormStatusDraft, LabelKey: "forms.unpublish"},
			FormEventUnlist:      {To: dbstore.FormStatusUnlisted},
			FormEventMakePrivate: {To: dbstore.FormStatusPrivate},
			FormEventDelete:      {To: dbstore.FormStateTrashed, LabelKey: "forms.delete"},
		}},

		dbstore.FormStatusUnlisted: {Events: map[string]Transition{
			FormEventPublish:     {To: dbstore.FormStatusPublished, LabelKey: "builder.publish"},
			FormEventUnpublish:   {To: dbstore.FormStatusDraft, LabelKey: "forms.unpublish"},
			FormEventMakePrivate: {To: dbstore.FormStatusPrivate},
			FormEventDelete:      {To: dbstore.FormStateTrashed, LabelKey: "forms.delete"},
			FormEventUnlist:      {To: dbstore.FormStatusUnlisted},
		}},

		// ── Templates (is_template) ─────────────────────────────────────────
		// A template is a forms row that is not a storefront: draft means "one
		// of my own templates" (issue #457) and published means "in the public
		// gallery". The publish edge carries the gallery-metadata guard, which
		// is the same rule setStatusSQL enforces (issue #594) — a template with
		// no category or no description block cannot be published, and the
		// table must say so rather than letting the caller learn it from a 409.
		//
		// ONLY TWO template states are modelled, and that is deliberate: a
		// template's status IS its gallery switch, so `unlist`/`make_private`
		// have no meaning any template surface offers. The status route would
		// still write those values onto a template ROW (it does not special-case
		// is_template), and dbstore.FormStateOf normalises such a row to
		// template_draft — a template that is not in the gallery. Declaring an
		// `unlist` edge here would promise an `unlisted` template state the
		// machine cannot represent.
		dbstore.FormStateTemplateDraft: {Events: map[string]Transition{
			FormEventPublish: {
				To:       dbstore.FormStateTemplatePublished,
				Guard:    templateGalleryMetadata,
				LabelKey: "builder.publish",
			},
			FormEventDelete:    {To: dbstore.FormStateTrashed, LabelKey: "forms.delete"},
			FormEventUnpublish: {To: dbstore.FormStateTemplateDraft},
		}},

		dbstore.FormStateTemplatePublished: {Events: map[string]Transition{
			// A published gallery template re-saved as published re-runs the
			// metadata gate (the SQL blocks on the predicate, not on the
			// transition), so the guard is here too and the edge stays hidden
			// from Available — re-publishing it is not an action.
			FormEventPublish:   {To: dbstore.FormStateTemplatePublished, Guard: templateGalleryMetadata},
			FormEventUnpublish: {To: dbstore.FormStateTemplateDraft, LabelKey: "forms.unpublish"},
			FormEventDelete:    {To: dbstore.FormStateTrashed, LabelKey: "forms.delete"},
		}},

		// ── Trash ───────────────────────────────────────────────────────────
		// deleted_at set. Both exits live here and nowhere else: a live form
		// cannot be purged (the store answers 404, never "deleted"), which is
		// the guard that makes the irreversible move reachable only through the
		// bin.
		dbstore.FormStateTrashed: {Events: map[string]Transition{
			// Restore returns the row to the state it was trashed FROM — a
			// published form comes back published, a draft comes back draft.
			FormEventRestore: {
				Target:   restoreTarget,
				LabelKey: "forms.restore",
			},
			// The irreversible move, and its guard: the store refuses to
			// destroy a form that has ever recorded a sale (sales history is
			// never destroyed), so the table must say so rather than let an
			// agent plan a purge that answers 409 form.hasSales. The count
			// rides the trash reads (dbstore.Form.SalesCount), which is how a
			// row can answer this without a second query.
			FormEventPurge: {
				To:       dbstore.FormStatePurged,
				Guard:    purgeNeedsNoSales,
				LabelKey: "forms.purge",
			},
		}},

		// The row is gone. Nothing can be done with it, and no read observes it.
		dbstore.FormStatePurged: {Events: map[string]Transition{}},
	},
}

// restoreTarget resolves a trashed row's landing state: the state it held before
// it was deleted. deleted_at is the ONLY thing SoftDelete wrote, so the live
// state is still on the row.
func restoreTarget(e Entity) string {
	f, ok := e.(*dbstore.Form)
	if !ok || f == nil {
		return ""
	}
	return dbstore.FormStateOf(f)
}

// FormPurgeNeedsNoSalesReason is the ONE sentence a purge of a sold form gets.
// It states the rule (not the internals), and it is what the table hands an
// agent as the disabled action's reason.
const FormPurgeNeedsNoSalesReason = "this form has sales — sales history is never destroyed, so it cannot be deleted for good and stays in the trash"

// soldForm is a form row that knows whether it was ever sold — implemented by
// *dbstore.Form through the trash reads (FormSalesCount). A row that does not
// carry the count is NOT refused: the store is still the decider, and refusing
// on a zero nobody measured would be a false refusal.
type soldForm interface {
	FormSalesCount() (int64, bool)
}

// purgeNeedsNoSales guards the irreversible edge: the store refuses to purge a
// form that has any sale (dbstore.ErrFormHasSales → 409 form.hasSales), because
// deleting the form would take the sales history with it. The count is carried
// by the trash reads; a row that does not carry it is purged as before, so this
// can only ever refuse a purge the store would refuse too.
func purgeNeedsNoSales(e Entity) error {
	sf, ok := e.(soldForm)
	if !ok {
		return nil
	}
	if n, known := sf.FormSalesCount(); known && n > 0 {
		return errors.New(FormPurgeNeedsNoSalesReason)
	}
	return nil
}

// templateGalleryMetadata is the publish precondition for a TEMPLATE: a gallery
// card with no category and no description is what issue #594 refused, and the
// sentence is the one both existing doors already print (formfields carries it
// now, so this package, REST and MCP cannot word it differently).
func templateGalleryMetadata(e Entity) error {
	f, ok := e.(*dbstore.Form)
	if !ok || f == nil {
		return errors.New("the form could not be inspected")
	}
	if formfields.TemplateGalleryMetadataMissing(f.Config) {
		return errors.New(formfields.TemplateNeedsMetadata)
	}
	return nil
}

// ─── The derived buyer state ────────────────────────────────────────────────

// Form order codes. They are stable tokens a surface keys its own wording off;
// a code never changes and two codes never mean the same thing. The two product
// codes are dbstore's own publish-warning codes, so the read surfaces and the
// publish refusal name the same defect with the same string.
const (
	// FormOrderAccepting is the empty code: the form takes orders right now.
	FormOrderAccepting = ""
	// FormOrderUnknown: there was no row to inspect.
	FormOrderUnknown = "unknown"
	// FormOrderTrashed: the row is in the bin.
	FormOrderTrashed = "trashed"
	// FormOrderNotPublished: the row is a draft (or a draft template).
	FormOrderNotPublished = "not_published"
	// FormOrderScheduled: start_at is in the future.
	FormOrderScheduled = "scheduled"
	// FormOrderClosed: end_at has passed.
	FormOrderClosed = "closed"
	// FormOrderEntryLimitReached: the entry limit is spent.
	FormOrderEntryLimitReached = "entry_limit_reached"
	// FormOrderNoProducts: a payment form with nothing sellable at all.
	FormOrderNoProducts = dbstore.PublishWarningNoProducts
	// FormOrderNoProductSelected: a payment form whose product block names no
	// product, or one whose product row was purged.
	FormOrderNoProductSelected = dbstore.PublishWarningNoProductSelected
)

// FormOrderEnv carries the facts a row cannot answer alone. Zero values are the
// safe side: no product table (nil) skips the purged-product arm rather than
// accusing a healthy form, and an unknown entry count (-1) skips the limit arm
// rather than inventing a reached one.
type FormOrderEnv struct {
	// LiveProductIDs is every product id the account still has a row for
	// (archived and trashed included). nil = unknown.
	LiveProductIDs map[string]bool
	// Entries is how many orders (payment form) or submissions (response form)
	// the form has recorded. -1 = unknown.
	Entries int64
	// Now is the instant the schedule window is judged at. Zero means time.Now()
	// — callers that need determinism pass it.
	Now time.Time
}

// FormOrderState is the answer to "will a buyer be able to submit this form
// right now", with the reason when the answer is no.
//
// For a RESPONSE form it means "accepting responses": the field is named
// accepting_orders because that is the buyer-facing vocabulary of the payment
// case, and the predicate is the same question — is the public page submittable.
type FormOrderState struct {
	Accepting bool `json:"accepting_orders"`
	// Code is the stable machine token (empty when Accepting).
	Code string `json:"code,omitempty"`
	// Reason is the English sentence a person can read (empty when Accepting).
	Reason string `json:"reason,omitempty"`
	// BlockID names the product block to fix, when there is a single one.
	BlockID string `json:"block_id,omitempty"`
}

// FormOrders is the ONE derived predicate. Order matters, because the reason a
// buyer is refused is the one the page shows first: a draft form is never
// served at all (404), so "not published" must win over "its window has not
// opened"; the schedule precedes the entry limit, which is the order the public
// page has always answered in (and the order its submit-side checks still use).
func FormOrders(f *dbstore.Form, env FormOrderEnv) FormOrderState {
	if f == nil {
		return FormOrderState{Code: FormOrderUnknown, Reason: "the form could not be inspected"}
	}
	if f.DeletedAt != nil {
		return FormOrderState{Code: FormOrderTrashed, Reason: "this form is in the trash — restore it first"}
	}
	// A draft — and a draft template, which has no public address at all — is
	// never served to a buyer, so it is not accepting anything.
	if !dbstore.IsLiveStatus(f.Status) {
		return FormOrderState{Code: FormOrderNotPublished, Reason: "this form is not published"}
	}
	now := env.Now
	if now.IsZero() {
		now = time.Now()
	}
	if f.StartAt != nil && now.Before(*f.StartAt) {
		return FormOrderState{Code: FormOrderScheduled, Reason: "this form is scheduled to open later"}
	}
	if f.EndAt != nil && now.After(*f.EndAt) {
		return FormOrderState{Code: FormOrderClosed, Reason: "this form has closed"}
	}
	if f.EntryLimit != nil && *f.EntryLimit > 0 && env.Entries >= 0 && env.Entries >= int64(*f.EntryLimit) {
		return FormOrderState{Code: FormOrderEntryLimitReached, Reason: "this form has reached its entry limit"}
	}
	// A response form collects answers, not orders: it is accepting unless a
	// restriction above said otherwise.
	if dbstore.FormMode(f) == dbstore.FormModeResponse {
		return FormOrderState{Accepting: true}
	}
	if blockID, dead := dbstore.NotAcceptingOrders(f, env.LiveProductIDs); dead {
		// dbstore's rule returns a block id exactly when the config HAS a
		// product block (a pointing one, or one whose product row was purged) —
		// the same split PublishWarning makes between its two codes.
		if blockID != "" {
			return FormOrderState{
				Code:    FormOrderNoProductSelected,
				Reason:  `this payment form has a product block with no product selected — buyers are shown "This form is not accepting orders right now"`,
				BlockID: blockID,
			}
		}
		return FormOrderState{
			Code:   FormOrderNoProducts,
			Reason: `this payment form has nothing to sell — buyers are shown "This form is not accepting orders right now"`,
		}
	}
	return FormOrderState{Accepting: true}
}

// FormOrderIsProductCode reports whether a refusal code names the PRODUCT rule
// (nothing to sell / a product block with no product) rather than a live-form
// restriction. The dashboard keys its "not accepting orders" pill off this, so a
// form that is merely scheduled does not get a sentence about products.
func FormOrderIsProductCode(code string) bool {
	return code == FormOrderNoProducts || code == FormOrderNoProductSelected
}
