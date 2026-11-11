import type { Metadata } from "next";
import { docsMetadata } from "../../lib/docsMetadata";
import Link from "next/link";

import type { Key } from "../../components/i18n";
import { T } from "./docsChrome";

export const metadata: Metadata = docsMetadata({
  title: "JomForm Docs — Overview & features",
  description:
    "Every JomForm feature in one line, grouped by category: start here, sales & payments, forms & fields, storefront, email, growth, security and developer guides.",
  path: "/docs",
});

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
      {
        // Card t_94e5cca3: the dormancy report and the retention policy behind the
        // /admin dormancy section's "?" link. One sentence, as the entry-shape
        // guard requires; the walk-through is on the page.
        title: "Dormant workspaces and retention",
        href: "/docs/dormancy",
        body: "A read-only report of how long each workspace has been quiet, why a silent merchant is not a silent form, which accounts are excluded, and the one reminder email.",
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
        // Shipping: the managed zone rate source, and the courier providers that
        // can quote the same block live (phase 2). The summary is short and the
        // walk-through lives on the guide; the five MCP tool names are asserted on
        // that page by docsFacts.ts.
        title: "Shipping by zone",
        href: "/docs/shipping",
        body: "Charge a delivery fee by zone — state, district or postcode — with a rate per weight band and a free-shipping threshold, or a courier quotes it at checkout, falling back to these rates.",
      },
      {
        // Card t_135c3c72: the buyer-borne processing fee. The summary is short;
        // the walk-through (gross-up table, disclosure, snapshot, refund rule)
        // lives on the guide, which also names both MCP tools.
        title: "Processing fees paid by the buyer",
        href: "/docs/payment-fees",
        body: "Let the buyer pay the CHIP processing fee: one rate per payment method, grossed up so you receive the full price.",
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
    // Card t_0356ed4c: both reference pages were split by task, and the sidebar
    // now carries the task pages. This row is how the index reaches them —
    // docsIndex.test.ts requires every sidebar route to be linked from the index.
    more: [
      { href: "/docs/mcp/connect", label: "Connect an MCP client" },
      { href: "/docs/mcp/tools", label: "MCP tool list" },
      { href: "/docs/mcp/oauth", label: "MCP OAuth & tokens" },
      { href: "/docs/mcp/troubleshooting", label: "MCP troubleshooting" },
      { href: "/docs/api/authentication", label: "API authentication & scopes" },
      { href: "/docs/api/endpoints", label: "API endpoints by resource" },
      { href: "/docs/api/webhooks", label: "API webhooks" },
      { href: "/docs/api/errors", label: "API errors & limits" },
    ],
  },
];

// The five first steps, raised to the top of the page (they used to sit under
// 2,811 words of feature text, where nobody found them).
//
// Card t_ff973d0b: they now sit above the reference chip row as well. Before
// this the page opened with six chips whose FIRST one was filled purple, so a
// reference shortcut read as the page's primary action while "Start here" — the
// actual onboarding path — started below it. The order is the claim: this
// section is the page's one primary path, and the chips are shortcuts that
// follow it.
const START_STEPS = [
  "Create a free account — your first form is ready in about 5 minutes.",
  "Build a form with the drag-and-drop builder and attach products.",
  "Add your CHIP payment credentials in Settings → CHIP.",
  "Publish the form — it goes live at your workspace URL or a custom domain.",
  "Track sales, subscriptions and email delivery from the dashboard.",
];

// Card t_ff973d0b: the picture-first route through the same material. It used to
// be one chip in the row below, where it was a third way to reach a page the
// sidebar already carried; it belongs to the onboarding path instead, so a new
// reader who would rather follow screenshots meets it INSIDE "Start here".
// `docsIndex.test.ts` reads this array as a fourth surface the index links a
// route from, so /docs/guides stays reachable from the page itself and not only
// from the sidebar row the guard separately checks.
const START_LINKS: { href: string; labelKey: Key }[] = [
  { href: "/docs/guides", labelKey: "docs.linkGuides" },
];


// One emoji per group, because a reader scrolling /docs scans for a shape before
// they read a label. Keyed on the group id, so a new group gets an explicit one
// rather than silently rendering none.
const GROUP_EMOJI: Record<string, string> = {
  "account": "🏢",
  "sales-payments": "💰",
  "forms-fields": "🧾",
  "storefront": "🛍️",
  "email": "✉️",
  "growth": "📈",
  "security": "🔒",
  "developers": "🧑‍💻",
};

// Popular references, rendered under their own label BELOW "Start here" (card
// t_ff973d0b). The row used to open with a FILLED purple chip — "Design → AI
// agent → MCP guide" — so on a docs home a reference shortcut read as the
// primary action. No chip is filled now: they are secondary shortcuts to the
// three look-up pages a returning reader hunts for, and the primary path is the
// section above them.
//
// Three, not six (card t_ff973d0b, "one destination, one home"): the dropped
// chips all pointed at a destination the page already carries — "Design → AI
// agent → MCP guide" is the "Design → AI agent → MCP customization" entry below
// AND the sidebar row, and "Embed on your site" is an entry with the same title
// below. "Screenshot guides" moved into START_LINKS above.
const QUICK_LINKS: { href: string; labelKey: Key }[] = [
  { href: "/docs/placeholders", labelKey: "docs.linkPlaceholders" },
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

      {/* Getting started — FIRST, above the shortcut row (card t_ff973d0b). A
          reference chip used to be the first thing under the intro. */}
      <section id="start" className="mt-8 scroll-mt-24">
        <h2 className="text-xl font-semibold text-slate-900">
          <T k="docs.gsTitle" />
        </h2>
        <ol className="mt-4 list-decimal space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
          {START_STEPS.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
        {START_LINKS.map((l) => (
          <p key={l.href} className="mt-1 text-sm text-slate-600">
            {/* `min-h-[44px]` (card t_ff973d0b): an inline link in a paragraph is
                a ~20px tap target, and the index is the densest page in the
                product on a phone. Every link this page owns is at least 44px
                tall; the row stays visually inline because the text is centred
                in the box. */}
            <Link
              href={l.href}
              className="inline-flex min-h-[44px] items-center font-medium text-[var(--jf-primary-strong)] underline underline-offset-2"
            >
              <T k={l.labelKey} />
            </Link>
          </p>
        ))}
      </section>

      {/* Popular references — SECONDARY shortcuts, under their own label and
          after the onboarding path (card t_ff973d0b). No chip is filled: a
          filled chip here is what made a reference link read as the page's CTA. */}
      <section id="popular" className="mt-8 scroll-mt-24">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-600">
          <T k="docs.popularTitle" />
        </h2>
        <div className="mt-2 flex flex-wrap gap-2">
          {QUICK_LINKS.map((l) => (
            <Link
              key={l.href}
              href={l.href}
              className="inline-flex min-h-[44px] items-center rounded-lg border border-slate-200 px-4 text-sm font-medium text-slate-700 hover:bg-slate-50"
            >
              <T k={l.labelKey} />
            </Link>
          ))}
        </div>
      </section>

      <h2 className="mt-12 text-xl font-semibold text-slate-900">
        <T k="docs.featuresTitle" />
      </h2>

      {GROUPS.map((g) => (
        <section key={g.id} id={g.id} className="mt-8 scroll-mt-24">
          {/* `data-toc-skip` (card t_ff973d0b): the right rail is "On this page"
              and nothing else. These category headings carried the sidebar's own
              taxonomy into it — emoji and all, twice on one screen — so the rail
              rendered a second site nav next to the ToC. The anchor still works
              for a deep link; only the rail entry is gone. */}
          <h3
            data-toc-skip
            className="flex items-center gap-2 text-base font-semibold uppercase tracking-wide text-[var(--jf-primary-strong)]"
          >
            <span aria-hidden="true" className="text-lg normal-case">
              {GROUP_EMOJI[g.id] ?? "📄"}
            </span>
            <T k={g.labelKey} />
          </h3>
          <div className="mt-3 space-y-4">
            {g.entries.map((f) => (
              <div
                key={f.title}
                id={f.slug}
                className="scroll-mt-24 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg-soft)] px-4 py-3"
              >
                <h4 className="text-base font-semibold text-[var(--jf-text)]">{f.title}</h4>
                <p className="mt-1 text-base leading-7 text-[var(--jf-text-secondary)]">
                  {f.body}
                </p>
                {/* `min-h-[44px]` (card t_ff973d0b): these rows are the index's
                    "next step" target on a phone — 20px tall is not a tap target. */}
                {f.href ? (
                  <Link
                    href={f.href}
                    className="mt-1 inline-flex min-h-[44px] items-center text-sm font-medium text-[var(--jf-primary-strong)] underline underline-offset-2"
                  >
                    <T k="docs.next" />
                  </Link>
                ) : null}
              </div>
            ))}
          </div>
          {g.more ? (
            <p className="mt-1 text-sm text-slate-600">
              {g.more.map((m, i) => (
                <span key={m.href}>
                  {i > 0 ? " · " : null}
                  <Link
                    href={m.href}
                    className="inline-flex min-h-[44px] items-center text-[var(--jf-primary-strong)] underline underline-offset-2"
                  >
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
          renders a question and its answer. Both live under this heading.
          `data-toc-skip` (card t_ff973d0b): the right rail stopped listing FAQ
          questions — "On this page" is the page's anchors, not a second FAQ
          index, and five questions pushed the page's real structure off the
          rail. Each question's heading stays a heading for a screen reader. */}
      <div className="mt-4 space-y-4">
        {FAQ.map((f) =>
          f.q !== undefined ? (
            <section key={f.q}>
              <h3 data-toc-skip className="text-sm font-semibold text-slate-900">{f.q}</h3>
              {/* `[overflow-wrap:anywhere]` (card t_68456ead): an FAQ answer is
                  prose that carries tokens — the MCP answer has a bare
                  https://mcp.jomform.com/mcp — and a URL has no whitespace to
                  break at. At a 24px browser root on a 320px viewport that one
                  token painted to right 344.95 and panned the whole page 25px
                  (no element rect exceeded the viewport: it was text-node INK
                  overflow). `anywhere` — not `break-word` — because it also
                  drops the token's min-content width, so the answer can shrink
                  inside the column. It only opens a break when the line would
                  otherwise overflow, so at 100% text size the layout is
                  unchanged (measured: pan 0 at 16px both before and after).
                  Pinned by src/lib/docsFaqProseWrap.test.ts. */}
              <p className="mt-1 text-sm leading-relaxed text-slate-600 [overflow-wrap:anywhere]">{f.a}</p>
            </section>
          ) : (
            <section key={f.title} id={f.slug} className="scroll-mt-24">
              <h3 data-toc-skip className="text-sm font-semibold text-slate-900">{f.title}</h3>
              <p className="mt-1 text-sm leading-relaxed text-slate-600">{f.body}</p>
            </section>
          ),
        )}
      </div>
    </div>
  );
}
