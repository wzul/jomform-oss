import type { Metadata } from "next";

// In-app feedback — the docs page the dashboard's inline "?" links to
// (AGENTS.md DOCUMENTATION PARITY rule 4), and the page the MCP tool reference
// entry points at.
//
// It documents the feature for BOTH audiences, because the same thread is seen
// from two sides: a user who files a report and reads the reply, and the
// platform owner who triages the queue. It also states the two design facts that
// are otherwise invisible from the UI — where a thread lives (nowhere near an
// account) and why the anti-abuse guard here is a rate limit rather than a spam
// classifier.

export const metadata: Metadata = {
  title: "In-app feedback — JomForm Docs",
  description:
    "How JomForm's in-app feedback works: file a report or feature request from the dashboard, follow the conversation in the thread, and triage the queue as a platform admin.",
  alternates: { canonical: "https://jomform.com/docs/feedback" },
};

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "what",
    title: "What in-app feedback is",
    body: [
      "Feedback is a conversation with the JomForm team, opened from inside the dashboard. Every signed-in user has a Feedback link in the sidebar — it is not restricted to account owners, and it does not depend on having a workspace at all.",
      "A feedback item is a thread rather than a message: you write it once, the team replies in the same place, and you can add more detail at any time. The thread keeps its whole history, so you can come back weeks later and read what was said.",
      "Use it for anything the product does not do well or does not do at all: a bug, a confusing screen, or a feature you need for your business. It is not a way to reach a specific merchant or buyer — those conversations stay inside the form and receipt features.",
    ],
  },
  {
    id: "where-it-lives",
    title: "Where a thread lives",
    body: [
      "A feedback thread belongs to the person who filed it, not to a workspace or a customer account. It is owned by the platform, which is why it never appears in a form list, in the Sales page, in a CSV export, or in the spam review queue of any account.",
      "Two consequences are worth knowing. First, the thread follows you rather than an account: if you change your email address you still see your existing threads under the new address. Second, feedback is never subject to the 30-day spam auto-delete that applies to spam-flagged form submissions — a report you filed stays until the conversation is closed.",
      "Your threads are private to you. Another signed-in user cannot list, open or reply to a thread that is not theirs; asking for one by its id answers the same way as asking for an id that does not exist.",
    ],
  },
  {
    id: "statuses",
    title: "What the statuses mean",
    body: [
      "open — filed and not yet picked up. This is the state a new thread starts in.",
      "in_progress — someone on the team has replied and is looking into it. A thread moves here automatically the first time an admin replies, because an answered report is by definition being worked on.",
      "resolved — the report has been dealt with. You can still read the thread and add a message if it is not actually fixed.",
      "closed — the conversation is finished and no further messages are expected.",
      "The status is a property of the whole thread, not of an individual message: it answers 'is this dealt with?', which is the question both you and the team need answered at a glance.",
    ],
  },
  {
    id: "notifications",
    title: "How you hear about a reply",
    body: [
      "When the team replies you receive an email telling you there is an answer, carrying the thread's subject and the reply itself in full. The full conversation is also always in the dashboard on the Feedback page, so the email is a prompt to go and read it rather than the only copy.",
      "When you file a thread the team is notified in the same way, through the same email system that delivers form notifications. There is no separate channel and no separate inbox to watch.",
      "If that email cannot be handed to the delivery queue, the failure is not silent: it is written to the log and marked on the thread itself, so the team sees it on the thread they are already looking at and can re-send it. A notification that fails is recorded as a fact about the thread, never dropped — a report that was answered but whose answer never arrived is the one outcome this channel must not hide.",
      "The Feedback page shows every thread you have filed, most recently active first, with its status. Selecting one opens the full exchange.",
    ],
  },
  {
    id: "limits",
    title: "Rate limits and what protects the queue",
    body: [
      "Only signed-in users can file feedback, so the queue cannot be flooded by anonymous submissions. The guard that applies instead is a per-user daily limit of 10 new threads. Its purpose is to keep one account from burying the queue, not to judge the quality of a report — a limit can delay a message but it cannot misclassify a genuine complaint.",
      "If you reach the limit you can still add messages to a thread you already have, which is usually the better place to continue anyway. The limit does not affect other users; one account reaching it does not change anyone else's allowance.",
      "A report must carry at least a few words of content — an empty submission is rejected with a message rather than silently accepted.",
    ],
  },
  {
    id: "formatting",
    title: "Formatting in a thread",
    body: [
      "A message body is rendered, not printed. A report filed from the in-app assistant can contain bold text, lists and code, and a reply you write can too — the thread shows those as real formatting, not as the markers that were typed. Before this, an asterisk written for emphasis appeared as an asterisk.",
      "Bold, italic, inline code, fenced code blocks, bulleted and numbered lists, links and paragraph breaks are rendered. Tables, images, headings and raw HTML are not: the body is never inserted into the page as code, so HTML in a message shows as text rather than becoming part of the page. A fenced code block scrolls sideways inside its card instead of stretching the thread.",
      "An underscore means italic only when it is written around a word, as in _condong_. Inside a word it is an ordinary character, so a report that names a column or a setting — form_answers_value, jf_2fa_pending — reads back with its underscores intact rather than with them consumed as emphasis.",
      "Your threads and the admin triage view render a body the same way, and the stored text is exactly what was written — nothing is stripped on the way in or out.",
    ],
  },
  {
    id: "admin",
    title: "For platform admins: triage",
    body: [
      "The admin page carries a Feedback section listing every thread filed by any user, newest activity first, with a filter per status and a count beside each status chip. Selecting a thread shows the full conversation and a reply box.",
      "Replying as an admin sends the filer an email and moves an open thread to in_progress. Changing the status is a separate, deliberate action — replying does not silently reopen a thread you have already resolved or closed.",
      "The same queue is available to a connected AI agent through the MCP tools list_feedback, get_feedback_thread, reply_feedback, set_feedback_status and retry_feedback_notify. Set_feedback_status and retry_feedback_notify are super-admin only; the other three are scoped to the caller, so an agent connected on behalf of a user sees that user's threads and no others. Retry_feedback_notify is the recovery path for a notification that could not be delivered: the thread carries a notify_failed_at marker naming the party who was not told, and the tool re-sends it through the same channel.",
      "A retry reports what it actually did in its notification field. queued or sent means a mail left for delivery; failed means the attempt was made and did not get through, so the marker stays and the banner remains. none means the thread had no recorded failure to re-send. skipped means nothing was attempted because no recipient is configured for that notification — ask a platform admin to check the super-admin allowlist; nothing was lost, and no mail is in flight.",
    ],
  },
];

export default function FeedbackDocsPage() {
  return (
    <>
      <h1 className="text-2xl font-semibold text-slate-900">In-app feedback</h1>
      <p className="mt-3 text-sm leading-6 text-slate-600">
        A direct line to the JomForm team from inside the dashboard: file a bug or a
        feature request, follow the reply in the same thread, and — for platform
        admins — triage the whole queue.
      </p>

      <nav className="mt-6 rounded-lg border border-slate-200 bg-slate-50 p-4">
        <div className="text-xs font-medium uppercase tracking-wide text-slate-500">
          On this page
        </div>
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
    </>
  );
}
