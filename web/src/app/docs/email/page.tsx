import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

export const metadata: Metadata = docsMetadata({
  title: "Email & notifications — JomForm Docs",
  description:
    "Receipt emails and per-form notification rules: how a form overrides the workspace, the default notification every new form starts with, dynamic placeholders, and delivery tracking.",
  path: "/docs/email",
});

// The full text behind the "Email & notifications" summary on /docs
// (t_d768342e): receipts, per-form notification rules, the default rule a new
// form starts with, and delivery tracking. Moved here from the docs index
// rather than deleted; /docs links to #notifications.
type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "notifications",
    title: "Receipt emails and notification rules",
    body: [
      "Customise the receipt email sent to buyers and per-form notification rules. Email settings are configured per workspace, and any form can override them (subject, body, receipt on/off, merchant notify) with a “use workspace default” option — resolution order is form → workspace → built-in default.",
      "The subject and body of a new workspace start empty, and an empty field is not a missing subject: it means JomForm's own receipt, written in the language the buyer's form is set to, so an English form gets an English receipt and a Malay form a Malay one without you filling anything in. As soon as you type your own wording it is sent exactly as you wrote it, in every language — JomForm does not translate your text.",
      "Every new form also starts with a default notification to you: a payment form notifies on sale.paid, a response form on response.submitted (its message uses the responder's name and email, because a response has no sale).",
      "That default rule's subject and body start empty too, for the same reason the receipt fields do: an empty field means JomForm's own message, written in the language the form is set to, so a rule you never edited shows a grey example rather than a wall of text you have to delete. Type your own subject or body and it is sent exactly as you wrote it, in every language — the half you did not touch still follows the form's language.",
      "Switching a form's mode re-points that default notification onto the event the new mode fires — renaming the label alone does not opt the rule out of it — so a form you started as an order form and later switched to responses still tells you when someone responds. Only the default rule is moved, and only while its message is untouched: a rule whose subject, body or recipient you changed stays exactly as you wrote it, and a form whose rules you deleted stays silent. The label is a dashboard name, not a decision about the message, so a re-pointed default takes the system's own label for the event it now fires.",
    ],
  },
  {
    id: "placeholders",
    title: "Dynamic placeholders",
    body: [
      "The subject and body support dynamic placeholders — {{ref_code}}, {{amount}}, {{items}}, {{buyer_name}}, {{form_url}}, {{date}} and any {{field_id}} — replaced with the real value for that sale or submission when the email is sent. The Placeholders reference lists every one, including the {admin_email} recipient token and the placeholders that carry no value on a response form.",
    ],
  },
  {
    id: "delivery",
    title: "Delivery tracking",
    body: [
      "Track delivery for every message: sent → delivered → opened → clicked. The status is per email, so a receipt that never arrived can be told apart from one that was never sent.",
      "An email that could not even be queued is listed as enqueue_failed. That one is not a delivery state: the message never left the platform, so no sent/delivered event will ever follow it. Filter the Email deliveries page by that status to find a buyer whose receipt or notification was lost before it was ever attempted.",
      "Addresses that should never be mailed are held on the platform's suppression list — see the email suppression list page for what gets on it and how an address comes off.",
    ],
  },
];

export default function EmailDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Email &amp; notifications</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Receipts and notifications: how a form overrides the workspace&apos;s email settings, what a new
        form starts with, the placeholders you can use, and how delivery is tracked.
      </p>

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

      <p className="mt-10 text-sm leading-relaxed text-slate-600">
        Related: the{" "}
        <a href="/docs/placeholders" className="text-[var(--jf-primary)] underline underline-offset-2">
          placeholders reference
        </a>
        , the{" "}
        <a href="/docs/ses-reputation" className="text-[var(--jf-primary)] underline underline-offset-2">
          email suppression list
        </a>{" "}
        and the{" "}
        <a href="/docs/mcp" className="text-[var(--jf-primary)] underline underline-offset-2">
          MCP tool reference
        </a>{" "}
        for the email settings tools.
      </p>
    </div>
  );
}
