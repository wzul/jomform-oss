import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

// Card t_94e5cca3: the dormancy policy, explained for the platform owner.
//
// The dashboard's dormancy section deep-links this page (AGENTS.md DOCUMENTATION
// PARITY rule 4). The important thing for a reader — and for anyone who later
// builds phases 2 and 3 — is what the report does NOT do: nothing on this page or
// in the product deletes, unpublishes or flags an account in phase 1.

export const metadata: Metadata = docsMetadata({
  title: "Dormant workspaces and the retention policy — JomForm Docs",
  description:
    "How JomForm decides that a workspace has gone quiet: the dormancy report, why a quiet merchant is not a quiet form, which accounts are excluded, and the single reminder email.",
  path: "/docs/dormancy",
});

const SECTIONS: Section[] = [
  {
    id: "what-it-is",
    title: "What the dormancy report is",
    icon: "🌙",
    body: [
      "The dormancy report lists **every workspace and how long it has been quiet**, and for each one it decides whether the account may be treated as dormant.",
      "It is a **read-only report**. It deletes nothing, unpublishes nothing and sets no flag that a later job could act on.",
      "The same report decides who receives the one **\"your form is still live\"** reminder email, so the table on the admin page and the email can never disagree.",
      "The report is on the **admin page** (super-admin only), and it is available as JSON or CSV at `GET /v1/admin/dormancy`.",
    ],
  },
  {
    id: "why",
    title: "Why a quiet merchant is not a quiet form",
    icon: "🧭",
    body: [
      "A workspace can go months without its owner signing in **while its published forms keep taking payments and responses**.",
      "Deleting or unpublishing that workspace would take a live checkout offline and destroy buyer records — an outage the product would have caused on purpose.",
      "So \"no activity\" is never read from a sign-in date. Activity is measured from **four account-scoped signals**: the account's event log, changes to its forms, its sales, and its responses.",
      "The account's **subscriptions** count as activity too: a workspace being billed is still working.",
      "The measured saving of deleting quiet accounts was negligible. The whole database was 16 MB, with a few megabytes of media.",
    ],
  },
  {
    id: "buckets",
    title: "The buckets",
    icon: "🪣",
    body: [
      "Each account falls in one **lower-bound bucket**:",
      {
        list: [
          "**Active** — quiet for less than 30 days.",
          "**30 days** — at least 30, less than 60.",
          "**60 days** — at least 60, less than 90.",
          "**90 days** — at least 90, less than 365.",
          "**365 days** — a year or more.",
          "**Excluded** — the account has a dormancy age, but it may not be treated as dormant. The reason is printed beside it.",
        ],
      },
      "The report is ordered **most dormant first**, and it is capped: the page shows the first 200 accounts and says so when the list was cut short.",
    ],
  },
  {
    id: "excluded",
    title: "Which accounts are excluded, and why",
    icon: "🛡️",
    body: [
      "An account is **never** a dormancy candidate — it is listed as EXCLUDED with its reason — when any of these is true:",
      {
        list: [
          "**has_sales** — it has at least one sale, ever. A merchant who has been paid is not dormant, and sales are never destroyed.",
          "**has_subscription** — it has a recurring customer who is still being charged.",
          "**recent_submissions** — a response arrived on one of its forms in the last 90 days. This is the guard that protects a live buyer flow behind a quiet merchant.",
          "**system_account** — the template gallery's own account, and any account with the admin role, are never in scope.",
          "**workspace_in_trash** — its workspace is in the bin, so its forms are not being served and a \"still live\" message would be false.",
          "**no_owner_email** — there is no address to contact, so a candidate tier would promise a reminder that cannot be delivered.",
        ],
      },
      "Excluded rows stay in the report on purpose: an operator can see that a live account was **considered and refused**, rather than never looked at.",
    ],
  },
  {
    id: "email",
    title: "The reminder email",
    icon: "✉️",
    body: [
      "Owners in the **60-day bucket and beyond** receive one email: the forms in their workspace are still online and still accepting responses, and nothing needs to be done.",
      "It is sent through the same mail path as every other JomForm email, so the **suppression list** applies: an address that hard-bounced is never mailed.",
      "The email follows the owner's own language setting.",
      "An account is emailed **at most once every 90 days**.",
      "The send is recorded as an audit event **before** it goes out. A lost record would otherwise mean a second email tomorrow.",
      "No email is sent for an account in the **30-day bucket**, for an **excluded** account, or for the **system account**.",
    ],
  },
  {
    id: "later-phases",
    title: "What is not built yet",
    icon: "🚧",
    body: [
      "The approved policy has **three phases**. Only phase 1 exists, and it is the report and the reminder described above.",
      "**Phase 2** would unpublish the forms of an account with twelve months of zero activity and zero responses, and mark the workspace dormant — fully reversible from the dashboard.",
      "**Phase 3** would delete only a narrow class of account, through the existing purge path.",
      "That means unverified accounts, or accounts with no forms, no sales and no responses for 90 days.",
      "It would carry tombstones, notices and a 30-day grace period.",
      "Neither phase is implemented, and nothing in phase 1 writes state that could be used to act on an account without a human decision.",
    ],
  },
];

export default function DormancyDocsPage() {
  return (
    <DocsPage
      title="Dormant workspaces and the retention policy"
      lead="Which workspaces have gone quiet, how JomForm decides that safely, and the one reminder it sends."
      sections={SECTIONS}
    />
  );
}
