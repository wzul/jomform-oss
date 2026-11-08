import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

export const metadata: Metadata = docsMetadata({
  title: "Sales, responses & analytics — JomForm Docs",
  description:
    "The Sales page: every sale and every response in one list, per-form counts, partial refunds, and the realtime notifications and order-screen beep.",
  path: "/docs/sales",
});

// The full text behind the summaries on /docs (t_d768342e): the Sales page, the
// combined sale+response list, response-only forms, the per-form counter, partial
// refunds and the realtime notification sound. Moved here from the docs index
// rather than deleted — the index links to these sections with `#id`, so the
// anchor names below are part of the contract with /docs and with the dashboard's
// inline "?" links (see the note on each one).
type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "sales",
    title: "Sales",
    body: [
      "Track every sale: reference code, status (pending/confirmed/failed/refunded), buyer, amount, payment method and date. Confirm bank-transfer sales, capture or release skip-capture authorisations, refund paid sales (full or partial), and export sales to CSV.",
      "Each amount is shown in the currency that sale was recorded in — US$24.90 for a USD sale, RM24.90 for an MYR one — never in the workspace's current setting, which would relabel history the moment the setting changed. The row carries it as currency on GET /v1/rest/sales and list_sales, and a refund is asked for in the same currency the sale was paid in.",
      "A form's mode decides what its entries are called, not whether you can reach them: a payment form (the default) takes payment and offers a Sales action on the Forms list, while a response-only form offers a Submission action. Both point at the same per-form list — an RSVP form is never labelled sales, and it still has a way into its responses.",
      "GET /v1/rest/forms (list, single and create) reports the mode as mode (payment or response; a form created before response mode existed reports payment).",
    ],
  },
  {
    id: "combined-list",
    title: "One list for sales and responses",
    body: [
      "Without a form filter the Sales page is the whole record set: every sale AND every response your response-mode forms recorded, merged newest first with one combined record count.",
      "That is deliberate — an account whose response entries were invisible on the unfiltered page had two surfaces reporting two different things (the per-form counter counted the responses, the list did not show them).",
      "Filtering by a form narrows the page to that form's own entries: a payment form shows its sales, a response-only form shows its responses and the table switches to the answers columns. The status filter is a payment question, so choosing a status hides the responses rather than reporting nothing.",
      "Export CSV produces the same rows as the list, with the same filters applied: a combined export carries a record_type column (sale or submission) plus the recorded answer columns, and a single-form export uses that form's own columns. Search and the date range apply to both halves. Money columns are total, refunded and outstanding — decimal major units in the sale's own currency, named by the currency column beside them, so a USD row reads USD + 24.90 rather than an RM label it was never charged in.",
      "The same decision drives the dashboard's recent list and the MCP tools list_sales and export_sales_csv, so the page, the CSV and your agent always agree.",
    ],
  },
  {
    id: "responses",
    title: "Responses (response-only forms)",
    body: [
      "A form can collect responses instead of payments (set the form mode to response in the builder — no products, no payment). Its entries are stored as responses, not sales, and they are shown on the Sales page too: open a response form from Forms → Submission (or Form Settings → Submission) and the list shows that form's responses — when each one arrived, the buyer's name/email/phone and the recorded answers — with a detail drawer holding every field value.",
      "A payment form's button says Sales instead, because its entries are sales; the destination is the same. The search box matches any recorded answer and the date range applies as usual; the payment-status filter does not appear, because a response has no payment status. Export CSV and the capture/refund actions are payment-only and do not apply.",
      "The dashboard's per-form counter counts responses for a response form, so a form that has collected entries never reads as zero. AI agents read the same data with list_form_submissions (and list_sales returns them for a response form id).",
    ],
  },
  {
    // The dashboard's inline "?" (issue #598, AGENTS.md docs-parity rule 4)
    // deep-links this section, and /docs carries the same anchor.
    id: "analytics",
    title: "Form analytics (the per-form counter)",
    body: [
      "The dashboard lists one card per form with a count of the entries that form has recorded — the rows the form actually produced, read from the table it writes to.",
      "Set the form mode to response and the card says Submissions, counting that form's responses; leave it as a payment form (the default) and the card says Entries, counting that form's sales in every status (paid, confirmed, pending, failed, refunded). The two tables are never added together, because a form's mode decides which one it writes: the counter counts exactly one of them, so a form cannot double count.",
      "Beside the counter the card shows the money figures as their own separately named values — Paid+Confirmed (how many of the form's sales reached the paid set, each sale counted once), Pending, Failed and revenue net of refunds, that revenue labelled in the currency each sale was recorded in.",
      "AI agents read the same rollup with get_form_analytics, which reports the form's mode as mode so the figure can be labelled correctly, and GET /v1/rest/dashboard-bundle carries it as form_analytics.",
    ],
  },
  {
    id: "refunds",
    title: "Partial refunds",
    body: [
      "Refund a paid or confirmed sale in full or in part. JomForm records how much has actually been returned on the sale (refunded_sen), so a partial refund is never mistaken for a full one: the sale keeps its paid/confirmed status with a “Partially refunded” marker and the remaining balance (outstanding_sen) stays refundable until it reaches zero — only then does the sale become refunded.",
      "Every read surface reports the same picture: the Sales page (status chip, plus a refunded/balance breakdown in the sale detail), get_sales_summary, analytics (revenue is net of refunds), the CSV export (refunded, outstanding, refund_status beside the sale's currency) and the MCP tools (list_sales, refund_sale, export_sales_csv).",
      "An amount that would push the refunded total past the sale total (refunded_sen + amount_sen > total_sen) or a non-positive amount is rejected before the gateway is ever called, and the guard is applied under a per-sale lock so two refunds racing each other cannot over-refund.",
    ],
  },
  {
    // The /sales sound toggle's inline "?" (issue #447, AGENTS.md docs-parity
    // rule 4) links here as /docs/sales#realtime-notifications.
    id: "realtime-notifications",
    title: "Realtime notifications & the order screen beep",
    body: [
      "The Sales page updates itself: while it is open it subscribes to your workspace's realtime stream, so a new sale or response appears without a manual refresh, and the Live pill shows whether that stream is connected.",
      "Beside the pill is the notification-sound switch, which plays a short beep when a new sale or response arrives — the thing that makes an unattended kitchen or counter screen useful, since nobody is watching the list.",
      "The switch is a per-workspace setting, saved the moment you flip it: enable it, reload the page (or open /sales from a different browser, or another device), and it is still on, because the value is stored on the workspace rather than in that browser. If the save is rejected the switch goes back to its previous position and the failure is shown, and if the stored value itself cannot be read the page says so rather than showing the switch as off — the control never displays a value the server does not hold.",
      "Every workspace starts muted, and the setting applies only to the dashboard — buyers are never played a sound.",
      "The same value is the sound_enabled field on GET/PUT /v1/workspaces/{id}/settings and the set_realtime_sound MCP tool, and it can also be set in Settings → Storefront. Use the MCP tool get_realtime_status to read a workspace's sound_enabled and stream_url.",
    ],
  },
];

export default function SalesDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Sales, responses &amp; analytics</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        What the Sales page shows, how a response-only form fits into it, what the per-form counter
        counts, how a partial refund behaves, and how the realtime stream and the order-screen beep
        work.
      </p>

      <nav className="mt-6 rounded-lg border border-slate-200 bg-slate-50 p-4">
        <div className="text-xs font-medium uppercase tracking-wide text-slate-500">On this page</div>
        <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm">
          {SECTIONS.map((s) => (
            <li key={s.id}>
              <a href={`#${s.id}`} className="text-sky-700 hover:underline">
                {s.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>

      {SECTIONS.map((s) => (
        <section key={s.id} id={s.id} className="mt-10 scroll-mt-24">
          <h2 className="text-lg font-semibold text-slate-900">{s.title}</h2>
          {s.body.map((p, i) => (
            <p key={i} className="mt-3 text-sm leading-6 text-slate-600">
              {p}
            </p>
          ))}
        </section>
      ))}
    </div>
  );
}
