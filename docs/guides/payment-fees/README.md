# Payment fees: merchant setup guide

A step-by-step guide to passing the CHIP processing fee to the buyer. Every screenshot below was
captured from the running product; the red box marks the control you touch at that step.

**What this does:** you set your acquirer rate per payment method. JomForm adds the fee on top of the
price and grosses the total up, so you still receive the full asking amount. The buyer sees the fee,
the method and the final total before paying.

## 1. Open Settings → Payment fees

![Settings tabs with the Payment fees tab boxed](01-settings-payment-fees-tab.png)

Settings is in the left sidebar. The **Payment fees** tab sits between *e-Invoice* and *Webhooks*.
Only workspace admins and owners see it.

## 2. Tick the payment methods you want to charge for

![Payment method rows with the Card checkbox boxed](02-method-rows.png)

One row per method: **FPX**, **Card**, **e-Wallet**, **DuitNow QR**. Ticking a row is what enables the
fee for that method; a method with no tick is passed through at cost.

## 3. Enter your rate

![Card row with the Percent field boxed, value 200](03-set-rate.png)

- **Fixed (sen)**: a flat fee in sen. `100` = RM1.00.
- **Percent (basis points)**: `100` = 1%, `200` = 2%, `140` = 1.4%.

You may use one or both. The rate you type is the rate the buyer pays; the total is grossed up so the
fee comes out of the buyer's side, not yours.

## 4. Acknowledge the disclosure

![Acknowledgement checkbox boxed](04-acknowledge.png)

Enabling a method requires this tick. It records that the fee is passed to the buyer and is subject
to acquirer and card-issuer rules. JomForm stores who acknowledged and when.

## 5. Save

![Saved confirmation and the Card 2.00% rate label](05-saved.png)

Saving is immediate: the green **Payment fees saved.** banner confirms it, and the rate label on the
row (`Card 2.00%`) is the rate your buyers will now be charged. No reload is needed.

## 6. What your buyer sees

![Buyer page with the payment-method picker and the fee line boxed](06-buyer-sees-fee.png)

The buyer picks **how** they want to pay on your own page, above the Pay button — FPX, DuitNow QR,
card or e-Wallet. The picker lists only the methods your workspace enables, preselects the first one,
and the fee line, the total and the button all follow that choice: switch the method and they re-price
on the spot. This example has FPX and Card both set to 2.00%, so FPX (the first row) shows the fee.
With RM100.00 at 2%:

| Line | Amount |
|---|---|
| Subtotal | RM100.00 |
| Processing fee — FPX | RM2.04 |
| **Total** | **RM102.04** |

The button reads **Pay RM102.04** with **via FPX** under it. The same pick is what CHIP is given, so
CHIP's own method screen is skipped and the buyer lands on the method they chose.

The fee is **RM2.04, not RM2.00**. Charging a flat 2% of RM100.00 would leave you with RM99.96 after
CHIP takes its 2%, so the total is solved backwards (`100 / (1 − 0.02) = 102.04`). You receive the
full RM100.00; the buyer covers the RM2.04. Rounding is capped so you never lose more than 1 sen.

## Notes

- The rate is snapshotted on each sale: changing your rate later does not alter past sales or receipts.
- SST is not added on top of these rates in this version. If your acquirer quotes a rate inclusive of
  SST, enter the all-in figure.
- Card acceptance is at your own risk as the merchant; the acknowledgement in step 4 records that.
