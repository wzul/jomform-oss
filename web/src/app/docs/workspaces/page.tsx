import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

export const metadata: Metadata = docsMetadata({
  title: "Workspaces — JomForm Docs",
  description:
    "What a workspace owns, its public address, its currency and exchange rates, how you create one, and what moving one to the Trash (and deleting it for good) requires.",
  path: "/docs/workspaces",
});

// The full text behind the summaries on /docs (t_5425c958): "Workspaces
// (your account's tenant)", "Renaming a workspace" and "Deleting a workspace".
// The first two arrived on main (e45ede2 / #682) after the /docs restructure was
// written, so the index carried their whole 231- and 251-word bodies; the rebase
// cuts them to summaries and moves the walk-through here, like every other cut
// entry. "Renaming a workspace" arrives with PR 691 — its 259-word body was a
// paragraph, which the /docs entry-shape guard (docsIndex.test.ts) refuses, so
// the walk-through lives here and the index keeps the one-line summary.
//
// The anchor names are part of the contract: /docs links to them by `#id`, the
// Settings → Workspace danger zone's inline "?" points at
// `/docs#delete-workspace`, the identity card's "?" points at
// `/docs#rename-workspace`, the first-run workspace ask points at
// `/docs#workspaces`, and Settings → Currency's inline "?" points at
// `/docs/workspaces#currency` (card t_467614db). The index slugs stay on the
// index itself.
type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "workspaces",
    title: "What a workspace is",
    body: [
      "A workspace holds your forms, products, sales, CHIP credentials, domains and settings. Every workspace owns its own account, so data never crosses between them, and each has its own public host: your forms are served from {slug}.jomform.com (or a custom domain you add).",
      "If you have no workspace yet, the dashboard asks you to create one the first time you sign in — name it and pick a URL slug, and you land inside it able to create a form straight away.",
      // t_e3ebe979 (the rebase onto main): this paragraph is main's #743 text,
      // corrected by #750 (the account-slug host no longer serves). It used to
      // live in the /docs entry's body; the restructure cuts that entry to a
      // summary, so the walk-through belongs here or it is lost.
      "If you joined before workspaces existed and already had forms but no workspace, JomForm creates that workspace for you on your next sign-in, naming it and its URL after the first five characters of your email address, and moves your existing forms into it so they appear in your dashboard straight away — nothing is deleted. Your forms then serve from that workspace's own address ({workspace-slug}.jomform.com): a form has exactly one public host, so if you had shared a link on an older address you will want to send the new one.",
      "Create more later from the workspace switcher in the top bar (\"New workspace\"); the switcher also sets which workspace you land on after login. A new workspace starts empty: no CHIP credentials, no products, no sales.",
    ],
  },
  {
    id: "rename-workspace",
    title: "Renaming a workspace",
    body: [
      // PR 691: this is the walk-through the /docs entry used to carry in full.
      // The name and the address are two different fields with two different
      // authorities, and the split is the part a merchant gets wrong.
      "A workspace's name and its public address can both be changed after it is created, in Settings → Workspace, where the identity card is editable.",
      "The NAME is only what you and your team see in the dashboard, the switcher and the workspace list, so it is safe to fix a typo or follow a rebrand. Anyone with the manage or admin role on the workspace can change it. It can be up to 120 characters, counted in characters rather than bytes — so a name in Chinese, Tamil, Arabic or with accents is measured the same way the field measures it, and 120 characters is 120 whatever the script. The same limit applies whether you create a workspace or rename one.",
      "The ADDRESS is different: <slug>.jomform.com is your workspace's public host, the thing printed on a QR stand, pasted into an iframe embed, and handed to an affiliate as a ?aff= link. Only the OWNER can change it — an admin member manages a workspace but does not own its public identity. The address must be 3–40 characters of a-z, 0-9 and dashes, cannot end in \"jomform\" (that is the product's own domain), and must not be in use by another workspace — including a workspace that has renamed away from it, because those former addresses still answer for their own merchant. A reserved name is refused with the reason stated; see the reserved workspace names below.",
      "Changing the address is safe by design: the old address is kept and keeps working, redirecting permanently (HTTP 308) to the new one, so a QR code already stuck to a table or a link already in an email does not break and nobody has to reprint anything. Unlike a form-slug rename, a former workspace address is never handed to another workspace — it always moves to the workspace that used to own it.",
      "Changing the address never requires touching the name: a request that changes only the address leaves the name exactly as it was, and an over-long name can never block an address change.",
      "The rename goes through PATCH /v1/workspaces/{id} — fields are optional, and an omitted one keeps its current value — or the MCP tool update_workspace, which requires confirm=true for an address change because moving a tenant's public identity is not something an agent should do by inference. The name alone needs no confirmation.",
    ],
  },
  {
    id: "delete-workspace",
    title: "Deleting a workspace",
    body: [
      // PR 804 (the workspace trash): a delete is a MARK, not a destroy. The
      // hard delete moved into Purge, which is a second, separate step.
      "A workspace is moved to the Trash with a typed confirmation — you type the workspace's own address (slug) back. Nothing is destroyed by that move: the workspace's forms, products, sales, payment account and API keys all stay exactly as they were, and it simply disappears from your workspace list and from the switcher.",
      "Two rules decide whether it may go. Only the OWNER may move it: an admin member manages a workspace but does not own its identity, its sales history or its payment account, and leaves through Settings → Members instead.",
      "You must have at least one other workspace, because a user always needs one — the last workspace can never go to the Trash, and the button is disabled with the reason stated rather than failing after you press it. A workspace that still holds content moves just as freely; there is nothing to empty first.",
      "From Trash you can restore a workspace whole, and its original address comes back with it — unless another workspace has taken that address while it sat in the Trash, in which case the restore is refused and you are told which name to deal with rather than having your workspace silently renamed.",
      "Deleting for good is a separate, second step: it only works on a workspace already in the Trash, it asks you to type the slug again, it refuses a workspace that has sales (sales are never destroyed or silently detached), and it cannot be undone.",
      "One thing it does not take with it: the payment account your sign-up created, when that account is older than the workspace — which it is for the account your first workspace was created onto. That account is detached from the destroyed workspace instead of being deleted, so the account and its CHIP credentials survive and a workspace you create later can use it again. Everything the workspace itself held goes with it.",
      "Settings → Workspace shows the identity and the Danger zone; the permanent step is reached through the REST API or an MCP client acting for the owner, never by clicking a dashboard control. AI agents use delete_workspace with check_workspace_deletion as the read-only pre-flight, list_trashed_workspaces and restore_workspace for the bin, and purge_workspace for the permanent step — which the in-app assistant can never propose.",
    ],
  },
  {
    id: "footer-legal-links",
    title: "The storefront footer and its legal links",
    body: [
      "Every public form can carry a footer with your business name, registration number, contact details and up to three legal links (Privacy, Terms, Refund). It is one footer per WORKSPACE, set in Settings → Storefront and saved with \"Save footer\"; every form that workspace publishes shows it, on {slug}.jomform.com and on a custom domain alike.",
      "The three URL fields take any https:// address, and a link to your own website always appears exactly as you typed it — JomForm does not fetch it and cannot know whether it works, so it is published unchanged.",
      "A link on your workspace's OWN JomForm host is different, because JomForm serves those pages itself. Two things must both be true for it to appear: the address must be exactly https://{slug}.jomform.com/privacy, /terms or /refund (no trailing slash, no capitalised segment, no extra path), and that hosted page must have saved content in Settings → Legal. A link to your own host that fails either test is NOT published — the footer is dropped rather than carrying a link that answers 404.",
      "When that happens you are told at the field you typed into: saving a legal URL the footer will not publish still succeeds and still stores exactly what you entered, and the field then shows the reason underneath it together with the address the route actually serves. The two reasons are a path JomForm does not serve (for example a trailing slash) and a hosted page with no saved text yet — in the second case write the page in Settings → Legal and save the footer again. An empty field is never reported: no link is not a problem.",
      "The REST settings endpoints answer with the same verdict: GET /v1/workspaces/{id}/settings and the PUT that saves it both carry legal_link_status — one entry per field, giving shown plus the reason and the address the route actually serves when the link will not be published — so a client saving over REST sees exactly the reason the dashboard shows. An AI agent writing through the MCP tool set_workspace_settings sets the same three fields (privacy_url, terms_url, refund_url) and the footer applies the same rule, so it should point a hosted link at the canonical address with the page written, or use an address on the merchant's own website.",
    ],
  },
  {
    id: "currency",
    title: "Workspace currency & exchange rates",
    body: [
      "Everything a workspace sells is priced in its own currency: Settings → Currency sets it (default MYR), and the workspace's CHIP account inherits it for purchases. Changing it applies to NEW products and sales only — a completed sale keeps the currency it was paid in, a refund must be issued in that same currency, and nothing already recorded is ever re-priced or re-converted. The Sales page labels each amount with the currency its sale was recorded in, so after a switch an old MYR sale still reads RM24.90 while a new USD sale reads US$24.90 (the row carries it as currency on GET /v1/rest/sales and list_sales). The dashboard follows the same rule: the revenue stat, the 14-day chart and the per-form/per-product panels group their money by the currency each sale was recorded in and show one label per currency (RM24.90 + US$1.00) instead of adding different monies under one symbol, and the CSV export names the currency in its own column beside total/refunded/outstanding. A PRICE is the other direction: a product, subscription, builder option or shipping-rate amount has no currency of its own, so every price field, list and canvas label names the workspace's current currency — a workspace set to USD reads US$12.00 on Products and in the form builder, never RM12.00 — and switching the setting relabels those prices (only recorded sales keep their original money).",
      // Card t_467614db: the surface's old hint said the rate table was "used for
      // display/conversion of new amounts" while nothing in the product converted
      // an amount at all. The guide states what the table is (reference data) and
      // what converts (one amount, on demand, through convert_currency).
      "Settings → Currency also shows the Bank Negara Malaysia (BNM) rates JomForm caches once a day: Ringgit per ONE unit of each foreign currency. This table is reference data. JomForm never converts a saved amount, so a rate moving cannot rewrite a completed order — and because 15 of the 27 quotes BNM publishes are quoted per 100 units (JPY, THB, IDR and others), JomForm divides each by its published block size before storing it. That is why 100 JPY reads 2.6044 and not 260.44.",
      "When you need a converted figure — a report, a quote for a customer — convert that ONE amount on demand. An AI agent or MCP client does it with the convert_currency tool (amount_minor is in the source currency's minor units, so 10000 means 100.00; the reply carries converted_minor in the target currency), reads the table with get_currency_rates, and sets the workspace currency with set_workspace_currency. The dashboard's rate table is read-only.",
      "The conversion is for display and reporting only. It is never used to charge a card: CHIP purchases and sales settle in the workspace's own currency.",
    ],
  },
];

export default function WorkspacesDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Workspaces</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A workspace is the tenant everything you build lives in. This page covers what it owns, its
        public address, its currency and exchange rates, how you get one, and exactly what moving
        one to the Trash — or deleting it for good — requires.
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
        See the{" "}
        <a
          href="/docs/customize#workspace-address"
          className="text-[var(--jf-primary)] underline underline-offset-2"
        >
          reserved workspace names
        </a>{" "}
        for the addresses you cannot claim, and the{" "}
        <a href="/docs/sales" className="text-[var(--jf-primary)] underline underline-offset-2">
          sales guide
        </a>{" "}
        for the history a delete refuses to discard.
      </p>
    </div>
  );
}
