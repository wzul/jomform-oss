import type { Metadata } from "next";

// Issue #601: the suppression list, explained for the platform owner.
//
// The runbook for this feature is docs/SES-REPUTATION.md in the repository —
// internal ops documentation. This page is the merchant-facing/operator-facing
// equivalent that the dashboard's inline "?" links to (AGENTS.md DOCUMENTATION
// PARITY rule 4), so the link resolves to a real page instead of a repo file
// that is not served.
//
// It documents the list itself (what gets on it, what a reason code means, and
// how to take an address off it), which is the part an operator needs; the
// sending-identity hardening this list belongs to is summarised at the end.

export const metadata: Metadata = {
  title: "Email suppression list — JomForm Docs",
  description:
    "How JomForm's email suppression list works: which addresses are never mailed, why each one was added, and how to remove a false positive.",
  alternates: { canonical: "https://jomform.com/docs/ses-reputation" },
};

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "suppression",
    title: "What the suppression list is",
    body: [
      "The suppression list is the set of addresses JomForm refuses to send email to. Before any message is handed to the mail provider the recipient is checked against this list; if the address is on it, the message is skipped and the skip is recorded instead of being sent.",
      "This protects everyone on the platform, because all JomForm email — buyer receipts, merchant notifications, verification links and password resets — leaves through one sending identity. A high bounce rate on that identity makes the provider slow or pause ALL of its mail, so a handful of dead addresses can silence receipts for unrelated merchants. Skipping them keeps the identity healthy.",
      "The list is managed by the platform owner (super-admin) on the admin page, and it is also available to a connected AI agent.",
    ],
  },
  {
    id: "added-automatically",
    title: "What gets added automatically",
    body: [
      "The mail provider reports every permanent bounce, complaint and delivery back to JomForm, and those reports are what feed the list. A hard bounce means the mailbox does not exist; a complaint means the recipient marked the message as spam. Both are added immediately, because sending again can only produce the same result and further damage the sending identity.",
      "A bounce the provider considers temporary is NOT added on the first event — a mailbox can be full or a server briefly unavailable, and the provider already retried before reporting it. An address is only added once the same address has produced several temporary bounces within a week, which is what distinguishes a struggling server from a dead mailbox.",
      "Addresses are stored lower-cased, so an address is recognised whether it arrives as Person@Example.com or person@example.com, and an address can only appear on the list once.",
    ],
  },
  {
    id: "reasons",
    title: "What the reason codes mean",
    body: [
      "hard_bounce — the mailbox rejected the message in a way that will not change: the address does not exist, or the recipient's server has permanently refused it. Also used for an address that produced repeated temporary bounces within a week.",
      "complaint — the recipient marked a JomForm message as spam. The provider treats this as the strongest possible signal and counts it against the sending identity more heavily than a bounce.",
      "manual — added by the platform owner rather than reported by the provider. This is how an address that should never be mailed is blocked without waiting for a bounce, for example a request made outside the platform.",
      "The note beside a reason carries the detail: the provider's own sub-type for a bounce, the tally for a repeated temporary bounce, or whatever the platform owner typed when adding the address by hand.",
    ],
  },
  {
    id: "removing",
    title: "Removing an address",
    body: [
      "A suppression can be wrong: a mailbox that was full for a week, a customer who changed their mind about a complaint, or an address that was mistyped when added manually. Removing an address takes it off the list, and email to it resumes from that point — nothing needs to be re-registered or re-sent.",
      "Removing an address is a deliberate, confirmed action on the admin page: it is the one way to make JomForm start mailing an address the provider has told us is bad, so it is recorded in the audit log like every other admin action.",
    ],
  },
  {
    id: "related",
    title: "Related protections",
    body: [
      "Two further layers guard the sending identity. A verification circuit breaker pauses verification email when the bounce ratio over the last hour crosses a threshold, so an attack that registers many fake addresses cannot burn the identity before anyone notices; it resets by itself after a quiet hour. An MX pre-check rejects a registration outright when the address's domain does not accept mail at all, before anything is sent.",
      "Buyer receipts are not gated by the breaker — a paying customer's receipt must not be delayed by an unrelated bounce storm — but they ARE checked against the suppression list, for the same reason everything else is.",
    ],
  },
];

export default function SesReputationDocsPage() {
  return (
    <>
      <h1 className="text-2xl font-semibold text-slate-900">Email suppression list</h1>
      <p className="mt-3 text-sm leading-6 text-slate-600">
        Every address JomForm will not send email to, why it was added, and how to
        take one off the list. This is what protects the sending identity that
        every receipt, notification and login link on the platform relies on.
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
