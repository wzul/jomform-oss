import type { Metadata } from "next";
import Link from "next/link";

import type { Key } from "../../components/i18n";
import { T } from "./docsChrome";

export const metadata: Metadata = {
  title: "JomForm Docs — Overview & features",
  description:
    "Every JomForm feature in one line, grouped by category: start here, sales & payments, forms & fields, storefront, email, growth, security and developer guides.",
  alternates: { canonical: "https://jomform.com/docs" },
};

// The knowledge base is grouped by category (t_d768342e). Each feature entry is
// a one-sentence summary; the full walk-through lives on the guide page named by
// `href`, and the entry links to it ("Selanjutnya"). Two things this structure
// exists to prevent:
//
//   * the 2,811-word flat wall this page used to be — `docsIndex.test.ts` fails
//     the build when any feature body grows past the one-sentence limit;
//   * a link to a guide section that does not exist — the same guard reads each
//     `href` and checks the destination file actually declares that `id`.
//
// The taxonomy (group labels, the intro, the quick links) is translated through
// `t()` in all five locales, like the rest of the app's chrome. The prose itself
// stays English: every /docs page is static English (see the note in
// customize/page.tsx) and half-translating the knowledge base would be worse
// than not translating it.
//
// t_5425c958 (the rebase onto main): main added four features after this
// structure was written — Workspaces, Deleting a workspace, Two-factor
// authentication (2FA) and the REST API reference. Each is a summary here plus a
// guide that carries the full text, like every other cut entry; the guide pages
// are /docs/workspaces (new, for the first two), /docs/security#two-factor and
// /docs/api. Fitting them in is why several existing summaries were tightened:
// the guard's page budget is 25 words per feature (it was a flat 800 words then
// — see PAGE_BUDGET_PER_ENTRY in docsIndex.test.ts, card t_7f0dd2b7), and the
// trimmed bodies are the entries whose full walk-through already lives on a guide.
type Entry = {
  title: string;
  body: string;
  // An anchor on THIS page that a dashboard inline "?" links to.
  slug?: string;
  // The guide that carries the full text, when the entry is a summary of it.
  href?: string;
};

type Group = { id: string; labelKey: Key; entries: Entry[]; more?: { href: string; label: string }[] };

// The FAQ block's entries. A question/answer pair is the common shape; a
// `title`/`slug`/`body` entry is a named section that carries its own anchor —
// the shape every other section on this page uses, and the one the passkey
// section needs, because api/internal/httpapi/passkey_seam_test.go requires the
// passkey text to be rendered here as a slug-anchored section. The anchor IS
// deep-linked, and honestly so: `two-factor`, `workspaces` and `form-url-slug`
// are linked from Settings, while the passkeys "?" is the one on Profile →
// Passkeys (web/src/app/profile/page.tsx), the surface a passkey is registered
// on. web/src/lib/passkeyDocs.test.ts requires that link, so this sentence
// cannot drift from the code. The login page renders the passkey sign-in
// control but carries no /docs link, so #passkeys is NOT reached from there.
//
// The Go guard locates this entry by the `slug` property line below, not by the
// first occurrence of that line's text anywhere in the file (t_1310d487): the
// unanchored search it used before that card sliced whatever came first, so a
// comment merely naming the section became the "section" — red on a correct
// entry, and able to satisfy the guard with the entry's own body gutted.
// web/src/lib/passkeyDocs.test.ts asks the same question of the same property
// (t_ec01dcc2), so neither guard can be answered by prose naming the literal.
//
// Both shapes are declared in one list only because both render below the FAQ
// heading; the render branches on the shape.
type FaqEntry =
  | { q: string; a: string; title?: undefined; slug?: undefined; body?: undefined }
  | { title: string; slug: string; body: string; q?: undefined; a?: undefined };

const GROUPS: Group[] = [
  {
    id: "account",
    labelKey: "docs.catWorkspaces",
    entries: [
      {
        title: "Workspaces (your account's tenant)",
        slug: "workspaces",
        href: "/docs/workspaces",
        body: "A workspace holds your forms, products, sales and payment account, and owns its own address ({slug}.jomform.com or your domain). With none, the dashboard asks you to create one.",
      },
      {
        // Settings → Workspace's identity card "?" deep-links this section
        // (AGENTS.md documentation-parity rule 4). The anchor stays on the index
        // even though the walk-through lives on the guide: an old bookmark has to
        // land somewhere, and the summary is what the entry-shape guard allows.
        title: "Renaming a workspace",
        slug: "rename-workspace",
        href: "/docs/workspaces#rename-workspace",
        body: "Rename a workspace's name or its public address; the old address keeps redirecting.",
      },
      {
        // The Settings → Workspace danger zone's "?" deep-links this section
        // (AGENTS.md documentation-parity rule 4).
        title: "Deleting a workspace",
        slug: "delete-workspace",
        href: "/docs/workspaces#delete-workspace",
        // PR 804 (the workspace trash): this summary said "Permanent, and it
        // takes the payment account with it ... this one is empty", which is the
        // hard delete the trash replaced — and main re-worded that same sentence
        // ("Permanent — the payment account goes with it") while this branch was
        // open, so the merge has to take THIS side's meaning and discard main's
        // wording: main's line asserts the permanence the trash removes. Kept to
        // one line under the 45-word entry limit; the walk-through moved to the
        // guide with it, and docsIndex.test.ts's facts (which are asserted at the
        // guide, not here) moved with the copy. The trim to fit the page's word
        // budget (a flat 800 then; 25 words per feature since card t_7f0dd2b7)
        // is where this line earns its keep: the trash needs MORE sentence than
        // "Permanent — ..." did, so the page came in at 804 words on the merge
        // and this wording brings it back under.
        body: "Reversible, not destructive: a typed slug confirmation sends it to the Trash, even a full workspace. Restoring is one click; deleting for good is a second step that refuses one with sales.",
      },
      {
        // Card t_eb09b25a (the rebase onto main, t_4c58a09e): the audit-trail
        // surface. It belongs to this category, not a new one — the whole
        // reason the Activity page exists is that a workspace deletion (and a
        // form purge, and a product delete) left no record a merchant could
        // read, and the row it writes names the workspace. The guide carries
        // the walk-through, like every other cut entry.
        //
        // Fitting it in is why two of the summaries above were tightened: the
        // guard capped the whole page at a flat 800 words then (card t_7f0dd2b7
        // has since made the budget 25 words per feature, so the ceiling moves
        // with the entry count instead of eating a fixed margin). Those two
        // trims plus the entry still left the page at 806 words — t_8ebcba5a
        // brought it back to 797 by tightening four more summaries, each
        // re-saying the same facts in fewer words; nothing was dropped.
        title: "Activity log (audit trail)",
        href: "/docs/activity",
        body: "Every action in this workspace, newest first, including the destructive ones — a deletion leaves no other trace. Append-only.",
      },
    ],
  },
  {
    id: "sales-payments",
    labelKey: "docs.catSales",
    entries: [
      {
        title: "Sales",
        href: "/docs/sales#sales",
        body: "Track every sale: reference code, status, buyer, amount, payment method and date. Confirm bank transfers, capture or release authorisations, refund, and export CSV.",
      },
      {
        title: "One list for sales and responses",
        href: "/docs/sales#combined-list",
        body: "Without a form filter the Sales page is one merged list: every sale and every response, newest first, with one combined count.",
      },
      {
        title: "Responses (response-only forms)",
        href: "/docs/sales#responses",
        body: "A form can collect answers instead of payments. Its entries are stored as responses, shown on the same Sales page, with a drawer holding every value.",
      },
      {
        title: "Form analytics (the per-form counter)",
        slug: "form-analytics",
        href: "/docs/sales#analytics",
        body: "The per-form counter counts the rows that form actually recorded: responses for a response form, sales in every status for a payment form. Never both.",
      },
      {
        title: "Partial refunds",
        href: "/docs/sales#refunds",
        body: "Refund a paid sale in full or in part: the sale records how much was returned and keeps the balance refundable until zero.",
      },
      {
        title: "Subscriptions",
        href: "/docs/subscriptions",
        body: "Charge customers automatically monthly or yearly, for a fixed number of payments or indefinitely. A late payment follows the schedule mode; a declined card is retried three times.",
      },
      {
        title: "Realtime notifications & the order screen beep",
        slug: "realtime-notifications",
        href: "/docs/sales#realtime-notifications",
        body: "The Sales page follows your workspace's realtime stream; the switch beside the Live pill beeps when something arrives. Per workspace, so it survives a reload.",
      },
    ],
  },
  {
    id: "forms-fields",
    labelKey: "docs.catForms",
    entries: [
      {
        title: "Order forms",
        href: "/docs/customize#builder",
        body: "Build order and response forms with a drag-and-drop builder — fields, products, images — and publish to your workspace URL or a custom domain.",
      },
      {
        // Issue #255's feature had no page in the knowledge base: the only
        // description of the field contract was the MCP tool schema. The entry
        // (and the guide it links) are the documentation-parity fix; the title
        // is in ORIGINAL_TITLES and the facts are at the destination, so it is
        // guarded like every other cut entry.
        title: "Custom HTML forms",
        href: "/docs/custom-forms",
        body: "Give a form your own HTML body instead of builder blocks: it is sanitized and rendered in a sandboxed iframe, and the answers are filtered to the fields registered in custom_fields — the buyer's full_name, email and phone are recorded alongside.",
      },
      {
        title: "Products",
        body: "Sell one-time items and subscriptions: each product has a name, price, stock, images and optional variations, and a subscription adds a billing period.",
      },
      {
        title: "Variation products",
        href: "/docs/variations",
        body: "A product with variations is variable: each variation carries its own name, price, stock and image, and can carry its own subscription cycle.",
      },
      {
        title: "Product images on forms",
        href: "/docs/customize#product-images",
        body: "Choose how each product photo appears on the form: size (small, medium, large or hidden) and layout (thumbnail-left, the default, or image-top for a menu-card look).",
      },
      {
        title: "Templates",
        href: "/docs/customize#templates",
        body: "Start from the built-in template gallery (order forms, booking, donation, contact, survey and more), or save any form as a private template you can clone and edit in the builder.",
      },
      {
        // Arrived on main (#657) after this card's structure was written; the
        // builder's inline "?" (AGENTS.md docs-parity rule 4) deep-links
        // /docs#publishing, so the slug stays on the index as the summary's
        // anchor and the full walk-through lives in the customize guide.
        title: "Publishing a form",
        slug: "publishing",
        href: "/docs/customize#publishing",
        body: "Press Publish and JomForm confirms the form is live with its public URL. A payment form with no products is refused once, unless you publish anyway.",
      },
      {
        // Arrived on main (#679) after this card's structure was written. The
        // slug stays on the index (Form Settings' inline "?" deep-links
        // /docs#form-url-slug, and slugRenameErr.test.ts asserts that anchor
        // exists here) and the full walk-through — the character rule, the
        // refusal, and the 301 — lives in the customize guide.
        title: "Form URLs and slugs (rename a form)",
        slug: "form-url-slug",
        href: "/docs/customize#form-url-slug",
        body: "Set the form's slug in Form Settings; rename it any time and the old URL keeps resolving.",
      },
      {
        // Shipping by zone (the managed rate source). The summary is short and
        // the walk-through lives on the guide; the two MCP tool names are
        // asserted on that page by docsFacts.ts.
        title: "Shipping by zone",
        href: "/docs/shipping",
        body: "Charge a delivery fee per zone: named zones (state, district or postcode range) with a price, a weight band and an optional free-shipping threshold, shared across your forms.",
      },
    ],
  },
  {
    id: "storefront",
    labelKey: "docs.catStorefront",
    entries: [
      {
        title: "Restaurant menu (QR ordering)",
        href: "/docs/customize#menu",
        body: "Turn a form into a QR restaurant menu: sort its products into named categories, highlight a best-seller, and diners order and pay from the table.",
      },
      {
        title: "Menu basket (multi-item ordering)",
        href: "/docs/customize#menu-basket",
        body: "A menu takes a whole table's order: every item is add-to-cart, with a sticky basket, quantity steppers and a running total that survives a reload.",
      },
      {
        title: "Landing pages",
        body: "Customise your workspace storefront with a hero, subtitle, body, image, theme colours, logo and call-to-action buttons that link to your forms.",
      },
      {
        title: "Custom domains",
        href: "/docs/customize#domains",
        body: "Put your forms on your own domain: add it, point a CNAME to custom.jomform.com and verify. With Cloudflare linked, JomForm can create the CNAME for you.",
      },
      {
        title: "Media library",
        body: "Upload images, code assets (css/js/html/svg/woff2) and PDFs. Images are optimised and served immediately; code assets are scanned before being served.",
      },
      {
        title: "Settings",
        body: "Configure CHIP payment credentials, custom domains, tracking scripts, e-Invoice (LHDN), outbound webhooks, currency, and per-workspace email templates.",
      },
    ],
  },
  {
    id: "email",
    labelKey: "docs.catEmail",
    entries: [
      {
        title: "Email & notifications",
        href: "/docs/email",
        body: "Customise the receipt email and the per-form notification rules (subject, body, receipt on/off, merchant notify); a form override falls back to the workspace and then the built-in default.",
      },
    ],
    more: [
      { href: "/docs/ses-reputation", label: "Email suppression list" },
      { href: "/docs/placeholders", label: "Placeholders reference" },
    ],  },
  {
    id: "growth",
    labelKey: "docs.catGrowth",
    entries: [
      {
        title: "Affiliate marketing",
        href: "/docs/customize#affiliate-marketing",
        body: "Pay promoters for the sales they bring: give each affiliate a short code and a commission, share their ?aff=CODE link, and the sale is attributed to them.",
      },
    ],
  },
  {
    id: "security",
    labelKey: "docs.catSecurity",
    entries: [
      {
        title: "Spam protection",
        href: "/docs/spam-detection",
        body: "AI review of every submission: flagged entries wait in a review queue, a false positive can be un-flagged, and an unreviewed flag is deleted after 30 days.",
      },
      {
        title: "Two-factor authentication (2FA)",
        // The Profile → Security 2FA controls (AGENTS.md docs-parity rule 4) and
        // the disable2fa.test.ts guard deep-link this section. The full walk-
        // through lives on the security guide.
        slug: "two-factor",
        href: "/docs/security#two-factor",
        body: "Add a second factor from Profile → Security: scan the QR into an authenticator app and confirm the 6-digit code. Turning it off asks for that code, and a magic-link session must prove itself first.",
      },
      {
        title: "Trusted devices (2FA)",
        href: "/docs/security#trusted-devices",
        body: "Trust a device for 30 days so it skips the code. Profile → Security, your account block, lists every device with a Revoke button.",
      },
    ],
  },
  {
    id: "developers",
    labelKey: "docs.catDev",
    entries: [
      {
        title: "MCP (AI agents)",
        href: "/docs/mcp",
        body: "Connect JomForm to Claude, Cursor and other AI agents over the Model Context Protocol. Agents can build forms and manage products, sales and more.",
      },
      {
        title: "Design → AI agent → MCP customization",
        href: "/docs/customize",
        body: "Design your form in a tool like Canva, then have an AI agent connected to the JomForm MCP apply that design with CSS and JS.",
      },
      {
        title: "Embed on your site",
        href: "/docs/embed",
        body: "Drop a form into any website with an iframe. The embedded form posts its height to your page, so it grows with the form.",
      },
      {
        title: "REST API",
        href: "/docs/api",
        body: "Drive JomForm from your own server: create forms, read sales and responses, manage products, subscriptions and webhooks.",
      },
    ],
  },
];

// The five first steps, raised to the top of the page (they used to sit under
// 2,811 words of feature text, where nobody found them).
const START_STEPS = [
  "Create a free account — your first form is ready in about 5 minutes.",
  "Build a form with the drag-and-drop builder and attach products.",
  "Add your CHIP payment credentials in Settings → CHIP.",
  "Publish the form — it goes live at your workspace URL or a custom domain.",
  "Track sales, subscriptions and email delivery from the dashboard.",
];

const QUICK_LINKS: { href: string; labelKey: Key; primary?: boolean }[] = [
  { href: "/docs/customize", labelKey: "docs.linkDesign", primary: true },
  { href: "/docs/placeholders", labelKey: "docs.linkPlaceholders" },
  { href: "/docs/embed", labelKey: "docs.linkEmbed" },
  { href: "/docs/mcp", labelKey: "docs.linkMCP" },
  // t_5425c958: the REST API reference arrived on main after this structure was
  // written. Every docs entry point is a quick link here, and
  // `docsApi.test.ts` requires the index to be able to reach /docs/api.
  { href: "/docs/api", labelKey: "docs.linkAPI" },
];

const FAQ: FaqEntry[] = [
  {
    q: "How do I connect an AI agent?",
    a: "Point any MCP client at the server URL https://mcp.jomform.com/mcp. It registers itself via OAuth 2.1 — no API keys needed. See the MCP tool reference for what agents can do.",
  },
  {
    q: "What are the placeholders in emails?",
    a: "Placeholders like {{ref_code}} and {{amount}} are replaced with real values when an email is sent. See the placeholders reference for the full list.",  },
  {
    q: "Can I use my own domain?",
    a: "Yes. Add your domain in Settings → Domains, point a CNAME to custom.jomform.com, and verify. Your forms then go live on your domain automatically.",
  },
  {
    // api/internal/httpapi/passkey_seam_test.go (TestPasskeyDocsDescribeTheRecoveryPath)
    // asserts this section as the passkey feature's documentation. Declared
    // as `title`/`body` with its own `slug` anchor, like every other section on
    // this page: the FAQ block renders only `q`/`a`, so the earlier shape put
    // this text in the source and on no page at all. #passkeys is the anchor
    // the Profile → Passkeys "?" deep-links (web/src/lib/passkeyDocs.test.ts).
    title: "Passkeys (sign in without a password)",
    slug: "passkeys",
    body: "Sign in with your face, fingerprint or device PIN instead of a password. Add a passkey from Profile → Security → Register passkey: your browser asks your device to create it, and the passkey appears in the list with the date it was added. Once you have one, the login page offers a passkey button before you type anything, because signing in with a passkey starts from a list of your credentials rather than an email address. Each passkey is bound to one device or password manager (iCloud Keychain, Google Password Manager, 1Password, a security key), so add one per device you sign in from, and remove any you no longer use with the delete button on its row. An older passkey may have been stored as a non-discoverable credential, which the login page's button cannot use: delete that row and register a new passkey, which is made discoverable so the button finds it.",
  },
];

export default function DocsIndexPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">
        <T k="docs.title" />
      </h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        <T k="docs.intro" />
      </p>

      <div className="mt-6 flex flex-wrap gap-3">
        {QUICK_LINKS.map((l) => (
          <Link
            key={l.href}
            href={l.href}
            className={
              l.primary
                ? "rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                : "rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            }
          >
            <T k={l.labelKey} />
          </Link>
        ))}
      </div>

      {/* Getting started — FIRST, not last. */}
      <section id="start" className="mt-10 scroll-mt-24">
        <h2 className="text-xl font-semibold text-slate-900">
          <T k="docs.gsTitle" />
        </h2>
        <ol className="mt-4 list-decimal space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
          {START_STEPS.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      </section>

      <h2 className="mt-12 text-xl font-semibold text-slate-900">
        <T k="docs.featuresTitle" />
      </h2>

      {GROUPS.map((g) => (
        <section key={g.id} id={g.id} className="mt-8 scroll-mt-24">
          <h3 className="text-base font-semibold uppercase tracking-wide text-[var(--jf-primary)]">
            <T k={g.labelKey} />
          </h3>
          <div className="mt-3 space-y-4">
            {g.entries.map((f) => (
              <div key={f.title} id={f.slug} className="scroll-mt-24">
                <h4 className="text-sm font-semibold text-slate-900">{f.title}</h4>
                <p className="mt-1 text-sm leading-relaxed text-slate-600">
                  {f.body}
                  {f.href ? (
                    <>
                      {" "}
                      <Link href={f.href} className="text-[var(--jf-primary)] underline underline-offset-2">
                        <T k="docs.next" />
                      </Link>
                    </>
                  ) : null}
                </p>
              </div>
            ))}
          </div>
          {g.more ? (
            <p className="mt-3 text-sm text-slate-600">
              {g.more.map((m, i) => (
                <span key={m.href}>
                  {i > 0 ? " · " : null}
                  <Link href={m.href} className="text-[var(--jf-primary)] underline underline-offset-2">
                    {m.label}
                  </Link>
                </span>
              ))}
            </p>
          ) : null}
        </section>
      ))}

      <h2 className="mt-12 text-xl font-semibold text-slate-900">
        <T k="docs.faqTitle" />
      </h2>
      {/* A section entry carries its own heading and anchor and renders prose
          (one <p> per sentence, like the section blocks above); a q/a entry
          renders a question and its answer. Both live under this heading. */}
      <div className="mt-4 space-y-4">
        {FAQ.map((f) =>
          f.q !== undefined ? (
            <section key={f.q}>
              <h3 className="text-sm font-semibold text-slate-900">{f.q}</h3>
              <p className="mt-1 text-sm leading-relaxed text-slate-600">{f.a}</p>
            </section>
          ) : (
            <section key={f.title} id={f.slug} className="scroll-mt-24">
              <h3 className="text-sm font-semibold text-slate-900">{f.title}</h3>
              <p className="mt-1 text-sm leading-relaxed text-slate-600">{f.body}</p>
            </section>
          ),
        )}
      </div>
    </div>
  );
}
