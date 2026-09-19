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
 * salesFormHref returns the per-form Sales deep-link for a form that can take
 * payment, or null for a response-only form.
 *
 * Returning null (rather than always building the URL) is what removes the
 * misleading action: a response-only form can never record a sale, so a
 * "Sales" button on its row leads to a surface that is empty by construction.
 * Callers render the action only when this returns a non-null href.
 */
export function salesFormHref(f: ModeCarrier | null | undefined, formId: string): string | null {
  if (!isSalesForm(f) || !formId) return null;
  return `/sales?form=${encodeURIComponent(formId)}`;
}
