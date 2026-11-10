import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// Activity is the docs page the Activity page's inline "?" link points at
// (AGENTS.md DOCUMENTATION PARITY rule 4).
//
// WHY THIS PAGE EXISTS (card t_eb09b25a). The audit trail was written, and
// readable over REST and MCP, but no shipped dashboard page rendered it, so
// from a merchant's point of view every event was write-only, including the
// workspace.deleted row a workspace deletion records. The Activity page is
// that surface; this page says what it shows, what it deliberately does not
// show, and how it relates to the two API surfaces that read the same table.

export const metadata: Metadata = docsMetadata({
  title: "Activity log — JomForm Docs",
  description:
    "The per-workspace audit trail: every recorded action in your workspace, newest first, filterable by event type and date. What it records, what it never records, how long rows are kept, and how the same data is read over the REST API and MCP.",
  path: "/docs/activity",
});

const SECTIONS: Section[] = [
  {
    id: "what",
    title: "What the activity log is",
    icon: "🧭",
    body: [
      "Activity records what happened in the workspace you have selected. It is **append-only** and read from the top, newest first.",
      {
        list: [
          "Forms created, published, restored and purged.",
          "Products edited, archived and deleted.",
          "Sales captured, confirmed and refunded.",
          "API keys and webhook endpoints added and revoked.",
          "Domains added and verified, and agent tool calls.",
          "Workspace changes such as a deletion.",
        ],
      },
      "It matters most for the **destructive actions**. Deleting a workspace and its payment account is permanent, and so is a form purge or a product delete.",
      "The log is **the only place in the product where you can see afterwards that one happened**, which workspace it named, and who did it.",
      "Without it, the only way to read that record is a hand-made REST call with an API key, or an MCP client.",
      "**The trail is per workspace, not per user**. Everyone who can reach the workspace sees the same rows.",
      "It answers what has happened to this workspace, so switching workspaces in the top bar shows the new one's history.",
      "A workspace you are not a member of is never readable here.",
    ],
  },
  {
    id: "reading",
    title: "Reading and filtering it",
    icon: "🔎",
    body: [
      "The table has three columns: **what happened**, the event's own detail, and when it happened.",
      "The first column is worded for people — “Workspace deleted” — and the raw event name (`workspace.deleted`) is the row's tooltip, as REST and MCP use it.",
      "The detail is the small JSON payload the action recorded: a form id and slug, a ref code and amount, or a key name.",
      {
        list: [
          "Times use your profile's timezone (Profile → Identity), or the browser's own zone when that is set to Auto.",
          "From and To are one range — From 00:00, To 23:59, both inclusive — and on a narrow screen the pair shares one line.",
        ],
      },
      {
        note: "The event-type filter is a **server-side prefix match**, so choosing `sale.*` asks the server for the sale rows rather than downloading everything.",
      },
      "Every family the API records has an option: form.*, product.*, sale.*, settings.*, workspace.*, template.*, media.*, upload.*, profile.*, auth.*, mcp.*, chat.*, admin.* and feedback.*",
      "**Twenty rows** are shown per page, with the page number, the total and a Previous/Next pair underneath.",
      {
        list: [
          "If a load fails the page says so and offers a retry rather than showing an empty table.",
          "An empty log and a broken request must not look the same.",
        ],
      },
    ],
  },
  {
    id: "not",
    title: "What it does not record",
    icon: "🚫",
    body: [
      "It records that **something happened**, not what was typed.",
      "A chat exchange is logged as a chat event with how many lookups it made, and a confirmed proposal with the tool name and whether it succeeded.",
      "Your messages, the replies and the values a confirmed change set are not in the log.",
      "A refund records the ref code and the amount, not the buyer's card or bank details.",
      "Nothing in this table is customer form-answer content. That lives with the submission itself, on the form's own list.",
      {
        warn: "There is no delete, no edit and no export control on the Activity page. **An audit trail that can be pruned is not a trail.**",
      },
      "It is deliberately not editable. Rows leave the table only through the retention sweep below.",
    ],
  },
  {
    id: "retention",
    title: "How long rows are kept",
    icon: "🧹",
    body: [
      "Events are kept for **90 days** by default.",
      "Sign-in events (auth.*) and settings changes (settings.*) are kept for 180 days, because those are the classes an audit question is asked about months later.",
      "The sweep that applies this runs on the server on a schedule.",
      "The page states the retention itself, so the bound on what you can see is visible where you read it.",
      {
        note: "Both numbers are deployment configuration (`EVENT_RETENTION_DAYS` and `EVENT_AUDIT_RETENTION_DAYS`), so a self-hosted JomForm can keep more. This deployment uses the 90 and 180 above.",
      },
    ],
  },
  {
    id: "api",
    title: "The same data over the REST API and MCP",
    icon: "🔌",
    body: [
      "Activity is the **dashboard surface** of one table. The page reads the same endpoint an API key or an agent uses.",
      {
        list: [
          "Over REST, `GET /v1/rest/events` returns the paginated list (event_name, from, to, page, limit) for the account your key belongs to.",
          "`GET /v1/rest/events/count` returns the count only.",
          "`GET /v1/rest/events/breakdown` returns counts grouped by event name.",
        ],
      },
      "With a session the `?workspace=<slug>` scope narrows all three to one workspace. An API key has no session, so it reads the account the key belongs to.",
      "Over MCP, `list_events` is the same read for a connected agent, with the same event_name, from, to, page and limit arguments.",
      "An agent that acted on your workspace can read back what it did.",
      "Every tool call is recorded as an `mcp.tool_call` event naming the tool, the agent and whether it succeeded.",
      {
        tip: "Both surfaces are **read-only**. There is no endpoint anywhere that edits or deletes an event row.",
      },
      {
        note: "Related: [the MCP tool reference](/docs/mcp) and [the REST API reference](/docs/api).",
      },
    ],
  },
];

export default function ActivityDocsPage() {
  return (
    <DocsPage
      title="Activity log"
      lead="Every action recorded in the workspace you have selected, including the destructive ones, newest first."
      sections={SECTIONS}
    />
  );
}
