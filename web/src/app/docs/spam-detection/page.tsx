import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

export const metadata: Metadata = docsMetadata({
  title: "AI spam detection — JomForm Docs",
  description:
    "How JomForm's AI spam detection works, what happens to a flagged entry, and how to turn it off for one workspace from the dashboard or over MCP.",
  path: "/docs/spam-detection",
});

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "how-it-works",
    title: "How AI spam detection works",
    body: [
      "When someone submits a public form on your storefront, JomForm accepts the entry immediately and records it. In the background it then sends the answers the sender gave — never the page's own hidden anti-spam fields — together with the submitter's email address, the client IP and the form's title to a dedicated AI decision model, which answers with a spam confidence — a number from 0 (certainly genuine) to 1 (certainly spam). JomForm flags the entry when that confidence is at or above the threshold (0.5 by default). Nothing about that call ever delays the submitter: the entry is already saved before the judge is asked.",
      "The judge reads the message, not its packaging. A phone or WhatsApp number a sender leaves so you can reply is an ordinary answer, not evidence of spam, and an enquiry written in Malay, English, Chinese, Tamil or Arabic is judged on what it says rather than on the language it is written in.",
      "An entry the judge scores below the threshold is left exactly as it was recorded. An entry it scores at or above the threshold is flagged on the row itself (is_spam) with the reason — which names the judge's model — and it appears in the Spam queue in your dashboard, where you can review it and un-flag a false positive.",
      "The judge is deliberately conservative about itself rather than about your entries: if the AI call fails, times out, or comes back with no confidence at all, the entry is left unflagged. A judge that cannot decide never marks your entry as spam.",
      "Two things always skip the judge: a submission made by a signed-in owner or admin of the workspace (so you can test your own form without being flagged), and any workspace that has turned the judge off (see below).",
    ],
  },
  {
    id: "what-a-flag-means",
    title: "What a flagged entry actually means",
    body: [
      "A flag is a suggestion, not a decision, and it has two consequences worth knowing before you rely on it.",
      "First, the entry is marked. Anything in your dashboard or over the API that reports the spam state will show that entry as flagged until someone clears it — an entry that is flagged and never reviewed stays flagged permanently.",
      "Second, a flagged entry is deleted. An hourly sweep permanently deletes spam-flagged submissions and orders older than the retention period (30 days by default). The deletion only ever touches rows that are still flagged, so un-flagging an entry is what saves it — an unflagged entry is never auto-deleted. This is the reason to review the queue: a real order or registration that the judge got wrong, and that nobody reviews, is gone 30 days later.",
      "Because the judge reads each entry on its own, a form can be flagged consistently even when every entry is genuine. If that happens, turn the judge off for the workspace.",
    ],
  },
  {
    id: "per-workspace-toggle",
    title: "Turning AI spam detection off (per workspace)",
    body: [
      "Spam detection is set per workspace, not per form. Turn it off in the dashboard under Settings → Storefront, where the AI spam detection switch sits with the other workspace settings, then save. The setting takes effect for new submissions immediately.",
      "When the switch is off, JomForm does not send anything to the AI judge at all — the work is never queued, so nothing is evaluated and nothing is flagged. New entries are recorded normally and are simply never marked as spam: the Spam page stays empty, and no entry can be auto-deleted by the spam retention sweep, because the sweep only deletes entries that are flagged.",
      "Turning it off does not un-flag entries that were already flagged. Those keep their flag and remain subject to the retention sweep until someone clears them, so clear the queue before switching the judge off if there is anything in it you want to keep.",
      "The setting is per workspace, so a second workspace is unaffected: you can run the judge on one storefront and switch it off on another.",
      "Every workspace starts with the judge on. Adding this setting changed nothing for existing workspaces — the judge only stops when you explicitly turn it off.",
    ],
  },
  {
    id: "mcp",
    title: "Configuring it over MCP",
    body: [
      "The switch is available to AI agents through the workspace settings tools, so an agent can read and change it the same way it changes any other workspace setting.",
      "Set spam_judge_enabled to false with set_workspace_settings(workspace_id, spam_judge_enabled: false) to turn the judge off, and to true to turn it back on. get_workspace_settings(workspace_id) returns the current value as spam_judge_enabled. Omitting the field when you call set_workspace_settings leaves it unchanged, so editing an unrelated setting — a footer, a contact phone number — never switches the judge on or off behind your back.",
      "The same field is on the REST settings endpoint (GET/PUT /v1/workspaces/{id}/settings), which is what the dashboard itself uses.",
    ],
  },
  {
    id: "unflagging",
    title: "Reviewing and clearing flagged entries",
    body: [
      "The Spam page lists flagged submissions and flagged orders. Clearing a flag restores the entry to normal: the flag and the reason are removed, the entry is no longer deleted by the retention sweep, and it no longer counts as spam anywhere.",
      "One case clears itself: an order that is flagged and then receives a successful payment is un-flagged automatically, because a customer who actually paid is not spam.",
      "The same review actions are available to AI agents through MCP with list_spam_submissions, list_spam_sales, unspam_submission and unspam_sale.",
    ],
  },
];

export default function SpamDetectionDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">AI spam detection</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        JomForm reviews form entries for spam with an AI judge running in the background. This page
        explains what the judge does, what a flagged entry means for your data, and how to turn the
        judge off for a workspace — from the dashboard or over MCP.
      </p>

      <div className="mt-8 space-y-8">
        {SECTIONS.map((s) => (
          <section key={s.id} id={s.id} className="scroll-mt-20">
            <h2 className="text-xl font-semibold text-slate-900">{s.title}</h2>
            <div className="mt-2 space-y-2">
              {s.body.map((p, i) => (
                <p key={i} className="text-sm leading-relaxed text-slate-600">
                  {p}
                </p>
              ))}
            </div>
          </section>
        ))}
      </div>

      <p className="mt-10 text-sm leading-relaxed text-slate-600">
        See also the MCP tool reference for the exact tool signatures, and the Settings page in your
        dashboard for the switch itself.
      </p>
    </div>
  );
}
