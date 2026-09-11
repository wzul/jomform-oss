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
    body: "Use set_menu_page to turn the workspace root into a clean restaurant menu: enabled, categories ([{name, product_ids}]), and best_seller_ids (a list of product IDs shown as the Best Seller section). Pair it with generate_qr to print a per-table QR stand — the ?table=N it encodes is read by the menu page, shown in the header (\"Table 8\") and carried onto the order as the table number. The menu checks out directly from the page (no form in the path): the buyer's name and email are required for CHIP, and the server charges the product's live price. If you want to collect extra info per order, set order_form_slug to a published form — the form's own fields (its builder blocks, or its registered_fields when set) render in the menu checkout step with their labels and required flags, are validated on the server, and are stored on the sale; there is no second field editor, so you create and edit those fields in the normal form builder. Name + email stay mandatory regardless of the attached form, and with no form attached the checkout collects name + email only. The menu is server-rendered and mobile-first: the header shows the table number, cafe name and location with an info panel; a category filter and search let a diner narrow the menu; best-sellers render as two-column square-image cards, and each category as list rows (text left, thumbnail right) with a separator between rows. Items without a photo render cleanly with no empty image slot. All labels follow the workspace default language (en/ms/zh/ta/ar).",
  },
  {
    title: "6. Customize with CSS and JS",
    body: "For full control, set custom_css (styling/layout) and custom_js (form behaviour) on a form via set_form_theme. Custom JS is owner-gated and scanned for malicious code before it goes live, so it is safe to run on every buyer's page. JomForm keeps authority over prices and payment.",
  },
];

const TOOLS: { name: string; purpose: string }[] = [
  { name: "set_form_theme", purpose: "Apply brand colours, font, logo, layout, spacing, product_image_size, product_image_layout, custom_css and custom_js to a form." },
  { name: "set_workspace_landing_page", purpose: "Customise the workspace storefront: hero, subtitle, body, image, theme colours, logo and CTAs." },
  { name: "upload_media", purpose: "Upload images and code assets (css/js/html/svg/woff2) for the landing page and forms." },
  { name: "upsert_form", purpose: "Create or update a form, including its optional theme." },
  { name: "generate_form", purpose: "Draft a validated form config (blocks + styling) from a natural-language prompt." },
  { name: "set_form_confirmations", purpose: "Set the confirmation message buyers see after submitting." },
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
