import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Sales, responses & analytics — JomForm Docs",
  description:
    "The Sales page: every sale and every response in one list, per-form counts, partial refunds, and the realtime notifications and order-screen beep.",
  path: "/docs/sales",
});

// SHORT ON PURPOSE. Every API name, tool name and field name survives, because
// /docs is what a developer greps. The section ids are a contract with /docs and
// with the dashboard's inline "?" links (issue #598, #447).
const SECTIONS: Section[] = [
  {
    id: "sales",
    title: "Sales",
    icon: "💰",
    body: [
      "Each row carries the **reference code**, status, buyer, amount, method and date.",
      "A status is **pending**, **confirmed**, **failed** or **refunded**. From here you confirm bank transfers, capture or release skip-capture authorisations, refund in full or in part, and export to CSV.",
      "Amounts stay in the currency the sale was recorded in: **US$24.90** for a USD sale, **RM24.90** for an MYR one.",
      "With a buyer-borne fee the amount column also shows the fee and **You receive** (total − fee); the drawer breaks it down in that order.",
      "The workspace's current setting never relabels history. `GET /v1/rest/sales` and `list_sales` carry it as **currency**, and a refund is asked for in that same currency.",
      {
        note: "A form's mode names its entries. A payment form offers **Sales**; a response-only form offers **Submission**. Both open the same per-form list. `GET /v1/rest/forms` reports the mode as **mode**.",
      },
    ],
  },
  {
    id: "combined-list",
    title: "One list for sales and responses",
    icon: "🧾",
    body: [
      "With no form filter the page is the whole record set: **every sale and every response**, newest first, under one combined count.",
      {
        list: [
          "Filter by a form to see that form's own entries. A response form switches the table to the answer columns.",
          "**Export CSV** writes the same rows with the same filters. A combined export adds a **record_type** column (sale or submission).",
          "Money columns are **total**, **fee**, **net**, **refunded** and **outstanding**, in the sale's own currency. **net** is total − fee: what you keep.",
        ],
      },
      {
        tip: "The dashboard's recent list and the MCP tools `list_sales` and `export_sales_csv` read the same rows, so the page, the CSV and your agent never disagree.",
      },
    ],
  },
  {
    id: "responses",
    title: "Responses (response-only forms)",
    icon: "📝",
    body: [
      "Set the form mode to **response** in the builder: no products, no payment. Entries are stored as responses and still appear on the Sales page.",
      "Open a response form from **Forms → Submission** to see when each entry arrived, plus the buyer and the answers. The detail drawer holds every field value.",
      "A response has no payment status, so the status filter does not appear. **Export CSV**, capture and refund are payment-only.",
      {
        note: "Agents read responses with `list_form_submissions`. `list_sales` also returns them for a response form's id.",
      },
    ],
  },
  {
    // The dashboard's inline "?" (issue #598, AGENTS.md docs-parity rule 4)
    // deep-links this section, and /docs carries the same anchor.
    id: "analytics",
    title: "Form analytics (the per-form counter)",
    icon: "📊",
    body: [
      "The dashboard shows one card per form with the number of entries that form recorded, read from the table it writes to.",
      {
        list: [
          "Response form: the card says **Submissions** and counts responses.",
          "Payment form: the card says **Entries** and counts sales in every status.",
        ],
      },
      "The two tables are never added together, so a form cannot double count.",
      "Beside the count sit **Paid+Confirmed**, **Pending**, **Failed** and revenue net of refunds, labelled in the currency each sale was recorded in.",
      {
        note: "`get_form_analytics` reports the same rollup and names the form's **mode**, so the figure is labelled correctly. `GET /v1/rest/dashboard-bundle` carries it as **form_analytics**.",
      },
    ],
  },
  {
    id: "refunds",
    title: "Partial refunds",
    icon: "↩️",
    body: [
      "Refund a paid or confirmed sale in full or in part. JomForm records how much was actually returned, in `refunded_sen`.",
      "A partial refund keeps the status and adds a **Partially refunded** marker. The balance in `outstanding_sen` stays refundable until it reaches zero, and only then does the sale read **refunded**.",
      "The Sales page, `get_sales_summary`, analytics, the CSV export and `refund_sale` all report the same refunded and outstanding figures.",
      {
        warn: "An amount that would push the refunded total past the sale total is rejected before the gateway is called. A **per-sale lock** stops two racing refunds from over-refunding.",
      },
    ],
  },
  {
    // The /sales sound toggle's inline "?" (issue #447, AGENTS.md docs-parity
    // rule 4) links here as /docs/sales#realtime-notifications.
    id: "realtime-notifications",
    title: "Realtime and the order-screen beep",
    icon: "🔔",
    body: [
      "While the page is open it listens to your workspace's realtime stream, so a new sale appears without a refresh. The **Live pill** shows whether the stream is connected.",
      "The **sound switch** beside it plays a short beep on each new sale or response. That is what makes an unattended kitchen or counter screen useful.",
      "It is a **per-workspace setting**, not a browser one: enable it, reload the page, and it is still on.",
      "If saving fails, the switch goes back to its previous position and the failure is shown.",
      "Every workspace starts muted: **only the dashboard plays a sound**.",
      {
        note: "The value is `sound_enabled` in the workspace settings, the `set_realtime_sound` tool, and Settings → Storefront. `get_realtime_status` reads `sound_enabled` and `stream_url`.",
      },
    ],
  },
];

export default function SalesDocsPage() {
  return (
    <DocsPage
      title="Sales, responses and analytics"
      lead="Every sale and every response in one list, with per-form counts, partial refunds and a beep when money arrives."
      sections={SECTIONS}
    />
  );
}
