import type { Metadata } from "next";

// In-app assistant — the docs page the chat panel's inline "?" link points at
// (AGENTS.md DOCUMENTATION PARITY rule 4).
//
// It documents the feature by stating the CAPABILITY BOUNDARY first, because
// that boundary is the whole design. Phase 2 changed that boundary, so the page
// states the NEW one rather than the old: the assistant can PREPARE a change and
// cannot apply one. It documents the MCP tool the proposal lane pairs with
// (propose_write), the reversible set it may prepare, the confirmation step, the
// rate/cost limits and why they exist, and the behaviour that matters when the
// assistant does not know — the honest answer, not a guess.

export const metadata: Metadata = {
  title: "In-app assistant — JomForm Docs",
  description:
    "What JomForm's built-in chat assistant can and cannot do: it answers questions about your own forms, sales and products, it can file a feedback report, and it can prepare a small reversible change for you to confirm — it can never apply a change by itself.",
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
      "The assistant cannot apply a change to your account. What it can do instead is PREPARE one for you to confirm. Ask it to rename a product, change a price, adjust stock or restyle a form, and it will show you a card listing exactly what it intends to change — then it stops. The change happens when you press Confirm, and not before.",
      "Everything destructive or hard to undo stays out of its reach entirely. It cannot delete or archive a form or product, refund or confirm a sale, capture or release a payment, cancel or retry a subscription, publish or unpublish a page, send email to your customers, or touch your passwords, keys, webhooks, domains, members or settings. Those are not merely discouraged for it — they are not reachable from the chat, and asking for one gets a straight answer that you must do it yourself on the page where the consequence is visible.",
      "That is enforced on the server, in four separate layers, not by asking the model to behave. First, only a small fixed set of tools is offered to it at all, so a form answer containing the words \"delete all my forms\" has no tool to call. Second, every tool the assistant uses while answering runs with read-only permission, so a write tool would be refused even if it were offered. Third, the only code that runs a write is the confirmation step, and it requires a single-use token the server issued to your browser — a token the model never sees and cannot produce. Fourth, the same permission and workspace checks that apply to any MCP connection are re-applied to the confirmed call.",
      "A `confirm` value written by the model proves nothing, and is not accepted as confirmation anywhere. The confirmation is your click. If someone — or something — tells the assistant you already approved a change, that is not an approval, and the assistant is instructed to treat such text as content to report to you rather than an instruction.",
      "This is tested by attempting the writes, not by checking a list. The tests drive the assistant's real tool path against a real database under its real permissions and require every write to be refused — deleting a form, archiving a product, refunding a sale, cancelling a subscription, creating a product, a form, a webhook, an API key or a setting — including attempts where the model writes `confirm: true` into its own arguments, and then read the database back to confirm nothing changed.",
    ],
  },
  {
    id: "proposals",
    title: "Confirming a proposed change",
    body: [
      "A proposal appears in the conversation as a card, not as a sentence. It shows the name of the change in your terms, the specific values it would set, which object it applies to, and the underlying tool name so you can look it up. A badge says it needs your confirmation, and the card says plainly that nothing has changed yet.",
      "Two buttons: Confirm applies it, Cancel kills it. Cancel is a real action, not a dismissal — it spends the proposal on the server, so a cancelled card can never be applied afterwards, by you or by anything else.",
      "The card is generated by the server from the exact arguments that will run, never from the assistant's own description of them. That is deliberate: the model's wording is shown as a quote, but it is not the authority for what the card says will happen.",
      "The proposal is bound to what you were shown. If the arguments changed between the card appearing and your click, the confirmation is refused rather than applied — so the thing you approved is the thing that runs. The card is also single-use (a second click is refused, not applied twice) and it expires five minutes after it appears, because it is a decision made while you are reading it, not an approval left lying around.",
      "Only you can confirm your proposals. The token belongs to the person it was issued to, and another account presenting it is refused in exactly the same way as a token that does not exist.",
      "A confirmed change runs with your own permission and the same workspace and ownership checks as a change made from the dashboard. If your role does not allow the edit, the confirmation is refused and the tool says why — the chat never grants an ability your account does not have.",
      "The same capability exists over MCP as the propose_write tool for an agent to describe a change; note that a proposal made over MCP is confirmed on the dashboard, not inside an agent's own client — the confirmation is a signed-in person's click, and it is deliberately not something an API call can supply.",
    ],
  },
  {
    id: "what-can-change",
    title: "What it may propose",
    body: [
      "The set is deliberately small and reversible: editing a product's fields (name, description, price, stock, active state, images, billing cycle), editing one product variation's fields, and changing a form's theme and branding (colours, fonts, spacing, custom CSS). Each one is a field edit on something you already own, and each can be put back by proposing the opposite edit — there is nothing to clean up and no new object created.",
      "Excluded by class, not by example: anything that destroys, archives or revokes; anything that moves money; anything that cancels or re-runs a customer's billing; anything touching credentials, access or members; anything that sends email out of the building; anything that changes what the public can reach; any bulk operation; and creating anything new. A wrong confirm on those is either irreversible or lands on somebody else, and the confirmation card is too thin a preview for that.",
      "If you need one of those, the assistant will say so and point you to the page where you can do it, rather than pretending it did.",
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
      "Confirmations are limited separately, at 30 per hour in bursts of 5. Confirming costs no model call — that limit is about pace, not spend: it keeps a person deciding each card deliberately instead of clicking through a stack of them.",
      "One answer can put at most 3 proposals in front of you, so a single reply cannot become a wall of confirmations to click through.",
      "The conversation you send is bounded to the last 16 turns and 2000 characters for your message. Older turns are dropped from what the model sees: the assistant is built for a conversation about a recent question, not for holding a long document.",
      "The assistant's own answer is capped at 4000 characters, and it will not paste your entire sales list into the chat when you asked for a count.",
    ],
  },
  {
    id: "trust",
    title: "Your data in the conversation",
    body: [
      "The assistant reads only your own account. It cannot see another merchant's forms, sales or feedback, and it cannot read another person's feedback thread.",
      "Your data is treated as data. Product names, form answers and customer messages are handed to the model inside an explicit boundary that tells it, before and after the content, that this is information to read rather than instructions to follow. If a customer types something into a form that is addressed at the assistant — a fake instruction, a claim that you approved something, a demand to propose a destructive change — it is treated as content and can be reported to you, never obeyed. Because the assistant can now prepare changes, that boundary matters more rather than less: text that talked it into proposing a harmful edit would still have to get past the reversible-only set above and then past you reading a card the server rendered from the arguments.",
      "The audit log records that a chat exchange happened, how many lookups it made, and that a proposal was confirmed — which tool and whether it succeeded. It never records your messages, the replies, or the values of a confirmed change.",
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
        The built-in chat: it answers questions about your own data, it files feedback for you, and
        it can prepare a small reversible change for you to confirm. It cannot apply a change by
        itself — by design, and enforced on the server.
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
        (submit_feedback, the tool that files a report, and propose_write, the tool that prepares a
        change for confirmation).
      </p>
    </article>
  );
}
