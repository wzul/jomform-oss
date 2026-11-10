// GENERATED FILE — do not edit. Run `npm run guides:sync` (web/scripts/guides.mjs).
//
// Source of truth: docs/guides/<slug>/README.md + its screenshots. This module exists
// because the app is statically rendered and the Docker image does not carry the repo-root
// docs/ tree, so the guide text and its screenshot manifest are materialised at build-prep
// time. src/lib/guidesContent.test.ts fails when this file drifts from docs/guides.
//
// The screenshots themselves are copied to web/public/guides/<slug>/ (same script) — the
// URLs the rendered <img> tags use.

export type GuideShot = {
  /** Bare filename, exactly as the README writes it: ![](01-step.png). */
  file: string;
  /** Intrinsic PNG size, so the <img> is emitted with width/height and cannot shift layout. */
  width: number;
  height: number;
};

export type Guide = {
  slug: string;
  /** The README's `# …` line. The page renders it as its <h1>; it is removed from body. */
  title: string;
  /** One plain-text line for the /docs/guides index. */
  summary: string;
  /** The README with the title line removed, rendered through <Markdown mode="docs">. */
  body: string;
  shots: GuideShot[];
};

export const GUIDES: Guide[] = [
  {
    "slug": "affiliate-programme",
    "title": "Affiliate programme: setup guide",
    "summary": "Hand someone a link, and every paid sale that arrives through it pays them a commission.",
    "body": "**What this does:** an affiliate is a unique code in the workspace. A sale that arrives with that\ncode attached is attributed to the affiliate, and the commission is computed on the **paid** amount.\n\n## 1. Create the affiliate\n\n![Settings → Affiliates with Code SITI, Name Siti Zubaidah, Commission 10 and the Add affiliate button boxed](01-create-affiliate.png)\n\nSettings → **Affiliates**: fill **Code** (`SITI` here), **Name**, **Commission %** and press\n**Add affiliate**. A code is unique inside the workspace, so a second `SITI` is refused.\n\n## 2. Hand over the link\n\n![The affiliate row with its referral link boxed](02-referral-link.png)\n\nThe row carries that affiliate's link, `https://<workspace>.jomform.com/?aff=SITI`, and anything a\nvisitor buys after opening it is attributed to SITI at the rate on the row.\n\n## Notes\n\n- **Only paid sales count.** An unpaid checkout, a refused card or a form response with no product\n  contributes nothing to an affiliate.\n- **The sale records the code, not the money.** Each sale carries the code that brought it in, and\n  cancelling a card never revokes anything on the affiliate side.\n- **One code per promotion, not per person.** Two channels (a WhatsApp group and an Instagram bio)\n  take two codes, so each gets its own commission line.\n- **The same three operations exist as MCP tools** (`create_affiliate`, `list_affiliates`,\n  `set_affiliate_commission`), and they return the same `referral_url` the tab shows.\n",
    "shots": [
      {
        "file": "01-create-affiliate.png",
        "width": 1920,
        "height": 824
      },
      {
        "file": "02-referral-link.png",
        "width": 1536,
        "height": 900
      }
    ]
  },
  {
    "slug": "club-membership",
    "title": "Club membership: setup guide",
    "summary": "Sell a yearly or monthly membership that renews itself, in five steps.",
    "body": "**What this does:** a membership is a single **Subscription** product sold from any payment form;\nthe member's card is saved at checkout and charged again every period after that.\n\n## 1. Create the plan as a Subscription product\n\n![Products page with Keahlian Kelab 2026 set to Subscription, price 240.00, and the Billing period boxed on Yearly](01-plan-product.png)\n\nProducts → name it (*Keahlian Kelab 2026*), set **Price (RM)** 240.00, switch **Type** to\n**Subscription** and set **Billing period** to **Yearly**.\n\n## 2. Check the plan in the catalogue\n\n![Products list with the Keahlian Kelab 2026 row boxed, its Type cell reading Subscription · Yearly](02-catalogue-row.png)\n\nThe row reads **Subscription · Yearly · RM240.00**, and that one row is the whole plan: a price\nchange is a single edit here.\n\n## 3. Sell it from a payment form\n\n![Builder with a PRODUCT block bound to the plan and the Product dropdown boxed](03-bind-plan.png)\n\nForms → **New form** → **Blank form**, add **Product** under *PRICING FIELDS* plus **Name** and\n**Email**, then bind the block to the plan in **BLOCK SETTINGS → Product**.\n\n## 4. Publish and hand out the link\n\n![Builder with the live banner and the Copy link button boxed](04-publish.png)\n\n**Publish** makes the form live and **Copy link** gives you one address for the whole club, with no\nper-member form.\n\n## 5. What the member does\n\n![Member checkout page with the plan line and the renewal note marked](05-member-view.png)\n\nThe member ticks the plan, sees RM240.00, fills name and email and pays once; the card is saved\nthere and charged every year after it.\n\n## Notes\n\n- **Cancelling stops the next charge, not the card.** The saved card stays, so a member who rejoins\n  does not re-enter it.\n- **Renewals live under Subscriptions**, where a due charge can be retried or a plan cancelled.\n- **Yearly vs monthly is a product setting.** Put both Product blocks on one form and let the member\n  pick.\n",
    "shots": [
      {
        "file": "01-plan-product.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "02-catalogue-row.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "03-bind-plan.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "04-publish.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "05-member-view.png",
        "width": 1536,
        "height": 969
      }
    ]
  },
  {
    "slug": "design-with-mcp",
    "title": "Design with an AI agent (MCP): setup guide",
    "summary": "Connect your agent to JomForm once, then drive the store with plain language.",
    "body": "**What this does:** JomForm speaks the Model Context Protocol, so a connected agent can add\nproducts, edit forms, read sales and refund a sale for you.\n\n## 1. Open the connect page and copy the server URL\n\n![The MCP connect page with the Copy button beside https://mcp.jomform.com/mcp boxed](01-server-url.png)\n\nThe **MCP** page (`/mcp` on your JomForm address) prints the one thing you need,\n`https://mcp.jomform.com/mcp`, and the `/mcp` path is required. Press **Copy**.\n\n## 2. Paste it into your MCP client\n\n![The Claude Desktop JSON snippet boxed](02-client-config.png)\n\nThe page prints the exact `claude_desktop_config.json` entry, and any client that takes a remote MCP\nserver over HTTP uses the same URL. Restart the client after saving.\n\n## 3. Authorise\n\nThe client finds the OAuth 2.1 (PKCE) endpoints itself and opens a JomForm login page. Sign in and\ngrant access — there is no API key to copy or rotate, because the grant is the credential.\n\n## 4. Pick the scope\n\n![The Read-only and Read & write scope cards boxed](03-scope.png)\n\nThe grant is enforced on every tool call:\n\n| scope | what the agent can do |\n|---|---|\n| **read** | look at forms, products, sales and analytics; every tool that changes anything is refused |\n| **readwrite** | do all of that **and** change the store: add a product, edit a form, refund a sale |\n\nA client that asks for no scope gets **readwrite**, and a read-only grant can never upgrade itself.\n\n## 5. Ask for what you want\n\n![The example commands card boxed](04-example-commands.png)\n\n*\"add product RM25.00\"*, *\"how many sales today?\"*, *\"refund sale JF-xxxx\"*: the agent calls the\nmatching tool and answers, and anything that would change the store has to pass the scope above.\n\n## Notes\n\n- **Discovery is automatic.** The server advertises `/.well-known/oauth-protected-resource` and\n  `/.well-known/oauth-authorization-server`, and an unauthenticated call to `/mcp` answers `401`\n  with a `WWW-Authenticate` header pointing at them.\n- **Connecting from Canva, or any agent that is not a coding client:** it must allow a remote MCP\n  server with HTTP OAuth; paste the URL into the field it calls *remote MCP server*.\n- **The agent acts inside one workspace.** Tools take a workspace, so one connection can drive every\n  workspace your account manages and nothing outside them.\n",
    "shots": [
      {
        "file": "01-server-url.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "02-client-config.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "03-scope.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "04-example-commands.png",
        "width": 1536,
        "height": 900
      }
    ]
  },
  {
    "slug": "donations",
    "title": "Collect donations: setup guide",
    "summary": "Take donations with a JomForm payment form in eight steps, from your CHIP keys to the donor page.",
    "body": "**What this does:** a donation is a normal payment form and an amount is a **product** — either\nfixed tiers, or one *Pay what you want* product the donor prices.\n\n## 1. Connect CHIP Collect in Settings\n\n![Settings with the CHIP tab open and the Brand ID field boxed](01-settings-chip.png)\n\nSettings → **CHIP**: paste the **Brand ID** and **Secret Key** from the CHIP Merchant Portal, pick\nthe **Mode** the key was created in, and save. Nothing can be charged until this is done.\n\n## 2. Add your first donation amount in Products\n\n![Products page with the add row filled in and the Add product button boxed](02-products-add.png)\n\nProducts → name it **Donation RM10**, set the price **10.00**, leave **Type** on *One-time*, then\n**Add product**.\n\n## 3. Add the other amounts\n\n![Product list with three donation tiers and the RM50 price cell boxed](03-tiers.png)\n\nRepeat step 2 for **Donation RM50** and **Donation RM100**. Each tier is its own product, and its\nprice is what a donor gives when they pick it.\n\n## 4. Start from the Donation template\n\n![Template gallery with the Donation card's Use template button boxed](04-donation-template.png)\n\nForms → **New form** → **Use template** on *Donation*, which arrives as a payment form with a\nproduct block, name, email and an optional message.\n\n## 5. Point each Product block at one amount\n\n![Builder with three product blocks and the block's Product dropdown boxed](05-product-block.png)\n\nAdd one **Product** block per amount (**⋯ → Duplicate** on the block the template gave you) and\nchoose its tier in **Product**.\n\n## 6. Publish\n\n![Builder toolbar with the Publish button boxed](06-publish.png)\n\n**Publish** puts the form live; until you click it the form stays a draft nobody else can open.\n\n## 7. Copy the donation link\n\n![Live banner with the Copy link button boxed](07-live-link.png)\n\nThe green *Your form is live* banner holds the donor URL, and **Copy link** is what you paste into\nWhatsApp, a poster QR code, or your website.\n\n## 8. What the donor sees\n\n![Buyer page with the RM50 tier ticked and the total reading RM50.00](08-buyer.png)\n\nThe donor ticks one amount, the total follows it, and *Quantity* lets them give a multiple of a tier\nbefore they proceed to payment.\n\n## Notes\n\n- **Pay what you want is one product, not a tier.** A product of type `custom_amount`\n  (*Pay what you want (donation)*) renders an amount field instead of a price; the server checks the\n  amount against that product's **Minimum amount** (never below RM1.00), optional **Maximum** and\n  optional **Suggested** amount before pricing the sale. The type is chosen on the product editor at\n  `/products/new`.\n- **Every donation is a sale from the first click.** It appears under **Sales** with a `JF-…`\n  reference and moves to `paid` when CHIP confirms it; a workspace with no CHIP credentials produces\n  a `failed` sale with its reason, never a free response.\n",
    "shots": [
      {
        "file": "01-settings-chip.png",
        "width": 1900,
        "height": 995
      },
      {
        "file": "02-products-add.png",
        "width": 1900,
        "height": 1041
      },
      {
        "file": "03-tiers.png",
        "width": 1900,
        "height": 1179
      },
      {
        "file": "04-donation-template.png",
        "width": 1900,
        "height": 760
      },
      {
        "file": "05-product-block.png",
        "width": 1900,
        "height": 1269
      },
      {
        "file": "06-publish.png",
        "width": 1900,
        "height": 875
      },
      {
        "file": "07-live-link.png",
        "width": 1900,
        "height": 875
      },
      {
        "file": "08-buyer.png",
        "width": 1900,
        "height": 1539
      }
    ]
  },
  {
    "slug": "payment-fees",
    "title": "Payment fees: merchant setup guide",
    "summary": "Pass the CHIP processing fee to your buyer in six steps, at the rate you set per payment method.",
    "body": "**What this does:** JomForm adds your acquirer's fee on top of the price and grosses the total up,\nso you still receive the full asking amount.\n\n## 1. Open Settings → Payment fees\n\n![Settings tabs with the Payment fees tab boxed](01-settings-payment-fees-tab.png)\n\nSettings → **Payment fees**, the tab between *e-Invoice* and *Webhooks*. Only workspace admins and\nowners see it.\n\n## 2. Tick the payment methods you want to charge for\n\n![Payment method rows with the Card checkbox boxed](02-method-rows.png)\n\nOne row per method — **FPX**, **Card**, **e-Wallet**, **DuitNow QR** — and a method with no tick is\npassed through at cost.\n\n## 3. Enter your rate\n\n![Card row with the Percent field boxed, value 200](03-set-rate.png)\n\n**Fixed (sen)** is a flat fee (`100` = RM1.00) and **Percent (basis points)** is a percentage\n(`200` = 2%); you may use one or both.\n\n## 4. Acknowledge the disclosure\n\n![Acknowledgement checkbox boxed](04-acknowledge.png)\n\nEnabling a method requires this tick, which records that the fee is passed to the buyer and who\nacknowledged it.\n\n## 5. Save\n\n![Saved confirmation and the Card 2.00% rate label](05-saved.png)\n\nSaving is immediate: the green **Payment fees saved.** banner confirms it and the row reads\n**Card 2.00%**.\n\n## 6. What your buyer sees\n\n![Buyer page with the payment-method picker boxed](06-buyer-sees-fee.png)\n\nThe buyer picks the method above the Pay button, and the fee line, the total and the button follow\nthat choice. With FPX and Card both set to 2.00% and RM100.00 on the form:\n\n| Line | Amount |\n|---|---|\n| Subtotal | RM100.00 |\n| Processing fee — FPX | RM2.04 |\n| **Total** | **RM102.04** |\n\nThe fee is **RM2.04, not RM2.00**: a flat 2% would leave you with RM99.96 after CHIP takes its 2%,\nso the total is solved backwards and you receive the full RM100.00.\n\n## Notes\n\n- The rate is snapshotted on each sale, so a later change does not alter past sales or receipts.\n- SST is not added on top of these rates in this version; if your acquirer quotes a rate inclusive\n  of SST, enter the all-in figure.\n- Card acceptance is at your own risk as the merchant, and the acknowledgement in step 4 records it.\n",
    "shots": [
      {
        "file": "01-settings-payment-fees-tab.png",
        "width": 1900,
        "height": 1250
      },
      {
        "file": "02-method-rows.png",
        "width": 1900,
        "height": 1250
      },
      {
        "file": "03-set-rate.png",
        "width": 1900,
        "height": 1250
      },
      {
        "file": "04-acknowledge.png",
        "width": 1900,
        "height": 1250
      },
      {
        "file": "05-saved.png",
        "width": 1900,
        "height": 1261
      },
      {
        "file": "06-buyer-sees-fee.png",
        "width": 1900,
        "height": 1200
      }
    ]
  },
  {
    "slug": "qr-table-ordering",
    "title": "Table ordering with a QR menu: setup guide",
    "summary": "Put a menu behind a QR code on every table, so each order arrives with its table number attached.",
    "body": "**What this does:** each dish is a *product* and a form holds one **Product** block per dish; the\n*Restaurant menu* switch serves that form's URL as a menu page, and the QR code is that URL with\n`?table=N` on the end.\n\n## 1. Add every dish as a product\n\n![Products page with a dish filled in and the Add product button boxed](01-add-dish.png)\n\nProducts → fill **Product name** and **Price (RM)** → **Add product**, one product per dish, with\n**Stock** left blank for ∞.\n\n## 2. Check the catalogue\n\n![Products list with four dishes and the Price column boxed](02-dish-catalogue.png)\n\nThe **PRICE (RM)** column is exactly what the diner pays, so fix prices here rather than in the\nform.\n\n## 3. Start from the F&B template\n\n![Template gallery with the F&B Order + Payment card's Use template button boxed](03-fnb-template.png)\n\nForms → **New form** → **F&B Order + Payment** → **Use template**, which arrives as a payment form\nwith a Section intro, one Product block, then Name, Email, Phone and Special instructions.\n\n## 4. One Product block per dish\n\n![Builder with three dish blocks in the canvas and the Product dropdown boxed](04-product-blocks.png)\n\nClick **Product** under *PRICING FIELDS* once per dish, and bind each block to its dish in\n**BLOCK SETTINGS → Product**.\n\n## 5. Turn the form into a menu\n\n![Form Settings → Restaurant menu with the Enable restaurant menu switch boxed and on](05-enable-menu.png)\n\n**Form Settings → Restaurant menu → Enable restaurant menu** serves the menu at this form's own URL\nand never touches the workspace home page.\n\n## 6. Generate the table QR\n\n![General tab with the QR code (table ordering) section open and the generated QR boxed](06-table-qr.png)\n\n**Form Settings → General → QR code (table ordering)**: type the table number and press\n**Generate QR**. The code encodes `…/f/f-b-order-payment?table=7`, so nothing but the number changes\nbetween tables.\n\n## 7. What the diner scans into\n\n![Public menu page for table 7 with four dishes, prices, add buttons and the Table 7 banner boxed](07-diner-menu.png)\n\nThe diner gets a **Best Seller** filter, a search box, the categories and a row per dish, plus a\n**Table 7** banner that came from the QR link. In the basket they also pick a payment method, and the\n**Bayar** button names the pick.\n\n## Notes\n\n- **The form has to be published.** The menu is the published form's public page; a draft shows the\n  normal form instead.\n- **Changing a price is a Products change.** The menu reads the live product, so no republish is\n  needed for a price.\n- **A dish with no Product block never appears.** The menu is built from the blocks, not from the\n  catalogue.\n",
    "shots": [
      {
        "file": "01-add-dish.png",
        "width": 1521,
        "height": 900
      },
      {
        "file": "02-dish-catalogue.png",
        "width": 1521,
        "height": 1008
      },
      {
        "file": "03-fnb-template.png",
        "width": 1536,
        "height": 643
      },
      {
        "file": "04-product-blocks.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "05-enable-menu.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "06-table-qr.png",
        "width": 1536,
        "height": 1470
      },
      {
        "file": "07-diner-menu.png",
        "width": 1512,
        "height": 770
      }
    ]
  },
  {
    "slug": "tuition-class",
    "title": "Tuition and class registration: setup guide",
    "summary": "Take a one-time registration fee and a monthly class fee on one form, in six steps.",
    "body": "**What this does:** products carry both shapes, **One-time** and **Subscription**, and one\n**Product** block per fee puts both lines in front of the parent.\n\n## 1. Make the monthly fee a Subscription product\n\n![Products page with a monthly class fee set to Subscription and the Billing period control boxed](01-monthly-fee.png)\n\nProducts → **Product name** + **Price (RM)** → switch **Type** to **Subscription**, then set\n**Billing period** (*Monthly* here) and **Billing count** (*Infinite (recurring)*).\n\n## 2. Add the one-time registration fee\n\n![Products list with the One-time and Subscription · Monthly rows marked](02-two-fees.png)\n\nAdd the registration fee with **Type** left on *One-time*; the list then shows **One-time** RM50.00\nbeside **Subscription · Monthly** RM150.00.\n\n## 3. Start from the Tuition Class Registration template\n\n![Template gallery with the Tuition Class Registration card's Use template button boxed](03-tuition-template.png)\n\nForms → **New form** → **Tuition Class Registration** → **Use template**, a payment form with a\nSection intro, one Product block and the student's details.\n\n## 4. Bind both fees to the form\n\n![Builder with the Registration fee and Yuran bulanan blocks and the Product dropdown boxed](04-bind-fees.png)\n\nPress **Product** under *PRICING FIELDS* once per fee, then pick the fee in **BLOCK SETTINGS →\nProduct**; **Text / label** is what the parent reads, so name each block after its fee.\n\n## 5. Publish and copy the link\n\n![Builder with the live banner and the Copy link button boxed](05-publish.png)\n\n**Publish** makes the form live and the banner hands you the address; **Copy link** is what goes\ninto the class group.\n\n## 6. What the parent sees\n\n![Public registration page with the one-time fee and the monthly fee marked](06-parent-view.png)\n\nThe parent sees both fees with a **Quantity** each, a **Total** and one **Proceed to Payment**\nbutton, so a family enrolling two children ticks 2 on the registration fee alone.\n\n## Notes\n\n- **Billing count decides whether it ever stops.** *Infinite (recurring)* charges until cancelled,\n  while *Fixed N times* stops after N charges, which suits a 10-month programme.\n- **The total is zero until a fee is ticked.** Both blocks start unchecked, so tick one and the\n  amount and the payment step follow.\n- **Any form can sell the same product.** The recurring behaviour comes from the product row, not\n  from the form.\n",
    "shots": [
      {
        "file": "01-monthly-fee.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "02-two-fees.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "03-tuition-template.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "04-bind-fees.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "05-publish.png",
        "width": 1536,
        "height": 900
      },
      {
        "file": "06-parent-view.png",
        "width": 1536,
        "height": 1203
      }
    ]
  },
  {
    "slug": "wedding-rsvp",
    "title": "Wedding page and RSVP form: setup guide",
    "summary": "Put a wedding page and an RSVP form online in nine steps, from a blank form to the list of replies.",
    "body": "**What this does:** an RSVP collects answers, not money, so the form runs in *response* mode and\nsells nothing.\n\n## 1. Start a new form\n\n![Forms page with the New form button boxed](01-new-form.png)\n\nForms → **New form**. The list behind it holds the drafts and the published forms of the workspace\nyou are in.\n\n## 2. Choose Blank form\n\n![Template gallery with the Blank form card boxed](02-blank-form.png)\n\nThere is no wedding template, so start from **Blank form** — an empty canvas with the full field\npalette.\n\n## 3. Set Form mode to Response form\n\n![Builder with the Form mode dialog open and the Response form option boxed](03-response-mode.png)\n\nThe mode sits in the builder's **top bar next to the slug**: the button reads *Payment form*, press\nit and pick **Response form**. A response form collects answers only, so there is no product to sell\nand no payment step.\n\n## 4. Add the questions and mark the required ones\n\n![Builder canvas with the RSVP blocks and the Required checkbox boxed](04-blocks-required.png)\n\nClick a field in the **palette** to add it; for this RSVP the six blocks are:\n\n| Block | Label used here | Notes |\n|---|---|---|\n| **Section** | We are getting married: join us for dinner and dancing | A heading, not a question |\n| **Name** | Full name | Required by default |\n| **Email** | Email | Required by default |\n| **Radio buttons** | Will you join us on 12 December 2026? | Edit the two choices in *Choices* |\n| **Number** | Number of guests (including you) | Optional. Only an attending guest answers it |\n| **Note** | Message to the couple | Free-form message |\n\nSelect a block and its settings open in the right-hand panel; tick **Required** on the attendance\nquestion so a guest cannot submit without answering it.\n\n## 5. Publish\n\n![Builder toolbar with the Save & publish button boxed](05-publish.png)\n\n**Save & publish** in the top bar writes the form and puts it live in one step.\n\n## 6. Copy the public link\n\n![Live banner with the Copy link button boxed](06-live-link.png)\n\nThe green **Your form is live** banner holds the guest URL, and **Copy link** is what goes into the\ninvitation.\n\n## 7. Set the theme colour\n\n![Form Settings → General with the Theme color control boxed](07-theme-colour.png)\n\n**Form Settings → General → Theme color (primary)** is the colour of the page's buttons and accents;\npress **Save** and the page answers **Saved ✓**.\n\n## 8. What the guest sees\n\n![Public RSVP page with the rose theme, five questions and a Submit button](08-guest-rsvp.png)\n\nThe guest gets the title, the invitation line, the five fields and one **Submit** button, with\n*(required)* beside the questions that need an answer.\n\n## 9. Read the replies\n\n![Sales → Responses (this form) with one RSVP answer row and the Export CSV button boxed](09-responses.png)\n\n**Submission** at the top of Form Settings opens **Sales** filtered to this form, one row per reply.\n**Export CSV** downloads every reply in that filter.\n\n## Notes\n\n- **A landing page in front of the form is optional.** Settings → **Storefront** turns the workspace\n  home page into a hero, subtitle, body, image and a CTA button that points at the form.\n- **A new slug does not break invitations.** The old address keeps working through a temporary\n  redirect.\n",
    "shots": [
      {
        "file": "01-new-form.png",
        "width": 1900,
        "height": 824
      },
      {
        "file": "02-blank-form.png",
        "width": 1900,
        "height": 824
      },
      {
        "file": "03-response-mode.png",
        "width": 1900,
        "height": 1217
      },
      {
        "file": "04-blocks-required.png",
        "width": 1900,
        "height": 1217
      },
      {
        "file": "05-publish.png",
        "width": 1900,
        "height": 1217
      },
      {
        "file": "06-live-link.png",
        "width": 1900,
        "height": 1302
      },
      {
        "file": "07-theme-colour.png",
        "width": 1900,
        "height": 1263
      },
      {
        "file": "08-guest-rsvp.png",
        "width": 1900,
        "height": 800
      },
      {
        "file": "09-responses.png",
        "width": 1900,
        "height": 760
      }
    ]
  }
];

/** The guide a page renders. Throws at build time rather than shipping a blank page. */
export function guideBySlug(slug: string): Guide {
  const guide = GUIDES.find((g) => g.slug === slug);
  if (!guide) {
    throw new Error(
      `no guide "${slug}" in src/lib/guides.gen.ts — run \`npm run guides:sync\`. ` +
        `Known slugs: ${GUIDES.map((g) => g.slug).join(", ")}`,
    );
  }
  return guide;
}
