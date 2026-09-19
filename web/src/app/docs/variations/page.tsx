import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Variation products — JomForm Docs",
  description:
    "Sell one product in several options: per-variation price, stock and image, plus a per-variation subscription cycle (monthly/yearly, trial days, charge count) that falls back to the product's.",
  alternates: { canonical: "https://jomform.com/docs/variations" },
};

type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "what-is-a-variation",
    title: "What a variation is",
    body: [
      "A product is simple when it has no variations: it uses its own price, stock and images. A product is variable when it has one or more variations, and each variation then carries its own name, price, stock and image. Selling one T-shirt in sizes S, M and L, or in several colours, is a variable product with one variation per option.",
      "A variation is a real row attached to its product, not a field inside it — the same shape WooCommerce uses. That is why editing the product replaces the whole list of variations at once: whatever is in the editor when you save is the authoritative set. Removing a variation removes it; the last variation removed turns the product back into a simple product.",
      "Variations are discrete rows, not an attribute grid. There is no size × colour generator: you create each combination you actually sell as its own line, which also means each combination gets its own price, stock and image.",
    ],
  },
  {
    id: "price-stock",
    title: "Price, stock and image",
    body: [
      "For a variable product the variation's price is what the buyer pays — the product-level price is ignored. The same applies to stock: set a quantity on the variation to limit it, or leave it blank for unlimited. A variation that reaches zero stock shows as sold out on the form and is rejected at checkout, so it can never be oversold.",
      "The first image on a variation is its cover in the buyer's option list. The variation's own image is shown next to the option the buyer picks.",
    ],
  },
  {
    id: "subscription-cycle",
    title: "A subscription cycle on a variation",
    body: [
      "A subscription product can also be variable. Each variation may carry its own subscription cycle — the billing period (monthly or yearly), the trial days and the number of charges — which lets you sell \"Size M, monthly\" and \"Size L, yearly\" from a single product and have each one billed on its own terms.",
      "A field you leave empty inherits the product's value for that field. This is a real distinction, not a convenience: \u201cnot set\u201d and \u201cset to the same value as the product\u201d are different states. A variation with no cycle of its own keeps following the product, so changing the product's period later still reaches it. A variation with its own period is fixed and does not follow the product.",
      "In the editor each field starts empty with the product's value shown as a hint, and every field can be reset back to empty to start inheriting again. Nothing is pre-filled, precisely so a product-level change keeps applying to the variations that never set their own.",
    ],
  },
  {
    id: "checkout",
    title: "What happens at checkout",
    body: [
      "The buyer sees the option list for each variable product and picks one. The variation's price is charged, the variation's stock is reduced, and the subscription — when the product is a subscription — is created with the cycle of the variation the buyer chose, falling back to the product's for any field the variation does not set.",
      "The terms are recorded on the sale at checkout, so a later change to the product or the variation never alters an existing subscription's schedule. The same rule is applied by the recurring engine on every later charge, so a variation's charge count is honoured for the whole plan and not just the first payment.",
      "One purchase makes one subscription. A cart that mixes a subscription variation with one-time items bills the subscription on its own terms and charges the one-time items once.",
    ],
  },
  {
    id: "mcp",
    title: "Managing variations from an AI agent",
    body: [
      "Every variation action is available over MCP: list_product_variations reads a product's variations, create_product_variation adds one, update_product_variation edits one, and delete_product_variation removes one. The product tools (create_product, update_product) also accept a full variations list when you want to set the whole set at once.",
      "In a tool result, a variation's billing_period, trial_days and subscription_charges are present only when the variation sets them itself. Absent means it inherits the product — so an agent can always tell the two states apart. To go back to inheriting, clear the field (clear_billing_period, clear_trial_days, clear_subscription_charges) rather than setting it to the product's current value.",
    ],
  },
];

export default function VariationsDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Variation products</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Sell one product in several options — sizes, colours, formats — each with its own price,
        stock and image, and each able to carry its own subscription cycle.
      </p>

      {SECTIONS.map((s) => (
        <section key={s.id} id={s.id} className="mt-8">
          <h2 className="text-xl font-semibold text-slate-900">{s.title}</h2>
          {s.body.map((p, i) => (
            <p key={i} className="mt-3 text-sm leading-relaxed text-slate-600">
              {p}
            </p>
          ))}
        </section>
      ))}
    </div>
  );
}
