// The forms row's LIFECYCLE STATE — the row read as one state name, so the
// state machine in internal/lifecycle can talk about a form the way it talks
// about a sale.
//
// WHY A DERIVED STATE, and not just f.Status. The forms table's status column
// has a CHECK constraint (draft|private|published|unlisted), but a form's
// buyer-facing reality is carried by THREE things at once:
//
//   - `status`               — draft is not served, private is served behind a
//     password gate, published and unlisted are served;
//   - `deleted_at`           — a soft-deleted row is invisible to every normal
//     read and its slug is free for another form, so "trashed" is a state and
//     not a filter;
//   - `is_template`          — a template row is a form that is NOT a storefront:
//     its published status puts it in the gallery, not at a buyer's URL.
//
// Those three are the hidden state the state-machine audit measured. Reading
// them in one place means the dashboard, the REST reads and the MCP tools cannot
// each answer "what is this form" a different way.
//
// The names are the machine's vocabulary (internal/lifecycle/forms.go) and a
// test pins the two lists equal, so a state cannot be added to one and silently
// missed in the other.
package dbstore

// Lifecycle states that are NOT a `forms.status` value. The four status values
// are states too, and they are spelled by the FormStatus* constants so there is
// only one spelling of "published" in the tree.
const (
	// FormStateTrashed: deleted_at is set. The row is out of every normal read,
	// its slug is released, and restore is the way back.
	FormStateTrashed = "trashed"
	// FormStateTemplateDraft: is_template with a draft status — one of the
	// account's OWN templates (issue #457). It has no public address at all.
	FormStateTemplateDraft = "template_draft"
	// FormStateTemplatePublished: is_template with a published status — a
	// gallery template. Its address is the template gallery, not a storefront.
	FormStateTemplatePublished = "template_published"
	// FormStatePurged: the row is gone (a hard delete). No read can observe it,
	// so this state exists only as the terminal target of `purge` — which is
	// exactly what makes "you cannot do anything else with it" stateable.
	FormStatePurged = "purged"
)

// FormStateOf returns the state of a LIVE row: the status vocabulary, with a
// template's two reachable states separated out. deleted_at is deliberately
// ignored — this is the state a restore returns the row to.
func FormStateOf(f *Form) string {
	if f == nil {
		return ""
	}
	if f.IsTemplate {
		if f.Status == FormStatusPublished {
			return FormStateTemplatePublished
		}
		return FormStateTemplateDraft
	}
	return f.Status
}

// LifecycleState returns the state the lifecycle machine names for this row.
// It is the Entity implementation internal/lifecycle applies events against.
//
// A nil row has no state (""), which the machine reports as an illegal
// transition rather than a panic.
func (f *Form) LifecycleState() string {
	if f == nil {
		return ""
	}
	if f.DeletedAt != nil {
		return FormStateTrashed
	}
	return FormStateOf(f)
}
