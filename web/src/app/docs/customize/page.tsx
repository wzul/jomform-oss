import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Design → AI agent → MCP customization — JomForm Docs",
  description:
    "Design your form or landing page in a tool like Canva, then use an AI agent (e.g. Claude) connected to the JomForm MCP to apply that design and customize it with CSS and JS.",
  alternates: { canonical: "https://jomform.com/docs/customize" },
};

// Issue #440: first-class documentation of the "design-tool → AI agent → MCP
// → customize" flow. A merchant designs their interface in a tool like Canva,
// then uses an AI agent (e.g. Claude) connected to the JomForm MCP to apply
// that design and further customize the form / landing page.
const STEPS: { title: string; body: string }[] = [
  {
    title: "1. Design your interface in a design tool",
    body: "Sketch your form or landing page in any design tool (Canva, Figma, etc.). Export the assets you need — a logo, a hero image, brand colours, and a font. JomForm keeps authority over prices and the payment flow, so you design the look and feel, not the checkout logic.",
  },
  {
    title: "2. Connect an AI agent to the JomForm MCP",
    body: "Point any MCP client (Claude, Cursor, etc.) at the server URL https://mcp.jomform.com/mcp. It registers itself via OAuth 2.1 — no API keys needed. The agent can then read and change your workspace through the MCP tools.",
  },
  {
    title: "3. Upload your media",
    body: "Use upload_media to upload your design assets (logo, hero image, and code assets like css/js/html/svg/woff2). Images are optimised and served immediately; code assets are scanned before being served.",
  },
  {
    title: "4. Apply the theme and landing page",
    body: "Use set_form_theme to apply your brand colours (primary, background, text, card_background), font, logo, layout and spacing to a form. Use product_image_size (small|medium|large|hidden) and product_image_layout (thumbnail-left|image-top) to control the product photo on the form — image-top gives a restaurant menu-card look with the photo above the name. Use set_workspace_landing_page to customise your workspace storefront with a hero, subtitle, body, image, theme colours, logo and call-to-action buttons.",
  },
  {
    title: "5. Add a restaurant menu (QR ordering)",
    body: "Use set_menu_page (issue #497: the menu is a PER-FORM setting, so it takes a form_id) to turn a form into a clean restaurant menu. Add Product blocks to the form for the items the menu sells — the menu lists ONLY the products the form references, so unrelated account products can never appear — then set enabled, categories ([{name, product_ids}]), and best_seller_ids (a list of product IDs shown as the Best Seller section). The menu is served at that form's own URL; the workspace home page is never affected and stays your normal landing page / form listing. Use a separate menu form per outlet or menu (lunch vs dinner, two brands) — they coexist independently in one workspace. Pair it with generate_qr to print a per-table QR stand — the ?table=N it encodes is read by the menu page, shown in the header (\"Table 8\") and carried onto the order as the table number. The menu checks out through the form's own order route: the buyer's name and email are required for CHIP, and the server charges the product's live price. If you want to collect extra info per order, set order_form_slug to a published form — the form's own fields (its builder blocks, or its registered_fields when set) render in the menu checkout step with their labels and required flags, are validated on the server, and are stored on the sale; there is no second field editor, so you create and edit those fields in the normal form builder. Name + email stay mandatory regardless of the attached form, and with no form attached the checkout collects name + email only. The menu is server-rendered and mobile-first: the header shows the table number, cafe name and location with an info panel; a category filter and search let a diner narrow the menu; best-sellers render as two-column square-image cards, and each category as list rows (text left, thumbnail right) with a separator between rows. Items without a photo render cleanly with no empty image slot. All labels follow the workspace default language (en/ms/zh/ta/ar). A diner never has to order a single item: the menu carries a basket (add-to-cart on every item, quantities, a sticky basket bar, checkout) — see the menu-basket section below for the full behaviour.",
  },
  {
    title: "6. Customize with CSS and JS",
    body: "For full control, set custom_css (styling/layout) and custom_js (form behaviour) on a form via set_form_theme. Custom JS is owner-gated and scanned for malicious code before it goes live, so it is safe to run on every buyer's page. JomForm keeps authority over prices and payment.",
  },
];

// Issue #513: the docs site is English-only today (the /docs pages are static
// server-rendered English, like the legal pages). The dashboard is translated
// (en/ms/zh/ta/ar), but the knowledge base is not — so this page, and every
// page it links to, stays in English. Do not half-translate a docs page.
const CART: { title: string; body: string }[] = [
  {
    title: "Tap an item to add it",
    body: "Every menu card and list row is the add button. Tapping it adds one of that item to the basket — no page load, no navigation to a form — and a small + badge on the item shows how many of it are in the basket. Tapping again adds another, so a diner can build up a multi-item order in one go.",
  },
  {
    title: "Set quantities in the basket",
    body: "The sticky basket bar appears across the bottom of the screen as soon as the basket is non-empty. It shows the live item count and running total, and opens the basket panel. Each basket line has a − / + quantity stepper (taking a line to zero removes it), a per-line Remove button, and a Clear basket button for the whole order. The panel also shows the Subtotal and — when a table number is present — the table it applies to. The total shown on screen is display-only: the server re-reads every price from the products table when the order is placed, so the buyer is always charged the live price.",
  },
  {
    title: "The basket survives filtering, searching and reloads",
    body: "A diner can narrow the menu by category or search, and the basket is untouched — the same after closing and reopening the page. The basket is stored in the buyer's own browser, keyed to the menu form, so two different menu forms in one workspace (two outlets, lunch vs dinner) keep separate baskets. The basket is cleared once an order is successfully placed.",
  },
  {
    title: "Checkout: name, email, then CHIP",
    body: "Checkout is part of the same basket panel: the diner's name and email are both mandatory (CHIP needs a name and an email to create a purchase), plus any extra fields you attached through order_form_slug. Submitting posts the basket to the form's own order route and redirects the diner to the CHIP checkout page to pay. If the order cannot be placed, an error is shown and the basket is kept so the diner can retry — nothing is lost.",
  },
  {
    title: "What lands on the sale",
    body: "The order becomes a normal JomForm sale: one line per basket item (product, quantity, live price), the buyer's name and email, the total, and the extra fields you asked for. A QR order also carries its table number, so the merchant sees which table to serve; and if the diner arrived through an affiliate link (?aff=CODE), the affiliate code is stored on the sale for commission.",
  },
];

const TOOLS: { name: string; purpose: string }[] = [
  { name: "set_form_theme", purpose: "Apply brand colours, font, logo, layout, spacing, product_image_size, product_image_layout, custom_css and custom_js to a form." },
  { name: "set_workspace_landing_page", purpose: "Customise the workspace storefront: hero, subtitle, body, image, theme colours, logo and CTAs." },
  { name: "upload_media", purpose: "Upload images and code assets (css/js/html/svg/woff2) for the landing page and forms." },
  { name: "upsert_form", purpose: "Create or update a form, including its optional theme." },
  { name: "generate_form", purpose: "Draft a validated form config (blocks + styling) from a natural-language prompt." },
  { name: "set_form_confirmations", purpose: "Set the confirmation message buyers see after submitting." },
  { name: "set_menu_page", purpose: "Turn a form into a restaurant menu (per-form: it takes a form_id) and set its categories, best-sellers and optional order form." },
  { name: "get_menu_page", purpose: "Read a form's restaurant-menu configuration." },
  { name: "generate_qr", purpose: "Generate a QR code (optionally per table) for a published form." },
  // Issue #513: affiliate marketing (#444) — documented here so the tools are
  // discoverable alongside the feature, not only in the MCP reference.
  { name: "create_affiliate", purpose: "Create an affiliate link (code + name + commission %) for a workspace. Returns the referral URL." },
  { name: "list_affiliates", purpose: "List a workspace's affiliates with their codes, commission and referral URLs." },
  { name: "get_affiliate_sales", purpose: "Read the sales attributed to one affiliate (read-only)." },
  { name: "set_affiliate_commission", purpose: "Change an affiliate's commission rate." },
];

export default function CustomizeDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">
        Design → AI agent → MCP customization
      </h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Design your form or landing page in a tool like Canva, then use an AI agent (e.g. Claude)
        connected to the JomForm MCP to apply that design and further customize it with CSS and JS.
        This is the fastest way to get a fully branded checkout without touching code.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">The flow</h2>
      <div className="mt-4 space-y-5">
        {STEPS.map((s) => (
          <section key={s.title}>
            <h3 className="text-base font-semibold text-slate-900">{s.title}</h3>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{s.body}</p>
          </section>
        ))}
      </div>

      <h2 id="menu-basket" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">The menu basket (multi-item ordering)</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A restaurant menu is not a one-item form: the menu page carries a full basket so a diner can
        order a whole table&apos;s food in one payment. Here is what the diner experiences and what
        reaches you. (Nothing to configure — the basket is part of every menu form.)
      </p>
      <div className="mt-4 space-y-5">
        {CART.map((s) => (
          <section key={s.title}>
            <h3 className="text-base font-semibold text-slate-900">{s.title}</h3>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{s.body}</p>
          </section>
        ))}
      </div>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">MCP tools used</h2>
      <div className="mt-4 overflow-hidden rounded-lg border border-slate-200">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-50 text-slate-700">
            <tr>
              <th className="px-4 py-2 font-semibold">Tool</th>
              <th className="px-4 py-2 font-semibold">Purpose</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 text-slate-600">
            {TOOLS.map((t) => (
              <tr key={t.name}>
                <td className="px-4 py-2 font-mono text-xs text-slate-800">{t.name}</td>
                <td className="px-4 py-2">{t.purpose}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Issue #513: affiliate marketing (#444) shipped wired (MCP + REST +
          Settings tab) with zero docs. It lives on the customize page — the
          flow where an agent manages a workspace — and has its own labelled
          section, with the REST paths so both paths are usable. */}
      <h2 id="affiliate-marketing" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">Affiliate marketing (referral links &amp; commission)</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        An <strong>affiliate</strong> is a promoter you pay for sales they bring. You give each
        affiliate a short <strong>code</strong>, and their link is your storefront URL with{" "}
        <span className="font-mono text-xs">?aff=CODE</span> appended — for example{" "}
        <span className="font-mono text-xs">https://your-workspace.jomform.com/?aff=ALI</span>. When a
        buyer arrives through that link and completes an order, the code is stored on the sale, so the
        sale is attributed to that affiliate and you can see what they earned. Attribution works on
        menu orders too (the code is carried to the basket checkout).
      </p>

      <h3 className="mt-5 text-base font-semibold text-slate-900">Create an affiliate link</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        In the dashboard, open <strong>Settings → Affiliates</strong> and add a code, an optional name
        and a commission percentage. The new affiliate appears in the list with its code, its
        commission and its ready-to-share referral URL. Over MCP, call{" "}
        <span className="font-mono text-xs">create_affiliate</span> with the{" "}
        <span className="font-mono text-xs">workspace_id</span>, a{" "}
        <span className="font-mono text-xs">code</span>, an optional{" "}
        <span className="font-mono text-xs">name</span> /{" "}
        <span className="font-mono text-xs">email</span>, and{" "}
        <span className="font-mono text-xs">commission_pct</span> (10 = 10%); it returns the affiliate,
        including the <span className="font-mono text-xs">referral_url</span>. A code must be unique
        within the workspace, and creating or changing an affiliate needs the{" "}
        <span className="font-mono text-xs">manage</span> or{" "}
        <span className="font-mono text-xs">admin</span> role.
      </p>

      <h3 className="mt-5 text-base font-semibold text-slate-900">Set the commission</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        Commission is a percentage stored per affiliate. Change it with{" "}
        <span className="font-mono text-xs">set_affiliate_commission</span> (takes{" "}
        <span className="font-mono text-xs">affiliate_id</span> and the new{" "}
        <span className="font-mono text-xs">commission_pct</span>). The rate used is the one stored on
        the affiliate, so a change applies to sales attributed from then on; it is not written back
        onto sales that already happened.
      </p>

      <h3 className="mt-5 text-base font-semibold text-slate-900">How a sale is attributed</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        The affiliate shares their referral URL. When a buyer opens it, the form (or menu) page reads{" "}
        <span className="font-mono text-xs">?aff=CODE</span> and carries it into the order it submits,
        where it is stored on the sale as the affiliate code. No cookie, no redirect chain: the code
        on the URL the buyer used is what attributes the sale. A buyer with no{" "}
        <span className="font-mono text-xs">?aff=</span> simply produces an unattributed sale.
      </p>

      <h3 className="mt-5 text-base font-semibold text-slate-900">Read an affiliate&apos;s sales</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        Use <span className="font-mono text-xs">list_affiliates</span> to get the affiliate ids, then{" "}
        <span className="font-mono text-xs">get_affiliate_sales</span> with the{" "}
        <span className="font-mono text-xs">affiliate_id</span> (read-only). Each row gives the sale
        reference, status, total, currency and date. Attribution is recorded on every sale that came
        in with the code; the commission percentage is the affiliate&apos;s current rate (the figures
        are returned, not a pre-computed payout ledger).
      </p>

      <h3 className="mt-5 text-base font-semibold text-slate-900">REST endpoints</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        The same surface is available over the REST API, authenticated as the workspace:
      </p>
      <div className="mt-3 overflow-hidden rounded-lg border border-slate-200">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-50 text-slate-700">
            <tr>
              <th className="px-4 py-2 font-semibold">Method &amp; path</th>
              <th className="px-4 py-2 font-semibold">Purpose</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 text-slate-600">
            <tr>
              <td className="px-4 py-2 font-mono text-xs text-slate-800">GET /v1/workspaces/{"{id}"}/affiliates</td>
              <td className="px-4 py-2">List the workspace&apos;s affiliates.</td>
            </tr>
            <tr>
              <td className="px-4 py-2 font-mono text-xs text-slate-800">POST /v1/workspaces/{"{id}"}/affiliates</td>
              <td className="px-4 py-2">Create an affiliate (<span className="font-mono text-xs">code</span>, <span className="font-mono text-xs">name</span>, <span className="font-mono text-xs">email</span>, <span className="font-mono text-xs">commission_pct</span>).</td>
            </tr>
            <tr>
              <td className="px-4 py-2 font-mono text-xs text-slate-800">PUT /v1/workspaces/{"{id}"}/affiliates/{"{affid}"}/commission</td>
              <td className="px-4 py-2">Set the affiliate&apos;s commission percentage.</td>
            </tr>
            <tr>
              <td className="px-4 py-2 font-mono text-xs text-slate-800">GET /v1/workspaces/{"{id}"}/affiliates/{"{affid}"}/sales</td>
              <td className="px-4 py-2">Read the sales attributed to that affiliate.</td>
            </tr>
          </tbody>
        </table>
      </div>

      {/* Issue #457 follow-up (Wan, 2026-09-19): editing one of your own
          templates. AGENTS.md documentation-parity rule — the feature is
          documented where templates are already documented, and the two
          semantics a merchant asks about (it does NOT change forms already
          created from it; a template is never published) are stated. */}
      <h2 id="templates" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">Templates (save a form &amp; reuse it)</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A <strong>template</strong> is a saved form you can start new forms from. It is private to
        your workspace: it never appears in another merchant&apos;s gallery and it has no
        draft/published state to manage. Build one either way:
      </p>
      <ul className="mt-3 list-disc space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>Save form as template</strong> in the builder, or <strong>Save form as template</strong> on
          the Templates page (pick the form) — its fields and design are copied into a template.
        </li>
        <li>
          <strong>New template</strong> on the Templates page — an empty template, ready to fill in.
        </li>
      </ul>

      <h3 className="mt-5 text-base font-semibold text-slate-900">Edit a template&apos;s fields and design</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        The <strong>Templates</strong> page lists your templates with <strong>Edit</strong>,{" "}
        <strong>Use template</strong>, <strong>Rename</strong> and <strong>Delete</strong>.{" "}
        <strong>Edit</strong> opens the template in the same builder you use for forms, so you change
        its fields, blocks and design exactly as you would on a form, then <strong>Save</strong>. The
        builder recognises that it is editing a template: there is no slug, no Publish and no Form
        Settings — a template is not published and has no public URL, so those controls are not
        shown.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        <strong>Editing a template does not change forms already created from it.</strong> Cloning a
        template copies its fields and design at that moment, so a form and its template are
        independent afterwards: fix a typo in a template and the forms you already made keep the old
        text until you edit them too. Over MCP the same edit is{" "}
        <span className="font-mono text-xs">update_template</span> (with{" "}
        <span className="font-mono text-xs">template_id</span> from{" "}
        <span className="font-mono text-xs">list_own_templates</span>); the REST equivalent is{" "}
        <span className="font-mono text-xs">PUT /v1/rest/templates/{`{id}`}</span> with{" "}
        <span className="font-mono text-xs">title</span> /{" "}
        <span className="font-mono text-xs">config</span> /{" "}
        <span className="font-mono text-xs">theme</span> (an omitted field is left unchanged).
      </p>

      <h3 className="mt-5 text-base font-semibold text-slate-900">A template never lands in another merchant&apos;s gallery by accident</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        Your templates and the public gallery are two separate lists, and the builder&apos;s template
        mode keeps them that way: saving a template writes only its fields and design, never a
        publish. Publishing a template into the shared gallery is a deliberate, separate act that
        requires a category and a short description line, so a template can never appear in someone
        else&apos;s New-form picker just because you edited it.
      </p>

      {/* Issue #501: the https-only scheme rule for image/URL fields is a
          user-visible constraint, so it is documented here (AGENTS.md
          documentation-parity rule). */}
      <h2 className="mt-8 text-xl font-semibold text-slate-900">Image URLs must be https://</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Every image/URL you set — the form logo (<span className="font-mono text-xs">set_form_theme</span>), the
        landing-page hero image and logo (<span className="font-mono text-xs">set_workspace_landing_page</span>, or
        Settings → Storefront), product images (<span className="font-mono text-xs">create_product</span> /{" "}
        <span className="font-mono text-xs">update_product</span>, or the Products editor) and a form&apos;s image
        block — must be an absolute <span className="font-mono text-xs">https://</span> URL. Uploaded media from the
        media library already qualifies: <span className="font-mono text-xs">upload_media</span> returns an absolute
        https URL on your own media host, which is accepted everywhere.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A value with any other scheme (<span className="font-mono text-xs">data:</span>,{" "}
        <span className="font-mono text-xs">http:</span>, <span className="font-mono text-xs">javascript:</span>, or a
        relative path) is rejected when you save it, with a clear message, on both the dashboard and the MCP — the
        same rule on every path. This is deliberate: the page renders the value into an <span className="font-mono text-xs">&lt;img src&gt;</span> for
        your buyers, and a browser will not load an unsafe scheme. Rejecting it at entry means you see the problem
        immediately instead of getting a silently missing image on the live page.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Leaving an image field <em>empty</em> is always allowed and means &ldquo;no image&rdquo; (or, when updating,
        &ldquo;keep the current one&rdquo;).
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Example prompt for your agent</h2>
      <pre className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100">
{`"Apply my Canva design to my JomForm checkout:
1. Upload the logo and hero image from my design (upload_media).
2. Set the form theme to my brand colours, font and layout (set_form_theme).
3. Customise my landing page hero and CTA (set_workspace_landing_page).
4. Add custom CSS for spacing and custom JS to auto-focus the first field (set_form_theme)."`}
      </pre>

      <p className="mt-8 text-sm leading-relaxed text-slate-600">
        JomForm keeps authority over prices and the payment flow — the agent customizes the look and
        feel, never the checkout logic. See the{" "}
        <a href="/docs/mcp" className="text-[var(--jf-primary)] hover:underline">
          MCP tool reference
        </a>{" "}
        for the full list of tools.
      </p>
    </div>
  );
}
