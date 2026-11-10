# Club membership: setup guide

Sell a yearly or monthly membership that renews itself, in five steps.

**What this does:** a membership is a single **Subscription** product sold from any payment form;
the member's card is saved at checkout and charged again every period after that.

## 1. Create the plan as a Subscription product

![Products page with Keahlian Kelab 2026 set to Subscription, price 240.00, and the Billing period boxed on Yearly](01-plan-product.png)

Products → name it (*Keahlian Kelab 2026*), set **Price (RM)** 240.00, switch **Type** to
**Subscription** and set **Billing period** to **Yearly**.

## 2. Check the plan in the catalogue

![Products list with the Keahlian Kelab 2026 row boxed, its Type cell reading Subscription · Yearly](02-catalogue-row.png)

The row reads **Subscription · Yearly · RM240.00**, and that one row is the whole plan: a price
change is a single edit here.

## 3. Sell it from a payment form

![Builder with a PRODUCT block bound to the plan and the Product dropdown boxed](03-bind-plan.png)

Forms → **New form** → **Blank form**, add **Product** under *PRICING FIELDS* plus **Name** and
**Email**, then bind the block to the plan in **BLOCK SETTINGS → Product**.

## 4. Publish and hand out the link

![Builder with the live banner and the Copy link button boxed](04-publish.png)

**Publish** makes the form live and **Copy link** gives you one address for the whole club, with no
per-member form.

## 5. What the member does

![Member checkout page with the plan line and the renewal note marked](05-member-view.png)

The member ticks the plan, sees RM240.00, fills name and email and pays once; the card is saved
there and charged every year after it.

## Notes

- **Cancelling stops the next charge, not the card.** The saved card stays, so a member who rejoins
  does not re-enter it.
- **Renewals live under Subscriptions**, where a due charge can be retried or a plan cancelled.
- **Yearly vs monthly is a product setting.** Put both Product blocks on one form and let the member
  pick.
