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
    title: "Sales",
    body: "Track every sale: reference code, status (pending/confirmed/failed/refunded), buyer, amount, payment method and date. Confirm bank-transfer sales, capture or release skip-capture authorisations, refund paid sales (full or partial), and export sales to CSV.",
  },
  {
    title: "Subscriptions",
    body: "Manage recurring customer subscriptions. Cancel a subscription without losing the customer's card token (it can be re-activated later), and send a secure link to update a card.",
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
    title: "Templates",
    body: "Start a new form from the built-in template gallery (order forms, booking, donation, contact, survey and more), or build your own: save any form as a reusable template from the builder or the Templates page. Your templates are private to your workspace — list, rename, edit and delete them, then clone one into a new draft whenever you need it. AI agents can manage the same templates over MCP (list_own_templates, create_template, update_template, delete_template).",
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
