import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "JomForm Docs — Overview & features",
  description:
    "Everything JomForm offers: order forms, products, sales, subscriptions, spam protection, email, media, settings, custom domains and landing pages.",
  alternates: { canonical: "https://jomform.com/docs" },
};

const FEATURES: { title: string; body: string }[] = [
  {
    title: "Order forms",
    body: "Build beautiful order forms with a drag-and-drop builder. Add fields (name, email, phone, textarea, number, select, radio, checkbox, date, time, website, address, consent, hidden, list, repeater, image-choice, captcha), attach products, and publish to your own workspace URL or a custom domain. Forms can be payment forms or response-only.",
  },
  {
    title: "Products",
    body: "Sell one-time items and subscriptions. Each product has a name, price (in sen), stock, images, and optional variations (size/colour) with their own price, stock and image. Subscriptions support monthly/yearly billing, trial days, and a fixed or infinite number of charges.",
  },
  {
    title: "Product images on forms",
    body: "Choose how each product photo appears on your form: size (small 52px, medium 96px, large 160px or hidden) and layout (thumbnail-left, the default, or image-top — a menu-card look with a full-width photo above the name, ideal for a restaurant menu). The renderer serves the matching optimised media variant for the chosen size. Set it in Form Settings → General (with a live preview) or over MCP via set_form_theme (product_image_size / product_image_layout).",
  },
  {
    title: "Sales",
    body: "Track every sale: reference code, status (pending/confirmed/failed/refunded), buyer, amount, payment method and date. Confirm bank-transfer sales, capture or release skip-capture authorisations, refund paid sales (full or partial), and export sales to CSV.",
  },
  {
    title: "Partial refunds",
    body: "Refund a paid or confirmed sale in full or in part. JomForm records how much has actually been returned on the sale (refunded_sen), so a partial refund is never mistaken for a full one: the sale keeps its paid/confirmed status with a “Partially refunded” marker and the remaining balance (outstanding_sen) stays refundable until it reaches zero — only then does the sale become refunded. Every read surface reports the same picture: the Sales page (status chip, plus a refunded/balance breakdown in the sale detail), get_sales_summary, analytics (revenue is net of refunds), the CSV export (refunded_rm, outstanding_rm, refund_status) and the MCP tools (list_sales, refund_sale, export_sales_csv). An amount that would push the refunded total past the sale total (refunded_sen + amount_sen > total_sen) or a non-positive amount is rejected before the gateway is ever called, and the guard is applied under a per-sale lock so two refunds racing each other cannot over-refund.",
  },
  {
    title: "Subscriptions",
    body: "Charge customers automatically on a schedule \u2014 monthly or yearly, for as long as the plan runs or for a fixed number of payments. A product can carry a billing period and, optionally, a fixed number of charges so a three-month plan completes itself once the third payment lands. When a payment is late you choose where the next one lands (from the day it succeeds, keep the original dates and collect the missed period, or keep them and skip it), which is what makes a fixed-term class or course bill correctly. If a charge is declined JomForm retries three times over three days, notifies you on the first failure and again when the subscription falls past due, and the customer can update their card through a secure link; you can also retry a past-due subscription yourself. Cancelling never revokes the customer's card token (it can be re-activated later). Every action is available to an AI agent over MCP.",
  },
  {
    title: "Spam protection",
    body: "AI-powered spam review. Spam-flagged submissions and sales are held in a queue for review; a false positive can be un-spammed. A spam-flagged sale that receives a successful payment is auto-un-spammed.",
  },
  {
    title: "Email & notifications",
    body: "Customise the receipt email sent to buyers and per-form notification rules. Email settings are configured per workspace, and any form can override them (subject, body, receipt on/off, merchant notify) with a \"use workspace default\" option — resolution order is form → workspace → built-in default. Use dynamic placeholders like {{ref_code}}, {{amount}}, {{items}}, {{buyer_name}}, {{form_url}}, {{date}} and any {{field_id}}. Track delivery (sent → delivered → opened → clicked).",
  },
  {
    title: "Media library",
    body: "Upload images, code assets (css/js/html/svg/woff2) and PDFs. Images are optimised and served immediately; code assets are scanned before being served.",
  },
  {
    title: "Settings",
    body: "Configure CHIP payment credentials, custom domains, tracking scripts, e-Invoice (LHDN), outbound webhooks, currency, and per-workspace email templates.",
  },
  {
    title: "Custom domains",
    body: "Put your forms on your own domain (e.g. shop.yourname.com). Add the domain, point a CNAME to custom.jomform.com, and verify. With Cloudflare linked, JomForm can auto-create the CNAME for you. Auto-TLS (Let's Encrypt) uses your account email as the ACME contact by default — you can change it per domain in Settings → Domains.",
  },
  {
    title: "Landing pages",
    body: "Customise your workspace storefront with a hero, subtitle, body, image, theme colours, logo and call-to-action buttons that link to your forms.",
  },
  {
    title: "Restaurant menu (QR ordering)",
    body: "Turn a form into a clean restaurant menu: the menu is a per-form setting, so you configure it on the form itself (Form Settings → Restaurant menu) and it is served at that form's own URL. The menu lists only the products the form references through its own product blocks — pick those into named categories with a searchable product picker and a live preview, and highlight a best-seller. Customers scan a QR stand at the table to order and pay online, and the order reaches you with the table number. Configure it over MCP via set_menu_page / get_menu_page (both take a form_id), or generate the QR stand with generate_qr. One workspace can run several menus (two outlets, lunch vs dinner, two brands) simply by using a menu form for each — they are fully independent, and the workspace home page always stays your normal landing page / form listing. The menu checkout always collects the buyer's name and email (CHIP requires both — they can never be removed). Need more per order — a delivery address, phone number, spice level, notes or pickup time? Attach an optional order form to the menu: the form's own fields render in the checkout step with their labels and required flags, are validated on the server, and are stored on the sale. There is no second field editor — you create and edit those fields in the normal form builder, and the menu settings link straight to it. With no form attached the checkout collects name + email only and still works.",
  },
  {
    title: "Menu basket (multi-item ordering)",
    body: "A restaurant menu takes a whole table's order, not one item. Every menu card and row is an add-to-cart button: tapping it adds the item and shows a quantity badge, tapping again adds another. A sticky basket bar appears at the bottom as soon as the basket is non-empty, showing the live item count and running total, and opens the basket panel — where each line has a − / + quantity stepper, a Remove button, and a Clear basket button, with the Subtotal and the table number shown above the checkout. The basket survives category filtering, searching and a page reload (it is stored in the buyer's browser, keyed to that menu form, so two menus in one workspace keep separate baskets) and is cleared only when an order is placed. Checkout collects the diner's name and email — both mandatory, because CHIP needs them — plus any extra fields from the order form you attached, then redirects to the CHIP checkout. The order lands as a normal sale with one line per item at the live product price, the buyer's details, the table number (for a QR order) and the affiliate code if the diner arrived through an affiliate link. The on-screen total is display-only; the server re-reads every price when the order is placed. See the customization guide for the full walk-through.",
  },
  {
    title: "Affiliate marketing",
    body: "Pay promoters for the sales they bring. Create an affiliate with a short code and a commission percentage (Settings → Affiliates, or the MCP tool create_affiliate), and JomForm gives you their referral link — your storefront URL with ?aff=CODE. When a buyer arrives through that link and orders, the code is stored on the sale, so the sale is attributed to that affiliate (menu orders included). Change the commission any time with set_affiliate_commission, and read what an affiliate brought in with get_affiliate_sales (list_affiliates gives you the ids). The same surface is on the REST API: GET/POST /v1/workspaces/{id}/affiliates, PUT /v1/workspaces/{id}/affiliates/{affid}/commission and GET /v1/workspaces/{id}/affiliates/{affid}/sales. See the customization guide for a step-by-step.",
  },
  {
    title: "Templates",
    body: "Start a new form from the built-in template gallery (order forms, booking, donation, contact, survey and more), or build your own: save any form as a reusable template from the builder or the Templates page. Every gallery card shows a category and a one-line description, so a template always reads as more than a title; those two fields live in the form's own config (the category and the first text block), which also means a template you write yourself gets the same treatment — set a category and put a short intro line in a Text block. Your templates are private to your workspace — list, rename, edit and delete them, then clone one into a new draft whenever you need it. Publishing a template into the gallery requires both fields: a template without them is refused with a clear message rather than appearing as a bare title, and a bulk publish skips such a template while still publishing the other forms you selected. AI agents can manage the same templates over MCP (list_own_templates, create_template, update_template, delete_template). See the customization guide for a step-by-step.",
  },
  {
    title: "MCP (AI agents)",
    body: "Connect JomForm to Claude, Cursor and other AI agents via the Model Context Protocol. Agents can build forms, manage products, sales, subscriptions, media and more. See the MCP tool reference.",
  },
  {
    title: "Design → AI agent → MCP customization",
    body: "Design your form or landing page in a tool like Canva, then use an AI agent (e.g. Claude) connected to the JomForm MCP to apply that design and customize it with CSS and JS. See the customization guide.",
  },
  {
    title: "Embed on your site",
    body: "Drop a form into any website with an iframe. The embedded form posts its height to your page (postMessage auto-height) so it grows with the form, with a 3-line merchant listener and a fixed-height fallback. See the embed guide.",
  },
  {
    title: "Trusted devices (2FA)",
    body: "When 2FA is enabled you can tick \"don't ask for a code on this device for 30 days\" at login — that device is then trusted and skips the 2FA code (the password is still required). Profile → Security lists every trusted device (label, IP, first trusted, expiry) with a Revoke button per device, plus \"revoke all / log out everywhere\". Revocation is immediate: the next login on a revoked device asks for a 2FA code again. Changing your password or disabling 2FA also clears all trusted devices.",
  },
];

export default function DocsIndexPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">JomForm Docs</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Welcome to the JomForm knowledge base. Here you'll find an overview of every feature, a
        reference for all dynamic placeholders, and a complete list of MCP tools.
      </p>

      <div className="mt-6 flex flex-wrap gap-3">
        <Link
          href="/docs/customize"
          className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          Design → AI agent → MCP guide
        </Link>
        <Link
          href="/docs/placeholders"
          className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          Placeholders reference
        </Link>
        <Link
          href="/docs/embed"
          className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          Embed on your site
        </Link>
        <Link
          href="/docs/mcp"
          className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          MCP tool reference
        </Link>
      </div>

      <h2 className="mt-10 text-xl font-semibold text-slate-900">Features</h2>
      <div className="mt-4 space-y-6">
        {FEATURES.map((f) => (
          <section key={f.title}>
            <h3 className="text-base font-semibold text-slate-900">{f.title}</h3>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{f.body}</p>
          </section>
        ))}
      </div>

      <h2 className="mt-10 text-xl font-semibold text-slate-900">Getting started</h2>
      <ol className="mt-4 list-decimal space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
        <li>Create a free account — your first form is ready in about 5 minutes.</li>
        <li>Build a form with the drag-and-drop builder and attach products.</li>
        <li>Add your CHIP payment credentials in Settings → CHIP.</li>
        <li>Publish the form — it goes live at your workspace URL or a custom domain.</li>
        <li>Track sales, subscriptions and email delivery from the dashboard.</li>
      </ol>

      <h2 className="mt-10 text-xl font-semibold text-slate-900">FAQ</h2>
      <div className="mt-4 space-y-4">
        <section>
          <h3 className="text-sm font-semibold text-slate-900">How do I connect an AI agent?</h3>
          <p className="mt-1 text-sm leading-relaxed text-slate-600">
            Point any MCP client at the server URL <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code>.
            It registers itself via OAuth 2.1 — no API keys needed. See the MCP tool reference for
            what agents can do.
          </p>
        </section>
        <section>
          <h3 className="text-sm font-semibold text-slate-900">What are the placeholders in emails?</h3>
          <p className="mt-1 text-sm leading-relaxed text-slate-600">
            Placeholders like <code className="rounded bg-slate-100 px-1">{"{{ref_code}}"}</code> and{" "}
            <code className="rounded bg-slate-100 px-1">{"{{amount}}"}</code> are replaced with real
            values when an email is sent. See the placeholders reference for the full list.
          </p>
        </section>
        <section>
          <h3 className="text-sm font-semibold text-slate-900">Can I use my own domain?</h3>
          <p className="mt-1 text-sm leading-relaxed text-slate-600">
            Yes. Add your domain in Settings → Domains, point a CNAME to custom.jomform.com, and
            verify. Your forms then go live on your domain automatically.
          </p>
        </section>
      </div>
    </div>
  );
}
