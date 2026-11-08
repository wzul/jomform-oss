// Pay-what-you-want (`type: "custom_amount"`) amounts — the ONE place the
// Products page turns typed money into the write payload.
//
// WHY THIS MODULE EXISTS (card t_7eb3b623)
//   Migration 000085 shipped custom_amount with REST + MCP create support and a
//   buyer-side amount input, but the merchant UI could not create one: the list
//   page's add row and its inline edit dialog offered only one_time and
//   subscription, and <option value="custom_amount"> lived on /products/new —
//   a route nothing linked to. The type was reachable only by typing a URL or
//   calling the API.
//
//   Making it reachable is not only "add an <option>". A custom_amount product
//   has NO asking price: `createProduct` accepts a request with no `price_sen`
//   for this type and derives the display price from the limits
//   (`rest_handlers.go`, migration 000085's header). The list page, by contrast,
//   opened `add()` by parsing a price that was a required field, so wiring the
//   option in without touching the payload would have created every donation
//   product at RM0.00 — or, with the price left blank, refused the request
//   before it left the browser.
//
//   The unit conversion therefore lives here, with no JSX and no fetch, so that
//   "a custom_amount payload carries limits and NO price_sen" is something a
//   test can assert directly instead of something a reviewer has to read out of
//   a 1,600-line component.
//
// WHAT THIS MODULE DOES NOT DO: it does not judge the limits. The floor (RM1.00,
// `payments.MinCustomAmountSen`), min <= max and min <= suggested are decided
// server-side (`httpapi/custom_amount.go`), and the UI surfaces that refusal
// verbatim rather than keeping a second copy of the rule. Here a value is only
// refused when it is not money at all (non-finite / negative), which is a
// PARSE failure, not a policy one.

/** Integer sen, matching the API's unit for every price field. */
export type Sen = number;

/**
 * parseRmToSen converts a typed amount into integer sen.
 *
 *   ""            -> null  (blank: "not set", e.g. an optional maximum)
 *   "12.34"       -> 1234
 *   "1e999"       -> undefined (Infinity serialises to null in JSON, which Go
 *                              decodes into a non-pointer int64 as 0 — the
 *                              RM 0.00 product of issue #551)
 *   "RM10" / "-1" -> undefined
 *
 * Money is integer sen everywhere in this repo, and this is the single place the
 * Products page performs the conversion.
 */
export function parseRmToSen(v: string): Sen | null | undefined {
  const s = (v ?? "").trim();
  if (s === "") return null;
  const n = Number(s);
  if (!Number.isFinite(n) || n < 0) return undefined;
  const sen = Math.round(n * 100);
  if (!Number.isFinite(sen) || sen < 0) return undefined;
  return sen;
}

/** The limits a custom_amount product is written with, in integer sen. */
export type CustomAmountPayload = {
  min_price_sen: Sen;
  max_price_sen: Sen | null;
  suggested_price_sen: Sen | null;
};

export type CustomAmountParse =
  | { ok: true; body: CustomAmountPayload }
  | { ok: false; field: "min" | "max" | "suggested" };

/**
 * customAmountBody builds the amount part of a create/update request for a
 * custom_amount product.
 *
 * THE PAYLOAD CARRIES NO `price_sen`, deliberately: for this type the price is
 * a DISPLAY value derived server-side from the suggestion (else the floor), so
 * sending one would be a bogus asking price — and on create it is not even
 * required. `min_price_sen` is required by the server for this type, so a blank
 * minimum is a refusal here rather than a request the API has to reject.
 */
export function customAmountBody(
  min: string,
  max: string,
  suggested: string,
): CustomAmountParse {
  const minSen = parseRmToSen(min);
  if (minSen === null || minSen === undefined) return { ok: false, field: "min" };
  const maxSen = parseRmToSen(max);
  if (maxSen === undefined) return { ok: false, field: "max" };
  const sugSen = parseRmToSen(suggested);
  if (sugSen === undefined) return { ok: false, field: "suggested" };
  return {
    ok: true,
    body: { min_price_sen: minSen, max_price_sen: maxSen, suggested_price_sen: sugSen },
  };
}

/** The three amount fields as typed text (RM), seeded from a stored product. */
export type AmountInputs = { min: string; max: string; suggested: string };

/**
 * limitsToInputs seeds the three amount fields from a product document.
 *
 * A product that is NOT custom_amount stores 0 in `min_price_sen`, which the
 * server would refuse as a minimum — so the field starts at the RM1.00 floor
 * (the same seed ProductEditor uses), meaning switching this product to
 * pay-what-you-want begins from a value the server accepts. An unset maximum /
 * suggestion stays BLANK so the field reads as optional; `null` and 0 must not
 * be conflated here, because 0 is not a usable maximum.
 */
export function limitsToInputs(p: {
  min_price_sen?: Sen | null;
  max_price_sen?: Sen | null;
  suggested_price_sen?: Sen | null;
}): AmountInputs {
  const minSen =
    typeof p.min_price_sen === "number" && p.min_price_sen >= 100 ? p.min_price_sen : 100;
  return {
    min: (minSen / 100).toFixed(2),
    max: typeof p.max_price_sen === "number" ? (p.max_price_sen / 100).toFixed(2) : "",
    suggested:
      typeof p.suggested_price_sen === "number"
        ? (p.suggested_price_sen / 100).toFixed(2)
        : "",
  };
}
