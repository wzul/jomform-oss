import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Subscriptions & recurring billing — JomForm Docs",
  description:
    "How JomForm recurring billing works: billing periods, a fixed number of charges, the next-charge schedule modes, the retry ladder, failed-card recovery, and the notifications a merchant receives.",
  alternates: { canonical: "https://jomform.com/docs/subscriptions" },
};

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "how-it-works",
    title: "How a subscription is created",
    body: [
      "A product is a subscription when its type is subscription and it has a billing period. When a buyer checks out through a form containing that product, JomForm records the sale as a subscription, tokenizes the card with the gateway, and creates the subscription with the product's own period and amount. The first payment is the buyer's checkout — it counts as charge 1.",
      "From then on a background sweep runs every hour. It finds subscriptions whose next charge date has passed and charges the customer's saved card. When the gateway confirms the payment, the charge is marked paid, the next charge date advances by the billing period, and — for a product with a fixed number of charges — the subscription is completed once the last charge is paid.",
      "Recurring billing is card-based. The customer's card must be saved at checkout; this is what allows later charges to happen without the customer present.",
    ],
  },
  {
    id: "billing-period",
    title: "Billing period",
    body: [
      "Set on the product: monthly or yearly. The period decides how far the next charge date moves after each successful payment. It is read from the product at checkout and stored on the sale, so a change to the product later never alters an existing subscription's schedule.",
    ],
  },
  {
    id: "charge-count",
    title: "A fixed number of charges",
    body: [
      "By default a subscription bills indefinitely. For a fixed-term plan — three months, six sessions, a one-year course — set the number of charges on the product. The subscription is then completed automatically once that many payments have been collected, and no further charge is scheduled.",
      "The first payment is charge 1, so a three-charge product collects three payments in total: the checkout payment plus two more. A one-charge product is fully paid at checkout and completes immediately.",
    ],
  },
  {
    id: "schedule-mode",
    title: "Next charge date — what happens when a payment is late",
    body: [
      "If a charge succeeds late, where should the following charge land? This is the schedule mode, set per product, and it decides whether a missed period is still collected.",
      "From the day it succeeds (default) — the next charge is counted from the moment the payment actually went through. Simple, and right for a continuous service where the customer is paying for the period ahead. The trade-off is that a late payment shifts every later payment by the same amount.",
      "Keep original dates, collect missed months — the schedule stays on its original dates (for example the 1st of each month) and a period that was missed is still collected. Right for a fixed-term class or course where the months are fixed: a three-month plan still collects three months even if one payment failed and was retried.",
      "Keep original dates, skip missed months — the schedule stays on its original dates and a period missed during a failure is not collected. Right when the customer should not be charged for a period they did not receive — a weekly service, or a class month the customer missed.",
      "An on-time payment behaves identically in every mode. The mode only matters when something was actually missed.",
    ],
  },
  {
    id: "failed-charge",
    title: "When a charge fails",
    body: [
      "If the gateway declines a charge, JomForm retries automatically: three attempts, one a day. If they all fail the subscription becomes past due and the customer is asked to update their card. A card that is never updated results in the subscription being cancelled after a seven-day grace period.",
      "Nothing is charged while a card update is in progress.",
      "A failed charge records the reason the gateway gave, in plain language (for example an invalid or inactive card token). A charge whose confirmation never arrives is also settled — after an hour it is marked failed with that reason, so a lost gateway response can never leave a subscription silently unable to bill.",
    ],
  },
  {
    id: "recovery",
    title: "Recovering a failed subscription",
    body: [
      "Two things bring a past-due subscription back to active: a successful payment (which proves the card works again), and the merchant's retry action. Retrying re-arms the subscription and lets the next sweep charge it — the engine charges, so a retry cannot double-charge the customer.",
      "Only a past-due subscription can be retried. A cancelled subscription is never revived and a completed fixed-term plan stays completed.",
      "The customer can also update their card at any time through a secure link the merchant sends; the subscription resumes and its billing dates are unchanged.",
    ],
  },
  {
    id: "notifications",
    title: "What you are told",
    body: [
      "When a charge fails, the merchant is notified through the same per-form notification rules used for other sale events, so the alert is configured in the form's notification settings alongside everything else. The alert fires on the first failure — not on every retry — and again when the subscription becomes past due, which is the point at which someone needs to act.",
      "Without this, a card that stops working mid-subscription goes unnoticed by both merchant and customer until someone complains.",
    ],
  },
  {
    id: "managing",
    title: "Managing subscriptions",
    body: [
      "The dashboard lists every subscription with its customer, product, amount, period, status and next charge date. From there a merchant can cancel a subscription, retry a past-due one, or create a card-update link for the customer.",
      "Cancelling never revokes the customer's saved card token — the subscription can be re-activated later without the customer re-entering their card.",
      "Every one of these actions is also available to an AI agent over MCP, so a subscription can be managed from a chat or an automation as well as the dashboard.",
    ],
  },
];

export default function SubscriptionsDocsPage() {
  return (
    <>
      <h1 className="text-2xl font-semibold text-slate-900">
        Subscriptions &amp; recurring billing
      </h1>
      <p className="mt-3 text-sm leading-6 text-slate-600">
        Charge customers automatically on a schedule — monthly or yearly, for as
        long as the plan runs or for a fixed number of payments. This page covers
        the billing period, how a fixed-term plan ends, what happens when a
        payment is late or fails, and how a subscription recovers.
      </p>

      <nav className="mt-6 rounded-lg border border-slate-200 bg-slate-50 p-4">
        <div className="text-xs font-medium uppercase tracking-wide text-slate-500">
          On this page
        </div>
        <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm">
          {SECTIONS.map((s) => (
            <li key={s.id}>
              <a href={`#${s.id}`} className="text-sky-700 hover:underline">
                {s.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>

      {SECTIONS.map((s) => (
        <section key={s.id} id={s.id} className="mt-10 scroll-mt-24">
          <h2 className="text-lg font-semibold text-slate-900">{s.title}</h2>
          {s.body.map((p, i) => (
            <p key={i} className="mt-3 text-sm leading-6 text-slate-600">
              {p}
            </p>
          ))}
        </section>
      ))}

      <section className="mt-10 rounded-lg border border-amber-200 bg-amber-50 p-4">
        <h2 className="text-sm font-semibold text-amber-900">
          What recurring billing requires
        </h2>
        <ul className="mt-2 list-disc space-y-1 pl-5 text-sm leading-6 text-amber-900">
          <li>
            A card that can be charged again later. The card is saved at checkout;
            without that there is nothing to charge on the next period.
          </li>
          <li>
            Your gateway account configured with valid credentials — recurring
            charges use the same credentials as the first payment.
          </li>
        </ul>
      </section>
    </>
  );
}
