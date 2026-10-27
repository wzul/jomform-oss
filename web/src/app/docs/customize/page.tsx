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
    body: "Use upload_media to upload your design assets (logo, hero image, and code assets like css/js/html/svg/woff2). Images are optimised and served immediately; code assets are scanned before being served. An SVG must be a well-formed XML document: a browser renders it as XML, so a file with text before the <svg> tag (for example a /* */ banner) is refused rather than stored as a broken image.",
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
    body: "For full control, set custom_css (styling/layout) and custom_js (form behaviour) on a form via set_form_theme. Custom JS is owner-gated and scanned for malicious code before it goes live, so it is safe to run on every buyer's page — including a custom HTML form, where it runs on the outer page and the HTML body itself stays inert (see Custom HTML forms). A script the scan blocks is not saved: the save is refused and tells you the Custom JS field is the one that was rejected, and which kind of pattern caused it (code execution, obfuscation/beacon code, or a reference to an external URL) — remove that part and save again. JomForm keeps authority over prices and payment.",
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
    title: "A sold-out dish cannot be added, and a stale basket line is dropped",
    body: "Stock is re-read live, not frozen when the page was served. A menu page stays open while a diner reads it, so a dish can sell out in between: tapping it then adds nothing and tells the diner the dish is sold out (naming it) rather than letting them carry it to checkout. That holds for a dish with variations and for a plain one — a sold-out plain dish is served the same button as an in-stock one, so the tap asks the server before it adds. A basket line whose dish sold out while the basket sat there is dropped when the page is opened again, with a notice naming the dish, so nothing is carried to checkout only to be rejected there. If the live check cannot answer, the page keeps the state it was served with, so the menu still works when the connection does not.",
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

      {/* t_d768342e: the full text of the /docs summaries that point here. Each
          id below is a link target declared on /docs (docsIndex.test.ts fails the
          build if the anchor stops existing), so the ids are the contract. */}
      <h2 id="menu" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        The restaurant menu (QR ordering)
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Turn a form into a clean restaurant menu. The menu is a <strong>per-form</strong> setting, so
        you configure it on the form itself (Form Settings → Restaurant menu) and it is served at
        that form&apos;s own URL. The workspace home page always stays your normal landing page /
        form listing, so one workspace can run several menus — two outlets, lunch vs dinner, two
        brands — simply by using a menu form for each. They are fully independent.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The menu lists <strong>only the products the form references</strong> through its own product
        blocks, so unrelated account products can never appear. Pick those into named categories with
        a searchable product picker and a live preview, and highlight a best-seller. Customers scan a
        QR stand at the table to order and pay online, and the order reaches you with the table
        number. Configure it over MCP via <span className="font-mono text-xs">set_menu_page</span> /
        <span className="font-mono text-xs">get_menu_page</span> (both take a{" "}
        <span className="font-mono text-xs">form_id</span>), or generate the QR stand with{" "}
        <span className="font-mono text-xs">generate_qr</span>.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Need more per order — a delivery address, phone number, spice level, notes or pickup time?
        Attach an optional order form to the menu: the form&apos;s own fields render in the checkout
        step with their labels and required flags, are validated on the server, and are stored on the
        sale. There is no second field editor — you create and edit those fields in the normal form
        builder, and the menu settings link straight to it. With no form attached the checkout
        collects name + email only and still works.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The menu checkout always collects the buyer&apos;s name and email (CHIP requires both — they
        can never be removed), and the server charges each product&apos;s live price. The menu is
        server-rendered and mobile-first: the header shows the table number, cafe name and location
        with an info panel; a category filter and search let a diner narrow the menu; best-sellers
        render as two-column square-image cards, and each category as list rows (text left, thumbnail
        right) with a separator between rows. Items without a photo render cleanly with no empty image
        slot. All labels follow the workspace default language.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A diner never has to order a single item: the menu carries a basket — see{" "}
        <a href="#menu-basket" className="text-[var(--jf-primary)] underline underline-offset-2">
          the menu basket
        </a>{" "}
        for the full behaviour.
      </p>

      <h2 id="product-images" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Product images on forms
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Choose how each product photo appears on your form: <strong>size</strong> (small 52px, medium
        96px, large 160px or hidden) and <strong>layout</strong> (thumbnail-left, the default, or
        image-top — a menu-card look with a full-width photo above the name, ideal for a restaurant
        menu). The renderer serves the matching optimised media variant for the chosen size. Set it in
        Form Settings → General (with a live preview) or over MCP via{" "}
        <span className="font-mono text-xs">set_form_theme</span> (
        <span className="font-mono text-xs">product_image_size</span> /{" "}
        <span className="font-mono text-xs">product_image_layout</span>).
      </p>

      <h2 id="brand-colour" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Your brand colour on the public form
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The colour you pick under Form Settings → General (<strong>Theme colour</strong>) paints the
        public form&apos;s own chrome: the <strong>submit button</strong> and its hover and pressed
        states, the checkbox and radio accent, the product price text, and the footer and
        confirmation links. It does not override anything you set in{" "}
        <span className="font-mono text-xs">custom_css</span>, and an image or video you embed keeps
        its own colours.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The button&apos;s text colour is chosen for you so the label always stays readable on the
        colour you picked: a dark brand colour gets a white label and a light one gets dark text,
        and the hover and pressed shades are derived from your colour rather than from a fixed
        indigo. A green or amber button therefore reads as one button in three states, not as your
        colour with an indigo hover. Each state is measured against the WCAG AA 4.5:1 floor before
        it is used.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Set it in Form Settings → General and the button previews it immediately; over MCP the same
        field is <span className="font-mono text-xs">theme.primary</span> through{" "}
        <span className="font-mono text-xs">set_form_theme</span>. Leave it unset and the form uses
        the default indigo, which is the same colour the dashboard uses.
      </p>

      <h2 id="builder" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Building an order form
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Build beautiful order forms with a drag-and-drop builder. Add fields (name, email, phone,
        textarea, text, number, select, radio, checkbox, date, time, website, address, consent, hidden,
        list, repeater, image-choice, captcha), attach products, and publish to your own workspace
        URL or a custom domain. Forms can be <strong>payment forms</strong> or{" "}
        <strong>response-only</strong> — the mode decides what the form collects and what its entries
        are called, and it is set in the builder.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        An agent can build the same form:{" "}
        <span className="font-mono text-xs">upsert_form</span> creates or updates it,{" "}
        <span className="font-mono text-xs">register_form_fields</span> sets its input fields under a
        strict contract, and <span className="font-mono text-xs">publish_form</span> takes it live.
      </p>

      <h2 id="publishing" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Publishing a form
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Publishing puts your form on the internet at its own URL — the link a buyer opens, or the
        address you embed. Press <strong>Publish</strong> in the builder and JomForm confirms it
        immediately: a panel names the form as live, shows the public URL, and gives you an{" "}
        <strong>Open form</strong> and a <strong>Copy link</strong> action, so a first publish never
        ends in silence. If the URL cannot be resolved yet (for example a workspace whose subdomain
        is not set up), the panel says so plainly rather than showing a link that goes nowhere.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Two rules decide whether a form may go live. A <strong>response-mode</strong> form always
        may — collecting answers is its whole job, and having no products is normal for it. A{" "}
        <strong>payment form</strong> needs something to sell: publishing one with no products is
        refused once with a clear message, because such a form can never take money, and you can
        confirm and publish it anyway if you only meant to collect details. Products can come from a
        product block, the form&apos;s product snapshot, a priced option or shipping choice, or an
        enabled restaurant menu; adding one any time removes the question.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The same rule covers every way a form goes live — the <strong>Publish</strong> button, the
        status control in <strong>Form Settings</strong>, publishing several forms at once from the{" "}
        <strong>Forms</strong> list, and AI agents over MCP (
        <span className="font-mono text-xs">publish_form</span> and{" "}
        <span className="font-mono text-xs">bulk_forms</span>, both of which accept{" "}
        <span className="font-mono text-xs">acknowledge</span>), so an agent is told the same thing
        you are. A form that is already live is never asked about again.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The link preview follows the same rule as the page. Every live form has its own share card
        at <span className="font-mono text-xs">/og/your-form-slug.png</span> — its title, workspace
        and address, drawn as a 1200×630 image — and that is what a chat app or social site shows
        when the link is pasted. A <strong>draft</strong> is not served at all: its card answers
        404, exactly as its page does, so an unpublished form&apos;s title never becomes public
        through the preview URL. A <strong>private</strong> form&apos;s card is served only to a
        browser that has already entered the password, and it is never handed to a shared cache,
        so the password gate is not bypassed by fetching the image. An <strong>unlisted</strong>{" "}
        form is live, so its card is served like a published one&apos;s.
      </p>

      <h2 id="domains" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Custom domains
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Put your forms on your own domain (e.g. <span className="font-mono text-xs">shop.yourname.com</span>
        ). Add the domain, point a CNAME to <span className="font-mono text-xs">custom.jomform.com</span>,
        and verify. With Cloudflare linked, JomForm can auto-create the CNAME for you. Auto-TLS
        (Let&apos;s Encrypt) uses your account email as the ACME contact by default — you can change it
        per domain in Settings → Domains.
      </p>

      <h2 id="form-url-slug" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Form URLs and slugs (rename a form)
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        {/* t_e3ebe979 (the rebase onto main): main (#679) documented the slug rename
            in the /docs entry&apos;s own body. The restructure cuts that entry to a
            summary, so its walk-through — the character rule, the refusal, and the
            temporary redirect — lives here, and /docs links this anchor. */}
        Every form is served at the slug you gave it —{" "}
        <span className="font-mono text-xs">your-workspace.jomform.com/your-form-slug</span> (or the
        same path on a verified custom domain). The slug is editable: change it in{" "}
        <strong>Form Settings → General</strong> and the form moves to the new URL immediately. A
        slug is 1–40 characters of <span className="font-mono text-xs">a-z</span>,{" "}
        <span className="font-mono text-xs">0-9</span> and dashes (uppercase and spaces are
        normalised), and it must be free within your workspace — a rename onto a slug another of
        your forms already serves is refused with an error rather than silently overwriting either
        form&apos;s URL.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Renaming never orphans the URL you already handed out: the old slug keeps working through a{" "}
        <strong>temporary redirect (HTTP 301)</strong> to the new one, so a printed QR stand, an
        embedded iframe, a menu&apos;s attached order form, and a link already shared in a campaign
        all keep resolving. The redirect is temporary, not a permanent alias: if another form in the
        workspace later claims the freed slug, that LIVE form wins and the old URL serves it — a
        rename&apos;s history can never shadow a real form&apos;s URL. Slugs cannot be renamed on
        templates (a template has no public URL) and AI agents do the same thing with the{" "}
        <span className="font-mono text-xs">slug</span> parameter on{" "}
        <span className="font-mono text-xs">upsert_form</span>, which returns the slug the form now
        owns.
      </p>

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
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        You do not have to start from a blank form either: the built-in gallery already covers the
        common shapes — order forms, booking, donation, contact, survey and more — and cloning one
        copies it into your workspace as a new draft.
      </p>

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
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        That is because every gallery card is rendered from two fields of the template&apos;s own
        config: the <strong>category</strong> is{" "}
        <span className="font-mono text-xs">config.category</span>, and the <strong>description</strong>{" "}
        — the card&apos;s subtitle — is the template&apos;s <strong>intro line</strong>: the value of
        its <strong>first Section block</strong>. A Text block also counts, because templates written
        before Section existed kept their intro there and their cards must keep working — but a Text
        block is a single-line <em>answer</em> field now, so its value is a field label, and a new
        template should put its intro in a Section block. A template you write yourself gets the same
        treatment: set a category and put a short intro line in a Section block, or its card has
        nothing to show.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Publishing is therefore checked: a template missing either field is{" "}
        <strong>refused with a clear message</strong> rather than appearing in the gallery as a bare
        title. Publishing several forms at once behaves differently on purpose — a{" "}
        <strong>bulk publish skips such a template while still publishing the other forms you
        selected</strong>, so one unfinished template cannot block a whole batch. The skipped ids are
        reported back, so the skip is never silent.
      </p>

      {/* Language placement (AGENTS.md documentation-parity rule): the split
          between the per-profile DASHBOARD language and the buyer-facing form
          language is the thing merchants get wrong, so it is stated as a rule
          and not just described. */}
      <h2 id="language" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">Language: dashboard vs. your forms</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        JomForm has <strong>two separate language settings</strong>, and they are deliberately not the
        same one. The supported languages are <strong>English, Bahasa Melayu, 中文, தமிழ்</strong> and{" "}
        <strong>العربية</strong> (Arabic also switches the dashboard to right-to-left).
      </p>
      <ul className="mt-3 list-disc space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>Dashboard language</strong> — the language <em>you</em> see when signed in. It is a{" "}
          <strong>per-user</strong> setting: change it on <strong>Profile</strong> (Dashboard language),
          reached by clicking your <strong>account block</strong> — your name and email at the bottom of
          the sidebar, or at the bottom of the menu on a phone — and
          it follows you into any browser you sign in from, on any machine. It never changes what a
          buyer sees. One part of the product does not follow it: this knowledge base and the legal
          pages are written in English only, so a guide or a policy stays English (and reads
          left-to-right) even while your dashboard is in Arabic — only the labels on the docs index
          are translated. Until you pick one, the dashboard uses the language last used in that browser,
          and English if there is none.
        </li>
        <li>
          <strong>Form language</strong> — the language your <em>customers</em> see on a public form, the
          menu, the thank-you page and the password gate. It is set per form or per workspace, never
          per browser and never from your dashboard choice.
        </li>
      </ul>

      <h3 className="mt-5 text-base font-semibold text-slate-900">Choosing your customers&apos; language</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        Resolution is <strong>form → workspace → English</strong>: a per-form language wins when set;
        otherwise the form uses its workspace&apos;s <strong>Default form language</strong> (Settings →
        Storefront); otherwise English. Set the workspace default once and every form you publish
        inherits it, so a Malay storefront does not need the language set on each form. Over MCP the
        workspace default is <span className="font-mono text-xs">set_workspace_settings</span> with{" "}
        <span className="font-mono text-xs">default_lang</span>; the per-form language is{" "}
        <span className="font-mono text-xs">config.lang</span> on{" "}
        <span className="font-mono text-xs">upsert_form</span>. An unsupported code is rejected rather
        than stored, so a form can never be left on a language with no strings.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The translation covers the buyer-facing chrome — field labels, the total, the submit button,
        error messages, the menu, and the footer links (Privacy / Terms / Refund) — while your own
        field labels, product names, prices and descriptions are shown exactly as you typed them.
        JomForm does not machine-translate your content.
      </p>

      {/* Email language (AGENTS.md documentation-parity rule): the two-axis
          rule above is invisible to a merchant until he sees WHICH email came
          out in which language, so the emails are stated explicitly — including
          the case he cannot test himself, the guest buyer with no profile. */}
      <h3 className="mt-5 text-base font-semibold text-slate-900">Which language your emails use</h3>
      <p className="mt-1 text-sm leading-relaxed text-slate-600">
        Every email JomForm sends follows the same two-axis rule — the language of the{" "}
        <strong>recipient</strong>, not of whoever triggered it:
      </p>
      <ul className="mt-3 list-disc space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
        <li>
          <strong>Emails to you and your team</strong> (verify your address, sign-in link, password
          reset, 2FA codes, connecting a social login, a workspace invitation, a feedback reply) use
          the recipient&apos;s <strong>dashboard language</strong>. Set it on <strong>Profile</strong> and
          the same emails arrive in that language.
        </li>
        <li>
          <strong>Emails to your customers</strong> (a payment receipt, the &ldquo;new sale&rdquo; and
          &ldquo;new response&rdquo; alerts) use the <strong>form language</strong> resolved the same way
          the form itself is: <strong>form → workspace → English</strong>. A guest who pays without an
          account still gets the language your form is set to — there is no profile to ask.
        </li>
      </ul>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A never-edited notification rule is translated too — you only get your own wording back once
        you have actually edited that rule, and JomForm never rewrites text you wrote. An unset or
        unsupported language always falls back to <strong>English</strong>, never to Malay. Arabic
        emails are sent right-to-left, and the email declares its own language so a mail client or
        screen reader reads it correctly.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The <strong>subject</strong> and the <strong>body</strong> are judged separately, so editing
        one of them does not pin the other. If you rewrite only the body, your words are kept and the
        untouched subject still follows the language above; if you rewrite only the subject, the
        untouched body still follows it. Only the half you actually typed is treated as yours.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Related:{" "}
        <a href="/docs/placeholders" className="font-medium text-slate-900 underline">
          the placeholder reference
        </a>{" "}
        lists every <span className="font-mono text-xs">{"{{token}}"}</span> you can use in a subject
        or body.
      </p>

      {/* Reserved workspace names (t_586ff0c3): a slug becomes a public host, so
          the refusal is a user-visible constraint and is documented here
          (AGENTS.md documentation-parity rule). */}
      <h2 id="workspace-address" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">Your workspace address, and names that are reserved</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A workspace slug becomes your public address: a workspace called{" "}
        <span className="font-mono text-xs">acme</span> is served at{" "}
        <span className="font-mono text-xs">acme.jomform.com</span>, and your forms live under it. A slug is
        3–40 characters of <span className="font-mono text-xs">a-z</span>,{" "}
        <span className="font-mono text-xs">0-9</span> and dashes, it cannot end in{" "}
        <span className="font-mono text-xs">jomform</span>, and it must not be a name{" "}
        <strong>reserved for JomForm</strong>.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Reserved names are the addresses JomForm owns or has to protect. They fall into three groups:
        infrastructure (<span className="font-mono text-xs">www</span>,{" "}
        <span className="font-mono text-xs">api</span>, <span className="font-mono text-xs">app</span>,{" "}
        <span className="font-mono text-xs">mail</span>, <span className="font-mono text-xs">s3</span>);
        JomForm&apos;s own product and edge addresses that are already routed, such as{" "}
        <span className="font-mono text-xs">docs</span>, <span className="font-mono text-xs">mcp</span>,{" "}
        <span className="font-mono text-xs">templates</span> and{" "}
        <span className="font-mono text-xs">custom</span>; and sensitive names that read as JomForm&apos;s own
        business or support channel, such as <span className="font-mono text-xs">support</span>,{" "}
        <span className="font-mono text-xs">help</span>, <span className="font-mono text-xs">status</span>,{" "}
        <span className="font-mono text-xs">billing</span>, <span className="font-mono text-xs">admin</span>,{" "}
        <span className="font-mono text-xs">account</span>, <span className="font-mono text-xs">login</span>,{" "}
        <span className="font-mono text-xs">payments</span> and{" "}
        <span className="font-mono text-xs">checkout</span>.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The reason is impersonation: an address like{" "}
        <span className="font-mono text-xs">billing.jomform.com</span> would let a buyer be asked for card or
        invoice details under JomForm&apos;s name, so a workspace cannot hold it even while JomForm is not using
        it. If you try one, JomForm tells you the name is reserved (not that your slug was malformed) and asks
        you to pick another. The same rule applies whether you create the workspace in the dashboard, over the
        REST API (<span className="font-mono text-xs">POST /v1/workspaces</span> — the refusal carries the code{" "}
        <span className="font-mono text-xs">reserved_subdomain</span>) or through an AI agent with the{" "}
        <span className="font-mono text-xs">create_workspace</span> MCP tool.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Renaming is covered by the same rule, because a rename can move your ADDRESS just as a create can set
        it. Changing the address of an existing workspace — in Settings → Workspace, over the REST API (
        <span className="font-mono text-xs">PATCH /v1/workspaces/&#123;id&#125;</span>) or through the{" "}
        <span className="font-mono text-xs">update_workspace</span> MCP tool — is refused with{" "}
        <span className="font-mono text-xs">reserved_subdomain</span> if the new slug is a reserved name, and
        the workspace keeps the address it had.
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
      <pre
        tabIndex={0}
        className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"
      >
{`"Apply my Canva design to my JomForm checkout:
1. Upload the logo and hero image from my design (upload_media).
2. Set the form theme to my brand colours, font and layout (set_form_theme).
3. Customise my landing page hero and CTA (set_workspace_landing_page).
4. Add custom CSS for spacing and custom JS to auto-focus the first field (set_form_theme)."`}
      </pre>

      <p className="mt-8 text-sm leading-relaxed text-slate-600">
        JomForm keeps authority over prices and the payment flow — the agent customizes the look and
        feel, never the checkout logic. See the{" "}
        <a
          href="/docs/mcp"
          className="text-[var(--jf-primary)] underline underline-offset-2 hover:underline"
        >
          MCP tool reference
        </a>{" "}
        for the full list of tools.
      </p>
    </div>
  );
}
