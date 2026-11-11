# Collect donations: setup guide

Take donations with a JomForm payment form in eight steps, from your CHIP keys to the donor page.

**What this does:** a donation is a normal payment form and an amount is a **product** — either
fixed tiers, or one *Pay what you want* product the donor prices.

## 1. Connect CHIP Collect in Settings

![Settings with the CHIP tab open and the Brand ID field boxed](01-settings-chip.png)

Settings → **CHIP**: paste the **Brand ID** and **Secret Key** from the CHIP Merchant Portal, and save.
There is no **Mode** to pick — the key itself decides whether a payment is a test — and JomForm checks the
pair with CHIP before storing it. Nothing can be charged until this is done.

## 2. Add your first donation amount in Products

![Products page with the add row filled in and the Add product button boxed](02-products-add.png)

Products → name it **Donation RM10**, set the price **10.00**, leave **Type** on *One-time*, then
**Add product**.

## 3. Add the other amounts

![Product list with three donation tiers and the RM50 price cell boxed](03-tiers.png)

Repeat step 2 for **Donation RM50** and **Donation RM100**. Each tier is its own product, and its
price is what a donor gives when they pick it.

## 4. Start from the Donation template

![Template gallery with the Donation card's Use template button boxed](04-donation-template.png)

Forms → **New form** → **Use template** on *Donation*, which arrives as a payment form with a
product block, name, email and an optional message.

## 5. Point each Product block at one amount

![Builder with three product blocks and the block's Product dropdown boxed](05-product-block.png)

Add one **Product** block per amount (**⋯ → Duplicate** on the block the template gave you) and
choose its tier in **Product**.

## 6. Publish

![Builder toolbar with the Publish button boxed](06-publish.png)

**Publish** puts the form live; until you click it the form stays a draft nobody else can open.

## 7. Copy the donation link

![Live banner with the Copy link button boxed](07-live-link.png)

The green *Your form is live* banner holds the donor URL, and **Copy link** is what you paste into
WhatsApp, a poster QR code, or your website.

## 8. What the donor sees

![Buyer page with the RM50 tier ticked and the total reading RM50.00](08-buyer.png)

The donor ticks one amount, the total follows it, and *Quantity* lets them give a multiple of a tier
before they proceed to payment.

## Notes

- **Pay what you want is one product, not a tier.** A product of type `custom_amount`
  (*Pay what you want (donation)*) renders an amount field instead of a price; the server checks the
  amount against that product's **Minimum amount** (never below RM1.00), optional **Maximum** and
  optional **Suggested** amount before pricing the sale. The type is chosen on the product editor at
  `/products/new`.
- **Every donation is a sale from the first click.** It appears under **Sales** with a `JF-…`
  reference and moves to `paid` when CHIP confirms it; a workspace with no CHIP credentials produces
  a `failed` sale with its reason, never a free response.
