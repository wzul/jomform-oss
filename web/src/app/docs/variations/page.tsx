import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Variation products — JomForm Docs",
  description:
    "Sell one product in several options: per-variation price, stock and image, plus a per-variation subscription cycle (monthly/yearly, trial days, charge count) that falls back to the product's.",
  path: "/docs/variations",
});

// Every field name, MCP tool name and behaviour survives. The section ids are a
// contract with /docs and with the dashboard's inline "?" links.
const SECTIONS: Section[] = [
  {
    id: "what-is-a-variation",
    title: "What a variation is",
    icon: "🧩",
    body: [
      "A **simple** product has no variations and uses its own price, stock and images.",
      "A **variable** product has one or more variations, and each variation carries its own name, price, stock and image.",
      "One T-shirt in sizes S, M and L, or in several colours, is a variable product with **one variation per option**.",
      "A variation is a **real row** attached to its product, not a field inside it. It keeps the same shape WooCommerce uses.",
      "Editing the product replaces the whole list at once, so whatever is in the editor when you save is the authoritative set.",
      "Removing a variation removes it. Removing the last one turns the product back into a **simple** product.",
      {
        note: "Variations are **discrete rows**, not an attribute grid. There is no size by colour generator: you create each combination you sell as its own line.",
      },
    ],
  },
  {
    id: "price-stock",
    title: "Price, stock and image",
    icon: "🏷️",
    body: [
      "For a **variable** product the variation's price is what the buyer pays. The product-level price is ignored.",
      "Set a quantity on the variation to limit stock, or leave it **blank for unlimited**.",
      "A variation that reaches zero shows as **sold out** on the form and is rejected at checkout, so it can never be oversold.",
      "The first image on a variation is its **cover** in the buyer's option list.",
      "The variation's own image is shown next to the option the buyer picks.",
    ],
  },
  {
    id: "subscription-cycle",
    title: "A subscription cycle on a variation",
    icon: "🔁",
    body: [
      "A **subscription** product can also be variable. Each variation may carry its own billing period (monthly or yearly), trial days and number of charges.",
      "That lets you sell **Size M, monthly** and **Size L, yearly** from one product, each billed on its own terms.",
      "A field you leave empty **inherits** the product's value for that field. This is a real distinction, not a convenience.",
      "**Not set** and **set to the same value as the product** are different states.",
      "A variation with no cycle of its own keeps following the product, so a later change to the product's period still reaches it.",
      "A variation with its own period is **fixed** and does not follow the product.",
      "In the editor each field starts empty with the product's value shown as a hint, and any field can be reset to empty.",
      {
        tip: "Nothing is pre-filled, precisely so a product-level change keeps applying to the variations that never set their own.",
      },
    ],
  },
  {
    id: "checkout",
    title: "What happens at checkout",
    icon: "🛒",
    body: [
      "A product that has variations must be ordered with one. You cannot buy the **product-level price** while variations exist.",
      "That holds for the form, for the menu, and for a checkout request crafted by hand.",
      "The buyer picks one option, the **variation's price** is charged, and the variation's stock is reduced.",
      "When the product is a subscription, it is created with the **cycle of the chosen variation**, falling back to the product's for any field the variation does not set.",
      "The terms are recorded on the sale at checkout, so a later change to the product or the variation never alters an existing subscription's schedule.",
      "The recurring engine applies the same rule on every later charge, so a variation's **charge count** is honoured for the whole plan, not only the first payment.",
      "One purchase makes one subscription. A cart that mixes a subscription variation with one-time items bills each on its own terms.",
    ],
  },
  {
    id: "menu",
    title: "On the restaurant menu",
    icon: "🍽️",
    body: [
      "A variable dish shows its options on the card, each with its own price. A **sold-out option** cannot be picked.",
      "The buyer cannot add the dish to the basket until they have chosen. The tap adds the chosen option at that option's price.",
      "A dish is never ordered at a price the buyer did not choose.",
      "Stock on the menu is **re-read live**, not frozen at page load, so a dish that sells out while the page is open is refused when it is picked.",
      "The tap asks the server what is buyable now rather than trusting the page it was served.",
      "A sold-out simple dish gets the same button as an in-stock one, so the tap checks first and tells the buyer the dish sold out.",
      "A basket line that sold out while it sat there is dropped on return with a notice naming the dish. Nothing is oversold.",
      {
        note: "If the live check cannot answer, the page keeps the state it was rendered with, so a menu still works when the connection does not.",
      },
    ],
  },
  {
    id: "mcp",
    title: "Managing variations from an AI agent",
    icon: "🤖",
    body: [
      "Every variation action is available over **MCP**.",
      {
        list: [
          "`list_product_variations` reads a product's variations.",
          "`create_product_variation` adds one, `update_product_variation` edits one, and `delete_product_variation` removes one.",
          "`create_product` and `update_product` accept a full **variations** list when you want to set the whole set at once.",
        ],
      },
      "In a tool result, **billing_period**, **trial_days** and **subscription_charges** appear only when the variation sets them itself.",
      "Absent means it inherits the product, so an agent can always tell the two states apart.",
      "To go back to inheriting, clear the field with `clear_billing_period`, `clear_trial_days` or `clear_subscription_charges` rather than setting the product's current value.",
    ],
  },
];

export default function VariationsDocsPage() {
  return (
    <DocsPage
      title="Variation products"
      lead="Sell one product in several options, each with its own price, stock and image, and each able to carry its own subscription cycle."
      sections={SECTIONS}
    />
  );
}
