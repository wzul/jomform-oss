import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

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
const SECTIONS: Section[] = [
  {
    id: "workspaces",
    title: "What a workspace is",
    icon: "🏢",
    body: [
      "A workspace holds your forms, products, sales, **CHIP credentials**, domains and settings. Data never crosses between workspaces.",
      "Each workspace has its own public host: your forms are served from {slug}.jomform.com, or a custom domain you add.",
      "With no workspace yet, the dashboard **asks you to create one** on your first sign-in: name it, pick a slug, then create a form.",
      "Joined before workspaces existed? JomForm creates one on your next sign-in, named after the **first five characters of your email address**, and moves your forms in.",
      "Your forms then serve from that workspace's address. **A form has exactly one public host**, so share the new link.",
      "Create more from the switcher in the top bar (**New workspace**). A new workspace starts empty: no credentials, no products, no sales.",
    ],
  },
  {
    id: "rename-workspace",
    title: "Renaming a workspace",
    icon: "✏️",
    body: [
      // PR 691: this is the walk-through the /docs entry used to carry in full.
      // The name and the address are two different fields with two different
      // authorities, and the split is the part a merchant gets wrong.
      "Name and **address** are two fields with two authorities, both editable in Settings → Workspace.",
      "The **NAME** is only what you and your team see in the dashboard and switcher. Anyone with the manage or admin role can change it.",
      "Up to 120 characters, **counted in characters rather than bytes**, so Chinese, Tamil, Arabic or accented names measure the same.",
      "The **ADDRESS** is your public host on a QR stand, an embed or an affiliate link. **Only the OWNER can change it**.",
      "It must be 3–40 characters of a-z, 0-9 and dashes, cannot end in jomform, and cannot be an address another workspace holds.",
      "That includes an address a workspace has renamed away from: it still answers for its own merchant. A reserved name is refused with the reason stated; see [reserved names](/docs/customize#workspace-address).",
      "The old address keeps working, **redirecting permanently (HTTP 308) to the new one**, and is **never handed to another workspace**.",
      "PATCH /v1/workspaces/{id} renames it with optional fields. The tool update_workspace **requires confirm=true for an address change**; an address change never touches the name.",
    ],
  },
  {
    id: "delete-workspace",
    title: "Deleting a workspace",
    icon: "🗑️",
    body: [
      // PR 804 (the workspace trash): a delete is a MARK, not a destroy. The
      // hard delete moved into Purge, which is a second, separate step.
      "Moving a workspace to the Trash needs a **typed confirmation**: you type its own address (slug) back. Nothing is destroyed: forms, products, sales, payment account and keys stay.",
      "Only the OWNER may move it: an admin manages the workspace but does not own its identity, and **leaves through Settings → Members instead**.",
      "You must keep at least one other workspace: **the last workspace can never go to the Trash**, and the button says why. Content does not block the move.",
      "From Trash you can restore a workspace whole with its original address, unless another workspace took that address meanwhile. The restore is then refused, naming the other workspace.",
      "**Deleting for good is a separate, second step**: Trash only, slug typed again, and it cannot be undone. It **refuses a workspace that has sales**. Chat can never propose it.",
      "A payment account older than the workspace is **detached from the destroyed workspace instead of being deleted**, so its CHIP credentials survive and a later workspace can use it.",
      "Settings → Workspace holds the Danger zone. The permanent step is REST or a **write-scoped MCP key** only, never by clicking a dashboard control. It runs **check_workspace_deletion** first, then purge_workspace.",
      "See the [sales guide](/docs/sales) for the history a delete refuses to discard.",
    ],
  },
  {
    id: "footer-legal-links",
    title: "The storefront footer and its legal links",
    icon: "📄",
    body: [
      "Every public form can carry one footer: business name, registration number, contact details and up to three legal links. It is set per **workspace** in Settings → Storefront.",
      "Save footer publishes it on every form the workspace serves, on {slug}.jomform.com and on a custom domain alike.",
      "The three URL fields take any https:// address. A link to your own website is published exactly as typed: JomForm does not fetch it.",
      "A link on your workspace's own JomForm host is different, because JomForm serves those pages. Two things must both be true for it to appear:",
      {
        list: [
          "The address must be exactly https://{slug}.jomform.com/privacy, /terms or /refund, with no trailing slash, capital or extra path.",
          "That hosted page must have saved content in **Settings → Legal**.",
        ],
      },
      "A link that fails either test is not published: the footer is dropped rather than carrying a link that answers 404.",
      "Saving such a URL still succeeds and stores what you entered. The field then shows the reason and the address the route serves; an empty field is never reported.",
      {
        note: "GET /v1/workspaces/{id}/settings and its PUT both return **legal_link_status**. Agents set the same fields with set_workspace_settings: privacy_url, terms_url, refund_url.",
      },
    ],
  },
  {
    id: "currency",
    title: "Workspace currency & exchange rates",
    icon: "💱",
    body: [
      "Everything a workspace sells is priced in its own currency. **Settings → Currency** sets it (default MYR), and the workspace's CHIP account inherits it.",
      "A change applies to **new products and sales only**. A completed sale keeps its currency, a refund must use that same currency, and nothing recorded is re-priced.",
      "The Sales page labels each amount in the currency of its sale (the `currency` field on GET /v1/rest/sales and list_sales), so an old MYR sale still reads RM24.90.",
      "A **price** has no currency of its own: every price field and canvas label names the workspace's current currency, so a USD workspace reads US$12.00 on Products.",
      "Settings → Currency shows the **BNM rates** JomForm caches daily, as Ringgit per one unit. This table is reference data: no saved amount is ever converted.",
      "Because 15 of the 27 quotes BNM publishes are per 100 units, JomForm divides each by its block size first. That is why 100 JPY reads 2.6044.",
      "Need a converted figure? Convert **one amount on demand** with convert_currency: amount_minor is in minor units, so 10000 means 100.00, and the reply carries converted_minor.",
      {
        note: "The dashboard table is read-only; get_currency_rates reads it and set_workspace_currency sets the setting. Conversion is for display and reporting only and never charges a card.",
      },
    ],
  },
];

export default function WorkspacesDocsPage() {
  return (
    <DocsPage
      title="Workspaces"
      lead="A workspace is the tenant everything you build lives in: what it owns, its public address, its currency, and what a delete requires."
      sections={SECTIONS}
    />
  );
}
