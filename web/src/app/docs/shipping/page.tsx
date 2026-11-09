import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Shipping by zone — JomForm Docs",
  description:
    "Charge a delivery fee per delivery zone: workspace-managed zones (state, district or postcode range) with rate rows, weight bands and a free-shipping threshold, resolved from the buyer's address at checkout into sales.shipping_sen.",
  path: "/docs/shipping",
});

// SHORT ON PURPOSE. Every tool name, field name, threshold and rule survives,
// because /docs is what a developer greps. The section ids are a contract with
// /docs and with the builder's inline "?" links, so they do not move.
const SECTIONS: Section[] = [
  {
    id: "setup",
    title: "Setting it up",
    icon: "⚙️",
    body: [
      {
        steps: [
          "**Create a zone set** in Form Settings → Shipping. Name it, tick Default if it should be offered first, and several forms can share it.",
          "**Add zones**. Each zone has a name and at least one rule: a state list, a postcode range or a district list. Any one match is enough.",
          "**Price each zone** with at least one rate row: ringgit, stored as integer sen, an optional name and an optional weight band in grams.",
          "**Point the form's shipping block** at the set and map which blocks carry the state, district and postcode, adding a select if needed. With no mapping the client envelope applies.",
          "**Decide the no-match outcome**: Reject refuses the order and creates no sale; Default charges the fallback zone's rate. There is no third behaviour.",
          "**Optional: set a free-over amount** on a rate row. Once the items subtotal reaches it the fee is zero, and the sale records the threshold crossed.",
        ],
      },
      "**Priority** orders the zones, lowest first, so a postcode exception is tested before the state zone around it. The fallback tick is used only when no zone matches.",
      {
        note: "A set is **refused for deletion** while a form's shipping block points at it, and the refusal names the form. Re-point the block first.",
      },
    ],
  },
  {
    id: "zones",
    title: "Zones and rate sources",
    icon: "🗺️",
    body: [
      "A shipping block has a **rate source**. **Static list** is the default and unchanged: it keeps its own choices and prices, and the chosen price is a line item.",
      "**Managed zones** renders your zone set: each zone with the price of its first rate. The buyer's pick confirms where they are, and the address sets the fee.",
      "A managed block carries **no price of its own**, so its `price_sen` and `choices` are never read. Save it with the source, zone set and address field names.",
      {
        warn: "A managed shipping block is a **delivery fee**, not something you sell, so a form whose only pricing block is one cannot take payment.",
      },
      "Publishing is allowed so you can add products later, but checkout refuses with `tiada produk untuk dibayar` (no product to pay for) rather than charge postage on an empty cart.",
      "Add a **product block** before you share the link, or every checkout is refused.",
      "A zone matches on any **one** rule: a state, a postcode range or a district name. Zones run by **priority**, lowest first.",
      {
        note: "A state entry accepts any Malaysian spelling (Johor, Penang, Pulau Pinang, KL, Melaka, Malacca), or the region tokens `peninsular` and `east_malaysia`.",
      },
    ],
  },
  {
    id: "no-match",
    title: "An address outside every zone",
    icon: "🚫",
    body: [
      "The outcome is always **explicit**. **Reject** refuses the order and tells the buyer delivery is not available at that address, and no sale is recorded.",
      "**Default** charges the rate of the zone ticked as the fallback in the set, and refuses if the set has no fallback zone.",
      {
        warn: "A zone-based fee is **never silently zero**. There is no third behaviour, so an address you did not price is refused, not undercharged.",
      },
      "The same rule covers a managed block whose set is missing or whose zone has no rate: checkout refuses and names the reason.",
    ],
  },
  {
    id: "weights",
    title: "Weight bands and the free-shipping threshold",
    icon: "⚖️",
    body: [
      "A rate row can carry a **minimum** and a **maximum** weight in grams. With a declared parcel weight, the row whose band contains it is the fee.",
      "A weight outside every band is **refused**, and a zone whose rows carry no bands keeps its first rate whatever the weight.",
      "A row may also carry a **free-over** amount: once the items subtotal reaches it the fee is zero, and the sale records the threshold crossed.",
      {
        note: "Rows are tried in order (Sort, then cheapest), so with no weight band the first row is the fee.",
      },
    ],
  },
  {
    id: "what-lands-on-the-sale",
    title: "What lands on the sale",
    icon: "🧾",
    body: [
      "**The fee is shipping, not a line item.** `sales.shipping_sen` carries it, and the total is items subtotal + shipping − discount, the same money rule every sale uses.",
      "A merchant's report of sold items stays accurate, because a delivery charge never appears as a product line.",
      "**Every resolved fee is recorded**: the zone and rate that applied, which rule matched, the list price, and the free-shipping threshold when crossed.",
      {
        note: "The match is one of **state**, **postcode**, **district**, **fallback** or **the buyer's own choice**, and the sale detail shows it.",
      },
      "**The address is the price authority.** The zone comes from the submitted address, and the form's shipping select shows its price before payment.",
      "If the buyer's choice disagrees with the address, the order is **refused** rather than re-priced behind their back. Checkout only charges the fee shown.",
    ],
  },
  {
    id: "mcp",
    title: "Managing it with an agent (MCP)",
    icon: "🤖",
    body: [
      "Zone sets and **rate rows** are on the JomForm MCP, so an agent can build a rate card for you. Money is integer sen and weight is grams.",
      {
        warn: "A set the payload **omits is deleted**, and that delete is refused while a form still points at it.",
      },
      {
        table: {
          label: "Shipping rate MCP tools",
          head: ["Tool", "Arguments", "What it does"],
          rows: [
            [
              "`get_shipping_rates`",
              "`workspace_id` (optional)",
              "Reads every zone set: match rules (states, districts, postcode range, priority, fallback, lat/lng) and rate rows (price in sen, weight band, free-over threshold).",
            ],
            [
              "`set_shipping_rates`",
              "`sets`",
              "Replaces the whole configuration in one call. A set sent with its id keeps it, so forms keep resolving; an omitted set is deleted unless a form uses it.",
            ],
          ],
        },
      },
      "An **empty sets list** removes every set the account has, which is the same state Form Settings → Shipping calls “No zone set yet”.",
      "The same configuration is on the REST API as `GET /v1/rest/shipping` and `PUT /v1/rest/shipping`.",
    ],
  },
  {
    id: "couriers",
    title: "Courier quotes (Lalamove, EasyParcel)",
    icon: "🚚",
    body: [
      "With **Courier quote** the fee comes from a real courier, and the attached zone set becomes the fallback. The form's price is an estimate and the quoted fee is final.",
      {
        list: [
          "**Lalamove** prices an intra-city route: Kuala Lumpur, Johor Bahru, Penang and the other cities it serves.",
          "**EasyParcel** prices a nationwide parcel across its courier network.",
        ],
      },
      "**Both figures** are recorded on the sale, and each zone can carry its latitude and longitude for a route-based quote.",
      "Connect a courier in **Form Settings → Shipping → Courier providers**. Lalamove goes live once you give its pickup point.",
      "EasyParcel asks the **account owner** to authorise by opening a link on their own device. Consent is theirs; JomForm never sees or asks for a password.",
      {
        warn: "A quote is short-lived. If the courier is not connected, cannot cover the address, fails or is slow, the zone rate is charged and the sale records which applied.",
      },
      "A **failed quote** is never a silent zero, and a courier block with no zone set is refused rather than charged nothing.",
      "A **sandbox connection** is labelled, and the sale records the environment. A simulated rate is never taken for a real one; without production keys the provider reads not configured.",
    ],
  },
  {
    id: "courier-mcp",
    title: "Connecting a courier with an agent (MCP)",
    icon: "🔌",
    body: [
      "The **courier surface** is on the MCP too, so an agent can connect a provider without the dashboard.",
      {
        table: {
          label: "Courier MCP tools",
          head: ["Tool", "Arguments", "What it does"],
          rows: [
            [
              "`get_shipping_couriers`",
              "`workspace_id` (optional)",
              "Lists lalamove and easyparcel for the workspace: whether the server holds credentials (never the value), whether the workspace connected it, the environment, the pickup point, and what is still needed.",
            ],
            [
              "`connect_shipping_courier`",
              "`provider`, `origin_postcode` + `origin_state`, or `origin_lat` + `origin_lng`",
              "Connects a provider. Give the pickup address: postcode and state for EasyParcel, latitude and longitude for Lalamove. EasyParcel returns an authorise link the owner must open on their own device.",
            ],
            [
              "`disconnect_shipping_courier`",
              "`provider`",
              "Disconnects a provider and deletes the stored authorisation, so a revoked account can never be quoted with. The pickup address is kept, so reconnecting is one call.",
            ],
          ],
        },
      },
      {
        warn: "An agent must **never open** the authorise link, type a password or ask for one. The link expires; calling the tool again issues a fresh one.",
      },
      "The same surface is on the REST API as `GET /v1/rest/shipping/couriers`, `POST /v1/rest/shipping/couriers/{provider}/connect` and `POST /v1/rest/shipping/couriers/{provider}/disconnect`.",
      "The EasyParcel OAuth return is `/api/shipping/easyparcel/callback`, which is the URL registered in the EasyParcel Developer Hub.",
    ],
  },
];

export default function ShippingDocsPage() {
  return (
    <DocsPage
      title="Shipping by zone"
      lead="One set of delivery zones shared by your forms, each with its own price, resolved from the buyer's address at checkout."
      sections={SECTIONS}
    />
  );
}
