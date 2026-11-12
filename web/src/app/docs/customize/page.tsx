import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Design → AI agent → MCP customization — JomForm Docs",
  description:
    "Design your form or landing page in a tool like Canva, then use an AI agent (e.g. Claude) connected to the JomForm MCP to apply that design and customize it with CSS and JS.",
  path: "/docs/customize",
});

// SHORT ON PURPOSE. Every tool name, field name and route survives, because
// /docs is what a developer greps. Every section id below is a contract with
// /docs, the docs sidebar and the dashboard's inline "?" links (#598, #447).
// The page was split where one idea needed more than eight blocks, and every
// new id is additive: no id that existed before was renamed or dropped.
const SECTIONS: Section[] = [
  {
    id: "the-flow",
    title: "The flow",
    icon: "🎨",
    body: [
      "Design the interface in a tool like **Canva**, then let an AI agent apply it over the JomForm MCP.",
      {
        steps: [
          "Design the form or landing page: logo, hero image, brand colours and a font.",
          "Connect an MCP client such as Claude or Cursor to https://mcp.jomform.com/mcp. It registers itself via OAuth 2.1, with no API keys.",
          "Upload the assets with `upload_media`.",
          "Apply the theme and the landing page with `set_form_theme` and `set_workspace_landing_page`.",
          "Set `product_image_size` and `product_image_layout` for the product photo.",
          "Add `custom_css` and `custom_js` for anything the theme controls do not cover.",
        ],
      },
      { tip: "JomForm keeps authority over prices and the payment flow. You design the look, and the agent applies it." },
    ],
  },
  {
    id: "media",
    title: "Media and custom code",
    icon: "🧰",
    body: [
      "Upload images and code assets (css, js, html, svg and woff2) with `upload_media`.",
      "Images are optimised and served straight away. Code assets are scanned before they are served.",
      "An **SVG** must be a well-formed XML document: a browser renders it as XML, so text before the `<svg>` tag is refused, not stored as a broken image.",
      "For full control, set `custom_css` for styling and layout and `custom_js` for form behaviour, both through `set_form_theme`.",
      "Custom JS is **owner-gated** and scanned for malicious code before it goes live. In a custom HTML form it runs on the outer page, and the HTML body stays inert.",
      "A blocked script is not saved: the save is refused and names the **Custom JS** field.",
      "The message says which kind of pattern caused it: code execution, obfuscation or beacon code, or an external URL. Remove that part and save again.",
    ],
  },
  {
    id: "menu",
    title: "The restaurant menu (QR ordering)",
    icon: "🍽️",
    body: [
      "Turn a form into a clean **restaurant menu**. The menu is a per-form setting, so it takes a `form_id`.",
      "Add Product blocks for the items it sells. The menu lists **only the products the form references**, so unrelated account products never appear.",
      "Set `enabled`, `categories` ([{name, product_ids}]) and `best_seller_ids`, the list shown as the Best Seller section.",
      "The menu is served at that form's own URL. The workspace home page stays your normal landing page and form listing.",
      "Use a **separate menu form** per outlet or menu, such as lunch and dinner or two brands. They coexist independently in one workspace.",
      "Configure it over MCP with `set_menu_page` and `get_menu_page`. Both take a `form_id`.",
      "Pair it with `generate_qr` to print a per-table QR stand. The `?table=N` it encodes is read by the menu page and shown in the header as \"Table 8\".",
      { tip: "The table number travels with the order, so the order reaches you with the **table number**." },
    ],
  },
  {
    id: "menu-checkout",
    title: "Menu checkout and the order form",
    icon: "🧾",
    body: [
      "The menu checks out through the form's own order route. The buyer's name and email are required for CHIP, and the server charges the product's live price.",
      "To collect more per order, set `order_form_slug` to a published **order form**.",
      "Its own fields render in the checkout step with their labels and required flags. The fields are the form's builder blocks, or its `registered_fields` when set.",
      "The server validates those fields and stores them on the sale. There is no second field editor: you create and edit the fields in the normal form builder.",
      "Name and email stay mandatory regardless of the attached form. With no form attached, the checkout collects name and email only.",
      "The menu is server-rendered and mobile-first. The header shows the table number, cafe name and location with an info panel.",
      "A category filter and search narrow the menu. Best-sellers render as two-column square-image cards, and each category as list rows, text left and thumbnail right, with a separator between rows.",
      "Items without a photo render cleanly with no empty image slot. All labels follow the workspace default language (en, ms, zh, ta, ar).",
    ],
  },
  {
    id: "menu-basket",
    title: "The menu basket (multi-item ordering)",
    icon: "🧺",
    body: [
      "A restaurant menu is not a one-item form. The page carries a full basket, so a diner can order a whole table's food in one payment.",
      "Nothing to configure: the basket is part of **every menu form**.",
      "Every menu card and list row is the **add-to-cart** button. A tap adds one of that item, with no page load and no navigation to a form.",
      "A small + badge shows how many of that item are in the basket. Tapping again adds another, so a diner can build a multi-item order.",
      "The sticky basket bar appears across the bottom as soon as the basket is not empty. It shows the live item count and running total, and opens the basket panel.",
      {
        list: [
          "Each line has a minus and plus **quantity stepper**, and taking a line to zero removes it.",
          "Each line has its own **Remove** button, and the panel has a **Clear basket** button for the whole order.",
          "The panel shows the Subtotal and, when a table number is present, the table it applies to.",
        ],
      },
      "The total on screen is display-only. The server re-reads every price from the products table when the order is placed, so the buyer is charged the live price.",
      "The basket survives filtering, searching and a **reload**, and it is still there after the page is closed and opened again.",
    ],
  },
  {
    id: "menu-basket-checkout",
    title: "Basket checkout, stock and the sale",
    icon: "💳",
    body: [
      "The basket is stored in the buyer's own browser, keyed to the menu form, so two menus keep **separate baskets**. It clears once an order is placed.",
      "Checkout is part of the same panel, and name and email are both mandatory because CHIP needs them to create a purchase.",
      "Submitting posts the basket to the form's own order route and redirects to the CHIP page. If it fails, an error shows and the basket is kept.",
      "Stock is re-read live. A dish that sells out cannot be added, and the message names it as sold out, whether it has variations or not.",
      "A basket line whose dish sold out is dropped when the page opens again, with a notice naming the dish.",
      "If the live check cannot answer, the page keeps the state it was served with, so the menu still works offline.",
      "The order becomes a normal sale: one line per item (product, quantity, live price), the buyer's name and email, the total and the extra fields you asked for.",
      "A QR order carries its **table number**, and an affiliate link (`?aff=CODE`) stores its code on the sale for commission.",
    ],
  },
  {
    id: "product-images",
    title: "Product images on forms",
    icon: "🖼️",
    body: [
      "Choose how each product photo appears on your form: **size** and **layout**.",
      {
        list: [
          "Size: small 52px, medium 96px, large 160px, or hidden.",
          "Layout: thumbnail-left (the default) or **image-top**, a menu-card look with a full-width photo above the name.",
        ],
      },
      "The renderer serves the matching optimised media variant for the chosen size.",
      "Set it in Form Settings → General, with a live preview, or over MCP with `set_form_theme` and `product_image_size` / `product_image_layout`.",
    ],
  },
  {
    id: "brand-colour",
    title: "Your brand colour on the public form",
    icon: "🎯",
    body: [
      "The colour you pick under Form Settings → General (**Theme colour**) paints the public form's own chrome.",
      {
        list: [
          "The submit button, with its hover and pressed states.",
          "The checkbox and radio accent, and the product price text.",
          "The footer and confirmation links.",
        ],
      },
      "It does not override anything you set in `custom_css`, and an embedded image or video keeps its own colours.",
      "The button label colour is chosen for you: a dark brand colour gets white text and a light one gets dark text.",
      "The hover and pressed shades are derived from your colour, not from a fixed indigo, so a green or amber button reads as one button in three states.",
      "Each state is measured against the WCAG AA 4.5:1 floor before it is used.",
      "Set it in Form Settings → General and the button previews it at once. Over MCP the field is `theme.primary` through `set_form_theme`.",
      { note: "Leave it unset and the form uses the default indigo, the same colour the dashboard uses." },
    ],
  },
  {
    id: "builder",
    title: "Building an order form",
    icon: "🧱",
    body: [
      "Build order forms with a **drag-and-drop builder**.",
      "Add fields (name, email, phone, textarea, text, number, select, radio, checkbox, date, time, website, address, consent, hidden, list, repeater, image-choice, captcha).",
      "Attach **products** and publish to your workspace URL or a **custom domain**.",
      "A form is a **payment form** or a **response-only** one. The mode decides what it collects and what its entries are called, and it is set in the builder.",
      "An agent can build the same form: `upsert_form` creates or updates it, and `register_form_fields` sets its input fields under a strict contract.",
      "`publish_form` then takes it live.",
    ],
  },
  {
    id: "publishing",
    title: "Publishing a form",
    icon: "🚀",
    body: [
      "Publishing puts your form on the internet at its own URL, the link a buyer opens or the address you embed.",
      "Press **Publish** in the builder and JomForm confirms it at once: a panel names the form as live, shows the public URL, and offers **Open form** and **Copy link**.",
      "If the URL cannot be resolved yet, for example because the workspace subdomain is not set up, the panel says so rather than showing a dead link.",
      "A **response-mode** form may always go live: collecting answers is its whole job, and having no products is normal.",
      "A **payment form** needs **something to sell**. Publishing one with no products is refused once with a clear message, because such a form can never take money.",
      "You can confirm and publish it anyway, but its page then tells buyers it is not accepting orders instead of showing a checkout that cannot start.",
      "It starts selling once you add a product: a product block, a priced option, a shipping choice or an enabled restaurant menu.",
      "A form built without blocks sells its product snapshot. Adding a product at any time removes the question.",
    ],
  },
  {
    id: "publishing-no-product",
    title: "When a form cannot take orders",
    icon: "⚠️",
    body: [
      "A **product block with no product selected** counts as nothing to sell, even when your catalogue has products.",
      "The builder says so beside the form, quotes the sentence buyers see and links straight to that block.",
      "A block pointing at a permanently deleted product gets the same signal, and the form list labels the row **Not accepting orders** instead of **LIVE**.",
    ],
  },
  {
    id: "publishing-paths",
    title: "Every path that publishes",
    icon: "🔁",
    body: [
      "The same rule covers every way a form goes live: the **Publish** button, the status control in **Form Settings**, and publishing several forms from the **Forms** list.",
      "It covers AI agents too: `publish_form` and `bulk_forms` both accept `acknowledge`, so an agent is told the same thing you are.",
      "A form that is already live is never asked about again.",
    ],
  },
  {
    id: "share-preview",
    title: "The link preview card",
    icon: "🔗",
    body: [
      "Every live form has its own share card at `/og/your-form-slug.png`: its title, workspace and address, drawn as a 1200×630 image.",
      "That is what a chat app or social site shows when the link is pasted.",
      "A **draft** is not served at all. Its card answers 404, exactly as its page does, so its title never becomes public through the preview URL.",
      "A **private** form's card is served only to a browser that has already entered the password, and it is never handed to a shared cache.",
      "An **unlisted** form is live, so its card is served like a published one's.",
      "The card is dropped from the cache the moment a form stops being live: set back to draft, switched to private, or moved to the trash.",
      "Cards are cached for five minutes at a time rather than a day, so a deployment that cannot clear that cache stops showing a stale one within minutes.",
    ],
  },
  {
    id: "qr-stand",
    title: "The printable QR stand",
    icon: "📱",
    body: [
      "The QR stand follows the page rule too. It prints the form's title and a QR of its public URL, so a **draft**'s stand answers 404.",
      "A **private** form's stand is served only to a browser that has already entered the password.",
      "Printing is not taken away from you: **Print stand** in Form Settings opens your own signed-in copy of the same stand, which works even on a draft.",
      "The QR image behind the stand (`/qr/your-form-slug.png`) follows the same rule and the same cache promise.",
      "It encodes your form's address, which tells anyone who fetches it that a form exists at that slug, so a form that is not live answers 404 there.",
      "A private form's image is served only to a browser that has entered the password, and the image leaves the cache when a form stops being live.",
      "It is cached five minutes at a time rather than a day, so a stale one stops within minutes even without a cache purge.",
    ],
  },
  {
    id: "form-url-slug",
    title: "Form URLs and slugs (rename a form)",
    icon: "✏️",
    body: [
      "Every form is served at the slug you gave it: `your-workspace.jomform.com/your-form-slug`, or the same path on a verified custom domain.",
      "The slug is editable. Change it in **Form Settings → General** and the form moves to the new URL immediately.",
      "A slug is **1–40 characters** of `a-z`, `0-9` and dashes. Uppercase and spaces are normalised.",
      "It must be free within your workspace. A rename onto a slug another of your forms already serves is **refused with an error** rather than overwriting either form's URL.",
      "A name JomForm keeps for itself is refused as well, on both creating and renaming a form: `login`, `api`, `docs`, and the paths it already serves, `privacy`, `terms` and `refund`.",
      "The refusal names the rule, so pick a different slug. Nothing is stored when a slug is refused.",
    ],
  },
  {
    id: "slug-redirect",
    title: "Renaming keeps the old URL working",
    icon: "🔀",
    body: [
      "Renaming never orphans the URL you already handed out: the old slug keeps working through a **temporary redirect (HTTP 301)** to the new one.",
      "A printed QR stand, an embedded iframe, a menu's attached order form and a link already shared in a campaign all keep resolving.",
      "The redirect is temporary, not a permanent alias. If another form later claims the freed slug, that **live form** wins and the old URL serves it.",
      "Slugs cannot be renamed on templates, because a template has no public URL.",
      "AI agents do the same thing with the `slug` parameter on `upsert_form`, which returns the slug the form now owns.",
    ],
  },
  {
    id: "domains",
    title: "Custom domains",
    icon: "🌐",
    body: [
      "Put your forms on your own domain, for example `shop.yourname.com`.",
      "Add the domain, point a CNAME to `custom.jomform.com`, and verify.",
      "With Cloudflare linked, JomForm can **auto-create the CNAME** for you.",
      "Auto-TLS (**Let's Encrypt**) uses your account email as the ACME contact by default. You can change it per domain in Settings → Domains.",
    ],
  },
  {
    id: "affiliate-marketing",
    title: "Affiliate marketing (referral links and commission)",
    icon: "💸",
    body: [
      "An **affiliate** is a promoter you pay for sales they bring. You give each one a short **code**, and their link is your storefront URL with `?aff=CODE` appended.",
      "For example, `https://your-workspace.jomform.com/?aff=ALI`.",
      "When a buyer arrives through that link and completes an order, the code is stored on the sale, so the sale is attributed to that affiliate.",
      "You can then see what they earned. Attribution works on menu orders too, because the code is carried to the basket checkout.",
      "The form or menu page reads `?aff=CODE` and carries it into the order it submits.",
      "There is no cookie and no redirect chain: the code on the URL the buyer used is what attributes the sale.",
      { note: "A buyer with no `?aff=` produces an unattributed sale." },
    ],
  },
  {
    id: "affiliate-create",
    title: "Create an affiliate link",
    icon: "🪪",
    body: [
      "In the dashboard, open **Settings → Affiliates** and add a code, an optional name and a commission percentage.",
      "The new affiliate appears in the list with its code, its commission and its ready-to-share referral URL.",
      "Over MCP, call `create_affiliate` with the `workspace_id`, a `code`, an optional `name` and `email`, and `commission_pct` (10 = 10%).",
      "It returns the affiliate, including the `referral_url`.",
      "A code must be unique within the workspace, and creating or changing an affiliate needs the `manage` or `admin` role.",
    ],
  },
  {
    id: "affiliate-commission",
    title: "Set the commission",
    icon: "📈",
    body: [
      "**Commission** is a percentage stored per affiliate. Change it with `set_affiliate_commission`, which takes `affiliate_id` and the new `commission_pct`.",
      "The rate used is the one stored on the affiliate, so a change applies to sales attributed from then on.",
      "It is not written back onto sales that already happened.",
    ],
  },
  {
    id: "affiliate-sales",
    title: "Read an affiliate's sales",
    icon: "📋",
    body: [
      "Use `list_affiliates` to get the affiliate ids, then `get_affiliate_sales` with the `affiliate_id`. It is read-only.",
      "Each row gives the sale reference, status, total, currency and date.",
      "**Attribution** is recorded on every sale that came in with the code. The commission percentage is the affiliate's current rate.",
      { note: "The figures are returned, not a pre-computed payout ledger." },
      { heading: "REST endpoints" },
      "The same surface is available over the REST API, authenticated as the workspace:",
      {
        table: {
          label: "Affiliate REST endpoints",
          head: ["Method and path", "Purpose"],
          rows: [
            ["GET /v1/workspaces/{id}/affiliates", "List the workspace's affiliates."],
            ["POST /v1/workspaces/{id}/affiliates", "Create an affiliate (`code`, `name`, `email`, `commission_pct`)."],
            ["PUT /v1/workspaces/{id}/affiliates/{affid}/commission", "Set the affiliate's commission percentage."],
            ["GET /v1/workspaces/{id}/affiliates/{affid}/sales", "Read the sales attributed to that affiliate."],
          ],
        },
      },
    ],
  },
  {
    id: "templates",
    title: "Templates (save a form and reuse it)",
    icon: "🗂️",
    body: [
      "A **template** is a saved form you can start new forms from.",
      "It is private to your workspace: it never appears in another merchant's gallery, and it has no draft or published state. Build one either way:",
      {
        list: [
          "**Save form as template** in the builder, or on the Templates page after picking a form. Its fields and design are copied into a template.",
          "**New template** on the Templates page, an empty template ready to fill in.",
        ],
      },
      "The built-in **gallery** covers order forms, booking, donation, contact, survey and more. Cloning one copies it into your workspace as a new draft.",
      "The **Templates** page lists your templates with **Edit**, **Use template**, **Rename** and **Delete**.",
      "**Edit** opens the template in the same builder you use for forms, so you change its fields, blocks and design, then **Save**.",
      "The builder knows it is editing a template: there is no slug, no Publish and no Form Settings, because a template is not published and has no public URL.",
      "**Editing a template does not change forms already created from it.** Cloning copies the fields and design at that moment, so the two are independent afterwards.",
    ],
  },
  {
    id: "template-gallery-card",
    title: "A template card, and editing by MCP or REST",
    icon: "🏷️",
    body: [
      "Over MCP the edit is `update_template`, with `template_id` from `list_own_templates`. The REST equivalent is `PUT /v1/rest/templates/{id}` with `title`, `config` or `theme`; an omitted field is unchanged.",
      { heading: "A template never lands in another merchant's gallery by accident" },
      "Your templates and the public gallery are **two separate lists**, and template mode keeps them that way: saving a template writes only its fields and design, never a publish.",
      "Publishing into the shared gallery is a deliberate, separate act that requires a category and a short description line.",
      "Every gallery card is rendered from two fields of the template's own config: the **category** is `config.category`.",
      "The **description**, the card's subtitle, is the template's **intro line**, the value of its **first Section block**.",
      "A Text block also counts, because older templates kept their intro there. A Text block is a single-line answer field now, so its value is a field label.",
      "A new template should **put a short intro line in a section block**.",
    ],
  },
  {
    id: "template-publish",
    title: "Publishing a template into the gallery",
    icon: "⚠️",
    body: [
      "Publishing is therefore checked. A template missing either field is **refused with a clear message**.",
      "The same holds for one carrying only spaces where either field belongs, so it never appears as a **bare title**.",
      "The gallery trims both the category and the intro line before rendering, so an older template shows its category without surrounding spaces.",
      "It shows no category chip at all rather than a chip holding only spaces.",
      "Publishing several forms at once behaves differently on purpose: a **bulk publish skips such a template** while still publishing the other forms you selected.",
      "That way one unfinished template cannot block a whole batch. The skipped ids are reported back, by the REST bulk endpoint and by the `bulk_forms` tool.",
      "Both return those ids with a reason and count only the forms that really changed, so the skip is never silent.",
    ],
  },
  {
    id: "language",
    title: "Language: dashboard vs your forms",
    icon: "🗣️",
    body: [
      "JomForm has **two separate language settings**, and they are deliberately not the same one.",
      "The supported languages are [[en|English]], [[ms|Bahasa Melayu]], [[zh|中文]], [[ta|தமிழ்]] and [[ar|العربية]]; Arabic also switches the dashboard to right-to-left.",
      "**Dashboard language** is the one you see when signed in. It is a **per-user** setting.",
      "Change it on **Profile** (Dashboard language), reached by clicking your **account block**, your name and email at the bottom of the sidebar or the menu on a phone.",
      "It follows you into any browser you sign in from, on any machine. It never changes what a buyer sees.",
      "The knowledge base and the legal pages are the exception: they are written in English only, so a guide or policy stays English even when your dashboard is in Arabic.",
      "Only the labels on the docs index are translated.",
      { note: "Until you pick one, the dashboard uses the language last used in that browser, and English if there is none." },
    ],
  },
  {
    id: "language-forms",
    title: "Choosing your customers' language",
    icon: "🌍",
    body: [
      "**Form language** is the language your customers see on a public form, the menu, the thank-you page and the password gate.",
      "It is set per form or per workspace, never per browser and never from your dashboard choice.",
      "Resolution is **form → workspace → English**: a per-form language wins when set. Otherwise the form uses its workspace's **Default form language** (Settings → Storefront), and otherwise English.",
      "Set the workspace default once and every form you publish inherits it, so a Malay storefront does not need the language set on each form.",
      "Over MCP the workspace default is `set_workspace_settings` with `default_lang`, and the per-form language is `config.lang` on `upsert_form`. An unsupported code is rejected.",
      "The translation covers the buyer-facing chrome: field labels, the total, the submit button, error messages, the menu and the footer links (Privacy, Terms, Refund).",
      "Your own field labels, product names, prices and descriptions are shown exactly as you typed them. JomForm does not machine-translate your content.",
    ],
  },
  {
    id: "language-emails",
    title: "Which language your emails use",
    icon: "✉️",
    body: [
      "Every email JomForm sends follows the same two-axis rule: the language of the **recipient**, not of whoever triggered it.",
      {
        list: [
          "**Emails to you and your team** (verify your address, sign-in link, password reset, 2FA codes, connecting a social login, a workspace invitation, a feedback reply) use the recipient's **dashboard language**.",
          "**Emails to your customers** (a payment receipt, the new sale and new response alerts) use the **form language**, resolved form → workspace → English.",
        ],
      },
      "A guest who pays without an account still gets the language your form is set to, because there is no profile to ask.",
      "A never-edited notification rule is translated too. You get your own wording back only once you edit that rule.",
      "An unset or unsupported language falls back to **English**, never to Malay. Arabic emails are right-to-left and declare their own language.",
      "The **subject** and the **body** are judged separately, so editing one does not pin the other. Only the half you typed is treated as yours.",
    ],
  },
  {
    id: "language-email-defaults",
    title: "Empty receipt and notification fields",
    icon: "📝",
    body: [
      "A new workspace's receipt subject and body start **empty**.",
      "An empty field is not a missing subject: it means JomForm's own receipt, written in the language your form is set to.",
      "There is nothing for you to fill in, which is why the fields show a grey example instead of text to delete.",
      "A form's default **notification rule** works the same way.",
      "Related: [the placeholder reference](/docs/placeholders) lists every `{{token}}` you can use in a subject or body.",
    ],
  },
  {
    id: "workspace-address",
    title: "Your workspace address, and names that are reserved",
    icon: "🏠",
    body: [
      "A workspace slug becomes your public address: a workspace called `acme` is served at `acme.jomform.com`, and your forms live under it.",
      "A slug is 3–40 characters of `a-z`, `0-9` and dashes. It cannot end in `jomform`, and it must not be a name **reserved for JomForm**.",
      "Reserved names are the addresses JomForm owns or has to protect. They fall into three groups.",
      {
        list: [
          "Infrastructure: `www`, `api`, `app`, `mail`, `s3`.",
          "Product and edge addresses already routed: `docs`, `mcp`, `templates`, `custom`.",
          "Sensitive names that read as JomForm's own business or support channel: `support`, `help`, `status`, `billing`, `admin`, `account`, `login`, `payments`, `checkout`.",
        ],
      },
      "The reason is impersonation: `billing.jomform.com` would let a buyer be asked for card or invoice details under JomForm's name, so a workspace cannot hold it.",
      "If you try one, JomForm says the name is reserved, not that your slug was malformed, and asks you to pick another.",
      "The same rule applies in the dashboard, over REST (`POST /v1/workspaces`, code `reserved_subdomain`) and through the `create_workspace` MCP tool.",
      "Renaming is covered too. Changing an address in Settings → Workspace, over REST (`PATCH /v1/workspaces/{id}`) or with `update_workspace`, is refused with `reserved_subdomain`. The workspace keeps the address it had.",
    ],
  },
  {
    id: "image-urls",
    title: "Image URLs must be https://",
    icon: "🔒",
    body: [
      "Every image or URL you set must be an **absolute** `https://` URL.",
      "That covers the form logo (`set_form_theme`), the landing-page hero and logo (`set_workspace_landing_page` or Settings → Storefront) and a form's image block.",
      "Product images (`create_product` and `update_product`, or the Products editor) follow the same rule.",
      "Uploaded media already qualifies: `upload_media` returns an absolute https URL on your own media host, which is accepted everywhere.",
      "Any other scheme (`data:`, `http:`, `javascript:`, or a relative path) is rejected when you save it, with a clear message, on both the dashboard and the MCP.",
      "This is deliberate. The page renders the value into an `<img src>` for your buyers, and a browser will not load an unsafe scheme.",
      "Rejecting it at entry means you see the problem immediately instead of getting a silently missing image on the live page.",
      { note: "Leaving an image field empty is always allowed. It means no image, or when updating, keep the current one." },
    ],
  },
  {
    id: "mcp-tools",
    title: "MCP tools used",
    icon: "🛠️",
    body: [
      "These are the **MCP tools** this flow uses. The full list is in [the MCP tool reference](/docs/mcp).",
      {
        table: {
          label: "MCP tools used",
          head: ["Tool", "Purpose"],
          rows: [
            ["set_form_theme", "Apply brand colours, font, logo, layout, spacing, product_image_size, product_image_layout, custom_css and custom_js to a form."],
            ["set_workspace_landing_page", "Customise the workspace storefront: hero, subtitle, body, image, theme colours, logo and CTAs."],
            ["upload_media", "Upload images and code assets (css, js, html, svg, woff2) for the landing page and forms."],
            ["upsert_form", "Create or update a form, including its optional theme."],
            ["generate_form", "Draft a validated form config (blocks and styling) from a natural-language prompt."],
            ["set_form_confirmations", "Set the confirmation message buyers see after submitting."],
            ["set_menu_page", "Turn a form into a restaurant menu (per-form: it takes a form_id) and set its categories, best-sellers and optional order form."],
            ["get_menu_page", "Read a form's restaurant-menu configuration."],
            ["generate_qr", "Generate a QR code (optionally per table) for a published form."],
            ["create_affiliate", "Create an affiliate link (code, name and commission %) for a workspace. Returns the referral URL."],
            ["list_affiliates", "List a workspace's affiliates with their codes, commission and referral URLs."],
            ["get_affiliate_sales", "Read the sales attributed to one affiliate (read-only)."],
            ["set_affiliate_commission", "Change an affiliate's commission rate."],
          ],
        },
      },
    ],
  },
  {
    id: "example-prompt",
    title: "Example prompt for your agent",
    icon: "🤖",
    body: [
      { code: "\"Apply my Canva design to my JomForm checkout:", caption: "Paste this into your agent." },
      { code: "1. Upload the logo and hero image from my design (upload_media)." },
      { code: "2. Set the form theme to my brand colours, font and layout (set_form_theme)." },
      { code: "3. Customise my landing page hero and CTA (set_workspace_landing_page)." },
      { code: "4. Add custom CSS for spacing and custom JS to auto-focus the first field (set_form_theme).\"" },
      "JomForm keeps **authority over prices and the payment flow**: the agent customises the look and feel, never the checkout logic.",
      "See [the MCP tool reference](/docs/mcp) for the full list of tools.",
    ],
  },
];

export default function CustomizeDocsPage() {
  return (
    <DocsPage
      title="Design → AI agent → MCP customization"
      lead="Design your form or landing page in a tool like Canva, then let an AI agent on the JomForm MCP apply it."
      sections={SECTIONS}
    />
  );
}
