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
    "summary": "Give someone a link, and the sales that arrive through it are attributed to them with a commission on every paid sale. Every screenshot here was captured from the running product; the red box marks…",
    "body": "Give someone a link, and the sales that arrive through it are attributed to them with a commission\non every paid sale. Every screenshot here was captured from the running product; the red box marks\nthe control you touch at that step.\n\n**What this does:** an affiliate is a code in a workspace. A sale that arrives with that code attached\nis attributed to the affiliate. The commission you set here is computed on the **paid** sale, not on\nthe form view and not on an abandoned checkout.\n\n## 1. Create the affiliate\n\n![Settings → Affiliates with Code SITI, Name Siti Zubaidah, Commission 10 and the Add affiliate button boxed](01-create-affiliate.png)\n\nSettings → **Affiliates**. Fill **Code** (the short code that goes in the link, `SITI` here),\n**Name** (who it belongs to), **Commission %** (10 here) and press **Add affiliate**. The code is\nunique inside the workspace; a second `SITI` is refused with *code already exists*.\n\n## 2. Hand over the link\n\n![The affiliate row with its referral link boxed](02-referral-link.png)\n\nThe new row carries the affiliate's own link:\n`https://<workspace>.jomform.com/?aff=SITI`. Anything a visitor buys after opening that URL is\nattributed to SITI, and 10% of the paid amount is her commission. The code is per workspace, so the\nsame person promoting two workspaces has two links.\n\n## How the commission is counted\n\n- **Only paid sales count.** A commission exists because a payment settled. An unpaid checkout, a\n  refused card or a form response with no product contributes nothing.\n- **The sale records the code, not the money.** Each sale carries the affiliate code that brought it\n  in, and the commission is the affiliate's rate applied to that sale. The row above is the one\n  place the percentage lives.\n- **Cancelling a card never revokes anything on the affiliate side.** Attribution follows the sale\n  record, not the card token.\n\n## Notes\n\n- **One code per promotion, not per person.** A merchant tracking two channels (a WhatsApp group and\n  an Instagram bio) uses two codes, and each channel gets its own commission line.\n- **Driving this from an agent:** the same three operations exist as MCP tools\n  (`create_affiliate`, `list_affiliates`, `set_affiliate_commission`), and they return the same\n  `referral_url` the tab shows.\n",
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
    "summary": "One membership, one fee that repeats, and nobody has to remember to pay next year. Every screenshot here was captured from the running product; the red box marks the control you touch at that step.",
    "body": "One membership, one fee that repeats, and nobody has to remember to pay next year. Every screenshot\nhere was captured from the running product; the red box marks the control you touch at that step.\n\n**What this does:** a membership is a single **Subscription** product, monthly or yearly, sold from\nany payment form. The member pays once with a card at checkout; the card is saved, and the same card\nis charged every period after that. There is no spreadsheet of who has paid and no bank transfer to\nreconcile.\n\n## 1. Create the plan as a Subscription product\n\n![Products page with Keahlian Kelab 2026 set to Subscription, price 240.00, and the Billing period boxed on Yearly](01-plan-product.png)\n\nProducts → **Product name** (here *Keahlian Kelab 2026*), **Price (RM)** 240.00, then change **Type**\nto **Subscription** and set **Billing period** to **Yearly** (or *Monthly* for a monthly club).\n**Billing count** *Infinite (recurring)* means it renews until cancelled. **Next charge date**\n*From the day it succeeds* starts the cycle on the first successful payment.\n\n## 2. Check the plan in the catalogue\n\n![Products list with the Keahlian Kelab 2026 row boxed, its Type cell reading Subscription · Yearly](02-catalogue-row.png)\n\nThe row reads **Subscription · Yearly · RM240.00**. That one row is the whole plan. Every member of\nthe club is sold this same product, so a price change is a single edit here.\n\n## 3. Sell it from a payment form\n\n![Builder with a PRODUCT block bound to the plan and the Product dropdown boxed](03-bind-plan.png)\n\nForms → **New form** → **Blank form**, then add **Product** under *PRICING FIELDS*, **Name** and\n**Email**. Select the Product block and pick the plan in **BLOCK SETTINGS → Product**. A **Section**\nblock under the email is worth the thirty seconds it costs: it renders as prose, where a *Note* block\nwould render an input the member has to fill. That is where you tell the member the fee renews.\n\n## 4. Publish and hand out the link\n\n![Builder with the live banner and the Copy link button boxed](04-publish.png)\n\n**Publish** makes the form live and the banner gives you the address\n(`…/f/keahlian-kelab-2026`). **Copy link** is what goes to the members: one link for the whole club,\nno per-member form.\n\n## 5. What the member does\n\n![Member checkout page with the plan line and the renewal note marked](05-member-view.png)\n\nThe member ticks the plan, sees **RM240.00**, fills **Full name** and **Email**, reads the renewal\nnote and presses **Proceed to Payment** once. That is the last time they type a card number: the card\nis saved at that payment and charged every year after it.\n\n## Notes\n\n- **Cancelling stops the next charge, not the card.** Cancelling a membership ends future charges and\n  leaves the saved card alone, so a member who rejoins does not re-enter it.\n- **Members and recurring plans live under Subscriptions** in the sidebar, where a due charge can be\n  retried or a plan cancelled. The same actions exist over MCP if you drive JomForm from an agent.\n- **Yearly vs monthly is a product setting, not a form setting.** The same form can sell a monthly\n  plan and a yearly plan as two products: put both Product blocks on the form and let the member\n  pick.\n",
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
        "height": 900
      }
    ]
  },
  {
    "slug": "design-with-mcp",
    "title": "Design with an AI agent (MCP): setup guide",
    "summary": "JomForm speaks the Model Context Protocol. Connect your AI agent once and it can drive the store with plain language: add products, edit forms, read sales, refund a sale. Every screenshot here was…",
    "body": "JomForm speaks the Model Context Protocol. Connect your AI agent once and it can drive the store with\nplain language: add products, edit forms, read sales, refund a sale. Every screenshot here was\ncaptured from the running product; the red box marks the control you touch at that step.\n\n## 1. Open the connect page and copy the server URL\n\n![The MCP connect page with the Copy button beside https://mcp.jomform.com/mcp boxed](01-server-url.png)\n\nThe **MCP** page (`/mcp` on your JomForm address) is the whole setup, and it prints the one thing you\nneed: **`https://mcp.jomform.com/mcp`**. The `/mcp` path is required: `mcp.jomform.com` without it\nfails. Press **Copy**.\n\n## 2. Paste it into your MCP client\n\n![The Claude Desktop JSON snippet boxed](02-client-config.png)\n\nClaude Desktop reads its servers from `claude_desktop_config.json`; the page prints the exact entry\n(`{\"mcpServers\": {\"jomform\": {\"type\": \"http\", \"url\": \"https://mcp.jomform.com/mcp\"}}}`). Any other\nclient that supports a remote MCP server over HTTP takes the same URL instead. Restart the client\nafter saving.\n\n## 3. Authorise\n\nThe client fetches the server, finds the OAuth 2.1 (PKCE) endpoints itself and opens a JomForm login\nor authorise page. Sign in and grant access. **There is no API key to copy and no token to rotate**:\nthe grant is the credential.\n\n## 4. Pick the scope\n\n![The Read-only and Read & write scope cards boxed](03-scope.png)\n\nThe client asks for a scope and the grant is enforced on every tool call:\n\n| scope | what the agent can do |\n|---|---|\n| **read** | look at forms, products, sales and analytics. Every tool that creates, updates, deletes, archives, cancels or refunds is refused with a scope error |\n| **readwrite** | do all of that **and** change the store: add a product, edit a form, refund a sale |\n\nA client that asks for no scope gets **readwrite**. A read-only grant cannot upgrade itself: it is\nrefused on the tool that mints API keys, so it can never manufacture a wider credential.\n\n## 5. Ask for what you want\n\n![The example commands card boxed](04-example-commands.png)\n\nThat is the whole interface from here on. *\"add product RM25.00\"*, *\"how many sales today?\"*,\n*\"refund sale JF-xxxx\"*, *\"show my product inventory\"*: the agent calls the matching tool and\nanswers. The same plain language works for forms, prices and analytics, and a tool that would change\nsomething is a call the scope above has to allow.\n\n## Notes\n\n- **Discovery is automatic.** The server advertises protected-resource metadata at\n  `/.well-known/oauth-protected-resource` and authorization-server metadata at\n  `/.well-known/oauth-authorization-server`. An unauthenticated call to `/mcp` answers `401` with a\n  `WWW-Authenticate` header pointing at the metadata.\n- **Connecting from Canva (or any agent that is not a coding client):** it needs to allow a remote\n  MCP server with HTTP OAuth. Where the client has no JSON config, paste the server URL into the\n  field it calls *remote MCP server* / *MCP connection*.\n- **The agent acts inside one workspace.** Tools take a workspace, so the same connection can drive\n  every workspace your account can manage, and nothing outside them.\n",
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
    "summary": "A step-by-step guide to taking donations with a JomForm payment form. Every screenshot below was captured from the running product; the red box marks the one control you touch at that step.",
    "body": "A step-by-step guide to taking donations with a JomForm payment form. Every screenshot below was\ncaptured from the running product; the red box marks the one control you touch at that step.\n\n**What this does:** a donation is a normal payment form, and an amount is a **product**. There are\ntwo shapes of donation product:\n\n- **Fixed denominations**: one product per amount you accept (`Donation RM10`, `Donation RM50`,\n  `Donation RM100`), and the donor ticks the one they want. Steps 2–8 below walk this shape.\n- **Pay what you want**: a single product set to *Pay what you want (donation)*. The form shows the\n  donor an amount field instead of a price, and the amount is checked against the limits you set.\n  See the first two Notes.\n\nEither way the money lands in the same sale list and receipt flow as any other paid form.\n\n## 1. Connect CHIP Collect in Settings\n\n![Settings with the CHIP tab open and the Brand ID field boxed](01-settings-chip.png)\n\nSettings → **CHIP**. Paste the **Brand ID** and **Secret Key** from the CHIP Merchant Portal, pick the **Mode** the key was created in there (Test or Live), and save. The mode is a label you choose, because the key itself carries no hint of which one it is. That save is what turns the form's pay button into a working payment.\n\n## 2. Add your first donation amount in Products\n\n![Products page with the add row filled in and the Add product button boxed](02-products-add.png)\n\nProducts → type the name (**Donation RM10**), the price (**10.00**), leave **Type** on *One-time*,\nthen **Add product**. The price you type is exactly what the donor pays for that tier.\n\n## 3. Add the other amounts\n\n![Product list with three donation tiers and the RM50 price cell boxed](03-tiers.png)\n\nRepeat step 2 for **Donation RM50** and **Donation RM100**. Each tier is its own product, and the\nprice cell is the amount a donor gives when they pick that tier.\n\n## 4. Start from the Donation template\n\n![Template gallery with the Donation card's Use template button boxed](04-donation-template.png)\n\nForms → **New form** opens the template gallery → **Use template** on *Donation*. That gives you a\npayment form already carrying a product block, name, email and an optional message.\n\n## 5. Point each Product block at one amount\n\n![Builder with three product blocks and the block's Product dropdown boxed](05-product-block.png)\n\nAdd one **Product** block per amount. The quickest is the block's **⋯ → Duplicate** menu on the block\nthe template gave you; *Pricing Fields → Product* in the palette also inserts one. Then select a\nblock and choose its tier in the **Product** dropdown. The buyer sees that product's name and price\non the form, and a pay-what-you-want product shows the amount field instead (see Notes).\n\n## 6. Publish\n\n![Builder toolbar with the Publish button boxed](06-publish.png)\n\n**Publish** writes the form and puts it live. Until you click it the toolbar shows the unsaved state,\nand the form stays a draft that no one else can open.\n\n## 7. Copy the donation link\n\n![Live banner with the Copy link button boxed](07-live-link.png)\n\nThe green *Your form is live* banner confirms the publish and holds the donor URL. **Copy link** (or\n**Open form**) is what you paste into WhatsApp, a poster QR code, or your website.\n\n## 8. What the donor sees\n\n![Buyer page with the RM50 tier ticked and the total reading RM50.00](08-buyer.png)\n\nThe donor ticks one amount (RM50.00 here), the total updates to **RM50.00**, and *Quantity* lets them\ngive a multiple of a tier. After filling name and email they proceed to payment.\n\n## Notes\n\n- **A product's price can be fixed or chosen by the donor.** A product's type is `one_time`,\n  `subscription` or `custom_amount`. The tiers in steps 2–3 are `one_time` products, one price each.\n  A `custom_amount` product (*Pay what you want (donation)*) has no fixed asking price: the form\n  renders an amount field instead of a price. The **server** checks the amount the donor typed\n  against that product's **Minimum amount** (never below RM1.00), its optional **Maximum amount**\n  and its optional **Suggested amount** before it prices the sale. An amount outside those limits is\n  refused with a readable reason and nothing is charged. An accepted one becomes the line item's\n  price, and the sale appears under **Sales** with that amount. The price stored on such a product\n  is for display only: the suggested amount when one is set, else the minimum.\n- **Where each shape lives.** The Products page add row in step 2 creates *One-time* products, which\n  is the fixed-denomination walkthrough in steps 2–8. The pay-what-you-want type is chosen on the\n  product editor page (`/products/new`, or `/products/<id>/edit`).\n- **A new amount later.** For a fixed tier: step 2 plus one more **Product** block in the builder,\n  then Publish again. For a pay-what-you-want product: a change to that product's minimum, maximum\n  or suggested amount.\n- **Nothing can be charged until step 1 is done.** On a workspace with no CHIP credentials the pay\n  button answers *\"We could not start the payment, so nothing was charged\"* with a sale reference.\n  That sale shows up under **Sales** as `failed`, and the donation is never silently collected as a\n  free response.\n- **Each donation is a sale from the first click.** The sale appears under **Sales** with its\n  reference (`JF-…`), the donor's name and email, what they picked and the amount. Once CHIP\n  confirms the payment the sale moves to `paid`, and the donor is emailed the workspace's receipt.\n",
    "shots": [
      {
        "file": "01-settings-chip.png",
        "width": 1900,
        "height": 960
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
        "height": 1129
      }
    ]
  },
  {
    "slug": "payment-fees",
    "title": "Payment fees: merchant setup guide",
    "summary": "A step-by-step guide to passing the CHIP processing fee to the buyer. Every screenshot below was captured from the running product; the red box marks the control you touch at that step.",
    "body": "A step-by-step guide to passing the CHIP processing fee to the buyer. Every screenshot below was\ncaptured from the running product; the red box marks the control you touch at that step.\n\n**What this does:** you set your acquirer rate per payment method. JomForm adds the fee on top of the\nprice and grosses the total up, so you still receive the full asking amount. The buyer sees the fee,\nthe method and the final total before paying.\n\n## 1. Open Settings → Payment fees\n\n![Settings tabs with the Payment fees tab boxed](01-settings-payment-fees-tab.png)\n\nSettings is in the left sidebar. The **Payment fees** tab sits between *e-Invoice* and *Webhooks*.\nOnly workspace admins and owners see it.\n\n## 2. Tick the payment methods you want to charge for\n\n![Payment method rows with the Card checkbox boxed](02-method-rows.png)\n\nOne row per method: **FPX**, **Card**, **e-Wallet**, **DuitNow QR**. Ticking a row is what enables the\nfee for that method; a method with no tick is passed through at cost.\n\n## 3. Enter your rate\n\n![Card row with the Percent field boxed, value 200](03-set-rate.png)\n\n- **Fixed (sen)**: a flat fee in sen. `100` = RM1.00.\n- **Percent (basis points)**: `100` = 1%, `200` = 2%, `140` = 1.4%.\n\nYou may use one or both. The rate you type is the rate the buyer pays; the total is grossed up so the\nfee comes out of the buyer's side, not yours.\n\n## 4. Acknowledge the disclosure\n\n![Acknowledgement checkbox boxed](04-acknowledge.png)\n\nEnabling a method requires this tick. It records that the fee is passed to the buyer and is subject\nto acquirer and card-issuer rules. JomForm stores who acknowledged and when.\n\n## 5. Save\n\n![Saved confirmation and the Card 2.00% rate label](05-saved.png)\n\nSaving is immediate: the green **Payment fees saved.** banner confirms it, and the rate label on the\nrow (`Card 2.00%`) is the rate your buyers will now be charged. No reload is needed.\n\n## 6. What your buyer sees\n\n![Buyer page with the method radio and the fee line boxed](06-buyer-sees-fee.png)\n\nOn the payment form the buyer picks a method, and the summary shows the fee line and the final total\nbefore they proceed. With RM100.00 at 2%:\n\n| Line | Amount |\n|---|---|\n| Subtotal | RM100.00 |\n| Processing fee (Card 2.00%) | RM2.04 |\n| **Total** | **RM102.04** |\n\nThe fee is **RM2.04, not RM2.00**. Charging a flat 2% of RM100.00 would leave you with RM99.96 after\nCHIP takes its 2%, so the total is solved backwards (`100 / (1 − 0.02) = 102.04`). You receive the\nfull RM100.00; the buyer covers the RM2.04. Rounding is capped so you never lose more than 1 sen.\n\n## Notes\n\n- The rate is snapshotted on each sale: changing your rate later does not alter past sales or receipts.\n- SST is not added on top of these rates in this version. If your acquirer quotes a rate inclusive of\n  SST, enter the all-in figure.\n- Card acceptance is at your own risk as the merchant; the acknowledgement in step 4 records that.\n",
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
        "height": 1250
      }
    ]
  },
  {
    "slug": "qr-table-ordering",
    "title": "QR table ordering: setup guide",
    "summary": "A QR code on the table, a menu that opens in the diner's browser, and the order arrives with the table number attached. Every screenshot below was captured from the running product; the red box marks…",
    "body": "A QR code on the table, a menu that opens in the diner's browser, and the order arrives with the\ntable number attached. Every screenshot below was captured from the running product; the red box\nmarks the control you touch at that step.\n\n**What this does:** each dish is a *product*. A form in payment mode holds one **Product** block per\ndish, and the *Restaurant menu* switch makes that form's own URL render a menu page instead of the\nplain form. The QR code is just that URL with `?table=N` on the end, so the order always knows which\ntable it came from.\n\n## 1. Add every dish as a product\n\n![Products page with a dish filled in and the Add product button boxed](01-add-dish.png)\n\nProducts → fill **Product name**, **Price (RM)** and press **Add product**. Leave **Stock** blank\nfor ∞ (unlimited) and keep **Type** on *One-time*. Repeat once per dish, one product per line item on\nthe menu.\n\n## 2. Check the catalogue\n\n![Products list with four dishes and the Price column boxed](02-dish-catalogue.png)\n\nThe list is now the source of truth for the menu: Nasi Lemak RM12.00, Char Kway Teow RM14.00, Teh\nTarik RM3.50 and Roti Bakar RM5.00. The **PRICE (RM)** column is exactly what the diner pays, so fix\na price here rather than in the form.\n\n## 3. Start from the F&B template\n\n![Template gallery with the F&B Order + Payment card's Use template button boxed](03-fnb-template.png)\n\nForms → **New form** → **F&B Order + Payment** → **Use template**. The template arrives as a payment\nform (a Section intro, one Product block, then Name / Email / Phone / Special instructions), which is\nthe shape a menu checkout needs.\n\n## 4. One Product block per dish\n\n![Builder with three dish blocks in the canvas and the Product dropdown boxed](04-product-blocks.png)\n\nIn the builder, click **Product** under *PRICING FIELDS* once per dish. With a Product block\nselected, **BLOCK SETTINGS → Product** is the dropdown that binds the block to the dish, and\n**Text / label** is the name the diner sees. Four dishes means four Product blocks.\n\n## 5. Turn the form into a menu\n\n![Form Settings → Restaurant menu with the Enable restaurant menu switch boxed and on](05-enable-menu.png)\n\n**Form Settings → Restaurant menu → Enable restaurant menu.** The screen itself says what the switch\ndoes: *the menu is served at this form's own URL and lists only the products this form's product\nblocks reference; the workspace home page is never affected.* The same panel holds the optional\n**Best-seller product**, the **Categories** list (**+ Add category**), the optional extra checkout\nfields, and a **Menu preview** that counts the items (4 here). Press **Save**.\n\n## 6. Generate the table QR\n\n![General tab with the QR code (table ordering) section open and the generated QR boxed](06-table-qr.png)\n\n**Form Settings → General → QR code (table ordering)**: type the table number, press **Generate QR**,\nand the panel shows the code plus the URL it encodes, `…/f/f-b-order-payment?table=7`. One code per\ntable: print it, or download it and put it on the table card. Nothing else has to change between\ntables.\n\n## 7. What the diner scans into\n\n![Public menu page for table 7 with four dishes, prices, add buttons and the Table 7 banner boxed](07-diner-menu.png)\n\nThe guest gets the restaurant name, a **Best Seller** filter, a search box, the category row and a\nrow per dish with its price and a **+** button. The **Table 7** banner at the top came from the QR\nlink, not from the diner, so the order that follows is already tied to the table.\n\n## Notes\n\n- **The form has to be published.** The menu is the published form's public page; a draft shows the\n  normal form (or nothing) instead.\n- **Changing a price is a Products change, not a form change.** The menu reads the live product, so a\n  price fix in Products shows up on the next page load. No republish is needed for the price itself.\n- **A dish with no Product block never appears.** The menu is built from the blocks, so a product that\n  exists in the catalogue but has no block is simply not on the menu.\n",
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
    "summary": "A registration form that takes a one-time fee today and a monthly class fee after that. Every screenshot here was captured from the running product; the red box marks the control you touch at that…",
    "body": "A registration form that takes a one-time fee today and a monthly class fee after that. Every\nscreenshot here was captured from the running product; the red box marks the control you touch at\nthat step.\n\n**What this does:** a tuition class needs two different amounts: a registration fee paid once, and a\nmonthly fee that repeats. JomForm products carry both shapes, **One-time** and **Subscription**. The\nform holds one **Product** block per fee, so the parent sees both lines on the same page and one\ncheckout covers them.\n\n## 1. Make the monthly fee a Subscription product\n\n![Products page with a monthly class fee set to Subscription and the Billing period control boxed](01-monthly-fee.png)\n\nProducts → **Product name** + **Price (RM)**, then switch **Type** from *One-time* to\n**Subscription**. The row expands into the recurring controls: **Billing period** (*Monthly* here),\n**Trial days**, **Billing count** (*Infinite (recurring)* or *Fixed N times*), and **Next charge\ndate** (*From the day it succeeds (drifts if late)*, or keep the original dates and collect or skip\nmissed months).\n\n## 2. Add the one-time registration fee\n\n![Products list with the One-time and Subscription · Monthly rows marked](02-two-fees.png)\n\nAdd the registration fee with **Type** left on *One-time*. The list now shows the two shapes side by\nside: **One-time** RM50.00 and **Subscription · Monthly** RM150.00. That difference is the whole\ntuition story, one charge to confirm the seat and one charge that repeats every month.\n\n## 3. Start from the Tuition Class Registration template\n\n![Template gallery with the Tuition Class Registration card's Use template button boxed](03-tuition-template.png)\n\nForms → **New form** → **Tuition Class Registration** → **Use template**. The template arrives as a\npayment form with a Section intro (*pay the registration fee to confirm your seat*) and one Product\nblock. It also carries **Student name**, **Parent email**, **Parent phone** and **Subject / level**.\n\n## 4. Bind both fees to the form\n\n![Builder with the Registration fee and Yuran bulanan blocks and the Product dropdown boxed](04-bind-fees.png)\n\nSelect the Product block, then **BLOCK SETTINGS → Product** picks the fee it sells, here\n*Yuran pendaftaran (sekali)*. Press **Product** once more under *PRICING FIELDS* and bind the second\nblock to *Yuran bulanan — Kelas Matematik*. **Text / label** is what the parent reads, so name the\nblocks after the fee, not after the template.\n\n## 5. Publish and copy the link\n\n![Builder with the live banner and the Copy link button boxed](05-publish.png)\n\n**Publish** puts the form live and the green banner hands you the address\n(`…/f/tuition-class-registration`). **Copy link** is what goes into the class WhatsApp group or the\nschool's page.\n\n## 6. What the parent sees\n\n![Public registration page with the one-time fee and the monthly fee marked](06-parent-view.png)\n\nThe parent gets the intro line, the **Registration fee RM50.00** line, the student's details, the\n**Yuran bulanan — Kelas Matematik RM150.00** line, a **Total** and one **Proceed to Payment** button.\nEach fee is its own checkbox with a **Quantity**, so a family enrolling two children ticks 2 on the\nregistration fee without touching the monthly one.\n\n## Notes\n\n- **A subscription product is still just a product.** The same block can sell it from any form in the\n  workspace. The recurring behaviour comes from the product row, not from the form.\n- **Billing count decides whether it ever stops.** *Infinite (recurring)* charges until cancelled,\n  while *Fixed N times* stops after N charges, which is useful for a 10-month programme.\n- **The total is zero until a fee is ticked.** Both fee blocks start unchecked, which is why the page\n  shows **Total RM0.00** on load: tick a fee and the amount and the payment step follow.\n",
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
        "height": 900
      }
    ]
  },
  {
    "slug": "wedding-rsvp",
    "title": "Wedding page and RSVP form: setup guide",
    "summary": "A step-by-step guide to putting a wedding page and an RSVP form online with JomForm. Every screenshot below was captured from the running product; the red box marks the control you touch at that step.",
    "body": "A step-by-step guide to putting a wedding page and an RSVP form online with JomForm. Every screenshot\nbelow was captured from the running product; the red box marks the control you touch at that step.\n\n**What this does:** an RSVP collects answers, not money, so the form runs in *response* mode. The\ncouple gets a page with a title, a short invitation line and the questions they need answered: who is\ncoming, how many guests, and a message. The form has its own public link, so it can go straight into\na WhatsApp invitation, and it can sit behind a full landing page (see the Notes).\n\n## 1. Start a new form\n\n![Forms page with the New form button boxed](01-new-form.png)\n\nForms → **New form**. Every form in JomForm starts here. The list on this page holds the drafts and\nthe published forms of the workspace you are in (its name is at the top right: *Panduan Demo* here).\n\n## 2. Choose Blank form\n\n![Template gallery with the Blank form card boxed](02-blank-form.png)\n\n**New form** opens the template gallery. There is no wedding template in it, so start from\n**Blank form**. The gallery's other cards (Event Registration, Donation, F&B Order + Payment, Tuition\nClass Registration, …) are for the other showcases. A blank form gives you an empty canvas and the\nfull field palette.\n\n## 3. Set Form mode to Response form\n\n![Builder with the Form mode dialog open and the Response form option boxed](03-response-mode.png)\n\nIn the builder, the mode sits in the **top bar next to the slug**: the button reads *Payment form*,\nthe default. Press it and pick **Response form**. You do not need to select a block or open the\nsettings panel to reach it. The change asks you to confirm first, and says what it is about to do.\n\nResponse form collects responses only, with no products and no payment. Leave it on *Payment form*\nand the form will expect a product to sell, while an RSVP has nothing to sell.\n\n## 4. Add the questions and mark the required ones\n\n![Builder canvas with the RSVP blocks and the Required checkbox boxed](04-blocks-required.png)\n\nClick a field in the **palette** on the left to add it to the form. For an RSVP:\n\n| Block | Label used here | Notes |\n|---|---|---|\n| **Section** | We are getting married: join us for dinner and dancing | A heading, not a question |\n| **Name** | Full name | Required by default |\n| **Email** | Email | Required by default |\n| **Radio buttons** | Will you join us on 12 December 2026? | Edit the two choices in *Choices* |\n| **Number** | Number of guests (including you) | Optional. Only an attending guest answers it |\n| **Note** | Message to the couple | Free-form message |\n\nSelect a block and its settings appear in the right-hand panel: **Text / label**, Description,\nPlaceholder, an **Advanced** disclosure, and **Required**. Tick **Required** on the question that\nmust be answered, here the attendance radio, so the guest cannot submit without it. A *Text* block\nthat reads like a sentence offers **Treat as intro line** and becomes a heading; use **Section** for\nthat from the start.\n\n## 5. Publish\n\n![Builder toolbar with the Save & publish button boxed](05-publish.png)\n\n**Save & publish** in the top bar writes the form and puts it live in one step. Until you click it\nthe toolbar shows **• unsaved** and the form stays a draft that nobody else can open.\n\n## 6. Copy the public link\n\n![Live banner with the Copy link button boxed](06-live-link.png)\n\nThe green **Your form is live** banner confirms the publish and holds the guest URL. **Copy link**\n(or **Open form**) is what you paste into the invitation, a poster QR code, or your website.\n\n## 7. Set the theme colour\n\n![Form Settings → General with the Theme color control boxed](07-theme-colour.png)\n\nThe form's look lives in **Form Settings**, not in the builder. Open it from the form's row in Forms,\nor from the **Form Settings →** button at the bottom of the builder's right panel. Under **General**,\n**Theme color (primary)** is the colour of the page's buttons and accents, and the PREVIEW panel next\nto it shows a sample product in that colour. The same tab carries **Status**\n(*Draft / Private / Published / Unlisted*), **Logo URL (https)**, **Custom JS** and\n**Allow embedding (iframe)**. Press **Save** at the bottom and the page answers **Saved ✓**.\n\n## 8. What the guest sees\n\n![Public RSVP page with the rose theme, five questions and a Submit button](08-guest-rsvp.png)\n\nAnyone with the link gets the title, the invitation line, the five fields and one **Submit** button\nin the theme colour you chose. The labels marked required carry *(required)*, and the guest never\nsees the dashboard.\n\n## 9. Read the replies\n\n![Sales → Responses (this form) with one RSVP answer row and the Export CSV button boxed](09-responses.png)\n\nThere is no separate inbox. **Submission** at the top of Form Settings opens **Sales** filtered to\nthis form (*Responses (this form)*). Each RSVP is one row with its **Received** time, the **Buyer**'s\nname and email, the answers, and a **View** action that opens the full reply. **Export CSV**\ndownloads every reply in the filter.\n\n## Notes\n\n- **A full page in front of the form is optional.** Settings → **Storefront** turns the workspace's\n  home page into a landing page (hero, subtitle, body, image, logo and a CTA button that points at\n  the form). Use it when you want the couple's story and photos on a page of their own rather than\n  just the form. The [design-with-MCP guide](../design-with-mcp/README.md) walks that page.\n- **The slug can change without breaking invitations.** Saving a new slug moves the form to the new\n  URL, and the old address keeps working through a temporary redirect. A link already sent still\n  lands on the form.\n",
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
