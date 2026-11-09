import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// Issue #601: the suppression list, explained for the platform owner.
//
// The runbook is docs/SES-REPUTATION.md in the repository, which is internal
// ops documentation. This page is the served equivalent the dashboard's inline
// "?" links to (AGENTS.md DOCUMENTATION PARITY rule 4).

export const metadata: Metadata = docsMetadata({
  title: "Email suppression list — JomForm Docs",
  description:
    "How JomForm's email suppression list works: which addresses are never mailed, why each one was added, and how to remove a false positive.",
  path: "/docs/ses-reputation",
});

const SECTIONS: Section[] = [
  {
    id: "suppression",
    title: "What the suppression list is",
    icon: "🛡️",
    body: [
      "The suppression list is the set of addresses JomForm **refuses to send email to**.",
      "Before any message is handed to the provider the recipient is checked against the list. If the address is on it, the message is skipped and the skip recorded.",
      "This protects everyone, because all JomForm email leaves through **one sending identity**: buyer receipts, merchant notifications, verification links and password resets.",
      "A high bounce rate on that identity makes the provider slow or pause **all** of its mail, so a few dead addresses can silence receipts for unrelated merchants.",
      "The list is managed by the platform owner (super-admin) on the admin page, and it is also available to a connected **AI agent**.",
    ],
  },
  {
    id: "added-automatically",
    title: "What gets added automatically",
    icon: "📥",
    body: [
      "The mail provider reports every permanent bounce, complaint and delivery back to JomForm, and those reports feed the list.",
      "A **hard bounce** means the mailbox does not exist. A **complaint** means the recipient marked the message as spam.",
      "Both are added immediately, because sending again can only produce the same result and damage the identity further.",
      "A bounce the provider calls **temporary** is not added on the first event. A mailbox can be full or a server briefly unavailable, and the provider already retried.",
      "An address is added only after several temporary bounces within a week, which is what separates a struggling server from a dead mailbox.",
      "Addresses are stored **lower-cased**, so `Person@Example.com` and `person@example.com` are the same entry.",
      "An address can only appear on the list once.",
    ],
  },
  {
    id: "reasons",
    title: "What the reason codes mean",
    icon: "🔎",
    body: [
      {
        list: [
          "**hard_bounce**: the mailbox rejected the message in a way that will not change. The address does not exist, or the server has refused it permanently.",
          "**complaint**: the recipient marked a JomForm message as spam. The provider counts this more heavily than a bounce.",
          "**manual**: added by the platform owner rather than reported by the provider, so an address can be blocked before any bounce.",
        ],
      },
      "**hard_bounce** also covers an address that produced repeated temporary bounces within a week.",
      "The **note** beside a reason carries the detail: the provider's own sub-type for a bounce, the tally for repeated temporary bounces, or what the owner typed by hand.",
    ],
  },
  {
    id: "removing",
    title: "Removing an address",
    icon: "🗑️",
    body: [
      "A suppression can be **wrong**: a mailbox that was full for a week, a customer who changed their mind about a complaint, or an address mistyped when added by hand.",
      "Removing an address takes it off the list, and email to it resumes from that point. Nothing needs to be re-registered or re-sent.",
      {
        warn: "Removing is a deliberate, confirmed action on the admin page. It is the one way to start mailing an address the provider has told us is bad.",
      },
      "The removal is recorded in the **audit log**, like every other admin action.",
    ],
  },
  {
    id: "never-delivered",
    title: "Addresses that can never receive mail",
    icon: "🚫",
    body: [
      "Some recipient domains **cannot accept delivery at all**, so a message to one is a guaranteed hard bounce, the kind counted most heavily against the sending identity.",
      "On the sign-in (magic-link) path JomForm refuses these before the mail is handed to the provider, so nothing is sent and no bounce is generated.",
      "That check covers the sign-in path only. Mail sent to the same address by any other path, such as a receipt or a password reset, still reaches the provider.",
      "The suppression list, filled by the bounce the provider then reports, stops the next attempt.",
      "Three families are refused:",
      {
        list: [
          "The reserved example domains: `example.com`, `example.net`, `example.org` and anything under them. `example.com` even publishes an MX record saying it accepts no mail.",
          "The reserved test names: `.test`, `.invalid`, `.localhost` and `.example`.",
          "A short list of **disposable** mail providers, whose domains resolve but whose addresses are throwaway.",
        ],
      },
      "A refused magic-link request looks exactly like an accepted one: the same confirmation is shown whether the address exists, does not exist, or was refused.",
      {
        note: "The refusal is counted on the monitoring endpoint and logged **without the address**, so the owner sees how often it happens and no address is kept.",
      },
    ],
  },
  {
    id: "related",
    title: "Related protections",
    icon: "🧱",
    body: [
      "Two more layers guard the sending identity: a **verification circuit breaker** and an **MX pre-check**.",
      "The breaker pauses verification email when the bounce ratio over the last hour crosses a threshold.",
      "That stops an attack that registers many fake addresses from burning the identity. It resets after a quiet hour.",
      "The MX pre-check rejects a registration outright when the address's domain does not accept mail, including a domain whose DNS answer is a **null MX** record.",
      "Buyer receipts are **not gated by the breaker**. A paying customer's receipt must not be delayed by an unrelated bounce storm.",
      "Receipts **are** checked against the suppression list, for the same reason as everything else.",
    ],
  },
];

export default function SesReputationDocsPage() {
  return (
    <DocsPage
      title="Email suppression list"
      lead="Every address JomForm will not send email to, why it was added, and how to take one off the list. This protects the identity that every receipt, notification and login link relies on."
      sections={SECTIONS}
    />
  );
}
