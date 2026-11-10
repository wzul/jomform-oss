# Payment fees: merchant setup guide

Pass the CHIP processing fee to your buyer in six steps, at the rate you set per payment method.

**What this does:** JomForm adds your acquirer's fee on top of the price and grosses the total up,
so you still receive the full asking amount.

## 1. Open Settings → Payment fees

![Settings tabs with the Payment fees tab boxed](01-settings-payment-fees-tab.png)

Settings → **Payment fees**, the tab between *e-Invoice* and *Webhooks*. Only workspace admins and
owners see it.

## 2. Tick the payment methods you want to charge for

![Payment method rows with the Card checkbox boxed](02-method-rows.png)

One row per method — **FPX**, **Card**, **e-Wallet**, **DuitNow QR** — and a method with no tick is
passed through at cost.

## 3. Enter your rate

![Card row with the Percent field boxed, value 200](03-set-rate.png)

**Fixed (sen)** is a flat fee (`100` = RM1.00) and **Percent (basis points)** is a percentage
(`200` = 2%); you may use one or both.

## 4. Acknowledge the disclosure

![Acknowledgement checkbox boxed](04-acknowledge.png)

Enabling a method requires this tick, which records that the fee is passed to the buyer and who
acknowledged it.

## 5. Save

![Saved confirmation and the Card 2.00% rate label](05-saved.png)

Saving is immediate: the green **Payment fees saved.** banner confirms it and the row reads
**Card 2.00%**.

## 6. What your buyer sees

![Buyer page with the payment-method picker boxed](06-buyer-sees-fee.png)

The buyer picks the method above the Pay button, and the fee line, the total and the button follow
that choice. With FPX and Card both set to 2.00% and RM100.00 on the form:

| Line | Amount |
|---|---|
| Subtotal | RM100.00 |
| Processing fee — FPX | RM2.04 |
| **Total** | **RM102.04** |

The fee is **RM2.04, not RM2.00**: a flat 2% would leave you with RM99.96 after CHIP takes its 2%,
so the total is solved backwards and you receive the full RM100.00.

## Notes

- The rate is snapshotted on each sale, so a later change does not alter past sales or receipts.
- SST is not added on top of these rates in this version; if your acquirer quotes a rate inclusive
  of SST, enter the all-in figure.
- Card acceptance is at your own risk as the merchant, and the acknowledgement in step 4 records it.
