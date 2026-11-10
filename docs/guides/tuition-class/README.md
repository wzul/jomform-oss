# Tuition and class registration: setup guide

Take a one-time registration fee and a monthly class fee on one form, in six steps.

**What this does:** products carry both shapes, **One-time** and **Subscription**, and one
**Product** block per fee puts both lines in front of the parent.

## 1. Make the monthly fee a Subscription product

![Products page with a monthly class fee set to Subscription and the Billing period control boxed](01-monthly-fee.png)

Products → **Product name** + **Price (RM)** → switch **Type** to **Subscription**, then set
**Billing period** (*Monthly* here) and **Billing count** (*Infinite (recurring)*).

## 2. Add the one-time registration fee

![Products list with the One-time and Subscription · Monthly rows marked](02-two-fees.png)

Add the registration fee with **Type** left on *One-time*; the list then shows **One-time** RM50.00
beside **Subscription · Monthly** RM150.00.

## 3. Start from the Tuition Class Registration template

![Template gallery with the Tuition Class Registration card's Use template button boxed](03-tuition-template.png)

Forms → **New form** → **Tuition Class Registration** → **Use template**, a payment form with a
Section intro, one Product block and the student's details.

## 4. Bind both fees to the form

![Builder with the Registration fee and Yuran bulanan blocks and the Product dropdown boxed](04-bind-fees.png)

Press **Product** under *PRICING FIELDS* once per fee, then pick the fee in **BLOCK SETTINGS →
Product**; **Text / label** is what the parent reads, so name each block after its fee.

## 5. Publish and copy the link

![Builder with the live banner and the Copy link button boxed](05-publish.png)

**Publish** makes the form live and the banner hands you the address; **Copy link** is what goes
into the class group.

## 6. What the parent sees

![Public registration page with the one-time fee and the monthly fee marked](06-parent-view.png)

The parent sees both fees with a **Quantity** each, a **Total** and one **Proceed to Payment**
button, so a family enrolling two children ticks 2 on the registration fee alone.

## Notes

- **Billing count decides whether it ever stops.** *Infinite (recurring)* charges until cancelled,
  while *Fixed N times* stops after N charges, which suits a 10-month programme.
- **The total is zero until a fee is ticked.** Both blocks start unchecked, so tick one and the
  amount and the payment step follow.
- **Any form can sell the same product.** The recurring behaviour comes from the product row, not
  from the form.
