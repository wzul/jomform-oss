// The HUMAN label for each event name the API records — the other half of
// `dbstore.Events.Record(...)`.
//
// THE DEFECT THIS EXISTS FOR (card t_804040d3). The Activity table printed the
// machine name verbatim — `sale.paid`, `workspace.renamed`,
// `admin.super_admin_action` — to the merchant who owns the workspace. Those are
// the tokens the REST/MCP surface answers in; they are not sentences, and a
// dashboard cell is not an API reference. The row now renders the label below
// and keeps the raw token as the cell's `title`, so the operator who needs the
// code still gets it on hover without the merchant reading snake_case.
//
// WHY A MAP AND NOT `t("act.event." + name)`. The key WOULD be the same shape,
// but a key built from runtime data is invisible to `src/lib/i18nKeys.ts`'s
// static scan: it would count as one more `dynamic` site and nothing would
// notice a name that has no entry. Declaring the population here, typed
// `Record<string, Key>`, makes the compiler check every target key EXISTS, and
// `src/lib/activityPage.test.ts` pins this map against the recorder call sites
// in `api/internal` in BOTH directions — a recorded name with no label fails,
// and a label for an event nothing records fails too. That is the same trade
// `src/lib/chatProposal.ts` makes for `chat.tool.*`, `src/lib/chatSteps.ts` for
// the chat nouns and `src/lib/saleActions.test.ts` for the sale buttons.
//
// THE THREE DYNAMIC NAMES. `form.status_`+status, `form.bulk_`+action and
// `product.bulk_`+action are built at the call site, so a literal scan sees only
// the prefix. Their expansions are listed here explicitly and pinned against the
// values the Go source allows (form statuses from `dbstore/form.go`, the bulk
// actions from the switches that build them).
//
// A NAME THAT IS NOT IN THIS MAP still renders — as the raw token, which is the
// pre-fix behaviour rather than a blank cell. That is deliberate: a new event
// recorder must not paint an empty column while waiting for a translation, and
// the guard above is what makes the omission visible in the suite instead.
import type { DICT } from "../components/i18nDict";

type Key = keyof typeof DICT;

/**
 * event name → dictionary key carrying its human label in all five locales.
 *
 * Keep in step with the recorders under `api/internal`: the guard in
 * `src/lib/activityPage.test.ts` reads those call sites and fails in both
 * directions.
 */
export const EVENT_LABEL_KEYS: Record<string, Key> = {
  // admin
  "admin.super_admin_action": "act.event.admin.super_admin_action",

  // auth
  "auth.login": "act.event.auth.login",
  "auth.logout": "act.event.auth.logout",
  "auth.stepup_required": "act.event.auth.stepup_required",

  // chat
  "chat.message": "act.event.chat.message",
  "chat.proposal_confirmed": "act.event.chat.proposal_confirmed",

  // feedback
  "feedback.admin_replied": "act.event.feedback.admin_replied",
  "feedback.created": "act.event.feedback.created",
  "feedback.notify_retried": "act.event.feedback.notify_retried",
  "feedback.replied": "act.event.feedback.replied",
  "feedback.status_changed": "act.event.feedback.status_changed",

  // form — the literal names
  "form.created": "act.event.form.created",
  "form.deleted": "act.event.form.deleted",
  "form.published": "act.event.form.published",
  "form.purged": "act.event.form.purged",
  "form.restored": "act.event.form.restored",
  "form.slug_renamed": "act.event.form.slug_renamed",
  "form.updated": "act.event.form.updated",
  // form — `"form.bulk_"+action`
  "form.bulk_publish": "act.event.form.bulk_publish",
  "form.bulk_draft": "act.event.form.bulk_draft",
  "form.bulk_delete": "act.event.form.bulk_delete",
  // form — `"form.status_"+status`
  "form.status_draft": "act.event.form.status_draft",
  "form.status_published": "act.event.form.status_published",
  "form.status_unlisted": "act.event.form.status_unlisted",
  "form.status_private": "act.event.form.status_private",

  // mcp
  "mcp.agent_disconnected": "act.event.mcp.agent_disconnected",
  "mcp.tool_call": "act.event.mcp.tool_call",

  // media
  "media.deleted": "act.event.media.deleted",
  "media.purged": "act.event.media.purged",
  "media.restored": "act.event.media.restored",

  // product
  "product.deleted": "act.event.product.deleted",
  "product.purged": "act.event.product.purged",
  "product.restored": "act.event.product.restored",
  "product.updated": "act.event.product.updated",
  "product.bulk_archive": "act.event.product.bulk_archive",

  // profile
  "profile.avatar_update": "act.event.profile.avatar_update",

  // sale
  "sale.bulk_captured": "act.event.sale.bulk_captured",
  "sale.captured": "act.event.sale.captured",
  "sale.confirmed": "act.event.sale.confirmed",
  "sale.refunded": "act.event.sale.refunded",
  "sale.released": "act.event.sale.released",

  // settings
  "settings.api_key_created": "act.event.settings.api_key_created",
  "settings.api_key_revoked": "act.event.settings.api_key_revoked",
  "settings.chip_cleared": "act.event.settings.chip_cleared",
  "settings.chip_updated": "act.event.settings.chip_updated",
  "settings.domain_added": "act.event.settings.domain_added",
  "settings.domain_claimed": "act.event.settings.domain_claimed",
  "settings.domain_cleared": "act.event.settings.domain_cleared",
  "settings.domain_custom_cert": "act.event.settings.domain_custom_cert",
  "settings.domain_removed": "act.event.settings.domain_removed",
  "settings.domain_tls_email": "act.event.settings.domain_tls_email",
  "settings.domain_tls_mode": "act.event.settings.domain_tls_mode",
  "settings.domain_verified": "act.event.settings.domain_verified",
  "settings.webhook_created": "act.event.settings.webhook_created",
  "settings.webhook_deleted": "act.event.settings.webhook_deleted",
  "settings.webhook_retried": "act.event.settings.webhook_retried",
  "settings.webhook_updated": "act.event.settings.webhook_updated",

  // template
  "template.created": "act.event.template.created",
  "template.deleted": "act.event.template.deleted",
  "template.updated": "act.event.template.updated",

  // upload
  "upload.asset": "act.event.upload.asset",
  "upload.avatar": "act.event.upload.avatar",
  "upload.image": "act.event.upload.image",
  "upload.pdf": "act.event.upload.pdf",

  // workspace
  "workspace.deleted": "act.event.workspace.deleted",
  "workspace.purged": "act.event.workspace.purged",
  "workspace.renamed": "act.event.workspace.renamed",
  "workspace.restored": "act.event.workspace.restored",
};

/**
 * The dictionary key for one recorded event name, or `null` when this map has no
 * entry (the row then falls back to the raw token).
 */
export function eventLabelKey(eventName: string): Key | null {
  return Object.prototype.hasOwnProperty.call(EVENT_LABEL_KEYS, eventName)
    ? EVENT_LABEL_KEYS[eventName]
    : null;
}
