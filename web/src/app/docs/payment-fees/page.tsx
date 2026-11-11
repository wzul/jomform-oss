import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Processing fees paid by the buyer — JomForm Docs",
  description:
    "Set your own acquirer rate per CHIP payment method (fixed sen and/or basis points). The fee is grossed up so you receive the full asking amount, and the buyer sees the method, the fee and the final total before paying.",
  path: "/docs/payment-fees",
});

// SHORT ON PURPOSE. Both MCP tool names, every rate unit and the refund rule
// survive, because /docs is what a developer greps and what the MCP parity
// guard reads. The section ids are a contract with /docs, so they do not move.
const SECTIONS: Section[] = [
  {
    id: "setup",
    title: "Setting it up",
    icon: "💳",
    body: [
      {
        steps: [
          "**Decide whether you may pass the fee on**. Card-network, acquirer and consumer-law rules apply, and the price shown must be the price paid. JomForm does not block it.",
          "**Type your own rate per method** in Settings → Payment fees: FPX B2C, FPX B2B1, Card, e-Wallet and DuitNow QR. FPX's two products are separate rows.",
          "**Tick the acknowledgement.** It is recorded with your account, the time and who accepted. Until then no fee reaches a buyer, even with rates saved.",
          "**Pick which forms carry it.** A workspace-wide rate applies to every published payment form; a single form can opt out with its fee override.",
          "**Watch what the buyer sees**: the subtotal, a fee row naming the method and its rate, and the final total, all before Proceed to Payment.",
        ],
      },
      {
        note: "The sentence you accept is “I understand this processing fee is passed on to the buyer and is subject to acquirer/card-issuer rules.” It is served by the API.",
      },
      "Units are exact: **RM1.00 = 100** sen, and 100 basis points is 1%, 140 is 1.4%, 200 is 2%. A fixed amount and a percentage can be combined.",
      "The rate is the one **your acquirer charges you**. JomForm does not guess it, and nothing is filled in for you.",
      "Set the override per form in Form Settings or `PUT /v1/rest/forms/{id}/fee-override`. It applies per form, not per buyer, so the form behaves the same for everyone.",
      "If only one method carries a rate it is **preselected**. With more than one the buyer must choose, and an order naming none is refused rather than guessed.",
      "Follow the [screenshot guide](/docs/guides/payment-fees): six steps from Settings to what your buyer sees.",
    ],
  },
  {
    id: "gross-up",
    title: "Why the total is not price plus percentage",
    icon: "🧮",
    body: [
      "CHIP charges its fee on the amount the buyer actually pays, not on your price. Adding price and percentage leaves you short by the fee on the fee.",
      "The total is **grossed up**: JomForm finds the smallest total where, after the fee is taken, you still receive your asking price.",
      {
        table: {
          label: "Gross-up worked example at RM1,000 and 1%",
          head: ["RM1,000 at 1%", "Total charged", "Fee", "You receive"],
          rows: [
            ["Naive: price + price × rate", "RM1,010.00", "RM10.10", "RM999.90, 10 sen short"],
            ["JomForm gross-up", "RM1,010.10", "RM10.10", "RM1,000.00"],
          ],
        },
      },
      "**RM1,000 at 1%** is charged to the buyer as RM1,010.10, a fee of RM10.10, and you receive RM1,000.00.",
      "The most you can be **short is one sen**, from integer rounding, and the rule holds for every fixed amount, percentage and price.",
    ],
  },
  {
    id: "choosing-the-method",
    title: "Choosing the method (CHIP does not ask again)",
    icon: "🧭",
    body: [
      "The buyer picks FPX (personal), FPX B2B1 (corporate), DuitNow QR, card or e-wallet **on your JomForm page**. The Pay button then names the pick.",
      "That choice is carried to CHIP. The purchase is created with **only that method enabled**, and the buyer lands on CHIP's page with it pre-selected.",
      "CHIP therefore does not show its own method screen. One question, asked once.",
      "**Only the methods you enabled appear.** Rows come from Settings → CHIP: FPX B2C and FPX B2B1 are separate rows with their own fees.",
      "A method you did not enable is refused with a message. JomForm never falls back to another method on the buyer's behalf, because that would charge the wrong way.",
      "The picker appears on both buyer surfaces: the **menu checkout sheet** and a **payment form's** last block.",
      "A workspace with no method JomForm can label shows no picker, and CHIP's page asks.",
      "The lock line under the rows is literal: the card number is typed on CHIP's page, never on yours, and JomForm stores no card data.",
    ],
  },
  {
    id: "what-lands-on-the-sale",
    title: "What lands on the sale",
    icon: "🧾",
    body: [
      "**The method cannot be switched** at the CHIP page: the purchase whitelists the chosen one, so an FPX buyer at RM1 cannot move to Card at 2%.",
      "The amount is the server's **grossed-up total**, so the buyer cannot pay an amount other than the one the page displayed.",
      "The fee, method and rate are **frozen** on the sale; a later rate change rewrites neither it nor a reprint. The sale keeps the fee you bore.",
      "**The server re-checks everything.** The method and total from the browser are claims: it resolves the rate, recomputes the total, and refuses a mismatch with HTTP 400.",
      "A refusal asks the buyer to reload. It never silently charges another amount, even after you edit a price or a rate.",
      "On the sale the total is items subtotal + shipping − discount + processing fee, so the fee is its own component, never folded into an item's price.",
      "The receipt and its PDF show it as its own line: **Processing fee, Card 1.00%, RM 10.10**.",
      "CHIP **does not return the processing fee**, and a refund is never recalculated: a full refund returns what the buyer paid, fee included; a partial refund returns what you pick.",
    ],
  },
  {
    id: "reporting",
    title: "What the dashboard counts",
    icon: "📊",
    body: [
      "Your buyer pays the fee, so it is not your income. The dashboard's revenue stat, its 14-day chart, the per-form totals and the CSV all count **what you keep**.",
      "`revenue_sen` is what buyers paid, `fees_sen` is the fee inside it, and `net_revenue_sen` is what you keep. Integer sen, on REST and on the MCP tools.",
      "The CSV writes **total**, **fee** and **net** side by side. A refund keeps its own column, so `net` is the payment's net, not a post-refund balance.",
      "Product revenue is the sum of item prices, so the fee was never part of it and needs no subtraction.",
      {
        note: "A workspace whose buyers never bore a fee sees exactly the numbers it saw before.",
      },
    ],
  },
  {
    id: "einvoice",
    title: "e-Invoice",
    icon: "📄",
    body: [
      "The fee is **part of the amount charged**, never hidden inside the total: it is its own line on the receipt and its PDF.",
      "It is recorded as `fee_sen`, **separate** from subtotal, shipping and discount, so an LHDN line-item document can carry it without reshaping past records.",
      {
        note: "SST is not calculated for this fee in the current release.",
      },
    ],
  },
  {
    id: "mcp",
    title: "Managing it with an agent (MCP)",
    icon: "🤖",
    body: [
      "The same rates are on the **JomForm MCP**, so an agent can read them or set them for you. Money is integer sen and the percentage is basis points.",
      "Enabling a method needs the **acknowledgement** in the same call, exactly as the settings page does.",
      {
        table: {
          label: "Payment fee MCP tools",
          head: ["Tool", "Arguments", "What it does"],
          rows: [
            [
              "`get_payment_fees`",
              "`workspace_id` (optional)",
              "Reads every method's stored rate, enabled or not: the fixed amount in sen, the percentage in basis points, and whether the pass-on acknowledgement was accepted, and by whom.",
            ],
            [
              "`set_payment_fees`",
              "`rates`",
              "Replaces the whole rate set in one call, and a method the payload omits is deleted. Enabling one requires acknowledged=true, and the tool records who set it and when.",
            ],
          ],
        },
      },
      "The same settings are on the REST API as `GET /v1/rest/settings/payment-fees` and `PUT /v1/rest/settings/payment-fees`, with the per-form switch-off at `PUT /v1/rest/forms/{id}/fee-override`.",
    ],
  },
];

export default function PaymentFeesDocsPage() {
  return (
    <DocsPage
      title="Processing fees paid by the buyer"
      lead="One rate per payment method that you set yourself, so you receive the price you asked for and the buyer sees the fee before paying."
      sections={SECTIONS}
    />
  );
}
