import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Subscriptions & recurring billing — JomForm Docs",
  description:
    "How JomForm recurring billing works: billing periods, a fixed number of charges, the next-charge schedule modes, the retry ladder, failed-card recovery, and the notifications a merchant receives.",
  path: "/docs/subscriptions",
});

// The section ids are a contract with /docs and with the dashboard's inline
// "?" links. Every field name, status name and number survives.
const SECTIONS: Section[] = [
  {
    id: "how-it-works",
    title: "How a subscription is created",
    icon: "🔁",
    body: [
      "A product is a **subscription** when its type is subscription and it carries a billing period.",
      "At checkout JomForm records the sale as a subscription, tokenizes the card with the gateway, and copies the product's period and amount.",
      "The first payment is the buyer's checkout, so it counts as **charge 1**.",
      "A background sweep runs **every hour**. It finds subscriptions whose next charge date has passed and charges the saved card.",
      "When the gateway confirms, the charge is marked paid and the next charge date advances by the billing period.",
      "A product with a fixed number of charges completes once the last charge is paid.",
      "Recurring billing is **card-based**. The card is saved at checkout, which is what allows later charges without the customer present.",
      {
        list: [
          "A card that can be charged again later. Without one there is nothing to charge on the next period.",
          "Your gateway account needs valid credentials. Recurring charges use the same ones as the first payment.",
        ],
      },
    ],
  },
  {
    id: "billing-period",
    title: "Billing period",
    icon: "📅",
    body: [
      "Set the **billing period** on the product: monthly or yearly. It decides how far the next charge date moves after each successful payment.",
      "The period is read from the product at checkout and stored on the sale.",
      "A later change to the product never alters an existing subscription's schedule.",
    ],
  },
  {
    id: "charge-count",
    title: "A fixed number of charges",
    icon: "🔢",
    body: [
      "By default a subscription bills **indefinitely**.",
      "Set the **number of charges** on the product for a fixed term: three months, six sessions, a one-year course.",
      "The subscription completes automatically once that many payments are collected, and no further charge is scheduled.",
      "The first payment is charge 1, so a three-charge product collects three payments in total.",
      "A one-charge product is fully paid at checkout and completes immediately.",
    ],
  },
  {
    id: "schedule-mode",
    title: "Next charge date and a late payment",
    icon: "📆",
    body: [
      "If a charge succeeds late, the **schedule mode** decides where the following charge lands. It is set per product.",
      {
        table: {
          label: "schedule mode comparison",
          head: ["Schedule mode", "What happens", "Right for"],
          rows: [
            [
              "From the day it succeeds (default)",
              "The next charge counts from the moment payment went through.",
              "A continuous service, paying for the period ahead.",
            ],
            [
              "Keep original dates, collect missed months",
              "The schedule holds its original dates and a missed period is still collected.",
              "A fixed-term class or course with fixed months.",
            ],
            [
              "Keep original dates, skip missed months",
              "The schedule holds its original dates and a missed period is not collected.",
              "A weekly service, or a class month the customer missed.",
            ],
          ],
        },
      },
      "An **on-time** payment behaves identically in every mode. The mode only matters when something was actually missed.",
    ],
  },
  {
    id: "failed-charge",
    title: "When a charge fails",
    icon: "💳",
    body: [
      "If the gateway declines a charge, JomForm **retries automatically**: three attempts, one a day.",
      "If all three fail the subscription becomes **past due** and the customer is asked to update their card.",
      "A card never updated means the subscription is cancelled after a **seven-day grace period**.",
      "Nothing is charged while a card update is in progress.",
      "A failed charge records the reason the gateway gave, in plain language, such as an invalid or inactive card token.",
      {
        warn: "A charge whose confirmation never arrives is settled after an hour and marked **failed**. A lost gateway response can never strand a subscription.",
      },
    ],
  },
  {
    id: "recovery",
    title: "Recovering a failed subscription",
    icon: "🔄",
    body: [
      "Two things bring a **past due** subscription back to active: a successful payment, which proves the card works again, and your retry action.",
      "Retrying re-arms the subscription and lets the next sweep charge it. The engine charges, so a retry cannot **double-charge** the customer.",
      "Only a past due subscription can be retried. A cancelled one is never revived, and a completed fixed-term plan stays completed.",
      "The customer can update their card at any time through a secure link you send. The subscription resumes with its billing dates unchanged.",
    ],
  },
  {
    id: "notifications",
    title: "What you are told",
    icon: "🔔",
    body: [
      "When a charge fails you are notified through the same per-form notification rules used for other sale events.",
      "Configure the alert in the form's **notification settings**, beside everything else.",
      "It fires on the first failure, not on every retry, and again when the subscription becomes past due.",
      "That second alert is the point at which someone needs to act.",
      {
        tip: "Without this, a card that stops working mid-subscription goes unnoticed by both merchant and customer until someone complains.",
      },
    ],
  },
  {
    id: "managing",
    title: "Managing subscriptions",
    icon: "🛠️",
    body: [
      "The dashboard lists every subscription with its customer, product, amount, period, **status** and next charge date.",
      "From there you can cancel a subscription, retry a past due one, or create a card-update link for the customer.",
      "Cancelling **never revokes the customer's saved card token**, so it can be re-activated later without asking for the card again.",
      {
        note: "Every one of these actions is also available to an AI agent over MCP.",
      },
      "A subscription can be managed from a chat or an automation as well as the dashboard.",
    ],
  },
];

export default function SubscriptionsDocsPage() {
  return (
    <DocsPage
      title="Subscriptions and recurring billing"
      lead="Charge customers automatically on a schedule, monthly or yearly, for a fixed number of payments or indefinitely."
      sections={SECTIONS}
    />
  );
}
