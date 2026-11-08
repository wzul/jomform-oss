// What the assistant is doing, said in the visitor's language (card t_148d7100).
//
// THE DEFECT. A tool step was rendered ONLY when it was refused. The ordinary
// case — the assistant reads something and then answers — showed the user a static
// "Looking that up…" for the whole round-trip, identical to the instant before the
// model was even called. Reported as "the assistant looks stuck".
//
// THE RULE THE API FOLLOWS, AND THIS MODULE UNDOES. The server sends the TOOL
// NAME and nothing else (`chat.Event.Tool`, api/internal/chat/stream.go): the API
// does not know this visitor's language (issue #6177 — a sentence built there
// reached all five dashboard locales in one language). So the wording is a
// dictionary decision, and this module is where the mapping is executed — it is
// the only place a tool name becomes text.
//
// A TOOL WITH NO NOUN STILL SAYS SOMETHING. The chat's read allowlist is a Go
// list that grows; a tool added there without an entry here must not leave the
// user with a blank status line, so it falls back to a generic sentence. The
// completeness of this table is guarded by src/lib/chatSteps.test.ts, which reads
// the allowlist out of the Go source — so a NEW read tool fails the suite instead
// of degrading silently.

import type { Key } from "../components/i18n";
// The dashboard's translator type, shared with the proposal/refusal helpers: a
// Key-taking function. Typing this as `(key: string) => string` would make the
// real `t` un-assignable (a function that accepts FEWER keys cannot stand in for
// one that accepts any string) and the page would not compile.
import type { Translate } from "./chatProposal";

/** The resource a chat read tool reads, keyed by tool name. */
export const NOUN_BY_TOOL: Record<string, Key> = {
  get_account: "chat.noun.account",
  list_workspaces: "chat.noun.workspaces",
  list_forms: "chat.noun.forms",
  get_form: "chat.noun.forms",
  get_form_analytics: "chat.noun.analytics",
  list_form_submissions: "chat.noun.submissions",
  list_products: "chat.noun.products",
  list_product_variations: "chat.noun.products",
  get_product_analytics: "chat.noun.products",
  list_sales: "chat.noun.sales",
  get_sales_summary: "chat.noun.sales",
  get_sale_email_status: "chat.noun.sales",
  list_subscriptions: "chat.noun.subscriptions",
  list_email_deliveries: "chat.noun.deliveries",
  list_spam_sales: "chat.noun.spam",
  list_spam_submissions: "chat.noun.spam",
  list_media: "chat.noun.media",
  list_events: "chat.noun.events",
  list_templates: "chat.noun.templates",
  list_own_templates: "chat.noun.templates",
  list_feedback: "chat.noun.feedback",
  get_feedback_thread: "chat.noun.feedback",
  // The SETTINGS reads. Each names what it actually reads, so the status line
  // stays true: mapping get_payment_fees onto chat.noun.sales would read
  // "Reading your sales…", which is a different question and a lie.
  get_workspace_settings: "chat.noun.settings",
  get_payment_fees: "chat.noun.fees",
  get_shipping_rates: "chat.noun.shippingRates",
  get_shipping_couriers: "chat.noun.couriers",
};

/** The tool that is not a read: it files feedback, and says so. */
export const FILING_TOOL = "submit_feedback";

/**
 * The sentence shown for a tool.
 *
 * Deliberately not the tool's own name: `list_form_submissions` is a wire
 * identifier, and printing it at a merchant is the same class of defect as
 * printing a machine key (t_3839995c).
 *
 * `done` selects the PAST tense for the finished trail under an answer. The live
 * status line and the trail are the same event at different times, so they share
 * one mapping — a tool can never read one way while running and another way
 * afterwards.
 */
export function stepLabel(tool: string, t: Translate, done = false): string {
  // Written as explicit calls rather than a ternary on the key name: the i18n
  // COVERAGE scan (src/lib/i18nKeys.test.ts) resolves literal `t("…")` call sites
  // and counts the rest against a ceiling, so a key chosen by an expression is a
  // key nothing checks. Only `t(nounKey)` below is genuinely dynamic — the noun
  // comes from the tool name, which is a wire value.
  if (tool === FILING_TOOL) return done ? t("chat.step.filed") : t("chat.step.filing");
  const nounKey = NOUN_BY_TOOL[tool];
  if (nounKey) {
    // ONE lookup, so the noun is resolved once and the i18n coverage scan counts
    // this site once (src/lib/i18nKeys.test.ts holds a ceiling on the call sites
    // it cannot resolve statically).
    const thing = t(nounKey);
    return done ? t("chat.step.read", { thing }) : t("chat.step.reading", { thing });
  }
  return done ? t("chat.step.worked") : t("chat.step.working");
}
