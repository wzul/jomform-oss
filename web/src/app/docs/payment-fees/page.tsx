import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

export const metadata: Metadata = docsMetadata({
  title: "Processing fees paid by the buyer — JomForm Docs",
  description:
    "Set your own acquirer rate per CHIP payment method (fixed sen and/or basis points). The fee is grossed up so you receive the full asking amount, and the buyer sees the method, the fee and the final total before paying.",
  path: "/docs/payment-fees",
});

// Docs for the buyer-borne processing fee. Static English like every other
// /docs guide (see the note in customize/page.tsx). It names both MCP tools,
// because api/internal/mcpsrv's parity guard requires every registered tool to
// appear on a docs page, and it states the disclosure rule the feature is built
// around: the price shown is the price paid.
//
// WHAT IT DOCUMENTS, precisely:
//   * the per-method rate model (one rate per CHIP payment method, fixed sen
//     and/or basis points, 100 = 1%) and that you type your OWN acquirer rate;
//   * the gross-up: the buyer pays the fee AND the fee on the fee, so you are at
//     most one sen short — never the naive `price + price × rate`, which loses
//     p² × A (10 sen on RM 1,000 at 1%);
//   * the buyer's side: they choose the method on the JomForm page, the fee, the
//     method and the final total are shown BEFORE they pay, and the payment is
//     restricted to that one method so it cannot be swapped;
//   * the acknowledgement sentence that must be ticked before a fee reaches a
//     buyer, and that it is recorded with who and when;
//   * the per-form switch-off, the fee snapshot on the sale, the receipt line,
//     and that a refund never RECALCULATES the fee (a full refund returns what
//     the buyer paid, fee included; the sale keeps the fee you bore).

const STEPS: { title: string; body: string }[] = [
  {
    title: "1. Decide whether you may pass the fee on",
    body: "Passing a card surcharge to the buyer is restricted by card-network and acquirer rules, and by consumer law in Malaysia the price shown must be the price paid. That is your call as the merchant: JomForm does not block the feature, it makes the disclosure explicit. The acknowledgement below is the record that you took that decision.",
  },
  {
    title: "2. Type your own rate per payment method",
    body: "Open Settings → Payment fees. Every CHIP payment method has its own row: FPX, Card, e-Wallet and DuitNow QR. Each accepts a fixed amount in sen (RM1.00 = 100) and/or a percentage in basis points (100 = 1%, 140 = 1.4%, 200 = 2%). Both are optional and can be combined: RM1 fixed plus 1.4% is a valid rate. The rate is the one YOUR acquirer charges you — JomForm does not guess it, and nothing is filled in for you.",
  },
  {
    title: "3. Tick the acknowledgement",
    body: "\"I understand this processing fee is passed on to the buyer and is subject to acquirer/card-issuer rules.\" The sentence is served by the API and recorded with your account, the time, and who accepted it. Until it is ticked, no fee reaches a buyer even if the rates are saved — the checkout simply charges the plain price.",
  },
  {
    title: "4. Pick which forms carry the fee",
    body: "A workspace-wide rate applies to every published payment form. A single form can opt out: set its fee override to off (Form Settings, or PUT /v1/rest/forms/{id}/fee-override), and that form charges no fee while the rest of the workspace keeps its rates. The override is per form, not per buyer, so the same form behaves the same way for everyone.",
  },
  {
    title: "5. Watch what the buyer sees",
    body: "On the checkout page the buyer chooses the payment method. The page shows the items subtotal, a separate processing-fee row naming the method and its rate, and the final total — before the Proceed to Payment button. If only one method carries a rate it is preselected, so there is no extra click. With more than one, the buyer must choose, and an order that names none is refused rather than priced with a guess.",
  },
];

const WHATLANDS: { title: string; body: string }[] = [
  {
    title: "The total is grossed up, not added to",
    body: "CHIP charges its fee on the amount the buyer actually pays, not on your price. Adding \"price + price × rate\" therefore leaves you short by the fee charged on the fee itself: 10 sen on RM1,000 at 1%. JomForm solves it the other way round — it finds the smallest total where, after the fee is taken, you still receive your asking price. RM1,000 at 1% is charged to the buyer as RM1,010.10 (fee RM10.10), and you receive RM1,000.00. The most you can be short is one sen, from integer rounding.",
  },
  {
    title: "The method cannot be switched at the CHIP page",
    body: "Because each method has its own rate, the payment is created for the one method the buyer chose: the CHIP purchase whitelists that method only, and its amount is the server's grossed-up total. A buyer who chose (say) FPX at RM1 cannot change to Card at 2% after seeing the charge, and cannot pay an amount other than the one the JomForm page displayed.",
  },
  {
    title: "The server re-checks everything",
    body: "The method and the total from the browser are treated as claims, never as inputs. The server resolves the rate from your stored settings, recomputes the total, and refuses (HTTP 400) an order whose method is not active or whose total does not match what the page showed — for example because you edited a price or a rate while the buyer was filling the form. A refusal asks the buyer to reload; it never silently charges a different amount.",
  },
  {
    title: "The fee is frozen on the sale",
    body: "When the sale is created it records the fee, the method and the rate that applied. Changing your rates later does not rewrite a past sale, and a receipt reprinted months later prints the rate the buyer actually paid. On the sale the total is items subtotal + shipping − discount + processing fee: the fee is its own component, never folded into an item's price, and the receipt and PDF show it as its own line (\"Processing fee — Card 1.00%: RM 10.10\").",
  },
  {
    title: "Refunds never recalculate the fee",
    body: "CHIP does not return the processing fee it charged you, and JomForm never recalculates, re-estimates or adds back a fee on a refund. A full refund returns the amount the buyer actually paid — which includes the fee — so the buyer is made whole and you still carry CHIP's charge; a partial refund returns the amount you choose. Either way the sale keeps showing the fee you bore at the rate in force when the buyer paid, and a later rate change never rewrites it. Whether a full refund should include the fee is your refund policy's decision; the system does not deduct it silently.",
  },
  {
    title: "e-Invoice",
    body: "The processing fee is part of the amount charged, so it is never hidden inside the total: it is its own line on the receipt and on the receipt PDF, alongside the frozen rate. JomForm records the fee as a distinct sale component (fee_sen, separate from subtotal / shipping / discount), so an LHDN line-item document can carry it without reshaping past records. SST is not calculated for this fee in the current release.",
  },
];

const TOOLS: { name: string; purpose: string }[] = [
  {
    name: "get_payment_fees",
    purpose:
      "Read every method's stored rate (enabled or not), the fixed amount in sen and the percentage in basis points, plus whether the account has accepted the pass-on acknowledgement and who accepted it.",
  },
  {
    name: "set_payment_fees",
    purpose:
      "Replace the whole rate set in one call. A method the payload omits is deleted. Enabling a method requires acknowledged=true — the same sentence the settings page shows — and the tool records who set it and when.",
  },
];

export default function PaymentFeesDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">
        Processing fees paid by the buyer
      </h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        JomForm can pass the CHIP processing fee to the buyer, with one rate per payment method
        that you set yourself. The buyer sees the method, the fee and the final total before
        paying, and the amount is calculated so that you still receive the price you asked for —
        at most one sen short, from rounding. This page documents how the rate works, what the
        buyer sees, and what lands on the sale.
      </p>

      {/* Card t_7b9f6d18: the screenshot walk-through for this feature already existed at
          docs/guides/payment-fees and was reachable from nowhere. This is the prose surface it
          belongs to, so the link lives here (the landing page's showcase is a different
          feature set and does not carry a payment-fees card). */}
      <p className="mt-3 text-sm text-slate-600">
        <a
          href="/docs/guides/payment-fees"
          className="underline decoration-dotted underline-offset-2 hover:text-slate-900"
        >
          Follow the screenshot guide: six steps from Settings to what your buyer sees →
        </a>
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Setting it up</h2>
      <div className="mt-4 space-y-5">
        {STEPS.map((s) => (
          <section key={s.title}>
            <h3 className="text-base font-semibold text-slate-900">{s.title}</h3>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{s.body}</p>
          </section>
        ))}
      </div>

      <h2 id="gross-up" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Why the total is not &ldquo;price plus percentage&rdquo;
      </h2>
      {/* The scroller is a tab stop so a keyboard user can reach the last
          column, and a named region so a screen reader announces what scrolled
          instead of a bare "group". Same shape as every other docs table. */}
      <div
        className="mt-3 overflow-x-auto"
        role="region"
        aria-label="Gross-up worked example at RM1,000 and 1%"
        tabIndex={0}
      >
        <table className="w-full min-w-[32rem] text-left text-sm text-slate-600">
          <thead>
            <tr className="border-b border-slate-200 text-slate-900">
              <th className="py-2 pr-4 font-semibold">RM1,000 at 1%</th>
              <th className="py-2 pr-4 font-semibold">Total charged</th>
              <th className="py-2 pr-4 font-semibold">Fee</th>
              <th className="py-2 font-semibold">You receive</th>
            </tr>
          </thead>
          <tbody>
            <tr className="border-b border-slate-100">
              <td className="py-2 pr-4">naive: price + price × rate</td>
              <td className="py-2 pr-4 font-mono">RM1,010.00</td>
              <td className="py-2 pr-4 font-mono">RM10.10</td>
              <td className="py-2 font-mono text-red-600">RM999.90 — 10 sen short</td>
            </tr>
            <tr>
              <td className="py-2 pr-4">JomForm gross-up</td>
              <td className="py-2 pr-4 font-mono">RM1,010.10</td>
              <td className="py-2 pr-4 font-mono">RM10.10</td>
              <td className="py-2 font-mono text-emerald-700">RM1,000.00</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The rule is the smallest total where the fee taken from it still leaves your asking price.
        It holds for every combination of fixed amount, percentage and price, and never leaves you
        more than one sen short.
      </p>

      <h2 id="what-lands-on-the-sale" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        What lands on the sale
      </h2>
      <div className="mt-4 space-y-5">
        {WHATLANDS.map((s) => (
          <section key={s.title}>
            <h3 className="text-base font-semibold text-slate-900">{s.title}</h3>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{s.body}</p>
          </section>
        ))}
      </div>

      <h2 id="mcp" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Managing it with an agent (MCP)
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The same rates are available over the JomForm MCP, so an agent can read them or set them
        for you. Money is integer sen and the percentage is basis points (100 = 1%); enabling a
        method requires the acknowledgement in the same call, exactly as the settings page does.
      </p>
      <div className="mt-4 space-y-4">
        {TOOLS.map((t) => (
          <section key={t.name}>
            <h3 className="font-mono text-sm font-semibold text-slate-900">{t.name}</h3>
            <p className="mt-1 text-sm leading-relaxed text-slate-600">{t.purpose}</p>
          </section>
        ))}
      </div>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        The same settings are on the REST API as{" "}
        <span className="font-mono text-xs [overflow-wrap:anywhere]">GET /v1/rest/settings/payment-fees</span> and{" "}
        <span className="font-mono text-xs [overflow-wrap:anywhere]">PUT /v1/rest/settings/payment-fees</span>, with the
        per-form switch-off at{" "}
        <span className="font-mono text-xs [overflow-wrap:anywhere]">PUT /v1/rest/forms/&#123;id&#125;/fee-override</span>.
      </p>
    </div>
  );
}
