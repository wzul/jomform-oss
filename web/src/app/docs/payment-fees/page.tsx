import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Platform fees paid by the buyer — JomForm Docs",
  description:
    "Charge a flat platform fee per CHIP payment method. It is added to the buyer's price as its own line in the CHIP purchase, so the amount the buyer typed never changes.",
  path: "/docs/payment-fees",
});

// SHORT ON PURPOSE. Both MCP tool names, every money unit and the refund rule
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
          "**Decide what the fee is for.** It is JomForm's own charge to the buyer, added on top of the price.",
          "**Set a flat amount per method** in Settings → Payment fees. FPX B2C, FPX B2B1, Card, e-Wallet and DuitNow QR each take an amount in sen.",
          "**FPX's two products are separate rows**: FPX B2C (personal) and FPX B2B1 (corporate) can carry their own amount.",
          "**Tick the acknowledgement.** It records who accepted the disclosure and when; until then no fee reaches a buyer.",
          "**Pick which forms carry it.** A workspace-wide setting covers every payment form; a form can opt out with its fee override.",
          "**Watch what the buyer sees**: their own amount, then a **Platform fee** line, then the total.",
        ],
      },
      {
        note: "The sentence you accept is “I understand this platform fee is charged to the buyer on top of the price and is separate from the fee CHIP charges me.”",
      },
      "Units are exact: **RM1.00 = 100** sen. A fee of `100` is one ringgit on every purchase of that method.",
      "If only one method carries a fee it is **preselected**. With more than one the buyer must choose, and an order naming none is refused.",
      "Follow the [screenshot guide](/docs/guides/payment-fees): six steps from Settings to what your buyer sees.",
    ],
  },
  {
    id: "platform-fee",
    title: "How the platform fee is added",
    icon: "🧾",
    body: [
      "The CHIP purchase carries the buyer's own item **plus the platform fee as a separate product line**:",
      {
        table: {
          label: "What CHIP is asked to charge, and what the buyer typed",
          head: ["Line", "Amount"],
          rows: [
            ["Derma surau (what the buyer typed)", "RM100.00"],
            ["Platform fee (flat, per method)", "RM1.00"],
            ["Total charged", "RM101.00"],
          ],
        },
      },
      "The buyer's amount is **not grossed up and not recalculated**: it is the item line, untouched. Only the fee line is added.",
      "The fee is **flat per method, not a percentage**: it does not grow with the price and is never a markup on the buyer's own amount.",
      "On the sale and in the CSV the fee is its own amount beside the total, and **net** is what you keep after it.",
      {
        note: "A workspace that stored a percentage rate keeps being charged exactly what it was charged before, for one release. The settings read marks that row as legacy.",
      },
    ],
  },
  {
    id: "choosing-the-method",
    title: "Choosing the method (CHIP does not ask again)",
    icon: "🧭",
    body: [
      "The buyer picks FPX (personal, B2C), FPX B2B1 (corporate), DuitNow QR, card or e-wallet **on your JomForm page**, above the Pay button.",
      "The choice is carried to CHIP, and the purchase enables **only that method**; the buyer lands on CHIP's page with it preselected.",
      "FPX B2C and FPX B2B1 are **two rows with their own amount**, so the buyer pays what was shown for the product they picked.",
      "**Only the methods you enabled appear** — the rows come from Settings → CHIP → payment methods, never a fixed list.",
      "A method you did not enable is refused with a message; JomForm never falls back to another method on the buyer's behalf.",
      "The picker appears on the **menu checkout sheet** and on a payment form's last block.",
      "The lock line under the rows is literal: the card number is typed on CHIP's page, never on yours.",
    ],
  },
  {
    id: "what-lands-on-the-sale",
    title: "What lands on the sale",
    icon: "📋",
    body: [
      "**The method cannot be switched** at the CHIP page: the purchase whitelists the chosen one, so an FPX buyer cannot move to Card.",
      "The charged amount is the server's total — items plus the platform fee — so the buyer cannot pay a different amount.",
      "The fee and the model are **frozen** on the sale: `fee_sen` is the charge, `platform_fee_sen` the flat line, `fee_model` names the model.",
      "**The server re-checks everything.** The method and total from the browser are claims; a mismatch is refused with HTTP 400.",
      "A refusal asks the buyer to reload; it never silently charges another amount, even after a price or fee change.",
      "On the sale the total is items + shipping − discount + platform fee, so the fee is never folded into an item's price.",
      "The receipt and its PDF show it as its own line: **Platform fee, RM 1.00**.",
      "CHIP **does not return the platform fee**, and a refund is never recalculated: a full refund returns what the buyer paid.",
    ],
  },
  {
    id: "reporting",
    title: "What the dashboard counts",
    icon: "📊",
    body: [
      "Your buyer pays the platform fee, so it is not your income. The revenue stat, the chart, the per-form totals and the CSV count **what you keep**.",
      "`revenue_sen` is what buyers paid, `fees_sen` is the platform fee inside it, and `net_revenue_sen` is **what you keep**.",
      "The CSV writes **total**, **fee**, **platform_fee** and **net** side by side; a refund keeps its own column.",
      "Product revenue is the sum of item prices, so the fee was never part of it.",
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
      "The fee is **part of the amount charged**, never hidden inside the total: it is its own line on the receipt and on the CHIP purchase.",
      "It is recorded as `platform_fee_sen` and `fee_sen`, **separate** from subtotal, shipping and discount.",
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
      "The same settings are on the **JomForm MCP**, so an agent can read them or set them for you. Money is integer sen.",
      "Enabling a method needs the **acknowledgement** in the same call, exactly as the settings page does.",
      {
        table: {
          label: "Payment fee MCP tools",
          head: ["Tool", "Arguments", "What it does"],
          rows: [
            [
              "`get_payment_fees`",
              "`workspace_id` (optional)",
              "Reads every method: the platform amount in sen, whether the row is still a legacy percentage, and who accepted the acknowledgement.",
            ],
            [
              "`set_payment_fees`",
              "`rates`",
              "Replaces the whole set; a method the payload omits is deleted. `platform_fee_sen` sets the flat amount, and omitting it keeps the stored one.",
            ],
          ],
        },
      },
      "The same settings are on REST at `GET`/`PUT /v1/rest/settings/payment-fees`, with the per-form switch-off at `PUT /v1/rest/forms/{id}/fee-override`.",
    ],
  },
];

export default function PaymentFeesDocsPage() {
  return (
    <DocsPage
      title="Platform fees paid by the buyer"
      lead="One flat amount per payment method, added to the buyer's price as its own line, so the price they typed is the price they see before paying."
      sections={SECTIONS}
    />
  );
}
