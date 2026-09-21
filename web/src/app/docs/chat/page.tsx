import type { Metadata } from "next";

// In-app chat (phase 1) — the docs page the chat panel's inline "?" link points
// at (AGENTS.md DOCUMENTATION PARITY rule 4).
//
// It documents the feature by stating the CAPABILITY BOUNDARY first, because
// that boundary is the whole design: the assistant reads and files feedback, and
// it cannot change anything. It also documents the MCP tool the chat's one write
// pairs with (submit_feedback), the rate/cost limits and why they exist, and the
// behaviour that matters when the assistant does not know — the honest answer,
// not a guess.

export const metadata: Metadata = {
  title: "In-app assistant — JomForm Docs",
  description:
    "What JomForm's built-in chat assistant can and cannot do: it answers questions about your own forms, sales and products, and can file a feedback report — it can never change your account.",
  alternates: { canonical: "https://jomform.com/docs/chat" },
};

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "what",
    title: "What the assistant is",
    body: [
      "The assistant is a chat panel inside the dashboard. Ask it a question about your own account in plain language — which forms are live, what a form recorded, how many sales came in, what a customer's submission said, which subscriptions are past due — and it looks the answer up in your data with the same tools an AI agent would use over MCP.",
      "It is not a general chatbot and it is not a support ticket by another name. It answers from your data, and when the answer is not in your data it says so.",
      "It speaks the language you write in: English, Bahasa Melayu, 中文, தமிழ் or العربية.",
    ],
  },
  {
    id: "boundary",
    title: "What it cannot do",
    body: [
      "The assistant cannot change anything about your account except one thing: filing feedback. It cannot delete or archive a form or product, refund or confirm a sale, cancel a subscription, publish a page, change a setting, invite a member or revoke a key. Those abilities are not merely discouraged for it — they are not reachable from the chat.",
      "That is enforced on the server, in three separate layers, not by asking the model to behave. First, only a small fixed set of read tools is offered to it at all, so a form answer containing the words \"delete all my forms\" has no tool to call. Second, every tool the assistant uses runs with read-only permission, so even a write tool would be refused. Third, the same permission and workspace checks that apply to any MCP connection are re-applied to each call.",
      "The practical consequence: a sentence typed into the chat box cannot delete a form. If you ask the assistant to delete something, it will tell you it cannot, and it will point you at the page where you can do it yourself.",
      "This is tested by attempting the writes, not by checking a list. The tests drive the assistant's real tool path against a real database under its real permissions and require every write to be refused — deleting a form, archiving a product, refunding a sale, cancelling a subscription, creating a product, a form, a webhook, an API key or a setting — and then read the database back to confirm nothing changed.",
    ],
  },
  {
    id: "feedback",
    title: "Filing feedback from the conversation",
    body: [
      "The assistant can file what you tell it as a feedback report or feature request. This is the same act as sending from the Feedback page: it creates a thread you own, the JomForm team is emailed, and the reply arrives in the same thread at /feedback.",
      "Filing is visible, never silent. When the assistant files something, the chat shows a note saying it filed a report, with the subject — so you are never surprised later by a thread you did not know existed. If you did not want it filed, you can read it and add to it from the Feedback page.",
      "The same limits apply as on the Feedback page: at least 10 characters, at most 5000, and at most 10 reports per user per day. Filing too many is answered with a rate limit rather than a judgement about whether your report is genuine — a complaint is never filtered by a classifier here.",
      "The same capability exists over MCP as the submit_feedback tool, so an agent can file a report from a conversation exactly as the in-app assistant does.",
    ],
  },
  {
    id: "formatting",
    title: "Formatting in the answer",
    body: [
      "Answers are rendered, not printed. When the assistant writes bold text, a list, a piece of code or a link, you see bold text, a list, code in a monospace chip or a real link — not the markers it typed. Before this, an answer that said **this** showed the asterisks.",
      "What is rendered: bold, italic, inline code, fenced code blocks (which scroll sideways instead of stretching the card), bulleted and numbered lists, links, and paragraph breaks. What is not: tables, images, headings and raw HTML — a table would arrive as a row of pipe characters and raw HTML as text, because the assistant's text is never inserted into the page as code. The assistant is told which subset it may use, so it produces lists where a table would have been.",
      "The same rendering applies to a feedback thread you file from the chat, and to the reply you read on the Feedback page: a report's body is rendered the same way in your own threads and in the team's triage view.",
      "The stored text is exactly what was written. Nothing is stripped on the way in or out, so a report is never altered by how it happens to be displayed.",
    ],
  },
  {
    id: "limits",
    title: "Limits and cost",
    body: [
      "Every message you send is at least one AI model call, and up to five if the assistant has to look several things up. Each lookup is bounded in size, and the answer is bounded too, so one question cannot become an unbounded bill.",
      "A single user can send 60 messages per hour, in bursts of 10. That is a real conversation's pace with room to spare; if you hit it, wait a moment rather than switching to a second account, because the limit exists to keep the feature affordable for everyone.",
      "The conversation you send is bounded to the last 16 turns and 2000 characters for your message. Older turns are dropped from what the model sees: the assistant is built for a conversation about a recent question, not for holding a long document.",
      "The assistant's own answer is capped at 4000 characters, and it will not paste your entire sales list into the chat when you asked for a count.",
    ],
  },
  {
    id: "trust",
    title: "Your data in the conversation",
    body: [
      "The assistant reads only your own account. It cannot see another merchant's forms, sales or feedback, and it cannot read another person's feedback thread.",
      "Your data is treated as data. Product names, form answers and customer messages are handed to the model inside an explicit boundary that tells it, before and after the content, that this is information to read rather than instructions to follow. If a customer types something into a form that is addressed at the assistant — a fake instruction, a claim that you approved something — it is treated as content and can be reported to you, never obeyed.",
      "The audit log records that a chat exchange happened and how many lookups it made. It does not record your messages or the replies.",
    ],
  },
  {
    id: "when-stuck",
    title: "When it does not know",
    body: [
      "If the assistant cannot answer from your data, it says so and offers to file it as feedback. It is not allowed to guess about your money, your customers or your settings, and it will not invent a number to look helpful.",
      "If it tells you it found nothing, that is a statement about the lookup that ran, not about your account — and saying which form or period it looked at is a fair question to ask.",
      "For anything about a payment that went wrong, a payout, or an account you cannot access, file it from the Feedback page or reply in the assistant's thread: a conversation with a person is the right channel, and the chat will say so rather than pretend otherwise.",
    ],
  },
];

export default function ChatDocsPage() {
  return (
    <article className="mx-auto w-full max-w-3xl px-6 py-12">
      <p className="text-xs font-medium uppercase tracking-wider text-[var(--jf-muted)]">
        JomForm Docs
      </p>
      <h1 className="mt-2 text-2xl font-semibold tracking-tight">In-app assistant</h1>
      <p className="mt-3 text-sm text-[var(--jf-muted)]">
        The built-in chat: it answers questions about your own data, and it files feedback for
        you. It cannot change your account — by design, and enforced on the server.
      </p>
      <nav className="mt-6 flex flex-wrap gap-2">
        {SECTIONS.map((s) => (
          <a
            key={s.id}
            href={`#${s.id}`}
            className="rounded-full bg-slate-100 px-3 py-1 text-xs text-[var(--jf-muted)] hover:bg-[var(--jf-primary)] hover:text-white"
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
        Related: <a className="underline decoration-dotted underline-offset-2" href="/docs/feedback">in-app feedback</a>{" "}
        and <a className="underline decoration-dotted underline-offset-2" href="/docs/mcp">the MCP tool reference</a>{" "}
        (submit_feedback, the tool that files a report).
      </p>
    </article>
  );
}
