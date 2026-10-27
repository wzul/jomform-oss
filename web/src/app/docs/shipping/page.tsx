import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Shipping by zone — JomForm Docs",
  description:
    "Charge a delivery fee per delivery zone: workspace-managed zones (state, district or postcode range) with rate rows, weight bands and a free-shipping threshold, resolved from the buyer's address at checkout into sales.shipping_sen.",
  alternates: { canonical: "https://jomform.com/docs/shipping" },
};

// Docs for the managed shipping rate source (phase 1). The page is static
// English like every other /docs guide (see the note in customize/page.tsx), and
// it carries the two MCP tool names because api/internal/mcpsrv's parity guard
// requires every registered tool to appear on a docs page.
//
// WHAT IT DOCUMENTS, precisely:
//   * a shipping block's two rate SOURCES — the per-form priced list (unchanged)
//     and the managed zone set — and what each one does at checkout;
//   * how the buyer's zone is derived, and what happens when NOTHING matches
//     (a refusal, or a fallback zone, never a silent 0);
//   * the weight band and the free-shipping threshold;
//   * what lands on the sale (shipping_sen and the shipping_detail record);
//   * the phase-2 seam: live courier quoting is NOT live yet and is refused.

const STEPS: { title: string; body: string }[] = [
  {
    title: "1. Create a zone set",
    body: "Open Form Settings → Shipping. A zone set is a named group of delivery zones; a form's shipping block points at one set, so several forms can share the same rates instead of retyping them. Give it a name and tick Default if it is the set you want offered first.",
  },
  {
    title: "2. Add your zones",
    body: "A zone is a region with a name and at least one match rule: a list of states, a postcode range, or a list of districts (a city). A state entry can be any Malaysian state in any spelling — Johor, Pulau Pinang, Penang, KL, Melaka, Malacca — or the region tokens peninsular / east_malaysia, which expand to the whole group so you do not have to list thirteen states. Any ONE rule matching is enough. Give a narrow zone a LOWER priority number so a postcode exception is tested before the broad state zone it sits inside. The zone ticked as the fallback is used only when nothing else matches (see step 5).",
  },
  {
    title: "3. Price each zone",
    body: "Every zone needs at least one rate row: a price in ringgit (stored as integer sen), an optional name (Standard, Express) and an optional weight band in grams. Rows are tried in order (Sort, then cheapest), so with no weight the first row is the fee. Leave the band empty and the rate applies to every parcel; give a band and a declared parcel weight picks the row whose band contains it — a weight outside every band refuses the order rather than charging an unrelated price.",
  },
  {
    title: "4. Point a form's shipping block at the set",
    body: "In the form builder, select the shipping block and set its rate source to Managed zones, then pick the zone set. Map the buyer's address: choose which of the form's own blocks carries the state, the district/city and the postcode (add a select or text block for the state if the form has none). With no mapping the submission's client envelope is used, which is what an API or bank-transfer buyer sends. Optionally declare the parcel weight for the block.",
  },
  {
    title: "5. Decide what happens outside your zones",
    body: "A zone-based block always states an outcome for an address no zone covers. Reject is the default: the buyer gets a refusal naming the reason and no sale is created. Default uses the fallback zone's rate instead (and refuses if the set has no fallback zone). There is no third behaviour — a zone rate is never silently zero.",
  },
  {
    title: "6. Set a free-shipping threshold (optional)",
    body: "A rate row may carry a free-over amount. Once the buyer's items subtotal (line items only, before shipping) reaches it, the fee is zero and the sale records the threshold it crossed, so you can always tell a waived fee from a missing one.",
  },
];

const WHATLANDS: { title: string; body: string }[] = [
  {
    title: "The fee is shipping, not a line item",
    body: "A managed-zone fee lands in the sale's shipping amount, so sales.shipping_sen carries it and the total is items subtotal + shipping − discount — the same money rule every other sale uses. The fee is not a line item, which keeps a merchant's report of sold items accurate.",
  },
  {
    title: "Every resolved fee is recorded",
    body: "The sale stores how the fee was reached: the zone and the rate that applied, which rule matched (state, postcode, district, fallback or the buyer's own choice), the list price, and the free-shipping threshold when it was crossed. The sales detail shows it, so a delivery charge can be explained rather than guessed.",
  },
  {
    title: "The address is the price authority",
    body: "The buyer's zone is derived from the submitted address, and the form's shipping select shows its price before payment. If the buyer's choice disagrees with the address, the order is refused rather than re-priced behind their back — the checkout page only ever charges the fee the buyer was shown.",
  },
];

const TOOLS: { name: string; purpose: string }[] = [
  {
    name: "get_shipping_rates",
    purpose:
      "Read a workspace's zone sets: every zone's match rules (states, districts, postcode range, priority, fallback) and its rate rows (price in sen, weight band in grams, free-shipping threshold).",
  },
  {
    name: "set_shipping_rates",
    purpose:
      "Replace the whole configuration in one call. A set sent with its id keeps it — so every form pointing at it keeps resolving — and a set the payload omits is deleted, refused while a form still uses it.",
  },
];

export default function ShippingDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Shipping by zone</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A form can charge a delivery fee. The simplest way is a shipping block with a short priced
        list you type yourself, which is what JomForm has always had. This page documents the
        managed rate source: one set of delivery zones shared by your forms, each with its own
        price, resolved from the buyer&apos;s address at checkout.
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

      <h2 id="zones" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Zones and rate sources
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A shipping block has a rate source. With <strong>Static list</strong> — the default, and the
        shape every form published before this feature has — the block keeps its own choices and
        prices, and the chosen price is a normal line item on the sale. Nothing about that path
        changed. With <strong>Managed zones</strong> the block renders your zone set: each zone with
        the price of its first rate, and the buyer&apos;s selection is a confirmation of where they
        are. The fee is then resolved from the address they submitted.
      </p>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A zone matches on any ONE of its rules — a state (including the region tokens{" "}
        <span className="font-mono text-xs">peninsular</span> and{" "}
        <span className="font-mono text-xs">east_malaysia</span>), a numeric postcode range, or a
        district/city name. Zones are evaluated by priority (lowest first), so you can put a single
        postcode above the state zone that contains it.
      </p>

      <h2 id="no-match" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        An address outside every zone
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        This outcome is always explicit. <strong>Reject</strong> refuses the order and tells the
        buyer delivery is not available to that address — no sale is recorded.{" "}
        <strong>Default</strong> falls back to the zone ticked as the fallback in the set (and
        refuses if the set has none). A zone-based fee is never silently zero.
      </p>

      <h2 id="weights" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Weight bands and the free-shipping threshold
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A rate row can carry a minimum and a maximum weight in grams. When the block declares a
        parcel weight, the row whose band contains it is the fee; a weight outside every band is
        refused, and a zone whose rows carry no bands keeps its first rate whatever the weight is.
        A rate row can also carry a free-over amount: once the buyer&apos;s items subtotal reaches
        it the fee becomes zero, and the sale records the threshold that was crossed.
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
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A form with a managed-zone block whose set is missing, whose zone has no rate, or whose
        address cannot be resolved is refused at checkout with a message naming the reason — the
        buyer never sees a zero delivery charge nobody intended.
      </p>

      <h2 id="mcp" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Managing it with an agent (MCP)
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Both dashboard surfaces are available over the JomForm MCP, so an agent can build a
        delivery rate card for you. Money is integer sen and weight is grams; a set the payload
        omits is deleted, and that delete is refused while a form still points at it.
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
        The same configuration is on the REST API as{" "}
        <span className="font-mono text-xs">GET /v1/rest/shipping</span> and{" "}
        <span className="font-mono text-xs">PUT /v1/rest/shipping</span>.
      </p>

      <h2 id="couriers" className="mt-8 scroll-mt-6 text-xl font-semibold text-slate-900">
        Courier quotes (not live yet)
      </h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        A block&apos;s rate source also accepts the courier setting reserved for live quotes from
        J&amp;T, PosLaju, Shopee Express, Lalamove and other Malaysian couriers. That integration is
        not live: a form whose block asks for it is refused at checkout with a message saying the
        source is not supported yet, never charged a wrong amount. It needs a merchant account with
        each courier first.
      </p>
    </div>
  );
}
