// The form's collection mode, and the one place that decides
// whether a form is a "sales" form.
//
// THE BUG: the /forms list offered every row a "Sales" action, because the
// forms list payload carried no way to tell an order form from a non-sales
// form (an RSVP, a contact form, an event registration). A merchant with an
// RSVP form saw it labelled "Sales"/"Jualan", which is simply wrong.
//
// The mode is the authoritative discriminator and it already existed: the
// builder writes it ("Form mode": Payment form / Response form), the public
// submit path branches on it (a response form records a submission instead of
// an order), and the API's entry-limit counter keys off it. The one thing
// missing was surfacing it on the forms payload — that is now done
// (restForm.Mode), and this module turns it into the display
// decision.
//
// THE RULE, and why it defaults to payment:
// anything that is not explicitly "response" is a payment (sales) form.
//
// Every form created before response mode existed has no `mode` at all, and
// those are payment forms — in production they are also the forms that
// actually have sales. Defaulting the other way would hide the Sales action on
// real sales forms, which is the worse failure.
//
// Why this is NOT derived from "has product blocks": a form with no pricing
// blocks can still be a sales form with real sales (legacy forms where the
// products came from the account, not the form). Production has such a row —
// 0 blocks, 13 sales. Mode is the only safe signal.

/** Collection mode of a form, as stored in `forms.config.mode`. */
export const FORM_MODE_PAYMENT = "payment";
export const FORM_MODE_RESPONSE = "response";

export type FormMode = typeof FORM_MODE_PAYMENT | typeof FORM_MODE_RESPONSE;

/** The subset of a form payload this module needs. */
export type ModeCarrier = {
  /** Top-level `mode` on the API payload. */
  mode?: string;
  /** Nested config; used only as a fallback for pre-mode payload shapes. */
  config?: { mode?: string } | Record<string, unknown> | null;
};

/**
 * formMode returns "response" only when the form is explicitly a
 * response-only form; everything else (including a form with no mode at all)
 * is a payment form. Mirrors `dbstore.FormMode` in the Go API.
 */
export function formMode(f: ModeCarrier | null | undefined): FormMode {
  const raw = f?.mode ?? (f?.config as { mode?: unknown } | null | undefined)?.mode;
  return raw === FORM_MODE_RESPONSE ? FORM_MODE_RESPONSE : FORM_MODE_PAYMENT;
}

/** isSalesForm reports whether this form can take payment (and so has sales). */
export function isSalesForm(f: ModeCarrier | null | undefined): boolean {
  return formMode(f) !== FORM_MODE_RESPONSE;
}

/**
 * defaultNotificationEvent returns the event a NEW per-form notification rule
 * should start on for this form.
 *
 * The two events are NOT interchangeable: a response-mode form records
 * submissions and never a sale, so a rule left on `sale.paid` can never fire and
 * the merchant is silently never told anyone responded. Mirrors the API's
 * EnsureDefault rule (`dbstore.defaultNotificationFor`, issue #597).
 */
export function defaultNotificationEvent(f: ModeCarrier | null | undefined): string {
  return isSalesForm(f) ? "sale.paid" : "response.submitted";
}

/**
 * The i18n key for a form's entries action, chosen by the form's mode: a
 * payment form's entries are Sales, a response form's entries are Submissions.
 * Kept as a literal union (not the renderer's `Key` type from
 * ../components/i18n.tsx) so this module stays loadable by the plain-node test
 * runner, which cannot execute a `.tsx` file — see the comment at the top of
 * src/lib/i18n.test.ts.
 */
export type FormEntriesLabelKey = "nav.sales" | "nav.submissions";

/** The one place the per-form entries URL is built. */
function formEntriesPath(formId: string): string {
  return `/sales?form=${encodeURIComponent(formId)}`;
}

/**
 * salesFormHref returns the per-form Sales deep-link for a form that can take
 * payment, or null for a response-only form.
 *
 * Returning null (rather than always building the URL) is the #602 decision: a
 * response-only form can never record a sale, so labelling it "Sales" is
 * wrong.
 *
 * The dashboard surfaces now reach their entries through formEntriesAction
 * below, which renders for BOTH modes — the #602 fix hid the Sales action from
 * a response form and, in doing so, removed the only way to reach that form's
 * submissions. This helper stays as the *Sales* half of the rule (and the
 * place a payment-only surface would call).
 */
export function salesFormHref(f: ModeCarrier | null | undefined, formId: string): string | null {
  if (!isSalesForm(f) || !formId) return null;
  return formEntriesPath(formId);
}

/**
 * formEntriesAction returns the per-form entries action — the deep-link plus
 * the label to give it — for a form of EITHER mode, or null when there is no id
 * to link to OR when the form has not resolved yet.
 *
 * The destination is the same for both modes (/sales?form=<id>): that page
 * lists the entries of the selected form, switching to the answers layout when
 * the form is a response form (#604). Only the label differs, because a
 * payment form's entries are sales and a response form's entries are
 * submissions.
 *
 * This is deliberately an action for both modes rather than a mode-gated one.
 * Gating it (as the Sales link alone was gated by #602) leaves a response form
 * — an event registration, an RSVP, a contact form — with NO action at all, so
 * its submissions become unreachable from the dashboard.
 *
 * WHY A FORM THAT HAS NOT RESOLVED GETS NO ACTION (t_105bdd71)
 * The label is chosen from the form's mode, and there is no mode until the
 * payload arrives: `isSalesForm(null)` is (correctly) `true`, because a
 * pre-mode *payload* is a legacy payment form. That default is about a form
 * whose mode field is absent, NOT about a form that is still (or never)
 * loading — and it is a lie there. This function used to return the action
 * whenever `formId` was set, so /form-settings rendered "Sales"
 * (`nav.sales`) on a RESPONSE-mode form while its GET was in flight and,
 * permanently, after a failed load (the page leaves `form` null on both the
 * 404 path and the throw path). The header action is now withheld until the
 * form resolves: a form that never resolves never claims a mode, so it can
 * never be mislabelled. `isSalesForm`'s null default is deliberately
 * untouched — flipping IT would mislabel every legacy payment form, which is
 * the worse failure (see the rationale at the top of this module).
 */
export function formEntriesAction(
  f: ModeCarrier | null | undefined,
  formId: string,
): { href: string; labelKey: FormEntriesLabelKey } | null {
  if (!formId) return null;
  // No payload yet: the mode is unknown, so no label here is honest.
  if (!f) return null;
  return {
    href: formEntriesPath(formId),
    labelKey: isSalesForm(f) ? "nav.sales" : "nav.submissions",
  };
}
