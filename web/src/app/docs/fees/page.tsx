import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Connect CHIP and set your fee — JomForm Docs",
  description:
    "Connect your CHIP keys, switch on the payment methods you accept, and charge a flat platform fee per method as its own line in the CHIP purchase. The price the buyer typed never changes.",
  path: "/docs/fees",
});

// Card t_1e4c1bba: this page is where the retired private surau guide's link now lands
// (/panduan/surau 301s here, see web/next.config.mjs). It is the TASK page — connect, enable,
// set the fee, check what the buyer sees — and it links to /docs/payment-fees for the
// reference half (reporting, refunds, e-Invoice, the MCP tools). One destination per job: a
// second page explaining the same fee model would be the duplication the /docs index exists
// to prevent.
//
// The prose follows docsStyle.test.ts: one emoji per section, one bold term per section, no
// string over 30 words, no section over 8 blocks.
const SECTIONS: Section[] = [
  {
    id: "connect-chip",
    title: "Connect your CHIP account",
    icon: "🔑",
    body: [
      {
        steps: [
          "Open **Settings → CHIP** in your dashboard.",
          "Copy the **Brand ID** and **Secret Key** from the CHIP Merchant Portal, under Developers → Brands.",
          "Paste both and **Save**. JomForm keeps them encrypted and shows only a masked key afterwards.",
        ],
      },
      {
        note: "The card reads **verified** once JomForm reaches CHIP with those keys. Replacing a Secret Key is a paste, not a reinstall.",
      },
    ],
  },
  {
    id: "methods",
    title: "Switch on the methods you accept",
    icon: "🧾",
    body: [
      "In **Settings → CHIP**, tick what your CHIP account offers: **FPX, Card, e-Wallet and DuitNow QR**.",
      "**Only ticked methods reach a buyer.** A method you did not enable is refused with a message, never swapped for another.",
      {
        note: "**FPX has two variants**: FPX is the personal (B2C) online-banking flow and FPX B2B1 the corporate one. They can carry different fees.",
      },
    ],
  },
  {
    id: "platform-fee",
    title: "Put a platform fee on each method",
    icon: "💳",
    body: [
      "Open **Settings → Payment fees**. Each method takes a flat amount in sen, not a percentage: 100 is RM1.00.",
      "**The buyer's price never changes.** The fee is added as its own line, so a RM100.00 donation stays RM100.00.",
      "FPX can carry **two amounts**, one for B2C and one for B2B1 — for example RM1.00 and RM2.00.",
      "Tick the **acknowledgement** once. It records who accepted and when, and no fee reaches a buyer before it.",
      "A workspace setting covers every payment form. A single form can opt out with its **fee override**.",
    ],
  },
  {
    id: "what-it-costs",
    title: "What each method costs",
    icon: "🏷️",
    body: [
      "The buyer pays their own amount plus the fee you set for the method they pick.",
      {
        table: {
          label: "What the buyer pays, per method",
          head: ["Method", "Buyer pays"],
          rows: [
            ["FPX (B2C)", "Amount + your FPX fee"],
            ["FPX B2B1", "Amount + your B2B1 fee"],
            ["Card", "Amount + your card fee"],
            ["e-Wallet", "Amount + your e-wallet fee"],
            ["DuitNow QR", "Amount + your QR fee"],
          ],
        },
      },
      "**CHIP also charges you its own MDR** on every sale. That is your cost, separate from the platform fee, and JomForm never adds it for you.",
    ],
  },
  {
    id: "worked-example",
    title: "Worked example",
    icon: "🧮",
    body: [
      "A donor gives **RM100.00** and pays by FPX, where your platform fee is **RM1.00**.",
      {
        table: {
          label: "An RM100.00 donation with a RM1.00 platform fee",
          head: ["Line", "Amount"],
          rows: [
            ["Derma (what the donor typed)", "RM100.00"],
            ["Platform fee — FPX", "RM1.00"],
            ["**Total charged**", "**RM101.00**"],
          ],
        },
      },
      "The donation is **not grossed up**. There is no percentage and no recalculation; the item line is exactly what the donor typed.",
      "With **RM2.00** on FPX B2B1 the same donation is RM102.00. Card, e-wallet and DuitNow QR work the same way.",
    ],
  },
  {
    id: "what-you-keep",
    title: "What you keep",
    icon: "📊",
    body: [
      "The sale records the total and the **platform fee** beside it, so net is the total minus that fee.",
      "The receipt and its PDF show the fee as its own line: **Platform fee, RM 1.00**.",
      "Refunds are never recalculated: a full refund returns what the buyer paid, fee included.",
      "The full reference — reporting, e-Invoice and the MCP tools — is on [platform fees](/docs/payment-fees).",
    ],
  },
];

export default function FeesDocsPage() {
  return (
    <DocsPage
      title="Connect CHIP and set your fee"
      lead="Connect your CHIP keys, switch on the methods you accept, and charge a flat platform fee per method as its own line in the purchase."
      sections={SECTIONS}
    />
  );
}
