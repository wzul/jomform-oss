import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// In-app feedback: the docs page the dashboard's inline "?" links to
// (AGENTS.md DOCUMENTATION PARITY rule 4), and the page the MCP tool reference
// points at. It serves both sides of the same thread: the user who files a
// report and reads the reply, and the platform owner who triages the queue.

export const metadata: Metadata = docsMetadata({
  title: "In-app feedback — JomForm Docs",
  description:
    "How JomForm's in-app feedback works: file a report or feature request from the dashboard, follow the conversation in the thread, and triage the queue as a platform admin.",
  path: "/docs/feedback",
});

const SECTIONS: Section[] = [
  {
    id: "what",
    title: "What in-app feedback is",
    icon: "💬",
    body: [
      "Feedback is a **conversation** with the JomForm team, opened from inside the dashboard.",
      "Every signed-in user has a **Feedback** link in the sidebar. It is not restricted to account owners, and it does not need a workspace at all.",
      "A feedback item is a **thread**, not a message. You write it once, the team replies in the same place, and you can add detail at any time.",
      "The thread keeps its whole history, so you can come back weeks later and read what was said.",
      "Use it for a bug, a confusing screen, or a feature your business needs.",
      {
        note: "It is not a way to reach a specific merchant or buyer. Those conversations stay inside the form and receipt features.",
      },
    ],
  },
  {
    id: "where-it-lives",
    title: "Where a thread lives",
    icon: "📌",
    body: [
      "A thread belongs to the **person who filed it**, not to a workspace or a customer account.",
      "It is owned by the platform, which is why it never appears in a form list, the Sales page, a CSV export, or any account's spam review queue.",
      {
        list: [
          "The thread follows **you**, not an account. Change your email address and your existing threads are still yours.",
          "Feedback is **never** subject to the 30-day spam auto-delete that applies to spam-flagged form submissions.",
          "A report you filed stays until the conversation is closed.",
        ],
      },
      "Your threads are **private to you**. Another signed-in user cannot list, open or reply to a thread that is not theirs.",
      "Asking for a thread that is not yours by its id answers the same way as asking for an id that does not exist.",
    ],
  },
  {
    id: "statuses",
    title: "What the statuses mean",
    icon: "🚦",
    body: [
      {
        list: [
          "**open**: filed and not yet picked up. Every new thread starts here.",
          "**in_progress**: a team member has replied and is looking into it.",
          "**resolved**: the report has been dealt with. You can still read the thread and add a message if it is not actually fixed.",
          "**closed**: the conversation is finished and no further messages are expected.",
        ],
      },
      "A thread moves to **in_progress** automatically the first time an admin replies, because an answered report is by definition being worked on.",
      "The status belongs to the **whole thread**, not to one message. It answers one question: is this dealt with?",
    ],
  },
  {
    id: "notifications",
    title: "How you hear about a reply",
    icon: "✉️",
    body: [
      "When the team replies you get an **email** carrying the thread's subject and the reply in full.",
      "The thread is always in the dashboard, so the email is a prompt to go and read it rather than the only copy.",
      "The **Feedback** page lists every thread you filed, most recently active first, with its status. Selecting one opens the full exchange.",
      "The email speaks your language. Your **dashboard language** sets the subject, the body and the footer.",
      "That is Profile, then language, stored per account. If you never chose one, the message is in **English**.",
      "The link opens the **exact thread** that was answered. Replying to the email also works, through a **Reply-To** address that a person reads.",
      "Replying by email does not replace the thread in the dashboard, which stays the record of the conversation.",
      {
        warn: "If that email cannot be handed to the delivery queue, the failure is written to the log and marked on the thread. The team can re-send it from there.",
      },
    ],
  },
  {
    id: "limits",
    title: "Rate limits and what protects the queue",
    icon: "⏳",
    body: [
      "Only **signed-in users** can file feedback, so the queue cannot be flooded by anonymous submissions.",
      "The guard is a **per-user daily limit of 10 new threads**. Its purpose is to keep one account from burying the queue, not to judge a report.",
      "A limit can delay a message but it cannot misclassify a genuine complaint.",
      "If you reach the limit you can still add messages to a thread you already have, which is usually the better place to continue.",
      "The limit does not affect other users. One account reaching it does not change anyone else's allowance.",
      "A report must carry at least a few words of content. An empty submission is rejected with a message rather than silently accepted.",
    ],
  },
  {
    id: "formatting",
    title: "Formatting in a thread",
    icon: "✍️",
    body: [
      "A message body is **rendered**, not printed. Bold, lists and code from the in-app assistant show as real formatting, not as the markers that were typed.",
      {
        list: [
          "**Rendered**: bold, italic, inline code, fenced code blocks, bulleted and numbered lists, links and paragraph breaks.",
          "**Not rendered**: tables, images, headings and raw HTML. HTML in a message shows as text and never becomes part of the page.",
          "A fenced code block scrolls sideways inside its card instead of stretching the thread.",
        ],
      },
      "An underscore means italic only when it is written around a word, as in _condong_.",
      "Inside a word it is an ordinary character, so `form_answers_value` and `jf_2fa_pending` read back with their underscores intact.",
      "Your threads and the admin triage view render a body the same way.",
      "The stored text is exactly what was written, so nothing is stripped on the way in or out.",
    ],
  },
  {
    id: "admin",
    title: "For platform admins: triage",
    icon: "🛠️",
    body: [
      "The admin page carries a **Feedback** section listing every thread filed by any user, newest activity first, with a filter and count per status.",
      "Selecting one shows the full conversation and a reply box. The team's notification email links straight to the filed thread.",
      "Replying as an admin sends the filer an email and moves an **open** thread to **in_progress**.",
      "Changing the status is a deliberate, separate action, so a reply never reopens a thread you resolved or closed.",
      "When a thread is filed the team is notified through the same system that delivers form notifications. There is no separate channel.",
      {
        note: "Agents use `list_feedback`, `get_feedback_thread` and `reply_feedback`, scoped to the caller, so an agent sees only that user's threads.",
      },
      {
        note: "`set_feedback_status` and `retry_feedback_notify` are **super-admin only**. A retry re-sends through the same channel, and the thread carries a `notify_failed_at` marker naming who was not told.",
      },
      {
        table: {
          label: "Retry notification outcomes",
          head: ["Value", "What it means"],
          rows: [
            ["`queued` or `sent`", "Mail left for delivery."],
            ["`failed`", "The attempt did not get through, so the marker and the banner stay."],
            ["`none`", "The thread had no recorded failure to re-send."],
            ["`skipped`", "No recipient is configured for that notification. Ask a platform admin to check the super-admin allowlist. Nothing was lost."],
          ],
        },
      },
    ],
  },
];

export default function FeedbackDocsPage() {
  return (
    <DocsPage
      title="In-app feedback"
      lead="A direct line to the JomForm team from inside the dashboard: file a report or a feature request, follow the reply in the same thread, and triage the queue as a platform admin."
      sections={SECTIONS}
    />
  );
}
