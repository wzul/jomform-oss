import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

// Activity — the docs page the Activity page's inline "?" link points at
// (AGENTS.md DOCUMENTATION PARITY rule 4).
//
// WHY THIS PAGE EXISTS (card t_eb09b25a). The audit trail was written, and
// readable over REST and MCP, but no shipped dashboard page rendered it — so
// from a merchant's point of view every event was write-only, including the
// `workspace.deleted` row that a workspace deletion records. The Activity page
// is that surface; this page says what it shows, what it deliberately does NOT
// show, and how it relates to the two API surfaces that read the same table.

export const metadata: Metadata = docsMetadata({
  title: "Activity log — JomForm Docs",
  description:
    "The per-workspace audit trail: every recorded action in your workspace, newest first, filterable by event type and date. What it records, what it never records, how long rows are kept, and how the same data is read over the REST API and MCP.",
  path: "/docs/activity",
});

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "what",
    title: "What the activity log is",
    body: [
      "Activity is a record of what happened in the workspace you have selected: forms created, published, restored and purged; products edited, archived and deleted; sales captured, confirmed and refunded; API keys and webhook endpoints added and revoked; domains added and verified; agent tool calls; and workspace-level changes such as a deletion. It is append-only and it is read from the top — newest first.",
      "It matters most for the destructive actions. Deleting a workspace and its payment account is permanent, and the same is true of a form purge and a product delete; the log is the only place in the product where you can see afterwards that one happened, which workspace it named, and who did it. Without it, the only way to read that record is a hand-made REST call with an API key or an MCP client.",
      "The trail is per workspace, not per user. Everyone who can reach the workspace sees the same rows, because the question it answers — what has happened to this workspace — is about the workspace. Opening Activity after switching workspaces in the top bar shows the newly selected workspace's history; a workspace you are not a member of can never be read from here.",
    ],
  },
  {
    id: "reading",
    title: "Reading and filtering it",
    body: [
      "The table has three columns: the event name, the event's own detail (the small JSON payload the action recorded — a form id and slug, a ref code and amount, a key name), and when it happened, in your profile's timezone (Profile → Identity; the browser's own zone when that is set to Auto).",
      "The event-type filter narrows the list to one family. Every family the API records has an option — form.*, product.*, sale.*, settings.*, workspace.*, template.*, media.*, upload.*, profile.*, auth.*, mcp.*, chat.*, admin.* and feedback.* — and the filter is a server-side prefix match, so choosing sale.* asks the server for the sale rows rather than downloading everything and hiding the rest. The two date fields filter by when the event happened; both ends are inclusive, and the From box starts at 00:00 while the To box runs to 23:59 of the day you picked.",
      "Twenty rows are shown per page, with the page number, the total and a Previous/Next pair underneath. If a load fails the page says so and offers a retry rather than showing an empty table — an empty log and a broken request must not look the same.",
    ],
  },
  {
    id: "not",
    title: "What it does not record",
    body: [
      "It records that something happened, not what was typed. A chat exchange is logged as a chat event with how many lookups it made, and a confirmed proposal is logged with the tool name and whether it succeeded — your messages, the replies, and the values a confirmed change set are not in the log. A refund records the ref code and the amount, not the buyer's card or bank details. Nothing in this table is customer form-answer content; that lives with the submission itself, on the form's own list.",
      "It is also, deliberately, not editable. There is no delete, no edit and no export control on the Activity page: an audit trail that the person being audited can prune is not a trail. Rows leave the table only through the retention sweep below.",
    ],
  },
  {
    id: "retention",
    title: "How long rows are kept",
    body: [
      "Events are kept for 90 days by default. Sign-in events (auth.*) and settings changes (settings.*) are kept for 180 days, because those are the classes an audit question is usually asked about months later. The sweep that applies this runs on the server on a schedule; the Activity page states the retention in the same terms on the page itself, so the bound on what you can see is visible where you read it rather than buried in a setting.",
      "The two numbers are deployment configuration (EVENT_RETENTION_DAYS and EVENT_AUDIT_RETENTION_DAYS), so a self-hosted JomForm can keep more; this deployment's defaults are the 90/180 stated here.",
    ],
  },
  {
    id: "api",
    title: "The same data over the REST API and MCP",
    body: [
      "Activity is the dashboard surface of one table, and it adds no capability the API lacked: the page reads the same endpoint an API key or an agent uses.",
      "Over REST: GET /v1/rest/events returns the paginated list (event_name, from, to, page, limit) for the account your key belongs to, GET /v1/rest/events/count returns just the number, and GET /v1/rest/events/breakdown returns counts grouped by event name. With a session the ?workspace=<slug> scope narrows all three to one workspace, exactly as this page does; an API key has no session, so it reads the account the key belongs to.",
      "Over MCP: list_events is the same read for a connected agent, with the same event_name / from / to / page / limit arguments. An agent that acted on your workspace can therefore read back what it did — every tool call is recorded as an mcp.tool_call event naming the tool, the agent and whether it succeeded.",
      "Both surfaces are read-only. There is no endpoint anywhere that edits or deletes an event row.",
    ],
  },
];

export default function ActivityDocsPage() {
  return (
    <article className="mx-auto w-full max-w-3xl px-6 py-12">
      <p className="text-xs font-medium uppercase tracking-wider text-[var(--jf-muted)]">JomForm Docs</p>
      <h1 className="mt-2 text-2xl font-semibold tracking-tight">Activity log</h1>
      <p className="mt-3 text-sm text-[var(--jf-muted)]">
        Every action recorded in the workspace you have selected — including the destructive ones —
        newest first, filterable by event type and date. Append-only, and readable over REST and MCP
        as well as here.
      </p>
      <nav className="mt-6 flex flex-wrap gap-2">
        {SECTIONS.map((s) => (
          <a
            key={s.id}
            href={`#${s.id}`}
            className="rounded-full bg-slate-100 px-3 py-1 text-xs text-slate-700 hover:bg-[var(--jf-primary)] hover:text-white"
          >
            {s.title}
          </a>
        ))}
      </nav>
      <div className="mt-8 space-y-8">
        {SECTIONS.map((s) => (
          <section key={s.id} id={s.id} className="scroll-mt-20">
            <h2 className="text-lg font-semibold tracking-tight">{s.title}</h2>
            <div className="mt-2 space-y-3">
              {s.body.map((p, i) => (
                <p key={i} className="text-sm leading-relaxed text-[var(--jf-text)]">
                  {p}
                </p>
              ))}
            </div>
          </section>
        ))}
      </div>
      <p className="mt-10 text-xs text-[var(--jf-muted)]">
        Related: <a className="underline decoration-dotted underline-offset-2" href="/docs/mcp">the MCP tool reference</a>{" "}
        (list_events) and <a className="underline decoration-dotted underline-offset-2" href="/docs/api">the REST API reference</a>{" "}
        (GET /v1/rest/events).
      </p>
    </article>
  );
}
