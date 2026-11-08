// Card t_d5f359c7 — every surface that shows a PRICE must label it in the
// workspace's currency, and no surface may reintroduce a hardcoded "RM".
//
// THE BUG (found while fixing t_06a16aeb): seven files each carried their own
// copy of
//
//     const money = (sen: number) =>
//       `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;
//
// and painted it over an amount denominated in `workspace_settings.currency`.
// A workspace set to USD (issue #185) therefore read "RM12.00" for a US$12.00
// product on /products, /products/[editor], /subscriptions, /spam, /builder,
// /form-settings and inside the menu ProductPicker — the number right, the
// money wrong. The API never sent the workspace currency to any of those pages,
// which is why the fix is one payload change (the session bundle) plus the
// shared formatter, not seven local repairs.
//
// WHAT THIS GUARD COVERS
//   1. formatPrice / currencyLabel: a PRICE is labelled from the workspace's
//      currency; an UNKNOWN currency (bundle still in flight) renders "—"/"…"
//      and never a guessed MYR, because a guessed RM is the defect itself.
//   2. A repo-wide scan: no file under web/src may contain the literal
//      `RM${…}` money helper (comments stripped, test files excluded), so the
//      NEXT page cannot quietly reintroduce an eighth copy.
//   3. The seven sites: each imports the shared formatter and labels its
//      amount with the workspace currency (or, for /spam, with the SALE's own
//      currency — the spam queue lists recorded sales, not prices).
//   4. The wire contract: GET /v1/rest/dashboard-bundle carries the active
//      workspace's `currency`, and the spam queue's sale rows carry the
//      currency each sale was recorded in.
//   5. The field labels: "Price (RM)" is a translated string, so it names the
//      workspace's money too — the six keys that used to spell RM must
//      interpolate `{cur}`.
//
// WHAT IT DOES NOT COVER: the rendered pixels. That is checked by driving the
// pages in a browser against a USD workspace (handoff evidence), not here.
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { currencyLabel, formatMoney, formatPrice } from "./money.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// The Go files this guard pins live outside web/, so they are read relative to
// the REPO root. Written as a `HERE`-based helper so buildContext.test's scan
// resolves the literals and can prove the web image actually carries api/
// (`COPY api/ /repo/api/`) — a host-only read would pass here and die in the
// image.
const HERE = new URL("../../../", import.meta.url);
const readRepo = (rel: string) => readFileSync(fileURLToPath(new URL(rel, HERE)), "utf8");

// Strip comments so a guard reads code, not the explanation of the bug.
function code(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((line) => !line.trim().startsWith("//"))
    .join("\n");
}

const money = read("money.ts");
const session = read("../components/session.tsx");
const dashboardGo = readRepo("api/internal/httpapi/rest_dashboard.go");
const spamGo = readRepo("api/internal/httpapi/rest_spam.go");
const i18n = read("../components/i18nDict.ts");

// web/src is copied into the web image wholesale (`COPY web/ ./` in
// web/Dockerfile), so a src-relative read needs no per-path COPY: only the two
// Go files above had to be routed through `readRepo`.
const readSrc = (rel: string) => read(`../${rel}`);

// Every file a browser can render, plus every helper it imports.
const sourceFiles: string[] = readdirSync(fileURLToPath(new URL("../", import.meta.url)), { recursive: true, encoding: "utf8" })
  .filter((f) => /\.(ts|tsx)$/.test(f))
  .filter((f) => !/\.test\.ts$/.test(f))
  .sort();

// ── the formatter ───────────────────────────────────────────────────────────
test("a price is labelled in the workspace's currency", () => {
  // The card's required case: US$12.00 in a workspace set to USD.
  assert.equal(formatPrice(1200, "USD"), "US$12.00");
  assert.notEqual(formatPrice(1200, "USD"), "RM12.00");
  assert.equal(formatPrice(2490, "SGD"), "S$24.90");
  assert.equal(formatPrice(2490, "EUR"), "€24.90");
});

test("the default workspace keeps the shipped MYR string", () => {
  // A currency parameter must not silently restyle the default workspace.
  assert.equal(formatPrice(1200, "MYR"), "RM12.00");
  assert.equal(formatPrice(1200, "MYR"), formatMoney(1200, "MYR"));
});

test("an unresolved currency renders no money at all — never a guessed RM", () => {
  // The bundle is one request; the products list is another. A page whose own
  // fetch lands first must not paint RM over a USD price in the gap.
  for (const unknown of ["", "   ", null, undefined]) {
    assert.equal(formatPrice(1200, unknown), "—", `formatPrice with ${JSON.stringify(unknown)}`);
  }
  assert.equal(currencyLabel(""), "…");
  assert.equal(currencyLabel("USD"), "US$");
  assert.equal(currencyLabel("MYR"), "RM");
});

test("a negative price keeps its sign", () => {
  assert.equal(formatPrice(-1200, "USD"), "-US$12.00");
});

// ── the repo-wide scan ──────────────────────────────────────────────────────
test("no surface under web/src hand-rolls an RM money helper again", () => {
  const offenders: string[] = [];
  for (const f of sourceFiles) {
    const body = code(readSrc(f));
    if (/RM\$\{/.test(body)) offenders.push(f);
    // The same helper written without a template literal.
    if (/["'`]RM["'`]\s*\+/.test(body)) offenders.push(f + " (string concat)");
  }
  assert.deepEqual(
    offenders,
    [],
    "a hardcoded RM helper is back; label the amount with lib/money.ts " +
      "(formatPrice for a workspace-currency PRICE, formatMoney with the row's own currency for a recorded sale)",
  );
});

// ── the seven sites ────────────────────────────────────────────────────────
const priceSites = [
  // file, the expression that must appear (the amount, labelled from the session)
  ["app/products/page.tsx", "formatPrice(sen, currency)"],
  ["app/subscriptions/page.tsx", "formatPrice(sen, currency)"],
  ["app/builder/page.tsx", "formatPrice(sen, currency)"],
  ["app/form-settings/page.tsx", "formatPrice(1200, currency)"],
  ["components/ProductPicker.tsx", "formatPrice(p.price_sen, currency)"],
];

test("every price surface reads the shared formatter and the session's currency", () => {
  for (const [file, call] of priceSites) {
    const body = code(readSrc(file));
    assert.ok(
      body.includes('from "../../lib/money"') || body.includes('from "../lib/money"'),
      `${file} does not import the shared money formatter`,
    );
    assert.ok(body.includes("currency"), `${file} never reads the workspace currency`);
    assert.ok(body.includes(call), `${file} does not label its price with ${call}`);
  }
  // /builder has three price call sites (product option list, canvas card,
  // choice rows) plus the total block's live zero.
  const builder = code(readSrc("app/builder/page.tsx"));
  assert.ok(
    builder.includes('{formatPrice(0, currency)}'),
    "the builder's total block preview prints a hardcoded currency",
  );
});

test("the products editor's price FIELDS name the workspace currency", () => {
  const body = code(readSrc("app/products/ProductEditor.tsx"));
  assert.ok(body.includes("currencyLabel(currency)"), "the price fields' labels do not name the currency");
  assert.ok(!/const money =/.test(body), "ProductEditor carries another hand-rolled money helper");
});

test("the spam queue labels each SALE in the currency it was recorded in", () => {
  // /spam is not a price surface: its only amount is a recorded sale total, so
  // the row's own currency wins (a workspace that switches to USD must not
  // relabel its MYR history).
  const page = code(readSrc("app/spam/page.tsx"));
  assert.ok(page.includes("formatMoney(s.total_sen, s.currency)"), "the spam sale total is not labelled per row");
  assert.ok(!page.includes("formatPrice("), "the spam page labels a sale as if it were a workspace price");
  assert.ok(/currency\?: string;/.test(page), "the spam sale row type lost its currency field");
});

test("the menu picker labels its price too, and reads the CURRENT product scope", () => {
  const body = code(readSrc("components/ProductPicker.tsx"));
  assert.ok(body.includes("formatPrice(p.price_sen, currency)"), "the picker price is not labelled from the session");
  // Defect found while fixing the picker's price (same card, because it made
  // the price unrenderable): `load` is a useCallback with no dependencies, so it
  // used to capture `onlyIds` from the FIRST render — the form-settings page
  // mounts the picker while its product list is still empty, so every open
  // filtered against [] and showed "No active products". The scope must be read
  // through a ref so the callback stays stable AND current.
  assert.ok(body.includes("onlyIdsRef.current"), "the picker's load() captures onlyIds again (stale [] scope)");
  assert.ok(!/\bif \(onlyIds\)/.test(body), "the picker's load() filters by the first render's onlyIds prop");
});

// ── the wire contract ──────────────────────────────────────────────────────
test("the dashboard bundle carries the active workspace's currency", () => {
  assert.match(
    dashboardGo,
    /Currency\s+string\s+`json:"currency,omitempty"`/,
    "the dashboard bundle no longer emits a top-level currency",
  );
  assert.match(dashboardGo, /workspaceCurrency\(ctx, actx\.Account\.ID\)/, "the bundle never fills currency");
  assert.ok(
    !/bundle\.Currency = dbstore\.DEFAULT/.test(dashboardGo),
    "the bundle hardcodes a currency instead of reading the workspace",
  );
  assert.ok(session.includes("bundle?.currency"), "the session context no longer exposes the bundle currency");
});

test("the spam queue's sale rows carry the currency each sale was recorded in", () => {
  assert.match(spamGo, /Currency\s+string\s+`json:"currency"`/, "restSpamSale lost its currency field");
  assert.match(spamGo, /CurrencyOrMYR\(\)/, "the spam sale's currency is not read from the SALE row");
});

// ── the field labels ───────────────────────────────────────────────────────
test("no price field label still spells RM", () => {
  const labels = code(i18n);
  for (const key of ["prod.price", "prod.simplePrice", "prod.variationPrice", "bld.choicePrice", "fs.shipRatePrice", "fs.shipFreeOver"]) {
    const line = labels.split("\n").find((l) => l.trimStart().startsWith(`"${key}":`)) ?? "";
    assert.ok(line !== "", `${key} is missing from the dictionary`);
    assert.ok(!line.includes("(RM)") && !line.includes("（RM）"), `${key} still names a literal RM`);
    assert.ok(line.includes("{cur}"), `${key} no longer names any currency at all`);
  }
});

test("the shared formatter is the only place a currency symbol is chosen", () => {
  // One table, in money.ts. A second symbol table anywhere else is how the
  // literal RM got copied seven times in the first place.
  assert.ok(money.includes('MYR: "RM"'), "money.ts lost its symbol table");
  const offenders = sourceFiles.filter((f) => f !== "lib/money.ts" && /MYR:\s*"RM"/.test(code(readSrc(f))));
  assert.deepEqual(offenders, [], "a second currency-symbol table appeared");
});
