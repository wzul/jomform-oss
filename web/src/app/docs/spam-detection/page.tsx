import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "AI spam detection — JomForm Docs",
  description:
    "How JomForm's AI spam detection works, what happens to a flagged entry, and how to turn it off for one workspace from the dashboard or over MCP.",
  path: "/docs/spam-detection",
});

const SECTIONS: Section[] = [
  {
    id: "how-it-works",
    title: "How AI spam detection works",
    icon: "🤖",
    body: [
      "When someone submits a public form, JomForm accepts and records the entry **immediately**.",
      "In the background it sends the answers the sender gave, never the page's own hidden anti-spam fields, to a dedicated AI decision model.",
      {
        list: [
          "It also sends the submitter's email address, the client IP and the form's title.",
          "The judge answers with a spam confidence: 0 for certainly genuine, 1 for certainly spam.",
          "A score at or above the **threshold**, 0.5 by default, flags the entry.",
        ],
      },
      "Nothing about that call delays the submitter. The entry is saved before the judge is asked.",
      {
        list: [
          "A phone or WhatsApp number a sender leaves so you can reply is an ordinary answer, not evidence of spam.",
          "An enquiry in Malay, English, Chinese, Tamil or Arabic is judged on what it says, not on its language.",
        ],
      },
      "A flagged entry carries `is_spam` with the reason, which names the judge's model, and lands in the **Spam queue**. There you can un-flag a false positive.",
      "The judge is conservative about itself: if the call fails, times out or returns no confidence, the entry stays unflagged.",
      {
        note: "Two things always skip the judge: a submission from a signed-in owner or admin of the workspace, and any workspace that has turned the judge off.",
      },
    ],
  },
  {
    id: "what-a-flag-means",
    title: "What a flagged entry actually means",
    icon: "🚩",
    body: [
      "A flag is a **suggestion**, not a decision. It has two consequences worth knowing before you rely on it.",
      "First, the entry is marked. Anything in the dashboard or over the API that reports the spam state shows it as flagged until someone clears it.",
      "A flagged entry that is never reviewed stays flagged **permanently**.",
      {
        warn: "Second, a flagged entry is deleted. An hourly sweep permanently deletes spam-flagged submissions and orders older than **30 days** by default.",
      },
      "The deletion only touches rows that are still flagged, so un-flagging an entry is what saves it. An unflagged entry is never auto-deleted.",
      {
        tip: "That is the reason to review the queue: a real order or registration the judge got wrong, and nobody reviews, is gone 30 days later.",
      },
      "Because the judge reads each entry on its own, a form can be flagged consistently even when every entry is genuine.",
      "If that happens, turn the judge off for the workspace.",
    ],
  },
  {
    id: "per-workspace-toggle",
    title: "Turning AI spam detection off (per workspace)",
    icon: "🎚️",
    body: [
      "Spam detection is set **per workspace**, not per form. Turn it off under Settings → Storefront, then save.",
      "It takes effect for new submissions **immediately**.",
      "With the switch off, JomForm sends nothing to the AI judge. The work is never queued, so nothing is evaluated and nothing is flagged.",
      "New entries are recorded normally and never marked as spam. The Spam page stays empty, and no entry can be auto-deleted.",
      {
        warn: "Turning it off does not un-flag entries already flagged. Those keep their flag and stay subject to the retention sweep.",
      },
      "Clear the queue before switching the judge off if there is anything in it you want to keep.",
      "The setting is per workspace, so a second workspace is unaffected. Run the judge on one storefront and switch it off on another.",
      {
        note: "Every workspace starts with the judge on. Existing workspaces changed nothing; the judge only stops when you turn it off explicitly.",
      },
    ],
  },
  {
    id: "mcp",
    title: "Configuring it over MCP",
    icon: "🔧",
    body: [
      "The switch is available to AI agents through the **workspace settings tools**, the same way any other workspace setting is.",
      "Set `spam_judge_enabled` to false with `set_workspace_settings(workspace_id, spam_judge_enabled: false)` to turn the judge off, and to true to turn it back on.",
      "`get_workspace_settings(workspace_id)` returns the current value as `spam_judge_enabled`.",
      "Omitting the field leaves it unchanged.",
      "Editing an unrelated setting, such as a footer or a contact phone number, never switches the judge on or off behind your back.",
      {
        note: "The same field is on the REST settings endpoint, `GET/PUT /v1/workspaces/{id}/settings`, which is what the dashboard itself uses.",
      },
    ],
  },
  {
    id: "unflagging",
    title: "Reviewing and clearing flagged entries",
    icon: "✅",
    body: [
      "The Spam page lists flagged **submissions** and flagged **orders**.",
      "Clearing a flag restores the entry to normal. The flag and the reason are removed, and it is no longer deleted by the retention sweep.",
      "It no longer counts as spam anywhere.",
      {
        tip: "One case clears itself: an order that is flagged and then receives a successful payment is un-flagged automatically, because a customer who actually paid is not spam.",
      },
      "The same review actions are open to agents over MCP with `list_spam_submissions`, `list_spam_sales`, `unspam_submission` and `unspam_sale`.",
      {
        note: "See the [MCP tool reference](/docs/mcp) for the exact tool signatures, and the Settings page in your dashboard for the switch itself.",
      },
    ],
  },
];

export default function SpamDetectionDocsPage() {
  return (
    <DocsPage
      title="AI spam detection"
      lead="An AI judge reviews form entries in the background. Here is what it does, what a flag means for your data, and how to turn it off."
      sections={SECTIONS}
    />
  );
}
